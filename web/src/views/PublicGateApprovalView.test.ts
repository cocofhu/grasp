// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import shell from '@/locales/zh-CN/shell.json'
import commonEn from '@/locales/en/common.json'
import pagesEn from '@/locales/en/pages.json'
import shellEn from '@/locales/en/shell.json'
import { setTheme } from '@/lib/shared/theme'
import { i18n as appI18n } from '@/lib/shared/i18n'
import { locale as appLocale } from '@/lib/shared/locale'

const mocks = vi.hoisted(() => ({
  preview: vi.fn(),
  upstream: vi.fn(),
  artifacts: vi.fn(),
  decide: vi.fn(),
  reply: vi.fn(),
  cancel: vi.fn(),
  queueRemove: vi.fn(),
  queueReorder: vi.fn(),
}))

class FakeWebSocket {
  static instances: FakeWebSocket[] = []
  url: string
  onopen: ((ev?: unknown) => void) | null = null
  onmessage: ((ev: { data: string }) => void) | null = null
  onclose: (() => void) | null = null
  sent: string[] = []
  constructor(url: string) {
    this.url = url
    FakeWebSocket.instances.push(this)
    queueMicrotask(() => this.onopen?.())
  }
  send(data: string) {
    this.sent.push(data)
  }
  close() {
    this.onclose?.()
  }
  emit(obj: unknown) {
    this.onmessage?.({ data: JSON.stringify(obj) })
  }
}

vi.mock('@/lib/inbox/gateShareLink', async () => {
  const actual = await vi.importActual<typeof import('@/lib/inbox/gateShareLink')>('@/lib/inbox/gateShareLink')
  return {
    ...actual,
    publicGateApi: {
      preview: mocks.preview,
      upstream: mocks.upstream,
      artifacts: mocks.artifacts,
      decide: mocks.decide,
      reply: mocks.reply,
      cancel: mocks.cancel,
      queueRemove: mocks.queueRemove,
      queueReorder: mocks.queueReorder,
      eventsWsUrl: () => 'ws://test/public/gate-approvals/events',
    },
  }
})

vi.mock('@/lib/shared/locale', async () => {
  const actual = await vi.importActual<typeof import('@/lib/shared/locale')>('@/lib/shared/locale')
  return {
    ...actual,
    applyPublicLocale: vi.fn().mockResolvedValue(undefined),
  }
})

import PublicGateApprovalView from './PublicGateApprovalView.vue'
import ClarifyChat from '@/components/run/ClarifyChat.vue'
import type { VueWrapper } from '@vue/test-utils'

const mounted: VueWrapper[] = []

function mountView(locale: 'zh-CN' | 'en' = 'zh-CN', props: Record<string, unknown> = {}) {
  const i18n = createI18n({
    legacy: false,
    locale,
    messages: {
      'zh-CN': { ...common, ...pages, ...shell },
      en: { ...commonEn, ...pagesEn, ...shellEn },
    },
  })
  const wrapper = mount(PublicGateApprovalView, { props, global: { plugins: [i18n] } })
  mounted.push(wrapper)
  return wrapper
}

function mountSharedLocaleView(locale: 'zh-CN' | 'en' = 'zh-CN') {
  appI18n.global.setLocaleMessage('zh-CN', { ...common, ...pages, ...shell })
  appI18n.global.setLocaleMessage('en', { ...commonEn, ...pagesEn, ...shellEn })
  appI18n.global.locale.value = locale
  appLocale.value = locale
  const wrapper = mount(PublicGateApprovalView, { global: { plugins: [appI18n] } })
  mounted.push(wrapper)
  return wrapper
}

beforeEach(() => {
  mocks.preview.mockReset()
  mocks.upstream.mockReset()
  mocks.artifacts.mockReset()
  mocks.artifacts.mockResolvedValue({ status: 'active', artifacts: [], nodes: [] })
  mocks.decide.mockReset()
  mocks.reply.mockReset()
  mocks.cancel.mockReset()
  FakeWebSocket.instances = []
  vi.stubGlobal('WebSocket', FakeWebSocket)
  window.location.hash = ''
  localStorage.clear()
  setTheme('dark')
})

afterEach(() => {
  while (mounted.length) mounted.pop()?.unmount()
  document.documentElement.classList.remove('light')
  vi.unstubAllGlobals()
})

