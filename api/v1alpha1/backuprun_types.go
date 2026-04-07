package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type BackupRunSpec struct {
	PolicyRef      *corev1.LocalObjectReference  `json:"policyRef,omitempty"`
	SourceRef      corev1.LocalObjectReference   `json:"sourceRef"`
	RepositoryRefs []corev1.LocalObjectReference `json:"repositoryRefs,omitempty"`
	Reason         string                        `json:"reason,omitempty"`
	Snapshot       string                        `json:"snapshot,omitempty"`
	DriverConfig   DriverConfig                  `json:"driverConfig,omitempty"`
}

type BackupRunStatus struct {
	Phase              ResourcePhase         `json:"phase,omitempty"`
	ObservedGeneration int64                 `json:"observedGeneration,omitempty"`
	StartedAt          *metav1.Time          `json:"startedAt,omitempty"`
	CompletedAt        *metav1.Time          `json:"completedAt,omitempty"`
	JobNames           []string              `json:"jobNames,omitempty"`
	Repositories       []RepositoryRunStatus `json:"repositories,omitempty"`
	Conditions         []metav1.Condition    `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type BackupRun struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BackupRunSpec   `json:"spec,omitempty"`
	Status BackupRunStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type BackupRunList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BackupRun `json:"items"`
}

func init() {
	SchemeBuilder.Register(&BackupRun{}, &BackupRunList{})
}
