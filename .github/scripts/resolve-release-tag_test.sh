#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
resolver="${repo_root}/.github/scripts/resolve-release-tag.sh"

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

make_fixture() {
  local root="$1"
  mkdir -p "${root}/bin"
  cat > "${root}/bin/gh" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail

endpoint=""
for arg in "$@"; do
  if [[ "${arg}" == repos/* ]]; then
    endpoint="${arg}"
    break
  fi
done

case "${endpoint}" in
  repos/test/repo/releases\?per_page=100)
    printf '%s\n' "${MOCK_RELEASE_TAGS:-}"
    ;;
  repos/test/repo/releases/tags/*)
    tag="${endpoint##*/}"
    grep -Fxq "${tag}" <<< "${MOCK_RELEASE_TAGS:-}" || exit 1
    printf '%s\n' "${tag}"
    ;;
  repos/test/repo/commits/*)
    tag="${endpoint##*/}"
    awk -v tag="${tag}" '$1 == tag { print $2; found = 1 } END { exit !found }' <<< "${MOCK_TAG_COMMITS:-}"
    ;;
  *)
    echo "unexpected gh endpoint: ${endpoint}" >&2
    exit 1
    ;;
esac
STUB
  chmod +x "${root}/bin/gh"
}

run_resolver() {
  local root="$1"
  shift
  GITHUB_REPOSITORY=test/repo PATH="${root}/bin:${PATH}" "${resolver}" "$@"
}

test_resolves_manual_release_by_exact_head() {
  local root output
  root="$(mktemp -d)"
  trap 'rm -rf "${root}"' RETURN
  make_fixture "${root}"
  output="$(
    MOCK_RELEASE_TAGS=$'v1.7.0\nv1.8.0' \
    MOCK_TAG_COMMITS=$'v1.7.0 old-sha\nv1.8.0 release-sha' \
    run_resolver "${root}" workflow_dispatch main release-sha
  )"
  [[ "${output}" == "v1.8.0" ]] || fail "manual release resolved to ${output}"
}

test_preserves_tag_triggered_release() {
  local root output
  root="$(mktemp -d)"
  trap 'rm -rf "${root}"' RETURN
  make_fixture "${root}"
  output="$(
    MOCK_RELEASE_TAGS='v1.8.0' \
    MOCK_TAG_COMMITS='v1.8.0 release-sha' \
    run_resolver "${root}" push v1.8.0 release-sha
  )"
  [[ "${output}" == "v1.8.0" ]] || fail "tag-triggered release resolved to ${output}"
}

test_rejects_ambiguous_manual_release() {
  local root
  root="$(mktemp -d)"
  trap 'rm -rf "${root}"' RETURN
  make_fixture "${root}"
  if MOCK_RELEASE_TAGS=$'v1.8.0\nv1.8.1' \
    MOCK_TAG_COMMITS=$'v1.8.0 release-sha\nv1.8.1 release-sha' \
    run_resolver "${root}" workflow_dispatch main release-sha > "${root}/output" 2> "${root}/error"; then
    fail "ambiguous manual release resolved successfully"
  fi
  grep -Fq 'expected exactly one published release for release-sha, found 2' "${root}/error" || \
    fail "ambiguous release did not produce the expected error"
}

test_rejects_missing_manual_release() {
  local root
  root="$(mktemp -d)"
  trap 'rm -rf "${root}"' RETURN
  make_fixture "${root}"
  if MOCK_RELEASE_TAGS='v1.7.0' \
    MOCK_TAG_COMMITS='v1.7.0 old-sha' \
    run_resolver "${root}" workflow_dispatch main release-sha > "${root}/output" 2> "${root}/error"; then
    fail "missing manual release resolved successfully"
  fi
  grep -Fq 'expected exactly one published release for release-sha, found 0' "${root}/error" || \
    fail "missing release did not produce the expected error"
}

test_ignores_release_with_unresolvable_tag() {
  local root output
  root="$(mktemp -d)"
  trap 'rm -rf "${root}"' RETURN
  make_fixture "${root}"
  output="$(
    MOCK_RELEASE_TAGS=$'v0.1.0\nv1.8.0' \
    MOCK_TAG_COMMITS='v1.8.0 release-sha' \
    run_resolver "${root}" workflow_dispatch main release-sha 2> "${root}/error"
  )"
  [[ "${output}" == "v1.8.0" ]] || fail "manual release resolved to ${output} after stale tag"
  grep -Fq 'skipping published release v0.1.0: tag cannot be resolved' "${root}/error" || \
    fail "unresolvable release tag did not produce a warning"
}

