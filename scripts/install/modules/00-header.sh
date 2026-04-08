#!/usr/bin/env bash

# Generated source layout:
# - edit scripts/install/modules/*.sh
# - regenerate install.sh via scripts/assemble-install.sh

set -Eeuo pipefail

APP_NAME="data-protection-operator"
APP_VERSION="${APP_VERSION:-0.2.3}"
WORKDIR="/tmp/${APP_NAME}-installer"
IMAGE_DIR="${WORKDIR}/images"
MANIFEST_DIR="${WORKDIR}/manifests"
CRD_DIR="${MANIFEST_DIR}/crds"
IMAGE_JSON="${IMAGE_DIR}/image.json"
INSTALL_TEMPLATE="${MANIFEST_DIR}/operator-install.yaml.tmpl"
PAYLOAD_MARKER="__PAYLOAD_BELOW__"

ACTION="install"
NAMESPACE="data-protection-system"
DEFAULT_REGISTRY="sealos.hub:5000/kube4"
REGISTRY="${REGISTRY:-${DEFAULT_REGISTRY}}"
REGISTRY_USER="${REGISTRY_USER:-}"
REGISTRY_PASSWORD="${REGISTRY_PASSWORD:-}"
IMAGE_PULL_POLICY="Always"
WAIT_TIMEOUT="5m"
AUTO_YES="false"
DELETE_CRDS="false"
SKIP_IMAGE_PREPARE="false"

OPERATOR_IMAGE_OVERRIDE=""
MYSQL_RUNNER_IMAGE_OVERRIDE=""
REDIS_RUNNER_IMAGE_OVERRIDE=""
MINIO_RUNNER_IMAGE_OVERRIDE=""
S3_HELPER_IMAGE_OVERRIDE=""
PLACEHOLDER_RUNNER_IMAGE_OVERRIDE=""

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

log() {
  echo -e "${CYAN}[INFO]${NC} $*"
}

success() {
  echo -e "${GREEN}[OK]${NC} $*"
}

warn() {
  echo -e "${YELLOW}[WARN]${NC} $*" >&2
}

die() {
  echo -e "${RED}[ERROR]${NC} $*" >&2
  exit 1
}

section() {
  echo
  echo -e "${BLUE}${BOLD}============================================================${NC}"
  echo -e "${BLUE}${BOLD}$*${NC}"
  echo -e "${BLUE}${BOLD}============================================================${NC}"
}
