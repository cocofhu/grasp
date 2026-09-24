<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import HtmlPreview from '@/components/ui/HtmlPreview.vue'
import Icon from '@/components/ui/Icon.vue'
import AppModal from '@/components/ui/AppModal.vue'
import LangSelect from '@/components/ui/LangSelect.vue'
import ReviewShell from '@/components/run/ReviewShell.vue'
import ReviewComposer from '@/components/run/ReviewComposer.vue'
import StructuredArtifactView from '@/components/run/StructuredArtifactView.vue'
import ReactArtifactStage from '@/components/run/ReactArtifactStage.vue'
import PublicAppPreviewPanel from '@/components/run/PublicAppPreviewPanel.vue'
import { applyPublicLocale, locale, setLocale, type AppLocale } from '@/lib/shared/locale'
import { REVIEW_SHELL_WIDTH_KEY_APPROVAL } from '@/lib/inbox/reviewLayoutBudget'
import { reapplyThemeChrome } from '@/lib/shared/theme'
import { useBreakpoint } from '@/lib/composables/useBreakpoint'
import { provideReviewAnnotate } from '@/lib/inbox/reviewAnnotate'
import { previewPickAnnotation } from '@/lib/shared/previewPickUrl'
import type { AppPreviewPickPayload } from '@/lib/shared/previewPickUrl'
import { isAbortError } from '@/lib/run/liveLogRehydrate'
import { createWsReconnectController } from '@/lib/run/wsReconnect'
import {
  createBusySeedRetryController,
  runBusySeedRetry,
} from '@/lib/run/busySeedRetry'
import { pickAcpRails } from '@/lib/run/pendingAcpBuffer'
import {
  formatRemainingSec,
  mergePublicGatePreview,
  parseShareTokenFromHash,
  publicGateApi,
  publicGateContentKey,
  normalizePermissionPreset,
  remainingSecFromExpiresAt,
  type PublicGateActiveItem,
  type PublicGateDecideResult,
  type PublicGatePreview,
  type PublicGatePreviewKnown,
  type PublicGateQueueItem,
} from '@/lib/inbox/gateShareLink'
import { isClarifyInteractive } from '@/lib/shared/clarifyInteractive'
import { playConfirmFlowCeremony } from '@/lib/inbox/confirmFlowCeremony'
import type { AcpEvent, Artifact, ClarifyImage, ClarifyTurn, NodeType, ReactAnnotation, Run } from '@/lib/shared/types'

const PUBLIC_SHARE_RUN_ID = 'public-share'

type PublicChatRef = {
  discardLastQueued?: () => void
  applyQueueState?: (
    waiting: number,
    items: PublicGateQueueItem[] | null,
    busy?: boolean,
    activeItem?: PublicGateActiveItem | null,
  ) => void
  applyReviewFrame?: (frame: Record<string, unknown>) => boolean | void
  applyAcpEvents?: (events: AcpEvent[] | undefined, nodeId?: string) => boolean | void
  isSessionBusy?: () => boolean
  playConfirmCeremony?: () => Promise<void>
}

const props = defineProps<{
  /** Chat-only drawer mode: the credential comes from the embed session instead of `#t=`. */
  embedToken?: string
}>()
const emit = defineEmits<{ status: [status: string] }>()
const chatOnly = computed(() => !!props.embedToken)

const POLL_MS = 2000
const IDLE_POLL_MS = 10_000
const REMAINING_TICK_MS = 15_000
const NONCE_TTL_MS = 15 * 60 * 1000
const NONCE_REFRESH_BEFORE_MS = 2 * 60 * 1000

const { t } = useI18n()
const { isMobile } = useBreakpoint()

const ready = ref(false)
const loading = ref(true)
const maybeStuck = ref(false)
const token = ref('')
const preview = ref<PublicGatePreview | null>(null)
const comment = ref('')
const reviewerName = ref('')
const submitting = ref(false)
const pendingKind = ref<'confirm' | 'reject' | null>(null)
const errorText = ref('')
const networkFailed = ref(false)
const workbenchSeen = ref(false)
const linkInvalid = ref(false)
const doneKind = ref<'approved' | 'rejected' | 'confirmed' | null>(null)
const upstreamOpen = ref(false)
const upstreamDocFull = ref<Record<string, unknown> | null>(null)
const upstreamLoading = ref(false)
const upstreamLoadErr = ref('')
const upstreamLoaded = ref(false)
const draft = ref('')
const attachments = ref<ClarifyImage[]>([])
const annotations = ref<ReactAnnotation[]>([])
const chatRef = ref<PublicChatRef | null>(null)
const shellRef = ref<{ playConfirmCeremony?: () => Promise<void> } | null>(null)
const replyInFlight = ref(false)
const pendingReplyText = ref('')

let lastKeptNonce = ''
let nonceIssuedAt = 0
let lastAppliedIdleQueue = false
let pollIntervalMs = POLL_MS
let remainingTimer: ReturnType<typeof setInterval> | null = null

let previewGen = 0
let previewAbort: AbortController | null = null
let decideAbort: AbortController | null = null
let upstreamAbort: AbortController | null = null
let artifactsAbort: AbortController | null = null
let stuckTimer: number | null = null

const publicArtifacts = ref<Artifact[]>([])
const publicRunGraph = ref<Run | null>(null)

function clearStuckTimer() {
  if (stuckTimer != null) {
    clearTimeout(stuckTimer)
    stuckTimer = null
  }
  maybeStuck.value = false
}

function abortPreview() {
  previewAbort?.abort()
  previewAbort = null
  clearStuckTimer()
}

