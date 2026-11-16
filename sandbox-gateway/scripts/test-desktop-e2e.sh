#!/usr/bin/env bash
# 桌面镜像 E2E：桌面镜像 + 运行时包，按平台参数（--privileged）启动后验证：
# 桌面外壳就绪、启动台白名单与 Origin、桌面 MCP 用 Mousepad 写文件、
# 用户接管时键鼠被拒而截图可用、Blender MCP 建模，并打印预览常驻内存。
#
# 用法：scripts/test-desktop-e2e.sh <image> <sandbox-runtime.tgz> [desktop|browser]
#   browser：默认镜像应回落浏览器模式，只验证模式并打印内存（容量对比基线）。
#   READY_TIMEOUT（默认 300s）；E2E_ARTIFACT_DIR 设了则失败时存一张桌面截图
set -euo pipefail

image="${1:?usage: test-desktop-e2e.sh <image> <bundle.tgz>}"
bundle="$(cd "$(dirname "${2:?usage: test-desktop-e2e.sh <image> <bundle.tgz>}")" && pwd)/$(basename "$2")"
mode="${3:-desktop}"
ready_timeout="${READY_TIMEOUT:-300}"
here="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=lib-runtime.sh
. "$here/lib-runtime.sh"

name="grasp-desktop-e2e-$$"
cleanup() {
  docker rm -f "$name" >/dev/null 2>&1 || true
  stop_runtime_servers
}
trap cleanup EXIT

