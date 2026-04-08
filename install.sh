#!/usr/bin/env bash

# Generated source layout:
# - edit scripts/install/modules/*.sh
# - regenerate install.sh via scripts/assemble-install.sh

set -Eeuo pipefail

APP_NAME="data-protection-operator"
APP_VERSION="${APP_VERSION:-0.2.3}"
WORKDIR="/tmp/${APP_NAME}-installer"
IMAGE_DIR="${WORKDIR}/images"
MANIFEST_DIR="${WORKDIR}/manifests"
CRD_DIR="${MANIFEST_DIR}/crds"
IMAGE_JSON="${IMAGE_DIR}/image.json"
INSTALL_TEMPLATE="${MANIFEST_DIR}/operator-install.yaml.tmpl"
PAYLOAD_MARKER="__PAYLOAD_BELOW__"

ACTION="install"
NAMESPACE="data-protection-system"
DEFAULT_REGISTRY="sealos.hub:5000/kube4"
REGISTRY="${REGISTRY:-${DEFAULT_REGISTRY}}"
REGISTRY_USER="${REGISTRY_USER:-}"
REGISTRY_PASSWORD="${REGISTRY_PASSWORD:-}"
IMAGE_PULL_POLICY="Always"
WAIT_TIMEOUT="5m"
AUTO_YES="false"
DELETE_CRDS="false"
SKIP_IMAGE_PREPARE="false"

OPERATOR_IMAGE_OVERRIDE=""
MYSQL_RUNNER_IMAGE_OVERRIDE=""
REDIS_RUNNER_IMAGE_OVERRIDE=""
MINIO_RUNNER_IMAGE_OVERRIDE=""
S3_HELPER_IMAGE_OVERRIDE=""
PLACEHOLDER_RUNNER_IMAGE_OVERRIDE=""

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

log() {
  echo -e "${CYAN}[INFO]${NC} $*"
}

success() {
  echo -e "${GREEN}[OK]${NC} $*"
}

warn() {
  echo -e "${YELLOW}[WARN]${NC} $*" >&2
}

die() {
  echo -e "${RED}[ERROR]${NC} $*" >&2
  exit 1
}

section() {
  echo
  echo -e "${BLUE}${BOLD}============================================================${NC}"
  echo -e "${BLUE}${BOLD}$*${NC}"
  echo -e "${BLUE}${BOLD}============================================================${NC}"
}

usage() {
  cat <<EOF
Usage:
  ./$(basename "$0") <install|uninstall|status|help> [options]

Actions:
  install       Prepare images, install CRDs, RBAC and controller Deployment
  uninstall     Remove controller resources; keep CRDs unless --delete-crds is set
  status        Show CRD and Deployment status
  help          Show this message

Options:
  -n, --namespace <ns>              Namespace for the controller, default: ${NAMESPACE}
  --registry <repo>                 Target registry repo prefix, default: ${REGISTRY}
  --registry-user <user>            Optional docker registry username
  --registry-password <password>    Optional docker registry password
  --operator-image <image>          Override controller image
  --mysql-runner-image <image>      Override default MySQL runner image
  --redis-runner-image <image>      Override default Redis runner image
  --minio-runner-image <image>      Override default MinIO runner image
  --s3-helper-image <image>         Override default S3 helper image
  --placeholder-runner-image <img>  Override default placeholder runner image
  --image-pull-policy <policy>      Always|IfNotPresent|Never, default: ${IMAGE_PULL_POLICY}
  --wait-timeout <duration>         rollout wait timeout, default: ${WAIT_TIMEOUT}
  --skip-image-prepare              Reuse images already present in the target registry
  --delete-crds                     With uninstall, also remove CRDs
  -y, --yes                         Skip confirmation

Examples:
  ./$(basename "$0") install --registry registry.example.com/archinfra -y
  ./$(basename "$0") status -n data-protection-system
  ./$(basename "$0") uninstall --delete-crds -y
EOF
}

