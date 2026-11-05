package registry

import (
	coordinationv1 "k8s.io/api/coordination/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/registry/generic/registry"
)

// NewLease returns the storage for coordination.k8s.io/v1 leases. Dens renew
// one Lease each; dennet only reads them.
func NewLease(typer runtime.ObjectTyper, opts generic.RESTOptionsGetter) (*registry.Store, error) {
	return newPlain(typer, opts, coordinationv1.Resource("leases"), "lease",
		func() runtime.Object { return &coordinationv1.Lease{} },
		func() runtime.Object { return &coordinationv1.LeaseList{} }, nil)
}
