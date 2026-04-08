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
	defaultBackupRootDir   = "backups"
	defaultMySQLPort       = 3306
	defaultBackupRetention = 5
	mysqlBackupMountPath   = "/backup"
	mysqlExportMountPath   = "/workspace/export"
	mysqlRestoreMountPath  = "/workspace/restore"
	mysqlStatusMountPath   = "/workspace/status"
)

const mysqlBackupScript = `set -euo pipefail

STATUS_DIR="${STATUS_DIR:-/tmp/mysql-backup-status}"
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

wait_for_mysql() {
  local retries=30
  local delay=5
  local attempt=1

  while (( attempt <= retries )); do
    if mysqladmin \
      --host="${MYSQL_HOST}" \
      --port="${MYSQL_PORT}" \
      --protocol=TCP \
      --user="${MYSQL_USER}" \
      ping >/dev/null 2>&1; then
      return 0
    fi
    echo "[WARN] mysql is not ready yet, retry ${attempt}/${retries}"
    sleep "${delay}"
    ((attempt++))
  done

  echo "[ERROR] mysql is not reachable"
  exit 1
}

discover_user_databases() {
  mysql \
    --host="${MYSQL_HOST}" \
    --port="${MYSQL_PORT}" \
    --protocol=TCP \
    --user="${MYSQL_USER}" \
    -Nse "SHOW DATABASES" \
    | grep -Ev '^(information_schema|performance_schema|mysql|sys)$' || true
}

normalize_csv_list() {
  local raw="${1:-}"
  local normalized=()
  local item

  raw="${raw//|/,}"
  IFS=',' read -r -a items <<< "${raw}"
  for item in "${items[@]}"; do
    item="${item#"${item%%[![:space:]]*}"}"
    item="${item%"${item##*[![:space:]]}"}"
    [[ -n "${item}" ]] || continue
    normalized+=("${item}")
  done

  (IFS=,; printf '%s' "${normalized[*]}")
}

mysqldump_base_args() {
  printf '%s\n' \
    "--host=${MYSQL_HOST}" \
    "--port=${MYSQL_PORT}" \
    "--protocol=TCP" \
    "--user=${MYSQL_USER}" \
    "--single-transaction" \
    "--quick" \
    "--routines" \
    "--events" \
    "--triggers" \
    "--hex-blob" \
    "--set-gtid-purged=OFF" \
    "--add-drop-database"
}

emit_selected_table_dump() {
  local requested_tables="${1:-}"
  local table_entry database_name table_name db
  local db_order=()
  local dump_args=()
  declare -A grouped_tables=()

  mapfile -t dump_args < <(mysqldump_base_args)

  IFS=',' read -r -a table_entries <<< "${requested_tables}"
  for table_entry in "${table_entries[@]}"; do
    [[ -n "${table_entry}" ]] || continue
    database_name="${table_entry%%.*}"
    table_name="${table_entry#*.}"
    if [[ -z "${database_name}" || -z "${table_name}" || "${database_name}" == "${table_entry}" ]]; then
      echo "[ERROR] invalid table selector: ${table_entry}" >&2
      return 1
    fi
    if [[ -z "${grouped_tables[${database_name}]:-}" ]]; then
      db_order+=("${database_name}")
    fi
    grouped_tables["${database_name}"]+="${table_name} "
  done

  for db in "${db_order[@]}"; do
    read -r -a table_names <<< "${grouped_tables[${db}]}"
    printf 'CREATE DATABASE IF NOT EXISTS %s;\nUSE %s;\n' "${db}" "${db}"
    mysqldump "${dump_args[@]}" "${db}" "${table_names[@]}"
  done
}

prune_snapshots() {
  local snapshot_dir="$1"
  local retention="$2"
  local snapshots=()

  mapfile -t snapshots < <(find "${snapshot_dir}" -maxdepth 1 -type f -name "*.sql.gz" -printf "%f\n" | sort -r)
  if (( ${#snapshots[@]} <= retention )); then
    return 0
  fi

  for snapshot in "${snapshots[@]:retention}"; do
    rm -f "${snapshot_dir}/${snapshot}" \
          "${snapshot_dir}/${snapshot}.sha256" \
          "${snapshot_dir}/${snapshot%.sql.gz}.meta"
  done
}

export MYSQL_PWD="${MYSQL_PASSWORD:-}"
dump_args=()
snapshot_name="${MYSQL_BACKUP_SNAPSHOT:-}"
if [[ -z "${snapshot_name}" ]]; then
  snapshot_name="$(date -u +%Y%m%dT%H%M%SZ).sql.gz"
elif [[ "${snapshot_name}" != *.sql.gz ]]; then
  snapshot_name="${snapshot_name}.sql.gz"
fi

component_root="${BACKUP_BASE_DIR}/${BACKUP_COMPONENT_PATH}"
snapshot_dir="${component_root}/snapshots"
snapshot_file="${snapshot_dir}/${snapshot_name}"
tmp_file="${snapshot_file}.tmp"
checksum_file="${snapshot_file}.sha256"
meta_file="${snapshot_dir}/${snapshot_name%.sql.gz}.meta"
BACKUP_DATABASES="$(normalize_csv_list "${BACKUP_DATABASES:-}")"
BACKUP_TABLES="$(normalize_csv_list "${BACKUP_TABLES:-}")"
scope_type="all"
meta_databases="none"
meta_tables="none"
retention="${BACKUP_RETENTION:-5}"
[[ "${retention}" =~ ^[0-9]+$ ]] || retention=5

mkdir -p "${snapshot_dir}" "${STATUS_DIR}"
probe_file="${snapshot_dir}/.write-test-$$"
: > "${probe_file}" || {
  echo "[ERROR] backup path is not writable: ${snapshot_dir}"
  exit 1
}
rm -f "${probe_file}"
wait_for_mysql
mapfile -t dump_args < <(mysqldump_base_args)

if [[ -n "${BACKUP_TABLES}" ]]; then
  scope_type="tables"
  meta_tables="${BACKUP_TABLES}"
  emit_selected_table_dump "${BACKUP_TABLES}" | gzip -c > "${tmp_file}"
else
  if [[ -n "${BACKUP_DATABASES}" ]]; then
    IFS=',' read -r -a databases <<< "${BACKUP_DATABASES}"
    scope_type="databases"
  else
    mapfile -t databases < <(discover_user_databases)
  fi

  if (( ${#databases[@]} == 0 )); then
    printf -- "-- mysql backup captured no user databases at %s\n" "${snapshot_name}" | gzip -c > "${tmp_file}"
  else
    meta_databases="$(IFS=,; printf '%s' "${databases[*]}")"
    mysqldump "${dump_args[@]}" --databases "${databases[@]}" | gzip -c > "${tmp_file}"
  fi
fi

mv "${tmp_file}" "${snapshot_file}"
sha256sum "${snapshot_file}" > "${checksum_file}"

{
  echo "snapshot=$(basename "${snapshot_file}")"
  echo "component=mysql"
  echo "source=${DP_SOURCE_NAME:-}"
  echo "storage=${DP_STORAGE_NAME:-}"
  echo "scope=${scope_type}"
  echo "databases=${meta_databases}"
  echo "tables=${meta_tables}"
  echo "created_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
} > "${meta_file}"

echo "$(basename "${snapshot_file}")" > "${component_root}/latest.txt"
prune_snapshots "${snapshot_dir}" "${retention}"

mark_done
echo "[INFO] mysql backup completed: ${snapshot_file}"`

