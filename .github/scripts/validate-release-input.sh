#!/usr/bin/env bash
# Run from the trusted control checkout, never the candidate checkout.
set -euo pipefail
die() { echo "release authorization: $*" >&2; exit 1; }
mode="${1:-validate}"
case "$mode" in validate|ensure-tag|recovery|checksums) ;; *) die 'unknown mode' ;; esac
[[ "${GITHUB_REPOSITORY:-}" == lewta/sendit ]] || die 'unexpected repository'
[[ "${GITHUB_REF:-}" == refs/heads/main && "${GITHUB_REF_PROTECTED:-}" == true ]] || die 'protected main is required'
[[ "${WORKFLOW_SHA:-}" =~ ^[0-9a-f]{40}$ ]] || die 'full workflow SHA required'
[[ "$(git rev-parse HEAD)" == "$WORKFLOW_SHA" && "$(git rev-parse refs/remotes/origin/main)" == "$WORKFLOW_SHA" ]] || die 'workflow/control SHA must equal freshly fetched main'
export RELEASE_TAG RELEASE_SHA
python3 - <<'PY'
import os, re, sys
number = r'(?:0|[1-9][0-9]*)'
pre = r'(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)'
semver = rf'v{number}\.{number}\.{number}(?:-{pre}(?:\.{pre})*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?'
if not re.fullmatch(semver, os.environ.get('RELEASE_TAG', ''), flags=re.ASCII):
    sys.exit('release authorization: invalid SemVer tag')
PY
git check-ref-format "refs/tags/$RELEASE_TAG" || die 'invalid tag ref'
[[ "${RELEASE_SHA:-}" =~ ^[0-9a-f]{40}$ ]] || die 'full candidate SHA required'
[[ "$(git cat-file -t "$RELEASE_SHA")" == commit ]] || die 'candidate must be a commit object'
git merge-base --is-ancestor "$RELEASE_SHA" refs/remotes/origin/main || die 'candidate is outside approved main ancestry'
ref="refs/tags/$RELEASE_TAG"
if git show-ref --verify --quiet "$ref"; then
  [[ "$(git rev-parse "$ref^{commit}")" == "$RELEASE_SHA" ]] || die 'existing local tag differs from candidate'
fi
remote_tag() {
  local remote
  remote="$(git ls-remote --refs origin "$ref")" || die 'cannot read remote tag'
  if [[ -z "$remote" ]]; then return 1; fi
  [[ "$remote" != *$'\n'* && "${remote#*$'\t'}" == "$ref" ]] || die 'unexpected remote tag response'
  git fetch --quiet --no-tags origin "+$ref:refs/release-validation/tag" || die 'cannot fetch remote tag'
  [[ "$(git rev-parse refs/release-validation/tag^{commit})" == "$RELEASE_SHA" ]] || die 'remote tag differs from candidate'
}
exists=false
if remote_tag; then exists=true; fi
if [[ "$mode" == ensure-tag ]]; then
  if [[ "$exists" == false ]]; then
    # GitHub creates refs atomically; never update/force an existing tag.
    # A failed create can be a race: only the exact candidate may proceed.
    gh api --method POST "repos/$GITHUB_REPOSITORY/git/refs" \
      -f "ref=$ref" -f "sha=$RELEASE_SHA" >/dev/null || true
  fi
  remote_tag || die 'release tag does not exist after create'
elif [[ "$mode" == recovery || "$mode" == checksums ]]; then
  [[ "$exists" == true ]] || die 'recovery requires an existing release tag'
  release_json="$(gh api "repos/$GITHUB_REPOSITORY/releases/tags/$RELEASE_TAG")" || die 'cannot read published release'
  RELEASE_JSON="$release_json" python3 - <<'PY'
import json, os, sys
try:
    data = json.loads(os.environ['RELEASE_JSON'])
    valid = (data['tag_name'] == os.environ['RELEASE_TAG'] and
             data['draft'] is False and isinstance(data['published_at'], str) and
             bool(data['published_at']))
except (ValueError, KeyError, TypeError):
    valid = False
if not valid:
    sys.exit('release authorization: exact published release required')
PY
  # Re-resolve after the API lookup as well as before it.
  remote_tag || die 'published release tag disappeared'
fi
extra=''
if [[ "$mode" == checksums ]]; then
  extra="$(python3 - <<'PY'
import os, re, sys
from pathlib import Path
version = os.environ['RELEASE_TAG'][1:]
platforms = {'darwin_amd64': 'tar.gz', 'darwin_arm64': 'tar.gz',
             'linux_amd64': 'tar.gz', 'linux_arm64': 'tar.gz',
             'windows_amd64': 'zip', 'windows_arm64': 'zip'}
expected = {f'sendit_{version}_{key}.{ext}': key for key, ext in platforms.items()}
found = {}
for line in Path(os.environ['CHECKSUM_FILE']).read_text().splitlines():
    match = re.fullmatch(r'([0-9a-fA-F]{64}) [ *]([^\s]+)', line)
    if not match:
        sys.exit('release authorization: malformed checksum entry')
    digest, name = match.groups()
    if name in found:
        sys.exit('release authorization: duplicate checksum filename')
    found[name] = digest.lower()
if not expected.keys() <= found.keys():
    sys.exit('release authorization: missing exact artifact filename')
for name, key in expected.items():
    print(f'{key}={found[name]}')
PY
)" || die 'invalid release checksums'
fi
# Emit nothing until every requested gate has passed.
{
  printf 'tag=%s\nsha=%s\ncontrol_sha=%s\n' "$RELEASE_TAG" "$RELEASE_SHA" "$WORKFLOW_SHA"
  if [[ -n "$extra" ]]; then printf '%s\n' "$extra"; fi
} >> "${GITHUB_OUTPUT:?GITHUB_OUTPUT is required}"
