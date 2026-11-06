package server

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"os/exec"
	"strings"
	"sync"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"

	denv1 "mkm.pub/dennet/pkg/apis/den/v1alpha1"
)

// remote sends requests for objects in namespace N to the dennet on the
// Host named N, where the same objects live in "default". It rewrites the
// path, and metadata.namespace in JSON bodies both ways, so N is all a
// client here ever sees.
//
// ponytail: only requests naming a namespace are forwarded; lists across
// all namespaces (-A) show this host's objects only. Merging them needs a
// fan-out and a combined resourceVersion.
type remote struct {
	next  http.Handler
	hosts rest.Getter

	mu      sync.Mutex
	proxies map[string]hostProxy
}

type hostProxy struct {
	spec  denv1.HostSpec
	proxy *httputil.ReverseProxy
}

// dialHost opens a connection to the dennet on a Host. Tests replace it.
var dialHost = func(spec denv1.HostSpec) (net.Conn, error) {
	// -- ends ssh's options, so the destination can't be read as one.
	return cmdConn(exec.Command("ssh", "-T", "-o", "BatchMode=yes", "-o", "ServerAliveInterval=15",
		"--", spec.SSH, cmp.Or(spec.Dennet, "dennet")+" server dial-stdio"))
}

func (h *remote) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	info, ok := request.RequestInfoFrom(r.Context())
	ns := info.Namespace
	if !ok || !info.IsResourceRequest || ns == "" || ns == metav1.NamespaceDefault || info.Resource == "namespaces" {
		h.next.ServeHTTP(w, r)
		return
	}
	obj, err := h.hosts.Get(request.WithNamespace(r.Context(), ""), ns, &metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		h.next.ServeHTTP(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.proxyFor(obj.(*denv1.Host)).ServeHTTP(w, r)
}

func (h *remote) proxyFor(host *denv1.Host) *httputil.ReverseProxy {
	h.mu.Lock()
	defer h.mu.Unlock()
	old, ok := h.proxies[host.Name]
	if ok && old.spec == host.Spec {
		return old.proxy
	}
	if ok {
		// ponytail: watches already open keep the old connection until they end.
		old.proxy.Transport.(*http.Transport).CloseIdleConnections()
	}
	// One HTTP/2 connection, so one ssh, carries every request to the host.
	t := &http.Transport{
		Protocols: new(http.Protocols),
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			return dialHost(host.Spec)
		},
	}
	t.Protocols.SetUnencryptedHTTP2(true)
	ns := host.Name
	p := &httputil.ReverseProxy{
		Transport:     t,
		FlushInterval: -1, // watches
		Rewrite: func(r *httputil.ProxyRequest) {
			u := r.Out.URL
			u.Scheme, u.Host, r.Out.Host = "http", "dennet", "dennet"
			u.Path = strings.Replace(u.Path, "/namespaces/"+ns+"/", "/namespaces/default/", 1)
			u.RawPath = ""
			// The local user token is not the remote's business.
			r.Out.Header.Del("Authorization")
			// Only JSON can be rewritten.
			if strings.Contains(r.Out.Header.Get("Accept"), "protobuf") {
				r.Out.Header.Set("Accept", "application/json")
			}
			if r.Out.Body != nil {
				b, err := io.ReadAll(r.Out.Body)
				r.Out.Body.Close()
				if err != nil {
					b = nil
				}
				var out bytes.Buffer
				if retag(&out, bytes.NewReader(b), ns, metav1.NamespaceDefault) == nil {
					b = out.Bytes()
				}
				r.Out.Body, r.Out.ContentLength = io.NopCloser(bytes.NewReader(b)), int64(len(b))
			}
		},
		ModifyResponse: func(resp *http.Response) error {
			if !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
				return nil // e.g. dens/log text
			}
			pr, pw := io.Pipe()
			go func(body io.ReadCloser) {
				pw.CloseWithError(retag(pw, body, metav1.NamespaceDefault, ns))
				body.Close()
			}(resp.Body)
			resp.Body, resp.ContentLength = pr, -1
			resp.Header.Del("Content-Length")
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			http.Error(w, fmt.Sprintf("host %s: %v", ns, err), http.StatusBadGateway)
		},
	}
	if h.proxies == nil {
		h.proxies = map[string]hostProxy{}
	}
	h.proxies[host.Name] = hostProxy{host.Spec, p}
	return p
}

// retag copies a stream of JSON values (one object, or watch events),
// changing every metadata.namespace that is from to to.
func retag(w io.Writer, r io.Reader, from, to string) error {
	dec, enc := json.NewDecoder(r), json.NewEncoder(w)
	dec.UseNumber()
	for {
		var v any
		if err := dec.Decode(&v); errors.Is(err, io.EOF) {
			return nil
		} else if err != nil {
			return err
		}
		renamespace(v, from, to)
		if err := enc.Encode(v); err != nil {
			return err
		}
	}
}

func renamespace(v any, from, to string) {
	switch v := v.(type) {
	case map[string]any:
		if m, ok := v["metadata"].(map[string]any); ok && m["namespace"] == from {
			m["namespace"] = to
		}
		for _, c := range v {
			renamespace(c, from, to)
		}
	case []any:
		for _, c := range v {
			renamespace(c, from, to)
		}
	}
}

// cmdConn runs cmd and is a connection to its stdin and stdout. Closing it
// closes stdin; the connection ends when cmd closes stdout.
func cmdConn(cmd *exec.Cmd) (net.Conn, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr // ssh's own errors
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	local, far := net.Pipe()
	go func() {
		io.Copy(stdin, far)
		stdin.Close()
	}()
	go func() {
		io.Copy(far, stdout)
		cmd.Process.Kill()
		cmd.Wait()
		far.Close()
	}()
	return local, nil
}
