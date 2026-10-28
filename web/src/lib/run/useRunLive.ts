import { computed, nextTick, onBeforeUnmount, ref, watch, type Ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api/api'
import { useToast } from '@/lib/composables/useToast'
import { isNodeEventsUnavailable } from '@/lib/run/nodeEventsResponse'
import { nodeConversationPath } from '@/lib/run/nodeConversation'
import { notifyRecentActivity, recentCurrentNode, runDetailPath, runLivePath } from '@/lib/run/recentRuns'
import { applySidebarEnter, armSidebarHandoff, clearSidebarHandoff, noteSidebarRunStatus } from '@/lib/inbox/sidebarHandoff'
import { isPlumbingNode } from '@/lib/inbox/runConversationChain'
import { resolveHandoffProcessAuto, sidebarAutoTarget } from '@/lib/inbox/sidebarRunRows'
import { bootKey, markBootLive } from '@/lib/run/bootRecord'
import {
  activeSegment,
  chainBootPending,
  clipTask,
  normalizeRunChain,
  priorSegments as chainPriorSegments,
  runChainFromRun,
  seedChainBoots,
  type ChainSegment,
  type RunChain,
} from '@/lib/inbox/runConversationChain'
import { decideLive, takeoverConflictRunStatus, takeoverErrorCode, takeoverRaceLost, type LiveFrame } from '@/lib/run/runLive'
import { createWsReconnectController } from '@/lib/run/wsReconnect'
import { createWsHeartbeat } from '@/lib/run/wsHeartbeat'
import { createReviewStateCursor } from '@/lib/chat/sessionQueue'
import { splitTurns } from '@/lib/run/llmTranscript'
import { clarifyTurnsFromLlm } from '@/lib/run/llmTranscriptThread'
import type { AcpEvent, ClarifyImage, ClarifyTurn, ReactAnnotation, Run, RunCurrentNode } from '@/lib/shared/types'

/** The ReviewComposer / ClarifyChat surface this view drives. */
export interface LiveChatSurface {
  applyReviewFrame: (frame: Record<string, unknown>) => boolean | void
  applyAcpEvents: (events: AcpEvent[] | undefined, nodeId?: string) => boolean | void
  releaseConfirmPending?: () => void
}

export const LIVE_POLL_MS = 4000
const REFRESH_DEBOUNCE_MS = 400

/** What the live page shows for the node on screen. */
/** Why the page ended in place: the run waits at a gate, or the run itself ended. */
export type LiveEndState = { kind: 'gate'; label: string } | { kind: 'run'; status: string }
export type LiveMode = 'observe' | 'chat'
/**
 * Hand-off row after 确认并流转 on this page (same shape as ClarifyChat's).
 * targetAuto is the boot-gate three-state: true automatic, false already not
 * automatic, absent while the next process is still unknown.
 */
export type LiveHandoff = { phase: 'pending' | 'done'; at: string; target?: string; targetAuto?: boolean }

/**
 * Live view of a run: read-only stream of an auto node (nodeEvents seed + run
 * WS acp frames) and Stop → POST takeover → the node's chat (re-review with
 * 确认并流转). Once a node is on screen the page never navigates by itself and
 * the conversation continues in place (#850): when the node finishes and the
 * run moves to its next node, earlier nodes fold above (conversation-chain),
 * then a separator and the next node's task, then its stream (next node auto)
 * or its chat (next node waiting for a person: reply and 确认并流转 right here).
 * A gate, or the end of the run, ends the page in place (`ended`). Only the
 * user's Stop, or the observed node itself being parked for a person, opens
 * the /gates chat.
 */
export function useRunLive(runId: Ref<string>, requestedNode: Ref<string>, chat: Ref<LiveChatSurface | null>) {
  const router = useRouter()
  const route = useRoute()
  const toast = useToast()
  const { t } = useI18n()

  const run = ref<Run | null>(null)
  const loading = ref(true)
  const loadError = ref(false)
  const nodeId = ref('')
  const iteration = ref(1)
  const stopping = ref(false)
  /** The node on screen finished; the view stays here, read-only. */
  const ended = ref(false)
  /** Node task preview (chain task / currentNode.task / nodeEvents task), shown as the opening User Message. */
  const task = ref('')
  /** observe: read-only auto stream; chat: the next node's conversation continued in place. */
  const mode = ref<LiveMode>('observe')
  const chatKind = ref<'clarify' | 'review'>('review')
  const nodeLabel = ref('')
  const chain = ref<RunChain | null>(null)
  const handoff = ref<LiveHandoff | null>(null)
  const confirmError = ref<string | null>(null)
  /** Chats confirmed on this page stay expanded above, like /gates. */
  const opened = ref<string[]>([])
  const priorSegments = computed(() =>
    chainPriorSegments(chain.value, nodeId.value, iteration.value).map((s: ChainSegment) => ({
      ...s,
      open: opened.value.includes(`${s.nodeId}:${s.iteration}`),
    })),
  )
  const completedEvents = ref<AcpEvent[] | null>(null)
  const turns = computed<ClarifyTurn[]>(() => {
    if (mode.value === 'chat') return run.value?.clarifyByNode?.[nodeId.value]?.turns ?? []
    if (!ended.value) return []
    const execution = run.value?.nodeExecutions?.[nodeId.value]?.find((row) => (row.iteration || 1) === iteration.value)
      ?? run.value?.nodeRuns?.[nodeId.value]
    const events = completedEvents.value?.some((event) => event.kind === 'turn_end')
      ? completedEvents.value
      : execution?.events?.some((event) => event.kind === 'turn_end')
        ? execution.events
        : completedEvents.value ?? execution?.events
    // Per-turn turn_end carries the reported usage. Execution totals may span
    // several turns and must not be attached to a single visible reply.
    return clarifyTurnsFromLlm(splitTurns(events)).filter((row) => row.role === 'agent')
  })
  /**
   * Locked only after confirm succeeds, while the same composer follows a
   * non-waiting node. `handoff.pending` (产物校验中) must not lock typing.
   */
  const composerLocked = computed(() => mode.value === 'chat' && handoff.value?.phase === 'done')
  let chatBusy = false
  const endState = ref<LiveEndState | null>(null)

  let ws: WebSocket | null = null
  let wsRun = ''
  /** `${run}:${node}` this page wrote into ?node= itself (not a new route). */
  let selfRoute = ''
  let pollTimer: number | undefined
  let refreshTimer: number | undefined
  let disposed = false
  let loadGen = 0
  // A visit changes even when navigation returns to the same run/node (A→B→A).
  let visitEpoch = 0
  let seedRequest = 0
  let chainRequest = 0
  let completedRequest = 0
  let seededKey = ''
  /**
   * The surface instance the current node was seeded on. RunLiveView re-keys
   * the surface per node, so after continueTo() the next node's stream must be
   * seeded on the new instance, never on the one being unmounted.
   */
  let seededSurface: LiveChatSurface | null = null
  let lastEvents: AcpEvent[] | null = null
  /** Latest snapshot of a node not on screen yet (the run's next node starts streaming before the page switches). */
  let aheadEvents: { nodeId: string; events: AcpEvent[] } | null = null
  /** A node has been shown on this page: from now on, no automatic navigation. */
  let shown = false
  let dialogueRevision = 0
  let acpRevision = 0
  const reviewCursor = createReviewStateCursor()
  const reconnect = createWsReconnectController({
    connect: () => connect(runId.value),
    shouldReconnect: () => !!runId.value && shown && !disposed && !ended.value,
  })
  const heartbeat = createWsHeartbeat({
    onTimeout: () => {
      const socket = ws
      ws = null
      heartbeat.stop()
      socket?.close()
      reconnect.onClose()
    },
  })

  function currentVisit(visit: number, id: string, node?: string, iter?: number) {
    return !disposed && visit === visitEpoch && id === runId.value &&
      (node === undefined || node === nodeId.value) &&
      (iter === undefined || iter === iteration.value)
  }

  function leaveTo(target: Parameters<typeof router.replace>[0]) {
    if (disposed) return
    disposed = true
    stop()
    void router.replace(target)
  }

  function goToChat(node: string) {
    notifyRecentActivity()
    const query: Record<string, string> = {}
    for (const key of ['wf', 'tag'] as const) {
      const raw = route.query[key]
      const value = Array.isArray(raw) ? raw[0] : raw
      if (typeof value === 'string' && value) query[key] = value
    }
    const path = nodeConversationPath(runId.value, node)
    leaveTo(Object.keys(query).length ? { path, query } : { path })
  }

  /** The node on screen finished: stop streaming and stay on this page. */
  function finish() {
    if (ended.value) return
    ended.value = true
    stopping.value = false
    stop()
    chat.value?.applyReviewFrame({ event: 'turn_done', nodeId: nodeId.value })
    if (mode.value === 'observe') void refreshCompletedEvents()
    notifyRecentActivity()
  }

  async function refreshCompletedEvents() {
    const visit = visitEpoch
    const request = ++completedRequest
    const id = runId.value
    const node = nodeId.value
    const iter = iteration.value
    try {
      const page = await api.nodeEvents(id, node)
      if (!currentVisit(visit, id, node, iter) || request !== completedRequest || !ended.value || isNodeEventsUnavailable(page)) return
      if (page.events?.length) completedEvents.value = page.events
    } catch {
      /* The terminal run snapshot still supplies its persisted turn metadata. */
    }
  }

  /** Earlier nodes of this run (folded above) and the current node's task. */
  async function loadChain() {
    const visit = visitEpoch
    const request = ++chainRequest
    const id = runId.value
    const node = nodeId.value
    const iter = iteration.value
    const graph = run.value?.nodes
    let c: RunChain | null = null
    try {
      c = normalizeRunChain(await api.runConversationChain(id), graph)
    } catch (e: unknown) {
      // Same rule as /gates: only a 404 (older backend) falls back to the run graph.
      if (currentVisit(visit, id, node, iter) && (e as { status?: number })?.status === 404 && run.value) c = runChainFromRun(run.value)
    }
    if (!c || !currentVisit(visit, id, node, iter) || request !== chainRequest) return
    seedChainBoots(c)
    chain.value = c
    const seg = activeSegment(c, nodeId.value, iteration.value)
    if (seg?.task) task.value = clipTask(seg.task)
    if (seg?.label && !nodeLabel.value) nodeLabel.value = seg.label
  }

  /**
   * The run moved to its next node while this page shows one: continue the
   * same conversation in place (no navigation). The surface re-keys on the
   * node, so the stream / chat restarts below the folded earlier nodes.
   */
  function continueTo(next: string, m: LiveMode, c: RunCurrentNode | null) {
    ended.value = false
    endState.value = null
    stopping.value = false
    handoff.value = null
    confirmError.value = null
    chatBusy = false
    nodeId.value = next
    iteration.value = c?.iteration || 1
    nodeLabel.value = (typeof c?.name === 'string' && c.name.trim()) || ''
    mode.value = m
    chatKind.value = c?.kind === 'clarify' ? 'clarify' : 'review'
    seededKey = ''
    seededSurface = null
    seedRequest++
    completedEvents.value = null
    lastEvents = aheadEvents?.nodeId === next ? aheadEvents.events : null
    aheadEvents = null
    task.value = typeof c?.task === 'string' ? c.task.trim() : ''
    shown = true
    syncUrlNode(next)
    notifyRecentActivity()
    connect(runId.value)
    void loadChain()
    // No seed now: the surface still on screen belongs to the previous node.
    // The re-keyed surface is seeded once mounted (watch(chat) below, or after
    // this render); ACP frames that arrive before that are kept in lastEvents
    // and replayed on it.
    if (m === 'observe') void nextTick(() => seed())
  }

  /**
   * Keep ?node= on the node on screen so a refresh lands there. Same path, so
   * the view is not remounted; replace, so Back does not step through nodes.
   * The route watcher below skips this change (the page is already there).
   */
  function syncUrlNode(next: string) {
    if (requestedNode.value === next) return
    selfRoute = `${runId.value}:${next}`
    const query = { ...router.currentRoute.value.query, node: next }
    void router.replace({ path: runLivePath(runId.value), query })
  }

  /**
   * Route on the server's run.currentNode (GET /runs/:id or the WS snapshot
   * frame). follow / detail / another node's chat only route the page while
   * it is opening; after a node was shown the run is followed in place, and
   * a gate or the end of the run ends it in place.
   */
  function applyRun(r: Run, id: string, fromSnapshot = false) {
    if (disposed || id !== runId.value || (r.id && r.id !== id) || ended.value) return
    const d = decideLive(r, requestedNode.value)
    const c = recentCurrentNode(r)
    if (shown && mode.value === 'chat' && d.kind === 'chat' && d.nodeId === nodeId.value) {
      // Reconnect independently of chatBusy: turn_done may have been lost.
      connect(id)
      if (fromSnapshot) {
        const session = r.reactSessions?.[nodeId.value]
        if (!reviewCursor.accept({ ...session, nodeId: nodeId.value })) return
        chatBusy = !!session?.busy
        chat.value?.applyReviewFrame({
          ...session,
          event: 'queue_state',
          nodeId: nodeId.value,
          busy: chatBusy,
          waiting: session?.waiting ?? 0,
          items: session?.items ?? [],
        })
      }
    }
    // Mid-turn in the continued chat: keep the live stream (refresh on turn_done).
    if (shown && mode.value === 'chat' && chatBusy && d.kind === 'chat' && d.nodeId === nodeId.value) return
    run.value = r
    loadError.value = false
    // Between nodes after a node was shown: stay and wait for the next one.
    if (shown && !c && (
      r.status === 'running' || r.status === 'queued' ||
      (r.status === 'waiting_human' && r.currentNode?.state === 'entering')
    )) return
    if (d.kind === 'chat') {
      if (!shown) return goToChat(d.nodeId)
      if (d.nodeId === nodeId.value) {
        if (mode.value === 'chat') return
        // The observed node itself was parked for a person (Stop elsewhere).
        return goToChat(d.nodeId)
      }
      if (c?.kind === 'gate') {
        const label = (typeof c.name === 'string' && c.name.trim()) || String(r.nodes?.find((n) => n.id === c.id)?.label || '') || c.id
        endState.value = { kind: 'gate', label }
        return finish()
      }
      return continueTo(d.nodeId, 'chat', c)
    }
    if (d.kind === 'detail') {
      if (!shown) return leaveTo(runDetailPath(id))
      endState.value = { kind: 'run', status: String(r.status || '') }
      return finish()
    }
    if (d.kind === 'follow') {
      // Shown already: only a node the page is not showing yet continues
      // (a poll racing the ?node= update still reads 「follow」 for this one).
      if (shown) return d.nodeId === nodeId.value ? observeCurrent(c) : continueTo(d.nodeId, 'observe', c)
      notifyRecentActivity()
      stopping.value = false
      void router.replace({ path: runLivePath(id), query: { node: d.nodeId } })
      return
    }
    if (shown && nodeId.value !== d.nodeId) return continueTo(d.nodeId, 'observe', c)
    applyObserve(d, id)
  }

  /** Same node still streaming (a poll / snapshot after the page continued in place). */
  function observeCurrent(c: RunCurrentNode | null) {
    if (c?.state === 'stopping') stopping.value = true
    connect(runId.value)
    void seed()
  }

  function applyObserve(d: Extract<ReturnType<typeof decideLive>, { kind: 'observe' }>, id: string) {
    if (nodeId.value !== d.nodeId) {
      nodeId.value = d.nodeId
      seededKey = ''
      seededSurface = null
      seedRequest++
      lastEvents = null
      task.value = ''
    }
    const first = !shown
    shown = true
    if (d.task) task.value = d.task
    if (d.iteration != null) iteration.value = d.iteration
    if (first) void loadChain()
    if (d.stopping) stopping.value = true
    connect(id)
    void seed()
  }

  async function load() {
    if (ended.value) return
    const gen = ++loadGen
    const visit = visitEpoch
    const id = runId.value
    const revision = dialogueRevision
    try {
      const r = await api.getRun(id)
      if (gen !== loadGen || !currentVisit(visit, id) || revision !== dialogueRevision) return
      applyRun(r, id)
    } catch {
      if (gen === loadGen && currentVisit(visit, id)) loadError.value = !run.value
    } finally {
      if (gen === loadGen && currentVisit(visit, id)) loading.value = false
    }
  }

  function scheduleRefresh() {
    if (refreshTimer) return
    refreshTimer = window.setTimeout(() => {
      refreshTimer = undefined
      void load()
    }, REFRESH_DEBOUNCE_MS)
  }

  function isSeeded() {
    return !!chat.value && seededSurface === chat.value && seededKey === `${runId.value}:${nodeId.value}:${iteration.value}`
  }

  /** Open the live bubble, then seed it from the node's current turn snapshot. */
  async function seed(force = false) {
    const visit = visitEpoch
    const id = runId.value
    const node = nodeId.value
    const iter = iteration.value
    if (!node || !run.value || (run.value.id && run.value.id !== id) || ended.value) return
    const key = `${id}:${node}:${iter}`
    const surface = chat.value
    const seedMode = mode.value
    const alreadySeeded = seededKey === key && seededSurface === surface
    if (!surface || (!force && alreadySeeded) || (mode.value !== 'observe' && (!force || !chatBusy))) return
    const request = ++seedRequest
    seededKey = key
    seededSurface = surface
    if (mode.value === 'observe' && !alreadySeeded) {
      surface.applyReviewFrame({ event: 'queue_state', nodeId: nodeId.value, waiting: 0, items: [], busy: true })
      surface.applyReviewFrame({ event: 'turn_begin', nodeId: nodeId.value, item: { text: '' } })
      if (lastEvents) surface.applyAcpEvents(lastEvents, nodeId.value)
    }
    const revision = acpRevision
    try {
      const r = await api.nodeEvents(id, node)
      if (!currentVisit(visit, id, node, iter) || request !== seedRequest || ended.value ||
        mode.value !== seedMode || (mode.value === 'chat' && !chatBusy) ||
        seededKey !== key || surface !== chat.value || isNodeEventsUnavailable(r)) return
      // A WS frame newer than the snapshot may already be on screen.
      // Top-level task on the first (non-paged) page only.
      const sent = 'task' in r ? r.task : undefined
      if (!task.value && typeof sent === 'string' && sent.trim()) task.value = sent.trim()
      if (r.events?.length && ((!lastEvents && !force) || (force && revision === acpRevision))) {
        lastEvents = r.events
        surface.applyAcpEvents(r.events, node)
      }
    } catch {
      /* stream frames still arrive over WS */
    }
  }

  function onFrame(m: LiveFrame) {
    const frameRun = (m as { runId?: unknown }).runId
    if (typeof frameRun === 'string' && frameRun && frameRun !== runId.value) return
    if (m.type === 'snapshot') {
      // Same run detail DTO as GET /runs/:id, currentNode included.
      const r = (m as { run?: Run }).run
      if (r && typeof r === 'object' && (!r.id || r.id === runId.value)) {
        loadGen++ // a GET still in flight is older than this frame
        applyRun(r, runId.value, true)
      }
      return
    }
    if (m.type === 'acp') {
      acpRevision++
      if (m.nodeId !== nodeId.value) {
        if (m.nodeId) {
          const ahead = Array.isArray(m.events) ? (m.events as AcpEvent[]) : []
          if (ahead.length) aheadEvents = { nodeId: String(m.nodeId), events: ahead }
          scheduleRefresh()
        }
        return
      }
      const events = Array.isArray(m.events) ? (m.events as AcpEvent[]) : []
      if (events.length && observeBooting()) {
        // The Agent is streaming: boot is over; the chain brings its ready time.
        markBootLive(bootKey(runId.value, nodeId.value, iteration.value))
        void loadChain()
      }
      if (events.length) {
        // Snapshots are cumulative: keep the latest for a surface not seeded yet.
        lastEvents = events
        if (mode.value === 'chat' || isSeeded()) chat.value?.applyAcpEvents(events, nodeId.value)
      }
      if (m.busy === false) scheduleRefresh()
      return
    }
    if (m.type === 'node_enter') {
      const frame = m as { nodeId?: string; name?: string; mode?: string }
      applySidebarEnter(runId.value, frame, run.value?.nodes)
      settleLiveHandoffFromEnter(frame)
      return
    }
    if (m.type === 'node_takeover') {
      if (m.nodeId !== nodeId.value) return
      notifyRecentActivity()
      if (m.state === 'waiting_human') goToChat(nodeId.value)
      else stopping.value = true
      return
    }
    if (m.type === 'review' && mode.value === 'chat') {
      const f = m as Record<string, unknown>
      if (typeof f.nodeId === 'string' && f.nodeId && f.nodeId !== nodeId.value) return
      if (!reviewCursor.acceptServer(f)) return
      dialogueRevision++
      notifyRecentActivity()
      if (f.event === 'turn_begin') chatBusy = true
      if (f.event === 'queue_state' && typeof f.busy === 'boolean') chatBusy = f.busy
      chat.value?.applyReviewFrame(f)
      if (f.event === 'turn_done' || f.event === 'error') {
        chatBusy = false
        scheduleRefresh()
      }
      return
    }
    if (m.type === 'status' || m.type === 'trace' || m.type === 'review' || m.type === 'react') {
      if (m.type === 'status') noteSidebarRunStatus(runId.value, (m as { status?: unknown }).status)
      notifyRecentActivity()
      scheduleRefresh()
    }
  }

  function connect(id: string) {
    if (!id || disposed || ended.value) return
    if (ws && wsRun === id) return
    heartbeat.stop()
    const previous = ws
    ws = null
    previous?.close()
    const recovering = wsRun === id
    const visit = visitEpoch
    wsRun = id
    reconnect.markConnecting()
    let socket: WebSocket
    try {
      socket = new WebSocket(api.runEventsWsUrl(id))
    } catch {
      ws = null
      reconnect.onClose()
      return
    }
    ws = socket
    heartbeat.start()
    let receivedSnapshot = false
    socket.onopen = () => {
      if (ws !== socket || !currentVisit(visit, id)) return
      reconnect.markOpened()
      heartbeat.touch()
    }
    socket.onmessage = (ev) => {
      if (ws !== socket || !currentVisit(visit, id)) return
      heartbeat.touch()
      try {
        const frame = JSON.parse(String(ev.data)) as LiveFrame
        if (frame.type === 'snapshot' && !receivedSnapshot) {
          receivedSnapshot = true
          const snapshot = (frame as { run?: Run }).run
          const sessions = snapshot?.reactSessions ?? {}
          const versioned = reviewCursor.hasVersion || typeof snapshot?.reviewSessionGeneration === 'number' || Object.values(sessions).some(snap => typeof snap.sessionGeneration === 'number')
          reviewCursor.reset()
          reviewCursor.installSnapshot(sessions, snapshot?.reviewSessionGeneration)
          if (versioned) chat.value?.applyReviewFrame({ event: 'session_reset', nodeId: nodeId.value })
        }
        onFrame(frame)
        if (recovering && frame.type === 'snapshot' && !ended.value) void seed(true)
      } catch {
        /* ignore malformed frames */
      }
    }
    socket.onclose = () => {
      if (ws !== socket || !currentVisit(visit, id)) return
      ws = null
      heartbeat.stop()
      reconnect.onClose()
    }
  }

  async function takeover() {
    if (stopping.value || !nodeId.value) return
    const visit = visitEpoch
    const id = runId.value
    const node = nodeId.value
    const iter = iteration.value
    stopping.value = true
    notifyRecentActivity()
    try {
      const res = await api.takeoverNode(id, node, iter)
      if (!currentVisit(visit, id, node, iter)) return
      // taken_over and already_manual (a concurrent takeover won) are both success.
      if (res.state === 'waiting_human') goToChat(node)
      // stopping: wait for WS node_takeover waiting_human (poll is the fallback).
    } catch (e) {
      if (!currentVisit(visit, id, node, iter)) return
      const code = takeoverErrorCode(e)
      if (code === 'run_not_running') {
        // The run was cancelled / failed under us: say so, then refresh (the
        // view ends in place once the run is no longer active).
        stopping.value = false
        const status = takeoverConflictRunStatus(e)
        toast.error(t('pages.runLive.run_not_running', { status: status ? t(`common.status.${status}`, status) : '-' }))
        void load()
        return
      }
      if (takeoverRaceLost(code)) {
        stopping.value = false
        toast.show(t(`pages.runLive.${code}`))
        void load()
        return
      }
      stopping.value = false
      const msg = code ? t(`pages.runLive.${code}`) : e instanceof Error ? e.message : String(e)
      toast.error(code ? msg : t('pages.runLive.takeoverFailed', { msg }))
    }
  }

  /** Reply in the continued chat (same API as /gates). */
  async function send(text: string, images: ClarifyImage[] = [], annotations: ReactAnnotation[] = [], clientMessageId?: string) {
    if (mode.value !== 'chat' || handoff.value) return
    const visit = visitEpoch
    const id = runId.value
    const node = nodeId.value
    const iter = iteration.value
    try {
      if (clientMessageId) await api.reactReply(id, node, text, images, false, annotations, false, false, clientMessageId)
      else await api.reactReply(id, node, text, images, false, annotations)
    } catch (e) {
      if (!currentVisit(visit, id, node, iter)) return
      toast.error(e instanceof Error ? e.message : String(e))
      void load()
    }
  }

  /** Graph three-state at click time. A later node_enter fills it in when this is null. */
  function graphHandoffAuto(node: string): boolean | null {
    const current = run.value?.currentNode
    return resolveHandoffProcessAuto({
      nodes: run.value?.nodes,
      edges: run.value?.edges,
      fromNodeId: node,
      current: current?.id && current.id !== node ? current : null,
    })
  }

  function assignLiveHandoff(phase: 'pending' | 'done', node: string) {
    const kept = phase === 'done' ? handoff.value?.targetAuto : undefined
    const auto = typeof kept === 'boolean' ? kept : graphHandoffAuto(node)
    handoff.value = {
      phase,
      at: new Date().toISOString(),
      ...(typeof auto === 'boolean' ? { targetAuto: auto } : {}),
    }
  }

  /**
   * The graph did not settle the next process. node_enter does:
   * automatic → true, anything else that is a real node → false.
   * Plumbing is not a destination. An unconfirmed mark stays unknown.
   */
  function settleLiveHandoffFromEnter(frame: { nodeId?: string; name?: string; mode?: string }) {
    const h = handoff.value
    if (!h || typeof h.targetAuto === 'boolean') return
    if (!frame.nodeId || !frame.mode || frame.nodeId === nodeId.value) return
    if (isPlumbingNode(run.value?.nodes, frame.nodeId)) return
    handoff.value = { ...h, targetAuto: frame.mode === 'auto' }
  }

  /** 确认并流转 in the continued chat: the page stays and follows the run in place. */
  async function confirm() {
    if (mode.value !== 'chat' || handoff.value) {
      // This click does not send. An in-flight handoff keeps its row and spinner.
      if (handoff.value?.phase !== 'pending') chat.value?.releaseConfirmPending?.()
      return
    }
    const visit = visitEpoch
    const id = runId.value
    const node = nodeId.value
    const iter = iteration.value
    confirmError.value = null
    assignLiveHandoff('pending', node)
    armSidebarHandoff(id, node, Date.now(), sidebarAutoTarget(run.value?.nodes, run.value?.edges, node))
    try {
      const prompt = chatKind.value === 'review' ? t('pages.clarify.confirmFlowPrompt') : t('pages.runDetail.clarifyFinishPrompt')
      await api.reactReply(id, node, prompt, [], true, [])
      if (!currentVisit(visit, id, node, iter)) return
      assignLiveHandoff('done', node)
      opened.value = [...opened.value.filter((k) => k !== `${node}:${iter}`), `${node}:${iter}`]
      notifyRecentActivity()
      void load()
    } catch (e) {
      if (!currentVisit(visit, id, node, iter)) return
      clearSidebarHandoff(id)
      handoff.value = null
      confirmError.value = e instanceof Error ? e.message : String(e)
      chat.value?.releaseConfirmPending?.()
    }
  }

  function stop() {
    reconnect.reset()
    heartbeat.stop()
    const socket = ws
    ws = null
    socket?.close()
    wsRun = ''
    if (pollTimer) window.clearInterval(pollTimer)
    pollTimer = undefined
    if (refreshTimer) window.clearTimeout(refreshTimer)
    refreshTimer = undefined
  }

  watch([runId, requestedNode], () => {
    if (disposed) return
    // ?node= caught up with an in-place continue: same page, nothing to reload.
    if (selfRoute && selfRoute === `${runId.value}:${requestedNode.value}`) {
      selfRoute = ''
      return
    }
    selfRoute = ''
    visitEpoch++
    loadGen++
    seedRequest++
    chainRequest++
    completedRequest++
    stop()
    reviewCursor.reset()
    loading.value = true
    loadError.value = false
    run.value = null
    nodeId.value = ''
    iteration.value = 1
    task.value = ''
    nodeLabel.value = ''
    stopping.value = false
    ended.value = false
    endState.value = null
    shown = false
    mode.value = 'observe'
    chatKind.value = 'review'
    completedEvents.value = null
    chain.value = null
    handoff.value = null
    confirmError.value = null
    opened.value = []
    chatBusy = false
    seededKey = ''
    seededSurface = null
    lastEvents = null
    aheadEvents = null
    dialogueRevision = 0
    acpRevision = 0
    pollTimer = window.setInterval(poll, LIVE_POLL_MS)
    void load()
  }, { immediate: true, flush: 'sync' })
  watch(chat, () => { void seed() }, { flush: 'post' })
  /** The observed node's sandbox is still starting: its boot steps follow the chain. */
  function observeBooting() {
    return mode.value === 'observe' && !ended.value && !!nodeId.value && !lastEvents && chainBootPending(chain.value, nodeId.value, iteration.value)
  }
  function poll() {
    if (document.hidden) return
    void load()
    if (observeBooting()) void loadChain()
  }
  onBeforeUnmount(() => { disposed = true; stop() })

  return {
    run,
    loading,
    loadError,
    nodeId,
    iteration,
    stopping,
    ended,
    endState,
    task,
    mode,
    chatKind,
    nodeLabel,
    priorSegments,
    turns,
    handoff,
    composerLocked,
    confirmError,
    takeover,
    send,
    confirm,
    reload: load,
    onFrame,
  }
}
