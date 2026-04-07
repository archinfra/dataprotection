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

    local name tag tar_name tar_path source_ref target_ref
    name="$(jq -r '.name' <<<"${item}")"
    tag="$(jq -r '.tag' <<<"${item}")"
    tar_name="$(jq -r '.tar' <<<"${item}")"
    tar_path="${IMAGE_DIR}/${tar_name}"
    target_ref="$(target_image_ref "${name}" "${tag}")"

    if docker image inspect "${target_ref}" >/dev/null 2>&1; then
      log "Reuse local image ${target_ref}"
    else
      [[ -f "${tar_path}" ]] || die "Image tar not found: ${tar_path}"
      log "Loading ${tar_name}"
      source_ref="$(docker load -i "${tar_path}" | awk -F': ' '/Loaded image/ {print $2}' | tail -n 1)"
      [[ -n "${source_ref}" ]] || die "Unable to detect loaded image name from ${tar_name}"
      docker tag "${source_ref}" "${target_ref}"
    fi

    log "Pushing ${target_ref}"
    docker push "${target_ref}" >/dev/null
  done < <(jq -c '.[]' "${IMAGE_JSON}")
}

