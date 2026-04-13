#!/usr/bin/env bash

set -Eeuo pipefail

APP_NAME="dataprotection-addon-mysql"
INSTALLER_VERSION="2.0.4"
DISPLAY_NAME="DataProtection MySQL Addon"
ADDON_NAME="mysql-dump"
WORKDIR="/tmp/${APP_NAME}-installer"
MANIFEST_DIR="${WORKDIR}/manifests"
IMAGE_DIR="${WORKDIR}/images"
IMAGE_JSON="${IMAGE_DIR}/image.json"
ADDON_TEMPLATE_FILE="${MANIFEST_DIR}/backupaddon.yaml.tmpl"
SAMPLES_DIR="${MANIFEST_DIR}/samples"
RENDERED_MANIFEST="${WORKDIR}/rendered-backupaddon.yaml"
DEFAULT_OUTPUT_DIR="./${APP_NAME}-samples"

ACTION="install"
AUTO_YES="false"
SKIP_IMAGE_PREPARE="false"
REGISTRY_REPO="sealos.hub:5000/kube4"
REGISTRY_ADDR="sealos.hub:5000"
REGISTRY_USER="admin"
REGISTRY_PASS="passw0rd"
OUTPUT_DIR="${DEFAULT_OUTPUT_DIR}"

log() {
  printf '[INFO] %s\n' "$*"
}

warn() {
  printf '[WARN] %s\n' "$*" >&2
}

die() {
  printf '[ERROR] %s\n' "$*" >&2
  exit 1
}

