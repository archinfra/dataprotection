package controllers

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dpv1alpha1 "github.com/archinfra/dataprotection/api/v1alpha1"
)

const (
	managedByLabel        = "dataprotection.archinfra.io/managed-by"
	resourceKindLabel     = "dataprotection.archinfra.io/resource-kind"
	resourceNameLabel     = "dataprotection.archinfra.io/resource-name"
	sourceNameLabel       = "dataprotection.archinfra.io/source-name"
	storageNameLabel      = "dataprotection.archinfra.io/storage-name"
	operationLabel        = "dataprotection.archinfra.io/operation"
	snapshotAnnotation    = "dataprotection.archinfra.io/snapshot"
	reasonAnnotation      = "dataprotection.archinfra.io/reason"
	targetModeAnnotation  = "dataprotection.archinfra.io/target-mode"
	storagePathAnnotation = "dataprotection.archinfra.io/storage-path"
	placeholderAnnotation = "dataprotection.archinfra.io/placeholder-runner"
	managedByValue        = "data-protection-operator"
)

// resolvedBackupRunRefs keeps the controller logic readable: resolve all
// cross-resource references once, then pass a small, typed bundle downward.
type resolvedBackupRunRefs struct {
	Policy      *dpv1alpha1.BackupPolicy
	Source      *dpv1alpha1.BackupSource
	Storages    []dpv1alpha1.BackupStorage
	StoragePath string
}

type permanentDependencyError struct {
	err error
}

func (e *permanentDependencyError) Error() string {
	return e.err.Error()
}

func newPermanentDependencyError(format string, args ...interface{}) error {
	return &permanentDependencyError{err: fmt.Errorf(format, args...)}
}

func isPermanentDependencyError(err error) bool {
	var target *permanentDependencyError
	return errors.As(err, &target)
}

func getBackupSource(ctx context.Context, c client.Client, namespace, name string) (*dpv1alpha1.BackupSource, error) {
	var source dpv1alpha1.BackupSource
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &source); err != nil {
		return nil, err
	}
	return &source, nil
}

func getBackupStorage(ctx context.Context, c client.Client, namespace, name string) (*dpv1alpha1.BackupStorage, error) {
	var storage dpv1alpha1.BackupStorage
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &storage); err != nil {
		return nil, err
	}
	return &storage, nil
}

func getBackupPolicy(ctx context.Context, c client.Client, namespace, name string) (*dpv1alpha1.BackupPolicy, error) {
	var policy dpv1alpha1.BackupPolicy
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &policy); err != nil {
		return nil, err
	}
	return &policy, nil
}

func getBackupRun(ctx context.Context, c client.Client, namespace, name string) (*dpv1alpha1.BackupRun, error) {
	var run dpv1alpha1.BackupRun
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &run); err != nil {
		return nil, err
	}
	return &run, nil
}

func getSnapshot(ctx context.Context, c client.Client, namespace, name string) (*dpv1alpha1.Snapshot, error) {
	var snapshot dpv1alpha1.Snapshot
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func getRetentionPolicy(ctx context.Context, c client.Client, namespace, name string) (*dpv1alpha1.RetentionPolicy, error) {
	var retentionPolicy dpv1alpha1.RetentionPolicy
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &retentionPolicy); err != nil {
		return nil, err
	}
	return &retentionPolicy, nil
}

func resolveStorages(ctx context.Context, c client.Client, namespace string, refs []corev1.LocalObjectReference) ([]dpv1alpha1.BackupStorage, error) {
	storages := make([]dpv1alpha1.BackupStorage, 0, len(refs))
	for _, ref := range refs {
		storage, err := getBackupStorage(ctx, c, namespace, ref.Name)
		if err != nil {
			return nil, err
		}
		if err := storage.Spec.ValidateBasic(); err != nil {
			return nil, newPermanentDependencyError("referenced BackupStorage %q is invalid: %v", storage.Name, err)
		}
		storages = append(storages, *storage)
	}
	return storages, nil
}

