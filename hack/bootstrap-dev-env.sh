#!/usr/bin/env bash

set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LOCALBIN="${ROOT_DIR}/bin"

if [[ -x /usr/local/go/bin/go ]]; then
  export PATH="/usr/local/go/bin:${PATH}"
fi

command -v go >/dev/null 2>&1 || {
  echo "go not found in PATH; please install Go >= 1.22 first" >&2
  exit 1
}

mkdir -p "${LOCALBIN}"

GOBIN="${LOCALBIN}" go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.16.5

go version
"${LOCALBIN}/controller-gen" --version

cat <<EOF
dev environment is ready
operator root: ${ROOT_DIR}
controller-gen: ${LOCALBIN}/controller-gen
EOF
