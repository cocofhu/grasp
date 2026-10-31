/**
 * Run 详情：WS 连接、对话面 ACP 再水合（seed/buffer/busy-retry）、reconnect。
 */
import { nextTick, watch, type ComputedRef, type Ref } from 'vue'
import { api } from '@/lib/api/api'
import { createPendingAcpBuffer } from '@/lib/run/pendingAcpBuffer'
import { deliverOrBufferDialogueAcp } from '@/lib/run/dialogueAcpDelivery'
import {
  createBusySeedRetryController,
  runBusySeedRetry,
} from '@/lib/run/busySeedRetry'
import { notifyRunArtifactEdit } from '@/lib/chat/artifactUnread'
import { applySidebarEnter, noteSidebarRunStatus } from '@/lib/inbox/sidebarHandoff'
import { createWsReconnectController } from '@/lib/run/wsReconnect'
import { createWsHeartbeat } from '@/lib/run/wsHeartbeat'
import { createReviewSessionState, hasAcpContent, type ReviewRecoveryCheckpoint, type ReviewSessionFrame } from '@/lib/chat/reviewSessionState'
import type { AcpEvent, Run } from '@/lib/shared/types'
import { ingestDialogue } from '@/lib/chat/dialogueStore'
import type { EventPageState } from '@/lib/run/useRunDetailLiveLog'

type DialogueSurface = {
  getNodeId?: () => string
  applyRequestRejected?: (frame: any) => void
  discardLastQueued?: () => void
  isSessionBusy?: () => boolean
  isChatReady?: () => boolean
} | null

type GateSurface = {
  applyReviewFrame?: (frame: any) => void
  applyAcpEvents?: (events: AcpEvent[] | undefined) => boolean | void
} | null

