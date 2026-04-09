package v1alpha1

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

func (s *BackupAddonSpec) ValidateBasic() error {
	if err := s.BackupTemplate.ValidateBasic("spec.backupTemplate"); err != nil {
		return err
	}
	if s.RestoreTemplate != nil {
		if err := s.RestoreTemplate.ValidateBasic("spec.restoreTemplate"); err != nil {
			return err
		}
	}
	if hasDuplicateAddonParameters(s.Parameters) {
		return fmt.Errorf("spec.parameters contains duplicate names")
	}
	return nil
}

func (s *AddonTemplateSpec) ValidateBasic(field string) error {
	if strings.TrimSpace(s.Image) == "" {
		return fmt.Errorf("%s.image is required", field)
	}
	return nil
}

func (s *BackupSourceSpec) ValidateBasic() error {
	if strings.TrimSpace(s.AddonRef.Name) == "" {
		return fmt.Errorf("spec.addonRef.name is required")
	}
	if s.TargetRef == nil && strings.TrimSpace(s.Endpoint.Host) == "" && s.Endpoint.ServiceRef == nil {
		return fmt.Errorf("spec.targetRef or spec.endpoint.host/serviceRef is required")
	}
	if hasDuplicateSecretParameterRefs(s.SecretRefs) {
		return fmt.Errorf("spec.secretRefs contains duplicate names")
	}
	return nil
}

func (s *BackupStorageSpec) ValidateBasic() error {
	switch s.Type {
	case StorageTypeNFS:
		if s.NFS == nil {
			return fmt.Errorf("spec.nfs is required when spec.type=nfs")
		}
		if strings.TrimSpace(s.NFS.Server) == "" || strings.TrimSpace(s.NFS.Path) == "" {
			return fmt.Errorf("spec.nfs.server and spec.nfs.path are required")
		}
	case StorageTypeMinIO:
		if s.MinIO == nil {
			return fmt.Errorf("spec.minio is required when spec.type=minio")
		}
		if strings.TrimSpace(s.MinIO.Endpoint) == "" || strings.TrimSpace(s.MinIO.Bucket) == "" {
			return fmt.Errorf("spec.minio.endpoint and spec.minio.bucket are required")
		}
	default:
		return fmt.Errorf("unsupported spec.type %q", s.Type)
	}
	return nil
}

func (s *BackupPolicySpec) ValidateBasic() error {
	if strings.TrimSpace(s.SourceRef.Name) == "" {
		return fmt.Errorf("spec.sourceRef.name is required")
	}
	if len(s.StorageRefs) == 0 {
		return fmt.Errorf("spec.storageRefs requires at least one storage")
	}
	if hasDuplicateLocalObjectReferenceNames(s.StorageRefs) {
		return fmt.Errorf("spec.storageRefs contains duplicate names")
	}
	if s.RetentionRef != nil && strings.TrimSpace(s.RetentionRef.Name) == "" {
		return fmt.Errorf("spec.retentionRef.name cannot be empty")
	}
	if hasDuplicateLocalObjectReferenceNames(s.NotificationRefs) {
		return fmt.Errorf("spec.notificationRefs contains duplicate names")
	}
	if strings.TrimSpace(s.Schedule.Cron) == "" && !s.Suspend {
		return fmt.Errorf("spec.schedule.cron is required unless the policy is suspended")
	}
	return s.JobRuntime.ValidateBasic()
}

func (s *BackupScheduleSpec) EffectiveConcurrencyPolicy() batchv1.ConcurrencyPolicy {
	if strings.TrimSpace(string(s.ConcurrencyPolicy)) == "" {
		return batchv1.ForbidConcurrent
	}
	return s.ConcurrencyPolicy
}

func (s *JobRuntimeSpec) ValidateBasic() error {
	if s.TTLSecondsAfterFinished != nil && *s.TTLSecondsAfterFinished < 0 {
		return fmt.Errorf("spec.jobRuntime.ttlSecondsAfterFinished cannot be negative")
	}
	return nil
}

func (s *BackupJobSpec) ValidateBasic() error {
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
	return s.JobRuntime.ValidateBasic()
}

func (s *RestoreJobSpec) ValidateBasic() error {
	if strings.TrimSpace(s.SourceRef.Name) == "" {
		return fmt.Errorf("spec.sourceRef.name is required")
	}
	if strings.TrimSpace(s.SnapshotRef.Name) == "" {
		return fmt.Errorf("spec.snapshotRef.name is required")
	}
	if hasDuplicateLocalObjectReferenceNames(s.NotificationRefs) {
		return fmt.Errorf("spec.notificationRefs contains duplicate names")
	}
	if hasDuplicateSecretParameterRefs(s.SecretRefs) {
		return fmt.Errorf("spec.secretRefs contains duplicate names")
	}
	return s.JobRuntime.ValidateBasic()
}

func (s *RetentionPolicySpec) ValidateBasic() error {
	if s.SuccessfulSnapshots.KeepLast < 0 {
		return fmt.Errorf("spec.successfulSnapshots.keepLast cannot be negative")
	}
	if s.FailedExecutions.KeepLast < 0 {
		return fmt.Errorf("spec.failedExecutions.keepLast cannot be negative")
	}
	return nil
}

func (s *NotificationEndpointSpec) ValidateBasic() error {
	if strings.TrimSpace(s.URL) == "" {
		return fmt.Errorf("spec.url is required")
	}
	if _, err := url.ParseRequestURI(strings.TrimSpace(s.URL)); err != nil {
		return fmt.Errorf("spec.url is invalid: %v", err)
	}
	if hasDuplicateSecretHeaderRefs(s.SecretHeaders) {
		return fmt.Errorf("spec.secretHeaders contains duplicate names")
	}
	if s.TimeoutSeconds < 0 {
		return fmt.Errorf("spec.timeoutSeconds cannot be negative")
	}
	return nil
}

func PredictCronJobNames(policyName string, storageRefs []corev1.LocalObjectReference) []string {
	names := make([]string, 0, len(storageRefs))
	for _, ref := range storageRefs {
		storageName := strings.TrimSpace(ref.Name)
		if storageName == "" {
			continue
		}
		names = append(names, BuildCronJobName(policyName, storageName, "backup"))
	}
	sort.Strings(names)
	return names
}

func hasDuplicateLocalObjectReferenceNames(refs []corev1.LocalObjectReference) bool {
	seen := map[string]struct{}{}
	for _, ref := range refs {
		name := strings.TrimSpace(ref.Name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			return true
		}
		seen[name] = struct{}{}
	}
	return false
}

func hasDuplicateSecretParameterRefs(refs []SecretParameterReference) bool {
	seen := map[string]struct{}{}
	for _, ref := range refs {
		name := strings.TrimSpace(ref.Name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			return true
		}
		seen[name] = struct{}{}
	}
	return false
}

func hasDuplicateSecretHeaderRefs(refs []SecretHeaderReference) bool {
	seen := map[string]struct{}{}
	for _, ref := range refs {
		name := strings.TrimSpace(ref.Name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			return true
		}
		seen[name] = struct{}{}
	}
	return false
}

func hasDuplicateAddonParameters(params []AddonParameterSpec) bool {
	seen := map[string]struct{}{}
	for _, param := range params {
		name := strings.TrimSpace(param.Name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			return true
		}
		seen[name] = struct{}{}
	}
	return false
}