func resolveBackupRunRefs(ctx context.Context, c client.Client, run *dpv1alpha1.BackupRun) (*resolvedBackupRunRefs, error) {
	result := &resolvedBackupRunRefs{}

	if run.Spec.PolicyRef != nil && strings.TrimSpace(run.Spec.PolicyRef.Name) != "" {
		policy, err := getBackupPolicy(ctx, c, run.Namespace, run.Spec.PolicyRef.Name)
		if err != nil {
			return nil, err
		}
		if policy.Spec.SourceRef.Name != run.Spec.SourceRef.Name {
			return nil, newPermanentDependencyError("spec.sourceRef.name %q does not match policy %q sourceRef %q", run.Spec.SourceRef.Name, policy.Name, policy.Spec.SourceRef.Name)
		}
		result.Policy = policy
	}

	source, err := getBackupSource(ctx, c, run.Namespace, run.Spec.SourceRef.Name)
	if err != nil {
		return nil, err
	}
	result.Source = source

	storageRefs := run.Spec.StorageRefs
	if len(storageRefs) == 0 && result.Policy != nil {
		storageRefs = result.Policy.Spec.StorageRefs
	}
	storages, err := resolveStorages(ctx, c, run.Namespace, storageRefs)
	if err != nil {
		return nil, err
	}
	result.Storages = storages
	result.StoragePath = backupArtifactPath(source, result.Policy, run)
	return result, nil
}

func resolveRestoreStorage(
	ctx context.Context,
	c client.Client,
	restore *dpv1alpha1.RestoreRequest,
	source *dpv1alpha1.BackupSource,
) (*dpv1alpha1.BackupRun, *dpv1alpha1.Snapshot, *dpv1alpha1.BackupStorage, string, error) {
	var backupRun *dpv1alpha1.BackupRun
	var snapshot *dpv1alpha1.Snapshot
	if restore.Spec.BackupRunRef != nil && strings.TrimSpace(restore.Spec.BackupRunRef.Name) != "" {
		resolvedRun, err := getBackupRun(ctx, c, restore.Namespace, restore.Spec.BackupRunRef.Name)
		if err != nil {
			return nil, nil, nil, "", err
		}
		backupRun = resolvedRun
	}
	if restore.Spec.SnapshotRef != nil && strings.TrimSpace(restore.Spec.SnapshotRef.Name) != "" {
		resolvedSnapshot, err := getSnapshot(ctx, c, restore.Namespace, restore.Spec.SnapshotRef.Name)
		if err != nil {
			return nil, nil, nil, "", err
		}
		snapshot = resolvedSnapshot
		if backupRun == nil && strings.TrimSpace(snapshot.Spec.BackupRunRef.Name) != "" {
			resolvedRun, err := getBackupRun(ctx, c, restore.Namespace, snapshot.Spec.BackupRunRef.Name)
			if err != nil {
				return nil, nil, nil, "", err
			}
			backupRun = resolvedRun
		}
	}

	storageName := ""
	if restore.Spec.StorageRef != nil {
		storageName = strings.TrimSpace(restore.Spec.StorageRef.Name)
	}
	if storageName == "" && snapshot != nil {
		storageName = strings.TrimSpace(snapshot.Spec.StorageRef.Name)
	}
	if storageName == "" && backupRun != nil {
		candidateNames := make([]string, 0, len(backupRun.Status.Storages))
		for _, storageStatus := range backupRun.Status.Storages {
			if strings.TrimSpace(storageStatus.Name) != "" {
				candidateNames = append(candidateNames, storageStatus.Name)
			}
		}
		if len(candidateNames) == 0 {
			for _, ref := range backupRun.Spec.StorageRefs {
				if strings.TrimSpace(ref.Name) != "" {
					candidateNames = append(candidateNames, ref.Name)
				}
			}
		}
		candidateNames = uniqueStrings(candidateNames)
		switch len(candidateNames) {
		case 0:
			return backupRun, snapshot, nil, "", newPermanentDependencyError("unable to determine storage from backupRun %q", backupRun.Name)
		case 1:
			storageName = candidateNames[0]
		default:
			return backupRun, snapshot, nil, "", newPermanentDependencyError("spec.storageRef is required because backupRun %q spans multiple storages: %s", backupRun.Name, strings.Join(candidateNames, ", "))
		}
	}
	if storageName == "" {
		return backupRun, snapshot, nil, "", newPermanentDependencyError("spec.storageRef is required")
	}

	storage, err := getBackupStorage(ctx, c, restore.Namespace, storageName)
	if err != nil {
		return backupRun, snapshot, nil, "", err
	}
	if err := storage.Spec.ValidateBasic(); err != nil {
		return backupRun, snapshot, nil, "", newPermanentDependencyError("referenced BackupStorage %q is invalid: %v", storage.Name, err)
	}

	storagePath := trimString(restore.Spec.StoragePath)
	if storagePath == "" && snapshot != nil {
		storagePath = trimString(snapshot.Spec.StoragePath)
	}
	if storagePath == "" && backupRun != nil {
		for _, storageStatus := range backupRun.Status.Storages {
			if storageStatus.Name == storage.Name && trimString(storageStatus.StoragePath) != "" {
				storagePath = trimString(storageStatus.StoragePath)
				break
			}
		}
	}
	if storagePath == "" && backupRun != nil {
		var policy *dpv1alpha1.BackupPolicy
		if backupRun.Spec.PolicyRef != nil && strings.TrimSpace(backupRun.Spec.PolicyRef.Name) != "" {
			policy, err = getBackupPolicy(ctx, c, backupRun.Namespace, backupRun.Spec.PolicyRef.Name)
			if err != nil {
				return backupRun, snapshot, nil, "", err
			}
		}
		storagePath = backupArtifactPath(source, policy, backupRun)
	}
	if storagePath == "" {
		return backupRun, snapshot, nil, "", newPermanentDependencyError("spec.storagePath is required when restoring from a raw snapshot without snapshotRef or backupRunRef")
	}

	return backupRun, snapshot, storage, storagePath, nil
}

