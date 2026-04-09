package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type BackupStorageSpec struct {
	Type  StorageType       `json:"type"`
	NFS   *NFSStorageSpec   `json:"nfs,omitempty"`
	MinIO *MinIOStorageSpec `json:"minio,omitempty"`
}

type BackupStorageStatus struct {
	Phase              ResourcePhase      `json:"phase,omitempty"`
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	LastValidatedTime  *metav1.Time       `json:"lastValidatedTime,omitempty"`
	LastProbeTime      *metav1.Time       `json:"lastProbeTime,omitempty"`
	LastProbeResult    ProbeResult        `json:"lastProbeResult,omitempty"`
	LastProbeMessage   string             `json:"lastProbeMessage,omitempty"`
	Message            string             `json:"message,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Probe",type=string,JSONPath=`.status.lastProbeResult`
// +kubebuilder:printcolumn:name="ProbedAt",type=date,JSONPath=`.status.lastProbeTime`
// +kubebuilder:printcolumn:name="Message",type=string,priority=1,JSONPath=`.status.message`
// +kubebuilder:resource:shortName=bst
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
