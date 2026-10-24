export type CredentialKindId =
  | 'cursor'
  | 'claude_code'
  | 'codebuddy'
  | 'trae'
  | 'codex'
  | 'opencode'
  | 'deepseek_harness'
  | 'github'
  | 'gitlab'
  | 'ssh_key'
  | 'ssh_hosts'

export type CredentialSecretKind = 'apiKey' | 'loginFile' | 'multiline' | 'model'

export type CredentialKind = {
  id: CredentialKindId
  type: 'ai' | 'git' | 'ssh'
  provider: string
  envKey: string
  secret: CredentialSecretKind
  logoProvider?: string
  icon?: 'key' | 'host'
}

export const CREDENTIAL_KINDS: CredentialKind[] = [
  { id: 'cursor', type: 'ai', provider: 'cursor', envKey: 'GRASP_CURSOR_API_KEY', secret: 'apiKey', logoProvider: 'cursor' },
  { id: 'claude_code', type: 'ai', provider: 'claude_code', envKey: 'GRASP_CLAUDE_API_KEY', secret: 'apiKey', logoProvider: 'claude' },
  { id: 'codebuddy', type: 'ai', provider: 'codebuddy', envKey: 'GRASP_CODEBUDDY_API_KEY', secret: 'apiKey', logoProvider: 'codebuddy' },
  { id: 'trae', type: 'ai', provider: 'trae', envKey: 'GRASP_TRAE_API_KEY', secret: 'apiKey', logoProvider: 'trae' },
  { id: 'codex', type: 'ai', provider: 'codex', envKey: 'GRASP_CODEX_AUTH_JSON', secret: 'loginFile', logoProvider: 'codex' },
  { id: 'opencode', type: 'ai', provider: 'opencode', envKey: 'GRASP_OPENCODE_API_KEY', secret: 'model', logoProvider: 'opencode' },
  { id: 'deepseek_harness', type: 'ai', provider: 'deepseek_harness', envKey: 'GRASP_DEEPSEEK_API_KEY', secret: 'apiKey', logoProvider: 'deepseek' },
  { id: 'github', type: 'git', provider: 'github', envKey: 'GITHUB_TOKEN', secret: 'apiKey', logoProvider: 'github' },
  { id: 'gitlab', type: 'git', provider: 'gitlab', envKey: 'GITLAB_TOKEN', secret: 'apiKey', logoProvider: 'gitlab' },
  { id: 'ssh_key', type: 'ssh', provider: 'ssh', envKey: 'GIT_SSH_PRIVATE_KEY', secret: 'multiline', icon: 'key' },
  { id: 'ssh_hosts', type: 'ssh', provider: 'ssh', envKey: 'GIT_SSH_KNOWN_HOSTS', secret: 'multiline', icon: 'host' },
]

type KindItem = {
  id?: string
  name?: string
  provider?: string
  envKey?: string
  type?: string
  purpose?: string
  toolId?: string
  secretKind?: string
}

export function aliasKey(name: string): string {
  return name.trim().toLowerCase()
}

export function kindById(id: string): CredentialKind | undefined {
  return CREDENTIAL_KINDS.find((kind) => kind.id === id)
}

export function kindOfItem(item: KindItem): CredentialKind | undefined {
  if (item.purpose === 'tool_auth') return CREDENTIAL_KINDS.find(kind => kind.type === 'ai' && kind.id === item.toolId && !['opencode', 'deepseek_harness'].includes(kind.id))
  if (item.purpose === 'git') return CREDENTIAL_KINDS.find(kind => kind.type === 'git' && kind.provider === item.provider)
  if (item.purpose === 'ssh') {
    if (item.secretKind === 'ssh_private_key') return kindById('ssh_key')
    if (item.secretKind === 'ssh_known_hosts') return kindById('ssh_hosts')
  }
  return undefined
}

/** List presentation only. Never use this fallback to authorize a run. */
export function displayKindOfItem(item: KindItem): CredentialKind | undefined {
  const declared = kindOfItem(item)
  if (declared || item.purpose) return declared
  const provider = item.provider?.trim().toLowerCase()
  const type = item.type?.toLowerCase()
  if (type === 'ai') return CREDENTIAL_KINDS.find(kind => kind.type === 'ai' && kind.provider === provider)
  if (type === 'git') return CREDENTIAL_KINDS.find(kind => kind.type === 'git' && kind.provider === provider)
  if (type === 'ssh' && provider === 'ssh') {
    if (item.envKey === 'GIT_SSH_KNOWN_HOSTS') return kindById('ssh_hosts')
    if (item.envKey === 'GIT_SSH_PRIVATE_KEY') return kindById('ssh_key')
  }
  return undefined
}

export function defaultGitCredentialHost(provider: string): string {
  return provider === 'github' ? 'github.com' : provider === 'gitlab' ? 'gitlab.com' : ''
}

/** A Git credential identifies one HTTP(S) origin, never a repository path. */
export function normalizeGitCredentialHost(raw: string): string | null {
  const value = raw.trim()
  if (!value || /[\\\s]/.test(value) || !/^(?:https?:\/\/)?[^/?#]+\/?$/i.test(value)) return null
  try {
    const url = new URL(value.includes('://') ? value : `https://${value}`)
    if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password || url.pathname !== '/' || url.search || url.hash || url.port === '0') return null
    if (!url.hostname.startsWith('[') && !url.hostname.split('.').every(label => /^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$/i.test(label))) return null
    return url.origin
  } catch { return null }
}

/** Existing display alias when this kind already uses the name, otherwise empty. */
export function conflictingAlias(items: KindItem[], kind: CredentialKind, alias: string, exceptId = ''): string {
  const want = aliasKey(alias)
  if (!want) return ''
  for (const item of items) {
    if (exceptId && item.id === exceptId) continue
    const itemKind = kindOfItem(item)
    if (!itemKind || itemKind.id !== kind.id) continue
    if (aliasKey(item.name || '') === want) return (item.name || '').trim()
  }
  return ''
}

export function backendKind(backend: string): CredentialKind | undefined {
  switch (backend) {
    case 'cursor':
      return kindById('cursor')
    case 'claude_code':
      return kindById('claude_code')
    case 'codebuddy':
      return kindById('codebuddy')
    case 'trae':
      return kindById('trae')
    case 'codex':
      return kindById('codex')
    case 'deepseek_harness':
      return kindById('deepseek_harness')
    case 'opencode':
      return kindById('opencode')
    default:
      return undefined
  }
}

export function gitKind(gitType: string): CredentialKind | undefined {
  switch (gitType) {
    case 'github_https':
      return kindById('github')
    case 'gitlab_https':
      return kindById('gitlab')
    case 'ssh':
      return kindById('ssh_key')
    default:
      return undefined
  }
}
