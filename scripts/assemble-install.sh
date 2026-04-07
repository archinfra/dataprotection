#!/usr/bin/env bash

set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MODULE_ROOT="${ROOT_DIR}/scripts/install/modules"
OUTPUT_FILE="${1:-${ROOT_DIR}/install.sh}"
TMP_FILE="$(mktemp)"

cleanup() {
  rm -f "${TMP_FILE}" >/dev/null 2>&1 || true
}

trap cleanup EXIT

[[ -d "${MODULE_ROOT}" ]] || {
  echo "module directory not found: ${MODULE_ROOT}" >&2
  exit 1
}

: > "${TMP_FILE}"
mapfile -t modules < <(find "${MODULE_ROOT}" -maxdepth 1 -type f -name '*.sh' | LC_ALL=C sort)

for ((i=0; i<${#modules[@]}; i++)); do
  module_path="${modules[$i]}"
  cat "${module_path}" >> "${TMP_FILE}"
  last_byte="$(tail -c 1 "${module_path}" 2>/dev/null | od -An -tx1 | tr -d ' \n' || true)"
  if [[ "${last_byte}" != "0a" ]]; then
    printf '\n' >> "${TMP_FILE}"
  fi
  if (( i + 1 < ${#modules[@]} )); then
    printf '\n' >> "${TMP_FILE}"
  fi
done

mv "${TMP_FILE}" "${OUTPUT_FILE}"
chmod +x "${OUTPUT_FILE}"
trap - EXIT

