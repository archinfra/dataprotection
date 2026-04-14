package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dpv1alpha1 "github.com/archinfra/dataprotection/api/v1alpha1"
)

const (
	managedByLabel             = "dataprotection.archinfra.io/managed-by"
	operationLabel             = "dataprotection.archinfra.io/operation"
	executionKindLabel         = "dataprotection.archinfra.io/execution-kind"
	executionNameLabel         = "dataprotection.archinfra.io/execution-name"
	sourceNameLabel            = "dataprotection.archinfra.io/source-name"
	storageNameLabel           = "dataprotection.archinfra.io/storage-name"
	policyNameLabel            = "dataprotection.archinfra.io/policy-name"
	addonNameLabel             = "dataprotection.archinfra.io/addon-name"
	seriesAnnotation           = "dataprotection.archinfra.io/series"
	backendPathAnnotation      = "dataprotection.archinfra.io/backend-path"
	importPathAnnotation       = "dataprotection.archinfra.io/import-path"
	notificationRefsAnnotation = "dataprotection.archinfra.io/notification-refs"
	managedByValue             = "data-protection-operator"
)

type resolvedBackupExecution struct {
	Policy          *dpv1alpha1.BackupPolicy
	Source          *dpv1alpha1.BackupSource
	Addon           *dpv1alpha1.BackupAddon
	Storage         *dpv1alpha1.BackupStorage
	RetentionPolicy *dpv1alpha1.RetentionPolicy
	Series          string
	BackendPath     string
	KeepLast        int32
}

type resolvedRestoreExecution struct {
	Source       *dpv1alpha1.BackupSource
	Addon        *dpv1alpha1.BackupAddon
	Storage      *dpv1alpha1.BackupStorage
	Snapshot     *dpv1alpha1.Snapshot
	ImportSource *dpv1alpha1.RestoreImportSource
}

func (r *resolvedRestoreExecution) usesSnapshot() bool {
	return r != nil && r.Snapshot != nil
}

func (r *resolvedRestoreExecution) artifactPath() string {
	if r == nil {
		return ""
	}
	if r.usesSnapshot() {
		return path.Join(r.Snapshot.Spec.BackendPath, "snapshots", r.Snapshot.Spec.Snapshot+".tgz")
	}
	if r.ImportSource == nil {
		return ""
	}
	return r.ImportSource.NormalizedPath()
}

func (r *resolvedRestoreExecution) artifactFormat() dpv1alpha1.RestoreArtifactFormat {
	if r == nil || r.usesSnapshot() {
		return dpv1alpha1.RestoreArtifactFormatArchive
	}
	return r.ImportSource.EffectiveFormat()
}

func (r *resolvedRestoreExecution) backendPath() string {
	if r == nil {
		return ""
	}
	if r.usesSnapshot() {
		return r.Snapshot.Spec.BackendPath
	}
	return r.artifactPath()
}

func (r *resolvedRestoreExecution) series() string {
	if r == nil {
		return ""
	}
	if r.usesSnapshot() {
		return r.Snapshot.Spec.Series
	}
	if series := trimString(r.ImportSource.Series); series != "" {
		return series
	}
	return strings.Join([]string{
		"import",
		"source",
		dpv1alpha1.BuildLabelValue(r.Source.Namespace),
		dpv1alpha1.BuildLabelValue(r.Source.Name),
		"storage",
		dpv1alpha1.BuildLabelValue(r.Storage.Name),
	}, "/")
}

func (r *resolvedRestoreExecution) snapshotName() string {
	if r == nil {
		return ""
	}
	if r.usesSnapshot() {
		return r.Snapshot.Spec.Snapshot
	}
	return trimString(r.ImportSource.EffectiveSnapshotName())
}

func (r *resolvedRestoreExecution) notificationAttributes() map[string]string {
	if r == nil || r.usesSnapshot() {
		return nil
	}
	return map[string]string{
		"restoreSource": "importSource",
		"importPath":    r.artifactPath(),
		"importFormat":  string(r.artifactFormat()),
	}
}

func resolvePolicyDependencies(ctx context.Context, c client.Client, policy *dpv1alpha1.BackupPolicy) (*dpv1alpha1.BackupSource, *dpv1alpha1.BackupAddon, []*dpv1alpha1.BackupStorage, *dpv1alpha1.RetentionPolicy, error) {
	source, err := getBackupSource(ctx, c, policy.Namespace, policy.Spec.SourceRef.Name)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	addon, err := getBackupAddon(ctx, c, source.Spec.AddonRef.Name)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	storages := make([]*dpv1alpha1.BackupStorage, 0, len(policy.Spec.StorageRefs))
	for _, ref := range policy.Spec.StorageRefs {
		storage, err := getBackupStorage(ctx, c, policy.Namespace, ref.Name)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		storages = append(storages, storage)
	}
	retention, err := resolveRetentionPolicy(ctx, c, policy.Namespace, localRefName(policy.Spec.RetentionRef))
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return source, addon, storages, retention, nil
}

func resolveBackupJobDependencies(ctx context.Context, c client.Client, job *dpv1alpha1.BackupJob) (*resolvedBackupExecution, error) {
	source, err := getBackupSource(ctx, c, job.Namespace, job.Spec.SourceRef.Name)
	if err != nil {
		return nil, err
	}
	addon, err := getBackupAddon(ctx, c, source.Spec.AddonRef.Name)
	if err != nil {
		return nil, err
	}
	storage, err := getBackupStorage(ctx, c, job.Namespace, job.Spec.StorageRef.Name)
	if err != nil {
		return nil, err
	}
	var policy *dpv1alpha1.BackupPolicy
	if job.Spec.PolicyRef != nil && trimString(job.Spec.PolicyRef.Name) != "" {
		policy, err = getBackupPolicy(ctx, c, job.Namespace, job.Spec.PolicyRef.Name)
		if err != nil {
			return nil, err
		}
	}
	refName := localRefName(job.Spec.RetentionRef)
	if refName == "" && policy != nil {
		refName = localRefName(policy.Spec.RetentionRef)
	}
	retention, err := resolveRetentionPolicy(ctx, c, job.Namespace, refName)
	if err != nil {
		return nil, err
	}
	series := buildSeries(source, storage.Name, localRefName(job.Spec.PolicyRef), job.Name)
	return &resolvedBackupExecution{
		Policy:          policy,
		Source:          source,
		Addon:           addon,
		Storage:         storage,
		RetentionPolicy: retention,
		Series:          series,
		BackendPath:     buildBackendPath(source, storage.Name, localRefName(job.Spec.PolicyRef), job.Name),
		KeepLast:        effectiveSuccessfulKeepLast(retention),
	}, nil
}

func resolveRestoreJobDependencies(ctx context.Context, c client.Client, job *dpv1alpha1.RestoreJob) (*resolvedRestoreExecution, error) {
	source, err := getBackupSource(ctx, c, job.Namespace, job.Spec.SourceRef.Name)
	if err != nil {
		return nil, err
	}
	addon, err := getBackupAddon(ctx, c, source.Spec.AddonRef.Name)
	if err != nil {
		return nil, err
	}
	if snapshotName := trimString(job.Spec.SnapshotRef.Name); snapshotName != "" {
		snapshot, err := getSnapshot(ctx, c, job.Namespace, snapshotName)
		if err != nil {
			return nil, err
		}
		storage, err := getBackupStorage(ctx, c, job.Namespace, snapshot.Spec.StorageRef.Name)
		if err != nil {
			return nil, err
		}
		return &resolvedRestoreExecution{Source: source, Addon: addon, Storage: storage, Snapshot: snapshot}, nil
	}
	importSource := *job.Spec.ImportSource
	importSource.Path = job.Spec.ImportSource.NormalizedPath()
	storage, err := getBackupStorage(ctx, c, job.Namespace, importSource.StorageRef.Name)
	if err != nil {
		return nil, err
	}
	return &resolvedRestoreExecution{
		Source:       source,
		Addon:        addon,
		Storage:      storage,
		ImportSource: &importSource,
	}, nil
}

