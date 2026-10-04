#!/usr/bin/env bash
# Error-level shellcheck for managed scripts.
# Paths: repo-root *.sh, server/scripts, scripts, sandbox-gateway, .github/scripts.
# Local command matches the always-on job in .github/workflows/ci.yml.
# Warnings and style notes do not fail. Inline exemptions must say why.
set -euo pipefail

VERSION="0.11.0"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

cache="${XDG_CACHE_HOME:-$HOME/.cache}/grasp-ci"
mkdir -p "$cache"
bin="${cache}/shellcheck-${VERSION}"

if [[ ! -x "$bin" ]]; then
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  machine="$(uname -m)"
  case "${os}-${machine}" in
    linux-x86_64 | linux-amd64) asset="linux.x86_64" ;;
    linux-aarch64 | linux-arm64) asset="linux.aarch64" ;;
    darwin-x86_64 | darwin-amd64) asset="darwin.x86_64" ;;
    darwin-arm64 | darwin-aarch64) asset="darwin.aarch64" ;;
    *)
      echo "shellcheck: unsupported platform ${os}-${machine}" >&2
      exit 1
      ;;
  esac
  tmp="$(mktemp -d)"
  url="https://github.com/koalaman/shellcheck/releases/download/v${VERSION}/shellcheck-v${VERSION}.${asset}.tar.xz"
  curl -fsSL "$url" | tar -xJ -C "$tmp"
  install -m 755 "${tmp}/shellcheck-v${VERSION}/shellcheck" "$bin"
  rm -rf "$tmp"
fi

files=()
while IFS= read -r path; do
  files+=("$path")
done < <(
  {
    find . -maxdepth 1 -type f -name '*.sh'
    find server/scripts scripts sandbox-gateway .github/scripts -type f -name '*.sh'
  } | sed 's#^\./##' | LC_ALL=C sort
)

if [[ "${#files[@]}" -eq 0 ]]; then
  echo "shellcheck-error: no managed scripts found" >&2
  exit 1
fi

"$bin" --severity=error "${files[@]}"
