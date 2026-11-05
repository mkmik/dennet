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
	"os"
	"path/filepath"
	"strings"

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

	"mkm.pub/dennet/internal/manifests"
	"mkm.pub/dennet/internal/memstore"
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
	// StateDir holds the token, serving certificate and kubeconfig.
	StateDir string
	// Socket is the Unix socket path. Access over it needs no token.
	Socket string
	// TCPAddr, if set, is a TLS listener that requires the user token.
	TCPAddr string
	// Manifests, if set, is a directory whose YAML and JSON files are
	// applied as they change.
	Manifests string
}

type localConnKey struct{}

// Run serves the API until ctx is cancelled.
func Run(ctx context.Context, o Options) error {
	// Only TCP needs the token; the Unix socket is guarded by its file mode.
	var token string
	if o.TCPAddr != "" {
		if err := os.MkdirAll(o.StateDir, 0o700); err != nil {
			return err
		}
		var err error
		if token, err = loadOrCreateToken(filepath.Join(o.StateDir, "token")); err != nil {
			return err
		}
	}

	srv, err := newAPIServer(token, o.Socket, o.Manifests)
	if err != nil {
		return err
	}
	prepared := srv.PrepareRun()
	handler := prepared.Handler

	errc := make(chan error, 3)

	unixLn, err := listenUnix(o.Socket)
	if err != nil {
		return err
	}
	unixSrv := &http.Server{
		Handler: handler,
		ConnContext: func(ctx context.Context, _ net.Conn) context.Context {
			return context.WithValue(ctx, localConnKey{}, true)
		},
	}
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

	go func() { errc <- prepared.RunWithContext(ctx) }()

	select {
	case err = <-errc:
	case <-ctx.Done():
	}
	for _, s := range servers {
		s.Close()
	}
	os.Remove(o.Socket)
	return err
}

func serve(err error) error {
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func newAPIServer(token, socket, manifestsDir string) (*genericapiserver.GenericAPIServer, error) {
	// ponytail: everything, Leases included, is in memory and lost on restart.
	backend := memstore.New()
	optsFor := func(gv schema.GroupVersion) generic.RESTOptionsGetter {
		return restOptions{gv: gv, backend: backend}
	}
	ns, err := registry.NewNamespace(Scheme, optsFor(corev1.SchemeGroupVersion))
	if err != nil {
		return nil, err
	}
	if err := ns.Ensure(context.Background(), metav1.NamespaceDefault); err != nil {
		return nil, err
	}

	cfg := genericapiserver.NewConfig(Codecs)
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
	if manifestsDir != "" {
		// Runs once the API is up; /readyz waits for the first sync.
		srv.AddPostStartHookOrDie("manifests", func(hc genericapiserver.PostStartHookContext) error {
			return manifests.Start(hc, manifestsDir, hc.LoopbackClientConfig)
		})
	}

	den, denStatus, err := registry.NewDen(Scheme, optsFor(denv1.SchemeGroupVersion))
	if err != nil {
		return nil, err
	}
	space, err := registry.NewSpace(Scheme, optsFor(denv1.SchemeGroupVersion))
	if err != nil {
		return nil, err
	}
	view, err := registry.NewView(Scheme, optsFor(denv1.SchemeGroupVersion))
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
		"views":       view,
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

type restOptions struct {
	gv      schema.GroupVersion
	backend *memstore.Backend
}

func (r restOptions) GetRESTOptions(gr schema.GroupResource, _ runtime.Object) (generic.RESTOptions, error) {
	return generic.RESTOptions{
		StorageConfig: &storagebackend.ConfigForResource{
			GroupResource: gr,
			Config:        storagebackend.Config{Codec: Codecs.LegacyCodec(r.gv)},
		},
		Decorator:      r.backend.Decorator(),
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
