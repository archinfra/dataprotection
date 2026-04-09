package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type BackupPolicySpec struct {
	SourceRef        corev1.LocalObjectReference   `json:"sourceRef"`
	StorageRefs      []corev1.LocalObjectReference `json:"storageRefs,omitempty"`
	Schedule         BackupScheduleSpec            `json:"schedule,omitempty"`
	RetentionRef     *corev1.LocalObjectReference  `json:"retentionRef,omitempty"`
	NotificationRefs []corev1.LocalObjectReference `json:"notificationRefs,omitempty"`
	JobRuntime       JobRuntimeSpec                `json:"jobRuntime,omitempty"`
	Suspend          bool                          `json:"suspend,omitempty"`
}

type BackupPolicyStatus struct {
	Phase              ResourcePhase      `json:"phase,omitempty"`
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	LastScheduleTime   *metav1.Time       `json:"lastScheduleTime,omitempty"`
	Message            string             `json:"message,omitempty"`
	CronJobNames       []string           `json:"cronJobNames,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Source",type=string,JSONPath=`.spec.sourceRef.name`
// +kubebuilder:printcolumn:name="Schedule",type=string,JSONPath=`.spec.schedule.cron`
// +kubebuilder:printcolumn:name="Suspend",type=boolean,JSONPath=`.spec.suspend`
// +kubebuilder:printcolumn:name="LastSchedule",type=date,JSONPath=`.status.lastScheduleTime`
// +kubebuilder:printcolumn:name="Message",type=string,priority=1,JSONPath=`.status.message`
// +kubebuilder:resource:shortName=bp
type BackupPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BackupPolicySpec   `json:"spec,omitempty"`
	Status BackupPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type BackupPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BackupPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&BackupPolicy{}, &BackupPolicyList{})
}
