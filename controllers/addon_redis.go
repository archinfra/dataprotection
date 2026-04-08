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
	defaultRedisPort     = 6379
	redisBackupMountPath = "/backup"
	redisExportMountPath = "/workspace/export"
	redisStatusMountPath = "/workspace/status"
	redisSnapshotSuffix  = ".tar.gz"
)

const redisBackupScript = `set -euo pipefail

STATUS_DIR="${STATUS_DIR:-/tmp/redis-backup-status}"
BACKUP_BASE_DIR="${BACKUP_BASE_DIR:-/backup}"
BACKUP_COMPONENT_PATH="${BACKUP_COMPONENT_PATH:?BACKUP_COMPONENT_PATH is required}"

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

trim() {
  local value="${1:-}"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  printf '%s' "${value}"
}

redis_cli() {
  local host="${1}"
  local port="${2}"
  shift 2
  local args=()
  if [[ "${REDIS_SCHEME:-redis}" == "rediss" || "${REDIS_SCHEME:-}" == "tls" ]]; then
    args+=(--tls)
  fi
  if [[ -n "${REDIS_USER:-}" ]]; then
    args+=(--user "${REDIS_USER}")
  fi
  if [[ -n "${REDIS_PASSWORD:-}" ]]; then
    REDISCLI_AUTH="${REDIS_PASSWORD}" redis-cli -h "${host}" -p "${port}" "${args[@]}" "$@"
    return
  fi
  redis-cli -h "${host}" -p "${port}" "${args[@]}" "$@"
}

wait_for_redis() {
  local retries=30
  local delay=5
  local attempt=1

  while (( attempt <= retries )); do
    if redis_cli "${REDIS_HOST}" "${REDIS_PORT}" PING >/dev/null 2>&1; then
      return 0
    fi
    echo "[WARN] redis is not ready yet, retry ${attempt}/${retries}"
    sleep "${delay}"
    ((attempt++))
  done

  echo "[ERROR] redis is not reachable"
  exit 1
}

ensure_snapshot_name() {
  local snapshot_name="${REDIS_BACKUP_SNAPSHOT:-}"
  if [[ -z "${snapshot_name}" ]]; then
    snapshot_name="$(date -u +%Y%m%dT%H%M%SZ)${REDIS_SNAPSHOT_SUFFIX}"
  elif [[ "${snapshot_name}" != *"${REDIS_SNAPSHOT_SUFFIX}" ]]; then
    snapshot_name="${snapshot_name}${REDIS_SNAPSHOT_SUFFIX}"
  fi
  printf '%s' "${snapshot_name}"
}

discover_cluster_masters() {
  redis_cli "${REDIS_HOST}" "${REDIS_PORT}" CLUSTER NODES \
    | awk '{
        addr=$2
        flags=$3
        if (flags ~ /master/ && flags !~ /fail/ && flags !~ /noaddr/) {
          split(addr, pair, "@")
          print pair[1]
        }
      }' \
    | sort -u
}

backup_mode() {
  if redis_cli "${REDIS_HOST}" "${REDIS_PORT}" CLUSTER INFO >/tmp/redis-cluster-info 2>/dev/null; then
    if grep -q '^cluster_state:' /tmp/redis-cluster-info; then
      printf '%s' "cluster"
      return
    fi
  fi
  printf '%s' "standalone"
}

prune_snapshots() {
  local snapshot_dir="$1"
  local retention="$2"
  local snapshots=()

  mapfile -t snapshots < <(find "${snapshot_dir}" -maxdepth 1 -type f -name "*${REDIS_SNAPSHOT_SUFFIX}" -printf "%f\n" | sort -r)
  if (( ${#snapshots[@]} <= retention )); then
    return 0
  fi

  for snapshot in "${snapshots[@]:retention}"; do
    rm -f "${snapshot_dir}/${snapshot}" \
          "${snapshot_dir}/${snapshot}.sha256" \
          "${snapshot_dir}/${snapshot%${REDIS_SNAPSHOT_SUFFIX}}.meta"
  done
}

warn_filter_limitations() {
  if [[ -n "$(trim "${REDIS_DATABASES:-}")" || -n "$(trim "${REDIS_KEY_PREFIXES:-}")" ]]; then
    echo "[WARN] built-in redis addon currently performs full-instance backups; databases/keyPrefix filters are ignored"
  fi
}

snapshot_name="$(ensure_snapshot_name)"
component_root="${BACKUP_BASE_DIR}/${BACKUP_COMPONENT_PATH}"
snapshot_dir="${component_root}/snapshots"
snapshot_file="${snapshot_dir}/${snapshot_name}"
tmp_snapshot_file="${snapshot_file}.tmp"
checksum_file="${snapshot_file}.sha256"
meta_file="${snapshot_dir}/${snapshot_name%${REDIS_SNAPSHOT_SUFFIX}}.meta"
bundle_dir="$(mktemp -d "${snapshot_dir}/bundle.XXXXXX")"
retention="${BACKUP_RETENTION:-5}"
[[ "${retention}" =~ ^[0-9]+$ ]] || retention=5

mkdir -p "${snapshot_dir}" "${STATUS_DIR}"
probe_file="${snapshot_dir}/.write-test-$$"
: > "${probe_file}" || {
  echo "[ERROR] backup path is not writable: ${snapshot_dir}"
  exit 1
}
rm -f "${probe_file}"

wait_for_redis
warn_filter_limitations

mode="$(backup_mode)"
case "${mode}" in
  cluster)
    mkdir -p "${bundle_dir}/nodes"
    mapfile -t master_nodes < <(discover_cluster_masters)
    (( ${#master_nodes[@]} > 0 )) || {
      echo "[ERROR] no redis cluster master nodes discovered"
      exit 1
    }
    for node in "${master_nodes[@]}"; do
      host="${node%:*}"
      port="${node##*:}"
      safe_name="${host//[^a-zA-Z0-9_.-]/_}-${port}"
      echo "[INFO] pulling RDB from redis cluster master ${host}:${port}"
      redis_cli "${host}" "${port}" --rdb "${bundle_dir}/nodes/${safe_name}.rdb" >/dev/null
    done
    node_summary="$(printf '%s\n' "${master_nodes[@]}" | paste -sd ',' -)"
    ;;
  *)
    redis_cli "${REDIS_HOST}" "${REDIS_PORT}" --rdb "${bundle_dir}/standalone.rdb" >/dev/null
    node_summary="${REDIS_HOST}:${REDIS_PORT}"
    ;;
esac

{
  echo "snapshot=${snapshot_name}"
  echo "component=redis"
  echo "source=${DP_SOURCE_NAME:-}"
  echo "storage=${DP_STORAGE_NAME:-}"
  echo "mode=${mode}"
  echo "nodes=${node_summary}"
  echo "created_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
} > "${bundle_dir}/manifest.env"

tar -C "${bundle_dir}" -czf "${tmp_snapshot_file}" .
mv "${tmp_snapshot_file}" "${snapshot_file}"
sha256sum "${snapshot_file}" > "${checksum_file}"
cp "${bundle_dir}/manifest.env" "${meta_file}"
rm -rf "${bundle_dir}"

echo "${snapshot_name}" > "${component_root}/latest.txt"
prune_snapshots "${snapshot_dir}" "${retention}"

mark_done
echo "[INFO] redis backup completed: ${snapshot_file}"`

