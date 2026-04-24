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
  if [[ -f "${IMAGE_JSON}" && -f "${IMAGE_INDEX}" && -d "${CRD_DIR}" && -f "${INSTALL_TEMPLATE}" ]]; then
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
      ;;
  esac
}

trim_slash() {
  local value="$1"
  value="${value%/}"
  printf '%s' "${value}"
}

load_image_metadata() {
  if (( ${#IMAGE_DEFAULT_REFS[@]} > 0 )); then
    return 0
  fi

  [[ -f "${IMAGE_INDEX}" ]] || extract_payload
  [[ -f "${IMAGE_INDEX}" ]] || die "Payload is missing images/image-index.tsv"

  while IFS=$'\t' read -r name _tar_name _load_ref default_target_ref _platform _pull _dockerfile; do
    [[ -n "${name}" ]] || continue
    IMAGE_DEFAULT_REFS["${name}"]="${default_target_ref}"
  done < "${IMAGE_INDEX}"

  (( ${#IMAGE_DEFAULT_REFS[@]} > 0 )) || die "No image metadata found in ${IMAGE_INDEX}"
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
  load_image_metadata
  default_ref="${IMAGE_DEFAULT_REFS[${name}]:-}"
  [[ -n "${default_ref}" ]] || die "Unable to resolve image tag for ${name} from image-index.tsv"
  retarget_image_ref "${default_ref}"
}

operator_image_ref() {
  if [[ -n "${OPERATOR_IMAGE_OVERRIDE}" ]]; then
    printf '%s' "${OPERATOR_IMAGE_OVERRIDE}"
    return
  fi
  default_image_ref "dataprotection-operator"
}

minio_helper_image_ref() {
  if [[ -n "${MINIO_HELPER_IMAGE_OVERRIDE}" ]]; then
    printf '%s' "${MINIO_HELPER_IMAGE_OVERRIDE}"
    return
  fi
  default_image_ref "dataprotection-minio-mc"
}

utility_image_ref() {
  if [[ -n "${UTILITY_IMAGE_OVERRIDE}" ]]; then
    printf '%s' "${UTILITY_IMAGE_OVERRIDE}"
    return
  fi
  default_image_ref "dataprotection-busybox"
}
