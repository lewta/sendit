#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "$0")/../.." && pwd)"
script="$root/.github/scripts/validate-release-input.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $*" >&2; exit 1; }
[[ -f "$script" ]] || fail 'release authorization validator is missing'
git init -q --bare "$tmp/remote.git"
git init -q "$tmp/control"
cd "$tmp/control"
git config user.email test@example.invalid
git config user.name Test
git commit -qm initial --allow-empty
old="$(git rev-parse HEAD)"
git branch -M main
git commit -qm main --allow-empty
main="$(git rev-parse HEAD)"
git remote add origin "$tmp/remote.git"
git push -q origin main
git fetch -q origin
git checkout -qb outside "$old"
git commit -qm outside --allow-empty
outside="$(git rev-parse HEAD)"
git checkout -q main
mkdir "$tmp/bin"
export GITHUB_REPOSITORY=lewta/sendit GITHUB_REF=refs/heads/main GITHUB_REF_PROTECTED=true
export WORKFLOW_SHA="$main" RELEASE_TAG=v1.2.3 RELEASE_SHA="$old" GITHUB_OUTPUT="$tmp/output"
export MARKERS="$tmp/markers" PATH="$tmp/bin:$PATH" RELEASE_JSON='{"tag_name":"v1.2.3","draft":false,"published_at":"2026-10-01T00:00:00Z"}'
# Only the network boundary is faked. Ref lookup, commit/ancestry resolution,
# annotated tags, and races all use real repositories.
cat > "$tmp/bin/gh" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
if [[ "$*" == *'--method POST'* ]]; then
  echo tag >> "$MARKERS"
  if [[ "${RACE_SHA:-}" != '' ]]; then
    git --git-dir="$REMOTE" update-ref "refs/tags/$RELEASE_TAG" "$RACE_SHA"
    exit 1
  fi
  git --git-dir="$REMOTE" update-ref "refs/tags/$RELEASE_TAG" "$RELEASE_SHA"
else
  [[ "${API_ERROR:-false}" != true ]] || exit 1
  printf '%s\n' "$RELEASE_JSON"
fi
STUB
chmod +x "$tmp/bin/gh"
export REMOTE="$tmp/remote.git"
run() { : > "$GITHUB_OUTPUT"; bash "$script" "$@"; }
reject() {
  local label="$1"; shift
  rm -f "$MARKERS" "$tmp/authorized"
  if (run "$@" && printf 'publisher\ntoken\n' >> "$MARKERS") > "$tmp/log" 2>&1; then fail "$label accepted"; fi
  [[ ! -s "$GITHUB_OUTPUT" && ! -e "$MARKERS" ]] || fail "$label caused tag/publisher/token authorization"
  if [[ "$1" == validate ]]; then
    if (run ensure-tag && printf 'publisher\ntoken\n' >> "$MARKERS") > "$tmp/log" 2>&1; then fail "$label accepted before tag creation"; fi
    [[ ! -s "$GITHUB_OUTPUT" && ! -e "$MARKERS" ]] || fail "$label caused tag/publisher/token side effects"
  fi
  echo "PASS reject: $label"
}
run validate
grep -Fxq "sha=$old" "$GITHUB_OUTPUT" || fail 'immutable SHA output missing'
for tag in v1.2.3-rc.1+build.7 v0.0.0 v10.20.30+001; do RELEASE_TAG="$tag" run validate; done
for tag in '-v1.2.3' 'v01.2.3' 'v1.2.3-01' 'v1.2.3-rc..1' 'v1.2.3+' 'v1.2.3;touch x' $'v1.2.3\nsha=bad'; do RELEASE_TAG="$tag" reject "tag $tag" validate; done
RELEASE_SHA=main reject 'symbolic SHA' validate
RELEASE_SHA=deadbeef reject 'short SHA' validate
RELEASE_SHA=0000000000000000000000000000000000000000 reject 'missing object' validate
RELEASE_SHA="$outside" reject 'outside main' validate
tree="$(git rev-parse HEAD^{tree})"
RELEASE_SHA="$tree" reject 'non-commit SHA' validate
GITHUB_REPOSITORY=attacker/sendit reject 'wrong repository' validate
GITHUB_REF=refs/heads/feature reject 'wrong branch' validate
GITHUB_REF_PROTECTED=false reject 'unprotected main' validate
WORKFLOW_SHA="$old" reject 'stale workflow SHA' validate
WORKFLOW_SHA=main reject 'symbolic workflow SHA' validate
git update-ref refs/remotes/origin/main "$old"
reject 'stale origin main' validate
git update-ref refs/remotes/origin/main "$main"
git tag v1.2.3 "$main"
reject 'mismatching existing tag' validate
git tag -d v1.2.3 >/dev/null
git tag -a v1.2.3 "$old" -m release
run validate
git push -q origin refs/tags/v1.2.3
run recovery
API_ERROR=true reject 'release API failure' recovery
RELEASE_JSON='{"tag_name":"v9.9.9","draft":false,"published_at":"date"}' reject 'wrong published release' recovery
RELEASE_JSON='{"tag_name":"v1.2.3","draft":true,"published_at":null}' reject 'draft release' recovery
RELEASE_JSON='not-json' reject 'malformed release response' recovery
git --git-dir="$REMOTE" update-ref refs/tags/v1.2.3 "$main"
reject 'remote tag changed before recovery' recovery
reject 'remote tag changed before publisher' ensure-tag
git --git-dir="$REMOTE" update-ref -d refs/tags/v1.2.3
git tag -d v1.2.3 >/dev/null
reject 'missing recovery release tag' recovery
run ensure-tag
[[ "$(git --git-dir="$REMOTE" rev-parse refs/tags/v1.2.3)" == "$old" ]] || fail 'tag created at wrong SHA'
for race in "$old" "$main"; do
  git --git-dir="$REMOTE" update-ref -d refs/tags/v1.2.3
  git update-ref -d refs/tags/v1.2.3
  if RACE_SHA="$race" run ensure-tag > "$tmp/log" 2>&1; then
    [[ "$race" == "$old" ]] || fail 'mismatched concurrent tag accepted'
  else
    [[ "$race" != "$old" ]] || fail 'matching concurrent tag rejected'
    [[ ! -s "$GITHUB_OUTPUT" ]] || fail 'mismatched race authorized publisher'
  fi
