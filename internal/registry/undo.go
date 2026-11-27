package registry

import (
	"context"
	"errors"
	"slices"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/apiserver/pkg/storage"

	denv1 "mkm.pub/dennet/pkg/apis/den/v1alpha1"
)

// Undoable is the storage of a kind whose deleted objects can be brought
// back for a while (see denv1.UndoFinalizer, and runUndo in the server,
// which removes it once the time is up):
//   - every object gets denv1.UndoFinalizer, so deleting one only marks it;
//   - creating an object whose name is taken by a marked one brings that
//     one back, with the labels, annotations and spec of the request;
//   - deleting with gracePeriodSeconds=0 removes the finalizer first, so
//     the object goes right away.
type Undoable struct{ InAll }

var errNotMarked = errors.New("not waiting for undo")

func (u Undoable) Create(ctx context.Context, obj runtime.Object, createValidation rest.ValidateObjectFunc, opts *metav1.CreateOptions) (runtime.Object, error) {
	if m, err := meta.Accessor(obj); err == nil && !slices.Contains(m.GetFinalizers(), denv1.UndoFinalizer) {
		m.SetFinalizers(append(m.GetFinalizers(), denv1.UndoFinalizer))
	}
	out, err := u.Store.Create(ctx, obj, createValidation, opts)
	if !apierrors.IsAlreadyExists(err) || len(opts.DryRun) > 0 {
		return out, err
	}
	// Store.Create has defaulted, validated and admitted obj in place.
	revived, rerr := u.revive(ctx, obj)
	if errors.Is(rerr, errNotMarked) {
		return nil, err
	}
	return revived, rerr
}

// revive puts obj in place of the marked object with its name. It keeps
// what the server owns: uid, creation time, generation (bumped if the spec
// changed) and status.
func (u Undoable) revive(ctx context.Context, obj runtime.Object) (runtime.Object, error) {
	m, err := meta.Accessor(obj)
	if err != nil {
		return nil, err
	}
	key, err := u.Store.KeyFunc(ctx, m.GetName())
	if err != nil {
		return nil, err
	}
	out := u.Store.NewFunc()
	err = u.Store.Storage.GuaranteedUpdate(ctx, key, out, false, nil, func(existing runtime.Object, _ storage.ResponseMeta) (runtime.Object, *uint64, error) {
		old, err := meta.Accessor(existing)
		if err != nil {
			return nil, nil, err
		}
		if old.GetDeletionTimestamp() == nil || !slices.Contains(old.GetFinalizers(), denv1.UndoFinalizer) {
			return nil, nil, errNotMarked
		}
		n := obj.DeepCopyObject()
		nm, _ := meta.Accessor(n)
		nm.SetUID(old.GetUID())
		nm.SetCreationTimestamp(old.GetCreationTimestamp())
		nm.SetGeneration(old.GetGeneration())
		u.Store.UpdateStrategy.PrepareForUpdate(ctx, n, existing)
		if errs := u.Store.UpdateStrategy.ValidateUpdate(ctx, n, existing); len(errs) > 0 {
			return nil, nil, apierrors.NewInvalid(obj.GetObjectKind().GroupVersionKind().GroupKind(), nm.GetName(), errs)
		}
		return n, nil, nil
	}, false, nil)
	return out, err
}

func (u Undoable) Delete(ctx context.Context, name string, deleteValidation rest.ValidateObjectFunc, opts *metav1.DeleteOptions) (runtime.Object, bool, error) {
	if opts != nil && opts.GracePeriodSeconds != nil && *opts.GracePeriodSeconds == 0 && len(opts.DryRun) == 0 {
		key, err := u.Store.KeyFunc(ctx, name)
		if err != nil {
			return nil, false, err
		}
		err = u.Store.Storage.GuaranteedUpdate(ctx, key, u.Store.NewFunc(), false, nil, func(existing runtime.Object, _ storage.ResponseMeta) (runtime.Object, *uint64, error) {
			m, err := meta.Accessor(existing)
			if err != nil {
				return nil, nil, err
			}
			i := slices.Index(m.GetFinalizers(), denv1.UndoFinalizer)
			if i < 0 {
				return nil, nil, errNotMarked
			}
			m.SetFinalizers(slices.Delete(slices.Clone(m.GetFinalizers()), i, i+1))
			return existing, nil, nil
		}, false, nil)
		// Missing objects are Store.Delete's to report.
		if err != nil && !errors.Is(err, errNotMarked) && !storage.IsNotFound(err) {
			return nil, false, err
		}
	}
	return u.Store.Delete(ctx, name, deleteValidation, opts)
}
