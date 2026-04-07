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

