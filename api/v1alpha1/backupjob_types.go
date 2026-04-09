package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type BackupJobSpec struct {
	PolicyRef        *corev1.LocalObjectReference  `json:"policyRef,omitempty"`
	SourceRef        corev1.LocalObjectReference   `json:"sourceRef"`
	StorageRef       corev1.LocalObjectReference   `json:"storageRef"`
	RetentionRef     *corev1.LocalObjectReference  `json:"retentionRef,omitempty"`
	NotificationRefs []corev1.LocalObjectReference `json:"notificationRefs,omitempty"`
	JobRuntime       JobRuntimeSpec                `json:"jobRuntime,omitempty"`
	SnapshotName     string                        `json:"snapshotName,omitempty"`
	Reason           string                        `json:"reason,omitempty"`
}

type BackupJobStatus struct {
	Phase               ResourcePhase              `json:"phase,omitempty"`
	ObservedGeneration  int64                      `json:"observedGeneration,omitempty"`
	StartedAt           *metav1.Time               `json:"startedAt,omitempty"`
	CompletedAt         *metav1.Time               `json:"completedAt,omitempty"`
	Message             string                     `json:"message,omitempty"`
	NativeJobName       string                     `json:"nativeJobName,omitempty"`
	Series              string                     `json:"series,omitempty"`
	SnapshotRef         string                     `json:"snapshotRef,omitempty"`
	StorageProbeResult  ProbeResult                `json:"storageProbeResult,omitempty"`
	StorageProbeMessage string                     `json:"storageProbeMessage,omitempty"`
	Notification        NotificationDeliveryStatus `json:"notification,omitempty"`
	Conditions          []metav1.Condition         `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Source",type=string,JSONPath=`.spec.sourceRef.name`
// +kubebuilder:printcolumn:name="Storage",type=string,JSONPath=`.spec.storageRef.name`
// +kubebuilder:printcolumn:name="Snapshot",type=string,JSONPath=`.status.snapshotRef`
// +kubebuilder:printcolumn:name="Completed",type=date,JSONPath=`.status.completedAt`
// +kubebuilder:printcolumn:name="Message",type=string,JSONPath=`.status.message`
// +kubebuilder:resource:shortName=bj
type BackupJob struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BackupJobSpec   `json:"spec,omitempty"`
	Status BackupJobStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type BackupJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BackupJob `json:"items"`
}

func init() {
	SchemeBuilder.Register(&BackupJob{}, &BackupJobList{})
}
