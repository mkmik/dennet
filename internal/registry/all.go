package registry

import "k8s.io/apiserver/pkg/registry/generic/registry"

// InAll puts a resource in the "all" category, which is what dennet get all lists.
type InAll struct{ *registry.Store }

func (InAll) Categories() []string { return []string{"all"} }
