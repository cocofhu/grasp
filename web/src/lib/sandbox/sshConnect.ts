import type { SSHGatewayInfo } from '@/lib/api/apiTypes'

/** Host alias the ~/.ssh/config snippet defines and IDE links point at. */
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

export type RemoteIDE = 'vscode' | 'cursor'

/** Opens the sandbox workspace in a local IDE via its Remote-SSH extension. */
export function remoteIDELink(ide: RemoteIDE, sandboxId: number, folder = SANDBOX_WORKSPACE): string {
  return `${ide}://vscode-remote/ssh-remote+${sshUser(sandboxId)}@${SSH_HOST_ALIAS}${folder}`
}
