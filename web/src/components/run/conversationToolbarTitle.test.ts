// @vitest-environment happy-dom
import { defineComponent } from 'vue'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import { sidebarRunTitle } from '@/lib/inbox/inboxDisplay'
import { usePendingGates } from '@/lib/inbox/usePendingGates'
import type { NodeRun, Run, WFNode } from '@/lib/shared/types'
import RunClarifyPanel from './RunClarifyPanel.vue'
import RunReviewPanel from './RunReviewPanel.vue'

const liveMocks = vi.hoisted(() => ({
  getRun: vi.fn(),
  nodeEvents: vi.fn(),
  runConversationChain: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      getRun: liveMocks.getRun,
      nodeEvents: liveMocks.nodeEvents,
      runConversationChain: liveMocks.runConversationChain,
      takeoverNode: vi.fn(),
      reactReply: vi.fn(),
      runEventsWsUrl: (id: string) => `ws://test/api/runs/${id}/events`,
    },
  }
})

vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({ show: vi.fn(), error: vi.fn(), info: vi.fn(), warning: vi.fn(), success: vi.fn() }),
}))

import RunLiveView from '@/views/RunLiveView.vue'

function i18n() {
  return createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
}

const stubNode = {
  id: 'n1',
  type: 'agent',
  label: 'Agent',
  position: { x: 0, y: 0 },
  config: {},
} as WFNode

const stubNodeRun = { nodeId: 'n1', status: 'waiting_human' } as NodeRun

function stubRun(title: string, workflowName = '产品迭代'): Run {
  return {
    id: 'run-1',
    workflowId: 'wf-1',
    workflowName,
    title,
    status: 'waiting_human',
    trigger: 'manual',
    startedAt: '2026-08-12T00:00:00Z',
    durationSec: 1,
    progress: 1,
    nodeRuns: { n1: stubNodeRun },
    artifacts: [],
  } as Run
}

const quietChildren = {
  ClarifyChat: true,
  ReviewComposer: true,
  ReactArtifactStage: true,
  ReactConnectingState: true,
  Icon: true,
  StatusPill: true,
  ArtifactLoadingPane: true,
  AppInlineError: true,
}

class FakeSocket {
  static last: FakeSocket | null = null
  onopen: (() => void) | null = null
  onmessage: ((ev: { data: string }) => void) | null = null
  onclose: (() => void) | null = null
  constructor(public url: string) { FakeSocket.last = this; queueMicrotask(() => this.onopen?.()) }
  close() { this.onclose?.() }
  send() {}
}

beforeEach(() => {
  vi.stubGlobal('WebSocket', FakeSocket)
  liveMocks.getRun.mockReset()
  liveMocks.nodeEvents.mockReset()
  liveMocks.nodeEvents.mockResolvedValue({ events: [] })
  liveMocks.runConversationChain.mockReset()
  liveMocks.runConversationChain.mockRejectedValue(Object.assign(new Error('until_not_found'), { status: 400 }))
})

afterEach(() => {
  usePendingGates().displayedItems.value = []
  vi.unstubAllGlobals()
})

describe('run conversation toolbar titles', () => {
  it('clarify panel shows sidebarRunTitle and follows a new run title', async () => {
    const wrapper = mount(RunClarifyPanel, {
      props: {
        sandboxFailed: false,
        nodeLabel: '澄清',
        nodeId: 'c1',
        clarify: { nodeId: 'c1', turns: [], done: false },
        runId: 'run-1',
        run: stubRun('优化这里的段间距'),
        draft: '',
        attachments: [],
        inputActive: true,
      },
      global: { plugins: [i18n()], stubs: quietChildren },
    })
    expect(wrapper.get('[data-testid="conversation-title"]').text()).toBe(
      sidebarRunTitle({ runTitle: '优化这里的段间距', workflowName: '产品迭代' }),
    )
    expect(wrapper.get('[data-testid="conversation-title"]').attributes('title')).toBe('优化这里的段间距')
    await wrapper.setProps({ run: stubRun('丙') })
    expect(wrapper.get('[data-testid="conversation-title"]').text()).toBe('丙')
    await wrapper.setProps({ run: stubRun('  ', '  ') })
    expect(wrapper.find('[data-testid="conversation-title-skeleton"]').exists()).toBe(true)
    expect(wrapper.get('.conversation-heading').text()).not.toContain('对话')
    wrapper.unmount()
  })

  it('review panel shows sidebarRunTitle for that run', () => {
    const wrapper = mount(RunReviewPanel, {
      props: {
        mobile: false,
        node: stubNode,
        nodeRun: stubNodeRun,
        run: stubRun('优化这里的段间距'),
        clarify: { nodeId: 'n1', turns: [], done: false },
        draft: '',
        attachments: [],
        annotations: [],
        inputActive: true,
      },
      global: { plugins: [i18n()], stubs: quietChildren },
    })
    expect(wrapper.get('[data-testid="conversation-title"]').text()).toBe(
      sidebarRunTitle({ runTitle: '优化这里的段间距', workflowName: '产品迭代' }),
    )
    wrapper.unmount()
  })

  it('live view shows sidebarRunTitle for the open run', async () => {
    liveMocks.getRun.mockResolvedValue({
      ...stubRun('优化这里的段间距'),
      currentNode: { id: 'c1', name: '澄清', iteration: 1, kind: 'clarify', mode: 'manual', state: 'waiting_human' },
    })
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/runs/:id/live', component: defineComponent({ template: '<div />' }) }],
    })
    await router.push('/runs/run-1/live')
    const wrapper = mount(RunLiveView, {
      global: {
        plugins: [router, i18n()],
        stubs: {
          ...quietChildren,
          ReviewComposer: defineComponent({
            name: 'ReviewComposer',
            setup(_, { expose }) {
              expose({
                applyReviewFrame: () => {},
                applyAcpEvents: () => {},
                releaseConfirmPending: () => {},
              })
              return () => null
            },
          }),
        },
      },
    })
    await flushPromises()
    expect(wrapper.get('[data-testid="conversation-title"]').text()).toBe(
      sidebarRunTitle({ runTitle: '优化这里的段间距', workflowName: '产品迭代' }),
    )
    wrapper.unmount()
  })
})

describe('inbox rename reaches an open conversation', () => {
  it('patchRunTitle replaces the toolbar title', async () => {
    const { displayedItems } = usePendingGates()
    displayedItems.value = [{
      type: 'clarify',
      runId: 'run-1',
      nodeId: 'c1',
      workflowName: '产品迭代',
      runTitle: '甲',
      label: '澄清',
      done: false,
      requestedAt: '2026-08-12T00:00:00Z',
      updatedAt: '2026-08-12T00:00:00Z',
    }]
    const wrapper = mount(RunClarifyPanel, {
      props: {
        sandboxFailed: false,
        nodeLabel: '澄清',
        nodeId: 'c1',
        clarify: { nodeId: 'c1', turns: [], done: false },
        runId: 'run-1',
        run: stubRun('甲'),
        draft: '',
        attachments: [],
        inputActive: true,
      },
      global: { plugins: [i18n()], stubs: quietChildren },
    })
    expect(wrapper.get('[data-testid="conversation-title"]').text()).toBe('甲')
    usePendingGates().patchRunTitle('run-1', '丙')
    await flushPromises()
    expect(wrapper.get('[data-testid="conversation-title"]').text()).toBe(
      sidebarRunTitle({ runTitle: '丙', workflowName: '产品迭代' }),
    )
    displayedItems.value = []
    wrapper.unmount()
  })
})
