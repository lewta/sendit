#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
verifier="${repo_root}/.github/scripts/verify-macos-release.sh"

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

make_fixture() {
  local root="$1"
  mkdir -p "${root}/bin" "${root}/payload/amd64" "${root}/payload/arm64" "${root}/release"
  printf '#!/usr/bin/env bash\nexit 0\n' > "${root}/payload/amd64/sendit"
  printf '#!/usr/bin/env bash\nexit 0\n' > "${root}/payload/arm64/sendit"
  chmod +x "${root}/payload/amd64/sendit" "${root}/payload/arm64/sendit"
  tar -czf "${root}/release/sendit_9.9.9_darwin_amd64.tar.gz" -C "${root}/payload/amd64" sendit
  tar -czf "${root}/release/sendit_9.9.9_darwin_arm64.tar.gz" -C "${root}/payload/arm64" sendit
  (
    cd "${root}/release"
    shasum -a 256 sendit_9.9.9_darwin_amd64.tar.gz sendit_9.9.9_darwin_arm64.tar.gz > checksums.txt
  )
  cat > "${root}/bin/codesign" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${CODESIGN_LOG}"
STUB
  chmod +x "${root}/bin/codesign"
}

test_verifies_both_archives_after_packaging() {
  local root
  root="$(mktemp -d)"
  trap 'rm -rf "${root}"' RETURN
  make_fixture "${root}"
  CODESIGN_LOG="${root}/codesign.log" PATH="${root}/bin:${PATH}" "${verifier}" "${root}/release"
  [[ "$(grep -c -- '--deep --strict' "${root}/codesign.log")" -eq 2 ]] || fail "expected strict signature verification for both architectures"
  [[ "$(grep -c -- '--check-notarization' "${root}/codesign.log")" -eq 2 ]] || fail "expected notarization verification for both architectures"
}

test_rejects_corrupted_archive() {
  local root
  root="$(mktemp -d)"
  trap 'rm -rf "${root}"' RETURN
  make_fixture "${root}"
  printf 'corrupt' >> "${root}/release/sendit_9.9.9_darwin_arm64.tar.gz"
  if CODESIGN_LOG="${root}/codesign.log" PATH="${root}/bin:${PATH}" "${verifier}" "${root}/release" > "${root}/corrupt.log" 2>&1; then
    fail "corrupted archive passed verification"
  fi
  grep -Fq 'FAILED' "${root}/corrupt.log" || fail "corrupted archive did not fail checksum validation"
}

test_requires_both_architectures() {
  local root
  root="$(mktemp -d)"
  trap 'rm -rf "${root}"' RETURN
  make_fixture "${root}"
  rm "${root}/release/sendit_9.9.9_darwin_arm64.tar.gz"
  if CODESIGN_LOG="${root}/codesign.log" PATH="${root}/bin:${PATH}" "${verifier}" "${root}/release" > "${root}/missing.log" 2>&1; then
    fail "missing arm64 archive passed verification"
  fi
  grep -Fq 'expected exactly one Darwin arm64 archive, found 0' "${root}/missing.log" || fail "missing archive did not produce the expected error"
}

test_requires_checksum_for_each_archive() {
  local root
  root="$(mktemp -d)"
  trap 'rm -rf "${root}"' RETURN
  make_fixture "${root}"
  grep -v 'darwin_arm64' "${root}/release/checksums.txt" > "${root}/release/checksums.filtered"
  mv "${root}/release/checksums.filtered" "${root}/release/checksums.txt"
  if CODESIGN_LOG="${root}/codesign.log" PATH="${root}/bin:${PATH}" "${verifier}" "${root}/release" > "${root}/checksum.log" 2>&1; then
    fail "archive without a checksum entry passed verification"
  fi
}

test_release_config_waits_and_runs_final_verifier() {
  grep -Eq '^[[:space:]]+wait:[[:space:]]+true$' "${repo_root}/.goreleaser.yaml" || fail "GoReleaser does not wait for notarization acceptance"
  grep -Eq '^[[:space:]]+verify-macos:' "${repo_root}/.github/workflows/release.yml" || fail "release workflow has no macOS verification job"
  grep -Fq '.github/scripts/verify-macos-release.sh' "${repo_root}/.github/workflows/release.yml" || fail "release workflow does not run the final artifact verifier"
}

test_verifies_both_archives_after_packaging
test_rejects_corrupted_archive
test_requires_both_architectures
test_requires_checksum_for_each_archive
test_release_config_waits_and_runs_final_verifier
echo "macOS release verifier tests passed"
