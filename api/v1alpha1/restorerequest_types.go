package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type RestoreRequestSpec struct {
	SourceRef    corev1.LocalObjectReference  `json:"sourceRef"`
	BackupRunRef *corev1.LocalObjectReference `json:"backupRunRef,omitempty"`
	// SnapshotRef is the preferred restore entrypoint because it carries both
	// the storage identity and the resolved storage path.
	SnapshotRef *corev1.LocalObjectReference `json:"snapshotRef,omitempty"`
	// StorageRef is only needed for raw restores that do not point at a
	// snapshot or a single-storage BackupRun.
	StorageRef *corev1.LocalObjectReference `json:"storageRef,omitempty"`
	// StoragePath is the effective path inside the selected storage backend.
	StoragePath string            `json:"storagePath,omitempty"`
	Snapshot    string            `json:"snapshot,omitempty"`
	Target      RestoreTargetSpec `json:"target,omitempty"`
	Reason      string            `json:"reason,omitempty"`
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
// +kubebuilder:resource:shortName=rr
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
