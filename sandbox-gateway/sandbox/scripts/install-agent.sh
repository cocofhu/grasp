#!/usr/bin/env bash
# install-agent.sh — 构建期安装一个或多个 Agent CLI。
#
# 由 Dockerfile 调用。默认装齐五个对外后端（cursor / claude_code / codebuddy /
# trae / opencode）；运行时仍由 AGENT_PROVIDER 单活选后端。
# 本地打薄镜像：--build-arg AGENT_PROVIDERS=cursor
#
# 约定：
#   $1 / $AGENT_PROVIDERS          逗号列表或 all（默认 all）。
#   $AGENT_OPTIONAL_PROVIDERS      失败只告警的 provider（默认空：任何 provider 装不上都让构建失败）。
#   $AGENT_INSTALL_CMD             可选：完全接管安装（用于未内置方式或私有源）。
set -euo pipefail

DEFAULT_PROVIDERS="cursor,claude_code,codebuddy,trae,opencode"
raw="${1:-${AGENT_PROVIDERS:-all}}"
custom_cmd="${AGENT_INSTALL_CMD:-}"
optional_raw="${AGENT_OPTIONAL_PROVIDERS:-}"

log() { echo "[install-agent] $*"; }

# retry <n> <cmd...> —— 带指数退避的重试，缓解构建期网络抖动。
retry() {
  local max="$1"; shift
  local i=1
  until "$@"; do
    if [ "$i" -ge "$max" ]; then return 1; fi
    log "第 $i 次失败，重试：$*"; sleep $((i * 5)); i=$((i + 1))
  done
}

npm_global() { retry 3 npm install -g "$@"; }

# curl_install <url> <shell> —— 官方 `curl | sh` 安装脚本。子 shell 不继承 pipefail，
# 下载失败时 sh 读到空输入也会返回 0，所以显式开 pipefail，装完再由调用方校验二进制。
curl_install() {
  retry 3 bash -o pipefail -c "curl -fsSL --connect-timeout 20 --max-time 120 '$1' | $2"
}

install_cursor() {
  # 官方 install.sh 无重试/无超时且会吞掉失败；改为解析版本→带重试下载解压→建软链→校验。
  local ok=0 script ver url dest
  for i in 1 2 3 4 5; do
    script="$(curl -fsSL --connect-timeout 20 --max-time 60 https://cursor.com/install || true)"
    ver="$(printf '%s' "$script" | grep -oE 'lab/[0-9]{4}\.[0-9]{2}\.[0-9]{2}-[a-f0-9]+/' | head -1 | sed -E 's#lab/(.+)/#\1#')"
    if [ -n "$ver" ]; then
      url="https://downloads.cursor.com/lab/${ver}/linux/x64/agent-cli-package.tar.gz"
      dest="/root/.local/share/cursor-agent/versions/${ver}"
      mkdir -p "$dest" /root/.local/bin
      if curl -fSL --retry 8 --retry-delay 5 --retry-all-errors --connect-timeout 30 --max-time 900 "$url" -o /tmp/agent.tgz \
         && tar --strip-components=1 -xzf /tmp/agent.tgz -C "$dest"; then
        rm -f /tmp/agent.tgz
        ln -sf "$dest/cursor-agent" /root/.local/bin/agent
        ln -sf "$dest/cursor-agent" /root/.local/bin/cursor-agent
        ok=1; break
      fi
      rm -f /tmp/agent.tgz
    fi
    log "cursor-agent 安装第 $i 次失败，重试..."; sleep 10
  done
  [ "$ok" = 1 ]
  cursor-agent --version
}

install_claude_native() {
  curl_install https://claude.ai/install.sh bash
  claude --version
}

