import type { AgentGitBindings, CredentialItem } from '@/lib/api/api'
import { kindOfItem } from '@/lib/workspace/credentialKinds'
import { credentialUsable } from './modelRouting'

export const LEGACY_GIT_AUTH_KEYS = new Set(['GITHUB_TOKEN', 'GITLAB_TOKEN', 'GIT_SSH_PRIVATE_KEY', 'GIT_SSH_KNOWN_HOSTS', 'GITHUB_URL', 'GITLAB_URL', 'GH_TOKEN', 'GH_ENTERPRISE_TOKEN', 'GITHUB_ENTERPRISE_TOKEN', 'GLAB_TOKEN', 'GITLAB_ACCESS_TOKEN'])

export function cloneGitBindings(value: AgentGitBindings): AgentGitBindings {
  return { items: value.items.map((item) => ({ ...item })), ...(value.preferredCredentialId ? { preferredCredentialId: value.preferredCredentialId } : {}) }
}

export function gitCredentialKind(item?: CredentialItem) {
  const kind = item ? kindOfItem(item) : undefined
  return kind && ['github', 'gitlab', 'ssh_key'].includes(kind.id) ? kind : undefined
}

export function usableGitCredential(item?: CredentialItem): boolean {
  return !!item && !!gitCredentialKind(item) && credentialUsable(item)
}

type LegacyGit = {
  gitCredentialId?: string
  sshHostsCredentialId?: string
  gitCredentialType?: string
  credentialBindings?: Record<string, string | null>
}

/** Display a legacy selection without migrating it until the user changes Git. */
export function legacyGitBindings(value: LegacyGit): AgentGitBindings | undefined {
  const selectedKey = value.gitCredentialType === 'ssh' ? 'GIT_SSH_PRIVATE_KEY' : value.gitCredentialType === 'gitlab_https' ? 'GITLAB_TOKEN' : value.gitCredentialType === 'github_https' ? 'GITHUB_TOKEN' : ''
  const generic = value.credentialBindings || {}
  const preferred = value.gitCredentialId || (selectedKey ? generic[selectedKey] : '')
  const ids = [...new Set([preferred, generic.GITHUB_TOKEN, generic.GITLAB_TOKEN, generic.GIT_SSH_PRIVATE_KEY].filter((id): id is string => typeof id === 'string' && !!id))]
  if (!ids.length) return value.sshHostsCredentialId || Object.keys(generic).some((key) => LEGACY_GIT_AUTH_KEYS.has(key)) ? { items: [] } : undefined
  const hosts = value.sshHostsCredentialId || generic.GIT_SSH_KNOWN_HOSTS
  return { items: ids.map((credentialId) => ({ credentialId, ...(((credentialId === value.gitCredentialId && value.gitCredentialType === 'ssh') || credentialId === generic.GIT_SSH_PRIVATE_KEY) && hosts ? { sshHostsCredentialId: hosts } : {}) })), preferredCredentialId: preferred || ids[0] }
}

/** A deliberate edit moves authorization to the new list and removes the legacy single slots. */
export function setDraftGitBindings<T extends LegacyGit & { gitBindings?: AgentGitBindings; env: { k: string; v: string }[] }>(draft: T, value: AgentGitBindings): void {
  draft.gitBindings = cloneGitBindings(value)
  draft.gitCredentialType = undefined
  draft.gitCredentialId = ''
  draft.sshHostsCredentialId = ''
  if (draft.credentialBindings) draft.credentialBindings = Object.fromEntries(Object.entries(draft.credentialBindings).filter(([key]) => !LEGACY_GIT_AUTH_KEYS.has(key)))
  draft.env = draft.env.filter((entry) => !LEGACY_GIT_AUTH_KEYS.has(entry.k.trim()))
}

function validScopeHost(raw: string): boolean {
  if (!raw || /[\\/?#@%\s]/.test(raw)) return false
  try {
    const host = new URL(`https://${raw}`)
    if (!host.hostname || host.username || host.password || host.pathname !== '/' || host.search || host.hash) return false
    const port = raw.startsWith('[') ? raw.slice(raw.indexOf(']') + 1) : raw.includes(':') ? raw.slice(raw.lastIndexOf(':')) : ''
    if (port && (!/^:\d+$/.test(port) || Number(port.slice(1)) < 1 || Number(port.slice(1)) > 65535)) return false
    if (host.hostname.startsWith('[')) return true // URL validates bracketed IPv6 addresses.
    return host.hostname.replace(/\.$/, '').split('.').every((part) => /^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$/i.test(part))
  } catch { return false }
}
function validScopePath(raw: string): boolean {
  try {
    const path = decodeURIComponent(raw).replace(/\/+$/, '')
    return !!path && !/[\\?#\s\x00-\x1f\x7f]/.test(path) && path.split('/').every((part) => !!part && part !== '.' && part !== '..')
  } catch { return false }
}

/** Validate the same host/repository grammar accepted by the control plane. */
export function validGitScope(raw: string, kind?: string): boolean {
  const value = raw.trim()
  if (!value) return true
  if (/[\\?#\s\x00-\x1f\x7f]/.test(value)) return false
  const scheme = /^([a-z][a-z\d+.-]*):\/\//i.exec(value)
  if (scheme) {
    const parts = /^(https?|ssh):\/\/([^/]+)(?:\/(.*))?$/i.exec(value)
    if (!parts) return false
    const protocol = parts[1].toLowerCase()
    if ((kind === 'ssh_key' && protocol !== 'ssh') || ((kind === 'github' || kind === 'gitlab') && protocol === 'ssh')) return false
    let host = parts[2]
    if (host.includes('@')) {
      if (protocol !== 'ssh') return false
      const user = host.slice(0, host.indexOf('@'))
      if (!/^[a-zA-Z0-9._-]+$/.test(user)) return false
      host = host.slice(host.indexOf('@') + 1)
    }
    if (!validScopeHost(host)) return false
    return parts[3] ? validScopePath(parts[3]) : protocol !== 'ssh'
  }
  if (value.includes('@')) {
    const parts = /^([a-zA-Z0-9._-]+)@([^:]+):(.+)$/.exec(value)
    return !!parts && kind !== 'github' && kind !== 'gitlab' && validScopeHost(parts[2]) && validScopePath(parts[3])
  }
  return validScopeHost(value)
}
