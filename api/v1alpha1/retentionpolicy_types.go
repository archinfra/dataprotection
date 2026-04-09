package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type RetentionPolicySpec struct {
	SuccessfulSnapshots SuccessfulSnapshotRetentionSpec `json:"successfulSnapshots,omitempty"`
	FailedExecutions    FailedExecutionRetentionSpec    `json:"failedExecutions,omitempty"`
}

type RetentionPolicyStatus struct {
	Phase              ResourcePhase      `json:"phase,omitempty"`
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Message            string             `json:"message,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="KeepSuccess",type=integer,JSONPath=`.spec.successfulSnapshots.keepLast`
// +kubebuilder:printcolumn:name="KeepFailed",type=integer,JSONPath=`.spec.failedExecutions.keepLast`
// +kubebuilder:printcolumn:name="Message",type=string,priority=1,JSONPath=`.status.message`
// +kubebuilder:resource:shortName=rp
type RetentionPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RetentionPolicySpec   `json:"spec,omitempty"`
	Status RetentionPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type RetentionPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RetentionPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RetentionPolicy{}, &RetentionPolicyList{})
}
