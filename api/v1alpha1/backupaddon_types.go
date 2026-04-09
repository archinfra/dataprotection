package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type BackupAddonSpec struct {
	DisplayName     string               `json:"displayName,omitempty"`
	Version         string               `json:"version,omitempty"`
	SupportedKinds  []string             `json:"supportedKinds,omitempty"`
	Parameters      []AddonParameterSpec `json:"parameters,omitempty"`
	BackupTemplate  AddonTemplateSpec    `json:"backupTemplate"`
	RestoreTemplate *AddonTemplateSpec   `json:"restoreTemplate,omitempty"`
}

type BackupAddonStatus struct {
	Phase              ResourcePhase      `json:"phase,omitempty"`
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Message            string             `json:"message,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.spec.version`
// +kubebuilder:printcolumn:name="Message",type=string,priority=1,JSONPath=`.status.message`
// +kubebuilder:resource:scope=Cluster,shortName=ba
type BackupAddon struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BackupAddonSpec   `json:"spec,omitempty"`
	Status BackupAddonStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type BackupAddonList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BackupAddon `json:"items"`
}

func init() {
	SchemeBuilder.Register(&BackupAddon{}, &BackupAddonList{})
}
