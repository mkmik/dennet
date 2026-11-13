package registry

import (
	"context"
	"io"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/utils/ptr"

	denv1 "mkm.pub/dennet/pkg/apis/den/v1alpha1"
)

// LeaseDuration is the leaseDurationSeconds of every Lease.
const LeaseDuration = 30

// Leases keeps a connection open to the control socket of each den on this
// host. While it is open the den is alive, and its Den has a Lease; when it
// closes (the den exited, even on kill -9), the Lease is gone. Nothing is
// stored, and dennet sends nothing on these connections: den only counts a
// client once it attaches.
//
// It is also the read-only storage of coordination.k8s.io/v1 leases. Having
// no create, update or patch, discovery lists no patch verb, so den doesn't
// renew Leases; dens started before that get 405.
type Leases struct {
	mu    sync.Mutex
	conns map[string]*denConn // by namespace/name
	rv    uint64              // bumped on every connect and disconnect
	b     *watch.Broadcaster
	host  string

	ready     chan struct{} // closed after the first Sync
	readyOnce sync.Once
	// Kick gets a value after a den connects or disconnects.
	Kick chan struct{}
}

type denConn struct {
	conn           net.Conn
	ns, name, sock string
	uid            types.UID
	since          time.Time
	rv             uint64
}

func NewLeases() *Leases {
	host, _ := os.Hostname()
	return &Leases{
		conns: map[string]*denConn{},
		// Start from the clock, so resourceVersions grow across restarts
		// and an old one is always answered with 410 Gone.
		rv: uint64(time.Now().UnixNano()),
		// ponytail: a watcher more than 1024 events behind misses events;
		// close slow watchers instead (so they re-list) if that ever happens.
		b:     watch.NewBroadcaster(1024, watch.DropIfChannelFull),
		host:  host,
		ready: make(chan struct{}),
		Kick:  make(chan struct{}, 1),
	}
}

// Sync connects to the den of every Den in dens on this host that isn't
// connected yet, and drops the connections of Dens not in dens. Leases are
// served once the first Sync is done, so that after a restart a missing
// Lease still means no den.
func (l *Leases) Sync(dens []*denv1.Den) {
	want := map[string]*denv1.Den{}
	var dial []*denv1.Den
	l.mu.Lock()
	for _, d := range dens {
		// Hosts' dens are their own dennet's.
		if d.Spec.HostName != "" {
			continue
		}
		k := d.Namespace + "/" + d.Name
		want[k] = d
		if c := l.conns[k]; c == nil || c.uid != d.UID {
			dial = append(dial, d)
		}
	}
	for k, c := range l.conns {
		if d := want[k]; d == nil || d.UID != c.uid {
			l.drop(c)
		}
	}
	l.mu.Unlock()

	for _, d := range dial {
		sock := DenSocket(d)
		// No socket file, or a stale one (connection refused): not running.
		conn, err := net.DialTimeout("unix", sock, time.Second)
		if err != nil {
			continue
		}
		c := &denConn{conn: conn, ns: d.Namespace, name: d.Name, sock: sock, uid: d.UID, since: time.Now()}
		l.mu.Lock()
		l.rv++
		c.rv = l.rv
		l.conns[d.Namespace+"/"+d.Name] = c
		l.b.Action(watch.Added, l.lease(c, c.since))
		l.mu.Unlock()
		l.kick()
		go func() {
			io.Copy(io.Discard, conn)
			l.mu.Lock()
			if l.conns[c.ns+"/"+c.name] == c {
				l.drop(c)
			}
			l.mu.Unlock()
			l.kick()
		}()
	}
	l.readyOnce.Do(func() { close(l.ready) })
}

// drop forgets c and closes it. l.mu is held.
func (l *Leases) drop(c *denConn) {
	delete(l.conns, c.ns+"/"+c.name)
	c.conn.Close()
	l.rv++
	c.rv = l.rv
	l.b.Action(watch.Deleted, l.lease(c, time.Now()))
}

func (l *Leases) kick() {
	select {
	case l.Kick <- struct{}{}:
	default:
	}
}

// Held says whether d's den is connected.
func (l *Leases) Held(d *denv1.Den) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	c := l.conns[d.Namespace+"/"+d.Name]
	return c != nil && c.uid == d.UID
}