test_manual_release_does_not_treat_tag_shaped_ref_as_tag() {
  local root output
  root="$(mktemp -d)"
  trap 'rm -rf "${root}"' RETURN
  make_fixture "${root}"
  output="$(
    MOCK_RELEASE_TAGS=$'v1.7.0\nv1.8.0' \
    MOCK_TAG_COMMITS=$'v1.7.0 dispatch-ref-sha\nv1.8.0 release-sha' \
    run_resolver "${root}" workflow_dispatch v1.7.0 release-sha
  )"
  [[ "${output}" == "v1.8.0" ]] || fail "manual release used tag-shaped dispatch ref: ${output}"
}

test_rejects_tag_head_mismatch() {
  local root
  root="$(mktemp -d)"
  trap 'rm -rf "${root}"' RETURN
  make_fixture "${root}"
  if MOCK_RELEASE_TAGS='v1.8.0' \
    MOCK_TAG_COMMITS='v1.8.0 other-sha' \
    run_resolver "${root}" push v1.8.0 release-sha > "${root}/output" 2> "${root}/error"; then
    fail "tag pointing to a different head resolved successfully"
  fi
  grep -Fq 'tag v1.8.0 points to other-sha, expected release-sha' "${root}/error" || \
    fail "tag mismatch did not produce the expected error"
}

test_rejects_non_tag_push() {
  local root
  root="$(mktemp -d)"
  trap 'rm -rf "${root}"' RETURN
  make_fixture "${root}"
  if MOCK_RELEASE_TAGS='main' \
    MOCK_TAG_COMMITS='main release-sha' \
    run_resolver "${root}" push main release-sha > "${root}/output" 2> "${root}/error"; then
    fail "non-tag push resolved successfully"
  fi
  grep -Fq 'release push branch main is not a version tag' "${root}/error" || \
    fail "non-tag push did not produce the expected error"
}

test_rejects_unsupported_release_event() {
  local root
  root="$(mktemp -d)"
  trap 'rm -rf "${root}"' RETURN
  make_fixture "${root}"
  if MOCK_RELEASE_TAGS='v1.8.0' \
    MOCK_TAG_COMMITS='v1.8.0 release-sha' \
    run_resolver "${root}" schedule main release-sha > "${root}/output" 2> "${root}/error"; then
    fail "unsupported release event resolved successfully"
  fi
  grep -Fq 'unsupported release event: schedule' "${root}/error" || \
    fail "unsupported event did not produce the expected error"
}

test_workflow_uses_resolver_without_changing_manual_recovery() {
  local workflow="${repo_root}/.github/workflows/release-taps.yml"
  grep -Fq 'actions/checkout@' "${workflow}" || fail "workflow does not check out the resolver"
  grep -Fq '.github/scripts/resolve-release-tag.sh' "${workflow}" || fail "workflow does not call the resolver"
  grep -Fq 'INPUT_TAG: ${{ inputs.tag }}' "${workflow}" || fail "manual recovery tag is not passed through the environment"
  grep -Fq 'HEAD_BRANCH: ${{ github.event.workflow_run.head_branch }}' "${workflow}" || fail "release branch is not passed through the environment"
  grep -Fq 'HEAD_SHA: ${{ github.event.workflow_run.head_sha }}' "${workflow}" || fail "release head is not passed through the environment"
  grep -Fq 'RELEASE_EVENT: ${{ github.event.workflow_run.event }}' "${workflow}" || fail "release event is not passed through the environment"
  grep -Fq 'echo "value=${INPUT_TAG}"' "${workflow}" || fail "manual recovery no longer uses its explicit tag input"
  if grep -Fq 'echo "value=${{ github.event.workflow_run.head_branch }}"' "${workflow}"; then
    fail "workflow still treats head_branch as the release tag"
  fi
}

test_resolves_manual_release_by_exact_head
test_preserves_tag_triggered_release
test_rejects_ambiguous_manual_release
test_rejects_missing_manual_release
test_ignores_release_with_unresolvable_tag
test_manual_release_does_not_treat_tag_shaped_ref_as_tag
test_rejects_tag_head_mismatch
test_rejects_non_tag_push
test_rejects_unsupported_release_event
test_workflow_uses_resolver_without_changing_manual_recovery
echo "release tag resolver tests passed"
