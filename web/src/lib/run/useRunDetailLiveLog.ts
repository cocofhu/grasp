/**
 * Run 详情：事件页 / ACP 再水合 / 沙箱日志 / 选中后 paint-then-work。
 * 从 RunDetailView 下移编排主债（选中节点副作用、rehydrate、sandbox 拉取）。
 */
import { computed, nextTick, reactive, ref, watch, type ComputedRef, type Ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { api, type SandboxView } from '@/lib/api/api'
import { applyLiveWsAcpPage } from '@/lib/run/applyLiveWsAcpPage'
import { mergeAcpEvents, type MergedAcpEvent } from '@/lib/run/mergeAcpEvents'
import type { LiveLogBootSession } from '@/lib/run/liveLogBootPhase'
import {
  isAbortError,
  RehydrateOrchestrator,
  type RehydrateStatus,
} from '@/lib/run/liveLogRehydrate'
import { isNodeEventsUnavailable } from '@/lib/run/nodeEventsResponse'
import {
  clearLiveLogSnapshotsExceptRun,
  cloneEventPageSnapshot,
  getLiveLogSnapshot,
  listLiveLogSnapshotsForRun,
  putLiveLogEventPage,
  putLiveLogMcpCalls,
} from '@/lib/run/liveLogSnapshotCache'
import type { RefreshIntent } from '@/lib/shared/loadingTypes'
import type { AcpEvent, McpCall, NodeRun, NodeRunStatus, Run } from '@/lib/shared/types'

export type EventPageState = {
  events: MergedAcpEvent[]
  nextCursor: string
  hasMore: boolean
  live: boolean
}

export type SbxLogState = { content: string; live: boolean; found: boolean; error?: string }

export function useRunDetailLiveLog(opts: {
  runId: ComputedRef<string>
  run: Ref<Run>
  selected: Ref<string | null>
  selExecIdx: ComputedRef<number>
  selIterIdx: Ref<number | null>
  selRun: ComputedRef<NodeRun | null>
  selStatus: ComputedRef<NodeRunStatus>
  viewingLatest: ComputedRef<boolean>
  nodeTab: Ref<string>
  hasLog: ComputedRef<boolean>
}) {
  const { t } = useI18n()
  const router = useRouter()
  const {
    runId,
    run,
    selected,
    selExecIdx,
    selIterIdx,
    selRun,
    selStatus,
    viewingLatest,
    nodeTab,
    hasLog,
  } = opts

  // Event log read on demand with cursor/limit pagination; older history is
  // prepended via "load earlier". Live WS events append to the loaded window.
  const eventPages = reactive<Record<string, EventPageState>>({})
  const liveEvents = reactive<Record<string, AcpEvent[]>>({})
  // Authoritative queue_state.busy per node (true while the agent is actively
  // processing the turn), pushed alongside the acp events over the WebSocket.
  const liveBusy = reactive<Record<string, boolean>>({})
  const liveNode = ref<string | null>(null)
  // Per-node fetch generation: discard stale REST responses so a slow empty
  // reply cannot overwrite a newer non-empty write-back.
  const eventFetchGen = reactive<Record<string, number>>({})

  // Per-node REST rehydrate status (idle|loading|ready|error). Independent of
  // Boot's 120s stage timeout — ~10s hang becomes a visible failure + retry.
  // Orchestrator owns generation + AbortController so an older in-flight attempt
  // cannot flip a newer loading→error (or discard a current-gen success).
  const rehydrateByNode = reactive<Record<string, RehydrateStatus>>({})
  const rehydrateOrchs: Record<string, RehydrateOrchestrator> = {}

  // Raw sandbox container logs (docker logs stdout/stderr): live while the node's
  // container runs, then the archived snapshot captured at teardown. Kept for
  // post-mortem troubleshooting (e.g. a failed git clone in startup.sh).
  const sbxLogs = reactive<Record<string, SbxLogState>>({})
  const sandboxLookup = ref<SandboxView | null>(null)
  const sbxLogLoading = ref(false)
  let sandboxLogGen = 0
  let sandboxLogAbort: AbortController | null = null
  // Boot dwell/timeout must survive LiveLogPanel remounts (log ↔ sandbox / other tabs).
  const liveLogBootSessions = reactive<Record<string, LiveLogBootSession>>({})

  function disposeAllRehydrateOrchs() {
    for (const id of Object.keys(rehydrateOrchs)) {
      rehydrateOrchs[id]?.dispose()
      delete rehydrateOrchs[id]
    }
  }

  function nodeNeedsRehydrate(nodeId: string): boolean {
    return run.value.nodeRuns[nodeId]?.status === 'running'
  }

  // Event log read on demand straight from the node's sandbox (or its persisted
  // snapshot). Returns false on failure so the rehydrate state machine can surface
  // errors instead of silently treating them as an empty timeline. Abort → stale
  // (caller must not treat as hard failure).
  async function fetchNodeEvents(
    nodeId: string | null,
    opts?: { signal?: AbortSignal },
  ): Promise<boolean> {
    if (!nodeId) return false
    const gen = (eventFetchGen[nodeId] = (eventFetchGen[nodeId] || 0) + 1)
    try {
      const r = await api.nodeEvents(runId.value, nodeId, {
        limit: 20,
        signal: opts?.signal,
      })
      if (opts?.signal?.aborted) return false
      if (eventFetchGen[nodeId] !== gen) return false
      if (isNodeEventsUnavailable(r) && !(r.events?.length)) return false
      const prev = eventPages[nodeId]?.events || []
      const merged = mergeAcpEvents(prev, r.events || [], { live: !!r.live })
      if ('hasMore' in r) {
        eventPages[nodeId] = {
          events: merged,
          nextCursor: r.nextCursor || '',
          hasMore: r.hasMore,
          live: !!r.live,
        }
      } else {
        eventPages[nodeId] = {
          events: merged,
          nextCursor: '',
          hasMore: false,
          live: r.live,
        }
      }
      syncEventPageToCache(nodeId)
      return true
    } catch (err) {
      if (isAbortError(err) || opts?.signal?.aborted) return false
      // Keep whatever we already have; caller decides error UI.
      return false
    }
  }

  function syncEventPageToCache(nodeId: string) {
    const page = eventPages[nodeId]
    if (!page) return
    putLiveLogEventPage(runId.value, nodeId, page)
  }

  function syncMcpCallsToCache(nodeId: string, mcpCalls: McpCall[] | undefined) {
    if (!mcpCalls?.length) return
    putLiveLogMcpCalls(runId.value, nodeId, mcpCalls)
  }

  /** Restore displayable event pages after hard remount within the same SPA visit. */
  function restoreEventPagesFromCache(id: string) {
    for (const { nodeId, snapshot } of listLiveLogSnapshotsForRun(id)) {
      if (!snapshot.eventPage?.events.length) continue
      eventPages[nodeId] = cloneEventPageSnapshot(snapshot.eventPage)
    }
  }

  function ensureRehydrateOrch(nodeId: string): RehydrateOrchestrator {
    let orch = rehydrateOrchs[nodeId]
    if (!orch) {
      orch = new RehydrateOrchestrator({
        fetch: async (signal) => {
          const ok = await fetchNodeEvents(nodeId, { signal })
          if (signal.aborted) throw new DOMException('Aborted', 'AbortError')
          return ok ? 'ok' : 'error'
        },
        onStatus: (s) => {
          rehydrateByNode[nodeId] = s
        },
      })
      rehydrateOrchs[nodeId] = orch
    }
    return orch
  }

  /** Entering a running node: loading → ready|error; ~10s hang → error. */
  async function rehydrateNodeEvents(nodeId: string | null, opts?: { force?: boolean }) {
    if (!nodeId) return
    const running = nodeNeedsRehydrate(nodeId)
    if (!running) {
      // Completed / non-running: clear rehydrate UI and still load persisted snapshot.
      if (rehydrateOrchs[nodeId]) {
        await rehydrateOrchs[nodeId].run({ running: false })
      } else {
        rehydrateByNode[nodeId] = 'idle'
      }
      await fetchNodeEvents(nodeId)
      return
    }
    await ensureRehydrateOrch(nodeId).run({ running: true, force: !!opts?.force })
  }

  function retryRehydrate() {
    const id = selected.value
    if (!id) return
    void rehydrateNodeEvents(id, { force: true })
  }

  const selRehydrateStatus = computed<RehydrateStatus>(() => {
    const id = selected.value
    if (!id) return 'idle'
    return rehydrateByNode[id] || 'idle'
  })

  async function loadEarlierEvents(nodeId: string) {
    const st = eventPages[nodeId]
    if (!st?.hasMore) return
    try {
      const r = await api.nodeEvents(runId.value, nodeId, { cursor: st.nextCursor, limit: 20 })
      if ('hasMore' in r && r.events?.length) {
        const older = mergeAcpEvents([], r.events, { live: false })
        st.events = [...older, ...st.events]
        st.nextCursor = r.nextCursor || ''
        st.hasMore = r.hasMore
        syncEventPageToCache(nodeId)
      }
    } catch {
      /* keep current window on failure */
    }
  }

  async function fetchSandboxLog(
    nodeId: string | null,
    opts?: { intent?: RefreshIntent },
  ) {
    if (!nodeId) {
      sandboxLogAbort?.abort()
      sandboxLogAbort = null
      sbxLogLoading.value = false
      return
    }
    const intent = opts?.intent ?? 'user_initiated'
    const silent = intent === 'silent_poll'
    // Do not interrupt an in-flight user refresh or flash RefreshStrip on poll.
    if (silent && sbxLogLoading.value) return

    const attemptGen = ++sandboxLogGen
    sandboxLogAbort?.abort()
    sandboxLogAbort = new AbortController()
    if (!silent) sbxLogLoading.value = true
    try {
      const r = await api.nodeSandboxLog(runId.value, nodeId, { signal: sandboxLogAbort.signal })
      if (attemptGen !== sandboxLogGen) return
      const next: SbxLogState = {
        content: r.content ?? '',
        live: !!r.live,
        found: !!r.found,
        error: r.error || undefined,
      }
      if (silent) {
        const prev = sbxLogs[nodeId]
        if (
          prev &&
          prev.content === next.content &&
          prev.live === next.live &&
          prev.found === next.found &&
          prev.error === next.error
        ) {
          return
        }
      }
      // Always write the response so empty live / found=false / error map correctly
      // (do not treat empty content as "no update").
      sbxLogs[nodeId] = next
    } catch (e) {
      if (attemptGen !== sandboxLogGen) return
      if (isAbortError(e)) return
      const prev = sbxLogs[nodeId]
      sbxLogs[nodeId] = {
        content: prev?.content ?? '',
        live: false,
        found: !!prev?.found,
        error: t('pages.runDetail.sandboxLog.loadFailed'),
      }
    } finally {
      if (!silent && attemptGen === sandboxLogGen) sbxLogLoading.value = false
    }
  }

  function clearReactiveRecord(rec: Record<string, unknown>) {
    for (const k of Object.keys(rec)) delete rec[k]
  }

  function resetLiveLogState(id: string) {
    // Drop other runs' snapshots; keep this run's session cache across remount.
    clearLiveLogSnapshotsExceptRun(id)
    liveNode.value = null
    disposeAllRehydrateOrchs()
    clearReactiveRecord(eventPages)
    clearReactiveRecord(liveEvents)
    clearReactiveRecord(liveBusy)
    clearReactiveRecord(eventFetchGen)
    clearReactiveRecord(sbxLogs)
    clearReactiveRecord(liveLogBootSessions as Record<string, unknown>)
    clearReactiveRecord(rehydrateByNode as Record<string, unknown>)
    sandboxLookup.value = null
    // Prefer cached timeline so re-entry is not blanked by loading/error UI.
    restoreEventPagesFromCache(id)
  }

  function abortSandboxFetches() {
    sandboxLogAbort?.abort()
    sandboxLogAbort = null
    sandboxLogGen++
    disposeAllRehydrateOrchs()
  }

  function syncAllMcpCallsFromRun() {
    const runs = run.value.nodeRuns || {}
    for (const [nodeId, nr] of Object.entries(runs)) {
      if (nr?.mcpCalls?.length) syncMcpCallsToCache(nodeId, nr.mcpCalls)
    }
  }

  // Events for the selected node: paginated fetch with optional prepended history,
  // merged with live WS updates for the running node. Treat empty [] as a miss so
  // it cannot block fallback to selRun.events.
  const logEvents = computed<AcpEvent[]>(() => {
    const sel = selected.value
    if (!sel) return []
    if (!viewingLatest.value) return selRun.value?.events || []
    const pageEvents = eventPages[sel]?.events
    if (pageEvents && pageEvents.length > 0) return pageEvents
    return selRun.value?.events || []
  })
  const logHasMore = computed(() => {
    const sel = selected.value
    return sel ? (eventPages[sel]?.hasMore ?? false) : false
  })
  const logLive = computed(() => viewingLatest.value && !!selected.value && liveNode.value === selected.value)
  // Authoritative busy for the selected node while it is live; undefined when the
  // node isn't the one currently streaming (LiveLogPanel then falls back).
  const logBusy = computed<boolean | undefined>(() =>
    logLive.value && selected.value ? liveBusy[selected.value] : undefined,
  )

  // Prefer authoritative run mcpCalls; fall back to session cache so re-entry
  // still has displayable content when REST rehydrate fails.
  const selMcpCalls = computed<McpCall[] | undefined>(() => {
    const fromRun = selRun.value?.mcpCalls
    if (fromRun?.length) return fromRun
    const sel = selected.value
    if (!sel) return undefined
    const cached = getLiveLogSnapshot(runId.value, sel)?.mcpCalls
    return cached?.length ? cached : undefined
  })

  watch(
    () => {
      const id = selected.value
      if (!id) return null
      const calls = run.value.nodeRuns[id]?.mcpCalls
      return calls?.length ? { id, calls } : null
    },
    (v) => {
      if (v) syncMcpCallsToCache(v.id, v.calls)
    },
    { deep: true },
  )

  const sbxLog = computed(() => (selected.value ? sbxLogs[selected.value] : null) || null)

  // Paint-then-work: commit selection + brief right-panel loading first, then
  // rehydrate/sandbox after a frame so pointer INP is not blocked by heavy work.
  const panelSwitching = ref(false)
  let selectionWorkGen = 0

  function afterNextPaint(): Promise<void> {
    return new Promise((resolve) => {
      requestAnimationFrame(() => resolve())
    })
  }

  async function runSelectionSideEffects(id: string | null) {
    const gen = ++selectionWorkGen
    if (!id) {
      panelSwitching.value = false
      void rehydrateNodeEvents(null)
      fetchSandboxLog(null)
      return
    }
    panelSwitching.value = true
    await nextTick()
    await afterNextPaint()
    if (gen !== selectionWorkGen) return
    void rehydrateNodeEvents(id)
    fetchSandboxLog(id)
    // Yield once more so heavy tab mounts land after the loading frame paints.
    await afterNextPaint()
    if (gen !== selectionWorkGen) return
    panelSwitching.value = false
  }

  // Pull the node's event + container logs whenever the selection changes (covers
  // refresh / re-entry: the running node's logs are read back from its container).
  // Deferred via paint-then-work so selected highlight paints before rehydrate.
  watch(selected, (id) => {
    void runSelectionSideEffects(id)
  })

  // Lazy lookup of the run/node sandbox for the console button. Only fetched when
  // viewing the latest execution on an agent node's log/sandbox tab.
  async function fetchRunNodeSandbox() {
    const id = selected.value
    if (
      !id ||
      !hasLog.value ||
      !viewingLatest.value ||
      (nodeTab.value !== 'log' && nodeTab.value !== 'sandbox')
    ) {
      sandboxLookup.value = null
      return
    }
    try {
      // null is confirmed absence. This lookup does not schedule its own retry.
      sandboxLookup.value = await api.getRunNodeSandbox(runId.value, id)
    } catch {
      sandboxLookup.value = null
    }
  }

  function openSandboxConsole() {
    const id = sandboxLookup.value?.id
    if (!id) return
    router.push({ path: `/sandboxes/${id}/console`, query: { tab: 'terminal' } })
  }

  watch([selected, selIterIdx, nodeTab, viewingLatest, hasLog], fetchRunNodeSandbox)

  // While execution log is in boot empty state, keep refreshing sandbox signals
  // so creating → container ready → running can advance the three stages.
  // Also poll on sandbox tab so stages can still advance after timeout CTA.
  function maybePollSandboxForBoot() {
    if (!hasLog.value || !viewingLatest.value) return
    if (nodeTab.value !== 'log' && nodeTab.value !== 'sandbox') return
    if (selStatus.value !== 'running') return
    if ((logEvents.value?.length || 0) > 0) return
    if (selRun.value?.mcpCalls?.length) return
    void fetchRunNodeSandbox()
  }

  function goSandboxLogTab() {
    nodeTab.value = 'sandbox'
  }

  function liveLogBootKey(nodeId: string | null | undefined, iterIdx: number): string {
    if (!nodeId) return ''
    return `${nodeId}:${iterIdx}`
  }

  const currentLiveLogBootSession = computed<LiveLogBootSession | null>(() => {
    const key = liveLogBootKey(selected.value, selExecIdx.value)
    return key ? liveLogBootSessions[key] ?? null : null
  })

  function onLiveLogBootSession(session: LiveLogBootSession) {
    const key = liveLogBootKey(selected.value, selExecIdx.value)
    if (!key) return
    if (session.confirmedPhase == null && session.stageEnteredAt == null && !session.timedOut) {
      delete liveLogBootSessions[key]
      return
    }
    liveLogBootSessions[key] = { ...session }
  }

  /** Apply live WS ACP page merge (kept for connectWs). */
  function mergeLiveWsAcpPage(nodeId: string, wsEvents: AcpEvent[]) {
    liveEvents[nodeId] = wsEvents
    // Empty busy-only frames: update busy/liveNode only — never wipe timeline
    // or sync an empty page into the session cache (f3 / soft-warn regression).
    const mergedPage = applyLiveWsAcpPage(eventPages[nodeId], wsEvents)
    if (mergedPage) {
      eventPages[nodeId] = mergedPage
      // WS only merges into the snapshot — never clears rehydrate error / soft warn.
      syncEventPageToCache(nodeId)
    }
  }

  return {
    eventPages,
    liveEvents,
    liveBusy,
    liveNode,
    eventFetchGen,
    rehydrateByNode,
    sbxLogs,
    sandboxLookup,
    sbxLogLoading,
    liveLogBootSessions,
    fetchNodeEvents,
    syncEventPageToCache,
    syncMcpCallsToCache,
    restoreEventPagesFromCache,
    rehydrateNodeEvents,
    retryRehydrate,
    selRehydrateStatus,
    loadEarlierEvents,
    fetchSandboxLog,
    resetLiveLogState,
    abortSandboxFetches,
    disposeAllRehydrateOrchs,
    syncAllMcpCallsFromRun,
    logEvents,
    logHasMore,
    logLive,
    logBusy,
    selMcpCalls,
    sbxLog,
    panelSwitching,
    afterNextPaint,
    runSelectionSideEffects,
    fetchRunNodeSandbox,
    openSandboxConsole,
    maybePollSandboxForBoot,
    goSandboxLogTab,
    currentLiveLogBootSession,
    onLiveLogBootSession,
    mergeLiveWsAcpPage,
    clearLiveLogSnapshotsExceptRun,
  }
}
