#!/usr/bin/env bash
set -euo pipefail

release_dir="${1:?usage: verify-macos-release.sh RELEASE_DIRECTORY}"
checksums="${release_dir}/checksums.txt"

[[ -f "${checksums}" ]] || {
  echo "missing checksums.txt in ${release_dir}" >&2
  exit 1
}

shopt -s nullglob
amd64_archives=("${release_dir}"/sendit_*_darwin_amd64.tar.gz)
arm64_archives=("${release_dir}"/sendit_*_darwin_arm64.tar.gz)
[[ "${#amd64_archives[@]}" -eq 1 ]] || {
  echo "expected exactly one Darwin amd64 archive, found ${#amd64_archives[@]}" >&2
  exit 1
}
[[ "${#arm64_archives[@]}" -eq 1 ]] || {
  echo "expected exactly one Darwin arm64 archive, found ${#arm64_archives[@]}" >&2
  exit 1
}

verify_checksum() {
  local archive="$1"
  local filename
  local checksum_line
  filename="$(basename "${archive}")"
  checksum_line="$(awk -v filename="${filename}" '$2 == filename { print }' "${checksums}")"
  [[ -n "${checksum_line}" ]] || {
    echo "checksums.txt has no entry for ${filename}" >&2
    exit 1
  }
  (
    cd "${release_dir}"
    printf '%s\n' "${checksum_line}" | shasum -a 256 --check --strict -
  )
}

verify_checksum "${amd64_archives[0]}"
verify_checksum "${arm64_archives[0]}"

work_dir="$(mktemp -d)"
trap 'rm -rf "${work_dir}"' EXIT

verify_archive() {
  local architecture="$1"
  local archive="$2"
  local destination="${work_dir}/${architecture}"
  local binary="${destination}/sendit"

  mkdir -p "${destination}"
  tar -xzf "${archive}" -C "${destination}"
  [[ -x "${binary}" ]] || {
    echo "archive $(basename "${archive}") does not contain an executable sendit binary" >&2
    exit 1
  }

  codesign --verify --deep --strict --verbose=4 "${binary}"
  codesign --verify --check-notarization --verbose=4 "${binary}"
}

verify_archive amd64 "${amd64_archives[0]}"
verify_archive arm64 "${arm64_archives[0]}"