type redisBuiltInAddon struct{}

func (redisBuiltInAddon) Name() string {
	return "redis"
}

func (redisBuiltInAddon) Supports(driver dpv1alpha1.BackupDriver, execution dpv1alpha1.ExecutionTemplateSpec) bool {
	return useBuiltInRedisRuntime(driver, execution)
}

func (redisBuiltInAddon) BuildBackupJob(request addonBackupJobRequest) (*batchv1.Job, error) {
	execution := defaultRedisExecutionTemplate(request.Execution)
	podSpec, err := buildBuiltInRedisBackupPodSpec(
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
		snapshotAnnotation:                           ensureRedisSnapshotFile(request.Snapshot),
		storagePathAnnotation:                        request.StoragePath,
		"dataprotection.archinfra.io/driver-runtime": "builtin-redis",
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
			Parallelism:             int32Ptr(1),
			Completions:             int32Ptr(1),
			PodReplacementPolicy:    podReplacementPolicyPtr(batchv1.Failed),
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

func (redisBuiltInAddon) BuildRestoreJob(request addonRestoreJobRequest) (*batchv1.Job, error) {
	return nil, newPermanentDependencyError("built-in redis addon currently supports backup only; use custom execution for restore")
}

func buildBuiltInRedisBackupPodSpec(
	execution dpv1alpha1.ExecutionTemplateSpec,
	source *dpv1alpha1.BackupSource,
	storage *dpv1alpha1.BackupStorage,
	storagePath string,
	driverConfig dpv1alpha1.DriverConfig,
	snapshot string,
	retention int32,
) (corev1.PodSpec, error) {
	connectionEnv, err := redisConnectionEnvVars(source.Spec.Endpoint, source.Spec.TargetRef, source.Namespace)
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
		{Name: "REDIS_SNAPSHOT_SUFFIX", Value: redisSnapshotSuffix},
	}
	if strings.TrimSpace(snapshot) != "" {
		envs = append(envs, corev1.EnvVar{Name: "REDIS_BACKUP_SNAPSHOT", Value: strings.TrimSuffix(ensureRedisSnapshotFile(snapshot), redisSnapshotSuffix)})
	}
	if driverConfig.Redis != nil {
		envs = append(envs,
			corev1.EnvVar{Name: "REDIS_DATABASES", Value: joinRedisDatabases(driverConfig.Redis.Databases)},
			corev1.EnvVar{Name: "REDIS_KEY_PREFIXES", Value: strings.Join(driverConfig.Redis.KeyPrefix, ",")},
		)
	}
	envs = append(envs, connectionEnv...)
	envs = mergeEnvVars(envs, execution.ExtraEnv)

	redisContainer := corev1.Container{
		Name:            "redis-backup",
		Image:           execution.RunnerImage,
		ImagePullPolicy: execution.ImagePullPolicy,
		Command:         []string{"/bin/sh", "-ceu"},
		Args:            []string{redisBackupScript},
		Env:             envs,
		Resources:       execution.Resources,
	}

	podSpec := corev1.PodSpec{
		RestartPolicy:      corev1.RestartPolicyNever,
		ServiceAccountName: execution.ServiceAccountName,
		NodeSelector:       copyStringMap(execution.NodeSelector),
		Tolerations:        cloneTolerations(execution.Tolerations),
		Containers:         []corev1.Container{redisContainer},
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
		podSpec.Containers[0].Env = append(podSpec.Containers[0].Env, corev1.EnvVar{Name: "BACKUP_BASE_DIR", Value: redisBackupMountPath})
		podSpec.Containers[0].VolumeMounts = []corev1.VolumeMount{{Name: "backup-storage", MountPath: redisBackupMountPath}}
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
					{Name: "BACKUP_BASE_DIR", Value: redisExportMountPath},
				}, s3Env...),
				VolumeMounts: []corev1.VolumeMount{
					{Name: "export-staging", MountPath: redisExportMountPath},
				},
			},
		}
		podSpec.Containers[0].Env = append(podSpec.Containers[0].Env,
			corev1.EnvVar{Name: "BACKUP_BASE_DIR", Value: redisExportMountPath},
			corev1.EnvVar{Name: "STATUS_DIR", Value: redisStatusMountPath},
		)
		podSpec.Containers[0].VolumeMounts = []corev1.VolumeMount{
			{Name: "export-staging", MountPath: redisExportMountPath},
			{Name: "status-dir", MountPath: redisStatusMountPath},
		}
		podSpec.Containers = append(podSpec.Containers, corev1.Container{
			Name:            "s3-upload",
			Image:           execution.HelperImage,
			ImagePullPolicy: execution.ImagePullPolicy,
			Command:         []string{"/bin/sh", "-ceu"},
			Args:            []string{mysqlS3UploadScript},
			Env: append([]corev1.EnvVar{
				{Name: "BACKUP_COMPONENT_PATH", Value: storagePath},
				{Name: "BACKUP_BASE_DIR", Value: redisExportMountPath},
				{Name: "STATUS_DIR", Value: redisStatusMountPath},
				{Name: "BACKUP_SNAPSHOT_KIND", Value: "file"},
			}, s3Env...),
			VolumeMounts: []corev1.VolumeMount{
				{Name: "export-staging", MountPath: redisExportMountPath},
				{Name: "status-dir", MountPath: redisStatusMountPath},
			},
		})
		podSpec.Volumes = []corev1.Volume{
			{Name: "export-staging", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
			{Name: "status-dir", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		}
	default:
		return corev1.PodSpec{}, newPermanentDependencyError("unsupported storage type %q for built-in redis backup runtime", storage.Spec.Type)
	}

	return podSpec, nil
}

