// @vitest-environment happy-dom
import { computed, nextTick, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { AcpEvent, Run } from '@/lib/shared/types'

const mocks = vi.hoisted(() => ({
  runEventsWsUrl: vi.fn((id: string) => `ws://test/${id}`),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return { ...actual, api: { ...actual.api, runEventsWsUrl: mocks.runEventsWsUrl } }
})

import { useRunDetailWs } from './useRunDetailWs'

const rails: AcpEvent[] = [
  { kind: 'thought', text: 'thinking', t: 1 },
  { kind: 'message', text: 'answer', t: 2 },
]

class MockWebSocket {
  static instances: MockWebSocket[] = []
  static throwNext = false
  onopen: ((event: Event) => void) | null = null
  onclose: ((event: CloseEvent) => void) | null = null
  onmessage: ((event: MessageEvent) => void) | null = null
  close = vi.fn(() => this.onclose?.(new CloseEvent('close')))

  constructor(public url: string) {
    if (MockWebSocket.throwNext) {
      MockWebSocket.throwNext = false
      throw new Error('socket construction failed')
    }
    MockWebSocket.instances.push(this)
  }

  open() {
    this.onopen?.(new Event('open'))
  }

  message(value: unknown) {
    const data = typeof value === 'string' ? value : JSON.stringify(value)
    this.onmessage?.(new MessageEvent('message', { data }))
  }

  remoteClose() {
    this.onclose?.(new CloseEvent('close'))
  }
}

function harness(over: {
  status?: Run['status']
  runId?: string
  selected?: string | null
  manual?: boolean
  nodeTab?: string
  sessions?: Record<string, any>
  gateNode?: string
  clarifyNode?: string | null
  eventPages?: Record<string, any>
  clarifyBusy?: boolean
} = {}) {
  const run = ref({
    id: over.runId ?? 'run-1',
    status: over.status ?? 'running',
    reactSessions: over.sessions ?? {},
    gate: over.gateNode ? { reactUpstreamNodeId: over.gateNode, nodeId: 'gate' } : undefined,
    nodeRuns: {},
  } as unknown as Run)
  const selected = ref<string | null>(over.selected === undefined ? 'n1' : over.selected)
  const manual = ref(over.manual ?? false)
  const liveBusy: Record<string, boolean> = {}
  const liveNode = ref<string | null>(null)
  const eventPages: Record<string, any> = over.eventPages ?? {}
  const nodeTab = ref(over.nodeTab ?? 'output')
  const review = {
    isSessionBusy: vi.fn(() => false),
  }
  const gate = {
    applyReviewFrame: vi.fn(),
    applyAcpEvents: vi.fn((): boolean | void => true),
  }
  const reviewChatRef = ref<any>(review)
  const gateApprovalRef = ref<any>(gate)
  const fetchNodeEvents = vi.fn(async () => true)
  const mergeLiveWsAcpPage = vi.fn()
  const rehydrateByNode: Record<string, string> = {}
  const rehydrateNodeEvents = vi.fn(async () => undefined)
  const fetchSandboxLog = vi.fn()
  const maybePollSandboxForBoot = vi.fn()
  const loadRun = vi.fn()
  const patchRunChrome = vi.fn()
  const refreshArtifactPreview = vi.fn()
  const clarifyNode = ref(over.clarifyNode === undefined ? 'n1' : over.clarifyNode)
  const api = useRunDetailWs({
    runId: computed(() => over.runId ?? 'run-1'),
    run,
    selected,
    manual,
    liveBusy,
    liveNode,
    eventPages,
    nodeTab,
    reviewChatRef,
    gateApprovalRef,
    selClarify: computed(() => (clarifyNode.value ? { nodeId: clarifyNode.value } : null)),
    fetchNodeEvents,
    mergeLiveWsAcpPage,
    rehydrateByNode,
    rehydrateNodeEvents,
    fetchSandboxLog,
    maybePollSandboxForBoot,
    isClarifySessionBusy: () => over.clarifyBusy ?? false,
    loadRun,
    patchRunChrome,
    refreshArtifactPreview,
  })
  return {
    api,
    run,
    selected,
    manual,
    liveBusy,
    liveNode,
    eventPages,
    nodeTab,
    review,
    gate,
    reviewChatRef,
    gateApprovalRef,
    fetchNodeEvents,
    mergeLiveWsAcpPage,
    rehydrateByNode,
    rehydrateNodeEvents,
    fetchSandboxLog,
    maybePollSandboxForBoot,
    loadRun,
    patchRunChrome,
    refreshArtifactPreview,
    clarifyNode,
  }
}

describe('useRunDetailWs coverage', () => {
  beforeEach(() => {
    MockWebSocket.instances = []
    MockWebSocket.throwNext = false
    mocks.runEventsWsUrl.mockClear()
    vi.stubGlobal('WebSocket', MockWebSocket)
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('restores busy from valid clarify/gate sessions', async () => {
    const h = harness({
      sessions: {
        n1: { busy: true, waiting: 2, items: ['x'], activeItem: 'x' },
        gateNode: { busy: false },
        bad: null,
      },
      gateNode: 'gateNode',
      eventPages: { n1: { events: rails } },
    })
    await h.api.projectDialogueAfterLoad({ reactSessions: h.run.value.reactSessions })
    expect(h.liveBusy.n1).toBe(true)
    h.api.teardownRealtime()
  })

  it.each([{}, { other: { busy: true, waiting: 0, items: [] } }])(
    'releases omitted current and gate sessions only for an authoritative map: %j',
    async (sessions) => {
      const h = harness({ gateNode: 'producer' })
      h.liveBusy.n1 = true
      h.liveBusy.producer = true
      await h.api.projectDialogueAfterLoad({})
      expect(h.liveBusy.n1).toBe(true)
      expect(h.liveBusy.producer).toBe(true)

      await h.api.projectDialogueAfterLoad({ reactSessions: sessions })
      expect(h.liveBusy.n1).toBe(false)
      expect(h.liveBusy.producer).toBe(false)
      if ('other' in sessions) expect(h.liveBusy.other).toBe(true)
      h.api.teardownRealtime()
    },
  )

  it.each([undefined, {}, { other: { busy: true, waiting: 0, items: [] } }])(
    'refreshes saved dialogue after reconnect omits a finished session: %j',
    async (sessions) => {
      const h = harness({
        sessions: { n1: { busy: true, waiting: 0, items: [], activeItem: { text: '我的选择' } } },
        eventPages: { n1: { events: rails } },
      })
      h.liveBusy.n1 = true
      h.api.connectWs({ fromReconnect: true })
      const socket = MockWebSocket.instances.at(-1)!
      socket.open()
      await nextTick()
      await nextTick()
      expect(h.liveBusy.n1).toBe(true)
      expect(h.loadRun).not.toHaveBeenCalled()

      socket.message({ type: 'snapshot', run: {} })
      await nextTick()
      expect(h.liveBusy.n1).toBe(true)
      expect(h.loadRun).not.toHaveBeenCalled()
      socket.message({
        type: 'snapshot',
        run: sessions === undefined
          ? { id: 'run-1', status: 'waiting_human' }
          : { id: 'run-1', reactSessions: sessions },
      })
      if (sessions && 'other' in sessions) {
        socket.message({ type: 'review', event: 'queue_state', nodeId: 'other', busy: true, waiting: 0 })
      }
      await nextTick()
      await nextTick()
      await nextTick()
      expect(h.liveBusy.n1).toBe(false)
      expect(h.loadRun).toHaveBeenCalledExactlyOnceWith(false, true)
      h.api.teardownRealtime()
    },
  )

  it('keeps live turn boundaries newer than a reconnect snapshot', async () => {
    const h = harness()
    h.liveBusy.n1 = true
    h.api.connectWs({ fromReconnect: true })
    const socket = MockWebSocket.instances.at(-1)!
    socket.open()
    await nextTick()
    await nextTick()
    // An empty cached map cannot clear the active turn before the server snapshot.
    expect(h.liveBusy.n1).toBe(true)

    socket.message({ type: 'snapshot', run: { reactSessions: {} } })
    socket.message({ type: 'review', event: 'turn_begin', nodeId: 'n1', item: { text: '新消息' } })
    await nextTick()
    await nextTick()
    await nextTick()
    expect(h.liveBusy.n1).toBe(true)
    expect(h.loadRun).not.toHaveBeenCalled()
    h.api.teardownRealtime()
  })

  it('rejects old server queue snapshots before mutating parent busy state', () => {
    const h = harness()
    h.api.connectWs()
    const socket = MockWebSocket.instances.at(-1)!
    socket.open()
    socket.message({ type: 'review', event: 'queue_state', nodeId: 'n1', sessionGeneration: 3, revision: 12,
      busy: true, waiting: 0, items: [], activeItem: { id: 'c', text: 'current' } })
    socket.message({ type: 'review', event: 'queue_state', nodeId: 'n1', sessionGeneration: 3, revision: 4,
      busy: false, waiting: 1, items: [{ id: 'b', text: 'completed earlier' }] })
    expect(h.liveBusy.n1).toBe(true)
    socket.message({ type: 'review', event: 'turn_done', nodeId: 'n1', sessionGeneration: 3, revision: 13,
      item: { id: 'c' }, busy: false, waiting: 0, items: [] })
    socket.message({ type: 'review', event: 'turn_begin', nodeId: 'n1', sessionGeneration: 3, revision: 8, item: { id: 'b' } })
    expect(h.liveBusy.n1).toBe(false)
    h.api.teardownRealtime()
  })

  it('reconciles an idle REST session despite stale local busy and a healthy heartbeat', async () => {
    const h = harness({ clarifyBusy: true })
    h.api.connectWs()
    const socket = MockWebSocket.instances.at(-1)!
    socket.open()
    socket.message({ type: 'review', nodeId: 'n1', event: 'turn_begin', sessionGeneration: 5, revision: 2,
      item: { id: 'finished', text: '我的选择' }, waiting: 1, items: [{ id: 'sent', text: '后续更正' }] })
    h.review.isSessionBusy.mockReturnValue(true)
    socket.message({ type: 'heartbeat' })
    const capture = h.api.captureReviewSnapshot()
    await h.api.reconcileReviewSnapshot({ id: 'run-1', reviewSessionGeneration: 5, reactSessions: {} } as Run, capture)
    expect(h.liveBusy.n1).toBe(false)
    expect(h.run.value.reactSessions?.n1?.busy).not.toBe(true)
    h.api.teardownRealtime()
  })

  it('keeps newer WS turn authority when an older REST session snapshot returns', async () => {
    const h = harness()
    h.api.connectWs()
    const socket = MockWebSocket.instances.at(-1)!
    socket.open()
    socket.message({ type: 'review', nodeId: 'n1', event: 'turn_begin', sessionGeneration: 5, revision: 2,
      item: { id: 'first', text: 'first' }, waiting: 0, items: [] })
    const capture = h.api.captureReviewSnapshot()
    socket.message({ type: 'review', nodeId: 'n1', event: 'turn_begin', sessionGeneration: 6, revision: 1,
      item: { id: 'new', text: 'newer turn' }, waiting: 0, items: [] })
    await h.api.reconcileReviewSnapshot({ id: 'run-1', reviewSessionGeneration: 5, reactSessions: {} } as Run, capture)
    expect(h.liveBusy.n1).toBe(true)
    expect(h.run.value.reactSessions?.n1?.activeItem?.id).toBe('new')
    h.api.teardownRealtime()
  })

  it('serializes snapshot and content recovery while live WS ingress remains immediate', async () => {
    const h = harness()
    h.api.connectWs()
    const socket = MockWebSocket.instances.at(-1)!
    socket.open()
    const state = { sessionGeneration: 5, revision: 2, busy: true, waiting: 0, items: [], activeItem: { id: 'active', text: 'work' } }
    socket.message({ type: 'review', nodeId: 'n1', event: 'queue_state', ...state })
    let releaseSnapshot!: () => void
    let snapshotPending = true
    const recovery = h.api.requestReviewRecovery(async () => {
      await new Promise<void>(resolve => { releaseSnapshot = resolve })
      snapshotPending = false
    })
    h.fetchNodeEvents.mockImplementationOnce(async nodeId => {
      expect(snapshotPending).toBe(false)
      h.eventPages[nodeId!] = { events: rails }
      return true
    })
    h.api.reconcileReviewSnapshot({ id: 'run-1', reactSessions: { n1: state } } as Run)
    expect(h.fetchNodeEvents).not.toHaveBeenCalled()
    socket.message({ type: 'acp', nodeId: 'n1', events: rails, busy: true })
    releaseSnapshot()
    await recovery
    expect(h.fetchNodeEvents).toHaveBeenCalledTimes(1)

    const beforeVisit = h.api.captureReviewSnapshot()
    socket.message({ type: 'node_enter', nodeId: 'n1', iteration: 2 })
    expect(h.api.reconcileReviewSnapshot({ id: 'run-1', reviewSessionGeneration: 5, reactSessions: {} } as Run, beforeVisit)).toBeUndefined()
    expect(h.liveBusy.n1).toBe(true)
    h.api.teardownRealtime()
  })

  it('keeps a newly connected idle view idle when a retired session publishes late', async () => {
    const h = harness()
    h.api.connectWs()
    const socket = MockWebSocket.instances.at(-1)!
    socket.open()
    socket.message({ type: 'snapshot', run: {
      id: 'run-1', reviewSessionGeneration: 7, reactSessions: {},
    } })
    await nextTick()
    await nextTick()
    await nextTick()

    // This viewer never saw generation 7 finish; its empty snapshot is enough.
    socket.message({ type: 'review', event: 'queue_state', nodeId: 'n1',
      sessionGeneration: 7, revision: 2, busy: true, waiting: 1,
      items: [{ id: 'already-completed', text: 'old request' }],
    })
    expect(h.liveBusy.n1).toBe(false)

    // A session created after the snapshot must still be admitted.
    socket.message({ type: 'review', event: 'turn_begin', nodeId: 'n1',
      sessionGeneration: 8, revision: 1, busy: true,
      item: { id: 'new-request', text: 'new request' },
    })
    expect(h.liveBusy.n1).toBe(true)
    h.api.teardownRealtime()
  })

  it('rebases revisions only on a new socket snapshot after a server restart', async () => {
    const h = harness()
    h.api.connectWs()
    let socket = MockWebSocket.instances.at(-1)!
    socket.open()
    socket.message({ type: 'review', event: 'turn_begin', nodeId: 'n1', sessionGeneration: 500, revision: 8, item: { id: 'before-restart' } })
    h.api.connectWs({ fromReconnect: true })
    socket = MockWebSocket.instances.at(-1)!
    socket.open()
    socket.message({ type: 'snapshot', run: { id: 'run-1', reactSessions: {
      n1: { sessionGeneration: 1, revision: 1, busy: false, waiting: 0, items: [] },
    } } })
    await nextTick()
    await nextTick()
    await nextTick()
    expect(h.liveBusy.n1).toBe(false)
    socket.message({ type: 'review', event: 'turn_begin', nodeId: 'n1', sessionGeneration: 1, revision: 2, item: { id: 'after-restart' } })
    expect(h.liveBusy.n1).toBe(true)
    h.api.teardownRealtime()
  })

  it('connects, reconnects with backoff, ignores stale sockets, and parses snapshots', async () => {
    vi.useFakeTimers()
    vi.spyOn(Math, 'random').mockReturnValue(0)
    const h = harness({
      sessions: { n1: { busy: false } },
      eventPages: { n1: { events: rails } },
    })
    h.api.connectWs()
    const first = MockWebSocket.instances[0]!
    expect(first.url).toBe('ws://test/run-1')
    first.open()
    expect(h.api.wsConnected).toBe(true)
    first.message('not json')
    first.message({ type: 'snapshot', run: { reactSessions: { n1: { busy: false } } } })
    await nextTick()
    expect(h.run.value.reactSessions).toEqual({ n1: { busy: false, waiting: 0, items: [] } })

    first.remoteClose()
    expect(h.api.wsConnected).toBe(false)
    await vi.advanceTimersByTimeAsync(800)
    const second = MockWebSocket.instances[1]!
    second.open()
    await nextTick()
    expect(h.api.wsConnected).toBe(true)
    // Events from the stale first socket are ignored.
    first.message({ type: 'status' })
    expect(h.loadRun).not.toHaveBeenCalled()

    // Explicit reconnect closes the current socket and creates a replacement.
    h.api.connectWs({ fromReconnect: true })
    const third = MockWebSocket.instances[2]!
    expect(second.close).toHaveBeenCalled()
    third.open()
    await nextTick()

    h.api.teardownRealtime()
    third.remoteClose()
    await vi.advanceTimersByTimeAsync(1000)
    expect(MockWebSocket.instances).toHaveLength(3)
  })

  it('recovers when construction throws, but does not reconnect terminal or empty runs', async () => {
    vi.useFakeTimers()
    vi.spyOn(Math, 'random').mockReturnValue(0)
    MockWebSocket.throwNext = true
    const running = harness()
    running.api.connectWs()
    await vi.advanceTimersByTimeAsync(800)
    expect(MockWebSocket.instances).toHaveLength(1)
    running.api.teardownRealtime()

    const terminal = harness({ status: 'completed' })
    terminal.api.connectWs()
    MockWebSocket.instances.at(-1)!.remoteClose()
    await vi.advanceTimersByTimeAsync(2000)
    expect(MockWebSocket.instances).toHaveLength(2)
    terminal.api.teardownRealtime()

    const empty = harness({ runId: '' })
    empty.api.connectWs()
    expect(mocks.runEventsWsUrl).not.toHaveBeenCalledWith('')
  })

  it('retries reconnect construction and handshake failures without consuming an intentional close', async () => {
    vi.useFakeTimers()
    vi.spyOn(Math, 'random').mockReturnValue(0)
    const h = harness()
    h.api.connectWs()
    MockWebSocket.instances[0]!.open()
    MockWebSocket.throwNext = true
    MockWebSocket.instances[0]!.remoteClose()
    await vi.advanceTimersByTimeAsync(800)
    expect(MockWebSocket.instances).toHaveLength(1)
    await vi.advanceTimersByTimeAsync(1600)
    expect(MockWebSocket.instances).toHaveLength(2)
    MockWebSocket.instances[1]!.remoteClose()
    await vi.advanceTimersByTimeAsync(3200)
    expect(MockWebSocket.instances).toHaveLength(3)
    h.api.teardownRealtime()
  })

  it('repairs a half-open socket without close and preserves the streaming session', async () => {
    vi.useFakeTimers()
    vi.spyOn(Math, 'random').mockReturnValue(0)
    const h = harness({ sessions: { n1: { busy: true, waiting: 0, items: [] } } })
    h.api.connectWs()
    const stalled = MockWebSocket.instances[0]!
    stalled.open()
    h.liveBusy.n1 = true
    await vi.advanceTimersByTimeAsync(60_800)
    expect(stalled.close).toHaveBeenCalledTimes(1)
    expect(MockWebSocket.instances).toHaveLength(2)
    expect(h.liveBusy.n1).toBe(true)
    const replacement = MockWebSocket.instances[1]!
    replacement.open()
    for (let i = 0; i < 3; i++) {
      await vi.advanceTimersByTimeAsync(25_000)
      replacement.message({ type: 'heartbeat' })
    }
    expect(MockWebSocket.instances).toHaveLength(2)
    h.api.teardownRealtime()
    await vi.advanceTimersByTimeAsync(70_000)
    expect(MockWebSocket.instances).toHaveLength(2)
  })

  it('reconnects queued runs before their first node starts', async () => {
    vi.useFakeTimers()
    vi.spyOn(Math, 'random').mockReturnValue(0)
    const h = harness({ status: 'queued' })
    h.api.connectWs()
    MockWebSocket.instances[0]!.open()
    MockWebSocket.instances[0]!.remoteClose()
    await vi.advanceTimersByTimeAsync(800)
    expect(MockWebSocket.instances).toHaveLength(2)
    h.api.teardownRealtime()
  })

  it('dispatches ACP and review frames to live and dialogue state', async () => {
    const h = harness({ gateNode: 'n1' })
    h.api.connectWs()
    const socket = MockWebSocket.instances[0]!
    socket.open()

    socket.message({ type: 'acp', nodeId: 'n1', events: rails, busy: true })
    expect(h.mergeLiveWsAcpPage).toHaveBeenCalledWith('n1', rails)
    expect(h.liveBusy.n1).toBe(true)
    expect(h.liveNode.value).toBe('n1')
    expect(h.selected.value).toBe('n1')

    h.manual.value = true
    h.selected.value = 'old'
    socket.message({ type: 'acp', nodeId: 'n2', events: [], busy: false })
    expect(h.selected.value).toBe('old')
    expect(h.liveBusy.n2).toBe(false)

    socket.message({ type: 'review', nodeId: 'n1', event: 'turn_begin' })
    expect(h.liveBusy.n1).toBe(true)
    socket.message({ type: 'review', nodeId: 'n1', event: 'queue_state', busy: false })
    expect(h.liveBusy.n1).toBe(false)
    socket.message({ type: 'review', nodeId: 'n1', event: 'turn_done' })
    socket.message({ type: 'review', nodeId: 'n1', event: 'error' })
    expect(h.loadRun).toHaveBeenCalledTimes(2)

    h.clarifyNode.value = 'different'
    socket.message({ type: 'review', nodeId: 'n1', event: 'noop' })
    h.clarifyNode.value = null
    socket.message({ type: 'review', nodeId: 'n1', event: 'noop' })

    h.api.teardownRealtime()
  })

  it('dispatches refresh frames and protects a busy clarify session', () => {
    const normal = harness()
    normal.api.connectWs()
    const socket = MockWebSocket.instances[0]!
    socket.open()
    for (const type of ['trace', 'react', 'artifact_edit'] as const) socket.message({ type })
    socket.message({ type: 'status' })
    expect(normal.loadRun).toHaveBeenCalledTimes(4)
    expect(normal.loadRun).toHaveBeenCalledWith(false, true)
    expect(normal.liveNode.value).toBeNull()
    expect(normal.refreshArtifactPreview).toHaveBeenCalledWith(expect.objectContaining({ type: 'artifact_edit' }))
    normal.api.teardownRealtime()

    const busy = harness({ clarifyBusy: true })
    busy.api.connectWs()
    const busySocket = MockWebSocket.instances.at(-1)!
    busySocket.message({ type: 'artifact_edit', previewArtifact: 'preview.png' })
    busySocket.message({ type: 'status' })
    expect(busy.refreshArtifactPreview).toHaveBeenCalledWith(
      expect.objectContaining({ previewArtifact: 'preview.png' }),
    )
    expect(busy.patchRunChrome).toHaveBeenCalled()
    expect(busy.loadRun).not.toHaveBeenCalled()
    expect(busy.loadRun.mock.calls.every((c) => c[0] !== true)).toBe(true)
    busy.api.teardownRealtime()
  })

  it('initializes selection and covers every polling state', async () => {
    vi.useFakeTimers()
    const h = harness({ selected: null, nodeTab: 'sandbox', clarifyNode: null })
    const deep = vi.fn(() => true)
    const output = vi.fn(() => false)
    const sync = vi.fn()
    await h.api.initAfterLoadSuccess({
      applyDetailArtifactsDeepLink: deep,
      applyOutputDeepLinkFocus: output,
      defaultNode: 'n1',
      syncAllMcpCallsFromRun: sync,
    })
    expect(h.selected.value).toBe('n1')
    expect(sync).toHaveBeenCalled()
    expect(h.rehydrateNodeEvents).toHaveBeenCalledWith('n1')
    expect(h.fetchSandboxLog).toHaveBeenCalledWith('n1')
    MockWebSocket.instances[0]!.open()
    h.fetchSandboxLog.mockClear()

    h.rehydrateByNode.n1 = 'ready'
    await vi.advanceTimersByTimeAsync(2000)
    expect(h.loadRun).toHaveBeenCalledWith(false, true)
    expect(h.fetchNodeEvents).toHaveBeenCalledWith('n1')
    expect(h.fetchSandboxLog).toHaveBeenCalledWith('n1', { intent: 'silent_poll' })
    expect(h.maybePollSandboxForBoot).toHaveBeenCalled()

    h.rehydrateByNode.n1 = 'idle'
    await vi.advanceTimersByTimeAsync(2000)
    expect(h.rehydrateNodeEvents).toHaveBeenCalledWith('n1')
    h.rehydrateByNode.n1 = 'loading'
    h.fetchNodeEvents.mockClear()
    await vi.advanceTimersByTimeAsync(2000)
    expect(h.fetchNodeEvents).not.toHaveBeenCalled()
    h.rehydrateByNode.n1 = 'error'
    await vi.advanceTimersByTimeAsync(2000)

    // Displayable live events suppress the REST event poll.
    h.eventPages.n1 = { live: true, events: rails }
    h.liveNode.value = 'n1'
    h.fetchNodeEvents.mockClear()
    h.rehydrateByNode.n1 = 'ready'
    await vi.advanceTimersByTimeAsync(2000)
    expect(h.fetchNodeEvents).not.toHaveBeenCalled()

    h.run.value = { ...h.run.value, status: 'completed' } as Run
    h.loadRun.mockClear()
    await vi.advanceTimersByTimeAsync(2000)
    expect(h.loadRun).not.toHaveBeenCalled()
    h.api.teardownRealtime()
  })

  it('does not overwrite focused selection and skips full polling while clarify is busy', async () => {
    vi.useFakeTimers()
    const focused = harness({ selected: 'kept', clarifyBusy: true })
    await focused.api.initAfterLoadSuccess({
      applyDetailArtifactsDeepLink: () => false,
      applyOutputDeepLinkFocus: () => true,
      defaultNode: 'default',
      syncAllMcpCallsFromRun: vi.fn(),
    })
    expect(focused.selected.value).toBe('kept')
    await vi.advanceTimersByTimeAsync(2000)
    expect(focused.loadRun).not.toHaveBeenCalled()
    expect(focused.patchRunChrome).toHaveBeenCalled()
    focused.api.teardownRealtime()
  })

  it('polls queued runs, skips hidden ticks, and never hard-loads on the timer (g1.2 / g1.3)', async () => {
    vi.useFakeTimers()
    const queued = harness({ status: 'queued' })
    await queued.api.initAfterLoadSuccess({
      applyDetailArtifactsDeepLink: () => false,
      applyOutputDeepLinkFocus: () => true,
      defaultNode: 'n1',
      syncAllMcpCallsFromRun: vi.fn(),
    })
    await vi.advanceTimersByTimeAsync(2000)
    expect(queued.loadRun).toHaveBeenCalledWith(false, true)
    expect(queued.loadRun.mock.calls.every((c) => c[0] !== true)).toBe(true)

    queued.loadRun.mockClear()
    Object.defineProperty(document, 'hidden', { configurable: true, get: () => true })
    await vi.advanceTimersByTimeAsync(2000)
    expect(queued.loadRun).not.toHaveBeenCalled()
    expect(queued.patchRunChrome).not.toHaveBeenCalled()
    Object.defineProperty(document, 'hidden', { configurable: true, get: () => false })
    queued.api.teardownRealtime()
    vi.useRealTimers()
  })
})
