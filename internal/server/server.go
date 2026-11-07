// Package server wires the embedded API server: scheme, storage, listeners
// and access.
package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.etcd.io/etcd/server/v3/embed"
	"go.uber.org/zap"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apiserver/pkg/authentication/authenticator"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/authorization/authorizerfactory"
	openapinamer "k8s.io/apiserver/pkg/endpoints/openapi"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/registry/rest"
	genericapiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/apiserver/pkg/storage/storagebackend"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/client-go/util/cert"
	basecompatibility "k8s.io/component-base/compatibility"
	baseversion "k8s.io/component-base/version"

	"mkm.pub/dennet/internal/registry"
	denv1 "mkm.pub/dennet/pkg/apis/den/v1alpha1"
	"mkm.pub/dennet/pkg/generated/openapi"
)

var (
	Scheme = runtime.NewScheme()
	Codecs = serializer.NewCodecFactory(Scheme)
)

func init() {
	utilruntime.Must(denv1.AddToScheme(Scheme))
	utilruntime.Must(coordinationv1.AddToScheme(Scheme))
	// No separate internal types: the versioned ones double as internal.
	Scheme.AddKnownTypes(schema.GroupVersion{Group: coordinationv1.GroupName, Version: runtime.APIVersionInternal},
		&coordinationv1.Lease{}, &coordinationv1.LeaseList{})
	Scheme.AddKnownTypes(corev1.SchemeGroupVersion, &corev1.Namespace{}, &corev1.NamespaceList{})
	Scheme.AddKnownTypes(schema.GroupVersion{Version: runtime.APIVersionInternal}, &corev1.Namespace{}, &corev1.NamespaceList{})

	unversioned := schema.GroupVersion{Version: "v1"}
	metav1.AddToGroupVersion(Scheme, unversioned)
	Scheme.AddUnversionedTypes(unversioned, &metav1.Status{}, &metav1.APIVersions{},
		&metav1.APIGroupList{}, &metav1.APIGroup{}, &metav1.APIResourceList{})
}

type Options struct {
	// StateDir holds the token, serving certificate, kubeconfig and the
	// etcd data dir.
	StateDir string
	// Socket is the Unix socket path. Access over it needs no token.
	Socket string
	// TCPAddr, if set, is a TLS listener that requires the user token.
	TCPAddr string
	// Den is the den binary started for each Den that has no den running.
	// Empty starts none; deleting a Den still stops its den.
	Den string
}

type localConnKey struct{}

// Run serves the API until ctx is cancelled.
func Run(ctx context.Context, o Options) error {
	if err := os.MkdirAll(o.StateDir, 0o700); err != nil {
		return err
	}
	// Only TCP needs the token; the Unix socket is guarded by its file mode.
	var token string
	if o.TCPAddr != "" {
		var err error
		if token, err = loadOrCreateToken(filepath.Join(o.StateDir, "token")); err != nil {
			return err
		}
	}

	// Listen first: it fails fast when another dennet is running, before
	// etcd waits on the lock of the data dir that one holds.
	unixLn, err := listenUnix(o.Socket)
	if err != nil {
		return err
	}
	defer os.Remove(o.Socket)
	etcd, etcdURL, closeEtcd, err := startEtcd(o.StateDir)
	if err != nil {
		unixLn.Close()
		return err
	}
	defer closeEtcd()
	fmt.Fprintf(os.Stderr, "dennet: storing objects in %s (stop dennet and delete it to start from nothing)\n", etcd.Config().Dir)

	srv, err := newAPIServer(token, o.Socket, etcdURL)
	if err != nil {
		unixLn.Close()
		return err
	}
	// Runs before etcd.Close (defers are LIFO): close the API server's etcd
	// clients first, or they log reconnect errors while dennet exits.
	defer srv.Destroy()
	prepared := srv.PrepareRun()
	handler := prepared.Handler

	errc := make(chan error, 4)
	go func() { errc <- <-etcd.Err() }()
	unixSrv := &http.Server{
		Handler:   handler,
		Protocols: new(http.Protocols),
		ConnContext: func(ctx context.Context, _ net.Conn) context.Context {
			return context.WithValue(ctx, localConnKey{}, true)
		},
	}
	// HTTP/2 without TLS lets one ssh connection carry all of a remote
	// dennet's requests (see remote.go).
	unixSrv.Protocols.SetHTTP1(true)
	unixSrv.Protocols.SetUnencryptedHTTP2(true)
	go func() { errc <- serve(unixSrv.Serve(unixLn)) }()
	fmt.Fprintf(os.Stderr, "dennet: serving on unix://%s\n", o.Socket)

	servers := []*http.Server{unixSrv}
	if o.TCPAddr != "" {
		tcpSrv, url, err := serveTLS(o, handler, token, errc)
		if err != nil {
			return err
		}
		servers = append(servers, tcpSrv)
		fmt.Fprintf(os.Stderr, "dennet: serving on %s (KUBECONFIG=%s)\n", url, filepath.Join(o.StateDir, "kubeconfig"))
	}

	// ponytail: RunWithContext never returns without its own SecureServing
	// (its listener-stopped channel is nil), so nothing waits for it and its
	// deferred Destroy never runs; Run calls Destroy itself.
	go func() { errc <- prepared.RunWithContext(ctx) }()
	go runDens(ctx, o.Den, o.Socket)

	select {
	case err = <-errc:
	case <-ctx.Done():
	}
	for _, s := range servers {
		s.Close()
	}
	return err
}