func resolveRetentionPolicy(ctx context.Context, c client.Client, namespace, name string) (*dpv1alpha1.RetentionPolicy, error) {
	if name == "" {
		return nil, nil
	}
	return getRetentionPolicy(ctx, c, namespace, name)
}

func buildSeries(source *dpv1alpha1.BackupSource, storageName, policyName, manualName string) string {
	parts := []string{"source", dpv1alpha1.BuildLabelValue(source.Namespace), dpv1alpha1.BuildLabelValue(source.Name)}
	if policyName != "" {
		parts = append(parts, "policy", dpv1alpha1.BuildLabelValue(policyName))
	} else {
		parts = append(parts, "manual", dpv1alpha1.BuildLabelValue(manualName))
	}
	parts = append(parts, "storage", dpv1alpha1.BuildLabelValue(storageName))
	return strings.Join(parts, "/")
}

func buildBackendPath(source *dpv1alpha1.BackupSource, storageName, policyName, manualName string) string {
	return strings.Join([]string{
		"series",
		dpv1alpha1.BuildLabelValue(source.Namespace),
		dpv1alpha1.BuildLabelValue(source.Name),
		func() string {
			if policyName != "" {
				return "policy"
			}
			return "manual"
		}(),
		func() string {
			if policyName != "" {
				return dpv1alpha1.BuildLabelValue(policyName)
			}
			return dpv1alpha1.BuildLabelValue(manualName)
		}(),
		"storage",
		dpv1alpha1.BuildLabelValue(storageName),
	}, "/")
}

func buildBackupCronJob(policy *dpv1alpha1.BackupPolicy, source *dpv1alpha1.BackupSource, addon *dpv1alpha1.BackupAddon, storage *dpv1alpha1.BackupStorage, retention *dpv1alpha1.RetentionPolicy) (*batchv1.CronJob, error) {
	series := buildSeries(source, storage.Name, policy.Name, "")
	backendPath := buildBackendPath(source, storage.Name, policy.Name, "")
	jobSpec, jobLabels, jobAnnotations, err := buildBackupJobSpec(
		"BackupPolicy",
		policy.Name,
		series,
		backendPath,
		policy.Spec.JobRuntime,
		policy.Spec.NotificationRefs,
		source,
		addon,
		storage,
		effectiveSuccessfulKeepLast(retention),
		"",
	)
	if err != nil {
		return nil, err
	}
	return &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dpv1alpha1.BuildCronJobName(policy.Name, storage.Name, "backup"),
			Namespace: policy.Namespace,
			Labels:    managedResourceLabels("BackupPolicy", policy.Name, "backup", source.Name, storage.Name),
		},
		Spec: batchv1.CronJobSpec{
			Schedule:                   policy.Spec.Schedule.Cron,
			Suspend:                    boolPtr(policy.Spec.Suspend || policy.Spec.Schedule.Suspend || source.Spec.Paused),
			ConcurrencyPolicy:          policy.Spec.Schedule.EffectiveConcurrencyPolicy(),
			StartingDeadlineSeconds:    cloneInt64Ptr(policy.Spec.Schedule.StartingDeadlineSeconds),
			SuccessfulJobsHistoryLimit: defaultCronJobSuccessfulHistoryLimit(),
			FailedJobsHistoryLimit:     defaultCronJobFailedHistoryLimit(),
			JobTemplate: batchv1.JobTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      copyStringMap(jobLabels),
					Annotations: copyStringMap(jobAnnotations),
				},
				Spec: jobSpec,
			},
		},
	}, nil
}

func buildManualBackupNativeJob(job *dpv1alpha1.BackupJob, resolved *resolvedBackupExecution) (*batchv1.Job, error) {
	spec, labels, annotations, err := buildBackupJobSpec(
		"BackupJob",
		job.Name,
		resolved.Series,
		resolved.BackendPath,
		mergeJobRuntime(job.Spec.JobRuntime, resolved.Policy),
		resolveJobNotificationRefs(job.Spec.NotificationRefs, resolved.Policy),
		resolved.Source,
		resolved.Addon,
		resolved.Storage,
		resolved.KeepLast,
		job.Spec.SnapshotName,
	)
	if err != nil {
		return nil, err
	}
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        dpv1alpha1.BuildJobName(job.Name, "backup"),
			Namespace:   job.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: spec,
	}, nil
}

func buildRestoreNativeJob(job *dpv1alpha1.RestoreJob, resolved *resolvedRestoreExecution) (*batchv1.Job, error) {
	spec, labels, annotations, err := buildRestoreJobSpec(job, resolved)
	if err != nil {
		return nil, err
	}
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        dpv1alpha1.BuildJobName(job.Name, "restore"),
			Namespace:   job.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: spec,
	}, nil
}

func buildBackupJobSpec(executionKind, executionName, series, backendPath string, runtime dpv1alpha1.JobRuntimeSpec, notificationRefs []corev1.LocalObjectReference, source *dpv1alpha1.BackupSource, addon *dpv1alpha1.BackupAddon, storage *dpv1alpha1.BackupStorage, keepLast int32, requestedSnapshot string) (batchv1.JobSpec, map[string]string, map[string]string, error) {
	runtime = defaultJobRuntime(runtime)
	labels := managedResourceLabels(executionKind, executionName, "backup", source.Name, storage.Name)
	labels[addonNameLabel] = dpv1alpha1.BuildLabelValue(addon.Name)
	if executionKind == "BackupPolicy" {
		labels[policyNameLabel] = dpv1alpha1.BuildLabelValue(executionName)
	}
	annotations := map[string]string{
		seriesAnnotation:           series,
		backendPathAnnotation:      backendPath,
		notificationRefsAnnotation: notificationRefNames(notificationRefs),
	}
	env := buildSourceEnv(source)
	env = append(env,
		corev1.EnvVar{Name: "DP_OPERATION", Value: "backup"},
		corev1.EnvVar{Name: "DP_EXECUTION_KIND", Value: executionKind},
		corev1.EnvVar{Name: "DP_EXECUTION_NAME", Value: executionName},
		corev1.EnvVar{Name: "DP_EXECUTION_SLUG", Value: dpv1alpha1.BuildLabelValue(executionName)},
		corev1.EnvVar{Name: "DP_SERIES", Value: series},
		corev1.EnvVar{Name: "DP_BACKEND_PATH", Value: backendPath},
		corev1.EnvVar{Name: "DP_KEEP_LAST", Value: strconv.Itoa(int(keepLast))},
		corev1.EnvVar{Name: "DP_WORKSPACE_OUTPUT", Value: "/workspace/output"},
		corev1.EnvVar{Name: "DP_STATUS_DIR", Value: "/workspace/status"},
		corev1.EnvVar{Name: "DP_REQUESTED_SNAPSHOT", Value: trimString(requestedSnapshot)},
	)
	podSpec := buildBackupPodSpec(runtime, source, addon, storage, env)
	return singleExecutionJobSpec(batchv1.JobSpec{
		ActiveDeadlineSeconds:   cloneInt64Ptr(runtime.ActiveDeadlineSeconds),
		BackoffLimit:            defaultJobBackoffLimit(),
		TTLSecondsAfterFinished: cloneInt32Ptr(runtime.TTLSecondsAfterFinished),
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels:      copyStringMap(labels),
				Annotations: copyStringMap(annotations),
			},
			Spec: podSpec,
		},
	}), labels, annotations, nil
}

