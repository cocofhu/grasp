<script setup lang="ts">
import { findGraphNode, isClarifyNode, isPreviewNode, type CapsNode } from '@/lib/shared/clarifyInteractive'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import HtmlPreview from '@/components/ui/HtmlPreview.vue'
import ArtifactPreview from '@/components/run/ArtifactPreview.vue'
import ArtifactVersionSelect from '@/components/ui/ArtifactVersionSelect.vue'
import NovncPreviewPanel from '@/components/run/NovncPreviewPanel.vue'
import AppPreviewPanel from '@/components/run/AppPreviewPanel.vue'
import PublicAppPreviewPanel from '@/components/run/PublicAppPreviewPanel.vue'
import SandboxStageSurface from '@/components/run/SandboxStageSurface.vue'
import ResourceCardIllustration from '@/components/run/ResourceCardIllustration.vue'
import StageWorkflow from '@/components/run/StageWorkflow.vue'
import { api, type SandboxView } from '@/lib/api/api'
import { publicGateApi } from '@/lib/inbox/gateShareLink'
import type { PublicPreviewPort } from '@/lib/inbox/gateShareLink'
import { relTime } from '@/lib/shared/format'
import { isAbortError } from '@/lib/run/liveLogRehydrate'
import { useToast } from '@/lib/composables/useToast'
import { provideReviewAnnotate, useReviewAnnotate } from '@/lib/inbox/reviewAnnotate'
import { addClarifyAnnotation } from '@/lib/inbox/useClarifyDraft'
import type { AppPreviewPickPayload } from '@/lib/shared/previewPickUrl'
import type { Artifact, ReactAnnotation, Run } from '@/lib/shared/types'
import {
  REACT_STAGE_TAB_GRID,
  REACT_STAGE_TAB_NOVNC,
  clarifyStageRemoteKind,
  artifactFriendlyNameKey,
  artifactKindLabelKey,
  artifactTechnicalDisplayName,
  buildArtifactFingerprintMap,
  buildStageCardThumb,
  clearStageTabUnread,
  closeStagePreviewTab,
  diffArtifactFingerprints,
  findArtifactByName,
  isAutoPinStageNode,
  isVisibleAutoPinArtifact,
  markStageTabUnread,
  openStagePreviewTab,
  previewTabId,
  previewTabName,
  artifactFingerprint,
  canAnnotateStageArtifact,
  expandStageArtifacts,
  isHistoricalStageArtifact,
  isOwnNodeArtifact,
  clearStageOpenState,
  loadStageOpenState,
  parseHistoricalStageArtifact,
  resolveEffectivePreviewPin,
  resolveStageRemoteKind,
  restoreStageOpenState,
  saveStageOpenState,
  shouldActivatePinnedPreview,
  shouldFocusPinnedOrAutoTab,
  stageGridArtifactsForNode,
  wantsTextSummaryThumb,
  type ArtifactFingerprintMap,
  type ReactStageRemoteKind,
  type StageCardThumb,
  type StageTabUnreadKind,
  type ArtifactVersionChoice,
} from '@/lib/run/reactArtifactPreview'
import { useArtifactVersions } from '@/lib/run/useArtifactVersions'
import { registerStageLinks } from '@/lib/run/stageLinks'
import { peekConversationDrawer } from '@/lib/chat/conversationDrawer'

const props = withDefaults(
  defineProps<{
    artifacts: Artifact[]
    previewArtifact?: string | null
    runId?: string
    run?: Run | null
    nodeId?: string
    /** Graph node (type + caps snapshot). Fallback: run.nodes lookup by nodeId. */
    node?: CapsNode
    /** When true, ArtifactPreview will not fetch (content already inlined). */
    inlineContent?: boolean
    /** Enable 取点 / 划选 / ⤴ 标注 on the current node's artifacts. */
    annotatable?: boolean
    remoteKind?: ReactStageRemoteKind
    /** Force-hide app / sandbox remote card and tab (artifact-only embed modal). */
    hideAppPreview?: boolean
    token?: string
    ports?: PublicPreviewPort[]
    publicActive?: boolean
    publicMobile?: boolean
    /** Review stage of a non-clarify node: show the app tab once set_preview registers. */
    probeRegisteredPreview?: boolean
  }>(),
  {
    previewArtifact: '',
    runId: '',
    run: null,
    nodeId: '',
    node: null,
    inlineContent: false,
    annotatable: false,
    hideAppPreview: false,
    token: '',
    ports: () => [],
    publicActive: true,
    publicMobile: false,
    probeRegisteredPreview: false,
  },
)

const emit = defineEmits<{
  pick: [payload: AppPreviewPickPayload]
  stagedPick: [payload: AppPreviewPickPayload | null]
}>()

const { t } = useI18n()
const toast = useToast()
const parentAnnotate = useReviewAnnotate()

function stageAnnotation(ann: ReactAnnotation) {
  if (!props.annotatable) return
  if (parentAnnotate) {
    parentAnnotate.annotate(ann)
    return
  }
  if (!props.runId || !props.nodeId) return
  if (addClarifyAnnotation(props.runId, props.nodeId, ann) === 'duplicate') {
    toast.warn(t('pages.reviewComposer.alreadyAdded'))
  }
}

provideReviewAnnotate({
  get enabled() {
    if (!props.annotatable) return false
    if (parentAnnotate) return parentAnnotate.enabled
    return !!props.runId && !!props.nodeId
  },
  annotate: (ann) => stageAnnotation(ann),
})

const initialOpen = restoreStageOpenState(
  loadStageOpenState(props.runId, props.nodeId),
  (props.artifacts || []).map((a) => a.name),
)
/** Default to workflow artifacts grid (no standalone preview chrome Tab). */
const activeTab = ref(initialOpen?.activeTab || REACT_STAGE_TAB_GRID)
type ResourceView = 'home' | 'ide' | 'terminal' | 'browser' | 'log' | 'app'
const resourceView = ref<ResourceView>('home')
const artifactView = ref('home')
function seedDrawerViews(tab: string) {
  if (tab === 'ide' || tab === 'terminal' || tab === 'browser' || tab === 'log') resourceView.value = tab
  else if (tab === REACT_STAGE_TAB_NOVNC) resourceView.value = 'app'
  else {
    const name = previewTabName(tab)
    if (name) artifactView.value = name
  }
}
seedDrawerViews(activeTab.value)
/** True after the user picks a grid tab, card, preview tab, or noVNC — auto-open must not steal focus.
 *  Also set when session open-state restore succeeds so pin auto-activate loses to refresh restore. */
const userMoved = ref(!!initialOpen)
const openNames = ref<string[]>(initialOpen?.openNames || [])
/** Session-local 「新 / 已更新」 marks on non-active preview tabs. */
const tabUnread = ref<Record<string, StageTabUnreadKind>>({})
/** Seeded fingerprint map for clarify auto-pin; null until first observe. */
const autoPinFingerprints = ref<ArtifactFingerprintMap | null>(null)
const summaryThumbs = ref<Record<string, StageCardThumb>>({})
const novncOpen = ref(!!initialOpen?.novncOpen)
const fixedOpen = ref<string[]>([...(initialOpen?.fixedOpen || [])])
const fixedMounted = ref<Record<string, boolean>>({})
for (const id of fixedOpen.value) fixedMounted.value[id] = true
const sandboxView = ref<SandboxView | null>(null)
const sandboxLoading = ref(false)
const sandboxId = computed(() => {
  const id = sandboxView.value?.id
  if (typeof id === 'number' && Number.isFinite(id) && id > 0) return id
  const n = Number(id)
  return Number.isFinite(n) && n > 0 ? n : null
})
let summaryThumbGen = 0
let summaryThumbAbort: AbortController | null = null
const summaryThumbFp: Record<string, string> = {}
const selectedVersionIndex = ref<Record<string, number>>({})
const versions = useArtifactVersions()

