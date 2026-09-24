// @vitest-environment happy-dom
import { defineComponent, h } from 'vue'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import pages from '@/locales/zh-CN/pages.json'

const mocks = vi.hoisted(() => ({ redeem: vi.fn() }))

vi.mock('vue-router', () => ({ useRoute: () => ({ params: { runId: 'run-1', nodeId: 'ap1' } }) }))
vi.mock('@/lib/inbox/embedChat', async () => {
  const actual = await vi.importActual<typeof import('@/lib/inbox/embedChat')>('@/lib/inbox/embedChat')
  return { ...actual, redeemEmbedTicket: mocks.redeem }
})
vi.mock('@/views/PublicGateApprovalView.vue', () => ({
  default: defineComponent({
    props: { embedToken: { type: String, default: '' } },
    emits: ['status'],
    setup(props, { expose }) {
      expose({ addPick: vi.fn() })
      return () => h('div', { 'data-testid': 'chat-stub', 'data-token': props.embedToken })
    },
  }),
}))

import EmbedNodeChatView from './EmbedNodeChatView.vue'
import { loadEmbedSession, saveEmbedSession } from '@/lib/inbox/embedChat'

function mountView() {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': pages } })
  return mount(EmbedNodeChatView, { global: { plugins: [i18n] } })
}

beforeEach(() => {
  sessionStorage.clear()
  mocks.redeem.mockReset()
  history.replaceState(null, '', '/embed/runs/run-1/nodes/ap1/chat')
})
afterEach(() => vi.restoreAllMocks())

describe('EmbedNodeChatView', () => {
  it('redeems the fragment ticket, strips it from the URL and keeps the token', async () => {
    history.replaceState(null, '', '/embed/runs/run-1/nodes/ap1/chat#ticket=tk1')
    mocks.redeem.mockResolvedValue({ token: 'gse_a', expiresAt: '2099-01-01T00:00:00Z' })
    const w = mountView()
    await flushPromises()
    expect(mocks.redeem).toHaveBeenCalledWith('tk1', 'run-1', 'ap1')
    expect(window.location.hash).toBe('')
    expect(w.get('[data-testid="chat-stub"]').attributes('data-token')).toBe('gse_a')
    expect(loadEmbedSession('run-1', 'ap1')?.token).toBe('gse_a')
  })

  it('reuses the stored token after a reload without a ticket', async () => {
    saveEmbedSession('run-1', 'ap1', { token: 'gse_b', expiresAt: '2099-01-01T00:00:00Z' })
    const w = mountView()
    await flushPromises()
    expect(mocks.redeem).not.toHaveBeenCalled()
    expect(w.get('[data-testid="chat-stub"]').attributes('data-token')).toBe('gse_b')
  })

  it('shows the reopen hint without a usable credential', async () => {
    history.replaceState(null, '', '/embed/runs/run-1/nodes/ap1/chat#ticket=spent')
    mocks.redeem.mockResolvedValue(null)
    const w = mountView()
    await flushPromises()
    expect(w.find('[data-testid="chat-stub"]').exists()).toBe(false)
    expect(w.get('[data-testid="embed-chat-expired"]').text()).toContain('对话已断开')
  })

  it('drops the token when the chat reports it invalid', async () => {
    saveEmbedSession('run-1', 'ap1', { token: 'gse_c', expiresAt: '2099-01-01T00:00:00Z' })
    const w = mountView()
    await flushPromises()
    await w.getComponent('[data-testid="chat-stub"]').vm.$emit('status', 'revoked')
    await flushPromises()
    expect(loadEmbedSession('run-1', 'ap1')).toBeNull()
    expect(w.find('[data-testid="embed-chat-expired"]').exists()).toBe(true)
  })

  it('shows a network error when redemption fails and nothing is stored', async () => {
    history.replaceState(null, '', '/embed/runs/run-1/nodes/ap1/chat#ticket=tk2')
    mocks.redeem.mockRejectedValue(new Error('502'))
    const w = mountView()
    await flushPromises()
    expect(w.find('[data-testid="embed-chat-network"]').exists()).toBe(true)
  })

  it('tells the preview page it is ready only once the chat is live', async () => {
    const parent = { postMessage: vi.fn() }
    Object.defineProperty(window, 'parent', { value: parent, configurable: true })
    try {
      saveEmbedSession('run-1', 'ap1', { token: 'gse_d', expiresAt: '2099-01-01T00:00:00Z' })
      const w = mountView()
      await flushPromises()
      expect(parent.postMessage).not.toHaveBeenCalled()
      await w.getComponent('[data-testid="chat-stub"]').vm.$emit('status', 'active')
      await w.getComponent('[data-testid="chat-stub"]').vm.$emit('status', 'active')
      expect(parent.postMessage).toHaveBeenCalledTimes(1)
      expect(parent.postMessage.mock.calls[0][0]).toEqual({ type: 'grasp-embed:ready' })
    } finally {
      Object.defineProperty(window, 'parent', { value: window, configurable: true })
    }
  })
})
