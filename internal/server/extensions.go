package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"mkm.pub/dennet/internal/registry"
	denv1 "mkm.pub/dennet/pkg/apis/den/v1alpha1"
)

// extensionGrace is how long an Extension stays Disconnected before dennet
// deletes it, and with it the Surfaces it owns. A var for tests.
var extensionGrace = 10 * time.Minute

// runExtensions runs dennet's extension controller until ctx is cancelled:
//   - it keeps a connection open to each Extension's spec.endpoint, as Leases
//     do to dens, and sends nothing on it; status.phase says whether it is
//     open, and status.since since when. Only changes are written, so a
//     running extension costs no writes.
//   - it deletes Extensions Disconnected for extensionGrace.
//   - it deletes Surfaces whose ownerReference names an Extension that is
//     gone: dennet has no general garbage collector, so this is the one for
//     ownerReferences to Extensions.
func runExtensions(ctx context.Context, socket string) {
	c := localClient(socket, denv1.SchemeGroupVersion)
	extensions := inform(c, "extensions", &denv1.Extension{})
	surfaces := inform(c, "surfaces", &denv1.Surface{})
	kick := kicker(extensions, surfaces)
	go extensions.Run(ctx.Done())
	go surfaces.Run(ctx.Done())
	if !cache.WaitForCacheSync(ctx.Done(), extensions.HasSynced, surfaces.HasSynced) {
		return
	}
	x := &extensionConns{conns: map[types.UID]*extensionConn{}, kick: make(chan struct{}, 1)}
	defer x.closeAll()
	// Extensions that weren't listening are dialled again on the tick.
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		syncExtensions(ctx, c, x, extensions.GetStore().List(), surfaces.GetStore().List(), time.Now())
		select {
		case <-ctx.Done():
			return
		case <-kick:
		case <-x.kick:
		case <-tick.C:
		}
	}
}

type extensionConns struct {
	mu    sync.Mutex
	conns map[types.UID]*extensionConn
	kick  chan struct{} // gets a value when a connection closes
}

type extensionConn struct {
	conn     net.Conn
	endpoint string
}

// connected says whether e's endpoint is connected, dialling it if not.
func (x *extensionConns) connected(e *denv1.Extension) bool {
	x.mu.Lock()
	c := x.conns[e.UID]
	if c != nil && c.endpoint != e.Spec.Endpoint {
		c.conn.Close() // its goroutine forgets it
		c = nil
	}
	x.mu.Unlock()
	if c != nil {
		return true
	}
	conn, err := net.DialTimeout("unix", e.Spec.Endpoint, time.Second)
	if err != nil {
		return false
	}
	c = &extensionConn{conn, e.Spec.Endpoint}
	x.mu.Lock()
	x.conns[e.UID] = c
	x.mu.Unlock()
	go func() {
		io.Copy(io.Discard, conn)
		conn.Close()
		x.mu.Lock()
		if x.conns[e.UID] == c {
			delete(x.conns, e.UID)
		}
		x.mu.Unlock()
		select {
		case x.kick <- struct{}{}:
		default:
		}
	}()
	return true
}

// keep closes the connections of Extensions not in uids.
func (x *extensionConns) keep(uids map[types.UID]bool) {
	x.mu.Lock()
	defer x.mu.Unlock()
	for uid, c := range x.conns {
		if !uids[uid] {
			c.conn.Close()
			delete(x.conns, uid)
		}
	}
}

func (x *extensionConns) closeAll() { x.keep(nil) }

// syncExtensions does one pass over the cached extensions and surfaces.
func syncExtensions(ctx context.Context, c *restclient.RESTClient, x *extensionConns, extensions, surfaces []any, now time.Time) {
	live := map[string]types.UID{}
	uids := map[types.UID]bool{}
	for _, o := range extensions {
		e := o.(*denv1.Extension)
		if e.DeletionTimestamp != nil {
			continue
		}
		live[e.Namespace+"/"+e.Name] = e.UID
		uids[e.UID] = true
		phase := denv1.ExtensionDisconnected
		if x.connected(e) {
			phase = denv1.ExtensionConnected
		}
		switch {
		case e.Status.Phase != phase:
			status, _ := json.Marshal(map[string]any{"status": denv1.ExtensionStatus{Phase: phase, Since: &metav1.Time{Time: now}}})
			err := c.Patch(types.MergePatchType).Namespace(e.Namespace).Resource("extensions").Name(e.Name).
				SubResource("status").Body(status).Do(ctx).Error()
			if err != nil && !apierrors.IsNotFound(err) {
				fmt.Fprintf(os.Stderr, "dennet: Extension %s/%s: %v\n", e.Namespace, e.Name, err)
			}
		case phase == denv1.ExtensionDisconnected && e.Status.Since != nil && now.Sub(e.Status.Since.Time) >= extensionGrace:
			fmt.Fprintf(os.Stderr, "dennet: deleting Extension %s/%s, disconnected since %s\n", e.Namespace, e.Name, e.Status.Since)
			removeOwned(ctx, c, e.Namespace, "extensions", e.Name, e.UID)
			delete(live, e.Namespace+"/"+e.Name)
		}
	}
	x.keep(uids)

	for _, o := range surfaces {
		s := o.(*denv1.Surface)
		owner := registry.Owner(s)
		if owner == nil || s.DeletionTimestamp != nil || live[s.Namespace+"/"+owner.Name] == owner.UID {
			continue
		}
		// The cache may not have an Extension created a moment ago: ask.
		var e denv1.Extension
		err := c.Get().Namespace(s.Namespace).Resource("extensions").Name(owner.Name).Do(ctx).Into(&e)
		if err == nil && e.UID == owner.UID && e.DeletionTimestamp == nil {
			continue
		}
		if err != nil && !apierrors.IsNotFound(err) {
			continue
		}
		removeOwned(ctx, c, s.Namespace, "surfaces", s.Name, s.UID)
	}
}

// removeOwned deletes an object if it is still the one with uid.
func removeOwned(ctx context.Context, c *restclient.RESTClient, ns, resource, name string, uid types.UID) {
	err := c.Delete().Namespace(ns).Resource(resource).Name(name).
		Body(&metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}).Do(ctx).Error()
	if err != nil && !apierrors.IsNotFound(err) && !apierrors.IsConflict(err) {
		fmt.Fprintf(os.Stderr, "dennet: deleting %s %s/%s: %v\n", resource, ns, name, err)
	}
}
