import { onBeforeUnmount, ref, watch, type Ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api/api'
import { useToast } from '@/lib/composables/useToast'
import { isNodeEventsUnavailable } from '@/lib/run/nodeEventsResponse'
import { notifyRecentActivity, runDetailPath, runLivePath } from '@/lib/run/recentRuns'
import { decideLive, takeoverConflictRunStatus, takeoverErrorCode, takeoverRaceLost, type LiveFrame } from '@/lib/run/runLive'
import type { AcpEvent, Run } from '@/lib/shared/types'

/** The ReviewComposer / ClarifyChat surface this view drives. */
export interface LiveChatSurface {
  applyReviewFrame: (frame: Record<string, unknown>) => boolean | void
  applyAcpEvents: (events: AcpEvent[] | undefined, nodeId?: string) => boolean | void
}

export const LIVE_POLL_MS = 4000
const REFRESH_DEBOUNCE_MS = 400

/**
 * Live view of an auto node: read-only stream (nodeEvents seed + run WS acp
 * frames) and Stop → POST takeover → the node's chat (re-review with
 * 确认并流转). Once a node is on screen the page never navigates by itself:
 * when that node finishes (the run moved on or ended), the stream stops and
 * the view shows the node as done (`ended`). Only the user's Stop, or this
 * same node being parked for a person, opens its chat.
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
  /** Node task preview (currentNode.task / nodeEvents task), shown as the opening User Message. */
  const task = ref('')

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

  /**
   * Route on the server's run.currentNode (GET /runs/:id or the WS snapshot
   * frame). follow / detail / another node's chat only route the page while
   * it is opening; after a node was shown they end it in place.
   */
  function applyRun(r: Run, id: string) {
    if (ended.value) return
    run.value = r
    loadError.value = false
    const d = decideLive(r, requestedNode.value)
    if (d.kind === 'chat') {
      if (!shown || d.nodeId === nodeId.value) return goToChat(d.nodeId)
      return finish()
    }
    if (d.kind === 'detail') return shown ? finish() : leaveTo(runDetailPath(id))
    if (d.kind === 'follow') {
      if (shown) return finish()
      notifyRecentActivity()
      stopping.value = false
      void router.replace({ path: runLivePath(id), query: { node: d.nodeId } })
      return
    }
    if (nodeId.value !== d.nodeId) {
      nodeId.value = d.nodeId
      seededKey = ''
      lastEvents = null
      task.value = ''
    }
    shown = true
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
    if (!surface || seededKey === key) return
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
    void load()
  }, { immediate: true })
  watch(chat, () => { void seed() })
  function poll() { if (!document.hidden) void load() }
  pollTimer = window.setInterval(poll, LIVE_POLL_MS)
  onBeforeUnmount(() => { disposed = true; stop() })

  return { run, loading, loadError, nodeId, iteration, stopping, ended, task, takeover, reload: load, onFrame }
}
