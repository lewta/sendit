#!/usr/bin/env bash
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"
GOPROXY=off GOSUMDB=off "${GO:-go}" test -count=1 -v ./docker
