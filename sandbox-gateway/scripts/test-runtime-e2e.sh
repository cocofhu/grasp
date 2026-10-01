#!/usr/bin/env bash
# 运行时包 E2E：沙箱镜像只带环境和 /grasp-bootstrap.sh，逻辑来自运行时包。
# 用构建好的镜像 + 运行时包验证：正常启动、直连预览注入、热更新、镜像版本不够、
# 缺少 GRASP_RUNTIME_URL、令牌不进日志。容器参数与平台一致（--privileged）。
#
# 用法：scripts/test-runtime-e2e.sh <image> <sandbox-runtime.tgz>
#   READY_TIMEOUT（默认 240s）
set -euo pipefail

image="${1:?usage: test-runtime-e2e.sh <image> <bundle.tgz>}"
bundle="$(cd "$(dirname "${2:?usage: test-runtime-e2e.sh <image> <bundle.tgz>}")" && pwd)/$(basename "$2")"
ready_timeout="${READY_TIMEOUT:-240}"
here="$(cd "$(dirname "$0")" && pwd)"
repo="$(cd "$here/../.." && pwd)"
# shellcheck source=lib-runtime.sh
. "$here/lib-runtime.sh"

work="$(mktemp -d)"
containers=()
name=""
cleanup() {
  local c
  for c in "${containers[@]}"; do docker rm -f "$c" >/dev/null 2>&1 || true; done
  stop_runtime_servers
  rm -rf "$work"
}
trap cleanup EXIT

log() { echo "[runtime-e2e] $*"; }

die() {
  echo "::error::[runtime-e2e] $*"
  if [ -n "$name" ]; then
    echo "::group::container logs ($name)"
    docker logs "$name" 2>&1 | tail -200 || true
    echo "::endgroup::"
    echo "::group::/tmp/preview-inject.log ($name)"
    docker exec "$name" cat /tmp/preview-inject.log 2>/dev/null | tail -100 || true
    echo "::endgroup::"
  fi
  exit 1
}

host_port() { docker port "$name" "$1/tcp" | head -1 | sed 's/.*://'; }
in_box() { docker exec "$name" "$@"; }

wait_backend() {
  local api="" deadline=$((SECONDS + ready_timeout))
  until [ -n "$api" ] && curl -fsS "http://127.0.0.1:$api/api/capabilities" -o /dev/null 2>/dev/null; do
    if [ "$(docker inspect -f '{{.State.Running}}' "$name" 2>/dev/null)" != true ]; then
      die "容器在 backend 就绪前退出（exit=$(docker inspect -f '{{.State.ExitCode}}' "$name" 2>/dev/null)）"
    fi
    [ "$SECONDS" -lt "$deadline" ] || die "${ready_timeout}s 内 backend 未就绪"
    api="$(host_port 8765 || true)"
    sleep 2
  done
}

# preview_injected: the direct-preview page served on PREVIEW_PORT carries the pick script.
preview_injected() {
  local port i
  port="$(host_port 3000)"
  for i in $(seq 1 30); do
    if curl -fsS -H 'Accept: text/html' "http://127.0.0.1:$port/" 2>/dev/null | grep -q '/__grasp/preview-pick.js'; then
      return 0
    fi
    sleep 2
  done
  return 1
}

version="$(runtime_manifest_get "$bundle" version)"
[[ "$version" =~ ^[0-9a-f]{64}$ ]] || die "运行时包 MANIFEST 无效：$bundle"
log "镜像 $image，运行时包 ${version:0:12}"

# --- 1. 正常启动 + 2. 直连预览注入 ---
start_runtime_server "$bundle"
token="$RUNTIME_TOKEN"
name="grasp-runtime-e2e-$$"
containers+=("$name")
docker run -d --privileged --name "$name" "${RUNTIME_DOCKER_ARGS[@]}" \
  -e AGENT_PROVIDER=cursor -e ROOT_PASSWORD=ci-e2e -e PREVIEW_DIRECT=1 -e PREVIEW_PORT=3000 \
  -p 127.0.0.1::8765 -p 127.0.0.1::3000 "$image" >/dev/null
wait_backend
[ "$(in_box /grasp-bootstrap.sh version)" = "$version" ] || die "/grasp-bootstrap.sh version 与 MANIFEST 不一致"
in_box test -s /etc/grasp-image-level || die "/etc/grasp-image-level 缺失"
for f in backend acp-bridge preview-inject preview-inject.sh vnc-preview.sh services.sh; do
  in_box test -e "/usr/local/bin/$f" || die "/usr/local/bin/$f 软链无法解析"
