import type { BackendId } from '@/lib/shared/regionPolicy'

/** Studio Env badge text (short). Kept for reuse with wizard auth step. */
export const BACKEND_AUTH_HINTS: Record<
  BackendId,
  { key: string; note: string }
> = {
  cursor: { key: 'GRASP_CURSOR_API_KEY', note: 'Cursor ACP 鉴权' },
  claude_code: {
    key: 'GRASP_CLAUDE_API_KEY',
    note: 'Claude Code ACP 鉴权',
  },
  codebuddy: {
    key: 'GRASP_CODEBUDDY_API_KEY',
    note: 'CodeBuddy ACP 鉴权',
  },
  trae: {
    key: 'GRASP_TRAE_API_KEY',
    note: 'Trae ACP 鉴权 (CLI 登录令牌)',
  },
  opencode: {
    key: 'GRASP_OPENCODE_API_KEY',
    note: 'OpenCode API Key 鉴权',
  },
  deepseek_harness: { key: 'GRASP_DEEPSEEK_API_KEY', note: 'DeepSeek Harness API Key 鉴权' },
  kimi_code: { key: 'GRASP_KIMI_CODE_API_KEY', note: 'Kimi Code 订阅 API Key 鉴权' },
  codex: {
    key: 'GRASP_CODEX_AUTH_JSON',
    note: 'Codex ChatGPT 登录文件',
  },
}

/** Relative path for backend config file written into Agent workspace/files. */
export const AGENT_SETTINGS_REL_PATH = 'settings.json'
const AGENT_OPENCODE_CONFIG_REL_PATH = 'opencode.json'

export function agentConfigRelPath(backend: BackendId): string {
  return backend === 'opencode' ? AGENT_OPENCODE_CONFIG_REL_PATH : AGENT_SETTINGS_REL_PATH
}

/** Default JSON placeholder when user switches to custom config mode. */
export function defaultSettingsPlaceholder(backend: BackendId): string {
  switch (backend) {
    case 'claude_code':
      return JSON.stringify({ env: { ANTHROPIC_API_KEY: 'sk-ant-...' } }, null, 2)
    case 'codebuddy':
      return JSON.stringify({ env: { CODEBUDDY_API_KEY: 'your-key' } }, null, 2)
    case 'cursor':
      return JSON.stringify({ env: { CURSOR_API_KEY: 'your-key' } }, null, 2)
    case 'trae':
      return JSON.stringify({ env: { TRAECLI_PERSONAL_ACCESS_TOKEN: 'trae-lt-...' } }, null, 2)
    case 'opencode':
      return JSON.stringify(
        {
          $schema: 'https://opencode.ai/config.json',
          model: 'openai/gpt-4.1',
          provider: {
            openai: {
              options: { apiKey: '{env:OPENCODE_API_KEY}' },
            },
          },
        },
        null,
        2,
      )
    default:
      return '{\n  \n}'
  }
}
