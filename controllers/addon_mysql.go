package controllers

import (
	batchv1 "k8s.io/api/batch/v1"

	dpv1alpha1 "github.com/archinfra/dataprotection/api/v1alpha1"
)

// mysqlBuiltInAddon keeps MySQL as a statically linked addon so new drivers
// can follow the same extension boundary without bloating reconcile code.
type mysqlBuiltInAddon struct{}

func (mysqlBuiltInAddon) Name() string {
	return "mysql"
}

func (mysqlBuiltInAddon) Supports(driver dpv1alpha1.BackupDriver, execution dpv1alpha1.ExecutionTemplateSpec) bool {
	return useBuiltInMySQLRuntime(driver, execution)
}

func (mysqlBuiltInAddon) BuildBackupJob(request addonBackupJobRequest) (*batchv1.Job, error) {
	return buildBuiltInMySQLBackupRunJob(
		request.Run,
		request.Policy,
		request.Source,
		request.Storage,
		request.StoragePath,
		request.Snapshot,
		request.KeepLast,
	)
}

func (mysqlBuiltInAddon) BuildRestoreJob(request addonRestoreJobRequest) (*batchv1.Job, error) {
	return buildBuiltInMySQLRestoreJob(
		request.Restore,
		request.BackupRun,
		request.Source,
		request.Storage,
		request.StoragePath,
		request.Execution,
		request.Snapshot,
	)
}