// lease is c's Lease, renewed at now. l.mu is held.
func (l *Leases) lease(c *denConn, now time.Time) *coordinationv1.Lease {
	return &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Name: c.name, Namespace: c.ns,
			ResourceVersion:   strconv.FormatUint(c.rv, 10),
			CreationTimestamp: metav1.NewTime(c.since),
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: denv1.SchemeGroupVersion.String(), Kind: "Den", Name: c.name, UID: c.uid,
			}},
		},
		Spec: coordinationv1.LeaseSpec{
			HolderIdentity:       ptr.To(l.host + "/" + c.sock),
			LeaseDurationSeconds: ptr.To[int32](LeaseDuration),
			AcquireTime:          &metav1.MicroTime{Time: c.since},
			// Renewed whenever it's read, so "renewTime + duration > now"
			// holds while the den is connected.
			RenewTime: &metav1.MicroTime{Time: now},
		},
	}
}

func (l *Leases) wait(ctx context.Context) error {
	select {
	case <-l.ready:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (*Leases) New() runtime.Object     { return &coordinationv1.Lease{} }
func (*Leases) NewList() runtime.Object { return &coordinationv1.LeaseList{} }
func (*Leases) Destroy()                {}
func (*Leases) NamespaceScoped() bool   { return true }
func (*Leases) GetSingularName() string { return "lease" }
func (*Leases) ConvertToTable(ctx context.Context, obj runtime.Object, opts runtime.Object) (*metav1.Table, error) {
	return rest.NewDefaultTableConvertor(coordinationv1.Resource("leases")).ConvertToTable(ctx, obj, opts)
}

func (l *Leases) Get(ctx context.Context, name string, _ *metav1.GetOptions) (runtime.Object, error) {
	if err := l.wait(ctx); err != nil {
		return nil, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	c := l.conns[genericapirequest.NamespaceValue(ctx)+"/"+name]
	if c == nil {
		return nil, apierrors.NewNotFound(coordinationv1.Resource("leases"), name)
	}
	return l.lease(c, time.Now()), nil
}

func (l *Leases) List(ctx context.Context, opts *metainternalversion.ListOptions) (runtime.Object, error) {
	if err := l.wait(ctx); err != nil {
		return nil, err
	}
	match := matcher(ctx, opts)
	l.mu.Lock()
	defer l.mu.Unlock()
	list := &coordinationv1.LeaseList{ListMeta: metav1.ListMeta{ResourceVersion: strconv.FormatUint(l.rv, 10)}}
	now := time.Now()
	for _, c := range l.conns {
		if match(c.ns, c.name) {
			list.Items = append(list.Items, *l.lease(c, now))
		}
	}
	return list, nil
}

// Watch sends ADDED when a den connects and DELETED when it disconnects.
// From no resourceVersion (or "0") it first sends ADDED for every Lease;
// from any other but the latest it answers 410 Gone, so informers re-list.
func (l *Leases) Watch(ctx context.Context, opts *metainternalversion.ListOptions) (watch.Interface, error) {
	if err := l.wait(ctx); err != nil {
		return nil, err
	}
	match := matcher(ctx, opts)
	l.mu.Lock()
	defer l.mu.Unlock()
	var w watch.Interface
	var err error
	switch opts.ResourceVersion {
	case strconv.FormatUint(l.rv, 10):
		w, err = l.b.Watch()
	case "", "0":
		var now []watch.Event
		for _, c := range l.conns {
			now = append(now, watch.Event{Type: watch.Added, Object: l.lease(c, time.Now())})
		}
		w, err = l.b.WatchWithPrefix(now)
	default:
		return nil, apierrors.NewResourceExpired("resourceVersion " + opts.ResourceVersion + " is gone")
	}
	if err != nil {
		return nil, err
	}
	return watch.Filter(w, func(e watch.Event) (watch.Event, bool) {
		lease := e.Object.(*coordinationv1.Lease)
		return e, match(lease.Namespace, lease.Name)
	}), nil
}

// matcher matches the request's namespace and the label and field
// selectors in opts. Leases have no labels.
func matcher(ctx context.Context, opts *metainternalversion.ListOptions) func(ns, name string) bool {
	want := genericapirequest.NamespaceValue(ctx)
	label, field := labels.Everything(), fields.Everything()
	if opts != nil && opts.LabelSelector != nil {
		label = opts.LabelSelector
	}
	if opts != nil && opts.FieldSelector != nil {
		field = opts.FieldSelector
	}
	return func(ns, name string) bool {
		return (want == "" || ns == want) && label.Matches(labels.Set{}) &&
			field.Matches(fields.Set{"metadata.name": name, "metadata.namespace": ns})
	}
}