describe('PublicGateApprovalView workbench', () => {
  it('switches language from the top-right control and persists it', async () => {
    const w = mountSharedLocaleView('zh-CN')
    await flushPromises()
    expect(w.get('[data-testid="public-gate-invalid"]').text()).toContain('链接无效')
    await w.get('[data-testid="lang-select-trigger"]').trigger('click')
    const english = w.findAll('[role="option"]').find((option) => option.text().includes('English'))
    expect(english).toBeTruthy()
    await english!.trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="public-gate-invalid"]').text()).toContain('invalid')
    expect(localStorage.getItem('grasp-locale')).toBe('en')
  })

  it('renders dark three-pane workbench for human_gate without purple chrome', async () => {
    window.location.hash = `#t=${'aa'.repeat(32)}`
    mocks.preview.mockResolvedValue({
      status: 'active',
      kind: 'human_gate',
      title: '审阅视觉稿',
      remainingSec: 3600,
      nonce: 'n1',
      reactSessionAlive: true,
      productKind: 'visual',
      productName: 'page.html',
      actions: { approve: 'approve', reject: 'revise', confirm: 'approve', reply: 'reply', cancel: 'cancel' },
      visualHtml: '<p>ok</p>',
      turns: [{ role: 'agent', text: '请审阅 page.html', at: '2026-08-01T00:00:00Z' }],
      upstream: { name: 'clarified_requirement.json', title: '澄清需求', summary: '可对照审阅当前主产物' },
    })
    const w = mountView()
    await flushPromises()
    expect(document.documentElement.classList.contains('light')).toBe(false)
    expect(w.find('[data-testid="public-gate-chrome"]').classes().join(' ')).not.toMatch(/bg-accent/)
    expect(w.get('[data-testid="public-gate-badge"]').text()).toBe('外部一次决策')
    expect(w.find('[data-testid="review-shell"]').exists()).toBe(true)
    expect(w.get('[data-testid="public-gate-product-label"]').text()).toContain('视觉网页产物')
    expect(w.get('[data-testid="public-gate-product-name"]').text()).toBe('page.html')
    expect(w.get('[data-testid="public-gate-upstream"]').text()).toContain('上游上下文')
    expect(w.get('[data-testid="public-gate-upstream-enlarge"]').text()).toContain('放大上游上下文')
    expect(w.get('[data-testid="clarify-confirm-flow"]').text()).toBe('确认并流转')
    expect(w.get('[data-testid="public-gate-reject"]').text()).toBe('驳回')
    expect(w.get('[data-testid="public-gate-sidebar"]').text()).toContain('Agent交互')
    expect(w.find('[data-testid="public-gate-confirm"]').exists()).toBe(false)
    expect(w.find('[data-testid="clarify-input"]').exists()).toBe(true)
    expect(w.get('[data-testid="public-gate-root"]').text()).not.toMatch(/请确认本次交付|这是交付预览|不是审批工作台|Approving|打开运行详情|run-/)
    expect(w.find('[data-testid="html-preview-inspect-toggle"]').exists()).toBe(true)
  })

  it('review hot session has ReAct + composer confirm and no reject', async () => {
    window.location.hash = `#t=${'ee'.repeat(32)}`
    mocks.preview.mockResolvedValue({
      status: 'active',
      kind: 'review',
      remainingSec: 3600,
      nonce: 'n3',
      reactSessionAlive: true,
      productKind: 'structured',
      productName: 'research.json',
      actions: { confirm: 'confirm', reply: 'reply', cancel: 'cancel' },
      structured: { name: 'research.json', title: '调研摘要', doc: { title: '调研摘要' } },
      turns: [
        { role: 'agent', text: '请复审', at: '2026-08-01T00:00:00Z' },
        { role: 'human', text: '改摘要', at: '2026-08-01T00:01:00Z' },
      ],
      upstream: { title: '澄清', summary: '已有澄清需求文档' },
    })
    const w = mountView()
    await flushPromises()
    expect(w.get('[data-testid="public-gate-badge"]').text()).toBe('外部复审')
    expect(w.find('[data-testid="public-gate-react-stage"]').exists()).toBe(true)
    expect(w.find('[data-testid="react-artifact-tab-grid"]').exists()).toBe(true)
    expect(w.get('[data-testid="public-gate-sidebar"]').text()).toContain('共 2 条')
    expect(w.find('[data-testid="clarify-confirm-flow"]').exists()).toBe(true)
    expect(w.find('[data-testid="public-gate-reject"]').exists()).toBe(false)
    expect(w.find('[data-testid="public-gate-name"]').exists()).toBe(false)
    expect(w.find('[data-testid="public-gate-confirm"]').exists()).toBe(false)
    expect(w.get('[data-testid="public-gate-upstream"]').text()).toContain('放大上游上下文')
    expect(w.find('[data-testid="public-gate-footer"]').exists()).toBe(false)

    mocks.decide.mockResolvedValue({ status: 'confirmed', action: 'confirm' })
    await w.get('[data-testid="clarify-confirm-flow"]').trigger('click')
    await flushPromises()
    expect(mocks.decide).toHaveBeenCalledWith(
      expect.objectContaining({ action: 'confirm' }),
      expect.any(AbortSignal),
    )
    expect(w.get('[data-testid="public-gate-done"]').text()).toContain('已确认')
  })

  it('cold review keeps three panes, hides send/inspect, footer still confirms', async () => {
    window.location.hash = `#t=${'cc'.repeat(32)}`
    mocks.preview.mockResolvedValue({
      status: 'active',
      kind: 'review',
      nonce: 'n-cold',
      reactSessionAlive: false,
      productKind: 'visual',
      productName: 'page.html',
      permissionPreset: 'full',
      actions: { confirm: 'confirm' },
      visualHtml: '<p>ok</p>',
      turns: [{ role: 'agent', text: '历史回合', at: '2026-08-01T00:00:00Z' }],
    })
    const w = mountView()
    await flushPromises()
    expect(w.find('[data-testid="review-shell"]').exists()).toBe(true)
    expect(w.get('[data-testid="public-gate-session-ended"]').text()).toContain('会话已结束')
    expect(w.get('[data-testid="public-gate-cold-hint"]').text()).toContain('仅可确认并流转')
    expect(w.find('[data-testid="clarify-input"]').exists()).toBe(false)
    expect(w.find('[data-testid="html-preview-inspect-toggle"]').exists()).toBe(false)
    expect(w.find('[data-testid="clarify-confirm-flow"]').exists()).toBe(true)
    expect(w.get('[data-testid="public-gate-sidebar"]').text()).toContain('历史回合')
  })

  it('react_only hot: reply only, no decide buttons; cold shows deadend', async () => {
    window.location.hash = `#t=${'ro'.repeat(32)}`
    mocks.preview.mockResolvedValue({
      status: 'active',
      kind: 'human_gate',
      nonce: 'n-ro',
      reactSessionAlive: true,
      permissionPreset: 'react_only',
      actions: { reply: 'reply', cancel: 'cancel' },
      turns: [{ role: 'agent', text: '请协作修改', at: '2026-08-01T00:00:00Z' }],
    })
    const w = mountView()
    await flushPromises()
    expect(w.get('[data-testid="public-gate-preset-chip"]').text()).toContain('仅 ReAct')
    expect(w.find('[data-testid="clarify-confirm-flow"]').exists()).toBe(false)
    expect(w.find('[data-testid="public-gate-reject"]').exists()).toBe(false)
    expect(w.find('[data-testid="public-gate-name"]').exists()).toBe(false)

    mocks.preview.mockResolvedValue({
      status: 'active',
      kind: 'human_gate',
      nonce: 'n-ro-cold',
      reactSessionAlive: false,
      permissionPreset: 'react_only',
      actions: {},
      turns: [{ role: 'agent', text: '历史', at: '2026-08-01T00:00:00Z' }],
    })
    await (w.vm as any).loadPreview?.()
    await flushPromises()
    expect(w.get('[data-testid="public-gate-react-only-deadend"]').text()).toContain('无法继续操作')
    expect(w.get('[data-testid="public-gate-react-only-deadend"]').text()).toContain('禁止确认')
    expect(w.find('[data-testid="clarify-confirm-flow"]').exists()).toBe(false)
    expect(w.find('[data-testid="public-gate-cold-hint"]').exists()).toBe(false)
  })

  it('human_gate requires name and comment before confirm or reject', async () => {
    window.location.hash = `#t=${'ff'.repeat(32)}`
    mocks.preview.mockResolvedValue({
      status: 'active',
      kind: 'human_gate',
      nonce: 'n4',
      reactSessionAlive: false,
      actions: { approve: 'approve', reject: 'revise', confirm: 'approve' },
    })
    const w = mountView()
    await flushPromises()
    await w.get('[data-testid="clarify-confirm-flow"]').trigger('click')
    await flushPromises()
    expect(mocks.decide).not.toHaveBeenCalled()
    expect(w.get('[data-testid="clarify-confirm-error"]').text()).toContain('姓名与意见')
    expect(w.find('[data-testid="public-gate-error"]').exists()).toBe(false)

    await w.get('[data-testid="public-gate-name"]').setValue('Jordan')
    await w.get('[data-testid="public-gate-comment"]').setValue('可以流转')
    mocks.decide.mockResolvedValue({ status: 'approved' })
    await w.get('[data-testid="clarify-confirm-flow"]').trigger('click')
    await flushPromises()
    expect(mocks.decide).toHaveBeenCalledWith(
      expect.objectContaining({
        action: 'approve',
        name: 'Jordan',
        comment: '可以流转',
      }),
      expect.any(AbortSignal),
    )
    expect(w.get('[data-testid="public-gate-done"]').text()).toContain('已确认')
  })

  it('reply uses token adapter and does not call decide', async () => {
    window.location.hash = `#t=${'aa'.repeat(32)}`
    mocks.preview.mockResolvedValue({
      status: 'active',
      kind: 'review',
      nonce: 'n-reply',
      reactSessionAlive: true,
      actions: { confirm: 'confirm', reply: 'reply', cancel: 'cancel' },
      turns: [],
    })
    mocks.reply.mockResolvedValue({ status: 'accepted' })
    mocks.preview.mockResolvedValueOnce({
      status: 'active',
      kind: 'review',
      nonce: 'n-reply',
      reactSessionAlive: true,
      actions: { confirm: 'confirm', reply: 'reply', cancel: 'cancel' },
      turns: [],
    }).mockResolvedValue({
      status: 'active',
      kind: 'review',
      nonce: 'n-reply',
      reactSessionAlive: true,
      actions: { confirm: 'confirm', reply: 'reply', cancel: 'cancel' },
      turns: [
        { role: 'human', text: '改标题', at: '2026-08-01T00:02:00Z' },
      ],
    })
    const w = mountView()
    await flushPromises()
    await w.get('[data-testid="clarify-input"]').setValue('改标题')
    await w.get('[data-testid="clarify-send-icon"]').trigger('click')
    await flushPromises()
    expect(mocks.reply).toHaveBeenCalledWith(expect.objectContaining({ token: 'aa'.repeat(32), text: '改标题' }))
    expect(mocks.decide).not.toHaveBeenCalled()
    expect(w.find('[data-testid="public-gate-done"]').exists()).toBe(false)
    expect(w.find('[data-testid="clarify-confirm-flow"]').exists()).toBe(true)
  })

  it('busy confirm stays on workbench and keeps the link', async () => {
    window.location.hash = `#t=${'aa'.repeat(32)}`
    mocks.preview.mockResolvedValue({
      status: 'active',
      kind: 'review',
      nonce: 'n-busy',
      reactSessionAlive: true,
      sessionBusy: false,
      actions: { confirm: 'confirm', reply: 'reply' },
    })
    const w = mountView()
    await flushPromises()
    mocks.decide.mockResolvedValueOnce({
      status: 'busy',
      error: 'review_busy',
      message: '复审进行中，请稍后再试',
    })
    await w.get('[data-testid="clarify-confirm-flow"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="clarify-confirm-error"]').text()).toContain('复审进行中')
    expect(w.find('[data-testid="clarify-confirm-flow"]').exists()).toBe(true)
    expect(w.find('[data-testid="public-gate-done"]').exists()).toBe(false)
  })

  it('silent preview without new turns does not drop optimistic ReAct queue', async () => {
    window.location.hash = `#t=${'aa'.repeat(32)}`
    const basePreview = {
      status: 'active',
      kind: 'review',
      nonce: 'n-race',
      reactSessionAlive: true,
      sessionBusy: false,
      waiting: 0,
      actions: { confirm: 'confirm', reply: 'reply', cancel: 'cancel' },
      turns: [{ role: 'agent', text: '请复审', at: '2026-08-01T00:00:00Z' }],
    }
    mocks.preview.mockResolvedValue({ ...basePreview })
    let resolveReply: ((value: unknown) => void) | undefined
    mocks.reply.mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveReply = resolve
        }),
    )
    const w = mountView()
    await flushPromises()
    await w.get('[data-testid="clarify-input"]').setValue('改标题')
    await w.get('[data-testid="clarify-send-icon"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="clarify-review-queue"]').exists()).toBe(true)
    expect(w.get('[data-testid="clarify-review-queue"]').text()).toContain('改标题')

    mocks.preview.mockResolvedValue({
      ...basePreview,
      sessionBusy: false,
      waiting: 0,
      turns: [{ role: 'agent', text: '请复审', at: '2026-08-01T00:00:00Z' }],
    })
    await (w.vm as unknown as { loadPreview: (opts?: { silent?: boolean }) => Promise<void> }).loadPreview({
      silent: true,
    })
    await flushPromises()
    expect(w.find('[data-testid="clarify-review-queue"]').exists()).toBe(true)
    expect(w.get('[data-testid="clarify-review-queue"]').text()).toContain('改标题')
    expect(w.find('[data-testid="public-gate-done"]').exists()).toBe(false)
    expect(mocks.decide).not.toHaveBeenCalled()

    resolveReply?.({ status: 'accepted' })
    await flushPromises()
    expect(w.find('[data-testid="clarify-review-queue"]').exists()).toBe(true)
    // plan g1/g2: busy keeps confirm mounted and disabled (not unmounted).
    const confirmBusy = w.find('[data-testid="clarify-confirm-flow"]')
    expect(confirmBusy.exists()).toBe(true)
    expect((confirmBusy.element as HTMLButtonElement).disabled).toBe(true)
    expect(w.find('[data-testid="clarify-review-cancel"]').exists()).toBe(true)
  })

  it('sessionBusy preview restores thinking placeholder and cancel', async () => {
    window.location.hash = `#t=${'dd'.repeat(32)}`
    mocks.preview.mockResolvedValue({
      status: 'active',
      kind: 'review',
      nonce: 'n-busy-resume',
      reactSessionAlive: true,
      sessionBusy: true,
      waiting: 0,
      activeItem: { text: '改标题' },
      actions: { confirm: 'confirm', reply: 'reply', cancel: 'cancel' },
      turns: [{ role: 'agent', text: '请复审', at: '2026-08-01T00:00:00Z' }],
    })
    const w = mountView()
    await flushPromises()
    await flushPromises()
    expect(w.find('[data-testid="clarify-busy-placeholder"]').exists()).toBe(true)
    expect(w.find('[data-testid="clarify-review-cancel"]').exists()).toBe(true)
    // plan g1/g2: sessionBusy resume keeps confirm visible + disabled.
    const confirmResume = w.find('[data-testid="clarify-confirm-flow"]')
    expect(confirmResume.exists()).toBe(true)
    expect((confirmResume.element as HTMLButtonElement).disabled).toBe(true)
    expect(w.find('[data-testid="public-gate-done"]').exists()).toBe(false)
  })

  it('applies preview liveEvents as streaming agent text (poll fallback)', async () => {
    window.location.hash = `#t=${'dd'.repeat(32)}`
    mocks.preview.mockResolvedValue({
      status: 'active',
      kind: 'review',
      nonce: 'n-live',
      reactSessionAlive: true,
      sessionBusy: true,
      waiting: 0,
      activeItem: { text: '改成绿的' },
      actions: { confirm: 'confirm', reply: 'reply', cancel: 'cancel' },
      turns: [{ role: 'agent', text: '请复审', at: '2026-08-01T00:00:00Z' }],
      liveEvents: [{ kind: 'message', text: '标题已改为绿色（#16a34a）' }],
    })
    const w = mountView()
    await flushPromises()
    await flushPromises()
    expect((w.text().match(/改成绿的/g) || []).length).toBe(1)
    expect(w.text()).toContain('标题已改为绿色')
    expect(w.find('[data-testid="clarify-stream-caret"]').exists()).toBe(true)
    expect(w.find('[data-testid="clarify-busy-status"]').text()).toContain('输出中')
  })

  it('does not duplicate human when turns already completed the busy activeItem', async () => {
    window.location.hash = `#t=${'dd'.repeat(32)}`
    mocks.preview.mockResolvedValue({
      status: 'active',
      kind: 'review',
      nonce: 'n-dup',
      reactSessionAlive: true,
      sessionBusy: true,
      waiting: 0,
      activeItem: { text: '改成绿的' },
      actions: { confirm: 'confirm', reply: 'reply', cancel: 'cancel' },
      turns: [
        { role: 'human', text: '改成绿的', at: '2026-08-01T00:01:00Z' },
        { role: 'agent', text: '标题已改为绿色（#16a34a）', at: '2026-08-01T00:02:00Z' },
      ],
    })
    const w = mountView()
    await flushPromises()
    await flushPromises()
    expect((w.text().match(/改成绿的/g) || []).length).toBe(1)
    expect(w.find('[data-testid="clarify-busy-placeholder"]').exists()).toBe(false)
    expect(w.text()).toContain('标题已改为绿色')
  })

  it('public events WS auth then ACP frame streams into the chat', async () => {
    window.location.hash = `#t=${'dd'.repeat(32)}`
    mocks.preview.mockResolvedValue({
      status: 'active',
      kind: 'review',
      nonce: 'n-ws',
      reactSessionAlive: true,
      sessionBusy: true,
      waiting: 0,
      activeItem: { text: '改成绿的' },
      actions: { confirm: 'confirm', reply: 'reply', cancel: 'cancel' },
      turns: [{ role: 'agent', text: '请复审', at: '2026-08-01T00:00:00Z' }],
    })
    const w = mountView()
    await flushPromises()
    await flushPromises()
    const sock = FakeWebSocket.instances[FakeWebSocket.instances.length - 1]
    expect(sock).toBeTruthy()
    expect(sock.sent.some((s) => s.includes('dd'.repeat(32)))).toBe(true)
    sock.emit({
      type: 'acp',
      nodeId: 'public-gate',
      events: [{ kind: 'message', text: '流式产出正文' }],
    })
    await flushPromises()
    expect(w.text()).toContain('流式产出正文')
    expect(w.find('[data-testid="clarify-stream-caret"]').exists()).toBe(true)
  })

  it('does not paint ACP buffered before a turn into that turn', async () => {
    window.location.hash = `#t=${'de'.repeat(32)}`
    mocks.preview.mockResolvedValue({
      status: 'active',
      kind: 'review',
      nonce: 'n-stale',
      reactSessionAlive: true,
      actions: { confirm: 'confirm', reply: 'reply', cancel: 'cancel' },
      turns: [{ role: 'agent', text: '请复审', at: '2026-08-01T00:00:00Z' }],
    })
    const w = mountView()
    await flushPromises()
    await flushPromises()
    const sock = FakeWebSocket.instances[FakeWebSocket.instances.length - 1]
    sock.emit({ type: 'acp', nodeId: 'public-gate', events: [{ kind: 'message', text: '上一轮的回复' }] })
    await flushPromises()
    sock.emit({ type: 'review', event: 'turn_begin', nodeId: 'public-gate', item: { id: 'i1', text: '新问题' } })
    await flushPromises()
    expect(w.text()).toContain('新问题')
    expect(w.text()).not.toContain('上一轮的回复')
    sock.emit({ type: 'acp', nodeId: 'public-gate', events: [{ kind: 'message', text: '这一轮的回复' }] })
    await flushPromises()
    expect(w.text()).toContain('这一轮的回复')
  })

  it('public poll/WS queueItems keep annotations badge and refill on edit (plan g1/f4)', async () => {
    window.location.hash = `#t=${'ee'.repeat(32)}`
    mocks.preview.mockResolvedValue({
      status: 'active',
      kind: 'review',
      nonce: 'n-q-ann',
      reactSessionAlive: true,
      sessionBusy: false,
      waiting: 1,
      queueItems: [
        {
          id: 'pub-q-1',
          text: '公共排队带标注',
          annotations: [{ label: '公共点', selector: '#pub-hero', jsonPath: 'goals[0]' }],
        },
      ],
      actions: { confirm: 'confirm', reply: 'reply', cancel: 'cancel' },
      turns: [{ role: 'agent', text: '请复审', at: '2026-08-01T00:00:00Z' }],
    })
    mocks.cancel.mockResolvedValue({})
    const w = mountView()
    await flushPromises()
    await flushPromises()
    expect(w.text()).toMatch(/批注/)
    expect(w.text()).toContain('公共排队带标注')

    // WS reconcile with annotations-only frame (no local hit) must keep badge.
    const sock = FakeWebSocket.instances[FakeWebSocket.instances.length - 1]
    expect(sock).toBeTruthy()
    sock.emit({
      type: 'review',
      event: 'queue_state',
      nodeId: 'public-gate',
      waiting: 1,
      busy: false,
      items: [
        {
          id: 'pub-q-1',
          text: '公共排队带标注',
          annotations: [{ label: '公共点', selector: '#pub-hero', jsonPath: 'goals[0]' }],
        },
      ],
    })
    await flushPromises()
    expect(w.text()).toMatch(/批注/)

    await w.find('[data-testid="clarify-queue-edit"]').trigger('click')
    await flushPromises()
    expect((w.get('[data-testid="clarify-input"]').element as HTMLTextAreaElement).value).toBe(
      '公共排队带标注',
    )
    const anns = (w.vm as unknown as { annotations: Array<{ label?: string; selector?: string }> })
      .annotations
    expect(anns).toEqual([{ label: '公共点', selector: '#pub-hero', jsonPath: 'goals[0]' }])
  })

  it('unavailable states keep dark chrome without confirm/send', async () => {
    window.location.hash = `#t=${'bb'.repeat(32)}`
    mocks.preview.mockResolvedValue({ status: 'expired', kind: 'human_gate' })
    const w = mountView()
    await flushPromises()
    expect(w.get('[data-testid="public-gate-badge"]').text()).toBe('外部一次决策')
    expect(w.get('[data-testid="public-gate-invalid"]').text()).toContain('已过期')
    expect(w.find('[data-testid="clarify-confirm-flow"]').exists()).toBe(false)
    expect(w.find('[data-testid="clarify-input"]').exists()).toBe(false)
    expect(w.get('[data-testid="public-gate-root"]').text()).not.toMatch(/请确认本次交付/)
  })

  it('clears the previous preview and discards a stale hash response', async () => {
    const tokenA = 'aa'.repeat(32)
    const tokenB = 'bb'.repeat(32)
    let resolveA!: (value: unknown) => void
    mocks.preview.mockImplementation((tok: string) => {
      if (tok === tokenA) {
        return new Promise((resolve) => {
          resolveA = resolve
        })
      }
      return Promise.resolve({
        status: 'active',
        kind: 'human_gate',
        nonce: 'nB',
        productKind: 'structured',
        productName: 'TokenB-only',
        structured: { name: 'TokenB-only', doc: { title: 'Token B' } },
        actions: { approve: 'approve' },
      })
    })

    history.replaceState(null, '', `#t=${tokenA}`)
    const w = mountView()
    await flushPromises()
    expect(w.find('[data-testid="public-gate-loading"]').exists()).toBe(true)
    expect(w.get('[data-testid="public-gate-loading"]').attributes('role')).toBe('status')
    expect(w.get('[data-testid="public-gate-root"]').text()).not.toMatch(/run-|projectId|TokenA/)

    history.replaceState(null, '', `#t=${tokenB}`)
    await (w.vm as unknown as { loadPreview: () => Promise<void> }).loadPreview()
    await flushPromises()
    expect(w.get('[data-testid="public-gate-product-name"]').text()).toBe('TokenB-only')

    resolveA({
      status: 'active',
      kind: 'human_gate',
      title: 'TokenA-SECRET',
      nonce: 'nA',
      visualHtml: '<p>run-old-internal</p>',
      actions: { approve: 'approve' },
    })
    await flushPromises()
    expect(w.text()).not.toContain('TokenA-SECRET')
    expect(w.text()).not.toContain('run-old-internal')
    expect(w.get('[data-testid="public-gate-product-name"]').text()).toBe('TokenB-only')
  })

  it('shows submitting state and ignores duplicate final actions', async () => {
    window.location.hash = `#t=${'aa'.repeat(32)}`
    mocks.preview.mockResolvedValue({
      status: 'active',
      kind: 'human_gate',
      nonce: 'n1',
      actions: { approve: 'approve', reject: 'revise' },
    })
    let resolveDecide!: (value: unknown) => void
    mocks.decide.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveDecide = resolve
        }),
    )
    const w = mountView()
    await flushPromises()
    await w.get('[data-testid="public-gate-name"]').setValue('Jordan')
    await w.get('[data-testid="public-gate-comment"]').setValue('可以流转')
    await w.get('[data-testid="clarify-confirm-flow"]').trigger('click')
    await w.get('[data-testid="clarify-confirm-flow"]').trigger('click')
    await w.get('[data-testid="public-gate-reject"]').trigger('click')
    await flushPromises()

    expect(mocks.decide).toHaveBeenCalledTimes(1)
    expect(w.get('[data-testid="clarify-confirm-flow"]').text()).toContain('校验中')
    expect(w.get('[data-testid="public-gate-reject"]').text()).toContain('提交中…')
    expect((w.get('[data-testid="clarify-confirm-flow"]').element as HTMLButtonElement).disabled).toBe(true)
    expect((w.get('[data-testid="public-gate-reject"]').element as HTMLButtonElement).disabled).toBe(true)

    resolveDecide({ status: 'approved' })
    await flushPromises()
    expect(w.get('[data-testid="public-gate-done"]').text()).toContain('已确认')
  })

  it('sandboxes preview and decision errors without rendering internal messages', async () => {
    window.location.hash = `#t=${'aa'.repeat(32)}`
    mocks.preview.mockRejectedValue(new Error('internal stack /api/runs/run-123 projectId=p1'))
    const w = mountView()
    await flushPromises()
    expect(w.get('[data-testid="public-gate-network-error"]').text()).toContain('网络错误')
    expect(w.text()).not.toContain('internal stack')
    expect(w.text()).not.toContain('run-123')
    expect(w.text()).not.toContain('projectId')
    expect(w.find('[data-testid="public-gate-invalid"]').exists()).toBe(false)

    mocks.preview.mockResolvedValue({
      status: 'active',
      kind: 'human_gate',
      nonce: 'n1',
      actions: { approve: 'approve' },
    })
    await w.get('[data-testid="public-gate-network-retry"]').trigger('click')
    await flushPromises()
    await w.get('[data-testid="public-gate-name"]').setValue('Jordan')
    await w.get('[data-testid="public-gate-comment"]').setValue('可以流转')
    mocks.decide.mockRejectedValueOnce(new Error('postgres connection refused at 10.1.2.3'))
    await w.get('[data-testid="clarify-confirm-flow"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="clarify-confirm-error"]').text()).toBe('安全校验未通过，请再试一次「确认并流转」')
    expect(w.get('[data-testid="clarify-confirm-error"]').text()).not.toMatch(/网络错误/)
    expect(w.text()).not.toContain('postgres')
    expect(w.text()).not.toContain('10.1.2.3')
    expect(w.text()).not.toMatch(/\bcsrf\b|\bnonce\b/i)
    expect(w.find('[data-testid="public-gate-workbench"]').exists()).toBe(true)
    expect((w.get('[data-testid="clarify-confirm-flow"]').element as HTMLButtonElement).disabled).toBe(false)
  })

  it('maps decide failures to Demo-locked footnotes without leaking internals', async () => {
    window.location.hash = `#t=${'aa'.repeat(32)}`
    mocks.preview.mockResolvedValue({
      status: 'active',
      kind: 'human_gate',
      nonce: 'n1',
      actions: { approve: 'approve' },
    })
    const w = mountView()
    await flushPromises()
    await w.get('[data-testid="public-gate-name"]').setValue('Jordan')
    await w.get('[data-testid="public-gate-comment"]').setValue('可以流转')

    const cases: Array<{ err: unknown; copy: string }> = [
      {
        err: Object.assign(new Error('csrf rejected'), { status: 403, body: { error: 'csrf', message: 'csrf rejected' } }),
        copy: '安全校验未通过，请再试一次「确认并流转」',
      },
      {
        err: Object.assign(new Error('rate_limited'), { status: 429, body: { error: 'rate_limited' } }),
        copy: '请求过于频繁，请稍后再试',
      },
      {
        err: Object.assign(new Error('Failed to fetch'), { name: 'TypeError', message: 'Failed to fetch' }),
        copy: '网络故障，请检查网络后重试',
      },
      {
        err: Object.assign(new Error('upstream 502'), { status: 502, body: { error: 'unavailable' } }),
        copy: '网络故障，请检查网络后重试',
      },
    ]
    for (const c of cases) {
      mocks.decide.mockRejectedValueOnce(c.err)
      await w.get('[data-testid="clarify-confirm-flow"]').trigger('click')
      await flushPromises()
      expect(w.get('[data-testid="clarify-confirm-error"]').text()).toBe(c.copy)
      expect(w.text()).not.toMatch(/\bcsrf\b|\bnonce\b/i)
      expect(w.find('[data-testid="public-gate-workbench"]').exists()).toBe(true)
      expect(w.find('[data-testid="public-gate-invalid"]').exists()).toBe(false)
      expect((w.get('[data-testid="clarify-confirm-flow"]').element as HTMLButtonElement).disabled).toBe(false)
    }
  })

  it('does not silent-poll preview while confirming', async () => {
    window.location.hash = `#t=${'aa'.repeat(32)}`
    mocks.preview.mockResolvedValue({
      status: 'active',
      kind: 'human_gate',
      nonce: 'n-poll',
      actions: { approve: 'approve', reject: 'revise' },
    })
    let resolveDecide!: (value: unknown) => void
    mocks.decide.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveDecide = resolve
        }),
    )
    const w = mountView()
    await flushPromises()
    await w.get('[data-testid="public-gate-name"]').setValue('Jordan')
    await w.get('[data-testid="public-gate-comment"]').setValue('可以流转')
    const previewCalls = mocks.preview.mock.calls.length
    await w.get('[data-testid="clarify-confirm-flow"]').trigger('click')
    await flushPromises()
    await new Promise((r) => setTimeout(r, 2500))
    await flushPromises()
    expect(mocks.preview.mock.calls.length).toBe(previewCalls)
    expect(w.get('[data-testid="clarify-confirm-flow"]').text()).toContain('校验中')
    resolveDecide({ status: 'approved' })
    await flushPromises()
    expect(w.get('[data-testid="public-gate-done"]').text()).toContain('已确认')
  })

  it('silently retries once on nonce failure and hides the error on success', async () => {
    window.location.hash = `#t=${'aa'.repeat(32)}`
    mocks.preview.mockResolvedValue({
      status: 'active',
      kind: 'review',
      nonce: 'n-old',
      reactSessionAlive: true,
      actions: { confirm: 'confirm' },
    })
    const w = mountView()
    await flushPromises()
    mocks.decide
      .mockRejectedValueOnce(
        Object.assign(new Error('nonce'), { status: 403, body: { error: 'nonce', message: 'nonce expired' } }),
      )
      .mockResolvedValueOnce({ status: 'confirmed' })
    mocks.preview.mockResolvedValue({
      status: 'active',
      kind: 'review',
      nonce: 'n-new',
      reactSessionAlive: true,
      actions: { confirm: 'confirm' },
    })
    await w.get('[data-testid="clarify-confirm-flow"]').trigger('click')
    await flushPromises()
    expect(mocks.decide).toHaveBeenCalledTimes(2)
    expect(mocks.decide.mock.calls[0][0]).toEqual(expect.objectContaining({ nonce: 'n-old' }))
    expect(mocks.decide.mock.calls[1][0]).toEqual(expect.objectContaining({ nonce: 'n-new' }))
    expect(w.find('[data-testid="clarify-confirm-error"]').exists()).toBe(false)
    expect(w.get('[data-testid="public-gate-done"]').text()).toContain('已确认')
    expect(w.text()).not.toMatch(/\bnonce\b/i)
  })

  it('retries nonce only once then shows security footnote', async () => {
    window.location.hash = `#t=${'aa'.repeat(32)}`
    mocks.preview.mockResolvedValue({
      status: 'active',
      kind: 'review',
      nonce: 'n-a',
      reactSessionAlive: true,
      actions: { confirm: 'confirm' },
    })
    const w = mountView()
    await flushPromises()
    mocks.decide.mockRejectedValue(
      Object.assign(new Error('nonce'), { status: 403, body: { error: 'nonce' } }),
    )
    mocks.preview.mockResolvedValue({
      status: 'active',
      kind: 'review',
      nonce: 'n-b',
      reactSessionAlive: true,
      actions: { confirm: 'confirm' },
    })
    await w.get('[data-testid="clarify-confirm-flow"]').trigger('click')
    await flushPromises()
    expect(mocks.decide).toHaveBeenCalledTimes(2)
    expect(w.get('[data-testid="clarify-confirm-error"]').text()).toBe('安全校验未通过，请再试一次「确认并流转」')
    expect(w.text()).not.toMatch(/\bnonce\b/i)
    expect((w.get('[data-testid="clarify-confirm-flow"]').element as HTMLButtonElement).disabled).toBe(false)
  })

  it('does not refresh nonce for csrf, rate limit, or network failures', async () => {
    window.location.hash = `#t=${'aa'.repeat(32)}`
    mocks.preview.mockResolvedValue({
      status: 'active',
      kind: 'review',
      nonce: 'n-fixed',
      reactSessionAlive: true,
      actions: { confirm: 'confirm' },
    })
    const w = mountView()
    await flushPromises()
    const previewCalls = mocks.preview.mock.calls.length
    mocks.decide.mockRejectedValueOnce(
      Object.assign(new Error('csrf'), { status: 403, body: { error: 'csrf' } }),
    )
    await w.get('[data-testid="clarify-confirm-flow"]').trigger('click')
    await flushPromises()
    expect(mocks.decide).toHaveBeenCalledTimes(1)
    expect(mocks.preview.mock.calls.length).toBe(previewCalls)
    expect(w.get('[data-testid="clarify-confirm-error"]').text()).toContain('安全校验未通过')

    mocks.decide.mockRejectedValueOnce(
      Object.assign(new Error('rate'), { status: 429, body: { error: 'rate_limited' } }),
    )
    await w.get('[data-testid="clarify-confirm-flow"]').trigger('click')
    await flushPromises()
    expect(mocks.decide).toHaveBeenCalledTimes(2)
    expect(mocks.preview.mock.calls.length).toBe(previewCalls)
    expect(w.get('[data-testid="clarify-confirm-error"]').text()).toBe('请求过于频繁，请稍后再试')

    mocks.decide.mockRejectedValueOnce(Object.assign(new Error('Failed to fetch'), { name: 'TypeError' }))
    await w.get('[data-testid="clarify-confirm-flow"]').trigger('click')
    await flushPromises()
    expect(mocks.decide).toHaveBeenCalledTimes(3)
    expect(mocks.preview.mock.calls.length).toBe(previewCalls)
    expect(w.get('[data-testid="clarify-confirm-error"]').text()).toBe('网络故障，请检查网络后重试')
  })

  it('keeps workbench with disabled confirm when the link becomes invalid after open', async () => {
    window.location.hash = `#t=${'aa'.repeat(32)}`
    mocks.preview.mockResolvedValue({
      status: 'active',
      kind: 'review',
      nonce: 'n-used',
      reactSessionAlive: true,
      actions: { confirm: 'confirm' },
    })
    const w = mountView()
    await flushPromises()
    expect(w.find('[data-testid="public-gate-workbench"]').exists()).toBe(true)
    mocks.decide.mockResolvedValueOnce({ status: 'used', error: 'conflict' })
    await w.get('[data-testid="clarify-confirm-flow"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="public-gate-workbench"]').exists()).toBe(true)
    expect(w.find('[data-testid="public-gate-invalid"]').exists()).toBe(false)
    expect(w.find('[data-testid="public-gate-done"]').exists()).toBe(false)
    expect(w.get('[data-testid="clarify-confirm-error"]').text()).toBe('链接失效，请重新打开复审链接')
    expect(w.find('[data-testid="clarify-confirm-flow"]').exists()).toBe(true)
    expect((w.get('[data-testid="clarify-confirm-flow"]').element as HTMLButtonElement).disabled).toBe(true)
  })

  it('silent poll merge keeps visualHtml when server omits unchanged body (plan g2.2)', async () => {
    window.location.hash = `#t=${'aa'.repeat(32)}`
    mocks.preview.mockResolvedValue({
      status: 'active',
      kind: 'human_gate',
      nonce: 'n-merge',
      visualHtml: '<p>keep-me</p>',
      visualHtmlHash: 'vh1',
      upstream: { name: 'clarified_requirement.json', title: '澄清', summary: '摘要' },
      upstreamHash: 'up1',
      remainingSec: 100,
      actions: { approve: 'approve', confirm: 'approve' },
      productKind: 'visual',
      productName: 'page.html',
    })
    const w = mountView()
    await flushPromises()
    expect(w.get('[data-testid="public-gate-visual"]').html()).toContain('keep-me')

    mocks.preview.mockResolvedValue({
      status: 'active',
      kind: 'human_gate',
      nonce: 'n-merge-2',
      visualHtmlHash: 'vh1',
      upstreamHash: 'up1',
      remainingSec: 98,
      actions: { approve: 'approve', confirm: 'approve' },
      productKind: 'visual',
      productName: 'page.html',
    })
    await (w.vm as unknown as { loadPreview: (opts?: { silent?: boolean }) => Promise<void> }).loadPreview({
      silent: true,
    })
    await flushPromises()
    expect(w.get('[data-testid="public-gate-visual"]').html()).toContain('keep-me')
    expect(mocks.preview.mock.calls[mocks.preview.mock.calls.length - 1]?.[2]).toEqual(
      expect.objectContaining({
        visualHtmlHash: 'vh1',
        upstreamHash: 'up1',
        silent: true,
        issueNonce: false,
      }),
    )
  })

  it('loads upstream doc on enlarge and retries on failure without blocking approve (plan g1.3)', async () => {
    window.location.hash = `#t=${'aa'.repeat(32)}`
    mocks.preview.mockResolvedValue({
      status: 'active',
      kind: 'human_gate',
      nonce: 'n-up',
      visualHtml: '<p>ok</p>',
      reactSessionAlive: true,
      upstream: { name: 'clarified_requirement.json', title: '澄清', summary: '仅摘要' },
      actions: { approve: 'approve', reject: 'revise', confirm: 'approve' },
      productKind: 'visual',
      productName: 'page.html',
    })
    mocks.upstream.mockRejectedValueOnce(new Error('upstream_timeout'))
    const w = mountView()
    await flushPromises()
    expect(mocks.upstream).not.toHaveBeenCalled()
    await w.get('[data-testid="public-gate-upstream-enlarge"]').trigger('click')
    await flushPromises()
    expect(mocks.upstream).toHaveBeenCalledTimes(1)
    const errEl = document.body.querySelector('[data-testid="public-gate-upstream-error"]')
    expect(errEl?.textContent || '').toContain('upstream_timeout')
    expect(w.find('[data-testid="clarify-confirm-flow"]').exists()).toBe(true)

    mocks.upstream.mockResolvedValueOnce({
      status: 'active',
      upstream: {
        name: 'clarified_requirement.json',
        title: '澄清',
        doc: { title: '澄清全文', goals: ['g1'] },
      },
    })
    const retryBtn = document.body.querySelector('[data-testid="public-gate-upstream-retry"]') as HTMLButtonElement | null
    expect(retryBtn).toBeTruthy()
    retryBtn!.click()
    await flushPromises()
    const docEl = document.body.querySelector('[data-testid="public-gate-upstream-doc"]')
    expect(docEl).toBeTruthy()
    expect(docEl?.textContent || '').toMatch(/g1|澄清/)
  })

  function setVisibility(state: DocumentVisibilityState) {
    Object.defineProperty(document, 'visibilityState', {
      configurable: true,
      get: () => state,
    })
    document.dispatchEvent(new Event('visibilitychange'))
  }

  it('stops polling while the tab is hidden and does not unmount preview (plan g1.1)', async () => {
    window.location.hash = `#t=${'aa'.repeat(32)}`
    mocks.preview.mockResolvedValue({
      status: 'active',
      kind: 'human_gate',
      nonce: 'n-vis',
      visualHtml: '<p>stay-mounted</p>',
      visualHtmlHash: 'vh-stay',
      expiresAt: new Date(Date.now() + 3600_000).toISOString(),
      remainingSec: 3600,
      actions: { approve: 'approve', confirm: 'approve' },
      productKind: 'visual',
      productName: 'page.html',
    })
    const w = mountView()
    await flushPromises()
    expect(w.get('[data-testid="public-gate-visual"]').html()).toContain('stay-mounted')
    const before = mocks.preview.mock.calls.length
    setVisibility('hidden')
    await flushPromises()
    await new Promise((r) => setTimeout(r, 2500))
    await flushPromises()
    expect(mocks.preview.mock.calls.length).toBe(before)
    expect(w.get('[data-testid="public-gate-visual"]').html()).toContain('stay-mounted')
    expect(w.find('[data-testid="public-gate-workbench"]').exists()).toBe(true)
    setVisibility('visible')
  })

  it('resumes with a silent refresh and keeps draft plus preview (plan g1.2 / g5.2)', async () => {
    window.location.hash = `#t=${'aa'.repeat(32)}`
    mocks.preview.mockResolvedValue({
      status: 'active',
      kind: 'human_gate',
      nonce: 'n-resume',
      visualHtml: '<p>keep-iframe</p>',
      visualHtmlHash: 'vh-resume',
      expiresAt: new Date(Date.now() + 3600_000).toISOString(),
      remainingSec: 3600,
      reactSessionAlive: true,
      actions: { approve: 'approve', confirm: 'approve', reply: 'reply' },
      productKind: 'visual',
      productName: 'page.html',
      turns: [{ role: 'agent', text: '请审阅', at: '2026-08-01T00:00:00Z' }],
    })
    const w = mountView()
    await flushPromises()
    await w.get('[data-testid="clarify-input"]').setValue('未发送草稿')
    setVisibility('hidden')
    await flushPromises()
    mocks.preview.mockResolvedValue({
      status: 'active',
      kind: 'human_gate',
      nonce: 'n-resume-2',
      visualHtmlHash: 'vh-resume',
      remainingSec: 3500,
      reactSessionAlive: true,
      actions: { approve: 'approve', confirm: 'approve', reply: 'reply' },
      productKind: 'visual',
      productName: 'page.html',
    })
    const before = mocks.preview.mock.calls.length
    setVisibility('visible')
    await flushPromises()
    expect(mocks.preview.mock.calls.length).toBeGreaterThan(before)
    const lastKnown = mocks.preview.mock.calls[mocks.preview.mock.calls.length - 1]?.[2]
    expect(lastKnown).toEqual(expect.objectContaining({ silent: true, issueNonce: true }))
    expect(w.get('[data-testid="clarify-input"]').element).toHaveProperty('value', '未发送草稿')
    expect(w.get('[data-testid="public-gate-visual"]').html()).toContain('keep-iframe')
    expect(w.find('[data-testid="public-gate-loading"]').exists()).toBe(false)
  })

  it('shows remaining time from expiresAt, not stale remainingSec (plan g2.1)', async () => {
    window.location.hash = `#t=${'aa'.repeat(32)}`
    mocks.preview.mockResolvedValue({
      status: 'active',
      kind: 'human_gate',
      nonce: 'n-clock',
      remainingSec: 30,
      expiresAt: new Date(Date.now() + 90 * 60 * 1000).toISOString(),
      actions: { approve: 'approve', confirm: 'approve' },
    })
    const w = mountView()
    await flushPromises()
    expect(w.get('[data-testid="public-gate-remaining"]').text()).toContain('1 小时')
    expect(w.get('[data-testid="public-gate-remaining"]').text()).not.toContain('1 分钟')
  })

  it('identical silent polls do not drop visual or idle-apply queue (plan g3.1 / g3.2)', async () => {
    window.location.hash = `#t=${'aa'.repeat(32)}`
    const base = {
      status: 'active',
      kind: 'human_gate',
      visualHtml: '<p>stable</p>',
      visualHtmlHash: 'vh-stable',
      structuredHash: 'st-stable',
      turnsHash: 'tn-stable',
      upstreamHash: 'up-stable',
      reactSessionAlive: true,
      sessionBusy: false,
      waiting: 0,
      actions: { approve: 'approve', confirm: 'approve', reply: 'reply' },
      productKind: 'visual',
      productName: 'page.html',
      turns: [{ role: 'agent', text: '请审阅', at: '2026-08-01T00:00:00Z' }],
    }
    mocks.preview.mockResolvedValue({ ...base, nonce: 'n0', remainingSec: 4000 })
    const w = mountView()
    await flushPromises()
    await w.get('[data-testid="clarify-input"]').setValue('idle-draft')
    const chat = w.getComponent(ClarifyChat)
    const applySpy = vi.spyOn(chat.vm as unknown as { applyQueueState: (...args: unknown[]) => void }, 'applyQueueState')
    for (let i = 1; i <= 8; i++) {
      mocks.preview.mockResolvedValue({
        ...base,
        nonce: `n${i}`,
        remainingSec: 4000 - i,
      })
      await (w.vm as unknown as { loadPreview: (opts?: { silent?: boolean }) => Promise<void> }).loadPreview({
        silent: true,
      })
      await flushPromises()
    }
    expect(w.get('[data-testid="public-gate-visual"]').html()).toContain('stable')
    expect(w.get('[data-testid="clarify-input"]').element).toHaveProperty('value', 'idle-draft')
    expect(applySpy).not.toHaveBeenCalled()
    applySpy.mockRestore()
  })

  it('retries nonce once on reject as well as confirm (plan g4.2)', async () => {
    window.location.hash = `#t=${'aa'.repeat(32)}`
    mocks.preview.mockResolvedValue({
      status: 'active',
      kind: 'human_gate',
      nonce: 'n-old-rej',
      actions: { approve: 'approve', reject: 'revise', confirm: 'approve' },
    })
    const w = mountView()
    await flushPromises()
    await w.get('[data-testid="public-gate-name"]').setValue('Jordan')
    await w.get('[data-testid="public-gate-comment"]').setValue('需要驳回')
    mocks.decide
      .mockRejectedValueOnce(
        Object.assign(new Error('nonce'), { status: 403, body: { error: 'nonce', message: 'nonce expired' } }),
      )
      .mockResolvedValueOnce({ status: 'rejected' })
    mocks.preview.mockResolvedValue({
      status: 'active',
      kind: 'human_gate',
      nonce: 'n-new-rej',
      actions: { approve: 'approve', reject: 'revise', confirm: 'approve' },
    })
    await w.get('[data-testid="public-gate-reject"]').trigger('click')
    await flushPromises()
    expect(mocks.decide).toHaveBeenCalledTimes(2)
    expect(mocks.decide.mock.calls[0][0]).toEqual(expect.objectContaining({ nonce: 'n-old-rej', action: 'revise' }))
    expect(mocks.decide.mock.calls[1][0]).toEqual(expect.objectContaining({ nonce: 'n-new-rej', action: 'revise' }))
    expect(w.get('[data-testid="public-gate-done"]').text()).toContain('已驳回')
  })

  it('silent 429 does not clear draft or leave the workbench (plan g5.2)', async () => {
    window.location.hash = `#t=${'aa'.repeat(32)}`
    mocks.preview.mockResolvedValue({
      status: 'active',
      kind: 'human_gate',
      nonce: 'n-429',
      visualHtml: '<p>ok</p>',
      reactSessionAlive: true,
      actions: { approve: 'approve', confirm: 'approve', reply: 'reply' },
      productKind: 'visual',
      productName: 'page.html',
    })
    const w = mountView()
    await flushPromises()
    await w.get('[data-testid="clarify-input"]').setValue('keep-on-429')
    mocks.preview.mockRejectedValueOnce(
      Object.assign(new Error('rate_limited'), { status: 429, body: { error: 'rate_limited' } }),
    )
    await (w.vm as unknown as { loadPreview: (opts?: { silent?: boolean }) => Promise<void> }).loadPreview({
      silent: true,
    })
    await flushPromises()
    expect(w.get('[data-testid="clarify-input"]').element).toHaveProperty('value', 'keep-on-429')
    expect(w.find('[data-testid="public-gate-workbench"]').exists()).toBe(true)
    expect(w.find('[data-testid="public-gate-network-error"]').exists()).toBe(false)
    expect(w.get('[data-testid="public-gate-visual"]').html()).toContain('ok')
  })

  it('plan g4.3: poll authoritative idle clears thinking/Cancel; confirm uses local !sessionBusy', async () => {
    window.location.hash = `#t=${'ee'.repeat(32)}`
    mocks.preview.mockResolvedValue({
      status: 'active',
      kind: 'review',
      nonce: 'n-sticky',
      reactSessionAlive: true,
      sessionBusy: true,
      waiting: 0,
      activeItem: { text: '改成绿的' },
      actions: { confirm: 'confirm', reply: 'reply', cancel: 'cancel' },
      turns: [{ role: 'agent', text: '请复审', at: '2026-08-01T00:00:00Z' }],
    })
    const w = mountView()
    await flushPromises()
    await flushPromises()
    expect(w.find('[data-testid="clarify-busy-placeholder"]').exists()).toBe(true)
    expect(w.find('[data-testid="clarify-review-cancel"]').exists()).toBe(true)
    // plan g1/g2: sticky busy keeps confirm visible + disabled until idle.
    const confirmSticky = w.find('[data-testid="clarify-confirm-flow"]')
    expect(confirmSticky.exists()).toBe(true)
    expect((confirmSticky.element as HTMLButtonElement).disabled).toBe(true)

    // Authoritative idle poll — clear stale activeItem explicitly (sparse merge).
    mocks.preview.mockResolvedValue({
      status: 'active',
      kind: 'review',
      nonce: 'n-idle',
      reactSessionAlive: true,
      sessionBusy: false,
      waiting: 0,
      activeItem: null,
      actions: { confirm: 'confirm', reply: 'reply', cancel: 'cancel' },
      turns: [
        { role: 'agent', text: '请复审', at: '2026-08-01T00:00:00Z' },
        { role: 'human', text: '改成绿的', at: '2026-08-01T00:01:00Z' },
        { role: 'agent', text: '标题已改为绿色', at: '2026-08-01T00:02:00Z' },
      ],
    })
    await (w.vm as unknown as { loadPreview: (opts?: { silent?: boolean }) => Promise<void> }).loadPreview({
      silent: true,
    })
    await flushPromises()
    await flushPromises()

    expect(w.text()).not.toContain('Agent 正在思考下一轮')
    expect(w.find('[data-testid="clarify-review-cancel"]').exists()).toBe(false)
    expect(w.find('[data-testid="clarify-busy-placeholder"]').exists()).toBe(false)
    const confirm = w.find('[data-testid="clarify-confirm-flow"]')
    expect(confirm.exists()).toBe(true)
    expect((confirm.element as HTMLButtonElement).disabled).toBe(false)

    // Lagged preview.sessionBusy must not re-disable confirm when local already idle.
    const preview = (w.vm as unknown as { preview: { sessionBusy?: boolean } }).preview
    if (preview) preview.sessionBusy = true
    await flushPromises()
    expect((w.find('[data-testid="clarify-confirm-flow"]').element as HTMLButtonElement).disabled).toBe(false)
  })

  it('plan g4.3: ghost queued + turn_done synthesizes idle; confirm follows local busy', async () => {
    window.location.hash = `#t=${'ff'.repeat(32)}`
    mocks.reply.mockResolvedValue({ status: 'accepted' })
    const base = {
      status: 'active',
      kind: 'review',
      nonce: 'n-ws-idle',
      reactSessionAlive: true,
      sessionBusy: false,
      waiting: 0,
      activeItem: null as null,
      actions: { confirm: 'confirm', reply: 'reply', cancel: 'cancel' },
      turns: [{ role: 'agent', text: '请复审', at: '2026-08-01T00:00:00Z' }],
    }
    mocks.preview.mockResolvedValue({ ...base })
    const w = mountView()
    await flushPromises()
    await flushPromises()

    const chat = w.getComponent(ClarifyChat)
    const vm = chat.vm as unknown as {
      applyReviewFrame: (f: Record<string, unknown>) => void
      applyAcpEvents: (e: { kind: string; text: string }[], nodeId?: string) => void
      isSessionBusy: () => boolean
    }
    await w.get('[data-testid="clarify-input"]').setValue('请改验收')
    const send = w.find('[data-testid="clarify-send-label"]')
    if (send.exists()) await send.trigger('click')
    else await w.find('[data-testid="clarify-send-icon"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="clarify-review-queue"]').exists()).toBe(true)
    expect(w.text()).toContain('Agent 正在思考下一轮')

    // Optimistic no-id ghost + turn_begin with id (no text fallback).
    vm.applyReviewFrame({
      event: 'turn_begin',
      nodeId: 'public-gate',
      item: { id: 'srv-p1', text: '请改验收' },
    })
    vm.applyAcpEvents([{ kind: 'message', text: '验收已更新' }], 'public-gate')
    await flushPromises()
    vm.applyReviewFrame({ event: 'turn_done', nodeId: 'public-gate' })
    await flushPromises()
    expect(vm.isSessionBusy()).toBe(false)
    expect(w.text()).not.toContain('Agent 正在思考下一轮')
    expect(w.find('[data-testid="clarify-review-cancel"]').exists()).toBe(false)

    // Persist turns so pendingReplyText clears; WS turn_done refreshes parent localChatBusy.
    mocks.preview.mockResolvedValue({
      ...base,
      nonce: 'n-ws-idle-2',
      turns: [
        { role: 'agent', text: '请复审', at: '2026-08-01T00:00:00Z' },
        { role: 'human', text: '请改验收', at: '2026-08-01T00:01:00Z' },
        { role: 'agent', text: '验收已更新', at: '2026-08-01T00:02:00Z' },
      ],
    })
    const sock = FakeWebSocket.instances[FakeWebSocket.instances.length - 1]
    sock.emit({ type: 'review', event: 'turn_done', nodeId: 'public-gate' })
    await (w.vm as unknown as { loadPreview: (opts?: { silent?: boolean }) => Promise<void> }).loadPreview({
      silent: true,
    })
    await flushPromises()

    expect(w.find('[data-testid="clarify-confirm-flow"]').exists()).toBe(true)
    expect((w.find('[data-testid="clarify-confirm-flow"]').element as HTMLButtonElement).disabled).toBe(false)

    // Even if preview.sessionBusy lags true, local idle keeps confirm clickable.
    const preview = (w.vm as unknown as { preview: { sessionBusy?: boolean } }).preview
    if (preview) preview.sessionBusy = true
    await flushPromises()
    expect((w.find('[data-testid="clarify-confirm-flow"]').element as HTMLButtonElement).disabled).toBe(false)
  })
})

