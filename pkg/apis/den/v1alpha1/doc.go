// Package v1alpha1 holds the den.mkm.pub/v1alpha1 API types served by dennet.
//
// +k8s:deepcopy-gen=package
// +k8s:openapi-gen=true
// +k8s:openapi-model-package=pub.mkm.den.v1alpha1
// +groupName=den.mkm.pub
package v1alpha1

//go:generate go tool deepcopy-gen --output-file zz_generated.deepcopy.go --go-header-file ../../../../hack/boilerplate.go.txt .
//go:generate go tool openapi-gen --output-file zz_generated.openapi.go --go-header-file ../../../../hack/boilerplate.go.txt --output-dir ../../../generated/openapi --output-pkg mkm.pub/dennet/pkg/generated/openapi --report-filename /dev/null --output-model-name-file zz_generated.model_name.go --readonly-pkg k8s.io/apimachinery/pkg/apis/meta/v1 --readonly-pkg k8s.io/apimachinery/pkg/runtime --readonly-pkg k8s.io/apimachinery/pkg/version --readonly-pkg k8s.io/apimachinery/pkg/api/resource --readonly-pkg k8s.io/apimachinery/pkg/types --readonly-pkg k8s.io/apimachinery/pkg/util/intstr --readonly-pkg k8s.io/api/coordination/v1 --readonly-pkg k8s.io/api/core/v1 k8s.io/apimachinery/pkg/apis/meta/v1 k8s.io/apimachinery/pkg/runtime k8s.io/apimachinery/pkg/version k8s.io/apimachinery/pkg/api/resource k8s.io/api/coordination/v1 k8s.io/api/core/v1 .
