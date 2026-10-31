import { ref, computed, onMounted, onUnmounted, watch, nextTick } from 'vue'
import { takeoverTask } from '@/lib/run/takeoverTimeline'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api/api'
import { isSandboxBusyError } from '@/lib/api/httpCore'
import { readClarifyStash } from '@/lib/inbox/clarifyDraftStash'
import { beginClarifySend, failClarifySend, handleClarifyVisitEnded } from '@/lib/inbox/clarifyVisitEnded'
import { notifyRunArtifactEdit } from '@/lib/chat/artifactUnread'
import { adaptInboxContextToRun } from '@/lib/inbox/inboxContext'
import { useWorkflowFilter } from '@/lib/composables/useWorkflowFilter'
import { useTagFilter } from '@/lib/composables/useTagFilter'
import { listAllPendingGates } from '@/lib/inbox/listAllPendingGates'
import { usePendingGates } from '@/lib/inbox/usePendingGates'
import { addClarifyAnnotation, useClarifyDraft } from '@/lib/inbox/useClarifyDraft'
import { previewPickAnnotation, type AppPreviewPickPayload } from '@/lib/shared/previewPickUrl'
import { useBreakpoint } from '@/lib/composables/useBreakpoint'
import { beginBoot, bootKey, getBootRecord, markBootLive, observeBootSandbox } from '@/lib/run/bootRecord'
import {
  applyInboxReplyingState,
  inboxSecondaryLine,
  isStartingInboxItem,
  currentInboxStateEpoch,
  mergeInboxReplyingFromRemote,
  noteInboxStatePatch,
} from '@/lib/inbox/inboxDisplay'
import {
  inboxComposerMode,
  pickInboxClarifySession,
  resolveInboxReviewState,
} from '@/lib/inbox/inboxReviewMode'
import {
  findInboxItemForHandoff,
  findInboxItemForQuery,
  inboxTripleKey,
  isInboxLeftPending,
  pickNextActiveAfterRemove,
} from '@/lib/inbox/inboxActiveSelection'
import {
  isClarifyAwaitingHuman,
  isClarifyStillStarting,
  INCOMING_GHOST_NODE_ID,
  isStartFailedRun,
  makeIncomingGhost,
  resolveIncomingApproval,
  vanishedStartingRows,
} from '@/lib/inbox/inboxStartingCards'
import { isAbortError } from '@/lib/run/liveLogRehydrate'
import { applyPreviewArtifactName, inboxStageRemoteKind, isAppPreviewRemoteNode } from '@/lib/run/reactArtifactPreview'
import { findGraphNode, isClarifyNode } from '@/lib/shared/clarifyInteractive'
import { createPendingAcpBuffer } from '@/lib/run/pendingAcpBuffer'
import { deliverOrBufferDialogueAcp } from '@/lib/run/dialogueAcpDelivery'
import {
  createBusySeedRetryController,
  runBusySeedRetry,
} from '@/lib/run/busySeedRetry'
import { agentPartsFromEvents, sameParts } from '@/lib/run/acpTools'
import { takeoverConflictRunStatus, takeoverErrorCode, takeoverRaceLost } from '@/lib/run/runLive'
import { isNodeEventsUnavailable } from '@/lib/run/nodeEventsResponse'
import { createWsReconnectController } from '@/lib/run/wsReconnect'
import { createWsHeartbeat } from '@/lib/run/wsHeartbeat'
import { createReviewSessionState, hasAcpContent, REVIEW_RECOVERY_TIMEOUT_MS, type ReviewRecoveryCheckpoint, type ReviewSessionFrame } from '@/lib/chat/reviewSessionState'
import { runWithTimeout } from '@/lib/shared/loadingRequest'
import { useToast } from '@/lib/composables/useToast'
import { inboxShareKind, isHumanGateInboxItem, isShareableInboxItem } from '@/lib/inbox/gateShareLink'
import { inboxCancelDepartureRunId, isInboxCancelDeparture } from '@/lib/inbox/inboxCancelDeparture'
import {
  INBOX_SHARE_INTENT_QUERY,
  INBOX_SHARE_INTENT_VALUE,
  knownInboxProcessingLock,
} from '@/lib/inbox/inboxShareIntent'
import { createConfirmFlowCeremony } from '@/lib/inbox/confirmFlowCeremony'
import { notifyRecentActivity } from '@/lib/run/recentRuns'
import { applySidebarEnter, armSidebarHandoff, clearSidebarHandoff, noteSidebarHandoffOutcome, noteSidebarRunStatus, readSidebarHandoff, sidebarChainHandoff } from '@/lib/inbox/sidebarHandoff'
import { sidebarAutoTarget } from '@/lib/inbox/sidebarRunRows'
import { clipRunTitle, displayRunTitle } from '@/lib/run/runTitle'
import {
  consumeHomeApproveHandoff,
  homeApproveHandoffMatchesRun,
  peekHomeApproveHandoff,
  type HomeApproveHandoff,
} from '@/lib/run/homeApproveHandoff'
import type GateApproval from '@/components/run/GateApproval.vue'
import type { AcpEvent, AgentPart, Gate, GateInboxItem, GateShareInboxStatus, InboxItem, Run } from '@/lib/shared/types'
import type { PageControlState } from '@/lib/inbox/embedPageControl'
import { ingestDialogue, ingestDialogues } from '@/lib/chat/dialogueStore'
import {
  activeSegment,
  clipTask,
  isPlumbingNode,
  isRunTerminal,
  nodeTaskFromConfig,
  normalizeRunChain,
  priorSegments,
  runChainFromRun,
  seedChainBoots,
  segmentKey,
  type ChainSegment,
  type RunChain,
} from '@/lib/inbox/runConversationChain'

/** Input idle time after which a focused, emptied field stops holding list auto-apply. */
export const TYPING_IDLE_MS = 2000