const mysqlRestoreScript = `set -euo pipefail

BACKUP_BASE_DIR="${BACKUP_BASE_DIR:-/backup}"
BACKUP_COMPONENT_PATH="${BACKUP_COMPONENT_PATH:?BACKUP_COMPONENT_PATH is required}"

wait_for_mysql() {
  local retries=30
  local delay=5
  local attempt=1

  while (( attempt <= retries )); do
    if mysqladmin \
      --host="${MYSQL_HOST}" \
      --port="${MYSQL_PORT}" \
      --protocol=TCP \
      --user="${MYSQL_USER}" \
      ping >/dev/null 2>&1; then
      return 0
    fi
    echo "[WARN] mysql is not ready yet, retry ${attempt}/${retries}"
    sleep "${delay}"
    ((attempt++))
  done

  echo "[ERROR] mysql is not reachable"
  exit 1
}

resolve_snapshot_file() {
  local component_root="$1"
  local snapshot_dir="$2"
  local snapshot_name="${MYSQL_RESTORE_SNAPSHOT:-latest}"

  if [[ "${snapshot_name}" == "latest" ]]; then
    if [[ -f "${component_root}/latest.txt" ]]; then
      snapshot_name="$(cat "${component_root}/latest.txt")"
      if [[ -n "${snapshot_name}" && "${snapshot_name}" != *.sql.gz ]]; then
        snapshot_name="${snapshot_name}.sql.gz"
      fi
      if [[ -z "${snapshot_name}" || ! -f "${snapshot_dir}/${snapshot_name}" ]]; then
        snapshot_name="$(find "${snapshot_dir}" -maxdepth 1 -type f -name "*.sql.gz" -printf "%f\n" | sort -r | head -n 1)"
      fi
    else
      snapshot_name="$(find "${snapshot_dir}" -maxdepth 1 -type f -name "*.sql.gz" -printf "%f\n" | sort -r | head -n 1)"
    fi
  fi

  [[ -n "${snapshot_name}" ]] || {
    echo "[ERROR] no mysql snapshot found"
    exit 1
  }

  if [[ "${snapshot_name}" != *.sql.gz ]]; then
    snapshot_name="${snapshot_name}.sql.gz"
  fi

  echo "${snapshot_dir}/${snapshot_name}"
}

drop_user_databases() {
  local db
  while IFS= read -r db; do
    [[ -n "${db}" ]] || continue
    mysql \
      --host="${MYSQL_HOST}" \
      --port="${MYSQL_PORT}" \
      --protocol=TCP \
      --user="${MYSQL_USER}" \
      -e "DROP DATABASE IF EXISTS ${db};"
  done < <(
    mysql \
      --host="${MYSQL_HOST}" \
      --port="${MYSQL_PORT}" \
      --protocol=TCP \
      --user="${MYSQL_USER}" \
      -Nse "SHOW DATABASES" \
      | grep -Ev '^(information_schema|performance_schema|mysql|sys)$' || true
  )
}

export MYSQL_PWD="${MYSQL_PASSWORD:-}"
component_root="${BACKUP_BASE_DIR}/${BACKUP_COMPONENT_PATH}"
snapshot_dir="${component_root}/snapshots"
snapshot_file="$(resolve_snapshot_file "${component_root}" "${snapshot_dir}")"

[[ -f "${snapshot_file}" ]] || {
  echo "[ERROR] mysql restore snapshot not found: ${snapshot_file}"
  exit 1
}

checksum_file="${snapshot_file}.sha256"
[[ ! -f "${checksum_file}" ]] || sha256sum -c "${checksum_file}"

wait_for_mysql

restore_mode="${MYSQL_RESTORE_MODE:-merge}"
if [[ "${restore_mode}" == "wipe-all-user-databases" ]]; then
  echo "[WARN] restore mode is wipe-all-user-databases, dropping existing user databases first"
  drop_user_databases
fi

gunzip -c "${snapshot_file}" | mysql \
  --host="${MYSQL_HOST}" \
  --port="${MYSQL_PORT}" \
  --protocol=TCP \
  --user="${MYSQL_USER}"

echo "[INFO] mysql restore completed from ${snapshot_file}"`