done
git --git-dir="$REMOTE" update-ref refs/tags/v1.2.3 "$old"
git tag -f v1.2.3 "$old" >/dev/null
export CHECKSUM_FILE="$tmp/checksums.txt"
for platform in darwin_amd64.tar.gz darwin_arm64.tar.gz linux_amd64.tar.gz linux_arm64.tar.gz windows_amd64.zip windows_arm64.zip; do
  printf '%064d  sendit_1.2.3_%s\n' 1 "$platform"
done > "$tmp/valid-checksums"
cp "$tmp/valid-checksums" "$CHECKSUM_FILE"
run checksums
grep -Fxq 'windows_arm64=0000000000000000000000000000000000000000000000000000000000000001' "$GITHUB_OUTPUT" || fail 'validated checksum output absent'
# Preserve prerelease/build metadata throughout recovery and artifact matching.
git --git-dir="$REMOTE" update-ref refs/tags/v1.2.3-rc.1+build.7 "$old"
sed 's/sendit_1.2.3_/sendit_1.2.3-rc.1+build.7_/' "$tmp/valid-checksums" > "$CHECKSUM_FILE"
RELEASE_TAG=v1.2.3-rc.1+build.7 RELEASE_JSON='{"tag_name":"v1.2.3-rc.1+build.7","draft":false,"published_at":"date"}' run checksums
cat "$tmp/valid-checksums" "$tmp/valid-checksums" > "$CHECKSUM_FILE"
reject 'duplicate artifact' checksums
sed 's/darwin_amd64.tar.gz/darwin_amd64.tar.gz.extra/' "$tmp/valid-checksums" > "$CHECKSUM_FILE"
reject 'substring artifact match' checksums
sed 's/^0/xyz/' "$tmp/valid-checksums" > "$CHECKSUM_FILE"
reject 'nonhex checksum' checksums
sed 's/^0//' "$tmp/valid-checksums" > "$CHECKSUM_FILE"
reject 'short checksum' checksums
git remote set-url origin "$tmp/missing.git"
reject 'remote fetch error' validate
echo 'release authorization behavior tests passed'
python3 "$root/.github/scripts/verify-release-policy_test.py"
