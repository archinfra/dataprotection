render_install_manifest() {
  local output_file="$1"
  sed \
    -e "s|{{NAMESPACE}}|${NAMESPACE}|g" \
    -e "s|{{OPERATOR_IMAGE}}|$(operator_image_ref)|g" \
    -e "s|{{MYSQL_RUNNER_IMAGE}}|$(mysql_runner_image_ref)|g" \
    -e "s|{{S3_HELPER_IMAGE}}|$(s3_helper_image_ref)|g" \
    -e "s|{{PLACEHOLDER_RUNNER_IMAGE}}|$(placeholder_runner_image_ref)|g" \
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

