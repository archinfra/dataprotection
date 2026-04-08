package controllers

import (
	"fmt"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	dpv1alpha1 "github.com/archinfra/dataprotection/api/v1alpha1"
)

const (
	defaultMinIOPort      = 9000
	minioBackupMountPath  = "/backup"
	minioExportMountPath  = "/workspace/export"
	minioRestoreMountPath = "/workspace/restore"
	minioStatusMountPath  = "/workspace/status"
)

const minioBackupScript = `set -euo pipefail

STATUS_DIR="${STATUS_DIR:-/tmp/minio-backup-status}"
BACKUP_BASE_DIR="${BACKUP_BASE_DIR:-/backup}"
BACKUP_COMPONENT_PATH="${BACKUP_COMPONENT_PATH:?BACKUP_COMPONENT_PATH is required}"
MINIO_ENDPOINT_URL="${MINIO_ENDPOINT_URL:?MINIO_ENDPOINT_URL is required}"

mark_failed() {
  mkdir -p "${STATUS_DIR}"
  echo "failed" > "${STATUS_DIR}/status"
  touch "${STATUS_DIR}/failed"
}

mark_done() {
  mkdir -p "${STATUS_DIR}"
  echo "done" > "${STATUS_DIR}/status"
  touch "${STATUS_DIR}/done"
}

trap mark_failed ERR

mc_cmd() {
  mc "$@"
}

trim() {
  local value="${1:-}"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  printf '%s' "${value}"
}

normalize_csv_list() {
  local raw="${1:-}"
  local normalized=()
  local item

  raw="${raw//|/,}"
  IFS=',' read -r -a items <<< "${raw}"
  for item in "${items[@]}"; do
    item="$(trim "${item}")"
    [[ -n "${item}" ]] || continue
    normalized+=("${item}")
  done

  (IFS=,; printf '%s' "${normalized[*]}")
}

discover_buckets() {
  mc_cmd ls source | awk '{print $NF}' | sed 's#/$##'
}

prune_snapshots() {
  local snapshot_root="$1"
  local retention="$2"
  local snapshots=()

  mapfile -t snapshots < <(find "${snapshot_root}" -mindepth 1 -maxdepth 1 -type d -printf "%f\n" | sort -r)
  if (( ${#snapshots[@]} <= retention )); then
    return 0
  fi

  for snapshot in "${snapshots[@]:retention}"; do
    rm -rf "${snapshot_root:?}/${snapshot}"
    rm -f "${snapshot_root}/${snapshot}.meta"
  done
}

snapshot_name="$(trim "${MINIO_BACKUP_SNAPSHOT:-}")"
if [[ -z "${snapshot_name}" ]]; then
  snapshot_name="$(date -u +%Y%m%dT%H%M%SZ)"
fi

if [[ "${MINIO_INCLUDE_VERSIONS:-false}" == "true" ]]; then
  echo "[ERROR] built-in minio addon does not support includeVersions=true yet"
  exit 1
fi

component_root="${BACKUP_BASE_DIR}/${BACKUP_COMPONENT_PATH}"
snapshot_root="${component_root}/snapshots"
snapshot_dir="${snapshot_root}/${snapshot_name}"
meta_file="${snapshot_root}/${snapshot_name}.meta"
retention="${BACKUP_RETENTION:-5}"
[[ "${retention}" =~ ^[0-9]+$ ]] || retention=5

mkdir -p "${snapshot_root}" "${STATUS_DIR}"
probe_file="${snapshot_root}/.write-test-$$"
: > "${probe_file}" || {
  echo "[ERROR] backup path is not writable: ${snapshot_root}"
  exit 1
}
rm -f "${probe_file}"

mc_cmd alias set source "${MINIO_ENDPOINT_URL}" "${MINIO_ACCESS_KEY}" "${MINIO_SECRET_KEY}" --api S3v4 >/dev/null

bucket_csv="$(normalize_csv_list "${MINIO_BUCKETS:-}")"
prefix_csv="$(normalize_csv_list "${MINIO_PREFIXES:-}")"
if [[ -z "${bucket_csv}" ]]; then
  mapfile -t buckets < <(discover_buckets)
else
  IFS=',' read -r -a buckets <<< "${bucket_csv}"
fi
(( ${#buckets[@]} > 0 )) || {
  echo "[ERROR] no buckets discovered from minio source"
  exit 1
}

for bucket in "${buckets[@]}"; do
  bucket="$(trim "${bucket}")"
  [[ -n "${bucket}" ]] || continue
  mc_cmd ls "source/${bucket}" >/dev/null 2>&1 || {
    echo "[ERROR] source bucket not found: ${bucket}"
    exit 1
  }

  if [[ -z "${prefix_csv}" ]]; then
    mkdir -p "${snapshot_dir}/${bucket}"
    echo "[INFO] mirroring source bucket ${bucket}"
    mc_cmd mirror "source/${bucket}" "${snapshot_dir}/${bucket}"
    continue
  fi

  IFS=',' read -r -a prefixes <<< "${prefix_csv}"
  for prefix in "${prefixes[@]}"; do
    prefix="$(trim "${prefix}")"
    prefix="${prefix#/}"
    prefix="${prefix%/}"
    [[ -n "${prefix}" ]] || continue
    mkdir -p "${snapshot_dir}/${bucket}/${prefix}"
    echo "[INFO] mirroring source bucket ${bucket} prefix ${prefix}"
    mc_cmd mirror "source/${bucket}/${prefix}" "${snapshot_dir}/${bucket}/${prefix}"
  done
done

{
  echo "snapshot=${snapshot_name}"
  echo "component=minio"
  echo "source=${DP_SOURCE_NAME:-}"
  echo "storage=${DP_STORAGE_NAME:-}"
  echo "buckets=$(IFS=,; printf '%s' "${buckets[*]}")"
  echo "prefixes=${prefix_csv:-all}"
  echo "created_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
} > "${meta_file}"

echo "${snapshot_name}" > "${component_root}/latest.txt"
prune_snapshots "${snapshot_root}" "${retention}"

mark_done
echo "[INFO] minio backup completed: ${snapshot_dir}"`

