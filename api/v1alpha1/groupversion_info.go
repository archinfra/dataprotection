// Package v1alpha1 contains API Schema definitions for the data protection v1alpha1 API group.
// +kubebuilder:object:generate=true
// +groupName=dataprotection.archinfra.io
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// GroupVersion identifies the API group and version for the operator.
	GroupVersion = schema.GroupVersion{Group: "dataprotection.archinfra.io", Version: "v1alpha1"}

	// SchemeBuilder registers API types into a scheme.
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// AddToScheme adds the API group to the supplied scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)