// backupArtifactPath defines the stable layout for backup artifacts inside a
// storage backend. Policies get a long-lived series path while standalone runs
// write into their own run-scoped path.
func backupArtifactPath(source *dpv1alpha1.BackupSource, policy *dpv1alpha1.BackupPolicy, run *dpv1alpha1.BackupRun) string {
	parts := []string{
		"backups",
		dpv1alpha1.BuildLabelValue(string(source.Spec.Driver)),
		dpv1alpha1.BuildLabelValue(source.Namespace),
		dpv1alpha1.BuildLabelValue(source.Name),
	}
	if policy != nil {
		parts = append(parts, "policies", dpv1alpha1.BuildLabelValue(policy.Name))
	} else {
		parts = append(parts, "runs", dpv1alpha1.BuildLabelValue(run.Name))
	}
	return strings.Join(parts, "/")
}

func defaultExecutionTemplate(spec dpv1alpha1.ExecutionTemplateSpec) dpv1alpha1.ExecutionTemplateSpec {
	spec.RunnerImage = strings.TrimSpace(spec.RunnerImage)
	if spec.RunnerImage == "" {
		spec.RunnerImage = defaultPlaceholderRunnerImage()
	}
	if spec.ImagePullPolicy == "" {
		spec.ImagePullPolicy = defaultImagePullPolicy(spec.RunnerImage)
	}
	if spec.BackoffLimit == nil {
		spec.BackoffLimit = int32Ptr(1)
	}
	if spec.TTLSecondsAfterFinished == nil {
		spec.TTLSecondsAfterFinished = defaultJobTTLSeconds()
	}
	return spec
}

// buildBackupCronJob renders a native Kubernetes CronJob that only creates a
// BackupRun CR. The actual data movement remains in the BackupRun controller.
func buildBackupCronJob(
	policy *dpv1alpha1.BackupPolicy,
	source *dpv1alpha1.BackupSource,
	storage *dpv1alpha1.BackupStorage,
	triggerServiceAccount string,
) (*batchv1.CronJob, error) {
	execution := defaultExecutionTemplate(policy.Spec.Execution)
	triggerImagePullPolicy := execution.ImagePullPolicy
	if policy.Spec.Execution.ImagePullPolicy == "" {
		triggerImagePullPolicy = defaultImagePullPolicy(defaultControllerImage())
	}
	name := dpv1alpha1.BuildCronJobName(policy.Name, storage.Name)
	suspended := policy.Spec.Suspend || policy.Spec.Schedule.Suspend
	labels := managedResourceLabels("BackupPolicy", policy.Name, "scheduled-trigger", source.Name, storage.Name)
	annotations := map[string]string{
		"dataprotection.archinfra.io/schedule-model": "cronjob-backuprun",
	}

	return &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   policy.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: batchv1.CronJobSpec{
			Schedule:                   policy.Spec.Schedule.Cron,
			Suspend:                    boolPtr(suspended),
			ConcurrencyPolicy:          policy.Spec.EffectiveConcurrencyPolicy(),
			StartingDeadlineSeconds:    cloneInt64Ptr(policy.Spec.Schedule.StartingDeadlineSeconds),
			SuccessfulJobsHistoryLimit: defaultCronJobSuccessfulHistoryLimit(),
			FailedJobsHistoryLimit:     defaultCronJobFailedHistoryLimit(),
			JobTemplate: batchv1.JobTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      copyStringMap(labels),
					Annotations: copyStringMap(annotations),
				},
				Spec: batchv1.JobSpec{
					BackoffLimit: int32Ptr(1),
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels:      copyStringMap(labels),
							Annotations: copyStringMap(annotations),
						},
						Spec: corev1.PodSpec{
							RestartPolicy:      corev1.RestartPolicyNever,
							ServiceAccountName: triggerServiceAccount,
							Containers: []corev1.Container{
								{
									Name:            "backup-trigger",
									Image:           defaultControllerImage(),
									ImagePullPolicy: triggerImagePullPolicy,
									Args: []string{
										"trigger-backup-run",
										"--namespace=" + policy.Namespace,
										"--policy=" + policy.Name,
										"--storage=" + storage.Name,
									},
								},
							},
						},
					},
				},
			},
		},
	}, nil
}