parse_args() {
  case "${1:-help}" in
    ""|-h|--help|help)
      ACTION="help"
      [[ $# -gt 0 ]] && shift
      ;;
    *)
      ACTION="$1"
      shift
      ;;
  esac

  while [[ $# -gt 0 ]]; do
    case "$1" in
      -n|--namespace)
        [[ $# -ge 2 ]] || die "Missing value for $1"
        NAMESPACE="$2"
        shift 2
        ;;
      --registry)
        [[ $# -ge 2 ]] || die "Missing value for $1"
        REGISTRY="$2"
        shift 2
        ;;
      --registry-user)
        [[ $# -ge 2 ]] || die "Missing value for $1"
        REGISTRY_USER="$2"
        shift 2
        ;;
      --registry-password)
        [[ $# -ge 2 ]] || die "Missing value for $1"
        REGISTRY_PASSWORD="$2"
        shift 2
        ;;
      --operator-image)
        [[ $# -ge 2 ]] || die "Missing value for $1"
        OPERATOR_IMAGE_OVERRIDE="$2"
        shift 2
        ;;
      --mysql-runner-image)
        [[ $# -ge 2 ]] || die "Missing value for $1"
        MYSQL_RUNNER_IMAGE_OVERRIDE="$2"
        shift 2
        ;;
      --redis-runner-image)
        [[ $# -ge 2 ]] || die "Missing value for $1"
        REDIS_RUNNER_IMAGE_OVERRIDE="$2"
        shift 2
        ;;
      --minio-runner-image)
        [[ $# -ge 2 ]] || die "Missing value for $1"
        MINIO_RUNNER_IMAGE_OVERRIDE="$2"
        shift 2
        ;;
      --s3-helper-image)
        [[ $# -ge 2 ]] || die "Missing value for $1"
        S3_HELPER_IMAGE_OVERRIDE="$2"
        shift 2
        ;;
      --placeholder-runner-image)
        [[ $# -ge 2 ]] || die "Missing value for $1"
        PLACEHOLDER_RUNNER_IMAGE_OVERRIDE="$2"
        shift 2
        ;;
      --image-pull-policy)
        [[ $# -ge 2 ]] || die "Missing value for $1"
        IMAGE_PULL_POLICY="$2"
        shift 2
        ;;
      --wait-timeout)
        [[ $# -ge 2 ]] || die "Missing value for $1"
        WAIT_TIMEOUT="$2"
        shift 2
        ;;
      --skip-image-prepare)
        SKIP_IMAGE_PREPARE="true"
        shift
        ;;
      --delete-crds)
        DELETE_CRDS="true"
        shift
        ;;
      -y|--yes)
        AUTO_YES="true"
        shift
        ;;
      -h|--help)
        ACTION="help"
        shift
        ;;
      *)
        die "Unknown argument: $1"
        ;;
    esac
  done

  case "${ACTION}" in
    install|uninstall|status|help)
      ;;
    *)
      die "Unsupported action: ${ACTION}"
      ;;
  esac
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "$1 is required"
}

confirm_plan() {
  [[ "${AUTO_YES}" == "true" ]] && return 0

  echo
  echo "Action: ${ACTION}"
  echo "Namespace: ${NAMESPACE}"
  echo "Registry: ${REGISTRY}"
  echo "Skip image prepare: ${SKIP_IMAGE_PREPARE}"
  echo
  read -r -p "Continue? [y/N] " answer
  case "${answer}" in
    y|Y|yes|YES)
      ;;
    *)
      die "Cancelled"
      ;;
  esac
}

extract_payload() {
  if [[ -f "${IMAGE_JSON}" && -d "${CRD_DIR}" && -f "${INSTALL_TEMPLATE}" ]]; then
    return 0
  fi

  rm -rf "${WORKDIR}"
  mkdir -p "${WORKDIR}"
  local marker_line payload_offset skip_bytes byte_hex
  marker_line="$(awk "/^${PAYLOAD_MARKER}$/ { print NR; exit 0; }" "$0")"
  [[ -n "${marker_line}" ]] || die "Payload marker not found"
  payload_offset="$(( $(head -n "${marker_line}" "$0" | wc -c | tr -d ' ') + 1 ))"

  skip_bytes=0
  while :; do
    byte_hex="$(dd if="$0" bs=1 skip="$((payload_offset + skip_bytes - 1))" count=1 2>/dev/null | od -An -tx1 | tr -d ' \n')"
    case "${byte_hex}" in
      0a|0d)
        skip_bytes=$((skip_bytes + 1))
        ;;
      "")
        die "Payload boundary is invalid or empty"
        ;;
      *)
        break
        ;;
    esac
  done

  if tail -c +"$((payload_offset + skip_bytes))" "$0" | tar -tzf - >/dev/null 2>&1; then
    tail -c +"$((payload_offset + skip_bytes))" "$0" | tar -xzf - -C "${WORKDIR}"
    return 0
  fi

  die "Unable to extract embedded payload"
}

validate_environment() {
  require_command kubectl
  case "${ACTION}" in
    install)
      require_command docker
      require_command jq
      ;;
    uninstall)
      require_command jq
      ;;
  esac
}

