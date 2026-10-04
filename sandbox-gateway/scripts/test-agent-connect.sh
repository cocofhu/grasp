#!/usr/bin/env bash
# Agent 连接 E2E：用构建好的沙箱镜像起容器（与平台相同的 --privileged + DinD + 预览开关），
# 逐个 AGENT_PROVIDER 验证：startup.sh 跑完不退出、backend 能力发现、code-server 可访问、
# WebSocket connect 拿到 connected（与平台「连接中 → 已连接」同一路径）。
#
# 用法：scripts/test-agent-connect.sh <image> [providers]
#   RUNTIME_BUNDLE：运行时包（默认 .devdata/sandbox-runtime/sandbox-runtime.tgz，
#   scripts/build-sandbox-runtime.sh 构建）；镜像本身不带逻辑，由本脚本像 Grasp 一样下发。
#   providers 默认 cursor,claude_code,codebuddy,opencode,trae（镜像预装的五个）
#   opencode 始终跑一轮 mock 对话，不需要厂商密钥：本脚本在宿主机拉起
#   mock-chat-model.mjs（夹具 ci-e2e，回复含 GRASP_AGENT_E2E_OK），容器通过
#   host.docker.internal 访问。配置与平台一致：GRASP_OPENCODE_PROVIDER=custom、
#   GRASP_OPENCODE_BASE_URL、OpenAI 兼容适配器写入 opencode.json，模型为 custom/ci-e2e。
#   请求没打到替身、缺标记、stopReason 不是 end_turn 或超时，检查失败并打印容器日志。
#   设置 CURSOR_API_KEY 时，额外对 cursor 跑一轮真实对话（回复里须含约定标记）。
#   未设置则只跳过这条真实对话，不跳过 opencode 的 mock 对话。
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
mock_chat="$here/mock-chat-model.mjs"
runtime_bundle="${RUNTIME_BUNDLE:-$here/../../.devdata/sandbox-runtime/sandbox-runtime.tgz}"
[ -f "$runtime_bundle" ] || { echo "[agent-e2e] 运行时包不存在：$runtime_bundle（先运行 scripts/build-sandbox-runtime.sh 或设置 RUNTIME_BUNDLE）" >&2; exit 1; }
# shellcheck source=lib-runtime.sh
. "$here/lib-runtime.sh"

log() { echo "[agent-e2e] $*"; }

name=""
mock_pid=""
mock_port=""
mock_log=""
cleanup() {
  [ -n "$name" ] && docker rm -f "$name" >/dev/null 2>&1 || true
  if [ -n "$mock_pid" ]; then
    kill "$mock_pid" >/dev/null 2>&1 || true
    wait "$mock_pid" 2>/dev/null || true
  fi
  stop_runtime_servers
}
trap cleanup EXIT

dump() {
  echo "::group::container logs ($name)"
  docker logs "$name" 2>&1 | tail -200 || true
  echo "::endgroup::"
  if [ -n "$mock_log" ] && [ -f "$mock_log" ]; then
    echo "::group::mock chat model"
    tail -100 "$mock_log" || true
    echo "::endgroup::"
  fi
}

die() {
  echo "::error::[agent-e2e] $*"
  dump
  exit 1
}

start_mock_chat() {
  [ -f "$mock_chat" ] || die "找不到 mock chat model：$mock_chat"
  mock_log="$(mktemp)"
  local out line i
  out="$(mktemp)"
  node "$mock_chat" --listen 0.0.0.0 --port 0 >"$out" 2>"$mock_log" &
  mock_pid=$!
  for i in $(seq 1 50); do
    line="$(grep -m1 '^MOCK_CHAT_PORT=' "$out" 2>/dev/null || true)"
    if [ -n "$line" ]; then
      mock_port="${line#MOCK_CHAT_PORT=}"
      break
    fi
    if ! kill -0 "$mock_pid" 2>/dev/null; then
      die "mock chat model 启动失败：$(cat "$mock_log" 2>/dev/null || true)"
    fi
    sleep 0.1
  done
  rm -f "$out"
  [ -n "$mock_port" ] || die "mock chat model 未报告端口：$(cat "$mock_log" 2>/dev/null || true)"
  curl -fsS "http://127.0.0.1:${mock_port}/health" >/dev/null || die "mock chat model 健康检查失败 port=${mock_port}"
  log "mock chat model 就绪 port=${mock_port}（容器经 host.docker.internal 访问）"
}