done
in_box test -e /usr/local/share/backend/web/index.html || die "/usr/local/share/backend 软链无法解析"
log "1. 正常启动：通过"

in_box bash -c 'mkdir -p /tmp/e2e-site && printf "<!doctype html><html><head><title>e2e</title></head><body>hi</body></html>" >/tmp/e2e-site/index.html'
docker exec -d -w /tmp/e2e-site "$name" python3 -m http.server 3000
preview_injected || die "直连预览页面里没有 /__grasp/preview-pick.js"
curl -fsS "http://127.0.0.1:$(host_port 3000)/__grasp/live-overlay.js" -o "$work/overlay.js" || die "/__grasp/live-overlay.js 请求失败"
cmp -s "$work/overlay.js" "$repo/sandbox-gateway/sandbox/internal/previewinject/live-overlay.js" ||
  die "/__grasp/live-overlay.js 与运行时包源码不一致"
log "2. 直连预览注入：通过"

# --- 3. 热更新：推第二个包，重启服务，不重启容器 ---
v2="$(repack_runtime "$bundle" "$work/v2.tgz" "" "runtime-e2e hot update")"
[ "$v2" != "$version" ] || die "第二个包版本没变"
pid1="$(in_box cat /run/grasp-runtime/backend.pid)"
got="$(docker exec -i "$name" /grasp-bootstrap.sh install-stdin <"$work/v2.tgz")" || die "install-stdin 失败"
[ "$got" = "$v2" ] || die "install-stdin 输出 $got，期望 $v2"
in_box /opt/grasp-runtime/current/scripts/services.sh restart preview-inject >/dev/null || die "services.sh restart preview-inject 失败"
rc=0
in_box /opt/grasp-runtime/current/scripts/services.sh restart backend --if-idle >/dev/null || rc=$?
[ "$rc" = 0 ] || die "services.sh restart backend --if-idle 退出码 $rc"
[ "$(in_box /grasp-bootstrap.sh version)" = "$v2" ] || die "热更新后版本不对"
pid2="$(in_box cat /run/grasp-runtime/backend.pid)"
[ "$pid2" != "$pid1" ] || die "backend pid 没变（$pid1）"
wait_backend
preview_injected || die "热更新后直连预览注入失效"
in_box grep -q "runtime-e2e hot update" /usr/local/bin/vnc-preview.sh || die "软链没有指向新版本"
n="$(in_box bash -c 'ls /opt/grasp-runtime | grep -cE "^[0-9a-f]{64}$"')"
[ "$n" = 2 ] || die "版本目录应剩 2 个，实际 $n"
log "3. 热更新：通过"

# --- 6. 令牌不进日志 ---
if docker logs "$name" 2>&1 | grep -qF "$token"; then
  die "docker logs 里出现了 Bearer 令牌"
fi
log "6. 令牌不进日志：通过"
docker rm -f "$name" >/dev/null
name=""

# --- 4. 镜像版本不够：min_image=999 ---
repack_runtime "$bundle" "$work/too-new.tgz" 999 >/dev/null
start_runtime_server "$work/too-new.tgz"
name="grasp-runtime-e2e-old-$$"
containers+=("$name")
docker run -d --privileged --name "$name" "${RUNTIME_DOCKER_ARGS[@]}" -e ROOT_PASSWORD=ci-e2e "$image" >/dev/null
code="$(timeout 120 docker wait "$name" || echo timeout)"
[ "$code" = 4 ] || die "min_image=999 应退出码 4，实际 $code"
docker logs "$name" 2>&1 | grep -q "请重建沙箱镜像" || die "日志里缺少“请重建沙箱镜像”"
log "4. 镜像版本不够：通过"
name=""

# --- 5. 缺少 GRASP_RUNTIME_URL ---
name="grasp-runtime-e2e-nourl-$$"
containers+=("$name")
docker run -d --privileged --name "$name" -e ROOT_PASSWORD=ci-e2e "$image" >/dev/null
code="$(timeout 120 docker wait "$name" || echo timeout)"
[ "$code" = 1 ] || die "缺少 GRASP_RUNTIME_URL 应退出码 1，实际 $code"
docker logs "$name" 2>&1 | grep -q "GRASP_RUNTIME_URL" || die "日志里缺少 GRASP_RUNTIME_URL 提示"
log "5. 缺少运行时地址：通过"
name=""

log "全部通过"
