package v1alpha1

import (
	"path"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:validation:Enum=auto;archive;filesystem
type RestoreArtifactFormat string

const (
	RestoreArtifactFormatAuto       RestoreArtifactFormat = "auto"
	RestoreArtifactFormatArchive    RestoreArtifactFormat = "archive"
	RestoreArtifactFormatFilesystem RestoreArtifactFormat = "filesystem"
)

type RestoreImportSource struct {
	StorageRef corev1.LocalObjectReference `json:"storageRef"`
	Path       string                      `json:"path"`
	Format     RestoreArtifactFormat       `json:"format,omitempty"`
	Series     string                      `json:"series,omitempty"`
	Snapshot   string                      `json:"snapshot,omitempty"`
}

type RestoreJobSpec struct {
	SourceRef        corev1.LocalObjectReference   `json:"sourceRef"`
	SnapshotRef      corev1.LocalObjectReference   `json:"snapshotRef,omitempty"`
	ImportSource     *RestoreImportSource          `json:"importSource,omitempty"`
	TargetRef        *NamespacedObjectReference    `json:"targetRef,omitempty"`
	Endpoint         *EndpointSpec                 `json:"endpoint,omitempty"`
	Parameters       map[string]string             `json:"parameters,omitempty"`
	SecretRefs       []SecretParameterReference    `json:"secretRefs,omitempty"`
	NotificationRefs []corev1.LocalObjectReference `json:"notificationRefs,omitempty"`
	JobRuntime       JobRuntimeSpec                `json:"jobRuntime,omitempty"`
	Reason           string                        `json:"reason,omitempty"`
}

func (s *RestoreImportSource) NormalizedPath() string {
	if s == nil {
		return ""
	}
	value := strings.TrimSpace(strings.ReplaceAll(s.Path, "\\", "/"))
	if value == "" {
		return ""
	}
	cleaned := path.Clean(value)
	if cleaned == "." {
		return ""
	}
	return strings.TrimPrefix(cleaned, "./")
}

func (s *RestoreImportSource) EffectiveFormat() RestoreArtifactFormat {
	if s == nil {
		return RestoreArtifactFormatAuto
	}
	switch RestoreArtifactFormat(strings.ToLower(strings.TrimSpace(string(s.Format)))) {
	case "", RestoreArtifactFormatAuto:
		return RestoreArtifactFormatAuto
	case RestoreArtifactFormatArchive:
		return RestoreArtifactFormatArchive
	case RestoreArtifactFormatFilesystem:
		return RestoreArtifactFormatFilesystem
	default:
		return RestoreArtifactFormat(strings.ToLower(strings.TrimSpace(string(s.Format))))
	}
}

func (s *RestoreImportSource) EffectiveSnapshotName() string {
	if s == nil {
		return ""
	}
	if name := strings.TrimSpace(s.Snapshot); name != "" {
		return name
	}
	base := path.Base(s.NormalizedPath())
	switch {
	case strings.HasSuffix(base, ".tar.gz"):
		base = strings.TrimSuffix(base, ".tar.gz")
	case strings.HasSuffix(base, ".tgz"):
		base = strings.TrimSuffix(base, ".tgz")
	case strings.HasSuffix(base, ".tar"):
		base = strings.TrimSuffix(base, ".tar")
	}
	if base == "." {
		return ""
	}
	return base
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
// +kubebuilder:printcolumn:name="ImportPath",type=string,priority=1,JSONPath=`.spec.importSource.path`
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