log() { echo "[desktop-e2e] $*"; }
die() {
  echo "::error::[desktop-e2e] $*"
  echo "::group::container logs"
  docker logs "$name" 2>&1 | tail -150 || true
  echo "::endgroup::"
  echo "::group::desktop logs"
  if [ -n "${E2E_ARTIFACT_DIR:-}" ]; then
    mkdir -p "$E2E_ARTIFACT_DIR"
    docker exec -e DISPLAY=:99 "$name" import -silent -window root png:- >"$E2E_ARTIFACT_DIR/desktop.png" 2>/dev/null || true
  fi
  docker exec -e DISPLAY=:99 "$name" bash -c 'cat /run/grasp-desktop/state /run/grasp-desktop/session.env; wmctrl -l
    tail -n 60 /tmp/grasp-desktop-*.log 2>/dev/null' || true
  echo "::endgroup::"
  exit 1
}
in_box() { docker exec -e DISPLAY=:99 "$name" "$@"; }
api() { in_box curl -fsS -H 'Content-Type: application/json' "$@"; }
status_field() { api http://127.0.0.1:8765/api/desktop/status | python3 -c "import json,sys; print(json.load(sys.stdin).get('$1'))"; }

# mcp CMD... : stdio JSON-RPC client inside the box. Reads one JSON request per
# stdin line (initialize is sent first) and prints each response line.
mcp() {
  docker exec -i "$name" python3 -c '
import json, subprocess, sys
p = subprocess.Popen(sys.argv[1:], stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True)
def call(i, method, params=None):
    p.stdin.write(json.dumps({"jsonrpc": "2.0", "id": i, "method": method, "params": params or {}}) + "\n")
    p.stdin.flush()
    while True:
        line = p.stdout.readline()
        if not line:
            raise SystemExit("mcp server exited")
        msg = json.loads(line)
        if msg.get("id") == i:
            return msg
call(0, "initialize", {"protocolVersion": "2025-06-18", "capabilities": {}, "clientInfo": {"name": "e2e", "version": "1"}})
p.stdin.write(json.dumps({"jsonrpc": "2.0", "method": "notifications/initialized"}) + "\n")
p.stdin.flush()
for n, line in enumerate(sys.stdin, 1):
    req = json.loads(line)
    print(json.dumps(call(n, req["method"], req.get("params")), ensure_ascii=False), flush=True)
p.stdin.close()
p.terminate()
' "$@"
}
tool() { printf '{"method":"tools/call","params":{"name":"%s","arguments":%s}}\n' "$1" "${2:-{\}}"; }

# print_memory: preview-stack memory (MiB, PSS summed per process name, so
# pages shared between Chromium processes are counted once).
print_memory() {
  log "预览常驻内存（$mode）："
  in_box bash -c 'for d in /proc/[0-9]*; do
    n=$(cat "$d/comm" 2>/dev/null) || continue
    r=$(sed -n "s/^Pss:[[:space:]]*\([0-9]*\).*/\1/p" "$d/smaps_rollup" 2>/dev/null)
    [ -n "$r" ] && echo "$n $r"
  done' | awk '
    $1 ~ /^(chrom|Xvfb|x11vnc|websockify|openbox|plank|fcitx5|grasp-desk|dbus-daemon)/ {
      k = $1; if (k ~ /^chrom/) k = "chromium"; s[k] += $2; t += $2 }
    END { for (k in s) printf "  %-14s %6.0f MiB\n", k, s[k]/1024; printf "  %-14s %6.0f MiB\n", "total", t/1024 }'
}

start_runtime_server "$bundle"
docker run -d --privileged --name "$name" "${RUNTIME_DOCKER_ARGS[@]}" \
  -e AGENT_PROVIDER=cursor -e ROOT_PASSWORD=ci-e2e "$image" >/dev/null

deadline=$((SECONDS + ready_timeout))
if [ "$mode" = browser ]; then
  until [ "$(status_field mode 2>/dev/null)" = browser ] && in_box pgrep -f 'chrom.*remote-debugging-port' >/dev/null; do
    [ "$(docker inspect -f '{{.State.Running}}' "$name")" = true ] || die "容器退出"
    [ "$SECONDS" -lt "$deadline" ] || die "${ready_timeout}s 内浏览器预览未就绪"
    sleep 3
  done
  in_box pgrep -x openbox >/dev/null && die "默认镜像不应启动桌面外壳"
  sleep 20
  print_memory
  log "默认镜像浏览器模式：通过"
  exit 0
fi

# --- 1. 桌面外壳就绪 ---
until [ "$(status_field shellReady 2>/dev/null)" = True ]; do
  [ "$(docker inspect -f '{{.State.Running}}' "$name")" = true ] || die "容器退出"
  [ "$SECONDS" -lt "$deadline" ] || die "${ready_timeout}s 内桌面外壳未就绪（mode=$(status_field mode 2>/dev/null)）"
  sleep 3
done
[ "$(status_field mode)" = desktop ] || die "mode 不是 desktop"
for p in openbox plank fcitx5 grasp-desktop; do
  in_box pgrep -x "$p" >/dev/null || die "$p 未运行"
done
log "1. 桌面外壳就绪：通过"
sleep 20
print_memory

# --- 2. 启动台 ---
api http://127.0.0.1:6091/apps | grep -q '"mousepad"' || die "启动台目录缺少 mousepad"
code="$(in_box curl -s -o /dev/null -w '%{http_code}' -X POST -H 'Content-Type: application/json' -H 'Origin: http://evil.example' \
  -d '{"id":"mousepad"}' http://127.0.0.1:6091/launch)"
[ "$code" = 403 ] || die "跨源启动应返回 403，实际 $code"
code="$(in_box curl -s -o /dev/null -w '%{http_code}' -X POST -H 'Content-Type: application/json' -d '{"id":"rm -rf"}' http://127.0.0.1:6091/launch)"
[ "$code" = 404 ] || die "白名单外 id 应返回 404，实际 $code"
log "2. 启动台白名单与 Origin：通过"

# --- 3. 桌面 MCP：Mousepad 写文件 ---
desk=/opt/grasp-runtime/current/bin/grasp-desktop
out="$(printf '%s\n' '{"method":"tools/list"}' "$(tool launch_app '{"id":"mousepad"}')" | mcp "$desk" mcp)"
grep -q '"name": "screenshot"' <<<"$out" || die "tools/list 缺少 screenshot"
for _ in $(seq 1 30); do
  in_box wmctrl -l | grep -qi mousepad && break
  sleep 1
done
in_box wmctrl -l | grep -qi mousepad || die "Mousepad 窗口未出现；launch_app 返回：$(tail -1 <<<"$out")"
target=/root/workspace/desktop-e2e.txt
out="$({
  tool focus_window '{"title":"Mousepad"}'
  tool type_text '{"text":"桌面 E2E hello"}'
  tool key '{"keys":"ctrl+s"}'
} | mcp "$desk" mcp)"
grep -q '"isError": true' <<<"$out" && die "桌面 MCP 操作失败：$out"
dialog=""
for _ in $(seq 1 20); do
  dialog="$(in_box wmctrl -l | awk '/另存为|保存|Save/ {print $1; exit}')"
  [ -n "$dialog" ] && break
  sleep 1
