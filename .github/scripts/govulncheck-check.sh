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
# Stdlib findings follow the Go that builds this binary. CI pins
# actions/setup-go go-version 1.26.9: the 1.26.x floating spec still resolves
# to 1.26.8 in actions/go-versions, which is below the 2026-10-08 fixes.
# Local runs need Go >= 1.26.9 and < 1.27. Cache per Go version so an older
# toolchain cannot reuse a newer scanner, or the reverse.
go_ver="$(go env GOVERSION)"
bin="${cache}/govulncheck-${VERSION}-${go_ver}"

if [[ ! -x "$bin" ]]; then
  GOBIN="$cache" GOPROXY="${GOPROXY:-https://proxy.golang.org,direct}" \
    go install "golang.org/x/vuln/cmd/govulncheck@${VERSION}"
  mv "${cache}/govulncheck" "$bin"
fi

GOVULNCHECK_BIN="$bin" node "${ROOT}/.github/scripts/govulncheck-check.mjs"
