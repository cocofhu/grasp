// @vitest-environment happy-dom
import { createApp, defineComponent, nextTick, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { Run, WFNode, Workflow } from '@/lib/shared/types'

const isMobile = ref(false)

const mocks = vi.hoisted(() => ({
  toastWarn: vi.fn(),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
  getRun: vi.fn(),
  getWorkflow: vi.fn(),
  getProject: vi.fn(),
  runArtifacts: vi.fn(),
  nodeEvents: vi.fn(),
  nodeSandboxLog: vi.fn(),
  getRunNodeSandbox: vi.fn(),
  updateRunPriority: vi.fn(),
}))

vi.mock('@/lib/composables/useBreakpoint', () => ({
  useBreakpoint: () => ({ isMobile }),
}))

vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({
    warn: mocks.toastWarn,
    success: mocks.toastSuccess,
    error: mocks.toastError,
    info: vi.fn(),
  }),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      getRun: mocks.getRun,
      getWorkflow: mocks.getWorkflow,
      getProject: mocks.getProject,
      runArtifacts: mocks.runArtifacts,
      nodeEvents: mocks.nodeEvents,
      nodeSandboxLog: mocks.nodeSandboxLog,
      getRunNodeSandbox: mocks.getRunNodeSandbox,
      updateRunPriority: mocks.updateRunPriority,
    },
  }
})

import { useRunDetail } from './useRunDetail'

function stubNode(partial: Partial<WFNode> & Pick<WFNode, 'id' | 'type'>): WFNode {
  return { label: partial.id, position: { x: 0, y: 0 }, config: {}, ...partial }
}

const sampleNodes = [
  stubNode({ id: 'n1', type: 'agent' }),
  stubNode({ id: 'n2', type: 'output' }),
]

const sampleRun = (): Run =>
  ({
    id: 'run-1',
    workflowId: 'wf-1',
    workflowName: '夜间回归',
    status: 'running',
    trigger: 'manual',
    startedAt: new Date().toISOString(),
    durationSec: 0,
    progress: 0.5,
    priority: 'normal',
    nodeRuns: {
      n1: { nodeId: 'n1', status: 'running', outputs: {}, events: [], mcpCalls: [] },
      n2: { nodeId: 'n2', status: 'pending', outputs: {} },
    },
    artifacts: [{ id: 'art-1', name: 'out.txt', nodeId: 'n2' }],
    trace: [],
    vars: [{ name: 'branch', value: 'main' }],
    nodes: sampleNodes,
    edges: [{ id: 'e1', source: 'n1', target: 'n2' }],
    reactSessions: {},
  }) as unknown as Run

const sampleWorkflow = (): Workflow => ({
  id: 'wf-1',
  projectId: 'proj-1',
  name: '夜间回归',
  description: '',
  status: 'published',
  version: 1,
  updatedAt: '',
  needsRepo: false,
  nodes: sampleNodes,
  edges: [{ id: 'e1', source: 'n1', target: 'n2' }],
})

class MockWebSocket {
  static CONNECTING = 0
  static OPEN = 1
  static CLOSING = 2
  static CLOSED = 3
  readyState = MockWebSocket.OPEN
  onopen: ((ev: Event) => void) | null = null
  onclose: ((ev: CloseEvent) => void) | null = null
  onmessage: ((ev: MessageEvent) => void) | null = null
  onerror: ((ev: Event) => void) | null = null
  constructor(_url: string) {
    queueMicrotask(() => this.onopen?.(new Event('open')))
  }
  close() {
    this.readyState = MockWebSocket.CLOSED
    this.onclose?.(new CloseEvent('close'))
  }
  send(_data: string) {}
}

class MockResizeObserver {
  observe = vi.fn()
  disconnect = vi.fn()
  unobserve = vi.fn()
  constructor(_cb: ResizeObserverCallback) {}
}

async function withRunDetail(routePath = '/runs/run-1') {
  let detail!: ReturnType<typeof useRunDetail>
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { path: '/runs/:id', component: { template: '<div />' } },
    ],
  })
  await router.push(routePath)
  await router.isReady()
  const Comp = defineComponent({
    setup() {
      detail = useRunDetail()
      return () => null
    },
  })
  const app = createApp(Comp)
  app.use(i18n)
  app.use(router)
  app.mount(document.createElement('div'))
  return { detail, app, router }
}

