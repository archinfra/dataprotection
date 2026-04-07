package v1alpha1

import (
	"fmt"
	"sort"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

func (s *BackupSourceSpec) ValidateBasic() error {
	if strings.TrimSpace(string(s.Driver)) == "" {
		return fmt.Errorf("spec.driver is required")
	}
	switch s.Driver {
	case BackupDriverMySQL, BackupDriverRedis, BackupDriverMongoDB, BackupDriverMinIO, BackupDriverRabbitMQ, BackupDriverMilvus:
	default:
		return fmt.Errorf("unsupported spec.driver %q", s.Driver)
	}

	if s.TargetRef == nil && strings.TrimSpace(s.Endpoint.Host) == "" && s.Endpoint.ServiceRef == nil {
		return fmt.Errorf("spec.targetRef or spec.endpoint.host/serviceRef is required")
	}
	if err := validateMySQLDriverConfig(s.DriverConfig.MySQL); err != nil {
		return err
	}

	return nil
}

func (s *BackupStorageSpec) ValidateBasic() error {
	if strings.TrimSpace(string(s.Type)) == "" {
		return fmt.Errorf("spec.type is required")
	}

	switch s.Type {
	case StorageTypeNFS:
		if s.NFS == nil {
			return fmt.Errorf("spec.nfs is required when spec.type=nfs")
		}
		if strings.TrimSpace(s.NFS.Server) == "" || strings.TrimSpace(s.NFS.Path) == "" {
			return fmt.Errorf("spec.nfs.server and spec.nfs.path are required")
		}
	case StorageTypeS3:
		if s.S3 == nil {
			return fmt.Errorf("spec.s3 is required when spec.type=s3")
		}
		if strings.TrimSpace(s.S3.Endpoint) == "" || strings.TrimSpace(s.S3.Bucket) == "" {
			return fmt.Errorf("spec.s3.endpoint and spec.s3.bucket are required")
		}
	default:
		return fmt.Errorf("unsupported storage type %q", s.Type)
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
		return fmt.Errorf("spec.storageRefs contains duplicate storage names")
	}
	if s.RetentionPolicyRef != nil && strings.TrimSpace(s.RetentionPolicyRef.Name) == "" {
		return fmt.Errorf("spec.retentionPolicyRef.name cannot be empty")
	}

	if strings.TrimSpace(s.Schedule.Cron) == "" && !s.Suspend {
		return fmt.Errorf("spec.schedule.cron is required unless the policy is suspended")
	}
	if err := validateMySQLDriverConfig(s.DriverConfig.MySQL); err != nil {
		return err
	}

	return nil
}

func (s *RetentionPolicySpec) ValidateBasic() error {
	if s.SuccessfulSnapshots.Last < 0 {
		return fmt.Errorf("spec.successfulSnapshots.last cannot be negative")
	}
	if s.FailedSnapshots.Last < 0 {
		return fmt.Errorf("spec.failedSnapshots.last cannot be negative")
	}
	return nil
}

func (s *BackupRunSpec) ValidateBasic() error {
	if strings.TrimSpace(s.SourceRef.Name) == "" {
		return fmt.Errorf("spec.sourceRef.name is required")
	}
	if len(s.StorageRefs) == 0 && s.PolicyRef == nil {
		return fmt.Errorf("spec.storageRefs or spec.policyRef is required")
	}
	if hasDuplicateLocalObjectReferenceNames(s.StorageRefs) {
		return fmt.Errorf("spec.storageRefs contains duplicate storage names")
	}
	if err := validateMySQLDriverConfig(s.DriverConfig.MySQL); err != nil {
		return err
	}
	return nil
}

func (s *RestoreRequestSpec) ValidateBasic() error {
	if strings.TrimSpace(s.SourceRef.Name) == "" {
		return fmt.Errorf("spec.sourceRef.name is required")
	}
	if s.BackupRunRef == nil && s.SnapshotRef == nil && strings.TrimSpace(s.Snapshot) == "" {
		return fmt.Errorf("spec.backupRunRef, spec.snapshotRef or spec.snapshot is required")
	}
	if s.StorageRef != nil && strings.TrimSpace(s.StorageRef.Name) == "" {
		return fmt.Errorf("spec.storageRef.name cannot be empty")
	}
	if s.SnapshotRef != nil && strings.TrimSpace(s.SnapshotRef.Name) == "" {
		return fmt.Errorf("spec.snapshotRef.name cannot be empty")
	}
	if strings.TrimSpace(string(s.Target.Mode)) != "" {
		switch s.Target.Mode {
		case RestoreTargetModeInPlace, RestoreTargetModeOutOfPlace:
		default:
			return fmt.Errorf("unsupported spec.target.mode %q", s.Target.Mode)
		}
	}
	if err := validateMySQLDriverConfig(s.Target.DriverConfig.MySQL); err != nil {
		return err
	}
	return nil
}

func (s *BackupPolicySpec) EffectiveConcurrencyPolicy() batchv1.ConcurrencyPolicy {
	if strings.TrimSpace(string(s.Schedule.ConcurrencyPolicy)) == "" {
		return batchv1.ForbidConcurrent
	}
	return s.Schedule.ConcurrencyPolicy
}

func PredictCronJobNames(policyName string, storageRefs []corev1.LocalObjectReference) []string {
	names := make([]string, 0, len(storageRefs))
	for _, ref := range storageRefs {
		storageName := strings.TrimSpace(ref.Name)
		if storageName == "" {
			continue
		}
		names = append(names, BuildCronJobName(policyName, storageName))
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

func validateMySQLDriverConfig(config *MySQLDriverConfig) error {
	if config == nil {
		return nil
	}
	if len(config.Databases) > 0 && len(config.Tables) > 0 {
		return fmt.Errorf("mysql driver config cannot set both databases and tables")
	}
	for _, table := range config.Tables {
		table = strings.TrimSpace(table)
		if table == "" {
			continue
		}
		parts := strings.Split(table, ".")
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return fmt.Errorf("mysql driver config table selector must be database.table, got %q", table)
		}
	}
	if restoreMode := strings.TrimSpace(config.RestoreMode); restoreMode != "" {
		switch restoreMode {
		case "merge", "wipe-all-user-databases":
		default:
			return fmt.Errorf("unsupported mysql restoreMode %q", config.RestoreMode)
		}
	}
	return nil
}
