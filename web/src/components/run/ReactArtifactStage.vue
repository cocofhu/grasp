<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import HtmlPreview from '@/components/ui/HtmlPreview.vue'
import ArtifactPreview from '@/components/run/ArtifactPreview.vue'
import ArtifactVersionSelect from '@/components/ui/ArtifactVersionSelect.vue'
import NovncPreviewPanel from '@/components/run/NovncPreviewPanel.vue'
import AppPreviewPanel from '@/components/run/AppPreviewPanel.vue'
import PublicAppPreviewPanel from '@/components/run/PublicAppPreviewPanel.vue'
import { api } from '@/lib/api/api'
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
  approveStageRemoteKind,
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
  nextTabAfterClose,
  openStagePreviewTab,
  previewTabId,
  previewTabName,
  artifactFingerprint,
  canAnnotateStageArtifact,
  expandStageArtifacts,
  isHistoricalStageArtifact,
  isOwnNodeArtifact,
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

const props = withDefaults(
  defineProps<{
    artifacts: Artifact[]
    previewArtifact?: string | null
    runId?: string
    run?: Run | null
    nodeId?: string
    /** Graph node type (visual/react/…). Fallback: run.nodes lookup by nodeId. */
    nodeType?: string
    /** When true, ArtifactPreview will not fetch (content already inlined). */
    inlineContent?: boolean
    /** Enable 取点 / 划选 / ⤴ 标注 on the current node's artifacts. */
    annotatable?: boolean
    remoteKind?: ReactStageRemoteKind
    token?: string
    ports?: PublicPreviewPort[]
    publicActive?: boolean
    publicMobile?: boolean
  }>(),
  {
    previewArtifact: '',
    runId: '',
    run: null,
    nodeId: '',
    nodeType: '',
    inlineContent: false,
    annotatable: false,
    token: '',
    ports: () => [],
    publicActive: true,
    publicMobile: false,
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
/** Default to pipeline artifacts grid (no standalone preview chrome Tab). */
const activeTab = ref(initialOpen?.activeTab || REACT_STAGE_TAB_GRID)
/** True after the user picks a grid tab, card, preview tab, or noVNC — auto-open must not steal focus.
 *  Also set when session open-state restore succeeds so pin auto-activate loses to refresh restore. */
const userMoved = ref(!!initialOpen)
const openNames = ref<string[]>(initialOpen?.openNames || [])
/** Session-local 「新 / 已更新」 marks on non-active preview tabs. */
const tabUnread = ref<Record<string, StageTabUnreadKind>>({})
/** Seeded fingerprint map for react/approve auto-pin; null until first observe. */
const autoPinFingerprints = ref<ArtifactFingerprintMap | null>(null)
const summaryThumbs = ref<Record<string, StageCardThumb>>({})
const novncOpen = ref(!!initialOpen?.novncOpen)
const sandboxId = ref<number | null>(null)
const sandboxLoading = ref(false)
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
const stageNode = computed(() => props.run?.nodes?.find((n) => n.id === props.nodeId) || null)
const resolvedNodeType = computed(() => String(props.nodeType || stageNode.value?.type || '').trim())

/** Approve: silent probe for set_preview registrations; hide app tab until ports exist. */
const APPROVE_PREVIEW_POLL_MS = 2500
const approvePreviewRegistered = ref(false)
let approveProbeTimer: ReturnType<typeof setInterval> | null = null
let approveProbeAbort: AbortController | null = null
let approveProbeGen = 0

function stopApprovePreviewProbe() {
  if (approveProbeTimer) {
    clearInterval(approveProbeTimer)
    approveProbeTimer = null
  }
  approveProbeAbort?.abort()
  approveProbeAbort = null
  approveProbeGen++
}

async function probeApprovePreviews() {
  if (resolvedNodeType.value !== 'approve') return
  const rid = String(props.runId || '').trim()
  const nid = String(props.nodeId || '').trim()
  if (!rid || !nid) {
    approvePreviewRegistered.value = false
    return
  }
  approveProbeAbort?.abort()
  const gen = ++approveProbeGen
  approveProbeAbort = new AbortController()
  try {
    const r = await api.nodePreviews(rid, nid, { signal: approveProbeAbort.signal })
    if (gen !== approveProbeGen) return
    approvePreviewRegistered.value = (r.ports || []).length > 0
  } catch (e) {
    if (gen !== approveProbeGen || isAbortError(e)) return
    // Keep last known registration on transient errors.
  }
}

watch(
  () => `${resolvedNodeType.value}|${props.runId}|${props.nodeId}`,
  () => {
    stopApprovePreviewProbe()
    approvePreviewRegistered.value = false
    if (resolvedNodeType.value !== 'approve') return
    void probeApprovePreviews()
    approveProbeTimer = setInterval(() => void probeApprovePreviews(), APPROVE_PREVIEW_POLL_MS)
  },
  { immediate: true },
)

const effectiveRemoteKind = computed(() => {
  if (resolvedNodeType.value === 'approve') {
    return approveStageRemoteKind(approvePreviewRegistered.value)
  }
  return resolvedRemoteKind.value
})

const stageArtifacts = computed(() => expandStageArtifacts(props.artifacts, props.run, stageNode.value))
const effectivePin = computed(() =>
  resolveEffectivePreviewPin({
    previewArtifact: props.previewArtifact,
    artifacts: stageArtifacts.value,
    nodeType: resolvedNodeType.value,
    nodeId: props.nodeId,
  }),
)
const autoPinEnabled = computed(() => isAutoPinStageNode(resolvedNodeType.value))
const gridArtifacts = computed(() =>
  stageGridArtifactsForNode(
    stageArtifacts.value,
    props.run,
    effectivePin.value,
    resolvedNodeType.value,
  ),
)
const canOpenNovnc = computed(() => effectiveRemoteKind.value !== 'off')
const showingNovnc = computed(() => activeTab.value === REACT_STAGE_TAB_NOVNC)
const showGridCards = computed(() => gridArtifacts.value.length > 0 || canOpenNovnc.value)
const remoteCardTitle = computed(() =>
  effectiveRemoteKind.value === 'sandbox'
    ? t('pages.reactArtifactStage.novncCardTitle')
    : t('pages.reactArtifactStage.appCardTitle'),
)
const remoteCardMeta = computed(() =>
  effectiveRemoteKind.value === 'sandbox'
    ? t('pages.reactArtifactStage.novncCardMeta')
    : t('pages.reactArtifactStage.appCardMeta'),
)
const remoteTabLabel = computed(() =>
  effectiveRemoteKind.value === 'sandbox'
    ? t('pages.reactArtifactStage.novncTab')
    : t('pages.reactArtifactStage.appTab'),
)

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
  })
}

