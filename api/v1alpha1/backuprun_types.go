package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type BackupRunSpec struct {
	// PolicyRef is optional for manual runs, but when set the run inherits the
	// policy defaults and writes into the same storage path layout.
	PolicyRef *corev1.LocalObjectReference `json:"policyRef,omitempty"`
	// SourceRef is always required so the run remains self-describing.
	SourceRef corev1.LocalObjectReference `json:"sourceRef"`
	// StorageRefs optionally narrows the policy storages to a subset. If empty
	// and PolicyRef is set, the run uses all storages from the policy.
	StorageRefs  []corev1.LocalObjectReference `json:"storageRefs,omitempty"`
	Reason       string                        `json:"reason,omitempty"`
	Snapshot     string                        `json:"snapshot,omitempty"`
	DriverConfig DriverConfig                  `json:"driverConfig,omitempty"`
}

type BackupRunStatus struct {
	Phase              ResourcePhase      `json:"phase,omitempty"`
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	StartedAt          *metav1.Time       `json:"startedAt,omitempty"`
	CompletedAt        *metav1.Time       `json:"completedAt,omitempty"`
	JobNames           []string           `json:"jobNames,omitempty"`
	Storages           []StorageRunStatus `json:"storages,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=br
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
