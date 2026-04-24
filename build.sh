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
  command -v docker >/dev/null 2>&1 || die "docker is required"
  command -v python >/dev/null 2>&1 || command -v python3 >/dev/null 2>&1 || die "python or python3 is required"
  command -v tar >/dev/null 2>&1 || die "tar is required"
  [[ -f "${ASSEMBLER}" ]] || die "scripts/assemble-install.sh is missing"
  [[ -f "${IMAGE_JSON}" ]] || die "images/image.json is missing"
  [[ -f "${MANIFESTS_DIR}/operator-install.yaml.tmpl" ]] || die "manifests/operator-install.yaml.tmpl is missing"
}

python_cmd() {
  if command -v python >/dev/null 2>&1; then
    printf 'python'
  else
    printf 'python3'
  fi
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

write_image_metadata() {
  local arch="$1"
  local output_json="$2"
  local output_index="$3"
  "$(python_cmd)" - "${IMAGE_JSON}" "${arch}" "${VERSION}" "${output_json}" "${output_index}" <<'PY'
import json
import sys

source_path, arch, version, output_json, output_index = sys.argv[1:]

with open(source_path, "r", encoding="utf-8") as fh:
    items = json.load(fh)

selected = []
for original in items:
    if original.get("arch") != arch:
        continue
    item = dict(original)
    if item.get("name") == "dataprotection-operator":
        item["tag"] = f"sealos.hub:5000/kube4/dataprotection-operator:{version}-{arch}"
    selected.append(item)

if not selected:
    raise SystemExit(f"no image definition found for arch={arch}")

with open(output_json, "w", encoding="utf-8") as fh:
    json.dump(selected, fh, ensure_ascii=False, indent=2)
    fh.write("\n")

with open(output_index, "w", encoding="utf-8", newline="") as fh:
    for item in selected:
        default_target_ref = item.get("tag") or item.get("pull") or ""
        fh.write("\t".join([
            item.get("name", ""),
            item.get("tar", ""),
            default_target_ref,
            default_target_ref,
            item.get("platform", ""),
            item.get("pull", ""),
            item.get("dockerfile", ""),
        ]) + "\n")
PY
}

prepare_images() {
  local count=0
  local payload_image_json="${TEMP_DIR}/images/image.json"
  local payload_image_index="${TEMP_DIR}/images/image-index.tsv"

  write_image_metadata "${ARCH}" "${payload_image_json}" "${payload_image_index}"

  while IFS=$'\t' read -r name tar_name load_ref default_target_ref platform pull dockerfile; do
    [[ -n "${name}" ]] || continue
    local build_ref
    [[ -n "${platform}" ]] || platform="${PLATFORM}"
    build_ref="${default_target_ref}"

    if [[ -n "${dockerfile}" ]]; then
      log "Building ${build_ref} for ${platform}"
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
      [[ "${pull}" == "${build_ref}" ]] || docker tag "${pull}" "${build_ref}"
    fi

    log "Saving ${build_ref} to ${tar_name}"
    docker save -o "${TEMP_DIR}/images/${tar_name}" "${build_ref}"
    count=$((count + 1))
  done < "${payload_image_index}"

  (( count > 0 )) || die "No image definition found for arch=${ARCH}"
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