function applyRestoredOpenState(runId: string, nodeId: string, names: string[]) {
  const restored = restoreStageOpenState(loadStageOpenState(runId, nodeId), names)
  if (!restored) return false
  openNames.value = restored.openNames
  activeTab.value = restored.activeTab
  novncOpen.value = restored.novncOpen
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

function closePreview(name: string) {
  markUserMoved()
  const next = nextTabAfterClose(openNames.value, name, activeTab.value, novncOpen.value)
  openNames.value = closeStagePreviewTab(openNames.value, name)
  activeTab.value = next
}

function openNovnc() {
  if (!canOpenNovnc.value) return
  markUserMoved()
  novncOpen.value = true
  activeTab.value = REACT_STAGE_TAB_NOVNC
}

function selectNovncTab() {
  markUserMoved()
  activeTab.value = REACT_STAGE_TAB_NOVNC
}

function closeNovnc() {
  markUserMoved()
  novncOpen.value = false
  if (openNames.value.length) {
    activeTab.value = previewTabId(openNames.value[openNames.value.length - 1])
    return
  }
  activeTab.value = REACT_STAGE_TAB_GRID
}

function onRemotePick(payload: AppPreviewPickPayload) {
  emit('pick', payload)
}

const showingGrid = computed(() => activeTab.value === REACT_STAGE_TAB_GRID)
const activePreviewName = computed(() => previewTabName(activeTab.value))

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
        activeTab.value = nextTabAfterClose(openNames.value, gone, activeTab.value, novncOpen.value)
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

/** react/approve: auto-pin visible own-node artifacts on create/overwrite. */
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
  [openNames, activeTab, novncOpen, () => props.runId, () => props.nodeId],
  () => {
    persistOpenState()
  },
  { deep: true },
)

watch(
  () => `${props.runId}|${props.nodeId}`,
  (key, prev) => {
    if (!prev || key === prev) return
    const rid = String(props.runId || '').trim()
    const nid = String(props.nodeId || '').trim()
    if (!rid || !nid) {
      openNames.value = []
      activeTab.value = REACT_STAGE_TAB_GRID
      novncOpen.value = false
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
      // Drop an empty Approve app tab if registration disappears.
      if (resolvedNodeType.value === 'approve' && novncOpen.value && activeTab.value === REACT_STAGE_TAB_NOVNC) {
        novncOpen.value = false
        activeTab.value = openNames.value.length
          ? previewTabId(openNames.value[openNames.value.length - 1])
          : REACT_STAGE_TAB_GRID
      }
      return
    }
    // Show the remote tab once kind is live; never steal focus after the user moved.
    novncOpen.value = true
    if (userMoved.value) return
    if (activeTab.value === REACT_STAGE_TAB_GRID) {
      activeTab.value = REACT_STAGE_TAB_NOVNC
    }
  },
  { immediate: true },
)

