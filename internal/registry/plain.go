package registry

import (
	"context"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/validation"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/registry/generic/registry"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/apiserver/pkg/storage/names"
)

// newPlain returns the storage for a namespaced kind with no status and no
// lifecycle: metadata is validated, then spec by validate (nil for none).
func newPlain(typer runtime.ObjectTyper, opts generic.RESTOptionsGetter, gr schema.GroupResource, singular string,
	newObj, newList func() runtime.Object, validate func(runtime.Object) field.ErrorList,
) (*registry.Store, error) {
	s := plainStrategy{typer, names.SimpleNameGenerator, validate}
	store := &registry.Store{
		NewFunc:                   newObj,
		NewListFunc:               newList,
		DefaultQualifiedResource:  gr,
		SingularQualifiedResource: schema.GroupResource{Group: gr.Group, Resource: singular},
		CreateStrategy:            s,
		UpdateStrategy:            s,
		DeleteStrategy:            s,
		TableConvertor:            rest.NewDefaultTableConvertor(gr),
	}
	return store, store.CompleteWithOptions(&generic.StoreOptions{RESTOptions: opts})
}

type plainStrategy struct {
	runtime.ObjectTyper
	names.NameGenerator
	validate func(runtime.Object) field.ErrorList
}

func (plainStrategy) NamespaceScoped() bool                                            { return true }
func (plainStrategy) AllowCreateOnUpdate(context.Context) bool                         { return false }
func (plainStrategy) AllowUnconditionalUpdate(context.Context) bool                    { return true }
func (plainStrategy) Canonicalize(runtime.Object)                                      {}
func (plainStrategy) PrepareForCreate(context.Context, runtime.Object)                 {}
func (plainStrategy) PrepareForUpdate(context.Context, runtime.Object, runtime.Object) {}
func (plainStrategy) WarningsOnCreate(context.Context, runtime.Object) []string        { return nil }
func (plainStrategy) WarningsOnUpdate(context.Context, runtime.Object, runtime.Object) []string {
	return nil
}

func (s plainStrategy) Validate(_ context.Context, obj runtime.Object) field.ErrorList {
	m, err := meta.Accessor(obj)
	if err != nil {
		return field.ErrorList{field.InternalError(field.NewPath("metadata"), err)}
	}
	errs := validation.ValidateObjectMetaAccessor(m, true, validation.NameIsDNSSubdomain, field.NewPath("metadata"))
	return append(errs, s.spec(obj)...)
}

func (s plainStrategy) ValidateUpdate(_ context.Context, obj, _ runtime.Object) field.ErrorList {
	return s.spec(obj)
}

func (s plainStrategy) spec(obj runtime.Object) field.ErrorList {
	if s.validate == nil {
		return nil
	}
	return s.validate(obj)
}