const isReview = computed(() => preview.value?.kind === 'review')
const isClarify = computed(() => isClarifyInteractive(preview.value?.nodeType))
const composerMode = computed<'clarify' | 'review'>(() => (isClarify.value ? 'clarify' : 'review'))
const status = computed(() => preview.value?.status || (token.value ? 'invalid' : 'invalid'))
const isActive = computed(() => status.value === 'active')
const remainingLabel = ref('')
const reactAlive = computed(() => !!preview.value?.reactSessionAlive)
/** ClarifyChat local session busy (thinking/queued/live); authoritative for confirm. */
const localChatBusy = ref(false)
function refreshLocalChatBusy() {
  localChatBusy.value = !!chatRef.value?.isSessionBusy?.()
}
const canReply = computed(() => isActive.value && reactAlive.value && !!preview.value?.actions?.reply)
const canReject = computed(() => !isReview.value && !!preview.value?.actions?.reject)
/** Business mountability only — busy/submitting must not hide the confirm control. */
const showConfirm = computed(() => {
  if (!isActive.value || doneKind.value) return false
  if (isReview.value) return !!preview.value?.actions?.confirm
  return !!preview.value?.actions?.approve || !!preview.value?.actions?.confirm
})
const permissionPreset = computed(() => normalizePermissionPreset(preview.value?.permissionPreset))
const isReactOnly = computed(() => permissionPreset.value === 'react_only')
const showReactOnlyDeadend = computed(
  () => isActive.value && !doneKind.value && isReactOnly.value && !reactAlive.value,
)
const coldHintText = computed(() => {
  if (chatOnly.value) return t('pages.embedChat.coldHint')
  if (isReactOnly.value) return t('pages.publicGate.sessionEndedHintReactOnly')
  return isReview.value ? t('pages.publicGate.sessionEndedHint') : t('pages.publicGate.sessionEndedHintGate')
})
const presetChipLabel = computed(() =>
  isReactOnly.value
    ? t('pages.publicGate.presetChipReactOnly')
    : t('pages.publicGate.presetChipFull'),
)
/** Clickability: busy / in-flight / submitting / linkInvalid disable (align GateApproval). */
const confirmDisabled = computed(
  () =>
    submitting.value ||
    localChatBusy.value ||
    replyInFlight.value ||
    linkInvalid.value ||
    !showConfirm.value,
)
const showDecideFields = computed(() => showConfirm.value || canReject.value || linkInvalid.value)
const productKind = computed(() => preview.value?.productKind || inferProductKind())
const publicStageArtifacts = computed<Artifact[]>(() => publicArtifacts.value)
const publicPreviewName = computed(() => {
  const pin = preview.value?.productName || preview.value?.structured?.name || ''
  if (pin && publicStageArtifacts.value.some((a) => a.name === pin)) return pin
  return publicStageArtifacts.value[0]?.name || pin
})
const productName = computed(() => preview.value?.productName || preview.value?.structured?.name || '')
const inspectable = computed(
  () =>
    isActive.value &&
    reactAlive.value &&
    (isReview.value || productKind.value === 'visual' || productKind.value === 'app_preview'),
)
const usePublicArtifactStage = computed(() => isReview.value)
const appPreviewPorts = computed(() => preview.value?.ports || [])
const turns = computed<ClarifyTurn[]>(() =>
  (preview.value?.turns || []).map((turn) => ({
    role: turn.role === 'human' ? 'human' : 'agent',
    text: turn.text || '',
    at: turn.at || '',
    interrupted: !!turn.interrupted,
    annotations: (turn.annotations || []).map((a) => ({
      selector: a.selector,
      jsonPath: a.jsonPath,
      label: a.label,
      note: a.note,
      quote: a.quote,
    })),
  })),
)
const structuredDoc = computed(() => preview.value?.structured?.doc || structuredFallbackDoc())
const upstreamDoc = computed(() => upstreamDocFull.value)
const hasUpstream = computed(() => !!preview.value?.upstream)
const statusHint = computed(() => {
  if (status.value === 'expired') return t('pages.publicGate.expiredHint')
  if (status.value === 'used') return t('pages.publicGate.usedHint')
  if (status.value === 'revoked') return t('pages.publicGate.revokedHint')
  return t('pages.publicGate.invalidHint')
})
const productLabel = computed(() => {
  if (productKind.value === 'visual') return t('pages.publicGate.visualProduct')
  if (productKind.value === 'app_preview') return t('pages.publicGate.appPreviewProduct')
  if (productKind.value === 'structured') return t('pages.publicGate.structuredProduct')
  return t('pages.publicGate.structuredProduct')
})

provideReviewAnnotate({
  get enabled() {
    return inspectable.value
  },
  annotate(ann) {
    const next: ReactAnnotation = {
      selector: ann.selector,
      jsonPath: ann.jsonPath,
      label: ann.label,
      quote: ann.quote,
      truncated: ann.truncated,
    }
    if (annotations.value.some((a) => a.selector === next.selector && a.jsonPath === next.jsonPath && a.quote === next.quote)) {
      return
    }
    annotations.value = [...annotations.value, next]
  },
})

function inferProductKind(): string {
  if (preview.value?.visualHtml) return 'visual'
  if (preview.value?.structured) return 'structured'
  return ''
}

function structuredFallbackDoc(): Record<string, unknown> | null {
  const s = preview.value?.structured
  if (!s) return null
  if (s.doc) return s.doc
  const out: Record<string, unknown> = {}
  if (s.title) out.title = s.title
  if (s.description) out.description = s.description
  if (s.goals) out.goals = s.goals
  if (s.text) out.summary = s.text
  return Object.keys(out).length ? out : null
}

function refreshRemainingLabel() {
  const p = preview.value
  const next = formatRemainingSec(remainingSecFromExpiresAt(p?.expiresAt, p?.remainingSec), t)
  if (next !== remainingLabel.value) remainingLabel.value = next
}

function onLocaleSelect(next: AppLocale) {
  void setLocale(next)
}

function noteNonceIssued(nonce?: string) {
  if (nonce) nonceIssuedAt = Date.now()
}

function shouldRefreshNonce(): boolean {
  const have = !!(preview.value?.nonce || lastKeptNonce)
  if (!have) return true
  if (!nonceIssuedAt) return true
  return Date.now() - nonceIssuedAt >= NONCE_TTL_MS - NONCE_REFRESH_BEFORE_MS
}

function patchClockAndNonce(prev: PublicGatePreview, next: PublicGatePreview) {
  if (next.nonce) {
    prev.nonce = next.nonce
    lastKeptNonce = next.nonce
    noteNonceIssued(next.nonce)
  }
  if (next.expiresAt) prev.expiresAt = next.expiresAt
  if (typeof next.remainingSec === 'number') prev.remainingSec = next.remainingSec
  refreshRemainingLabel()
}

function applyPreviewPayload(next: PublicGatePreview, silent: boolean) {
  const prev = preview.value
  if (silent && prev) {
    const merged = mergePublicGatePreview(prev, next)
    if (publicGateContentKey(merged) === publicGateContentKey(prev)) {
      patchClockAndNonce(prev, next)
      noteIdlePoll(true)
      return false
    }
    preview.value = merged
  } else {
    preview.value = next
  }
  if (preview.value?.nonce) {
    lastKeptNonce = preview.value.nonce
    noteNonceIssued(preview.value.nonce)
  }
  refreshRemainingLabel()
  noteIdlePoll(false)
  return true
}

function abortArtifacts() {
  artifactsAbort?.abort()
  artifactsAbort = null
}

function readToken(): string {
  return props.embedToken || parseShareTokenFromHash(window.location.hash)
}