export function useRunDetailWs(opts: {
  runId: ComputedRef<string>
  run: Ref<Run>
  selected: Ref<string | null>
  manual: Ref<boolean>
  liveBusy: Record<string, boolean>
  liveNode: Ref<string | null>
  eventPages: Record<string, EventPageState>
  nodeTab: Ref<string>
  reviewChatRef: Ref<DialogueSurface>
  /** LLM transcript chat. Mounted only in that view; frames still belong to it. */
  llmTranscriptRef?: Ref<DialogueSurface>
  gateApprovalRef: Ref<GateSurface>
  selClarify: ComputedRef<{ nodeId: string } | null>
  fetchNodeEvents: (nodeId: string | null, opts?: { signal?: AbortSignal; isCurrent?: () => boolean }) => Promise<boolean>
  mergeLiveWsAcpPage: (nodeId: string, wsEvents: AcpEvent[]) => void
  rehydrateByNode: Record<string, string>
  rehydrateNodeEvents: (nodeId: string | null, opts?: { force?: boolean }) => Promise<void>
  fetchSandboxLog: (
    nodeId: string | null,
    opts?: { intent?: 'user_initiated' | 'silent_poll' },
  ) => Promise<void> | void
  maybePollSandboxForBoot: () => void
  isClarifySessionBusy: () => boolean
  loadRun: (hard?: boolean, silent?: boolean) => Promise<void> | void
  patchRunChrome?: () => Promise<void> | void
  refreshArtifactPreview?: (frame?: { previewArtifact?: string }) => void
}) {
  const {
    runId,
    run,
    selected,
    manual,
    liveBusy,
    liveNode,
    eventPages,
    nodeTab,
    reviewChatRef,
    llmTranscriptRef,
    gateApprovalRef,
    selClarify,
    fetchNodeEvents,
    mergeLiveWsAcpPage,
    rehydrateByNode,
    rehydrateNodeEvents,
    fetchSandboxLog,
    maybePollSandboxForBoot,
    isClarifySessionBusy,
    loadRun,
    patchRunChrome,
    refreshArtifactPreview,
  } = opts

  function dialogueChat(nodeId: string) {
    const llm = llmTranscriptRef?.value
    if (llm && llm.getNodeId?.() === nodeId) return llm
    return reviewChatRef.value
  }

  function showsDialogue(nodeId: string) {
    return selClarify.value?.nodeId === nodeId || llmTranscriptRef?.value?.getNodeId?.() === nodeId
  }

  /**
   * ACP frames that arrived while ClarifyChat / GateApproval was unmounted
   * during hard load. Flushed after queue_state rebuilds the streaming slot.
   */
  const pendingDialogueAcp = createPendingAcpBuffer()
  /** Rails applied via seed or live — stops busy seed retry (g3). */
  let dialogueRailsFilled = false
  let dialogueLiveIncremental = false
  const busySeedRetry = createBusySeedRetryController()
  const reviewState = createReviewSessionState()
  const wsReconnect = createWsReconnectController({
    connect: () => connectWs({ fromReconnect: true }),
    shouldReconnect: () =>
      !!runId.value &&
      (run.value.status === 'queued' || run.value.status === 'running' || run.value.status === 'waiting_human'),
  })
  const heartbeat = createWsHeartbeat({
    onTimeout: () => {
      const socket = ws
      ws = undefined
      wsConnected = false
      heartbeat.stop()
      socket?.close()
      wsReconnect.onClose()
    },
  })

  let timer: number | undefined
  let ws: WebSocket | undefined
  let wsConnected = false

  /**
   * Align Inbox hard-load: multi nextTick so ReviewComposer/GateApproval mount
   * before queue_state → seed → flush → live (g2.1).
   */
  function dialogueNodes() {
    return [...new Set([selClarify.value?.nodeId, llmTranscriptRef?.value?.getNodeId?.(),
      run.value.gate?.reactUpstreamNodeId].filter((nodeId): nodeId is string => !!nodeId))]
  }

  // A node revisit can reuse its id while replacing the conversation. Request
  // checkpoints therefore include the visit as well as the shared WS clocks.
  function dialogueVisit() {
    return JSON.stringify([runId.value, run.value.attempt,
      run.value.clarify?.nodeId, run.value.clarify?.iteration,
      Object.entries(run.value.clarifyByNode ?? {}).map(([id, slot]) => [id, slot.iteration]),
      Object.entries(run.value.nodeRuns ?? {}).map(([id, node]) => [id, node.iteration, node.startedAt])])
  }

  function captureReviewSnapshot() {
    return { runId: runId.value, visit: dialogueVisit(), state: reviewState.capture() }
  }

  function currentReviewRead(capture: ReturnType<typeof captureReviewSnapshot>, nodeId?: string) {
    return capture.runId === runId.value && capture.visit === dialogueVisit() &&
      reviewState.isCurrent(capture.state, nodeId)
  }

  function projectReviewFrame(frame: ReviewSessionFrame, restore = false) {
    const nodeId = frame.nodeId
    if (!nodeId) return
    const session = reviewState.session(nodeId)
    if (session) {
      liveBusy[nodeId] = !!session.busy
      if (!session.busy) busySeedRetry.stop()
    }
    // ClarifyChat draws the dialogue state; only a refused send is its to handle.
    if (!restore && frame.event === 'request_rejected' && showsDialogue(nodeId)) {
      dialogueChat(nodeId)?.applyRequestRejected?.(frame)
    }
    if (!restore || run.value.gate?.reactUpstreamNodeId === nodeId) {
      gateApprovalRef.value?.applyReviewFrame?.(frame)
    }
  }

  async function projectDialogueAfterLoad(
    r: Pick<Run, 'reactSessions' | 'reviewSessionGeneration'>,
    isCurrent = () => true,
  ) {
    const checkpoint = reviewState.capture()
    await nextTick()
    await nextTick()
    if (!isCurrent()) return
    restoreReactSessions(r, checkpoint)
  }

  function restoreReactSessions(
    r: Pick<Run, 'reactSessions' | 'reviewSessionGeneration'>,
    checkpoint?: ReviewRecoveryCheckpoint,
  ) {
    const frames = reviewState.reconcileSnapshot(r, checkpoint, dialogueNodes())
    if (!frames.length) return
    dialogueRailsFilled = false
    dialogueLiveIncremental = false
    const busyNodes: string[] = []
    for (const frame of frames) {
      projectReviewFrame(frame, true)
      if (frame.busy && frame.nodeId && run.value.gate?.reactUpstreamNodeId === frame.nodeId) busyNodes.push(frame.nodeId)
    }
    if (busyNodes.length) {
      void seedDialogueAcpAfterRestore(busyNodes)
    } else if (!dialogueNodes().some(nodeId => reviewState.session(nodeId)?.busy)) {
      busySeedRetry.stop()
      void nextTick(() => flushPendingDialogueAcp())
    }
  }

  /** Existing REST polling also repairs missed dialogue frames. Local busy is
   * display state; it never vetoes an authoritative server reconciliation. */
  function reconcileReviewSnapshot(snapshot: Run, capture = captureReviewSnapshot(), recoverProgress = true): Run | undefined {
    if (snapshot.id !== runId.value || !currentReviewRead(capture)) return
    const nodes = [...new Set([...dialogueNodes(), ...Object.keys(run.value.reactSessions ?? {}),
      ...Object.keys(snapshot.reactSessions ?? {})])]
    const authority = { ...snapshot, reactSessions: snapshot.reactSessions ?? {} }
    const frames = reviewState.reconcileSnapshot(authority, capture.state, nodes)
    const sessions = { ...run.value.reactSessions }
    for (const frame of frames) {
      if (!frame.nodeId) continue
      sessions[frame.nodeId] = reviewState.session(frame.nodeId)!
      projectReviewFrame(frame, true)
    }
    run.value = { ...run.value, reactSessions: sessions }
    const reconciled = { ...snapshot, reactSessions: sessions }
    if (recoverProgress) void reviewState.requestRecovery((_checkpoint, signal) => recoverDialogueProgress(signal))
    return reconciled
  }

  async function recoverDialogueProgress(signal?: AbortSignal) {
    // Snapshot reconciliation can advance active identity. Capture afterwards
    // and guard both the event-page write and the surface projection.
    const progress = captureReviewSnapshot()
    for (const nodeId of dialogueNodes()) {
      if (!reviewState.session(nodeId)?.busy) continue
      const isCurrent = () => !signal?.aborted && currentReviewRead(progress, nodeId)
      if (!await fetchNodeEvents(nodeId, { signal, isCurrent }) || !isCurrent() ||
        !eventPages[nodeId]?.events?.length) continue
      await seedDialogueNodeOnce(nodeId, isCurrent)
    }
  }

  /**
   * Seed-then-live: after busy slot rebuild, replay eventPages / nodeEvents into
   * the dialogue surface, then flush any ACP buffered during hard-load remount.
   * Busy + empty rails → periodic retry (g3).
   */
  async function seedDialogueAcpAfterRestore(nodeIds: string[]) {
    await nextTick()
    for (const nodeId of nodeIds) {
      await seedDialogueNodeOnce(nodeId)
    }
    await nextTick()
    flushPendingDialogueAcp()
    const stillEmptyBusy = nodeIds.filter(
      (nid) =>
        !!reviewState.session(nid)?.busy &&
        !dialogueRailsFilled &&
        !dialogueLiveIncremental,
    )
    if (stillEmptyBusy.length) {
      startDialogueBusySeedRetry(stillEmptyBusy)
    }
  }

  async function seedDialogueNodeOnce(nodeId: string, isCurrent = () => true): Promise<boolean> {
    if (run.value.gate?.reactUpstreamNodeId !== nodeId) return false
    const capture = captureReviewSnapshot()
    const current = () => isCurrent() && currentReviewRead(capture, nodeId)
    // Live seed rails are current-turn only (server AggregateLastTurnFrames /
    // timeline). Applied as absolute snapshot onto the live bubble — never
    // appended onto prior-turn transcript text.
    let events = eventPages[nodeId]?.events as AcpEvent[] | undefined
    if (!events?.length) {
      try {
        await reviewState.requestRecovery(async (_checkpoint, signal) => {
          if (current()) await fetchNodeEvents(nodeId, { signal, isCurrent: current })
        })
      } catch {
        return false
      }
      events = eventPages[nodeId]?.events as AcpEvent[] | undefined
    }
    if (!events?.length || !current()) return false
    if (!hasAcpContent(events)) return false
    const forGate = run.value.gate?.reactUpstreamNodeId === nodeId
    const result = deliverOrBufferDialogueAcp({
      forClarify: false,
      forGate,
      events,
      nodeId,
        applyClarify: () => false,
      applyGate: (evs) => {
        if (!gateApprovalRef.value?.applyAcpEvents) return false
        return gateApprovalRef.value.applyAcpEvents(evs) !== false
      },
    })
    if (result === 'buffer') {
      pendingDialogueAcp.push({ nodeId, events, busy: true })
      return false
    }
    dialogueRailsFilled = true
    return true
  }

  function startDialogueBusySeedRetry(nodeIds: string[]) {
    busySeedRetry.start(async (signal) => {
      await runBusySeedRetry({
        signal,
        // Platform session busy only — ACP sandbox busy=false must not idle out (g1.2).
        isBusy: () =>
          nodeIds.some(nid => !!reviewState.session(nid)?.busy),
        hasContent: () => dialogueRailsFilled,
        liveIncrementalReceived: () => dialogueLiveIncremental,
        seed: async () => {
          let any = false
          for (const nid of nodeIds) {
            if (!reviewState.session(nid)?.busy) continue
            if (await seedDialogueNodeOnce(nid)) any = true
          }
          flushPendingDialogueAcp()
          return any || dialogueRailsFilled
        },
      })
    })
  }

  function flushPendingDialogueAcp() {
    if (!pendingDialogueAcp.size) return
    const leftover: { nodeId?: string; events: AcpEvent[]; busy?: boolean }[] = []
    for (const frame of pendingDialogueAcp.takeAll()) {
      const nodeId = frame.nodeId || ''
        const forGate =
        !!run.value.gate?.reactUpstreamNodeId &&
        (run.value.gate.reactUpstreamNodeId === nodeId || !nodeId)
      const result = deliverOrBufferDialogueAcp({
        forClarify: false,
        forGate,
        events: frame.events,
        nodeId: nodeId || selClarify.value?.nodeId || run.value.gate?.reactUpstreamNodeId || '',
        applyClarify: () => false,
        applyGate: (events) => {
          if (!gateApprovalRef.value?.applyAcpEvents) return false
          return gateApprovalRef.value.applyAcpEvents(events) !== false
        },
      })
      if (result === 'applied') {
        if (hasAcpContent(frame.events)) dialogueRailsFilled = true
      }
      if (result === 'buffer') leftover.push(frame)
    }
    for (const frame of leftover) pendingDialogueAcp.push(frame)
  }

  /** Deliver ACP to dialogue surfaces or buffer until they remount. */
  function applyOrBufferDialogueAcp(
    nodeId: string,
    events: AcpEvent[],
    busy?: boolean,
  ) {
    const forGate = run.value.gate?.reactUpstreamNodeId === nodeId
    const result = deliverOrBufferDialogueAcp({
      forClarify: false,
      forGate,
      events,
      nodeId,
        applyClarify: () => false,
      applyGate: (evs) => {
        if (!gateApprovalRef.value?.applyAcpEvents) return false
        return gateApprovalRef.value.applyAcpEvents(evs) !== false
      },
    })
    if (result === 'applied') {
      if (hasAcpContent(events)) {
        dialogueRailsFilled = true
        dialogueLiveIncremental = true
      }
    }
    if (result === 'buffer') {
      pendingDialogueAcp.push({ nodeId, events, busy })
    }
  }

  watch(
    () => [llmTranscriptRef?.value?.getNodeId?.(), llmTranscriptRef?.value?.isChatReady?.()] as const,
    ([nodeId, ready]) => {
      if (nodeId && ready !== false) void projectDialogueAfterLoad(run.value)
    },
    { flush: 'post' },
  )

  function resetDialogueState() {
    reviewState.reset()
    pendingDialogueAcp.clear()
    dialogueRailsFilled = false
    dialogueLiveIncremental = false
    busySeedRetry.stop()
  }

  function teardownRealtime() {
    reviewState.reset()
    busySeedRetry.stop()
    wsReconnect.reset()
    heartbeat.stop()
    const socket = ws
    ws = undefined
    socket?.close()
    wsConnected = false
    if (timer) {
      window.clearInterval(timer)
      timer = undefined
    }
  }

  async function initAfterLoadSuccess(args: {
    applyDetailArtifactsDeepLink: () => boolean
    applyOutputDeepLinkFocus: () => boolean
    defaultNode: string | null | undefined
    syncAllMcpCallsFromRun: () => void
  }) {
    args.applyDetailArtifactsDeepLink()
    if (!args.applyOutputDeepLinkFocus() && !selected.value) selected.value = args.defaultNode || null
    args.syncAllMcpCallsFromRun()
    const dialogueSelected = selected.value && (showsDialogue(selected.value) ||
      run.value.gate?.reactUpstreamNodeId === selected.value)
    // Dialogue startup shares the same recovery lane as later snapshots.
    if (dialogueSelected) {
      void reviewState.requestRecovery((_checkpoint, signal) => recoverDialogueProgress(signal))
    } else {
      void rehydrateNodeEvents(selected.value)
    }
    fetchSandboxLog(selected.value)
    connectWs()
    if (!timer) {
      timer = window.setInterval(() => {
        const active =
          run.value.status === 'queued' ||
          run.value.status === 'running' ||
          run.value.status === 'waiting_human'
        if (!active) return
        // Hidden tab: skip the frequent 2s tick (g1.2 / f5).
        if (typeof document !== 'undefined' && document.hidden) return
        // Auto path is always soft + silent — never hard load (g1.3).
        if (isClarifySessionBusy()) {
          void patchRunChrome?.()
        } else {
          loadRun(false, true)
        }
        const sel = selected.value
        // Only skip REST once we already have displayable live events;
        // busy-only / empty live frames must keep the 2s poll fallback.
        const skipPoll =
          wsConnected &&
          !!sel &&
          liveNode.value === sel &&
          !!eventPages[sel]?.live &&
          (eventPages[sel]?.events.length || 0) > 0
        const dialogueOwnsProgress = !!sel && (showsDialogue(sel) || run.value.gate?.reactUpstreamNodeId === sel)
        if (!skipPoll && sel && !dialogueOwnsProgress) {
          const rh = rehydrateByNode[sel] || 'idle'
          // Do not auto-recover from error; keep polling only after ready.
          if (rh === 'error' || rh === 'loading') {
            /* stay put */
          } else if (rh === 'ready') {
            void fetchNodeEvents(sel)
          } else {
            void rehydrateNodeEvents(sel)
          }
        }
        if (nodeTab.value === 'sandbox') {
          fetchSandboxLog(selected.value, { intent: 'silent_poll' })
        }
        // Boot-stage empty state needs fresh sandbox row status/containerStatus.
        maybePollSandboxForBoot()
      }, 2000)
    }
  }

  function connectWs(opts?: { fromReconnect?: boolean }) {
    const id = runId.value
    if (!id) return
    if (opts?.fromReconnect) {
      const socket = ws
      ws = undefined
      wsConnected = false
      heartbeat.stop()
      socket?.close()
    }
    wsReconnect.markConnecting()
    let socket: WebSocket
    let dialogueRevision = 0
    let receivedSnapshot = false
    try {
      socket = new WebSocket(api.runEventsWsUrl(id))
      ws = socket
      heartbeat.start()
    } catch {
      wsReconnect.onClose()
      return
    }
    socket.onopen = () => {
      if (ws !== socket) return
      wsConnected = true
      wsReconnect.markOpened()
      heartbeat.touch()
    }
    socket.onclose = () => {
      if (ws !== socket) return
      wsConnected = false
      ws = undefined
      heartbeat.stop()
      wsReconnect.onClose()
    }
    socket.onmessage = (ev) => {
      if (ws !== socket) return
      heartbeat.touch()
      let m: any
      try {
        m = JSON.parse(ev.data)
      } catch {
        return
      }
      if (m.type === 'dialogue') {
        ingestDialogue(m)
        return
      }
      if (m.type === 'node_enter') {
        reviewState.noteVisit(m.nodeId || '')
        applySidebarEnter(runId.value, m, run.value?.nodes)
        return
      }
      if (m.type === 'status') noteSidebarRunStatus(runId.value, m.status)
      if (m.type === 'snapshot' && (m.run?.reactSessions || m.run?.id === id)) {
        // A full run DTO omits the map once its last FIFO has gone idle.
        const sessions = m.run.reactSessions ?? {}
        if (!receivedSnapshot) {
          receivedSnapshot = true
          const versioned = reviewState.hasVersion || typeof m.run.reviewSessionGeneration === 'number' || Object.values(sessions).some((snap: any) => typeof snap?.sessionGeneration === 'number')
          reviewState.reset()
          if (versioned) {
            const nodeId = run.value.gate?.reactUpstreamNodeId
            if (nodeId) gateApprovalRef.value?.applyReviewFrame?.({ event: 'session_reset', nodeId })
          }
        }
        const restored = reviewState.reconcileSnapshot({ reactSessions: sessions,
          reviewSessionGeneration: m.run.reviewSessionGeneration }, undefined, dialogueNodes())
        const restoredSessions = { ...run.value.reactSessions }
        for (const frame of restored) {
          if (frame.nodeId) restoredSessions[frame.nodeId] = reviewState.session(frame.nodeId)!
        }
        const revision = ++dialogueRevision
        const nodeId = selClarify.value?.nodeId || run.value.gate?.reactUpstreamNodeId
        const sameDialogue = () =>
          ws === socket &&
          (selClarify.value?.nodeId || run.value.gate?.reactUpstreamNodeId) === nodeId
        const isCurrent = () => sameDialogue() && dialogueRevision === revision
        run.value = { ...run.value, reactSessions: restoredSessions }
        // queue_state → seed → flush → live (parity with Inbox g4.2).
        void projectDialogueAfterLoad(run.value, isCurrent).then(() => {
          // The disconnected interval may include turn_done and new questions.
          // Restore the saved dialogue after its current producer becomes idle.
          if (!sameDialogue() || !opts?.fromReconnect) return
          const snap = nodeId ? run.value.reactSessions?.[nodeId] : undefined
          if (
            nodeId && !snap?.busy && !snap?.waiting && !snap?.activeItem
          ) {
            loadRun(false, true)
          }
        })
        return
      }
      if (m.type === 'acp' && m.nodeId) {
        const wsEvents: AcpEvent[] = Array.isArray(m.events) ? m.events : []
        reviewState.noteAcp(m.nodeId, wsEvents)
        mergeLiveWsAcpPage(m.nodeId, wsEvents)
        if (typeof m.busy === 'boolean') {
          // LiveLog / sandbox bridge only. Do not stop dialogue busySeedRetry and
          // do not treat ACP busy=false as platform session idle (g1.2).
          liveBusy[m.nodeId] = m.busy
        }
        liveNode.value = m.nodeId
        if (!manual.value) selected.value = m.nodeId
        // Dialogue-surface stream (ClarifyChat / GateApproval), not only LiveLog.
        applyOrBufferDialogueAcp(m.nodeId, wsEvents, m.busy)
      } else if (m.type === 'review' && m.nodeId) {
        if (!reviewState.acceptServer(m)) return
        const session = reviewState.session(m.nodeId)
        if (session) {
          run.value = { ...run.value, reactSessions: { ...run.value.reactSessions, [m.nodeId]: session } }
        }
        if (
          m.nodeId === selClarify.value?.nodeId ||
          m.nodeId === run.value.gate?.reactUpstreamNodeId
        ) dialogueRevision++
        projectReviewFrame(m)
        if (m.event === 'turn_done' || m.event === 'error') {
          liveBusy[m.nodeId] = false
          busySeedRetry.stop()
          // Session is idle: loadRun writes back persisted clarify turns when
          // the transcript fingerprint changed (plan g1.2). A shorter snapshot
          // than a still-streaming local row is held inside fetchRunData.
          loadRun(false, true)
        }
      } else if (
        m.type === 'trace' ||
        m.type === 'status' ||
        m.type === 'react' ||
        m.type === 'artifact_edit'
      ) {
        if (m.type === 'artifact_edit') notifyRunArtifactEdit()
        // Auto path: never hard load (g1.3). Busy → chrome patch + shared preview entry (g1.1 / g2.1).
        // patchRunChrome still merges a longer idle transcript so an empty Agent
        // panel is not frozen while this gate remains true (plan g1.3).
        if (isClarifySessionBusy()) {
          void patchRunChrome?.()
          if (m.type === 'artifact_edit') refreshArtifactPreview?.(m)
          return
        }
        if (m.type === 'status') liveNode.value = null
        loadRun(false, true)
        if (m.type === 'artifact_edit') refreshArtifactPreview?.(m)
      }
    }
  }

  return {
    pendingDialogueAcp,
    captureReviewSnapshot,
    requestReviewRecovery: reviewState.requestRecovery,
    recoverDialogueProgress,
    reconcileReviewSnapshot,
    projectDialogueAfterLoad,
    seedDialogueAcpAfterRestore,
    applyOrBufferDialogueAcp,
    resetDialogueState,
    teardownRealtime,
    initAfterLoadSuccess,
    connectWs,
    get wsConnected() {
      return wsConnected
    },
  }
}