func buildRestoreJobSpec(job *dpv1alpha1.RestoreJob, resolved *resolvedRestoreExecution) (batchv1.JobSpec, map[string]string, map[string]string, error) {
	runtime := defaultJobRuntime(job.Spec.JobRuntime)
	labels := managedResourceLabels("RestoreJob", job.Name, "restore", resolved.Source.Name, resolved.Storage.Name)
	labels[addonNameLabel] = dpv1alpha1.BuildLabelValue(resolved.Addon.Name)
	annotations := map[string]string{
		seriesAnnotation:           resolved.series(),
		backendPathAnnotation:      resolved.backendPath(),
		notificationRefsAnnotation: notificationRefNames(job.Spec.NotificationRefs),
	}
	if !resolved.usesSnapshot() {
		annotations[importPathAnnotation] = resolved.artifactPath()
	}
	env := buildSourceEnvWithSecretRefs(mergeRestoreEndpoint(resolved.Source, job), mergeRestoreSecretRefs(resolved.Source, job))
	env = append(env, buildTargetEnv(mergeRestoreTargetRef(resolved.Source, job))...)
	env = append(env, buildAddonParameterEnvFromMap(mergeRestoreParameters(resolved.Source, job))...)
	env = append(env,
		corev1.EnvVar{Name: "DP_OPERATION", Value: "restore"},
		corev1.EnvVar{Name: "DP_WORKSPACE_INPUT", Value: "/workspace/input"},
		corev1.EnvVar{Name: "DP_STATUS_DIR", Value: "/workspace/status"},
		corev1.EnvVar{Name: "DP_SNAPSHOT", Value: resolved.snapshotName()},
		corev1.EnvVar{Name: "DP_BACKEND_PATH", Value: resolved.backendPath()},
		corev1.EnvVar{Name: "DP_SERIES", Value: resolved.series()},
		corev1.EnvVar{Name: "DP_ARTIFACT_PATH", Value: resolved.artifactPath()},
		corev1.EnvVar{Name: "DP_IMPORT_FORMAT", Value: string(resolved.artifactFormat())},
	)
	podSpec := buildRestorePodSpec(runtime, resolved, env)
	return singleExecutionJobSpec(batchv1.JobSpec{
		ActiveDeadlineSeconds:   cloneInt64Ptr(runtime.ActiveDeadlineSeconds),
		BackoffLimit:            defaultJobBackoffLimit(),
		TTLSecondsAfterFinished: cloneInt32Ptr(runtime.TTLSecondsAfterFinished),
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels:      copyStringMap(labels),
				Annotations: copyStringMap(annotations),
			},
			Spec: podSpec,
		},
	}), labels, annotations, nil
}

