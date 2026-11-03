package registry

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apiserver/pkg/admission"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/generic"
	"k8s.io/apiserver/pkg/registry/generic/registry"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/apiserver/pkg/storage/names"
)

// Namespaces is the storage for core/v1 namespaces.
// ponytail: no lifecycle. Deleting a namespace leaves its objects in place,
// and nothing stops writes into a namespace being deleted.
type Namespaces struct{ *registry.Store }

func NewNamespace(typer runtime.ObjectTyper, opts generic.RESTOptionsGetter) (*Namespaces, error) {
	s := namespaceStrategy{typer, names.SimpleNameGenerator}
	store := &registry.Store{
		NewFunc:                   func() runtime.Object { return &corev1.Namespace{} },
		NewListFunc:               func() runtime.Object { return &corev1.NamespaceList{} },
		DefaultQualifiedResource:  corev1.Resource("namespaces"),
		SingularQualifiedResource: corev1.Resource("namespace"),
		CreateStrategy:            s,
		UpdateStrategy:            s,
		DeleteStrategy:            s,
		TableConvertor:            rest.NewDefaultTableConvertor(corev1.Resource("namespaces")),
	}
	return &Namespaces{store}, store.CompleteWithOptions(&generic.StoreOptions{RESTOptions: opts})
}

func (*Namespaces) ShortNames() []string { return []string{"ns"} }

// Ensure creates the namespace unless it exists.
func (n *Namespaces) Ensure(ctx context.Context, name string) error {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	_, err := n.Create(genericapirequest.WithNamespace(ctx, ""), ns, rest.ValidateAllObjectFunc, &metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

// AutoCreate is admission that creates the namespace of any object being
// created in one. dens write into a namespace without creating it first,
// and kubectl reports `namespaces "x" not found` for objects in a namespace
// that has no Namespace.
func (n *Namespaces) AutoCreate() admission.MutationInterface {
	return autoCreate{admission.NewHandler(admission.Create), n}
}

type autoCreate struct {
	*admission.Handler
	ns *Namespaces
}

func (a autoCreate) Admit(ctx context.Context, attrs admission.Attributes, _ admission.ObjectInterfaces) error {
	// A Namespace's own create carries its name as the namespace.
	if attrs.GetNamespace() == "" || attrs.IsDryRun() || attrs.GetResource().GroupResource() == corev1.Resource("namespaces") {
		return nil
	}
	return a.ns.Ensure(ctx, attrs.GetNamespace())
}

type namespaceStrategy struct {
	runtime.ObjectTyper
	names.NameGenerator
}

func (namespaceStrategy) NamespaceScoped() bool                         { return false }
func (namespaceStrategy) AllowCreateOnUpdate(context.Context) bool      { return false }
func (namespaceStrategy) AllowUnconditionalUpdate(context.Context) bool { return true }
func (namespaceStrategy) Canonicalize(runtime.Object)                   {}
func (namespaceStrategy) WarningsOnCreate(context.Context, runtime.Object) []string {
	return nil
}
func (namespaceStrategy) WarningsOnUpdate(context.Context, runtime.Object, runtime.Object) []string {
	return nil
}

func (namespaceStrategy) PrepareForCreate(_ context.Context, obj runtime.Object) {
	obj.(*corev1.Namespace).Status = corev1.NamespaceStatus{Phase: corev1.NamespaceActive}
}

func (namespaceStrategy) PrepareForUpdate(_ context.Context, obj, old runtime.Object) {
	obj.(*corev1.Namespace).Status = old.(*corev1.Namespace).Status
}

func (namespaceStrategy) Validate(_ context.Context, obj runtime.Object) field.ErrorList {
	ns := obj.(*corev1.Namespace)
	return validation.ValidateObjectMeta(&ns.ObjectMeta, false, validation.ValidateNamespaceName, field.NewPath("metadata"))
}

func (namespaceStrategy) ValidateUpdate(context.Context, runtime.Object, runtime.Object) field.ErrorList {
	return nil
}