export function useGatesInbox() {

const router = useRouter()
const route = useRoute()
const { t } = useI18n()
const toast = useToast()
const {
  displayedItems,
  remoteItems,
  totalCount,
  refresh,
  peek,
  applyPending,
  removeItemLocally,
  restoreItemLocally,
  setSidebarExtras,
  markStartFailed,
  clearStartFailed,
  setActiveInboxKey,
  patchItemReplying,
  syncDisplayedBaseline,
  inboxRemoteObservedAt,
  clearVisibleMembership,
  hasPendingUpdate,
  lastPeekAt,
  lastAutoAppliedAt,
  autoApplyPending,
  addAutoApplyHold,
  itemKey,
  ariaBusy,
} = usePendingGates()
const PAGE_SIZE = 20
const SKELETON_CARDS = 6
const listItems = ref<InboxItem[]>([])
/** Snapshot before the latest list mutation — used for neighbor active selection. */
let listSnapshotForNeighbor: InboxItem[] = []
const listTotal = ref(0)
const listPage = ref(1)
/** true on first paint so EmptyState cannot flash before onMounted loadList (plan g2.1). */
const listLoading = ref(true)
const listLoadError = ref<string | null>(null)
/** Monotonic generation: stale listGates responses must not overwrite local converge. */
let listLoadGeneration = 0
const { isMobile } = useBreakpoint()
const active = ref<InboxItem | null>(null)
// Async actions belong to one selection visit, even when A → B → A returns
// to the same run/node/iteration. Badge and startup row updates keep the visit.
let activeVisitEpoch = 0
watch(() => active.value ? inboxTripleKey(active.value) : '', () => { activeVisitEpoch++ }, { flush: 'sync' })
function captureActiveVisit() {
  const epoch = activeVisitEpoch
  const triple = active.value ? inboxTripleKey(active.value) : ''
  return () => epoch === activeVisitEpoch && !!active.value && inboxTripleKey(active.value) === triple
}
const homeSeed = ref<HomeApproveHandoff | null>(null)
/** Optimistic card while listGates has not yet returned the just-started Approve. */
const incomingGhost = ref<InboxItem | null>(null)
/**
 * A starting card that left the list before its sandbox ever parked (boot
 * failed). Kept locally so the loading card does not silently vanish; the user
 * dismisses it explicitly.
 */
const startFailedItem = ref<InboxItem | null>(null)
/**
 * One-shot arm for the incoming ghost: set on navigation, cleared the moment the
 * server lists the run. Without it the ghost would be a permanent fixture, since
 * `?run=` and the consumed handoff both outlive the approval itself.
 */
const incomingArmed = ref(true)
const activeHomeSeed = computed(() => {
  const it = active.value
  if (!it) return null
  const s = homeSeed.value || peekHomeApproveHandoff()
  if (!s || !homeApproveHandoffMatchesRun(s, it.runId)) return null
  return s
})
/**
 * Backend copy of each run's opening message, kept once seen: a context reload
 * nulls activeRun first, and the queued row must not blink out meanwhile.
 */
const queuedFirstMessages = ref<Record<string, string>>({})
/** The run's opening message while the agent starts: the home hand-off, else the backend's queued copy (survives reload). */
const activeQueuedFirstMessage = computed(() => {
  const it = active.value
  if (!it) return ''
  return activeHomeSeed.value?.text?.trim() || queuedFirstMessages.value[it.runId] || ''
})
const mobileView = ref<'list' | 'detail'>('list')
const listScrollTop = ref(0)
const listEl = ref<HTMLElement | null>(null)
const gateApprovalRef = ref<InstanceType<typeof GateApproval> | null>(null)
/** ReviewShell hosts ConfirmFlowOverlay for run-detail panels; inbox plays page-level ceremony. */
const reviewShellRef = ref<{ playConfirmCeremony?: () => Promise<void> } | null>(null)
const reviewChatRef = ref<{
  applyRequestRejected?: (frame: any) => void
  discardLastQueued?: (clientMessageId?: string) => void
  releaseConfirmPending?: () => void
  settleConfirmedTurn?: () => void
  restoreUnsentQueued?: import('@/lib/inbox/clarifyVisitEnded').RestoreUnsent
  isSessionBusy?: () => boolean
  playConfirmCeremony?: () => Promise<void>
} | null>(null)
/**
 * Page-level ceremony — survives active switch / shell remount so the overlay stays
 * visible after the list card leaves (plan g1.2). Child inject often misses slotted provide.
 */
const inboxConfirmFlow = createConfirmFlowCeremony()
/**
 * Keep the desk column mounted while the overlay plays after the last card leaves
 * (listItems empty would otherwise tear down the host via v-if).
 */
const confirmFlowDeskHold = ref(false)

/**
 * Inline hand-off (replaces the full-desk overlay for clarify/review confirm):
 * the confirmed conversation stays on screen with a status row at the end of
 * the transcript — pending while reactReply(force) runs, done once it returns —
 * then the inbox moves on. Keyed by itemKey so a neighbour never inherits it.
 */
const handoffState = ref<{
  key: string
  phase: 'pending' | 'done'
  at: string
  target: string
  /** Successor recorded with the row, so a stale chain can reuse it. */
  targetId?: string
  /** True when that successor runs on its own; false when a person is still starting. */
  targetAuto?: boolean
} | null>(null)
const HANDOFF_HOLD_MS = 900
const HANDOFF_HOLD_REDUCED_MS = 200
function handoffHoldMs(): number {
  if (typeof process !== 'undefined' && process.env.VITEST) return 0
  const reduce = typeof window !== 'undefined' && !!window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
  return reduce ? HANDOFF_HOLD_REDUCED_MS : HANDOFF_HOLD_MS
}
/**
 * Edges inbox-context omitted. The run graph is pinned at start, so one
 * successful GET /runs/:id is enough; a miss is remembered so chain fallback
 * stays the only 404 path.
 */
const graphEdgesByRun = new Map<string, NonNullable<Run['edges']>>()
const graphEdgesMiss = new Set<string>()
function humanFollowNode(node: NonNullable<Run['nodes']>[number]): boolean {
  return node.type === 'human_gate' || isClarifyNode(node)
}
function edgeSuccessor(run: Run, sourceId: string): { id: string; label: string; human: boolean } | null {
  const edge = (run.edges ?? []).find((e) => e.source === sourceId)
  if (!edge) return null
  const node = (run.nodes ?? []).find((n) => n.id === edge.target)
  const label = (node?.label || '').trim()
  if (!node || !label) return null
  return { id: node.id, label, human: humanFollowNode(node) }
}
function applyCachedEdges(run: Run): Run {
  if (run.edges?.length) return run
  const cached = graphEdgesByRun.get(run.id)
  if (!cached?.length) return run
  return { ...run, edges: cached }
}
function mergeGraphNodes(current: Run['nodes'], incoming: Run['nodes']): Run['nodes'] {
  if (!incoming?.length) return current
  if (!current?.length) return incoming
  const seen = new Set(current.map((n) => n.id))
  const extra = incoming.filter((n) => n.id && !seen.has(n.id))
  return extra.length ? [...current, ...extra] : current
}
async function ensureActiveGraphEdges(run: Run): Promise<Run> {
  if (run.edges?.length) {
    graphEdgesByRun.set(run.id, run.edges)
    graphEdgesMiss.delete(run.id)
    return run
  }
  const cached = applyCachedEdges(run)
  if (cached.edges?.length) return cached
  if (graphEdgesMiss.has(run.id)) return run
  try {
    const full = await api.getRun(run.id)
    if (full?.edges?.length) {
      graphEdgesByRun.set(run.id, full.edges)
      return { ...run, edges: full.edges, nodes: mergeGraphNodes(run.nodes, full.nodes) }
    }
    graphEdgesMiss.add(run.id)
  } catch {
    /* a later follow can retry */
  }
  return run
}
async function attachActiveGraphEdges(isCurrent = () => true) {
  const run = activeRun.value
  if (!run) return
  const next = await ensureActiveGraphEdges(run)
  if (isCurrent() && next !== run && activeRun.value?.id === run.id) activeRun.value = next
}
/** Name, id, and mode of the node the confirmed one flows into, for the status row. */
function nextHandoffTarget(nodeId: string): { target: string; targetId?: string; targetAuto?: boolean } {
  const run = activeRun.value
  if (!run) return { target: '' }
  // Chain already moved: the live node is the hand-off target.
  const cur = runChains.value[run.id]?.current
  if (cur && cur.id !== nodeId && cur.name) {
    return {
      target: cur.name,
      targetId: cur.id,
      ...(cur.mode === 'auto' ? { targetAuto: true } : cur.mode === 'manual' ? { targetAuto: false } : {}),
    }
  }
  const next = edgeSuccessor(applyCachedEdges(run), nodeId)
  if (!next) return { target: '' }
  return { target: next.label, targetId: next.id, targetAuto: !next.human }
}
const activeHandoff = computed(() =>
  handoffState.value && active.value && handoffState.value.key === itemKey(active.value) ? handoffState.value : null,
)

/**
 * Continuous run transcript: after 确认并流转 the next node's chat continues
 * below the previous nodes' conversations (one fold row per node). Chains are
 * cached per run; GET /runs/:id/conversation-chain (#850) with a GET
 * /runs/:id fallback only when the chain endpoint 404s (unknown run / older
 * backend). Other errors (400 invalid_iteration / until_not_found, 5xx,
 * network) are errors: no chain → main transcript, no fallback.
 */
const runChains = ref<Record<string, RunChain>>({})
/** Segments the user just handed off in this session open expanded (continuity); older ones stay folded. */
const openedSegments = ref<string[]>([])
/** Graph meta (label / type) for name:"" fallback + hiding input/output; inbox-context carries nodes. */
function chainNodeMeta(runId: string) {
  return activeRun.value?.id === runId ? activeRun.value.nodes : undefined
}
async function fetchRunChain(runId: string): Promise<RunChain | null> {
  try {
    return normalizeRunChain(await api.runConversationChain(runId), chainNodeMeta(runId))
  } catch (e: any) {
    if (e?.status !== 404) return null
  }
  try {
    return runChainFromRun(await api.getRun(runId))
  } catch {
    return null
  }
}
async function loadRunChain(runId: string, isCurrent = () => true): Promise<RunChain | null> {
  const c = await fetchRunChain(runId)
  if (!isCurrent()) return null
  if (c) {
    seedChainBoots(c)
    runChains.value = { ...runChains.value, [runId]: c }
  }
  return c
}
type PriorSegmentView = ChainSegment & { open: boolean }
const activeChainSegments = computed<PriorSegmentView[]>(() => {
  const it = active.value
  if (!it || it.type !== 'clarify') return []
  return priorSegments(runChains.value[it.runId], it.nodeId, it.iteration ?? 1).map((seg) => ({
    ...seg,
    open: openedSegments.value.includes(segmentKey(it.runId, seg)),
  }))
})
/**
 * Opening user bubble when this node was reached by a hand-off (earlier nodes
 * shown above): what the step is doing. Chain task (interpolated, proposed
 * backend field) first; else the node's raw config.prompt from the run graph.
 */
const chainNodeTask = computed(() => {
  const it = active.value
  if (!it || it.type !== 'clarify' || !activeChainSegments.value.length) return ''
  const seg = activeSegment(runChains.value[it.runId], it.nodeId, it.iteration ?? 1)
  if (seg?.task) return clipTask(seg.task)
  const node = (activeRun.value?.nodes ?? []).find((n) => n.id === it.nodeId)
  return nodeTaskFromConfig(node?.config) || ''
})
watch(
  () => (active.value && active.value.type === 'clarify' ? active.value.runId : ''),
  (runId) => {
    if (runId && !runChains.value[runId]) void loadRunChain(runId)
  },
)

/**
 * Follow the run after a hand-off whose next node is not (yet) waiting for the
 * user: the confirmed conversation stays with a tail row — next node running
 * unattended / not started yet / run ended — until the run's next waiting
 * node appears (then it opens below, same transcript).
 */
type FollowTail = {
  key: string
  runId: string
  kind: 'waiting' | 'auto' | 'ended' | 'error'
  label?: string
  status?: string
  at: string
  nodeId?: string
  iteration?: number
}
/** One downstream auto visit drawn under the hand-off row, keyed by node and iteration. */
type FollowProcessSegment = {
  nodeId: string
  iteration: number
  label: string
  parts: AgentPart[]
  live: boolean
  startedAt: string
  endedAt?: string
}
const followTail = ref<FollowTail | null>(null)
/** Stop on the auto-run bar was accepted; placeholder stays「正在停止…」until the chat opens. */
const followStopping = ref(false)
const followProcess = ref<FollowProcessSegment[]>([])
let followTimer: ReturnType<typeof setTimeout> | null = null
let followItem: InboxItem | null = null
let followInFlight = false
let followAgain = false
/** Consecutive chain fetch failures while following; 2+ → error row (keeps retrying). */
let followFailures = 0
const FOLLOW_ERROR_AFTER = 2
/**
 * Poll cadence. With the run socket open, node_enter / status frames drive
 * the follow and the poll is only a safety net (node_enter is not re-pushed on
 * a server-restart reattach; the waiting row also needs the list to show the
 * parked node). Without a socket: the old 2 s poll, which also refills steps
 * from the node record.
 */
const FOLLOW_POLL_MS = 2000
const FOLLOW_WS_FALLBACK_MS = 10000
/**
 * Dedicated socket on WS /runs/:id/events while following. The active-run
 * socket is closed by beginProcessingIntent on 确认并流转; this one keeps
 * node_enter / status and projects the current downstream node's acp steps
 * into the conversation still on screen. It reconnects after a drop; the poll
 * covers the gap.
 */
let followWs: WebSocket | null = null
/** Latest acp snapshot seen before the follow tail (or for a node not current yet). */
let followEarlyAcp = new Map<string, { nodeId: string; iteration?: number; events: AcpEvent[] }>()
/** Bumped on every acp write so an older nodeEvents fill cannot overwrite it. */
let followSnapGen = 0
/**
 * The follow socket is opened before the 确认并流转 POST so the next node's
 * node_enter is not lost while the POST, refresh and hand-off hold run; a
 * node_enter seen before the follow tail exists is remembered and makes the
 * follow re-read the chain at once instead of waiting for the safety poll.
 */
let followSawNodeEnter = false
const followReconnect = createWsReconnectController({
  connect: () => {
    const id = followTail.value?.runId
    if (id) openFollowWs(id)
  },
  shouldReconnect: () => {
    const ft = followTail.value
    return !!ft && ft.kind !== 'ended' && !!followItem && !!active.value && itemKey(active.value) === ft.key
  },
})
const followHeartbeat = createWsHeartbeat({
  onTimeout: () => {
    const ws = followWs
    followWs = null
    ws?.close()
    followReconnect.onClose()
    kickFollow()
  },
})
function closeFollowWs() {
  followHeartbeat.stop()
  followReconnect.markIntentionalClose()
  const ws = followWs
  followWs = null
  if (ws) {
    ws.onmessage = ws.onclose = ws.onerror = null
    try {
      ws.close()
    } catch {
      /* ignore */
    }
  }
}
function followWsLive(runId: string): boolean {
  return !!followWs && followWs.readyState === 1 && (followWs as WebSocket & { _runId?: string })._runId === runId
}
/** Graph captured when the handoff socket opens, so a later enter can skip plumbing after the inbox run is cleared. */
let followGraphNodes: Parameters<typeof applySidebarEnter>[2]
function openFollowWs(runId: string, nodes?: Parameters<typeof applySidebarEnter>[2]) {
  if (nodes) followGraphNodes = nodes
  if (followWsLive(runId)) return
  closeFollowWs()
  if (typeof WebSocket === 'undefined') return
  followReconnect.markConnecting()
  let ws: WebSocket
  try {
    ws = new WebSocket(api.runEventsWsUrl(runId))
  } catch {
    followReconnect.onClose()
    return
  }
  ;(ws as WebSocket & { _runId?: string })._runId = runId
  followWs = ws
  followHeartbeat.start()
  ws.onopen = () => {
    if (followWs !== ws) return
    followReconnect.markOpened()
    followHeartbeat.touch()
  }
  ws.onmessage = (ev) => {
    if (followWs !== ws) return
    followHeartbeat.touch()
    let m: {
      type?: string
      runId?: string
      nodeId?: string
      iteration?: number
      name?: string
      kind?: string
      mode?: string
      state?: 'stopping' | 'waiting_human'
      events?: AcpEvent[]
      busy?: boolean
      status?: string
    }
    try {
      m = JSON.parse(String(ev.data))
    } catch {
      return
    }
    if (m.type === 'node_enter') onFollowNodeEnter(runId, m)
    else if (m.type === 'node_takeover') onFollowTakeover(runId, m)
    else if (m.type === 'status') {
      noteSidebarRunStatus(runId, (m as { status?: unknown }).status)
      kickFollow()
    }
    else if (m.type === 'acp') onFollowAcp(runId, m)
  }
  ws.onclose = () => {
    if (followWs !== ws) return
    followHeartbeat.stop()
    followWs = null
    const ft = followTail.value
    const item = followItem
    // Gap: chain poll updates the status line; node records refill steps.
    if (ft && item && active.value && itemKey(active.value) === ft.key && ft.kind !== 'ended') {
      void followStep(item, ft.key)
    }
    followReconnect.onClose()
  }
}
/**
 * WS node_enter {runId,nodeId,iteration,name,kind,mode} (#850): the run
 * opened its next visit. Update the tail at once (no fetch round-trip), then
 * re-read chain + list. input/output plumbing visits are skipped (the status
 * frame / run end will follow).
 */
function onFollowNodeEnter(runId: string, m: { nodeId?: string; iteration?: number; name?: string; kind?: string; mode?: string }) {
  const ft = followTail.value
  if (!ft) followSawNodeEnter = true
  const nodes = followGraphNodes ?? activeRun.value?.nodes
  const plumbing = !!m.nodeId && isPlumbingNode(nodes, m.nodeId)
  applySidebarEnter(runId, m, nodes)
  if (!ft || ft.runId !== runId || ft.kind === 'ended' || !active.value || itemKey(active.value) !== ft.key) return
  if (m.nodeId && !plumbing) {
    const label =
      m.name || ((activeRun.value?.nodes ?? []).find((n) => n.id === m.nodeId)?.label as string | undefined) || m.nodeId
    setFollowTail({
      ...ft,
      kind: m.mode === 'auto' ? 'auto' : 'waiting',
      label,
      nodeId: m.nodeId,
      iteration: typeof m.iteration === 'number' ? m.iteration : ft.iteration,
      at: new Date().toISOString(),
    })
    if (m.mode === 'auto') void fillFollowFromNodeEvents()
  }
  kickFollow()
}
function earlyAcpKey(nodeId: string, iteration?: number) {
  return `${nodeId}:${iteration ?? ''}`
}
function rememberEarlyAcp(m: { nodeId?: string; iteration?: number; events?: AcpEvent[] }) {
  const nodeId = m.nodeId || ''
  const events = Array.isArray(m.events) ? m.events : []
  if (!nodeId || !events.length) return
  followEarlyAcp.set(earlyAcpKey(nodeId, m.iteration), { nodeId, iteration: m.iteration, events })
}
function chainVisitTimes(runId: string, nodeId: string, iteration: number) {
  const seg = runChains.value[runId]?.segments.find((s) => s.nodeId === nodeId && s.iteration === iteration)
  return { startedAt: seg?.at, endedAt: seg?.endedAt }
}
function sealFollowProcess() {
  const now = new Date().toISOString()
  const runId = followTail.value?.runId
  let changed = false
  const next = followProcess.value.map((seg) => {
    if (!seg.live) return seg
    changed = true
    const times = runId ? chainVisitTimes(runId, seg.nodeId, seg.iteration) : undefined
    return { ...seg, live: false, endedAt: seg.endedAt || times?.endedAt || now }
  })
  if (changed) followProcess.value = next
}
/** Keep one segment per node visit. Switching nodes folds the previous one. */
function ensureAutoSegment(tail: FollowTail) {
  if (tail.kind !== 'auto' || !tail.nodeId) {
    sealFollowProcess()
    return
  }
  const iteration = tail.iteration ?? 1
  const now = new Date().toISOString()
  const list = followProcess.value.map((seg) =>
    seg.live && (seg.nodeId !== tail.nodeId || seg.iteration !== iteration)
      ? { ...seg, live: false, endedAt: seg.endedAt || chainVisitTimes(tail.runId, seg.nodeId, seg.iteration).endedAt || now }
      : seg,
  )
  const idx = list.findIndex((s) => s.nodeId === tail.nodeId && s.iteration === iteration)
  if (idx < 0) {
    const times = chainVisitTimes(tail.runId, tail.nodeId, iteration)
    list.push({
      nodeId: tail.nodeId,
      iteration,
      label: tail.label || tail.nodeId,
      parts: [],
      live: true,
      startedAt: times.startedAt || now,
    })
  } else if (tail.label && list[idx]!.label !== tail.label) {
    list[idx] = { ...list[idx]!, label: tail.label }
  }
  followProcess.value = list
}
function writeFollowParts(nodeId: string, iteration: number, label: string, events: AcpEvent[], live: boolean) {
  const parsed = agentPartsFromEvents(events)
  const now = new Date().toISOString()
  const list = followProcess.value.slice()
  const idx = list.findIndex((s) => s.nodeId === nodeId && s.iteration === iteration)
  const runId = followTail.value?.runId || ''
  if (idx < 0) {
    if (!parsed?.length && !live) return
    const times = chainVisitTimes(runId, nodeId, iteration)
    list.push({
      nodeId,
      iteration,
      label,
      parts: parsed ?? [],
      live,
      startedAt: times.startedAt || now,
      endedAt: live ? undefined : times.endedAt || now,
    })
  } else {
    const prev = list[idx]!
    const parts = parsed?.length ? parsed : prev.parts
    if (sameParts(prev.parts, parts) && prev.live === live && (label || prev.label) === prev.label) return
    list[idx] = {
      ...prev,
      label: label || prev.label,
      parts,
      live,
      endedAt: live ? undefined : prev.endedAt || chainVisitTimes(runId, nodeId, iteration).endedAt || now,
    }
  }
  followProcess.value = list
}
function replayEarlyAcp(tail: FollowTail) {
  if (!tail.nodeId) return
  const hit =
    followEarlyAcp.get(earlyAcpKey(tail.nodeId, tail.iteration)) ||
    followEarlyAcp.get(earlyAcpKey(tail.nodeId)) ||
    [...followEarlyAcp.values()].find(
      (e) => e.nodeId === tail.nodeId && (e.iteration == null || tail.iteration == null || e.iteration === tail.iteration),
    )
  if (!hit) return
  for (const [key, value] of followEarlyAcp) {
    if (value.nodeId === tail.nodeId) followEarlyAcp.delete(key)
  }
  followSnapGen++
  writeFollowParts(tail.nodeId, tail.iteration ?? hit.iteration ?? 1, tail.label || tail.nodeId, hit.events, tail.kind === 'auto')
}
function setFollowTail(next: FollowTail) {
  if (next.kind !== 'auto') followStopping.value = false
  followTail.value = next
  if (next.kind === 'auto' && next.nodeId) ensureAutoSegment(next)
  else sealFollowProcess()
  replayEarlyAcp(next)
}
/**
 * ACP for the node we are following is drawn into that visit only.
 * A snapshot for some other node is kept until that node is current,
 * or folded into an already-shown visit without reopening it.
 */
function onFollowAcp(runId: string, m: { nodeId?: string; iteration?: number; events?: AcpEvent[] }) {
  const ft = followTail.value
  const events = Array.isArray(m.events) ? m.events : []
  if (!ft || ft.runId !== runId || !active.value || itemKey(active.value) !== ft.key) {
    rememberEarlyAcp(m)
    return
  }
  const nodeId = m.nodeId || ''
  if (!nodeId || !events.length) return
  const iteration = typeof m.iteration === 'number' ? m.iteration : undefined
  const sameVisit =
    ft.kind === 'auto' && !!ft.nodeId && nodeId === ft.nodeId && (iteration == null || ft.iteration == null || iteration === ft.iteration)
  if (!sameVisit) {
    const seg = followProcess.value.find((s) => s.nodeId === nodeId && (iteration == null || s.iteration === iteration))
    if (seg) {
      followSnapGen++
      writeFollowParts(seg.nodeId, seg.iteration, seg.label, events, false)
      return
    }
    rememberEarlyAcp({ nodeId, iteration, events })
    return
  }
  followSnapGen++
  writeFollowParts(nodeId, iteration ?? ft.iteration ?? 1, ft.label || nodeId, events, true)
}
async function fillFollowFromNodeEvents() {
  const ft = followTail.value
  if (!ft || ft.kind !== 'auto' || !ft.nodeId || !followItem) return
  if (!active.value || itemKey(active.value) !== ft.key) return
  const nodeId = ft.nodeId
  const iteration = ft.iteration ?? 1
  const gen = followSnapGen
  try {
    const res = await api.nodeEvents(ft.runId, nodeId)
    if (gen !== followSnapGen) return
    const cur = followTail.value
    if (!cur || cur.key !== ft.key || cur.nodeId !== nodeId || cur.kind === 'ended') return
    if (!active.value || itemKey(active.value) !== ft.key) return
    if (isNodeEventsUnavailable(res)) return
    const events = (Array.isArray(res.events) ? res.events : []) as AcpEvent[]
    const live = res.live !== false && cur.kind === 'auto'
    if (!events.length && live) return
    followSnapGen++
    writeFollowParts(nodeId, iteration, cur.label || nodeId, events, live)
  } catch {
    /* the chain poll still moves the status line */
  }
}
function kickFollow() {
  const ft = followTail.value
  if (!ft || !followItem) return
  if (followTimer) clearTimeout(followTimer)
  followTimer = null
  void followStep(followItem, ft.key)
}
function stopFollow() {
  if (followTimer) clearTimeout(followTimer)
  followTimer = null
  followTail.value = null
  followProcess.value = []
  followItem = null
  followAgain = false
  followFailures = 0
  followSawNodeEnter = false
  followEarlyAcp = new Map()
  followSnapGen++
  followStopping.value = false
  closeFollowWs()
}
function isFollowingActive(): boolean {
  return !!followTail.value && !!active.value && followTail.value.key === itemKey(active.value)
}
const activeFollowTail = computed(() =>
  followTail.value && active.value && followTail.value.key === itemKey(active.value) ? followTail.value : null,
)
const activeFollowProcess = computed(() => (activeFollowTail.value ? followProcess.value : []))
/** Auto node on the followed conversation: the existing observe bar, not the done footer. */
const followObserving = computed(() => activeFollowTail.value?.kind === 'auto')
function tailFromChain(key: string, chain: RunChain): FollowTail {
  const at = new Date().toISOString()
  const cur = chain.current
  const nodeId = cur?.id
  const iteration = cur?.iteration
  const label = cur?.name
  if (isRunTerminal(chain.status)) return { key, runId: chain.runId, kind: 'ended', status: String(chain.status), at, nodeId, iteration, label }
  if (cur && cur.mode === 'auto') return { key, runId: chain.runId, kind: 'auto', label, nodeId, iteration, at }
  return { key, runId: chain.runId, kind: 'waiting', label, nodeId, iteration, at }
}
/**
 * Chain still names the visit that was just confirmed. A later iteration of
 * the same node has moved on and is not this snapshot.
 */
function chainStillOnDeparted(chain: RunChain, submitted: InboxItem): boolean {
  const cur = chain.current
  if (!cur?.id || cur.id !== submitted.nodeId) return false
  const left = submitted.iteration
  if (typeof cur.iteration === 'number' && typeof left === 'number' && cur.iteration > left) return false
  return true
}
/**
 * First outgoing edge, else the successor the hand-off row already named.
 * A clarify node or a human gate is a person still starting; anything else
 * runs on its own. No label → the unnamed「等待下一节点开始」row.
 */
function recordedHandoffSuccessor(run: Run, departedId: string): { id: string; label: string; human: boolean } | null {
  const row = handoffState.value
  const label = (row?.target || '').trim()
  if (!row || !label) return null
  if (row.targetId && row.targetId !== departedId) {
    if (row.targetAuto === true) return { id: row.targetId, label, human: false }
    if (row.targetAuto === false) return { id: row.targetId, label, human: true }
    const node = (run.nodes ?? []).find((n) => n.id === row.targetId)
    if (node) return { id: node.id, label: (node.label || label).trim() || label, human: humanFollowNode(node) }
  }
  const node = (run.nodes ?? []).find((n) => n.id !== departedId && (n.label || '').trim() === label)
  if (!node) return null
  return { id: node.id, label, human: humanFollowNode(node) }
}
function handoffSuccessor(runId: string, departedId: string): { id: string; label: string; human: boolean } | null {
  const run = activeRun.value
  if (!run || run.id !== runId) return null
  const withEdges = applyCachedEdges(run)
  if (withEdges !== run) activeRun.value = withEdges
  // The row already named the successor. Edges fill in only when it has not.
  return recordedHandoffSuccessor(withEdges, departedId) ?? edgeSuccessor(withEdges, departedId)
}
function fillEmptyHandoffTarget(key: string, tail: FollowTail, departedId: string) {
  if (tail.kind !== 'auto' && tail.kind !== 'waiting') return
  const label = (tail.label || '').trim()
  if (!label || !tail.nodeId || tail.nodeId === departedId) return
  const cur = handoffState.value
  if (!cur || cur.key !== key || cur.target) return
  handoffState.value = { ...cur, target: label, targetId: tail.nodeId, targetAuto: tail.kind === 'auto' }
}
function tailFromHandoffTarget(key: string, chain: RunChain, submitted: InboxItem): FollowTail {
  const at = new Date().toISOString()
  const next = handoffSuccessor(chain.runId, submitted.nodeId)
  if (!next) return { key, runId: chain.runId, kind: 'waiting', at }
  if (next.human) return { key, runId: chain.runId, kind: 'waiting', label: next.label, nodeId: next.id, at }
  return { key, runId: chain.runId, kind: 'auto', label: next.label, nodeId: next.id, at }
}
/** Follow row already names a node after the one just confirmed. A stale chain must not put it back. */
function followAlreadyAhead(key: string, submitted: InboxItem): FollowTail | null {
  const kept = followTail.value
  if (!kept || kept.key !== key || kept.kind === 'ended' || kept.kind === 'error') return null
  if (!kept.nodeId || kept.nodeId === submitted.nodeId) return null
  return kept
}
function resolveFollowTail(submitted: InboxItem, key: string, chain: RunChain): FollowTail {
  if (isRunTerminal(chain.status)) return tailFromChain(key, chain)
  if (!chainStillOnDeparted(chain, submitted)) return tailFromChain(key, chain)
  return followAlreadyAhead(key, submitted) ?? tailFromHandoffTarget(key, chain, submitted)
}
/** Stale departed snapshot must not cancel an automatic successor the row already chose. */
function noteFollowFromSnapshot(submitted: InboxItem, chain: RunChain, tail: FollowTail) {
  if (isRunTerminal(chain.status) || !chainStillOnDeparted(chain, submitted)) {
    syncSidebarHandoffFromChain(submitted.runId, chain)
    return
  }
  if (tail.kind === 'auto') noteSidebarHandoffOutcome(submitted.runId, 'auto')
  else if (tail.kind === 'waiting' && tail.nodeId && tail.nodeId !== submitted.nodeId) {
    noteSidebarHandoffOutcome(submitted.runId, 'human')
  }
}
/** Snapshot after 确认并流转: automatic confirms the fly; a person or a finished run cancels it. Plumbing does not. */
function syncSidebarHandoffFromChain(runId: string, chain: RunChain) {
  const mark = readSidebarHandoff(runId)
  const decision = sidebarChainHandoff(chain, mark?.fromNodeId, followGraphNodes ?? activeRun.value?.nodes)
  if (!decision) return
  noteSidebarHandoffOutcome(runId, decision.outcome, decision.target)
}
/**
 * Same-run waiting node in the list → open it under the chain. A row still
 * booting (state: starting — the real backend lists a clarify node while its
 * sandbox comes up) is not opened yet: its starting card would replace the
 * transcript; the follow tail shows「等待…」until it parks (status frame).
 */
async function continueInRun(submitted: InboxItem, submittedKey: string, isCurrent = () => true): Promise<boolean> {
  const next = listItems.value.find(
    (k) => k.runId === submitted.runId && itemKey(k) !== submittedKey && !isStartingInboxItem(k),
  )
  if (!next) return false
  noteSidebarHandoffOutcome(submitted.runId, 'human')
  await loadRunChain(submitted.runId, isCurrent)
  if (!isCurrent()) return false
  openedSegments.value = [...openedSegments.value, `${submitted.runId}:${submitted.nodeId}:${submitted.iteration ?? 1}`]
  if (handoffState.value?.key === submittedKey) handoffState.value = null
  stopFollow()
  active.value = next
  showProcessedBanner.value = false
  return true
}
async function followStep(submitted: InboxItem, submittedKey: string) {
  followTimer = null
  if (followInFlight) {
    followAgain = true
    return
  }
  if (!followTail.value || followTail.value.key !== submittedKey) return
  if (!active.value || itemKey(active.value) !== submittedKey) return stopFollow()
  followInFlight = true
  try {
    await attachActiveGraphEdges()
    const chain = await loadRunChain(submitted.runId)
    if (!followTail.value || followTail.value.key !== submittedKey) return
    try {
      await loadList()
    } catch {
      /* soft */
    }
    if (await continueInRun(submitted, submittedKey)) return
    const prevNode = followTail.value?.nodeId
    const prevIter = followTail.value?.iteration
    if (chain) {
      followFailures = 0
      const tail = resolveFollowTail(submitted, submittedKey, chain)
      setFollowTail(tail)
      fillEmptyHandoffTarget(submittedKey, tail, submitted.nodeId)
      noteFollowFromSnapshot(submitted, chain, tail)
    } else if (++followFailures >= FOLLOW_ERROR_AFTER && followTail.value) {
      setFollowTail({ ...followTail.value, kind: 'error', at: new Date().toISOString() })
    }
    const ft = followTail.value
    // Socket down, or the downstream node changed: the record fills steps the frames missed.
    if (
      ft?.kind === 'auto' &&
      ft.nodeId &&
      (!followWsLive(submitted.runId) || ft.nodeId !== prevNode || ft.iteration !== prevIter)
    ) {
      void fillFollowFromNodeEvents()
    }
    // Run done: stay on this conversation with the ended row (never jump to another run).
    if (followTail.value?.kind === 'ended') return closeFollowWs()
  } finally {
    followInFlight = false
  }
  if (followAgain) {
    followAgain = false
    return void followStep(submitted, submittedKey)
  }
  scheduleFollow(submitted, submittedKey)
}
function scheduleFollow(submitted: InboxItem, submittedKey: string) {
  if (typeof process !== 'undefined' && process.env.VITEST) return
  if (followTimer) clearTimeout(followTimer)
  if (!followTail.value || followTail.value.key !== submittedKey || followTail.value.kind === 'ended') return
  const ms = followWsLive(submitted.runId) ? FOLLOW_WS_FALLBACK_MS : FOLLOW_POLL_MS
  followTimer = setTimeout(() => void followStep(submitted, submittedKey), ms)
}
/**
 * After the hand-off returned and no same-run node is waiting yet: stay and
 * follow the run (「流转了继续聊，不要跳」). Never hands the selection to
 * another pending item — not even when the run ended or the chain is
 * unavailable (then: waiting row, keep polling the list for this run).
 */
async function beginFollow(submitted: InboxItem, submittedKey: string, isCurrent = () => true): Promise<boolean> {
  if (!isCurrent()) return false
  followItem = submitted
  openFollowWs(submitted.runId)
  followSawNodeEnter = false
  await attachActiveGraphEdges(isCurrent)
  if (!isCurrent()) return false
  const chain = await loadRunChain(submitted.runId, isCurrent)
  if (!isCurrent()) return false
  followFailures = chain ? 0 : 1
  const tail: FollowTail = chain
    ? resolveFollowTail(submitted, submittedKey, chain)
    : { key: submittedKey, runId: submitted.runId, kind: 'waiting', at: new Date().toISOString() }
  setFollowTail(tail)
  if (chain) fillEmptyHandoffTarget(submittedKey, tail, submitted.nodeId)
  if (chain) noteFollowFromSnapshot(submitted, chain, tail)
  else if (tail.kind === 'ended') noteSidebarHandoffOutcome(submitted.runId, 'ended')
  if (tail.kind === 'auto') void fillFollowFromNodeEvents()
  if (tail.kind === 'ended') closeFollowWs()
  else if (followSawNodeEnter) {
    // The run moved again while the chain was being read.
    followSawNodeEnter = false
    kickFollow()
  } else scheduleFollow(submitted, submittedKey)
  return true
}
/**
 * Takeover finished (or the node parked itself): open the same-run chat in
 * place. The row may still be starting — then keep following until it parks.
 */
async function settleTakeoverChat(item: InboxItem, key: string) {
  if (!followTail.value || followTail.value.key !== key || !active.value || itemKey(active.value) !== key) return
  try {
    await loadList()
  } catch {
    /* soft */
  }
  if (!followTail.value || followTail.value.key !== key) return
  if (await continueInRun(item, key)) return
  kickFollow()
}
/** WS node_takeover while the auto-run bar is up. Same contract as the live page, without leaving this chat. */
function onFollowTakeover(runId: string, m: { nodeId?: string; state?: 'stopping' | 'waiting_human' }) {
  const ft = followTail.value
  const item = followItem
  if (!ft || ft.runId !== runId || !item || !active.value || itemKey(active.value) !== ft.key) return
  if (m.nodeId && ft.nodeId && m.nodeId !== ft.nodeId) return
  notifyRecentActivity()
  if (m.state === 'waiting_human') {
    void settleTakeoverChat(item, ft.key)
    return
  }
  if (m.state === 'stopping' && ft.kind === 'auto') followStopping.value = true
}
/**
 * Stop on the followed auto node. One POST /takeover; a second click while
 * stopping does not send again. Failure uses the live page's notices and
 * drops「正在停止…」.
 */
async function takeoverFollow() {
  const ft = activeFollowTail.value
  const item = followItem
  if (followStopping.value || !ft || ft.kind !== 'auto' || !ft.nodeId || !item) return
  const key = ft.key
  followStopping.value = true
  notifyRecentActivity()
  try {
    const res = await api.takeoverNode(ft.runId, ft.nodeId, ft.iteration)
    if (!followTail.value || followTail.value.key !== key) return
    if (res.state === 'waiting_human') await settleTakeoverChat(item, key)
  } catch (e) {
    if (!followTail.value || followTail.value.key !== key) return
    followStopping.value = false
    const code = takeoverErrorCode(e)
    if (code === 'run_not_running') {
      const status = takeoverConflictRunStatus(e)
      toast.error(t('pages.runLive.run_not_running', { status: status ? t(`common.status.${status}`, status) : '-' }))
      kickFollow()
      return
    }
    if (takeoverRaceLost(code)) {
      toast.show(t(`pages.runLive.${code}`))
      kickFollow()
      return
    }
    const msg = code ? t(`pages.runLive.${code}`) : e instanceof Error ? e.message : String(e)
    toast.error(code ? msg : t('pages.runLive.takeoverFailed', { msg }))
  }
}
/** A confirmed conversation reopened by a reload: its transcript, the 已流转 row, then the run followed in place. */
async function resumeHandedOff(run: Run, nodeId: string) {
  const conv = run.clarifyByNode![nodeId]!
  const item: InboxItem = {
    ...makeIncomingGhost({ runId: run.id, nodeId }, '', run.startedAt || new Date().toISOString()),
    state: undefined,
    iteration: conv.iteration ?? run.nodeRuns?.[nodeId]?.iteration ?? 1,
    workflowId: run.workflowId,
    workflowName: run.workflowName || '',
    runTitle: displayRunTitle(run.title) || undefined,
    label: String((run.nodes ?? []).find((n) => n.id === nodeId)?.label || nodeId),
    done: true,
  }
  const key = itemKey(item)
  listItems.value = listItems.value.filter((it) => itemKey(it) !== key)
  if (run.edges?.length) graphEdgesByRun.set(run.id, run.edges)
  activeRun.value = run
  activeRunLoadError.value = false
  followItem = item
  followStopping.value = false
  followTail.value = { key, runId: run.id, kind: 'waiting', at: new Date().toISOString() }
  active.value = item
  handoffState.value = { key, phase: 'done', at: new Date().toISOString(), ...nextHandoffTarget(nodeId) }
  await beginFollow(item, key)
}
watch(
  () => (active.value ? itemKey(active.value) : ''),
  (k) => {
    if (followTail.value && followTail.value.key !== k) {
      if (handoffState.value?.key === followTail.value.key) handoffState.value = null
      stopFollow()
    }
  },
)

function playInboxConfirmFlowCeremony(): Promise<void> {
  confirmFlowDeskHold.value = true
  return inboxConfirmFlow.play().finally(() => {
    confirmFlowDeskHold.value = false
  })
}
/**
 * Review/clarify WS frames that arrived while ClarifyChat was unmounted
 * (hard loadActiveRun nulls activeRun → ReviewComposer gone). Flushed after mount.
 */
let pendingReviewFrames: Record<string, unknown>[] = []
/**
 * ACP frames during the same hard-load window (parity with review buffer).
 * Latest cumulative events per nodeId; flushed after busy slot rebuild.
 */
const pendingAcpFrames = createPendingAcpBuffer()
/** Snapshot reactSessions stashed when activeRun is still null during hard load. */
let pendingSnapshotSessions: Run['reactSessions'] | null = null
const { selected } = useWorkflowFilter()
const { selectedTags } = useTagFilter()
const workflowFilterOpen = ref(false)
const tagFilterOpen = ref(false)

watch(workflowFilterOpen, (v) => {
  if (v) {
    tagFilterOpen.value = false
  }
})
watch(tagFilterOpen, (v) => {
  if (v) {
    workflowFilterOpen.value = false
  }
})

const showProcessedBanner = ref(false)
const manualRefreshing = ref(false)
/** First-screen skeleton when loading with no rows yet (plan g2.1). */
const showListSkeleton = computed(
  () => (listLoading.value || manualRefreshing.value) && !listItems.value.length && !listLoadError.value,
)
/** First-screen error when load failed and nothing to show. */
const showListError = computed(() => !!listLoadError.value && !listItems.value.length)
/** Always lead with the actionable copy; append the raw cause when it adds detail. */
const listErrorMessage = computed(() => {
  const desc = String(t('common.asyncState.loadFailedDesc'))
  const raw = listLoadError.value
  return raw && raw !== desc ? `${desc} · ${raw}` : desc
})
/** Keep old rows + RefreshStrip/fade on user refresh or filter reload (plan g2.2). */
const showListRefresh = computed(
  () =>
    (listLoading.value || manualRefreshing.value) &&
    listItems.value.length > 0 &&
    !incomingGhost.value,
)
/** Includes silent peek ariaBusy — no RefreshStrip for silent poll (plan g2.3). */
const listPanelBusy = computed(
  () => listLoading.value || manualRefreshing.value || ariaBusy.value,
)
/** Review confirm validation failure (bottom status bar; replaces force-failure toast). */
const clarifyConfirmError = ref<string | null>(null)
const clarifyConfirmCanAbort = ref(false)

/** Triples that left pending — never softRefresh/load inbox-context for these. */
const processedTriples = new Set<string>()
/** Processed triples observed absent from a subsequent list load (safe to unmark if they reappear). */
const confirmedAbsentTriples = new Set<string>()
/** Per-triple AbortController for in-flight inbox-context fetches. */
const inboxContextAborts = new Map<string, AbortController>()
/** Single-flight: one softRefresh/loadActiveRun at a time for the selected triple. */
let inboxContextInFlight: string | null = null
/** Terminal frames received during that flight retain one coalesced trailing refresh. */
const inboxContextTrailingRefreshes = new Set<string>()
/**
 * In-flight confirm intents (triple keys). Non-empty ⇒ list reselection / refresh
 * stay gated (race with lagging list). Force confirm is gated per-item only so a
 * neighbor can confirm while an earlier reactReply/resume is still pending.
 */
const processingIntentKeys = ref(new Set<string>())
const processingLock = computed(() => processingIntentKeys.value.size > 0)

function addProcessingIntent(triple: string) {
  const next = new Set(processingIntentKeys.value)
  next.add(triple)
  processingIntentKeys.value = next
}

function removeProcessingIntent(triple: string) {
  if (!processingIntentKeys.value.has(triple)) return
  const next = new Set(processingIntentKeys.value)
  next.delete(triple)
  processingIntentKeys.value = next
}

function isProcessingIntent(it: Pick<InboxItem, 'runId' | 'nodeId' | 'iteration'> | null | undefined) {
  if (!it) return false
  return processingIntentKeys.value.has(inboxTripleKey(it))
}

/** Per-item card/select gate — not the global processingLock (plan g1.2). */
function isItemCardDisabled(it: InboxItem) {
  return isProcessingIntent(it) || isProcessedTriple(it)
}

function markProcessed(it: Pick<InboxItem, 'runId' | 'nodeId' | 'iteration'>) {
  processedTriples.add(inboxTripleKey(it))
}

function unmarkProcessed(it: Pick<InboxItem, 'runId' | 'nodeId' | 'iteration'>) {
  const key = inboxTripleKey(it)
  processedTriples.delete(key)
  confirmedAbsentTriples.delete(key)
}

function isProcessedTriple(it: Pick<InboxItem, 'runId' | 'nodeId' | 'iteration'> | null | undefined) {
  if (!it) return false
  return processedTriples.has(inboxTripleKey(it))
}

function abortInboxContext(triple: string) {
  inboxContextTrailingRefreshes.delete(triple)
  const ctrl = inboxContextAborts.get(triple)
  if (!ctrl) return
  ctrl.abort()
  inboxContextAborts.delete(triple)
  if (inboxContextInFlight === triple) inboxContextInFlight = null
}

function acquireInboxContextSignal(triple: string): AbortSignal | null {
  // Single-flight: drop duplicate softRefresh/load while one request is in flight.
  if (inboxContextInFlight === triple) return null
  // Switching selection: abort the previous triple's in-flight request.
  if (inboxContextInFlight && inboxContextInFlight !== triple) {
    abortInboxContext(inboxContextInFlight)
  }
  abortInboxContext(triple)
  const ctrl = new AbortController()
  inboxContextAborts.set(triple, ctrl)
  inboxContextInFlight = triple
  return ctrl.signal
}

function releaseInboxContextSignal(triple: string, signal: AbortSignal) {
  const ctrl = inboxContextAborts.get(triple)
  const ownsFlight = ctrl?.signal === signal && inboxContextInFlight === triple
  if (ctrl?.signal === signal) inboxContextAborts.delete(triple)
  if (ownsFlight) inboxContextInFlight = null
  if (!ownsFlight || signal.aborted || !inboxContextTrailingRefreshes.delete(triple)) return
  // Run after the owner's finally finishes its loading cleanup. The selected
  // triple may have moved or been processed while the request was in flight.
  queueMicrotask(() => {
    if (!active.value || inboxTripleKey(active.value) !== triple) return
    if (!shouldFetchActiveInboxContext()) return
    void softRefreshActiveRun()
  })
}

/**
 * User clicked approve / force-finish clarify: short-circuit immediately so the
 * resume/finish window cannot emit more inbox-context traffic for this triple.
 * Multiple intents may be in flight (consecutive neighbor confirms).
 */
function beginProcessingIntent(it: Pick<InboxItem, 'runId' | 'nodeId' | 'iteration'>) {
  const triple = inboxTripleKey(it)
  addProcessingIntent(triple)
  markProcessed(it)
  abortInboxContext(triple)
  closeActiveRunWs()
}

/** resumeGate / reactReply(force) failed — restore fetchability and drop this intent. */
function rollbackProcessingIntent(it: Pick<InboxItem, 'runId' | 'nodeId' | 'iteration'>) {
  const triple = inboxTripleKey(it)
  unmarkProcessed(it)
  removeProcessingIntent(triple)
  // Reopen Active WS only if this item is still the selected pending row.
  if (
    active.value &&
    inboxTripleKey(active.value) === triple &&
    isActiveStillInList(active.value) &&
    !isProcessedTriple(active.value)
  ) {
    connectActiveRunWs(active.value.runId)
  }
}

/** Converge finished for one intent — other in-flight neighbors keep the list lock. */
function endProcessingIntent(it: Pick<InboxItem, 'runId' | 'nodeId' | 'iteration'>) {
  removeProcessingIntent(inboxTripleKey(it))
}

function isActiveStillInList(it: InboxItem | null | undefined = active.value) {
  if (!it) return false
  const key = itemKey(it)
  return listItems.value.some((row) => itemKey(row) === key)
}

function shouldFetchActiveInboxContext() {
  const it = active.value
  if (!it) return false
  if (isProcessedTriple(it)) return false
  if (!isActiveStillInList(it)) return false
  // Locally retained failure card: the server no longer has this item.
  if (startFailedActive.value) return false
  return true
}

/** The selected card is the retained boot-failure card. */
const startFailedActive = computed(() => {
  const failed = startFailedItem.value
  if (!failed || !active.value) return false
  return itemKey(active.value) === itemKey(failed)
})

/** The selected card's sandbox is still booting — show the boot loader. */
const activeStarting = computed(
  () => !startFailedActive.value && isStartingInboxItem(active.value),
)

/** Gateway lifecycle while starting (pulling | creating | …) for ClarifyBootLoader. */
const startingSandboxPhase = ref<string | null>(null)

/** Boot record key of the selected starting card (lib/run/bootRecord.ts). */
const startingBootKey = computed(() => {
  const it = active.value
  return activeStarting.value && it ? bootKey(it.runId, it.nodeId, it.iteration) : null
})

async function refreshStartingSandboxPhase() {
  if (!activeStarting.value || !active.value) {
    startingSandboxPhase.value = null
    return
  }
  const runId = active.value.runId
  const nodeId = active.value.nodeId
  const triple = inboxTripleKey(active.value)
  const key = bootKey(runId, nodeId, active.value.iteration)
  if (!runId || !nodeId) {
    startingSandboxPhase.value = null
    return
  }
  try {
    const sb = await api.getRunNodeSandbox(runId, nodeId)
    if (!activeStarting.value || !active.value || inboxTripleKey(active.value) !== triple) return
    // null is confirmed absence. The starting poll above is already capped.
    startingSandboxPhase.value = sb?.status ? String(sb.status) : null
    observeBootSandbox(key, sb)
  } catch {
    // Keep last known phase; list poll / next tick retries.
  }
}

/** Invalidate in-flight loadList writebacks (e.g. after local approve converge). */
function invalidateListLoads() {
  listLoadGeneration++
}

/** Drop a lagging list row so users cannot re-select a processed ghost. */
function removeListItemLocally(removedKey: string) {
  // Any older listGates still in flight must not restore this row after unlock.
  invalidateListLoads()
  const before = listItems.value.length
  listItems.value = listItems.value.filter((row) => itemKey(row) !== removedKey)
  const removed = before - listItems.value.length
  if (removed > 0) listTotal.value = Math.max(0, listTotal.value - removed)
  // Keep sidebar badge / singleton in lockstep with the page list.
  removeItemLocally(removedKey)
}

/**
 * Confirm/resume failed after optimistic leave — put the row back so the user can retry.
 * `prevList` is the snapshot taken at confirm initiation (preserves card order).
 * Re-inserts only this row into the current list so a concurrent neighbor leave is kept.
 */
function restoreListItemLocally(it: InboxItem, prevList: InboxItem[]) {
  invalidateListLoads()
  const key = itemKey(it)
  if (!listItems.value.some((row) => itemKey(row) === key)) {
    const idx = prevList.findIndex((row) => itemKey(row) === key)
    const next = listItems.value.slice()
    const insertAt = idx < 0 ? 0 : Math.min(idx, next.length)
    next.splice(insertAt, 0, it)
    listItems.value = next
    listTotal.value = Math.max(listTotal.value, next.length)
  }
  restoreItemLocally(it)
}

function conversationRoute(): { run: string; node: string } {
  const params = (route.params ?? {}) as Record<string, unknown>
  const run = queryText(params.runId)
  const node = queryText(params.nodeId)
  if (run || node) return { run, node }
  return { run: queryText(route.query.run), node: queryText(route.query.node) }
}

function incomingTarget(): { runId: string; nodeId: string } | null {
  if (!incomingArmed.value) return null
  const id = conversationRoute()
  return resolveIncomingApproval(
    id.run,
    id.node,
    peekHomeApproveHandoff() || homeSeed.value,
  )
}

function mergeIncomingGhost(items: InboxItem[]): InboxItem[] {
  const t = incomingTarget()
  if (!t) {
    incomingGhost.value = null
    return items
  }
  if (items.some((it) => it.runId === t.runId)) {
    // The server owns this run's rows from now on. Disarming here is what stops
    // the ghost from being rebuilt on every later load that no longer lists the
    // run — e.g. right after the approval is answered and leaves pending.
    incomingArmed.value = false
    incomingGhost.value = null
    return items
  }
  // Cold refresh / reopen with ?run= after the approval already left pending:
  // confirm against the run before keeping a "启动中" ghost forever.
  void confirmIncomingGhostStillNeeded(t)
  // Prefer a parked deep-link pin (plan g2.1) over rebuilding a starting card.
  const pinned = incomingGhost.value
  if (pinned && pinned.runId === t.runId && !isStartingInboxItem(pinned)) {
    return [pinned, ...items]
  }
  const ghost = withRunTitle(makeIncomingGhost(t, seedTextForRun(t.runId)))
  incomingGhost.value = ghost
  return [ghost, ...items]
}

/** Home handoff text, only when it belongs to this run (another run's text must never title it). */
function seedTextForRun(runId: string): string {
  const seed = peekHomeApproveHandoff() || homeSeed.value
  return seed && homeApproveHandoffMatchesRun(seed, runId) ? String(seed.text || '') : ''
}

/** Run title read while a ghost had no handoff text; reused by rebuilt ghosts of the same run. */
const ghostRunTitles = new Map<string, { title: string; workflowName: string }>()
/** An untitled starting row of a run whose title is known gets it; a titled one is never rewritten. */
function withRunTitle(it: InboxItem): InboxItem {
  const known = ghostRunTitles.get(it.runId)
  if (!known || it.type !== 'clarify' || !isStartingInboxItem(it) || it.runTitle?.trim() || it.workflowName) return it
  return { ...it, runTitle: known.title, label: it.label || known.title, workflowName: known.workflowName }
}
function fillGhostTitle(runId: string, run: Pick<Run, 'title' | 'workflowName'>) {
  const title = clipRunTitle(String(run.title || '').trim())
  const workflowName = String(run.workflowName || '').trim()
  if (!title && !workflowName) return
  if (!ghostRunTitles.has(runId)) ghostRunTitles.set(runId, { title, workflowName })
  const fill = (it: InboxItem): InboxItem => (it.runId === runId ? withRunTitle(it) : it)
  const ghost = incomingGhost.value
  if (ghost && fill(ghost) !== ghost) incomingGhost.value = fill(ghost)
  if (listItems.value.some((it) => fill(it) !== it)) listItems.value = listItems.value.map(fill)
  if (active.value && fill(active.value) !== active.value) active.value = fill(active.value)
}

/**
 * Drop (or fail) a client-only starting ghost when the run is no longer booting.
 * Without this, a stale `?run=&node=` after successful flow keeps rebuilding
 * an empty "启动中" card on every loadList.
 *
 * When the approve has parked (`waiting_human`) but the current filters omit the
 * row, pin it from an unfiltered peek so the deep-link target stays visible
 * until it truly leaves pending (plan g2.1).
 */
let incomingGhostConfirmInFlight = ''
async function confirmIncomingGhostStillNeeded(target: { runId: string; nodeId: string }) {
  const nodeId = target.nodeId || INCOMING_GHOST_NODE_ID
  const key = `${target.runId}:${nodeId}`
  // loadList / starting-poll can call this every few seconds for the same deep
  // link; one in-flight check is enough.
  if (incomingGhostConfirmInFlight === key) return
  incomingGhostConfirmInFlight = key
  try {
    const run = await api.getRun(target.runId)
    fillGhostTitle(target.runId, run)
    // A newer navigation may have re-armed a different target while we awaited.
    const cur = incomingTarget()
    if (!cur || `${cur.runId}:${cur.nodeId || INCOMING_GHOST_NODE_ID}` !== key) return
    if (isStartFailedRun(run, nodeId)) {
      const ghost =
        incomingGhost.value && itemKey(incomingGhost.value) === key
          ? incomingGhost.value
          : makeIncomingGhost(target, seedTextForRun(target.runId))
      incomingArmed.value = false
      incomingGhost.value = null
      queryWaitGen++
      await confirmStartingVanished(ghost)
      return
    }
    if (isClarifyStillStarting(run, nodeId)) return

    // Reload after 确认并流转: the confirmed chat left the inbox, so it is not a
    // pending row any more. Rebuild it from the run and follow on, as before the reload.
    if (run.clarifyByNode?.[nodeId]?.done) {
      incomingArmed.value = false
      incomingGhost.value = null
      queryWaitGen++
      await resumeHandedOff(run, nodeId)
      return
    }

    // Parked but maybe filtered out of loadList — pin the real pending row
    // and keep armed so later filtered loadList passes still merge it (g2.1).
    if (isClarifyAwaitingHuman(run, nodeId)) {
      try {
        const data = await listAllPendingGates()
        if (!incomingArmed.value) return
        const curAfter = incomingTarget()
        if (!curAfter || `${curAfter.runId}:${curAfter.nodeId || INCOMING_GHOST_NODE_ID}` !== key) return
        const hit = data.items.find((it) => it.runId === target.runId)
        if (hit) {
          incomingGhost.value = hit
          const hitKey = itemKey(hit)
          // A starting ghost uses the same run:node key as the real row. Replace it.
          const hasRealRow = listItems.value.some(
            (it) => itemKey(it) === hitKey && !isStartingInboxItem(it),
          )
          if (!hasRealRow) {
            const rest = listItems.value.filter(
              (it) => itemKey(it) !== hitKey && !(it.runId === target.runId && isStartingInboxItem(it)),
            )
            listItems.value = [hit, ...rest]
            listTotal.value = Math.max(listTotal.value, listItems.value.length)
          }
          if (!processingLock.value) ensureValidActive()
          return
        }
      } catch {
        // Transient unfiltered peek failure: keep the ghost; next load retries.
      }
      // Still pending but not yet listed — keep armed so the deep link stays.
      return
    }

    incomingArmed.value = false
    incomingGhost.value = null
    queryWaitGen++ // stop waitForQueryItem from polling a dead deep link
    if (holdsCancelledConversation()) {
      // The open conversation is leaving for the new-request page. Do not drop
      // it or open whatever row is still pending.
    } else if (listItems.value.some((it) => itemKey(it) === key)) {
      const prevList = listItems.value.slice()
      listItems.value = listItems.value.filter((it) => itemKey(it) !== key)
      if (active.value && itemKey(active.value) === key) {
        closeActiveRunWs()
        activeRun.value = null
        activeRunLoadError.value = false
        selectActiveAfterRemove(prevList, key)
      }
    } else if (active.value && itemKey(active.value) === key) {
      closeActiveRunWs()
      activeRun.value = null
      activeRunLoadError.value = false
      active.value = listItems.value[0] || null
    }
  } catch {
    // Transient getRun failure: keep the ghost; the starting poll / next load retries.
  } finally {
    if (incomingGhostConfirmInFlight === key) incomingGhostConfirmInFlight = ''
  }
}

function seedIncomingIfNeeded() {
  const ghostKey = incomingGhost.value ? itemKey(incomingGhost.value) : ''
  const withoutGhost = ghostKey
    ? listItems.value.filter((it) => itemKey(it) !== ghostKey)
    : listItems.value
  listItems.value = mergeIncomingGhost(withoutGhost)
  if (!processingLock.value) ensureValidActive()
}

function isIncomingContextPending(target: InboxItem): boolean {
  const ghost = incomingGhost.value
  if (!ghost || itemKey(ghost) !== itemKey(target)) return false
  // A real pending row pinned over the starting placeholder shares this key
  // (gates past the inbox page of 20). That pin already has a panel — keeping
  // the flag true leaves 「加载运行详情…」 up after inbox-context returns.
  return isStartingInboxItem(ghost)
}

/**
 * Re-bind active to the freshly loaded row when its starting flag flipped: the
 * sandbox parked, so the transcript now exists and the context must reload.
 */
function rebindActiveFromList(list: InboxItem[]) {
  const cur = active.value
  if (!cur) return
  const fresh = list.find((it) => itemKey(it) === itemKey(cur))
  if (!fresh || fresh === cur) return
  // Only starting↔parked needs a new active object (transcript now exists and
  // watch(active) hard-reloads context). Badge-only state=replying must not
  // replace active — that remounts ClarifyChat and wipes live stream bubbles.
  if (isStartingInboxItem(fresh) !== isStartingInboxItem(cur)) active.value = fresh
}

/** Patch current-page + sidebar cards from sessionBusy without triggering the update banner. */
function patchVisibleCardBusy(runId: string, nodeId: string, busy: boolean) {
  const key = `${runId}:${nodeId}`
  let listChanged = false
  const nextList = listItems.value.map((row) => {
    if (itemKey(row) !== key || row.type !== 'clarify') return row
    const patched = applyInboxReplyingState(row, busy)
    if (patched !== row) {
      listChanged = true
      noteInboxStatePatch(key)
    }
    return patched
  })
  if (listChanged) listItems.value = nextList
  // Do not replace active.value here. watch(active) always loadActiveRun(true),
  // which nulls activeRun and unmounts the live composer mid-turn.
  patchItemReplying(key, busy)
}

/** Keep the retained failure card visible until it is dismissed or reappears. */
function mergeFailedStarting(rows: InboxItem[]): InboxItem[] {
  const failed = startFailedItem.value
  if (!failed) return rows
  const key = itemKey(failed)
  if (rows.some((it) => itemKey(it) === key)) {
    startFailedItem.value = null
    return rows
  }
  return [failed, ...rows]
}

/**
 * A starting card vanished from the list. Confirm against the run before
 * showing a failure: a filter/page change can drop a live row too.
 */
async function confirmStartingVanished(it: InboxItem) {
  try {
    if (!isStartFailedRun(await api.getRun(it.runId), it.nodeId)) return
  } catch {
    return
  }
  if (startFailedItem.value && itemKey(startFailedItem.value) === itemKey(it)) return
  startFailedItem.value = it
  if (!listItems.value.some((row) => itemKey(row) === itemKey(it))) {
    listItems.value = [it, ...listItems.value]
  }
  // The list drop may have cleared/moved selection while we were confirming.
  if (!active.value || itemKey(active.value) !== itemKey(it)) {
    closeActiveRunWs()
    activeRun.value = null
    activeRunLoadError.value = false
    active.value = it
  }
}

function detectStartingFailures(before: InboxItem[], rows: InboxItem[]) {
  const ghostKey = incomingGhost.value ? itemKey(incomingGhost.value) : ''
  for (const it of vanishedStartingRows(before, rows, itemKey, ghostKey)) {
    void confirmStartingVanished(it)
  }
}

/** Dismiss the retained failure card and move selection on. */
function dismissStartFailure() {
  const it = startFailedItem.value
  startFailedItem.value = null
  if (!it) return
  const key = itemKey(it)
  const prevList = listItems.value.slice()
  markProcessed(it)
  removeListItemLocally(key)
  if (active.value && itemKey(active.value) === key) {
    closeActiveRunWs()
    activeRun.value = null
    activeRunLoadError.value = false
    selectActiveAfterRemove(prevList, key)
  }
}

/**
 * While a starting card is selected we are waiting for the sandbox to park. WS
 * status frames drive that; this poll is the fallback and, once armed, the only
 * loop that keeps reloading the list. Bounded so a node stuck in `running`
 * cannot leave a tab polling forever — WS frames still refresh after that.
 */
const STARTING_POLL_MS = 2500
const STARTING_POLL_MAX_TICKS = 240
let startingPollTimer: number | undefined
function stopStartingPoll() {
  if (startingPollTimer) window.clearInterval(startingPollTimer)
  startingPollTimer = undefined
}
function startStartingPoll() {
  stopStartingPoll()
  let ticks = 0
  void refreshStartingSandboxPhase()
  startingPollTimer = window.setInterval(() => {
    if (!activeStarting.value || ++ticks > STARTING_POLL_MAX_TICKS) {
      stopStartingPoll()
      startingSandboxPhase.value = null
      return
    }
    void loadList()
    void refreshStartingSandboxPhase()
  }, STARTING_POLL_MS)
}

function selectFromQuery({ keepActive = false }: { keepActive?: boolean } = {}): boolean {
  const id = conversationRoute()
  const hit = findInboxItemForQuery(listItems.value, id.run, id.node)
  if (!hit) return false
  // Silent auto-apply reload with the same gate already selected: keep the
  // object. A fresh row for the same key would retrigger watch(active) →
  // loadActiveRun(true) and remount the chat with its focused composer.
  // Starting↔parked still swaps (rebindActiveFromList + this check).
  const cur = active.value
  if (
    keepActive &&
    cur &&
    itemKey(cur) === itemKey(hit) &&
    isStartingInboxItem(cur) === isStartingInboxItem(hit)
  ) {
    return true
  }
  active.value = hit
  return true
}

function applyHomeHandoff() {
  const it = active.value
  if (!it) return
  const h = consumeHomeApproveHandoff(it.runId, it.nodeId)
  if (h) homeSeed.value = h
}

function selectFromHandoff(): boolean {
  const h = peekHomeApproveHandoff()
  if (!h) return false
  const hit = findInboxItemForHandoff(listItems.value, h)
  if (!hit) return false
  active.value = hit
  applyHomeHandoff()
  return true
}

let queryWaitGen = 0
async function waitForQueryItem() {
  const gen = ++queryWaitGen
  if (!incomingTarget()) return
  seedIncomingIfNeeded()
  for (let i = 0; i < 40; i++) {
    if (gen !== queryWaitGen) return
    if (selectFromQuery()) applyHomeHandoff()
    else selectFromHandoff()
    // Done once the real row is selected, a starting card is up (bounded poll
    // owns the rest), or a parked deep-link pin replaced the starting ghost.
    if (
      active.value &&
      (!incomingGhost.value ||
        activeStarting.value ||
        !isStartingInboxItem(incomingGhost.value))
    ) {
      return
    }
    await new Promise((r) => setTimeout(r, 400))
    if (gen !== queryWaitGen) return
    await loadList()
  }
}

/**
 * Sidebar cancel of the conversation on screen. Hold that conversation until
 * the sidebar finishes leaving /gates, so a list refresh cannot clear it
 * (empty inbox) or replace it with the next pending row.
 */
function holdsCancelledConversation(): boolean {
  const departing = inboxCancelDepartureRunId()
  if (!departing) return false
  if (active.value?.runId === departing) return true
  return conversationRoute().run === departing
}

/**
 * List non-empty + invalid/missing active → pick first valid item.
 * Empty list → clear active (full empty inbox). Never leave a Run # - shell.
 * A sidebar cancel of this conversation skips both: the page is about to leave.
 */
function ensureValidActive({ keepActive = false }: { keepActive?: boolean } = {}) {
  if (processingLock.value) return
  // Following a run after hand-off: the confirmed conversation stays although it left the list.
  if (isFollowingActive()) return
  if (holdsCancelledConversation()) return
  if (selectFromQuery({ keepActive })) {
    applyHomeHandoff()
    return
  }
  if (selectFromHandoff()) return
  const list = listItems.value
  if (!list.length) {
    if (active.value) active.value = null
    return
  }
  if (active.value && list.some((it) => itemKey(it) === itemKey(active.value!))) {
    return
  }
  active.value = list[0]
}

/**
 * Allow re-fetch only after a processed triple was confirmed gone, then reappears as pending.
 * Avoids clearing the mark on the immediate lagging loadList that still contains the item.
 */
function reconcileProcessedWithList(list: InboxItem[]) {
  const pending = new Set(list.map((it) => inboxTripleKey(it)))
  for (const key of [...processedTriples]) {
    if (!pending.has(key)) {
      confirmedAbsentTriples.add(key)
      continue
    }
    if (confirmedAbsentTriples.has(key)) {
      processedTriples.delete(key)
      confirmedAbsentTriples.delete(key)
    }
  }
}

/** After an item leaves the list: neighbor active (or null). Never keep stale active. */
/**
 * Where the inbox goes after an inline hand-off. Returns false to fall back to
 * the usual next pending item (selectActiveAfterRemove).
 * Same run first: a clarify/review next node continues in place.
 */
function pickHandoffNext(submitted: InboxItem, removedKey: string): boolean {
  // 方案 B: follow the run — when its next node is already waiting for the user
  // (clarify / review), open that conversation instead of the next inbox row.
  const next = listItems.value.find((k) => k.runId === submitted.runId && itemKey(k) !== removedKey)
  if (!next) return false
  active.value = next
  showProcessedBanner.value = false
  return true
}

function selectActiveAfterRemove(prevList: InboxItem[], removedKey: string) {
  if (holdsCancelledConversation()) return
  active.value = pickNextActiveAfterRemove(prevList, removedKey, itemKey)
  showProcessedBanner.value = false
  if (isMobile.value && !listItems.value.length) mobileView.value = 'list'
}

/** Quietly converge when inbox-context says the item left pending. */
function handleLeftInboxContext(it: InboxItem) {
  if (isInboxCancelDeparture(it.runId) && holdsCancelledConversation()) return
  markProcessed(it)
  const key = itemKey(it)
  const prevList = listItems.value.some((row) => itemKey(row) === key)
    ? listItems.value.slice()
    : [...listItems.value, it]
  // Sync UI: lagging list must not keep a clickable ghost with shouldFetch=false.
  removeListItemLocally(key)
  const stillActive = active.value !== null && itemKey(active.value) === key
  if (!stillActive) return
  closeActiveRunWs()
  activeRun.value = null
  activeRunLoadError.value = false
  selectActiveAfterRemove(prevList, key)
}

async function loadList({
  showLoading = false,
  keepActive = false,
}: { showLoading?: boolean; keepActive?: boolean } = {}) {
  const snapshotEpoch = currentInboxStateEpoch()
  const gen = ++listLoadGeneration
  listLoadError.value = null
  if (showLoading) listLoading.value = true
  try {
    const data = await api.listGates({
      page: listPage.value,
      pageSize: PAGE_SIZE,
      wf: selected.value || undefined,
      tag: selectedTags.value.join(',') || undefined,
    })
    // Discard overdue snapshots so they cannot resurrect a just-removed gate.
    if (gen !== listLoadGeneration) return
    listSnapshotForNeighbor = listItems.value.slice()
    const rows = data.items.map((row) => {
      if (!isStartingInboxItem(row)) return row
      const boot = getBootRecord(bootKey(row.runId, row.nodeId, row.iteration))
      const startedAt = Date.parse(row.requestedAt || '')
      // A list captured before this visit became ready cannot put it back in
      // startup. A different start timestamp or iteration is a different visit.
      if (!boot?.live || !Number.isFinite(startedAt) || boot.nodeStartedAt !== startedAt) return row
      const selected = active.value
      const sameVisit = selected?.runId === row.runId && selected.nodeId === row.nodeId &&
        (selected.iteration ?? 1) === (row.iteration ?? 1) &&
        Date.parse(selected.requestedAt || '') === startedAt
      return { ...row, state: sameVisit && reviewState.session(row.nodeId)?.busy ? 'replying' : undefined } as InboxItem
    })
    const total = data.total
    detectStartingFailures(listItems.value, rows)
    // Hide locally-confirmed rows even if the server list still lags (f4 ghost guard).
    const visible = rows.filter((it) => !isProcessedTriple(it))
    listItems.value = mergeFailedStarting(mergeIncomingGhost(visible))
    listTotal.value = Math.max(total - (rows.length - visible.length), listItems.value.length)
    rebindActiveFromList(listItems.value)
    listLoadError.value = null
    reconcileProcessedWithList(rows)
    // Independent loadList success path: repair invalid selection (no Run # - shell).
    if (!processingLock.value) ensureValidActive({ keepActive })
    // Align peek banner baseline with the visible list (plan g1.2 / g2.1).
    syncDisplayedBaseline(listItems.value, snapshotEpoch)
  } catch (err) {
    if (gen !== listLoadGeneration) return
    /* keep previous list on transient failure; surface error when nothing to show */
    listLoadError.value =
      err instanceof Error && err.message
        ? err.message
        : String(t('common.asyncState.loadFailedDesc'))
  } finally {
    if (gen === listLoadGeneration) listLoading.value = false
  }
}

function retryListLoad() {
  void loadList({ showLoading: true })
}

// The selected clarify item vanished from /gates (run ended under it): if its
// unsent text was kept for resume, say so — the composer it was in is gone.
watch(active, (_cur, prev) => {
  if (!prev || prev.type !== 'clarify' || prev.done) return
  const prevKey = itemKey(prev)
  if (listItems.value.some((x) => itemKey(x) === prevKey)) return
  // The chat stashes on unmount; read after it has gone.
  void nextTick(() => {
    if (readClarifyStash(prev.runId, prev.nodeId)) toast.warn(t('pages.clarify.draftKept'))
  })
})

watch(selected, () => {
  listPage.value = 1
  loadList({ showLoading: true })
})

watch(listPage, () => {
  loadList({ showLoading: true })
})
watch(() => route.query.tag, () => {
  listPage.value = 1
  loadList({ showLoading: true })
})

const isGateEditing = computed(() => gateApprovalRef.value?.isEditing ?? false)

const {
  draft: clarifyDraft,
  attachments: clarifyAttachments,
  annotations: clarifyAnnotations,
} = useClarifyDraft(
  () => (active.value?.type === 'clarify' ? active.value.runId : null),
  () => (active.value?.type === 'clarify' ? active.value.nodeId : null),
)

/** VNC pick on the app-preview inbox stage → same ReAct annotation chips as Run review. */
const lastStagedAppPreviewPick = ref<AppPreviewPickPayload | null>(null)

watch(
  () => (active.value ? itemKey(active.value) : ''),
  () => {
    lastStagedAppPreviewPick.value = null
  },
)

function onAppPreviewStagedPick(payload: AppPreviewPickPayload | null) {
  lastStagedAppPreviewPick.value = payload
}

function onAppPreviewReviewPick(payload: AppPreviewPickPayload) {
  const rid = active.value?.type === 'clarify' ? active.value.runId : ''
  const nid = active.value?.type === 'clarify' ? active.value.nodeId : ''
  if (!rid || !nid) return
  const result = addClarifyAnnotation(rid, nid, previewPickAnnotation(payload))
  if (result === 'duplicate') toast.warn(t('pages.reviewComposer.alreadyAdded'))
  lastStagedAppPreviewPick.value = null
}

function mergeStagedAppPreviewPick(
  annotations: import('@/lib/shared/types').ReactAnnotation[],
): import('@/lib/shared/types').ReactAnnotation[] {
  const staged = lastStagedAppPreviewPick.value
  if (!staged?.selector) return annotations
  const url = (staged.url || '').trim()
  if (
    annotations.some(
      (a) => a.selector === staged.selector && (a.url || '').trim() === url,
    )
  ) {
    return annotations
  }
  return [...annotations, previewPickAnnotation(staged)]
}

const isClarifyEditing = computed(
  () =>
    clarifyDraft.value.trim().length > 0 ||
    clarifyAttachments.value.length > 0 ||
    clarifyAnnotations.value.length > 0,
)

const isEditing = computed(() => isGateEditing.value || isClarifyEditing.value)

/**
 * The focused text field (gate form, clarify composer, filters…) holds a
 * non-empty draft. Counted as editing for list auto-apply so the caret and the
 * typed text are never disturbed; an empty focused field (e.g. the composer
 * right after sending) does not freeze the list.
 */
const focusedFieldHasDraft = ref(false)
function isTextEntry(el: Element | null): el is HTMLElement {
  if (!(el instanceof HTMLElement)) return false
  if (el.isContentEditable) return true
  if (el instanceof HTMLTextAreaElement) return true
  if (el instanceof HTMLInputElement) {
    return !['button', 'checkbox', 'radio', 'submit', 'reset', 'file', 'image', 'range', 'color'].includes(el.type)
  }
  return false
}
function fieldHasDraft(el: HTMLElement): boolean {
  if (el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement) return el.value.trim().length > 0
  return (el.textContent || '').trim().length > 0
}
/**
 * The user typed into the focused field within the last TYPING_IDLE_MS. Clearing
 * a draft (Ctrl+A, Backspace) and typing on must not count as "done editing":
 * the hold lasts until the field blurs or input has been idle for a while.
 */
const typingRecently = ref(false)
let typingIdleTimer: ReturnType<typeof setTimeout> | null = null
function clearTypingIdle() {
  if (typingIdleTimer) clearTimeout(typingIdleTimer)
  typingIdleTimer = null
  typingRecently.value = false
}
function syncFocusedFieldDraft() {
  const el = typeof document !== 'undefined' ? document.activeElement : null
  const entry = isTextEntry(el)
  focusedFieldHasDraft.value = entry && fieldHasDraft(el)
  if (!entry) clearTypingIdle()
}
function onFieldInput(e: Event) {
  syncFocusedFieldDraft()
  const el = typeof document !== 'undefined' ? document.activeElement : null
  if (!isTextEntry(el) || (e.target instanceof Node && !el.contains(e.target))) return
  typingRecently.value = true
  if (typingIdleTimer) clearTimeout(typingIdleTimer)
  typingIdleTimer = setTimeout(() => {
    typingIdleTimer = null
    typingRecently.value = false
  }, TYPING_IDLE_MS)
}
function onFocusOut() {
  // activeElement is still the old field during focusout; read it next tick.
  void nextTick(syncFocusedFieldDraft)
}

/**
 * List auto-apply waits while the user is working: a non-empty gate form or
 * clarify draft, a non-empty draft in the focused field, typing in the focused
 * field within TYPING_IDLE_MS (an emptied draft the user keeps typing into), or
 * a processing intent (approve / confirm in flight). Pending membership changes
 * apply once this clears: on blur, or after input has gone idle.
 */
let releaseAutoApplyHold: (() => void) | null = null
const autoApplyHeld = computed(
  () => isEditing.value || focusedFieldHasDraft.value || typingRecently.value || processingLock.value,
)

function syncActiveAfterApply(list: InboxItem[], prevKey: string | null) {
  // Processing intent owns selection until converge/unlock — ignore list-apply swaps.
  if (processingLock.value) return
  if (isFollowingActive()) return
  if (holdsCancelledConversation()) return
  if (!active.value && list.length) {
    active.value = list[0]
    return
  }
  if (!active.value) return

  const key = prevKey ?? itemKey(active.value)
  const stillExists = list.some((it) => itemKey(it) === key)

  if (stillExists) {
    showProcessedBanner.value = false
    return
  }

  if (isEditing.value) {
    showProcessedBanner.value = true
    return
  }

  const prevList =
    listSnapshotForNeighbor.some((it) => itemKey(it) === key)
      ? listSnapshotForNeighbor
      : active.value
        ? [active.value, ...list.filter((it) => itemKey(it) !== key)]
        : list
  active.value = pickNextActiveAfterRemove(prevList, key, itemKey)
  showProcessedBanner.value = false
  if (isMobile.value && !list.length) mobileView.value = 'list'
}

function checkProcessedWhileEditing() {
  if (!active.value || !isEditing.value) {
    if (!isEditing.value) showProcessedBanner.value = false
    return
  }
  const key = itemKey(active.value)
  const inRemote = remoteItems.value.some((it) => itemKey(it) === key)
  if (!inRemote) showProcessedBanner.value = true
}

watch(lastPeekAt, () => {
  checkProcessedWhileEditing()
})

// Hold ended (blur, draft cleared, processing done): apply what the peek found.
watch(autoApplyHeld, (held) => {
  if (!held) autoApplyPending()
})

// usePendingGates applied a membership change to displayedItems (sidebar):
// reload this page's list silently, keep the selection and the scroll offset.
watch(lastAutoAppliedAt, () => {
  void syncListAfterAutoApply()
})

/**
 * Snapshot the focused text field so a silent list reload can never take the
 * caret away. If the field is still connected nothing happens; if a re-render
 * replaced it, focus moves to its replacement (same data-testid) with the same
 * selection — but only when focus fell to <body>, never away from a field the
 * user moved to themselves.
 */
function captureFocus(): (() => void) | null {
  if (typeof document === 'undefined') return null
  const el = document.activeElement
  if (!isTextEntry(el)) return null
  const tid = el.getAttribute('data-testid')
  const sel =
    el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement
      ? { start: el.selectionStart, end: el.selectionEnd }
      : null
  return () => {
    if (el.isConnected || !tid) return
    const cur = document.activeElement
    if (cur && cur !== document.body) return
    const next = document.querySelector(`[data-testid="${CSS.escape(tid)}"]`)
    if (!(next instanceof HTMLElement) || !isTextEntry(next)) return
    next.focus({ preventScroll: true })
    if (sel && (next instanceof HTMLInputElement || next instanceof HTMLTextAreaElement)) {
      const max = next.value.length
      next.setSelectionRange(Math.min(sel.start ?? max, max), Math.min(sel.end ?? max, max))
    }
  }
}

let autoApplyGen = 0
async function syncListAfterAutoApply() {
  const gen = ++autoApplyGen
  const restoreFocus = captureFocus()
  const prevKey = active.value ? itemKey(active.value) : null
  const scrollTop = listEl.value?.scrollTop ?? null
  await loadList({ keepActive: true })
  if (gen !== autoApplyGen) return
  // The user may have started typing while listGates was in flight: keep the
  // current selection untouched; the processed banner still covers removal.
  if (!autoApplyHeld.value) syncActiveAfterApply(listItems.value, prevKey)
  else checkProcessedWhileEditing()
  await nextTick()
  restoreFocus?.()
  if (scrollTop != null) {
    if (listEl.value && listEl.value.scrollTop !== scrollTop) listEl.value.scrollTop = scrollTop
  }
}

// Peek/poll may only change state=replying on existing keys. Merge onto the
// current page so badges follow sessionBusy without hanging the update banner.
watch(remoteItems, (remote) => {
  if (!remote.length) return
  const merged = mergeInboxReplyingFromRemote(
    listItems.value,
    remote,
    itemKey,
    inboxRemoteObservedAt?.(),
  )
  if (merged !== listItems.value) listItems.value = merged
})

watch(isEditing, (editing) => {
  if (!editing) showProcessedBanner.value = false
  else checkProcessedWhileEditing()
})

watch(isMobile, (mobile) => {
  if (!mobile) mobileView.value = 'list'
})

watch(
  [incomingGhost, startFailedItem],
  () => {
    const extras: InboxItem[] = []
    const failed = startFailedItem.value
    const ghost = incomingGhost.value
    if (failed) extras.push(failed)
    if (ghost && (!failed || itemKey(ghost) !== itemKey(failed))) extras.push(ghost)
    setSidebarExtras(extras)
    if (failed) markStartFailed(itemKey(failed))
  },
  { immediate: true },
)

watch(startFailedItem, (it, prev) => {
  if (prev && (!it || itemKey(it) !== itemKey(prev))) clearStartFailed(itemKey(prev))
})

watch(
  active,
  (it) => {
    setActiveInboxKey(it ? itemKey(it) : null)
  },
  { immediate: true },
)

watch(
  startingBootKey,
  (key, prev) => {
    if (key) beginBoot(key, active.value?.requestedAt)
    if (!prev || key === prev) return
    const it = active.value
    // Same card left `starting` without a boot failure → the session is live.
    if (!it || bootKey(it.runId, it.nodeId, it.iteration) !== prev || startFailedActive.value) return
    markBootLive(prev)
    // Ready usually lands between two polls: read the row once for its server timestamp.
    void api.getRunNodeSandbox(it.runId, it.nodeId)
      .then((sb) => observeBootSandbox(prev, sb))
      .catch(() => {})
  },
  { immediate: true },
)

watch(
  activeStarting,
  (starting) => {
    if (starting) startStartingPoll()
    else stopStartingPoll()
  },
  { immediate: true },
)

function isActive(it: InboxItem) {
  return active.value !== null && itemKey(active.value) === itemKey(it)
}

const activeRun = ref<Run | null>(null)
watch(activeRun, (run) => {
  const text = run && Object.values(run.clarifyByNode ?? {}).find((c) => c.firstMessage?.trim())?.firstMessage?.trim()
  if (run && text && queuedFirstMessages.value[run.id] !== text) {
    queuedFirstMessages.value = { ...queuedFirstMessages.value, [run.id]: text }
  }
})
const activeRunLoading = ref(false)
const activeRunLoadError = ref(false)

/** Active-run event socket — mirrors RunDetailView artifact_edit/react → reload. */
let activeRunWs: WebSocket | undefined
let activeRunWsRunId = ''
/**
 * Live busy from review/acp WS frames (not stale activeRun.reactSessions snapshot).
 * Used to gate softRefresh while clarify session is mid-turn (g3.2 / review v3).
 */
const clarifyLiveBusy = ref(false)
/** This user's page-control state per node of the active run (page_control_state frames). */
const pageControlByNode = ref<Record<string, PageControlState>>({})
/** True once thought/message rails were applied to a dialogue surface (seed or live). */
let dialogueRailsFilled = false
/** True once a live WS ACP frame applied content (stops seed retry per f2). */
let dialogueLiveIncremental = false
/** Node whose sandbox is still writing after the platform queue went idle. */
let orphanLiveNodeId = ''
const busySeedRetry = createBusySeedRetryController()
const reviewState = createReviewSessionState()
let heartbeatRecoveryCheckpoint: ReviewRecoveryCheckpoint | null = null
const activeRunWsReconnect = createWsReconnectController({
  connect: () => {
    const id = active.value?.runId
    if (id) connectActiveRunWs(id, { fromReconnect: true })
  },
  shouldReconnect: () =>
    !!active.value?.runId &&
    (active.value.type === 'clarify' || active.value.type === 'gate') &&
    !isProcessedTriple(active.value) &&
    isActiveStillInList(active.value),
})
const activeRunHeartbeat = createWsHeartbeat({
  onTimeout: () => {
    const socket = activeRunWs
    activeRunWs = undefined
    activeRunWsRunId = ''
    socket?.close()
    activeRunWsReconnect.onClose()
  },
})

function isClarifySoftRefreshBlocked(): boolean {
  if (active.value?.type !== 'clarify') return false
  if (clarifyLiveBusy.value) return true
  if (reviewChatRef.value?.isSessionBusy?.()) return true
  return false
}

function closeActiveRunWs() {
  reviewState.reset()
  heartbeatRecoveryCheckpoint = null
  activeRunHeartbeat.stop()
  stopOrphanRealign()
  orphanLiveNodeId = ''
  stopOrphanLiveSeed()
  busySeedRetry.stop()
  activeRunWsReconnect.markIntentionalClose()
  const socket = activeRunWs
  activeRunWs = undefined
  socket?.close()
  activeRunWsRunId = ''
  clarifyLiveBusy.value = false
  pageControlByNode.value = {}
  dialogueRailsFilled = false
  dialogueLiveIncremental = false
  pendingReviewFrames = []
  pendingAcpFrames.clear()
  pendingSnapshotSessions = null
}

/** Inbox context carries the server dialogue of every node of the run. */
function ingestContextDialogues(ctx: unknown, runId: string) {
  if (isInboxLeftPending(ctx as never)) return
  ingestDialogues((ctx as { dialogueByNode?: Record<string, unknown> }).dialogueByNode, runId)
}

/** Producer nodeId for busy session restore (clarify node or gate react upstream). */
function activeDialogueNodeId(r?: Run | null): string | null {
  if (active.value?.type === 'clarify') return active.value.nodeId
  if (active.value?.type === 'gate') {
    return (
      r?.gate?.reactUpstreamNodeId ||
      activeRun.value?.gate?.reactUpstreamNodeId ||
      (active.value as Gate).reactUpstreamNodeId ||
      null
    )
  }
  return null
}

/** Deliver ACP to chat/gate, or buffer until surface remounts after hard load. */
function applyOrBufferAcpFrame(m: {
  nodeId?: string
  events?: AcpEvent[]
  busy?: boolean
}) {
  const events = Array.isArray(m.events) ? m.events : []
  const nodeId = m.nodeId || ''
  // ClarifyChat draws the server dialogue state; only the gate still takes ACP.
  const forClarify = false
  const forGate = active.value?.type === 'gate'
  const result = deliverOrBufferDialogueAcp({
    forClarify,
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
  // Hard-load / slot-not-ready: keep latest cumulative ACP for seed-then-live.
  if (result === 'buffer' && (forClarify || forGate)) {
    pendingAcpFrames.push({
      nodeId: m.nodeId,
      events,
      busy: m.busy,
    })
  }
}

function flushPendingAcpFrames() {
  if (!pendingAcpFrames.size) return
  const frames = pendingAcpFrames.takeAll()
  for (const frame of frames) {
    applyOrBufferAcpFrame(frame)
  }
}

/**
 * One-shot REST seed from LiveNodeEvents / persisted snapshot.
 * Returns true only when non-empty rails were actually applied (not merely buffered).
 *
 * Contract (multi-turn hard refresh): platform nodeEvents live seed is the
 * current turn only — pickAcpRails / applyAcpEvents write those absolute rails
 * into the live streaming bubble. Historical turns stay in transcript bubbles
 * rebuilt by queue_state / persisted turns, never concatenated here.
 */
async function readClarifyAcpFromNodeEventsOnce(
  runId: string,
  nodeId: string,
  isCurrent: () => boolean,
  signal: AbortSignal,
): Promise<boolean> {
  if (active.value?.type !== 'gate') return false
  try {
    if (signal.aborted || !isCurrent()) return false
    const r = await api.nodeEvents(runId, nodeId, { limit: 50, signal })
    if (signal.aborted || !isCurrent()) return false
    if (isNodeEventsUnavailable(r)) return false
    if (!r.events?.length) return false
    if (!hasAcpContent(r.events)) return false
    const result = deliverOrBufferDialogueAcp({
      forClarify: false,
      forGate: true,
      events: r.events,
      nodeId,
      applyClarify: () => false,
      applyGate: (evs) => {
        if (!gateApprovalRef.value?.applyAcpEvents) return false
        return gateApprovalRef.value.applyAcpEvents(evs) !== false
      },
    })
    if (result === 'buffer') {
      pendingAcpFrames.push({ nodeId, events: r.events, busy: true })
      return false
    }
    dialogueRailsFilled = true
    return true
  } catch {
    /* soft-fail / empty — caller retries while busy */
    return false
  }
}

/** Standalone startup/retry/orphan reads share the recovery lane. Their
 * captured selection and socket still own the response when it returns. */
async function seedClarifyAcpFromNodeEventsOnce(
  runId: string,
  nodeId: string,
  callerSignal?: AbortSignal,
): Promise<boolean> {
  const target = active.value
  if (!target || target.runId !== runId || activeDialogueNodeId() !== nodeId || callerSignal?.aborted) return false
  const triple = inboxTripleKey(target)
  const socket = activeRunWs
  let applied = false
  await reviewState.requestRecovery(async (checkpoint, recoverySignal) => {
    const controller = new AbortController()
    const abort = () => controller.abort()
    recoverySignal.addEventListener('abort', abort, { once: true })
    callerSignal?.addEventListener('abort', abort, { once: true })
    if (recoverySignal.aborted || callerSignal?.aborted) abort()
    const isCurrent = () => !controller.signal.aborted && activeRunWs === socket &&
      !!active.value && inboxTripleKey(active.value) === triple &&
      activeDialogueNodeId() === nodeId && reviewState.isCurrent(checkpoint, nodeId)
    try {
      applied = await readClarifyAcpFromNodeEventsOnce(runId, nodeId, isCurrent, controller.signal)
    } finally {
      recoverySignal.removeEventListener('abort', abort)
      callerSignal?.removeEventListener('abort', abort)
    }
  })
  return applied
}

/** Busy-guarded seed with periodic retry until content / live / idle (f2). */
function startBusySeedRetry(runId: string, nodeId: string) {
  busySeedRetry.start(async (signal) => {
    await runBusySeedRetry({
      signal,
      isBusy: () => {
        if (clarifyLiveBusy.value) return true
        const snap = activeRun.value?.reactSessions?.[nodeId]
        return !!snap?.busy
      },
      hasContent: () => dialogueRailsFilled,
      liveIncrementalReceived: () => dialogueLiveIncremental,
      seed: async () => seedClarifyAcpFromNodeEventsOnce(runId, nodeId, signal),
    })
  })
}

/** Workspace authoritative busy/queue onto ClarifyChat / GateApproval. */
function restoreReactSessions(r: { reactSessions?: Run['reactSessions']; reviewSessionGeneration?: number; gate?: Run['gate'] } | null) {
  const nodeId = activeDialogueNodeId(r as Run | null)
  if (!nodeId) return
  const sessions = r?.reactSessions ?? pendingSnapshotSessions
  const frame = reviewState.reconcileSnapshot({
    reactSessions: sessions ?? undefined,
    reviewSessionGeneration: r?.reviewSessionGeneration,
  }, undefined, [nodeId]).find(frame => frame.nodeId === nodeId)
  if (!frame) return
  deliverReviewQueueFrame(frame)
  pendingSnapshotSessions = null
}

function deliverReviewQueueFrame(frame: ReviewSessionFrame) {
  clarifyLiveBusy.value = !!frame.busy
  let delivered = false
  if (active.value?.type === 'gate' && gateApprovalRef.value?.applyReviewFrame) {
    gateApprovalRef.value.applyReviewFrame(frame)
    delivered = true
  }
  if (!delivered && active.value?.type === 'gate') pendingReviewFrames.push(frame)
}

function flushPendingReviewFrames() {
  if (!pendingReviewFrames.length) return
  const frames = pendingReviewFrames
  pendingReviewFrames = []
  for (const frame of frames) {
    if (!reviewState.acceptServer(frame)) continue
    gateApprovalRef.value?.applyReviewFrame?.(frame)
  }
}

/** Deliver review frame to chat/gate, or buffer until surface remounts after hard load. */
function applyOrBufferReviewFrame(m: Record<string, unknown>) {
  let applied = false
  if (gateApprovalRef.value?.applyReviewFrame) {
    gateApprovalRef.value.applyReviewFrame(m)
    applied = true
  }
  if (m.event === 'request_rejected' && active.value?.type === 'clarify' && m.nodeId === activeDialogueNodeId()) {
    reviewChatRef.value?.applyRequestRejected?.(m)
  }
  if (!applied && active.value?.type === 'gate') {
    pendingReviewFrames.push(m)
  }
}

/**
 * After hard load / WS reconnect: project reactSessions + flush frames buffered
 * while ReviewComposer / GateApproval was unmounted (WS Broadcast race).
 * Order: queue_state → seed nodeEvents → flush pendingAcpBuffer → live (g2.1).
 * Covers clarify and gate.
 */
async function projectClarifySessionAfterLoad(r: Run, isCurrent = () => true) {
  const target = active.value
  if (!target || (r.id && target.runId !== r.id)) return
  const triple = inboxTripleKey(target)
  const socket = activeRunWs
  const current = () => isCurrent() && activeRunWs === socket &&
    !!active.value && inboxTripleKey(active.value) === triple
  dialogueRailsFilled = false
  dialogueLiveIncremental = false
  await nextTick()
  if (!current()) return
  restoreReactSessions(r)
  await nextTick()
  if (!current()) return
  flushPendingReviewFrames()
  await nextTick()
  if (!current()) return
  const nodeId = activeDialogueNodeId(r)
  const snap = nodeId ? r.reactSessions?.[nodeId] : null
  if (snap?.busy || !(snap?.sandboxBusy || snap?.sandboxDesynced)) {
    orphanLiveNodeId = ''
    stopOrphanLiveSeed()
  }
  if (nodeId && snap?.busy && active.value?.runId) {
    // First attempt immediately, then busy-guarded retry (g3).
    await seedClarifyAcpFromNodeEventsOnce(target.runId, nodeId)
    if (!current()) return
    await nextTick()
    if (!current()) return
    flushPendingAcpFrames()
    if (!dialogueRailsFilled && !dialogueLiveIncremental) {
      startBusySeedRetry(active.value.runId, nodeId)
    }
  } else {
    // Authority not busy (f3): queue_state already tore down empty placeholder.
    busySeedRetry.stop()
    flushPendingAcpFrames()
    // Platform queue is idle, but the sandbox is still writing. Keep pulling
    // the same live log a refresh would replay, and paint it on the open page.
    if (nodeId && active.value?.runId && (snap?.sandboxBusy || snap?.sandboxDesynced)) {
      orphanLiveNodeId = nodeId
      await seedClarifyAcpFromNodeEventsOnce(target.runId, nodeId)
      if (!current()) return
      scheduleOrphanLiveSeed(active.value.runId, nodeId)
    }
  }
}

/** Re-seed after WS reconnect / snapshot — same depth as hard refresh (g4.2). */
async function reseedAfterWsReconnect(runId: string, isCurrent = () => true) {
  if (!active.value || active.value.runId !== runId) return
  if (!activeRun.value) return
  await projectClarifySessionAfterLoad(activeRun.value, isCurrent)
}

function connectActiveRunWs(runId: string, opts?: { fromReconnect?: boolean }) {
  if (!runId) {
    closeActiveRunWs()
    return
  }
  if (activeRunWs && activeRunWsRunId === runId && !opts?.fromReconnect) return
  // Soft close for reconnect: keep buffers; only mark intentional when switching away.
  if (opts?.fromReconnect) {
    activeRunHeartbeat.stop()
    const previous = activeRunWs
    activeRunWs = undefined
    previous?.close()
    activeRunWsRunId = ''
  } else {
    closeActiveRunWs()
  }
  activeRunWsRunId = runId
  activeRunWsReconnect.markConnecting()
  let socket: WebSocket
  let dialogueRevision = 0
  let receivedSnapshot = false
  try {
    socket = new WebSocket(api.runEventsWsUrl(runId))
    activeRunWs = socket
  } catch {
    activeRunWs = undefined
    activeRunWsRunId = ''
    activeRunWsReconnect.onClose()
    return
  }
  activeRunHeartbeat.start()
  socket.onopen = () => {
    if (activeRunWs !== socket) return
    activeRunHeartbeat.touch()
    activeRunWsReconnect.markOpened()
  }
  socket.onmessage = (ev) => {
    if (activeRunWs !== socket) return
    activeRunHeartbeat.touch()
    let m: ReviewSessionFrame & {
      type?: string
      event?: string
      nodeId?: string
      events?: any[]
      busy?: boolean
      waiting?: number
      sandboxBusy?: boolean
      sandboxDesynced?: boolean
      status?: string
      iteration?: number
      entry?: { nodeId?: string; event?: string; iteration?: number; detail?: string }
      run?: { id?: string; reviewSessionGeneration?: number; reactSessions?: Run['reactSessions'] }
      previewArtifact?: string
      name?: string
    }
    try {
      m = JSON.parse(String(ev.data))
    } catch {
      return
    }
    if (m.type === 'dialogue') {
      ingestDialogue(m)
      return
    }
    if (m.type === 'heartbeat') {
      // Transport heartbeats continue even when a review terminal was lost.
      // Read the actual queue at their existing cadence; local busy cannot
      // veto the read that would release it. Genuine active turns stay active.
      if (active.value?.type === 'clarify' && (activeStarting.value || isClarifySoftRefreshBlocked())) {
        const nodeId = activeDialogueNodeId() || ''
        const recoverContent = !heartbeatRecoveryCheckpoint || !reviewState.hasAcpProgress(heartbeatRecoveryCheckpoint, nodeId)
        heartbeatRecoveryCheckpoint = reviewState.capture()
        void realignClarifyToRefresh(false, recoverContent)
      }
      return
    }
    if (m.type === 'page_control_state') {
      const st = (m as { state?: unknown }).state
      if (m.nodeId) {
        pageControlByNode.value = {
          ...pageControlByNode.value,
          [m.nodeId]: st === 'online' || st === 'paused' ? st : 'offline',
        }
      }
      return
    }
    // WS connect snapshot may carry reactSessions before BroadcastReviewSessions.
    if (m.type === 'snapshot') {
      if (!receivedSnapshot) {
        receivedSnapshot = true
        const sessions = m.run?.reactSessions ?? {}
        const versioned = reviewState.hasVersion || typeof m.run?.reviewSessionGeneration === 'number' || Object.values(sessions).some(snap => typeof snap?.sessionGeneration === 'number')
        reviewState.reset()
        heartbeatRecoveryCheckpoint = null
        if (versioned) {
          pendingReviewFrames = []
          applyOrBufferReviewFrame({ event: 'session_reset', nodeId: activeDialogueNodeId() || '' })
        }
      }
      // The full run DTO omits reactSessions after its last FIFO is removed.
      // Only a matching full snapshot makes that omission authoritative idle.
      const sessions = m.run?.reactSessions ?? (m.run?.id === runId ? {} : undefined)
      const dialogueActive =
        active.value &&
        (active.value.type === 'clarify' || active.value.type === 'gate') &&
        active.value.runId === runId
      if (sessions && dialogueActive) {
        const revision = ++dialogueRevision
        const activeKey = inboxTripleKey(active.value!)
        const sameDialogue = () =>
          activeRunWs === socket &&
          !!active.value && inboxTripleKey(active.value) === activeKey
        const isCurrent = () => sameDialogue() && dialogueRevision === revision
        if (activeRun.value) {
          activeRun.value = { ...activeRun.value, reactSessions: sessions, reviewSessionGeneration: m.run?.reviewSessionGeneration }
          // Same depth as hard refresh: queue_state → seed → flush → live (g4.2).
          void reseedAfterWsReconnect(runId, isCurrent).then(() => {
            // A turn may have finished while disconnected. Reload its saved
            // questions once the authoritative snapshot has released busy.
            if (!sameDialogue() || !opts?.fromReconnect) return
            const nodeId = activeDialogueNodeId()
            const snap = nodeId ? sessions[nodeId] : undefined
            if (
              nodeId && !snap?.busy && !snap?.waiting && !snap?.activeItem &&
              !clarifyLiveBusy.value && !isClarifySoftRefreshBlocked()
            ) {
              void softRefreshActiveRun()
            }
          })
        } else {
          pendingSnapshotSessions = sessions
        }
      }
      return
    }
    if (m.type === 'review') {
      if (!reviewState.acceptServer(m)) return
      const currentDialogue = m.nodeId === activeDialogueNodeId()
      if (currentDialogue) {
        dialogueRevision++
      }
      // Sidebar「最近」has no channel of its own: relay turn boundaries.
      if (
        m.event === 'turn_begin' ||
        m.event === 'turn_done' ||
        m.event === 'error' ||
        m.event === 'queue_state'
      ) notifyRecentActivity()
      if (currentDialogue && m.event === 'turn_begin') clarifyLiveBusy.value = true
      if (currentDialogue && m.event === 'queue_state' && typeof m.busy === 'boolean') {
        clarifyLiveBusy.value = !!m.busy
        if (!m.busy) busySeedRetry.stop()
      }
      const cardNodeId = typeof m.nodeId === 'string' ? m.nodeId : ''
      if (cardNodeId) {
        if (m.event === 'turn_begin') {
          patchVisibleCardBusy(runId, cardNodeId, true)
        } else if (m.event === 'queue_state') {
          const waiting = typeof m.waiting === 'number' ? m.waiting : 0
          const sessionBusy = (typeof m.busy === 'boolean' ? m.busy : false) || waiting > 0
          patchVisibleCardBusy(runId, cardNodeId, sessionBusy)
        } else if (m.event === 'turn_done' || m.event === 'error') {
          const waiting = typeof m.waiting === 'number' ? m.waiting : 0
          patchVisibleCardBusy(runId, cardNodeId, waiting > 0)
        }
      }
      applyOrBufferReviewFrame(m as Record<string, unknown>)
      if (currentDialogue && activeStarting.value &&
        (m.event === 'turn_begin' || (m.event === 'queue_state' && m.busy && m.activeItem))) {
        void realignClarifyToRefresh(false, true)
      }
      if (currentDialogue && m.event === 'queue_state') {
        if (queueStateIsOrphan(m)) {
          const nid = typeof m.nodeId === 'string' && m.nodeId ? m.nodeId : activeDialogueNodeId(activeRun.value) || ''
          if (nid) {
            orphanLiveNodeId = nid
            flushPendingAcpFrames()
            void pullOrphanLive(runId, nid)
          }
        } else {
          orphanLiveNodeId = ''
          stopOrphanLiveSeed()
        }
      }
      if (currentDialogue && (m.event === 'turn_done' || m.event === 'error')) {
        clarifyLiveBusy.value = false
        busySeedRetry.stop()
        if (active.value && active.value.runId === runId) void softRefreshActiveRun(true)
      }
      // Platform idle while the bridge is still busy: the terminal frame may
      // never arrive. Re-read the same snapshot a refresh would, without
      // remounting the composer.
      if (active.value?.type === 'clarify' && active.value.runId === runId && queueStateIsOrphan(m)) {
        void realignClarifyToRefresh()
      }
      return
    }
    if (m.type === 'acp') {
      reviewState.noteAcp(m.nodeId || '', Array.isArray(m.events) ? m.events as AcpEvent[] : [])
      // ACP/sandbox busy is observational only — must not overwrite platform
      // session busy (clarifyLiveBusy) or stop busySeedRetry (g1.2).
      // Tear-down / stop retry only from review queue_state idle / turn_done / error.
      applyOrBufferAcpFrame(m as { nodeId?: string; events?: AcpEvent[]; busy?: boolean })
      if (activeStarting.value && m.nodeId === activeDialogueNodeId()) void realignClarifyToRefresh(false, true)
      return
    }
    if (
      m.type === 'node_enter' &&
      active.value?.type === 'clarify' &&
      active.value.runId === runId &&
      activeRun.value &&
      m.nodeId
    ) {
      patchActiveRunNodeStatus(m.nodeId, 'running', m.iteration)
    }
    if (m.type === 'node_takeover' || m.type === 'status') notifyRecentActivity()
    if (m.type === 'artifact_edit' || m.type === 'react' || m.type === 'trace' || m.type === 'status') {
      // Ignore events for runs that no longer match the current pending active item.
      if (!active.value || active.value.runId !== runId) return
      if (isProcessedTriple(active.value) || !isActiveStillInList()) return
      if (m.type === 'artifact_edit') notifyRunArtifactEdit()
      // Starting card: a run transition is exactly the signal we wait for — the
      // approve node has parked, so the list row loses `state: starting` and the
      // rebind reloads the context with the real transcript.
      if (activeStarting.value && (m.type === 'status' || m.type === 'react')) {
        void loadList()
        return
      }
      // status/trace are noisy and must not softRefresh. Patch the open
      // snapshot in place so the workflow list moves without clearing dialogue.
      if (active.value?.type === 'clarify' && activeRun.value) {
        if (m.type === 'status' && m.status) {
          activeRun.value = { ...activeRun.value, status: m.status as Run['status'] }
        } else if (m.type === 'trace' && m.entry?.nodeId) {
          const st = nodeStatusFromTrace(m.entry)
          if (st) patchActiveRunNodeStatus(m.entry.nodeId, st, m.entry.iteration)
        }
      }
      // status/trace are noisy (run transitions / appendTrace) and must not softRefresh
      // while reviewing — peek/list cover "left pending" awareness instead.
      if (m.type === 'status' || m.type === 'trace') return
      // Visual gates: only artifact_edit here; review turn_done/error already refresh above.
      // Ignore react (e.g. turn_begin) — no product change yet.
      if (active.value.type === 'gate') {
        if (m.type === 'artifact_edit') void softRefreshActiveRun()
        return
      }
      // Clarify: react/artifact_edit mid-turn projected via review/acp — skip
      // softRefresh while busy so we do not wipe busy/queue/stream (g3.2 / review v4).
      // Soft-refresh semantics unchanged (g2.3).
      if (active.value.type === 'clarify') {
        if (m.type === 'artifact_edit') {
          // Cards only. A full context reload would replace the open transcript.
          void patchActiveRunArtifacts(m)
        } else if (m.type === 'react') {
          // Gate on live WS busy / sessionBusy — not stale reactSessions snapshot.
          if (!isClarifySoftRefreshBlocked()) void softRefreshActiveRun()
        }
      }
    }
  }
  socket.onclose = () => {
    // Ignore stale close from a superseded socket (reconnect race).
    if (activeRunWs !== socket) return
    activeRunHeartbeat.stop()
    activeRunWs = undefined
    activeRunWsRunId = ''
    activeRunWsReconnect.onClose()
  }
}

const ORPHAN_REALIGN_MS = 2000
const ORPHAN_LIVE_MS = 1500
let orphanRealignTimer: ReturnType<typeof setTimeout> | null = null
let orphanLiveTimer: ReturnType<typeof setTimeout> | null = null

function stopOrphanRealign() {
  if (orphanRealignTimer) clearTimeout(orphanRealignTimer)
  orphanRealignTimer = null
}

function stopOrphanLiveSeed() {
  if (orphanLiveTimer) clearTimeout(orphanLiveTimer)
  orphanLiveTimer = null
}

function scheduleOrphanLiveSeed(runId: string, nodeId: string) {
  if (orphanLiveTimer || orphanLiveNodeId !== nodeId) return
  orphanLiveTimer = setTimeout(() => {
    orphanLiveTimer = null
    void pullOrphanLive(runId, nodeId)
  }, ORPHAN_LIVE_MS)
}

/** Live sandbox log for a turn the platform is no longer tracking. */
async function pullOrphanLive(runId: string, nodeId: string) {
  if (orphanLiveNodeId !== nodeId) return
  if (active.value?.type !== 'clarify' || active.value.runId !== runId) return
  try {
    await seedClarifyAcpFromNodeEventsOnce(runId, nodeId)
  } catch {
    /* the next tick tries again */
  }
  if (orphanLiveNodeId !== nodeId) return
  if (active.value?.type !== 'clarify' || active.value.runId !== runId) return
  scheduleOrphanLiveSeed(runId, nodeId)
}

function scheduleOrphanRealign() {
  if (orphanRealignTimer) return
  orphanRealignTimer = setTimeout(() => {
    orphanRealignTimer = null
    void realignClarifyToRefresh()
  }, ORPHAN_REALIGN_MS)
}

function queueStateIsOrphan(m: {
  event?: string
  busy?: boolean
  waiting?: number
  activeItem?: unknown
  sandboxBusy?: boolean
  sandboxDesynced?: boolean
}): boolean {
  if (m.event !== 'queue_state') return false
  if (m.busy === true) return false
  if ((typeof m.waiting === 'number' ? m.waiting : 0) !== 0) return false
  if (m.activeItem) return false
  return m.sandboxBusy === true || m.sandboxDesynced === true
}

function nodeStatusFromTrace(entry: { event?: string; detail?: string }): Run['nodeRuns'][string]['status'] | null {
  const detail = entry.detail || ''
  const failed = /fail|失败/i.test(detail)
  switch (entry.event) {
    case 'enter':
    case 'reattach':
      return 'running'
    case 'pause':
      return 'waiting_human'
    case 'resume':
      return failed ? 'failed' : 'running'
    case 'exit':
      return failed ? 'failed' : 'completed'
    default:
      return null
  }
}

function patchActiveRunNodeStatus(
  nodeId: string,
  status: Run['nodeRuns'][string]['status'],
  iteration?: number,
) {
  const run = activeRun.value
  if (!run || !nodeId) return
  const prev = run.nodeRuns?.[nodeId]
  const row = {
    ...(prev || { nodeId, status }),
    nodeId,
    status,
    ...(typeof iteration === 'number' && iteration > 0 ? { iteration } : {}),
  }
  const executions = { ...(run.nodeExecutions || {}) }
  const hist = [...(executions[nodeId] || [])]
  if (hist.length) hist[hist.length - 1] = { ...hist[hist.length - 1]!, ...row }
  else hist.push(row)
  executions[nodeId] = hist
  activeRun.value = {
    ...run,
    nodeRuns: { ...(run.nodeRuns || {}), [nodeId]: row },
    nodeExecutions: executions,
  }
}

/**
 * Lost terminal frame: load the same inbox snapshot a refresh would, project
 * the bridge fact onto the banner, and leave the composer draft alone.
 * While the bridge stays busy, try again so a later idle clears the banner
 * even if no further queue_state arrives.
 */
function realignClarifyToRefresh(retryOrphan = true, recoverContent = false) {
  return reviewState.requestRecovery((checkpoint, signal) =>
    recoverClarifySession(checkpoint, signal, retryOrphan, recoverContent))
}

async function recoverClarifySession(
  checkpoint: ReviewRecoveryCheckpoint,
  recoverySignal: AbortSignal,
  retryOrphan: boolean,
  recoverContent: boolean,
) {
  if (recoverySignal.aborted) return
  if (active.value?.type !== 'clarify') return
  if (!shouldFetchActiveInboxContext()) return
  const target = active.value
  const triple = inboxTripleKey(target)
  const socket = activeRunWs
  const signal = acquireInboxContextSignal(triple)
  if (!signal) {
    if (retryOrphan) scheduleOrphanRealign()
    return
  }
  const abortRecovery = () => {
    const controller = inboxContextAborts.get(triple)
    if (controller?.signal === signal) abortInboxContext(triple)
  }
  recoverySignal.addEventListener('abort', abortRecovery, { once: true })
  let readyCheckpoint: ReviewRecoveryCheckpoint | undefined
  try {
    const ctx = await api.inboxContext(target.runId, target.nodeId, target.iteration ?? 1, { signal })
    ingestContextDialogues(ctx, target.runId)
    if (!active.value || inboxTripleKey(active.value) !== triple) return
    if (signal.aborted) return
    if (activeRunWs === socket && reviewState.isStateCurrent(checkpoint, target.nodeId) &&
      isStartingInboxItem(target) && !isInboxLeftPending(ctx) && ctx.type === 'clarify' &&
      !ctx.clarify.starting && (ctx.clarify.iteration ?? target.iteration ?? 1) === (target.iteration ?? 1)) {
      readyCheckpoint = reviewState.capture()
    }
    if (activeRunWs !== socket || !reviewState.isCurrent(checkpoint, target.nodeId)) {
      if (retryOrphan) scheduleOrphanRealign()
      return
    }
    if (isProcessedTriple(target)) return
    if (isInboxLeftPending(ctx)) {
      if (isIncomingContextPending(target)) return
      handleLeftInboxContext(target)
      return
    }
    const next = adaptInboxContextToRun(ctx, target.runId)
    const nodeId = activeDialogueNodeId(next)
    const snap = nodeId ? next.reactSessions?.[nodeId] : undefined
    const frame = reviewState.reconcileSnapshot(next, checkpoint, nodeId ? [nodeId] : []).find(frame => frame.nodeId === nodeId)
    if (!frame) return
    if (readyCheckpoint) readyCheckpoint = reviewState.capture()
    activeRun.value = next
    activeRunLoadError.value = false
    deliverReviewQueueFrame(frame)
    if (recoverContent && frame.busy && frame.activeItem && nodeId) {
      const contentCheckpoint = reviewState.capture()
      await readClarifyAcpFromNodeEventsOnce(target.runId, nodeId, () =>
        activeRunWs === socket && !!active.value && inboxTripleKey(active.value) === triple &&
        reviewState.isCurrent(contentCheckpoint, nodeId), recoverySignal)
    }
    if (!snap?.busy && !snap?.waiting && !snap?.activeItem && (snap?.sandboxBusy || snap?.sandboxDesynced)) scheduleOrphanRealign()
    else stopOrphanRealign()
  } catch (e) {
    if (isAbortError(e) || signal.aborted) return
    if (retryOrphan) scheduleOrphanRealign()
  } finally {
    recoverySignal.removeEventListener('abort', abortRecovery)
    releaseInboxContextSignal(triple, signal)
    // Readiness is monotonic within a visit. ACP progressing during the GET
    // invalidates its older content, but must not strand the launch screen.
    if (readyCheckpoint && !signal.aborted && activeRunWs === socket &&
      reviewState.isStateCurrent(readyCheckpoint, target.nodeId) &&
      active.value && inboxTripleKey(active.value) === triple && isStartingInboxItem(active.value)) {
      const ready = { ...target, state: reviewState.session(target.nodeId)?.busy ? 'replying' : undefined } as InboxItem
      listItems.value = listItems.value.map(row => inboxTripleKey(row) === triple ? ready : row)
      if (incomingGhost.value && inboxTripleKey(incomingGhost.value) === triple) incomingGhost.value = ready
      active.value = ready
    }
  }
}

let artifactReadGeneration = 0
async function patchActiveRunArtifacts(frame?: { previewArtifact?: string; name?: string }) {
  const target = active.value
  if (!target || !activeRun.value || activeRun.value.id !== target.runId) return
  const isCurrentVisit = captureActiveVisit()
  const generation = ++artifactReadGeneration
  const name = String(frame?.previewArtifact || '').trim()
  if (name) {
    activeRun.value = applyPreviewArtifactName(activeRun.value, target.nodeId, name)
  }
  try {
    const arts = await api.runArtifacts(target.runId)
    if (!isCurrentVisit() || generation !== artifactReadGeneration || activeRun.value?.id !== target.runId) return
    activeRun.value = { ...activeRun.value, artifacts: Array.isArray(arts) ? arts : activeRun.value.artifacts }
  } catch {
    /* keep last known artifacts */
  }
}

function softRefreshActiveRun(afterTurnEnd = false) {
  return reviewState.requestRecovery((checkpoint, signal) =>
    refreshActiveContext(checkpoint, signal, afterTurnEnd))
}

async function refreshActiveContext(
  checkpoint: ReviewRecoveryCheckpoint,
  recoverySignal: AbortSignal,
  afterTurnEnd: boolean,
) {
  if (recoverySignal.aborted) return
  if (!shouldFetchActiveInboxContext()) return
  const target = active.value!
  const triple = inboxTripleKey(target)
  const socket = activeRunWs
  const signal = acquireInboxContextSignal(triple)
  if (!signal) {
    if (afterTurnEnd && inboxContextInFlight === triple) inboxContextTrailingRefreshes.add(triple)
    return
  }
  const abortRecovery = () => {
    const controller = inboxContextAborts.get(triple)
    if (controller?.signal === signal) abortInboxContext(triple)
  }
  recoverySignal.addEventListener('abort', abortRecovery, { once: true })
  const isCurrent = () => !signal.aborted && activeRunWs === socket &&
    !!active.value && inboxTripleKey(active.value) === triple &&
    reviewState.isCurrent(checkpoint, target.nodeId)
  try {
    const ctx = await api.inboxContext(target.runId, target.nodeId, target.iteration ?? 1, { signal })
    ingestContextDialogues(ctx, target.runId)
    if (!isCurrent()) return
    if (isProcessedTriple(target)) return
    if (isInboxLeftPending(ctx)) {
      if (isIncomingContextPending(target)) return
      handleLeftInboxContext(target)
      return
    }
    const adapted = await ensureActiveGraphEdges(adaptInboxContextToRun(ctx, target.runId))
    if (!isCurrent()) return
    const nodeId = activeDialogueNodeId(adapted)
    let frame: ReviewSessionFrame | undefined
    if (adapted.reactSessions || typeof adapted.reviewSessionGeneration === 'number') {
      frame = reviewState.reconcileSnapshot(adapted, checkpoint, nodeId ? [nodeId] : [])
        .find(frame => frame.nodeId === nodeId)
      if (nodeId && !frame) return
    } else {
      // A legacy context without queue authority can update saved history,
      // but cannot erase the current server session already seen live.
      adapted.reactSessions = activeRun.value?.reactSessions
      adapted.reviewSessionGeneration = activeRun.value?.reviewSessionGeneration
    }
    activeRun.value = adapted
    activeRunLoadError.value = false
    if (frame) deliverReviewQueueFrame(frame)
    // Soft refresh keeps the surface mounted. Same-item queue reconciliation
    // preserves its live content; only newer server authority can settle it.
  } catch (e) {
    if (isAbortError(e) || signal.aborted) return
    if (isIncomingContextPending(target)) return
    if (
      isProcessedTriple(target) ||
      !listItems.value.some((it) => itemKey(it) === itemKey(target))
    ) {
      handleLeftInboxContext(target)
      return
    }
    /* keep last known run on transient errors */
  } finally {
    recoverySignal.removeEventListener('abort', abortRecovery)
    releaseInboxContextSignal(triple, signal)
  }
}

async function loadActiveRun(hard = true) {
  if (!active.value) {
    activeRun.value = null
    activeRunLoadError.value = false
    closeActiveRunWs()
    return
  }
  if (!shouldFetchActiveInboxContext()) {
    if (hard) {
      activeRun.value = null
      activeRunLoadError.value = false
      activeRunLoading.value = false
    }
    return
  }
  const target = active.value
  const triple = inboxTripleKey(target)
  const signal = acquireInboxContextSignal(triple)
  if (!signal) return
  if (hard) {
    activeRun.value = null
    activeRunLoadError.value = false
    activeRunLoading.value = true
    // Chat unmounts — clear stale buffer for this load cycle (WS may refill).
    pendingReviewFrames = []
    pendingAcpFrames.clear()
  }
  let projectAfterLoad = false
  try {
    // Initial loading owns a context lease before any surface exists. Bound
    // this read too, so it cannot hold the lease against all later recovery.
    const read = await runWithTimeout(async readSignal => {
      const ctx = await api.inboxContext(target.runId, target.nodeId, target.iteration ?? 1, { signal: readSignal })
      ingestContextDialogues(ctx, target.runId)
      const adapted = readSignal.aborted || isInboxLeftPending(ctx) ? null
        : await ensureActiveGraphEdges(adaptInboxContextToRun(ctx, target.runId))
      return { ctx, adapted }
    }, { signal, timeoutMs: REVIEW_RECOVERY_TIMEOUT_MS })
    if (read.status === 'cancelled') return
    if (read.status !== 'success') throw read.error
    const { ctx, adapted } = read.value!
    if (!active.value || inboxTripleKey(active.value) !== triple) return
    if (signal.aborted) return
    if (isProcessedTriple(target)) return
    if (isInboxLeftPending(ctx)) {
      if (isFollowingActive()) return
      if (isIncomingContextPending(target)) {
        if (hard) activeRunLoadError.value = false
        return
      }
      handleLeftInboxContext(target)
      return
    }
    if (!adapted) return
    activeRun.value = adapted
    activeRunLoadError.value = false
    // Context is in. Clear even when a same-key ghost was pending: the finally
    // below used to leave the flag set, so a gate past page 1 never mounted.
    if (hard) activeRunLoading.value = false
    // Must project AFTER loading flag clears so ReviewComposer / GateApproval can mount.
    projectAfterLoad = hard && (target.type === 'clarify' || target.type === 'gate')
  } catch (e) {
    if (isAbortError(e) || signal.aborted) return
    // A reload reopened this confirmed chat meanwhile (resumeHandedOff): it is followed, not gone.
    if (isFollowingActive()) return
    if (isIncomingContextPending(target)) {
      if (hard) activeRunLoadError.value = false
      return
    }
    if (
      isProcessedTriple(target) ||
      !listItems.value.some((it) => itemKey(it) === itemKey(target))
    ) {
      handleLeftInboxContext(target)
      return
    }
    if (hard) {
      activeRun.value = null
      activeRunLoadError.value = true
    }
  } finally {
    const ownsFlight = inboxContextAborts.get(triple)?.signal === signal
    releaseInboxContextSignal(triple, signal)
    if (hard && ownsFlight && active.value && inboxTripleKey(active.value) === triple &&
      !isIncomingContextPending(target)) activeRunLoading.value = false
  }
  if (projectAfterLoad && activeRun.value) {
    await projectClarifySessionAfterLoad(activeRun.value)
  }
}
watch(
  active,
  (item) => {
    if (!item) {
      closeActiveRunWs()
      activeRun.value = null
      activeRunLoadError.value = false
      return
    }
    if (isProcessedTriple(item) || !isActiveStillInList(item)) {
      closeActiveRunWs()
      return
    }
    void loadActiveRun(true)
    connectActiveRunWs(item.runId)
  },
  { immediate: true },
)

function retryActiveRun() {
  return loadActiveRun(true)
}

const activeClarify = computed(() => {
  if (active.value?.type !== 'clarify' || !activeRun.value) return null
  return pickInboxClarifySession(activeRun.value, active.value.nodeId)
})

const inboxStageNode = computed(() => {
  const id = active.value?.type === 'clarify' ? active.value.nodeId : ''
  return findGraphNode(activeRun.value?.nodes, id) || null
})

/** Inbox review of an Agent with set_preview: the stage exposes the remote app. */
const inboxAppPreviewActive = computed(
  () => active.value?.type === 'clarify' && isAppPreviewRemoteNode(inboxStageNode.value),
)

const inboxRemoteKind = computed(() =>
  inboxStageRemoteKind({
    appPreview: inboxAppPreviewActive.value,
    run: activeRun.value,
    nodeId: active.value?.type === 'clarify' ? active.value.nodeId : '',
    // Clarify upgrades inside ReactArtifactStage after silent probe; parents stay off.
    hasRegisteredPreview: false,
  }),
)

/** Run whose chat shell is on screen. */
const shellRunId = ref('')
/**
 * Keep the chat (and home-chat attachments) up while the product stage is still
 * loading. A same-run node switch (hand-off) keeps the shell while the next
 * node's context loads: one run, one chat.
 */
const showClarifyReviewShell = computed(
  () =>
    !!active.value &&
    active.value.type === 'clarify' &&
    !startFailedActive.value &&
    (activeStarting.value ||
      !!activeClarify.value ||
      !!activeHomeSeed.value ||
      (activeRunLoading.value && shellRunId.value === active.value.runId)),
)
watch(showClarifyReviewShell, (shown) => {
  shellRunId.value = shown && active.value ? active.value.runId : ''
}, { immediate: true })
const clarifyComposerNodeId = computed(
  () => activeClarify.value?.nodeId || active.value?.nodeId || '',
)
const clarifyComposerIteration = computed(() => activeClarify.value?.iteration ?? 1)
const clarifyComposerTurns = computed(() => activeClarify.value?.turns ?? [])
/**
 * The node's task, shown as the conversation's opening User Message: the chain
 * segment's task when the node was reached by a hand-off (below the separator),
 * else the taken-over node's task (#849 clarifyByNode[nodeId].task).
 */
const activeNodeTask = computed(() => chainNodeTask.value || takeoverTask(activeClarify.value))
const clarifyComposerDone = computed(() => activeClarify.value?.done ?? false)

const inboxClarifyStageKind = computed(() => (activeRunLoadError.value ? 'loadFailed' : 'pending'))

// Mirror RunDetailView.reviewActive: post-run product review on a non-react
// producer (backend only seeds clarify sessions for ReviewCapable nodes).
// Inbox API type stays "clarify"; mode is decided from the loaded graph.
const inboxReviewState = computed(() =>
  resolveInboxReviewState(active.value, activeRun.value, activeClarify.value),
)
const reviewActive = computed(() => inboxReviewState.value.reviewActive)
const composerMode = computed<'clarify' | 'review'>(() => inboxComposerMode(reviewActive.value))

watch(
  () => ({
    missing: inboxReviewState.value.nodeMissing,
    nodeId: active.value?.type === 'clarify' ? active.value.nodeId : '',
  }),
  ({ missing, nodeId }) => {
    if (missing && nodeId) {
      console.warn(
        '[GatesInboxView] inbox review node missing from run graph; falling back to clarify mode',
        nodeId,
      )
    }
  },
)

// The inbox-list gate item lacks the engine-derived ReAct fields (built in the
// store layer). Overlay them from the freshly loaded run's gate DTO so the
// ReAct reject entry appears when the upstream session is alive.
const activeGate = computed<Gate | null>(() => {
  if (active.value?.type !== 'gate') return null
  const g = active.value
  const rg = activeRun.value?.gate
  if (rg && rg.nodeId === g.nodeId) {
    return { ...g, reactUpstreamNodeId: rg.reactUpstreamNodeId, reactSessionAlive: rg.reactSessionAlive }
  }
  return g
})

const clarifyInputActive = computed(() =>
  activeRun.value ? ['queued', 'running', 'waiting_human'].includes(activeRun.value.status) : false,
)
/**
 * Following the run after a hand-off: the same composer stays mounted (never the
 * closed/done footer) but is locked until the run's next node waits for the user.
 * Confirm pending (校验中) is not a follow lock — input stays available.
 */
const clarifyComposerLocked = computed(() => isFollowingActive())

/** Manual refresh with a held pending change: the user asked, so apply now. */
async function applyListUpdate() {
  if (processingLock.value) return
  const prevKey = active.value ? itemKey(active.value) : null
  applyPending()
  await loadList()
  syncActiveAfterApply(listItems.value, prevKey)
  await loadActiveRun(false)
  await softRefreshActiveRun()
}

async function onManualRefresh() {
  if (processingLock.value) return
  manualRefreshing.value = true
  try {
    if (hasPendingUpdate.value) {
      await applyListUpdate()
    } else {
      const prevKey = active.value ? itemKey(active.value) : null
      await refresh({ source: 'manual', mode: 'force' })
      await loadList({ showLoading: listItems.value.length === 0 })
      syncActiveAfterApply(listItems.value, prevKey)
      await loadActiveRun(isEditing.value ? false : true)
      checkProcessedWhileEditing()
    }
  } finally {
    manualRefreshing.value = false
  }
}

// ReAct reject-and-revise edited the upstream producer in place; the gate stays
// pending, so just reload the active run to show the refreshed gate body.
async function onReactRevised() {
  await loadActiveRun(false)
}
async function onResolve(action: string, form: Record<string, any> = {}) {
  const g = active.value
  if (!g || g.type !== 'gate') return
  // Per-item only: another in-flight confirm must not block this neighbor.
  if (isProcessedTriple(g) || isProcessingIntent(g)) return
  // Intent moment: short-circuit before await resume so WS cannot race.
  beginProcessingIntent(g)
  showProcessedBanner.value = false
  const submittedKey = itemKey(g)
  const submittedItem = g
  const prevList = listItems.value.slice()
  const positive = action === 'pass' || action === 'approve'
  const handoffNodes = activeRun.value?.nodes
  const handoffEdges = activeRun.value?.edges
  // Leave pending at confirm click — do not wait for resume/network (plan g1.2).
  // Positive path: start page-level overlay on click (g1.1); do not await before leave.
  // Page-level host survives selectActiveAfterRemove / shell remount (g1.2).
  if (positive) void playInboxConfirmFlowCeremony()
  removeListItemLocally(submittedKey)
  selectActiveAfterRemove(prevList, submittedKey)
  if (!active.value) {
    activeRun.value = null
    activeRunLoadError.value = false
  }
  if (positive) {
    armSidebarHandoff(g.runId, g.nodeId, Date.now(), sidebarAutoTarget(handoffNodes, handoffEdges, g.nodeId, action))
    openFollowWs(g.runId, handoffNodes)
  }
  try {
    await api.resumeGate(g.runId, g.nodeId, action, form)
  } catch {
    if (positive) {
      clearSidebarHandoff(g.runId)
      if (!followTail.value) closeFollowWs()
    }
    confirmFlowDeskHold.value = false
    inboxConfirmFlow.reset()
    restoreListItemLocally(submittedItem, prevList)
    rollbackProcessingIntent(submittedItem)
    // Reclaim for retry unless a newer neighbor confirm already owns the selection.
    if (!isProcessingIntent(active.value)) {
      active.value = submittedItem
    }
    return
  }
  try {
    await refresh({ source: 'submit', mode: 'force' })
    await loadList()
  } catch {
    /* refresh/loadList already soft-fail in most paths; still converge below */
  } finally {
    // Re-drop lagging list rows; unlock even when refresh throws.
    removeListItemLocally(submittedKey)
    if (active.value && itemKey(active.value) === submittedKey) {
      selectActiveAfterRemove(listItems.value, submittedKey)
    }
    if (!active.value) {
      activeRun.value = null
      activeRunLoadError.value = false
    }
    endProcessingIntent(submittedItem)
  }
}

const sharePanelOpen = ref(false)
const shareTarget = ref<InboxItem | null>(null)

function patchShareStatus(it: InboxItem, status: GateShareInboxStatus) {
  const next = { ...it, shareLink: status } as InboxItem
  listItems.value = listItems.value.map((row) => (itemKey(row) === itemKey(it) ? next : row))
  if (active.value && itemKey(active.value) === itemKey(it)) {
    active.value = next
  }
  shareTarget.value = next
}

function openSharePanel(it: InboxItem, alsoOpenDetail = false) {
  if (processingLock.value || !isShareableInboxItem(it)) return
  selectItem(it)
  shareTarget.value = it
  sharePanelOpen.value = true
  if (alsoOpenDetail) openDetail(it)
}

function queryText(value: unknown): string {
  if (Array.isArray(value)) return String(value[0] ?? '').trim()
  return String(value ?? '').trim()
}

function shareIntentPending(): boolean {
  return queryText(route.query[INBOX_SHARE_INTENT_QUERY]) === INBOX_SHARE_INTENT_VALUE
}

function activeMatchesShareQuery(it: InboxItem): boolean {
  const { run, node } = conversationRoute()
  return !!run && !!node && it.runId === run && it.nodeId === node
}

function shareIntentBlocked(it: InboxItem): boolean {
  if (isStartingInboxItem(it) || processingLock.value || !isShareableInboxItem(it)) return true
  return 'shareLink' in it && it.shareLink?.state === 'used'
}

function clearShareIntent() {
  if (!(INBOX_SHARE_INTENT_QUERY in route.query)) return
  const query: Record<string, string | string[]> = {}
  for (const [key, value] of Object.entries(route.query)) {
    if (key === INBOX_SHARE_INTENT_QUERY || value == null) continue
    query[key] = value as string | string[]
  }
  void router.replace({ path: route.path, query })
}

/**
 * Identity of the share query currently being consumed.
 * Cleared once `share` leaves the URL, so the next menu click can open again.
 * A refresh without the query does not reopen the panel.
 */
let settledShareKey = ''
function shareIntentKey(): string {
  if (!shareIntentPending()) return ''
  const { run, node } = conversationRoute()
  return `${run}\0${node}`
}

function consumeShareIntent() {
  const key = shareIntentKey()
  if (!key) {
    settledShareKey = ''
    return
  }
  if (settledShareKey === key || !active.value) return
  if (!activeMatchesShareQuery(active.value)) return
  // Deep-link placeholder uses the same run and node while it is still booting.
  // Settling here would drop the intent before the real row can open the panel.
  if (isIncomingContextPending(active.value)) return
  settledShareKey = key
  const it = active.value
  if (!shareIntentBlocked(it)) openSharePanel(it)
  clearShareIntent()
}

watch(processingLock, (locked) => {
  knownInboxProcessingLock.value = locked
}, { immediate: true })

watch(
  () => [
    active.value ? itemKey(active.value) : '',
    active.value && isStartingInboxItem(active.value) ? 'starting' : '',
    incomingGhost.value ? itemKey(incomingGhost.value) : '',
    conversationRoute().run,
    conversationRoute().node,
    route.query[INBOX_SHARE_INTENT_QUERY],
    processingLock.value,
  ],
  () => {
    consumeShareIntent()
  },
)

function selectItem(it: InboxItem) {
  if (isItemCardDisabled(it)) return
  showProcessedBanner.value = false
  clarifyConfirmError.value = null
  clarifyConfirmCanAbort.value = false
  active.value = it
}

function openDetail(it: InboxItem) {
  if (isItemCardDisabled(it)) return
  if (listEl.value) listScrollTop.value = listEl.value.scrollTop
  selectItem(it)
  mobileView.value = 'detail'
}

function backToList() {
  mobileView.value = 'list'
  nextTick(() => {
    if (listEl.value) listEl.value.scrollTop = listScrollTop.value
  })
}

async function onClarifySend(
  text: string,
  images: import('@/lib/shared/types').ClarifyImage[] = [],
  annotations: import('@/lib/shared/types').ReactAnnotation[] = [],
  force = false,
  retryLast = false,
  abortRunning = false,
  sentClientMessageId?: string,
) {
  const it = active.value
  if (!it || it.type !== 'clarify' || it.done) {
    if (force) reviewChatRef.value?.releaseConfirmPending?.()
    return
  }
  // Suppress duplicate force on the same triple only — neighbor confirm must proceed
  // while an earlier reactReply(force) is still in flight (plan g1.2 / f1).
  if (force && isProcessingIntent(it)) {
    // The in-flight confirm owns 校验中. Drop a parallel error so the button
    // is not left clickable on top of "react already done".
    clarifyConfirmError.value = null
    return
  }
  if (force && isProcessedTriple(it)) {
    reviewChatRef.value?.releaseConfirmPending?.()
    return
  }
  // Force-finish leaves pending — short-circuit at intent moment (symmetric with gate).
  // Ordinary turn replies stay pending and must not markProcessed.
  const submittedKey = itemKey(it)
  const submittedItem = it
  const isCurrentVisit = captureActiveVisit()
  const submittedChat = reviewChatRef.value
  const currentChat = () => isCurrentVisit() && reviewChatRef.value === submittedChat ? submittedChat : null
  const prevList = force ? listItems.value.slice() : null
  if (force) {
    beginProcessingIntent(it)
    // Click intent: the conversation stays and shows the inline hand-off row
    // (no full-desk overlay); the inbox leaves once the hand-off is done.
    handoffState.value = { key: submittedKey, phase: 'pending', at: new Date().toISOString(), ...nextHandoffTarget(it.nodeId) }
    // Move now only when the loaded graph has one automatic destination. No edges: wait for node_enter.
    armSidebarHandoff(
      it.runId,
      it.nodeId,
      Date.now(),
      sidebarAutoTarget(activeRun.value?.nodes, activeRun.value?.edges, it.nodeId),
    )
    // Socket first, then the POST: the next node's node_enter can arrive right after it.
    openFollowWs(it.runId, activeRun.value?.nodes)
  }
  const submittedHandoff = force ? handoffState.value : null
  const hasReplacementHandoff = () => handoffState.value?.key === submittedKey && handoffState.value !== submittedHandoff
  // Always merge a staged pick when present (Approve may gain app preview mid-session).
  const mergedAnnotations =
    !force && !retryLast ? mergeStagedAppPreviewPick(annotations) : annotations
  clarifyConfirmError.value = null
  if (!abortRunning) clarifyConfirmCanAbort.value = false
  let ok = true
  const clientMessageId = beginClarifySend(
    it.runId,
    it.nodeId,
    { text, images, annotations: mergedAnnotations },
    { force, retryLast, clientMessageId: sentClientMessageId },
  )
  try {
    if (retryLast) {
      await api.reactReply(
        it.runId,
        it.nodeId,
        text,
        images,
        force,
        mergedAnnotations,
        true,
      )
    } else {
      await api.reactReply(
        it.runId,
        it.nodeId,
        text,
        images,
        force,
        mergedAnnotations,
        false,
        abortRunning,
        ...(clientMessageId ? [clientMessageId] : []),
      )
    }
    if (!isCurrentVisit()) {
      if (force && !hasReplacementHandoff()) endProcessingIntent(submittedItem)
      return
    }
    if (!force && !retryLast) lastStagedAppPreviewPick.value = null
  } catch (e: any) {
    ok = false
    if (!isCurrentVisit()) {
      if (force) {
        // Release only the originating intent. Reopening its socket or
        // reclaiming selection would interfere with the newer visit.
        const handoff = handoffState.value
        if (!hasReplacementHandoff()) {
          unmarkProcessed(submittedItem)
          endProcessingIntent(submittedItem)
        }
        // A failed departed confirmation must not reappear as pending when
        // its card is opened again. Only this request owns this exact row.
        if (handoff === submittedHandoff) handoffState.value = null
      } else if (!handleClarifyVisitEnded(e, t, {
        chat: null, runId: it.runId, nodeId: it.nodeId,
        sent: { text, images, annotations: mergedAnnotations }, retryLast, clientMessageId,
      })) failClarifySend(it.runId, it.nodeId, clientMessageId)
      return
    }
    /* refresh below to reflect real state */
    console.warn('reactReply failed', e?.message || e)
    const msg = isSandboxBusyError(e)
      ? t('pages.clarify.sandboxBusy')
      : e?.message || t('pages.gatesInbox.reviewFinishFailed')
    if (force && isSandboxBusyError(e)) clarifyConfirmCanAbort.value = true
    if (force) {
      // The hand-off did not happen: drop the socket opened for it (unless already following).
      if (!followTail.value) {
        followEarlyAcp = new Map()
        closeFollowWs()
      }
      // Align with RunDetail: bottom status bar, not toast.
      // Release even when msg equals the previous confirmError — that watch
      // does not run, and the button would stay on 校验中.
      clarifyConfirmError.value = msg
      currentChat()?.releaseConfirmPending?.()
      if (handoffState.value?.key === submittedKey) handoffState.value = null
      clearSidebarHandoff(submittedItem.runId)
      restoreListItemLocally(submittedItem, prevList!)
      rollbackProcessingIntent(submittedItem)
      // Reclaim for retry unless a newer neighbor confirm already owns the selection.
      if (!isProcessingIntent(active.value)) {
        active.value = submittedItem
      }
      return
    }
    // Run/visit already ended: the reply was never sent. Hand it back to the
    // composer with a notice instead of an error reply (no confirm-error bar).
    const visitEnded = handleClarifyVisitEnded(e, t, {
      chat: currentChat(),
      runId: it.runId,
      nodeId: it.nodeId,
      sent: { text, images, annotations: mergedAnnotations },
      retryLast,
      clientMessageId,
    })
    if (visitEnded) {
      if (!visitEnded.shownInChat) toast.warn(visitEnded.notice)
      return
    }
    failClarifySend(it.runId, it.nodeId, clientMessageId)
    // Non-force enqueue failed: roll back optimistic queue + surface error.
    if (!retryLast) currentChat()?.discardLastQueued?.(clientMessageId)
    // A confirm already left the dialogue pending (conv.Done). This reply is
    // refused immediately; showing it under the composer unlocks 确认并流转
    // while the hand-off row is still 校验中, and the next click does not send.
    if (handoffState.value?.key === submittedKey && handoffState.value.phase === 'pending') return
    clarifyConfirmError.value = msg
  }
  const finished = force && ok
  // The visit socket was closed at the click, so turn_done never arrives.
  // Success means that turn is over: drop 思考中 / 生成中. Failure stays retryable.
  if (finished) currentChat()?.settleConfirmedTurn?.()

  // Force-finish: already left locally; sync counts and re-drop lagging ghosts.
  if (force) {
    let stillThere = true
    if (handoffState.value?.key === submittedKey) {
      handoffState.value = { ...handoffState.value, phase: 'done', at: new Date().toISOString() }
    }
    const holdMs = handoffHoldMs()
    const hold = holdMs > 0 ? new Promise<void>((resolve) => setTimeout(resolve, holdMs)) : null
    try {
      showProcessedBanner.value = false
      await refresh({ source: 'submit', mode: 'force' })
      if (!isCurrentVisit()) return
      await loadList()
      if (!isCurrentVisit()) return
      stillThere = listItems.value.some((k) => itemKey(k) === submittedKey)
    } catch {
      /* refresh/loadList already soft-fail in most paths; still unlock below */
    } finally {
      // Let the「已流转」row read before the conversation leaves.
      if (hold) await hold
      if (!isCurrentVisit()) {
        endProcessingIntent(submittedItem)
        return
      }
      const leaving = !!active.value && itemKey(active.value) === submittedKey
      removeListItemLocally(submittedKey)
      let stayed = false
      if (leaving && !stillThere) {
        // Same run first: its next waiting node continues below; else follow the live run.
        stayed = await continueInRun(submittedItem, submittedKey, isCurrentVisit)
        if (!stayed && isCurrentVisit()) stayed = await beginFollow(submittedItem, submittedKey, isCurrentVisit)
        if (!isCurrentVisit()) {
          endProcessingIntent(submittedItem)
          return
        }
      }
      // Not following (same-run chat continued below, or left): drop the early socket.
      if (!followTail.value) closeFollowWs()
      if (!stayed && handoffState.value?.key === submittedKey) handoffState.value = null
      if (leaving && !stayed) {
        if (!pickHandoffNext(submittedItem, submittedKey)) {
          selectActiveAfterRemove(prevList!, submittedKey)
          // The click-time neighbour may have left meanwhile; fall back to the live list.
          if (active.value && !listItems.value.some((k) => itemKey(k) === itemKey(active.value!))) {
            selectActiveAfterRemove(listItems.value, submittedKey)
          }
        }
      }
      if (!active.value) {
        activeRun.value = null
        activeRunLoadError.value = false
      }
      endProcessingIntent(submittedItem)
      if (finished) notifyRecentActivity()
      if (finished) {
        clarifyConfirmError.value = null
        // Success is shown inline; only the still-pending case needs a toast.
        if (stillThere) toast.success(t('pages.gatesInbox.reviewFinishedPending'))
      }
    }
    return
  }

  // Ordinary turn enqueue returns before the turn finishes — keep live
  // queue/stream bubbles (clarify + review shared session UX). No force refresh.
  // Optimistic: this page's send is sessionBusy; WS queue_state confirms/clears.
  if (ok) {
    patchVisibleCardBusy(it.runId, it.nodeId, true)
    notifyRecentActivity()
  }
}

// Review: confirm product & advance (same contract as RunDetailView.onClarifyFinish).
// Classic clarify hides finish via ReviewComposer, so this only fires in review mode.
function onClarifyFinish() {
  const prompt = reviewActive.value
    ? t('pages.clarify.confirmFlowPrompt')
    : t('pages.runDetail.clarifyFinishPrompt')
  onClarifySend(prompt, [], [], true)
}

function onClarifyFinishAbort() {
  const prompt = reviewActive.value
    ? t('pages.clarify.confirmFlowPrompt')
    : t('pages.runDetail.clarifyFinishPrompt')
  onClarifySend(prompt, [], [], true, false, true)
}

/** Cover-this-turn retry for empty/failure agent (plan g2.1). */
async function onClarifyRetryLast() {
  await onClarifySend('', [], [], false, true)
}

async function onClarifyCancel() {
  const it = active.value
  if (!it || it.type !== 'clarify' || it.done) return
  try {
    await api.reactCancel(it.runId, it.nodeId)
  } catch (e: any) {
    console.warn('reactCancel failed', e?.message || e)
  }
}

async function onClarifyQueueRemove(itemId: string | undefined) {
  if (!itemId) return
  const it = active.value
  if (!it || it.type !== 'clarify' || it.done) return
  try {
    await api.reactQueueRemove(it.runId, it.nodeId, itemId)
  } catch (e: any) {
    console.warn('reactQueueRemove failed', e?.message || e)
  }
}

async function onClarifyQueueReorder(itemIds: string[]) {
  if (!itemIds.length) return
  const it = active.value
  if (!it || it.type !== 'clarify' || it.done) return
  try {
    await api.reactQueueReorder(it.runId, it.nodeId, itemIds)
  } catch (e: any) {
    console.warn('reactQueueReorder failed', e?.message || e)
  }
}


function onFocus() {
  peek({ source: 'focus' })
}

function onVisible() {
  if (document.visibilityState === 'visible') peek({ source: 'visibility' })
}

watch(() => {
  const id = conversationRoute()
  return [id.run, id.node]
}, () => {
  // Fresh navigation intent: re-arm so the new target can show a loading card.
  incomingArmed.value = true
  void waitForQueryItem()
})

seedIncomingIfNeeded()

onMounted(() => {
  loadList({ showLoading: true }).then(() => {
    if (incomingTarget()) {
      void waitForQueryItem()
      return
    }
    if (!active.value) active.value = listItems.value[0] || null
  })
  peek({ source: 'mount' })
  releaseAutoApplyHold = addAutoApplyHold(() => autoApplyHeld.value)
  syncFocusedFieldDraft()
  document.addEventListener('focusin', syncFocusedFieldDraft)
  document.addEventListener('focusout', onFocusOut)
  document.addEventListener('input', onFieldInput)
  document.addEventListener('visibilitychange', onVisible)
  window.addEventListener('focus', onFocus)
})
onUnmounted(() => {
  activeVisitEpoch++
  queryWaitGen++
  stopStartingPoll()
  document.removeEventListener('visibilitychange', onVisible)
  window.removeEventListener('focus', onFocus)
  document.removeEventListener('focusin', syncFocusedFieldDraft)
  document.removeEventListener('focusout', onFocusOut)
  document.removeEventListener('input', onFieldInput)
  clearTypingIdle()
  // Leaving the page: this page no longer holds the list, so apply what is pending.
  releaseAutoApplyHold?.()
  releaseAutoApplyHold = null
  autoApplyPending()
  closeActiveRunWs()
  reviewState.dispose()
  stopFollow()
  for (const triple of [...inboxContextAborts.keys()]) abortInboxContext(triple)
  confirmFlowDeskHold.value = false
  inboxConfirmFlow.reset()
  clearVisibleMembership()
  setSidebarExtras([])
  setActiveInboxKey(null)
  knownInboxProcessingLock.value = false
})

function itemTitle(it: InboxItem) {
  return it.type === 'gate' ? it.title : it.label
}

function itemSecondary(it: InboxItem) {
  return inboxSecondaryLine(it)
}

  return {
    addProcessingIntent,
    removeProcessingIntent,
    isProcessingIntent,
    isItemCardDisabled,
    markProcessed,
    unmarkProcessed,
    isProcessedTriple,
    abortInboxContext,
    acquireInboxContextSignal,
    releaseInboxContextSignal,
    beginProcessingIntent,
    rollbackProcessingIntent,
    endProcessingIntent,
    isActiveStillInList,
    shouldFetchActiveInboxContext,
    invalidateListLoads,
    removeListItemLocally,
    restoreListItemLocally,
    incomingTarget,
    mergeIncomingGhost,
    confirmIncomingGhostStillNeeded,
    seedIncomingIfNeeded,
    isIncomingContextPending,
    rebindActiveFromList,
    mergeFailedStarting,
    confirmStartingVanished,
    detectStartingFailures,
    dismissStartFailure,
    stopStartingPoll,
    startStartingPoll,
    selectFromQuery,
    applyHomeHandoff,
    selectFromHandoff,
    waitForQueryItem,
    ensureValidActive,
    reconcileProcessedWithList,
    selectActiveAfterRemove,
    handleLeftInboxContext,
    loadList,
    retryListLoad,
    onAppPreviewStagedPick,
    onAppPreviewReviewPick,
    mergeStagedAppPreviewPick,
    syncActiveAfterApply,
    checkProcessedWhileEditing,
    isActive,
    isClarifySoftRefreshBlocked,
    closeActiveRunWs,
    activeDialogueNodeId,
    applyOrBufferAcpFrame,
    flushPendingAcpFrames,
    seedClarifyAcpFromNodeEventsOnce,
    startBusySeedRetry,
    restoreReactSessions,
    flushPendingReviewFrames,
    applyOrBufferReviewFrame,
    projectClarifySessionAfterLoad,
    reseedAfterWsReconnect,
    connectActiveRunWs,
    patchActiveRunArtifacts,
    softRefreshActiveRun,
    loadActiveRun,
    retryActiveRun,
    onManualRefresh,
    onReactRevised,
    onResolve,
    patchShareStatus,
    openSharePanel,
    selectItem,
    openDetail,
    backToList,
    onClarifySend,
    onClarifyRetryLast,
    onClarifyFinish,
    onClarifyFinishAbort,
    onClarifyCancel,
    onClarifyQueueRemove,
    onClarifyQueueReorder,
    onFocus,
    onVisible,
    itemTitle,
    itemSecondary,
    router,
    route,
    toast,
    PAGE_SIZE,
    SKELETON_CARDS,
    listItems,
    listTotal,
    listPage,
    listLoading,
    listLoadError,
    listErrorMessage,
    listLoadGeneration,
    active,
    homeSeed,
    incomingGhost,
    startFailedItem,
    incomingArmed,
    activeHomeSeed,
    activeQueuedFirstMessage,
    mobileView,
    listScrollTop,
    listEl,
    gateApprovalRef,
    reviewShellRef,
    reviewChatRef,
    confirmFlowDeskHold,
    activeHandoff,
    activeChainSegments,
    activeFollowTail,
    activeFollowProcess,
    followObserving,
    followStopping,
    takeoverFollow,
    inboxConfirmFlow,
    inboxConfirmFlowPhase: inboxConfirmFlow.phase,
    inboxConfirmFlowReduce: inboxConfirmFlow.reduceMotion,
    inboxConfirmFlowToken: inboxConfirmFlow.playToken,
    pendingAcpFrames,
    workflowFilterOpen,
    tagFilterOpen,
    showProcessedBanner,
    manualRefreshing,
    showListSkeleton,
    showListError,
    showListRefresh,
    listPanelBusy,
    clarifyConfirmError,
    clarifyConfirmCanAbort,
    processedTriples,
    confirmedAbsentTriples,
    inboxContextAborts,
    processingIntentKeys,
    processingLock,
    startFailedActive,
    activeStarting,
    startingSandboxPhase,
    startingBootKey,
    incomingGhostConfirmInFlight,
    STARTING_POLL_MS,
    STARTING_POLL_MAX_TICKS,
    queryWaitGen,
    isGateEditing,
    lastStagedAppPreviewPick,
    isClarifyEditing,
    isEditing,
    focusedFieldHasDraft,
    typingRecently,
    autoApplyHeld,
    activeRun,
    activeRunLoading,
    activeRunLoadError,
    activeRunWsRunId,
    clarifyLiveBusy,
    pageControlByNode,
    dialogueRailsFilled,
    dialogueLiveIncremental,
    busySeedRetry,
    activeRunWsReconnect,
    activeClarify,
    inboxAppPreviewActive,
    inboxRemoteKind,
    inboxStageNode,
    showClarifyReviewShell,
    clarifyComposerNodeId,
    clarifyComposerIteration,
    clarifyComposerTurns,
    activeNodeTask,
    clarifyComposerDone,
    inboxClarifyStageKind,
    inboxReviewState,
    reviewActive,
    composerMode,
    activeGate,
    clarifyInputActive,
    clarifyComposerLocked,
    sharePanelOpen,
    shareTarget,
    t,
    displayedItems,
    remoteItems,
    totalCount,
    refresh,
    peek,
    applyPending,
    removeItemLocally,
    restoreItemLocally,
    hasPendingUpdate,
    lastPeekAt,
    lastAutoAppliedAt,
    itemKey,
    patchVisibleCardBusy,
    ariaBusy,
    isMobile,
    selected,
    selectedTags,
    clarifyDraft,
    clarifyAttachments,
    clarifyAnnotations,
    isShareableInboxItem,
    isHumanGateInboxItem,
    inboxShareKind,
  }
}
