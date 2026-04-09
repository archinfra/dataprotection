package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type NotificationEndpointSpec struct {
	URL                   string                  `json:"url"`
	Method                string                  `json:"method,omitempty"`
	Headers               map[string]string       `json:"headers,omitempty"`
	SecretHeaders         []SecretHeaderReference `json:"secretHeaders,omitempty"`
	TimeoutSeconds        int32                   `json:"timeoutSeconds,omitempty"`
	InsecureSkipTLSVerify bool                    `json:"insecureSkipTLSVerify,omitempty"`
}

type NotificationEndpointStatus struct {
	Phase              ResourcePhase      `json:"phase,omitempty"`
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Message            string             `json:"message,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="URL",type=string,JSONPath=`.spec.url`
// +kubebuilder:printcolumn:name="Message",type=string,priority=1,JSONPath=`.status.message`
// +kubebuilder:resource:shortName=ne
type NotificationEndpoint struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NotificationEndpointSpec   `json:"spec,omitempty"`
	Status NotificationEndpointStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type NotificationEndpointList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NotificationEndpoint `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NotificationEndpoint{}, &NotificationEndpointList{})
}
