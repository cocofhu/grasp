import { req } from '../httpCore'
import type { SSHGatewayInfo, UserSSHKey } from '../apiTypes'

export const sshClient = {
  sshGateway: () => req<SSHGatewayInfo>('/ssh-gateway'),
  listSSHKeys: () => req<{ keys: UserSSHKey[] }>('/me/ssh-keys'),
  addSSHKey: (publicKey: string, name = '') =>
    req<UserSSHKey>('/me/ssh-keys', { method: 'POST', body: JSON.stringify({ name, publicKey }) }),
  deleteSSHKey: (id: number) => req<{ status: string }>(`/me/ssh-keys/${id}`, { method: 'DELETE' }),
}