func buildBackupRunJob(
	run *dpv1alpha1.BackupRun,
	policy *dpv1alpha1.BackupPolicy,
	source *dpv1alpha1.BackupSource,
	storage *dpv1alpha1.BackupStorage,
	storagePath string,
	snapshot string,
	keepLast int32,
) (*batchv1.Job, error) {
	execution := dpv1alpha1.ExecutionTemplateSpec{}
	driverConfig := run.Spec.DriverConfig
	if policy != nil {
		execution = policy.Spec.Execution
		driverConfig = effectiveDriverConfig(policy.Spec.DriverConfig, run.Spec.DriverConfig)
	}
	if addon := resolveBuiltInAddon(source.Spec.Driver, execution); addon != nil {
		return addon.BuildBackupJob(addonBackupJobRequest{
			Run:          run,
			Policy:       policy,
			Source:       source,
			Storage:      storage,
			StoragePath:  storagePath,
			Snapshot:     snapshot,
			KeepLast:     keepLast,
			Execution:    execution,
			DriverConfig: driverConfig,
		})
	}
	execution = defaultExecutionTemplate(execution)
	name := dpv1alpha1.BuildJobName(run.Name, storage.Name)
	labels := managedResourceLabels("BackupRun", run.Name, "manual-backup", source.Name, storage.Name)
	annotations := map[string]string{
		snapshotAnnotation:    snapshot,
		storagePathAnnotation: storagePath,
		placeholderAnnotation: boolString(len(execution.Command) == 0 && len(execution.Args) == 0),
	}
	if reason := strings.TrimSpace(run.Spec.Reason); reason != "" {
		annotations[reasonAnnotation] = reason
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   run.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: buildJobSpec(execution, labels, buildBackupEnvVars("", run.Name, source, storage, storagePath, driverConfig, execution.ExtraEnv, snapshot, strings.TrimSpace(run.Spec.Reason)), "manual-backup"),
	}, nil
}

func buildRestoreJob(
	restore *dpv1alpha1.RestoreRequest,
	backupRun *dpv1alpha1.BackupRun,
	source *dpv1alpha1.BackupSource,
	storage *dpv1alpha1.BackupStorage,
	storagePath string,
	execution dpv1alpha1.ExecutionTemplateSpec,
	snapshot string,
) (*batchv1.Job, error) {
	driverConfig := effectiveDriverConfig(source.Spec.DriverConfig, restore.Spec.Target.DriverConfig)
	if addon := resolveBuiltInAddon(source.Spec.Driver, execution); addon != nil {
		return addon.BuildRestoreJob(addonRestoreJobRequest{
			Restore:      restore,
			BackupRun:    backupRun,
			Source:       source,
			Storage:      storage,
			StoragePath:  storagePath,
			Execution:    execution,
			DriverConfig: driverConfig,
			Snapshot:     snapshot,
		})
	}
	execution = defaultExecutionTemplate(execution)
	name := dpv1alpha1.BuildJobName(restore.Name, "restore")
	labels := managedResourceLabels("RestoreRequest", restore.Name, "restore", source.Name, storage.Name)
	annotations := map[string]string{
		snapshotAnnotation:    snapshot,
		targetModeAnnotation:  string(effectiveRestoreTargetMode(restore.Spec.Target.Mode)),
		storagePathAnnotation: storagePath,
		placeholderAnnotation: boolString(len(execution.Command) == 0 && len(execution.Args) == 0),
	}
	if reason := strings.TrimSpace(restore.Spec.Reason); reason != "" {
		annotations[reasonAnnotation] = reason
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   restore.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: buildJobSpec(execution, labels, buildRestoreEnvVars(restore, backupRun, source, storage, storagePath, snapshot), "restore"),
	}, nil
}

func buildJobSpec(execution dpv1alpha1.ExecutionTemplateSpec, labels map[string]string, env []corev1.EnvVar, operation string) batchv1.JobSpec {
	return batchv1.JobSpec{
		BackoffLimit:            cloneInt32Ptr(execution.BackoffLimit),
		TTLSecondsAfterFinished: cloneInt32Ptr(execution.TTLSecondsAfterFinished),
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: copyStringMap(labels),
			},
			Spec: buildPodSpec(execution, env, operation),
		},
	}
}

