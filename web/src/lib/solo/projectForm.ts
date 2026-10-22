import { isSecretEnvKey } from '@/lib/agent/secretEnvKeys'
import type { CredentialItem, GitCredentialType, Project, ProjectInput } from '@/lib/api/apiTypes'
import { repoProtocol } from './soloPrefs'

export type KV = { k: string; v: string }

export type ProjectDraft = {
  name: string
  description: string
  repoUrl: string
  defaultBranch: string
  repoDirName: string
  gitCredentialType: GitCredentialType
  gitCredentialId: string
  sshHostsCredentialId: string
  env: KV[]
  bindings: KV[]
}

const ENV_KEY_RE = /^[A-Za-z_][A-Za-z0-9_]*$/
const SENSITIVE_MARKERS = ['TOKEN', 'SECRET', 'PASSWORD', 'PASSWD', 'API_KEY', 'APIKEY', 'PRIVATE_KEY', 'ACCESS_KEY', 'CREDENTIAL']
const RESERVED = new Set(['GIT_REPOS', 'GRASP_THREAD_ID', 'CODEX_HOME', 'ACP_BRIDGE_MODEL'])

/** Env key each git access mode reads its credential from. */
export const GIT_CREDENTIAL_ENV: Record<Exclude<GitCredentialType, ''>, string> = {
  github_https: 'GITHUB_TOKEN',
  gitlab_https: 'GITLAB_TOKEN',
  ssh: 'GIT_SSH_PRIVATE_KEY',
}
export const SSH_HOSTS_ENV = 'GIT_SSH_KNOWN_HOSTS'

export function isSensitiveEnvName(key: string): boolean {
  const k = key.trim().toUpperCase()
  return isSecretEnvKey(k) || SENSITIVE_MARKERS.some((m) => k.includes(m))
}

export type EnvKeyProblem = '' | 'invalid' | 'reserved' | 'sensitive'

export function envKeyProblem(key: string): EnvKeyProblem {
  const k = key.trim()
  if (!k) return ''
  if (!ENV_KEY_RE.test(k)) return 'invalid'
  if (RESERVED.has(k.toUpperCase())) return 'reserved'
  if (isSensitiveEnvName(k)) return 'sensitive'
  return ''
}

export function bindingKeyProblem(key: string): EnvKeyProblem {
  const k = key.trim()
  if (!k) return ''
  if (!ENV_KEY_RE.test(k)) return 'invalid'
  if (RESERVED.has(k.toUpperCase())) return 'reserved'
  return ''
}

export function emptyDraft(): ProjectDraft {
  return {
    name: '',
    description: '',
    repoUrl: '',
    defaultBranch: '',
    repoDirName: '',
    gitCredentialType: '',
    gitCredentialId: '',
    sshHostsCredentialId: '',
    env: [],
    bindings: [],
  }
}

export function draftFromProject(p: Project): ProjectDraft {
  return {
    name: p.name,
    description: p.description ?? '',
    repoUrl: p.repoUrl,
    defaultBranch: p.defaultBranch ?? '',
    repoDirName: p.repoDirName ?? '',
    gitCredentialType: p.gitCredentialType ?? '',
    gitCredentialId: p.gitCredentialId ?? '',
    sshHostsCredentialId: p.sshHostsCredentialId ?? '',
    env: Object.entries(p.env ?? {}).map(([k, v]) => ({ k, v })),
    bindings: Object.entries(p.credentialBindings ?? {}).map(([k, v]) => ({ k, v: v ?? '' })),
  }
}

export function inputFromDraft(d: ProjectDraft): ProjectInput {
  const env: Record<string, string> = {}
  for (const { k, v } of d.env) if (k.trim()) env[k.trim()] = v
  const credentialBindings: Record<string, string | null> = {}
  for (const { k, v } of d.bindings) if (k.trim() && v) credentialBindings[k.trim()] = v
  const type = d.gitCredentialType
  return {
    name: d.name.trim(),
    description: d.description.trim(),
    repoUrl: d.repoUrl.trim(),
    defaultBranch: d.defaultBranch.trim(),
    repoDirName: d.repoDirName.trim(),
    gitCredentialType: type,
    gitCredentialId: type ? d.gitCredentialId : '',
    sshHostsCredentialId: type === 'ssh' ? d.sshHostsCredentialId : '',
    env,
    credentialBindings,
  }
}

/** Default directory name the server derives from the URL. */
export function repoNameFromUrl(url: string): string {
  const s = url.trim().replace(/\/+$/, '').replace(/\.git$/i, '')
  const tail = s.split(/[/:]/).pop() ?? ''
  return /^[A-Za-z0-9._-]+$/.test(tail) ? tail : ''
}

export function isRepoUrl(url: string): boolean {
  const s = url.trim()
  return /^(https?|ssh|git):\/\/\S+$/i.test(s) || /^[\w.-]+@[^:\s]+:\S+$/.test(s)
}

/** URL scheme does not fit the chosen access mode. */
export function accessMismatch(url: string, type: GitCredentialType): 'https' | 'ssh' | '' {
  const proto = repoProtocol(url)
  if (!proto || !type) return ''
  if (type === 'ssh' && proto === 'https') return 'ssh'
  if (type !== 'ssh' && proto === 'ssh') return 'https'
  return ''
}

/** Credentials usable for an env key; falls back to everything when none match. */
/** Access-mode key for copy. Empty is this repository's own unset slot, not the Agent. */
export function repoAccessKey(type?: string): 'none' | 'github_https' | 'gitlab_https' | 'ssh' {
  switch (type) {
    case 'github_https':
    case 'gitlab_https':
    case 'ssh':
      return type
    default:
      return 'none'
  }
}

export function credentialsFor(items: CredentialItem[], envKey: string): CredentialItem[] {
  const usable = items.filter((c) => c.enabled !== false && !c.revokedAt)
  const hit = usable.filter((c) => c.envKey === envKey)
  return hit.length ? hit : usable
}

export function draftProblems(d: ProjectDraft): string[] {
  const out: string[] = []
  if (!d.name.trim()) out.push('name')
  if (!isRepoUrl(d.repoUrl)) out.push('repoUrl')
  if (/[\s|,]/.test(d.defaultBranch.trim())) out.push('branch')
  if (d.repoDirName.trim() && !/^[A-Za-z0-9._-]+$/.test(d.repoDirName.trim())) out.push('repoDir')
  if (d.gitCredentialType && !d.gitCredentialId) out.push('credential')
  if (d.env.some((e) => envKeyProblem(e.k))) out.push('env')
  if (d.bindings.some((b) => bindingKeyProblem(b.k) || (b.k.trim() && !b.v))) out.push('bindings')
  return out
}
