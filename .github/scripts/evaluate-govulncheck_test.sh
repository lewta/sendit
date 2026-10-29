#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
"${PYTHON:-python3}" -B -m unittest discover -s .github/scripts -p 'security_evidence_test.py' -v