// startEtcd runs etcd in this process with its data in <stateDir>/etcd,
// reachable only over a Unix socket in stateDir (mode 0700). close stops
// it without the errors etcd logs about its own listeners closing.
func startEtcd(stateDir string) (e *embed.Etcd, clientURL string, close func(), err error) {
	cfg := embed.NewConfig()
	cfg.Dir = filepath.Join(stateDir, "etcd")
	logCfg := zap.NewProductionConfig()
	logCfg.Level = zap.NewAtomicLevelAt(zap.ErrorLevel)
	lg, err := logCfg.Build()
	if err != nil {
		return nil, "", nil, err
	}
	cfg.ZapLoggerBuilder = embed.NewZapLoggerBuilder(lg)
	client, _ := url.Parse("unix://" + filepath.Join(stateDir, "etcd.sock"))
	cfg.ListenClientUrls, cfg.AdvertiseClientUrls = []url.URL{*client}, []url.URL{*client}
	// A single member needs no peers: listen for none. The advertised peer
	// URL only names this member in the cluster config.
	cfg.ListenPeerUrls = nil
	cfg.InitialCluster = cfg.InitialClusterFromName(cfg.Name)
	if e, err = embed.StartEtcd(cfg); err != nil {
		return nil, "", nil, fmt.Errorf("etcd: %w", err)
	}
	close = func() {
		logCfg.Level.SetLevel(zap.FatalLevel)
		e.Close()
	}
	select {
	case <-e.Server.ReadyNotify():
		return e, client.String(), close, nil
	case <-time.After(time.Minute):
		close()
		return nil, "", nil, errors.New("etcd did not become ready")
	}
}

