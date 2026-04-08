package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type SnapshotSpec struct {
	SourceRef    corev1.LocalObjectReference `json:"sourceRef"`
	BackupRunRef corev1.LocalObjectReference `json:"backupRunRef"`
	// StorageRef tells us which backend owns this snapshot artifact.
	StorageRef corev1.LocalObjectReference `json:"storageRef"`
	// StoragePath records the effective backend path used during backup.
	StoragePath string       `json:"storagePath,omitempty"`
	Driver      BackupDriver `json:"driver"`
	Snapshot    string       `json:"snapshot"`
}

type SnapshotStatus struct {
	Phase              ResourcePhase      `json:"phase,omitempty"`
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	StartedAt          *metav1.Time       `json:"startedAt,omitempty"`
	CompletedAt        *metav1.Time       `json:"completedAt,omitempty"`
	Message            string             `json:"message,omitempty"`
	Latest             bool               `json:"latest,omitempty"`
	ArtifactReady      bool               `json:"artifactReady,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Latest",type=boolean,JSONPath=`.status.latest`
// +kubebuilder:printcolumn:name="Ready",type=boolean,JSONPath=`.status.artifactReady`
// +kubebuilder:printcolumn:name="Source",type=string,JSONPath=`.spec.sourceRef.name`
// +kubebuilder:printcolumn:name="Storage",type=string,JSONPath=`.spec.storageRef.name`
// +kubebuilder:printcolumn:name="Completed",type=date,JSONPath=`.status.completedAt`
// +kubebuilder:printcolumn:name="Snapshot",type=string,priority=1,JSONPath=`.spec.snapshot`
// +kubebuilder:printcolumn:name="Message",type=string,priority=1,JSONPath=`.status.message`
// +kubebuilder:resource:shortName=snap
type Snapshot struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SnapshotSpec   `json:"spec,omitempty"`
	Status SnapshotStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type SnapshotList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Snapshot `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Snapshot{}, &SnapshotList{})
}
