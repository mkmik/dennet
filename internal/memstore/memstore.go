// Package memstore is an in-memory implementation of the k8s.io/apiserver
// storage.Interface. All resources share one Backend, so resource versions
// are global and increase by one on every write, like etcd revisions.
//
// Objects are kept encoded with the resource's storage codec, exactly as etcd
// would hold them, and decoded on every read.
package memstore

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/storage"
	"k8s.io/apiserver/pkg/storage/storagebackend"
	"k8s.io/apiserver/pkg/storage/storagebackend/factory"
	"k8s.io/client-go/tools/cache"
)

// ponytail: bounded event history; a watch older than it gets 410 Gone and
// re-lists, same as after an etcd compaction.
const (
	historySize     = 10000
	watcherMaxQueue = 10000
)

type record struct {
	data []byte
	rv   uint64
}

type event struct {
	typ  watch.EventType
	key  string
	rv   uint64
	data []byte // object after the change (nil for Deleted)
	prev []byte // object before the change (nil for Added)
}

// Backend holds every object of every resource.
type Backend struct {
	mu        sync.Mutex
	rv        uint64
	compacted uint64 // events at or below this rv are no longer in history
	objs      map[string]record
	history   []event
	watchers  map[*watcher]struct{}
}

func New() *Backend {
	// Start at 1 like etcd: clients treat resourceVersion 0 as "any".
	return &Backend{rv: 1, objs: map[string]record{}, watchers: map[*watcher]struct{}{}}
}

// Decorator returns a generic.StorageDecorator that stores the resource in b.
func (b *Backend) Decorator() generic.StorageDecorator {
	return func(config *storagebackend.ConfigForResource, prefix string, _ func(runtime.Object) (string, error),
		newFunc, _ func() runtime.Object, _ storage.AttrFunc, _ storage.IndexerFuncs, _ *cache.Indexers,
	) (storage.Interface, factory.DestroyFunc, error) {
		return &store{b: b, codec: config.Codec, newFunc: newFunc, prefix: prefix}, func() {}, nil
	}
}

// write records a change and fans it out to watchers. Caller holds b.mu.
func (b *Backend) write(typ watch.EventType, key string, data, prev []byte) uint64 {
	b.rv++
	if typ == watch.Deleted {
		delete(b.objs, key)
	} else {
		b.objs[key] = record{data: data, rv: b.rv}
	}
	e := event{typ: typ, key: key, rv: b.rv, data: data, prev: prev}
	if len(b.history) >= historySize {
		drop := historySize / 2
		b.compacted = b.history[drop-1].rv
		b.history = slices.Clone(b.history[drop:])
	}
	b.history = append(b.history, e)
	for w := range b.watchers {
		if w.wants(key) {
			w.push(e)
		}
	}
	return b.rv
}

func (b *Backend) get(key string) (record, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	r, ok := b.objs[key]
	return r, ok
}

// keys returns the sorted keys matched by key/recursive. Caller holds b.mu.
func (b *Backend) keys(key string, recursive bool) []string {
	if !recursive {
		if _, ok := b.objs[key]; ok {
			return []string{key}
		}
		return nil
	}
	var ks []string
	for k := range b.objs {
		if strings.HasPrefix(k, key) {
			ks = append(ks, k)
		}
	}
	slices.Sort(ks)
	return ks
}

type store struct {
	b         *Backend
	codec     runtime.Codec
	newFunc   func() runtime.Object
	prefix    string
	versioner storage.APIObjectVersioner
}

var _ storage.Interface = (*store)(nil)

func (s *store) decode(data []byte, rv uint64, into runtime.Object) error {
	if err := runtime.DecodeInto(s.codec, data, into); err != nil {
		return storage.NewCorruptObjError("", err)
	}
	return s.versioner.UpdateObject(into, rv)
}

func (s *store) encode(obj runtime.Object) ([]byte, error) {
	if err := s.versioner.PrepareObjectForStorage(obj); err != nil {
		return nil, err
	}
	return runtime.Encode(s.codec, obj)
}

func (s *store) Versioner() storage.Versioner { return s.versioner }

func (s *store) Create(_ context.Context, key string, obj, out runtime.Object, _ uint64) error {
	if v, err := s.versioner.ObjectResourceVersion(obj); err == nil && v != 0 {
		return storage.ErrResourceVersionSetOnCreate
	}
	data, err := s.encode(obj)
	if err != nil {
		return err
	}
	s.b.mu.Lock()
	if _, ok := s.b.objs[key]; ok {
		s.b.mu.Unlock()
		return storage.NewKeyExistsError(key, 0)
	}
	rv := s.b.write(watch.Added, key, data, nil)
	s.b.mu.Unlock()
	return s.decode(data, rv, out)
}

func (s *store) Get(_ context.Context, key string, opts storage.GetOptions, out runtime.Object) error {
	r, ok := s.b.get(key)
	if !ok {
		if opts.IgnoreNotFound {
			return runtime.SetZeroValue(out)
		}
		return storage.NewKeyNotFoundError(key, 0)
	}
	return s.decode(r.data, r.rv, out)
}