func serve(err error) error {
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func newAPIServer(token, socket, etcdURL string) (*genericapiserver.GenericAPIServer, error) {
	optsFor := func(gv schema.GroupVersion) generic.RESTOptionsGetter {
		cfg := storagebackend.NewDefaultConfig("/registry", Codecs.LegacyCodec(gv))
		cfg.Transport.ServerList = []string{etcdURL}
		return restOptions{cfg}
	}
	ns, err := registry.NewNamespace(Scheme, optsFor(corev1.SchemeGroupVersion))
	if err != nil {
		return nil, err
	}
	if err := ns.Ensure(context.Background(), metav1.NamespaceDefault); err != nil {
		return nil, err
	}

	hosts, err := registry.NewHost(Scheme, optsFor(denv1.SchemeGroupVersion))
	if err != nil {
		return nil, err
	}
	hosts.AfterDelete = ns.DeleteOwned

	cfg := genericapiserver.NewConfig(Codecs)
	// Inside authentication, which runs before any request reaches a Host.
	cfg.BuildHandlerChainFunc = func(h http.Handler, c *genericapiserver.Config) http.Handler {
		return genericapiserver.DefaultBuildHandlerChain(&remote{next: h, hosts: hosts}, c)
	}
	cfg.AdmissionControl = ns.AutoCreate()
	cfg.EffectiveVersion = basecompatibility.NewEffectiveVersionFromString(baseversion.DefaultKubeBinaryVersion, "", "")
	cfg.ExternalAddress = "localhost:0" // only shown in discovery
	cfg.LoopbackClientConfig = &restclient.Config{
		Host: "http://dennet",
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		},
	}
	cfg.Authentication.Authenticator = authn(token)
	cfg.Authorization.Authorizer = authorizerfactory.NewAlwaysAllowAuthorizer()
	namer := openapinamer.NewDefinitionNamer(Scheme)
	cfg.OpenAPIConfig = genericapiserver.DefaultOpenAPIConfig(openapi.GetOpenAPIDefinitions, namer)
	cfg.OpenAPIConfig.Info.Title, cfg.OpenAPIConfig.Info.Version = "dennet", "v1alpha1"
	cfg.OpenAPIV3Config = genericapiserver.DefaultOpenAPIV3Config(openapi.GetOpenAPIDefinitions, namer)
	cfg.OpenAPIV3Config.Info.Title, cfg.OpenAPIV3Config.Info.Version = "dennet", "v1alpha1"

	srv, err := cfg.Complete(nil).New("dennet", genericapiserver.NewEmptyDelegate())
	if err != nil {
		return nil, err
	}
	den, denStatus, err := registry.NewDen(Scheme, optsFor(denv1.SchemeGroupVersion))
	if err != nil {
		return nil, err
	}
	space, err := registry.NewSpace(Scheme, optsFor(denv1.SchemeGroupVersion))
	if err != nil {
		return nil, err
	}
	surface, err := registry.NewSurface(Scheme, optsFor(denv1.SchemeGroupVersion))
	if err != nil {
		return nil, err
	}
	lease, err := registry.NewLease(Scheme, optsFor(coordinationv1.SchemeGroupVersion))
	if err != nil {
		return nil, err
	}

	denGroup := genericapiserver.NewDefaultAPIGroupInfo(denv1.GroupName, Scheme, metav1.ParameterCodec, Codecs)
	denGroup.VersionedResourcesStorageMap[denv1.SchemeGroupVersion.Version] = map[string]rest.Storage{
		"dens":        den,
		"dens/status": denStatus,
		"dens/log":    registry.NewDenLog(den),
		"spaces":      space,
		"surfaces":    surface,
		"hosts":       hosts,
	}
	leaseGroup := genericapiserver.NewDefaultAPIGroupInfo(coordinationv1.GroupName, Scheme, metav1.ParameterCodec, Codecs)
	leaseGroup.VersionedResourcesStorageMap[coordinationv1.SchemeGroupVersion.Version] = map[string]rest.Storage{
		"leases": lease,
	}
	if err := srv.InstallAPIGroups(&denGroup, &leaseGroup); err != nil {
		return nil, err
	}
	coreGroup := genericapiserver.NewDefaultAPIGroupInfo(corev1.GroupName, Scheme, metav1.ParameterCodec, Codecs)
	coreGroup.VersionedResourcesStorageMap["v1"] = map[string]rest.Storage{"namespaces": ns}
	if err := srv.InstallLegacyAPIGroup(genericapiserver.DefaultLegacyAPIPrefix, &coreGroup); err != nil {
		return nil, err
	}
	return srv, nil
}

type restOptions struct{ cfg *storagebackend.Config }

// ponytail: no watch cache, every watch is its own etcd watch; add
// genericregistry.StorageWithCacher if watchers ever number in the hundreds.
func (r restOptions) GetRESTOptions(gr schema.GroupResource, _ runtime.Object) (generic.RESTOptions, error) {
	return generic.RESTOptions{
		StorageConfig:  r.cfg.ForResource(gr),
		Decorator:      generic.UndecoratedStorage,
		ResourcePrefix: "/" + gr.Group + "/" + gr.Resource,
	}, nil
}

