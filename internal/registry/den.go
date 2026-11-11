// Package registry holds the REST storage and strategies for the kinds
// dennet serves.
package registry

import (
	"context"
	"fmt"
	"slices"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/duration"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/registry/generic/registry"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/apiserver/pkg/storage/names"

	denv1 "mkm.pub/dennet/pkg/apis/den/v1alpha1"
)

// NewDen returns the storage for dens and dens/status.
func NewDen(typer runtime.ObjectTyper, opts generic.RESTOptionsGetter) (*registry.Store, rest.Storage, error) {
	s := denStrategy{typer, names.SimpleNameGenerator}
	store := &registry.Store{
		NewFunc:                   func() runtime.Object { return &denv1.Den{} },
		NewListFunc:               func() runtime.Object { return &denv1.DenList{} },
		DefaultQualifiedResource:  denv1.SchemeGroupVersion.WithResource("dens").GroupResource(),
		SingularQualifiedResource: denv1.SchemeGroupVersion.WithResource("den").GroupResource(),
		CreateStrategy:            s,
		UpdateStrategy:            s,
		DeleteStrategy:            s,
		TableConvertor:            denTable{},
	}
	if err := store.CompleteWithOptions(&generic.StoreOptions{RESTOptions: opts}); err != nil {
		return nil, nil, err
	}
	status := *store
	status.UpdateStrategy = denStatusStrategy{s}
	return store, &statusREST{&status}, nil
}

type denStrategy struct {
	runtime.ObjectTyper
	names.NameGenerator
}

func (denStrategy) NamespaceScoped() bool                         { return true }
func (denStrategy) AllowCreateOnUpdate(context.Context) bool      { return false }
func (denStrategy) AllowUnconditionalUpdate(context.Context) bool { return true }
func (denStrategy) Canonicalize(runtime.Object)                   {}

func (denStrategy) WarningsOnCreate(context.Context, runtime.Object) []string { return nil }
func (denStrategy) WarningsOnUpdate(context.Context, runtime.Object, runtime.Object) []string {
	return nil
}

// PrepareForCreate drops any status (only den writes it) and adds the
// finalizer that keeps the Den until its den has stopped.
func (denStrategy) PrepareForCreate(_ context.Context, obj runtime.Object) {
	d := obj.(*denv1.Den)
	d.Status = denv1.DenStatus{}
	d.Generation = 1
	if !slices.Contains(d.Finalizers, denv1.DenFinalizer) {
		d.Finalizers = append(d.Finalizers, denv1.DenFinalizer)
	}
}

// PrepareForUpdate keeps status as stored and bumps generation on spec changes.
func (denStrategy) PrepareForUpdate(_ context.Context, obj, old runtime.Object) {
	n, o := obj.(*denv1.Den), old.(*denv1.Den)
	n.Status = o.Status
	if !equality.Semantic.DeepEqual(n.Spec, o.Spec) {
		n.Generation = o.Generation + 1
	}
}

func (denStrategy) Validate(_ context.Context, obj runtime.Object) field.ErrorList {
	d := obj.(*denv1.Den)
	errs := validation.ValidateObjectMeta(&d.ObjectMeta, true, validation.NameIsDNSSubdomain, field.NewPath("metadata"))
	return append(errs, validateSpec(&d.Spec, field.NewPath("spec"))...)
}

func (s denStrategy) ValidateUpdate(ctx context.Context, obj, old runtime.Object) field.ErrorList {
	n, o := obj.(*denv1.Den), old.(*denv1.Den)
	errs := validateSpec(&n.Spec, field.NewPath("spec"))
	return append(errs, validation.ValidateImmutableField(n.Spec.Command, o.Spec.Command, field.NewPath("spec", "command"))...)
}

func validateSpec(s *denv1.DenSpec, p *field.Path) field.ErrorList {
	var errs field.ErrorList
	for i, e := range s.Env {
		if e.Name == "" {
			errs = append(errs, field.Required(p.Child("env").Index(i).Child("name"), ""))
		}
	}
	// den stores cols and rows as u16.
	if s.Size != nil && (s.Size.Cols <= 0 || s.Size.Rows <= 0 || s.Size.Cols > 65535 || s.Size.Rows > 65535) {
		errs = append(errs, field.Invalid(p.Child("size"), s.Size, "cols and rows must be between 1 and 65535"))
	}
	if s.History.MemoryLines < 0 {
		errs = append(errs, field.Invalid(p.Child("history", "memoryLines"), s.History.MemoryLines, "must not be negative"))
	}
	switch s.RestartPolicy {
	case denv1.RestartPolicyNever, denv1.RestartPolicyOnFailure:
	default:
		errs = append(errs, field.NotSupported(p.Child("restartPolicy"), s.RestartPolicy,
			[]denv1.RestartPolicy{denv1.RestartPolicyNever, denv1.RestartPolicyOnFailure}))
	}
	return errs
}

