cleanup() {
  :
}

main() {
  trap cleanup EXIT
  parse_args "$@"

  if [[ "${ACTION}" == "help" ]]; then
    usage
    exit 0
  fi

  validate_environment
  confirm_plan

  case "${ACTION}" in
    install)
      install_operator
      ;;
    uninstall)
      uninstall_operator
      ;;
    status)
      show_status
      ;;
  esac
}

main "$@"

exit 0

__PAYLOAD_BELOW__