trim_slash() {
  local value="$1"
  value="${value%/}"
  printf '%s' "${value}"
}

image_json_field() {
  local name="$1"
  local field="$2"
  jq -r --arg name "${name}" --arg field "${field}" 'map(select(.name == $name)) | first | .[$field] // empty' "${IMAGE_JSON}"
}

retarget_image_ref() {
  local source_ref="$1"
  local registry_repo
  registry_repo="$(trim_slash "${REGISTRY}")"
  [[ -n "${source_ref}" ]] || die "image source ref is empty"
  [[ -n "${registry_repo}" ]] || {
    printf '%s' "${source_ref}"
    return
  }
  printf '%s/%s' "${registry_repo}" "${source_ref##*/}"
}

default_image_ref() {
  local name="$1"
  local default_ref
  default_ref="$(image_json_field "${name}" tag)"
  [[ -n "${default_ref}" ]] || die "Unable to resolve image tag for ${name} from image.json"
  retarget_image_ref "${default_ref}"
}

operator_image_ref() {
  if [[ -n "${OPERATOR_IMAGE_OVERRIDE}" ]]; then
    printf '%s' "${OPERATOR_IMAGE_OVERRIDE}"
    return
  fi
  default_image_ref "dataprotection-operator"
}

mysql_runner_image_ref() {
  if [[ -n "${MYSQL_RUNNER_IMAGE_OVERRIDE}" ]]; then
    printf '%s' "${MYSQL_RUNNER_IMAGE_OVERRIDE}"
    return
  fi
  default_image_ref "dataprotection-mysql"
}

redis_runner_image_ref() {
  if [[ -n "${REDIS_RUNNER_IMAGE_OVERRIDE}" ]]; then
    printf '%s' "${REDIS_RUNNER_IMAGE_OVERRIDE}"
    return
  fi
  default_image_ref "dataprotection-redis"
}

minio_runner_image_ref() {
  if [[ -n "${MINIO_RUNNER_IMAGE_OVERRIDE}" ]]; then
    printf '%s' "${MINIO_RUNNER_IMAGE_OVERRIDE}"
    return
  fi
  default_image_ref "dataprotection-minio-mc"
}

s3_helper_image_ref() {
  if [[ -n "${S3_HELPER_IMAGE_OVERRIDE}" ]]; then
    printf '%s' "${S3_HELPER_IMAGE_OVERRIDE}"
    return
  fi
  default_image_ref "dataprotection-minio-mc"
}

placeholder_runner_image_ref() {
  if [[ -n "${PLACEHOLDER_RUNNER_IMAGE_OVERRIDE}" ]]; then
    printf '%s' "${PLACEHOLDER_RUNNER_IMAGE_OVERRIDE}"
    return
  fi
  default_image_ref "dataprotection-busybox"
}

docker_login_if_requested() {
  [[ -n "${REGISTRY_USER}" ]] || return 0
  [[ -n "${REGISTRY_PASSWORD}" ]] || die "--registry-password is required when --registry-user is set"
  printf '%s' "${REGISTRY_PASSWORD}" | docker login "$(trim_slash "${REGISTRY}" | awk -F/ '{print $1}')" --username "${REGISTRY_USER}" --password-stdin >/dev/null
}

prepare_images() {
  extract_payload
  [[ "${SKIP_IMAGE_PREPARE}" == "true" ]] && return 0

  docker_login_if_requested

  section "Preparing images"
  while IFS= read -r item; do
    [[ -n "${item}" ]] || continue

    local default_target_ref tar_name tar_path source_ref target_ref
    default_target_ref="$(jq -r '.tag' <<<"${item}")"
    tar_name="$(jq -r '.tar' <<<"${item}")"
    tar_path="${IMAGE_DIR}/${tar_name}"
    target_ref="$(retarget_image_ref "${default_target_ref}")"

    if docker image inspect "${target_ref}" >/dev/null 2>&1; then
      log "Reuse local image ${target_ref}"
    else
      [[ -f "${tar_path}" ]] || die "Image tar not found: ${tar_path}"
      log "Loading ${tar_name}"
      source_ref="$(docker load -i "${tar_path}" | awk -F': ' '/Loaded image/ {print $2}' | tail -n 1)"
      [[ -n "${source_ref}" ]] || die "Unable to detect loaded image name from ${tar_name}"
      if [[ "${source_ref}" != "${target_ref}" ]]; then
        docker tag "${source_ref}" "${target_ref}"
      fi
    fi

    log "Pushing ${target_ref}"
    docker push "${target_ref}" >/dev/null
  done < <(jq -c '.[]' "${IMAGE_JSON}")
}

