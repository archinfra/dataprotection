package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type RestoreRequestSpec struct {
	SourceRef     corev1.LocalObjectReference  `json:"sourceRef"`
	BackupRunRef  *corev1.LocalObjectReference `json:"backupRunRef,omitempty"`
	SnapshotRef   *corev1.LocalObjectReference `json:"snapshotRef,omitempty"`
	RepositoryRef *corev1.LocalObjectReference `json:"repositoryRef,omitempty"`
	Snapshot      string                       `json:"snapshot,omitempty"`
	Target        RestoreTargetSpec            `json:"target,omitempty"`
	Reason        string                       `json:"reason,omitempty"`
}

type RestoreRequestStatus struct {
	Phase              ResourcePhase      `json:"phase,omitempty"`
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	StartedAt          *metav1.Time       `json:"startedAt,omitempty"`
	CompletedAt        *metav1.Time       `json:"completedAt,omitempty"`
	JobName            string             `json:"jobName,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type RestoreRequest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RestoreRequestSpec   `json:"spec,omitempty"`
	Status RestoreRequestStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type RestoreRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RestoreRequest `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RestoreRequest{}, &RestoreRequestList{})
}