// denStatusStrategy is used for writes to dens/status: only status changes.
type denStatusStrategy struct{ denStrategy }

func (denStatusStrategy) PrepareForUpdate(_ context.Context, obj, old runtime.Object) {
	n, o := obj.(*denv1.Den), old.(*denv1.Den)
	n.Spec = o.Spec
}

func (denStatusStrategy) ValidateUpdate(_ context.Context, obj, _ runtime.Object) field.ErrorList {
	d := obj.(*denv1.Den)
	switch d.Status.Phase {
	case "", denv1.DenPending, denv1.DenRunning, denv1.DenExited, denv1.DenFailed, denv1.DenLost:
		return nil
	}
	return field.ErrorList{field.NotSupported(field.NewPath("status", "phase"), d.Status.Phase,
		[]denv1.DenPhase{denv1.DenPending, denv1.DenRunning, denv1.DenExited, denv1.DenFailed, denv1.DenLost})}
}

// statusREST exposes only get and update (and so patch) on a status subresource.
type statusREST struct{ store *registry.Store }

func (r *statusREST) New() runtime.Object { return r.store.New() }
func (r *statusREST) Destroy()            {}

func (r *statusREST) Get(ctx context.Context, name string, opts *metav1.GetOptions) (runtime.Object, error) {
	return r.store.Get(ctx, name, opts)
}

func (r *statusREST) Update(ctx context.Context, name string, objInfo rest.UpdatedObjectInfo, createValidation rest.ValidateObjectFunc,
	updateValidation rest.ValidateObjectUpdateFunc, _ bool, opts *metav1.UpdateOptions,
) (runtime.Object, bool, error) {
	// Never create through status.
	return r.store.Update(ctx, name, uidChecked{objInfo, r.store.DefaultQualifiedResource}, createValidation, updateValidation, false, opts)
}

// uidChecked turns a status write carrying another incarnation's
// metadata.uid into 409 Conflict (rather than 422 for an immutable field),
// which is how den learns its object was replaced and stops writing.
type uidChecked struct {
	rest.UpdatedObjectInfo
	resource schema.GroupResource
}

func (u uidChecked) UpdatedObject(ctx context.Context, old runtime.Object) (runtime.Object, error) {
	obj, err := u.UpdatedObjectInfo.UpdatedObject(ctx, old)
	if err != nil {
		return nil, err
	}
	n, _ := meta.Accessor(obj)
	o, _ := meta.Accessor(old)
	if n.GetUID() != "" && n.GetUID() != o.GetUID() {
		return nil, apierrors.NewConflict(u.resource, o.GetName(),
			fmt.Errorf("uid %s does not match the stored object (%s): it was replaced", n.GetUID(), o.GetUID()))
	}
	return obj, nil
}

// denTable is what `kubectl get dens` prints.
type denTable struct{}

func (denTable) ConvertToTable(_ context.Context, obj runtime.Object, _ runtime.Object) (*metav1.Table, error) {
	t := &metav1.Table{ColumnDefinitions: []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name"},
		{Name: "Phase", Type: "string"},
		{Name: "Host", Type: "string"},
		{Name: "Title", Type: "string"},
		{Name: "Age", Type: "date"},
	}}
	row := func(d *denv1.Den) {
		age := "<unknown>"
		if !d.CreationTimestamp.IsZero() {
			age = duration.HumanDuration(time.Since(d.CreationTimestamp.Time))
		}
		t.Rows = append(t.Rows, metav1.TableRow{
			Cells:  []any{d.Name, string(d.Status.Phase), d.Status.Host, d.Status.Title, age},
			Object: runtime.RawExtension{Object: d},
		})
	}
	switch o := obj.(type) {
	case *denv1.Den:
		row(o)
		t.ResourceVersion = o.ResourceVersion
	case *denv1.DenList:
		for i := range o.Items {
			row(&o.Items[i])
		}
		t.ResourceVersion, t.Continue = o.ResourceVersion, o.Continue
	default:
		return nil, fmt.Errorf("unexpected type %T", obj)
	}
	return t, nil
}