render_install_manifest() {
  local output_file="$1"
  local operator_image
  local mysql_runner_image
  local redis_runner_image
  local minio_runner_image
  local s3_helper_image
  local placeholder_runner_image

  operator_image="$(operator_image_ref)"
  mysql_runner_image="$(mysql_runner_image_ref)"
  redis_runner_image="$(redis_runner_image_ref)"
  minio_runner_image="$(minio_runner_image_ref)"
  s3_helper_image="$(s3_helper_image_ref)"
  placeholder_runner_image="$(placeholder_runner_image_ref)"

  [[ -n "${operator_image}" ]] || die "operator image ref is empty"
  [[ -n "${mysql_runner_image}" ]] || die "mysql runner image ref is empty"
  [[ -n "${redis_runner_image}" ]] || die "redis runner image ref is empty"
  [[ -n "${minio_runner_image}" ]] || die "minio runner image ref is empty"
  [[ -n "${s3_helper_image}" ]] || die "s3 helper image ref is empty"
  [[ -n "${placeholder_runner_image}" ]] || die "placeholder runner image ref is empty"

  sed \
    -e "s|{{NAMESPACE}}|${NAMESPACE}|g" \
    -e "s|{{OPERATOR_IMAGE}}|${operator_image}|g" \
    -e "s|{{MYSQL_RUNNER_IMAGE}}|${mysql_runner_image}|g" \
    -e "s|{{REDIS_RUNNER_IMAGE}}|${redis_runner_image}|g" \
    -e "s|{{MINIO_RUNNER_IMAGE}}|${minio_runner_image}|g" \
    -e "s|{{S3_HELPER_IMAGE}}|${s3_helper_image}|g" \
    -e "s|{{PLACEHOLDER_RUNNER_IMAGE}}|${placeholder_runner_image}|g" \
    -e "s|{{IMAGE_PULL_POLICY}}|${IMAGE_PULL_POLICY}|g" \
    "${INSTALL_TEMPLATE}" > "${output_file}"
}

install_operator() {
  extract_payload
  prepare_images

  local rendered_manifest="${WORKDIR}/rendered-install.yaml"

  section "Installing CRDs"
  kubectl apply -f "${CRD_DIR}"

  section "Installing controller"
  render_install_manifest "${rendered_manifest}"
  kubectl apply -f "${rendered_manifest}"
  kubectl rollout status deployment/data-protection-operator-controller-manager -n "${NAMESPACE}" --timeout="${WAIT_TIMEOUT}"
  success "data-protection-operator installed"
}

uninstall_operator() {
  extract_payload
  local rendered_manifest="${WORKDIR}/rendered-install.yaml"

  render_install_manifest "${rendered_manifest}"

  section "Removing controller"
  kubectl delete -f "${rendered_manifest}" --ignore-not-found >/dev/null 2>&1 || true

  if [[ "${DELETE_CRDS}" == "true" ]]; then
    section "Removing CRDs"
    kubectl delete -f "${CRD_DIR}" --ignore-not-found >/dev/null 2>&1 || true
  fi

  success "data-protection-operator removed"
}

show_status() {
  section "CRDs"
  kubectl get crd | grep 'dataprotection.archinfra.io' || true
  echo
  section "Controller"
  kubectl get deployment,pods -n "${NAMESPACE}" -l app.kubernetes.io/name=data-protection-operator || true
}

cleanup() {
  :
}

main() {
  trap cleanup EXIT
  parse_args "$@"

  if [[ "${ACTION}" == "help" ]]; then
    usage
    exit 0
  fi

  validate_environment
  confirm_plan

  case "${ACTION}" in
    install)
      install_operator
      ;;
    uninstall)
      uninstall_operator
      ;;
    status)
      show_status
      ;;
  esac
}

main "$@"

exit 0

__PAYLOAD_BELOW__