watch(
  () => `${props.runId}|${props.nodeId}|${effectiveRemoteKind.value}`,
  async () => {
    sandboxId.value = null
    if (effectiveRemoteKind.value !== 'sandbox') return
    sandboxLoading.value = true
    try {
      const sbx = await api.getRunNodeSandbox(props.runId, props.nodeId)
      sandboxId.value = typeof sbx?.id === 'number' ? sbx.id : Number(sbx?.id) || null
    } catch (e) {
      if (isAbortError(e)) return
      sandboxId.value = null
    } finally {
      sandboxLoading.value = false
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

onBeforeUnmount(() => {
  stopApprovePreviewProbe()
  summaryThumbGen++
  summaryThumbAbort?.abort()
})
</script>

<template>
  <div class="flex h-full min-h-0 flex-col bg-base" data-testid="react-artifact-stage">
    <div
      class="flex shrink-0 items-center gap-1 overflow-x-auto border-b border-line px-2 py-1.5 max-md:px-1.5 max-md:py-1"
      data-testid="react-artifact-tabs"
      role="tablist"
    >
      <button
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
        <span class="truncate">{{ t('pages.reactArtifactStage.pipelineTab') }}</span>
      </button>
      <div
        v-for="name in openNames"
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
        v-if="novncOpen"
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
          <span class="truncate">{{ remoteTabLabel }}</span>
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
    </div>

    <div v-show="showingGrid" class="min-h-0 flex-1 overflow-y-auto p-4" data-testid="react-artifact-grid">
      <div
        v-if="!showGridCards"
        class="flex h-full min-h-[160px] flex-col items-center justify-center text-center text-[12px] text-txt3"
        data-testid="react-artifact-grid-empty"
      >
        <Icon name="artifact" :size="26" class="mb-2 opacity-40" />
        {{ t('pages.reactArtifactStage.gridEmpty') }}
      </div>
      <div
        v-else
        class="grid grid-cols-[repeat(auto-fill,minmax(176px,1fr))] gap-3"
      >
        <button
          v-if="canOpenNovnc"
          type="button"
          class="overflow-hidden rounded-lg border border-line bg-surface text-left transition hover:border-line-strong"
          data-testid="react-artifact-card-novnc"
          @click="openNovnc"
        >
          <div class="relative flex h-[110px] items-center justify-center overflow-hidden bg-elevated text-txt3">
            <Icon name="globe" :size="28" class="opacity-50" />
          </div>
          <div class="px-2.5 py-2">
            <div class="truncate text-[12px] font-medium text-txt">{{ remoteCardTitle }}</div>
            <div class="mt-0.5 truncate text-[11px] text-txt3">{{ remoteCardMeta }}</div>
          </div>
        </button>
        <div
          v-for="a in gridArtifacts"
          :key="a.id"
          role="button"
          tabindex="0"
          class="overflow-hidden rounded-lg border border-line bg-surface text-left transition hover:border-line-strong"
          :data-testid="'react-artifact-card-' + a.name"
          @click="openArtifact(a)"
          @keydown.enter.prevent="openArtifact(a)"
        >
          <div class="relative h-[110px] overflow-hidden bg-elevated">
            <div
              v-if="textThumb(a)"
              class="pointer-events-none h-full overflow-hidden border-b border-line px-3 py-2.5"
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
              <div class="min-w-0 truncate text-[12px] font-medium text-txt" :title="artifactTitle(a)">{{ artifactTitle(a) }}</div>
              <span
                v-if="artifactReadonly(a)"
                class="shrink-0 rounded border border-line px-1 py-px text-[10px] text-txt3"
                data-testid="react-artifact-card-readonly"
              >{{ t('pages.reactArtifactStage.readonlyBadge') }}</span>
            </div>
            <div class="mt-0.5 flex items-center justify-between gap-1">
              <div class="min-w-0 truncate text-[11px] text-txt3">{{ metaLine(a) }}</div>
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
      </div>
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
      v-if="novncOpen"
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
      <NovncPreviewPanel
        v-else-if="sandboxId"
        :sandbox-id="sandboxId"
        fill
        :inspectable="annotatable"
        @pick="onRemotePick"
      />
      <div
        v-else-if="effectiveRemoteKind === 'sandbox'"
        class="flex h-full flex-col items-center justify-center p-6 text-center text-[12px] text-txt3"
        data-testid="react-artifact-novnc-missing"
      >
        <Icon name="globe" :size="26" class="mb-2 opacity-40" />
        {{ sandboxLoading ? t('pages.appPreview.loading') : t('pages.reactArtifactStage.novncMissing') }}
      </div>
    </div>
  </div>
</template>