const mysqlS3PrefetchScript = `set -eu

BACKUP_COMPONENT_PATH="${BACKUP_COMPONENT_PATH:?BACKUP_COMPONENT_PATH is required}"
BACKUP_BASE_DIR="${BACKUP_BASE_DIR:-/workspace/export}"

mc_cmd() {
  if [ "${S3_INSECURE:-false}" = "true" ]; then
    mc --insecure "$@"
  else
    mc "$@"
  fi
}

remote_path="${S3_BUCKET}"
if [ -n "${S3_PREFIX:-}" ]; then
  remote_path="${remote_path}/${S3_PREFIX}"
fi
remote_path="${remote_path}/${BACKUP_COMPONENT_PATH}"

mkdir -p "${BACKUP_BASE_DIR}/${BACKUP_COMPONENT_PATH}"
mc_cmd alias set backup "${S3_ENDPOINT}" "${S3_ACCESS_KEY}" "${S3_SECRET_KEY}" --api S3v4 >/dev/null

if mc_cmd ls "backup/${remote_path}" >/dev/null 2>&1; then
  mc_cmd mirror "backup/${remote_path}" "${BACKUP_BASE_DIR}/${BACKUP_COMPONENT_PATH}"
  echo "[INFO] s3 prefetch completed from ${remote_path}"
else
  echo "[INFO] no remote snapshot found at ${remote_path}, skip prefetch"
fi`

