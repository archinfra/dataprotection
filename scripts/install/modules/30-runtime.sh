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
