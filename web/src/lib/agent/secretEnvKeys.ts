/**
 * Env keys that may only be supplied by project credentials (credential preset
 * keys plus the CLI auth keys they map to). Agent / shared Agent env saves that
 * contain any of them are rejected with 400. Keep in sync with
 * server/internal/envauth SecretEnvKeys.
 */
export const SECRET_ENV_KEYS = [
  'GRASP_CURSOR_API_KEY',
  'GRASP_CLAUDE_API_KEY',
  'GRASP_CODEBUDDY_API_KEY',
  'GRASP_TRAE_API_KEY',
  'GRASP_OPENCODE_API_KEY',
  'GITHUB_TOKEN',
  'GITLAB_TOKEN',
  'GIT_SSH_PRIVATE_KEY',
  'GIT_SSH_KNOWN_HOSTS',
  'CURSOR_API_KEY',
  'ANTHROPIC_API_KEY',
  'CODEBUDDY_API_KEY',
  'TRAECLI_PERSONAL_ACCESS_TOKEN',
  'OPENCODE_API_KEY',
] as const

export type SecretEnvKey = (typeof SECRET_ENV_KEYS)[number]

const SECRET_SET = new Set<string>(SECRET_ENV_KEYS)

export function isSecretEnvKey(key: string): boolean {
  return SECRET_SET.has(key.trim())
}

export function stripSecretKeysFromRecord(env: Record<string, string>): Record<string, string> {
  const out: Record<string, string> = {}
  for (const [k, v] of Object.entries(env)) {
    if (!isSecretEnvKey(k)) out[k] = v
  }
  return out
}

export function stripSecretKeysFromKV<T extends { k: string; v: string }>(env: T[]): T[] {
  return env.filter((e) => !isSecretEnvKey(e.k))
}