install_opencode_mock_config() {
  [ -n "$mock_port" ] || die "provider=opencode: mock chat model 未启动"
  local base="http://host.docker.internal:${mock_port}/v1" cfg
  cfg="$(mktemp)"
  node "$mock_chat" --print-opencode-config --base-url "$base" >"$cfg" \
    || die "provider=opencode: 生成 opencode.json 失败"
  docker exec "$name" mkdir -p /root/.config/opencode \
    || die "provider=opencode: 无法创建 OpenCode 配置目录"
  docker cp "$cfg" "$name:/root/.config/opencode/opencode.json" \
    || die "provider=opencode: 无法写入 opencode.json"
  rm -f "$cfg"
  docker exec "$name" test -s /root/.config/opencode/opencode.json \
    || die "provider=opencode: opencode.json 为空"
  log "provider=opencode: GRASP_OPENCODE_BASE_URL=${base}（custom / OpenAI 兼容适配器）"
}

assert_mock_hit() {
  local body hits
  body="$(curl -fsS "http://127.0.0.1:${mock_port}/health")" \
    || die "provider=opencode: 读取 mock health 失败"
  hits="$(printf '%s' "$body" | node -e 'let s="";process.stdin.on("data",d=>s+=d);process.stdin.on("end",()=>{const j=JSON.parse(s);process.stdout.write(String(j.hits??""))})')" \
    || die "provider=opencode: mock health 无法解析"
  [ "$hits" -ge 1 ] || die "provider=opencode: 对话没有打到 mock chat model（hits=${hits:-0}）"
  log "provider=opencode: mock 收到 ${hits} 次补全请求"
}

host_port() {
  docker port "$name" "$1/tcp" | head -1 | sed 's/.*://'
}

check_provider() {
  local provider="$1"
  name="grasp-agent-e2e-${provider}-$$"
  log "provider=$provider: 启动容器"
  local env=(-e AGENT_PROVIDER="$provider" -e ROOT_PASSWORD=ci-e2e -e VNC_PREVIEW=1 -e PREVIEW_DIRECT=1)
  if [ "$provider" = opencode ]; then
    [ -n "$mock_port" ] || die "provider=opencode: mock chat model 未启动"
    env+=(
      -e GRASP_OPENCODE_PROVIDER=custom
      -e "GRASP_OPENCODE_BASE_URL=http://host.docker.internal:${mock_port}/v1"
      -e ACP_BRIDGE_MODEL=custom/ci-e2e
      -e OPENCODE_API_KEY=ci-e2e
      -e GRASP_OPENCODE_API_KEY=ci-e2e
      -e OPENCODE_CONFIG=/root/.config/opencode/opencode.json
    )
  fi
  if [ "$provider" = cursor ] && [ -n "${CURSOR_API_KEY:-}" ]; then
    env+=(-e CURSOR_API_KEY)
  fi
  if [ "$provider" = trae ] && [ -n "${TRAECLI_PERSONAL_ACCESS_TOKEN:-}" ]; then
    env+=(-e TRAECLI_PERSONAL_ACCESS_TOKEN)
  fi
  docker run -d --privileged --name "$name" "${RUNTIME_DOCKER_ARGS[@]}" "${env[@]}" \
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

  if [ "$provider" = opencode ]; then
    install_opencode_mock_config
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

  if [ "$provider" = opencode ]; then
    node "$ws_check" "ws://127.0.0.1:$api/ws" chat "$chat_timeout" \
      || die "provider=$provider: mock 对话失败"
    assert_mock_hit
  fi

  [ "$(docker inspect -f '{{.State.Running}}' "$name")" = true ] || die "provider=$provider: 容器在检查过程中退出"
  docker rm -f "$name" >/dev/null
  name=""
  log "provider=$provider: 通过"
}

start_runtime_server "$runtime_bundle"
IFS=',' read -r -a list <<<"$providers"
need_mock=0
for p in "${list[@]}"; do
  p="$(echo "$p" | tr -d '[:space:]')"
  [ "$p" = opencode ] && need_mock=1
done
if [ "$need_mock" = 1 ]; then
  start_mock_chat
fi
for p in "${list[@]}"; do
  p="$(echo "$p" | tr -d '[:space:]')"
  [ -n "$p" ] && check_provider "$p"
done
if [ -z "${CURSOR_API_KEY:-}" ]; then
  log "未设置 CURSOR_API_KEY：已跳过 cursor 真实对话（opencode 的 mock 对话不依赖该密钥）"
fi
log "全部通过：$providers"