func buildBackupPodSpec(runtime dpv1alpha1.JobRuntimeSpec, source *dpv1alpha1.BackupSource, addon *dpv1alpha1.BackupAddon, storage *dpv1alpha1.BackupStorage, env []corev1.EnvVar) corev1.PodSpec {
	env = mergeEnvVars(env, buildAddonParameterEnv(source))
	storageEnv := mergeEnvVars(buildStorageEnv(storage), env)
	volumes := []corev1.Volume{
		{Name: "workspace-output", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		{Name: "workspace-status", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
	}
	if storage.Spec.Type == dpv1alpha1.StorageTypeNFS {
		volumes = append(volumes, corev1.Volume{
			Name: "backend-storage",
			VolumeSource: corev1.VolumeSource{
				NFS: &corev1.NFSVolumeSource{
					Server: storage.Spec.NFS.Server,
					Path:   storage.Spec.NFS.Path,
				},
			},
		})
	}
	return corev1.PodSpec{
		RestartPolicy:      corev1.RestartPolicyNever,
		ServiceAccountName: runtime.ServiceAccountName,
		PriorityClassName:  runtime.PriorityClassName,
		NodeSelector:       copyStringMap(runtime.NodeSelector),
		Tolerations:        cloneTolerations(runtime.Tolerations),
		Volumes:            volumes,
		InitContainers:     []corev1.Container{buildBackupStoragePreflightContainer(storage, storageEnv)},
		Containers: []corev1.Container{
			buildAddonBackupContainer(addon, runtime, env),
			buildArtifactPackageContainer(env),
			buildArtifactUploadContainer(storage, storageEnv),
		},
	}
}

func buildRestorePodSpec(runtime dpv1alpha1.JobRuntimeSpec, resolved *resolvedRestoreExecution, env []corev1.EnvVar) corev1.PodSpec {
	storageEnv := mergeEnvVars(buildStorageEnv(resolved.Storage), env)
	volumes := []corev1.Volume{
		{Name: "workspace-input", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		{Name: "workspace-status", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
	}
	if resolved.Storage.Spec.Type == dpv1alpha1.StorageTypeNFS {
		volumes = append(volumes, corev1.Volume{
			Name: "backend-storage",
			VolumeSource: corev1.VolumeSource{
				NFS: &corev1.NFSVolumeSource{
					Server: resolved.Storage.Spec.NFS.Server,
					Path:   resolved.Storage.Spec.NFS.Path,
				},
			},
		})
	}
	restoreTemplate := resolved.Addon.Spec.BackupTemplate
	if resolved.Addon.Spec.RestoreTemplate != nil {
		restoreTemplate = *resolved.Addon.Spec.RestoreTemplate
	}
	command := addonWrappedCommand(restoreTemplate.Command, restoreTemplate.Args, restoreTemplate.WorkingDir, "/workspace/status/restore.done", "/workspace/status/restore.failed")
	return corev1.PodSpec{
		RestartPolicy:      corev1.RestartPolicyNever,
		ServiceAccountName: runtime.ServiceAccountName,
		PriorityClassName:  runtime.PriorityClassName,
		NodeSelector:       copyStringMap(runtime.NodeSelector),
		Tolerations:        cloneTolerations(runtime.Tolerations),
		Volumes:            volumes,
		InitContainers: []corev1.Container{
			buildRestoreStoragePreflightContainer(resolved.Storage, storageEnv),
			buildArtifactDownloadContainer(resolved, storageEnv),
		},
		Containers: []corev1.Container{{
			Name:            "addon-restore",
			Image:           restoreTemplate.Image,
			ImagePullPolicy: effectivePullPolicy(runtime.ImagePullPolicy, restoreTemplate.Image),
			Command:         []string{"/bin/sh", "-ceu"},
			Args:            []string{command},
			Env:             mergeEnvVars(env, restoreTemplate.Env),
			WorkingDir:      restoreTemplate.WorkingDir,
			VolumeMounts: []corev1.VolumeMount{
				{Name: "workspace-input", MountPath: "/workspace/input"},
				{Name: "workspace-status", MountPath: "/workspace/status"},
			},
			Resources: runtime.Resources,
		}},
	}
}

func buildBackupStoragePreflightContainer(storage *dpv1alpha1.BackupStorage, env []corev1.EnvVar) corev1.Container {
	image := defaultUtilityImage()
	script := buildNFSBackupPreflightScript()
	if storage.Spec.Type == dpv1alpha1.StorageTypeMinIO {
		image = defaultMinIOHelperImage()
		script = buildMinIOBackupPreflightScript()
	}
	return corev1.Container{
		Name:            "storage-preflight",
		Image:           image,
		ImagePullPolicy: defaultImagePullPolicy(image),
		Command:         []string{"/bin/sh", "-ceu"},
		Args:            []string{script},
		Env:             env,
		VolumeMounts:    storageVolumeMounts(storage),
	}
}

func buildRestoreStoragePreflightContainer(storage *dpv1alpha1.BackupStorage, env []corev1.EnvVar) corev1.Container {
	image := defaultUtilityImage()
	script := buildNFSRestorePreflightScript()
	if storage.Spec.Type == dpv1alpha1.StorageTypeMinIO {
		image = defaultMinIOHelperImage()
		script = buildMinIORestorePreflightScript()
	}
	return corev1.Container{
		Name:            "storage-preflight",
		Image:           image,
		ImagePullPolicy: defaultImagePullPolicy(image),
		Command:         []string{"/bin/sh", "-ceu"},
		Args:            []string{script},
		Env:             env,
		VolumeMounts:    storageVolumeMounts(storage),
	}
}

func buildAddonBackupContainer(addon *dpv1alpha1.BackupAddon, runtime dpv1alpha1.JobRuntimeSpec, env []corev1.EnvVar) corev1.Container {
	template := addon.Spec.BackupTemplate
	command := addonWrappedCommand(template.Command, template.Args, template.WorkingDir, "/workspace/status/plugin.done", "/workspace/status/plugin.failed")
	return corev1.Container{
		Name:            "addon-backup",
		Image:           template.Image,
		ImagePullPolicy: effectivePullPolicy(runtime.ImagePullPolicy, template.Image),
		Command:         []string{"/bin/sh", "-ceu"},
		Args:            []string{command},
		Env:             mergeEnvVars(env, template.Env),
		WorkingDir:      template.WorkingDir,
		VolumeMounts: []corev1.VolumeMount{
			{Name: "workspace-output", MountPath: "/workspace/output"},
			{Name: "workspace-status", MountPath: "/workspace/status"},
		},
		Resources: runtime.Resources,
	}
}

func buildArtifactPackageContainer(env []corev1.EnvVar) corev1.Container {
	script := strings.Join([]string{
		"set -eu",
		"trap 'touch /workspace/status/package.failed' ERR",
		"while [ ! -f /workspace/status/plugin.done ]; do",
		"  if [ -f /workspace/status/plugin.failed ]; then",
		"    echo '[ERROR] addon backup failed'",
		"    touch /workspace/status/package.failed",
		"    exit 1",
		"  fi",
		"  sleep 2",
		"done",
		"snapshot_suffix=\"${DP_EXECUTION_SLUG}\"",
		"if [ -n \"${DP_REQUESTED_SNAPSHOT:-}\" ]; then",
		"  snapshot_suffix=\"$(echo \"$DP_REQUESTED_SNAPSHOT\" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9]/-/g')\"",
		"fi",
		"snapshot=\"$(date -u +%Y%m%dT%H%M%SZ)-${snapshot_suffix}\"",
		"mkdir -p /workspace/status/package",
		"tar -czf \"/workspace/status/package/${snapshot}.tgz\" -C /workspace/output .",
		"checksum=\"$(sha256sum \"/workspace/status/package/${snapshot}.tgz\" | awk '{print $1}')\"",
		"size=\"$(wc -c < \"/workspace/status/package/${snapshot}.tgz\" | tr -d ' ')\"",
		"printf '%s' \"$checksum\" > \"/workspace/status/package/${snapshot}.tgz.sha256\"",
		"cat > \"/workspace/status/package/${snapshot}.metadata.json\" <<EOF",
		"{\"series\":\"${DP_SERIES}\",\"snapshot\":\"${snapshot}\",\"checksum\":\"${checksum}\",\"size\":${size},\"completedAt\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\"}",
		"EOF",
		"printf '%s' \"$snapshot\" > /workspace/status/current-snapshot",
		"touch /workspace/status/package.done",
	}, "\n")
	return corev1.Container{
		Name:            "artifact-package",
		Image:           defaultUtilityImage(),
		ImagePullPolicy: defaultImagePullPolicy(defaultUtilityImage()),
		Command:         []string{"/bin/sh", "-ceu"},
		Args:            []string{script},
		Env:             env,
		VolumeMounts: []corev1.VolumeMount{
			{Name: "workspace-output", MountPath: "/workspace/output"},
			{Name: "workspace-status", MountPath: "/workspace/status"},
		},
	}
}

func buildArtifactUploadContainer(storage *dpv1alpha1.BackupStorage, env []corev1.EnvVar) corev1.Container {
	image := defaultUtilityImage()
	script := buildNFSUploadScript()
	if storage.Spec.Type == dpv1alpha1.StorageTypeMinIO {
		image = defaultMinIOHelperImage()
		script = buildMinIOUploadScript()
	}
	return corev1.Container{
		Name:            "artifact-upload",
		Image:           image,
		ImagePullPolicy: defaultImagePullPolicy(image),
		Command:         []string{"/bin/sh", "-ceu"},
		Args:            []string{script},
		Env:             env,
		VolumeMounts: append([]corev1.VolumeMount{
			{Name: "workspace-status", MountPath: "/workspace/status"},
		}, storageVolumeMounts(storage)...),
	}
}

func buildArtifactDownloadContainer(resolved *resolvedRestoreExecution, env []corev1.EnvVar) corev1.Container {
	storage := resolved.Storage
	image := defaultUtilityImage()
	script := buildNFSDownloadScript(resolved.artifactPath(), resolved.artifactFormat())
	if storage.Spec.Type == dpv1alpha1.StorageTypeMinIO {
		image = defaultMinIOHelperImage()
		script = buildMinIODownloadScript(resolved.artifactPath(), resolved.artifactFormat())
	}
	return corev1.Container{
		Name:            "artifact-download",
		Image:           image,
		ImagePullPolicy: defaultImagePullPolicy(image),
		Command:         []string{"/bin/sh", "-ceu"},
		Args:            []string{script},
		Env:             env,
		VolumeMounts: append([]corev1.VolumeMount{
			{Name: "workspace-input", MountPath: "/workspace/input"},
			{Name: "workspace-status", MountPath: "/workspace/status"},
		}, storageVolumeMounts(storage)...),
	}
}

func buildStorageEnv(storage *dpv1alpha1.BackupStorage) []corev1.EnvVar {
	env := []corev1.EnvVar{{Name: "DP_STORAGE_TYPE", Value: string(storage.Spec.Type)}}
	switch storage.Spec.Type {
	case dpv1alpha1.StorageTypeNFS:
		env = append(env,
			corev1.EnvVar{Name: "DP_NFS_SERVER", Value: storage.Spec.NFS.Server},
			corev1.EnvVar{Name: "DP_NFS_PATH", Value: storage.Spec.NFS.Path},
		)
	case dpv1alpha1.StorageTypeMinIO:
		env = append(env,
			corev1.EnvVar{Name: "DP_MINIO_ENDPOINT", Value: storage.Spec.MinIO.Endpoint},
			corev1.EnvVar{Name: "DP_MINIO_BUCKET", Value: storage.Spec.MinIO.Bucket},
			corev1.EnvVar{Name: "DP_MINIO_PREFIX", Value: storage.Spec.MinIO.Prefix},
			corev1.EnvVar{Name: "DP_MINIO_REGION", Value: storage.Spec.MinIO.Region},
			corev1.EnvVar{Name: "DP_MINIO_INSECURE", Value: boolString(storage.Spec.MinIO.Insecure)},
			corev1.EnvVar{Name: "DP_MINIO_AUTO_CREATE_BUCKET", Value: boolString(storage.Spec.MinIO.AutoCreateBucket)},
		)
		env = appendSecretEnvVar(env, "DP_MINIO_ACCESS_KEY", storage.Spec.MinIO.AccessKeyFrom)
		env = appendSecretEnvVar(env, "DP_MINIO_SECRET_KEY", storage.Spec.MinIO.SecretKeyFrom)
	}
	return env
}

func buildSourceEnv(source *dpv1alpha1.BackupSource) []corev1.EnvVar {
	env := buildSourceEnvWithSecretRefs(source.Spec.Endpoint, source.Spec.SecretRefs)
	env = append(env, buildTargetEnv(source.Spec.TargetRef)...)
	return env
}

func buildSourceEnvWithSecretRefs(endpoint dpv1alpha1.EndpointSpec, secretRefs []dpv1alpha1.SecretParameterReference) []corev1.EnvVar {
	env := []corev1.EnvVar{
		{Name: "DP_SOURCE_HOST", Value: endpoint.Host},
		{Name: "DP_SOURCE_PORT", Value: strconv.Itoa(int(endpoint.Port))},
		{Name: "DP_SOURCE_SCHEME", Value: endpoint.Scheme},
		{Name: "DP_SOURCE_USERNAME", Value: endpoint.Username},
	}
	if endpoint.ServiceRef != nil {
		env = append(env,
			corev1.EnvVar{Name: "DP_SOURCE_SERVICE_NAME", Value: endpoint.ServiceRef.Name},
			corev1.EnvVar{Name: "DP_SOURCE_SERVICE_NAMESPACE", Value: endpoint.ServiceRef.Namespace},
			corev1.EnvVar{Name: "DP_SOURCE_SERVICE_PORT", Value: strconv.Itoa(int(endpoint.ServiceRef.Port))},
		)
	}
	env = appendSecretEnvVar(env, "DP_SOURCE_USERNAME_FROM_SECRET", endpoint.UsernameFrom)
	env = appendSecretEnvVar(env, "DP_SOURCE_PASSWORD", endpoint.PasswordFrom)
	for _, ref := range secretRefs {
		env = appendSecretEnvVar(env, envKey("DP_SECRET_", ref.Name), &ref.SecretKeyRef)
	}
	return env
}

func buildTargetEnv(target *dpv1alpha1.NamespacedObjectReference) []corev1.EnvVar {
	if target == nil {
		return nil
	}
	return []corev1.EnvVar{
		{Name: "DP_TARGET_API_VERSION", Value: target.APIVersion},
		{Name: "DP_TARGET_KIND", Value: target.Kind},
		{Name: "DP_TARGET_NAMESPACE", Value: target.Namespace},
		{Name: "DP_TARGET_NAME", Value: target.Name},
	}
}

func buildAddonParameterEnv(source *dpv1alpha1.BackupSource) []corev1.EnvVar {
	return buildAddonParameterEnvFromMap(source.Spec.Parameters)
}

func buildAddonParameterEnvFromMap(parameters map[string]string) []corev1.EnvVar {
	keys := make([]string, 0, len(parameters))
	for key := range parameters {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	env := make([]corev1.EnvVar, 0, len(keys))
	for _, key := range keys {
		env = append(env, corev1.EnvVar{Name: envKey("DP_PARAM_", key), Value: parameters[key]})
	}
	return env
}

func buildNFSBackupPreflightScript() string {
	return strings.Join([]string{
		"set -eu",
		"base=\"/backend/${DP_BACKEND_PATH}\"",
		"mkdir -p \"${base}/snapshots\"",
		"probe=\"${base}/.probe-$$\"",
		"touch \"$probe\"",
		"rm -f \"$probe\"",
		"printf '%s' '" + marshalTerminationSummary(podExecutionSummary{StorageProbe: &storageProbeSummary{Result: dpv1alpha1.ProbeResultSucceeded, Message: "nfs backend is writable"}}) + "' > /dev/termination-log",
	}, "\n")
}

func buildMinIOBackupPreflightScript() string {
	return strings.Join([]string{
		"set -eu",
		"mc_cmd() {",
		"  if [ \"${DP_MINIO_INSECURE:-false}\" = \"true\" ]; then",
		"    mc --insecure \"$@\"",
		"  else",
		"    mc \"$@\"",
		"  fi",
		"}",
		"remote=\"backup/${DP_MINIO_BUCKET}\"",
		"if [ -n \"${DP_MINIO_PREFIX:-}\" ]; then remote=\"${remote}/${DP_MINIO_PREFIX}\"; fi",
		"mc_cmd alias set backup \"${DP_MINIO_ENDPOINT}\" \"${DP_MINIO_ACCESS_KEY}\" \"${DP_MINIO_SECRET_KEY}\" >/dev/null",
		"if ! mc_cmd stat \"backup/${DP_MINIO_BUCKET}\" >/dev/null 2>&1; then",
		"  if [ \"${DP_MINIO_AUTO_CREATE_BUCKET:-false}\" = \"true\" ]; then",
		"    mc_cmd mb \"backup/${DP_MINIO_BUCKET}\" >/dev/null",
		"  else",
		"    echo '[ERROR] minio bucket does not exist and autoCreateBucket=false'",
		"    exit 1",
		"  fi",
		"fi",
		"probe=\"${remote}/.probe-$(date +%s)\"",
		"printf 'ok' > /tmp/dp-probe",
		"mc_cmd cp /tmp/dp-probe \"${probe}\" >/dev/null",
		"mc_cmd rm \"${probe}\" >/dev/null",
		"printf '%s' '" + marshalTerminationSummary(podExecutionSummary{StorageProbe: &storageProbeSummary{Result: dpv1alpha1.ProbeResultSucceeded, Message: "minio backend is reachable"}}) + "' > /dev/termination-log",
	}, "\n")
}

func buildNFSRestorePreflightScript() string {
	return strings.Join([]string{
		"set -eu",
		"artifact=\"/backend/${DP_ARTIFACT_PATH}\"",
		"[ -e \"$artifact\" ] || { echo '[ERROR] restore artifact path not found on nfs backend'; exit 1; }",
		"printf '%s' '" + marshalTerminationSummary(podExecutionSummary{StorageProbe: &storageProbeSummary{Result: dpv1alpha1.ProbeResultSucceeded, Message: "nfs restore artifact is reachable"}}) + "' > /dev/termination-log",
	}, "\n")
}

func buildMinIORestorePreflightScript() string {
	return strings.Join([]string{
		"set -eu",
		"mc_cmd() {",
		"  if [ \"${DP_MINIO_INSECURE:-false}\" = \"true\" ]; then",
		"    mc --insecure \"$@\"",
		"  else",
		"    mc \"$@\"",
		"  fi",
		"}",
		"remote=\"backup/${DP_MINIO_BUCKET}\"",
		"if [ -n \"${DP_MINIO_PREFIX:-}\" ]; then remote=\"${remote}/${DP_MINIO_PREFIX}\"; fi",
		"remote=\"${remote}/${DP_ARTIFACT_PATH}\"",
		"mc_cmd alias set backup \"${DP_MINIO_ENDPOINT}\" \"${DP_MINIO_ACCESS_KEY}\" \"${DP_MINIO_SECRET_KEY}\" >/dev/null",
		"if ! mc_cmd stat \"$remote\" >/dev/null 2>&1 && ! mc_cmd ls \"$remote\" >/dev/null 2>&1; then",
		"  echo '[ERROR] restore artifact path not found on minio backend'",
		"  exit 1",
		"fi",
		"printf '%s' '" + marshalTerminationSummary(podExecutionSummary{StorageProbe: &storageProbeSummary{Result: dpv1alpha1.ProbeResultSucceeded, Message: "minio restore artifact is reachable"}}) + "' > /dev/termination-log",
	}, "\n")
}

func buildNFSUploadScript() string {
	return strings.Join([]string{
		"set -eu",
		"while [ ! -f /workspace/status/package.done ]; do",
		"  if [ -f /workspace/status/package.failed ]; then echo '[ERROR] package stage failed'; exit 1; fi",
		"  sleep 2",
		"done",
		"snapshot=\"$(cat /workspace/status/current-snapshot)\"",
		"base=\"/backend/${DP_BACKEND_PATH}\"",
		"mkdir -p \"${base}/snapshots\"",
		"cp \"/workspace/status/package/${snapshot}.tgz\" \"${base}/snapshots/${snapshot}.tgz\"",
		"cp \"/workspace/status/package/${snapshot}.tgz.sha256\" \"${base}/snapshots/${snapshot}.tgz.sha256\"",
		"cp \"/workspace/status/package/${snapshot}.metadata.json\" \"${base}/snapshots/${snapshot}.metadata.json\"",
		"cp \"/workspace/status/package/${snapshot}.metadata.json\" \"${base}/latest.json\"",
		"[ -f \"${base}/snapshots/${snapshot}.tgz\" ] || { echo '[ERROR] uploaded snapshot not found on nfs backend'; exit 1; }",
		"keep_last=\"${DP_KEEP_LAST:-3}\"",
		"if [ \"$keep_last\" -gt 0 ]; then",
		"  count=0",
		"  for file in $(find \"${base}/snapshots\" -maxdepth 1 -type f -name '*.tgz' -printf '%f\n' | sort -r); do",
		"    count=$((count + 1))",
		"    if [ \"$count\" -le \"$keep_last\" ]; then continue; fi",
		"    name=\"${file%.tgz}\"",
		"    rm -f \"${base}/snapshots/${name}.tgz\" \"${base}/snapshots/${name}.tgz.sha256\" \"${base}/snapshots/${name}.metadata.json\"",
		"  done",
		"fi",
		"checksum=\"$(cat \"/workspace/status/package/${snapshot}.tgz.sha256\")\"",
		"size=\"$(wc -c < \"/workspace/status/package/${snapshot}.tgz\" | tr -d ' ')\"",
		"cat > /dev/termination-log <<EOF",
		fmt.Sprintf(`{"storageProbe":{"result":"%s","message":"artifact uploaded to nfs"},"artifact":{"snapshot":"${snapshot}","backendPath":"${DP_BACKEND_PATH}","checksum":"${checksum}","size":${size},"completedAt":"$(date -u +%%Y-%%m-%%dT%%H:%%M:%%SZ)"}}`, dpv1alpha1.ProbeResultSucceeded),
		"EOF",
	}, "\n")
}

func buildMinIOUploadScript() string {
	return strings.Join([]string{
		"set -eu",
		"mc_cmd() {",
		"  if [ \"${DP_MINIO_INSECURE:-false}\" = \"true\" ]; then",
		"    mc --insecure \"$@\"",
		"  else",
		"    mc \"$@\"",
		"  fi",
		"}",
		"while [ ! -f /workspace/status/package.done ]; do",
		"  if [ -f /workspace/status/package.failed ]; then echo '[ERROR] package stage failed'; exit 1; fi",
		"  sleep 2",
		"done",
		"snapshot=\"$(cat /workspace/status/current-snapshot)\"",
		"remote_base=\"backup/${DP_MINIO_BUCKET}\"",
		"if [ -n \"${DP_MINIO_PREFIX:-}\" ]; then remote_base=\"${remote_base}/${DP_MINIO_PREFIX}\"; fi",
		"remote_base=\"${remote_base}/${DP_BACKEND_PATH}\"",
		"mc_cmd alias set backup \"${DP_MINIO_ENDPOINT}\" \"${DP_MINIO_ACCESS_KEY}\" \"${DP_MINIO_SECRET_KEY}\" >/dev/null",
		"if ! mc_cmd stat \"backup/${DP_MINIO_BUCKET}\" >/dev/null 2>&1; then",
		"  if [ \"${DP_MINIO_AUTO_CREATE_BUCKET:-false}\" = \"true\" ]; then",
		"    mc_cmd mb \"backup/${DP_MINIO_BUCKET}\" >/dev/null",
		"  else",
		"    echo '[ERROR] minio bucket does not exist and autoCreateBucket=false'",
		"    exit 1",
		"  fi",
		"fi",
		"mc_cmd cp \"/workspace/status/package/${snapshot}.tgz\" \"${remote_base}/snapshots/${snapshot}.tgz\" >/dev/null",
		"mc_cmd cp \"/workspace/status/package/${snapshot}.tgz.sha256\" \"${remote_base}/snapshots/${snapshot}.tgz.sha256\" >/dev/null",
		"mc_cmd cp \"/workspace/status/package/${snapshot}.metadata.json\" \"${remote_base}/snapshots/${snapshot}.metadata.json\" >/dev/null",
		"mc_cmd cp \"/workspace/status/package/${snapshot}.metadata.json\" \"${remote_base}/latest.json\" >/dev/null",
		"mc_cmd stat \"${remote_base}/snapshots/${snapshot}.tgz\" >/dev/null",
		"keep_last=\"${DP_KEEP_LAST:-3}\"",
		"if [ \"$keep_last\" -gt 0 ]; then",
		"  count=0",
		"  for file in $(mc_cmd ls \"${remote_base}/snapshots\" | awk '{print $NF}' | sed 's#/$##' | grep '\\.tgz$' | sort -r); do",
		"    count=$((count + 1))",
		"    if [ \"$count\" -le \"$keep_last\" ]; then continue; fi",
		"    name=\"${file%.tgz}\"",
		"    mc_cmd rm --force \"${remote_base}/snapshots/${name}.tgz\" >/dev/null || true",
		"    mc_cmd rm --force \"${remote_base}/snapshots/${name}.tgz.sha256\" >/dev/null || true",
		"    mc_cmd rm --force \"${remote_base}/snapshots/${name}.metadata.json\" >/dev/null || true",
		"  done",
		"fi",
		"checksum=\"$(cat \"/workspace/status/package/${snapshot}.tgz.sha256\")\"",
		"size=\"$(wc -c < \"/workspace/status/package/${snapshot}.tgz\" | tr -d ' ')\"",
		"cat > /dev/termination-log <<EOF",
		fmt.Sprintf(`{"storageProbe":{"result":"%s","message":"artifact uploaded to minio"},"artifact":{"snapshot":"${snapshot}","backendPath":"${DP_BACKEND_PATH}","checksum":"${checksum}","size":${size},"completedAt":"$(date -u +%%Y-%%m-%%dT%%H:%%M:%%SZ)"}}`, dpv1alpha1.ProbeResultSucceeded),
		"EOF",
	}, "\n")
}

func buildNFSDownloadScript(artifactPath string, format dpv1alpha1.RestoreArtifactFormat) string {
	artifactPath = shellQuote(artifactPath)
	format = dpv1alpha1.RestoreArtifactFormat(strings.TrimSpace(string(format)))
	return strings.Join([]string{
		"set -eu",
		fmt.Sprintf("artifact_path=%s", artifactPath),
		fmt.Sprintf("artifact_format=%s", shellQuote(string(format))),
		"mkdir -p /workspace/input /workspace/status/restore",
		"artifact=\"/backend/${artifact_path}\"",
		"if [ \"${artifact_format}\" = \"auto\" ]; then",
		"  case \"$artifact_path\" in",
		"    *.tgz|*.tar.gz|*.tar) artifact_format='archive' ;;",
		"    *) artifact_format='filesystem' ;;",
		"  esac",
		"fi",
		"if [ \"${artifact_format}\" = \"filesystem\" ]; then",
		"  if [ -d \"$artifact\" ]; then",
		"    cp -a \"$artifact\"/. /workspace/input/",
		"  elif [ -f \"$artifact\" ]; then",
		"    cp \"$artifact\" \"/workspace/input/$(basename \"$artifact\")\"",
		"  else",
		"    echo '[ERROR] restore artifact path not found on nfs backend'",
		"    exit 1",
		"  fi",
		"else",
		"  [ -f \"$artifact\" ] || { echo '[ERROR] restore archive not found on nfs backend'; exit 1; }",
		"  cp \"$artifact\" /workspace/status/restore/source.tgz",
		"  tar -xzf /workspace/status/restore/source.tgz -C /workspace/input",
		"fi",
		"printf '%s' '" + marshalTerminationSummary(podExecutionSummary{StorageProbe: &storageProbeSummary{Result: dpv1alpha1.ProbeResultSucceeded, Message: "artifact downloaded from nfs"}}) + "' > /dev/termination-log",
	}, "\n")
}

func buildMinIODownloadScript(artifactPath string, format dpv1alpha1.RestoreArtifactFormat) string {
	artifactPath = shellQuote(artifactPath)
	format = dpv1alpha1.RestoreArtifactFormat(strings.TrimSpace(string(format)))
	return strings.Join([]string{
		"set -eu",
		"mc_cmd() {",
		"  if [ \"${DP_MINIO_INSECURE:-false}\" = \"true\" ]; then",
		"    mc --insecure \"$@\"",
		"  else",
		"    mc \"$@\"",
		"  fi",
		"}",
		fmt.Sprintf("artifact_path=%s", artifactPath),
		fmt.Sprintf("artifact_format=%s", shellQuote(string(format))),
		"remote_base=\"backup/${DP_MINIO_BUCKET}\"",
		"if [ -n \"${DP_MINIO_PREFIX:-}\" ]; then remote_base=\"${remote_base}/${DP_MINIO_PREFIX}\"; fi",
		"artifact=\"${remote_base}/${artifact_path}\"",
		"mc_cmd alias set backup \"${DP_MINIO_ENDPOINT}\" \"${DP_MINIO_ACCESS_KEY}\" \"${DP_MINIO_SECRET_KEY}\" >/dev/null",
		"mkdir -p /workspace/input /workspace/status/restore",
		"if [ \"${artifact_format}\" = \"auto\" ]; then",
		"  case \"$artifact_path\" in",
		"    *.tgz|*.tar.gz|*.tar) artifact_format='archive' ;;",
		"    *) artifact_format='filesystem' ;;",
		"  esac",
		"fi",
		"if [ \"${artifact_format}\" = \"filesystem\" ]; then",
		"  if mc_cmd stat \"$artifact\" >/dev/null 2>&1; then",
		"    file_name=\"$(basename \"$artifact_path\")\"",
		"    mc_cmd cp \"$artifact\" \"/workspace/input/${file_name}\" >/dev/null",
		"  elif mc_cmd ls \"$artifact\" >/dev/null 2>&1; then",
		"    mc_cmd mirror --overwrite \"$artifact\" /workspace/input >/dev/null",
		"  else",
		"    echo '[ERROR] restore artifact path not found on minio backend'",
		"    exit 1",
		"  fi",
		"else",
		"  mc_cmd cp \"$artifact\" /workspace/status/restore/source.tgz >/dev/null",
		"  tar -xzf /workspace/status/restore/source.tgz -C /workspace/input",
		"fi",
		"printf '%s' '" + marshalTerminationSummary(podExecutionSummary{StorageProbe: &storageProbeSummary{Result: dpv1alpha1.ProbeResultSucceeded, Message: "artifact downloaded from minio"}}) + "' > /dev/termination-log",
	}, "\n")
}

func addonWrappedCommand(command, args []string, workingDir, doneFile, failedFile string) string {
	actual := shellCommand(command, args, workingDir)
	return strings.Join([]string{
		"set -eu",
		fmt.Sprintf("trap 'touch %s' ERR", shellQuote(failedFile)),
		actual,
		fmt.Sprintf("touch %s", shellQuote(doneFile)),
	}, "\n")
}

func shellCommand(command, args []string, workingDir string) string {
	lines := make([]string, 0, 3)
	if trimString(workingDir) != "" {
		lines = append(lines, fmt.Sprintf("cd %s", shellQuote(workingDir)))
	}
	switch {
	case len(command) > 0:
		parts := append([]string{}, command...)
		parts = append(parts, args...)
		lines = append(lines, strings.Join(shellQuoteSlice(parts), " "))
	case len(args) > 0:
		lines = append(lines, strings.Join(shellQuoteSlice(args), " "))
	default:
		lines = append(lines, "echo '[ERROR] addon template requires command or args'; exit 1")
	}
	return strings.Join(lines, "\n")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func shellQuoteSlice(values []string) []string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, shellQuote(value))
	}
	return quoted
}

func buildStorageObservationFromPod(pod *corev1.Pod) (*storageProbeSummary, *artifactSummary, string) {
	var probe *storageProbeSummary
	var artifact *artifactSummary
	for _, status := range pod.Status.InitContainerStatuses {
		if status.Name != "storage-preflight" || status.State.Terminated == nil {
			continue
		}
		summary, err := parseJSONTerminationMessage(status.State.Terminated.Message)
		if err != nil || summary == nil {
			continue
		}
		if summary.StorageProbe != nil {
			probe = summary.StorageProbe
		}
	}
	for _, status := range pod.Status.ContainerStatuses {
		if status.State.Terminated == nil {
			continue
		}
		summary, err := parseJSONTerminationMessage(status.State.Terminated.Message)
		if err != nil || summary == nil {
			continue
		}
		if summary.StorageProbe != nil && probe == nil {
			probe = summary.StorageProbe
		}
		if summary.Artifact != nil {
			artifact = summary.Artifact
		}
	}
	message := latestContainerFailureMessage(pod)
	if pod.Status.Phase == corev1.PodSucceeded {
		message = fmt.Sprintf("latest pod %s completed successfully", pod.Name)
	}
	return probe, artifact, message
}

func managedResourceLabels(executionKind, executionName, operation, sourceName, storageName string) map[string]string {
	labels := map[string]string{
		managedByLabel:     managedByValue,
		operationLabel:     dpv1alpha1.BuildLabelValue(operation),
		executionKindLabel: dpv1alpha1.BuildLabelValue(executionKind),
		executionNameLabel: dpv1alpha1.BuildLabelValue(executionName),
		sourceNameLabel:    dpv1alpha1.BuildLabelValue(sourceName),
		storageNameLabel:   dpv1alpha1.BuildLabelValue(storageName),
	}
	if executionKind == "BackupPolicy" {
		labels[policyNameLabel] = dpv1alpha1.BuildLabelValue(executionName)
	}
	return labels
}

func singleExecutionJobSpec(spec batchv1.JobSpec) batchv1.JobSpec {
	spec.Parallelism = int32Ptr(1)
	spec.Completions = int32Ptr(1)
	return spec
}

func mergeEnvVars(base []corev1.EnvVar, overlays ...[]corev1.EnvVar) []corev1.EnvVar {
	result := append([]corev1.EnvVar{}, base...)
	index := map[string]int{}
	for i, env := range result {
		index[env.Name] = i
	}
	for _, overlay := range overlays {
		for _, env := range overlay {
			if existing, ok := index[env.Name]; ok {
				result[existing] = env
				continue
			}
			index[env.Name] = len(result)
			result = append(result, env)
		}
	}
	return result
}

func appendSecretEnvVar(env []corev1.EnvVar, name string, ref *dpv1alpha1.SecretKeyReference) []corev1.EnvVar {
	if ref == nil || trimString(ref.Name) == "" || trimString(ref.Key) == "" {
		return env
	}
	return append(env, corev1.EnvVar{
		Name: name,
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: ref.Name},
				Key:                  ref.Key,
			},
		},
	})
}

