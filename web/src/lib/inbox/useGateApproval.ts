import { ref, computed, watch, provide, onUnmounted, reactive, toRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { renderMarkdown } from '@/lib/shared/markdown'
import { api, type PreviewIssue } from '@/lib/api/api'
import { useToast } from '@/lib/composables/useToast'
import { isCompositeFilled, normalizeCompositeSubmit } from '@/lib/shared/compositeText'
import { useBreakpoint } from '@/lib/composables/useBreakpoint'
import { provideReviewAnnotate } from '@/lib/inbox/reviewAnnotate'
import { pushAnnotationUnique } from '@/lib/inbox/reviewQuote'
import { isAbortError } from '@/lib/run/liveLogRehydrate'
import { REVIEW_SHELL_WIDTH_KEY_APPROVAL } from '@/lib/inbox/reviewLayoutBudget'
import { OUTPUT_KEY_TO_ARTIFACT } from '@/lib/run/structuredArtifacts'
import {
  CONTENT_FIT_PREVIEW_MAX_VH,
  CONTENT_FIT_REVIEWING_STRIP_PX,
  dataUrlToImageParts,
  type InspectElementStyle,
} from '@/lib/shared/htmlPreviewSandbox'
import {
  saveCommentPin,
  deleteCommentPin,
  getCommentPinRound,
  markCommentPinsCommitted,
  buildAnnotationArtifactPayload,
  type CommentPin,
} from '@/lib/inbox/useCommentPins'
import {
  listPrimaryProducts,
  listExcludedProduces,
  pickProductRef,
  resolveUpstreamOutputs,
  reviewingUpstreamN,
  inferArtifactKind,
  isReadonlyArtifactKind,
  type GatePrimaryProductRef,
} from '@/lib/inbox/gateUpstream'
import type { ClarifyImage, Gate, GateShareInboxStatus, Run, ReactAnnotation } from '@/lib/shared/types'
import { previewPickAnnotation, type AppPreviewPickPayload } from '@/lib/shared/previewPickUrl'
import { REVERT_ACTION_IDS, POSITIVE_ACTION_IDS } from '@/components/run/gateApproval/gateApprovalActions'
import { gateApprovalKey } from '@/components/run/gateApproval/gateApprovalContext'
import { type PlanDoc } from '@/components/run/PlanView.vue'
import { type ProposalsDoc } from '@/components/run/ProposalSelectView.vue'
import { isStructuredArtifactName } from '@/components/run/StructuredArtifactView.vue'

/** Element-level clone so queue rows never share annotation object refs with composer. */
function cloneReactAnnotations(anns?: ReactAnnotation[] | null): ReactAnnotation[] {
  if (!anns?.length) return []
  return anns.map((a) => ({ ...a }))
}

/** Element-level clone for attachment lists (same contract as annotations). */
function cloneClarifyImages(imgs?: ClarifyImage[] | null): ClarifyImage[] {
  if (!imgs?.length) return []
  return imgs.map((im) => ({ ...im }))
}

export type GateApprovalProps = {
  gate: Gate
  run?: Run
  compact?: boolean
  submitError?: string | null
  fillPreview?: boolean
  unifiedPreviewBudget?: boolean
  mobileFillRemaining?: boolean
  shareLink?: GateShareInboxStatus | null
}

export type GateApprovalEmit = {
  (e: 'resolve', action: string, form: Record<string, any>): void
  (e: 'react-revised'): void
  (e: 'open-share'): void
}

export function useGateApproval(
  props: GateApprovalProps,
  emit: GateApprovalEmit,
) {

// props injected
// emit injected

provide('gateShareOpen', () => emit('open-share'))
provide(
  'gateShareEnabled',
  computed(() => props.shareLink != null),
)

const { t } = useI18n()
const { isMobile } = useBreakpoint()
const toast = useToast()

const formText = ref<Record<string, string>>({})
const formImages = ref<Record<string, ClarifyImage[]>>({})
const resolved = ref<string | null>(null)
const formError = ref<string | null>(null)
const productEditorRef = ref<{ isDirty: { value: boolean }; discard: () => void } | null>(null)
const productDirty = ref(false)
const savedProductContent = ref<Record<string, string>>({})
const savedProductMeta = ref<Record<string, { etag?: string; updatedAt?: string; sizeBytes?: number }>>({})

function formSchemaKey(fields: Gate['form']): string {
  return (fields || []).map((f) => `${f.key}:${f.label}:${f.required}`).join('|')
}

watch(
  () => formSchemaKey(props.gate.form),
  (key, prev) => {
    if (prev !== undefined && key === prev) return
    const text: Record<string, string> = {}
    const imgs: Record<string, ClarifyImage[]> = {}
    for (const f of props.gate.form || []) {
      text[f.key] = ''
      imgs[f.key] = []
    }
    formText.value = text
    formImages.value = imgs
  },
  { immediate: true },
)

// A submission rejected by the backend (parent passes the error down): clear the
// optimistic "已提交" lock and show the reason so the reviewer can retry.
watch(
  () => props.submitError,
  (e) => {
    if (e) {
      resolved.value = null
      actionSubmitting.value = false
      formError.value = e
    }
  },
)

// A proposal_select gate presents candidate proposals as a card picker (parsed
// from the upstream proposals artifact) instead of a markdown wall + generic
// buttons. Detected via the gate's originating node type; defensively also when
// the gate's actions look like proposal ids (p1, p2, …) so it still engages if
// the run's pinned graph / node config differs.
const gateNode = computed(() => props.run?.nodes?.find((n) => n.id === props.gate.nodeId))
const looksLikeProposalActions = computed(
  () => !!props.gate.actions?.length && props.gate.actions.every((a) => /^p\d+$/.test(a.id)),
)
const isProposalSelect = computed(
  () => gateNode.value?.type === 'proposal_select' || looksLikeProposalActions.value,
)
const isAppPreview = computed(() => gateNode.value?.type === 'app_preview')

const PASS_ACTION_IDS = new Set(['pass', 'approve'])
const FAIL_ACTION_IDS = new Set(['fail', 'revise'])

const previewIssues = ref<PreviewIssue[]>([])
const previewIssuesLoading = ref(false)
const previewIssuesError = ref<string | null>(null)
const pickedSelector = ref('')
const pickedElementImage = ref<ClarifyImage | null>(null)
/** Staged ReAct annotations (jsonPath / selector / quote chips) for hot revise. */
const reactAnnotations = ref<ReactAnnotation[]>([])
const gateStageEl = ref<HTMLElement | null>(null)

/** CommentPin MVP: session pins for current gate iteration (not PreviewIssue). */
const commentPins = ref<CommentPin[]>([])
const commentPinSelectedId = ref<string | null>(null)
const commentArtifactCommitted = ref(false)
const commentArtifactWriting = ref(false)
const commentArtifactWriteError = ref<string | null>(null)
const annotateDraft = ref<{
  selector: string
  imageDataUrl?: string
  screenshotMissing?: boolean
  initialComment?: string
  bounds?: { left: number; top: number; width: number; height: number } | null
  currentText?: string
  editingId?: string | null
  style?: InspectElementStyle | null
} | null>(null)

const gateIteration = computed(() => props.gate.iteration || 1)

const commentPinBadges = computed(() =>
  commentPins.value.map((p) => ({
    id: p.id,
    seq: p.seq,
    bounds: p.bounds,
    active: p.id === commentPinSelectedId.value,
  })),
)

function pushReactAnnotation(ann: ReactAnnotation) {
  const result = pushAnnotationUnique(reactAnnotations.value, ann)
  if (result === 'duplicate') toast.warn(t('pages.reviewComposer.alreadyAdded'))
}

const annotateEnabled = computed(
  () => !!props.gate.reactSessionAlive && resolved.value == null,
)

// StructuredArtifactView AnnotateBtn → side-panel chips (only when hot session alive).
provideReviewAnnotate({
  get enabled() {
    return annotateEnabled.value
  },
  annotate: (ann) => {
    if (!annotateEnabled.value) return
    pushReactAnnotation(ann)
  },
})

function onQuoteAdd(ann: ReactAnnotation) {
  if (!annotateEnabled.value) return
  pushReactAnnotation(ann)
}

function onHtmlPreviewPick(payload: {
  selector: string
  tagName: string
  imageDataUrl: string
  bounds?: { left: number; top: number; width: number; height: number }
  currentText?: string
  style?: InspectElementStyle
}) {
  pickedSelector.value = payload.selector
  const parts = dataUrlToImageParts(payload.imageDataUrl)
  pickedElementImage.value = parts
  const missingShot = !parts
  if (missingShot) {
    // Annotation path: warn only — never drop selector. PreviewIssue chat rules unchanged.
    toast.warn(t('pages.gateApproval.commentPins.screenshotWarn'))
  }
  // Dual-channel: PreviewIssue path keeps selector/screenshot; ReAct annotations
  // get the same selector chip for hot gateReactRevise.
  if (canReactRevise.value) {
    pushReactAnnotation({
      selector: payload.selector,
      label: payload.selector || payload.tagName,
    })
  }
  // OpenDesign annotate card (CommentPin) — visual HtmlPreview only.
  if (isVisualBody.value) {
    annotateDraft.value = {
      selector: payload.selector,
      imageDataUrl: missingShot ? undefined : payload.imageDataUrl,
      screenshotMissing: missingShot,
      initialComment: '',
      bounds: payload.bounds || null,
      currentText: payload.currentText,
      editingId: null,
      style: payload.style || null,
    }
  }
}

function persistAnnotateComment(comment: string) {
  if (!props.run?.id || !annotateDraft.value) return null
  const draft = annotateDraft.value
  const result = saveCommentPin(props.run.id, props.gate.nodeId, gateIteration.value, {
    selector: draft.selector,
    comment,
    currentText: draft.currentText,
    imageDataUrl: draft.imageDataUrl,
    bounds: draft.bounds || undefined,
    editingId: draft.editingId,
  })
  if (!result) return null
  reloadCommentPins()
  commentPinSelectedId.value = result.pin.id
  annotateDraft.value = null
  if (result.needsServerInvalidate) {
    void invalidateServerAnnotationArtifact()
  }
  return result
}

function onAnnotateClose() {
  annotateDraft.value = null
}

function onAnnotateSave(comment: string) {
  const result = persistAnnotateComment(comment)
  if (!result) return
  toast.success(t('pages.gateApproval.commentPins.savedToast', { n: result.pin.seq }))
}

function onAnnotateSendChat(comment: string) {
  const result = persistAnnotateComment(comment)
  if (!result) return
  const existing = reactText.value.trim()
  reactText.value = existing ? `${existing}\n${comment}` : comment
  toast.success(t('pages.gateApproval.commentPins.sentToChatToast', { n: result.pin.seq }))
}

function onCommentPinSelect(pinId: string) {
  commentPinSelectedId.value = pinId
  const pin = commentPins.value.find((p) => p.id === pinId)
  if (!pin) return
  annotateDraft.value = {
    selector: pin.selector,
    imageDataUrl: pin.imageDataUrl,
    screenshotMissing: pin.screenshot === 'MISSING',
    initialComment: pin.comment,
    bounds: pin.bounds || null,
    currentText: pin.currentText,
    editingId: pin.id,
  }
}

function onCommentPinDelete(pinId: string) {
  if (!props.run?.id) return
  const pin = commentPins.value.find((p) => p.id === pinId)
  if (!pin) return
  const result = deleteCommentPin(props.run.id, props.gate.nodeId, gateIteration.value, pinId)
  if (!result.deleted) return
  if (commentPinSelectedId.value === pinId) commentPinSelectedId.value = null
  if (annotateDraft.value?.editingId === pinId) annotateDraft.value = null
  reloadCommentPins()
  toast.success(t('pages.gateApproval.commentPins.deletedToast', { n: pin.seq }))
  if (result.needsServerInvalidate) {
    void invalidateServerAnnotationArtifact()
  }
}

/** f4: after a committed write, any mutate must clear server delivery so Pass
 * cannot hand a stale package while UI shows draft / empty. */
let annotationArtifactWriteGen = 0
async function invalidateServerAnnotationArtifact() {
  if (!props.run?.id) return
  const gen = ++annotationArtifactWriteGen
  try {
    await api.saveAnnotationArtifact(
      props.run.id,
      props.gate.nodeId,
      buildAnnotationArtifactPayload([]),
    )
    if (gen !== annotationArtifactWriteGen) return
  } catch (e: unknown) {
    if (gen !== annotationArtifactWriteGen) return
    const msg = e instanceof Error ? e.message : t('pages.gateApproval.commentPins.writeFailed')
    commentArtifactWriteError.value = msg
    toast.warn(t('pages.gateApproval.commentPins.invalidateFailed'))
  }
}

async function onWriteCommentArtifact() {
  if (!props.run?.id || !commentPins.value.length) return
  commentArtifactWriting.value = true
  commentArtifactWriteError.value = null
  const gen = ++annotationArtifactWriteGen
  try {
    const body = buildAnnotationArtifactPayload(commentPins.value)
    await api.saveAnnotationArtifact(props.run.id, props.gate.nodeId, body)
    if (gen !== annotationArtifactWriteGen) return
    markCommentPinsCommitted(props.run.id, props.gate.nodeId, gateIteration.value)
    reloadCommentPins()
    toast.success(t('pages.gateApproval.commentPins.writtenToast'))
  } catch (e: unknown) {
    if (gen !== annotationArtifactWriteGen) return
    const msg = e instanceof Error ? e.message : t('pages.gateApproval.commentPins.writeFailed')
    commentArtifactWriteError.value = msg
    toast.error(t('pages.gateApproval.commentPins.writeFailed'))
  } finally {
    commentArtifactWriting.value = false
  }
}

/** VNC/app_preview pick payload uses outerHTML (no imageDataUrl); url is page href at pick. */
function onAppPreviewPick(payload: AppPreviewPickPayload) {
  pickedSelector.value = payload.selector
  pickedElementImage.value = null
  if (canReactRevise.value) pushReactAnnotation(previewPickAnnotation(payload))
}

function clearHtmlPreviewPick() {
  pickedSelector.value = ''
  pickedElementImage.value = null
}
const proposalsDoc = ref<ProposalsDoc | null>(null)
const proposalsLoading = ref(false)
async function loadProposals() {
  if (!isProposalSelect.value) {
    proposalsDoc.value = null
    return
  }
  const from = (gateNode.value?.config?.from || 'proposals.json').toString()
  // Prefer the configured source; fall back to the conventional proposals.json.
  const a =
    props.run?.artifacts.find((x) => x.name === from) ||
    props.run?.artifacts.find((x) => x.name === 'proposals.json')
  if (!a) {
    proposalsDoc.value = null
    return
  }
  proposalsLoading.value = true
  try {
    const full = await api.artifactContent(a.id)
    const doc = JSON.parse(full.content || '{}') as ProposalsDoc
    proposalsDoc.value = Array.isArray(doc.proposals) && doc.proposals.length ? doc : null
  } catch {
    proposalsDoc.value = null
  } finally {
    proposalsLoading.value = false
  }
}
watch(
  () => [props.gate.nodeId, isProposalSelect.value, props.run?.artifacts.map((x) => x.name).join(',')],
  loadProposals,
  { immediate: true },
)

// A plan gate resolves its body to the plan checklist markdown (see
// RenderPlanMarkdown); rendering that raw is ugly, so when we detect it we swap
// in the structured PlanView fed from the run's plan.json artifact.
const isPlanBody = computed(() => /- \[[ x]\] `g\d/.test(props.gate.bodyMd || ''))
const planDoc = ref<PlanDoc | null>(null)
const planLoading = ref(false)
async function loadPlan() {
  const a = props.run?.artifacts.find((x) => x.name === 'plan.json')
  if (!isPlanBody.value || !a) {
    planDoc.value = null
    return
  }
  planLoading.value = true
  try {
    const full = await api.artifactContent(a.id)
    const doc = JSON.parse(full.content || '{}') as PlanDoc
    planDoc.value = Array.isArray(doc.goals) ? doc : null
  } catch {
    planDoc.value = null
  } finally {
    planLoading.value = false
  }
}
watch(() => [props.gate.bodyMd, props.run?.artifacts.find((x) => x.name === 'plan.json')?.id], loadPlan, { immediate: true })

// Generalized structured body: when the gate's configured 展示内容 references an
// upstream node's structured product ({{nodes.<id>.outputs.<key>}}), render it
// with the SAME structured views as the node's 产物 tab — not raw markdown.
const bodyTemplate = computed(() => (gateNode.value?.config?.body_template || '').toString())
/** Client-side parse of body_template; used offline / when API is unavailable. */
const clientPrimaryProducts = computed<GatePrimaryProductRef[]>(() =>
  listPrimaryProducts(bodyTemplate.value, {
    isProposalSelect: isProposalSelect.value,
    proposalSelectFrom: (gateNode.value?.config?.from || 'proposals.json').toString(),
  }),
)
/**
 * Server ListGatePrimaryProducts is the SSOT for kind/readonly (store Kind may
 * mark image even when the filename has no image suffix). null = use client fallback.
 */
const apiPrimaryProducts = ref<GatePrimaryProductRef[] | null>(null)
/** True after listGatePrimaryArtifacts settles (success/offline); gates first loadProduct. */
const primaryProductsHydrated = ref(false)

function normalizeApiPrimaryItem(item: {
  name: string
  kind: string
  readonly?: boolean
  nodeId?: string
  outputKey?: string
}): GatePrimaryProductRef {
  const kind = (item.kind || inferArtifactKind(item.name)) as GatePrimaryProductRef['kind']
  return {
    name: item.name,
    kind,
    readonly: item.readonly ?? isReadonlyArtifactKind(kind),
    nodeId: item.nodeId,
    outputKey: item.outputKey,
  }
}

async function loadPrimaryProductsFromApi() {
  // Server ListGatePrimaryArtifacts requires waiting_human; skip silently otherwise
  // so resume→running (DTO may still carry gate) does not spam console 400s.
  if (
    !props.run?.id ||
    !props.gate.nodeId ||
    resolved.value != null ||
    props.run.status !== 'waiting_human'
  ) {
    apiPrimaryProducts.value = null
    primaryProductsHydrated.value = true
    return
  }
  primaryProductsHydrated.value = false
  try {
    const r = await api.listGatePrimaryArtifacts(props.run.id, props.gate.nodeId)
    const items = r.items || []
    apiPrimaryProducts.value = items.length
      ? items.map(normalizeApiPrimaryItem)
      : null
  } catch {
    // Offline / older server: keep client listPrimaryProducts as fallback.
    apiPrimaryProducts.value = null
  } finally {
    primaryProductsHydrated.value = true
  }
}

watch(
  () =>
    [
      props.run?.id,
      props.run?.status,
      props.gate.nodeId,
      resolved.value,
      bodyTemplate.value,
    ] as const,
  () => {
    void loadPrimaryProductsFromApi()
  },
  { immediate: true },
)

const primaryProducts = computed<GatePrimaryProductRef[]>(
  () => apiPrimaryProducts.value ?? clientPrimaryProducts.value,
)
const excludedProduces = computed(() =>
  listExcludedProduces(bodyTemplate.value, props.run?.nodes, {
    isProposalSelect: isProposalSelect.value,
    proposalSelectFrom: (gateNode.value?.config?.from || 'proposals.json').toString(),
  }),
)
// page/page.html preferred over the first template ref (aligned with backend pointer).
const productRef = computed<{ nodeId: string; key: string } | null>(() =>
  pickProductRef(bodyTemplate.value),
)
const productName = computed<string | null>(() => {
  const key = productRef.value?.key
  if (key && OUTPUT_KEY_TO_ARTIFACT[key]) return OUTPUT_KEY_TO_ARTIFACT[key]
  return primaryProducts.value[0]?.name || null
})
/** Editable only while gate pending (not yet submitted) and products exist. */
const canEditProducts = computed(
  () => resolved.value == null && primaryProducts.value.length > 0 && !!props.run?.id,
)
// The visual node's product is a raw HTML page: preview it in an iframe instead
// of parsing it as a structured JSON doc.
const isVisualBody = computed(() => productName.value === 'page.html')

function reloadCommentPins() {
  if (!props.run?.id || !isVisualBody.value) {
    commentPins.value = []
    commentArtifactCommitted.value = false
    return
  }
  const round = getCommentPinRound(props.run.id, props.gate.nodeId, gateIteration.value)
  commentPins.value = round.pins
  commentArtifactCommitted.value = round.artifactCommitted
}

watch(
  () => [props.run?.id, props.gate.nodeId, gateIteration.value, isVisualBody.value] as const,
  () => {
    annotateDraft.value = null
    commentPinSelectedId.value = null
    commentArtifactWriteError.value = null
    reloadCommentPins()
  },
  { immediate: true },
)

/** Paragraph quote float: structured products only (HTML keeps pick-to-annotate). */
const selectionQuoteEnabled = computed(
  () =>
    annotateEnabled.value &&
    !!productName.value &&
    isStructuredArtifactName(productName.value) &&
    !isVisualBody.value,
)
/** page.html HtmlPreview path shares PreviewIssue + Pass/Fail-by-count with app_preview. */
const usesPreviewIssues = computed(() => isAppPreview.value || isVisualBody.value)

async function loadPreviewIssues() {
  if (!usesPreviewIssues.value || !props.run?.id) {
    previewIssues.value = []
    previewIssuesLoading.value = false
    previewIssuesError.value = null
    return
  }
  previewIssuesLoading.value = true
  previewIssuesError.value = null
  try {
    const r = await api.listPreviewIssues(props.run.id, props.gate.nodeId)
    previewIssues.value = r.issues || []
  } catch {
    previewIssues.value = []
    previewIssuesError.value = 'load failed'
    toast.error(t('pages.gateApproval.loadPreviewIssuesFailed'))
  } finally {
    previewIssuesLoading.value = false
  }
}

watch(
  () => [props.run?.id, props.gate.nodeId, usesPreviewIssues.value] as const,
  loadPreviewIssues,
  { immediate: true },
)

/** Open PreviewIssue count for the current gate node (drafts / resolved excluded). */
const openPreviewIssueCount = computed(
  () => previewIssues.value.filter((i) => i.status === 'open').length,
)

/**
 * Standard review UI only exposes a single exit at a time:
 * - PreviewIssues with ≥1 open issue: the negative exit (fail/revise), since
 *   the reviewer already recorded feedback via 框选+提交问题 — confirming
 *   would silently drop it. Keep the gate's own configured label (e.g.
 *   「退回修改」/「取消需求」) so the actual downstream effect stays clear.
 * - Otherwise: the positive exit (approve/pass → 确认并流转), same as before.
 * Config-layer actions beyond these two remain on the gate DTO for silent
 * edge compat but are never rendered as dual buttons.
 */
const visibleActions = computed(() => {
  if (usesPreviewIssues.value && openPreviewIssueCount.value >= 1) {
    const negative = props.gate.actions.filter((a) => FAIL_ACTION_IDS.has(a.id))
    if (negative.length) return negative
  }
  const positive = props.gate.actions.filter((a) => PASS_ACTION_IDS.has(a.id))
  return positive.map((a) => ({
    ...a,
    label: t('pages.clarify.confirmFlow'),
  }))
})

const footerActions = computed(() => visibleActions.value)

/** True when the gate exposes at least one form field (comment channel). */
const hasFormFields = computed(() => (props.gate.form?.length ?? 0) > 0)

/**
 * Preview path: hide gate.form when PreviewFeedbackChat is the sole input.
 * Also hide when hot ReAct composer is active — form text overlaps reject draft (v3).
 */
const shouldHideGateForm = computed(
  () =>
    (usesPreviewIssues.value && hasFormFields.value) ||
    (!!props.gate.reactSessionAlive && resolved.value == null && !!props.run?.id),
)

const actionSubmitting = ref(false)

/** Action buttons lock while a cold approve/reject is in flight. */
function isActionDisabled(_actionId: string): boolean {
  return actionSubmitting.value || resolved.value != null
}

function actionPendingLabel(id: string): string {
  return POSITIVE_ACTION_IDS.has(id) ? t('common.buttons.approving') : t('common.buttons.rejecting')
}

function actionButtonTitle(_actionId: string): string {
  return ''
}

/** Help line when n_open===0 (PreviewIssues path) or non-preview without form. */
const helpReviseDetailNoIssuesText = computed(() =>
  usesPreviewIssues.value || !hasFormFields.value
    ? t('pages.gateApproval.helpReviseDetailNoIssuesNoForm')
    : t('pages.gateApproval.helpReviseDetailNoIssues'),
)

/**
 * Help line when n_open≥1 (PreviewIssues path): hot can still send in-place
 * (确认并流转 stays disabled); without a live ReAct session there is no send —
 * the footer swaps to the gate's own 退回/revise exit instead, so say that.
 */
const helpReviseWithIssuesText = computed(() =>
  canReactRevise.value
    ? t('pages.gateApproval.helpReviseWithIssuesDetail')
    : t('pages.gateApproval.helpReviseWithIssuesColdDetail'),
)

/** Hot send label (review semantics — no「打回」verb). */
const composerRejectLabel = computed(() => t('pages.reviewComposer.send'))

/**
 * Shared hot-path visibility (cold footer uses positive-only visibleActions).
 * Review semantics: always offer send + confirm when hot; open issues disable confirm.
 * send visibility uses reactSessionAlive directly (canReactRevise is defined later).
 */
const showHotPass = computed(() => props.gate.actions.some((a) => PASS_ACTION_IDS.has(a.id)))
const showHotReject = computed(
  () => !!props.gate.reactSessionAlive && resolved.value == null && !!props.run?.id,
)
/** PreviewIssues with open issues: send without extra composer draft. */
const hotRejectAllowEmpty = computed(
  () => usesPreviewIssues.value && openPreviewIssueCount.value >= 1,
)
/** Cold session: send unavailable — ordinary confirm-only approval (no ReAct/hot hints). */
const isColdSession = computed(
  () => props.gate.reactSessionAlive === false && resolved.value == null,
)

/** Cold-path help: confirm & continue; mention manual edit only when canEditProducts. */
const helpColdText = computed(() =>
  canEditProducts.value
    ? t('pages.gateApproval.helpColdWithEdit')
    : t('pages.gateApproval.helpCold'),
)

const useFillLayout = computed(() => !!props.fillPreview)
/**
 * Visual + fillPreview: desktop shell fills stage remainder (capped ≈60vh) with
 * fillParent iframe; form stays pinned in the ReviewShell sidebar.
 */
const shouldFillPreview = computed(
  () => useFillLayout.value && isVisualBody.value && !!productHtml.value,
)
/**
 * Run-detail mobile visual: remaining-height shell (not Inbox / desktop /
 * structured). Active for visual body even while loading/editing.
 */
const useMobileFillRemaining = computed(
  () =>
    !!props.mobileFillRemaining &&
    isMobile.value &&
    useFillLayout.value &&
    isVisualBody.value &&
    !isAppPreview.value &&
    !isProposalSelect.value,
)
const shouldFillAppPreview = computed(
  () => useFillLayout.value && isAppPreview.value && !isMobile.value,
)
const productDoc = ref<any>(null)
const productHtml = ref('')
const productLoading = ref(false)
/** Distinguishes load failure from empty body; null when last load succeeded. */
const productLoadError = ref<string | null>(null)
const productHasSavedContent = computed(() =>
  Object.values(savedProductContent.value).some((c) => (c || '').trim().length > 0),
)
let productLoadGen = 0
let productLoadAbort: AbortController | null = null
/**
 * Structured artifact + fillPreview: same content-fit column as visual.
 * Requires a parsed productDoc; loading / parse failure stay on the v-else path.
 * Exclude proposal_select — interactive ProposalSelectView must stay on the
 * default path (p1/p2 select); ReviewShell would swap in readonly StructuredArtifactView.
 */
const shouldFitStructured = computed(
  () =>
    useFillLayout.value &&
    !isProposalSelect.value &&
    !!productName.value &&
    isStructuredArtifactName(productName.value) &&
    !!productDoc.value &&
    !productLoading.value,
)
/** Shared enter condition for the content-fit shell (visual or structured). */
const shouldContentFit = computed(() => shouldFillPreview.value || shouldFitStructured.value)
const selectedUpstreamIteration = ref<number | null>(null)
/** True when pointer was set but snapshot missed and preview came from run artifact store. */
const previewFromArtifactFallback = ref(false)

const reviewingIteration = computed(() =>
  reviewingUpstreamN({
    upstreamIteration: props.gate.upstreamIteration,
    selectedIteration: selectedUpstreamIteration.value,
  }),
)
/** Reserve reviewing-upstream strip so HtmlPreview clamp stays within 60vh shell. */
const contentFitChromeOffsetPx = computed(() =>
  reviewingIteration.value != null ? CONTENT_FIT_REVIEWING_STRIP_PX : 0,
)

/** Narrow execution fingerprint to upstream product nodes + gate (poll anti-noise). */
function upstreamExecutionsFingerprint(
  execs: Record<string, { iteration?: number; status?: string }[] | undefined> | undefined,
): string {
  if (!execs) return ''
  const ids = new Set<string>()
  for (const p of primaryProducts.value) {
    if (p.nodeId) ids.add(p.nodeId)
  }
  if (props.gate.upstreamNodeId) ids.add(props.gate.upstreamNodeId)
  if (props.gate.nodeId) ids.add(props.gate.nodeId)
  return [...ids]
    .sort()
    .map((id) => {
      const rows = execs[id] || []
      return `${id}:${rows.map((e) => `${e.iteration ?? 0}:${e.status || ''}`).join(',')}`
    })
    .join('|')
}

function hashContentFingerprint(text: string): string {
  let hash = 0
  for (let i = 0; i < text.length; i++) {
    hash = (Math.imul(31, hash) + text.charCodeAt(i)) | 0
  }
  return `${text.length}-${hash}`
}

function buildProductLoadFingerprint(): string {
  const products = primaryProducts.value
  const pending = resolved.value == null
  const parts: string[] = [
    products.map((p) => p.name).join('|'),
    String(props.gate.iteration ?? ''),
    props.gate.upstreamNodeId ?? '',
    String(props.gate.upstreamIteration ?? ''),
    props.gate.nodeId ?? '',
    String(resolved.value ?? ''),
    pending ? 'live' : 'snap',
  ]
  for (const p of products) {
    const a = props.run?.artifacts.find((x) => x.name === p.name)
    // Resolved/history: ignore sizeBytes poll noise — content changes are
    // detected via snap hash. Pending/live: include sizeBytes (not updatedAt)
    // so a store-only write still forces a reload; etag is usually empty on the
    // list DTO poll path. Pure updatedAt bumps from Artifact.Save must not reload.
    parts.push(`${p.name}:${a?.etag ?? ''}`)
    // Pending/live: sizeBytes alone (not updatedAt) — Artifact.Save bumps UpdatedAt on
    // every upsert, so timestamp noise would force page.html reload → srcdoc flicker.
    // Content/snap hash below still catch same-size rewrites after fetch.
    if (pending && a) {
      parts.push(`meta:${a.sizeBytes ?? 0}`)
    }
    if (p.name === 'page.html' || p.outputKey === 'page') {
      const ref =
        p.nodeId && p.outputKey ? { nodeId: p.nodeId, key: p.outputKey } : productRef.value
      if (ref && props.run) {
        const result = resolveUpstreamOutputs({
          productNodeId: ref.nodeId,
          execsByNode: props.run.nodeExecutions || {},
          upstreamNodeId: props.gate.upstreamNodeId,
          upstreamIteration: props.gate.upstreamIteration,
          gateIteration: props.gate.iteration,
          pending,
        })
        const snap = result.outputs?.page
        if (typeof snap === 'string') parts.push(`snap:${hashContentFingerprint(snap)}`)
      }
    } else if (p.outputKey) {
      const ref =
        p.nodeId && p.outputKey ? { nodeId: p.nodeId, key: p.outputKey } : productRef.value
      if (ref && props.run) {
        const result = resolveUpstreamOutputs({
          productNodeId: ref.nodeId,
          execsByNode: props.run.nodeExecutions || {},
          upstreamNodeId: props.gate.upstreamNodeId,
          upstreamIteration: props.gate.upstreamIteration,
          gateIteration: props.gate.iteration,
          pending,
        })
        const snap = result.outputs?.[`${p.outputKey}_json`]
        if (typeof snap === 'string') parts.push(`snap:${hashContentFingerprint(snap)}`)
      }
    }
  }
  parts.push(upstreamExecutionsFingerprint(props.run?.nodeExecutions))
  return parts.join('::')
}

const lastProductLoadFingerprint = ref<string | null>(null)

// Upstream outputs for this gate: pointer → exact exec; miss → artifact store;
// legacy (no pointer) → max(completed) while pending, else ≤ gate.iteration.
function upstreamOutputs(): { outputs: Record<string, any> | null; pointerMiss: boolean } {
  const ref = productRef.value
  if (!ref || !props.run) {
    selectedUpstreamIteration.value = null
    return { outputs: null, pointerMiss: false }
  }
  const result = resolveUpstreamOutputs({
    productNodeId: ref.nodeId,
    execsByNode: props.run.nodeExecutions || {},
    upstreamNodeId: props.gate.upstreamNodeId,
    upstreamIteration: props.gate.upstreamIteration,
    gateIteration: props.gate.iteration,
    pending: resolved.value == null,
  })
  selectedUpstreamIteration.value = result.selectedIteration
  if (result.pointerMiss) return { outputs: null, pointerMiss: true }
  return { outputs: result.outputs, pointerMiss: false }
}

async function loadOneProductContent(
  p: GatePrimaryProductRef,
  opts?: { preferSnapshot?: boolean; signal?: AbortSignal },
): Promise<string> {
  let snapContent = ''
  const pending = resolved.value == null
  const ref = p.nodeId && p.outputKey ? { nodeId: p.nodeId, key: p.outputKey } : productRef.value
  if (ref && props.run) {
    const result = resolveUpstreamOutputs({
      productNodeId: ref.nodeId,
      execsByNode: props.run.nodeExecutions || {},
      upstreamNodeId: props.gate.upstreamNodeId,
      upstreamIteration: props.gate.upstreamIteration,
      gateIteration: props.gate.iteration,
      pending,
    })
    selectedUpstreamIteration.value = result.selectedIteration
    if (!result.pointerMiss && result.outputs) {
      if (p.name === 'page.html' || p.outputKey === 'page') {
        const snap = result.outputs.page
        if (typeof snap === 'string' && snap.trim()) snapContent = snap
      } else if (p.outputKey) {
        const snap = result.outputs[`${p.outputKey}_json`]
        if (typeof snap === 'string' && snap.trim()) snapContent = snap
      }
    }
    if (result.pointerMiss) previewFromArtifactFallback.value = true
  }
  // Pending/live gates follow the artifact store; history/resolved keep snapshots.
  if (opts?.preferSnapshot && snapContent) return snapContent
  const a = props.run?.artifacts.find((x) => x.name === p.name)
  if (!a) return snapContent
  const full = await api.artifactContent(a.id, opts?.signal ? { signal: opts.signal } : undefined)
  const storeContent = full.content || ''
  // Prefer store ETag on first load so subsequent saves send If-Match by default.
  savedProductMeta.value = {
    ...savedProductMeta.value,
    [p.name]: {
      etag: full.etag,
      updatedAt: full.updatedAt || a.createdAt,
      sizeBytes: full.sizeBytes ?? a.sizeBytes,
    },
  }
  if (pending) return storeContent || snapContent
  // Snapshot is the gate-bound preview source when present; otherwise store content.
  return snapContent || storeContent
}

async function loadProduct(opts?: { force?: boolean }) {
  previewFromArtifactFallback.value = false
  productLoadError.value = null
  const products = primaryProducts.value
  if (!products.length) {
    productDoc.value = null
    productHtml.value = ''
    selectedUpstreamIteration.value = null
    savedProductContent.value = {}
    lastProductLoadFingerprint.value = null
    return
  }

  const fingerprint = buildProductLoadFingerprint()
  const hasContent = products.some((p) => (savedProductContent.value[p.name] ?? '').trim())

  if (!opts?.force && fingerprint === lastProductLoadFingerprint.value && hasContent) {
    return
  }

  const isInitialLoad = !hasContent
  const showLoading = opts?.force || isInitialLoad

  productLoadAbort?.abort()
  const gen = ++productLoadGen
  productLoadAbort = new AbortController()
  const signal = productLoadAbort.signal

  if (showLoading) productLoading.value = true
  try {
    const next: Record<string, string> = { ...savedProductContent.value }
    const errors: string[] = []
    for (const p of products) {
      try {
        next[p.name] = await loadOneProductContent(p, {
          // Inbox/Run omit large *_json snapshots — always prefer artifact content.
          preferSnapshot: false,
          signal,
        })
      } catch (e: any) {
        if (gen !== productLoadGen || isAbortError(e) || signal.aborted) return
        // Keep prior content if any; do not collapse failure into empty string.
        const msg = e?.message || String(e)
        errors.push(`${p.name}: ${msg}`)
      }
    }
    if (gen !== productLoadGen) return
    if (errors.length) {
      productLoadError.value = errors.join('; ')
    } else {
      lastProductLoadFingerprint.value = fingerprint
    }
    savedProductContent.value = next
    // Keep legacy single-product fields for read-only / content-fit paths.
    const primary = products[0]
    const text = next[primary.name] || ''
    if (primary.name === 'page.html') {
      // Avoid HtmlPreview srcdoc reassignment when body is unchanged (no iframe flicker).
      if (productHtml.value !== text) productHtml.value = text
      productDoc.value = null
    } else if (isStructuredArtifactName(primary.name)) {
      try {
        productDoc.value = JSON.parse(text || '{}')
      } catch {
        productDoc.value = null
      }
      productHtml.value = ''
    } else {
      productDoc.value = null
      productHtml.value = ''
    }
  } finally {
    if (gen === productLoadGen) productLoading.value = false
  }
}

function retryLoadProduct() {
  productLoadError.value = null
  void loadProduct({ force: true })
}
watch(
  () =>
    [
      buildProductLoadFingerprint(),
      resolved.value,
      primaryProductsHydrated.value,
    ] as const,
  ([, , hydrated]) => {
    if (!hydrated) return
    void loadProduct()
  },
  { immediate: true },
)

onUnmounted(() => {
  productLoadAbort?.abort()
  productLoadAbort = null
  productLoadGen++
})

function onProductSaved(payload: {
  name: string
  content: string
  etag?: string
  updatedAt?: string
  sizeBytes?: number
}) {
  savedProductContent.value = { ...savedProductContent.value, [payload.name]: payload.content }
  savedProductMeta.value = {
    ...savedProductMeta.value,
    [payload.name]: {
      etag: payload.etag,
      updatedAt: payload.updatedAt,
      sizeBytes: payload.sizeBytes,
    },
  }
  if (payload.name === 'page.html') {
    productHtml.value = payload.content
  } else if (isStructuredArtifactName(payload.name)) {
    try {
      productDoc.value = JSON.parse(payload.content || '{}')
    } catch {
      /* keep */
    }
  }
  // Refresh proposals picker if that artifact was edited.
  if (payload.name === 'proposals.json' || payload.name.endsWith('proposals.json')) {
    loadProposals()
  }
}

async function onProductRefresh(name: string) {
  const p = primaryProducts.value.find((x) => x.name === name)
  if (!p) return
  productLoadError.value = null
  // Keep GateProductEditor mounted: toggling productLoading would tear down
  // draft / mode / external-change UI in the content-fit shell.
  try {
    const content = await loadOneProductContent(p, { preferSnapshot: false })
    savedProductContent.value = { ...savedProductContent.value, [name]: content }
    if (name === 'page.html') {
      productHtml.value = content
    } else if (isStructuredArtifactName(name)) {
      try {
        productDoc.value = JSON.parse(content || '{}')
      } catch {
        /* keep prior parse */
      }
    }
    lastProductLoadFingerprint.value = buildProductLoadFingerprint()
  } catch (e: any) {
    const msg = e?.message || String(e)
    productLoadError.value = `${name}: ${msg}`
    toast.error(t('pages.gateApproval.loadPreviewIssuesFailed'))
  }
}

function buildFormPayload(): Record<string, any> {
  const out: Record<string, any> = {}
  for (const f of props.gate.form || []) {
    out[f.key] = normalizeCompositeSubmit(formText.value[f.key] ?? '', formImages.value[f.key] ?? [])
  }
  return out
}

// A form field must be filled when it is itself required, or when the chosen
// action mandates the form (e.g. a "reject" action requiring a review comment).
// F5: with ≥1 PreviewIssue, skip requireForm on fail/revise so empty comment is OK.
function validate(id: string): string | null {
  const action = props.gate.actions.find((a) => a.id === id)
  let requireAll = !!action?.requireForm
  if (
    requireAll &&
    usesPreviewIssues.value &&
    (FAIL_ACTION_IDS.has(id) || PASS_ACTION_IDS.has(id))
  ) {
    requireAll = false
  }
  for (const f of props.gate.form || []) {
    if (!f.required && !requireAll) continue
    if (!isCompositeFilled({ text: formText.value[f.key] ?? '', images: formImages.value[f.key] ?? [] })) {
      return t('pages.gateApproval.submitMissing', { label: f.label || f.key })
    }
  }
  return null
}

const hasUnsentReactDraft = computed(
  () =>
    reactText.value.trim().length > 0 ||
    reactImages.value.length > 0 ||
    reactAnnotations.value.length > 0 ||
    !!pickedElementImage.value?.data ||
    !!pickedSelector.value.trim(),
)

function clearUnifiedDraft() {
  reactText.value = ''
  reactImages.value = []
  reactAnnotations.value = []
  clearHtmlPreviewPick()
  hotRejectHistorySynced.value = false
}

function choose(id: string) {
  if (actionSubmitting.value || resolved.value != null) return
  const err = validate(id)
  if (err) {
    formError.value = err
    return
  }
  if (productDirty.value) {
    const label = props.gate.actions.find((a) => a.id === id)?.label || id
    const isRevise = REVERT_ACTION_IDS.has(id)
    const ok = window.confirm(
      isRevise
        ? t('pages.gateApproval.discardConfirmRevise', { label })
        : t('pages.gateApproval.discardConfirmApprove', { label }),
    )
    if (!ok) return
    productEditorRef.value?.discard()
    productDirty.value = false
  }
  // Pass/approve: silently discard unsent draft (no confirm).
  if (POSITIVE_ACTION_IDS.has(id) && hasUnsentReactDraft.value) {
    clearUnifiedDraft()
  }
  formError.value = null
  actionSubmitting.value = true
  resolved.value = id
  emit('resolve', id, buildFormPayload())
}

const isEditing = computed(() => {
  if (productDirty.value) return true
  if (hasUnsentReactDraft.value) return true
  for (const f of props.gate.form || []) {
    if ((formText.value[f.key] ?? '').trim().length > 0) return true
    if ((formImages.value[f.key] ?? []).length > 0) return true
  }
  return false
})

// --- ReAct 打回修改 (in-place edit of the upstream producer's live session) ---
// Only offered when the DTO reports the upstream producer's review session is
// still alive; otherwise the reviewer uses the normal approve/reject buttons
// (whose reject follows the failure edge with a cold restart).
const canReactRevise = computed(() => !!props.gate.reactSessionAlive && resolved.value == null && !!props.run?.id)
const reactText = ref('')
const reactImages = ref<ClarifyImage[]>([])
const reactSending = ref(false)
/** Sandbox-aligned: pending-send queue + in-flight turn (HTTP returns on enqueue). */
const reactQueued = ref<
  { id?: string; text: string; images: ClarifyImage[]; annotations: ReactAnnotation[] }[]
>([])
const reactQueueNotice = ref<string | null>(null)
const reactQueueToast = ref<string | null>(null)
let reactQueueToastTimer = 0
const reactThinking = ref(false)
/** True between turn_begin and turn_done/error (mirrors ClarifyChat liveAgentIdx). */
const reactInFlight = ref(false)
const reactStreamText = ref('')
/** ACP thought rail (separate from message — Demo: thought must not be dropped). */
const reactStreamThought = ref('')
const reactInterrupted = ref(false)
/** Normal completion timestamp for restrained「已完成」footnote (Demo phase 4). */
const reactStreamCompletedAt = ref<string | null>(null)
const reactError = ref<string | null>(null)
const feedbackChatRef = ref<{
  send: (opts?: { body?: string }) => Promise<boolean>
  flush: () => Promise<boolean>
  reload: () => Promise<void>
  clearDraft: () => void
} | null>(null)

/**
 * After history was written but gateReactRevise failed, skip re-writing the same
 * issue on retry (avoids duplicate history entries).
 */
const hotRejectHistorySynced = ref(false)

/** Hot 打回 threshold: text ∪ images ∪ annotations ∪ pick screenshot. */
const canSubmitReact = computed(
  () =>
    !reactSending.value &&
    (reactText.value.trim().length > 0 ||
      reactImages.value.length > 0 ||
      reactAnnotations.value.length > 0 ||
      !!pickedElementImage.value?.data),
)

function hasReactComposerDraft(): boolean {
  return !!(
    reactText.value.trim() ||
    reactImages.value.length > 0 ||
    reactAnnotations.value.length > 0 ||
    pickedElementImage.value?.data
  )
}

function showReactQueueToast(msg: string) {
  reactQueueToast.value = msg
  clearTimeout(reactQueueToastTimer)
  reactQueueToastTimer = window.setTimeout(() => {
    reactQueueToast.value = null
  }, 2200)
}

function syncReactQueueThinking() {
  reactThinking.value = reactInFlight.value || reactQueued.value.length > 0
}

async function cancelReactQueuedItem(index: number) {
  if (index < 0 || index >= reactQueued.value.length) return
  const removed = reactQueued.value.splice(index, 1)[0]
  syncReactQueueThinking()
  const preview = (removed.text || '').slice(0, 18)
  showReactQueueToast(t('pages.clarify.queueCancelled', { text: preview }))
  if (removed.id && props.run?.id) {
    try {
      await api.gateReactQueueRemove(props.run.id, props.gate.nodeId, removed.id)
    } catch (e: any) {
      reactError.value = e?.message || t('pages.gateApproval.reactRevise.failed')
    }
  }
}

async function reorderReactQueuedItems(fromIndex: number, toIndex: number) {
  if (
    fromIndex < 0 ||
    toIndex < 0 ||
    fromIndex >= reactQueued.value.length ||
    toIndex >= reactQueued.value.length ||
    fromIndex === toIndex
  ) {
    return
  }
  const [moved] = reactQueued.value.splice(fromIndex, 1)
  reactQueued.value.splice(toIndex, 0, moved)
  showReactQueueToast(t('pages.clarify.queueReordered'))
  const ids = reactQueued.value.map((q) => q.id).filter((id): id is string => !!id)
  if (ids.length === reactQueued.value.length && ids.length > 0 && props.run?.id) {
    try {
      await api.gateReactQueueReorder(props.run.id, props.gate.nodeId, ids)
    } catch (e: any) {
      reactError.value = e?.message || t('pages.gateApproval.reactRevise.failed')
    }
  }
}

async function editReactQueuedItem(index: number) {
  if (index < 0 || index >= reactQueued.value.length) return
  reactQueueNotice.value = null

  // Snapshot before stash / queue_state race so we never lose the edit target.
  const target = reactQueued.value[index]
  const targetSnapshot = {
    id: target?.id,
    text: target?.text ?? '',
    images: cloneClarifyImages(target?.images),
    annotations: cloneReactAnnotations(target?.annotations),
  }

  // Composer already has an unsent draft (incl. annotation chips / pick):
  // re-enqueue it first so chips are never discarded to unblock edit.
  if (hasReactComposerDraft()) {
    if (props.run?.id) {
      if (usesPreviewIssues.value) {
        await sendHotReject()
      } else {
        await sendReactRevise()
      }
      if (reactError.value) return
    } else {
      // Local-only stash when no run (unit probes): keep chips on the queue.
      const body = reactText.value.trim()
      reactQueued.value.push({
        text: body || (reactAnnotations.value.length || reactImages.value.length ? '(annotate)' : body),
        images: cloneClarifyImages(reactImages.value),
        annotations: cloneReactAnnotations(reactAnnotations.value),
      })
      clearUnifiedDraft()
      reactThinking.value = true
    }
    // Re-locate target after append (prefer stable id; text fallback).
    index = targetSnapshot.id
      ? reactQueued.value.findIndex((q) => q.id === targetSnapshot.id)
      : reactQueued.value.findIndex((q) => q.text === targetSnapshot.text)
    if (index < 0) {
      // queue_state raced: target missing locally — refill from pre-stash snapshot.
      reactText.value = targetSnapshot.text === '(annotate)' ? '' : targetSnapshot.text
      reactImages.value = cloneClarifyImages(targetSnapshot.images)
      reactAnnotations.value = cloneReactAnnotations(targetSnapshot.annotations)
      syncReactQueueThinking()
      showReactQueueToast(t('pages.clarify.queueEditRefilled'))
      if (targetSnapshot.id && props.run?.id) {
        try {
          await api.gateReactQueueRemove(props.run.id, props.gate.nodeId, targetSnapshot.id)
        } catch (e: any) {
          reactError.value = e?.message || t('pages.gateApproval.reactRevise.failed')
        }
      } else if (!targetSnapshot.id) {
        reactQueueNotice.value = t('pages.clarify.queueEditLost')
      }
      return
    }
  }

  const item = reactQueued.value.splice(index, 1)[0]
  reactText.value = item.text === '(annotate)' ? '' : item.text
  reactImages.value = cloneClarifyImages(item.images)
  reactAnnotations.value = cloneReactAnnotations(item.annotations)
  syncReactQueueThinking()
  showReactQueueToast(t('pages.clarify.queueEditRefilled'))
  if (item.id && props.run?.id) {
    try {
      await api.gateReactQueueRemove(props.run.id, props.gate.nodeId, item.id)
    } catch (e: any) {
      reactError.value = e?.message || t('pages.gateApproval.reactRevise.failed')
    }
  }
}

/**
 * Authoritative idle (waiting=0 ∧ !busy ∧ no activeItem): force-clear local
 * sticky busy — ghost queued, unfinished inFlight, and thinking.
 */
function forceReactAuthoritativeIdle() {
  if (reactQueued.value.length) reactQueued.value = []
  if (reactInFlight.value) {
    reactInFlight.value = false
    if (
      (reactStreamText.value || reactStreamThought.value) &&
      !reactStreamCompletedAt.value &&
      !reactInterrupted.value
    ) {
      reactStreamCompletedAt.value = new Date().toISOString()
    }
  }
  reactThinking.value = false
}

/**
 * After turn_done/error: drop only ghost optimistic rows (no server id).
 * Keep authoritative waiters so multi-turn does not briefly unlock confirm.
 */
function settleReactAfterTurnEnd() {
  if (reactInFlight.value) {
    reactInFlight.value = false
  }
  if (reactQueued.value.some((q) => !q.id)) {
    reactQueued.value = reactQueued.value.filter((q) => !!q.id)
  }
  reactThinking.value = reactQueued.value.length > 0
}

function applyReviewFrame(frame: {
  event?: string
  nodeId?: string
  item?: {
    id?: string
    text?: string
    images?: ClarifyImage[]
    annotations?: ReactAnnotation[]
  }
  interrupted?: boolean
  message?: string
  waiting?: number
  items?: {
    id?: string
    text?: string
    images?: ClarifyImage[]
    annotations?: ReactAnnotation[]
  }[]
  busy?: boolean
  activeItem?: {
    id?: string
    text?: string
    images?: ClarifyImage[]
    annotations?: ReactAnnotation[]
  } | null
}) {
  const producer = props.gate.reactUpstreamNodeId
  if (producer && frame.nodeId && frame.nodeId !== producer) return
  switch (frame.event) {
    case 'turn_begin':
      reactQueued.value.shift()
      reactThinking.value = true
      reactInFlight.value = true
      reactStreamText.value = ''
      reactStreamThought.value = ''
      reactInterrupted.value = false
      reactStreamCompletedAt.value = null
      break
    case 'turn_done':
      if (frame.interrupted) {
        reactInterrupted.value = true
        reactStreamCompletedAt.value = null
      } else if (reactStreamText.value || reactStreamThought.value) {
        reactStreamCompletedAt.value = new Date().toISOString()
      }
      // Ghosts only; keep real id waiters (review v1 / ClarifyChat settleAfterTurnEnd).
      settleReactAfterTurnEnd()
      break
    case 'error':
      reactError.value = frame.message || reactError.value
      reactStreamCompletedAt.value = null
      if (frame.interrupted) reactInterrupted.value = true
      settleReactAfterTurnEnd()
      break
    case 'queue_state': {
      // Platform-authoritative: remote Cancel / cross-entry must clear ghost rows.
      // Refresh resume: busy+activeItem must keep thinking/inFlight even when waiting=0.
      const waiting = typeof frame.waiting === 'number' ? frame.waiting : 0
      const items = Array.isArray(frame.items) ? frame.items : null
      const busy = !!frame.busy
      const activeItem = frame.activeItem
      const authoritativeIdle = waiting === 0 && !busy && !activeItem
      if (authoritativeIdle) {
        forceReactAuthoritativeIdle()
        break
      }
      if (busy && activeItem && !reactInFlight.value) {
        reactInFlight.value = true
        reactThinking.value = true
        reactStreamCompletedAt.value = null
        // Empty rails — host seeds ACP (pending buffer / nodeEvents) next.
        if (!reactStreamText.value && !reactStreamThought.value) {
          reactStreamText.value = ''
          reactStreamThought.value = ''
        }
      }
      // Authority !busy: end unfinished inFlight (keep completed footnote when content exists).
      if (!busy) {
        const emptyRails = !reactStreamText.value && !reactStreamThought.value
        if (emptyRails) {
          reactInFlight.value = false
          reactStreamCompletedAt.value = null
        } else if (reactInFlight.value) {
          reactInFlight.value = false
          if (!reactStreamCompletedAt.value) {
            reactStreamCompletedAt.value = new Date().toISOString()
          }
        }
      }
      if (waiting === 0) {
        reactQueued.value = []
        if (!reactInFlight.value && !busy) reactThinking.value = false
        else reactThinking.value = reactInFlight.value || busy
        break
      }
      if (items) {
        // Preserve server id so turn_done can distinguish ghost vs real waiters.
        // Prefer frame images/annotations; local only when frame omits; always slice.
        const rebuilt = items.map((it) => {
          const text = it.text ?? ''
          const id = typeof it.id === 'string' && it.id ? it.id : undefined
          const local = id
            ? reactQueued.value.find((q) => q.id === id) ??
              reactQueued.value.find((q) => !q.id && q.text === text)
            : reactQueued.value.find((q) => q.text === text)
          const images = Array.isArray(it.images)
            ? cloneClarifyImages(it.images)
            : cloneClarifyImages(local?.images)
          const annotations = Array.isArray(it.annotations)
            ? cloneReactAnnotations(it.annotations)
            : cloneReactAnnotations(local?.annotations)
          return {
            id: id ?? local?.id,
            text,
            images,
            annotations,
          }
        })
        const maxLocal = reactInFlight.value || busy ? rebuilt.length : rebuilt.length + 1
        if (reactQueued.value.length > maxLocal) {
          const optimistic = reactQueued.value
            .slice(rebuilt.length)
            .slice(0, Math.max(0, maxLocal - rebuilt.length))
          reactQueued.value = [...rebuilt, ...optimistic]
        } else if (reactQueued.value.length < rebuilt.length) {
          reactQueued.value = rebuilt
        } else {
          const optimistic = reactQueued.value.slice(rebuilt.length)
          reactQueued.value = [...rebuilt, ...optimistic]
        }
      }
      reactThinking.value = reactInFlight.value || reactQueued.value.length > 0 || busy
      break
    }
  }
}

/**
 * Apply cumulative ACP to gate hot-revise rails.
 * Returns false when not ready (!thinking && !inFlight) so host buffers —
 * never silent-noop as applied (mounted-but-no-slot race).
 */
function applyAcpEvents(events: { kind?: string; text?: string }[] | undefined): boolean {
  if (!events?.length) return true
  // Accept ACP while busy/inFlight even if waiting=0 cleared the queue panel.
  if (!reactThinking.value && !reactInFlight.value) return false
  if (!reactThinking.value) reactThinking.value = true
  for (const ev of events) {
    // Ignore tool_call / plan UI; keep rails separate so thought is never overwritten.
    if (ev.kind === 'message' && ev.text) reactStreamText.value = ev.text
    if (ev.kind === 'thought' && ev.text) reactStreamThought.value = ev.text
  }
  return true
}

async function cancelReactRevise() {
  if (!props.run?.id || !canReactRevise.value) return
  reactQueued.value = []
  reactThinking.value = false
  reactInFlight.value = false
  reactInterrupted.value = true
  reactStreamCompletedAt.value = null
  try {
    await api.gateReactCancel(props.run.id, props.gate.nodeId)
  } catch (e: any) {
    reactError.value = e?.message || t('pages.gateApproval.reactRevise.failed')
  }
}

/** 记入意见 requires PreviewIssue-capable content (API: body or images). */
const canRecordIssue = computed(
  () =>
    !reactSending.value &&
    (reactText.value.trim().length > 0 ||
      reactImages.value.length > 0 ||
      !!pickedElementImage.value?.data),
)

function collectUnifiedIssueImages(): ClarifyImage[] {
  const images: ClarifyImage[] = []
  if (pickedElementImage.value?.data) {
    images.push({
      data: pickedElementImage.value.data,
      mimeType: pickedElementImage.value.mimeType || 'image/png',
    })
  }
  for (const im of reactImages.value) {
    if (im.data) images.push({ data: im.data, mimeType: im.mimeType, name: im.name })
  }
  return images
}

function annotationHistoryBody(anns: ReactAnnotation[]): string {
  const parts = anns
    .map((a) => a.label || a.jsonPath || a.selector || '')
    .map((s) => s.trim())
    .filter(Boolean)
  if (!parts.length) return t('pages.gateApproval.reactRevise.annotationOnly')
  return t('pages.gateApproval.reactRevise.annotationSummary', { summary: parts.join('、') })
}

/**
 * Write unsent unified draft into PreviewIssue history via PreviewFeedbackChat
 * so the sidebar list updates immediately (push + issues-changed).
 */
async function flushFeedbackDraft(): Promise<boolean> {
  if (!props.run?.id || !usesPreviewIssues.value) return false
  const chat = feedbackChatRef.value
  if (chat) {
    try {
      return await chat.flush()
    } catch (e: any) {
      reactError.value = e?.message || t('pages.gateApproval.reactRevise.failed')
      return false
    }
  }
  // Fallback when chat ref is unavailable (should be rare on ReviewShell paths).
  const body = reactText.value.trim()
  const images = collectUnifiedIssueImages()
  if (!body && images.length === 0) return false
  try {
    await api.createPreviewIssue(
      props.run.id,
      props.gate.nodeId,
      body,
      pickedSelector.value || '',
      0,
      images,
    )
    reactText.value = ''
    reactImages.value = []
    clearHtmlPreviewPick()
    await loadPreviewIssues()
    return true
  } catch (e: any) {
    reactError.value = e?.message || t('pages.gateApproval.reactRevise.failed')
    return false
  }
}

/** Hot: 记入意见 — PreviewIssue only, no gateReactRevise. */
async function recordFeedbackIssue() {
  if (!canRecordIssue.value) return
  reactSending.value = true
  reactError.value = null
  try {
    const ok = await flushFeedbackDraft()
    if (!ok && !reactError.value) {
      reactError.value = t('pages.gateApproval.reactRevise.failed')
    }
  } finally {
    reactSending.value = false
  }
}

/** Non-preview ReviewComposer path: gateReactRevise only. */
async function sendReactRevise() {
  if (!props.run?.id || !canSubmitReact.value) return
  const body = reactText.value.trim()
  reactQueued.value.push({
    text: body,
    images: cloneClarifyImages(reactImages.value),
    annotations: cloneReactAnnotations(reactAnnotations.value),
  })
  reactThinking.value = true
  reactSending.value = true
  reactError.value = null
  try {
    await api.gateReactRevise(
      props.run.id,
      props.gate.nodeId,
      body,
      reactImages.value,
      cloneReactAnnotations(reactAnnotations.value),
    )
    clearUnifiedDraft()
    emit('react-revised')
  } catch (e: any) {
    reactQueued.value.pop()
    reactThinking.value = reactQueued.value.length > 0 || reactStreamText.value.length > 0
    reactError.value = e?.message || t('pages.gateApproval.reactRevise.failed')
  } finally {
    reactSending.value = false
  }
}

/**
 * Sync PreviewIssue history for hot reject (text/images via flush; annotation-only
 * via placeholder body). Returns false if a required history write failed.
 */
async function syncHotRejectHistory(anns: ReactAnnotation[]): Promise<boolean> {
  if (!usesPreviewIssues.value || !props.run?.id) return true
  if (hotRejectHistorySynced.value) return true

  const body = reactText.value.trim()
  const issueImages = collectUnifiedIssueImages()
  const chat = feedbackChatRef.value

  if (body || issueImages.length > 0) {
    const ok = await flushFeedbackDraft()
    if (ok) hotRejectHistorySynced.value = true
    return ok
  }

  if (anns.length === 0) return true

  // Annotation-only: write a placeholder so f4 history stays consistent.
  const summary = annotationHistoryBody(anns)
  try {
    if (chat) {
      const ok = await chat.send({ body: summary })
      if (ok) hotRejectHistorySynced.value = true
      return ok
    }
    await api.createPreviewIssue(props.run.id, props.gate.nodeId, summary, '', 0, [])
    await loadPreviewIssues()
    await feedbackChatRef.value?.reload?.()
    hotRejectHistorySynced.value = true
    return true
  } catch (e: any) {
    reactError.value = e?.message || t('pages.gateApproval.reactRevise.failed')
    return false
  }
}

/**
 * Hot preview path: sync PreviewIssue history then gateReactRevise (send).
 * When n_open≥1, allow send without extra draft. Empty send with n_open===0
 * still needs payload. On revise failure after history write, keep the draft.
 */
async function sendHotReject() {
  if (!props.run?.id || reactSending.value) return
  const hasPayload =
    reactText.value.trim().length > 0 ||
    reactImages.value.length > 0 ||
    reactAnnotations.value.length > 0 ||
    !!pickedElementImage.value?.data
  // n_open≥1 may send without extra draft; otherwise need payload.
  if (!hasPayload && openPreviewIssueCount.value === 0) return
  reactSending.value = true
  reactError.value = null
  const body = reactText.value.trim()
  const issueImages = collectUnifiedIssueImages()
  const anns = cloneReactAnnotations(reactAnnotations.value)
  const reviseImages = issueImages.map((im) => ({ data: im.data, mimeType: im.mimeType }))
  const draftSnap = {
    text: reactText.value,
    images: cloneClarifyImages(reactImages.value),
    anns: cloneReactAnnotations(anns),
    selector: pickedSelector.value,
    elementImage: pickedElementImage.value,
  }
  reactQueued.value.push({
    text: body || '(annotate)',
    images: reviseImages.map((im) => ({ data: im.data, mimeType: im.mimeType })),
    annotations: cloneReactAnnotations(anns),
  })
  reactThinking.value = true
  try {
    const synced = await syncHotRejectHistory(anns)
    if (!synced) {
      reactQueued.value.pop()
      return
    }

    await api.gateReactRevise(props.run.id, props.gate.nodeId, body, reviseImages, anns)
    clearUnifiedDraft()
    emit('react-revised')
  } catch (e: any) {
    reactQueued.value.pop()
    reactThinking.value = reactQueued.value.length > 0
    if (hotRejectHistorySynced.value) {
      reactError.value = t('pages.gateApproval.reactRevise.issueSavedReviseFailed')
      reactText.value = draftSnap.text
      reactImages.value = draftSnap.images
      reactAnnotations.value = draftSnap.anns
      pickedSelector.value = draftSnap.selector
      pickedElementImage.value = draftSnap.elementImage
    } else {
      reactError.value = e?.message || t('pages.gateApproval.reactRevise.failed')
    }
  } finally {
    reactSending.value = false
  }
}

/** Cold sidebar action: flush unsent feedback text before revise; approve via choose. */
async function onSidebarAction(id: string) {
  if (actionSubmitting.value || resolved.value != null) return
  if (REVERT_ACTION_IDS.has(id) && usesPreviewIssues.value) {
    reactSending.value = true
    reactError.value = null
    try {
      await flushFeedbackDraft()
    } finally {
      reactSending.value = false
    }
  }
  choose(id)
}

/** Desktop/narrow ReviewShell for content-fit gates (stage | sidebar). */
const useReviewShellLayout = computed(
  () =>
    !isProposalSelect.value &&
    (shouldContentFit.value ||
      (canEditProducts.value && !!props.fillPreview && !isAppPreview.value)) &&
    !useMobileFillRemaining.value,
)

/**
 * Inbox desktop: one budget wraps preview + upstream and fills the stage.
 * Desktop-only so mobile content-fit keeps per-preview maxHeight.
 */
const useUnifiedPreviewBudget = computed(
  () => !!props.unifiedPreviewBudget && !isMobile.value && useReviewShellLayout.value,
)

const passAction = computed(() => {
  // Prefer configured positive action; fall back to first approve/pass id for confirm.
  return (
    props.gate.actions.find((a) => POSITIVE_ACTION_IDS.has(a.id)) ||
    footerActions.value.find((a) => POSITIVE_ACTION_IDS.has(a.id))
  )
})

/** Align with review semantics: confirm disabled when open PreviewIssues or busy.
 * Also FR4: busy (thinking / queued) disables confirm — Cancel first or wait ready. */
const composerPassDisabled = computed(
  () =>
    !passAction.value ||
    isProposalSelect.value ||
    isActionDisabled(passAction.value.id) ||
    reactThinking.value ||
    reactQueued.value.length > 0 ||
    (usesPreviewIssues.value && openPreviewIssueCount.value >= 1),
)

function onComposerPass() {
  if (!showHotPass.value || composerPassDisabled.value) return
  const a = passAction.value
  if (a) choose(a.id)
}

/** PreviewIssues hot reject syncs history; non-preview uses draft-only revise. */
function onComposerReject() {
  if (!showHotReject.value) return
  if (usesPreviewIssues.value) {
    void sendHotReject()
    return
  }
  void sendReactRevise()
}

provide(gateApprovalKey, {
  s: reactive({
    gate: toRef(props, 'gate'),
    run: toRef(props, 'run'),
    isMobile,
    t,
    formText,
    formImages,
    resolved,
    formError,
    actionSubmitting,
    productDirty,
    savedProductContent,
    savedProductMeta,
    primaryProducts,
    excludedProduces,
    productName,
    canEditProducts,
    isVisualBody,
    isProposalSelect,
    isAppPreview,
    bodyTemplate,
    usesPreviewIssues,
    openPreviewIssueCount,
    previewIssuesLoading,
    previewIssuesError,
    pickedSelector,
    pickedElementImage,
    commentPins,
    commentPinBadges,
    commentPinSelectedId,
    commentArtifactCommitted,
    commentArtifactWriting,
    commentArtifactWriteError,
    annotateDraft,
    proposalsDoc,
    proposalsLoading,
    planDoc,
    planLoading,
    productDoc,
    productHtml,
    productLoading,
    productLoadError,
    productHasSavedContent,
    reviewingIteration,
    previewFromArtifactFallback,
    shouldFillPreview,
    shouldFitStructured,
    shouldFillAppPreview,
    useFillLayout,
    useUnifiedPreviewBudget,
    contentFitChromeOffsetPx,
    CONTENT_FIT_PREVIEW_MAX_VH,
    REVIEW_SHELL_WIDTH_KEY_APPROVAL,
    shouldHideGateForm,
    footerActions,
    isColdSession,
    helpColdText,
    helpReviseDetailNoIssuesText,
    helpReviseWithIssuesText,
    canReactRevise,
    canRecordIssue,
    canSubmitReact,
    showHotPass,
    showHotReject,
    hotRejectAllowEmpty,
    composerRejectLabel,
    composerPassDisabled,
    passAction,
    reactText,
    reactImages,
    reactAnnotations,
    reactSending,
    reactError,
    reactQueued,
    reactQueueNotice,
    reactQueueToast,
    cancelReactQueuedItem,
    reorderReactQueuedItems,
    editReactQueuedItem,
    reactThinking,
    reactStreamText,
    reactStreamThought,
    reactInterrupted,
    reactStreamCompletedAt,
    isActionDisabled,
    actionPendingLabel,
    actionButtonTitle,
    onProductSaved,
    onProductRefresh,
    retryLoadProduct,
    onHtmlPreviewPick,
    onAppPreviewPick,
    clearHtmlPreviewPick,
    onAnnotateSave,
    onAnnotateSendChat,
    onAnnotateClose,
    onCommentPinSelect,
    onCommentPinDelete,
    onWriteCommentArtifact,
    loadPreviewIssues,
    choose,
    recordFeedbackIssue,
    sendHotReject,
    onComposerPass,
    onComposerReject,
    cancelReactRevise,
    onSidebarAction,
    renderMarkdown,
  }),
  productEditorRef,
  feedbackChatRef,
  gateStageEl,
})


  return {
    formSchemaKey,
    pushReactAnnotation,
    onQuoteAdd,
    onHtmlPreviewPick,
    persistAnnotateComment,
    onAnnotateClose,
    onAnnotateSave,
    onAnnotateSendChat,
    onCommentPinSelect,
    onCommentPinDelete,
    invalidateServerAnnotationArtifact,
    onWriteCommentArtifact,
    onAppPreviewPick,
    clearHtmlPreviewPick,
    loadProposals,
    loadPlan,
    normalizeApiPrimaryItem,
    loadPrimaryProductsFromApi,
    reloadCommentPins,
    loadPreviewIssues,
    isActionDisabled,
    actionPendingLabel,
    actionButtonTitle,
    upstreamExecutionsFingerprint,
    hashContentFingerprint,
    buildProductLoadFingerprint,
    upstreamOutputs,
    loadOneProductContent,
    loadProduct,
    retryLoadProduct,
    onProductSaved,
    onProductRefresh,
    buildFormPayload,
    validate,
    clearUnifiedDraft,
    choose,
    forceReactAuthoritativeIdle,
    settleReactAfterTurnEnd,
    applyReviewFrame,
    applyAcpEvents,
    cancelReactRevise,
    collectUnifiedIssueImages,
    annotationHistoryBody,
    flushFeedbackDraft,
    recordFeedbackIssue,
    sendReactRevise,
    syncHotRejectHistory,
    sendHotReject,
    onSidebarAction,
    onComposerPass,
    onComposerReject,
    toast,
    formText,
    formImages,
    resolved,
    formError,
    productEditorRef,
    productDirty,
    savedProductContent,
    savedProductMeta,
    gateNode,
    looksLikeProposalActions,
    isProposalSelect,
    isAppPreview,
    PASS_ACTION_IDS,
    FAIL_ACTION_IDS,
    previewIssues,
    previewIssuesLoading,
    previewIssuesError,
    pickedSelector,
    pickedElementImage,
    reactAnnotations,
    gateStageEl,
    commentPins,
    commentPinSelectedId,
    commentArtifactCommitted,
    commentArtifactWriting,
    commentArtifactWriteError,
    annotateDraft,
    gateIteration,
    commentPinBadges,
    annotateEnabled,
    annotationArtifactWriteGen,
    proposalsDoc,
    proposalsLoading,
    isPlanBody,
    planDoc,
    planLoading,
    bodyTemplate,
    clientPrimaryProducts,
    apiPrimaryProducts,
    primaryProductsHydrated,
    primaryProducts,
    excludedProduces,
    productRef,
    productName,
    canEditProducts,
    isVisualBody,
    selectionQuoteEnabled,
    usesPreviewIssues,
    openPreviewIssueCount,
    visibleActions,
    footerActions,
    hasFormFields,
    shouldHideGateForm,
    actionSubmitting,
    helpReviseDetailNoIssuesText,
    helpReviseWithIssuesText,
    composerRejectLabel,
    showHotPass,
    showHotReject,
    hotRejectAllowEmpty,
    isColdSession,
    helpColdText,
    useFillLayout,
    shouldFillPreview,
    useMobileFillRemaining,
    shouldFillAppPreview,
    productDoc,
    productHtml,
    productLoading,
    productLoadError,
    productHasSavedContent,
    productLoadGen,
    shouldFitStructured,
    shouldContentFit,
    selectedUpstreamIteration,
    previewFromArtifactFallback,
    reviewingIteration,
    contentFitChromeOffsetPx,
    lastProductLoadFingerprint,
    hasUnsentReactDraft,
    isEditing,
    canReactRevise,
    reactText,
    reactImages,
    reactSending,
    reactQueued,
    reactQueueNotice,
    reactQueueToast,
    cancelReactQueuedItem,
    reorderReactQueuedItems,
    editReactQueuedItem,
    reactThinking,
    reactInFlight,
    reactStreamText,
    reactStreamThought,
    reactInterrupted,
    reactStreamCompletedAt,
    reactError,
    feedbackChatRef,
    hotRejectHistorySynced,
    canSubmitReact,
    canRecordIssue,
    useReviewShellLayout,
    useUnifiedPreviewBudget,
    passAction,
    composerPassDisabled,
    t,
    isMobile,
  }
}
