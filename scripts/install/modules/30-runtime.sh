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

  if tail -n +"${marker_line}" "$0" | tar -tzf - >/dev/null 2>&1; then
    tail -n +"${marker_line}" "$0" | tar -xzf - -C "${WORKDIR}"
    return 0
  fi

  warn "Payload stream has unexpected leading bytes, retrying with one-byte trim"
  if tail -n +"${marker_line}" "$0" | tail -c +2 | tar -tzf - >/dev/null 2>&1; then
    tail -n +"${marker_line}" "$0" | tail -c +2 | tar -xzf - -C "${WORKDIR}"
    return 0
  fi

  warn "Payload stream still invalid, retrying with two-byte trim"
  if tail -n +"${marker_line}" "$0" | tail -c +3 | tar -tzf - >/dev/null 2>&1; then
    tail -n +"${marker_line}" "$0" | tail -c +3 | tar -xzf - -C "${WORKDIR}"
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
