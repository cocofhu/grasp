#!/usr/bin/env bash
# Agent 连接 E2E：用构建好的沙箱镜像起容器（与平台相同的 --privileged + DinD + 预览开关），
# 逐个 AGENT_PROVIDER 验证：startup.sh 跑完不退出、backend 能力发现、code-server 可访问、
# WebSocket connect 拿到 connected（与平台「连接中 → 已连接」同一路径）。
#
# 用法：scripts/test-agent-connect.sh <image> [providers]
#   providers 默认 cursor,claude_code,codebuddy,opencode,trae（镜像预装的五个）
#   设置 CURSOR_API_KEY 时，额外对 cursor 跑一轮真实对话（回复里须含约定标记）。
#   trae 的 ACP 服务启动前必须登录：只有设置 TRAECLI_PERSONAL_ACCESS_TOKEN 时才做握手，
#   否则只校验 `traecli acp serve --help`（仍能发现漏装）。
#   READY_TIMEOUT（默认 240s）/ CONNECT_TIMEOUT（默认 120s）/ CHAT_TIMEOUT（默认 240s）
set -euo pipefail

image="${1:?usage: test-agent-connect.sh <image> [providers]}"
providers="${2:-cursor,claude_code,codebuddy,opencode,trae}"
ready_timeout="${READY_TIMEOUT:-240}"
connect_timeout="${CONNECT_TIMEOUT:-120}"
chat_timeout="${CHAT_TIMEOUT:-240}"
here="$(cd "$(dirname "$0")" && pwd)"
ws_check="$here/agent-ws-check.mjs"

log() { echo "[agent-e2e] $*"; }

name=""
cleanup() {
  [ -n "$name" ] && docker rm -f "$name" >/dev/null 2>&1 || true
}
trap cleanup EXIT

dump() {
  echo "::group::container logs ($name)"
  docker logs "$name" 2>&1 | tail -200 || true
  echo "::endgroup::"
}

die() {
  echo "::error::[agent-e2e] $*"
  dump
  exit 1
}

host_port() {
  docker port "$name" "$1/tcp" | head -1 | sed 's/.*://'
}

check_provider() {
  local provider="$1"
  name="grasp-agent-e2e-${provider}-$$"
  log "provider=$provider: 启动容器"
  local env=(-e AGENT_PROVIDER="$provider" -e ROOT_PASSWORD=ci-e2e -e VNC_PREVIEW=1 -e PREVIEW_DIRECT=1)
  if [ "$provider" = cursor ] && [ -n "${CURSOR_API_KEY:-}" ]; then
    env+=(-e CURSOR_API_KEY)
  fi
  if [ "$provider" = trae ] && [ -n "${TRAECLI_PERSONAL_ACCESS_TOKEN:-}" ]; then
    env+=(-e TRAECLI_PERSONAL_ACCESS_TOKEN)
  fi
  docker run -d --privileged --name "$name" "${env[@]}" \
    -p 127.0.0.1::8765 -p 127.0.0.1::8744 "$image" >/dev/null

  local api="" ide="" deadline=$((SECONDS + ready_timeout))
  until [ -n "$api" ] && curl -fsS "http://127.0.0.1:$api/api/capabilities" -o /tmp/agent-e2e-caps.json 2>/dev/null; do
    if [ "$(docker inspect -f '{{.State.Running}}' "$name" 2>/dev/null)" != true ]; then
      die "provider=$provider: 容器在就绪前退出（exit=$(docker inspect -f '{{.State.ExitCode}}' "$name" 2>/dev/null)）"
    fi
    [ "$SECONDS" -lt "$deadline" ] || die "provider=$provider: ${ready_timeout}s 内 backend 未就绪"
    api="$(host_port 8765 || true)"
    sleep 3
  done
  local runtime
  runtime="$(node -e 'const c=require("/tmp/agent-e2e-caps.json"); if (c.protocol !== "wsp/1" || !c.agent || !c.agent.runtime) process.exit(1); console.log(c.agent.runtime)')" \
    || die "provider=$provider: /api/capabilities 内容不对：$(cat /tmp/agent-e2e-caps.json)"
  log "provider=$provider: backend 就绪 runtime=$runtime"

  ide="$(host_port 8744)"
  deadline=$((SECONDS + 60))
  local code=000
  while :; do
    code="$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$ide/" || true)"
    [ "$code" -ge 200 ] && [ "$code" -lt 400 ] && break
    [ "$SECONDS" -lt "$deadline" ] || die "provider=$provider: code-server 端口 8744 无响应（HTTP $code）"
    sleep 2
  done
  log "provider=$provider: code-server 可访问（HTTP $code）"

  # one-shot provider 的 connect 只查 PATH；这里确认二进制真能跑。
  local probe
  case "$provider" in
    cursor) probe="cursor-agent --version" ;;
    claude_code) probe="claude --version" ;;
    codebuddy) probe="codebuddy --version" ;;
    opencode) probe="opencode --version" ;;
    trae) probe="traecli acp serve --help" ;;
    *) probe="" ;;
  esac
  if [ -n "$probe" ]; then
    docker exec "$name" bash -lc "$probe" >/dev/null 2>&1 || die "provider=$provider: \`$probe\` 执行失败"
    log "provider=$provider: \`$probe\` 正常"
  fi

  if [ "$provider" = trae ] && [ -z "${TRAECLI_PERSONAL_ACCESS_TOKEN:-}" ]; then
    log "provider=$provider: 未设置 TRAECLI_PERSONAL_ACCESS_TOKEN，跳过握手"
  else
    node "$ws_check" "ws://127.0.0.1:$api/ws" connect "$connect_timeout" \
      || die "provider=$provider: Agent connect 失败"
  fi

  if [ "$provider" = cursor ] && [ -n "${CURSOR_API_KEY:-}" ]; then
    node "$ws_check" "ws://127.0.0.1:$api/ws" chat "$chat_timeout" \
      || die "provider=$provider: 真实对话失败"
  fi

  [ "$(docker inspect -f '{{.State.Running}}' "$name")" = true ] || die "provider=$provider: 容器在检查过程中退出"
  docker rm -f "$name" >/dev/null
  name=""
  log "provider=$provider: 通过"
}

IFS=',' read -r -a list <<<"$providers"
for p in "${list[@]}"; do
  p="$(echo "$p" | tr -d '[:space:]')"
  [ -n "$p" ] && check_provider "$p"
done
if [ -z "${CURSOR_API_KEY:-}" ]; then
  log "未设置 CURSOR_API_KEY：跳过真实对话，只验证到 Agent 握手"
fi
log "全部通过：$providers"