// GetList ignores limit/continue and resourceVersion: it always returns
// the full, current list. Clients handle an unpaginated answer.
func (s *store) GetList(_ context.Context, key string, opts storage.ListOptions, listObj runtime.Object) error {
	key = dirKey(key, opts.Recursive)
	s.b.mu.Lock()
	var recs []record
	for _, k := range s.b.keys(key, opts.Recursive) {
		recs = append(recs, s.b.objs[k])
	}
	rv := s.b.rv
	s.b.mu.Unlock()

	items := make([]runtime.Object, 0, len(recs))
	for _, r := range recs {
		obj := s.newFunc()
		if err := s.decode(r.data, r.rv, obj); err != nil {
			return err
		}
		if ok, err := opts.Predicate.Matches(obj); err != nil {
			return err
		} else if ok {
			items = append(items, obj)
		}
	}
	if err := meta.SetList(listObj, items); err != nil {
		return err
	}
	return s.versioner.UpdateList(listObj, rv, "", nil)
}

func (s *store) Delete(ctx context.Context, key string, out runtime.Object, preconditions *storage.Preconditions,
	validateDeletion storage.ValidateObjectFunc, _ runtime.Object, _ storage.DeleteOptions,
) error {
	for {
		r, ok := s.b.get(key)
		if !ok {
			return storage.NewKeyNotFoundError(key, 0)
		}
		existing := s.newFunc()
		if err := s.decode(r.data, r.rv, existing); err != nil {
			return err
		}
		if preconditions != nil {
			if err := preconditions.Check(key, existing); err != nil {
				return err
			}
		}
		if err := validateDeletion(ctx, existing); err != nil {
			return err
		}
		s.b.mu.Lock()
		if cur, ok := s.b.objs[key]; !ok || cur.rv != r.rv {
			s.b.mu.Unlock()
			continue // changed under us; re-check against the new version
		}
		rv := s.b.write(watch.Deleted, key, nil, r.data)
		s.b.mu.Unlock()
		return s.decode(r.data, rv, out)
	}
}

// GuaranteedUpdate runs tryUpdate outside the lock (it may run admission and
// other slow code), then commits only if the object did not change meanwhile.
func (s *store) GuaranteedUpdate(ctx context.Context, key string, dest runtime.Object, ignoreNotFound bool,
	preconditions *storage.Preconditions, tryUpdate storage.UpdateFunc, _ runtime.Object,
) error {
	for {
		r, exists := s.b.get(key)
		existing := s.newFunc()
		if exists {
			if err := s.decode(r.data, r.rv, existing); err != nil {
				return err
			}
		} else if !ignoreNotFound {
			return storage.NewKeyNotFoundError(key, 0)
		}
		if preconditions != nil {
			if err := preconditions.Check(key, existing); err != nil {
				return err
			}
		}
		ret, _, err := tryUpdate(existing, storage.ResponseMeta{ResourceVersion: r.rv})
		if err != nil {
			return err
		}
		data, err := s.encode(ret)
		if err != nil {
			return err
		}
		if exists && bytes.Equal(data, r.data) {
			return s.decode(r.data, r.rv, dest) // no-op update keeps the resource version
		}

		s.b.mu.Lock()
		cur, stillExists := s.b.objs[key]
		if stillExists != exists || cur.rv != r.rv {
			s.b.mu.Unlock()
			continue
		}
		typ := watch.Modified
		if !exists {
			typ = watch.Added
		}
		rv := s.b.write(typ, key, data, r.data)
		s.b.mu.Unlock()
		return s.decode(data, rv, dest)
	}
}

// Watch follows etcd semantics: resourceVersion "" or "0" first replays the
// current state as ADDED events; a newer version replays history after it.
// A version outside the retained history (including one from before a
// dennet restart) yields a single 410 Gone ERROR event.
func (s *store) Watch(ctx context.Context, key string, opts storage.ListOptions) (watch.Interface, error) {
	rv, err := s.versioner.ParseResourceVersion(opts.ResourceVersion)
	if err != nil {
		return nil, err
	}
	sendInitial := rv == 0
	if opts.SendInitialEvents != nil {
		sendInitial = *opts.SendInitialEvents
	}
	w := &watcher{
		s:         s,
		key:       dirKey(key, opts.Recursive),
		recursive: opts.Recursive,
		pred:      opts.Predicate,
		result:    make(chan watch.Event),
		signal:    make(chan struct{}, 1),
		done:      make(chan struct{}),
	}

	b := s.b
	b.mu.Lock()
	switch {
	case sendInitial:
		for _, k := range b.keys(w.key, w.recursive) {
			r := b.objs[k]
			w.push(event{typ: watch.Added, key: k, rv: r.rv, data: r.data})
		}
		if opts.SendInitialEvents != nil && opts.Predicate.AllowWatchBookmarks {
			w.push(event{typ: watch.Bookmark, rv: b.rv})
		}
	case rv < b.compacted || rv > b.rv:
		w.push(event{typ: watch.Error, rv: rv})
	default:
		i, _ := slices.BinarySearchFunc(b.history, rv+1, func(e event, rv uint64) int { return int(int64(e.rv) - int64(rv)) })
		for _, e := range b.history[i:] {
			if w.wants(e.key) {
				w.push(e)
			}
		}
	}
	b.watchers[w] = struct{}{}
	b.mu.Unlock()

	go w.run(ctx)
	return w, nil
}