async function loadPublicArtifacts(opts?: { silent?: boolean }) {
  if (!isReview.value || !token.value || chatOnly.value) {
    publicArtifacts.value = []
    publicRunGraph.value = null
    return
  }
  if (!isActive.value) {
    publicArtifacts.value = []
    return
  }
  abortArtifacts()
  artifactsAbort = new AbortController()
  const signal = artifactsAbort.signal
  try {
    const res = await publicGateApi.artifacts(token.value, signal)
    if (signal.aborted) return
    if (res.status !== 'active') {
      if (!opts?.silent) {
        publicArtifacts.value = []
      }
      return
    }
    publicArtifacts.value = (res.artifacts || []).map((a) => ({
      id: a.id,
      name: a.name,
      kind: a.kind,
      nodeId: a.nodeId,
      runId: PUBLIC_SHARE_RUN_ID,
      workflowName: '',
      sizeBytes: a.sizeBytes,
      createdAt: a.createdAt,
      updatedAt: a.updatedAt,
      revision: a.revision,
    }))
    if (res.nodes?.length) {
      publicRunGraph.value = {
        id: PUBLIC_SHARE_RUN_ID,
        workflowId: '',
        workflowName: '',
        status: 'waiting_human',
        trigger: '',
        startedAt: '',
        durationSec: 0,
        progress: 0,
        branch: '',
        nodeRuns: {},
        nodes: res.nodes.map((n) => ({
          id: n.id,
          type: n.type as NodeType,
          label: n.label || n.id,
          position: { x: 0, y: 0 },
          config: n.config || {},
        })),
        edges: [],
        artifacts: publicArtifacts.value,
        vars: [],
        trace: [],
        priority: 'normal',
        tags: [],
      }
    } else {
      publicRunGraph.value = null
    }
  } catch (e) {
    if (isAbortError(e) || signal.aborted) return
    if (!opts?.silent) {
      publicArtifacts.value = []
      publicRunGraph.value = null
    }
  }
}

async function loadPreview(opts?: { silent?: boolean; issueNonce?: boolean }) {
  if (doneKind.value) return
  const attemptGen = ++previewGen
  abortPreview()
  previewAbort = new AbortController()
  const signal = previewAbort.signal

  if (!opts?.silent) {
    preview.value = null
    loading.value = true
    errorText.value = ''
    networkFailed.value = false
    workbenchSeen.value = false
    linkInvalid.value = false
    stuckTimer = window.setTimeout(() => {
      if (attemptGen === previewGen) maybeStuck.value = true
    }, 10_000)
  }
  const tok = readToken()
  token.value = tok
  if (!tok) {
    if (attemptGen !== previewGen) return
    preview.value = { status: 'invalid' }
    loading.value = false
    clearStuckTimer()
    refreshRemainingLabel()
    return
  }
  try {
    const known: PublicGatePreviewKnown | undefined =
      opts?.silent && preview.value
        ? {
            visualHtmlHash: preview.value.visualHtmlHash,
            upstreamHash: preview.value.upstreamHash,
            structuredHash: preview.value.structuredHash,
            turnsHash: preview.value.turnsHash,
            silent: true,
            issueNonce: !!opts.issueNonce || shouldRefreshNonce(),
          }
        : opts?.issueNonce
          ? { issueNonce: true }
          : undefined
    const next = await publicGateApi.preview(tok, signal, known)
    if (attemptGen !== previewGen) return
    const wroteTree = applyPreviewPayload(next, !!opts?.silent)
    if (next.status === 'active') workbenchSeen.value = true
    noteWorkbenchLinkInvalid(preview.value)
    if (!opts?.silent && !linkInvalid.value) errorText.value = ''
    if (!wroteTree) {
      if (attemptGen === previewGen) {
        loading.value = false
        clearStuckTimer()
      }
      // Content unchanged (often already idle) must still reconcile local sticky busy.
      if (attemptGen === previewGen) {
        await nextTick()
        syncChatQueueFromPreview()
        await loadPublicArtifacts({ silent: true })
      }
      return
    }
  } catch (e) {
    if (attemptGen !== previewGen || isAbortError(e)) return
    if (!opts?.silent) {
      preview.value = null
      networkFailed.value = true
      errorText.value = t('pages.publicGate.networkError')
    } else {
      noteIdlePoll(true)
    }
  } finally {
    if (attemptGen === previewGen) {
      loading.value = false
      clearStuckTimer()
    }
  }
  if (attemptGen !== previewGen) return
  await nextTick()
  syncChatQueueFromPreview()
  await loadPublicArtifacts({ silent: !!opts?.silent })
}

function abortUpstream() {
  upstreamAbort?.abort()
  upstreamAbort = null
}

async function loadUpstreamFull() {
  const tok = token.value || readToken()
  if (!tok || !hasUpstream.value) return
  if (upstreamLoaded.value || upstreamLoading.value) return
  abortUpstream()
  upstreamAbort = new AbortController()
  const signal = upstreamAbort.signal
  upstreamLoading.value = true
  upstreamLoadErr.value = ''
  try {
    const res = await publicGateApi.upstream(tok, signal)
    if (signal.aborted) return
    const doc = res.upstream?.doc
    if (doc && typeof doc === 'object') {
      upstreamDocFull.value = doc as Record<string, unknown>
    } else {
      // Fallback to summary fields when doc absent / truncated path.
      const u = res.upstream || preview.value?.upstream
      const fallback: Record<string, unknown> = {}
      if (u?.title) fallback.title = u.title
      if (u?.summary) fallback.summary = u.summary
      if (u?.description) fallback.description = u.description
      if (u?.text) fallback.summary = u.text
      upstreamDocFull.value = Object.keys(fallback).length ? fallback : null
    }
    upstreamLoaded.value = true
  } catch (e) {
    if (isAbortError(e) || signal.aborted) return
    upstreamLoadErr.value = e instanceof Error ? e.message : String(e || '')
    upstreamDocFull.value = null
  } finally {
    if (!signal.aborted) upstreamLoading.value = false
  }
}

function retryUpstreamLoad() {
  upstreamLoaded.value = false
  upstreamLoadErr.value = ''
  upstreamDocFull.value = null
  void loadUpstreamFull()
}

function openUpstreamModal() {
  upstreamOpen.value = true
  void loadUpstreamFull()
}

function turnsIncludeHumanText(text: string): boolean {
  const want = text.trim()
  if (!want) return false
  return turns.value.some((turn) => turn.role === 'human' && (turn.text || '').trim() === want)
}