func buildPodSpec(execution dpv1alpha1.ExecutionTemplateSpec, env []corev1.EnvVar, operation string) corev1.PodSpec {
	command := cloneStringSlice(execution.Command)
	args := cloneStringSlice(execution.Args)
	if len(command) == 0 && len(args) == 0 {
		command = []string{"/bin/sh", "-c"}
		args = []string{placeholderScript(operation)}
	}

	resources := execution.Resources.DeepCopy()
	return corev1.PodSpec{
		RestartPolicy:      corev1.RestartPolicyNever,
		ServiceAccountName: execution.ServiceAccountName,
		NodeSelector:       copyStringMap(execution.NodeSelector),
		Tolerations:        cloneTolerations(execution.Tolerations),
		Containers: []corev1.Container{
			{
				Name:            "runner",
				Image:           execution.RunnerImage,
				ImagePullPolicy: execution.ImagePullPolicy,
				Command:         command,
				Args:            args,
				Env:             env,
				Resources:       *resources,
			},
		},
	}
}

func buildBackupEnvVars(
	policyName, runName string,
	source *dpv1alpha1.BackupSource,
	storage *dpv1alpha1.BackupStorage,
	storagePath string,
	driverConfig dpv1alpha1.DriverConfig,
	extraEnv []corev1.EnvVar,
	snapshot, reason string,
) []corev1.EnvVar {
	envs := []corev1.EnvVar{
		{Name: "DP_OPERATION", Value: "backup"},
		{Name: "DP_SOURCE_NAME", Value: source.Name},
		{Name: "DP_SOURCE_DRIVER", Value: string(source.Spec.Driver)},
		{Name: "DP_STORAGE_NAME", Value: storage.Name},
		{Name: "DP_STORAGE_TYPE", Value: string(storage.Spec.Type)},
		{Name: "DP_STORAGE_PATH", Value: storagePath},
	}
	if policyName != "" {
		envs = append(envs, corev1.EnvVar{Name: "DP_POLICY_NAME", Value: policyName})
	}
	if runName != "" {
		envs = append(envs, corev1.EnvVar{Name: "DP_BACKUP_RUN_NAME", Value: runName})
	}
	if snapshot != "" {
		envs = append(envs, corev1.EnvVar{Name: "DP_SNAPSHOT", Value: snapshot})
	}
	if reason != "" {
		envs = append(envs, corev1.EnvVar{Name: "DP_REASON", Value: reason})
	}
	envs = append(envs, endpointEnvVars("DP_SOURCE", source.Spec.Endpoint)...)
	envs = append(envs, storageEnvVars(storage)...)
	envs = append(envs, driverConfigEnvVars(driverConfig)...)
	return mergeEnvVars(envs, extraEnv)
}

func buildRestoreEnvVars(
	restore *dpv1alpha1.RestoreRequest,
	backupRun *dpv1alpha1.BackupRun,
	source *dpv1alpha1.BackupSource,
	storage *dpv1alpha1.BackupStorage,
	storagePath, snapshot string,
) []corev1.EnvVar {
	envs := []corev1.EnvVar{
		{Name: "DP_OPERATION", Value: "restore"},
		{Name: "DP_SOURCE_NAME", Value: source.Name},
		{Name: "DP_SOURCE_DRIVER", Value: string(source.Spec.Driver)},
		{Name: "DP_STORAGE_NAME", Value: storage.Name},
		{Name: "DP_STORAGE_TYPE", Value: string(storage.Spec.Type)},
		{Name: "DP_STORAGE_PATH", Value: storagePath},
		{Name: "DP_RESTORE_REQUEST_NAME", Value: restore.Name},
		{Name: "DP_RESTORE_TARGET_MODE", Value: string(effectiveRestoreTargetMode(restore.Spec.Target.Mode))},
	}
	if snapshot != "" {
		envs = append(envs, corev1.EnvVar{Name: "DP_SNAPSHOT", Value: snapshot})
	}
	if backupRun != nil {
		envs = append(envs, corev1.EnvVar{Name: "DP_BACKUP_RUN_NAME", Value: backupRun.Name})
	}
	if reason := strings.TrimSpace(restore.Spec.Reason); reason != "" {
		envs = append(envs, corev1.EnvVar{Name: "DP_REASON", Value: reason})
	}
	envs = append(envs, endpointEnvVars("DP_SOURCE", source.Spec.Endpoint)...)
	envs = append(envs, storageEnvVars(storage)...)
	if restore.Spec.Target.Endpoint != nil {
		envs = append(envs, endpointEnvVars("DP_TARGET", *restore.Spec.Target.Endpoint)...)
	}
	return envs
}

