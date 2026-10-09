#!/usr/bin/env bash
set -euo pipefail

release_event="${1:?usage: resolve-release-tag.sh RELEASE_EVENT HEAD_BRANCH HEAD_SHA}"
head_branch="${2:?usage: resolve-release-tag.sh RELEASE_EVENT HEAD_BRANCH HEAD_SHA}"
head_sha="${3:?usage: resolve-release-tag.sh RELEASE_EVENT HEAD_BRANCH HEAD_SHA}"
repository="${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}"

if [[ "${release_event}" == "push" ]]; then
  if [[ "${head_branch}" != v* ]]; then
    echo "release push branch ${head_branch} is not a version tag" >&2
    exit 1
  fi
  published_tag="$(gh api "repos/${repository}/releases/tags/${head_branch}" --jq .tag_name)"
  tag_sha="$(gh api "repos/${repository}/commits/${published_tag}" --jq .sha)"
  if [[ "${tag_sha}" != "${head_sha}" ]]; then
    echo "tag ${published_tag} points to ${tag_sha}, expected ${head_sha}" >&2
    exit 1
  fi
  printf '%s\n' "${published_tag}"
  exit 0
fi

if [[ "${release_event}" != "workflow_dispatch" ]]; then
  echo "unsupported release event: ${release_event}" >&2
  exit 1
fi

if ! published_tag_list="$(
  gh api --paginate "repos/${repository}/releases?per_page=100" \
    --jq '.[] | select(.draft == false) | .tag_name'
)"; then
  echo "failed to list published releases" >&2
  exit 1
fi

published_tags=()
while IFS= read -r tag; do
  [[ -n "${tag}" ]] && published_tags+=("${tag}")
done <<< "${published_tag_list}"

matches=()
api_error_file="$(mktemp)"
trap 'rm -f "${api_error_file}"' EXIT
for tag in "${published_tags[@]}"; do
  if ! tag_sha="$(gh api "repos/${repository}/commits/${tag}" --jq .sha 2> "${api_error_file}")"; then
    api_error="$(< "${api_error_file}")"
    if [[ "${api_error}" == *"(HTTP 404)"* ]]; then
      echo "skipping published release ${tag}: tag cannot be resolved" >&2
      continue
    fi
    echo "failed to resolve published release ${tag}: ${api_error}" >&2
    exit 1
  fi
  if [[ "${tag_sha}" == "${head_sha}" ]]; then
    matches+=("${tag}")
  fi
done

if [[ "${#matches[@]}" -ne 1 ]]; then
  echo "expected exactly one published release for ${head_sha}, found ${#matches[@]}" >&2
  if [[ "${#matches[@]}" -gt 0 ]]; then
    printf 'matching tags: %s\n' "${matches[*]}" >&2
  fi
  exit 1
fi

printf '%s\n' "${matches[0]}"
