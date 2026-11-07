package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"mkm.pub/dennet/internal/registry"
	denv1 "mkm.pub/dennet/pkg/apis/den/v1alpha1"
)

// denTerm is how long dennet leaves a Den it acted on before acting on it
// again, and how long a den has to renew its Lease after dennet starts. It
// matches the leaseDurationSeconds den writes.
const denTerm = 30 * time.Second

// runDens runs dennet's den controller until ctx is cancelled:
//   - it starts a den (den apply -f <the Den's URL>) for every Den on this
//     host that no den holds the Lease of, unless den is empty. den reads its
//     spec from dennet's socket, reports status and renews its Lease there,
//     and goes into the background on its own.
//   - for a Den being deleted, it shuts its den down over the den's socket,
//     then removes the Den's finalizer and deletes its Lease.
func runDens(ctx context.Context, den, socket string) {
	client := func(gv schema.GroupVersion) *restclient.RESTClient {
		c, err := restclient.RESTClientFor(&restclient.Config{
			Host:    "http://dennet",
			APIPath: "/apis",
			Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", socket)
			},
			ContentConfig: restclient.ContentConfig{GroupVersion: &gv, NegotiatedSerializer: Codecs.WithoutConversion()},
		})
		if err != nil {
			panic(err) // only for a bad config, which is fixed above
		}
		return c
	}
	denClient, leaseClient := client(denv1.SchemeGroupVersion), client(coordinationv1.SchemeGroupVersion)
	// All namespaces: those of Hosts are not forwarded (see remote), so these
	// are this host's objects only.
	inform := func(c *restclient.RESTClient, resource string, obj runtime.Object) cache.SharedIndexInformer {
		return cache.NewSharedIndexInformer(cache.NewListWatchFromClient(c, resource, "", fields.Everything()), obj, 0, cache.Indexers{})
	}
	dens := inform(denClient, "dens", &denv1.Den{})
	leases := inform(leaseClient, "leases", &coordinationv1.Lease{})
	leaseOf := func(d *denv1.Den) *coordinationv1.Lease {
		l, _, _ := leases.GetStore().GetByKey(d.Namespace + "/" + d.Name)
		lease, _ := l.(*coordinationv1.Lease)
		return lease
	}

	kick := make(chan struct{}, 1)
	poke := func() {
		select {
		case kick <- struct{}{}:
		default:
		}
	}
	dens.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(any) { poke() },
		UpdateFunc: func(any, any) { poke() },
	})
	go dens.Run(ctx.Done())
	go leases.Run(ctx.Done())
	if !cache.WaitForCacheSync(ctx.Done(), dens.HasSynced, leases.HasSynced) {
		return
	}

	since := time.Now()
	spawned, stopping := map[types.UID]time.Time{}, map[types.UID]time.Time{}
	// Lease expiry is no event, so look again now and then.
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		now := time.Now()
		old := func(_ types.UID, t time.Time) bool { return now.Sub(t) >= denTerm }
		maps.DeleteFunc(spawned, old)
		maps.DeleteFunc(stopping, old)
		for _, obj := range dens.GetStore().List() {
			d := obj.(*denv1.Den)
			switch {
			case d.DeletionTimestamp != nil:
				if _, ok := stopping[d.UID]; !ok && slices.Contains(d.Finalizers, denv1.DenFinalizer) {
					stopping[d.UID] = now
					go func() {
						if stopDen(d) || !held(d, leaseOf(d), since, time.Now()) {
							release(ctx, denClient, leaseClient, d, leaseOf(d))
						}
					}()
				}
			case den != "":
				if _, ok := spawned[d.UID]; !ok && needsDen(d, leaseOf(d), since, now) {
					spawned[d.UID] = now
					go startDen(den, socket, d.Namespace, d.Name)
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-kick:
		case <-tick.C:
		}
	}
}

