/** Canonical Token-class env keys (ACP auth + Git tokens). Keep in sync with server/internal/envauth. */
export const TOKEN_ENV_KEYS = [
  'GRASP_CURSOR_API_KEY',
  'CURSOR_API_KEY',
  'GRASP_CLAUDE_API_KEY',
  'ANTHROPIC_API_KEY',
  'GRASP_CODEBUDDY_API_KEY',
  'CODEBUDDY_API_KEY',
  'GRASP_TRAE_API_KEY',
  'TRAE_API_KEY',
  'TRAECLI_PERSONAL_ACCESS_TOKEN',
  'GRASP_OPENCODE_API_KEY',
  'OPENCODE_API_KEY',
  'OPENAI_API_KEY',
  'CODEX_API_KEY',
  'GITHUB_TOKEN',
  'GITLAB_TOKEN',
  'GIT_SSH_PRIVATE_KEY',
] as const

export type TokenEnvKey = (typeof TOKEN_ENV_KEYS)[number]

const TOKEN_SET = new Set<string>(TOKEN_ENV_KEYS)

export function isTokenEnvKey(key: string): boolean {
  return TOKEN_SET.has(key.trim())
}

/** Git Token keys that Agent-management「添加推荐变量」must not inject. */
export const GIT_TOKEN_ENV_KEYS = [
  'GITHUB_TOKEN',
  'GITLAB_TOKEN',
  'GIT_SSH_PRIVATE_KEY',
] as const

const GIT_TOKEN_SET = new Set<string>(GIT_TOKEN_ENV_KEYS)

export function isGitTokenEnvKey(key: string): boolean {
  return GIT_TOKEN_SET.has(key.trim())
}

export function stripTokenKeysFromRecord(env: Record<string, string>): Record<string, string> {
  const out: Record<string, string> = {}
  for (const [k, v] of Object.entries(env)) {
    if (!isTokenEnvKey(k)) out[k] = v
  }
  return out
}

export function stripTokenKeysFromKV<T extends { k: string; v: string }>(env: T[]): T[] {
  return env.filter((e) => !isTokenEnvKey(e.k))
}
