/**
 * Run detail view orchestration (WS/live log/selection composed here).
 */
import { ref, computed, watch, nextTick, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api/api'
import { useToast } from '@/lib/composables/useToast'
import {
  applyOuterSashMem,
  clampOuterRight,
  estimateOuterWorkspace,
  initOuterSashFromMemory,
  outerRightMax,
  outerRightMin,
  readSharedOuterSashMem,
  reviewDefaultRightPx,
  writeSharedOuterSashMem,
} from '@/lib/inbox/reviewLayoutBudget'
import { addClarifyAnnotation, useClarifyDraft } from '@/lib/inbox/useClarifyDraft'
import { previewPickAnnotation, type AppPreviewPickPayload } from '@/lib/shared/previewPickUrl'
import { resolveNodeDisplayLabelFromNode } from '@/lib/run/resolveNodeDisplayLabel'
import { applyPreviewArtifactName } from '@/lib/run/reactArtifactPreview'
import {
  artifactsListFingerprint,
  mergeRunChromeFields,
  runChromeFingerprint,
  runSnapshotUnchanged,
} from '@/lib/run/mergeRunChrome'
import { fmtTime, fmtDuration, formatTrigger } from '@/lib/shared/format'
import { pickDefaultTimelineNodeId } from '@/lib/run/runStats'
import { useRunDetailLiveLog } from '@/lib/run/useRunDetailLiveLog'
import { useRunDetailWs } from '@/lib/run/useRunDetailWs'
import { useRunDetailSelection } from '@/lib/run/useRunDetailSelection'
import { playConfirmFlowCeremony } from '@/lib/inbox/confirmFlowCeremony'
import type { AcpEvent, NodeRun, NodeRunStatus, Run, Workflow } from '@/lib/shared/types'
import { isClearlyInvalidRunRouteId } from '@/lib/pm/pmCitationShape'
import type { RunPriority } from '@/components/ui/PrioritySegmented.vue'

type RunLoadErrorKind = 'not_found' | 'network_or_server'


export function useRunDetail() {
const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const toast = useToast()
const runId = computed(() => route.params.id as string)

function emptyRun(id: string): Run {
  return {
    id, workflowId: '', workflowName: t('pages.runDetail.loadingWorkflow'), status: 'running', trigger: '',
    startedAt: new Date().toISOString(), durationSec: 0, progress: 0,
    nodeRuns: {}, artifacts: [], trace: [], vars: [],
  }
}

function emptyWorkflow(): Workflow {
  return { id: '', name: '', description: '', status: 'draft', version: 1, updatedAt: '', needsRepo: false, nodes: [], edges: [] }
}

const run = ref<Run>(emptyRun(runId.value))
const wf = ref<Workflow>(emptyWorkflow())
/** Project alias for 「未知/未分桶」display on Run token table. */
const unknownModelDisplayName = ref('')
/** projectId for which unknownModelDisplayName was last successfully loaded. */
let unknownAliasLoadedForProjectId: string | null = null

async function loadUnknownModelDisplayName(projectId?: string | null) {
  const pid = projectId ?? null
  if (!pid) {
    unknownModelDisplayName.value = ''
    unknownAliasLoadedForProjectId = null
    return
  }

  const projectChanged =
    unknownAliasLoadedForProjectId !== null && unknownAliasLoadedForProjectId !== pid
  if (projectChanged) {
    unknownModelDisplayName.value = ''
    unknownAliasLoadedForProjectId = null
  }

  // Soft refresh: keep the cached alias and skip redundant getProject polls.
  if (unknownAliasLoadedForProjectId === pid) return

  try {
    const p = await api.getProject(pid)
    unknownModelDisplayName.value = p.unknownModelDisplayName || ''
    unknownAliasLoadedForProjectId = pid
  } catch {
    // Keep the last successful alias when refresh fails; only clear on first load miss.
    if (unknownAliasLoadedForProjectId !== pid) {
      unknownModelDisplayName.value = ''
    }
  }
}
const runLoading = ref(false)
const loadError = ref(false)
const loadErrorKind = ref<RunLoadErrorKind | null>(null)
const refreshing = ref(false)
const manual = ref(false)

/** ClarifyChat / ReviewComposer surface for review WS frames (queue/stream/Cancel). */
const reviewChatRef = ref<{
  applyReviewFrame?: (frame: any) => boolean | void
  applyAcpEvents?: (events: AcpEvent[] | undefined, nodeId?: string) => boolean | void
  discardLastQueued?: () => void
  isSessionBusy?: () => boolean
  isChatReady?: () => boolean
  playConfirmCeremony?: () => Promise<void>
} | null>(null)

const gateApprovalRef = ref<{
  applyReviewFrame?: (frame: any) => void
  applyAcpEvents?: (events: AcpEvent[] | undefined) => boolean | void
  playConfirmCeremony?: () => Promise<void>
} | null>(null)

const ACTIVE = ['queued', 'running', 'waiting_human']

// Live wall-clock tick (1s) so the elapsed timer advances without waiting for
// the 2s state poll. Backend never fills Run.durationSec mid-run, so we derive
// it here from startedAt instead of showing a frozen 00:00.
const nowMs = ref(Date.now())
const elapsedSec = computed(() => {
  const start = Date.parse(run.value.startedAt)
  if (isNaN(start)) return run.value.durationSec || 0
  if (ACTIVE.includes(run.value.status)) return Math.max(0, Math.floor((nowMs.value - start) / 1000))
  if (run.value.durationSec > 0) return run.value.durationSec
  // Terminal run without a backend duration: derive the end from the latest
  // node finish (startedAt + its own duration).
  let end = start
  for (const nr of Object.values(run.value.nodeRuns)) {
    if (!nr.startedAt) continue
    const t = Date.parse(nr.startedAt) + (nr.durationSec || 0) * 1000
    if (!isNaN(t) && t > end) end = t
  }
  return Math.max(0, Math.floor((end - start) / 1000))
})

// Progress that counts the in-flight node as half-done, so a run sitting on its
// first (long) node reads as "in progress" instead of a flat 0%. Falls back to
// the backend fraction when the pinned graph isn't available yet.
const progressFrac = computed(() => {
  const nodes = wf.value.nodes.length ? wf.value.nodes : run.value.nodes || []
  const total = nodes.length
  if (!total) return run.value.progress || 0
  if (run.value.status === 'completed') return 1
  let done = 0
  let active = 0
  for (const n of nodes) {
    const st = statusMap.value[n.id]
    if (st === 'completed' || st === 'skipped') done++
    else if (st === 'running' || st === 'waiting_human') active++
  }
  return Math.min(1, (done + active * 0.5) / total)
})


// default selection: last running/waiting_human in timeline order, else most recent execution
const defaultNode = computed(() => pickDefaultTimelineNodeId(run.value))

const selected = ref<string | null>(defaultNode.value || null)
const selNode = computed(() => wf.value.nodes.find((n) => n.id === selected.value) || null)
const selNodeDisplayLabel = computed(() =>
  selNode.value ? resolveNodeDisplayLabelFromNode(selNode.value, t) : '',
)

// The selected node's execution history (oldest→newest). A node that ran
// several times (loop-back / gate revise / rollback retry) has one entry per
// execution. Falls back to the single latest run for older runs / live nodes.
const selExecutions = computed<NodeRun[]>(() => {
  const id = selected.value
  if (!id) return []
  const list = run.value.nodeExecutions?.[id]
  if (list && list.length) return list
  const single = run.value.nodeRuns[id]
  return single ? [single] : []
})
// Which execution the user is viewing; null = the latest. Reset on re-selection.
const selIterIdx = ref<number | null>(null)
// When the timeline selects a specific execution it also changes `selected`;
// this carries the desired iteration index across that change so the reset
// below lands on the picked execution instead of the latest.
const pendingIter = ref<number | null>(null)
watch(selected, () => {
  selIterIdx.value = pendingIter.value
  pendingIter.value = null
})
// Clamp/refresh the pointer when the history grows (e.g. a new iteration starts
// while viewing the latest): stay pinned to latest unless the user picked one.
const selExecIdx = computed(() => {
  const n = selExecutions.value.length
  if (!n) return -1
  if (selIterIdx.value == null) return n - 1
  return Math.min(selIterIdx.value, n - 1)
})
const viewingLatest = computed(() => selExecIdx.value === selExecutions.value.length - 1)
const selRun = computed<NodeRun | null>(() => {
  const idx = selExecIdx.value
  return idx >= 0 ? selExecutions.value[idx] : selected.value ? run.value.nodeRuns[selected.value] : null
})

// Effective status of the selected node, falling back to the canvas status map
// (which surfaces the in-flight node as running even before a StateRun exists).
const selStatus = computed<NodeRunStatus>(() => selRun.value?.status || statusMap.value[selected.value || ''] || 'pending')
// Always give NodeOutputPanel a node-run to render, even before the node has
// produced one — so the panel shows "waiting / running" instead of vanishing.
const selRunView = computed<NodeRun | null>(() =>
  selNode.value ? selRun.value || { nodeId: selNode.value.id, status: selStatus.value, outputs: {} } : null,
)


const selection = useRunDetailSelection({
  run,
  wf,
  selected,
  manual,
  selNode,
  selStatus,
  selRun,
  runLoading,
})
const {
  isMobile,
  selClarify,
  clarifyInputActive,
  clarifySandboxFailed,
  hasLog,
  hasAppPreview,
  reviewActive,
  nodeTabs,
  onNodeTabDisabledClick,
  nodeTab,
  outputFocusLock,
  queryParam,
  mobileMainPanel,
  timelineScrollToken,
  mobileDetailPanelLabel,
  showMobileTimelinePanel,
  showMobileDetailPanel,
  backToMobileTimeline,
  applyOutputDeepLinkFocus,
  selectNode,
} = selection

// Run-level failure banner for any failed run (research/agent early fails included).
const runFailureReason = computed(() => {
  if (run.value.status !== 'failed') return ''
  return (run.value.error || run.value.failedReason || '').trim()
})
const showRunFailureBanner = computed(() => !!runFailureReason.value)
const { draft: clarifyDraft, attachments: clarifyAttachments, annotations: clarifyAnnotations } = useClarifyDraft(() => runId.value, () => selected.value)

/** VNC pick on app_preview review stage → same ReAct annotation chips as structured ⤴. */
const lastStagedAppPreviewPick = ref<AppPreviewPickPayload | null>(null)

function onAppPreviewStagedPick(payload: AppPreviewPickPayload | null) {
  lastStagedAppPreviewPick.value = payload
}

function onAppPreviewReviewPick(payload: AppPreviewPickPayload) {
  const rid = run.value?.id
  const nid = selNode.value?.id
  if (!rid || !nid) return
  const result = addClarifyAnnotation(rid, nid, previewPickAnnotation(payload))
  if (result === 'duplicate') toast.warn(t('pages.reviewComposer.alreadyAdded'))
  // Added to pending chips — clear staged so send won't double-attach.
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

const live = useRunDetailLiveLog({
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
})
const {
  eventPages,
  liveBusy,
  liveNode,
  rehydrateByNode,
  sandboxLookup,
  sbxLogLoading,
  fetchNodeEvents,
  rehydrateNodeEvents,
  retryRehydrate,
  selRehydrateStatus,
  loadEarlierEvents,
  fetchSandboxLog,
  resetLiveLogState,
  abortSandboxFetches,
  syncAllMcpCallsFromRun,
  logEvents,
  logHasMore,
  logLive,
  logBusy,
  selMcpCalls,
  sbxLog,
  panelSwitching,
  openSandboxConsole,
  maybePollSandboxForBoot,
  goSandboxLogTab,
  currentLiveLogBootSession,
  onLiveLogBootSession,
  mergeLiveWsAcpPage,
} = live

/** Clarify/review session in-flight: skip full-page loadRun (g3.2 / review v1). */
function isClarifySessionBusy(): boolean {
  const nodeId = selClarify.value?.nodeId
  if (!nodeId) return false
  if (liveBusy[nodeId] === true) return true
  if (!!(run.value as any)?.reactSessions?.[nodeId]?.busy) return true
  if (reviewChatRef.value?.isSessionBusy?.()) return true
  return false
}

async function refreshArtifactPreviewState(frame?: { previewArtifact?: string }) {
  const name = String(frame?.previewArtifact || '').trim()
  const nodeId = selClarify.value?.nodeId || selected.value
  if (name && nodeId) {
    run.value = applyPreviewArtifactName(run.value, nodeId, name)
  }
  try {
    const arts = await api.runArtifacts(runId.value)
    if (!Array.isArray(arts)) return
    if (artifactsListFingerprint(run.value.artifacts) === artifactsListFingerprint(arts)) return
    run.value = { ...run.value, artifacts: arts }
  } catch {
    /* keep last known artifacts */
  }
}

/** Busy-safe REST patch: chrome only, never hard load / dialogue overwrite (g1.1 / g1.3). */
let chromePatchInflight = false
async function patchRunChrome() {
  const id = runId.value
  if (chromePatchInflight || runLoading.value || loadError.value) return
  if (isClearlyInvalidRunRouteId(id)) return
  chromePatchInflight = true
  try {
    const snapshot = await api.getRun(id)
    const prev = run.value
    const merged = mergeRunChromeFields(prev, snapshot)
    const artsChanged =
      artifactsListFingerprint(prev.artifacts) !== artifactsListFingerprint(merged.artifacts)
    if (runChromeFingerprint(prev) !== runChromeFingerprint(merged)) {
      run.value = merged
    }
    if (artsChanged) {
      void refreshArtifactPreviewState()
    }
  } catch {
    /* keep last known chrome */
  } finally {
    chromePatchInflight = false
  }
}

const wsApi = useRunDetailWs({
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
  loadRun: (hard = false, silent = false) => loadRun(hard, silent),
  patchRunChrome,
  refreshArtifactPreview: (frame) => {
    void refreshArtifactPreviewState(frame)
  },
})
const {
  projectDialogueAfterLoad,
  resetDialogueState,
  teardownRealtime,
  initAfterLoadSuccess,
} = wsApi

function resetRunState(id: string) {
  resetLiveLogState(id)
  run.value = emptyRun(id)
  wf.value = emptyWorkflow()
  unknownModelDisplayName.value = ''
  unknownAliasLoadedForProjectId = null
  selected.value = null
  manual.value = false
  gateError.value = null
  clarifyConfirmError.value = null
  resumeError.value = null
  resetDialogueState()
}

function classifyRunLoadError(err: unknown): RunLoadErrorKind {
  const msg = err instanceof Error ? err.message : String(err ?? '')
  // Prefer explicit not-found over apiState.online (404 also flips online=false).
  if (/not found/i.test(msg) || /^404\b/.test(msg) || msg.includes(' 404 ')) {
    return 'not_found'
  }
  if (err instanceof TypeError || /failed to fetch/i.test(msg) || /network/i.test(msg)) {
    return 'network_or_server'
  }
  // Other non-ok HTTP / server errors remain retryable.
  return 'network_or_server'
}

async function fetchRunData(): Promise<true | RunLoadErrorKind> {
  const id = runId.value
  if (isClearlyInvalidRunRouteId(id)) {
    return 'not_found'
  }
  try {
    const r = await api.getRun(id)
    if (run.value.id === r.id && runSnapshotUnchanged(run.value, r)) {
      await loadUnknownModelDisplayName(wf.value.projectId)
      return true
    }
    run.value = r
    // Prefer the run's own pinned graph so the canvas reflects exactly what
    // executed, even if the live workflow was since edited or deleted. Fall
    // back to fetching the workflow definition only if the run lacks a graph.
    if (r.nodes?.length) {
      wf.value = {
        id: r.workflowId, name: r.workflowName, description: '', status: 'published',
        version: r.workflowVersion || 1, updatedAt: '', needsRepo: false, nodes: r.nodes, edges: r.edges || [],
      }
      // Still resolve projectId for unknown-model display alias when graph is pinned.
      if (r.workflowId) {
        try {
          const live = await api.getWorkflow(r.workflowId)
          if (live.projectId) wf.value.projectId = live.projectId
        } catch {
          // Workflow may have been deleted; alias stays unset → default label.
        }
      }
    } else if (r.workflowId) {
      wf.value = await api.getWorkflow(r.workflowId)
    }
    await loadUnknownModelDisplayName(wf.value.projectId)
    // Refresh-resume: wait for dialogue surfaces to mount, then project busy/queue.
    void projectDialogueAfterLoad(r)
    return true
  } catch (err) {
    return classifyRunLoadError(err)
  }
}

async function loadRun(hard = false, silent = false) {
  const id = runId.value
  if (hard) {
    runLoading.value = true
    loadError.value = false
    loadErrorKind.value = null
    resetRunState(id)
    teardownRealtime()
  } else if (!silent) {
    refreshing.value = true
  }

  try {
    const result = await fetchRunData()
    if (hard) {
      if (result === true) {
        loadError.value = false
        loadErrorKind.value = null
        await initAfterLoadSuccess({
          applyDetailArtifactsDeepLink,
          applyOutputDeepLinkFocus,
          defaultNode: defaultNode.value,
          syncAllMcpCallsFromRun,
        })
      } else {
        loadError.value = true
        loadErrorKind.value = result
      }
    }
  } finally {
    if (hard) runLoading.value = false
    else if (!silent) refreshing.value = false
  }
}

let clock: number | undefined

onMounted(async () => {
  await loadRun(true)
  clock = window.setInterval(() => {
    if (ACTIVE.includes(run.value.status)) nowMs.value = Date.now()
  }, 1000)
  document.addEventListener('visibilitychange', onVisible)
  window.addEventListener('focus', onFocusRefresh)
})
function onFocusRefresh() {
  if (runLoading.value || loadError.value) return
  // Auto / visibility: never hard load (g1.3). Busy → chrome patch only (g1.2).
  if (isClarifySessionBusy()) {
    void patchRunChrome()
    return
  }
  void loadRun(false, true)
}
function onVisible() {
  if (document.visibilityState === 'visible') onFocusRefresh()
}
watch(
  () => route.params.id,
  (newId, oldId) => {
    if (!newId || newId === oldId) return
    loadRun(true)
  },
)
onUnmounted(() => {
  if (clock) window.clearInterval(clock)
  abortSandboxFetches()
  document.removeEventListener('visibilitychange', onVisible)
  window.removeEventListener('focus', onFocusRefresh)
  teardownRealtime()
})

function onArtifactDeleted(id: string) {
  run.value.artifacts = run.value.artifacts.filter((a) => a.id !== id)
}

const gateError = ref<string | null>(null)
const gateSubmitting = ref(false)
const clarifyConfirmError = ref<string | null>(null)
async function onGateResolve(action: string, form: Record<string, any> = {}) {
  if (!run.value.gate || gateSubmitting.value) return
  gateSubmitting.value = true
  gateError.value = null
  const positive = action === 'pass' || action === 'approve'
  // Click intent: play overlay before resume returns (plan g1.1).
  if (positive) void playConfirmFlowCeremony(gateApprovalRef.value)
  try {
    await api.resumeGate(runId.value, run.value.gate.nodeId, action, form)
    toast.success(positive ? t('pages.gateApproval.approveSuccess') : t('pages.gateApproval.rejectSuccess'))
  } catch (e: any) {
    // Surface the backend rejection (e.g. a required form field, or the run
    // having ended while paused) instead of silently swallowing it and leaving
    // the gate showing an optimistic "已提交".
    gateError.value = e?.message || t('pages.runDetail.gateError')
  } finally {
    gateSubmitting.value = false
  }
  await loadRun(false)
}
async function onClarifySend(
  text: string,
  images: import('@/lib/shared/types').ClarifyImage[] = [],
  annotations: import('@/lib/shared/types').ReactAnnotation[] = [],
  force = false,
  retryLast = false,
) {
  const conv = selClarify.value
  if (!conv || conv.done) return
  const nodeId = conv.nodeId
  clarifyConfirmError.value = null
  // Demo: send may attach the latest staged pick even if user skipped「添加到聊天」.
  const anns =
    hasAppPreview.value && !force && !retryLast
      ? mergeStagedAppPreviewPick(annotations)
      : annotations
  if (anns !== annotations) lastStagedAppPreviewPick.value = null
  // Click intent: play overlay before wrap-up HTTP (plan g1.1); never wait for forceOk/done.
  if (force) void playConfirmFlowCeremony(reviewChatRef.value)
  try {
    if (retryLast) {
      await api.reactReply(runId.value, nodeId, text, images, force, anns, true)
    } else {
      await api.reactReply(runId.value, nodeId, text, images, force, anns)
    }
  } catch (e: any) {
    // Re-sync below so the UI reflects the real state (e.g. the dialogue has
    // already completed) instead of leaving the input enabled to re-click.
    console.warn('reactReply failed', e?.message || e)
    const msg = e?.message || t('pages.runDetail.gateError')
    // Non-force: roll back optimistic pending-send row so FR4 / send lock is not stuck.
    if (!force && !retryLast) reviewChatRef.value?.discardLastQueued?.()
    clarifyConfirmError.value = msg
  }
  // Enqueue returns before the turn finishes — avoid wiping live bubbles.
  // Force finish still needs a snapshot refresh.
  if (force) {
    lastStagedAppPreviewPick.value = null
    await loadRun(false)
  }
}

/** Cover-this-turn retry: re-run last human without a new bubble (plan g2.1). */
async function onClarifyRetryLast() {
  await onClarifySend('', [], [], false, true)
}
async function onClarifyCancel() {
  const conv = selClarify.value
  if (!conv || conv.done) return
  try {
    await api.reactCancel(runId.value, conv.nodeId)
  } catch (e: any) {
    console.warn('reactCancel failed', e?.message || e)
  }
}

async function onClarifyQueueRemove(itemId: string | undefined) {
  if (!itemId) return
  const conv = selClarify.value
  if (!conv || conv.done) return
  try {
    await api.reactQueueRemove(runId.value, conv.nodeId, itemId)
  } catch (e: any) {
    console.warn('reactQueueRemove failed', e?.message || e)
  }
}

async function onClarifyQueueReorder(itemIds: string[]) {
  if (!itemIds.length) return
  const conv = selClarify.value
  if (!conv || conv.done) return
  try {
    await api.reactQueueReorder(runId.value, conv.nodeId, itemIds)
  } catch (e: any) {
    console.warn('reactQueueReorder failed', e?.message || e)
  }
}
// Clarify: finish early. Review: confirm product & advance (different prompt).
function onClarifyFinish() {
  const prompt = reviewActive.value
    ? t('pages.clarify.confirmFlowPrompt')
    : t('pages.runDetail.clarifyFinishPrompt')
  onClarifySend(prompt, [], [], true)
}
const canCancelRun = computed(() => {
  const s = run.value?.status
  return s === 'queued' || s === 'running' || s === 'waiting_human'
})

const showCancelConfirm = ref(false)
const cancellingRun = ref(false)
const cancelRunError = ref('')

function openCancelConfirm() {
  if (!canCancelRun.value || cancellingRun.value) return
  cancelRunError.value = ''
  showCancelConfirm.value = true
}

function closeCancelConfirm() {
  if (cancellingRun.value) return
  showCancelConfirm.value = false
  cancelRunError.value = ''
}

function mapCancelRunError(e: unknown): string {
  const status = (e as { status?: number })?.status
  const msg = e instanceof Error ? e.message : String(e || '')
  if (status === 404 || /not found/i.test(msg)) return t('pages.runDetail.cancelErrorNotFound')
  if (status === 400 || /already finished|cannot cancel/i.test(msg)) {
    return t('pages.runDetail.cancelErrorNotCancellable')
  }
  return msg || t('pages.runDetail.cancelErrorGeneric')
}

async function confirmCancelRun() {
  if (!canCancelRun.value || cancellingRun.value) return
  cancellingRun.value = true
  cancelRunError.value = ''
  try {
    await api.cancelRun(runId.value)
    showCancelConfirm.value = false
    toast.success(t('pages.runDetail.cancelSuccess'))
    await loadRun(false)
  } catch (e) {
    cancelRunError.value = mapCancelRunError(e)
  } finally {
    cancellingRun.value = false
  }
}

const canDeleteRun = computed(() => {
  const s = run.value?.status
  return s === 'completed' || s === 'failed' || s === 'cancelled'
})

const deleteRunHint = computed(() => {
  if (canDeleteRun.value) return ''
  const s = run.value?.status
  if (s === 'queued' || s === 'running' || s === 'waiting_human') {
    return t('pages.runDetail.deleteHintActive')
  }
  return ''
})

const showDeleteConfirm = ref(false)
const deletingRun = ref(false)
const deleteRunError = ref('')

function openDeleteConfirm() {
  if (!canDeleteRun.value || deletingRun.value) return
  deleteRunError.value = ''
  showDeleteConfirm.value = true
}

function closeDeleteConfirm() {
  if (deletingRun.value) return
  showDeleteConfirm.value = false
  deleteRunError.value = ''
}

function mapDeleteRunError(e: unknown): string {
  const status = (e as { status?: number })?.status
  const msg = e instanceof Error ? e.message : String(e || '')
  if (status === 404 || /not found/i.test(msg)) return t('pages.runDetail.deleteErrorNotFound')
  if (status === 409 || /cannot delete run/i.test(msg)) return t('pages.runDetail.deleteErrorNotDeletable')
  return msg || t('pages.runDetail.deleteErrorGeneric')
}

async function confirmDeleteRun() {
  if (!canDeleteRun.value || deletingRun.value) return
  const wfId = run.value?.workflowId || ''
  deletingRun.value = true
  deleteRunError.value = ''
  try {
    await api.deleteRun(runId.value)
    showDeleteConfirm.value = false
    toast.success(t('pages.runDetail.deleteSuccess'))
    const qs = wfId ? `?wf=${encodeURIComponent(wfId)}` : ''
    await router.push('/runs' + qs)
  } catch (e) {
    deleteRunError.value = mapDeleteRunError(e)
  } finally {
    deletingRun.value = false
  }
}

// Download the full logs of this run as a single text file for offline error
// diagnosis (FSM trace + every node/iteration's agent events, MCP calls,
// errors, output + sandbox docker logs), assembled server-side.
function exportLogs() {
  const a = document.createElement('a')
  a.href = api.exportRunLogsUrl(runId.value)
  a.download = `${runId.value}-logs.txt`
  a.click()
}

// Continue a failed/cancelled run from a node (default: the node that failed),
// reusing everything the original run already produced. Used after a transient
// fault (e.g. a sandbox/ACP hiccup) so automation recovers without re-running
// the whole workflow.
const resuming = ref(false)
const resumeError = ref<string | null>(null)
async function onResume(nodeId = '') {
  if (resuming.value) return
  resuming.value = true
  resumeError.value = null
  try {
    await api.resumeRun(runId.value, nodeId)
  } catch (e: any) {
    resumeError.value = e?.message || t('pages.runDetail.resumeError')
  } finally {
    resuming.value = false
  }
  await loadRun(false)
}

const priorityDraft = ref<RunPriority>('normal')
const prioritySaving = ref(false)
const priorityError = ref<string | null>(null)
const priorityOk = ref(false)
const priorityPopoverOpen = ref(false)
const priorityBadgeRef = ref<HTMLElement | null>(null)
const priorityEditorRef = ref<HTMLElement | null>(null)
const priorityPopoverStyle = ref<Record<string, string>>({
  top: '0px',
  left: '0px',
  width: '320px',
})
/** Success tip (+ auto-close) is cleared on its own timer — never by draft sync after save. */
let priorityOkTimer: ReturnType<typeof setTimeout> | null = null

const priorityEditable = computed(() =>
  ['queued', 'running', 'waiting_human'].includes(run.value.status),
)

/** Weak discoverability cue: chevron only while queued. */
const showPriorityChevron = computed(
  () => priorityEditable.value && run.value.status === 'queued',
)

const committedPriority = computed<RunPriority>(() => {
  const p = run.value.priority
  return p === 'high' || p === 'low' || p === 'normal' ? p : 'normal'
})

/** Match badge semantic color so the chevron reads as part of the trigger. */
const priorityChevronClass = computed(() => {
  const p = committedPriority.value
  if (p === 'high') return 'text-[#f87171]'
  if (p === 'low') return 'text-txt3'
  return 'text-accent-2'
})

const priorityTriggerTitle = computed(() =>
  priorityEditable.value
    ? t('pages.runDetail.priorityEditTrigger')
    : t('common.priority.label'),
)

const priorityTriggerAria = computed(() => {
  const priority = t(`common.priority.${committedPriority.value}`)
  if (priorityEditable.value) {
    return t('pages.runDetail.priorityEditAria', { priority })
  }
  return `${t('common.priority.label')} ${priority}`
})

const priorityHint = computed(() => {
  switch (run.value.status) {
    case 'running':
      return t('pages.runDetail.priorityHintRunning')
    case 'waiting_human':
      return t('pages.runDetail.priorityHintWaiting')
    case 'queued':
      return t('pages.runDetail.priorityHintQueued')
    default:
      return t('pages.runDetail.priorityHintTerminal')
  }
})

function clearPriorityOkTimer() {
  if (priorityOkTimer) {
    clearTimeout(priorityOkTimer)
    priorityOkTimer = null
  }
}

function placePriorityPopover() {
  const anchor = priorityBadgeRef.value
  if (!anchor || !priorityPopoverOpen.value) return
  const margin = 16
  const gap = 8
  const width = Math.min(320, window.innerWidth - 32)
  const rect = anchor.getBoundingClientRect()
  let left = rect.left
  if (left + width > window.innerWidth - margin) {
    left = window.innerWidth - margin - width
  }
  left = Math.max(margin, left)
  let top = rect.bottom + gap
  // Keep panel in viewport when near the bottom edge.
  const estimatedHeight = 220
  if (top + estimatedHeight > window.innerHeight - margin) {
    top = Math.max(margin, rect.top - estimatedHeight - gap)
  }
  priorityPopoverStyle.value = {
    top: `${Math.round(top)}px`,
    left: `${Math.round(left)}px`,
    width: `${Math.round(width)}px`,
  }
}

function openPriorityPopover() {
  if (!priorityEditable.value) return
  syncPriorityDraft()
  priorityOk.value = false
  priorityError.value = null
  clearPriorityOkTimer()
  priorityPopoverOpen.value = true
  void nextTick(() => {
    placePriorityPopover()
    priorityEditorRef.value?.focus()
  })
}

function closePriorityPopover(discard = true) {
  if (!priorityPopoverOpen.value) return
  priorityPopoverOpen.value = false
  clearPriorityOkTimer()
  priorityOk.value = false
  if (discard) syncPriorityDraft()
  void nextTick(() => priorityBadgeRef.value?.focus())
}

function togglePriorityPopover() {
  if (!priorityEditable.value) return
  if (priorityPopoverOpen.value) closePriorityPopover(true)
  else openPriorityPopover()
}

/** Brief in-popover tip, then auto-close the editor layer. */
function showPrioritySaved() {
  clearPriorityOkTimer()
  priorityOk.value = true
  priorityOkTimer = setTimeout(() => {
    priorityOk.value = false
    priorityOkTimer = null
    closePriorityPopover(false)
  }, 1000)
}

function syncPriorityDraft() {
  const p = run.value.priority
  priorityDraft.value = p === 'high' || p === 'low' || p === 'normal' ? p : 'normal'
  priorityError.value = null
  // Do not clear priorityOk here: save writes run.priority and would flash the tip away.
}

function onPriorityKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && priorityPopoverOpen.value) {
    e.preventDefault()
    closePriorityPopover(true)
  }
}

function onPriorityReposition() {
  if (priorityPopoverOpen.value) placePriorityPopover()
}

watch(
  () => run.value.id,
  () => {
    closePriorityPopover(true)
    clearPriorityOkTimer()
    priorityOk.value = false
    syncPriorityDraft()
  },
)

// Watch scalar sources (not a fresh array each tick): poll/WS that replaces
// run.value with the same priority+status must not re-fire and wipe a dirty draft.
watch(
  [() => run.value.priority, () => run.value.status],
  () => {
    const saved =
      run.value.priority === 'high' || run.value.priority === 'low' || run.value.priority === 'normal'
        ? run.value.priority
        : 'normal'
    // Keep unsaved draft while still editable (running/waiting_human poll) —
    // including while the popover is open with a dirty draft.
    if (priorityEditable.value && priorityDraft.value !== saved) return
    syncPriorityDraft()
  },
)

watch(priorityEditable, (editable) => {
  if (!editable) closePriorityPopover(true)
})

async function savePriority() {
  if (!priorityEditable.value || prioritySaving.value) return
  if (priorityDraft.value === committedPriority.value) return
  prioritySaving.value = true
  priorityError.value = null
  priorityOk.value = false
  clearPriorityOkTimer()
  try {
    const res = await api.updateRunPriority(runId.value, priorityDraft.value)
    run.value = { ...run.value, priority: (res.priority as RunPriority) || priorityDraft.value }
    showPrioritySaved()
  } catch (e: any) {
    priorityError.value = e?.message || t('pages.runDetail.prioritySaveFailed')
  } finally {
    prioritySaving.value = false
  }
}

onMounted(() => {
  document.addEventListener('keydown', onPriorityKeydown)
  window.addEventListener('resize', onPriorityReposition)
  window.addEventListener('scroll', onPriorityReposition, true)
})

onUnmounted(() => {
  clearPriorityOkTimer()
  document.removeEventListener('keydown', onPriorityKeydown)
  window.removeEventListener('resize', onPriorityReposition)
  window.removeEventListener('scroll', onPriorityReposition, true)
})

// A terminated run can be resumed; the header button auto-picks the failed node.
const canResume = computed(() => run.value.status === 'failed' || run.value.status === 'cancelled')
// The selected node can be individually retried when the run is terminated
// (any graph node) or still running with a failed node (react sandbox setup).
const canResumeSelected = computed(() => {
  if (!selected.value || !selNode.value) return false
  if (run.value.status === 'failed' || run.value.status === 'cancelled') return true
  return run.value.status === 'running' && selStatus.value === 'failed'
})

const statusMap = computed<Record<string, NodeRunStatus>>(() => {
  const m: Record<string, NodeRunStatus> = {}
  for (const [k, v] of Object.entries(run.value.nodeRuns)) m[k] = v.status
  // Surface the in-flight node (no StateRun yet) as running on the canvas.
  if (liveNode.value && !m[liveNode.value]) m[liveNode.value] = 'running'
  return m
})

const activePath = computed(() => {
  const ids: string[] = []
  for (const e of wf.value.edges) {
    const s = run.value.nodeRuns[e.source]?.status
    const t = run.value.nodeRuns[e.target]?.status
    if (s === 'completed' && (t === 'running' || t === 'waiting_human')) ids.push(e.id)
  }
  return ids
})

/**
 * Desktop Run Detail: outer sash (canvas/timeline vs whole right panel).
 * All node content tabs share the same width — no per-tab md:w-[520px] fallback.
 * REVIEW_CANVAS_MIN is default reserve only — drag canvas min is 0.
 */
const desktopOuterSashLayout = computed(() => !isMobile.value)
const splitRootRef = ref<HTMLElement | null>(null)
const initialOuterWs = estimateOuterWorkspace()
const initialOuterLayout = initOuterSashFromMemory(initialOuterWs)
const workspacePx = ref(initialOuterWs)
const outerRightPx = ref(initialOuterLayout.width)
const outerFullOpen = ref(initialOuterLayout.fullOpen)
/** Hide split until memory/default width is applied — prevents default→memory flash. */
const outerSashBooting = ref(!isMobile.value)
const outerSashDragging = ref(false)
let outerSashStartX = 0
let outerSashStartW = 0
let outerSashDidDrag = false
const OUTER_SASH_DRAG_THRESHOLD_PX = 3

function measureWorkspace(): number {
  const w = splitRootRef.value?.getBoundingClientRect().width
  return w && w > 0 ? w : 0
}

function revealOuterSashLayout() {
  outerSashBooting.value = false
}

function applyOuterLayout() {
  if (!desktopOuterSashLayout.value) {
    outerSashBooting.value = false
    return
  }
  const ws = measureWorkspace()
  if (ws <= 0) return
  workspacePx.value = ws
  const next = applyOuterSashMem(readSharedOuterSashMem(), ws)
  outerRightPx.value = next.width
  outerFullOpen.value = next.fullOpen
  revealOuterSashLayout()
}

function persistOuterLayout() {
  if (!desktopOuterSashLayout.value) return
  writeSharedOuterSashMem({
    width: outerRightPx.value,
    fullOpen: outerFullOpen.value,
  })
}

function setOuterSashDraggingUi(on: boolean) {
  if (typeof document === 'undefined') return
  document.body.classList.toggle('run-detail-outer-sash-dragging', on)
}

function onOuterSashPointerDown(e: PointerEvent) {
  if (!desktopOuterSashLayout.value) return
  e.stopPropagation()
  e.preventDefault()
  outerSashDragging.value = true
  outerSashDidDrag = false
  outerSashStartX = e.clientX
  outerSashStartW = outerRightPx.value
  setOuterSashDraggingUi(true)
  ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
}

function onOuterSashPointerMove(e: PointerEvent) {
  if (!outerSashDragging.value) return
  e.preventDefault()
  const ws = measureWorkspace()
  if (ws <= 0) return
  workspacePx.value = ws
  const dx = outerSashStartX - e.clientX
  if (Math.abs(dx) > OUTER_SASH_DRAG_THRESHOLD_PX) outerSashDidDrag = true
  const next = clampOuterRight(outerSashStartW + dx, ws, true)
  outerRightPx.value = next.width
  outerFullOpen.value = next.fullOpen
}

function onOuterSashPointerUp() {
  if (!outerSashDragging.value) return
  outerSashDragging.value = false
  setOuterSashDraggingUi(false)
  if (outerSashDidDrag) persistOuterLayout()
}

function onOuterSashDblClick() {
  if (!desktopOuterSashLayout.value || outerSashDidDrag) return
  const ws = measureWorkspace()
  if (ws <= 0) return
  workspacePx.value = ws
  outerFullOpen.value = false
  outerRightPx.value = reviewDefaultRightPx(ws)
  persistOuterLayout()
}

function onOuterSashWindowResize() {
  if (!desktopOuterSashLayout.value || outerSashDragging.value) return
  const ws = measureWorkspace()
  if (ws <= 0) return
  workspacePx.value = ws
  if (outerFullOpen.value) {
    const max = outerRightMax(ws)
    outerRightPx.value = max
    return
  }
  const next = clampOuterRight(outerRightPx.value, ws, false)
  outerRightPx.value = next.width
  outerFullOpen.value = next.fullOpen
}

const reviewRightPanelStyle = computed(() => {
  if (!desktopOuterSashLayout.value) return undefined
  return { width: `${outerRightPx.value}px` }
})

const outerAriaMin = computed(() => outerRightMin(workspacePx.value))
const outerAriaMax = computed(() => outerRightMax(workspacePx.value))

const leftPaneStyle = computed(() => {
  if (!desktopOuterSashLayout.value) return undefined
  if (outerFullOpen.value) {
    return {
      minWidth: '0px',
      width: '0px',
      flexBasis: '0px',
      flexGrow: 0,
      flexShrink: 0,
      overflow: 'hidden',
    }
  }
  return { minWidth: '0px', overflow: 'hidden' }
})

// Re-init only when entering/leaving desktop sash — tab switches keep in-memory width.
watch(
  () => desktopOuterSashLayout.value,
  (on) => {
    if (on) {
      outerSashBooting.value = true
      nextTick(() => applyOuterLayout())
    } else {
      outerSashBooting.value = false
    }
  },
)

watch(
  () => runLoading.value,
  (loading) => {
    if (!loading && !loadError.value) nextTick(() => applyOuterLayout())
  },
)

let splitRootObserver: ResizeObserver | undefined

onMounted(() => {
  window.addEventListener('resize', onOuterSashWindowResize)
  nextTick(() => {
    applyOuterLayout()
    if (typeof ResizeObserver !== 'undefined' && splitRootRef.value) {
      splitRootObserver = new ResizeObserver(() => onOuterSashWindowResize())
      splitRootObserver.observe(splitRootRef.value)
    }
  })
})
onUnmounted(() => {
  splitRootObserver?.disconnect()
  splitRootObserver = undefined
  window.removeEventListener('resize', onOuterSashWindowResize)
  outerSashDragging.value = false
  setOuterSashDraggingUi(false)
})

// Overall run detail drawer: cross-node info not tied to a single node.
const showDetail = ref(false)
const detailTab = ref('trace')
const detailTabs = computed(() => {
  const tabs = [{ id: 'trace', label: t('pages.runDetail.tabs.trace') }]
  if (run.value.vars?.length) tabs.push({ id: 'vars', label: t('pages.runDetail.tabs.vars') })
  // Always offer the run-level env tab so empty snapshot shows an empty state (no error).
  tabs.push({ id: 'sandboxEnv', label: t('pages.runDetail.tabs.sandboxEnv') })
  tabs.push({ id: 'artifacts', label: t('pages.runDetail.tabs.artifacts') })
  return tabs
})

/** Parse ?detail=artifacts (run-output empty-state / triage deep link). */
function applyDetailArtifactsDeepLink(): boolean {
  if (queryParam('detail') !== 'artifacts') return false
  showDetail.value = true
  detailTab.value = 'artifacts'
  return true
}


// Main-area view: canvas / timeline (+ node detail) or execution-stats split.
// Narrow screens default to timeline (no persistence); desktop keeps canvas.
const viewMode = ref<'canvas' | 'timeline' | 'stats'>(isMobile.value ? 'timeline' : 'canvas')
/** Stats sub-tab: single-run (timeline + panel) vs multi-run aggregate (full width). */
const statsTab = ref<'single' | 'multi'>('single')

// Narrow: canvas is not in the allowed set — silently normalize without Toast.
watch(
  isMobile,
  (mobile) => {
    if (mobile && viewMode.value === 'canvas') viewMode.value = 'timeline'
  },
  { immediate: true },
)

// Entering mobile timeline should already have a meaningful selection when possible.
watch([viewMode, isMobile, defaultNode], () => {
  if (isMobile.value && viewMode.value === 'timeline' && !selected.value && defaultNode.value) {
    selected.value = defaultNode.value
  }
})

// Timeline click: focus a node AND a specific past execution so the right panel
// renders that exact iteration's product/log/overview. Setting selected resets
// the iteration pointer (watch above), so route the desired index through
// pendingIter when the node actually changes.
function selectExecution(nodeId: string, idx: number) {
  outputFocusLock.value = false
  manual.value = true
  if (selected.value === nodeId) {
    selIterIdx.value = idx
  } else {
    pendingIter.value = idx
    selected.value = nodeId
  }
  if (isMobile.value) mobileMainPanel.value = 'detail'
}

  return {
  t,
  route,
  router,
  toast,
  runId,
  emptyRun,
  emptyWorkflow,
  run,
  wf,
  unknownModelDisplayName,
  loadUnknownModelDisplayName,
  runLoading,
  loadError,
  loadErrorKind,
  refreshing,
  manual,
  reviewChatRef,
  gateApprovalRef,
  ACTIVE,
  nowMs,
  elapsedSec,
  progressFrac,
  defaultNode,
  selected,
  selNode,
  selNodeDisplayLabel,
  selExecutions,
  selIterIdx,
  pendingIter,
  selExecIdx,
  viewingLatest,
  selRun,
  selStatus,
  selRunView,
  selection,
  runFailureReason,
  showRunFailureBanner,
  lastStagedAppPreviewPick,
  onAppPreviewStagedPick,
  onAppPreviewReviewPick,
  mergeStagedAppPreviewPick,
  live,
  isClarifySessionBusy,
  refreshArtifactPreviewState,
  patchRunChrome,
  wsApi,
  resetRunState,
  classifyRunLoadError,
  fetchRunData,
  loadRun,
  onFocusRefresh,
  onVisible,
  onArtifactDeleted,
  gateError,
  gateSubmitting,
  clarifyConfirmError,
  onGateResolve,
  onClarifySend,
  onClarifyRetryLast,
  onClarifyCancel,
  onClarifyQueueRemove,
  onClarifyQueueReorder,
  onClarifyFinish,
  canCancelRun,
  showCancelConfirm,
  cancellingRun,
  cancelRunError,
  openCancelConfirm,
  closeCancelConfirm,
  mapCancelRunError,
  confirmCancelRun,
  canDeleteRun,
  deleteRunHint,
  showDeleteConfirm,
  deletingRun,
  deleteRunError,
  openDeleteConfirm,
  closeDeleteConfirm,
  mapDeleteRunError,
  confirmDeleteRun,
  exportLogs,
  resuming,
  resumeError,
  onResume,
  priorityDraft,
  prioritySaving,
  priorityError,
  priorityOk,
  priorityPopoverOpen,
  priorityBadgeRef,
  priorityEditorRef,
  priorityPopoverStyle,
  priorityEditable,
  showPriorityChevron,
  committedPriority,
  priorityChevronClass,
  priorityTriggerTitle,
  priorityTriggerAria,
  priorityHint,
  clearPriorityOkTimer,
  placePriorityPopover,
  openPriorityPopover,
  closePriorityPopover,
  togglePriorityPopover,
  showPrioritySaved,
  syncPriorityDraft,
  onPriorityKeydown,
  onPriorityReposition,
  savePriority,
  canResume,
  canResumeSelected,
  statusMap,
  activePath,
  desktopOuterSashLayout,
  splitRootRef,
  workspacePx,
  outerRightPx,
  outerFullOpen,
  outerSashBooting,
  outerSashDragging,
  outerSashStartX,
  outerSashStartW,
  outerSashDidDrag,
  OUTER_SASH_DRAG_THRESHOLD_PX,
  measureWorkspace,
  applyOuterLayout,
  persistOuterLayout,
  setOuterSashDraggingUi,
  onOuterSashPointerDown,
  onOuterSashPointerMove,
  onOuterSashPointerUp,
  onOuterSashDblClick,
  onOuterSashWindowResize,
  reviewRightPanelStyle,
  outerAriaMin,
  outerAriaMax,
  leftPaneStyle,
  showDetail,
  detailTab,
  detailTabs,
  applyDetailArtifactsDeepLink,
  viewMode,
  statsTab,
  selectExecution,
  fmtTime,
  fmtDuration,
  formatTrigger,
  isMobile,
  mobileMainPanel,
  timelineScrollToken,
  mobileDetailPanelLabel,
  showMobileTimelinePanel,
  showMobileDetailPanel,
  backToMobileTimeline,
  selectNode,
  nodeTab,
  nodeTabs,
  onNodeTabDisabledClick,
  clarifyDraft,
  clarifyAttachments,
  clarifyAnnotations,
  clarifyInputActive,
  clarifySandboxFailed,
  selClarify,
  sandboxLookup,
  sbxLogLoading,
  sbxLog,
  fetchSandboxLog,
  retryRehydrate,
  selRehydrateStatus,
  loadEarlierEvents,
  logEvents,
  logHasMore,
  logLive,
  logBusy,
  selMcpCalls,
  panelSwitching,
  openSandboxConsole,
  goSandboxLogTab,
  currentLiveLogBootSession,
  onLiveLogBootSession,
  }
}
