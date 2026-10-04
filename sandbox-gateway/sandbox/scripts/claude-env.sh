# shellcheck shell=bash
# Sourced fragment (defines ai-code). No shebang: direct exec stays unchanged.
# Claude Code 环境变量：均由容器环境注入，不在通用镜像里硬编码任何具体端点/模型/密钥。
# 平台/业务方可注入 ANTHROPIC_BASE_URL、ANTHROPIC_AUTH_TOKEN、CLAUDE_MODEL 等（claude 会自行读取）。
export IS_SANDBOX="${IS_SANDBOX:-1}"
export API_TIMEOUT_MS="${API_TIMEOUT_MS:-6000000}"

# ai-code：claude 便捷封装；模型由 CLAUDE_MODEL 指定，未设则用 claude 默认模型。
ai-code() {
  if [ -n "${CLAUDE_MODEL:-}" ]; then
    claude --model "${CLAUDE_MODEL}" --dangerously-skip-permissions "$@"
  else
    claude --dangerously-skip-permissions "$@"
  fi
}