func endpointEnvVars(prefix string, endpoint dpv1alpha1.EndpointSpec) []corev1.EnvVar {
	envs := []corev1.EnvVar{}
	if host := strings.TrimSpace(endpoint.Host); host != "" {
		envs = append(envs, corev1.EnvVar{Name: prefix + "_HOST", Value: host})
	}
	if endpoint.Port > 0 {
		envs = append(envs, corev1.EnvVar{Name: prefix + "_PORT", Value: fmt.Sprintf("%d", endpoint.Port)})
	}
	if scheme := strings.TrimSpace(endpoint.Scheme); scheme != "" {
		envs = append(envs, corev1.EnvVar{Name: prefix + "_SCHEME", Value: scheme})
	}
	if username := strings.TrimSpace(endpoint.Username); username != "" {
		envs = append(envs, corev1.EnvVar{Name: prefix + "_USERNAME", Value: username})
	}
	if endpoint.ServiceRef != nil {
		envs = append(envs,
			corev1.EnvVar{Name: prefix + "_SERVICE_NAME", Value: endpoint.ServiceRef.Name},
			corev1.EnvVar{Name: prefix + "_SERVICE_NAMESPACE", Value: endpoint.ServiceRef.Namespace},
		)
		if endpoint.ServiceRef.Port > 0 {
			envs = append(envs, corev1.EnvVar{Name: prefix + "_SERVICE_PORT", Value: fmt.Sprintf("%d", endpoint.ServiceRef.Port)})
		}
	}
	envs = appendSecretEnvVar(envs, prefix+"_USERNAME", endpoint.UsernameFrom)
	envs = appendSecretEnvVar(envs, prefix+"_PASSWORD", endpoint.PasswordFrom)
	return envs
}

func storageEnvVars(storage *dpv1alpha1.BackupStorage) []corev1.EnvVar {
	envs := []corev1.EnvVar{}
	switch storage.Spec.Type {
	case dpv1alpha1.StorageTypeNFS:
		if storage.Spec.NFS != nil {
			envs = append(envs,
				corev1.EnvVar{Name: "DP_STORAGE_NFS_SERVER", Value: storage.Spec.NFS.Server},
				corev1.EnvVar{Name: "DP_STORAGE_NFS_PATH", Value: storage.Spec.NFS.Path},
			)
		}
	case dpv1alpha1.StorageTypeS3:
		if storage.Spec.S3 != nil {
			envs = append(envs,
				corev1.EnvVar{Name: "DP_STORAGE_S3_ENDPOINT", Value: storage.Spec.S3.Endpoint},
				corev1.EnvVar{Name: "DP_STORAGE_S3_BUCKET", Value: storage.Spec.S3.Bucket},
				corev1.EnvVar{Name: "DP_STORAGE_S3_PREFIX", Value: storage.Spec.S3.Prefix},
				corev1.EnvVar{Name: "DP_STORAGE_S3_REGION", Value: storage.Spec.S3.Region},
				corev1.EnvVar{Name: "DP_STORAGE_S3_INSECURE", Value: boolString(storage.Spec.S3.Insecure)},
			)
			envs = appendSecretEnvVar(envs, "DP_STORAGE_S3_ACCESS_KEY", storage.Spec.S3.AccessKeyFrom)
			envs = appendSecretEnvVar(envs, "DP_STORAGE_S3_SECRET_KEY", storage.Spec.S3.SecretKeyFrom)
			envs = appendSecretEnvVar(envs, "DP_STORAGE_S3_SESSION_TOKEN", storage.Spec.S3.SessionTokenRef)
		}
	}
	return envs
}