const mysqlS3UploadScript = `set -eu

BACKUP_COMPONENT_PATH="${BACKUP_COMPONENT_PATH:?BACKUP_COMPONENT_PATH is required}"
BACKUP_BASE_DIR="${BACKUP_BASE_DIR:-/workspace/export}"
STATUS_DIR="${STATUS_DIR:-/workspace/status}"

mc_cmd() {
  if [ "${S3_INSECURE:-false}" = "true" ]; then
    mc --insecure "$@"
  else
    mc "$@"
  fi
}

ensure_bucket() {
  if mc_cmd ls "backup/${S3_BUCKET}" >/dev/null 2>&1; then
    return 0
  fi
  echo "[INFO] remote bucket ${S3_BUCKET} not found, creating it"
  mc_cmd mb "backup/${S3_BUCKET}" >/dev/null
}

remote_path="${S3_BUCKET}"
if [ -n "${S3_PREFIX:-}" ]; then
  remote_path="${remote_path}/${S3_PREFIX}"
fi
remote_path="${remote_path}/${BACKUP_COMPONENT_PATH}"

local_dir="${BACKUP_BASE_DIR}/${BACKUP_COMPONENT_PATH}"
mkdir -p "${STATUS_DIR}"

timeout_seconds="${S3_SYNC_TIMEOUT:-3600}"
waited=0
while [ ! -f "${STATUS_DIR}/done" ]; do
  if [ -f "${STATUS_DIR}/failed" ]; then
    echo "[ERROR] backup container reported failure"
    exit 1
  fi
  if [ "${waited}" -ge "${timeout_seconds}" ]; then
    echo "[ERROR] wait backup artifact timeout"
    exit 1
  fi
  sleep 5
  waited=$((waited + 5))
done

[ -d "${local_dir}" ] || {
  echo "[ERROR] local backup directory not found: ${local_dir}"
  exit 1
}

mc_cmd alias set backup "${S3_ENDPOINT}" "${S3_ACCESS_KEY}" "${S3_SECRET_KEY}" --api S3v4 >/dev/null
ensure_bucket
mc_cmd mirror --overwrite --remove "${local_dir}" "backup/${remote_path}"
echo "[INFO] s3 upload completed to ${remote_path}"`

const mysqlS3DownloadScript = `set -eu

BACKUP_COMPONENT_PATH="${BACKUP_COMPONENT_PATH:?BACKUP_COMPONENT_PATH is required}"
BACKUP_BASE_DIR="${BACKUP_BASE_DIR:-/workspace/restore}"

mc_cmd() {
  if [ "${S3_INSECURE:-false}" = "true" ]; then
    mc --insecure "$@"
  else
    mc "$@"
  fi
}

remote_path="${S3_BUCKET}"
if [ -n "${S3_PREFIX:-}" ]; then
  remote_path="${remote_path}/${S3_PREFIX}"
fi
remote_path="${remote_path}/${BACKUP_COMPONENT_PATH}"

local_dir="${BACKUP_BASE_DIR}/${BACKUP_COMPONENT_PATH}"
mkdir -p "${local_dir}"

mc_cmd alias set backup "${S3_ENDPOINT}" "${S3_ACCESS_KEY}" "${S3_SECRET_KEY}" --api S3v4 >/dev/null
mc_cmd ls "backup/${remote_path}" >/dev/null 2>&1 || {
  echo "[ERROR] remote backup path not found: ${remote_path}"
  exit 1
}
mc_cmd mirror "backup/${remote_path}" "${local_dir}"
echo "[INFO] s3 download completed from ${remote_path}"`

