#!/usr/bin/env bash
# Builds the sandbox runtime bundle Grasp serves to every sandbox: the Go
# programs (backend / ACP bridge, preview-inject), the bridge web assets and the
# startup / service scripts. The sandbox image only ships the environment plus
# /grasp-bootstrap.sh, which fetches and installs this bundle.
#
# Usage: scripts/build-sandbox-runtime.sh [OUT_DIR]
#   OUT_DIR defaults to .devdata/sandbox-runtime; writes sandbox-runtime.tgz
#   there and prints the bundle version on stdout.
# Env: SANDBOX_SRC (default sandbox-gateway/sandbox), GOARCH (default amd64).
#
# The output is reproducible: same sources + same Go toolchain => same bytes.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="${1:-$ROOT/.devdata/sandbox-runtime}"
SRC="${SANDBOX_SRC:-$ROOT/sandbox-gateway/sandbox}"
ARCH="${GOARCH:-amd64}"
SCRIPTS=(startup.sh preview-inject.sh vnc-preview.sh services.sh claude-env.sh)

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT

mkdir -p "$STAGE/bin" "$STAGE/share/backend" "$STAGE/scripts"
(
  cd "$SRC"
  export CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH"
  go build -trimpath -buildvcs=false -ldflags="-s -w" -o "$STAGE/bin/backend" ./cmd/backend
  go build -trimpath -buildvcs=false -ldflags="-s -w" -o "$STAGE/bin/preview-inject" ./cmd/preview-inject
)
cp -R "$SRC/web" "$STAGE/share/backend/web"
for s in "${SCRIPTS[@]}"; do
  install -m 0755 "$SRC/scripts/$s" "$STAGE/scripts/$s"
done
# 00755 also clears setgid inherited from the temp dir's parent.
find "$STAGE" -type d -exec chmod 00755 {} +
find "$STAGE/share" -type f -exec chmod 0644 {} +

# Keep runtime_digest byte-identical with sandbox-gateway/sandbox/scripts/grasp-bootstrap.sh.
runtime_digest() {
  (
    cd "$1"
    find . -type f ! -path ./MANIFEST -print0 | LC_ALL=C sort -z | xargs -0 sha256sum
    find . -type f -perm -u+x ! -path ./MANIFEST | LC_ALL=C sort
  ) | sha256sum | cut -c1-64
}

min_image="$(tr -d '[:space:]' <"$SRC/IMAGE_LEVEL")"
version="$(runtime_digest "$STAGE")"
printf 'version=%s\narch=%s\nmin_image=%s\n' "$version" "$ARCH" "$min_image" >"$STAGE/MANIFEST"
chmod 0644 "$STAGE/MANIFEST"

mkdir -p "$OUT"
tmp="$OUT/.sandbox-runtime.tgz.$$"
tar --sort=name --mtime=@0 --owner=0 --group=0 --numeric-owner -C "$STAGE" -cf - . | gzip -n -9 >"$tmp"
mv -f "$tmp" "$OUT/sandbox-runtime.tgz"
echo "$version"
