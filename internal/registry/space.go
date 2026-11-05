package registry

import (
	"k8s.io/apimachinery/pkg/api/validation"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/registry/generic/registry"

	denv1 "mkm.pub/dennet/pkg/apis/den/v1alpha1"
)

// NewSpace returns the storage for spaces, the nodes of dense's sidebar tree.
func NewSpace(typer runtime.ObjectTyper, opts generic.RESTOptionsGetter) (*registry.Store, error) {
	return newPlain(typer, opts, denv1.SchemeGroupVersion.WithResource("spaces").GroupResource(), "space",
		func() runtime.Object { return &denv1.Space{} },
		func() runtime.Object { return &denv1.SpaceList{} },
		func(obj runtime.Object) field.ErrorList {
			s := obj.(*denv1.Space)
			p := field.NewPath("spec")
			var errs field.ErrorList
			// ponytail: only self-parenting is caught; longer cycles and
			// missing parents are left for dense to draw as top level.
			if s.Spec.Parent == s.Name {
				errs = append(errs, field.Invalid(p.Child("parent"), s.Spec.Parent, "a space cannot be its own parent"))
			}
			errs = append(errs, validRef(p.Child("parent"), s.Spec.Parent)...)
			for i, b := range s.Spec.Badges {
				if b.Text == "" {
					errs = append(errs, field.Required(p.Child("badges").Index(i).Child("text"), ""))
				}
			}
			switch s.Spec.State {
			case "", denv1.SpaceIdle, denv1.SpaceBusy, denv1.SpaceWaiting, denv1.SpaceError:
			default:
				errs = append(errs, field.NotSupported(p.Child("state"), s.Spec.State,
					[]denv1.SpaceState{denv1.SpaceIdle, denv1.SpaceBusy, denv1.SpaceWaiting, denv1.SpaceError}))
			}
			return errs
		})
}

// NewView returns the storage for views, the tabs inside a Space.
func NewView(typer runtime.ObjectTyper, opts generic.RESTOptionsGetter) (*registry.Store, error) {
	return newPlain(typer, opts, denv1.SchemeGroupVersion.WithResource("views").GroupResource(), "view",
		func() runtime.Object { return &denv1.View{} },
		func() runtime.Object { return &denv1.ViewList{} },
		func(obj runtime.Object) field.ErrorList {
			v := obj.(*denv1.View)
			p := field.NewPath("spec")
			var errs field.ErrorList
			if v.Spec.Space == "" {
				errs = append(errs, field.Required(p.Child("space"), ""))
			}
			errs = append(errs, validRef(p.Child("space"), v.Spec.Space)...)
			switch v.Spec.Kind {
			case denv1.ViewTerminal:
				if v.Spec.Terminal == nil || v.Spec.Terminal.Den == "" {
					errs = append(errs, field.Required(p.Child("terminal", "den"), "kind is terminal"))
				} else {
					errs = append(errs, validRef(p.Child("terminal", "den"), v.Spec.Terminal.Den)...)
				}
			default:
				errs = append(errs, field.NotSupported(p.Child("kind"), v.Spec.Kind, []denv1.ViewKind{denv1.ViewTerminal}))
			}
			return errs
		})
}

// validRef checks that a non-empty reference could name an object.
func validRef(p *field.Path, name string) field.ErrorList {
	var errs field.ErrorList
	if name == "" {
		return nil
	}
	for _, msg := range validation.NameIsDNSSubdomain(name, false) {
		errs = append(errs, field.Invalid(p, name, msg))
	}
	return errs
}
