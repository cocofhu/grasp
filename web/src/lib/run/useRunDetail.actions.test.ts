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
  resumeGate: vi.fn(),
  reactReply: vi.fn(),
  reactCancel: vi.fn(),
  reactQueueRemove: vi.fn(),
  reactQueueReorder: vi.fn(),
  cancelRun: vi.fn(),
  deleteRun: vi.fn(),
  resumeRun: vi.fn(),
  exportRunLogsUrl: vi.fn(() => 'http://localhost/api/runs/run-1/logs/export'),
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
      resumeGate: mocks.resumeGate,
      reactReply: mocks.reactReply,
      reactCancel: mocks.reactCancel,
      reactQueueRemove: mocks.reactQueueRemove,
      reactQueueReorder: mocks.reactQueueReorder,
      cancelRun: mocks.cancelRun,
      deleteRun: mocks.deleteRun,
      resumeRun: mocks.resumeRun,
      exportRunLogsUrl: mocks.exportRunLogsUrl,
    },
  }
})

import { useRunDetail } from './useRunDetail'

function stubNode(partial: Partial<WFNode> & Pick<WFNode, 'id' | 'type'>): WFNode {
  return { label: partial.id, position: { x: 0, y: 0 }, config: {}, ...partial }
}

const sampleNodes = [
  stubNode({ id: 'n1', type: 'react' }),
  stubNode({ id: 'n2', type: 'app_preview' }),
  stubNode({ id: 'n3', type: 'output' }),
]

const sampleRun = (over: Partial<Run> = {}): Run =>
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
      n1: { nodeId: 'n1', status: 'waiting_human', outputs: {}, events: [], mcpCalls: [] },
      n2: { nodeId: 'n2', status: 'completed', outputs: {} },
      n3: { nodeId: 'n3', status: 'running', outputs: {} },
    },
    artifacts: [{ id: 'art-1', name: 'out.txt', nodeId: 'n3' }],
    trace: [],
    vars: [{ name: 'branch', value: 'main' }],
    nodes: sampleNodes,
    edges: [
      { id: 'e1', source: 'n1', target: 'n2' },
      { id: 'e2', source: 'n2', target: 'n3' },
    ],
    reactSessions: {},
    clarifyByNode: {
      n1: { nodeId: 'n1', turns: [], done: false },
      n2: { nodeId: 'n2', turns: [], done: false },
    },
    gate: { nodeId: 'n1', kind: 'approve', fields: [] },
    ...over,
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
  edges: [
    { id: 'e1', source: 'n1', target: 'n2' },
    { id: 'e2', source: 'n2', target: 'n3' },
  ],
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

/** Element whose measured box is fixed, so layout math is deterministic. */
function boxEl(rect: Partial<DOMRect>): HTMLElement {
  const el = document.createElement('div')
  el.getBoundingClientRect = () =>
    ({ x: 0, y: 0, top: 0, left: 0, right: 0, bottom: 0, width: 0, height: 0, ...rect }) as DOMRect
  return el
}

/** Minimal PointerEvent stand-in: happy-dom lacks pointer capture on elements. */
function pointerEvt(clientX: number) {
  const target = document.createElement('div')
  ;(target as unknown as { setPointerCapture: (id: number) => void }).setPointerCapture = vi.fn()
  return {
    clientX,
    pointerId: 1,
    currentTarget: target,
    stopPropagation: vi.fn(),
    preventDefault: vi.fn(),
  } as unknown as PointerEvent
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
      { path: '/runs', component: { template: '<div />' } },
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
  await flushPromises()
  await nextTick()
  return { detail, app, router }
}

