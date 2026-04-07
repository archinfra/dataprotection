package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type BackupRepositorySpec struct {
	Type    RepositoryType     `json:"type"`
	Default bool               `json:"default,omitempty"`
	NFS     *NFSRepositorySpec `json:"nfs,omitempty"`
	S3      *S3RepositorySpec  `json:"s3,omitempty"`
}

type BackupRepositoryStatus struct {
	Phase              ResourcePhase      `json:"phase,omitempty"`
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	LastValidatedTime  *metav1.Time       `json:"lastValidatedTime,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type BackupRepository struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BackupRepositorySpec   `json:"spec,omitempty"`
	Status BackupRepositoryStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type BackupRepositoryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BackupRepository `json:"items"`
}

func init() {
	SchemeBuilder.Register(&BackupRepository{}, &BackupRepositoryList{})
}
