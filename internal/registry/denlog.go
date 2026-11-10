package registry

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/registry/generic/registry"
	"k8s.io/apiserver/pkg/registry/rest"

	denv1 "mkm.pub/dennet/pkg/apis/den/v1alpha1"
)

// NewDenLog returns dens/log: GET it for what `den logs` prints, the
// scrollback and screen as text. ?color=true keeps colors.
func NewDenLog(dens *registry.Store) rest.Storage { return &logREST{dens} }

type logREST struct{ dens *registry.Store }

func (*logREST) New() runtime.Object                               { return &denv1.Den{} }
func (*logREST) Destroy()                                          {}
func (*logREST) ConnectMethods() []string                          { return []string{http.MethodGet} }
func (*logREST) NewConnectOptions() (runtime.Object, bool, string) { return nil, false, "" }

// Den's control socket frames: u32 length (little-endian, counts type and
// payload), u8 type, payload. See proto.zig in den.
const denHistory = 7

func (r *logREST) Connect(ctx context.Context, name string, _ runtime.Object, resp rest.Responder) (http.Handler, error) {
	obj, err := r.dens.Get(ctx, name, &metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	d := obj.(*denv1.Den)
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		conn, n, err := history(req.Context(), DenSocket(d), req.URL.Query().Get("color") == "true")
		if err != nil {
			resp.Error(apierrors.NewServiceUnavailable(fmt.Sprintf("den %s/%s: %v", d.Namespace, d.Name, err)))
			return
		}
		defer conn.Close()
		// A den that stops mid-reply must not hold the handler after the
		// client has gone.
		context.AfterFunc(req.Context(), func() { conn.Close() })
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		io.CopyN(w, conn, n)
	}), nil
}

// history asks den for its history and returns the connection positioned at
// the start of the reply's n bytes of text.
func history(ctx context.Context, socket string, color bool) (net.Conn, int64, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", socket)
	if err != nil {
		return nil, 0, fmt.Errorf("not running: %w", err)
	}
	flags := byte(0)
	if color {
		flags = 1
	}
	// den answers within 5s or not at all (a den older than the request).
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	var hdr [5]byte
	_, err = conn.Write([]byte{2, 0, 0, 0, denHistory, flags})
	if err == nil {
		_, err = io.ReadFull(conn, hdr[:])
	}
	if err == nil && hdr[4] != denHistory {
		err = fmt.Errorf("got frame type %d", hdr[4])
	}
	if err != nil {
		conn.Close()
		return nil, 0, fmt.Errorf("no history reply on %s: %w", socket, err)
	}
	conn.SetDeadline(time.Time{})
	return conn, int64(binary.LittleEndian.Uint32(hdr[:4])) - 1, nil
}

// DenSocket is where the den is listening: its status says, or else where
// den puts it on this host. See socketPath in den.
// ponytail: assumes the den runs on this host; status.host is ignored until
// dennet reaches dens on other hosts.
func DenSocket(d *denv1.Den) string {
	if d.Status.Socket != "" {
		return d.Status.Socket
	}
	abs := func(k string) string {
		if v := os.Getenv(k); filepath.IsAbs(v) {
			return v
		}
		return ""
	}
	if dir := abs("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "den", d.Namespace, d.Name+".sock")
	}
	dir := abs("XDG_STATE_HOME")
	if dir == "" {
		dir = filepath.Join(abs("HOME"), ".local", "state")
	}
	return filepath.Join(dir, "den", d.Namespace, d.Name+".sock")
}
