package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type BackupPolicySpec struct {
	SourceRef      corev1.LocalObjectReference   `json:"sourceRef"`
	RepositoryRefs []corev1.LocalObjectReference `json:"repositoryRefs,omitempty"`
	Schedule       BackupScheduleSpec            `json:"schedule,omitempty"`
	Retention      RetentionPolicy               `json:"retention,omitempty"`
	Verification   VerificationSpec              `json:"verification,omitempty"`
	Execution      ExecutionTemplateSpec         `json:"execution,omitempty"`
	DriverConfig   DriverConfig                  `json:"driverConfig,omitempty"`
	Suspend        bool                          `json:"suspend,omitempty"`
}

type BackupPolicyStatus struct {
	Phase              ResourcePhase      `json:"phase,omitempty"`
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	LastScheduleTime   *metav1.Time       `json:"lastScheduleTime,omitempty"`
	NextScheduleTime   *metav1.Time       `json:"nextScheduleTime,omitempty"`
	CronJobNames       []string           `json:"cronJobNames,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
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