done
[ -n "$dialog" ] || die "没有出现保存对话框；窗口：$(in_box wmctrl -l | tr '\n' ';')"
out="$({
  tool focus_window "{\"window_id\":\"$dialog\"}"
  tool key '{"keys":"ctrl+a"}'
  tool type_text "{\"text\":\"$target\"}"
} | mcp "$desk" mcp)"
grep -q '"isError": true' <<<"$out" && die "保存对话框操作失败：$out"
# The file chooser's path completion can swallow the first Return.
for _ in 1 2 3; do
  sleep 1
  tool key '{"keys":"Return"}' | mcp "$desk" mcp >/dev/null
  for _ in $(seq 1 5); do
    in_box test -s "$target" && break 2
    sleep 1
  done
done
in_box grep -q '桌面 E2E hello' "$target" || die "Mousepad 没有保存 $target；窗口：$(in_box wmctrl -l | tr '\n' ';')"
log "3. 桌面 MCP 写文件：通过"

# --- 4. 软互锁 ---
api -X PUT -d '{"holder":"user","client":"e2e"}' http://127.0.0.1:8765/api/desktop/control >/dev/null
[ "$(status_field holder)" = user ] || die "接管后 holder 不是 user"
out="$({ tool click '{"x":10,"y":10}'; tool screenshot; } | mcp "$desk" mcp)"
head -1 <<<"$out" | grep -q '"isError": true' || die "用户接管时 click 应被拒绝"
head -1 <<<"$out" | grep -q '用户正在控制桌面' || die "拒绝信息不对：$(head -1 <<<"$out")"
sed -n 2p <<<"$out" | grep -q '"type": "image"' || die "用户接管时截图应可用"
api -X PUT -d '{"holder":"agent","client":"e2e"}' http://127.0.0.1:8765/api/desktop/control >/dev/null
out="$(tool click '{"x":10,"y":10}' | mcp "$desk" mcp)"
grep -q '"isError": true' <<<"$out" && die "交还后 click 仍失败：$out"
log "4. 软互锁：通过"

# --- 5. Blender MCP ---
api -X POST -d '{"id":"blender"}' http://127.0.0.1:6091/launch >/dev/null || die "启动 Blender 失败"
for _ in $(seq 1 90); do
  in_box bash -c 'echo >/dev/tcp/127.0.0.1/6092' 2>/dev/null && break
  sleep 2
done
in_box bash -c 'echo >/dev/tcp/127.0.0.1/6092' 2>/dev/null || die "Blender MCP 插件未监听 6092"
code='import bpy\nbpy.ops.mesh.primitive_cube_add()\nresult = {\"objects\": sorted(o.name for o in bpy.data.objects)}'
out="$(tool execute_blender_code "{\"code\":\"$code\"}" | mcp env BLENDER_MCP_PORT=6092 blender-mcp)"
grep -q '"isError": true' <<<"$out" && die "Blender MCP 执行失败：$out"
grep -q 'Cube' <<<"$out" || die "Blender MCP 结果没有 Cube：$out"
log "5. Blender MCP：通过"

log "全部通过"
