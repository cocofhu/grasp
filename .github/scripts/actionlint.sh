#!/usr/bin/env bash
# Pinned actionlint for every file under .github/workflows.
# Local command matches the always-on job in .github/workflows/ci.yml.
set -euo pipefail

VERSION="1.7.12"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

cache="${XDG_CACHE_HOME:-$HOME/.cache}/grasp-ci"
mkdir -p "$cache"
bin="${cache}/actionlint-${VERSION}"

if [[ ! -x "$bin" ]]; then
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  machine="$(uname -m)"
  case "$machine" in
    x86_64 | amd64) arch="amd64" ;;
    aarch64 | arm64) arch="arm64" ;;
    *)
      echo "actionlint: unsupported architecture ${machine}" >&2
      exit 1
      ;;
  esac
  tmp="$(mktemp -d)"
  url="https://github.com/rhysd/actionlint/releases/download/v${VERSION}/actionlint_${VERSION}_${os}_${arch}.tar.gz"
  curl -fsSL "$url" | tar -xz -C "$tmp" actionlint
  install -m 755 "${tmp}/actionlint" "$bin"
  rm -rf "$tmp"
fi

"$bin"
