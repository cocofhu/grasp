#!/usr/bin/env bash
# Pin govulncheck, then apply govulncheck-allowlist.json.
# Local command matches the govulncheck job in .github/workflows/security.yml.
# Does not read Actions secrets.
set -euo pipefail

VERSION="v1.1.4"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

cache="${XDG_CACHE_HOME:-$HOME/.cache}/grasp-ci"
mkdir -p "$cache"
# Stdlib findings follow the Go that builds this binary. CI uses
# actions/setup-go go-version 1.25.x (latest patch). Cache per Go version so
# an older toolchain cannot reuse a newer scanner, or the reverse.
go_ver="$(go env GOVERSION)"
bin="${cache}/govulncheck-${VERSION}-${go_ver}"

if [[ ! -x "$bin" ]]; then
  GOBIN="$cache" GOPROXY="${GOPROXY:-https://proxy.golang.org,direct}" \
    go install "golang.org/x/vuln/cmd/govulncheck@${VERSION}"
  mv "${cache}/govulncheck" "$bin"
fi

GOVULNCHECK_BIN="$bin" node "${ROOT}/.github/scripts/govulncheck-check.mjs"
