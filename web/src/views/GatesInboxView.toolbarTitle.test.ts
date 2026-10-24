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

const mocks = vi.hoisted(() => ({
  listGates: vi.fn(),
  inboxContext: vi.fn(),
}))

const routeState = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports -- vi.hoisted runs before ESM imports
  const { reactive: makeReactive } = require('vue') as typeof import('vue')
  return makeReactive({ query: {} as Record<string, string>, mobile: false })
})

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      listGates: mocks.listGates,
      inboxContext: mocks.inboxContext,
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
  useRoute: () => ({ query: routeState.query, params: {} }),
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


describe('GatesInboxView mobile toolbar navigation', () => {
  function mountMobile() {
    return mount(GatesInboxView, {
      global: {
        plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages, ...shell } } })],
        stubs: { Icon: true, EmptyState: true, GateApproval: true, ReviewComposer: true, ReactArtifactStage: true, ReactConnectingState: true, ArtifactLoadingPane: true, ClarifyProductStage: true },
      },
    })
  }
  async function seed(items: InboxItem[], context: Record<string, unknown>) {
    routeState.mobile = true
    mocks.listGates.mockResolvedValue({ items, total: items.length, page: 1, pageSize: 20 })
    await usePendingGates().refresh({ mode: 'force' })
    mocks.inboxContext.mockResolvedValue(context)
  }

  it('keeps share, navigation and resources in one chat toolbar and restores the list scroll', async () => {
    const longTitle = '这是一段需要省略但保留完整提示的会话标题'.repeat(6)
    const item = { ...clarifyItem(longTitle), kind: 'clarify' } as InboxItem
    await seed([item], { type: 'clarify', status: 'waiting_human', nodes: [{ id: 'c1', type: 'agent', caps: CLARIFY_CAPS, label: '澄清' }], artifacts: [], nodeExecutions: {}, clarify: { nodeId: 'c1', iteration: 1, turns: [], done: false, label: '澄清' } })
    const wrapper = mountMobile()
    await flushPromises()
    const scroll = wrapper.get('[data-testid="inbox-list-fade"]').element.closest('.scroll-area') as HTMLElement
    scroll.scrollTop = 73
    await wrapper.get('[data-testid="inbox-item-card"] > button').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="inbox-mobile-detail-header"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-testid="conversation-title"]')).toHaveLength(1)
    const title = wrapper.get('[data-testid="conversation-title"]')
    expect(title.text()).toBe(longTitle)
    expect(title.attributes('title')).toBe(longTitle)
    expect(title.classes()).toContain('truncate')
    const back = wrapper.get('.conversation-toolbar [data-testid="inbox-mobile-chat-back"]')
    expect(back.attributes('aria-label')).toBe('返回列表')
    const toolbar = wrapper.get('.conversation-toolbar')
    expect(wrapper.findAll('.conversation-toolbar')).toHaveLength(1)
    expect(toolbar.find('[data-testid="conversation-resource-toggle"]').exists()).toBe(true)
    expect(toolbar.find('[data-testid="conversation-panel-toggle"]').exists()).toBe(true)
    expect(wrapper.findAll('[data-testid="gate-share-copy-btn-detail"]')).toHaveLength(1)
    const share = toolbar.get('[data-testid="gate-share-copy-btn-detail"]')
    expect(share.attributes('aria-label')).toBe(pages.pages.gatesInbox.share.copyLinkAria)
    expect(share.text()).toBe('')
    await share.trigger('click')
    await flushPromises()
    const sharePanel = wrapper.getComponent(GateShareLinkPanel)
    expect(sharePanel.props('open')).toBe(true)
    expect(sharePanel.props('target')).toMatchObject({ runId: item.runId, nodeId: item.nodeId })
    expect(sharePanel.props('kind')).toBe('review')
    sharePanel.vm.$emit('close')
    await flushPromises()
    await back.trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="inbox-active-detail"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-testid="inbox-item-card"]')).toHaveLength(1)
    const restored = wrapper.get('[data-testid="inbox-list-fade"]').element.closest('.scroll-area') as HTMLElement
    expect(restored.scrollTop).toBe(73)
    wrapper.unmount()
  })

  it('preserves the existing separate header and return action for a regular approval', async () => {
    const gate = { type: 'gate', nodeType: 'human_gate', runId: 'run-gate', nodeId: 'gate', workflowName: '审批工作流', title: '发布审批', bodyMd: '', actions: [{ id: 'approve', label: '通过' }], requestedAt: '2026-08-12T00:00:00Z' } as InboxItem
    await seed([gate], { type: 'gate', status: 'waiting_human', nodes: [], artifacts: [], nodeExecutions: {} })
    const wrapper = mountMobile()
    await flushPromises()
    await wrapper.get('[data-testid="inbox-item-card"] > button').trigger('click')
    await flushPromises()
    const header = wrapper.get('[data-testid="inbox-mobile-detail-header"]')
    expect(header.text()).toContain('发布审批')
    expect(wrapper.find('[data-testid="inbox-mobile-chat-back"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="gate-share-copy-btn-detail"]').text()).toBe(pages.pages.gatesInbox.share.copyLink)
    await header.get('button').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="inbox-active-detail"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="inbox-item-card"]').exists()).toBe(true)
    wrapper.unmount()
  })
})
