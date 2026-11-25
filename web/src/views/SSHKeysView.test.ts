// @vitest-environment happy-dom
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import pages from '@/locales/zh-CN/pages.json'
import common from '@/locales/zh-CN/common.json'
import type { SSHGatewayInfo, UserSSHKey } from '@/lib/api/apiTypes'
import { resetSSHGatewayForTest } from '@/lib/sandbox/useSSHGateway'
import SSHKeysView from './SSHKeysView.vue'

const state = vi.hoisted(() => ({
  keys: [] as UserSSHKey[],
  gateway: { enabled: true, allowed: true } as SSHGatewayInfo,
  addError: '' as string,
  listError: '' as string,
}))
const toasts = vi.hoisted(() => [] as string[])

vi.mock('@/lib/api/api', () => ({
  api: {
    sshGateway: vi.fn(async () => state.gateway),
    listSSHKeys: vi.fn(async () => {
      if (state.listError) throw new Error(state.listError)
      return { keys: state.keys }
    }),
    addSSHKey: vi.fn(async (publicKey: string, name: string) => {
      if (state.addError) throw new Error(state.addError)
      return { id: 9, name: name || 'me@laptop', publicKey, fingerprint: 'SHA256:new', createdAt: new Date().toISOString() }
    }),
    deleteSSHKey: vi.fn(async () => ({ status: 'ok' })),
  },
}))
vi.mock('@/lib/composables/useToast', () => ({ useToast: () => ({ show: (m: string) => toasts.push(m) }) }))

const mounted: Array<ReturnType<typeof mount>> = []

async function render() {
  const wrapper = mount(SSHKeysView, {
    attachTo: document.body,
    global: {
      plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...pages, ...common } } })],
      stubs: { Icon: true, Teleport: true },
    },
  })
  mounted.push(wrapper)
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  resetSSHGatewayForTest()
  state.keys = [{ id: 1, name: 'laptop', publicKey: 'ssh-ed25519 AAAA', fingerprint: 'SHA256:old', createdAt: '2026-11-20T10:00:00Z' }]
  state.gateway = { enabled: true, allowed: true }
  state.addError = ''
  state.listError = ''
  toasts.length = 0
})

afterEach(() => {
  while (mounted.length) mounted.pop()!.unmount()
  document.body.innerHTML = ''
})

describe('SSHKeysView', () => {
  it('lists keys, adds one and deletes one', async () => {
    const wrapper = await render()
    expect(wrapper.findAll('[data-testid="ssh-key-row"]')).toHaveLength(1)
    expect(wrapper.text()).toContain('SHA256:old')
    expect(wrapper.text()).toContain('尚未使用')
    expect(wrapper.find('[data-testid="ssh-keys-gateway-notice"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="ssh-keys-submit"]').attributes('disabled')).toBeDefined()

    await wrapper.get('[data-testid="ssh-keys-public-key"]').setValue('ssh-ed25519 BBBB me@laptop')
    await wrapper.get('[data-testid="ssh-keys-form"]').trigger('submit')
    await flushPromises()
    expect(wrapper.findAll('[data-testid="ssh-key-row"]')).toHaveLength(2)
    expect(toasts).toEqual(['公钥已添加'])
    expect((wrapper.get('[data-testid="ssh-keys-public-key"]').element as HTMLTextAreaElement).value).toBe('')

    await wrapper.findAll('[data-testid="ssh-key-delete"]')[0]!.trigger('click')
    expect(wrapper.get('[data-testid="ssh-key-delete-body"]').text()).toContain('「laptop」')
    await wrapper.get('[data-testid="ssh-key-delete-confirm"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('[data-testid="ssh-key-row"]')).toHaveLength(1)
    expect(toasts).toContain('公钥已删除')
  })

  it('shows validation errors and the empty state', async () => {
    state.keys = []
    state.addError = '无法识别的 SSH 公钥'
    const wrapper = await render()
    expect(wrapper.find('[data-testid="ssh-keys-empty"]').exists()).toBe(true)
    await wrapper.get('[data-testid="ssh-keys-public-key"]').setValue('nope')
    await wrapper.get('[data-testid="ssh-keys-form"]').trigger('submit')
    await flushPromises()
    expect(wrapper.get('[data-testid="ssh-keys-error"]').text()).toBe('无法识别的 SSH 公钥')
  })

  it('warns when the gateway is off or admin-only, and retries a failed load', async () => {
    state.gateway = { enabled: false }
    let wrapper = await render()
    expect(wrapper.get('[data-testid="ssh-keys-gateway-notice"]').text()).toContain('尚未开启 SSH 网关')
    wrapper.unmount()
    mounted.pop()

    resetSSHGatewayForTest()
    state.gateway = { enabled: true, allowed: false }
    state.listError = 'boom'
    wrapper = await render()
    expect(wrapper.get('[data-testid="ssh-keys-gateway-notice"]').text()).toContain('只对管理员开放')
    expect(wrapper.text()).toContain('boom')
    state.listError = ''
    await wrapper.findAll('button').find((b) => b.text() === '重试')!.trigger('click')
    await flushPromises()
    expect(wrapper.findAll('[data-testid="ssh-key-row"]')).toHaveLength(1)
  })
})