func driverConfigEnvVars(driverConfig dpv1alpha1.DriverConfig) []corev1.EnvVar {
	envs := []corev1.EnvVar{}
	if driverConfig.MySQL != nil {
		envs = append(envs,
			corev1.EnvVar{Name: "DP_MYSQL_MODE", Value: driverConfig.MySQL.Mode},
			corev1.EnvVar{Name: "DP_MYSQL_DATABASES", Value: strings.Join(driverConfig.MySQL.Databases, ",")},
			corev1.EnvVar{Name: "DP_MYSQL_TABLES", Value: strings.Join(driverConfig.MySQL.Tables, ",")},
			corev1.EnvVar{Name: "DP_MYSQL_RESTORE_MODE", Value: driverConfig.MySQL.RestoreMode},
		)
	}
	if driverConfig.Redis != nil {
		databases := make([]string, 0, len(driverConfig.Redis.Databases))
		for _, database := range driverConfig.Redis.Databases {
			databases = append(databases, fmt.Sprintf("%d", database))
		}
		envs = append(envs,
			corev1.EnvVar{Name: "DP_REDIS_MODE", Value: driverConfig.Redis.Mode},
			corev1.EnvVar{Name: "DP_REDIS_DATABASES", Value: strings.Join(databases, ",")},
			corev1.EnvVar{Name: "DP_REDIS_KEY_PREFIX", Value: strings.Join(driverConfig.Redis.KeyPrefix, ",")},
		)
	}
	if driverConfig.MongoDB != nil {
		envs = append(envs,
			corev1.EnvVar{Name: "DP_MONGODB_DATABASES", Value: strings.Join(driverConfig.MongoDB.Databases, ",")},
			corev1.EnvVar{Name: "DP_MONGODB_COLLECTIONS", Value: strings.Join(driverConfig.MongoDB.Collections, ",")},
			corev1.EnvVar{Name: "DP_MONGODB_INCLUDE_USERS_ROLES", Value: boolString(driverConfig.MongoDB.IncludeUsersRoles)},
		)
	}
	if driverConfig.MinIO != nil {
		envs = append(envs,
			corev1.EnvVar{Name: "DP_MINIO_BUCKETS", Value: strings.Join(driverConfig.MinIO.Buckets, ",")},
			corev1.EnvVar{Name: "DP_MINIO_PREFIXES", Value: strings.Join(driverConfig.MinIO.Prefixes, ",")},
			corev1.EnvVar{Name: "DP_MINIO_INCLUDE_VERSIONS", Value: boolString(driverConfig.MinIO.IncludeVersions)},
		)
	}
	if driverConfig.RabbitMQ != nil {
		envs = append(envs,
			corev1.EnvVar{Name: "DP_RABBITMQ_INCLUDE_DEFINITIONS", Value: boolString(driverConfig.RabbitMQ.IncludeDefinitions)},
			corev1.EnvVar{Name: "DP_RABBITMQ_VHOSTS", Value: strings.Join(driverConfig.RabbitMQ.Vhosts, ",")},
			corev1.EnvVar{Name: "DP_RABBITMQ_QUEUES", Value: strings.Join(driverConfig.RabbitMQ.Queues, ",")},
		)
	}
	if driverConfig.Milvus != nil {
		envs = append(envs,
			corev1.EnvVar{Name: "DP_MILVUS_DATABASES", Value: strings.Join(driverConfig.Milvus.Databases, ",")},
			corev1.EnvVar{Name: "DP_MILVUS_COLLECTIONS", Value: strings.Join(driverConfig.Milvus.Collections, ",")},
			corev1.EnvVar{Name: "DP_MILVUS_INCLUDE_OBJECT_STORAGE", Value: boolString(driverConfig.Milvus.IncludeObjectStorage)},
		)
	}
	return envs
}

func managedResourceLabels(ownerKind, ownerName, operation, sourceName, storageName string) map[string]string {
	labels := map[string]string{
		managedByLabel:    managedByValue,
		resourceKindLabel: ownerKind,
		resourceNameLabel: dpv1alpha1.BuildLabelValue(ownerName),
		operationLabel:    operation,
		sourceNameLabel:   dpv1alpha1.BuildLabelValue(sourceName),
	}
	if strings.TrimSpace(storageName) != "" {
		labels[storageNameLabel] = dpv1alpha1.BuildLabelValue(storageName)
	}
	return labels
}

func appendSecretEnvVar(envs []corev1.EnvVar, name string, ref *dpv1alpha1.SecretKeyReference) []corev1.EnvVar {
	if ref == nil || strings.TrimSpace(ref.Name) == "" || strings.TrimSpace(ref.Key) == "" {
		return envs
	}
	return append(envs, corev1.EnvVar{
		Name: name,
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: ref.Name},
				Key:                  ref.Key,
			},
		},
	})
}

func mergeEnvVars(base, overrides []corev1.EnvVar) []corev1.EnvVar {
	merged := make([]corev1.EnvVar, 0, len(base)+len(overrides))
	indexByName := map[string]int{}

	appendEnv := func(env corev1.EnvVar) {
		if existing, ok := indexByName[env.Name]; ok {
			merged[existing] = env
			return
		}
		indexByName[env.Name] = len(merged)
		merged = append(merged, env)
	}

	for _, env := range base {
		appendEnv(env)
	}
	for _, env := range overrides {
		appendEnv(env)
	}
	return merged
}

func copyStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func mergeStringMaps(base, overlay map[string]string) map[string]string {
	merged := copyStringMap(base)
	if merged == nil {
		merged = map[string]string{}
	}
	for key, value := range overlay {
		merged[key] = value
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

func cloneStringSlice(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, len(input))
	copy(output, input)
	return output
}

func cloneTolerations(input []corev1.Toleration) []corev1.Toleration {
	if len(input) == 0 {
		return nil
	}
	output := make([]corev1.Toleration, len(input))
	copy(output, input)
	return output
}

func cloneInt32Ptr(value *int32) *int32 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneInt64Ptr(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func int32Ptr(value int32) *int32 {
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	unique := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	sort.Strings(unique)
	return unique
}

func placeholderScript(operation string) string {
	return strings.Join([]string{
		"set -eu",
		"echo \"data-protection operator placeholder runner\"",
		"echo \"operation=" + operation + "\"",
		"echo \"source=${DP_SOURCE_NAME:-}\"",
		"echo \"storage=${DP_STORAGE_NAME:-}\"",
		"echo \"snapshot=${DP_SNAPSHOT:-}\"",
		"date -u +\"%Y-%m-%dT%H:%M:%SZ\"",
	}, "\n")
}

func effectiveRestoreTargetMode(mode dpv1alpha1.RestoreTargetMode) dpv1alpha1.RestoreTargetMode {
	if strings.TrimSpace(string(mode)) == "" {
		return dpv1alpha1.RestoreTargetModeInPlace
	}
	return mode
}

func effectiveDriverConfig(base, override dpv1alpha1.DriverConfig) dpv1alpha1.DriverConfig {
	result := dpv1alpha1.DriverConfig{}
	switch {
	case override.MySQL != nil:
		result.MySQL = override.MySQL.DeepCopy()
	case base.MySQL != nil:
		result.MySQL = base.MySQL.DeepCopy()
	}
	switch {
	case override.Redis != nil:
		result.Redis = override.Redis.DeepCopy()
	case base.Redis != nil:
		result.Redis = base.Redis.DeepCopy()
	}
	switch {
	case override.MongoDB != nil:
		result.MongoDB = override.MongoDB.DeepCopy()
	case base.MongoDB != nil:
		result.MongoDB = base.MongoDB.DeepCopy()
	}
	switch {
	case override.MinIO != nil:
		result.MinIO = override.MinIO.DeepCopy()
	case base.MinIO != nil:
		result.MinIO = base.MinIO.DeepCopy()
	}
	switch {
	case override.RabbitMQ != nil:
		result.RabbitMQ = override.RabbitMQ.DeepCopy()
	case base.RabbitMQ != nil:
		result.RabbitMQ = base.RabbitMQ.DeepCopy()
	}
	switch {
	case override.Milvus != nil:
		result.Milvus = override.Milvus.DeepCopy()
	case base.Milvus != nil:
		result.Milvus = base.Milvus.DeepCopy()
	}
	return result
}

func jobPhase(job *batchv1.Job) (dpv1alpha1.ResourcePhase, string, *metav1.Time) {
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			completedAt := condition.LastTransitionTime
			return dpv1alpha1.ResourcePhaseFailed, coalesceConditionMessage(condition, "job failed"), &completedAt
		}
		if condition.Type == batchv1.JobComplete && condition.Status == corev1.ConditionTrue {
			completedAt := condition.LastTransitionTime
			return dpv1alpha1.ResourcePhaseSucceeded, coalesceConditionMessage(condition, "job completed successfully"), &completedAt
		}
	}

	if job.Status.Active > 0 {
		return dpv1alpha1.ResourcePhaseRunning, fmt.Sprintf("job is running with %d active pod(s)", job.Status.Active), nil
	}
	if job.Status.Succeeded > 0 {
		return dpv1alpha1.ResourcePhaseSucceeded, "job completed successfully", nowTime()
	}
	if job.Status.Failed > 0 {
		return dpv1alpha1.ResourcePhaseRunning, fmt.Sprintf("job has %d failed pod attempt(s) and may retry", job.Status.Failed), nil
	}
	return dpv1alpha1.ResourcePhasePending, "job is pending scheduling or startup", nil
}

func coalesceConditionMessage(condition batchv1.JobCondition, fallback string) string {
	if message := strings.TrimSpace(condition.Message); message != "" {
		return message
	}
	if reason := strings.TrimSpace(condition.Reason); reason != "" {
		return reason
	}
	return fallback
}

func isDependencyMissing(err error) bool {
	return apierrors.IsNotFound(err)
}
