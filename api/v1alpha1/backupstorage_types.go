package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type BackupStorageSpec struct {
	// Type selects which backend driver this reusable storage uses.
	Type StorageType `json:"type"`
	// Default marks the namespace-level default storage for future expansion.
	Default bool `json:"default,omitempty"`
	// NFS config is required when type=nfs.
	NFS *NFSStorageSpec `json:"nfs,omitempty"`
	// S3 config is required when type=s3.
	S3 *S3StorageSpec `json:"s3,omitempty"`
}

type BackupStorageStatus struct {
	Phase              ResourcePhase      `json:"phase,omitempty"`
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	LastValidatedTime  *metav1.Time       `json:"lastValidatedTime,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type BackupStorage struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BackupStorageSpec   `json:"spec,omitempty"`
	Status BackupStorageStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type BackupStorageList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BackupStorage `json:"items"`
}

func init() {
	SchemeBuilder.Register(&BackupStorage{}, &BackupStorageList{})
}