describe('PublicGateApprovalView chat-only drawer mode', () => {
  const drawerToken = `gse_${'ab'.repeat(16)}`

  it('drives the chat with the embed token and hides stage, chrome and decide', async () => {
    window.location.hash = `#t=${'cc'.repeat(32)}`
    mocks.preview.mockResolvedValue({
      status: 'active',
      kind: 'review',
      nodeType: 'app_preview',
      remainingSec: 3600,
      reactSessionAlive: true,
      productKind: 'app_preview',
      actions: { confirm: 'confirm', reply: 'reply', cancel: 'cancel' },
      turns: [{ role: 'agent', text: '预览已就绪', at: '2026-08-01T00:00:00Z' }],
    })
    const w = mountView('zh-CN', { embedToken: drawerToken })
    await flushPromises()
    expect(mocks.preview.mock.calls[0][0]).toBe(drawerToken)
    expect(mocks.artifacts).not.toHaveBeenCalled()
    expect(w.find('[data-testid="public-gate-chrome"]').exists()).toBe(false)
    expect(w.find('[data-testid="review-shell-stage"]').exists()).toBe(false)
    expect(w.find('[data-testid="review-shell-sash"]').exists()).toBe(false)
    expect(w.find('[data-testid="clarify-confirm-flow"]').exists()).toBe(false)
    expect(w.find('[data-testid="public-gate-footer"]').exists()).toBe(false)
    expect(w.find('[data-testid="clarify-input"]').exists()).toBe(true)
    expect(w.emitted('status')?.at(-1)).toEqual(['active'])

    ;(w.vm as unknown as { addPick: (p: unknown) => void }).addPick({
      selector: 'button.buy',
      tagName: 'BUTTON',
      outerHTML: '<button class="buy">Buy</button>',
      url: 'http://127.0.0.1:18080/cart',
    })
    await flushPromises()
    expect(w.get('[data-testid="public-gate-sidebar"]').text()).toContain('/cart · button.buy')
  })

  it('cold drawer session offers no confirm and points back to Grasp', async () => {
    mocks.preview.mockResolvedValue({
      status: 'active',
      kind: 'review',
      nodeType: 'grasp',
      remainingSec: 3600,
      reactSessionAlive: false,
      actions: { confirm: 'confirm', reply: 'reply' },
      turns: [{ role: 'agent', text: '完成', at: '2026-08-01T00:00:00Z' }],
    })
    const w = mountView('zh-CN', { embedToken: drawerToken })
    await flushPromises()
    expect(w.find('[data-testid="clarify-confirm-flow"]').exists()).toBe(false)
    expect(w.find('[data-testid="clarify-input"]').exists()).toBe(false)
    expect(w.get('[data-testid="public-gate-cold-hint"]').text()).toContain('回到 Grasp')
  })

  it('reports a dead drawer token so the host can drop it', async () => {
    mocks.preview.mockResolvedValue({ status: 'invalid' })
    const w = mountView('zh-CN', { embedToken: drawerToken })
    await flushPromises()
    expect(w.emitted('status')?.at(-1)).toEqual(['invalid'])
  })
})