describe('useRunDetail', () => {
  beforeEach(() => {
    vi.stubGlobal('WebSocket', MockWebSocket)
    vi.stubGlobal('ResizeObserver', MockResizeObserver)
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
      cb(0)
      return 1
    })
    isMobile.value = false
    mocks.getRun.mockReset()
    mocks.getWorkflow.mockReset()
    mocks.getProject.mockReset()
    mocks.runArtifacts.mockReset()
    mocks.nodeEvents.mockReset()
    mocks.nodeSandboxLog.mockReset()
    mocks.getRunNodeSandbox.mockReset()
    mocks.updateRunPriority.mockReset()
    mocks.toastWarn.mockReset()
    mocks.toastSuccess.mockReset()
    mocks.toastError.mockReset()

    mocks.getRun.mockResolvedValue(sampleRun())
    mocks.getWorkflow.mockResolvedValue(sampleWorkflow())
    mocks.getProject.mockResolvedValue({ id: 'proj-1', unknownModelDisplayName: '未知模型' })
    mocks.runArtifacts.mockResolvedValue([])
    mocks.nodeEvents.mockResolvedValue({ events: [], nextCursor: '', hasMore: false })
    mocks.nodeSandboxLog.mockResolvedValue({ content: 'boot log', live: false, found: true })
    mocks.getRunNodeSandbox.mockResolvedValue({ id: 'sbx-1' })
    mocks.updateRunPriority.mockResolvedValue({ priority: 'high' })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('mounts orchestration, loads run, and exposes core state without hanging', async () => {
    const { detail, app } = await withRunDetail()
    await flushPromises()
    await nextTick()

    expect(detail.runId.value).toBe('run-1')
    expect(detail.run.value.id).toBe('run-1')
    expect(detail.wf.value.nodes.length).toBeGreaterThan(0)
    expect(detail.runLoading.value).toBe(false)
    expect(detail.elapsedSec.value).toBeGreaterThanOrEqual(0)
    expect(detail.progressFrac.value).toBeGreaterThan(0)
    expect(detail.nodeTabs.value.length).toBeGreaterThan(0)

    detail.selectNode('n1')
    expect(detail.selected.value).toBe('n1')
    detail.selectExecution('n1', 0)
    detail.onAppPreviewStagedPick(null)
    detail.onArtifactDeleted('art-1')
    detail.openCancelConfirm()
    detail.closeCancelConfirm()
    detail.openDeleteConfirm()
    detail.closeDeleteConfirm()
    detail.goSandboxLogTab()
    detail.mergeStagedAppPreviewPick([])
    expect(detail.classifyRunLoadError(new Error('404 not found'))).toBe('not_found')
    detail.fmtTime(new Date().toISOString())
    detail.fmtDuration(12)
    detail.formatTrigger('manual')
    detail.onNodeTabDisabledClick()
    await detail.fetchSandboxLog()
    await detail.retryRehydrate()
    await detail.loadEarlierEvents()
    detail.onLiveLogBootSession('s1')
    detail.syncPriorityDraft()
    detail.showPrioritySaved()
    detail.openPriorityPopover()
    detail.onPriorityKeydown(new KeyboardEvent('keydown', { key: 'Escape' }))
    await detail.savePriority()
    detail.measureWorkspace()
    detail.applyOuterLayout()
    detail.persistOuterLayout()
    detail.applyDetailArtifactsDeepLink()
    detail.showMobileTimelinePanel()

    detail.togglePriorityPopover()
    await nextTick()
    detail.closePriorityPopover(true)

    document.dispatchEvent(new Event('visibilitychange'))
    window.dispatchEvent(new Event('focus'))
    window.dispatchEvent(new Event('resize'))

    app.unmount()
    await flushPromises()
  })

  it('normalizes mobile view mode and timeline selection', async () => {
    isMobile.value = true
    const { detail, app } = await withRunDetail()
    await flushPromises()
    await nextTick()

    expect(detail.viewMode.value).toBe('timeline')
    detail.showMobileDetailPanel()
    detail.backToMobileTimeline()
    expect(detail.mobileMainPanel.value).toBe('timeline')

    app.unmount()
  })

  it('keeps unknown model alias stable during soft refresh while getProject is pending', async () => {
    let resolveProject: ((value: { id: string; unknownModelDisplayName: string }) => void) | null = null
    mocks.getProject.mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveProject = resolve
        }),
    )

    const { detail, app } = await withRunDetail()
    await flushPromises()
    await nextTick()

    // First load: alias resolves to Auto.
    resolveProject!({ id: 'proj-1', unknownModelDisplayName: 'Auto' })
    await flushPromises()
    expect(detail.unknownModelDisplayName.value).toBe('Auto')
    expect(mocks.getProject).toHaveBeenCalledTimes(1)

    // Soft refresh while getProject would be slow: alias must not clear.
    mocks.getProject.mockClear()
    mocks.getProject.mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveProject = resolve
        }),
    )
    await detail.loadRun(false)
    expect(detail.unknownModelDisplayName.value).toBe('Auto')
    expect(mocks.getProject).not.toHaveBeenCalled()

    resolveProject!({ id: 'proj-1', unknownModelDisplayName: 'Auto' })
    await flushPromises()
    expect(detail.unknownModelDisplayName.value).toBe('Auto')
    expect(mocks.getProject).not.toHaveBeenCalled()

    app.unmount()
  })

  it('reloads unknown model alias when projectId changes on hard refresh', async () => {
    mocks.getProject
      .mockResolvedValueOnce({ id: 'proj-1', unknownModelDisplayName: 'Auto' })
      .mockResolvedValueOnce({ id: 'proj-2', unknownModelDisplayName: 'gpt-5' })

    const { detail, app, router } = await withRunDetail()
    await flushPromises()
    expect(detail.unknownModelDisplayName.value).toBe('Auto')

    mocks.getRun.mockResolvedValue({
      ...sampleRun(),
      id: 'run-2',
      workflowId: 'wf-2',
    })
    mocks.getWorkflow.mockResolvedValue({
      ...sampleWorkflow(),
      id: 'wf-2',
      projectId: 'proj-2',
    })

    await router.push('/runs/run-2')
    await flushPromises()
    await nextTick()

    expect(detail.unknownModelDisplayName.value).toBe('gpt-5')
    expect(mocks.getProject).toHaveBeenLastCalledWith('proj-2')

    app.unmount()
  })

  it('retains last successful alias when getProject fails after project switch on hard refresh', async () => {
    mocks.getProject.mockResolvedValueOnce({ id: 'proj-1', unknownModelDisplayName: 'Auto' })

    const { detail, app, router } = await withRunDetail()
    await flushPromises()
    expect(detail.unknownModelDisplayName.value).toBe('Auto')

    mocks.getRun.mockResolvedValue({
      ...sampleRun(),
      id: 'run-2',
      workflowId: 'wf-2',
    })
    mocks.getWorkflow.mockResolvedValue({
      ...sampleWorkflow(),
      id: 'wf-2',
      projectId: 'proj-2',
    })
    mocks.getProject.mockRejectedValueOnce(new Error('network down'))

    await router.push('/runs/run-2')
    await flushPromises()
    await nextTick()

    // Hard refresh to new project failed to load alias → empty until success.
    expect(detail.unknownModelDisplayName.value).toBe('')

    mocks.getProject.mockResolvedValueOnce({ id: 'proj-2', unknownModelDisplayName: 'gpt-5' })
    await detail.loadRun(false)
    await flushPromises()
    expect(detail.unknownModelDisplayName.value).toBe('gpt-5')

    mocks.getProject.mockRejectedValueOnce(new Error('network down'))
    await detail.loadRun(false)
    await flushPromises()
    expect(detail.unknownModelDisplayName.value).toBe('gpt-5')

    app.unmount()
  })

  it('soft refresh writes persisted Agent turns when chrome is unchanged (g1.2 / g2.2)', async () => {
    const parked = (
      turns: { role: 'human' | 'agent'; text: string; thought?: string; streaming?: boolean }[],
      busy = false,
    ): Run => {
      const slot = {
        nodeId: 'n1',
        done: false,
        turns: turns.map((t) => ({ at: 't', thought: '', ...t })),
      }
      return {
        ...sampleRun(),
        status: 'waiting_human',
        progress: 0.4,
        nodeRuns: {
          n1: { nodeId: 'n1', status: 'waiting_human', outputs: {}, events: [], mcpCalls: [] },
          n2: { nodeId: 'n2', status: 'pending', outputs: {} },
        },
        reactSessions: { n1: { busy, waiting: 0, items: [] } },
        clarify: slot,
        clarifyByNode: { n1: { ...slot, turns: slot.turns.map((t) => ({ ...t })) } },
      } as unknown as Run
    }

    mocks.getRun.mockResolvedValue(parked([]))
    const { detail, app } = await withRunDetail()
    await flushPromises()
    expect(detail.run.value.clarify?.turns ?? []).toHaveLength(0)
    expect(detail.run.value.clarifyByNode?.n1?.turns ?? []).toHaveLength(0)

    mocks.getRun.mockResolvedValue(
      parked([
        { role: 'human', text: '开始' },
        { role: 'agent', text: '已落盘回复', thought: '先想清楚' },
      ]),
    )
    await detail.loadRun(false, true)
    await flushPromises()
    expect(detail.run.value.status).toBe('waiting_human')
    expect(detail.run.value.clarify?.turns.map((t) => t.text)).toEqual(['开始', '已落盘回复'])
    expect(detail.run.value.clarify?.turns[1]?.thought).toBe('先想清楚')
    expect(detail.run.value.clarifyByNode?.n1?.turns.map((t) => t.text)).toEqual(['开始', '已落盘回复'])

    const live = {
      nodeId: 'n1',
      done: false,
      turns: [{ role: 'agent' as const, text: 'live bubble', thought: '思考中', at: 't', streaming: true }],
    }
    detail.run.value = {
      ...detail.run.value,
      reactSessions: { n1: { busy: true, waiting: 0, items: [] } },
      clarify: live,
      clarifyByNode: { n1: { ...live, turns: live.turns.map((t) => ({ ...t })) } },
    }
    mocks.getRun.mockResolvedValue(parked([]))
    await detail.fetchRunData()
    expect(detail.run.value.clarify?.turns[0]?.text).toBe('live bubble')
    expect(detail.run.value.clarify?.turns[0]?.streaming).toBe(true)
    expect(detail.run.value.clarifyByNode?.n1?.turns[0]?.text).toBe('live bubble')

    await detail.patchRunChrome()
    expect(detail.run.value.clarify?.turns[0]?.text).toBe('live bubble')
    expect(detail.run.value.clarifyByNode?.n1?.turns[0]?.thought).toBe('思考中')

    app.unmount()
  })

  it('patchRunChrome adopts a longer idle transcript over an empty busy panel (g1.3)', async () => {
    const parked = (
      turns: { role: 'human' | 'agent'; text: string; thought?: string }[],
      busy: boolean,
    ): Run => {
      const slot = {
        nodeId: 'n1',
        done: false,
        turns: turns.map((t) => ({ at: 't', thought: '', ...t })),
      }
      return {
        ...sampleRun(),
        status: 'waiting_human',
        progress: 0.4,
        nodeRuns: {
          n1: { nodeId: 'n1', status: 'waiting_human', outputs: {}, events: [], mcpCalls: [] },
          n2: { nodeId: 'n2', status: 'pending', outputs: {} },
        },
        reactSessions: { n1: { busy, waiting: 0, items: [] } },
        clarify: slot,
        clarifyByNode: { n1: { ...slot, turns: slot.turns.map((t) => ({ ...t })) } },
      } as unknown as Run
    }

    mocks.getRun.mockResolvedValue(parked([], false))
    const { detail, app } = await withRunDetail()
    await flushPromises()
    detail.run.value = {
      ...detail.run.value,
      reactSessions: { n1: { busy: true, waiting: 0, items: [] } },
    }
    mocks.getRun.mockResolvedValue(
      parked(
        [
          { role: 'human', text: '开始' },
          { role: 'agent', text: '已落盘回复', thought: '先想清楚' },
        ],
        false,
      ),
    )
    await detail.patchRunChrome()
    expect(detail.run.value.clarify?.turns.map((t) => t.text)).toEqual(['开始', '已落盘回复'])
    expect(detail.run.value.clarifyByNode?.n1?.turns[1]?.thought).toBe('先想清楚')
    expect(detail.run.value.reactSessions?.n1?.busy).toBe(false)
    app.unmount()
  })
})