const resolvedRemoteKind = computed(() =>
  resolveStageRemoteKind({
    remoteKind: props.remoteKind,
    runId: props.runId,
    nodeId: props.nodeId,
    inlineContent: props.inlineContent,
  }),
)
const stageNode = computed(() => findGraphNode(props.run?.nodes, props.nodeId) || null)
const resolvedNode = computed<CapsNode>(() => props.node || stageNode.value)
const clarifyPreview = computed(() => isClarifyNode(resolvedNode.value) && isPreviewNode(resolvedNode.value))

/** Clarify with set_preview, or a review stage that opts in: silent probe for set_preview registrations. */
const REGISTERED_PREVIEW_POLL_MS = 2500
const previewRegistered = ref(false)
let previewProbeTimer: ReturnType<typeof setInterval> | null = null
let previewProbeAbort: AbortController | null = null
let previewProbeGen = 0
const probesRegisteredPreview = computed(() => clarifyPreview.value || props.probeRegisteredPreview)

function stopRegisteredPreviewProbe() {
  if (previewProbeTimer) {
    clearInterval(previewProbeTimer)
    previewProbeTimer = null
  }
  previewProbeAbort?.abort()
  previewProbeAbort = null
  previewProbeGen++
}

async function probeRegisteredPreviews() {
  if (!probesRegisteredPreview.value) return
  const rid = String(props.runId || '').trim()
  const nid = String(props.nodeId || '').trim()
  if (!rid || !nid) {
    previewRegistered.value = false
    return
  }
  previewProbeAbort?.abort()
  const gen = ++previewProbeGen
  previewProbeAbort = new AbortController()
  try {
    const r = await api.nodePreviews(rid, nid, { signal: previewProbeAbort.signal })
    if (gen !== previewProbeGen) return
    previewRegistered.value = (r.ports || []).length > 0
  } catch (e) {
    if (gen !== previewProbeGen || isAbortError(e)) return
    // Keep last known registration on transient errors.
  }
}

watch(
  () => `${probesRegisteredPreview.value}|${props.runId}|${props.nodeId}|${props.hideAppPreview}`,
  () => {
    stopRegisteredPreviewProbe()
    previewRegistered.value = false
    if (props.hideAppPreview) return
    if (!probesRegisteredPreview.value) return
    void probeRegisteredPreviews()
    previewProbeTimer = setInterval(() => void probeRegisteredPreviews(), REGISTERED_PREVIEW_POLL_MS)
  },
  { immediate: true },
)

const effectiveRemoteKind = computed(() => {
  if (props.hideAppPreview) return 'off'
  if (clarifyPreview.value) {
    if (resolvedRemoteKind.value === 'public') return props.ports?.length ? 'public' : 'off'
    return clarifyStageRemoteKind(previewRegistered.value)
  }
  if (props.probeRegisteredPreview && previewRegistered.value && resolvedRemoteKind.value !== 'public') return 'app'
  return resolvedRemoteKind.value
})

const stageArtifacts = computed(() => expandStageArtifacts(props.artifacts, props.run, stageNode.value))
const effectivePin = computed(() =>
  resolveEffectivePreviewPin({
    previewArtifact: props.previewArtifact,
    artifacts: stageArtifacts.value,
    node: resolvedNode.value,
    nodeId: props.nodeId,
  }),
)
const autoPinEnabled = computed(() => isAutoPinStageNode(resolvedNode.value))
const gridArtifacts = computed(() =>
  stageGridArtifactsForNode(
    stageArtifacts.value,
    props.run,
    effectivePin.value,
    resolvedNode.value,
  ),
)
const showingNovnc = computed(() => activeTab.value === REACT_STAGE_TAB_NOVNC)
/** Artifact-only embed keeps the grid free of sandbox and app cards. */
const showFixedCards = computed(() => !props.hideAppPreview)
const showAppCard = computed(
  () =>
    showFixedCards.value &&
    (effectiveRemoteKind.value === 'app' || effectiveRemoteKind.value === 'public'),
)
const showGridCards = computed(() => showFixedCards.value || gridArtifacts.value.length > 0)
/** Fixed resources are visible and this run has no workflow artifact cards yet. */
const showNoArtifactHint = computed(
  () => showFixedCards.value && gridArtifacts.value.length === 0,
)
const fixedCards = computed(() => {
  if (!showFixedCards.value) return []
  const cards: { id: string; icon: string; title: string; meta: string }[] = [
    {
      id: 'ide',
      icon: 'stage-ide',
      title: t('pages.reactArtifactStage.fixedIdeTitle'),
      meta: t('pages.reactArtifactStage.fixedIdeMeta'),
    },
    {
      id: 'terminal',
      icon: 'stage-terminal',
      title: t('pages.reactArtifactStage.fixedTerminalTitle'),
      meta: t('pages.reactArtifactStage.fixedTerminalMeta'),
    },
    {
      id: 'browser',
      icon: 'stage-browser',
      title: t('pages.reactArtifactStage.fixedBrowserTitle'),
      meta: t('pages.reactArtifactStage.novncCardMeta'),
    },
  ]
  if (showAppCard.value) {
    cards.push({
      id: 'app',
      icon: 'globe',
      title: t('pages.reactArtifactStage.appCardTitle'),
      meta: t('pages.reactArtifactStage.appCardMeta'),
    })
  }
  cards.push({
    id: 'log',
    icon: 'stage-log',
    title: t('pages.reactArtifactStage.fixedLogTitle'),
    meta: t('pages.reactArtifactStage.fixedLogMeta'),
  })
  return cards
})
const fixedTabsBeforeApp = computed(() =>
  (['ide', 'terminal', 'browser'] as const).filter((id) => fixedOpen.value.includes(id)),
)
const fixedTabsAfterApp = computed(() =>
  (['log'] as const).filter((id) => fixedOpen.value.includes(id)),
)

function fixedTabLabel(id: string): string {
  if (id === 'ide') return t('pages.reactArtifactStage.fixedIdeTitle')
  if (id === 'terminal') return t('pages.reactArtifactStage.fixedTerminalTitle')
  if (id === 'browser') return t('pages.reactArtifactStage.fixedBrowserTitle')
  if (id === 'log') return t('pages.reactArtifactStage.fixedLogTitle')
  return id
}
function fixedTabIcon(id: string): string {
  if (id === 'ide') return 'stage-ide'
  if (id === 'terminal') return 'stage-terminal'
  if (id === 'browser') return 'stage-browser'
  if (id === 'log') return 'stage-log'
  return 'doc'
}

function artifactAnnotatable(a: Artifact | null): boolean {
  if (!a) return false
  const sel = selectedVersion(a)
  if (sel && !sel.latest) return false
  return canAnnotateStageArtifact(!!props.annotatable, a, props.nodeId)
}

function artifactReadonly(a: Artifact): boolean {
  const sel = selectedVersion(a)
  if (sel && !sel.latest) return true
  return !isOwnNodeArtifact(a, props.nodeId) || isHistoricalStageArtifact(a)
}

