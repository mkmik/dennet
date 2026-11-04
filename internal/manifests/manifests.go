// Package manifests keeps the API server in step with a directory of YAML
// and JSON files: a file's objects are applied when the file is added or
// changed, and deleted when the file is removed or no longer lists them.
package manifests

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
)

// FieldManager owns the fields dennet applies from files.
const FieldManager = "dennet-manifests"

// Start applies the files in dir, then keeps applying them as they change
// until ctx is cancelled. Subdirectories are not read.
func Start(ctx context.Context, dir string, cfg *rest.Config) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return err
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return err
	}
	// fsnotify uses inotify on Linux and kqueue on macOS: no polling.
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	// Watch before the first sync, so no change falls between the two.
	if err := w.Add(dir); err != nil {
		w.Close()
		return err
	}
	s := &syncer{
		dir:    dir,
		dyn:    dyn,
		mapper: restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(dc)),
		files:  map[string]file{},
	}
	s.sync(ctx)
	fmt.Fprintf(os.Stderr, "dennet: applying manifests from %s\n", dir)

	go func() {
		defer w.Close()
		// One save is often several events (write, chmod, rename into
		// place), so sync once things go quiet.
		var quiet <-chan time.Time
		for {
			select {
			case <-ctx.Done():
				return
			case <-w.Events:
				quiet = time.After(100 * time.Millisecond)
			case err := <-w.Errors:
				fmt.Fprintf(os.Stderr, "dennet: watching %s: %v\n", dir, err)
			case <-quiet:
				quiet = nil
				s.sync(ctx)
			}
		}
	}()
	return nil
}

type ref struct {
	gvr      schema.GroupVersionResource
	ns, name string
}

type file struct {
	sum  [sha256.Size]byte
	objs []ref
}

type syncer struct {
	dir    string
	dyn    dynamic.Interface
	mapper meta.RESTMapper
	files  map[string]file // by base name
}

// sync applies new and changed files and prunes what removed files made.
// An unchanged file is not applied again, so a kubectl edit to its objects
// lasts until the file next changes.
// ponytail: an object listed in two files is deleted when either drops it.
func (s *syncer) sync(ctx context.Context) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dennet: %v\n", err)
		return
	}
	seen := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		// Skip editor swap and backup files.
		if e.IsDir() || strings.HasPrefix(name, ".") || !slices.Contains([]string{".yaml", ".yml", ".json"}, filepath.Ext(name)) {
			continue
		}
		seen[name] = true
		b, err := os.ReadFile(filepath.Join(s.dir, name))
		if err != nil {
			fmt.Fprintf(os.Stderr, "dennet: %v\n", err)
			continue
		}
		old, sum := s.files[name], sha256.Sum256(b)
		if old.sum == sum {
			continue
		}
		objs, err := s.apply(ctx, b)
		if err != nil {
			// Keep the old objects: one bad document should not delete
			// what the last good version of the file made.
			fmt.Fprintf(os.Stderr, "dennet: %s: %v\n", name, err)
			for _, r := range old.objs {
				if !slices.Contains(objs, r) {
					objs = append(objs, r)
				}
			}
		} else {
			fmt.Fprintf(os.Stderr, "dennet: applied %s\n", name)
			s.prune(ctx, old.objs, objs)
		}
		s.files[name] = file{sum, objs}
	}
	for name, f := range s.files {
		if !seen[name] {
			s.prune(ctx, f.objs, nil)
			delete(s.files, name)
			fmt.Fprintf(os.Stderr, "dennet: removed %s\n", name)
		}
	}
}

// apply server-side applies each document in b and returns the objects
// it applied.
func (s *syncer) apply(ctx context.Context, b []byte) ([]ref, error) {
	var refs []ref
	var errs []error
	d := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(b), 4096)
	for {
		var u unstructured.Unstructured
		if err := d.Decode(&u.Object); err == io.EOF {
			break
		} else if err != nil {
			return refs, errors.Join(append(errs, err)...)
		}
		if len(u.Object) == 0 { // empty document, e.g. a trailing ---
			continue
		}
		gvk := u.GroupVersionKind()
		m, err := s.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		ns := ""
		if m.Scope.Name() == meta.RESTScopeNameNamespace {
			if ns = u.GetNamespace(); ns == "" {
				ns = metav1.NamespaceDefault
			}
		}
		_, err = s.dyn.Resource(m.Resource).Namespace(ns).Apply(ctx, u.GetName(), &u, metav1.ApplyOptions{FieldManager: FieldManager, Force: true})
		if err != nil {
			errs = append(errs, err)
			continue
		}
		refs = append(refs, ref{m.Resource, ns, u.GetName()})
	}
	return refs, errors.Join(errs...)
}

// prune deletes the objects in old that are not in keep.
func (s *syncer) prune(ctx context.Context, old, keep []ref) {
	for _, r := range old {
		if slices.Contains(keep, r) {
			continue
		}
		err := s.dyn.Resource(r.gvr).Namespace(r.ns).Delete(ctx, r.name, metav1.DeleteOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			fmt.Fprintf(os.Stderr, "dennet: deleting %s %s/%s: %v\n", r.gvr.Resource, r.ns, r.name, err)
		}
	}
}
