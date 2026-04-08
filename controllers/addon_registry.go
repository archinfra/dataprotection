package controllers

import (
	batchv1 "k8s.io/api/batch/v1"

	dpv1alpha1 "github.com/archinfra/dataprotection/api/v1alpha1"
)

// builtInAddon keeps the core reconciler focused on scheduling and status
// while driver-specific data movement lives in narrowly scoped modules.
type builtInAddon interface {
	Name() string
	Supports(driver dpv1alpha1.BackupDriver, execution dpv1alpha1.ExecutionTemplateSpec) bool
	BuildBackupJob(request addonBackupJobRequest) (*batchv1.Job, error)
	BuildRestoreJob(request addonRestoreJobRequest) (*batchv1.Job, error)
}

type addonBackupJobRequest struct {
	Run          *dpv1alpha1.BackupRun
	Policy       *dpv1alpha1.BackupPolicy
	Source       *dpv1alpha1.BackupSource
	Storage      *dpv1alpha1.BackupStorage
	StoragePath  string
	Snapshot     string
	KeepLast     int32
	Execution    dpv1alpha1.ExecutionTemplateSpec
	DriverConfig dpv1alpha1.DriverConfig
}

type addonRestoreJobRequest struct {
	Restore      *dpv1alpha1.RestoreRequest
	BackupRun    *dpv1alpha1.BackupRun
	Source       *dpv1alpha1.BackupSource
	Storage      *dpv1alpha1.BackupStorage
	StoragePath  string
	Snapshot     string
	Execution    dpv1alpha1.ExecutionTemplateSpec
	DriverConfig dpv1alpha1.DriverConfig
}

func resolveBuiltInAddon(driver dpv1alpha1.BackupDriver, execution dpv1alpha1.ExecutionTemplateSpec) builtInAddon {
	for _, addon := range []builtInAddon{
		mysqlBuiltInAddon{},
		redisBuiltInAddon{},
		minioBuiltInAddon{},
	} {
		if addon.Supports(driver, execution) {
			return addon
		}
	}
	return nil
}