const minioRestoreScript = `set -euo pipefail

BACKUP_BASE_DIR="${BACKUP_BASE_DIR:-/backup}"
BACKUP_COMPONENT_PATH="${BACKUP_COMPONENT_PATH:?BACKUP_COMPONENT_PATH is required}"
MINIO_ENDPOINT_URL="${MINIO_ENDPOINT_URL:?MINIO_ENDPOINT_URL is required}"

mc_cmd() {
  mc "$@"
}

resolve_snapshot_dir() {
  local component_root="$1"
  local snapshot_root="$2"
  local snapshot_name="${MINIO_RESTORE_SNAPSHOT:-latest}"

  if [[ "${snapshot_name}" == "latest" ]]; then
    if [[ -f "${component_root}/latest.txt" ]]; then
      snapshot_name="$(cat "${component_root}/latest.txt")"
    else
      snapshot_name="$(find "${snapshot_root}" -mindepth 1 -maxdepth 1 -type d -printf "%f\n" | sort -r | head -n 1)"
    fi
  fi

  [[ -n "${snapshot_name}" ]] || {
    echo "[ERROR] no minio snapshot found"
    exit 1
  }

  echo "${snapshot_root}/${snapshot_name}"
}

component_root="${BACKUP_BASE_DIR}/${BACKUP_COMPONENT_PATH}"
snapshot_root="${component_root}/snapshots"
snapshot_dir="$(resolve_snapshot_dir "${component_root}" "${snapshot_root}")"

[[ -d "${snapshot_dir}" ]] || {
  echo "[ERROR] minio snapshot directory not found: ${snapshot_dir}"
  exit 1
}

mc_cmd alias set source "${MINIO_ENDPOINT_URL}" "${MINIO_ACCESS_KEY}" "${MINIO_SECRET_KEY}" --api S3v4 >/dev/null

mapfile -t buckets < <(find "${snapshot_dir}" -mindepth 1 -maxdepth 1 -type d -printf "%f\n" | sort)
(( ${#buckets[@]} > 0 )) || {
  echo "[ERROR] no bucket data found under ${snapshot_dir}"
  exit 1
}

for bucket in "${buckets[@]}"; do
  if ! mc_cmd ls "source/${bucket}" >/dev/null 2>&1; then
    echo "[INFO] creating source bucket ${bucket}"
    mc_cmd mb "source/${bucket}" >/dev/null
  fi
  echo "[INFO] restoring minio bucket ${bucket}"
  mc_cmd mirror --overwrite "${snapshot_dir}/${bucket}" "source/${bucket}"
done

echo "[INFO] minio restore completed from ${snapshot_dir}"`

