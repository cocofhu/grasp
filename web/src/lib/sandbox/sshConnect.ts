import type { SSHGatewayInfo } from '@/lib/api/apiTypes'

/** Host alias the optional ~/.ssh/config snippet defines. */
export const SSH_HOST_ALIAS = 'grasp'

export const SANDBOX_WORKSPACE = '/root/workspace'

export interface SSHEndpoint {
  host: string
  port: number
}

/** The gateway address users connect to; an empty host means the page's host. */
export function sshEndpoint(info: SSHGatewayInfo, pageHost: string): SSHEndpoint {
  return { host: (info.host || '').trim() || pageHost, port: info.port || 22 }
}

export function sshUser(sandboxId: number): string {
  return `sbx-${sandboxId}`
}

/** user@host[:port], the Remote-SSH target that needs no ~/.ssh/config entry. */
export function sshTarget(sandboxId: number, ep: SSHEndpoint): string {
  return `${sshUser(sandboxId)}@${ep.host}${ep.port === 22 ? '' : `:${ep.port}`}`
}

/** Works without any ~/.ssh/config entry. */
export function sshCommand(sandboxId: number, ep: SSHEndpoint): string {
  const port = ep.port === 22 ? '' : `-p ${ep.port} `
  return `ssh ${port}${sshUser(sandboxId)}@${ep.host}`
}

export function sshConfigSnippet(ep: SSHEndpoint): string {
  return [
    `Host ${SSH_HOST_ALIAS}`,
    `  HostName ${ep.host}`,
    `  Port ${ep.port}`,
    '  ServerAliveInterval 30',
    '',
  ].join('\n')
}

export type RemoteIDE = 'vscode' | 'cursor' | 'codebuddy' | 'trae' | 'jetbrains'

export interface RemoteIDEOption {
  id: RemoteIDE
  label: string
  /** URL scheme of the VS Code based app; absent when there is no one-click link. */
  scheme?: string
  /** The China edition is a separate app with its own scheme. */
  cnScheme?: string
}

export const REMOTE_IDES: readonly RemoteIDEOption[] = [
  { id: 'vscode', label: 'VS Code', scheme: 'vscode' },
  { id: 'cursor', label: 'Cursor', scheme: 'cursor' },
  { id: 'codebuddy', label: 'CodeBuddy', scheme: 'codebuddy', cnScheme: 'codebuddycn' },
  { id: 'trae', label: 'Trae', scheme: 'trae', cnScheme: 'trae-cn' },
  // Gateway links only attach to an IDE already installed on the host, so
  // users create the SSH connection in Gateway from the connection details.
  { id: 'jetbrains', label: 'JetBrains Gateway' },
]

export function remoteIDEOption(id: string): RemoteIDEOption | undefined {
  return REMOTE_IDES.find((ide) => ide.id === id)
}

/**
 * Opens the sandbox workspace through the app's Remote-SSH support. Empty when
 * the IDE has no one-click link.
 */
export function remoteIDELink(ide: RemoteIDE, sandboxId: number, ep: SSHEndpoint, opts: { cn?: boolean; folder?: string } = {}): string {
  const option = remoteIDEOption(ide)
  const scheme = opts.cn ? option?.cnScheme : option?.scheme
  if (!scheme) return ''
  return `${scheme}://vscode-remote/ssh-remote+${sshTarget(sandboxId, ep)}${opts.folder ?? SANDBOX_WORKSPACE}`
}

const PREFERRED_KEY = 'grasp.sandbox.localIde'

export interface PreferredIDE {
  id: RemoteIDE
  cn: boolean
}

export function loadPreferredIDE(): PreferredIDE {
  try {
    const raw = JSON.parse(localStorage.getItem(PREFERRED_KEY) || 'null') as Partial<PreferredIDE> | null
    const option = raw && remoteIDEOption(String(raw.id))
    if (option) return { id: option.id, cn: !!raw?.cn && !!option.cnScheme }
  } catch {
    // Unreadable storage falls back to the default.
  }
  return { id: 'vscode', cn: false }
}

export function savePreferredIDE(value: PreferredIDE) {
  try {
    localStorage.setItem(PREFERRED_KEY, JSON.stringify(value))
  } catch {
    // Private mode or full storage: the choice just is not remembered.
  }
}
