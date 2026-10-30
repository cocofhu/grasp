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
  getWorkspace: vi.fn(),
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
      getWorkspace: mocks.getWorkspace,
      runArtifacts: mocks.runArtifacts,
      nodeEvents: mocks.nodeEvents,
      nodeSandboxLog: mocks.nodeSandboxLog,
      getRunNodeSandbox: mocks.getRunNodeSandbox,
      updateRunPriority: mocks.updateRunPriority,
    },
  }
})

import { useRunDetail } from './useRunDetail'
import { REVIEW_RECOVERY_TIMEOUT_MS } from '@/lib/chat/reviewSessionState'

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
  name: '夜间回归',
  description: '',
  status: 'published',
  version: 1,
  updatedAt: '',
  needsRepo: false,
  nodes: sampleNodes,
  edges: [{ id: 'e1', source: 'n1', target: 'n2' }],
})

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

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
    mocks.getWorkspace.mockReset()
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
    mocks.getWorkspace.mockResolvedValue({ unknownModelDisplayName: '未知模型' })
    mocks.runArtifacts.mockResolvedValue([])
    mocks.nodeEvents.mockResolvedValue({ events: [], nextCursor: '', hasMore: false })
    mocks.nodeSandboxLog.mockResolvedValue({ content: 'boot log', live: false, found: true })
    mocks.getRunNodeSandbox.mockResolvedValue({ id: 'sbx-1' })
    mocks.updateRunPriority.mockResolvedValue({ priority: 'high' })
  })

  afterEach(() => {
    vi.useRealTimers()
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

  it.each([true, false])('shows a retryable initial-load error after recovery deadline (read honors abort: %s)', async honorsAbort => {
    vi.useFakeTimers()
    const pending = deferred<Run>()
    mocks.getRun.mockImplementationOnce((_id: string, options?: { signal?: AbortSignal }) => {
      if (honorsAbort) options?.signal?.addEventListener('abort', () => pending.reject(options.signal?.reason), { once: true })
      return pending.promise
    })
    const { detail, app } = await withRunDetail()
    await flushPromises()
    expect(detail.runLoading.value).toBe(true)
    await vi.advanceTimersByTimeAsync(REVIEW_RECOVERY_TIMEOUT_MS + 1)
    await flushPromises()
    expect(detail.runLoading.value).toBe(false)
    expect(detail.loadError.value).toBe(true)
    expect(detail.loadErrorKind.value).toBe('network_or_server')

    pending.resolve({ ...sampleRun(), workflowName: 'expired read' })
    await flushPromises()
    expect(detail.run.value.workflowName).not.toBe('expired read')
    expect(detail.loadError.value).toBe(true)

    mocks.getRun.mockResolvedValueOnce({ ...sampleRun(), workflowName: 'recovered run' })
    await detail.loadRun(true)
    expect(detail.loadError.value).toBe(false)
    expect(detail.run.value.workflowName).toBe('recovered run')
    app.unmount()
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

  it('applies one-shot view=canvas on desktop and clears the query', async () => {
    const { detail, app, router } = await withRunDetail('/runs/run-1?view=canvas&wf=nightly')
    await flushPromises()
    await nextTick()

    expect(detail.viewMode.value).toBe('canvas')
    expect(router.currentRoute.value.path).toBe('/runs/run-1')
    expect(router.currentRoute.value.query).toEqual({ wf: 'nightly' })

    for (const mode of ['timeline', 'stats', 'llm'] as const) {
      detail.viewMode.value = mode
      await router.push({ path: '/runs/run-1', query: { view: 'canvas', wf: 'nightly' } })
      await flushPromises()
      expect(detail.viewMode.value).toBe('canvas')
      expect(router.currentRoute.value.query).toEqual({ wf: 'nightly' })
    }

    await router.push({ path: '/runs/run-1', query: { view: 'canvas' } })
    await flushPromises()
    expect(detail.viewMode.value).toBe('canvas')
    expect(router.currentRoute.value.query.view).toBeUndefined()
    expect(router.currentRoute.value.path).toBe('/runs/run-1')

    app.unmount()
  })

  it('keeps the timeline when view=canvas arrives on a narrow screen', async () => {
    isMobile.value = true
    const { detail, app, router } = await withRunDetail('/runs/run-1?view=canvas')
    await flushPromises()
    await nextTick()

    expect(detail.viewMode.value).toBe('timeline')
    expect(router.currentRoute.value.path).toBe('/runs/run-1')
    expect(router.currentRoute.value.query.view).toBeUndefined()

    await router.push({ path: '/runs/run-1', query: { view: 'canvas' } })
    await flushPromises()
    expect(detail.viewMode.value).toBe('timeline')
    expect(router.currentRoute.value.query.view).toBeUndefined()

    app.unmount()
  })

  it('keeps unknown model alias stable during soft refresh while getWorkspace is pending', async () => {
    let resolveWorkspace: ((value: { unknownModelDisplayName: string }) => void) | null = null
    mocks.getWorkspace.mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveWorkspace = resolve
        }),
    )

    const { detail, app } = await withRunDetail()
    await flushPromises()
    await nextTick()

    // First load: alias resolves to Auto.
    resolveWorkspace!({ unknownModelDisplayName: 'Auto' })
    await flushPromises()
    expect(detail.unknownModelDisplayName.value).toBe('Auto')
    expect(mocks.getWorkspace).toHaveBeenCalledTimes(1)

    // Soft refresh while getWorkspace would be slow: alias must not clear.
    mocks.getWorkspace.mockClear()
    mocks.getWorkspace.mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveWorkspace = resolve
        }),
    )
    await detail.loadRun(false)
    expect(detail.unknownModelDisplayName.value).toBe('Auto')
    expect(mocks.getWorkspace).not.toHaveBeenCalled()

    resolveWorkspace!({ unknownModelDisplayName: 'Auto' })
    await flushPromises()
    expect(detail.unknownModelDisplayName.value).toBe('Auto')
    expect(mocks.getWorkspace).not.toHaveBeenCalled()

    app.unmount()
  })

  it('reloads unknown model alias when navigating to another run', async () => {
    mocks.getWorkspace
      .mockResolvedValueOnce({ unknownModelDisplayName: 'Auto' })
      .mockResolvedValueOnce({ unknownModelDisplayName: 'gpt-5' })

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
    })

    await router.push('/runs/run-2')
    await flushPromises()
    await nextTick()

    expect(detail.unknownModelDisplayName.value).toBe('gpt-5')
    expect(mocks.getWorkspace).toHaveBeenLastCalledWith()

    app.unmount()
  })

  it('retains last successful alias when getWorkspace fails after navigating to another run', async () => {
    mocks.getWorkspace.mockResolvedValueOnce({ unknownModelDisplayName: 'Auto' })

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
    })
    mocks.getWorkspace.mockRejectedValueOnce(new Error('network down'))

    await router.push('/runs/run-2')
    await flushPromises()
    await nextTick()

    // Hard refresh to another run failed to load alias → empty until success.
    expect(detail.unknownModelDisplayName.value).toBe('')

    mocks.getWorkspace.mockResolvedValueOnce({ unknownModelDisplayName: 'gpt-5' })
    await detail.loadRun(false)
    await flushPromises()
    expect(detail.unknownModelDisplayName.value).toBe('gpt-5')

    mocks.getWorkspace.mockRejectedValueOnce(new Error('network down'))
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

  it('busy-safe REST polling releases a retired FIFO even when transcript and local busy stay unchanged', async () => {
    const clarify = { nodeId: 'n1', iteration: 1, done: false, turns: [
      { role: 'human' as const, text: '我的选择', thought: '', at: 't' },
      { role: 'agent' as const, text: '已记录', thought: '', at: 't' },
    ] }
    const busy = { ...sampleRun(), status: 'waiting_human', clarify, clarifyByNode: { n1: clarify },
      reviewSessionGeneration: 5,
      reactSessions: { n1: { sessionGeneration: 5, revision: 2, busy: true, waiting: 1,
        activeItem: { id: 'choice', text: '我的选择' }, items: [{ id: 'correction', text: '仅修改前端' }] } },
    } as Run
    mocks.getRun.mockResolvedValue(busy)
    const { detail, app } = await withRunDetail()
    await flushPromises()
    detail.selected.value = 'n1'
    const applyReviewFrame = vi.fn()
    detail.reviewChatRef.value = { applyReviewFrame, isSessionBusy: () => true }
    await detail.wsApi.projectDialogueAfterLoad(busy)
    expect(detail.isClarifySessionBusy()).toBe(true)
    applyReviewFrame.mockClear()

    // The server drops idle FIFOs from its registry; the rest of the run and
    // saved transcript need not change when the final queue frame was missed.
    mocks.getRun.mockResolvedValue({ ...busy, reactSessions: undefined })
    await detail.patchRunChrome()
    expect(detail.run.value.reactSessions?.n1?.busy).toBe(false)
    expect(detail.run.value.reactSessions?.n1?.waiting).toBe(0)
    expect(detail.live.liveBusy.n1).toBe(false)
    expect(applyReviewFrame).toHaveBeenCalledWith(expect.objectContaining({
      event: 'queue_state', nodeId: 'n1', busy: false, waiting: 0, items: [],
    }))
    expect(applyReviewFrame).not.toHaveBeenCalledWith(expect.objectContaining({ event: 'session_reset' }))
    expect(detail.run.value.clarify?.turns).toEqual(clarify.turns)
    app.unmount()
  })

  it('coalesces overlapping refreshes into the latest trailing request and loading owner', async () => {
    const { detail, app } = await withRunDetail()
    await flushPromises()
    mocks.getRun.mockClear()
    const older = deferred<Run>()
    const newest = deferred<Run>()
    mocks.getRun
      .mockImplementationOnce(() => older.promise)
      .mockImplementationOnce(() => newest.promise)

    const oldLoad = detail.loadRun(false)
    const replacedLoad = detail.loadRun(false)
    const newestLoad = detail.loadRun(false)
    expect(mocks.getRun).toHaveBeenCalledTimes(1)
    expect(detail.refreshing.value).toBe(true)

    older.resolve({ ...sampleRun(), workflowName: '晚到的旧快照', progress: 0.1 })
    await flushPromises()
    expect(mocks.getRun).toHaveBeenCalledTimes(2)
    expect(detail.run.value.workflowName).not.toBe('晚到的旧快照')
    expect(detail.refreshing.value).toBe(true)

    newest.resolve({ ...sampleRun(), workflowName: '最终快照', progress: 0.9 })
    await Promise.all([oldLoad, replacedLoad, newestLoad])
    expect(detail.run.value.workflowName).toBe('最终快照')
    expect(detail.run.value.progress).toBe(0.9)
    expect(detail.refreshing.value).toBe(false)
    app.unmount()
  })

  it('does not project old reactSessions after a newer load starts', async () => {
    const { detail, app } = await withRunDetail()
    await flushPromises()
    mocks.getRun.mockClear()
    const applyReviewFrame = vi.fn()
    detail.reviewChatRef.value = { applyReviewFrame }
    detail.selected.value = 'n1'
    const clarify = { nodeId: 'n1', done: false, turns: [] }
    const first = deferred<Run>()
    const second = deferred<Run>()
    mocks.getRun.mockImplementationOnce(() => first.promise).mockImplementationOnce(() => second.promise)

    const oldLoad = detail.loadRun(false)
    first.resolve({
      ...sampleRun(),
      progress: 0.4,
      clarify,
      clarifyByNode: { n1: clarify },
      reactSessions: { n1: { busy: true, waiting: 0, items: [] } },
    } as Run)
    // 让旧请求进入 projectDialogueAfterLoad 的 nextTick 等待窗口。
    await Promise.resolve()
    await Promise.resolve()
    await Promise.resolve()
    const newerLoad = detail.loadRun(false)
    second.resolve({
      ...sampleRun(), progress: 0.9, clarify, clarifyByNode: { n1: clarify }, reactSessions: {},
    } as Run)
    await Promise.all([oldLoad, newerLoad])
    await flushPromises()
    expect(applyReviewFrame.mock.calls.some(([frame]) => frame?.busy === true)).toBe(false)
    app.unmount()
  })

  it('ignores an old route response and its finally after navigation', async () => {
    const { detail, app, router } = await withRunDetail()
    await flushPromises()
    mocks.getRun.mockClear()
    const oldRoute = deferred<Run>()
    const newRoute = deferred<Run>()
    mocks.getRun
      .mockImplementationOnce(() => oldRoute.promise)
      .mockImplementationOnce(() => newRoute.promise)

    const oldLoad = detail.loadRun(false)
    await router.push('/runs/run-2')
    await nextTick()
    expect(detail.runLoading.value).toBe(true)
    oldRoute.resolve({ ...sampleRun(), id: 'run-1', workflowName: '旧路由响应' })
    await oldLoad
    expect(detail.runLoading.value).toBe(true)
    expect(detail.run.value.id).toBe('run-2')

    newRoute.resolve({ ...sampleRun(), id: 'run-2', workflowId: 'wf-2', workflowName: '新路由快照' })
    await flushPromises()
    expect(detail.run.value.id).toBe('run-2')
    expect(detail.run.value.workflowName).toBe('新路由快照')
    expect(detail.runLoading.value).toBe(false)
    app.unmount()
  })

  it('does not apply old runArtifacts after the route changes', async () => {
    const { detail, app, router } = await withRunDetail()
    await flushPromises()
    const arts = deferred<{ id: string; name: string; nodeId: string }[]>()
    mocks.runArtifacts.mockImplementationOnce(() => arts.promise)
    const refresh = detail.refreshArtifactPreviewState()
    mocks.getRun.mockResolvedValueOnce({ ...sampleRun(), id: 'run-2', workflowName: '新路由' })
    await router.push('/runs/run-2')
    await flushPromises()
    expect(detail.run.value.id).toBe('run-2')

    arts.resolve([{ id: 'old-art', name: 'old.txt', nodeId: 'n1' }])
    await refresh
    await flushPromises()
    expect(detail.run.value.artifacts?.some((artifact) => artifact.id === 'old-art')).toBe(false)
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
