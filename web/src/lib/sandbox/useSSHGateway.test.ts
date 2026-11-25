// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { sshClient } from '@/lib/api/clients/sshClient'
import { resetSSHGatewayForTest, useSSHGateway } from './useSSHGateway'

describe('sshClient', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ enabled: true, keys: [] }), { status: 200, headers: { 'Content-Type': 'application/json' } })))
  })
  afterEach(() => vi.unstubAllGlobals())

  it('builds the gateway and key endpoints', async () => {
    await sshClient.sshGateway()
    await sshClient.listSSHKeys()
    await sshClient.addSSHKey('ssh-ed25519 AAAA', 'laptop')
    await sshClient.addSSHKey('ssh-ed25519 BBBB')
    await sshClient.deleteSSHKey(4)
    const calls = vi.mocked(fetch).mock.calls.map(([url, o]) => [String(url), o?.method || 'GET', o?.body])
    expect(calls).toEqual([
      ['/api/ssh-gateway', 'GET', undefined],
      ['/api/me/ssh-keys', 'GET', undefined],
      ['/api/me/ssh-keys', 'POST', JSON.stringify({ name: 'laptop', publicKey: 'ssh-ed25519 AAAA' })],
      ['/api/me/ssh-keys', 'POST', JSON.stringify({ name: '', publicKey: 'ssh-ed25519 BBBB' })],
      ['/api/me/ssh-keys/4', 'DELETE', undefined],
    ])
  })
})

describe('useSSHGateway', () => {
  beforeEach(() => resetSSHGatewayForTest())
  afterEach(() => vi.unstubAllGlobals())

  it('caches the status, shares in-flight loads and refreshes on demand', async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ enabled: true, port: 2222 }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)
    const { info, load } = useSSHGateway()
    const [a, b] = await Promise.all([load(), load()])
    expect(a).toEqual({ enabled: true, port: 2222 })
    expect(b).toBe(a)
    expect(info.value).toEqual({ enabled: true, port: 2222 })
    await load()
    expect(fetchMock).toHaveBeenCalledTimes(1)
    await load(true)
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('treats a failed lookup as disabled', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('{}', { status: 500, headers: { 'Content-Type': 'application/json' } })))
    const { info, load } = useSSHGateway()
    expect(await load()).toEqual({ enabled: false })
    expect(info.value).toEqual({ enabled: false })
  })
})
