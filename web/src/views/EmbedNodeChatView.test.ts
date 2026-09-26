// @vitest-environment happy-dom
import { defineComponent, h } from 'vue'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import pages from '@/locales/zh-CN/pages.json'

const mocks = vi.hoisted(() => ({ redeem: vi.fn(), sendFrame: vi.fn(() => true) }))

vi.mock('vue-router', () => ({ useRoute: () => ({ params: { runId: 'run-1', nodeId: 'ap1' } }) }))
vi.mock('@/lib/inbox/embedChat', async () => {
  const actual = await vi.importActual<typeof import('@/lib/inbox/embedChat')>('@/lib/inbox/embedChat')
  return { ...actual, redeemEmbedTicket: mocks.redeem }
})
vi.mock('@/views/PublicGateApprovalView.vue', () => ({
  default: defineComponent({
    props: { embedToken: { type: String, default: '' } },
    emits: ['status', 'events-ready', 'events-closed', 'page-frame'],
    setup(props, { expose }) {
      expose({ addPick: vi.fn(), sendEventsFrame: mocks.sendFrame })
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
  localStorage.clear()
  mocks.redeem.mockReset()
  mocks.sendFrame.mockClear()
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

  it('tells the preview page when there is no usable session, so it can grey out Pick and Chat', async () => {
    const parent = { postMessage: vi.fn() }
    Object.defineProperty(window, 'parent', { value: parent, configurable: true })
    try {
      const w = mountView()
      await flushPromises()
      expect(w.find('[data-testid="embed-chat-expired"]').exists()).toBe(true)
      expect(parent.postMessage.mock.calls.map((c) => c[0])).toEqual([{ type: 'grasp-embed:session', ok: false }])

      parent.postMessage.mockClear()
      saveEmbedSession('run-1', 'ap1', { token: 'gse_e', expiresAt: '2099-01-01T00:00:00Z' })
      const live = mountView()
      await flushPromises()
      await live.getComponent('[data-testid="chat-stub"]').vm.$emit('status', 'expired')
      expect(parent.postMessage.mock.calls.map((c) => c[0])).toEqual([{ type: 'grasp-embed:session', ok: false }])
    } finally {
      Object.defineProperty(window, 'parent', { value: window, configurable: true })
    }
  })

  it('follows the preview page theme without saving it', async () => {
    localStorage.setItem('grasp-theme', 'dark')
    history.replaceState(null, '', '/embed/runs/run-1/nodes/ap1/chat#theme=light')
    const parent = { postMessage: vi.fn() }
    Object.defineProperty(window, 'parent', { value: parent, configurable: true })
    const root = document.documentElement
    try {
      const w = mountView()
      await flushPromises()
      expect(window.location.hash).toBe('')
      expect(root.classList.contains('light')).toBe(true)

      window.dispatchEvent(
        new MessageEvent('message', { data: { type: 'grasp-embed:theme', theme: 'dark' }, source: parent as never }),
      )
      expect(root.classList.contains('light')).toBe(false)
      window.dispatchEvent(new MessageEvent('message', { data: { type: 'grasp-embed:theme', theme: 'light' } }))
      expect(root.classList.contains('light')).toBe(false)
      expect(localStorage.getItem('grasp-theme')).toBe('dark')

      w.unmount()
      expect(root.classList.contains('light')).toBe(false)
    } finally {
      Object.defineProperty(window, 'parent', { value: window, configurable: true })
    }
  })

  describe('page control', () => {
    function fromParent(parent: unknown, data: unknown) {
      window.dispatchEvent(new MessageEvent('message', { data, source: parent as never }))
    }

    async function liveDrawer() {
      const parent = { postMessage: vi.fn() }
      Object.defineProperty(window, 'parent', { value: parent, configurable: true })
      saveEmbedSession('run-1', 'ap1', { token: 'gse_p', expiresAt: '2099-01-01T00:00:00Z' })
      const w = mountView()
      await flushPromises()
      await w.getComponent('[data-testid="chat-stub"]').vm.$emit('status', 'active')
      return { w, parent }
    }

    afterEach(() => {
      Object.defineProperty(window, 'parent', { value: window, configurable: true })
      vi.useRealTimers()
    })

    it('offers the toggle once the page announces support and relays commands', async () => {
      const { w, parent } = await liveDrawer()
      expect(w.find('[data-testid="page-control-bar"]').exists()).toBe(false)
      fromParent(parent, { type: 'grasp-embed:control', caps: ['page-control'], tab: 't1' })
      await flushPromises()
      expect(w.get('[data-testid="page-control-bar"]').text()).toContain('允许 Agent 操作页面')

      await w.get('[data-testid="page-control-toggle"]').trigger('click')
      expect(mocks.sendFrame).toHaveBeenLastCalledWith({ type: 'page_control', on: true, visible: true })
      expect(parent.postMessage).toHaveBeenLastCalledWith({ type: 'grasp-embed:control', on: true }, '*')

      const chat = w.getComponent('[data-testid="chat-stub"]')
      await chat.vm.$emit('page-frame', { type: 'page_cmd', id: 'c1', action: 'state', args: {} })
      const cmd = parent.postMessage.mock.calls.at(-1)![0] as { type: string; nonce: string }
      expect(cmd.type).toBe('grasp-embed:cmd')

      // Results only count from the parent page.
      window.dispatchEvent(new MessageEvent('message', { data: { type: 'grasp-embed:cmd-result', nonce: cmd.nonce, ok: true } }))
      expect(mocks.sendFrame).not.toHaveBeenCalledWith(expect.objectContaining({ type: 'page_result' }))
      fromParent(parent, { type: 'grasp-embed:cmd-result', nonce: cmd.nonce, ok: true, state: { stateId: 'p:1' } })
      expect(mocks.sendFrame).toHaveBeenLastCalledWith(expect.objectContaining({ type: 'page_result', id: 'c1', ok: true }))

      await chat.vm.$emit('page-frame', { type: 'page_control_state', state: 'paused' })
      expect(w.get('[data-testid="page-control-status"]').text()).toContain('已暂停')
      await chat.vm.$emit('events-ready')
      expect(mocks.sendFrame).toHaveBeenLastCalledWith({ type: 'page_control', on: true, visible: true })
    })

    it('says the page script is too old when it never answers', async () => {
      vi.useFakeTimers()
      const { w } = await liveDrawer()
      vi.advanceTimersByTime(3000)
      await flushPromises()
      expect(w.get('[data-testid="page-control-unsupported"]').text()).toContain('版本过旧')
    })
  })
})
