#!/usr/bin/env bash
# Retry an explicitly selected network operation, never a test or quality gate.
# Preserve stdout and the final exit status; diagnostics do not echo arguments
# because a caller may pass credentials in them.
set -euo pipefail

if [[ $# -eq 0 ]]; then
  echo "usage: retry-network.sh command [args...]" >&2
  exit 64
fi

for attempt in 1 2 3; do
  if "$@"; then
    exit 0
  else
    status=$?
  fi
  if [[ "$attempt" -eq 3 ]]; then
    exit "$status"
  fi
  echo "network operation failed (exit ${status}, attempt ${attempt}/3); retrying" >&2
  sleep "$((attempt * 2))"
done