// authn trusts every request on the Unix socket (guarded by file mode 0600)
// and requires the user token on TCP.
// ponytail: one user, all-powerful; per-den tokens and policy come in milestone 5.
func authn(token string) authenticator.Request {
	admin := &authenticator.Response{User: &user.DefaultInfo{
		Name:   "dennet-user",
		Groups: []string{user.SystemPrivilegedGroup, user.AllAuthenticated},
	}}
	return authenticator.RequestFunc(func(req *http.Request) (*authenticator.Response, bool, error) {
		if req.Context().Value(localConnKey{}) != nil {
			return admin, true, nil
		}
		got, ok := strings.CutPrefix(req.Header.Get("Authorization"), "Bearer ")
		// An empty token (no --tcp) must never match "Bearer ".
		if ok && token != "" && subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1 {
			return admin, true, nil
		}
		return nil, false, nil
	})
}

func listenUnix(path string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	// A leftover socket from a previous run blocks Listen; a live one
	// means another dennet is running.
	if c, err := net.Dial("unix", path); err == nil {
		c.Close()
		return nil, fmt.Errorf("another dennet is serving on %s", path)
	}
	os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}

func serveTLS(o Options, handler http.Handler, token string, errc chan<- error) (*http.Server, string, error) {
	certPEM, keyPEM, err := loadOrCreateCert(o.StateDir, o.TCPAddr)
	if err != nil {
		return nil, "", err
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, "", err
	}
	ln, err := net.Listen("tcp", o.TCPAddr)
	if err != nil {
		return nil, "", err
	}
	url := "https://" + ln.Addr().String()
	if err := writeKubeconfig(filepath.Join(o.StateDir, "kubeconfig"), url, certPEM, token); err != nil {
		ln.Close()
		return nil, "", err
	}
	s := &http.Server{Handler: handler, TLSConfig: &tls.Config{Certificates: []tls.Certificate{pair}, NextProtos: []string{"h2", "http/1.1"}}}
	go func() { errc <- serve(s.ServeTLS(ln, "", "")) }()
	return s, url, nil
}

func loadOrCreateToken(path string) (string, error) {
	if b, err := os.ReadFile(path); err == nil {
		return strings.TrimSpace(string(b)), nil
	}
	b := make([]byte, 32)
	rand.Read(b)
	t := hex.EncodeToString(b)
	return t, os.WriteFile(path, []byte(t+"\n"), 0o600)
}

// loadOrCreateCert keeps a self-signed certificate in the state dir, so the
// kubeconfig stays valid across restarts.
func loadOrCreateCert(dir, addr string) ([]byte, []byte, error) {
	certPath, keyPath := filepath.Join(dir, "serving.crt"), filepath.Join(dir, "serving.key")
	c, cerr := os.ReadFile(certPath)
	k, kerr := os.ReadFile(keyPath)
	if cerr == nil && kerr == nil {
		return c, k, nil
	}
	ips := []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback}
	if host, _, err := net.SplitHostPort(addr); err == nil {
		if ip := net.ParseIP(host); ip != nil && !ip.IsUnspecified() {
			ips = append(ips, ip)
		}
	}
	c, k, err := cert.GenerateSelfSignedCertKey("localhost", ips, nil)
	if err != nil {
		return nil, nil, err
	}
	if err := os.WriteFile(keyPath, k, 0o600); err != nil {
		return nil, nil, err
	}
	return c, k, os.WriteFile(certPath, c, 0o600)
}

func writeKubeconfig(path, server string, ca []byte, token string) error {
	cfg := clientcmdapi.NewConfig()
	cfg.Clusters["dennet"] = &clientcmdapi.Cluster{Server: server, CertificateAuthorityData: ca}
	cfg.AuthInfos["dennet"] = &clientcmdapi.AuthInfo{Token: token}
	cfg.Contexts["dennet"] = &clientcmdapi.Context{Cluster: "dennet", AuthInfo: "dennet", Namespace: "default"}
	cfg.CurrentContext = "dennet"
	return clientcmd.WriteToFile(*cfg, path)
}
