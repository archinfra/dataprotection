docker_login_if_requested() {
  [[ -n "${REGISTRY_USER}" ]] || return 0
  [[ -n "${REGISTRY_PASSWORD}" ]] || die "--registry-password is required when --registry-user is set"
  printf '%s' "${REGISTRY_PASSWORD}" | docker login "$(trim_slash "${REGISTRY}" | awk -F/ '{print $1}')" --username "${REGISTRY_USER}" --password-stdin >/dev/null
}

prepare_images() {
  load_image_metadata
  [[ "${SKIP_IMAGE_PREPARE}" == "true" ]] && return 0

  docker_login_if_requested

  section "Preparing images"
  while IFS=$'\t' read -r _name tar_name load_ref default_target_ref _platform _pull _dockerfile; do
    [[ -n "${tar_name}" ]] || continue

    local tar_path target_ref
    tar_path="${IMAGE_DIR}/${tar_name}"
    target_ref="$(retarget_image_ref "${default_target_ref}")"

    if docker image inspect "${target_ref}" >/dev/null 2>&1; then
      log "Reuse local image ${target_ref}"
    else
      [[ -f "${tar_path}" ]] || die "Image tar not found: ${tar_path}"
      log "Loading ${tar_name}"
      docker load -i "${tar_path}" >/dev/null
      if [[ "${load_ref}" != "${target_ref}" ]]; then
        docker tag "${load_ref}" "${target_ref}"
      fi
    fi

    log "Pushing ${target_ref}"
    docker push "${target_ref}" >/dev/null
  done < "${IMAGE_INDEX}"
}