func (s *store) Stats(context.Context) (storage.Stats, error) {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	return storage.Stats{ObjectCount: int64(len(s.b.keys(s.prefix+"/", true)))}, nil
}

func (s *store) ReadinessCheck() error                               { return nil }
func (s *store) RequestWatchProgress(context.Context) error          { return nil }
func (s *store) EnableResourceSizeEstimation(storage.KeysFunc) error { return nil }
func (s *store) CompactRevision() int64 {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	return int64(s.b.compacted)
}
func (s *store) GetCurrentResourceVersion(context.Context) (uint64, error) {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	return s.b.rv, nil
}

func dirKey(key string, recursive bool) string {
	if recursive && !strings.HasSuffix(key, "/") {
		return key + "/"
	}
	return key
}

type watcher struct {
	s         *store
	key       string
	recursive bool
	pred      storage.SelectionPredicate
	result    chan watch.Event
	signal    chan struct{}
	done      chan struct{}
	stopOnce  sync.Once

	mu       sync.Mutex
	queue    []event
	overflow bool
}

func (w *watcher) wants(key string) bool {
	if w.recursive {
		return strings.HasPrefix(key, w.key)
	}
	return key == w.key
}

// push queues e without blocking. Caller holds the backend lock.
func (w *watcher) push(e event) {
	w.mu.Lock()
	if len(w.queue) >= watcherMaxQueue {
		w.overflow = true
	} else {
		w.queue = append(w.queue, e)
	}
	w.mu.Unlock()
	select {
	case w.signal <- struct{}{}:
	default:
	}
}

func (w *watcher) Stop() { w.stopOnce.Do(func() { close(w.done) }) }

func (w *watcher) ResultChan() <-chan watch.Event { return w.result }

func (w *watcher) run(ctx context.Context) {
	defer func() {
		w.s.b.mu.Lock()
		delete(w.s.b.watchers, w)
		w.s.b.mu.Unlock()
		close(w.result)
	}()
	for {
		select {
		case <-w.signal:
		case <-w.done:
			return
		case <-ctx.Done():
			return
		}
		w.mu.Lock()
		queue, overflow := w.queue, w.overflow
		w.queue = nil
		w.mu.Unlock()

		for _, e := range queue {
			ev, ok, err := w.convert(e)
			if err != nil {
				ev, ok = watch.Event{Type: watch.Error, Object: &apierrors.NewInternalError(err).ErrStatus}, true
			}
			if !ok {
				continue
			}
			select {
			case w.result <- ev:
			case <-w.done:
				return
			case <-ctx.Done():
				return
			}
			if ev.Type == watch.Error {
				return
			}
		}
		if overflow {
			return // slow consumer: end the watch so the client re-lists
		}
	}
}

// convert turns a stored event into what this watcher should see, applying
// its predicate: an object that starts or stops matching is reported as
// ADDED or DELETED, like etcd's watcher does.
func (w *watcher) convert(e event) (watch.Event, bool, error) {
	switch e.typ {
	case watch.Error:
		st := apierrors.NewResourceExpired(fmt.Sprintf("resourceVersion %d is too old or from before a restart", e.rv)).ErrStatus
		return watch.Event{Type: watch.Error, Object: &st}, true, nil
	case watch.Bookmark:
		obj := w.s.newFunc()
		if err := w.s.versioner.UpdateObject(obj, e.rv); err != nil {
			return watch.Event{}, false, err
		}
		m, err := meta.Accessor(obj)
		if err != nil {
			return watch.Event{}, false, err
		}
		m.SetAnnotations(map[string]string{metav1.InitialEventsAnnotationKey: "true"})
		return watch.Event{Type: watch.Bookmark, Object: obj}, true, nil
	}

	decode := func(data []byte) (runtime.Object, bool, error) {
		if data == nil {
			return nil, false, nil
		}
		obj := w.s.newFunc()
		if err := w.s.decode(data, e.rv, obj); err != nil {
			return nil, false, err
		}
		ok, err := w.pred.Matches(obj)
		return obj, ok, err
	}
	cur, curOK, err := decode(e.data)
	if err != nil {
		return watch.Event{}, false, err
	}
	prev, prevOK, err := decode(e.prev)
	if err != nil {
		return watch.Event{}, false, err
	}
	switch {
	case curOK && prevOK:
		return watch.Event{Type: watch.Modified, Object: cur}, true, nil
	case curOK:
		return watch.Event{Type: watch.Added, Object: cur}, true, nil
	case prevOK:
		return watch.Event{Type: watch.Deleted, Object: prev}, true, nil
	}
	return watch.Event{}, false, nil
}
