package registry

import (
	"context"

	coordinationv1 "k8s.io/api/coordination/v1"
	"k8s.io/apimachinery/pkg/api/validation"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/registry/generic/registry"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/apiserver/pkg/storage/names"
)

// NewLease returns the storage for coordination.k8s.io/v1 leases. Dens renew
// one Lease each; dennet only reads them.
func NewLease(typer runtime.ObjectTyper, opts generic.RESTOptionsGetter) (*registry.Store, error) {
	s := leaseStrategy{typer, names.SimpleNameGenerator}
	store := &registry.Store{
		NewFunc:                   func() runtime.Object { return &coordinationv1.Lease{} },
		NewListFunc:               func() runtime.Object { return &coordinationv1.LeaseList{} },
		DefaultQualifiedResource:  coordinationv1.SchemeGroupVersion.WithResource("leases").GroupResource(),
		SingularQualifiedResource: coordinationv1.SchemeGroupVersion.WithResource("lease").GroupResource(),
		CreateStrategy:            s,
		UpdateStrategy:            s,
		DeleteStrategy:            s,
		TableConvertor:            rest.NewDefaultTableConvertor(coordinationv1.Resource("leases")),
	}
	return store, store.CompleteWithOptions(&generic.StoreOptions{RESTOptions: opts})
}

type leaseStrategy struct {
	runtime.ObjectTyper
	names.NameGenerator
}

func (leaseStrategy) NamespaceScoped() bool                                            { return true }
func (leaseStrategy) AllowCreateOnUpdate(context.Context) bool                         { return false }
func (leaseStrategy) AllowUnconditionalUpdate(context.Context) bool                    { return true }
func (leaseStrategy) Canonicalize(runtime.Object)                                      {}
func (leaseStrategy) PrepareForCreate(context.Context, runtime.Object)                 {}
func (leaseStrategy) PrepareForUpdate(context.Context, runtime.Object, runtime.Object) {}
func (leaseStrategy) WarningsOnCreate(context.Context, runtime.Object) []string        { return nil }
func (leaseStrategy) WarningsOnUpdate(context.Context, runtime.Object, runtime.Object) []string {
	return nil
}

func (leaseStrategy) Validate(_ context.Context, obj runtime.Object) field.ErrorList {
	l := obj.(*coordinationv1.Lease)
	return validation.ValidateObjectMeta(&l.ObjectMeta, true, validation.NameIsDNSSubdomain, field.NewPath("metadata"))
}

func (leaseStrategy) ValidateUpdate(context.Context, runtime.Object, runtime.Object) field.ErrorList {
	return nil
}