function syncChatQueueFromPreview() {
  const chat = chatRef.value
  const p = preview.value
  if (!chat || !p || !isActive.value) return
  const waiting = typeof p.waiting === 'number' ? p.waiting : 0
  const items = p.queueItems || []
  const activeItem = p.activeItem || null
  // sessionBusy/waiting are authoritative; do not let a stale merged activeItem
  // keep the chat sticky-busy after the server has gone idle.
  const busy = !!p.sessionBusy || waiting > 0

  if (busy) {
    lastAppliedIdleQueue = false
    chat.applyQueueState?.(waiting, items.length ? items : null, true, activeItem)
    if (pendingReplyText.value && (turnsIncludeHumanText(pendingReplyText.value) || activeItem?.text?.trim() === pendingReplyText.value)) {
      pendingReplyText.value = ''
    }
  } else if (!replyInFlight.value && !(pendingReplyText.value && !turnsIncludeHumanText(pendingReplyText.value))) {
    if (pendingReplyText.value && turnsIncludeHumanText(pendingReplyText.value)) {
      chat.discardLastQueued?.()
      pendingReplyText.value = ''
    }
    // Re-apply when local still sticky-busy even if we already applied idle once.
    if (!(lastAppliedIdleQueue && !chat.isSessionBusy?.())) {
      lastAppliedIdleQueue = true
      chat.applyQueueState?.(0, [], false, null)
    }
  }
  applyPreviewLiveEvents()
  flushPendingPublicAcp()
  maybeStartPublicBusySeedRetry()
  refreshLocalChatBusy()
}

function toAcpEvents(
  events: { kind?: string; text?: string }[] | undefined,
): AcpEvent[] {
  if (!events?.length) return []
  return events
    .filter((e) => (e.kind === 'message' || e.kind === 'thought') && e.text)
    .map((e) => ({ t: 0, kind: e.kind as AcpEvent['kind'], text: e.text }))
}

let pendingPublicAcp: AcpEvent[] | null = null
let publicWs: WebSocket | undefined
/** Thought/message rails applied via seed or live — stops busy seed retry. */
let publicRailsFilled = false
let publicLiveIncremental = false
const publicBusySeedRetry = createBusySeedRetryController()

function markPublicRailsFromEvents(events: AcpEvent[] | undefined) {
  const rails = pickAcpRails(events || [])
  if (rails.thought || rails.message) {
    publicRailsFilled = true
  }
}

function publicSessionBusy(): boolean {
  const p = preview.value
  if (!p) return false
  const waiting = typeof p.waiting === 'number' ? p.waiting : 0
  return !!p.sessionBusy || waiting > 0 || !!chatRef.value?.isSessionBusy?.()
}

function startPublicBusySeedRetry() {
  publicBusySeedRetry.start(async (signal) => {
    await runBusySeedRetry({
      signal,
      isBusy: () => publicSessionBusy(),
      hasContent: () => publicRailsFilled,
      liveIncrementalReceived: () => publicLiveIncremental,
      seed: async () => {
        await loadPreview({ silent: true })
        const events = toAcpEvents(preview.value?.liveEvents)
        if (!events.length) return publicRailsFilled
        const applied = deliverPublicAcp(events)
        if (applied) markPublicRailsFromEvents(events)
        return publicRailsFilled
      },
    })
  })
}

/** Start busy-guarded seed when session busy and rails still empty (g2.2). */
function maybeStartPublicBusySeedRetry() {
  if (!publicSessionBusy()) {
    publicBusySeedRetry.stop()
    return
  }
  if (publicRailsFilled || publicLiveIncremental) {
    publicBusySeedRetry.stop()
    return
  }
  // Already looping — do not reset from silent poll / sync re-entry.
  if (!publicBusySeedRetry.aborted) return
  startPublicBusySeedRetry()
}

function deliverPublicAcp(events: AcpEvent[] | undefined): boolean {
  if (!events?.length) return true
  const chat = chatRef.value
  if (!chat?.applyAcpEvents) {
    pendingPublicAcp = events
    return false
  }
  if (chat.applyAcpEvents(events) === false) {
    pendingPublicAcp = events
    return false
  }
  pendingPublicAcp = null
  markPublicRailsFromEvents(events)
  return true
}

function flushPendingPublicAcp() {
  if (pendingPublicAcp) deliverPublicAcp(pendingPublicAcp)
}

function applyPreviewLiveEvents() {
  deliverPublicAcp(toAcpEvents(preview.value?.liveEvents))
}

function handlePublicWsMessage(raw: string) {
  let m: Record<string, unknown>
  try {
    m = JSON.parse(raw) as Record<string, unknown>
  } catch {
    return
  }
  const typ = String(m.type || '')
  if (typ === 'ready' || typ === 'error') return
  if (typ === 'review') {
    chatRef.value?.applyReviewFrame?.(m)
    flushPendingPublicAcp()
    refreshLocalChatBusy()
    return
  }
  if (typ === 'acp') {
    const events = Array.isArray(m.events) ? (m.events as AcpEvent[]) : []
    // ACP busy (if present) must not stop publicBusySeedRetry — platform sessionBusy is authority (g1.2).
    if (deliverPublicAcp(events)) {
      const rails = pickAcpRails(events)
      if (rails.thought || rails.message) publicLiveIncremental = true
    }
    refreshLocalChatBusy()
  }
}

const publicWsReconnect = createWsReconnectController({
  connect: () => connectPublicEvents(),
  shouldReconnect: () => isActive.value && !doneKind.value && !linkInvalid.value && !!token.value,
})

function connectPublicEvents() {
  if (!isActive.value || doneKind.value || linkInvalid.value || !token.value) return
  const prev = publicWs
  if (prev) {
    publicWsReconnect.markIntentionalClose()
    prev.close()
    if (publicWs === prev) publicWs = undefined
  }
  let socket: WebSocket
  try {
    socket = new WebSocket(publicGateApi.eventsWsUrl())
    publicWs = socket
  } catch {
    publicWsReconnect.onClose()
    return
  }
  socket.onopen = () => {
    if (publicWs !== socket) return
    publicWsReconnect.markOpened()
    try {
      socket.send(JSON.stringify({ token: token.value }))
    } catch {
      socket.close()
    }
  }
  socket.onmessage = (ev) => {
    if (publicWs !== socket) return
    handlePublicWsMessage(String(ev.data || ''))
  }
  socket.onclose = () => {
    if (publicWs !== socket) return
    publicWs = undefined
    publicWsReconnect.onClose()
  }
}

function stopPublicEvents() {
  publicBusySeedRetry.stop()
  publicWsReconnect.markIntentionalClose()
  publicWs?.close()
  publicWs = undefined
  pendingPublicAcp = null
  publicRailsFilled = false
  publicLiveIncremental = false
}

function clearHash() {
  try {
    history.replaceState(null, '', `${window.location.pathname}${window.location.search}`)
  } catch {
    // ignore
  }
}

function auditReady(): boolean {
  if (isReview.value) return true
  if (!reviewerName.value.trim() || !comment.value.trim()) {
    errorText.value = t('pages.publicGate.auditRequired')
    return false
  }
  return true
}

function onComposerFinish() {
  if (!isReview.value && !auditReady()) {
    chatRef.value?.applyQueueState?.(0, [], false, null)
    return
  }
  void submitFinal('confirm')
}

type DecideFailure = {
  status?: number
  body?: { error?: string; status?: string; message?: string }
  message?: string
  name?: string
}