func buildBuiltInMySQLBackupRunJob(run *dpv1alpha1.BackupRun, policy *dpv1alpha1.BackupPolicy, source *dpv1alpha1.BackupSource, storage *dpv1alpha1.BackupStorage, storagePath string, snapshot string, keepLast int32) (*batchv1.Job, error) {
	execution := dpv1alpha1.ExecutionTemplateSpec{}
	policyDriverConfig := dpv1alpha1.DriverConfig{}
	if policy != nil {
		execution = policy.Spec.Execution
		policyDriverConfig = policy.Spec.DriverConfig
	}
	execution = defaultMySQLExecutionTemplate(execution)
	driverConfig := effectiveMySQLDriverConfig(source.Spec.DriverConfig, policyDriverConfig, run.Spec.DriverConfig)
	podSpec, err := buildBuiltInMySQLBackupPodSpec(execution, source, storage, storagePath, driverConfig, snapshot, keepLast)
	if err != nil {
		return nil, err
	}

	name := dpv1alpha1.BuildJobName(run.Name, storage.Name)
	labels := managedResourceLabels("BackupRun", run.Name, "manual-backup", source.Name, storage.Name)
	annotations := map[string]string{
		snapshotAnnotation:                           ensureMySQLSnapshotFile(snapshot),
		storagePathAnnotation:                        storagePath,
		"dataprotection.archinfra.io/driver-runtime": "builtin-mysql",
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

func buildBuiltInMySQLRestoreJob(restore *dpv1alpha1.RestoreRequest, backupRun *dpv1alpha1.BackupRun, source *dpv1alpha1.BackupSource, storage *dpv1alpha1.BackupStorage, storagePath string, execution dpv1alpha1.ExecutionTemplateSpec, snapshot string) (*batchv1.Job, error) {
	execution = defaultMySQLExecutionTemplate(execution)
	driverConfig := effectiveMySQLDriverConfig(source.Spec.DriverConfig, dpv1alpha1.DriverConfig{}, restore.Spec.Target.DriverConfig)
	podSpec, err := buildBuiltInMySQLRestorePodSpec(execution, restore, source, storage, storagePath, driverConfig, snapshot)
	if err != nil {
		return nil, err
	}

	name := dpv1alpha1.BuildJobName(restore.Name, "restore")
	labels := managedResourceLabels("RestoreRequest", restore.Name, "restore", source.Name, storage.Name)
	annotations := map[string]string{
		snapshotAnnotation:                           ensureMySQLSnapshotFile(snapshot),
		targetModeAnnotation:                         string(effectiveRestoreTargetMode(restore.Spec.Target.Mode)),
		storagePathAnnotation:                        storagePath,
		"dataprotection.archinfra.io/driver-runtime": "builtin-mysql",
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

func buildBuiltInMySQLBackupPodSpec(execution dpv1alpha1.ExecutionTemplateSpec, source *dpv1alpha1.BackupSource, storage *dpv1alpha1.BackupStorage, storagePath string, driverConfig dpv1alpha1.DriverConfig, snapshot string, retention int32) (corev1.PodSpec, error) {
	connectionEnv, err := mysqlConnectionEnvVars(source.Spec.Endpoint, source.Spec.TargetRef, source.Namespace)
	if err != nil {
		return corev1.PodSpec{}, err
	}
	if retention <= 0 {
		retention = defaultBackupRetention
	}

	mysqlConfig := &dpv1alpha1.MySQLDriverConfig{}
	if driverConfig.MySQL != nil {
		mysqlConfig = driverConfig.MySQL
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
		{Name: "BACKUP_DATABASES", Value: strings.Join(mysqlConfig.Databases, ",")},
		{Name: "BACKUP_TABLES", Value: strings.Join(mysqlConfig.Tables, ",")},
	}
	if strings.TrimSpace(snapshot) != "" {
		envs = append(envs, corev1.EnvVar{Name: "MYSQL_BACKUP_SNAPSHOT", Value: ensureMySQLSnapshotBase(snapshot)})
	}
	envs = append(envs, connectionEnv...)
	envs = mergeEnvVars(envs, execution.ExtraEnv)

	mysqlContainer := corev1.Container{
		Name:            "mysql-backup",
		Image:           execution.RunnerImage,
		ImagePullPolicy: execution.ImagePullPolicy,
		Command:         []string{"/bin/bash", "-ceu"},
		Args:            []string{mysqlBackupScript},
		Env:             envs,
		Resources:       execution.Resources,
	}

	podSpec := corev1.PodSpec{
		RestartPolicy:      corev1.RestartPolicyNever,
		ServiceAccountName: execution.ServiceAccountName,
		NodeSelector:       copyStringMap(execution.NodeSelector),
		Tolerations:        cloneTolerations(execution.Tolerations),
		Containers:         []corev1.Container{mysqlContainer},
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
		podSpec.Containers[0].Env = append(podSpec.Containers[0].Env, corev1.EnvVar{Name: "BACKUP_BASE_DIR", Value: mysqlBackupMountPath})
		podSpec.Containers[0].VolumeMounts = []corev1.VolumeMount{{Name: "backup-storage", MountPath: mysqlBackupMountPath}}
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
					{Name: "BACKUP_BASE_DIR", Value: mysqlExportMountPath},
				}, s3Env...),
				VolumeMounts: []corev1.VolumeMount{
					{Name: "export-staging", MountPath: mysqlExportMountPath},
				},
			},
		}
		podSpec.Containers[0].Env = append(podSpec.Containers[0].Env,
			corev1.EnvVar{Name: "BACKUP_BASE_DIR", Value: mysqlExportMountPath},
			corev1.EnvVar{Name: "STATUS_DIR", Value: mysqlStatusMountPath},
		)
		podSpec.Containers[0].VolumeMounts = []corev1.VolumeMount{
			{Name: "export-staging", MountPath: mysqlExportMountPath},
			{Name: "status-dir", MountPath: mysqlStatusMountPath},
		}
		podSpec.Containers = append(podSpec.Containers, corev1.Container{
			Name:            "s3-upload",
			Image:           execution.HelperImage,
			ImagePullPolicy: execution.ImagePullPolicy,
			Command:         []string{"/bin/sh", "-ceu"},
			Args:            []string{mysqlS3UploadScript},
			Env: append([]corev1.EnvVar{
				{Name: "BACKUP_COMPONENT_PATH", Value: storagePath},
				{Name: "BACKUP_BASE_DIR", Value: mysqlExportMountPath},
				{Name: "STATUS_DIR", Value: mysqlStatusMountPath},
			}, s3Env...),
			VolumeMounts: []corev1.VolumeMount{
				{Name: "export-staging", MountPath: mysqlExportMountPath},
				{Name: "status-dir", MountPath: mysqlStatusMountPath},
			},
		})
		podSpec.Volumes = []corev1.Volume{
			{Name: "export-staging", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
			{Name: "status-dir", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		}
	default:
		return corev1.PodSpec{}, newPermanentDependencyError("unsupported storage type %q for built-in mysql backup runtime", storage.Spec.Type)
	}

	return podSpec, nil
}

func buildBuiltInMySQLRestorePodSpec(execution dpv1alpha1.ExecutionTemplateSpec, restore *dpv1alpha1.RestoreRequest, source *dpv1alpha1.BackupSource, storage *dpv1alpha1.BackupStorage, storagePath string, driverConfig dpv1alpha1.DriverConfig, snapshot string) (corev1.PodSpec, error) {
	connectionEndpoint, connectionTargetRef := effectiveRestoreEndpoint(restore, source)
	connectionEnv, err := mysqlConnectionEnvVars(connectionEndpoint, connectionTargetRef, restore.Namespace)
	if err != nil {
		return corev1.PodSpec{}, err
	}

	restoreMode := "merge"
	if driverConfig.MySQL != nil && strings.TrimSpace(driverConfig.MySQL.RestoreMode) != "" {
		restoreMode = driverConfig.MySQL.RestoreMode
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
		{Name: "MYSQL_RESTORE_SNAPSHOT", Value: snapshot},
		{Name: "MYSQL_RESTORE_MODE", Value: restoreMode},
	}
	envs = append(envs, connectionEnv...)
	envs = mergeEnvVars(envs, execution.ExtraEnv)

	mysqlContainer := corev1.Container{
		Name:            "mysql-restore",
		Image:           execution.RunnerImage,
		ImagePullPolicy: execution.ImagePullPolicy,
		Command:         []string{"/bin/bash", "-ceu"},
		Args:            []string{mysqlRestoreScript},
		Env:             envs,
		Resources:       execution.Resources,
	}

	podSpec := corev1.PodSpec{
		RestartPolicy:      corev1.RestartPolicyNever,
		ServiceAccountName: execution.ServiceAccountName,
		NodeSelector:       copyStringMap(execution.NodeSelector),
		Tolerations:        cloneTolerations(execution.Tolerations),
		Containers:         []corev1.Container{mysqlContainer},
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
		podSpec.Containers[0].Env = append(podSpec.Containers[0].Env, corev1.EnvVar{Name: "BACKUP_BASE_DIR", Value: mysqlBackupMountPath})
		podSpec.Containers[0].VolumeMounts = []corev1.VolumeMount{{Name: "backup-storage", MountPath: mysqlBackupMountPath}}
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
					{Name: "BACKUP_BASE_DIR", Value: mysqlRestoreMountPath},
				}, s3Env...),
				VolumeMounts: []corev1.VolumeMount{
					{Name: "restore-staging", MountPath: mysqlRestoreMountPath},
				},
			},
		}
		podSpec.Containers[0].Env = append(podSpec.Containers[0].Env, corev1.EnvVar{Name: "BACKUP_BASE_DIR", Value: mysqlRestoreMountPath})
		podSpec.Containers[0].VolumeMounts = []corev1.VolumeMount{
			{Name: "restore-staging", MountPath: mysqlRestoreMountPath},
		}
		podSpec.Volumes = []corev1.Volume{
			{Name: "restore-staging", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		}
	default:
		return corev1.PodSpec{}, newPermanentDependencyError("unsupported storage type %q for built-in mysql restore runtime", storage.Spec.Type)
	}

	return podSpec, nil
}