type minioBuiltInAddon struct{}

func (minioBuiltInAddon) Name() string {
	return "minio"
}

func (minioBuiltInAddon) Supports(driver dpv1alpha1.BackupDriver, execution dpv1alpha1.ExecutionTemplateSpec) bool {
	return useBuiltInMinIORuntime(driver, execution)
}

func (minioBuiltInAddon) BuildBackupJob(request addonBackupJobRequest) (*batchv1.Job, error) {
	execution := defaultMinIOExecutionTemplate(request.Execution)
	podSpec, err := buildBuiltInMinIOBackupPodSpec(
		execution,
		request.Source,
		request.Storage,
		request.StoragePath,
		request.DriverConfig,
		request.Snapshot,
		request.KeepLast,
	)
	if err != nil {
		return nil, err
	}

	name := dpv1alpha1.BuildJobName(request.Run.Name, request.Storage.Name)
	labels := managedResourceLabels("BackupRun", request.Run.Name, "manual-backup", request.Source.Name, request.Storage.Name)
	annotations := map[string]string{
		snapshotAnnotation:                           ensureMinIOSnapshotName(request.Snapshot),
		storagePathAnnotation:                        request.StoragePath,
		"dataprotection.archinfra.io/driver-runtime": "builtin-minio",
	}
	if reason := strings.TrimSpace(request.Run.Spec.Reason); reason != "" {
		annotations[reasonAnnotation] = reason
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   request.Run.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            cloneInt32Ptr(execution.BackoffLimit),
			TTLSecondsAfterFinished: cloneInt32Ptr(execution.TTLSecondsAfterFinished),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      copyStringMap(labels),
					Annotations: copyStringMap(annotations),
				},
				Spec: podSpec,
			},
		},
	}, nil
}

func (minioBuiltInAddon) BuildRestoreJob(request addonRestoreJobRequest) (*batchv1.Job, error) {
	execution := defaultMinIOExecutionTemplate(request.Execution)
	podSpec, err := buildBuiltInMinIORestorePodSpec(
		execution,
		request.Restore,
		request.Source,
		request.Storage,
		request.StoragePath,
		request.DriverConfig,
		request.Snapshot,
	)
	if err != nil {
		return nil, err
	}

	name := dpv1alpha1.BuildJobName(request.Restore.Name, "restore")
	labels := managedResourceLabels("RestoreRequest", request.Restore.Name, "restore", request.Source.Name, request.Storage.Name)
	annotations := map[string]string{
		snapshotAnnotation:                           ensureMinIOSnapshotName(request.Snapshot),
		targetModeAnnotation:                         string(effectiveRestoreTargetMode(request.Restore.Spec.Target.Mode)),
		storagePathAnnotation:                        request.StoragePath,
		"dataprotection.archinfra.io/driver-runtime": "builtin-minio",
	}
	if reason := strings.TrimSpace(request.Restore.Spec.Reason); reason != "" {
		annotations[reasonAnnotation] = reason
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   request.Restore.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            cloneInt32Ptr(execution.BackoffLimit),
			TTLSecondsAfterFinished: cloneInt32Ptr(execution.TTLSecondsAfterFinished),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      copyStringMap(labels),
					Annotations: copyStringMap(annotations),
				},
				Spec: podSpec,
			},
		},
	}, nil
}

