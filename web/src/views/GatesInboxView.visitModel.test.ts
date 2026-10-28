// @vitest-environment happy-dom
import { computed, defineComponent, nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { InboxItem } from '@/lib/shared/types'
import { CLARIFY_CAPS } from '@/test/capsFixtures'

const mocks = vi.hoisted(() => ({
  listGates: vi.fn(),
  inboxContext: vi.fn(),
  getRun: vi.fn(async () => {
    throw new Error('no run in this suite')
  }),
  getVisitModel: vi.fn(),
  setVisitModel: vi.fn(),
  runEventsWsUrl: vi.fn((id: string) => `ws://test/runs/${id}/events`),
}))

const routeState = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports -- vi.hoisted runs before ESM imports
  const { reactive } = require('vue') as typeof import('vue')
  return reactive({ query: {} as Record<string, string> })
})

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      listGates: mocks.listGates,
      inboxContext: mocks.inboxContext,
      getRun: mocks.getRun,
      getVisitModel: mocks.getVisitModel,
      setVisitModel: mocks.setVisitModel,
      resumeGate: vi.fn(),
      reactReply: vi.fn(),
      nodeEvents: vi.fn(async () => ({ events: [], live: false })),
      runArtifacts: vi.fn(async () => []),
      runEventsWsUrl: mocks.runEventsWsUrl,
      runConversationChain: async () => {
        throw new Error('chain unavailable in this suite')
      },
    },
  }
})

vi.mock('@/lib/solo/useSessionModelCatalog', () => ({
  useSessionModelCatalog: () => ({
    choices: computed(() => [{ id: 'kimi-k2.5', name: 'Kimi K2.5', hint: 'kimi-k2.5', vendor: '', backend: false, unavailable: false, configurationId: 'group-kimi', modelId: 'kimi-k2.5' }]),
    ready: computed(() => true),
    blocked: computed(() => false),
    configLoaded: computed(() => true),
    catalogConfirmed: computed(() => true),
    modelInConfiguration: () => true,
    defaultConfigurationId: computed(() => 'group-kimi'),
  }),
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  useRoute: () => ({ query: routeState.query, params: {} }),
}))

vi.mock('@/lib/composables/useBreakpoint', async () => {
  const { ref: vueRef } = await import('vue')
  return { useBreakpoint: () => ({ isMobile: vueRef(false) }) }
})

vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({ error: vi.fn(), success: vi.fn(), info: vi.fn(), warning: vi.fn(), warn: vi.fn(), show: vi.fn() }),
}))

vi.mock('@/lib/composables/useWorkflowFilter', async () => {
  const { ref: vueRef } = await import('vue')
  return { useWorkflowFilter: () => ({ selected: vueRef('') }) }
})

vi.mock('@/lib/inbox/useClarifyDraft', async () => {
  const { ref: vueRef } = await import('vue')
  return { useClarifyDraft: () => ({ draft: vueRef(''), attachments: vueRef([]), annotations: vueRef([]) }) }
})

import GatesInboxView from './GatesInboxView.vue'
import { usePendingGates } from '@/lib/inbox/usePendingGates'

class FakeWebSocket {
  onmessage: ((ev: { data: string }) => void) | null = null
  onclose: (() => void) | null = null
  readyState = 1
  constructor(public url: string) {}
  close() {
    this.readyState = 3
    this.onclose?.()
  }
}

function clarifyItem(): InboxItem {
  return {
    type: 'clarify',
    runId: 'run-clarify-model',
    nodeId: 'clarify',
    iteration: 1,
    workflowName: 'wf',
    label: '需求澄清',
    done: false,
    requestedAt: new Date().toISOString(),
    updatedAt: new Date().toISOString(),
  }
}

function mountInbox() {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    missingWarn: false,
    fallbackWarn: false,
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(GatesInboxView, {
    global: {
      plugins: [i18n],
      stubs: {
        Icon: true,
        EmptyState: true,
        WorkflowFilter: true,
        Pagination: true,
        ArtifactLoadingPane: true,
        GateApproval: true,
        ClarifyProductStage: true,
        ReactArtifactStage: true,
        ReactConnectingState: true,
        ConfirmFlowOverlay: true,
        ReviewShell: defineComponent({
          props: { runName: { type: String, default: '' } },
          setup() {
            return {}
          },
          template: '<div data-testid="review-shell"><slot name="stage" /><slot name="sidebar" /></div>',
        }),
      },
    },
  })
}

const here = dirname(fileURLToPath(import.meta.url))

describe('GatesInboxView visit model select', () => {
  beforeEach(async () => {
    vi.clearAllMocks()
    routeState.query = {}
    vi.stubGlobal('WebSocket', FakeWebSocket as unknown as typeof WebSocket)
    mocks.getVisitModel.mockResolvedValue({
      currentModelSet: true,
      currentModelId: 'kimi-k2.5',
      currentConfigurationId: 'group-kimi',
    })
    mocks.listGates.mockResolvedValue({ items: [], total: 0, page: 1, pageSize: 20 })
    await usePendingGates().refresh({ mode: 'force' })
    mocks.inboxContext.mockImplementation(async (_runId: string, nodeId: string) => ({
      type: 'clarify',
      status: 'waiting_human',
      nodes: [{
        id: nodeId,
        type: 'agent',
        label: '需求澄清',
        position: { x: 0, y: 0 },
        caps: CLARIFY_CAPS,
        config: { agent_profile: '澄清' },
      }],
      artifacts: [],
      nodeExecutions: {},
      clarify: {
        nodeId,
        iteration: 1,
        turns: [],
        done: false,
        label: '需求澄清',
        currentModelSet: true,
        currentModelId: 'kimi-k2.5',
        currentConfigurationId: 'group-kimi',
      },
    }))
  })

  it('shows the model selector on a sendable clarify composer', async () => {
    const item = clarifyItem()
    mocks.listGates.mockResolvedValue({ items: [item], total: 1, page: 1, pageSize: 20 })
    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()
    await flushPromises()
    const input = wrapper.get('[data-testid="clarify-input"]')
    expect(input.attributes('placeholder')).toContain('请先描述目标')
    expect(wrapper.find('[data-testid="visit-model-select"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="visit-model-select"]').text()).toContain('Kimi K2.5')
    wrapper.unmount()
  })

  it('passes the same switch into the desktop and narrow composers, and into the live run chat', () => {
    const inbox = readFileSync(join(here, 'GatesInboxView.vue'), 'utf8')
    const live = readFileSync(join(here, 'RunLiveView.vue'), 'utf8')
    expect(inbox.match(/:model-switch="!!inboxAgentName"/g)?.length).toBe(2)
    expect(inbox.match(/:agent-name="inboxAgentName"/g)?.length).toBe(2)
    expect(live).toMatch(/:model-switch="mode === 'chat' && !!liveAgentName"/)
    expect(live).toMatch(/:agent-name="liveAgentName"/)
  })
})