func storageVolumeMounts(storage *dpv1alpha1.BackupStorage) []corev1.VolumeMount {
	if storage.Spec.Type != dpv1alpha1.StorageTypeNFS {
		return nil
	}
	return []corev1.VolumeMount{{Name: "backend-storage", MountPath: "/backend"}}
}

func defaultJobRuntime(runtime dpv1alpha1.JobRuntimeSpec) dpv1alpha1.JobRuntimeSpec {
	if runtime.ActiveDeadlineSeconds == nil {
		runtime.ActiveDeadlineSeconds = defaultJobActiveDeadlineSeconds()
	}
	if runtime.TTLSecondsAfterFinished == nil {
		runtime.TTLSecondsAfterFinished = defaultJobTTLSeconds()
	}
	return runtime
}

func mergeJobRuntime(runtime dpv1alpha1.JobRuntimeSpec, policy *dpv1alpha1.BackupPolicy) dpv1alpha1.JobRuntimeSpec {
	if policy == nil {
		return defaultJobRuntime(runtime)
	}
	merged := policy.Spec.JobRuntime
	if trimString(runtime.ServiceAccountName) != "" {
		merged.ServiceAccountName = runtime.ServiceAccountName
	}
	if runtime.ImagePullPolicy != "" {
		merged.ImagePullPolicy = runtime.ImagePullPolicy
	}
	if len(runtime.NodeSelector) > 0 {
		merged.NodeSelector = copyStringMap(runtime.NodeSelector)
	}
	if len(runtime.Tolerations) > 0 {
		merged.Tolerations = cloneTolerations(runtime.Tolerations)
	}
	if runtime.Resources.Requests != nil || runtime.Resources.Limits != nil {
		merged.Resources = runtime.Resources
	}
	if runtime.ActiveDeadlineSeconds != nil {
		merged.ActiveDeadlineSeconds = cloneInt64Ptr(runtime.ActiveDeadlineSeconds)
	}
	if runtime.TTLSecondsAfterFinished != nil {
		merged.TTLSecondsAfterFinished = cloneInt32Ptr(runtime.TTLSecondsAfterFinished)
	}
	if trimString(runtime.PriorityClassName) != "" {
		merged.PriorityClassName = runtime.PriorityClassName
	}
	return defaultJobRuntime(merged)
}

