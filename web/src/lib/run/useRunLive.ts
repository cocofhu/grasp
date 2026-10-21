import { computed, onBeforeUnmount, ref, watch, type Ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api/api'
import { useToast } from '@/lib/composables/useToast'
import { isNodeEventsUnavailable } from '@/lib/run/nodeEventsResponse'
import { notifyRecentActivity, recentCurrentNode, runDetailPath, runLivePath } from '@/lib/run/recentRuns'
import {
  activeSegment,
  clipTask,
  normalizeRunChain,
  priorSegments as chainPriorSegments,
  runChainFromRun,
  type ChainSegment,
  type RunChain,
} from '@/lib/inbox/runConversationChain'
import { decideLive, takeoverConflictRunStatus, takeoverErrorCode, takeoverRaceLost, type LiveFrame } from '@/lib/run/runLive'
import type { AcpEvent, ClarifyImage, ClarifyTurn, ReactAnnotation, Run, RunCurrentNode } from '@/lib/shared/types'

/** The ReviewComposer / ClarifyChat surface this view drives. */
export interface LiveChatSurface {
  applyReviewFrame: (frame: Record<string, unknown>) => boolean | void
  applyAcpEvents: (events: AcpEvent[] | undefined, nodeId?: string) => boolean | void
}

export const LIVE_POLL_MS = 4000
const REFRESH_DEBOUNCE_MS = 400

/** What the live page shows for the node on screen. */
export type LiveMode = 'observe' | 'chat'
/** Hand-off row after 确认并流转 on this page (same shape as ClarifyChat's). */
export type LiveHandoff = { phase: 'pending' | 'done'; at: string; target?: string }

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
  const turns = computed<ClarifyTurn[]>(() =>
    mode.value === 'chat' ? (run.value?.clarifyByNode?.[nodeId.value]?.turns ?? []) : [],
  )
  /** Locked between 确认并流转 and the next node (the composer itself is never replaced). */
  const composerLocked = computed(() => mode.value === 'chat' && !!handoff.value)
  let chatBusy = false

  let ws: WebSocket | null = null
  let wsRun = ''
  let pollTimer: number | undefined
  let refreshTimer: number | undefined
  let disposed = false
  let loadGen = 0
  let seededKey = ''
  let lastEvents: AcpEvent[] | null = null
  /** A node has been shown on this page: from now on, no automatic navigation. */
  let shown = false

  function leaveTo(target: Parameters<typeof router.replace>[0]) {
    if (disposed) return
    disposed = true
    stop()
    void router.replace(target)
  }

  function goToChat(node: string) {
    notifyRecentActivity()
    leaveTo({ path: '/gates', query: { run: runId.value, node } })
  }

  /** The node on screen finished: stop streaming and stay on this page. */
  function finish() {
    if (ended.value) return
    ended.value = true
    stopping.value = false
    stop()
    chat.value?.applyReviewFrame({ event: 'turn_done', nodeId: nodeId.value })
    notifyRecentActivity()
  }

  /** Earlier nodes of this run (folded above) and the current node's task. */
  async function loadChain() {
    const id = runId.value
    let c: RunChain | null = null
    try {
      c = normalizeRunChain(await api.runConversationChain(id), run.value?.nodes)
    } catch (e: unknown) {
      // Same rule as /gates: only a 404 (older backend) falls back to the run graph.
      if ((e as { status?: number })?.status === 404 && run.value) c = runChainFromRun(run.value)
    }
    if (!c || disposed || id !== runId.value) return
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
    lastEvents = null
    task.value = typeof c?.task === 'string' ? c.task.trim() : ''
    shown = true
    notifyRecentActivity()
    connect(runId.value)
    void loadChain()
    if (m === 'observe') void seed()
  }

  /**
   * Route on the server's run.currentNode (GET /runs/:id or the WS snapshot
   * frame). follow / detail / another node's chat only route the page while
   * it is opening; after a node was shown the run is followed in place, and
   * a gate or the end of the run ends it in place.
   */
  function applyRun(r: Run, id: string) {
    if (ended.value) return
    const d = decideLive(r, requestedNode.value)
    const c = recentCurrentNode(r)
    // Mid-turn in the continued chat: keep the live stream (refresh on turn_done).
    if (shown && mode.value === 'chat' && chatBusy && d.kind === 'chat' && d.nodeId === nodeId.value) return
    run.value = r
    loadError.value = false
    // Between nodes after a node was shown: stay and wait for the next one.
    if (shown && !c && (r.status === 'running' || r.status === 'queued')) return
    if (d.kind === 'chat') {
      if (!shown) return goToChat(d.nodeId)
      if (d.nodeId === nodeId.value) {
        if (mode.value === 'chat') return
        // The observed node itself was parked for a person (Stop elsewhere).
        return goToChat(d.nodeId)
      }
      if (c?.kind === 'gate') return finish()
      return continueTo(d.nodeId, 'chat', c)
    }
    if (d.kind === 'detail') return shown ? finish() : leaveTo(runDetailPath(id))
    if (d.kind === 'follow') {
      if (shown) return continueTo(d.nodeId, 'observe', c)
      notifyRecentActivity()
      stopping.value = false
      void router.replace({ path: runLivePath(id), query: { node: d.nodeId } })
      return
    }
    if (shown && nodeId.value !== d.nodeId) return continueTo(d.nodeId, 'observe', c)
    if (nodeId.value !== d.nodeId) {
      nodeId.value = d.nodeId
      seededKey = ''
      lastEvents = null
      task.value = ''
    }
    const first = !shown
    shown = true
    if (first) void loadChain()
    if (d.task) task.value = d.task
    if (d.iteration != null) iteration.value = d.iteration
    if (d.stopping) stopping.value = true
    connect(id)
    void seed()
  }

  async function load() {
    if (ended.value) return
    const gen = ++loadGen
    const id = runId.value
    try {
      const r = await api.getRun(id)
      if (gen !== loadGen || disposed) return
      applyRun(r, id)
    } catch {
      if (gen === loadGen) loadError.value = !run.value
    } finally {
      if (gen === loadGen) loading.value = false
    }
  }

  function scheduleRefresh() {
    if (refreshTimer) return
    refreshTimer = window.setTimeout(() => {
      refreshTimer = undefined
      void load()
    }, REFRESH_DEBOUNCE_MS)
  }

  /** Open the live bubble, then seed it from the node's current turn snapshot. */
  async function seed() {
    const key = `${runId.value}:${nodeId.value}:${iteration.value}`
    const surface = chat.value
    if (!surface || seededKey === key || mode.value !== 'observe') return
    seededKey = key
    surface.applyReviewFrame({ event: 'queue_state', nodeId: nodeId.value, waiting: 0, items: [], busy: true })
    surface.applyReviewFrame({ event: 'turn_begin', nodeId: nodeId.value, item: { text: '' } })
    if (lastEvents) surface.applyAcpEvents(lastEvents, nodeId.value)
    try {
      const r = await api.nodeEvents(runId.value, nodeId.value)
      if (disposed || seededKey !== key || isNodeEventsUnavailable(r)) return
      // A WS frame newer than the snapshot may already be on screen.
      // Top-level task on the first (non-paged) page only.
      const sent = 'task' in r ? r.task : undefined
      if (!task.value && typeof sent === 'string' && sent.trim()) task.value = sent.trim()
      if (!lastEvents && r.events?.length) chat.value?.applyAcpEvents(r.events, nodeId.value)
    } catch {
      /* stream frames still arrive over WS */
    }
  }

  function onFrame(m: LiveFrame) {
    if (m.type === 'snapshot') {
      // Same run detail DTO as GET /runs/:id, currentNode included.
      const r = (m as { run?: Run }).run
      if (r && typeof r === 'object' && (!r.id || r.id === runId.value)) {
        loadGen++ // a GET still in flight is older than this frame
        applyRun(r, runId.value)
      }
      return
    }
    if (m.type === 'acp') {
      if (m.nodeId !== nodeId.value) {
        if (m.nodeId) scheduleRefresh()
        return
      }
      const events = Array.isArray(m.events) ? (m.events as AcpEvent[]) : []
      if (events.length) {
        lastEvents = events
        chat.value?.applyAcpEvents(events, nodeId.value)
      }
      if (m.busy === false) scheduleRefresh()
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
      notifyRecentActivity()
      scheduleRefresh()
    }
  }

  function connect(id: string) {
    if (ws && wsRun === id) return
    ws?.close()
    wsRun = id
    let socket: WebSocket
    try {
      socket = new WebSocket(api.runEventsWsUrl(id))
    } catch {
      ws = null
      return
    }
    ws = socket
    socket.onmessage = (ev) => {
      if (ws !== socket) return
      try {
        onFrame(JSON.parse(String(ev.data)) as LiveFrame)
      } catch {
        /* ignore malformed frames */
      }
    }
    socket.onclose = () => {
      if (ws === socket) ws = null
    }
  }

  async function takeover() {
    if (stopping.value || !nodeId.value) return
    stopping.value = true
    notifyRecentActivity()
    try {
      const res = await api.takeoverNode(runId.value, nodeId.value, iteration.value)
      // taken_over and already_manual (a concurrent takeover won) are both success.
      if (res.state === 'waiting_human') goToChat(nodeId.value)
      // stopping: wait for WS node_takeover waiting_human (poll is the fallback).
    } catch (e) {
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
  async function send(text: string, images: ClarifyImage[] = [], annotations: ReactAnnotation[] = []) {
    if (mode.value !== 'chat' || handoff.value) return
    try {
      await api.reactReply(runId.value, nodeId.value, text, images, false, annotations)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e))
      void load()
    }
  }

  /** 确认并流转 in the continued chat: the page stays and follows the run in place. */
  async function confirm() {
    if (mode.value !== 'chat' || handoff.value) return
    const node = nodeId.value
    const iter = iteration.value
    confirmError.value = null
    handoff.value = { phase: 'pending', at: new Date().toISOString() }
    try {
      const prompt = chatKind.value === 'review' ? t('pages.clarify.confirmFlowPrompt') : t('pages.runDetail.clarifyFinishPrompt')
      await api.reactReply(runId.value, node, prompt, [], true, [])
      if (nodeId.value !== node) return
      handoff.value = { phase: 'done', at: new Date().toISOString() }
      opened.value = [...opened.value.filter((k) => k !== `${node}:${iter}`), `${node}:${iter}`]
      notifyRecentActivity()
      void load()
    } catch (e) {
      if (nodeId.value !== node) return
      handoff.value = null
      confirmError.value = e instanceof Error ? e.message : String(e)
    }
  }

  function stop() {
    ws?.close()
    ws = null
    wsRun = ''
    if (pollTimer) window.clearInterval(pollTimer)
    pollTimer = undefined
    if (refreshTimer) window.clearTimeout(refreshTimer)
    refreshTimer = undefined
  }

  watch([runId, requestedNode], () => {
    if (disposed) return
    loading.value = !run.value || run.value.id !== runId.value
    stopping.value = false
    // A new route (sidebar click / follow while opening) starts a fresh page.
    if (ended.value) pollTimer = window.setInterval(poll, LIVE_POLL_MS)
    ended.value = false
    shown = false
    mode.value = 'observe'
    chain.value = null
    handoff.value = null
    void load()
  }, { immediate: true })
  watch(chat, () => { void seed() })
  function poll() { if (!document.hidden) void load() }
  pollTimer = window.setInterval(poll, LIVE_POLL_MS)
  onBeforeUnmount(() => { disposed = true; stop() })

  return {
    run,
    loading,
    loadError,
    nodeId,
    iteration,
    stopping,
    ended,
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
