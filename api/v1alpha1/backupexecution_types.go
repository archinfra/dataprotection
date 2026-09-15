package v1alpha1

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type BackupExecutionTrigger string

const (
	BackupExecutionTriggerManual    BackupExecutionTrigger = "Manual"
	BackupExecutionTriggerScheduled BackupExecutionTrigger = "Scheduled"
)

type BackupExecutionSpec struct {
	PolicyRef        *corev1.LocalObjectReference  `json:"policyRef,omitempty"`
	SourceRef        corev1.LocalObjectReference   `json:"sourceRef"`
	StorageRef       corev1.LocalObjectReference   `json:"storageRef"`
	RetentionRef     *corev1.LocalObjectReference  `json:"retentionRef,omitempty"`
	NotificationRefs []corev1.LocalObjectReference `json:"notificationRefs,omitempty"`
	JobRuntime       JobRuntimeSpec                `json:"jobRuntime,omitempty"`
	SnapshotName     string                        `json:"snapshotName,omitempty"`
	Reason           string                        `json:"reason,omitempty"`
	// +kubebuilder:default=Manual
	// +kubebuilder:validation:Enum=Manual;Scheduled
	Trigger BackupExecutionTrigger `json:"trigger,omitempty"`
}

type BackupExecutionStatus struct {
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
// +kubebuilder:printcolumn:name="Trigger",type=string,JSONPath=`.spec.trigger`
// +kubebuilder:printcolumn:name="Source",type=string,JSONPath=`.spec.sourceRef.name`
// +kubebuilder:printcolumn:name="Storage",type=string,JSONPath=`.spec.storageRef.name`
// +kubebuilder:printcolumn:name="Snapshot",type=string,JSONPath=`.status.snapshotRef`
// +kubebuilder:printcolumn:name="Completed",type=date,JSONPath=`.status.completedAt`
// +kubebuilder:printcolumn:name="Message",type=string,priority=1,JSONPath=`.status.message`
// +kubebuilder:resource:shortName=backupexec
// BackupExecution is the single source of truth for one concrete backup execution.
type BackupExecution struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BackupExecutionSpec   `json:"spec,omitempty"`
	Status BackupExecutionStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type BackupExecutionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BackupExecution `json:"items"`
}

func (s *BackupExecutionSpec) ValidateBasic() error {
	if strings.TrimSpace(s.SourceRef.Name) == "" {
		return fmt.Errorf("spec.sourceRef.name is required")
	}
	if strings.TrimSpace(s.StorageRef.Name) == "" {
		return fmt.Errorf("spec.storageRef.name is required")
	}
	if s.RetentionRef != nil && strings.TrimSpace(s.RetentionRef.Name) == "" {
		return fmt.Errorf("spec.retentionRef.name cannot be empty")
	}
	if hasDuplicateLocalObjectReferenceNames(s.NotificationRefs) {
		return fmt.Errorf("spec.notificationRefs contains duplicate names")
	}
	switch s.Trigger {
	case "", BackupExecutionTriggerManual, BackupExecutionTriggerScheduled:
	default:
		return fmt.Errorf("spec.trigger must be one of Manual, Scheduled")
	}
	return s.JobRuntime.ValidateBasic()
}

func init() {
	SchemeBuilder.Register(&BackupExecution{}, &BackupExecutionList{})
}
