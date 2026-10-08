export type CredentialKindId =
  | 'cursor'
  | 'claude_code'
  | 'codebuddy'
  | 'trae'
  | 'codex'
  | 'opencode'
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
}

export function aliasKey(name: string): string {
  return name.trim().toLowerCase()
}

export function kindById(id: string): CredentialKind | undefined {
  return CREDENTIAL_KINDS.find((kind) => kind.id === id)
}

export function kindOfItem(item: KindItem): CredentialKind | undefined {
  const env = (item.envKey || '').trim()
  if (env) return CREDENTIAL_KINDS.find((kind) => kind.envKey === env)
  const provider = (item.provider || '').trim().toLowerCase()
  const blob = `${item.name || ''} ${item.type || ''}`.toLowerCase()
  if (provider === 'ssh' || (item.type || '').toLowerCase() === 'ssh') {
    if (blob.includes('known') || blob.includes('host')) return kindById('ssh_hosts')
    return kindById('ssh_key')
  }
  if (provider === 'claude' || provider === 'anthropic') return kindById('claude_code')
  return CREDENTIAL_KINDS.find((kind) => kind.provider === provider)
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
