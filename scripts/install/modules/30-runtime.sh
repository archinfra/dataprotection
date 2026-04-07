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
  local marker_line
  marker_line="$(awk "/^${PAYLOAD_MARKER}$/ { print NR + 1; exit 0; }" "$0")"
  [[ -n "${marker_line}" ]] || die "Payload marker not found"
  tail -n +"${marker_line}" "$0" | tar -xz -C "${WORKDIR}"
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

target_image_ref() {
  local name="$1"
  local tag="$2"
  printf '%s/%s:%s' "$(trim_slash "${REGISTRY}")" "${name}" "${tag}"
}

operator_image_ref() {
  if [[ -n "${OPERATOR_IMAGE_OVERRIDE}" ]]; then
    printf '%s' "${OPERATOR_IMAGE_OVERRIDE}"
    return
  fi
  target_image_ref "dataprotection-operator" "${APP_VERSION}-$(installer_arch)"
}

mysql_runner_image_ref() {
  if [[ -n "${MYSQL_RUNNER_IMAGE_OVERRIDE}" ]]; then
    printf '%s' "${MYSQL_RUNNER_IMAGE_OVERRIDE}"
    return
  fi
  target_image_ref "dataprotection-mysql" "8.0.45"
}

s3_helper_image_ref() {
  if [[ -n "${S3_HELPER_IMAGE_OVERRIDE}" ]]; then
    printf '%s' "${S3_HELPER_IMAGE_OVERRIDE}"
    return
  fi
  target_image_ref "dataprotection-minio-mc" "latest"
}

placeholder_runner_image_ref() {
  if [[ -n "${PLACEHOLDER_RUNNER_IMAGE_OVERRIDE}" ]]; then
    printf '%s' "${PLACEHOLDER_RUNNER_IMAGE_OVERRIDE}"
    return
  fi
  target_image_ref "dataprotection-busybox" "1.36"
}

installer_arch() {
  local arch
  arch="$(jq -r 'first(.[]).arch' "${IMAGE_JSON}")"
  [[ -n "${arch}" && "${arch}" != "null" ]] || die "Unable to detect installer arch from image.json"
  printf '%s' "${arch}"
}