function decideFailureOf(e: unknown): DecideFailure {
  if (!e || typeof e !== 'object') return { message: String(e || '') }
  const err = e as DecideFailure & { message?: string; name?: string }
  return {
    status: err.status,
    body: err.body,
    message: err.message || '',
    name: err.name,
  }
}

function isNonceError(e: unknown): boolean {
  return String(decideFailureOf(e).body?.error || '').toLowerCase() === 'nonce'
}

function isLinkInvalidStatus(status?: string, error?: string): boolean {
  const s = String(status || '').toLowerCase()
  const err = String(error || '').toLowerCase()
  if (['invalid', 'expired', 'revoked', 'used'].includes(s)) return true
  if (err === 'conflict' || ['invalid', 'expired', 'revoked', 'used'].includes(err)) return true
  return false
}

function noteWorkbenchLinkInvalid(p?: PublicGatePreview | null) {
  if (!workbenchSeen.value || doneKind.value || !p) return
  const expiredRemain = typeof p.remainingSec === 'number' && p.remainingSec <= 0
  if ((p.status && p.status !== 'active') || expiredRemain) {
    linkInvalid.value = true
    errorText.value = t('pages.publicGate.linkInvalid')
  }
}

function mapDecideFootnote(e: unknown): { key: string; linkInvalid: boolean } {
  const f = decideFailureOf(e)
  const code = String(f.body?.error || '').toLowerCase()
  const st = String(f.body?.status || '').toLowerCase()
  const msg = String(f.message || '')
  if (f.status === 429 || code === 'rate_limited') {
    return { key: 'pages.publicGate.rateLimited', linkInvalid: false }
  }
  if (f.status === 409 || isLinkInvalidStatus(st, code)) {
    return { key: 'pages.publicGate.linkInvalid', linkInvalid: true }
  }
  const networkLike =
    (typeof f.status === 'number' && f.status >= 500) ||
    f.name === 'TypeError' ||
    /failed to fetch|networkerror|network request failed|timeout|timed out/i.test(msg)
  if (networkLike) {
    return { key: 'pages.publicGate.networkFault', linkInvalid: false }
  }
  return { key: 'pages.publicGate.securityCheckFailed', linkInvalid: false }
}

function markLinkInvalid(status?: string) {
  linkInvalid.value = true
  errorText.value = t('pages.publicGate.linkInvalid')
  if (preview.value && status) {
    preview.value = { ...preview.value, status }
  }
}

async function applyDecideResult(kind: 'confirm' | 'reject', res: PublicGateDecideResult) {
  if (res.status === 'confirmed' || (res.alreadyProcessed && kind === 'confirm' && isReview.value)) {
    // Overlay already started on click; do not wait for decide success to play (g1.1).
    doneKind.value = 'confirmed'
    clearHash()
    return
  }
  if (res.status === 'approved' || (res.alreadyProcessed && kind === 'confirm' && !isReview.value)) {
    doneKind.value = 'approved'
    clearHash()
    return
  }
  if (res.status === 'rejected' || (res.alreadyProcessed && kind === 'reject')) {
    doneKind.value = 'rejected'
    clearHash()
    return
  }
  if (res.status === 'busy' || res.error === 'review_busy') {
    errorText.value = t('pages.publicGate.busy')
    await loadPreview({ silent: true })
    return
  }
  if (res.status === 'validation_failed' || res.error === 'review_validation_failed') {
    errorText.value = isClarify.value
      ? t('pages.publicGate.clarifyNotFinished')
      : t('pages.publicGate.validationFailed')
    await loadPreview({ silent: true })
    return
  }
  if (isLinkInvalidStatus(res.status, res.error) || res.error === 'conflict') {
    markLinkInvalid(res.status || 'used')
    return
  }
  errorText.value = t('pages.publicGate.securityCheckFailed')
}

async function decideOnce(
  kind: 'confirm' | 'reject',
  nonce: string,
  signal: AbortSignal,
): Promise<PublicGateDecideResult> {
  const action =
    kind === 'reject'
      ? preview.value?.actions?.reject
      : preview.value?.actions?.confirm || preview.value?.actions?.approve || 'confirm'
  return publicGateApi.decide(
    {
      token: token.value,
      action: action || 'confirm',
      comment: isReview.value ? undefined : comment.value,
      name: isReview.value ? undefined : reviewerName.value,
      nonce,
    },
    signal,
  )
}

async function submitFinal(kind: 'confirm' | 'reject') {
  if (!preview.value || submitting.value || (linkInvalid.value && kind === 'confirm')) return
  if (localChatBusy.value && kind === 'confirm') {
    errorText.value = t('pages.publicGate.busy')
    return
  }
  if (!auditReady() && kind === 'reject') return
  if (!isReview.value && !auditReady()) return
  if (!(preview.value.nonce || lastKeptNonce)) {
    errorText.value = t('pages.publicGate.unavailable')
    return
  }
  const action =
    kind === 'reject'
      ? preview.value.actions?.reject
      : preview.value.actions?.confirm || preview.value.actions?.approve || 'confirm'
  if (!action) return
  submitting.value = true
  pendingKind.value = kind
  errorText.value = ''
  // Click intent: play overlay before decide HTTP (plan g1.1); not after node_complete.
  if (kind === 'confirm') void playConfirmFlowCeremony(shellRef.value ?? chatRef.value)
  stopPoll()
  abortPreview()
  decideAbort?.abort()
  decideAbort = new AbortController()
  const signal = decideAbort.signal
  try {
    let res: PublicGateDecideResult
    try {
      res = await decideOnce(kind, preview.value.nonce || lastKeptNonce, signal)
    } catch (e) {
      if (isAbortError(e)) return
      if (!isNonceError(e)) throw e
      await loadPreview({ silent: true, issueNonce: true })
      if (signal.aborted) return
      const nextNonce = preview.value?.nonce || lastKeptNonce
      if (!nextNonce) throw e
      res = await decideOnce(kind, nextNonce, signal)
    }
    await applyDecideResult(kind, res)
  } catch (e) {
    if (isAbortError(e)) return
    const mapped = mapDecideFootnote(e)
    errorText.value = t(mapped.key)
    if (mapped.linkInvalid) {
      linkInvalid.value = true
    }
  } finally {
    submitting.value = false
    pendingKind.value = null
    if (isActive.value && !doneKind.value && !linkInvalid.value) startPoll()
  }
}