describe('useRunDetail actions', () => {
  beforeEach(() => {
    vi.stubGlobal('WebSocket', MockWebSocket)
    vi.stubGlobal('ResizeObserver', MockResizeObserver)
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
      cb(0)
      return 1
    })
    isMobile.value = false
    for (const fn of Object.values(mocks)) (fn as ReturnType<typeof vi.fn>).mockReset()

    mocks.getRun.mockResolvedValue(sampleRun())
    mocks.getWorkflow.mockResolvedValue(sampleWorkflow())
    mocks.getProject.mockResolvedValue({ id: 'proj-1', unknownModelDisplayName: '未知模型' })
    mocks.runArtifacts.mockResolvedValue([])
    mocks.nodeEvents.mockResolvedValue({ events: [], nextCursor: '', hasMore: false })
    mocks.nodeSandboxLog.mockResolvedValue({ content: 'boot log', live: false, found: true })
    mocks.getRunNodeSandbox.mockResolvedValue({ id: 'sbx-1' })
    mocks.updateRunPriority.mockResolvedValue({ priority: 'high' })
    mocks.resumeGate.mockResolvedValue({ status: 'ok' })
    mocks.reactReply.mockResolvedValue({ status: 'queued' })
    mocks.reactCancel.mockResolvedValue({ status: 'ok' })
    mocks.reactQueueRemove.mockResolvedValue({ status: 'ok' })
    mocks.reactQueueReorder.mockResolvedValue({ status: 'ok' })
    mocks.cancelRun.mockResolvedValue({ status: 'cancelled' })
    mocks.deleteRun.mockResolvedValue({ status: 'deleted' })
    mocks.resumeRun.mockResolvedValue({ status: 'running' })
    mocks.exportRunLogsUrl.mockReturnValue('http://localhost/api/runs/run-1/logs/export')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('resolves a gate and surfaces backend rejection on the gate banner', async () => {
    const { detail, app } = await withRunDetail()

    await detail.onGateResolve('pass', { note: 'ok' })
    expect(mocks.resumeGate).toHaveBeenCalledWith('run-1', 'n1', 'pass', { note: 'ok' })
    expect(mocks.toastSuccess).toHaveBeenCalled()
    expect(detail.gateError.value).toBeNull()
    expect(detail.gateSubmitting.value).toBe(false)

    mocks.resumeGate.mockRejectedValueOnce(new Error('field required'))
    await detail.onGateResolve('reject')
    expect(detail.gateError.value).toBe('field required')

    // Reject path still reports through the reject toast copy.
    mocks.resumeGate.mockResolvedValueOnce({ status: 'ok' })
    await detail.onGateResolve('reject')
    expect(detail.gateError.value).toBeNull()

    // Missing gate is a no-op.
    detail.run.value = { ...detail.run.value, gate: undefined } as Run
    mocks.resumeGate.mockClear()
    await detail.onGateResolve('pass')
    expect(mocks.resumeGate).not.toHaveBeenCalled()

    app.unmount()
  })

  it('sends, cancels and re-orders the clarify queue', async () => {
    const { detail, app } = await withRunDetail()
    detail.selectNode('n1')
    await nextTick()

    await detail.onClarifySend('hello')
    expect(mocks.reactReply).toHaveBeenCalledWith('run-1', 'n1', 'hello', [], false, [], false, false)

    // Failure rolls back the optimistic queued row and shows the error.
    const discardLastQueued = vi.fn()
    detail.reviewChatRef.value = { discardLastQueued }
    mocks.reactReply.mockRejectedValueOnce(new Error('dialogue already done'))
    await detail.onClarifySend('again')
    expect(discardLastQueued).toHaveBeenCalled()
    expect(detail.clarifyConfirmError.value).toBe('dialogue already done')

    // Force finish refreshes the snapshot instead of rolling back.
    mocks.getRun.mockClear()
    await detail.onClarifySend('done', [], [], true)
    expect(mocks.reactReply).toHaveBeenLastCalledWith('run-1', 'n1', 'done', [], true, [], false, false)
    expect(mocks.getRun).toHaveBeenCalled()

    await detail.onClarifyCancel()
    expect(mocks.reactCancel).toHaveBeenCalledWith('run-1', 'n1')
    mocks.reactCancel.mockRejectedValueOnce(new Error('nope'))
    await detail.onClarifyCancel()

    await detail.onClarifyQueueRemove('')
    expect(mocks.reactQueueRemove).not.toHaveBeenCalled()
    await detail.onClarifyQueueRemove('item-1')
    expect(mocks.reactQueueRemove).toHaveBeenCalledWith('run-1', 'n1', 'item-1')
    mocks.reactQueueRemove.mockRejectedValueOnce(new Error('gone'))
    await detail.onClarifyQueueRemove('item-1')

    await detail.onClarifyQueueReorder([])
    expect(mocks.reactQueueReorder).not.toHaveBeenCalled()
    await detail.onClarifyQueueReorder(['a', 'b'])
    expect(mocks.reactQueueReorder).toHaveBeenCalledWith('run-1', 'n1', ['a', 'b'])
    mocks.reactQueueReorder.mockRejectedValueOnce(new Error('stale'))
    await detail.onClarifyQueueReorder(['a'])

    // Finish uses the clarify prompt for a react node.
    mocks.reactReply.mockClear()
    detail.onClarifyFinish()
    await flushPromises()
    expect(mocks.reactReply).toHaveBeenCalled()
    expect(mocks.reactReply.mock.calls[0]![4]).toBe(true)

    // A finished dialogue accepts nothing further.
    detail.run.value = {
      ...detail.run.value,
      clarifyByNode: { n1: { nodeId: 'n1', turns: [], done: true } },
    } as Run
    await nextTick()
    mocks.reactReply.mockClear()
    mocks.reactCancel.mockClear()
    await detail.onClarifySend('after done')
    await detail.onClarifyCancel()
    await detail.onClarifyQueueRemove('x')
    await detail.onClarifyQueueReorder(['x'])
    expect(mocks.reactReply).not.toHaveBeenCalled()
    expect(mocks.reactCancel).not.toHaveBeenCalled()

    app.unmount()
  })

  it('stages app-preview picks and merges them into the next review reply', async () => {
    const { detail, app } = await withRunDetail()
    detail.selectNode('n2')
    await nextTick()
    expect(detail.selection.hasAppPreview.value).toBe(true)

    detail.onAppPreviewStagedPick({ selector: '#btn', url: 'http://app/', tagName: 'BUTTON' })
    const merged = detail.mergeStagedAppPreviewPick([])
    expect(merged).toHaveLength(1)
    expect(merged[0]!.selector).toBe('#btn')
    // Already present → returned untouched.
    expect(detail.mergeStagedAppPreviewPick(merged)).toBe(merged)

    // Sending without force pulls the staged pick in and then clears it.
    await detail.onClarifySend('look here')
    expect(mocks.reactReply.mock.calls[0]![5]).toHaveLength(1)
    expect(detail.lastStagedAppPreviewPick.value).toBeNull()

    detail.onAppPreviewStagedPick({ selector: '#btn2', url: 'http://app/', tagName: 'A' })
    detail.onAppPreviewReviewPick({ selector: '#btn2', url: 'http://app/', tagName: 'A' })
    expect(detail.lastStagedAppPreviewPick.value).toBeNull()
    // Second identical pick dedupes and warns.
    detail.onAppPreviewReviewPick({ selector: '#btn2', url: 'http://app/', tagName: 'A' })
    expect(mocks.toastWarn).toHaveBeenCalled()

    // No selected node → ignored.
    detail.selected.value = null
    await nextTick()
    detail.onAppPreviewReviewPick({ selector: '#x', url: '', tagName: 'DIV' })

    app.unmount()
  })

  it('maps and performs run cancellation', async () => {
    const { detail, app } = await withRunDetail()

    expect(detail.canCancelRun.value).toBe(true)
    expect(detail.mapCancelRunError({ status: 404 })).toBeTruthy()
    expect(detail.mapCancelRunError(new Error('not found'))).toBeTruthy()
    expect(detail.mapCancelRunError({ status: 400 })).toBeTruthy()
    expect(detail.mapCancelRunError(new Error('already finished'))).toBeTruthy()
    expect(detail.mapCancelRunError(new Error('boom'))).toBe('boom')
    expect(detail.mapCancelRunError(null)).toBeTruthy()

    detail.openCancelConfirm()
    expect(detail.showCancelConfirm.value).toBe(true)
    await detail.confirmCancelRun()
    expect(mocks.cancelRun).toHaveBeenCalledWith('run-1')
    expect(detail.showCancelConfirm.value).toBe(false)

    mocks.cancelRun.mockRejectedValueOnce(new Error('already finished'))
    detail.openCancelConfirm()
    await detail.confirmCancelRun()
    expect(detail.cancelRunError.value).toBeTruthy()

    // Terminal run: no cancel affordance at all.
    detail.run.value = { ...detail.run.value, status: 'completed' } as Run
    await nextTick()
    expect(detail.canCancelRun.value).toBe(false)
    mocks.cancelRun.mockClear()
    detail.openCancelConfirm()
    await detail.confirmCancelRun()
    expect(mocks.cancelRun).not.toHaveBeenCalled()

    app.unmount()
  })

  it('maps and performs run deletion, then routes back to the run list', async () => {
    const { detail, app, router } = await withRunDetail()

    expect(detail.canDeleteRun.value).toBe(false)
    expect(detail.deleteRunHint.value).toBeTruthy()
    detail.openDeleteConfirm()
    expect(detail.showDeleteConfirm.value).toBe(false)

    detail.run.value = { ...detail.run.value, status: 'failed' } as Run
    await nextTick()
    expect(detail.canDeleteRun.value).toBe(true)
    expect(detail.deleteRunHint.value).toBe('')

    expect(detail.mapDeleteRunError({ status: 404 })).toBeTruthy()
    expect(detail.mapDeleteRunError(new Error('cannot delete run'))).toBeTruthy()
    expect(detail.mapDeleteRunError({ status: 409 })).toBeTruthy()
    expect(detail.mapDeleteRunError(new Error('kaboom'))).toBe('kaboom')
    expect(detail.mapDeleteRunError(undefined)).toBeTruthy()

    detail.openDeleteConfirm()
    expect(detail.showDeleteConfirm.value).toBe(true)
    detail.closeDeleteConfirm()
    expect(detail.showDeleteConfirm.value).toBe(false)

    detail.openDeleteConfirm()
    await detail.confirmDeleteRun()
    await flushPromises()
    expect(mocks.deleteRun).toHaveBeenCalledWith('run-1')
    expect(router.currentRoute.value.fullPath).toContain('/runs')

    mocks.deleteRun.mockRejectedValueOnce(new Error('nope'))
    await detail.confirmDeleteRun()
    expect(detail.deleteRunError.value).toBe('nope')

    app.unmount()
  })

  it('exports logs through an anchor click', async () => {
    const { detail, app } = await withRunDetail()
    const click = vi.fn()
    const create = vi.spyOn(document, 'createElement').mockImplementation(
      () => ({ click, set href(_v: string) {}, set download(_v: string) {} }) as unknown as HTMLAnchorElement,
    )
    detail.exportLogs()
    expect(click).toHaveBeenCalled()
    expect(mocks.exportRunLogsUrl).toHaveBeenCalledWith('run-1')
    create.mockRestore()
    app.unmount()
  })

  it('resumes a terminated run and reports resume failures', async () => {
    const { detail, app } = await withRunDetail()
    detail.run.value = { ...detail.run.value, status: 'failed' } as Run
    await nextTick()
    expect(detail.canResume.value).toBe(true)

    await detail.onResume('n1')
    expect(mocks.resumeRun).toHaveBeenCalledWith('run-1', 'n1')
    expect(detail.resumeError.value).toBeNull()
    expect(detail.resuming.value).toBe(false)

    mocks.resumeRun.mockRejectedValueOnce(new Error('sandbox gone'))
    await detail.onResume()
    expect(detail.resumeError.value).toBe('sandbox gone')

    detail.selectNode('n1')
    detail.run.value = { ...detail.run.value, status: 'cancelled' } as Run
    await nextTick()
    expect(detail.canResumeSelected.value).toBe(true)

    detail.run.value = { ...detail.run.value, status: 'running' } as Run
    await nextTick()
    expect(detail.canResumeSelected.value).toBe(false)
    detail.run.value = {
      ...detail.run.value,
      nodeRuns: { ...detail.run.value.nodeRuns, n1: { nodeId: 'n1', status: 'failed', outputs: {} } },
    } as Run
    await nextTick()
    expect(detail.canResumeSelected.value).toBe(true)

    detail.selected.value = null
    await nextTick()
    expect(detail.canResumeSelected.value).toBe(false)

    app.unmount()
  })

  it('derives priority presentation and saves the draft', async () => {
    const { detail, app } = await withRunDetail()

    expect(detail.priorityEditable.value).toBe(true)
    expect(detail.committedPriority.value).toBe('normal')
    expect(detail.priorityChevronClass.value).toContain('accent')
    expect(detail.priorityTriggerTitle.value).toBeTruthy()
    expect(detail.priorityTriggerAria.value).toBeTruthy()
    expect(detail.priorityHint.value).toBeTruthy()

    for (const [status, priority] of [
      ['running', 'high'],
      ['waiting_human', 'low'],
      ['queued', 'normal'],
      ['completed', 'weird'],
    ] as const) {
      detail.run.value = { ...detail.run.value, status, priority } as unknown as Run
      await nextTick()
      expect(detail.priorityHint.value).toBeTruthy()
      expect(detail.priorityChevronClass.value).toBeTruthy()
      expect(detail.priorityTriggerAria.value).toBeTruthy()
    }
    expect(detail.committedPriority.value).toBe('normal')

    detail.run.value = { ...detail.run.value, status: 'queued', priority: 'normal' } as Run
    await nextTick()
    expect(detail.showPriorityChevron.value).toBe(true)

    // No change → no request.
    await detail.savePriority()
    expect(mocks.updateRunPriority).not.toHaveBeenCalled()

    detail.priorityDraft.value = 'high'
    await detail.savePriority()
    expect(mocks.updateRunPriority).toHaveBeenCalledWith('run-1', 'high')
    expect(detail.run.value.priority).toBe('high')

    detail.priorityDraft.value = 'low'
    mocks.updateRunPriority.mockRejectedValueOnce(new Error('locked'))
    await detail.savePriority()
    expect(detail.priorityError.value).toBe('locked')

    // Terminal run closes the editor and refuses saves.
    detail.run.value = { ...detail.run.value, status: 'completed' } as Run
    await nextTick()
    expect(detail.priorityPopoverOpen.value).toBe(false)
    mocks.updateRunPriority.mockClear()
    detail.priorityDraft.value = 'high'
    await detail.savePriority()
    expect(mocks.updateRunPriority).not.toHaveBeenCalled()
    detail.openPriorityPopover()
    expect(detail.priorityPopoverOpen.value).toBe(false)
    detail.togglePriorityPopover()
    expect(detail.priorityPopoverOpen.value).toBe(false)

    app.unmount()
  })

  it('places the priority popover inside the viewport', async () => {
    const { detail, app } = await withRunDetail()
    Object.defineProperty(window, 'innerWidth', { value: 800, configurable: true })
    Object.defineProperty(window, 'innerHeight', { value: 600, configurable: true })

    // Closed popover / missing anchor: nothing to place.
    detail.placePriorityPopover()
    detail.priorityBadgeRef.value = boxEl({ left: 700, right: 760, top: 500, bottom: 520 })
    detail.placePriorityPopover()

    detail.togglePriorityPopover()
    await nextTick()
    expect(detail.priorityPopoverOpen.value).toBe(true)
    // Right/bottom overflow both flip the panel back inside the viewport.
    expect(Number.parseInt(detail.priorityPopoverStyle.value.left!, 10)).toBeLessThan(700)
    expect(Number.parseInt(detail.priorityPopoverStyle.value.top!, 10)).toBeLessThan(500)

    detail.priorityBadgeRef.value = boxEl({ left: 20, right: 80, top: 10, bottom: 30 })
    detail.onPriorityReposition()
    expect(detail.priorityPopoverStyle.value.top).toBe('38px')

    detail.onPriorityKeydown(new KeyboardEvent('keydown', { key: 'a' }))
    expect(detail.priorityPopoverOpen.value).toBe(true)
    detail.onPriorityKeydown(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(detail.priorityPopoverOpen.value).toBe(false)
    // Closing twice is a no-op.
    detail.closePriorityPopover(false)
    detail.onPriorityReposition()

    app.unmount()
  })

  it('drives the desktop outer sash through drag, double-click and resize', async () => {
    const { detail, app } = await withRunDetail()
    detail.splitRootRef.value = boxEl({ width: 1400 })
    expect(detail.measureWorkspace()).toBe(1400)

    detail.applyOuterLayout()
    expect(detail.outerSashBooting.value).toBe(false)
    expect(detail.workspacePx.value).toBe(1400)
    expect(detail.reviewRightPanelStyle.value).toEqual({ width: `${detail.outerRightPx.value}px` })
    expect(detail.outerAriaMin.value).toBeGreaterThan(0)
    expect(detail.outerAriaMax.value).toBeGreaterThan(detail.outerAriaMin.value)

    detail.onOuterSashPointerDown(pointerEvt(900))
    expect(detail.outerSashDragging.value).toBe(true)
    detail.onOuterSashPointerMove(pointerEvt(400))
    expect(detail.outerRightPx.value).toBeGreaterThan(0)
    detail.onOuterSashPointerUp()
    expect(detail.outerSashDragging.value).toBe(false)
    // A second pointerup without a drag is ignored.
    detail.onOuterSashPointerUp()
    detail.onOuterSashPointerMove(pointerEvt(100))

    // Dragged past the full-open clamp → left pane collapses to zero width.
    detail.onOuterSashPointerDown(pointerEvt(900))
    detail.onOuterSashPointerMove(pointerEvt(-5000))
    detail.onOuterSashPointerUp()
    expect(detail.outerFullOpen.value).toBe(true)
    expect(detail.leftPaneStyle.value).toMatchObject({ width: '0px', flexGrow: 0 })
    detail.onOuterSashWindowResize()
    expect(detail.outerRightPx.value).toBe(detail.outerAriaMax.value)

    // A press with no movement clears the drag flag, so dblclick may reset.
    detail.onOuterSashPointerDown(pointerEvt(900))
    detail.onOuterSashPointerUp()
    detail.onOuterSashDblClick()
    expect(detail.outerFullOpen.value).toBe(false)
    expect(detail.leftPaneStyle.value).toMatchObject({ minWidth: '0px' })
    detail.onOuterSashWindowResize()

    // Unmeasurable root: every layout entry point bails out safely.
    detail.splitRootRef.value = boxEl({ width: 0 })
    detail.applyOuterLayout()
    detail.onOuterSashDblClick()
    detail.onOuterSashWindowResize()
    detail.onOuterSashPointerDown(pointerEvt(10))
    detail.onOuterSashPointerMove(pointerEvt(20))
    detail.onOuterSashPointerUp()

    detail.setOuterSashDraggingUi(true)
    expect(document.body.classList.contains('run-detail-outer-sash-dragging')).toBe(true)
    detail.setOuterSashDraggingUi(false)

    app.unmount()
  })

  it('skips desktop sash layout on mobile', async () => {
    isMobile.value = true
    const { detail, app } = await withRunDetail()
    expect(detail.desktopOuterSashLayout.value).toBe(false)
    detail.splitRootRef.value = boxEl({ width: 1400 })
    detail.applyOuterLayout()
    expect(detail.outerSashBooting.value).toBe(false)
    expect(detail.reviewRightPanelStyle.value).toBeUndefined()
    expect(detail.leftPaneStyle.value).toBeUndefined()
    detail.persistOuterLayout()
    detail.onOuterSashPointerDown(pointerEvt(10))
    expect(detail.outerSashDragging.value).toBe(false)
    detail.onOuterSashDblClick()
    detail.onOuterSashWindowResize()

    // Returning to desktop re-runs the boot → apply cycle.
    isMobile.value = false
    await nextTick()
    await nextTick()
    expect(detail.desktopOuterSashLayout.value).toBe(true)

    app.unmount()
  })

  it('derives status map, active path and detail tabs', async () => {
    const { detail, app } = await withRunDetail()

    expect(detail.statusMap.value.n1).toBe('waiting_human')
    expect(detail.activePath.value).toEqual(['e2'])

    const tabIds = detail.detailTabs.value.map((tb) => tb.id)
    expect(tabIds).toEqual(['trace', 'vars', 'sandboxEnv', 'artifacts'])

    detail.run.value = { ...detail.run.value, vars: [] } as Run
    await nextTick()
    expect(detail.detailTabs.value.map((tb) => tb.id)).not.toContain('vars')

    expect(detail.applyDetailArtifactsDeepLink()).toBe(false)

    app.unmount()
  })

  it('opens the run detail drawer from ?detail=artifacts', async () => {
    const { detail, app } = await withRunDetail('/runs/run-1?detail=artifacts')
    expect(detail.showDetail.value).toBe(true)
    expect(detail.detailTab.value).toBe('artifacts')
    app.unmount()
  })

  it('routes timeline execution clicks to the picked iteration', async () => {
    mocks.getRun.mockResolvedValue(
      sampleRun({
        nodeExecutions: {
          n3: [
            { nodeId: 'n3', status: 'failed', outputs: {} },
            { nodeId: 'n3', status: 'completed', outputs: {} },
          ],
        },
      } as Partial<Run>),
    )
    const { detail, app } = await withRunDetail()

    detail.selectNode('n3')
    await nextTick()
    expect(detail.selExecutions.value).toHaveLength(2)
    expect(detail.viewingLatest.value).toBe(true)

    // Different node → index carried through pendingIter.
    detail.selectExecution('n1', 0)
    await nextTick()
    expect(detail.selected.value).toBe('n1')
    detail.selectExecution('n3', 0)
    await nextTick()
    // Same node → direct pointer move.
    detail.selectExecution('n3', 1)
    await nextTick()
    expect(detail.selExecIdx.value).toBe(1)
    expect(detail.viewingLatest.value).toBe(true)

    // Out-of-range pointer clamps to the newest execution.
    detail.selIterIdx.value = 99
    await nextTick()
    expect(detail.selExecIdx.value).toBe(1)

    isMobile.value = true
    await nextTick()
    detail.selectExecution('n3', 0)
    expect(detail.mobileMainPanel.value).toBe('detail')

    app.unmount()
  })

  it('reports a not-found run and keeps the retry classification for network faults', async () => {
    mocks.getRun.mockRejectedValue(new Error('404 not found'))
    const { detail, app } = await withRunDetail()
    expect(detail.loadError.value).toBe(true)
    expect(detail.loadErrorKind.value).toBe('not_found')

    expect(detail.classifyRunLoadError(new TypeError('Failed to fetch'))).toBe('network_or_server')
    expect(detail.classifyRunLoadError(new Error('500 server error'))).toBe('network_or_server')
    expect(detail.classifyRunLoadError(null)).toBe('network_or_server')

    // A syntactically invalid route id never reaches the network.
    mocks.getRun.mockClear()
    app.unmount()
  })

  it('treats obviously invalid route ids as not found without calling the API', async () => {
    const { detail, app } = await withRunDetail('/runs/undefined')
    expect(mocks.getRun).not.toHaveBeenCalled()
    expect(detail.loadErrorKind.value).toBe('not_found')
    app.unmount()
  })

  it('refreshes artifacts for the preview frame and tolerates artifact fetch errors', async () => {
    const { detail, app } = await withRunDetail()
    detail.selectNode('n1')
    await nextTick()

    mocks.runArtifacts.mockResolvedValueOnce([{ id: 'art-9', name: 'preview.png', nodeId: 'n1' }])
    await detail.refreshArtifactPreviewState({ previewArtifact: 'preview.png' })
    expect(detail.run.value.artifacts.map((a) => a.id)).toContain('art-9')

    mocks.runArtifacts.mockRejectedValueOnce(new Error('offline'))
    await detail.refreshArtifactPreviewState({ previewArtifact: '' })
    expect(detail.run.value.artifacts.map((a) => a.id)).toContain('art-9')

    app.unmount()
  })

  it('skips focus refresh while a clarify session is busy', async () => {
    const { detail, app } = await withRunDetail()
    detail.selectNode('n1')
    await nextTick()

    expect(detail.isClarifySessionBusy()).toBe(false)
    detail.reviewChatRef.value = { isSessionBusy: () => true }
    expect(detail.isClarifySessionBusy()).toBe(true)

    const sessions = { n1: { busy: true, items: [{ id: 'keep', text: 'stream' }] } }
    detail.run.value = {
      ...detail.run.value,
      reactSessions: sessions,
    } as unknown as Run
    const turns = detail.run.value.clarifyByNode?.n1

    mocks.getRun.mockClear()
    mocks.getRun.mockResolvedValueOnce({
      ...detail.run.value,
      progress: 0.88,
      reactSessions: { n1: { busy: false, items: [] } },
    })
    detail.onFocusRefresh()
    await flushPromises()
    // Busy focus still patches chrome (g1.2) but must not replace dialogue buffers (g1.1).
    expect(mocks.getRun).toHaveBeenCalled()
    expect(detail.run.value.progress).toBe(0.88)
    expect(detail.run.value.reactSessions?.n1?.busy).toBe(true)
    expect(detail.run.value.reactSessions?.n1?.items?.[0]?.text).toBe('stream')
    expect(detail.run.value.clarifyByNode?.n1).toBe(turns)
    expect(detail.refreshing.value).toBe(false)

    detail.reviewChatRef.value = null
    detail.run.value = {
      ...detail.run.value,
      reactSessions: { n1: { busy: true } },
    } as unknown as Run
    await nextTick()
    expect(detail.isClarifySessionBusy()).toBe(true)

    detail.run.value = { ...detail.run.value, reactSessions: {} } as Run
    await nextTick()
    detail.onVisible()
    await flushPromises()
    expect(mocks.getRun).toHaveBeenCalled()

    detail.selected.value = null
    await nextTick()
    expect(detail.isClarifySessionBusy()).toBe(false)

    app.unmount()
  })

  it('derives elapsed time for terminal runs from node finish times', async () => {
    const started = new Date(Date.now() - 60_000).toISOString()
    mocks.getRun.mockResolvedValue(
      sampleRun({
        status: 'completed',
        startedAt: started,
        durationSec: 0,
        nodeRuns: {
          n1: { nodeId: 'n1', status: 'completed', outputs: {}, startedAt: started, durationSec: 12 },
          n2: { nodeId: 'n2', status: 'completed', outputs: {} },
        },
      } as unknown as Partial<Run>),
    )
    const { detail, app } = await withRunDetail()
    expect(detail.elapsedSec.value).toBe(12)
    expect(detail.progressFrac.value).toBe(1)

    detail.run.value = { ...detail.run.value, durationSec: 42 } as Run
    await nextTick()
    expect(detail.elapsedSec.value).toBe(42)

    detail.run.value = { ...detail.run.value, startedAt: 'not-a-date', durationSec: 7 } as Run
    await nextTick()
    expect(detail.elapsedSec.value).toBe(7)

    app.unmount()
  })

  it('falls back to the backend progress fraction without a pinned graph', async () => {
    mocks.getRun.mockResolvedValue(sampleRun({ nodes: [], edges: [], progress: 0.25 }))
    mocks.getWorkflow.mockResolvedValue({ ...sampleWorkflow(), nodes: [], edges: [] })
    const { detail, app } = await withRunDetail()
    expect(detail.progressFrac.value).toBe(0.25)
    expect(detail.activePath.value).toEqual([])
    app.unmount()
  })

  it('shows the run failure banner with the backend reason', async () => {
    mocks.getRun.mockResolvedValue(
      sampleRun({ status: 'failed', failedReason: '  sandbox boot failed  ' } as Partial<Run>),
    )
    const { detail, app } = await withRunDetail()
    expect(detail.showRunFailureBanner.value).toBe(true)
    expect(detail.runFailureReason.value).toBe('sandbox boot failed')
    app.unmount()
  })

  it('resets composed state when the route id changes', async () => {
    const { detail, app, router } = await withRunDetail()
    detail.gateError.value = 'stale'
    detail.clarifyConfirmError.value = 'stale'
    detail.resumeError.value = 'stale'

    mocks.getRun.mockResolvedValue(sampleRun({ id: 'run-2' }))
    await router.push('/runs/run-2')
    await flushPromises()
    await nextTick()

    expect(detail.gateError.value).toBeNull()
    expect(detail.clarifyConfirmError.value).toBeNull()
    expect(detail.resumeError.value).toBeNull()
    expect(detail.run.value.id).toBe('run-2')

    // Same id is not a reload.
    mocks.getRun.mockClear()
    await router.push('/runs/run-2')
    await flushPromises()
    expect(mocks.getRun).not.toHaveBeenCalled()

    app.unmount()
  })

  it('removes a deleted artifact from the run snapshot', async () => {
    const { detail, app } = await withRunDetail()
    expect(detail.run.value.artifacts).toHaveLength(1)
    detail.onArtifactDeleted('art-1')
    expect(detail.run.value.artifacts).toHaveLength(0)
    app.unmount()
  })
})
