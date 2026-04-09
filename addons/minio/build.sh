#!/usr/bin/env bash

set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VERSION="$(tr -d '\r\n' < "${ROOT_DIR}/VERSION")"
PAYLOAD_FILE="${ROOT_DIR}/payload.tar.gz"
TEMP_DIR="${ROOT_DIR}/.build-payload"
IMAGES_DIR="${ROOT_DIR}/images"
MANIFESTS_DIR="${ROOT_DIR}/manifests"
DIST_DIR="${ROOT_DIR}/dist"
IMAGE_JSON="${IMAGES_DIR}/image.json"

ARCH="amd64"
PLATFORM="linux/amd64"
BUILD_ALL="false"
INSTALLER_NAME=""

log() {
  printf '[INFO] %s\n' "$*"
}

die() {
  printf '[ERROR] %s\n' "$*" >&2
  exit 1
}

usage() {
  cat <<'EOF'
Usage:
  ./build.sh [--arch amd64|arm64|all]

Examples:
  ./build.sh --arch amd64
  ./build.sh --arch arm64
  ./build.sh --arch all
EOF
}

normalize_arch() {
  case "$1" in
    amd64|amd|x86_64)
      ARCH="amd64"
      PLATFORM="linux/amd64"
      BUILD_ALL="false"
      ;;
    arm64|arm|aarch64)
      ARCH="arm64"
      PLATFORM="linux/arm64"
      BUILD_ALL="false"
      ;;
    all)
      BUILD_ALL="true"
      ;;
    *)
      die "Unsupported arch: $1"
      ;;
  esac
  INSTALLER_NAME="dataprotection-addon-minio-${ARCH}.run"
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --arch|-a)
        [[ $# -ge 2 ]] || die "Missing value for $1"
        normalize_arch "$2"
        shift 2
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        die "Unknown argument: $1"
        ;;
    esac
  done
}

check_requirements() {
  command -v jq >/dev/null 2>&1 || die "jq is required"
  command -v docker >/dev/null 2>&1 || die "docker is required"
  [[ -f "${ROOT_DIR}/install.sh" ]] || die "install.sh is missing"
  [[ -d "${MANIFESTS_DIR}" ]] || die "manifests directory is missing"
  [[ -d "${IMAGES_DIR}" ]] || die "images directory is missing"
  [[ -f "${IMAGE_JSON}" ]] || die "images/image.json is missing"
  grep -q '^__PAYLOAD_BELOW__$' "${ROOT_DIR}/install.sh" || die "install.sh is missing __PAYLOAD_BELOW__ marker"
}

docker_buildx_available() {
  docker buildx version >/dev/null 2>&1
}

prepare_directories() {
  rm -rf "${TEMP_DIR}" "${PAYLOAD_FILE}"
  mkdir -p "${TEMP_DIR}/images" "${TEMP_DIR}/manifests" "${DIST_DIR}"
}

prepare_images() {
  local count=0
  while IFS= read -r item; do
    [[ -n "${item}" ]] || continue
    local pull tag tar_name platform dockerfile build_ref
    pull="$(jq -r '.pull // empty' <<<"${item}")"
    tag="$(jq -r '.tag // .pull' <<<"${item}")"
    tar_name="$(jq -r '.tar' <<<"${item}")"
    platform="$(jq -r '.platform // empty' <<<"${item}")"
    dockerfile="$(jq -r '.dockerfile // empty' <<<"${item}")"
    [[ -n "${platform}" ]] || platform="${PLATFORM}"
    build_ref="${tag}"

    if [[ -n "${dockerfile}" ]]; then
      log "Building ${build_ref} from ${dockerfile} for ${platform}"
      if docker_buildx_available; then
        docker buildx build --load --platform "${platform}" \
          --build-arg TARGETOS=linux \
          --build-arg TARGETARCH="${ARCH}" \
          -t "${build_ref}" -f "${ROOT_DIR}/${dockerfile}" "${ROOT_DIR}"
      elif [[ "${platform}" == "${PLATFORM}" ]]; then
        docker build \
          --build-arg TARGETOS=linux \
          --build-arg TARGETARCH="${ARCH}" \
          -t "${build_ref}" -f "${ROOT_DIR}/${dockerfile}" "${ROOT_DIR}"
      else
        die "docker buildx is required to build ${build_ref} for ${platform}"
      fi
    else
      log "Pulling ${pull} for ${platform}"
      docker pull --platform "${platform}" "${pull}"
      [[ "${pull}" == "${build_ref}" ]] || docker tag "${pull}" "${build_ref}"
    fi

    log "Saving ${build_ref} to ${tar_name}"
    docker save -o "${TEMP_DIR}/images/${tar_name}" "${build_ref}"
    count=$((count + 1))
  done < <(jq -c --arg arch "${ARCH}" '.[] | select(.arch == $arch)' "${IMAGE_JSON}")

  (( count > 0 )) || die "No image definitions found for arch=${ARCH}"
  cp "${IMAGE_JSON}" "${TEMP_DIR}/images/"
}

package_payload() {
  cp -r "${MANIFESTS_DIR}/"* "${TEMP_DIR}/manifests/"
  (
    cd "${TEMP_DIR}"
    tar -czf "${PAYLOAD_FILE}" .
  )
  tar -tzf "${PAYLOAD_FILE}" >/dev/null 2>&1 || die "Payload verification failed"
}

build_installer() {
  local installer_path="${DIST_DIR}/${INSTALLER_NAME}"
  cat "${ROOT_DIR}/install.sh" "${PAYLOAD_FILE}" > "${installer_path}"
  chmod +x "${installer_path}"
  sha256sum "${installer_path}" > "${installer_path}.sha256"
  log "Built ${installer_path}"
}

cleanup() {
  rm -rf "${TEMP_DIR}" "${PAYLOAD_FILE}" >/dev/null 2>&1 || true
}

build_one() {
  normalize_arch "$1"
  prepare_directories
  prepare_images
  package_payload
  build_installer
}

main() {
  trap cleanup EXIT
  normalize_arch "${ARCH}"
  parse_args "$@"
  check_requirements

  if [[ "${BUILD_ALL}" == "true" ]]; then
    build_one amd64
    build_one arm64
  else
    build_one "${ARCH}"
  fi
}

main "$@"
