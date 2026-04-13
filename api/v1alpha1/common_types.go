package v1alpha1

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ResourcePhase string

const (
	ResourcePhasePending    ResourcePhase = "Pending"
	ResourcePhaseConfigured ResourcePhase = "Configured"
	ResourcePhaseRunning    ResourcePhase = "Running"
	ResourcePhaseSucceeded  ResourcePhase = "Succeeded"
	ResourcePhaseFailed     ResourcePhase = "Failed"
	ResourcePhasePaused     ResourcePhase = "Paused"
)

type ProbeResult string

const (
	ProbeResultUnknown   ProbeResult = "Unknown"
	ProbeResultSucceeded ProbeResult = "Succeeded"
	ProbeResultFailed    ProbeResult = "Failed"
)

type NotificationDeliveryPhase string

const (
	NotificationDeliveryPending   NotificationDeliveryPhase = "Pending"
	NotificationDeliverySucceeded NotificationDeliveryPhase = "Succeeded"
	NotificationDeliveryFailed    NotificationDeliveryPhase = "Failed"
)

type StorageType string

const (
	StorageTypeNFS   StorageType = "nfs"
	StorageTypeMinIO StorageType = "minio"
)

type SecretKeyReference struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

type SecretParameterReference struct {
	Name         string             `json:"name"`
	SecretKeyRef SecretKeyReference `json:"secretKeyRef"`
}

type SecretHeaderReference struct {
	Name         string             `json:"name"`
	SecretKeyRef SecretKeyReference `json:"secretKeyRef"`
}

type ServiceReference struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Port      int32  `json:"port,omitempty"`
}

type NamespacedObjectReference struct {
	APIVersion string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name"`
}

type EndpointSpec struct {
	Host         string              `json:"host,omitempty"`
	Port         int32               `json:"port,omitempty"`
	Scheme       string              `json:"scheme,omitempty"`
	ServiceRef   *ServiceReference   `json:"serviceRef,omitempty"`
	Username     string              `json:"username,omitempty"`
	UsernameFrom *SecretKeyReference `json:"usernameFrom,omitempty"`
	PasswordFrom *SecretKeyReference `json:"passwordFrom,omitempty"`
}

type BackupScheduleSpec struct {
	Cron                    string                    `json:"cron,omitempty"`
	Suspend                 bool                      `json:"suspend,omitempty"`
	StartingDeadlineSeconds *int64                    `json:"startingDeadlineSeconds,omitempty"`
	ConcurrencyPolicy       batchv1.ConcurrencyPolicy `json:"concurrencyPolicy,omitempty"`
}

type JobRuntimeSpec struct {
	ServiceAccountName      string                      `json:"serviceAccountName,omitempty"`
	ImagePullPolicy         corev1.PullPolicy           `json:"imagePullPolicy,omitempty"`
	NodeSelector            map[string]string           `json:"nodeSelector,omitempty"`
	Tolerations             []corev1.Toleration         `json:"tolerations,omitempty"`
	Resources               corev1.ResourceRequirements `json:"resources,omitempty"`
	ActiveDeadlineSeconds   *int64                      `json:"activeDeadlineSeconds,omitempty"`
	TTLSecondsAfterFinished *int32                      `json:"ttlSecondsAfterFinished,omitempty"`
	PriorityClassName       string                      `json:"priorityClassName,omitempty"`
}

type AddonParameterSpec struct {
	Name         string `json:"name"`
	Required     bool   `json:"required,omitempty"`
	Secret       bool   `json:"secret,omitempty"`
	Description  string `json:"description,omitempty"`
	DefaultValue string `json:"defaultValue,omitempty"`
}

type AddonTemplateSpec struct {
	Image      string          `json:"image,omitempty"`
	Command    []string        `json:"command,omitempty"`
	Args       []string        `json:"args,omitempty"`
	WorkingDir string          `json:"workingDir,omitempty"`
	Env        []corev1.EnvVar `json:"env,omitempty"`
}

type MinIOStorageSpec struct {
	Endpoint         string              `json:"endpoint"`
	Bucket           string              `json:"bucket"`
	Prefix           string              `json:"prefix,omitempty"`
	Region           string              `json:"region,omitempty"`
	Insecure         bool                `json:"insecure,omitempty"`
	AutoCreateBucket bool                `json:"autoCreateBucket,omitempty"`
	AccessKeyFrom    *SecretKeyReference `json:"accessKeyFrom,omitempty"`
	SecretKeyFrom    *SecretKeyReference `json:"secretKeyFrom,omitempty"`
}

type NFSStorageSpec struct {
	Server string `json:"server"`
	Path   string `json:"path"`
}

type NotificationDeliveryStatus struct {
	Phase             NotificationDeliveryPhase `json:"phase,omitempty"`
	Attempts          int32                     `json:"attempts,omitempty"`
	LastAttemptTime   *metav1.Time              `json:"lastAttemptTime,omitempty"`
	LastDeliveredTime *metav1.Time              `json:"lastDeliveredTime,omitempty"`
	Message           string                    `json:"message,omitempty"`
}

type SuccessfulSnapshotRetentionSpec struct {
	KeepLast int32 `json:"keepLast,omitempty"`
}

type FailedExecutionRetentionSpec struct {
	KeepLast int32 `json:"keepLast,omitempty"`
}
