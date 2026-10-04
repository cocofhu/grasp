/**
 * Run 详情：WS 连接、对话面 ACP 再水合（seed/buffer/busy-retry）、reconnect。
 */
import { nextTick, type ComputedRef, type Ref } from 'vue'
import { api } from '@/lib/api/api'
import { createPendingAcpBuffer, pickAcpRails } from '@/lib/run/pendingAcpBuffer'
import { deliverOrBufferDialogueAcp } from '@/lib/run/dialogueAcpDelivery'
import {
  createBusySeedRetryController,
  runBusySeedRetry,
} from '@/lib/run/busySeedRetry'
import { createWsReconnectController } from '@/lib/run/wsReconnect'
import type { AcpEvent, Run } from '@/lib/shared/types'
import type { EventPageState } from '@/lib/run/useRunDetailLiveLog'

type DialogueSurface = {
  applyReviewFrame?: (frame: any) => boolean | void
  applyAcpEvents?: (events: AcpEvent[] | undefined, nodeId?: string) => boolean | void
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
  gateApprovalRef: Ref<GateSurface>
  selClarify: ComputedRef<{ nodeId: string } | null>
  fetchNodeEvents: (nodeId: string | null, opts?: { signal?: AbortSignal }) => Promise<boolean>
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

  /**
   * ACP frames that arrived while ClarifyChat / GateApproval was unmounted
   * during hard load. Flushed after queue_state rebuilds the streaming slot.
   */
  const pendingDialogueAcp = createPendingAcpBuffer()
  /** Rails applied via seed or live — stops busy seed retry (g3). */
  let dialogueRailsFilled = false
  let dialogueLiveIncremental = false
  /**
   * Platform review/clarify session busy per node (queue_state / turn_begin /
   * reactSessions). Separate from liveBusy which also tracks ACP sandbox dips
   * for LiveLog — dialogue seed retry must not go idle on ACP busy=false (g1.2).
   */
  const dialoguePlatformBusy: Record<string, boolean> = Object.create(null)
  const busySeedRetry = createBusySeedRetryController()
  const wsReconnect = createWsReconnectController({
    connect: () => connectWs({ fromReconnect: true }),
    shouldReconnect: () =>
      !!runId.value &&
      (run.value.status === 'running' || run.value.status === 'waiting_human'),
  })

  let timer: number | undefined
  let ws: WebSocket | undefined
  let wsConnected = false

  /**
   * Align Inbox hard-load: multi nextTick so ReviewComposer/GateApproval mount
   * before queue_state → seed → flush → live (g2.1).
   */
  async function projectDialogueAfterLoad(r: { reactSessions?: Record<string, any> }) {
    dialogueRailsFilled = false
    dialogueLiveIncremental = false
    await nextTick()
    await nextTick()
    restoreReactSessions(r)
  }

  function restoreReactSessions(r: { reactSessions?: Record<string, any> }) {
    const sessions = r.reactSessions
    if (!sessions) return
    const busyNodes: string[] = []
    for (const [nodeId, snap] of Object.entries(sessions)) {
      if (!snap || typeof snap !== 'object') continue
      liveBusy[nodeId] = !!snap.busy
      dialoguePlatformBusy[nodeId] = !!snap.busy
      if (snap.busy) busyNodes.push(nodeId)
      const frame = {
        event: 'queue_state',
        nodeId,
        waiting: snap.waiting ?? 0,
        items: snap.items ?? [],
        busy: !!snap.busy,
        activeItem: snap.activeItem,
      }
      if (selClarify.value?.nodeId === nodeId) {
        const ok = reviewChatRef.value?.applyReviewFrame?.(frame)
        if (ok === false) {
          /* inner ClarifyChat not ready — seed path still buffers ACP */
        }
      }
      // Gate hot-revise shares the producer reactSessions key.
      if (run.value.gate?.reactUpstreamNodeId === nodeId) {
        gateApprovalRef.value?.applyReviewFrame?.(frame)
      }
    }
    if (busyNodes.length) {
      void seedDialogueAcpAfterRestore(busyNodes)
    } else {
      // Authority not busy (f3): surfaces already tore down empty placeholders.
      busySeedRetry.stop()
      void nextTick(() => flushPendingDialogueAcp())
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
        (!!dialoguePlatformBusy[nid] || !!liveBusy[nid]) &&
        !dialogueRailsFilled &&
        !dialogueLiveIncremental,
    )
    if (stillEmptyBusy.length) {
      startDialogueBusySeedRetry(stillEmptyBusy)
    }
  }

  async function seedDialogueNodeOnce(nodeId: string): Promise<boolean> {
    // Live seed rails are current-turn only (server AggregateLastTurnFrames /
    // timeline). Applied as absolute snapshot onto the live bubble — never
    // appended onto prior-turn transcript text.
    let events = eventPages[nodeId]?.events as AcpEvent[] | undefined
    if (!events?.length) {
      try {
        await fetchNodeEvents(nodeId)
      } catch {
        return false
      }
      events = eventPages[nodeId]?.events as AcpEvent[] | undefined
    }
    if (!events?.length) return false
    const rails = pickAcpRails(events)
    if (!rails.thought && !rails.message) return false
    const forClarify = selClarify.value?.nodeId === nodeId
    const forGate = run.value.gate?.reactUpstreamNodeId === nodeId
    const result = deliverOrBufferDialogueAcp({
      forClarify,
      forGate,
      events,
      nodeId,
      applyClarify: (evs, nid) => {
        if (!reviewChatRef.value?.applyAcpEvents) return false
        return reviewChatRef.value.applyAcpEvents(evs, nid)
      },
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
          nodeIds.some(
            (nid) =>
              !!dialoguePlatformBusy[nid] ||
              !!run.value.reactSessions?.[nid]?.busy ||
              !!reviewChatRef.value?.isSessionBusy?.(),
          ),
        hasContent: () => dialogueRailsFilled,
        liveIncrementalReceived: () => dialogueLiveIncremental,
        seed: async () => {
          let any = false
          for (const nid of nodeIds) {
            if (!dialoguePlatformBusy[nid] && !run.value.reactSessions?.[nid]?.busy) continue
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
      const forClarify = !!selClarify.value?.nodeId && (selClarify.value.nodeId === nodeId || !nodeId)
      const forGate =
        !!run.value.gate?.reactUpstreamNodeId &&
        (run.value.gate.reactUpstreamNodeId === nodeId || !nodeId)
      const result = deliverOrBufferDialogueAcp({
        forClarify,
        forGate,
        events: frame.events,
        nodeId: nodeId || selClarify.value?.nodeId || run.value.gate?.reactUpstreamNodeId || '',
        applyClarify: (events, nid) => {
          if (!reviewChatRef.value?.applyAcpEvents) return false
          return reviewChatRef.value.applyAcpEvents(events, nid)
        },
        applyGate: (events) => {
          if (!gateApprovalRef.value?.applyAcpEvents) return false
          return gateApprovalRef.value.applyAcpEvents(events) !== false
        },
      })
      if (result === 'applied') {
        const rails = pickAcpRails(frame.events)
        if (rails.thought || rails.message) dialogueRailsFilled = true
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
    const forClarify = selClarify.value?.nodeId === nodeId
    const forGate = run.value.gate?.reactUpstreamNodeId === nodeId
    const result = deliverOrBufferDialogueAcp({
      forClarify,
      forGate,
      events,
      nodeId,
      applyClarify: (evs, nid) => {
        if (!reviewChatRef.value?.applyAcpEvents) return false
        return reviewChatRef.value.applyAcpEvents(evs, nid)
      },
      applyGate: (evs) => {
        if (!gateApprovalRef.value?.applyAcpEvents) return false
        return gateApprovalRef.value.applyAcpEvents(evs) !== false
      },
    })
    if (result === 'applied') {
      const rails = pickAcpRails(events)
      if (rails.thought || rails.message) {
        dialogueRailsFilled = true
        dialogueLiveIncremental = true
      }
    }
    if (result === 'buffer') {
      pendingDialogueAcp.push({ nodeId, events, busy })
    }
  }

  function resetDialogueState() {
    pendingDialogueAcp.clear()
    dialogueRailsFilled = false
    dialogueLiveIncremental = false
    for (const k of Object.keys(dialoguePlatformBusy)) delete dialoguePlatformBusy[k]
    busySeedRetry.stop()
  }

  function teardownRealtime() {
    busySeedRetry.stop()
    wsReconnect.markIntentionalClose()
    ws?.close()
    ws = undefined
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
    // Rehydrate regardless of which tab is active (default tab stays unchanged).
    void rehydrateNodeEvents(selected.value)
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
        if (!skipPoll && sel) {
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
      wsReconnect.markIntentionalClose()
      ws?.close()
      ws = undefined
      wsConnected = false
    }
    let socket: WebSocket
    try {
      socket = new WebSocket(api.runEventsWsUrl(id))
      ws = socket
    } catch {
      wsReconnect.onClose()
      return
    }
    socket.onopen = () => {
      if (ws !== socket) return
      wsConnected = true
      wsReconnect.markOpened()
      // Reconnect: force same-depth re-seed as hard refresh (g4.2).
      if (opts?.fromReconnect && run.value.reactSessions) {
        void projectDialogueAfterLoad(run.value)
      }
    }
    socket.onclose = () => {
      if (ws !== socket) return
      wsConnected = false
      ws = undefined
      wsReconnect.onClose()
    }
    socket.onmessage = (ev) => {
      if (ws !== socket) return
      let m: any
      try {
        m = JSON.parse(ev.data)
      } catch {
        return
      }
      if (m.type === 'snapshot' && m.run?.reactSessions) {
        run.value = { ...run.value, reactSessions: m.run.reactSessions }
        // queue_state → seed → flush → live (parity with Inbox g4.2).
        void projectDialogueAfterLoad(run.value)
        return
      }
      if (m.type === 'acp' && m.nodeId) {
        const wsEvents: AcpEvent[] = m.events || []
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
        // Prefer matching producer; components also filter by nodeId defensively.
        // Platform session busy (review) is the authority for dialogue seed retry.
        if (m.event === 'turn_begin') {
          liveBusy[m.nodeId] = true
          dialoguePlatformBusy[m.nodeId] = true
        }
        if (m.event === 'queue_state' && typeof m.busy === 'boolean') {
          liveBusy[m.nodeId] = !!m.busy
          dialoguePlatformBusy[m.nodeId] = !!m.busy
          if (!m.busy) busySeedRetry.stop()
        }
        if (!selClarify.value || selClarify.value.nodeId === m.nodeId) {
          reviewChatRef.value?.applyReviewFrame?.(m)
        }
        gateApprovalRef.value?.applyReviewFrame?.(m)
        if (m.event === 'turn_done' || m.event === 'error') {
          liveBusy[m.nodeId] = false
          dialoguePlatformBusy[m.nodeId] = false
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