func mergeRestoreEndpoint(source *dpv1alpha1.BackupSource, job *dpv1alpha1.RestoreJob) dpv1alpha1.EndpointSpec {
	if job.Spec.Endpoint != nil {
		return *job.Spec.Endpoint
	}
	return source.Spec.Endpoint
}

func mergeRestoreSecretRefs(source *dpv1alpha1.BackupSource, job *dpv1alpha1.RestoreJob) []dpv1alpha1.SecretParameterReference {
	if len(job.Spec.SecretRefs) == 0 {
		return append([]dpv1alpha1.SecretParameterReference{}, source.Spec.SecretRefs...)
	}
	result := append([]dpv1alpha1.SecretParameterReference{}, source.Spec.SecretRefs...)
	index := map[string]int{}
	for i, ref := range result {
		index[ref.Name] = i
	}
	for _, ref := range job.Spec.SecretRefs {
		if existing, ok := index[ref.Name]; ok {
			result[existing] = ref
			continue
		}
		index[ref.Name] = len(result)
		result = append(result, ref)
	}
	return result
}

func mergeRestoreParameters(source *dpv1alpha1.BackupSource, job *dpv1alpha1.RestoreJob) map[string]string {
	result := map[string]string{}
	for key, value := range source.Spec.Parameters {
		result[key] = value
	}
	for key, value := range job.Spec.Parameters {
		result[key] = value
	}
	return result
}

