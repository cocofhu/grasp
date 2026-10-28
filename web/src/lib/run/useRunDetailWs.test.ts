// @vitest-environment happy-dom
import { computed, effectScope, ref } from 'vue'
import { flushPromises } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { AcpEvent, Run } from '@/lib/shared/types'
import { useRunDetailWs } from './useRunDetailWs'

const sampleEvents: AcpEvent[] = [
  { kind: 'thought', text: 'thinking', t: 1 },
  { kind: 'message', text: 'hello', t: 2 },
]

describe('useRunDetailWs', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('seeds busy dialogue rails, buffers ACP, and tears down realtime', async () => {
    const run = ref({
      id: 'run-1',
      status: 'running',
      reactSessions: {
        c1: { busy: true, waiting: 0, items: [], activeItem: null },
      },
      gate: { reactUpstreamNodeId: 'c1', nodeId: 'g1' },
      nodeRuns: {},
    } as unknown as Run)
    const selected = ref<string | null>('c1')
    const manual = ref(false)
    const liveBusy: Record<string, boolean> = {}
    const liveNode = ref<string | null>(null)
    const eventPages: Record<string, any> = {
      c1: { events: sampleEvents, nextCursor: '', hasMore: false, live: false },
    }
    const nodeTab = ref('clarify')
    const reviewChatRef = ref<{
      applyReviewFrame: (frame: any) => boolean
      applyAcpEvents: (events: AcpEvent[] | undefined, nodeId?: string) => boolean
    } | null>({
      applyReviewFrame: () => true,
      applyAcpEvents: () => true,
    })
    const gateApprovalRef = ref<{
      applyReviewFrame: (frame: any) => void
      applyAcpEvents: (events: AcpEvent[] | undefined) => boolean
    } | null>({
      applyReviewFrame: () => undefined,
      applyAcpEvents: () => true,
    })
    const rehydrateByNode: Record<string, string> = {}
    const fetchNodeEvents = vi.fn(async () => true)
    const loadRun = vi.fn()

    const api = useRunDetailWs({
      runId: computed(() => 'run-1'),
      run,
      selected,
      manual,
      liveBusy,
      liveNode,
      eventPages,
      nodeTab,
      reviewChatRef,
      gateApprovalRef,
      selClarify: computed(() => ({ nodeId: 'c1' })),
      fetchNodeEvents,
      mergeLiveWsAcpPage: vi.fn(),
      rehydrateByNode,
      rehydrateNodeEvents: vi.fn(async () => undefined),
      fetchSandboxLog: vi.fn(),
      maybePollSandboxForBoot: vi.fn(),
      isClarifySessionBusy: () => false,
      loadRun,
    })

    await api.projectDialogueAfterLoad({
      reactSessions: {
        c1: { busy: true, waiting: 0, items: [], activeItem: null },
      },
    })
    expect(liveBusy.c1).toBe(true)

    await api.seedDialogueAcpAfterRestore(['c1'])
    api.applyOrBufferDialogueAcp('c1', sampleEvents, true)

    // Buffer path when surfaces missing
    reviewChatRef.value = null
    gateApprovalRef.value = null
    api.applyOrBufferDialogueAcp('c1', sampleEvents, true)

    api.resetDialogueState()
    await api.initAfterLoadSuccess({
      applyDetailArtifactsDeepLink: () => false,
      applyOutputDeepLinkFocus: () => false,
      defaultNode: 'c1',
      syncAllMcpCallsFromRun: () => undefined,
    })
    api.teardownRealtime()
    expect(api.wsConnected).toBe(false)
    expect(typeof api.connectWs).toBe('function')
  })

  it('polls sandbox log silently on sandbox tab interval', async () => {
    vi.useFakeTimers()
    const fetchSandboxLog = vi.fn()
    const run = ref({
      id: 'run-3',
      status: 'running',
      reactSessions: {},
      nodeRuns: { n1: { nodeId: 'n1', status: 'running' } },
    } as unknown as Run)
    const selected = ref<string | null>('n1')
    const nodeTab = ref('sandbox')
    const eventPages: Record<string, any> = {
      n1: { events: [{ kind: 'message', text: 'x', t: 1 }], nextCursor: '', hasMore: false, live: true },
    }

    const api = useRunDetailWs({
      runId: computed(() => 'run-3'),
      run,
      selected,
      manual: ref(false),
      liveBusy: {},
      liveNode: ref('n1'),
      eventPages,
      nodeTab,
      reviewChatRef: ref(null),
      gateApprovalRef: ref(null),
      selClarify: computed(() => null),
      fetchNodeEvents: vi.fn(async () => true),
      mergeLiveWsAcpPage: vi.fn(),
      rehydrateByNode: { n1: 'ready' },
      rehydrateNodeEvents: vi.fn(async () => undefined),
      fetchSandboxLog,
      maybePollSandboxForBoot: vi.fn(),
      isClarifySessionBusy: () => false,
      loadRun: vi.fn(),
    })

    await api.initAfterLoadSuccess({
      applyDetailArtifactsDeepLink: () => false,
      applyOutputDeepLinkFocus: () => false,
      defaultNode: 'n1',
      syncAllMcpCallsFromRun: () => undefined,
    })
    fetchSandboxLog.mockClear()

    vi.advanceTimersByTime(2000)
    expect(fetchSandboxLog).toHaveBeenCalledWith('n1', { intent: 'silent_poll' })

    api.teardownRealtime()
    vi.useRealTimers()
  })

  it('restores idle sessions and flushes without busy seed', async () => {
    const run = ref({
      id: 'run-2',
      status: 'waiting_human',
      reactSessions: {},
      nodeRuns: {},
    } as unknown as Run)
    const api = useRunDetailWs({
      runId: computed(() => 'run-2'),
      run,
      selected: ref(null),
      manual: ref(false),
      liveBusy: {},
      liveNode: ref(null),
      eventPages: {},
      nodeTab: ref('output'),
      reviewChatRef: ref(null),
      gateApprovalRef: ref(null),
      selClarify: computed(() => null),
      fetchNodeEvents: vi.fn(async () => false),
      mergeLiveWsAcpPage: vi.fn(),
      rehydrateByNode: {},
      rehydrateNodeEvents: vi.fn(async () => undefined),
      fetchSandboxLog: vi.fn(),
      maybePollSandboxForBoot: vi.fn(),
      isClarifySessionBusy: () => false,
      loadRun: vi.fn(),
    })

    await api.projectDialogueAfterLoad({
      reactSessions: {
        idle: { busy: false, waiting: 0, items: [] },
      },
    })
    api.teardownRealtime()
  })
  it('restores a late-mounted LLM conversation from current frames and buffered ACP', async () => {
    let socket!: { onmessage?: (event: { data: string }) => void; close: () => void }
    vi.stubGlobal('WebSocket', class {
      onmessage?: (event: { data: string }) => void
      constructor() { socket = this }
      close() {}
    })
    const run = ref({ id: 'r1', status: 'waiting_human', nodeRuns: {}, reactSessions: {} } as unknown as Run)
    const ready = ref(false)
    const applyReviewFrame = vi.fn(() => ready.value)
    const applyAcpEvents = vi.fn(() => ready.value)
    const llmTranscriptRef = ref<{
      getNodeId: () => string
      isChatReady: () => boolean
      applyReviewFrame: typeof applyReviewFrame
      applyAcpEvents: typeof applyAcpEvents
    } | null>(null)
    const scope = effectScope()
    const ws = scope.run(() => useRunDetailWs({
      runId: computed(() => 'r1'), run, selected: ref('other'), manual: ref(true),
      liveBusy: {}, liveNode: ref(null), eventPages: {}, nodeTab: ref('output'),
      reviewChatRef: ref(null), llmTranscriptRef, gateApprovalRef: ref(null),
      selClarify: computed(() => null), fetchNodeEvents: vi.fn(async () => false),
      mergeLiveWsAcpPage: vi.fn(), rehydrateByNode: {},
      rehydrateNodeEvents: vi.fn(async () => undefined), fetchSandboxLog: vi.fn(),
      maybePollSandboxForBoot: vi.fn(), isClarifySessionBusy: () => false, loadRun: vi.fn(),
    }))!
    try {
      ws.connectWs()
      socket.onmessage?.({ data: JSON.stringify({ type: 'review', event: 'turn_begin', nodeId: 'chat', item: { id: 'a', text: 'request' } }) })
      socket.onmessage?.({ data: JSON.stringify({ type: 'acp', nodeId: 'chat', events: sampleEvents, busy: true }) })
      llmTranscriptRef.value = { getNodeId: () => 'chat', isChatReady: () => ready.value, applyReviewFrame, applyAcpEvents }
      await flushPromises()
      ready.value = true
      await flushPromises()
      expect(run.value.reactSessions?.chat?.busy).toBe(true)
      expect(applyReviewFrame).toHaveBeenCalledWith(expect.objectContaining({ event: 'queue_state', nodeId: 'chat', busy: true, activeItem: { id: 'a', text: 'request' } }))
      expect(applyAcpEvents).toHaveBeenCalledWith(sampleEvents, 'chat')
      socket.onmessage?.({ data: JSON.stringify({ type: 'review', event: 'turn_done', nodeId: 'chat' }) })
      expect(applyReviewFrame).toHaveBeenCalledWith(expect.objectContaining({ event: 'turn_done', nodeId: 'chat' }))
      expect(run.value.reactSessions?.chat?.busy).toBe(false)
    } finally {
      ws.teardownRealtime()
      scope.stop()
      vi.unstubAllGlobals()
    }
  })

})
