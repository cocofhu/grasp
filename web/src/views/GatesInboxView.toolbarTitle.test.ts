// @vitest-environment happy-dom
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import shell from '@/locales/zh-CN/shell.json'
import { sidebarRunTitle } from '@/lib/inbox/inboxDisplay'
import { usePendingGates } from '@/lib/inbox/usePendingGates'
import type { InboxItem } from '@/lib/shared/types'
import { CLARIFY_CAPS } from '@/test/capsFixtures'
import { INBOX_SHARE_INTENT_QUERY, INBOX_SHARE_INTENT_VALUE } from '@/lib/inbox/inboxShareIntent'
import ReviewShell from '@/components/run/ReviewShell.vue'
import GateApproval from '@/components/run/GateApproval.vue'

const mocks = vi.hoisted(() => ({
  listGates: vi.fn(),
  inboxContext: vi.fn(),
}))

const routeState = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports -- vi.hoisted runs before ESM imports
  const { reactive: makeReactive } = require('vue') as typeof import('vue')
  return makeReactive({ query: {} as Record<string, string>, params: {} as Record<string, string>, mobile: false })
})

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      listGates: mocks.listGates,
      inboxContext: mocks.inboxContext,
      getRun: vi.fn(async (id: string) => ({ id, nodes: [], edges: [] })),
      nodeEvents: vi.fn().mockResolvedValue({ events: [], live: false }),
      runArtifacts: vi.fn().mockResolvedValue([]),
      resumeGate: vi.fn(),
      reactReply: vi.fn(),
      runEventsWsUrl: (id: string) => `ws://test/runs/${id}/events`,
      runConversationChain: vi.fn().mockRejectedValue(new Error('chain unavailable')),
    },
  }
})

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  useRoute: () => ({ query: routeState.query, params: routeState.params }),
}))

vi.mock('@/lib/composables/useBreakpoint', async () => {
  const { toRef } = await import('vue')
  return { useBreakpoint: () => ({ isMobile: toRef(routeState, 'mobile') }) }
})

vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({ error: vi.fn(), success: vi.fn(), info: vi.fn(), warning: vi.fn(), show: vi.fn() }),
}))

vi.mock('@/lib/composables/useWorkflowFilter', async () => {
  const { ref } = await import('vue')
  return { useWorkflowFilter: () => ({ selected: ref('') }) }
})

import GatesInboxView from './GatesInboxView.vue'
import GateShareLinkPanel from '@/components/run/GateShareLinkPanel.vue'

class FakeWebSocket {
  onmessage: ((ev: { data: string }) => void) | null = null
  onclose: (() => void) | null = null
  readyState = 1
  constructor(public url: string) {}
  close() { this.readyState = 3; this.onclose?.() }
}

function clarifyItem(runTitle: string, workflowName = '产品迭代'): InboxItem {
  return {
    type: 'clarify',
    runId: 'run-1',
    nodeId: 'c1',
    iteration: 1,
    workflowName,
    runTitle,
    label: '澄清',
    done: false,
    requestedAt: '2026-08-12T00:00:00Z',
    updatedAt: '2026-08-12T00:00:00Z',
  }
}

beforeEach(async () => {
  routeState.query = {}
  routeState.params = {}
  routeState.mobile = false
  vi.stubGlobal('WebSocket', FakeWebSocket)
  mocks.listGates.mockReset()
  mocks.inboxContext.mockReset()
  mocks.listGates.mockResolvedValue({ items: [], total: 0, page: 1, pageSize: 20 })
  await usePendingGates().refresh({ mode: 'force' })
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('GatesInboxView conversation toolbar', () => {
  it('shows sidebarRunTitle and follows patchRunTitle', async () => {
    const item = clarifyItem('优化这里的段间距')
    mocks.listGates.mockResolvedValue({ items: [item], total: 1, page: 1, pageSize: 20 })
    await usePendingGates().refresh({ mode: 'force' })
    mocks.inboxContext.mockResolvedValue({
      type: 'clarify',
      status: 'waiting_human',
      nodes: [{ id: 'c1', type: 'agent', caps: CLARIFY_CAPS, label: '澄清' }],
      artifacts: [],
      nodeExecutions: {},
      clarify: { nodeId: 'c1', iteration: 1, turns: [], done: false, label: '澄清' },
    })
    routeState.query = { run: 'run-1', node: 'c1' }
    const wrapper = mount(GatesInboxView, {
      global: {
        plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages, ...shell } } })],
        stubs: {
          Icon: true,
          EmptyState: true,
          GateApproval: true,
          ReviewComposer: true,
          ReactArtifactStage: true,
          ReactConnectingState: true,
          ArtifactLoadingPane: true,
          ClarifyProductStage: true,
        },
      },
    })
    await flushPromises()
    await nextTick()
    await flushPromises()
    const expected = sidebarRunTitle({ runTitle: '优化这里的段间距', workflowName: '产品迭代' })
    expect(wrapper.get('[data-testid="conversation-title"]').text()).toBe(expected)
    usePendingGates().patchRunTitle('run-1', '丙')
    await nextTick()
    expect(wrapper.get('[data-testid="conversation-title"]').text()).toBe(
      sidebarRunTitle({ runTitle: '丙', workflowName: '产品迭代' }),
    )
    wrapper.unmount()
  })

  it('shows the placeholder when the inbox run has no name yet', async () => {
    const item = clarifyItem('', '')
    mocks.listGates.mockResolvedValue({ items: [item], total: 1, page: 1, pageSize: 20 })
    await usePendingGates().refresh({ mode: 'force' })
    mocks.inboxContext.mockResolvedValue({
      type: 'clarify',
      status: 'waiting_human',
      nodes: [{ id: 'c1', type: 'agent', caps: CLARIFY_CAPS, label: '澄清' }],
      artifacts: [],
      nodeExecutions: {},
      clarify: { nodeId: 'c1', iteration: 1, turns: [], done: false, label: '澄清' },
    })
    routeState.query = { run: 'run-1', node: 'c1' }
    const wrapper = mount(GatesInboxView, {
      global: {
        plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages, ...shell } } })],
        stubs: {
          Icon: true,
          EmptyState: true,
          GateApproval: true,
          ReviewComposer: true,
          ReactArtifactStage: true,
          ReactConnectingState: true,
          ArtifactLoadingPane: true,
          ClarifyProductStage: true,
        },
      },
    })
    await flushPromises()
    await nextTick()
    await flushPromises()
    expect(wrapper.find('[data-testid="conversation-title-skeleton"]').exists()).toBe(true)
    expect(wrapper.get('.conversation-heading').text()).not.toContain('对话')
    wrapper.unmount()
  })
})