install_trae() {
  # TraeCode CLI 1.x（https://docs.trae.cn/cli_get-started-with-trae-cli）。官方 install.sh
  # 所在的 trae.cn 对海外 IP（GitHub runner）返回 403，这里照它的逻辑直接从 CDN 下载。
  # 2.0（install_v2.sh）的 `acp serve` 去掉了 backend 依赖的 --model，切换前须先改 backend/trae 的 Argv。
  local cdn="https://lf-cdn.trae.com.cn/obj/trae-com-cn/trae-cli" ver arch data="/root/.local/share/trae-cli"
  case "$(uname -m)" in
    x86_64|amd64) arch=amd64 ;;
    aarch64|arm64) arch=arm64 ;;
    *) log "错误：trae 不支持架构 $(uname -m)"; return 1 ;;
  esac
  ver="$(curl -fsSL --retry 5 --retry-delay 5 --retry-all-errors --connect-timeout 20 --max-time 60 "$cdn/trae-cli_latest_version.txt" | tr -d '[:space:]')"
  ver="${ver#v}"
  [ -n "$ver" ] || { log "错误：未取到 trae-cli 版本号"; return 1; }
  curl -fsSL --retry 8 --retry-delay 5 --retry-all-errors --connect-timeout 30 --max-time 600 \
    "$cdn/trae-cli_${ver}_linux_${arch}.tar.gz" -o /tmp/trae-cli.tgz
  rm -rf "$data" && mkdir -p "$data" /root/.local/bin
  tar -C "$data" -xzf /tmp/trae-cli.tgz
  rm -f /tmp/trae-cli.tgz
  chmod +x "$data/trae-cli"
  for n in trae-cli traecli trae-agent; do ln -sf "$data/trae-cli" "/root/.local/bin/$n"; done
  traecli --version
  traecli acp serve --help >/dev/null
}

install_one() {
  local provider="$1"
  log "安装 provider=$provider"
  case "$provider" in
    cursor|cursor_acp)
      install_cursor ;;                                      # 同一 cursor-agent 二进制（stream-json / ACP 两用）
    claude_code|claude_stream_json)
      install_claude_native ;;                               # 原生 claude CLI（stream-json，默认）
    claude_code_acp)
      install_claude_native && npm_global @zed-industries/claude-code-acp ;;
    codebuddy|codebuddy_acp)
      npm_global @tencent-ai/codebuddy-code ;;               # 同一 codebuddy 二进制（stream-json / ACP 两用）
    trae)
      install_trae ;;
    opencode)
      npm_global opencode-ai && opencode --version ;;
    codex)
      npm_global @openai/codex && codex --version ;;
    gemini)
      npm_global @google/gemini-cli && gemini --version ;;
    copilot)
      npm_global @github/copilot && copilot --version ;;
    kiro|qoder|grok|kimi|hermes|deveco|openclaw|antigravity|pi)
      log "错误：provider=$provider 暂无内置安装方式。"
      log "请通过 --build-arg AGENT_INSTALL_CMD='<安装命令>' 提供其官方安装步骤后再构建。"
      return 1 ;;
    *)
      log "错误：未知 provider=$provider（且未提供 AGENT_INSTALL_CMD）。"
      return 1 ;;
  esac
}

# 完全覆盖：给了 AGENT_INSTALL_CMD 就以它为准（适用于未内置安装方式的 provider）。
if [ -n "$custom_cmd" ]; then
  log "使用 AGENT_INSTALL_CMD 覆盖安装（providers=$raw）"
  eval "$custom_cmd"
  exit 0
fi

if [ "$raw" = "all" ]; then
  raw="$DEFAULT_PROVIDERS"
fi

# shellcheck disable=SC2206
optional=(${optional_raw//,/ })
is_optional() {
  local p="$1" o
  for o in "${optional[@]}"; do
    [ "$o" = "$p" ] && return 0
  done
  return 1
}

# shellcheck disable=SC2206
providers=(${raw//,/ })
if [ "${#providers[@]}" -eq 0 ]; then
  log "错误：AGENT_PROVIDERS 为空"
  exit 1
fi

failed_required=()
failed_optional=()
for provider in "${providers[@]}"; do
  provider="$(echo "$provider" | tr -d '[:space:]')"
  [ -n "$provider" ] || continue
  if install_one "$provider"; then
    log "provider=$provider 安装完成"
  elif is_optional "$provider"; then
    log "警告：可选 provider=$provider 安装失败，继续（AGENT_OPTIONAL_PROVIDERS=$optional_raw）"
    failed_optional+=("$provider")
  else
    log "错误：必需 provider=$provider 安装失败"
    failed_required+=("$provider")
  fi
done

if [ "${#failed_required[@]}" -gt 0 ]; then
  log "错误：必需 provider 安装失败：${failed_required[*]}"
  exit 1
fi
if [ "${#failed_optional[@]}" -gt 0 ]; then
  log "可选 provider 未装上（镜像仍可用）：${failed_optional[*]}"
fi
log "安装结束 providers=$raw"
