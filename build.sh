#!/usr/bin/env bash

set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VERSION="$(tr -d '\r\n' < "${ROOT_DIR}/VERSION")"
TEMP_DIR="${ROOT_DIR}/.build-payload"
PAYLOAD_FILE="${ROOT_DIR}/payload.tar.gz"
DIST_DIR="${ROOT_DIR}/dist"
IMAGES_DIR="${ROOT_DIR}/images"
MANIFESTS_DIR="${ROOT_DIR}/manifests"
IMAGE_JSON="${IMAGES_DIR}/image.json"
ASSEMBLER="${ROOT_DIR}/scripts/assemble-install.sh"

ARCH="amd64"
PLATFORM="linux/amd64"
BUILD_ALL_ARCH="false"

log() {
  printf '[INFO] %s\n' "$*"
}

die() {
  printf '[ERROR] %s\n' "$*" >&2
  exit 1
}

usage() {
  cat <<EOF
Usage:
  ./build.sh [--arch amd64|arm64|all]

Examples:
  ./build.sh --arch amd64
  ./build.sh --arch all
EOF
}

normalize_arch() {
  case "$1" in
    amd64|amd|x86_64)
      ARCH="amd64"
      PLATFORM="linux/amd64"
      BUILD_ALL_ARCH="false"
      ;;
    arm64|arm|aarch64)
      ARCH="arm64"
      PLATFORM="linux/arm64"
      BUILD_ALL_ARCH="false"
      ;;
    all)
      BUILD_ALL_ARCH="true"
      ;;
    *)
      die "Unsupported arch: $1"
      ;;
  esac
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
  command -v tar >/dev/null 2>&1 || die "tar is required"
  [[ -f "${ASSEMBLER}" ]] || die "scripts/assemble-install.sh is missing"
  [[ -f "${IMAGE_JSON}" ]] || die "images/image.json is missing"
  [[ -f "${MANIFESTS_DIR}/operator-install.yaml.tmpl" ]] || die "manifests/operator-install.yaml.tmpl is missing"
}

docker_buildx_available() {
  docker buildx version >/dev/null 2>&1
}

assemble_installer() {
  APP_VERSION="${VERSION}" bash "${ASSEMBLER}" "${ROOT_DIR}/install.sh"
}

prepare_directories() {
  rm -rf "${TEMP_DIR}" "${PAYLOAD_FILE}"
  mkdir -p "${TEMP_DIR}/images" "${TEMP_DIR}/manifests/crds" "${DIST_DIR}"
}

prepare_manifests() {
  make -C "${ROOT_DIR}" manifests
  cp "${MANIFESTS_DIR}/operator-install.yaml.tmpl" "${TEMP_DIR}/manifests/"
  cp "${ROOT_DIR}"/config/crd/bases/*.yaml "${TEMP_DIR}/manifests/crds/"
}

prepare_images() {
  local count=0
  while IFS= read -r item; do
    [[ -n "${item}" ]] || continue
    local name tag tar_name pull dockerfile platform build_ref
    name="$(jq -r '.name' <<<"${item}")"
    tag="$(jq -r '.tag' <<<"${item}")"
    tar_name="$(jq -r '.tar' <<<"${item}")"
    pull="$(jq -r '.pull // empty' <<<"${item}")"
    dockerfile="$(jq -r '.dockerfile // empty' <<<"${item}")"
    platform="$(jq -r '.platform // empty' <<<"${item}")"
    [[ -n "${platform}" ]] || platform="${PLATFORM}"
    if [[ "${name}" == "dataprotection-operator" ]]; then
      tag="sealos.hub:5000/kube4/dataprotection-operator:${VERSION}-${ARCH}"
      item="$(jq -c --arg tag "${tag}" '.tag = $tag' <<<"${item}")"
    fi
    build_ref="${tag}"

    if [[ -n "${dockerfile}" ]]; then
      log "Building ${name}:${tag} for ${platform}"
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
        die "docker buildx is required to build ${name} for ${platform}"
      fi
    else
      log "Pulling ${pull} for ${platform}"
      if docker_buildx_available; then
        docker pull --platform "${platform}" "${pull}"
      elif [[ "${platform}" == "${PLATFORM}" ]]; then
        docker pull "${pull}"
      else
        die "docker buildx is required to pull ${pull} for ${platform}"
      fi
      docker tag "${pull}" "${build_ref}"
    fi

    log "Saving ${build_ref} to ${tar_name}"
    docker save -o "${TEMP_DIR}/images/${tar_name}" "${build_ref}"
    jq -c . <<<"${item}" >> "${TEMP_DIR}/images/image.jsonl"
    count=$((count + 1))
  done < <(jq -c --arg arch "${ARCH}" '.[] | select(.arch == $arch)' "${IMAGE_JSON}")

  (( count > 0 )) || die "No image definition found for arch=${ARCH}"
  jq -s '.' "${TEMP_DIR}/images/image.jsonl" > "${TEMP_DIR}/images/image.json"
  rm -f "${TEMP_DIR}/images/image.jsonl"
}

package_payload() {
  (
    cd "${TEMP_DIR}"
    tar -czf "${PAYLOAD_FILE}" .
  )
}

build_installer() {
  local installer_path="${DIST_DIR}/data-protection-operator-${ARCH}.run"
  local marker_line payload_offset skip_bytes first_bytes byte_hex
  cat "${ROOT_DIR}/install.sh" "${PAYLOAD_FILE}" > "${installer_path}"
  chmod +x "${installer_path}"
  marker_line="$(awk '/^__PAYLOAD_BELOW__$/ { print NR; exit 0; }' "${installer_path}")"
  [[ -n "${marker_line}" ]] || die "Installer payload marker not found in ${installer_path}"

  payload_offset="$(( $(head -n "${marker_line}" "${installer_path}" | wc -c | tr -d ' ') + 1 ))"
  skip_bytes=0
  while :; do
    byte_hex="$(dd if="${installer_path}" bs=1 skip="$((payload_offset + skip_bytes - 1))" count=1 2>/dev/null | od -An -tx1 | tr -d ' \n')"
    case "${byte_hex}" in
      0a|0d)
        skip_bytes=$((skip_bytes + 1))
        ;;
      "")
        die "Installer payload verification failed for ${installer_path}: payload is empty"
        ;;
      *)
        break
        ;;
    esac
  done

  first_bytes="$(dd if="${installer_path}" bs=1 skip="$((payload_offset + skip_bytes - 1))" count=2 2>/dev/null | od -An -tx1 | tr -d ' \n')"
  [[ "${first_bytes}" == "1f8b" ]] || die "Installer payload verification failed for ${installer_path}: expected gzip header, got ${first_bytes:-<empty>}"
  sha256sum "${installer_path}" > "${installer_path}.sha256"
  log "Built ${installer_path}"
}

cleanup() {
  rm -rf "${TEMP_DIR}" "${PAYLOAD_FILE}" >/dev/null 2>&1 || true
}

build_one() {
  normalize_arch "$1"
  prepare_directories
  prepare_manifests
  prepare_images
  package_payload
  build_installer
}

build_matrix() {
  local arch
  if [[ "${BUILD_ALL_ARCH}" == "true" ]]; then
    for arch in amd64 arm64; do
      build_one "${arch}"
    done
    return
  fi
  build_one "${ARCH}"
}

main() {
  trap cleanup EXIT
  parse_args "$@"
  check_requirements
  assemble_installer
  build_matrix
}

main "$@"
