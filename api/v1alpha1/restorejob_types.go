package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type RestoreJobSpec struct {
	SourceRef        corev1.LocalObjectReference   `json:"sourceRef"`
	SnapshotRef      corev1.LocalObjectReference   `json:"snapshotRef"`
	TargetRef        *NamespacedObjectReference    `json:"targetRef,omitempty"`
	Endpoint         *EndpointSpec                 `json:"endpoint,omitempty"`
	Parameters       map[string]string             `json:"parameters,omitempty"`
	SecretRefs       []SecretParameterReference    `json:"secretRefs,omitempty"`
	NotificationRefs []corev1.LocalObjectReference `json:"notificationRefs,omitempty"`
	JobRuntime       JobRuntimeSpec                `json:"jobRuntime,omitempty"`
	Reason           string                        `json:"reason,omitempty"`
}

type RestoreJobStatus struct {
	Phase               ResourcePhase              `json:"phase,omitempty"`
	ObservedGeneration  int64                      `json:"observedGeneration,omitempty"`
	StartedAt           *metav1.Time               `json:"startedAt,omitempty"`
	CompletedAt         *metav1.Time               `json:"completedAt,omitempty"`
	Message             string                     `json:"message,omitempty"`
	NativeJobName       string                     `json:"nativeJobName,omitempty"`
	StorageProbeResult  ProbeResult                `json:"storageProbeResult,omitempty"`
	StorageProbeMessage string                     `json:"storageProbeMessage,omitempty"`
	Notification        NotificationDeliveryStatus `json:"notification,omitempty"`
	Conditions          []metav1.Condition         `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Source",type=string,JSONPath=`.spec.sourceRef.name`
// +kubebuilder:printcolumn:name="SnapshotRef",type=string,JSONPath=`.spec.snapshotRef.name`
// +kubebuilder:printcolumn:name="Completed",type=date,JSONPath=`.status.completedAt`
// +kubebuilder:printcolumn:name="Message",type=string,JSONPath=`.status.message`
// +kubebuilder:resource:shortName=rj
type RestoreJob struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RestoreJobSpec   `json:"spec,omitempty"`
	Status RestoreJobStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type RestoreJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RestoreJob `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RestoreJob{}, &RestoreJobList{})
}
