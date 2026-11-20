package registry

import (
	"context"
	"path/filepath"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/registry/generic/registry"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/apiserver/pkg/storage/names"

	denv1 "mkm.pub/dennet/pkg/apis/den/v1alpha1"
)

// NewExtension returns the storage for extensions, the programs that draw
// Surfaces of kind ui, and extensions/status, which only dennet writes.
func NewExtension(typer runtime.ObjectTyper, opts generic.RESTOptionsGetter) (*registry.Store, rest.Storage, error) {
	s := extensionStrategy{plainStrategy{typer, names.SimpleNameGenerator, func(obj runtime.Object) field.ErrorList {
		e := obj.(*denv1.Extension)
		p := field.NewPath("spec", "endpoint")
		switch {
		case e.Spec.Endpoint == "":
			return field.ErrorList{field.Required(p, "")}
		case !filepath.IsAbs(e.Spec.Endpoint):
			return field.ErrorList{field.Invalid(p, e.Spec.Endpoint, "must be an absolute path")}
		}
		return nil
	}, false}}
	store, err := newStore(s, opts, denv1.SchemeGroupVersion.WithResource("extensions").GroupResource(), "extension",
		func() runtime.Object { return &denv1.Extension{} },
		func() runtime.Object { return &denv1.ExtensionList{} })
	if err != nil {
		return nil, nil, err
	}
	status := *store
	status.UpdateStrategy = extensionStatusStrategy{s}
	return store, &statusREST{&status}, nil
}

// extensionStrategy keeps status for dennet to write, on extensions/status.
type extensionStrategy struct{ plainStrategy }

func (extensionStrategy) PrepareForCreate(_ context.Context, obj runtime.Object) {
	obj.(*denv1.Extension).Status = denv1.ExtensionStatus{}
}

func (extensionStrategy) PrepareForUpdate(_ context.Context, obj, old runtime.Object) {
	obj.(*denv1.Extension).Status = old.(*denv1.Extension).Status
}

// extensionStatusStrategy is used for writes to extensions/status: only status changes.
type extensionStatusStrategy struct{ extensionStrategy }

func (extensionStatusStrategy) PrepareForUpdate(_ context.Context, obj, old runtime.Object) {
	obj.(*denv1.Extension).Spec = old.(*denv1.Extension).Spec
}

// Owner is the reference to the Extension that owns obj, or nil if none does.
func Owner(obj runtime.Object) *metav1.OwnerReference {
	m, err := meta.Accessor(obj)
	if err != nil {
		return nil
	}
	for _, r := range m.GetOwnerReferences() {
		if r.Kind == "Extension" && r.APIVersion == denv1.SchemeGroupVersion.String() && r.Name != "" {
			return &r
		}
	}
	return nil
}
