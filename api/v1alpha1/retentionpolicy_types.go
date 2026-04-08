package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type SnapshotRetentionRule struct {
	Last int32 `json:"last,omitempty"`
}

type RetentionPolicySpec struct {
	Default             bool                  `json:"default,omitempty"`
	MaxRetentionPeriod  string                `json:"maxRetentionPeriod,omitempty"`
	SuccessfulSnapshots SnapshotRetentionRule `json:"successfulSnapshots,omitempty"`
	FailedSnapshots     SnapshotRetentionRule `json:"failedSnapshots,omitempty"`
}

type RetentionPolicyStatus struct {
	Phase              ResourcePhase      `json:"phase,omitempty"`
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
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