describe('GatesInboxView mobile conversations', () => {
  function mountMobile() {
    return mount(GatesInboxView, {
      global: {
        plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages, ...shell } } })],
        stubs: { Icon: true, EmptyState: true, GateApproval: true, ReviewComposer: true, ReactArtifactStage: true, ReactConnectingState: true, ArtifactLoadingPane: true, ClarifyProductStage: true },
      },
    })
  }
  async function seedChat() {
    routeState.mobile = true
    routeState.params = { runId: 'run-1', nodeId: 'c1' }
    const item = { ...clarifyItem('当前会话'), kind: 'clarify' } as InboxItem
    mocks.listGates.mockResolvedValue({ items: [item], total: 1, page: 1, pageSize: 100 })
    await usePendingGates().refresh({ mode: 'force' })
    mocks.inboxContext.mockResolvedValue({
      type: 'clarify', status: 'waiting_human',
      nodes: [{ id: 'c1', type: 'agent', caps: CLARIFY_CAPS, label: '澄清' }],
      artifacts: [], nodeExecutions: {},
      clarify: { nodeId: 'c1', iteration: 1, turns: [], done: false, label: '澄清' },
    })
  }

  it('opens a mobile deep link and a refreshed conversation directly in chat', async () => {
    await seedChat()
    for (let visit = 0; visit < 2; visit++) {
      const wrapper = mountMobile()
      await flushPromises()
      expect(wrapper.get('[data-testid="conversation-title"]').text()).toBe('当前会话')
      expect(wrapper.get('[data-testid="inbox-active-detail"]').attributes('data-run-id')).toBe('run-1')
      expect(wrapper.find('[data-testid="inbox-item-card"]').exists()).toBe(false)
      expect(wrapper.find('[data-testid="inbox-mobile-chat-back"]').exists()).toBe(false)
      expect(wrapper.find('[data-testid="gate-share-copy-btn-detail"]').exists()).toBe(false)
      expect(wrapper.getComponent(GateShareLinkPanel).props('open')).toBe(false)
      wrapper.unmount()
    }
  })

  it('keeps the same conversation mounted when the screen size changes', async () => {
    await seedChat()
    const wrapper = mountMobile()
    await flushPromises()
    const chat = wrapper.getComponent(ReviewShell).element
    routeState.mobile = false
    await nextTick()
    expect(wrapper.getComponent(ReviewShell).element).toBe(chat)
    routeState.mobile = true
    await nextTick()
    expect(wrapper.getComponent(ReviewShell).element).toBe(chat)
    expect(wrapper.get('[data-testid="conversation-title"]').text()).toBe('当前会话')
    wrapper.unmount()
  })

  it('opens sharing only for the sidebar menu intent', async () => {
    await seedChat()
    routeState.query = { [INBOX_SHARE_INTENT_QUERY]: INBOX_SHARE_INTENT_VALUE }
    const wrapper = mountMobile()
    await flushPromises()
    const panel = wrapper.getComponent(GateShareLinkPanel)
    expect(panel.props('open')).toBe(true)
    expect(panel.props('target')).toMatchObject({ runId: 'run-1', nodeId: 'c1' })
    expect(panel.props('kind')).toBe('review')
    panel.vm.$emit('close')
    await nextTick()
    expect(panel.props('open')).toBe(false)
    expect(wrapper.find('[data-testid="conversation-title"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('opens a regular human approval directly without the old list navigation', async () => {
    routeState.mobile = true
    routeState.params = { runId: 'run-gate', nodeId: 'gate' }
    const gate = { type: 'gate', nodeType: 'human_gate', runId: 'run-gate', nodeId: 'gate', workflowName: '审批工作流', title: '发布审批', bodyMd: '', actions: [{ id: 'approve', label: '通过' }], requestedAt: '2026-08-12T00:00:00Z' } as InboxItem
    mocks.listGates.mockResolvedValue({ items: [gate], total: 1 })
    mocks.inboxContext.mockResolvedValue({ type: 'gate', status: 'waiting_human', nodes: [], artifacts: [], nodeExecutions: {} })
    const wrapper = mountMobile()
    await flushPromises()
    expect(wrapper.getComponent(GateApproval).props('gate')).toMatchObject({ runId: 'run-gate', title: '发布审批' })
    expect(wrapper.getComponent(GateApproval).props('fillPreview')).toBe(true)
    expect(wrapper.getComponent(GateApproval).props('shareLink')).toEqual({ state: 'none' })
    wrapper.getComponent(GateApproval).vm.$emit('open-share')
    await nextTick()
    expect(wrapper.getComponent(GateShareLinkPanel).props('open')).toBe(true)
    expect(wrapper.getComponent(GateShareLinkPanel).props('kind')).toBe('human_gate')
    expect(wrapper.find('[data-testid="inbox-mobile-detail-header"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="inbox-item-card"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