func defaultMySQLExecutionTemplate(spec dpv1alpha1.ExecutionTemplateSpec) dpv1alpha1.ExecutionTemplateSpec {
	spec.HelperImage = strings.TrimSpace(spec.HelperImage)
	spec.RunnerImage = strings.TrimSpace(spec.RunnerImage)
	if spec.RunnerImage == "" {
		spec.RunnerImage = defaultMySQLRunnerImage()
	}
	if spec.HelperImage == "" {
		spec.HelperImage = defaultS3HelperImage()
	}
	if spec.ImagePullPolicy == "" {
		spec.ImagePullPolicy = corev1.PullIfNotPresent
	}
	if spec.BackoffLimit == nil {
		spec.BackoffLimit = int32Ptr(1)
	}
	if spec.TTLSecondsAfterFinished == nil {
		spec.TTLSecondsAfterFinished = defaultJobTTLSeconds()
	}
	return spec
}

func useBuiltInMySQLRuntime(driver dpv1alpha1.BackupDriver, execution dpv1alpha1.ExecutionTemplateSpec) bool {
	if driver != dpv1alpha1.BackupDriverMySQL {
		return false
	}
	return len(execution.Command) == 0 && len(execution.Args) == 0
}

func mysqlConnectionEnvVars(endpoint dpv1alpha1.EndpointSpec, targetRef *dpv1alpha1.NamespacedObjectReference, defaultNamespace string) ([]corev1.EnvVar, error) {
	host, err := resolveMySQLHost(endpoint, targetRef, defaultNamespace)
	if err != nil {
		return nil, err
	}
	port := endpoint.Port
	if port == 0 {
		port = defaultMySQLPort
	}

	envs := []corev1.EnvVar{
		{Name: "MYSQL_HOST", Value: host},
		{Name: "MYSQL_PORT", Value: fmt.Sprintf("%d", port)},
	}
	if endpoint.UsernameFrom != nil {
		envs = appendSecretEnvVar(envs, "MYSQL_USER", endpoint.UsernameFrom)
	} else if username := strings.TrimSpace(endpoint.Username); username != "" {
		envs = append(envs, corev1.EnvVar{Name: "MYSQL_USER", Value: username})
	} else {
		envs = append(envs, corev1.EnvVar{Name: "MYSQL_USER", Value: "root"})
	}
	envs = appendSecretEnvVar(envs, "MYSQL_PASSWORD", endpoint.PasswordFrom)
	return envs, nil
}