async function onSend(text: string, images: ClarifyImage[], anns: ReactAnnotation[]) {
  errorText.value = ''
  refreshLocalChatBusy()
  replyInFlight.value = true
  pendingReplyText.value = text.trim()
  try {
    await publicGateApi.reply({
      token: token.value,
      text,
      annotations: anns,
      images: images.map((im) => ({ data: im.data, mimeType: im.mimeType, name: im.name })),
    })
    await loadPreview({ silent: true })
  } catch (e) {
    chatRef.value?.discardLastQueued?.()
    pendingReplyText.value = ''
    errorText.value = e instanceof Error ? e.message : t('pages.publicGate.replyFailed')
  } finally {
    replyInFlight.value = false
    await nextTick()
    syncChatQueueFromPreview()
  }
}

async function onCancel() {
  errorText.value = ''
  try {
    await publicGateApi.cancel(token.value)
    await loadPreview({ silent: true })
  } catch (e) {
    errorText.value = e instanceof Error ? e.message : t('pages.publicGate.cancelFailed')
  }
}

async function onQueueRemove(itemId: string | undefined) {
  if (!itemId) return
  try {
    await publicGateApi.queueRemove(token.value, itemId)
    await loadPreview({ silent: true })
  } catch (e) {
    errorText.value = e instanceof Error ? e.message : t('pages.publicGate.replyFailed')
  }
}

async function onQueueReorder(itemIds: string[]) {
  if (!itemIds.length) return
  try {
    await publicGateApi.queueReorder(token.value, itemIds)
    await loadPreview({ silent: true })
  } catch (e) {
    errorText.value = e instanceof Error ? e.message : t('pages.publicGate.replyFailed')
  }
}

function onHtmlPick(payload: { selector: string; tagName: string }) {
  if (!inspectable.value) return
  const next: ReactAnnotation = { selector: payload.selector, label: payload.selector || payload.tagName }
  if (annotations.value.some((a) => a.selector === next.selector)) return
  annotations.value = [...annotations.value, next]
}

function onAppPreviewPick(payload: AppPreviewPickPayload) {
  if (!isActive.value || !reactAlive.value) return
  const next: ReactAnnotation = previewPickAnnotation(payload)
  if (annotations.value.some((a) => a.selector === next.selector && a.label === next.label)) return
  annotations.value = [...annotations.value, next]
}

let pollTimer: ReturnType<typeof setInterval> | null = null
function pageHidden(): boolean {
  return typeof document !== 'undefined' && document.visibilityState === 'hidden'
}
function canPoll(): boolean {
  return (
    isActive.value &&
    !doneKind.value &&
    !submitting.value &&
    !linkInvalid.value &&
    !pageHidden()
  )
}
function onHashChange() {
  if (chatOnly.value || doneKind.value || submitting.value) return
  void loadPreview()
}
function noteIdlePoll(sameContent: boolean) {
  const next = sameContent ? IDLE_POLL_MS : POLL_MS
  if (next === pollIntervalMs) return
  pollIntervalMs = next
  if (pollTimer && canPoll()) startPoll()
}
function startPoll() {
  stopPoll()
  if (!canPoll()) return
  pollTimer = setInterval(() => {
    if (!canPoll() || !token.value) return
    void loadPreview({ silent: true })
  }, pollIntervalMs)
}
function stopPoll() {
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}
function startRemainingTick() {
  stopRemainingTick()
  refreshRemainingLabel()
  if (pageHidden() || !isActive.value || doneKind.value) return
  remainingTimer = setInterval(refreshRemainingLabel, REMAINING_TICK_MS)
}
function stopRemainingTick() {
  if (remainingTimer) {
    clearInterval(remainingTimer)
    remainingTimer = null
  }
}
async function resumeFromForeground() {
  if (!canPoll()) {
    startRemainingTick()
    return
  }
  startRemainingTick()
  // Same depth as hard load: re-seed liveEvents under busy guard (g2.2).
  publicRailsFilled = false
  publicLiveIncremental = false
  publicBusySeedRetry.stop()
  await loadPreview({ silent: true, issueNonce: true })
  if (canPoll()) startPoll()
}
function onVisibilityChange() {
  if (pageHidden()) {
    stopPoll()
    stopRemainingTick()
    return
  }
  void resumeFromForeground()
}

watch([isActive, doneKind], () => {
  if (canPoll()) {
    startPoll()
    connectPublicEvents()
  } else {
    stopPoll()
    stopPublicEvents()
  }
  if (isActive.value && !doneKind.value && !pageHidden()) startRemainingTick()
  else stopRemainingTick()
})

watch(token, (tok, prev) => {
  if (tok && tok !== prev && isActive.value && !doneKind.value && !linkInvalid.value) connectPublicEvents()
})

watch(chatRef, (chat) => {
  if (chat) syncChatQueueFromPreview()
})

// When silent poll brings a new upstream summary, drop cached full doc.
watch(
  () => preview.value?.upstreamHash,
  (hash, prev) => {
    if (prev != null && hash !== prev) {
      upstreamLoaded.value = false
      upstreamDocFull.value = null
      upstreamLoadErr.value = ''
      if (upstreamOpen.value) void loadUpstreamFull()
    }
  },
)

onMounted(async () => {
  await applyPublicLocale()
  ready.value = true
  await loadPreview()
  window.addEventListener('hashchange', onHashChange)
  document.addEventListener('visibilitychange', onVisibilityChange)
  startRemainingTick()
  if (canPoll()) startPoll()
  connectPublicEvents()
})
onUnmounted(() => {
  stopPoll()
  stopRemainingTick()
  stopPublicEvents()
  window.removeEventListener('hashchange', onHashChange)
  document.removeEventListener('visibilitychange', onVisibilityChange)
  abortPreview()
  abortUpstream()
  abortArtifacts()
  decideAbort?.abort()
  decideAbort = null
  reapplyThemeChrome()
})

watch(
  () => preview.value?.status,
  (next) => {
    if (next) emit('status', next)
  },
)

defineExpose({ loadPreview, loadUpstreamFull, openUpstreamModal, addPick: onAppPreviewPick })
</script>

