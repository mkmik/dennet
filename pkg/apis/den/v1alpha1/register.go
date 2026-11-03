package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const GroupName = "den.mkm.pub"

var SchemeGroupVersion = schema.GroupVersion{Group: GroupName, Version: "v1alpha1"}

// AddToScheme registers the types under v1alpha1 and, since dennet has no
// separate internal types, under the internal version too.
func AddToScheme(s *runtime.Scheme) error {
	for _, gv := range []schema.GroupVersion{SchemeGroupVersion, {Group: GroupName, Version: runtime.APIVersionInternal}} {
		s.AddKnownTypes(gv, &Den{}, &DenList{})
	}
	metav1.AddToGroupVersion(s, SchemeGroupVersion)
	s.AddTypeDefaultingFunc(&Den{}, func(o any) { SetDefaults(o.(*Den)) })
	return nil
}

func SetDefaults(d *Den) {
	if d.Spec.RestartPolicy == "" {
		d.Spec.RestartPolicy = RestartPolicyNever
	}
	if d.Spec.History.MemoryLines == 0 {
		d.Spec.History.MemoryLines = 5000
	}
}