func defaultRedisExecutionTemplate(spec dpv1alpha1.ExecutionTemplateSpec) dpv1alpha1.ExecutionTemplateSpec {
	spec.HelperImage = strings.TrimSpace(spec.HelperImage)
	spec.RunnerImage = strings.TrimSpace(spec.RunnerImage)
	if spec.RunnerImage == "" {
		spec.RunnerImage = defaultRedisRunnerImage()
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

func useBuiltInRedisRuntime(driver dpv1alpha1.BackupDriver, execution dpv1alpha1.ExecutionTemplateSpec) bool {
	if driver != dpv1alpha1.BackupDriverRedis {
		return false
	}
	return len(execution.Command) == 0 && len(execution.Args) == 0
}

func redisConnectionEnvVars(endpoint dpv1alpha1.EndpointSpec, targetRef *dpv1alpha1.NamespacedObjectReference, defaultNamespace string) ([]corev1.EnvVar, error) {
	host, err := resolveRedisHost(endpoint, targetRef, defaultNamespace)
	if err != nil {
		return nil, err
	}
	port := endpoint.Port
	if port == 0 {
		port = defaultRedisPort
	}

	envs := []corev1.EnvVar{
		{Name: "REDIS_HOST", Value: host},
		{Name: "REDIS_PORT", Value: fmt.Sprintf("%d", port)},
		{Name: "REDIS_SCHEME", Value: defaultRedisScheme(endpoint.Scheme)},
	}
	if endpoint.UsernameFrom != nil {
		envs = appendSecretEnvVar(envs, "REDIS_USER", endpoint.UsernameFrom)
	} else if username := strings.TrimSpace(endpoint.Username); username != "" {
		envs = append(envs, corev1.EnvVar{Name: "REDIS_USER", Value: username})
	}
	envs = appendSecretEnvVar(envs, "REDIS_PASSWORD", endpoint.PasswordFrom)
	return envs, nil
}

func resolveRedisHost(endpoint dpv1alpha1.EndpointSpec, targetRef *dpv1alpha1.NamespacedObjectReference, defaultNamespace string) (string, error) {
	if host := strings.TrimSpace(endpoint.Host); host != "" {
		return host, nil
	}
	if endpoint.ServiceRef != nil && strings.TrimSpace(endpoint.ServiceRef.Name) != "" {
		serviceNamespace := strings.TrimSpace(endpoint.ServiceRef.Namespace)
		if serviceNamespace == "" {
			serviceNamespace = defaultNamespace
		}
		return fmt.Sprintf("%s.%s.svc.cluster.local", endpoint.ServiceRef.Name, serviceNamespace), nil
	}
	if targetRef != nil && strings.TrimSpace(targetRef.Name) != "" {
		kind := strings.TrimSpace(strings.ToLower(targetRef.Kind))
		if kind == "" || kind == "service" {
			serviceNamespace := strings.TrimSpace(targetRef.Namespace)
			if serviceNamespace == "" {
				serviceNamespace = defaultNamespace
			}
			return fmt.Sprintf("%s.%s.svc.cluster.local", targetRef.Name, serviceNamespace), nil
		}
		return "", newPermanentDependencyError("built-in redis runtime only supports targetRef kind Service, got %q", targetRef.Kind)
	}
	return "", newPermanentDependencyError("built-in redis runtime requires endpoint.host/serviceRef or targetRef service")
}

func ensureRedisSnapshotFile(snapshot string) string {
	snapshot = strings.TrimSpace(snapshot)
	if snapshot == "" {
		return ""
	}
	if strings.HasSuffix(snapshot, redisSnapshotSuffix) {
		return snapshot
	}
	return snapshot + redisSnapshotSuffix
}

func defaultRedisScheme(scheme string) string {
	scheme = strings.TrimSpace(strings.ToLower(scheme))
	if scheme == "" {
		return "redis"
	}
	return scheme
}

func joinRedisDatabases(databases []int32) string {
	if len(databases) == 0 {
		return ""
	}
	values := make([]string, 0, len(databases))
	for _, database := range databases {
		values = append(values, fmt.Sprintf("%d", database))
	}
	return strings.Join(values, ",")
}
