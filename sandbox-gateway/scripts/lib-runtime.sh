# shellcheck shell=bash
# Sourced by the sandbox image E2E scripts. The image only starts with a runtime
# bundle served the way the Grasp server does it; these helpers stand in for the
# server (scripts/runtime-server.sh) and derive test bundles without Go.

_lib_runtime_here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
_lib_runtime_repo="$(cd "$_lib_runtime_here/../.." && pwd)"
RUNTIME_SERVER_PIDS=()

# Same digest as scripts/build-sandbox-runtime.sh and grasp-bootstrap.sh.
eval "$(sed -n '/^runtime_digest() {/,/^}/p' "$_lib_runtime_repo/scripts/build-sandbox-runtime.sh")"

runtime_free_port() {
  python3 -c 'import socket; s=socket.socket(); s.bind(("0.0.0.0",0)); print(s.getsockname()[1])'
}

# start_runtime_server BUNDLE: serves BUNDLE behind a fresh Bearer token and
# sets RUNTIME_TOKEN plus RUNTIME_DOCKER_ARGS (docker run flags for the URL and
# header env a Grasp-created sandbox gets).
start_runtime_server() {
  local bundle="$1" port i
  port="$(runtime_free_port)"
  RUNTIME_TOKEN="ci-$(od -An -tx1 -N16 /dev/urandom | tr -d ' \n')"
  bash "$_lib_runtime_here/runtime-server.sh" "$bundle" "$port" "$RUNTIME_TOKEN" >/dev/null 2>>"${RUNTIME_SERVER_LOG:-/dev/null}" &
  RUNTIME_SERVER_PIDS+=("$!")
  for i in $(seq 1 50); do
    (echo >/dev/tcp/127.0.0.1/"$port") >/dev/null 2>&1 && break
    sleep 0.1
  done
  RUNTIME_DOCKER_ARGS=(
    --add-host host.docker.internal:host-gateway
    -e "GRASP_RUNTIME_URL=http://host.docker.internal:${port}/sandbox-inject/ci.tgz"
    -e "SANDBOX_INJECT_HEADERS=Authorization: Bearer ${RUNTIME_TOKEN}"
  )
}

stop_runtime_servers() {
  local p
  for p in "${RUNTIME_SERVER_PIDS[@]}"; do kill "$p" 2>/dev/null || true; done
  RUNTIME_SERVER_PIDS=()
}

runtime_manifest_get() { tar -xzOf "$1" ./MANIFEST | sed -n "s/^$2=//p" | head -n 1; }

# repack_runtime SRC OUT [MIN_IMAGE] [NOTE]: copy of bundle SRC with MIN_IMAGE
# in MANIFEST and, when NOTE is set, a comment appended to vnc-preview.sh (so
# the version changes). Prints the new version.
repack_runtime() {
  local src="$1" out="$2" min="${3:-}" note="${4:-}" d version arch
  d="$(mktemp -d)"
  tar -xzf "$src" -C "$d"
  [ -n "$min" ] || min="$(sed -n 's/^min_image=//p' "$d/MANIFEST")"
  arch="$(sed -n 's/^arch=//p' "$d/MANIFEST")"
  [ -z "$note" ] || echo "# $note" >>"$d/scripts/vnc-preview.sh"
  version="$(runtime_digest "$d")"
  printf 'version=%s\narch=%s\nmin_image=%s\n' "$version" "$arch" "$min" >"$d/MANIFEST"
  tar --sort=name --mtime=@0 --owner=0 --group=0 --numeric-owner -C "$d" -cf - . | gzip -n -9 >"$out"
  rm -rf "$d"
  echo "$version"
}