function artifactTitle(a: Artifact | null | undefined): string {
  if (!a) return ''
  const friendlyKey = artifactFriendlyNameKey(a.name)
  if (friendlyKey) return t(friendlyKey)
  const hist = parseHistoricalStageArtifact(a)
  if (!hist) return a.name
  return a.name.replace(/#iter-\d+$/, '') || a.name
}

function versionChoices(a: Artifact): ArtifactVersionChoice[] {
  return versions.choicesFor(a)
}

function showVersionChip(a: Artifact): boolean {
  return versionChoices(a).length >= 2
}

function selectedVersion(a: Artifact): ArtifactVersionChoice | null {
  const choices = versionChoices(a)
  if (!choices.length) return null
  const picked = selectedVersionIndex.value[a.name]
  return choices.find((c) => c.index === picked) || choices[choices.length - 1]
}

function versionChipLabel(choice: ArtifactVersionChoice): string {
  if (choice.latest) return t('pages.reactArtifactStage.versionChipLatest', { n: choice.index })
  return t('pages.reactArtifactStage.versionChip', { n: choice.index })
}

function currentChipLabel(a: Artifact): string {
  const sel = selectedVersion(a)
  return sel ? versionChipLabel(sel) : ''
}

function resolvedArtifact(a: Artifact | null): Artifact | null {
  if (!a) return null
  return versions.resolveVersionedArtifact(a, selectedVersion(a))
}

async function selectVersion(a: Artifact, choice: ArtifactVersionChoice) {
  if (!choice.available) return
  if (!choice.latest) {
    try {
      await versions.ensureVersionContent(a.id, choice.revision)
    } catch {
      return
    }
  }
  selectedVersionIndex.value = { ...selectedVersionIndex.value, [a.name]: choice.index }
  activatePreview(a.name)
}

const kindIcon: Record<string, string> = {
  html: 'dashboard',
  json: 'doc',
  markdown: 'doc',
  yaml: 'doc',
  image: 'artifact',
  text: 'doc',
}

function metaLine(a: Artifact): string {
  return t('pages.reactArtifactStage.metaNameKindTime', {
    name: artifactTechnicalDisplayName(a.name),
    kind: t(artifactKindLabelKey(a.kind)),
    time: relTime(a.updatedAt || a.createdAt),
  })
}

function textThumb(a: Artifact): { title: string; summary: string } | null {
  const thumb = summaryThumbs.value[a.id]
  return thumb?.kind === 'text' ? thumb : null
}

function htmlThumbContent(a: Artifact): string | null {
  const thumb = summaryThumbs.value[a.id]
  return thumb?.kind === 'html' ? thumb.html : null
}

function persistOpenState() {
  saveStageOpenState(props.runId, props.nodeId, {
    openNames: openNames.value,
    activeTab: activeTab.value,
    novncOpen: novncOpen.value,
    fixedOpen: fixedOpen.value,
  })
}

function applyRestoredOpenState(runId: string, nodeId: string, names: string[]) {
  const restored = restoreStageOpenState(loadStageOpenState(runId, nodeId), names)
  if (!restored) return false
  openNames.value = restored.openNames
  activeTab.value = restored.activeTab
  novncOpen.value = restored.novncOpen
  fixedOpen.value = [...(restored.fixedOpen || [])]
  const mounted = { ...fixedMounted.value }
  for (const id of fixedOpen.value) mounted[id] = true
  fixedMounted.value = mounted
  userMoved.value = true
  return true
}

function artifactByName(name: string): Artifact | null {
  return findArtifactByName(stageArtifacts.value, name)
}

function markUserMoved() {
  userMoved.value = true
}

function activatePreview(name: string) {
  const art = findArtifactByName(stageArtifacts.value, name)
  if (!art) return
  openNames.value = openStagePreviewTab(openNames.value, art.name)
  artifactView.value = art.name
  if (splitActive.value && drawerMode.value === 'resources') return
  activeTab.value = previewTabId(art.name)
  tabUnread.value = clearStageTabUnread(tabUnread.value, art.name)
}

function ensurePreviewTab(name: string, unread?: StageTabUnreadKind) {
  const art = findArtifactByName(stageArtifacts.value, name)
  if (!art) return
  openNames.value = openStagePreviewTab(openNames.value, art.name)
  if (unread && previewTabName(activeTab.value) !== art.name) {
    tabUnread.value = markStageTabUnread(tabUnread.value, art.name, unread)
  }
}

function openArtifact(a: Artifact) {
  markUserMoved()
  activatePreview(a.name)
}

function selectGridTab() {
  markUserMoved()
  activeTab.value = REACT_STAGE_TAB_GRID
}

function selectPreviewTab(name: string) {
  markUserMoved()
  activeTab.value = previewTabId(name)
  tabUnread.value = clearStageTabUnread(tabUnread.value, name)
}

function tabUnreadKind(name: string): StageTabUnreadKind | null {
  return tabUnread.value[name] || null
}

const FIXED_PANEL_IDS = ['ide', 'terminal', 'browser', 'log'] as const
type FixedPanelId = (typeof FIXED_PANEL_IDS)[number]

function isFixedPanel(id: string): id is FixedPanelId {
  return (FIXED_PANEL_IDS as readonly string[]).includes(id)
}

function closableTabIds(opts?: { includeApp?: boolean; names?: string[] }): string[] {
  const names = opts?.names || openNames.value
  const ids = names.map((n) => previewTabId(n))
  for (const id of ['ide', 'terminal', 'browser'] as const) {
    if (fixedOpen.value.includes(id)) ids.push(id)
  }
  if (opts?.includeApp || (novncOpen.value && showAppCard.value)) ids.push(REACT_STAGE_TAB_NOVNC)
  if (fixedOpen.value.includes('log')) ids.push('log')
  return ids
}

function neighborTab(closedId: string, ids: string[]): string {
  const remaining = ids.filter((id) => id !== closedId)
  if (!remaining.length) return REACT_STAGE_TAB_GRID
  const i = ids.indexOf(closedId)
  if (i < 0) return remaining[remaining.length - 1]
  return remaining[Math.min(Math.max(i, 0), remaining.length - 1)] ?? REACT_STAGE_TAB_GRID
}

function closePreview(name: string) {
  markUserMoved()
  if (splitActive.value) {
    openNames.value = closeStagePreviewTab(openNames.value, name)
    activeTab.value = REACT_STAGE_TAB_GRID
    return
  }
  const closedId = previewTabId(name)
  const before = closableTabIds()
  const wasActive = previewTabName(activeTab.value) === name
  openNames.value = closeStagePreviewTab(openNames.value, name)
  if (wasActive) activeTab.value = neighborTab(closedId, before)
}

function openFixed(id: FixedPanelId) {
  markUserMoved()
  fixedMounted.value = { ...fixedMounted.value, [id]: true }
  if (!fixedOpen.value.includes(id)) fixedOpen.value = [...fixedOpen.value, id]
  activeTab.value = id
}

function openAppTab() {
  if (!showAppCard.value) return
  markUserMoved()
  novncOpen.value = true
  activeTab.value = REACT_STAGE_TAB_NOVNC
}

function onFixedCard(id: string) {
  if (id === 'app') {
    openAppTab()
    return
  }
  if (isFixedPanel(id)) openFixed(id)
}

function selectFixedTab(id: string) {
  markUserMoved()
  activeTab.value = id
}

function selectNovncTab() {
  markUserMoved()
  activeTab.value = REACT_STAGE_TAB_NOVNC
}

function closeFixed(id: string) {
  if (!isFixedPanel(id)) return
  markUserMoved()
  if (splitActive.value) {
    fixedOpen.value = fixedOpen.value.filter((x) => x !== id)
    activeTab.value = REACT_STAGE_TAB_GRID
    return
  }
  const before = closableTabIds()
  const wasActive = activeTab.value === id
  fixedOpen.value = fixedOpen.value.filter((x) => x !== id)
  if (wasActive) activeTab.value = neighborTab(id, before)
}

function closeNovnc() {
  markUserMoved()
  if (splitActive.value) {
    novncOpen.value = false
    activeTab.value = REACT_STAGE_TAB_GRID
    return
  }
  const before = closableTabIds({ includeApp: true })
  const wasActive = activeTab.value === REACT_STAGE_TAB_NOVNC
  novncOpen.value = false
  if (wasActive) activeTab.value = neighborTab(REACT_STAGE_TAB_NOVNC, before)
}

function resetSplitHome() {
  openNames.value = []
  activeTab.value = REACT_STAGE_TAB_GRID
  novncOpen.value = false
  fixedOpen.value = []
  fixedMounted.value = {}
  userMoved.value = false
  tabUnread.value = {}
  resourceView.value = 'home'
  artifactView.value = 'home'
  autoPinFingerprints.value = null
}

function syncDrawerMode(next: 'resources' | 'artifacts') {
  if (next === 'resources') {
    const view = resourceView.value
    if (view === 'home') {
      activeTab.value = REACT_STAGE_TAB_GRID
      return
    }
    if (view === 'app') {
      novncOpen.value = true
      activeTab.value = REACT_STAGE_TAB_NOVNC
      return
    }
    fixedMounted.value = { ...fixedMounted.value, [view]: true }
    if (!fixedOpen.value.includes(view)) fixedOpen.value = [...fixedOpen.value, view]
    activeTab.value = view
    return
  }
  const name = artifactView.value
  if (!name || name === 'home' || !artifactByName(name)) {
    artifactView.value = 'home'
    activeTab.value = REACT_STAGE_TAB_GRID
    return
  }
  activatePreview(name)
}

function onRemotePick(payload: AppPreviewPickPayload) {
  emit('pick', payload)
}

const showingGrid = computed(() => activeTab.value === REACT_STAGE_TAB_GRID)
const activePreviewName = computed(() => previewTabName(activeTab.value))
const drawer = computed(() => peekConversationDrawer(props.runId))
const splitActive = computed(() => !!drawer.value?.split && !props.hideAppPreview)
const drawerMode = computed(() => (splitActive.value ? drawer.value?.mode ?? null : null))
const showResourceCards = computed(() => showFixedCards.value && (!splitActive.value || drawerMode.value === 'resources'))
const showArtifactCards = computed(() => !splitActive.value || drawerMode.value === 'artifacts')
const showWorkflow = computed(
  () => splitActive.value && drawerMode.value === 'artifacts' && showingGrid.value && (props.run?.nodes?.length || 0) > 0,
)
const showSplitArtifactEmpty = computed(
  () => splitActive.value && drawerMode.value === 'artifacts' && showingGrid.value && gridArtifacts.value.length === 0,
)
const showStageTabs = computed(() => !splitActive.value || activeTab.value !== REACT_STAGE_TAB_GRID)
const displayedPreviewNames = computed(() => {
  if (!splitActive.value) return openNames.value
  if (drawerMode.value !== 'artifacts') return []
  const name = previewTabName(activeTab.value)
  return name && openNames.value.includes(name) ? [name] : []
})
const displayedFixedBefore = computed(() => {
  if (!splitActive.value) return fixedTabsBeforeApp.value
  if (drawerMode.value !== 'resources') return []
  return fixedTabsBeforeApp.value.filter((id) => id === activeTab.value)
})
const displayedNovnc = computed(() => {
  if (!novncOpen.value || !showAppCard.value) return false
  if (!splitActive.value) return true
  return drawerMode.value === 'resources' && activeTab.value === REACT_STAGE_TAB_NOVNC
})
const displayedFixedAfter = computed(() => {
  if (!splitActive.value) return fixedTabsAfterApp.value
  if (drawerMode.value !== 'resources') return []
  return fixedTabsAfterApp.value.filter((id) => id === activeTab.value)
})

watch(
  () => {
    const arts = stageArtifacts.value
    return [effectivePin.value, arts.map((a) => a.name)] as const
  },
  ([pin, names], prev) => {
    const nameSet = new Set(names)
    const kept = openNames.value.filter((n) => nameSet.has(n))
    if (kept.length !== openNames.value.length) {
      const gone = openNames.value.find((n) => !nameSet.has(n))
      if (gone && previewTabName(activeTab.value) === gone) {
        const before = closableTabIds({ names: openNames.value })
        activeTab.value = neighborTab(previewTabId(gone), before)
      }
      openNames.value = kept
      const nextUnread = { ...tabUnread.value }
      for (const key of Object.keys(nextUnread)) {
        if (!nameSet.has(key)) delete nextUnread[key]
      }
      tabUnread.value = nextUnread
    }
    const pinName = String(pin || '').trim()
    if (!pinName || !nameSet.has(pinName)) return
    // Detect pin/appearance events without letting userMoved suppress the signal.
    const pinEvent = shouldActivatePinnedPreview(
      pinName,
      names,
      prev?.[0],
      prev?.[1] !== undefined ? [...prev[1]] : undefined,
      false,
    )
    if (!pinEvent) return
    // Split homes stay on the workflow until the user opens a card, or the
    // conversation asks for a named artifact. An automatic pin must not
    // replace that home or the preview this mode already remembers.
    if (splitActive.value) return
    const canFocus = shouldFocusPinnedOrAutoTab({
      userMoved: userMoved.value,
      activeTab: activeTab.value,
    })
    if (canFocus) {
      activatePreview(pinName)
      return
    }
    // Mid-session only: never re-open a restored-closed pin on first paint.
    if (prev !== undefined) {
      ensurePreviewTab(pinName, 'new')
    }
  },
  { immediate: true },
)

/** Clarify: auto-pin visible own-node artifacts on create/overwrite. */
watch(
  () => {
    if (!autoPinEnabled.value) return 'off'
    const own = String(props.nodeId || '').trim()
    const fps = stageArtifacts.value
      .filter(
        (a) =>
          isVisibleAutoPinArtifact(a, stageArtifacts.value, props.run) &&
          isOwnNodeArtifact(a, own || undefined),
      )
      .map((a) => `${a.name}\0${artifactFingerprint(a)}`)
      .join('\n')
    return `${props.runId}\0${props.nodeId}\0${fps}`
  },
  (key, prevKey) => {
    if (!autoPinEnabled.value) {
      autoPinFingerprints.value = null
      return
    }
    const own = String(props.nodeId || '').trim()
    const visible = stageArtifacts.value.filter(
      (a) =>
        isVisibleAutoPinArtifact(a, stageArtifacts.value, props.run) &&
        isOwnNodeArtifact(a, own || undefined),
    )
    const nextMap = buildArtifactFingerprintMap(visible)
    const scope = key.split('\0').slice(0, 2).join('\0')
    const prevScope = prevKey ? prevKey.split('\0').slice(0, 2).join('\0') : ''
    const prevMap = autoPinFingerprints.value
    autoPinFingerprints.value = nextMap
    // First observation or run/node switch: seed only (do not open every existing product).
    if (!prevMap || scope !== prevScope) return
    const { created, updated } = diffArtifactFingerprints(prevMap, nextMap)
    if (!created.length && !updated.length) return
    if (splitActive.value) return

    const pinName = String(effectivePin.value || '').trim()
    const changed = [...created, ...updated]
    for (const name of created) {
      ensurePreviewTab(name, 'new')
    }
    for (const name of updated) {
      ensurePreviewTab(name, 'updated')
    }

    const canFocus = shouldFocusPinnedOrAutoTab({
      userMoved: userMoved.value,
      activeTab: activeTab.value,
      onlyIdle: true,
    })
    if (!canFocus) return
    // Prefer explicit set_artifact_preview pin when it is among this batch.
    const focusName =
      (pinName && changed.includes(pinName) ? pinName : '') ||
      changed[changed.length - 1] ||
      ''
    if (focusName) activatePreview(focusName)
  },
  { immediate: true },
)

watch(
  () =>
    gridArtifacts.value
      .filter((a) => a.kind === 'html' || a.kind === 'json' || wantsTextSummaryThumb(a))
      .map((a) => artifactFingerprint(a))
      .join('|'),
  async () => {
    const gen = ++summaryThumbGen
    summaryThumbAbort?.abort()
    const ac = new AbortController()
    summaryThumbAbort = ac
    const thumbArts = gridArtifacts.value.filter(
      (a) => a.kind === 'html' || a.kind === 'json' || wantsTextSummaryThumb(a),
    )
    const next: Record<string, StageCardThumb> = {}
    for (const a of thumbArts) {
      const fp = artifactFingerprint(a)
      let content: string | undefined
      if (typeof a.content === 'string') {
        content = a.content
        summaryThumbFp[a.id] = fp
      } else if (props.inlineContent) {
        continue
      } else if (summaryThumbFp[a.id] === fp && summaryThumbs.value[a.id] !== undefined) {
        next[a.id] = summaryThumbs.value[a.id]
        continue
      } else {
        try {
          const full = props.token
            ? await publicGateApi.artifactContent(props.token, a.name, ac.signal)
            : await api.artifactContent(a.id, { signal: ac.signal })
          if (gen !== summaryThumbGen) return
          content = full.content ?? ''
          summaryThumbFp[a.id] = fp
        } catch (e) {
          if (isAbortError(e) || gen !== summaryThumbGen) return
          if (summaryThumbs.value[a.id] !== undefined) next[a.id] = summaryThumbs.value[a.id]
          continue
        }
      }
      const thumb = buildStageCardThumb(a, content)
      if (thumb) next[a.id] = thumb
    }
    if (gen !== summaryThumbGen) return
    for (const id of Object.keys(summaryThumbFp)) {
      if (!thumbArts.some((a) => a.id === id)) delete summaryThumbFp[id]
    }
    summaryThumbs.value = next
  },
  { immediate: true },
)

watch(
  [openNames, activeTab, novncOpen, fixedOpen, () => props.runId, () => props.nodeId],
  () => {
    persistOpenState()
  },
  { deep: true },
)

watch(
  () => `${props.runId}|${props.nodeId}`,
  (key, prev) => {
    if (!prev || key === prev) return
    if (splitActive.value) {
      const [oldRun, oldNode] = prev.split('|')
      clearStageOpenState(oldRun, oldNode)
      clearStageOpenState(props.runId, props.nodeId)
      resetSplitHome()
      return
    }
    const rid = String(props.runId || '').trim()
    const nid = String(props.nodeId || '').trim()
    if (!rid || !nid) {
      openNames.value = []
      activeTab.value = REACT_STAGE_TAB_GRID
      novncOpen.value = false
      fixedOpen.value = []
      fixedMounted.value = {}
      userMoved.value = false
      tabUnread.value = {}
      autoPinFingerprints.value = null
      return
    }
    if (
      !applyRestoredOpenState(
        rid,
        nid,
        stageArtifacts.value.map((a) => a.name),
      )
    ) {
      openNames.value = []
      activeTab.value = REACT_STAGE_TAB_GRID
      novncOpen.value = false
      fixedOpen.value = []
      fixedMounted.value = {}
      userMoved.value = false
      tabUnread.value = {}
    }
    autoPinFingerprints.value = null
  },
)

watch(
  () => effectiveRemoteKind.value,
  (kind) => {
    if (kind !== 'app' && kind !== 'public') {
      if (!novncOpen.value) return
      const before = closableTabIds({ includeApp: true })
      const wasActive = activeTab.value === REACT_STAGE_TAB_NOVNC
      novncOpen.value = false
      if (wasActive) {
        activeTab.value = splitActive.value ? REACT_STAGE_TAB_GRID : neighborTab(REACT_STAGE_TAB_NOVNC, before)
      }
      return
    }
    // Show the app tab once a preview is registered; never steal focus after the user moved.
    novncOpen.value = true
    if (splitActive.value) return
    if (userMoved.value) return
    if (activeTab.value === REACT_STAGE_TAB_GRID) {
      activeTab.value = REACT_STAGE_TAB_NOVNC
    }
  },
  { immediate: true },
)

watch(activeTab, (tab) => {
  if (!splitActive.value) return
  if (tab === 'ide' || tab === 'terminal' || tab === 'browser' || tab === 'log') {
    resourceView.value = tab
    return
  }
  if (tab === REACT_STAGE_TAB_NOVNC) {
    resourceView.value = 'app'
    return
  }
  const name = previewTabName(tab)
  if (name) {
    artifactView.value = name
    return
  }
  if (tab !== REACT_STAGE_TAB_GRID) return
  if (drawerMode.value === 'resources') resourceView.value = 'home'
  else if (drawerMode.value === 'artifacts') artifactView.value = 'home'
})
watch(drawerMode, (mode, prev) => {
  if (!splitActive.value || !mode || mode === prev) return
  syncDrawerMode(mode)
})
watch(
  () => drawer.value?.resetEpoch ?? 0,
  (epoch, prev) => {
    if (!splitActive.value || prev == null || epoch === prev) return
    clearStageOpenState(props.runId, props.nodeId)
    resetSplitHome()
  },
)
onMounted(() => {
  const mode = drawerMode.value
  if (splitActive.value && (mode === 'resources' || mode === 'artifacts')) syncDrawerMode(mode)
})

let sandboxGen = 0
watch(
  () => `${props.runId}|${props.nodeId}|${props.hideAppPreview ? 1 : 0}`,
  async () => {
    const gen = ++sandboxGen
    sandboxView.value = null
    if (props.hideAppPreview) return
    const rid = String(props.runId || '').trim()
    const nid = String(props.nodeId || '').trim()
    if (!rid || !nid) return
    sandboxLoading.value = true
    try {
      const sbx = await api.getRunNodeSandbox(rid, nid)
      if (gen !== sandboxGen) return
      sandboxView.value = sbx
    } catch (e) {
      if (gen !== sandboxGen || isAbortError(e)) return
      sandboxView.value = null
    } finally {
      if (gen === sandboxGen) sandboxLoading.value = false
    }
  },
  { immediate: true },
)

watch(
  () => stageArtifacts.value.map((a) => `${a.id}:${a.revision ?? ''}`).join('|'),
  () => {
    for (const a of stageArtifacts.value) {
      if (a.id) void versions.ensureVersions(a.id, undefined, true)
    }
  },
  { immediate: true },
)

watch(
  () =>
    stageArtifacts.value
      .map((a) => `${a.name}:${versionChoices(a).map((c) => `${c.index}:${c.available ? '1' : '0'}`).join(',')}`)
      .join('|'),
  () => {
    const next = { ...selectedVersionIndex.value }
    let changed = false
    for (const a of stageArtifacts.value) {
      const choices = versionChoices(a)
      if (choices.length < 2) continue
      const current = next[a.name]
      if (!choices.some((c) => c.index === current && c.available)) {
        next[a.name] = choices[choices.length - 1].index
        changed = true
      }
    }
    if (changed) selectedVersionIndex.value = next
  },
)

watch(
  () => props.runId,
  (runId, _, onCleanup) => {
    if (!runId) return
    onCleanup(
      registerStageLinks(runId, {
        hasArtifact: (name) => !!artifactByName(name),
        openArtifact: (name) => {
          markUserMoved()
          activatePreview(name)
        },
        canOpenPreview: () => showAppCard.value || effectiveRemoteKind.value === 'sandbox',
        openPreview: () => {
          if (showAppCard.value) {
            openAppTab()
            return
          }
          if (effectiveRemoteKind.value === 'sandbox') openFixed('browser')
        },
      }),
    )
  },
  { immediate: true },
)

onBeforeUnmount(() => {
  stopRegisteredPreviewProbe()
  summaryThumbGen++
  summaryThumbAbort?.abort()
})
</script>

<template>
  <div class="artifact-stage-surface flex h-full min-h-0 flex-col bg-base" data-testid="react-artifact-stage" :data-drawer-mode="splitActive ? (drawerMode || 'closed') : undefined">
    <div
      v-show="showStageTabs"
      class="artifact-stage-tabs flex shrink-0 items-center gap-1 overflow-x-auto border-b border-line px-2 py-1.5 max-md:px-1.5 max-md:py-1"
      data-testid="react-artifact-tabs"
      role="tablist"
    >
      <button
        v-if="!splitActive"
        type="button"
        role="tab"
        class="inline-flex max-w-[200px] items-center gap-1.5 rounded-md px-2.5 py-1 text-[12px] transition max-md:px-2 max-md:py-0.5 max-md:text-[11px]"
        :class="
          showingGrid
            ? 'bg-elevated text-txt'
            : 'text-txt3 hover:bg-elevated/60 hover:text-txt2'
        "
        :aria-selected="showingGrid ? 'true' : 'false'"
        data-testid="react-artifact-tab-grid"
        @click="selectGridTab"
      >
        <Icon name="dashboard" :size="13" />
        <span class="truncate">{{ t('pages.reactArtifactStage.workflowTab') }}</span>
      </button>
      <div
        v-for="name in displayedPreviewNames"
        :key="name"
        class="group inline-flex max-w-[220px] items-center gap-1 rounded-md text-[12px] transition max-md:text-[11px]"
        :class="
          activePreviewName === name
            ? 'bg-elevated text-txt'
            : 'text-txt3 hover:bg-elevated/60 hover:text-txt2'
        "
      >
        <button
          type="button"
          role="tab"
          class="inline-flex min-w-0 items-center gap-1.5 py-1 pl-2.5 pr-1"
          :aria-selected="activePreviewName === name ? 'true' : 'false'"
          :data-testid="'react-artifact-tab-' + name"
          @click="selectPreviewTab(name)"
        >
          <Icon :name="kindIcon[artifactByName(name)?.kind || ''] || 'doc'" :size="13" />
          <span class="truncate">{{ artifactTitle(artifactByName(name)) }}</span>
          <span
            v-if="tabUnreadKind(name)"
            class="shrink-0 rounded border border-line px-1 py-px text-[10px] leading-none text-txt2"
            :data-testid="'react-artifact-tab-unread-' + name"
            :data-unread="tabUnreadKind(name)"
          >{{
            tabUnreadKind(name) === 'new'
              ? t('pages.reactArtifactStage.tabBadgeNew')
              : t('pages.reactArtifactStage.tabBadgeUpdated')
          }}</span>
        </button>
        <button
          type="button"
          class="mr-1 inline-flex h-5 w-5 shrink-0 items-center justify-center rounded text-txt3 hover:bg-overlay hover:text-txt"
          :aria-label="t('pages.reactArtifactStage.closeTab')"
          :title="t('pages.reactArtifactStage.closeTab')"
          :data-testid="'react-artifact-tab-close-' + name"
          @click.stop="closePreview(name)"
        >
          <Icon name="close" :size="12" />
        </button>
      </div>
      <div
        v-for="id in displayedFixedBefore"
        :key="id"
        class="group inline-flex max-w-[220px] items-center gap-1 rounded-md text-[12px] transition max-md:text-[11px]"
        :class="
          activeTab === id
            ? 'bg-elevated text-txt'
            : 'text-txt3 hover:bg-elevated/60 hover:text-txt2'
        "
      >
        <button
          type="button"
          role="tab"
          class="inline-flex min-w-0 items-center gap-1.5 py-1 pl-2.5 pr-1"
          :aria-selected="activeTab === id ? 'true' : 'false'"
          :data-testid="'react-artifact-tab-' + id"
          @click="selectFixedTab(id)"
        >
          <Icon :name="fixedTabIcon(id)" :size="13" />
          <span class="truncate">{{ fixedTabLabel(id) }}</span>
        </button>
        <button
          type="button"
          class="mr-1 inline-flex h-5 w-5 shrink-0 items-center justify-center rounded text-txt3 hover:bg-overlay hover:text-txt"
          :aria-label="t('pages.reactArtifactStage.closeTab')"
          :title="t('pages.reactArtifactStage.closeTab')"
          :data-testid="'react-artifact-tab-close-' + id"
          @click.stop="closeFixed(id)"
        >
          <Icon name="close" :size="12" />
        </button>
      </div>
      <div
        v-if="displayedNovnc"
        class="group inline-flex max-w-[220px] items-center gap-1 rounded-md text-[12px] transition max-md:text-[11px]"
        :class="
          showingNovnc
            ? 'bg-elevated text-txt'
            : 'text-txt3 hover:bg-elevated/60 hover:text-txt2'
        "
      >
        <button
          type="button"
          role="tab"
          class="inline-flex min-w-0 items-center gap-1.5 py-1 pl-2.5 pr-1"
          :aria-selected="showingNovnc ? 'true' : 'false'"
          data-testid="react-artifact-tab-novnc"
          @click="selectNovncTab"
        >
          <Icon name="globe" :size="13" />
          <span class="truncate">{{ t('pages.reactArtifactStage.appTab') }}</span>
        </button>
        <button
          type="button"
          class="mr-1 inline-flex h-5 w-5 shrink-0 items-center justify-center rounded text-txt3 hover:bg-overlay hover:text-txt"
          :aria-label="t('pages.reactArtifactStage.closeTab')"
          :title="t('pages.reactArtifactStage.closeTab')"
          data-testid="react-artifact-tab-close-novnc"
          @click.stop="closeNovnc"
        >
          <Icon name="close" :size="12" />
        </button>
      </div>
      <div
        v-for="id in displayedFixedAfter"
        :key="id"
        class="group inline-flex max-w-[220px] items-center gap-1 rounded-md text-[12px] transition max-md:text-[11px]"
        :class="
          activeTab === id
            ? 'bg-elevated text-txt'
            : 'text-txt3 hover:bg-elevated/60 hover:text-txt2'
        "
      >
        <button
          type="button"
          role="tab"
          class="inline-flex min-w-0 items-center gap-1.5 py-1 pl-2.5 pr-1"
          :aria-selected="activeTab === id ? 'true' : 'false'"
          :data-testid="'react-artifact-tab-' + id"
          @click="selectFixedTab(id)"
        >
          <Icon :name="fixedTabIcon(id)" :size="13" />
          <span class="truncate">{{ fixedTabLabel(id) }}</span>
        </button>
        <button
          type="button"
          class="mr-1 inline-flex h-5 w-5 shrink-0 items-center justify-center rounded text-txt3 hover:bg-overlay hover:text-txt"
          :aria-label="t('pages.reactArtifactStage.closeTab')"
          :title="t('pages.reactArtifactStage.closeTab')"
          :data-testid="'react-artifact-tab-close-' + id"
          @click.stop="closeFixed(id)"
        >
          <Icon name="close" :size="12" />
        </button>
      </div>
    </div>

    <div v-show="showingGrid" class="min-h-0 flex-1 overflow-x-hidden overflow-y-auto p-4" data-testid="react-artifact-grid">
      <div
        v-if="!showGridCards"
        class="flex h-full min-h-[160px] flex-col items-center justify-center text-center text-[12px] text-txt3"
        data-testid="react-artifact-grid-empty"
      >
        <Icon name="artifact" :size="26" class="mb-2 opacity-40" />
        {{ t('pages.reactArtifactStage.gridEmpty') }}
      </div>
      <template v-else>
      <StageWorkflow v-if="showWorkflow" :run="run" :current-node-id="nodeId" />
      <p
        v-if="showSplitArtifactEmpty"
        class="py-6 text-center text-[12px] text-txt3"
        data-testid="react-artifact-split-empty"
      >{{ t('pages.reactArtifactStage.gridEmpty') }}</p>
      <div
        v-if="(showResourceCards && fixedCards.length) || (showArtifactCards && gridArtifacts.length)"
        class="grid grid-cols-[repeat(auto-fill,minmax(176px,1fr))] gap-3"
      >
        <template v-if="showResourceCards">
        <button
          v-for="card in fixedCards"
          :key="card.id"
          type="button"
          class="artifact-card artifact-resource-card overflow-hidden rounded-lg border border-line bg-accent-dim/40 text-left transition hover:border-line-strong"
          :data-testid="'react-artifact-card-' + card.id"
          @click="onFixedCard(card.id)"
        >
          <div
            class="artifact-card-cover relative flex h-[110px] items-center justify-center overflow-hidden bg-accent-dim/70"
            :class="card.id === 'app' ? 'text-txt3' : undefined"
          >
            <span
              class="absolute left-2 top-2 z-[1] rounded-[4px] bg-accent-dim px-[5px] py-[3px] text-[10px] font-semibold leading-none text-accent"
              data-testid="react-artifact-kind-badge"
            >{{ t('pages.reactArtifactStage.resourceBadge') }}</span>
            <ResourceCardIllustration v-if="card.id !== 'app'" :kind="card.id" />
            <Icon
              v-else
              :name="card.icon"
              :size="card.id === 'app' ? 28 : 30"
              :class="card.id === 'app' ? 'opacity-50' : 'text-txt2'"
            />
          </div>
          <div class="px-2.5 py-2">
            <div class="artifact-card-title truncate text-[12px] font-medium text-txt">{{ card.title }}</div>
            <div class="artifact-card-meta mt-0.5 truncate text-[11px] text-txt3">{{ card.meta }}</div>
          </div>
        </button>
        </template>
        <template v-if="showArtifactCards">
        <div
          v-for="a in gridArtifacts"
          :key="a.id"
          role="button"
          tabindex="0"
          class="artifact-card artifact-file-card overflow-hidden rounded-lg border border-line bg-surface text-left transition hover:border-line-strong"
          :data-testid="'react-artifact-card-' + a.name"
          @click="openArtifact(a)"
          @keydown.enter.prevent="openArtifact(a)"
        >
          <div class="artifact-card-cover relative h-[110px] overflow-hidden bg-elevated">
            <span
              class="absolute left-2 top-2 z-[1] rounded-[4px] bg-ok/15 px-[5px] py-[3px] text-[10px] font-semibold leading-none text-ok"
              data-testid="react-artifact-kind-badge"
            >{{ t('pages.reactArtifactStage.artifactBadge') }}</span>
            <span class="artifact-paper-corner" aria-hidden="true" />
            <div
              v-if="textThumb(a)"
              class="artifact-card-summary pointer-events-none h-full overflow-hidden border-b border-line px-3 pb-2 pt-7"
              data-testid="react-artifact-card-summary"
            >
              <div
                v-if="textThumb(a)?.title"
                class="line-clamp-2 text-[12px] font-semibold leading-snug text-txt"
              >{{ textThumb(a)?.title }}</div>
              <div
                v-if="textThumb(a)?.summary"
                class="mt-1.5 line-clamp-4 text-[11px] leading-snug text-txt2"
              >{{ textThumb(a)?.summary }}</div>
            </div>
            <HtmlPreview
              v-else-if="htmlThumbContent(a)"
              :html="htmlThumbContent(a) || ''"
              mode="demo"
              :enlargeable="false"
              class="pointer-events-none h-full w-full"
            />
            <div v-else class="flex h-full items-center justify-center text-txt3">
              <Icon :name="kindIcon[a.kind] || 'artifact'" :size="28" class="opacity-50" />
            </div>
          </div>
          <div class="px-2.5 py-2">
            <div class="flex items-center gap-1.5">
              <div class="artifact-card-title min-w-0 truncate text-[12px] font-medium text-txt" :title="artifactTitle(a)">{{ artifactTitle(a) }}</div>
              <span
                v-if="artifactReadonly(a)"
                class="shrink-0 rounded border border-line px-1 py-px text-[10px] text-txt3"
                data-testid="react-artifact-card-readonly"
              >{{ t('pages.reactArtifactStage.readonlyBadge') }}</span>
            </div>
            <div class="mt-0.5 flex items-center justify-between gap-1">
              <div class="artifact-card-meta min-w-0 truncate text-[11px] text-txt3">{{ metaLine(a) }}</div>
              <ArtifactVersionSelect
                v-if="showVersionChip(a)"
                :choices="versionChoices(a)"
                :selected-index="selectedVersion(a)?.index"
                :current-label="currentChipLabel(a)"
                :menu-aria-label="t('pages.reactArtifactStage.versionMenu')"
                chip-test-id="react-artifact-version-chip"
                :button-test-id="'react-artifact-version-chip-btn-' + a.name"
                menu-test-id="react-artifact-version-menu"
                option-test-id-prefix="react-artifact-version-option-v"
                :label-for="versionChipLabel"
                @select="selectVersion(a, $event)"
              />
            </div>
          </div>
        </div>
        </template>
      </div>
      <div
        v-if="showNoArtifactHint && !splitActive"
        class="mt-4 rounded-lg border border-dashed border-line px-3 py-[22px] text-center"
        data-testid="react-artifact-grid-empty-hint"
      >
        <div class="text-[12px] font-semibold text-txt2">{{ t('pages.reactArtifactStage.workflowTab') }}</div>
        <p class="mt-1.5 text-[12px] text-txt3">{{ t('pages.reactArtifactStage.gridEmpty') }}</p>
      </div>
      </template>
    </div>

    <div
      v-for="name in openNames"
      v-show="activePreviewName === name"
      :key="name"
      class="flex min-h-0 flex-1 flex-col"
      :data-testid="'react-artifact-preview-' + name"
    >
      <ArtifactPreview
        v-if="resolvedArtifact(artifactByName(name))"
        :artifact="resolvedArtifact(artifactByName(name))"
        :artifacts="stageArtifacts"
        :run-id="runId"
        hide-delete
        hide-version-chip
        :share-token="token"
        :annotatable="artifactAnnotatable(artifactByName(name))"
        class="min-h-0 flex-1"
      />
    </div>

    <div
      v-if="fixedMounted.ide"
      v-show="activeTab === 'ide'"
      class="flex min-h-0 flex-1 flex-col"
      data-testid="react-artifact-preview-ide"
    >
      <SandboxStageSurface
        kind="ide"
        :sandbox="sandboxView"
        :loading="sandboxLoading"
        :active="activeTab === 'ide'"
        class="min-h-0 flex-1"
      />
    </div>
    <div
      v-if="fixedMounted.terminal"
      v-show="activeTab === 'terminal'"
      class="flex min-h-0 flex-1 flex-col"
      data-testid="react-artifact-preview-terminal"
    >
      <SandboxStageSurface
        kind="terminal"
        :sandbox="sandboxView"
        :loading="sandboxLoading"
        :active="activeTab === 'terminal'"
        class="min-h-0 flex-1"
      />
    </div>
    <div
      v-if="fixedMounted.browser"
      v-show="activeTab === 'browser'"
      class="flex min-h-0 flex-1 flex-col"
      data-testid="react-artifact-preview-browser"
    >
      <NovncPreviewPanel
        v-if="sandboxId"
        :sandbox-id="sandboxId"
        console
        fill
        :inspectable="annotatable"
        @pick="onRemotePick"
      />
      <div
        v-else
        class="flex h-full flex-col items-center justify-center p-6 text-center text-[12px] text-txt3"
        data-testid="react-artifact-browser-missing"
      >
        <Icon name="globe" :size="26" class="mb-2 opacity-40" />
        {{ sandboxLoading ? t('pages.appPreview.loading') : t('pages.reactArtifactStage.novncMissing') }}
      </div>
    </div>
    <div
      v-if="novncOpen && showAppCard"
      v-show="showingNovnc"
      class="flex min-h-0 flex-1 flex-col"
      data-testid="react-artifact-preview-novnc"
    >
      <AppPreviewPanel
        v-if="effectiveRemoteKind === 'app'"
        :run-id="runId"
        :node-id="nodeId"
        fill
        :show-feedback="false"
        @pick="onRemotePick"
        @staged-pick="emit('stagedPick', $event)"
      />
      <PublicAppPreviewPanel
        v-else-if="effectiveRemoteKind === 'public'"
        :token="token"
        :ports="ports"
        :active="publicActive"
        :mobile="publicMobile"
        fill
        @pick="onRemotePick"
        @staged-pick="emit('stagedPick', $event)"
      />
    </div>
    <div
      v-if="fixedMounted.log"
      v-show="activeTab === 'log'"
      class="flex min-h-0 flex-1 flex-col"
      data-testid="react-artifact-preview-log"
    >
      <SandboxStageSurface
        kind="log"
        :run-id="runId"
        :node-id="nodeId"
        :loading="sandboxLoading"
        :active="activeTab === 'log'"
        class="min-h-0 flex-1"
      />
    </div>
  </div>
</template>

<style scoped>
.artifact-stage-surface {
  --ra-bg: light-dark(#f8f9fc, #181a21);
  --ra-surface: light-dark(#fff, #242630);
  --ra-cover: light-dark(#f4f4fa, #292b38);
  --ra-ink: light-dark(#262934, #eeeff6);
  --ra-muted: light-dark(#7c8193, #a5abbd);
  --ra-line: light-dark(#e5e8f0, #3b3f4e);
  --ra-accent: light-dark(#7b67da, #b3a3ef);
  --ra-green: light-dark(#4a9271, #88c6a6);
  --ra-specular: light-dark(#ffffffdf, #ffffff15);
  --ra-shadow: light-dark(#27334d0b, #00000026);
  --ra-shadow-deep: light-dark(#27334d13, #00000040);
  --ra-fx: .7;
  --ra-glow: color-mix(in srgb, var(--ra-accent) calc(var(--ra-fx) * 16%), transparent);
  --ra-ease: cubic-bezier(.22, 1, .36, 1);
  background: var(--ra-bg);
}
.artifact-stage-tabs { border-color: var(--ra-line); }
.artifact-stage-tabs > button, .artifact-stage-tabs > div { color: var(--ra-muted); }
.artifact-stage-tabs > .bg-elevated {
  color: var(--ra-ink);
  background: var(--ra-surface);
  box-shadow: inset 0 0 0 1px var(--ra-line), 0 1px 2px var(--ra-shadow);
}
.artifact-card {
  --ra-card-hover: 0;
  border-color: var(--ra-line);
  background: var(--ra-surface);
  box-shadow: inset 0 1px 0 var(--ra-specular), 0 2px 3px var(--ra-shadow);
  transition: transform 350ms var(--ra-ease), box-shadow 350ms var(--ra-ease), border-color 220ms;
}
.artifact-card:is(:hover, :focus-visible) {
  --ra-card-hover: 1;
  transform: translateY(-2px);
  border-color: color-mix(in srgb, var(--ra-accent) 30%, var(--ra-line));
  box-shadow: inset 0 1px 0 var(--ra-specular), 0 3px 5px var(--ra-shadow), 0 10px 20px var(--ra-shadow-deep);
}
.artifact-card:focus-visible {
  outline: 2px solid color-mix(in srgb, var(--ra-accent) 55%, transparent);
  outline-offset: 3px;
}
.artifact-card-cover {
  background: radial-gradient(ellipse at 50% 110%, var(--ra-glow), transparent 70%), linear-gradient(145deg, var(--ra-surface), var(--ra-cover));
  box-shadow: inset 0 -1px 0 color-mix(in srgb, var(--ra-line) 65%, transparent);
}
.artifact-card-title { color: var(--ra-ink); }
.artifact-card-meta { color: var(--ra-muted); }
.artifact-card [data-testid="react-artifact-kind-badge"] {
  border: 1px solid color-mix(in srgb, var(--ra-accent) 9%, transparent);
  color: var(--ra-accent);
  background: color-mix(in srgb, var(--ra-accent) 9%, var(--ra-surface));
}
.artifact-file-card [data-testid="react-artifact-kind-badge"] {
  border-color: color-mix(in srgb, var(--ra-green) 10%, transparent);
  color: var(--ra-green);
  background: color-mix(in srgb, var(--ra-green) 9%, var(--ra-surface));
}
.artifact-card-summary { border-color: var(--ra-line); }
.artifact-card-summary .text-txt { color: var(--ra-ink); }
.artifact-card-summary .text-txt2 { color: var(--ra-muted); }
.artifact-paper-corner {
  position: absolute;
  top: 0;
  right: 0;
  z-index: 1;
  width: 18px;
  height: 18px;
  border-left: 1px solid var(--ra-line);
  border-bottom: 1px solid var(--ra-line);
  border-radius: 0 0 0 5px;
  background: linear-gradient(45deg, var(--ra-surface) 45%, var(--ra-cover) 52%);
  transform-origin: 100% 0;
  transition: transform 500ms var(--ra-ease), box-shadow 500ms;
  pointer-events: none;
}
.artifact-file-card:is(:hover, :focus-visible) .artifact-paper-corner {
  transform: rotate(4deg) scale(1.1);
  box-shadow: -2px 3px 4px var(--ra-shadow);
}
.artifact-resource-card:is(:hover, :focus-visible) :deep(.resource-terminal-cursor) {
  animation: resource-cursor 650ms steps(1) 2;
}
@keyframes resource-cursor {
  0%, 49% { opacity: 1; }
  50%, 100% { opacity: 0; }
}
@media (prefers-reduced-motion: reduce) {
  .artifact-card, .artifact-paper-corner { transition: none; }
  .artifact-card:is(:hover, :focus-visible),
  .artifact-file-card:is(:hover, :focus-visible) .artifact-paper-corner { transform: none; }
  .artifact-resource-card :deep(.resource-terminal-cursor) { animation: none !important; }
}
</style>