refresh_registry_addr() {
  if [[ "${REGISTRY_REPO}" == */* ]]; then
    REGISTRY_ADDR="${REGISTRY_REPO%%/*}"
  else
    REGISTRY_ADDR="${REGISTRY_REPO}"
  fi
}

usage() {
  cat <<EOF
Usage:
  ./${APP_NAME}-<arch>.run <action> [options]

Actions:
  install      load addon image tarballs and apply the BackupAddon manifest
  uninstall    delete BackupAddon/${ADDON_NAME}
  status       show BackupAddon/${ADDON_NAME}
  samples      extract example YAMLs to a local directory
  help         show this message

Options:
  --registry <repo-prefix>      default: ${REGISTRY_REPO}
  --registry-user <user>        default: ${REGISTRY_USER}
  --registry-pass <pass>        default: ${REGISTRY_PASS}
  --skip-image-prepare          skip docker load/tag/push
  --output-dir <dir>            used by action=samples
  -y, --yes                     skip confirmation

Examples:
  ./${APP_NAME}-amd64.run install -y
  ./${APP_NAME}-amd64.run install --registry sealos.hub:5000/kube4 -y
  ./${APP_NAME}-amd64.run samples --output-dir ./samples/mysql
  ./${APP_NAME}-amd64.run status
EOF
}

parse_action() {
  if [[ $# -eq 0 ]]; then
    ACTION="install"
    return
  fi
  case "$1" in
    install|uninstall|status|samples)
      ACTION="$1"
      shift
      ;;
    help|-h|--help)
      ACTION="help"
      shift
      ;;
  esac
  parse_args "$@"
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --registry)
        [[ $# -ge 2 ]] || die "Missing value for $1"
        REGISTRY_REPO="$2"
        refresh_registry_addr
        shift 2
        ;;
      --registry-user)
        [[ $# -ge 2 ]] || die "Missing value for $1"
        REGISTRY_USER="$2"
        shift 2
        ;;
      --registry-pass)
        [[ $# -ge 2 ]] || die "Missing value for $1"
        REGISTRY_PASS="$2"
        shift 2
        ;;
      --skip-image-prepare)
        SKIP_IMAGE_PREPARE="true"
        shift
        ;;
      --output-dir)
        [[ $# -ge 2 ]] || die "Missing value for $1"
        OUTPUT_DIR="$2"
        shift 2
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
}

check_requirements() {
  case "${ACTION}" in
    install)
      command -v kubectl >/dev/null 2>&1 || die "kubectl is required"
      command -v jq >/dev/null 2>&1 || die "jq is required"
      command -v tar >/dev/null 2>&1 || die "tar is required"
      command -v awk >/dev/null 2>&1 || die "awk is required"
      command -v head >/dev/null 2>&1 || die "head is required"
      command -v tail >/dev/null 2>&1 || die "tail is required"
      command -v dd >/dev/null 2>&1 || die "dd is required"
      command -v od >/dev/null 2>&1 || die "od is required"
      if [[ "${SKIP_IMAGE_PREPARE}" != "true" ]]; then
        command -v docker >/dev/null 2>&1 || die "docker is required when image preparation is enabled"
      fi
      ;;
    uninstall|status)
      command -v kubectl >/dev/null 2>&1 || die "kubectl is required"
      ;;
    samples)
      command -v tar >/dev/null 2>&1 || die "tar is required"
      command -v awk >/dev/null 2>&1 || die "awk is required"
      command -v head >/dev/null 2>&1 || die "head is required"
      command -v tail >/dev/null 2>&1 || die "tail is required"
      command -v dd >/dev/null 2>&1 || die "dd is required"
      command -v od >/dev/null 2>&1 || die "od is required"
      ;;
  esac
}

print_plan() {
  echo
  echo "============================================================"
  echo "Execution Plan"
  echo "============================================================"
  echo "Action               : ${ACTION}"
  echo "Addon                : ${ADDON_NAME}"
  echo "Registry             : ${REGISTRY_REPO}"
  echo "Skip image prepare   : ${SKIP_IMAGE_PREPARE}"
  if [[ "${ACTION}" == "samples" ]]; then
    echo "Output directory     : ${OUTPUT_DIR}"
  fi
}

confirm_plan() {
  [[ "${AUTO_YES}" == "true" ]] && return 0
  echo
  read -r -p "Continue? [y/N] " answer
  case "${answer}" in
    y|Y|yes|YES) ;;
    *) die "Canceled by user" ;;
  esac
}

extract_payload() {
  rm -rf "${WORKDIR}"
  mkdir -p "${WORKDIR}"

  local marker_line offset skip byte_hex
  marker_line="$(awk '/^__PAYLOAD_BELOW__$/ { print NR; exit }' "$0")"
  [[ -n "${marker_line}" ]] || die "Unable to locate payload marker"

  offset="$(( $(head -n "${marker_line}" "$0" | wc -c | tr -d ' ') + 1 ))"
  skip=0
  while :; do
    byte_hex="$(dd if="$0" bs=1 skip="$((offset + skip - 1))" count=1 2>/dev/null | od -An -tx1 | tr -d ' \n')"
    case "${byte_hex}" in
      0a|0d) skip=$((skip + 1)) ;;
      "") die "Payload is empty" ;;
      *) break ;;
    esac
  done

  tail -c +"$((offset + skip))" "$0" | tar -xzf - -C "${WORKDIR}" || die "Failed to extract payload"
  [[ -f "${ADDON_TEMPLATE_FILE}" ]] || die "Payload is missing manifests/backupaddon.yaml.tmpl"
}

docker_login() {
  log "Logging into registry ${REGISTRY_ADDR}"
  if echo "${REGISTRY_PASS}" | docker login "${REGISTRY_ADDR}" -u "${REGISTRY_USER}" --password-stdin >/dev/null 2>&1; then
    log "Registry login succeeded"
  else
    warn "Registry login failed, continuing"
  fi
}

resolve_target_image_tag() {
  local source_tag="$1"
  local suffix="${source_tag#*/kube4/}"
  if [[ "${suffix}" == "${source_tag}" ]]; then
    suffix="${source_tag##*/}"
  fi
  printf '%s/%s' "${REGISTRY_REPO}" "${suffix}"
}

prepare_images() {
  [[ "${SKIP_IMAGE_PREPARE}" == "true" ]] && return 0
  [[ -f "${IMAGE_JSON}" ]] || die "Payload is missing images/image.json"

  docker_login

  local count=0
  while IFS= read -r item; do
    [[ -n "${item}" ]] || continue
    local tar_name image_tag target_tag tar_path
    tar_name="$(jq -r '.tar' <<<"${item}")"
    image_tag="$(jq -r '.tag // .pull' <<<"${item}")"
    target_tag="$(resolve_target_image_tag "${image_tag}")"
    tar_path="${IMAGE_DIR}/${tar_name}"
    [[ -f "${tar_path}" ]] || die "Image tar not found: ${tar_path}"

    docker load -i "${tar_path}" >/dev/null
    [[ "${target_tag}" == "${image_tag}" ]] || docker tag "${image_tag}" "${target_tag}"
    docker push "${target_tag}" >/dev/null
    count=$((count + 1))
  done < <(jq -c '.[]' "${IMAGE_JSON}")
  (( count > 0 )) || die "No images were prepared"
}

render_manifest() {
  local template rendered runner_image
  template="$(< "${ADDON_TEMPLATE_FILE}")"
  runner_image="$(resolve_target_image_tag "$(jq -r '.[0].tag // .[0].pull' "${IMAGE_JSON}")")"
  rendered="${template//__RUNNER_IMAGE__/${runner_image}}"
  rendered="${rendered//__ADDON_VERSION__/${INSTALLER_VERSION}}"
  printf '%s' "${rendered}" > "${RENDERED_MANIFEST}"
}

install_addon() {
  extract_payload
  prepare_images
  render_manifest
  kubectl apply -f "${RENDERED_MANIFEST}"
  log "Installed BackupAddon/${ADDON_NAME}"
  log "Use '${0##*/} samples --output-dir ./samples' to export example YAMLs"
}

uninstall_addon() {
  kubectl delete backupaddon "${ADDON_NAME}" --ignore-not-found=true >/dev/null
  log "Removed BackupAddon/${ADDON_NAME}"
}

show_status() {
  kubectl get backupaddon "${ADDON_NAME}" -o wide
}

export_samples() {
  extract_payload
  rm -rf "${OUTPUT_DIR}"
  mkdir -p "${OUTPUT_DIR}"
  cp -r "${SAMPLES_DIR}/"* "${OUTPUT_DIR}/"
  log "Samples exported to ${OUTPUT_DIR}"
}

cleanup() {
  rm -rf "${WORKDIR}" >/dev/null 2>&1 || true
}

main() {
  trap cleanup EXIT
  refresh_registry_addr
  parse_action "$@"

  if [[ "${ACTION}" == "help" ]]; then
    usage
    exit 0
  fi

  check_requirements
  print_plan
  if [[ "${ACTION}" != "status" ]]; then
    confirm_plan
  fi

  case "${ACTION}" in
    install) install_addon ;;
    uninstall) uninstall_addon ;;
    status) show_status ;;
    samples) export_samples ;;
    *) die "Unsupported action: ${ACTION}" ;;
  esac
}

main "$@"

exit 0
__PAYLOAD_BELOW__