func resolveMySQLHost(endpoint dpv1alpha1.EndpointSpec, targetRef *dpv1alpha1.NamespacedObjectReference, defaultNamespace string) (string, error) {
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
		return "", newPermanentDependencyError("built-in mysql runtime only supports targetRef kind Service, got %q", targetRef.Kind)
	}
	return "", newPermanentDependencyError("built-in mysql runtime requires endpoint.host/serviceRef or targetRef service")
}

func s3StorageEnvVars(storage *dpv1alpha1.BackupStorage) ([]corev1.EnvVar, error) {
	if storage.Spec.Type != dpv1alpha1.StorageTypeS3 || storage.Spec.S3 == nil {
		return nil, newPermanentDependencyError("storage %q is not an s3 storage", storage.Name)
	}
	envs := []corev1.EnvVar{
		{Name: "S3_ENDPOINT", Value: storage.Spec.S3.Endpoint},
		{Name: "S3_BUCKET", Value: storage.Spec.S3.Bucket},
		{Name: "S3_PREFIX", Value: storage.Spec.S3.Prefix},
		{Name: "S3_INSECURE", Value: boolString(storage.Spec.S3.Insecure)},
	}
	envs = appendSecretEnvVar(envs, "S3_ACCESS_KEY", storage.Spec.S3.AccessKeyFrom)
	envs = appendSecretEnvVar(envs, "S3_SECRET_KEY", storage.Spec.S3.SecretKeyFrom)
	envs = appendSecretEnvVar(envs, "S3_SESSION_TOKEN", storage.Spec.S3.SessionTokenRef)
	return envs, nil
}

