package registry

import (
	"strings"

	"k8s.io/apimachinery/pkg/api/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/registry/generic/registry"
	"k8s.io/apiserver/pkg/storage/names"

	denv1 "mkm.pub/dennet/pkg/apis/den/v1alpha1"
)

// NewHost returns the storage for hosts: other machines running dennet,
// each standing in for the namespace with its name.
func NewHost(typer runtime.ObjectTyper, opts generic.RESTOptionsGetter) (*registry.Store, error) {
	validate := func(obj runtime.Object) field.ErrorList {
		h := obj.(*denv1.Host)
		var errs field.ErrorList
		name := field.NewPath("metadata", "name")
		for _, msg := range validation.ValidateNamespaceName(h.Name, false) {
			errs = append(errs, field.Invalid(name, h.Name, msg))
		}
		if h.Name == metav1.NamespaceDefault {
			errs = append(errs, field.Invalid(name, h.Name, "default is this host's own namespace"))
		}
		ssh := field.NewPath("spec", "ssh")
		switch {
		case h.Spec.SSH == "":
			errs = append(errs, field.Required(ssh, ""))
		case strings.HasPrefix(h.Spec.SSH, "-"):
			errs = append(errs, field.Invalid(ssh, h.Spec.SSH, "must not start with -"))
		}
		if s := h.Spec.SocketPath; s != "" && !strings.HasPrefix(s, "/") {
			errs = append(errs, field.Invalid(field.NewPath("spec", "socketPath"), s, "must be an absolute path"))
		}
		return errs
	}
	return newStore(plainStrategy{typer, names.SimpleNameGenerator, validate, true}, opts,
		denv1.SchemeGroupVersion.WithResource("hosts").GroupResource(), "host",
		func() runtime.Object { return &denv1.Host{} },
		func() runtime.Object { return &denv1.HostList{} })
}
