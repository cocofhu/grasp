#!/usr/bin/env bash
# Server coverage gate: core unit-testable packages
# (auth/apikey / config / crypto / textutil / logging / nodereg / models / shutdown / router / mcp / chatsession).
# Excludes handlers/services/engine/sandbox/runtime/database/browser/channels/cmd
# (integration/IO surfaces covered via go test ./... and e2e subsets).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
MIN="${1:-90}"
cd "$ROOT"
COVER="$(mktemp)"; trap 'rm -f "$COVER"' EXIT
PKGS=(
  ./internal/auth/...
  ./internal/config
  ./internal/crypto
  ./internal/textutil
  ./internal/version
  ./internal/logging
  ./internal/nodereg
  ./internal/models/...
  ./internal/shutdown
  ./internal/router
  ./internal/mcp/...
  ./internal/chatsession
)
LIST="$(go list "${PKGS[@]}" | paste -sd, -)"
go test "${PKGS[@]}" -count=1 -timeout 20m -coverpkg="$LIST" -coverprofile="$COVER"
TOTAL="$(go tool cover -func="$COVER" | awk '/^total:/{gsub(/%/,"",$NF); print $NF}')"
if [[ -z "${TOTAL}" ]]; then
  echo "FAIL: unable to parse total coverage from go tool cover" >&2
  exit 1
fi
echo "server-core coverage=${TOTAL}% (min=${MIN}%)"
# Export for CI badge publishing (does not affect the gate below).
if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
  echo "coverage_pct=${TOTAL}" >>"$GITHUB_OUTPUT"
fi
if [[ -n "${COVER_CHECK_OUT:-}" ]]; then
  printf '%s\n' "$TOTAL" >"$COVER_CHECK_OUT"
fi
awk -v t="$TOTAL" -v m="$MIN" 'BEGIN{ if ((t+0)<(m+0)) { print "FAIL"; exit 1 } print "OK" }'