func mergeRestoreTargetRef(source *dpv1alpha1.BackupSource, job *dpv1alpha1.RestoreJob) *dpv1alpha1.NamespacedObjectReference {
	if job.Spec.TargetRef != nil {
		target := *job.Spec.TargetRef
		return &target
	}
	if source.Spec.TargetRef != nil {
		target := *source.Spec.TargetRef
		return &target
	}
	return nil
}

func effectivePullPolicy(requested corev1.PullPolicy, image string) corev1.PullPolicy {
	if requested != "" {
		return requested
	}
	return defaultImagePullPolicy(image)
}

func copyStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func cloneTolerations(values []corev1.Toleration) []corev1.Toleration {
	if len(values) == 0 {
		return nil
	}
	cloned := make([]corev1.Toleration, len(values))
	copy(cloned, values)
	return cloned
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

func envKey(prefix, raw string) string {
	raw = strings.ToUpper(strings.TrimSpace(raw))
	if raw == "" {
		return strings.TrimSuffix(prefix, "_")
	}
	var builder strings.Builder
	builder.WriteString(prefix)
	lastUnderscore := strings.HasSuffix(prefix, "_")
	for _, r := range raw {
		switch {
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
			lastUnderscore = false
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				builder.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	result := strings.Trim(builder.String(), "_")
	if result == "" {
		return strings.TrimSuffix(prefix, "_")
	}
	return result
}

func resolveJobNotificationRefs(explicit []corev1.LocalObjectReference, policy *dpv1alpha1.BackupPolicy) []corev1.LocalObjectReference {
	if len(explicit) > 0 {
		return explicit
	}
	if policy == nil {
		return nil
	}
	return append([]corev1.LocalObjectReference{}, policy.Spec.NotificationRefs...)
}

func marshalTerminationSummary(summary podExecutionSummary) string {
	payload, err := json.Marshal(summary)
	if err != nil {
		return `{"message":"unable to encode termination summary"}`
	}
	return string(payload)
}