// needsDen says whether d should get a den: it hasn't finished or isn't
// going away, and no den holds its Lease.
func needsDen(d *denv1.Den, l *coordinationv1.Lease, since, now time.Time) bool {
	// ponytail: a non-empty spec.hostName is never this host; Hosts' dens
	// live in their namespaces and are their own dennet's to start.
	if d.DeletionTimestamp != nil || d.Spec.HostName != "" {
		return false
	}
	switch d.Status.Phase {
	case denv1.DenExited, denv1.DenFailed:
		return false
	}
	return !held(d, l, since, now)
}

// held says whether a den of d renewed its Lease l within the Lease's
// duration. The clock starts no earlier than since, when dennet started, so
// dens that kept running through a dennet restart get a full term to renew.
func held(d *denv1.Den, l *coordinationv1.Lease, since, now time.Time) bool {
	if l == nil || l.Spec.RenewTime == nil || l.Spec.LeaseDurationSeconds == nil {
		return false
	}
	for _, o := range l.OwnerReferences {
		if o.UID != d.UID {
			return false // left by an earlier Den of the same name
		}
	}
	renewed := l.Spec.RenewTime.Time
	if renewed.Before(since) {
		renewed = since
	}
	return !now.After(renewed.Add(time.Duration(*l.Spec.LeaseDurationSeconds) * time.Second))
}

// startDen runs den apply, which returns once den is in the background. A
// den that is still running for this Den refuses to start a second one.
func startDen(den, socket, ns, name string) {
	url := fmt.Sprintf("http://dennet/apis/%s/namespaces/%s/dens/%s", denv1.SchemeGroupVersion, ns, name)
	out, err := exec.Command(den, "apply", "-f", url, "--unix-socket", socket).CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "dennet: starting den for %s/%s: %v: %s\n", ns, name, err, strings.TrimSpace(string(out)))
		return
	}
	fmt.Fprintf(os.Stderr, "dennet: started den for %s/%s on %s\n", ns, name, strings.TrimSpace(string(out)))
}

// stopDen asks d's den to shut down, as den delete does, and says whether it
// is gone. den stops the program (SIGHUP, then SIGKILL after 5 s), writes a
// final status and exits, which closes the connection. No den listening
// counts as gone.
func stopDen(d *denv1.Den) bool {
	conn, err := net.Dial("unix", registry.DenSocket(d))
	if err != nil {
		return true
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(15 * time.Second))
	// den's control socket frame: u32 length, u8 type 11 (shutdown).
	if _, err = conn.Write([]byte{1, 0, 0, 0, 11}); err == nil {
		_, err = io.Copy(io.Discard, conn)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "dennet: stopping den for %s/%s: %v\n", d.Namespace, d.Name, err)
		return false
	}
	return true
}

// release removes the finalizer from d, so the Den goes away, and deletes
// the Lease its den held. The patch tests that the finalizer is still where
// d had it, so other changes (den's final status) don't get in the way.
func release(ctx context.Context, dens, leases *restclient.RESTClient, d *denv1.Den, l *coordinationv1.Lease) {
	at := fmt.Sprintf("/metadata/finalizers/%d", slices.Index(d.Finalizers, denv1.DenFinalizer))
	patch, _ := json.Marshal([]map[string]string{{"op": "test", "path": at, "value": denv1.DenFinalizer}, {"op": "remove", "path": at}})
	err := dens.Patch(types.JSONPatchType).Namespace(d.Namespace).Resource("dens").Name(d.Name).Body(patch).Do(ctx).Error()
	if err != nil {
		fmt.Fprintf(os.Stderr, "dennet: releasing Den %s/%s: %v\n", d.Namespace, d.Name, err)
		return
	}
	if l == nil || !slices.ContainsFunc(l.OwnerReferences, func(o metav1.OwnerReference) bool { return o.UID == d.UID }) {
		return
	}
	// ponytail: dennet has no garbage collector; this is the only place a
	// Lease owned by a Den goes away.
	err = leases.Delete().Namespace(l.Namespace).Resource("leases").Name(l.Name).
		Body(&metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &l.UID}}).Do(ctx).Error()
	if err != nil {
		fmt.Fprintf(os.Stderr, "dennet: deleting Lease %s/%s: %v\n", l.Namespace, l.Name, err)
	}
}
