package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type BackupSourceSpec struct {
	Driver       BackupDriver               `json:"driver"`
	TargetRef    *NamespacedObjectReference `json:"targetRef,omitempty"`
	Endpoint     EndpointSpec               `json:"endpoint,omitempty"`
	DriverConfig DriverConfig               `json:"driverConfig,omitempty"`
	Paused       bool                       `json:"paused,omitempty"`
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
// +kubebuilder:printcolumn:name="Driver",type=string,JSONPath=`.spec.driver`
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
