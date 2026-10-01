#!/bin/bash
# Sandbox image entrypoint. The image only provides the environment; Grasp's
# scripts and programs come as a runtime bundle served by the Grasp server
# (built by scripts/build-sandbox-runtime.sh). This script installs a bundle
# under /opt/grasp-runtime/<version>, points /opt/grasp-runtime/current at it
# and, on container start, hands off to its startup.sh.
#
# Subcommands and exit codes are a contract with the Grasp server; changing
# them requires bumping sandbox-gateway/sandbox/IMAGE_LEVEL.
#   fetch          download GRASP_RUNTIME_URL (headers: SANDBOX_INJECT_HEADERS),
#                  install it, exec current/scripts/startup.sh
#   install-stdin  install a bundle read from stdin, print its version
#   version        print the installed (current) version
# Exit: 0 ok · 1 failed · 2 usage · 4 bundle needs a newer image (min_image)
set -euo pipefail

ROOT="${GRASP_RUNTIME_ROOT:-/opt/grasp-runtime}"
LEVEL_FILE="${GRASP_IMAGE_LEVEL_FILE:-/etc/grasp-image-level}"

log() { echo "grasp-bootstrap: $*" >&2; }

# Keep runtime_digest byte-identical with scripts/build-sandbox-runtime.sh.
runtime_digest() {
  (
    cd "$1"
    find . -type f ! -path ./MANIFEST -print0 | LC_ALL=C sort -z | xargs -0 sha256sum
    find . -type f -perm -u+x ! -path ./MANIFEST | LC_ALL=C sort
  ) | sha256sum | cut -c1-64
}

manifest_get() { sed -n "s/^$2=//p" "$1/MANIFEST" | head -n 1; }

machine_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo amd64 ;;
    aarch64|arm64) echo arm64 ;;
    *) uname -m ;;
  esac
}

# install_tgz FILE: prints the installed version; returns 4 when the image is too old.
install_tgz() {
  local tgz="$1" tmp version arch min level prev
  mkdir -p "$ROOT"
  tmp="$(mktemp -d "$ROOT/.incoming.XXXXXX")"
  if ! tar -xzf "$tgz" -C "$tmp" 2>/dev/null || [ ! -f "$tmp/MANIFEST" ]; then
    rm -rf "$tmp"
    log "运行时包无效（不是 tgz 或缺少 MANIFEST）"
    return 1
  fi
  version="$(manifest_get "$tmp" version)"
  arch="$(manifest_get "$tmp" arch)"
  min="$(manifest_get "$tmp" min_image)"
  level="$(tr -d '[:space:]' <"$LEVEL_FILE" 2>/dev/null || true)"
  [[ "$level" =~ ^[0-9]+$ ]] || level=0
  if ! [[ "$min" =~ ^[0-9]+$ ]] || [ "$level" -lt "$min" ]; then
    rm -rf "$tmp"
    log "运行时包需要镜像版本 ≥ ${min:-?}，当前是 ${level}，请重建沙箱镜像"
    return 4
  fi
  if [ "$arch" != "$(machine_arch)" ]; then
    rm -rf "$tmp"
    log "运行时包架构 ${arch:-?} 与本机 $(machine_arch) 不符"
    return 1
  fi
  if ! [[ "$version" =~ ^[0-9a-f]{64}$ ]] || [ "$(runtime_digest "$tmp")" != "$version" ]; then
    rm -rf "$tmp"
    log "运行时包内容与 MANIFEST 版本不符"
    return 1
  fi
  if [ -d "$ROOT/$version" ]; then
    rm -rf "$tmp"
  else
    mv "$tmp" "$ROOT/$version"
  fi
  prev="$(readlink "$ROOT/current" 2>/dev/null || true)"
  ln -sfn "$version" "$ROOT/.current.tmp"
  mv -Tf "$ROOT/.current.tmp" "$ROOT/current"
  local d name
  for d in "$ROOT"/*/; do
    name="$(basename "$d")"
    [ "$name" = "$version" ] || [ "$name" = "$prev" ] || [ "$name" = current ] || rm -rf "$d"
  done
  echo "$version"
}

fetch() {
  local url="${GRASP_RUNTIME_URL:-}" delay=1 attempt rc=0 h
  local attempts="${GRASP_BOOTSTRAP_ATTEMPTS:-5}"
  if [ -z "$url" ]; then
    log "GRASP_RUNTIME_URL 未设置：这个镜像需要 Grasp 下发运行时包，请通过 Grasp 创建沙箱"
    exit 1
  fi
  FETCH_CFG="$(mktemp)"
  FETCH_TGZ="$(mktemp)"
  local cfg="$FETCH_CFG" tgz="$FETCH_TGZ"
  chmod 600 "$cfg"
  trap 'rm -f "$FETCH_CFG" "$FETCH_TGZ"' EXIT
  # URL and headers go through a curl config file so tokens stay out of argv.
  printf 'url = "%s"\n' "$url" >"$cfg"
  while IFS= read -r h; do
    [ -n "$h" ] && printf 'header = "%s"\n' "$h" >>"$cfg"
  done <<<"${SANDBOX_INJECT_HEADERS:-}"
  log "下载运行时包 ${url%%\?*}"
  for ((attempt = 1; attempt <= attempts; attempt++)); do
    if curl -fsSL --connect-timeout 10 --max-time 300 -K "$cfg" -o "$tgz"; then
      rc=0
      break
    fi
    rc=1
    [ "$attempt" -ge "$attempts" ] || { log "下载失败，${delay}s 后重试（${attempt}/${attempts}）"; sleep "$delay"; }
    delay=$((delay * 2))
  done
  if [ "$rc" != 0 ]; then
    log "运行时包下载失败"
    exit 1
  fi
  local version
  version="$(install_tgz "$tgz")" || exit $?
  log "运行时 ${version:0:12} 已安装"
  rm -f "$cfg" "$tgz"
  trap - EXIT
  [ "${GRASP_BOOTSTRAP_NO_EXEC:-}" = "1" ] && exit 0
  exec "$ROOT/current/scripts/startup.sh"
}

install_stdin() {
  STDIN_TGZ="$(mktemp)"
  trap 'rm -f "$STDIN_TGZ"' EXIT
  cat >"$STDIN_TGZ"
  install_tgz "$STDIN_TGZ"
}

version() {
  if [ ! -f "$ROOT/current/MANIFEST" ]; then
    log "尚未安装运行时包"
    exit 1
  fi
  manifest_get "$ROOT/current" version
}

case "${1:-}" in
  fetch) fetch ;;
  install-stdin) install_stdin ;;
  version) version ;;
  *)
    echo "usage: grasp-bootstrap.sh <fetch|install-stdin|version>" >&2
    exit 2
    ;;
esac
