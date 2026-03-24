#!/usr/bin/env bash
# fuzz.sh — run a single fuzz target and tolerate the Go fuzz engine's known
# behaviour of reporting "context deadline exceeded" when -fuzztime expires.
#
# Usage: fuzz.sh -fuzz=FuzzFoo -fuzztime=30s ./path/to/pkg/
#
# A real fuzz finding (new failure corpus entry) produces distinct output that
# does not match the timeout pattern, so those are still surfaced as failures.
#
# Background: Go's fuzz engine uses a context deadline internally. When the
# deadline fires while a worker goroutine is mid-execution the engine can report
# "context deadline exceeded" and exit non-zero instead of exiting cleanly. This
# is a known Go fuzz engine behaviour and is not a test failure.

set -euo pipefail

FUZZ_OUT=$(mktemp)
trap 'rm -f "$FUZZ_OUT"' EXIT

set +e
go test "$@" 2>&1 | tee "$FUZZ_OUT"
EXIT=${PIPESTATUS[0]}
set -e

if [ $EXIT -eq 0 ]; then
  exit 0
fi

# A real finding prints "Failing input written to" and a corpus path.
if grep -q "Failing input written to" "$FUZZ_OUT"; then
  echo "::error::Fuzz test found a failure — see corpus entry above."
  exit $EXIT
fi

# The benign timeout pattern: only "context deadline exceeded" with no corpus
# entry. Treat as success.
if grep -q "context deadline exceeded" "$FUZZ_OUT"; then
  echo "Fuzz timed out cleanly (known Go fuzz engine behaviour — not a failure)."
  exit 0
fi

# Any other non-zero exit is a real error.
exit $EXIT