func buildBuiltInMinIOBackupPodSpec(
	execution dpv1alpha1.ExecutionTemplateSpec,
	source *dpv1alpha1.BackupSource,
	storage *dpv1alpha1.BackupStorage,
	storagePath string,
	driverConfig dpv1alpha1.DriverConfig,
	snapshot string,
	retention int32,
) (corev1.PodSpec, error) {
	connectionEnv, err := minioSourceEnvVars(source.Spec.Endpoint, source.Spec.TargetRef, source.Namespace)
	if err != nil {
		return corev1.PodSpec{}, err
	}
	if retention <= 0 {
		retention = defaultBackupRetention
	}

	envs := []corev1.EnvVar{
		{Name: "DP_OPERATION", Value: "backup"},
		{Name: "DP_SOURCE_NAME", Value: source.Name},
		{Name: "DP_SOURCE_DRIVER", Value: string(source.Spec.Driver)},
		{Name: "DP_STORAGE_NAME", Value: storage.Name},
		{Name: "DP_STORAGE_TYPE", Value: string(storage.Spec.Type)},
		{Name: "DP_STORAGE_PATH", Value: storagePath},
		{Name: "BACKUP_COMPONENT_PATH", Value: storagePath},
		{Name: "BACKUP_RETENTION", Value: fmt.Sprintf("%d", retention)},
	}
	if strings.TrimSpace(snapshot) != "" {
		envs = append(envs, corev1.EnvVar{Name: "MINIO_BACKUP_SNAPSHOT", Value: ensureMinIOSnapshotName(snapshot)})
	}
	if driverConfig.MinIO != nil {
		envs = append(envs,
			corev1.EnvVar{Name: "MINIO_BUCKETS", Value: strings.Join(driverConfig.MinIO.Buckets, ",")},
			corev1.EnvVar{Name: "MINIO_PREFIXES", Value: strings.Join(driverConfig.MinIO.Prefixes, ",")},
			corev1.EnvVar{Name: "MINIO_INCLUDE_VERSIONS", Value: boolString(driverConfig.MinIO.IncludeVersions)},
		)
	}
	envs = append(envs, connectionEnv...)
	envs = mergeEnvVars(envs, execution.ExtraEnv)

	minioContainer := corev1.Container{
		Name:            "minio-backup",
		Image:           execution.RunnerImage,
		ImagePullPolicy: execution.ImagePullPolicy,
		Command:         []string{"/bin/sh", "-ceu"},
		Args:            []string{minioBackupScript},
		Env:             envs,
		Resources:       execution.Resources,
	}

	podSpec := corev1.PodSpec{
		RestartPolicy:      corev1.RestartPolicyNever,
		ServiceAccountName: execution.ServiceAccountName,
		NodeSelector:       copyStringMap(execution.NodeSelector),
		Tolerations:        cloneTolerations(execution.Tolerations),
		Containers:         []corev1.Container{minioContainer},
	}

	switch storage.Spec.Type {
	case dpv1alpha1.StorageTypeNFS:
		podSpec.Volumes = []corev1.Volume{
			{
				Name: "backup-storage",
				VolumeSource: corev1.VolumeSource{
					NFS: &corev1.NFSVolumeSource{
						Server: storage.Spec.NFS.Server,
						Path:   storage.Spec.NFS.Path,
					},
				},
			},
		}
		podSpec.Containers[0].Env = append(podSpec.Containers[0].Env, corev1.EnvVar{Name: "BACKUP_BASE_DIR", Value: minioBackupMountPath})
		podSpec.Containers[0].VolumeMounts = []corev1.VolumeMount{{Name: "backup-storage", MountPath: minioBackupMountPath}}
	case dpv1alpha1.StorageTypeS3:
		s3Env, s3Err := s3StorageEnvVars(storage)
		if s3Err != nil {
			return corev1.PodSpec{}, s3Err
		}
		podSpec.InitContainers = []corev1.Container{
			{
				Name:            "s3-prefetch",
				Image:           execution.HelperImage,
				ImagePullPolicy: execution.ImagePullPolicy,
				Command:         []string{"/bin/sh", "-ceu"},
				Args:            []string{mysqlS3PrefetchScript},
				Env: append([]corev1.EnvVar{
					{Name: "BACKUP_COMPONENT_PATH", Value: storagePath},
					{Name: "BACKUP_BASE_DIR", Value: minioExportMountPath},
				}, s3Env...),
				VolumeMounts: []corev1.VolumeMount{
					{Name: "export-staging", MountPath: minioExportMountPath},
				},
			},
		}
		podSpec.Containers[0].Env = append(podSpec.Containers[0].Env,
			corev1.EnvVar{Name: "BACKUP_BASE_DIR", Value: minioExportMountPath},
			corev1.EnvVar{Name: "STATUS_DIR", Value: minioStatusMountPath},
		)
		podSpec.Containers[0].VolumeMounts = []corev1.VolumeMount{
			{Name: "export-staging", MountPath: minioExportMountPath},
			{Name: "status-dir", MountPath: minioStatusMountPath},
		}
		podSpec.Containers = append(podSpec.Containers, corev1.Container{
			Name:            "s3-upload",
			Image:           execution.HelperImage,
			ImagePullPolicy: execution.ImagePullPolicy,
			Command:         []string{"/bin/sh", "-ceu"},
			Args:            []string{mysqlS3UploadScript},
			Env: append([]corev1.EnvVar{
				{Name: "BACKUP_COMPONENT_PATH", Value: storagePath},
				{Name: "BACKUP_BASE_DIR", Value: minioExportMountPath},
				{Name: "STATUS_DIR", Value: minioStatusMountPath},
				{Name: "BACKUP_SNAPSHOT_KIND", Value: "directory"},
			}, s3Env...),
			VolumeMounts: []corev1.VolumeMount{
				{Name: "export-staging", MountPath: minioExportMountPath},
				{Name: "status-dir", MountPath: minioStatusMountPath},
			},
		})
		podSpec.Volumes = []corev1.Volume{
			{Name: "export-staging", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
			{Name: "status-dir", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		}
	default:
		return corev1.PodSpec{}, newPermanentDependencyError("unsupported storage type %q for built-in minio backup runtime", storage.Spec.Type)
	}

	return podSpec, nil
}

func buildBuiltInMinIORestorePodSpec(
	execution dpv1alpha1.ExecutionTemplateSpec,
	restore *dpv1alpha1.RestoreRequest,
	source *dpv1alpha1.BackupSource,
	storage *dpv1alpha1.BackupStorage,
	storagePath string,
	driverConfig dpv1alpha1.DriverConfig,
	snapshot string,
) (corev1.PodSpec, error) {
	connectionEndpoint, connectionTargetRef := effectiveRestoreEndpoint(restore, source)
	connectionEnv, err := minioSourceEnvVars(connectionEndpoint, connectionTargetRef, restore.Namespace)
	if err != nil {
		return corev1.PodSpec{}, err
	}

	envs := []corev1.EnvVar{
		{Name: "DP_OPERATION", Value: "restore"},
		{Name: "DP_SOURCE_NAME", Value: source.Name},
		{Name: "DP_SOURCE_DRIVER", Value: string(source.Spec.Driver)},
		{Name: "DP_STORAGE_NAME", Value: storage.Name},
		{Name: "DP_STORAGE_TYPE", Value: string(storage.Spec.Type)},
		{Name: "DP_STORAGE_PATH", Value: storagePath},
		{Name: "DP_RESTORE_REQUEST_NAME", Value: restore.Name},
		{Name: "DP_RESTORE_TARGET_MODE", Value: string(effectiveRestoreTargetMode(restore.Spec.Target.Mode))},
		{Name: "BACKUP_COMPONENT_PATH", Value: storagePath},
		{Name: "MINIO_RESTORE_SNAPSHOT", Value: ensureMinIOSnapshotName(snapshot)},
	}
	if driverConfig.MinIO != nil {
		envs = append(envs,
			corev1.EnvVar{Name: "MINIO_BUCKETS", Value: strings.Join(driverConfig.MinIO.Buckets, ",")},
			corev1.EnvVar{Name: "MINIO_PREFIXES", Value: strings.Join(driverConfig.MinIO.Prefixes, ",")},
			corev1.EnvVar{Name: "MINIO_INCLUDE_VERSIONS", Value: boolString(driverConfig.MinIO.IncludeVersions)},
		)
	}
	envs = append(envs, connectionEnv...)
	envs = mergeEnvVars(envs, execution.ExtraEnv)

	minioContainer := corev1.Container{
		Name:            "minio-restore",
		Image:           execution.RunnerImage,
		ImagePullPolicy: execution.ImagePullPolicy,
		Command:         []string{"/bin/sh", "-ceu"},
		Args:            []string{minioRestoreScript},
		Env:             envs,
		Resources:       execution.Resources,
	}

	podSpec := corev1.PodSpec{
		RestartPolicy:      corev1.RestartPolicyNever,
		ServiceAccountName: execution.ServiceAccountName,
		NodeSelector:       copyStringMap(execution.NodeSelector),
		Tolerations:        cloneTolerations(execution.Tolerations),
		Containers:         []corev1.Container{minioContainer},
	}

	switch storage.Spec.Type {
	case dpv1alpha1.StorageTypeNFS:
		podSpec.Volumes = []corev1.Volume{
			{
				Name: "backup-storage",
				VolumeSource: corev1.VolumeSource{
					NFS: &corev1.NFSVolumeSource{
						Server: storage.Spec.NFS.Server,
						Path:   storage.Spec.NFS.Path,
					},
				},
			},
		}
		podSpec.Containers[0].Env = append(podSpec.Containers[0].Env, corev1.EnvVar{Name: "BACKUP_BASE_DIR", Value: minioBackupMountPath})
		podSpec.Containers[0].VolumeMounts = []corev1.VolumeMount{{Name: "backup-storage", MountPath: minioBackupMountPath}}
	case dpv1alpha1.StorageTypeS3:
		s3Env, s3Err := s3StorageEnvVars(storage)
		if s3Err != nil {
			return corev1.PodSpec{}, s3Err
		}
		podSpec.InitContainers = []corev1.Container{
			{
				Name:            "s3-download",
				Image:           execution.HelperImage,
				ImagePullPolicy: execution.ImagePullPolicy,
				Command:         []string{"/bin/sh", "-ceu"},
				Args:            []string{mysqlS3DownloadScript},
				Env: append([]corev1.EnvVar{
					{Name: "BACKUP_COMPONENT_PATH", Value: storagePath},
					{Name: "BACKUP_BASE_DIR", Value: minioRestoreMountPath},
				}, s3Env...),
				VolumeMounts: []corev1.VolumeMount{
					{Name: "restore-staging", MountPath: minioRestoreMountPath},
				},
			},
		}
		podSpec.Containers[0].Env = append(podSpec.Containers[0].Env, corev1.EnvVar{Name: "BACKUP_BASE_DIR", Value: minioRestoreMountPath})
		podSpec.Containers[0].VolumeMounts = []corev1.VolumeMount{
			{Name: "restore-staging", MountPath: minioRestoreMountPath},
		}
		podSpec.Volumes = []corev1.Volume{
			{Name: "restore-staging", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		}
	default:
		return corev1.PodSpec{}, newPermanentDependencyError("unsupported storage type %q for built-in minio restore runtime", storage.Spec.Type)
	}

	return podSpec, nil
}

func defaultMinIOExecutionTemplate(spec dpv1alpha1.ExecutionTemplateSpec) dpv1alpha1.ExecutionTemplateSpec {
	spec.HelperImage = strings.TrimSpace(spec.HelperImage)
	spec.RunnerImage = strings.TrimSpace(spec.RunnerImage)
	if spec.RunnerImage == "" {
		spec.RunnerImage = defaultMinIORunnerImage()
	}
	if spec.HelperImage == "" {
		spec.HelperImage = defaultS3HelperImage()
	}
	if spec.ImagePullPolicy == "" {
		spec.ImagePullPolicy = defaultImagePullPolicy(spec.RunnerImage, spec.HelperImage)
	}
	if spec.BackoffLimit == nil {
		spec.BackoffLimit = int32Ptr(1)
	}
	if spec.TTLSecondsAfterFinished == nil {
		spec.TTLSecondsAfterFinished = defaultJobTTLSeconds()
	}
	return spec
}

func useBuiltInMinIORuntime(driver dpv1alpha1.BackupDriver, execution dpv1alpha1.ExecutionTemplateSpec) bool {
	if driver != dpv1alpha1.BackupDriverMinIO {
		return false
	}
	return len(execution.Command) == 0 && len(execution.Args) == 0
}

func minioSourceEnvVars(endpoint dpv1alpha1.EndpointSpec, targetRef *dpv1alpha1.NamespacedObjectReference, defaultNamespace string) ([]corev1.EnvVar, error) {
	endpointURL, err := resolveMinIOEndpointURL(endpoint, targetRef, defaultNamespace)
	if err != nil {
		return nil, err
	}
	if endpoint.PasswordFrom == nil || strings.TrimSpace(endpoint.PasswordFrom.Name) == "" || strings.TrimSpace(endpoint.PasswordFrom.Key) == "" {
		return nil, newPermanentDependencyError("built-in minio runtime requires endpoint.passwordFrom")
	}

	envs := []corev1.EnvVar{
		{Name: "MINIO_ENDPOINT_URL", Value: endpointURL},
	}
	if endpoint.UsernameFrom != nil {
		envs = appendSecretEnvVar(envs, "MINIO_ACCESS_KEY", endpoint.UsernameFrom)
	} else if username := strings.TrimSpace(endpoint.Username); username != "" {
		envs = append(envs, corev1.EnvVar{Name: "MINIO_ACCESS_KEY", Value: username})
	} else {
		return nil, newPermanentDependencyError("built-in minio runtime requires endpoint.username or endpoint.usernameFrom")
	}
	envs = appendSecretEnvVar(envs, "MINIO_SECRET_KEY", endpoint.PasswordFrom)
	return envs, nil
}

func resolveMinIOEndpointURL(endpoint dpv1alpha1.EndpointSpec, targetRef *dpv1alpha1.NamespacedObjectReference, defaultNamespace string) (string, error) {
	if host := strings.TrimSpace(endpoint.Host); host != "" {
		if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
			return host, nil
		}
		port := endpoint.Port
		if port == 0 {
			port = defaultMinIOPort
		}
		return fmt.Sprintf("%s://%s:%d", defaultMinIOScheme(endpoint.Scheme), host, port), nil
	}
	if endpoint.ServiceRef != nil && strings.TrimSpace(endpoint.ServiceRef.Name) != "" {
		serviceNamespace := strings.TrimSpace(endpoint.ServiceRef.Namespace)
		if serviceNamespace == "" {
			serviceNamespace = defaultNamespace
		}
		port := endpoint.ServiceRef.Port
		if port == 0 {
			port = defaultMinIOPort
		}
		return fmt.Sprintf("%s://%s.%s.svc.cluster.local:%d", defaultMinIOScheme(endpoint.Scheme), endpoint.ServiceRef.Name, serviceNamespace, port), nil
	}
	if targetRef != nil && strings.TrimSpace(targetRef.Name) != "" {
		kind := strings.TrimSpace(strings.ToLower(targetRef.Kind))
		if kind == "" || kind == "service" {
			serviceNamespace := strings.TrimSpace(targetRef.Namespace)
			if serviceNamespace == "" {
				serviceNamespace = defaultNamespace
			}
			return fmt.Sprintf("%s://%s.%s.svc.cluster.local:%d", defaultMinIOScheme(endpoint.Scheme), targetRef.Name, serviceNamespace, defaultMinIOPort), nil
		}
		return "", newPermanentDependencyError("built-in minio runtime only supports targetRef kind Service, got %q", targetRef.Kind)
	}
	return "", newPermanentDependencyError("built-in minio runtime requires endpoint.host/serviceRef or targetRef service")
}

func ensureMinIOSnapshotName(snapshot string) string {
	return strings.TrimSpace(snapshot)
}

func defaultMinIOScheme(scheme string) string {
	scheme = strings.TrimSpace(strings.ToLower(scheme))
	if scheme == "" {
		return "http"
	}
	return scheme
}