<template>
  <div
    class="flex h-screen flex-col overflow-hidden bg-base text-txt"
    data-testid="public-gate-root"
    :aria-busy="(!ready || loading || submitting) ? 'true' : 'false'"
  >
    <header
      v-if="!chatOnly"
      class="flex shrink-0 items-center justify-between border-b border-line bg-surface text-txt"
      :class="isMobile ? 'px-3 py-1.5' : 'px-4 py-2'"
      data-testid="public-gate-chrome"
    >
      <div class="flex items-center gap-2">
        <span
          class="rounded-md border border-line bg-elevated px-2 py-0.5 text-[11px] text-txt2"
          data-testid="public-gate-badge"
        >
          {{
            isClarify
              ? t('pages.publicGate.badgeClarify')
              : isReview
                ? t('pages.publicGate.badgeReview')
                : t('pages.publicGate.badge')
          }}
        </span>
        <span
          v-if="isClarify && isActive && !doneKind"
          class="text-[11px] text-txt3"
          data-testid="public-gate-kind-hint"
        >
          {{ t('pages.publicGate.kindHintClarify') }}
        </span>
        <span
          v-if="isActive && !doneKind && !reactAlive"
          class="text-[11px] text-txt3"
          data-testid="public-gate-session-ended"
        >
          {{ t('pages.publicGate.sessionEnded') }}
        </span>
        <span
          v-if="isActive && !doneKind"
          class="rounded-md border border-accent/45 bg-accent/10 px-2 py-0.5 text-[11px] text-accent-2"
          data-testid="public-gate-preset-chip"
        >
          {{ presetChipLabel }}
        </span>
      </div>
      <div class="flex shrink-0 items-center gap-2">
        <span v-if="isActive && !doneKind" class="text-[12px] text-txt3" data-testid="public-gate-remaining">
          {{ t('pages.publicGate.remaining', { remaining: remainingLabel }) }}
        </span>
        <LangSelect
          :model-value="locale"
          data-testid="public-gate-lang-select"
          @update:model-value="onLocaleSelect"
        />
      </div>
    </header>

    <div
      v-if="!ready || loading"
      class="flex flex-1 flex-col items-center justify-center gap-3 py-16 text-center"
      role="status"
      aria-busy="true"
      data-testid="public-gate-loading"
    >
      <Icon name="spinner" :size="28" class="animate-spin text-accent" aria-hidden="true" />
      <p class="text-sm text-txt3">{{ t('pages.publicGate.loading') }}</p>
      <p v-if="maybeStuck" class="max-w-[40ch] text-xs text-txt3" data-testid="public-gate-maybe-stuck">
        {{ t('pages.publicGate.maybeStuck') }}
      </p>
    </div>

    <div
      v-else-if="networkFailed"
      class="flex flex-1 flex-col items-center justify-center gap-3 py-16 text-center"
      data-testid="public-gate-network-error"
      role="alert"
    >
      <Icon name="alert" :size="28" class="text-warn" />
      <h1 class="text-lg font-semibold">{{ t('pages.publicGate.networkError') }}</h1>
      <button
        type="button"
        class="rounded-lg inline-flex min-h-11 items-center justify-center border border-line bg-surface px-4 text-sm font-medium text-txt"
        data-testid="public-gate-network-retry"
        @click="loadPreview()"
      >
        {{ t('common.buttons.retry') }}
      </button>
    </div>

    <div
      v-else-if="doneKind"
      class="flex flex-1 flex-col items-center justify-center gap-3 py-16 text-center"
      data-testid="public-gate-done"
    >
      <Icon :name="doneKind === 'rejected' ? 'alert' : 'check'" :size="28" :class="doneKind === 'rejected' ? 'text-warn' : 'text-ok'" />
      <h1 class="text-lg font-semibold">
        {{
          doneKind === 'confirmed'
            ? t('pages.publicGate.doneConfirmed')
            : doneKind === 'approved'
              ? t('pages.publicGate.doneApproved')
              : t('pages.publicGate.doneRejected')
        }}
      </h1>
      <p class="text-sm text-txt3">{{ t('pages.publicGate.doneHint') }}</p>
    </div>

    <div
      v-else-if="!isActive && !workbenchSeen"
      class="flex flex-1 flex-col items-center justify-center gap-2 py-16 text-center"
      data-testid="public-gate-invalid"
      role="status"
    >
      <Icon name="alert" :size="28" class="text-warn" />
      <h1 class="text-lg font-semibold">
        {{
          status === 'expired'
            ? t('pages.publicGate.expired')
            : status === 'used'
              ? t('pages.publicGate.used')
              : status === 'revoked'
                ? t('pages.publicGate.revoked')
                : t('pages.publicGate.invalid')
        }}
      </h1>
      <p class="max-w-[40ch] text-sm text-txt3">{{ statusHint }}</p>
    </div>

    <div v-else class="flex min-h-0 flex-1 flex-col" data-testid="public-gate-workbench">
      <ReviewShell
        ref="shellRef"
        class="min-h-0 flex-1"
        :chat-only="chatOnly"
        :mobile="isMobile"
        :sidebar-width="400"
        :storage-key="REVIEW_SHELL_WIDTH_KEY_APPROVAL"
      >
        <template #stage>
          <div
            v-if="usePublicArtifactStage"
            class="flex min-h-0 flex-1 flex-col"
            data-testid="public-gate-react-stage"
          >
            <ReactArtifactStage
              :artifacts="publicStageArtifacts"
              :preview-artifact="publicPreviewName"
              :run="publicRunGraph"
              :node-type="preview?.nodeType"
              :annotatable="inspectable"
              :remote-kind="productKind === 'app_preview' || appPreviewPorts.length ? 'public' : 'off'"
              :token="token"
              :ports="appPreviewPorts"
              :public-active="isActive"
              :public-mobile="isMobile"
              @pick="onAppPreviewPick"
            />
          </div>
          <section v-else class="flex min-h-0 flex-1 flex-col" data-testid="public-gate-stage">
            <div
              class="flex shrink-0 items-baseline gap-2 border-b border-line"
              :class="isMobile ? 'px-3 py-1.5' : 'px-4 py-2'"
            >
              <h2 class="text-sm font-semibold" data-testid="public-gate-product-label">{{ productLabel }}</h2>
              <span v-if="productName" class="text-[11px] text-txt3" data-testid="public-gate-product-name">{{ productName }}</span>
            </div>
            <div class="min-h-0 flex-1 overflow-hidden">
              <div v-if="preview?.visualHtml" class="h-full min-h-[200px]" data-testid="public-gate-visual">
                <HtmlPreview
                  :html="preview.visualHtml"
                  mode="inline"
                  fill-parent
                  :enlargeable="false"
                  :inspectable="inspectable"
                  @pick="onHtmlPick"
                />
              </div>
              <PublicAppPreviewPanel
                v-else-if="productKind === 'app_preview'"
                :token="token"
                :ports="appPreviewPorts"
                :active="isActive"
                :mobile="isMobile"
                fill
                @pick="onAppPreviewPick"
              />
              <div
                v-else-if="structuredDoc"
                class="scroll-area h-full overflow-y-auto px-4 py-3"
                data-testid="public-gate-structured"
              >
                <StructuredArtifactView
                  :name="productName || preview?.structured?.name || 'research.json'"
                  :doc="structuredDoc"
                />
              </div>
              <div v-else class="flex h-full items-center justify-center text-sm text-txt3" data-testid="public-gate-empty-product">
                {{ t('pages.publicGate.emptyProduct') }}
              </div>
            </div>
          </section>
          <div
            class="flex shrink-0 items-center justify-between gap-3 border-t border-line bg-elevated px-3 py-2 text-xs text-txt2"
            data-testid="public-gate-upstream"
          >
            <div class="min-w-0 truncate">
              <template v-if="hasUpstream">
                <span class="font-medium text-txt">{{ t('pages.gateApproval.upstreamContext') }}</span>
                <span class="text-txt3"> · {{ preview?.upstream?.summary || preview?.upstream?.title || t('pages.gateApproval.upstreamBarHint') }}</span>
              </template>
              <span v-else class="text-txt3">{{ t('pages.publicGate.upstreamEmpty') }}</span>
            </div>
            <button
              v-if="hasUpstream"
              type="button"
              class="rounded-md inline-flex shrink-0 items-center gap-1.5 bg-accent px-2.5 py-1 text-[11px] font-medium text-white hover:bg-accent-hover"
              data-testid="public-gate-upstream-enlarge"
              @click="openUpstreamModal"
            >
              <Icon name="expand" :size="14" />
              {{ t('pages.gateApproval.upstreamEnlarge') }}
            </button>
          </div>
        </template>
        <template #sidebar>
          <div class="flex h-full min-h-0 flex-col" data-testid="public-gate-sidebar">
            <div
              v-if="showReactOnlyDeadend"
              class="rounded-lg flex flex-1 flex-col items-center justify-center gap-2 border border-dashed border-line-strong bg-elevated px-6 py-10 text-center"
              data-testid="public-gate-react-only-deadend"
              role="status"
            >
              <h2 class="text-sm font-semibold text-txt">{{ t('pages.publicGate.reactOnlyDeadendTitle') }}</h2>
              <p class="max-w-[42ch] text-xs text-txt2">{{ t('pages.publicGate.reactOnlyDeadendBody') }}</p>
            </div>
            <template v-else>
              <p
                v-if="!reactAlive"
                class="shrink-0 border-b border-line text-[11px] text-txt3"
                :class="isMobile ? 'px-3 py-1.5' : 'px-4 py-2'"
                data-testid="public-gate-cold-hint"
              >
                {{ coldHintText }}
              </p>
              <div class="flex min-h-0 flex-1 flex-col" data-testid="public-gate-chat-host">
                <ReviewComposer
                  ref="chatRef"
                  :mode="composerMode"
                  run-id="public-share"
                  node-id="public-gate"
                  :iteration="1"
                  v-model:draft="draft"
                  v-model:attachments="attachments"
                  v-model:annotations="annotations"
                  :turns="turns"
                  :node-type="preview?.nodeType"
                  :done="false"
                  :active="canReply"
                  :cold-session="!chatOnly && !canReply"
                  :can-pass="!chatOnly && (showConfirm || linkInvalid)"
                  :pass-disabled="confirmDisabled"
                  :force-confirm="!chatOnly && (showConfirm || linkInvalid)"
                  :confirm-error="errorText || null"
                  @send="onSend"
                  @finish="onComposerFinish"
                  @cancel="onCancel"
                  @queue-remove="(itemId) => onQueueRemove(itemId)"
                  @queue-reorder="onQueueReorder"
                />
              </div>
            </template>
          </div>
        </template>
      </ReviewShell>

      <footer
        v-if="!chatOnly && !isReview && (showDecideFields || canReject)"
        class="flex shrink-0 flex-col gap-2 border-t border-line bg-surface md:flex-row md:items-center"
        :class="isMobile ? 'px-3 py-2' : 'px-4 py-2.5'"
        data-testid="public-gate-footer"
      >
        <div class="flex min-w-0 flex-1 flex-wrap items-center justify-end gap-2">
          <template v-if="!isReview && showDecideFields">
            <input
              v-model="reviewerName"
              type="text"
              maxlength="80"
              class="rounded-md w-[8rem] border border-line bg-elevated px-2 py-1 text-xs text-txt"
              data-testid="public-gate-name"
              :placeholder="t('pages.publicGate.namePh')"
              autocomplete="name"
            />
            <input
              v-model="comment"
              type="text"
              maxlength="4000"
              class="rounded-md min-w-[10rem] flex-1 border border-line bg-elevated px-2 py-1 text-xs text-txt md:w-[16rem] md:flex-none"
              data-testid="public-gate-comment"
              :placeholder="t('pages.publicGate.commentPh')"
            />
          </template>
          <button
            v-if="canReject"
            type="button"
            class="inline-flex min-h-9 items-center gap-2 bg-transparent px-2 text-sm text-txt2 underline underline-offset-4 hover:text-txt disabled:opacity-45"
            data-testid="public-gate-reject"
            :disabled="submitting"
            :aria-busy="submitting ? 'true' : 'false'"
            :aria-label="t('pages.publicGate.rejectAria')"
            @click="submitFinal('reject')"
          >
            <Icon v-if="submitting" name="spinner" :size="16" class="animate-spin" aria-hidden="true" />
            {{ submitting ? t('pages.publicGate.submitting') : t('pages.publicGate.reject') }}
          </button>
        </div>
      </footer>
    </div>

    <AppModal
      :open="upstreamOpen"
      :title="t('pages.gateApproval.upstreamContext')"
      :width="720"
      close-on-esc
      data-testid="public-gate-upstream-modal"
      @close="upstreamOpen = false"
    >
      <div v-if="upstreamLoading" class="px-4 py-6 text-sm text-txt2" data-testid="public-gate-upstream-loading">
        {{ t('pages.publicGate.loading') }}
      </div>
      <div
        v-else-if="upstreamLoadErr"
        class="space-y-2 px-4 py-3 text-sm"
        role="alert"
        data-testid="public-gate-upstream-error"
      >
        <p class="text-err">{{ t('pages.gateApproval.upstreamLoadFailed', { error: upstreamLoadErr }) }}</p>
        <button
          type="button"
          class="rounded-md inline-flex items-center gap-1.5 bg-accent px-2.5 py-1 text-[11px] font-medium text-white hover:bg-accent-hover"
          data-testid="public-gate-upstream-retry"
          @click="retryUpstreamLoad"
        >
          {{ t('pages.gateApproval.upstreamRetry') }}
        </button>
      </div>
      <div v-else-if="upstreamDoc" class="scroll-area max-h-[70vh] overflow-y-auto px-4 py-3" data-testid="public-gate-upstream-doc">
        <StructuredArtifactView name="clarified_requirement.json" :doc="upstreamDoc" />
      </div>
      <div v-else class="space-y-2 px-4 py-3 text-sm text-txt2" data-testid="public-gate-upstream-summary">
        <p v-if="preview?.upstream?.title" class="font-medium text-txt">{{ preview.upstream.title }}</p>
        <p v-if="preview?.upstream?.summary">{{ preview.upstream.summary }}</p>
        <p v-if="preview?.upstream?.description">{{ preview.upstream.description }}</p>
        <p v-if="preview?.upstream?.text">{{ preview.upstream.text }}</p>
      </div>
    </AppModal>
  </div>
</template>