func effectiveMySQLDriverConfig(base, middle, override dpv1alpha1.DriverConfig) dpv1alpha1.DriverConfig {
	result := dpv1alpha1.DriverConfig{}
	switch {
	case override.MySQL != nil:
		result.MySQL = override.MySQL.DeepCopy()
	case middle.MySQL != nil:
		result.MySQL = middle.MySQL.DeepCopy()
	case base.MySQL != nil:
		result.MySQL = base.MySQL.DeepCopy()
	}
	return result
}

func effectiveRestoreEndpoint(restore *dpv1alpha1.RestoreRequest, source *dpv1alpha1.BackupSource) (dpv1alpha1.EndpointSpec, *dpv1alpha1.NamespacedObjectReference) {
	if restore.Spec.Target.Endpoint != nil {
		return *restore.Spec.Target.Endpoint.DeepCopy(), restore.Spec.Target.TargetRef
	}
	if restore.Spec.Target.TargetRef != nil {
		return dpv1alpha1.EndpointSpec{}, restore.Spec.Target.TargetRef
	}
	return *source.Spec.Endpoint.DeepCopy(), source.Spec.TargetRef
}

func ensureMySQLSnapshotFile(snapshot string) string {
	snapshot = strings.TrimSpace(snapshot)
	if snapshot == "" {
		return ""
	}
	if strings.HasSuffix(snapshot, ".sql.gz") {
		return snapshot
	}
	return snapshot + ".sql.gz"
}

func ensureMySQLSnapshotBase(snapshot string) string {
	snapshot = strings.TrimSpace(snapshot)
	if snapshot == "" {
		return ""
	}
	return strings.TrimSuffix(ensureMySQLSnapshotFile(snapshot), ".sql.gz")
}

func retentionValue(retention dpv1alpha1.RetentionRule) int32 {
	if retention.KeepLast <= 0 {
		return defaultBackupRetention
	}
	return retention.KeepLast
}
