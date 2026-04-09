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
      --minio-helper-image|--s3-helper-image)
        [[ $# -ge 2 ]] || die "Missing value for $1"
        MINIO_HELPER_IMAGE_OVERRIDE="$2"
        shift 2
        ;;
      --utility-image|--placeholder-runner-image)
        [[ $# -ge 2 ]] || die "Missing value for $1"
        UTILITY_IMAGE_OVERRIDE="$2"
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
