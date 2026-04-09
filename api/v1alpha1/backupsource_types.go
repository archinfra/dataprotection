package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type BackupSourceSpec struct {
	AddonRef   corev1.LocalObjectReference `json:"addonRef"`
	TargetRef  *NamespacedObjectReference  `json:"targetRef,omitempty"`
	Endpoint   EndpointSpec                `json:"endpoint,omitempty"`
	Parameters map[string]string           `json:"parameters,omitempty"`
	SecretRefs []SecretParameterReference  `json:"secretRefs,omitempty"`
	Paused     bool                        `json:"paused,omitempty"`
}

type BackupSourceStatus struct {
	Phase              ResourcePhase      `json:"phase,omitempty"`
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	LastValidatedTime  *metav1.Time       `json:"lastValidatedTime,omitempty"`
	Message            string             `json:"message,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Addon",type=string,JSONPath=`.spec.addonRef.name`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Paused",type=boolean,JSONPath=`.spec.paused`
// +kubebuilder:printcolumn:name="Message",type=string,priority=1,JSONPath=`.status.message`
// +kubebuilder:resource:shortName=bsrc
type BackupSource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BackupSourceSpec   `json:"spec,omitempty"`
	Status BackupSourceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type BackupSourceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BackupSource `json:"items"`
}

func init() {
	SchemeBuilder.Register(&BackupSource{}, &BackupSourceList{})
}
