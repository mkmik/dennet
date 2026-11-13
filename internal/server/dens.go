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
// again: a den it just started takes a moment to open its socket.
const denTerm = 30 * time.Second

// runDens runs dennet's den controller until ctx is cancelled:
//   - it starts a den (den apply -f <the Den's URL>) for every Den on this
//     host whose den isn't connected to leases, unless den is empty. den
//     reads its spec from dennet's socket, reports status there, and goes
//     into the background on its own.
//   - for a Den being deleted, it shuts its den down over the den's socket,
//     then removes the Den's finalizer.
//
// It connects to every den once before acting at all, so after a restart
// running dens are found, not started again.
func runDens(ctx context.Context, den, socket string, leases *registry.Leases) {
	denClient := localClient(socket, denv1.SchemeGroupVersion)
	dens := inform(denClient, "dens", &denv1.Den{})
	kick := kicker(dens)
	go dens.Run(ctx.Done())
	if !cache.WaitForCacheSync(ctx.Done(), dens.HasSynced) {
		return
	}

	spawned, stopping := map[types.UID]time.Time{}, map[types.UID]time.Time{}
	// Retry dens that weren't listening now and then.
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		now := time.Now()
		old := func(_ types.UID, t time.Time) bool { return now.Sub(t) >= denTerm }
		maps.DeleteFunc(spawned, old)
		maps.DeleteFunc(stopping, old)
		var all []*denv1.Den
		for _, obj := range dens.GetStore().List() {
			all = append(all, obj.(*denv1.Den))
		}
		leases.Sync(all)
		for _, d := range all {
			switch {
			case d.DeletionTimestamp != nil:
				if _, ok := stopping[d.UID]; !ok && slices.Contains(d.Finalizers, denv1.DenFinalizer) {
					stopping[d.UID] = now
					go func() {
						if stopDen(d) || !leases.Held(d) {
							release(ctx, denClient, d)
						}
					}()
				}
			case den != "":
				if _, ok := spawned[d.UID]; !ok && needsDen(d, leases.Held(d)) {
					spawned[d.UID] = now
					go startDen(den, socket, d.Namespace, d.Name)
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-kick:
		case <-leases.Kick:
		case <-tick.C:
		}
	}
}

// localClient is a client for group version gv on dennet's own socket.
func localClient(socket string, gv schema.GroupVersion) *restclient.RESTClient {
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

// inform watches resource in all namespaces. Those of Hosts are not
// forwarded (see remote), so these are this host's objects only.
func inform(c *restclient.RESTClient, resource string, obj runtime.Object) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(cache.NewListWatchFromClient(c, resource, "", fields.Everything()), obj, 0, cache.Indexers{})
}

// kicker returns a channel that gets a value after any change to the
// informers' objects; changes that come close together share one.
func kicker(informers ...cache.SharedIndexInformer) <-chan struct{} {
	kick := make(chan struct{}, 1)
	poke := func() {
		select {
		case kick <- struct{}{}:
		default:
		}
	}
	for _, i := range informers {
		i.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    func(any) { poke() },
			UpdateFunc: func(any, any) { poke() },
			DeleteFunc: func(any) { poke() },
		})
	}
	return kick
}

// needsDen says whether d should get a den: it hasn't finished or isn't
// going away, and its den isn't connected (held).
func needsDen(d *denv1.Den, held bool) bool {
	// ponytail: a non-empty spec.hostName is never this host; Hosts' dens
	// live in their namespaces and are their own dennet's to start.
	if d.DeletionTimestamp != nil || d.Spec.HostName != "" {
		return false
	}
	switch d.Status.Phase {
	case denv1.DenExited, denv1.DenFailed:
		return false
	}
	return !held
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

// release removes the finalizer from d, so the Den goes away. The patch
// tests that the finalizer is still where d had it, so other changes (den's
// final status) don't get in the way.
func release(ctx context.Context, dens *restclient.RESTClient, d *denv1.Den) {
	at := fmt.Sprintf("/metadata/finalizers/%d", slices.Index(d.Finalizers, denv1.DenFinalizer))
	patch, _ := json.Marshal([]map[string]string{{"op": "test", "path": at, "value": denv1.DenFinalizer}, {"op": "remove", "path": at}})
	err := dens.Patch(types.JSONPatchType).Namespace(d.Namespace).Resource("dens").Name(d.Name).Body(patch).Do(ctx).Error()
	if err != nil {
		fmt.Fprintf(os.Stderr, "dennet: releasing Den %s/%s: %v\n", d.Namespace, d.Name, err)
	}
}
