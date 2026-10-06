<script setup lang="ts">
import { ref, reactive, computed, onMounted, nextTick, onBeforeUnmount, watch, provide } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate, useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import AppButton from '@/components/ui/AppButton.vue'
import StatusPill from '@/components/ui/StatusPill.vue'
import WorkflowCanvas from '@/components/canvas/WorkflowCanvas.vue'
import NodePalette from '@/components/canvas/NodePalette.vue'
import NodeInspector from '@/components/canvas/NodeInspector.vue'
import WorkflowVersionDrawer from '@/components/canvas/WorkflowVersionDrawer.vue'
import AppDrawer from '@/components/ui/AppDrawer.vue'
import AppModal from '@/components/ui/AppModal.vue'
import RunLaunchModal, { type InputField } from '@/components/workflow/RunLaunchModal.vue'
import ExportVersionModal from '@/components/workflow/ExportVersionModal.vue'
import CopyWorkflowModal from '@/components/workflow/CopyWorkflowModal.vue'
import WorkflowApiTab from '@/components/workflow/WorkflowApiTab.vue'
import WorkflowRunHistoryTab from '@/components/workflow/WorkflowRunHistoryTab.vue'
import HardLoadLayer from '@/components/run/HardLoadLayer.vue'
import { useWorkflowAskInputs } from '@/lib/run/useWorkflowAskInputs'
import { api } from '@/lib/api/api'
import type { Agent } from '@/lib/api/apiTypes'
import { useWorkflowImport } from '@/lib/run/useWorkflowImport'
import { useNodeDefs } from '@/lib/run/useNodeDefs'
import { fmtTime } from '@/lib/shared/format'
import { clearRunDraft, mergeRunDraft, saveRunDraft } from '@/lib/run/runDraft'
import { useToast } from '@/lib/composables/useToast'
import { useWorkflowFavorites } from '@/lib/run/useWorkflowFavorites'
import { workflowGraphError } from '@/lib/workflow/graphValidation'
import type { ClarifyImage, NodeType, WFEdge, WFNode, Workflow } from '@/lib/shared/types'
import { readStoredProjectId } from '@/lib/composables/useProjectContext'
import { useBreakpoint } from '@/lib/composables/useBreakpoint'
import { CANVAS_EDITOR, useCanvasEditor } from '@/components/canvas/composables/useCanvasEditor'
import { pendingBackup, useWorkflowSave, type WorkflowBackup } from '@/components/canvas/composables/useWorkflowSave'
import { buildPaletteItems, NODE_ICONS, paletteKey } from '@/components/canvas/composables/paletteItems'
import { isMac, resolveShortcut } from '@/components/canvas/composables/useCanvasShortcuts'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const tr = (key: string, named?: Record<string, unknown>) => (named ? t(key, named) : t(key))
const toast = useToast()
const { NODE_DEFS } = useNodeDefs()
const { isFavorite, toggleFavorite } = useWorkflowFavorites()
const { isMobile } = useBreakpoint()
const showFlowPeek = ref(false)
let routeId = route.params.id as string

function toggleCurrentFavorite() {
  if (!wf.id) return
  toggleFavorite(wf.id, { name: wf.name })
}

type EditorTab = 'canvas' | 'runs' | 'api'
const activeTab = ref<EditorTab>('canvas')
const TABS: EditorTab[] = ['canvas', 'runs', 'api']
const editorTabTrack = ref<HTMLElement | null>(null)
const editorTabIndicator = ref<Record<string, string>>({ opacity: '0', transform: 'translateX(0)', width: '0px' })

function updateEditorTabIndicator() {
  const root = editorTabTrack.value
  if (!root) return
  const active = root.querySelector<HTMLElement>('[data-editor-tab-active="true"]')
  if (!active) {
    editorTabIndicator.value = { opacity: '0', transform: 'translateX(0)', width: '0px' }
    return
  }
  const left = active.offsetLeft + 8
  const width = Math.max(0, active.offsetWidth - 16)
  editorTabIndicator.value = { opacity: '1', transform: `translateX(${left}px)`, width: `${width}px` }
}

watch(activeTab, () => void nextTick(updateEditorTabIndicator))

function initialProjectId(): string {
  const q = typeof route.query.projectId === 'string' ? route.query.projectId : ''
  return q || readStoredProjectId() || ''
}

const wf = reactive<Workflow>({
  id: routeId === 'new' ? '' : routeId,
  projectId: routeId === 'new' ? initialProjectId() : undefined,
  name: t('pages.workflowEditor.unnamedWorkflow'),
  description: '',
  status: 'draft',
  version: 1,
  publishedVersion: 0,
  updatedAt: '',
  needsRepo: false,
  nodes: [],
  edges: [],
})
const { fields: askFieldsComputed } = useWorkflowAskInputs(wf)

const PROJECT_WORKFLOWS_TAB = 'workflows'
const projectName = ref('')

function projectLink() {
  return wf.projectId ? `/projects/${wf.projectId}?tab=${PROJECT_WORKFLOWS_TAB}` : '/projects'
}

function goBackToProject() {
  router.push(projectLink())
}

const running = ref(false)
const errorMsg = ref('')
const hydrating = ref(routeId !== 'new')
const hydrateFailed = ref(false)

// ── Agents & editor ──
const allAgents = ref<Agent[] | null>(null)
const projectAgents = computed(() =>
  allAgents.value === null ? null : allAgents.value.filter((a) => !!wf.projectId && a.projectId === wf.projectId),
)

const editor = useCanvasEditor({
  graph: wf,
  agents: projectAgents,
  t: tr,
  typeLabel: (type: NodeType) => NODE_DEFS.value[type]?.label ?? type,
  notify: (m, action) => (action ? toast.show(m, 'default', { action }) : toast.warn(m)),
})
provide(CANVAS_EDITOR, editor)

const canvasRef = ref<InstanceType<typeof WorkflowCanvas> | null>(null)
const paletteItems = computed(() =>
  buildPaletteItems(
    editor.agents.value,
    (type) => ({ label: NODE_DEFS.value[type]?.label ?? type, desc: NODE_DEFS.value[type]?.desc ?? '' }),
    tr,
  ),
)

const agentLookupMap = computed(() => (projectAgents.value ? editor.lookup.value : undefined))
const graphError = computed(() => workflowGraphError(wf, { t: tr, agents: agentLookupMap.value }))

async function loadAgents() {
  try {
    allAgents.value = (await api.listAgents()) || []
  } catch {
    allAgents.value = []
  }
}

async function loadProjectName() {
  if (!wf.projectId) return
  try {
    projectName.value = (await api.getProject(wf.projectId))?.name || ''
  } catch {
    projectName.value = ''
  }
}

// ── Save ──
function payload(): Workflow {
  return JSON.parse(JSON.stringify(wf)) as Workflow
}

function absorb(res: Partial<Workflow> | null | undefined) {
  if (!res) return
  const wasNew = !wf.id
  if (res.id) wf.id = res.id
  if (res.version !== undefined) wf.version = res.version
  if (res.publishedVersion !== undefined) wf.publishedVersion = res.publishedVersion
  if (res.status === 'draft' && wf.status === 'published' && wf.showOnHome) toast.warn(t('pages.workflowEditor.hiddenFromHome'))
  if (res.status) wf.status = res.status
  if (res.updatedAt) wf.updatedAt = res.updatedAt
  if (res.projectId && !wf.projectId) wf.projectId = res.projectId
  if (wasNew && wf.id) {
    routeId = wf.id
    void router.replace({ path: `/workflows/${wf.id}/edit`, query: {} })
  }
}

/** The editable content; a change from the last saved value means unsaved changes. */
type EditableSnapshot = Pick<Workflow, 'name' | 'description' | 'needsRepo' | 'nodes' | 'edges'>
function snapshotOf(w: EditableSnapshot): string {
  return JSON.stringify({ name: w.name, description: w.description, needsRepo: w.needsRepo, nodes: w.nodes, edges: w.edges })
}

let deleting = false
/** Read-only version shown on the canvas instead of the editable graph. */
const preview = ref<{ version: number; nodes: WFNode[]; edges: WFEdge[] } | null>(null)

const saver = useWorkflowSave({
  source: () => snapshotOf(wf),
  save: async () => {
    absorb(await api.saveWorkflow(payload()))
  },
  backupId: () => wf.id,
  enabled: () => !hydrating.value && !hydrateFailed.value && !!wf.projectId && !deleting,
})

/** Saves unsaved changes (a new workflow is created even when untouched). */
async function saveNow(silent = false): Promise<boolean> {
  if (!saver.dirty.value && wf.id) return true
  const ok = await saver.save(!wf.id)
  if (!ok && !silent) toast.error(t('canvas.topbar.saveError', { error: saver.error.value || t('canvas.save.error') }))
  return ok
}

const saveShortcut = (isMac() ? '⌘' : 'Ctrl+') + 'S'
const saveLabel = computed(() => {
  switch (saver.status.value) {
    case 'saving':
      return t('canvas.save.saving')
    case 'dirty':
      return t('canvas.save.dirty')
    case 'error':
      return t('canvas.save.error')
    default:
      return wf.id ? t('canvas.save.saved', { n: wf.version }) : t('canvas.save.notCreated')
  }
})

function onSaveKey(e: KeyboardEvent) {
  if (resolveShortcut(e) !== 'save') return
  if (e.defaultPrevented) return
  e.preventDefault()
  if (editable.value && !preview.value) void saveNow()
}

// ── Unsaved-change backup ──
const backupOffer = ref<WorkflowBackup | null>(null)

function restoreBackup() {
  const b = backupOffer.value
  backupOffer.value = null
  if (!b) return
  try {
    const snap = JSON.parse(b.snapshot) as EditableSnapshot
    editor.clearSelection()
    Object.assign(wf, { name: snap.name, description: snap.description, needsRepo: snap.needsRepo })
    editor.replaceGraph(snap.nodes || [], snap.edges || [])
  } catch {
    saver.discardBackup()
  }
}

function discardBackupOffer() {
  backupOffer.value = null
  saver.discardBackup()
}

// ── Leaving with unsaved changes ──
type LeaveChoice = 'save' | 'discard' | 'cancel'
const leavePrompt = ref<((choice: LeaveChoice) => void) | null>(null)

function chooseLeave(choice: LeaveChoice) {
  const resolve = leavePrompt.value
  leavePrompt.value = null
  resolve?.(choice)
}

/** Resolves true when it is fine to drop the editor state (saved, saved now, or discarded). */
async function confirmUnsaved(): Promise<boolean> {
  if (!saver.dirty.value || deleting) return true
  const choice = await new Promise<LeaveChoice>((resolve) => (leavePrompt.value = resolve))
  if (choice === 'cancel') return false
  if (choice === 'discard') {
    saver.discardBackup()
    return true
  }
  return saveNow()
}

// ── Load ──
async function hydrate(fn: () => Promise<Partial<Workflow>>) {
  Object.assign(wf, await fn())
  await nextTick()
  editor.clearSelection()
  editor.history.reset()
  saver.markSaved()
}

async function loadExistingWorkflow() {
  hydrating.value = true
  hydrateFailed.value = false
  errorMsg.value = ''
  try {
    await hydrate(() => api.getWorkflow(routeId))
    backupOffer.value = pendingBackup(wf.id, snapshotOf(wf), wf.updatedAt)
    void loadProjectName()
  } catch {
    hydrateFailed.value = true
    errorMsg.value = t('pages.workflowEditor.loadFailed')
  } finally {
    hydrating.value = false
  }
}

function onBeforeUnload(e: BeforeUnloadEvent) {
  if (!saver.dirty.value || deleting) return
  saver.flushBackup()
  e.preventDefault()
  e.returnValue = ''
}

onMounted(async () => {
  void nextTick(updateEditorTabIndicator)
  window.addEventListener('resize', updateEditorTabIndicator)
  window.addEventListener('beforeunload', onBeforeUnload)
  window.addEventListener('keydown', onSaveKey)
  void loadAgents()
  if (routeId === 'new') {
    hydrating.value = false
    if (!wf.projectId) errorMsg.value = t('pages.workflowEditor.projectRequired')
    else void loadProjectName()
    saver.markSaved()
    editor.history.reset()
    return
  }
  await loadExistingWorkflow()
})

onBeforeUnmount(() => {
  window.removeEventListener('resize', updateEditorTabIndicator)
  window.removeEventListener('beforeunload', onBeforeUnload)
  window.removeEventListener('keydown', onSaveKey)
})

onBeforeRouteLeave(() => confirmUnsaved())
onBeforeRouteUpdate((to) => (to.params.id === wf.id ? true : confirmUnsaved()))

watch(
  () => route.params.id,
  (id) => {
    if (typeof id !== 'string' || id === 'new' || id === wf.id) return
    routeId = id
    wf.id = id
    void loadExistingWorkflow()
  },
)

// ── Issues ──
const showIssues = ref(false)
const issueCount = computed(() => editor.issues.value.length)

function nodeLabel(id?: string) {
  const n = id ? wf.nodes.find((x) => x.id === id) : null
  return n ? n.label || n.id : ''
}

function jumpToIssue(nodeId?: string) {
  showIssues.value = false
  if (!nodeId) return
  exitPreview()
  activeTab.value = 'canvas'
  editor.setSelection([nodeId])
  canvasRef.value?.centerOn(nodeId)
}

// ── More menu ──
const showMore = ref(false)
const moreRoot = ref<HTMLElement | null>(null)
const issuesRoot = ref<HTMLElement | null>(null)
function onDocDown(ev: MouseEvent) {
  if (showMore.value && moreRoot.value && !moreRoot.value.contains(ev.target as Node)) showMore.value = false
  if (showIssues.value && issuesRoot.value && !issuesRoot.value.contains(ev.target as Node)) showIssues.value = false
}
onMounted(() => document.addEventListener('mousedown', onDocDown, true))
onBeforeUnmount(() => document.removeEventListener('mousedown', onDocDown, true))

function menu(action: () => void) {
  showMore.value = false
  action()
}

const showExport = ref(false)
async function openExport() {
  if (!wf.id || !(await saveNow())) return
  showExport.value = true
}
const { fileInput, showDiscardConfirm, triggerImport, onDiscardCancel, onDiscardConfirm, handleFileChange } = useWorkflowImport({
  dirty: () => saver.dirty.value,
  projectId: () => wf.projectId,
  onImported: async (imported) => {
    // Unsaved changes were already discarded in the import confirmation.
    saver.discardBackup()
    saver.markSaved()
    await router.push('/workflows/' + imported.id + '/edit')
  },
})

const copyModal = ref<{ sourceId: string; sourceName: string; suggestedName: string; existing: string[] } | null>(null)
async function openCopy() {
  if (!wf.id || !(await saveNow())) return
  try {
    const [preview, list] = await Promise.all([
      api.copyPreviewWorkflow(wf.id),
      api.listWorkflows({ projectId: wf.projectId }).catch(() => [] as Workflow[]),
    ])
    copyModal.value = { ...preview, existing: (list || []).map((w) => w.name) }
  } catch {
    toast.error(t('common.toast.copyNameFailed'))
  }
}
function onCopied(copy: Workflow) {
  copyModal.value = null
  toast.success(t('common.toast.copied', { name: copy.name }))
  void router.push(`/workflows/${copy.id}/edit`)
}

const showDelete = ref(false)
const deleteBusy = ref(false)
async function confirmDelete() {
  if (!wf.id) return
  deleteBusy.value = true
  deleting = true
  try {
    await api.deleteWorkflow(wf.id)
    showDelete.value = false
    saver.discardBackup()
    saver.stop()
    void router.push(projectLink())
  } catch (e: any) {
    deleting = false
    toast.error(t('canvas.topbar.deleteFailed', { error: String(e?.message || e) }))
  } finally {
    deleteBusy.value = false
  }
}

// ── Publish ──
const showPublish = ref(false)
const publishing = ref(false)
const publishError = ref('')
const published = ref(false)

function openPublish() {
  publishError.value = ''
  published.value = false
  showPublish.value = true
}

async function confirmPublish() {
  if (graphError.value) {
    publishError.value = graphError.value
    return
  }
  publishing.value = true
  publishError.value = ''
  try {
    if (!(await saveNow(true)) || !wf.id) throw new Error(saver.error.value || t('canvas.save.error'))
    absorb(await api.publishWorkflow(wf.id))
    published.value = true
    setTimeout(() => {
      showPublish.value = false
    }, 1100)
  } catch (e: any) {
    publishError.value = t('pages.workflowEditor.publishFailed') + String(e?.message || e)
  } finally {
    publishing.value = false
  }
}

// ── Run ──
const showRun = ref(false)
const runFields = ref<InputField[]>([])
const runInputs = ref<Record<string, string>>({})
const runImages = ref<Record<string, ClarifyImage[]>>({})
const draftRestored = ref(false)

function fieldOptions(f: InputField): string[] {
  return String(f.options || '').split(/[,，]/).map((s) => s.trim()).filter(Boolean)
}

async function openRun() {
  if (graphError.value) {
    errorMsg.value = graphError.value
    return
  }
  if (!(await saveNow())) return
  draftRestored.value = false
  runFields.value = askFieldsComputed.value
  const seed: Record<string, string> = {}
  const imgSeed: Record<string, ClarifyImage[]> = {}
  for (const f of runFields.value) {
    seed[f.key] = f.default || (f.type === 'select' ? fieldOptions(f)[0] || '' : '')
    imgSeed[f.key] = []
  }
  const merged = await mergeRunDraft(wf.id, seed, imgSeed, runFields.value.map((f) => f.key))
  runInputs.value = merged.inputs
  runImages.value = merged.images
  draftRestored.value = merged.restored
  showRun.value = true
}

async function saveRunDraftClick() {
  if (!wf.id) return
  const images: Record<string, ClarifyImage[]> = {}
  for (const [k, v] of Object.entries(runImages.value)) images[k] = v ? [...v] : []
  const result = await saveRunDraft(wf.id, { ...runInputs.value }, images)
  if (result === 'ok') toast.success(t('common.toast.draftSaved'))
  else if (result === 'quota_exceeded' || result === 'partial') toast.warn(t('common.toast.draftTooLarge'))
  else toast.error(t('common.toast.draftSaveFailed'))
}

async function beforeRunStart() {
  errorMsg.value = ''
  if (!(await saveNow(true))) throw new Error(saver.error.value || t('canvas.save.saveFirstFailed'))
}

function onRunStarted() {
  void clearRunDraft(wf.id)
}

function onViewRun(runId: string) {
  router.push('/runs/' + runId)
}

// ── Drawers ──
const showOverview = ref(false)

function nodeChips(n: (typeof wf.nodes)[number]): string[] {
  const c = (n.config || {}) as Record<string, any>
  const out: string[] = []
  const profile = String(c.agent_profile ?? '').trim()
  if (profile) out.push(t('pages.workflowEditor.nodeChips.agent', { name: profile }))
  if (n.type === 'branch') out.push(t('pages.workflowEditor.nodeChips.routes', { n: c.cases?.length || 0 }))
  if (n.type === 'input') out.push(t('pages.workflowEditor.nodeChips.variables', { n: (c.variables || []).filter((v: any) => v?.name).length }))
  if (n.type === 'human_gate') out.push(t('pages.workflowEditor.nodeChips.actions', { n: (c.actions || []).length }))
  return out
}

// ── Versions ──
const showVersions = ref(false)
const restoreAsk = ref<number | null>(null)
const restoringVersion = ref<number | null>(null)
let previewSeq = 0

function toggleVersions() {
  if (!wf.id) return
  showVersions.value = !showVersions.value
  if (!showVersions.value) exitPreview()
}

function closeVersions() {
  showVersions.value = false
  exitPreview()
}

async function previewVersion(version: number) {
  if (!wf.id) return
  if (preview.value?.version === version) return
  const seq = ++previewSeq
  try {
    const g = await api.getWorkflowVersionGraph(wf.id, version)
    if (seq !== previewSeq) return
    editor.cancelPlacing()
    preview.value = { version, nodes: g?.nodes || [], edges: g?.edges || [] }
  } catch {
    if (seq === previewSeq) toast.error(t('canvas.versions.previewFailed'))
  }
}

function exitPreview() {
  previewSeq++
  preview.value = null
}

function askRestore(version: number) {
  restoreAsk.value = version
}

async function confirmRestore() {
  const version = restoreAsk.value
  if (!wf.id || version === null) return
  restoringVersion.value = version
  try {
    const res = await api.restoreWorkflowVersion(wf.id, version)
    saver.discardBackup()
    backupOffer.value = null
    restoreAsk.value = null
    exitPreview()
    await hydrate(async () => res)
    toast.success(t('canvas.versions.restored', { from: version, to: wf.version }))
  } catch (e: any) {
    toast.error(t('canvas.versions.restoreFailed', { error: String(e?.message || e) }))
  } finally {
    restoringVersion.value = null
  }
}

// ── Inspector ──
const inspectorNode = computed(() => editor.inspectorNode.value)
function deleteInspectorNode() {
  if (inspectorNode.value) editor.removeNodes([inspectorNode.value.id])
}

type PaletteSpec = Parameters<typeof editor.addNode>[0]

function placeFromPalette(spec: PaletteSpec) {
  activeTab.value = 'canvas'
  editor.togglePlacing(spec)
}

function addFromPalette(spec: PaletteSpec) {
  editor.cancelPlacing()
  editor.addNode(spec)
}

const placingKey = computed(() => (editor.placing.value ? paletteKey(editor.placing.value) : null))

const editable = computed(() => !hydrating.value && !hydrateFailed.value)
</script>

<template>
  <div v-if="isMobile" class="flex h-full min-h-0 flex-col overflow-auto bg-base" data-testid="workflow-editor-mobile">
    <div class="flex shrink-0 items-center gap-2 border-b border-line px-3 py-2">
      <button
        type="button"
        class="flex min-h-11 items-center gap-1 text-[13px] text-txt3 hover:text-txt"
        data-testid="workflow-editor-back"
        @click="goBackToProject"
      >
        <Icon name="arrow-left" :size="15" />{{ t('pages.sandboxConsole.back') }}
      </button>
      <span class="truncate text-[13px] font-medium text-txt">{{ wf.name }}</span>
    </div>
    <div class="flex flex-1 flex-col items-center px-5 py-8 text-center">
      <div class="mb-3 flex h-10 w-10 items-center justify-center rounded-lg border border-info/35 bg-info/10 text-info">◇</div>
      <h3 class="text-[14px] font-semibold text-txt">{{ t('pages.workflowEditor.mobile.title') }}</h3>
      <p class="mt-2 max-w-[32ch] text-[12.5px] leading-relaxed text-txt2">{{ t('pages.workflowEditor.mobile.desc') }}</p>
      <button
        type="button"
        class="mt-4 min-h-11 rounded-md border border-line bg-transparent px-4 text-[13px] text-txt2 hover:border-accent hover:text-txt"
        data-testid="workflow-editor-peek"
        @click="showFlowPeek = !showFlowPeek"
      >
        {{ t('pages.workflowEditor.mobile.peek') }}
      </button>
    </div>
    <div v-if="showFlowPeek" class="mx-4 mb-6 rounded-lg border border-line bg-surface p-3 text-left" data-testid="workflow-editor-summary">
      <div class="mb-2 text-[11px] uppercase tracking-wider text-txt3">{{ t('pages.workflowEditor.mobile.summaryLabel') }}</div>
      <p v-if="!wf.nodes.length" class="text-[12px] text-txt3">{{ t('pages.workflowEditor.mobile.empty') }}</p>
      <div
        v-for="n in wf.nodes"
        :key="n.id"
        class="flex items-center gap-2 border-b border-line/60 py-2 last:border-b-0"
        data-testid="workflow-editor-node-row"
      >
        <span class="h-2 w-2 shrink-0 bg-accent" :data-node-type="n.type" />
        <span class="min-w-0 truncate text-[13px] text-txt">{{ NODE_DEFS[n.type]?.label || n.type }} · {{ n.label }}</span>
      </div>
      <p class="mt-2 text-[11px] text-txt3">{{ t('pages.workflowEditor.mobile.noEdit') }}</p>
    </div>
  </div>

  <div v-else class="flex h-full flex-col bg-base">
    <header class="flex h-14 shrink-0 items-center gap-2 border-b border-line bg-surface px-3" data-testid="workflow-editor-topbar">
      <button
        type="button"
        class="flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-txt2 hover:bg-elevated hover:text-txt"
        :aria-label="t('pages.sandboxConsole.back')"
        data-testid="workflow-editor-back"
        @click="goBackToProject"
      >
        <Icon name="arrow-left" :size="17" />
      </button>
      <nav class="flex min-w-0 items-center gap-1 text-[13px]" :aria-label="t('canvas.topbar.projects')">
        <RouterLink :to="projectLink()" class="max-w-[180px] truncate text-txt3 hover:text-txt" data-testid="editor-breadcrumb-project">
          {{ projectName || t('canvas.topbar.projects') }}
        </RouterLink>
        <span class="text-txt3" aria-hidden="true">/</span>
        <span class="inline-grid min-w-0 max-w-[320px] text-[14px] font-semibold">
          <span class="invisible col-start-1 row-start-1 overflow-hidden whitespace-pre px-1.5 py-1" aria-hidden="true">{{ wf.name || ' ' }}&#8203;&nbsp;</span>
          <input
            v-model="wf.name"
            class="col-start-1 row-start-1 w-full min-w-[6ch] rounded-md bg-transparent px-1.5 py-1 text-txt outline-none hover:bg-elevated focus:bg-elevated focus:ring-1 focus:ring-accent/40"
            :title="t('canvas.topbar.rename')"
            :aria-label="t('canvas.topbar.rename')"
            data-testid="editor-name"
            @keydown.enter="($event.target as HTMLInputElement).blur()"
          />
        </span>
      </nav>
      <StatusPill :status="wf.status" size="sm" />
      <span
        class="inline-flex shrink-0 items-center gap-1 text-[11.5px]"
        :class="{
          'text-txt3': saver.status.value === 'saved',
          'text-txt2': saver.status.value === 'saving',
          'text-warn': saver.status.value === 'dirty',
          'text-err': saver.status.value === 'error',
        }"
        role="status"
        aria-live="polite"
        :title="saver.error.value || undefined"
        data-testid="editor-save-status"
        :data-status="saver.status.value"
      >
        <Icon v-if="saver.status.value === 'saving'" name="spinner" :size="12" class="animate-spin" />
        <Icon v-else-if="saver.status.value === 'saved'" name="check" :size="12" />
        <Icon v-else-if="saver.status.value === 'error'" name="alert" :size="12" />
        <span v-else class="h-1.5 w-1.5 rounded-full bg-warn" aria-hidden="true" />
        {{ saveLabel }}
      </span>

      <div class="flex-1" />

      <div ref="issuesRoot" class="relative">
        <button
          type="button"
          class="inline-flex h-8 items-center gap-1.5 rounded-md px-2.5 text-[12px] transition-colors"
          :class="issueCount ? 'text-warn hover:bg-warn/10' : 'text-txt3 hover:bg-elevated'"
          :aria-expanded="showIssues"
          data-testid="editor-issues"
          @click="showIssues = !showIssues"
        >
          <Icon :name="issueCount ? 'alert' : 'check'" :size="13" />
          {{ issueCount ? t('canvas.issues.count', { n: issueCount }) : t('canvas.issues.none') }}
        </button>
        <div v-if="showIssues && issueCount" class="cchrome absolute right-0 top-full z-40 mt-1 max-h-80 w-80 overflow-y-auto p-1" role="menu" data-testid="editor-issues-list">
          <button
            v-for="(iss, i) in editor.issues.value"
            :key="i"
            type="button"
            role="menuitem"
            class="flex w-full items-start gap-2 rounded-md px-2.5 py-1.5 text-left text-[12px] hover:bg-elevated"
            :class="iss.nodeId ? '' : 'cursor-default'"
            :data-testid="`editor-issue-${i}`"
            @click="jumpToIssue(iss.nodeId)"
          >
            <Icon name="alert" :size="12" class="mt-0.5 shrink-0 text-warn" />
            <span class="min-w-0 flex-1">
              <span v-if="iss.nodeId" class="block truncate text-[11px] text-txt3">{{ nodeLabel(iss.nodeId) }}</span>
              <span class="block text-txt">{{ iss.message }}</span>
            </span>
          </button>
        </div>
      </div>

      <AppButton
        variant="ghost"
        size="sm"
        :icon="isFavorite(wf.id) ? 'star-filled' : 'star'"
        :disabled="!wf.id || !editable"
        data-testid="editor-favorite-btn"
        :class="{ 'text-warn': isFavorite(wf.id) }"
        :aria-label="isFavorite(wf.id) ? t('common.buttons.unfavorite') : t('common.buttons.favorite')"
        @click="toggleCurrentFavorite"
      />
      <AppButton
        variant="ghost"
        size="sm"
        icon="history"
        :disabled="!wf.id || !editable"
        :class="{ 'text-accent-2': showVersions }"
        :aria-pressed="showVersions"
        data-testid="editor-versions"
        @click="toggleVersions"
      >
        {{ t('canvas.topbar.versions') }}
      </AppButton>
      <AppButton
        :variant="saver.dirty.value || !wf.id ? 'primary' : 'outline'"
        size="sm"
        icon="check"
        :disabled="!editable || !!preview || saver.status.value === 'saving' || (!saver.dirty.value && !!wf.id)"
        :title="t('canvas.save.title', { key: saveShortcut })"
        data-testid="editor-save"
        @click="saveNow()"
      >
        {{ saver.status.value === 'saving' ? t('canvas.save.saving') : t('canvas.save.button') }}
      </AppButton>
      <AppButton variant="outline" size="sm" icon="check" :disabled="publishing || !editable" data-testid="editor-publish" @click="openPublish">
        {{ t('canvas.topbar.publish') }}
      </AppButton>
      <AppButton variant="primary" size="sm" icon="play" :disabled="running || !editable" data-testid="editor-run" @click="openRun">
        {{ running ? t('common.buttons.starting') : t('canvas.topbar.run') }}
      </AppButton>
      <div ref="moreRoot" class="relative">
        <button
          type="button"
          class="flex h-8 w-8 items-center justify-center rounded-md text-txt2 hover:bg-elevated hover:text-txt"
          :aria-label="t('canvas.topbar.more')"
          :aria-expanded="showMore"
          aria-haspopup="menu"
          data-testid="editor-more"
          @click="showMore = !showMore"
        >
          <Icon name="more" :size="16" />
        </button>
        <div v-if="showMore" class="cchrome absolute right-0 top-full z-40 mt-1 w-48 p-1" role="menu" data-testid="editor-more-menu">
          <button type="button" role="menuitem" class="editor-menu-item" data-testid="editor-menu-import" @click="menu(triggerImport)">
            <Icon name="input" :size="14" />{{ t('canvas.topbar.menu.import') }}
          </button>
          <button type="button" role="menuitem" class="editor-menu-item" :disabled="!wf.id" data-testid="editor-menu-export" @click="menu(openExport)">
            <Icon name="download" :size="14" />{{ t('canvas.topbar.menu.export') }}
          </button>
          <button type="button" role="menuitem" class="editor-menu-item" :disabled="!wf.id" data-testid="editor-menu-duplicate" @click="menu(openCopy)">
            <Icon name="copy" :size="14" />{{ t('canvas.topbar.menu.duplicate') }}
          </button>
          <div class="my-1 h-px bg-line" />
          <button type="button" role="menuitem" class="editor-menu-item" :disabled="!wf.nodes.length" @click="menu(() => (showOverview = true))">
            <Icon name="doc" :size="14" />{{ t('canvas.topbar.menu.details') }}
          </button>
          <div class="my-1 h-px bg-line" />
          <button
            type="button"
            role="menuitem"
            class="editor-menu-item !text-err hover:!bg-err/10"
            :disabled="!wf.id"
            data-testid="editor-menu-delete"
            @click="menu(() => (showDelete = true))"
          >
            <Icon name="trash" :size="14" />{{ t('canvas.topbar.menu.delete') }}
          </button>
        </div>
      </div>
    </header>

    <div v-if="errorMsg" class="flex shrink-0 items-center gap-2 border-b border-err/30 bg-err/10 px-4 py-2 text-[12px] text-err" role="alert">
      <Icon name="alert" :size="14" class="shrink-0" />{{ errorMsg }}
      <button
        v-if="hydrateFailed"
        type="button"
        class="ml-auto rounded-md border border-err/40 px-2.5 py-1 text-xs text-err hover:bg-err/10"
        data-testid="workflow-editor-hydrate-retry"
        @click="loadExistingWorkflow"
      >
        {{ t('common.loading.retry') }}
      </button>
      <button v-else class="ml-auto text-err/70 hover:text-err" :aria-label="t('common.buttons.close')" @click="errorMsg = ''"><Icon name="close" :size="14" /></button>
    </div>

    <div
      v-if="backupOffer"
      class="flex shrink-0 items-center gap-2 border-b border-warn/30 bg-warn/10 px-4 py-2 text-[12px] text-txt"
      role="status"
      data-testid="editor-backup-banner"
    >
      <Icon name="alert" :size="14" class="shrink-0 text-warn" />
      <span class="min-w-0 flex-1">{{ t('canvas.backup.banner', { time: fmtTime(new Date(backupOffer.savedAt).toISOString()) }) }}</span>
      <AppButton size="sm" variant="primary" data-testid="editor-backup-restore" @click="restoreBackup">{{ t('canvas.backup.restore') }}</AppButton>
      <AppButton size="sm" variant="ghost" data-testid="editor-backup-discard" @click="discardBackupOffer">{{ t('canvas.backup.discard') }}</AppButton>
    </div>

    <div ref="editorTabTrack" class="relative flex shrink-0 gap-1 border-b border-line bg-surface px-4" role="tablist">
      <button
        v-for="tab in TABS"
        :key="tab"
        type="button"
        role="tab"
        class="relative px-3.5 py-2.5 text-[13px] font-medium transition"
        :class="activeTab === tab ? 'text-txt' : 'text-txt3 hover:text-txt2'"
        :aria-selected="activeTab === tab"
        :disabled="tab !== 'canvas' && !wf.id"
        :data-testid="`workflow-tab-${tab}`"
        :data-editor-tab-active="activeTab === tab ? 'true' : undefined"
        @click="activeTab = tab"
      >
        {{ t(`canvas.topbar.tabs.${tab}`) }}
      </button>
      <span
        class="app-tabs-indicator pointer-events-none absolute bottom-0 left-0 h-0.5 rounded-full bg-accent"
        data-testid="workflow-tabs-indicator"
        :style="editorTabIndicator"
        aria-hidden="true"
      />
    </div>

    <div v-show="activeTab === 'canvas'" class="flex min-h-0 flex-1">
      <NodePalette
        v-if="!preview"
        :items="paletteItems"
        :agents-loading="!editor.agentsLoaded.value"
        :placing-key="placingKey"
        @place="placeFromPalette"
        @add="addFromPalette"
      />
      <div class="relative min-w-0 flex-1 overflow-hidden" data-testid="workflow-editor-canvas-host">
        <WorkflowCanvas
          v-if="editable && preview"
          :key="`preview-${preview.version}`"
          :nodes="preview.nodes"
          :edges="preview.edges"
          mode="view"
          :agents="editor.agents.value"
          auto-layout-on-init
          data-testid="workflow-editor-preview-canvas"
        />
        <WorkflowCanvas
          v-else-if="editable"
          ref="canvasRef"
          :nodes="wf.nodes"
          :edges="wf.edges"
          mode="edit"
          :editor="editor"
          auto-layout-on-init
          @save="saveNow()"
        />
        <div
          v-if="preview"
          class="cchrome absolute left-1/2 top-3 z-20 inline-flex -translate-x-1/2 items-center gap-2 py-1 pl-3 pr-1 text-[12px] text-txt"
          role="status"
          data-testid="editor-preview-banner"
        >
          <Icon name="history" :size="13" class="text-accent-2" />
          <span>{{ t('canvas.versions.previewing', { n: preview.version }) }}</span>
          <AppButton
            v-if="preview.version !== wf.version"
            size="sm"
            variant="primary"
            :disabled="restoringVersion !== null"
            data-testid="editor-preview-restore"
            @click="askRestore(preview.version)"
          >
            {{ t('canvas.versions.restoreThis') }}
          </AppButton>
          <AppButton size="sm" variant="ghost" data-testid="editor-preview-exit" @click="exitPreview">{{ t('canvas.versions.exitPreview') }}</AppButton>
        </div>
        <WorkflowVersionDrawer
          v-if="showVersions && wf.id && editable"
          :workflow-id="wf.id"
          :latest-version="wf.version"
          :published-version="wf.publishedVersion ?? 0"
          :preview-version="preview?.version ?? null"
          :restoring="restoringVersion"
          @close="closeVersions"
          @preview="previewVersion"
          @restore="askRestore"
        />
        <HardLoadLayer
          v-if="hydrating"
          :stage="t('pages.workflowEditor.loading')"
          data-testid="workflow-editor-hydrate-layer"
          :show-retry="false"
        />
        <div
          v-else-if="hydrateFailed"
          class="absolute inset-0 z-[2] flex flex-col items-center justify-center gap-3 bg-base/90 px-4 text-center"
          data-testid="workflow-editor-hydrate-failed"
          role="alert"
        >
          <p class="text-[13px] text-err">{{ t('pages.workflowEditor.loadFailed') }}</p>
          <button
            type="button"
            class="inline-flex min-h-11 items-center rounded-lg border border-line bg-surface px-3 text-[12px] font-medium text-txt hover:bg-elevated"
            data-testid="workflow-editor-hydrate-retry-canvas"
            @click="loadExistingWorkflow"
          >
            {{ t('common.loading.retry') }}
          </button>
        </div>

        <Transition name="insp">
          <div v-if="inspectorNode && !preview && !showVersions" class="absolute bottom-0 right-0 top-0 z-20 flex">
            <NodeInspector
              :key="inspectorNode.id"
              :node="inspectorNode"
              :all-nodes="wf.nodes"
              :edges="wf.edges"
              :agents="editor.agents.value"
              :agents-loaded="editor.agentsLoaded.value"
              :focus-goal-tick="editor.focusGoalTick.value"
              @close="editor.closeInspector()"
              @delete="deleteInspectorNode"
            />
          </div>
        </Transition>
      </div>
    </div>

    <Transition name="ui-fade" mode="out-in">
      <WorkflowRunHistoryTab v-if="activeTab === 'runs' && wf.id" :key="'runs'" :workflow-id="wf.id" class="min-h-0 flex-1" />
      <WorkflowApiTab v-else-if="activeTab === 'api' && wf.id" :key="'api'" :workflow="wf" class="min-h-0 flex-1" />
    </Transition>

    <AppModal :open="showPublish" :title="t('pages.workflowEditor.publish.title', { name: wf.name })" :width="440" @close="!publishing && (showPublish = false)">
      <Transition name="pub" mode="out-in">
        <div v-if="published" key="done" class="flex flex-col items-center py-6 text-center">
          <div class="pub-pop mb-3 flex h-16 w-16 items-center justify-center rounded-full bg-ok/15 text-ok">
            <Icon name="check" :size="34" />
          </div>
          <div class="text-[15px] font-semibold text-txt">{{ t('canvas.publish.done', { n: wf.publishedVersion }) }}</div>
          <div class="mt-1 text-[12px] text-txt3">{{ t('canvas.publish.doneNote') }}</div>
        </div>
        <div v-else key="confirm" class="space-y-4">
          <div class="flex items-center justify-center gap-3 py-1">
            <span class="chip text-txt2">
              {{ wf.publishedVersion ? t('canvas.publish.currentPublished', { n: wf.publishedVersion }) : t('canvas.publish.neverPublished') }}
            </span>
            <Icon name="arrow-left" :size="16" class="rotate-180 text-txt3" />
            <span class="rounded-md bg-accent-dim px-2.5 py-1 text-[13px] font-semibold text-accent-2">{{ t('canvas.publish.target', { n: wf.version }) }}</span>
          </div>
          <p class="text-[12px] leading-5 text-txt3">{{ t('canvas.publish.body') }}</p>
          <p v-if="saver.dirty.value" class="text-[12px] leading-5 text-warn" data-testid="editor-publish-dirty">{{ t('canvas.publish.dirtyNote') }}</p>
          <p v-else-if="wf.status === 'published'" class="text-[12px] leading-5 text-ok">{{ t('canvas.publish.alreadyPublished') }}</p>
          <div v-if="graphError" class="flex items-start gap-2 rounded-md border border-err/30 bg-err/10 px-3 py-2 text-[12px] text-err">
            <Icon name="alert" :size="14" class="mt-0.5 shrink-0" />{{ graphError }}
          </div>
          <div v-if="publishError && publishError !== graphError" class="flex items-start gap-2 rounded-md border border-err/30 bg-err/10 px-3 py-2 text-[12px] text-err">
            <Icon name="alert" :size="14" class="mt-0.5" />{{ publishError }}
          </div>
        </div>
      </Transition>
      <template v-if="!published" #footer>
        <AppButton variant="ghost" :disabled="publishing" @click="showPublish = false">{{ t('common.buttons.cancel') }}</AppButton>
        <AppButton
          variant="primary"
          icon="check"
          :disabled="publishing || !!graphError || (wf.status === 'published' && !saver.dirty.value)"
          data-testid="editor-publish-confirm"
          @click="confirmPublish"
        >
          {{ publishing ? t('common.buttons.publishing') : t('common.buttons.confirmPublish') }}
        </AppButton>
      </template>
    </AppModal>

    <RunLaunchModal
      :open="showRun"
      :workflow-id="wf.id"
      :project-id="wf.projectId"
      :workflow-name="wf.name"
      :fields="runFields"
      :run-inputs="runInputs"
      :run-images="runImages"
      :draft-restored="draftRestored"
      :hint-extra="t('pages.workflowEditor.runDraftHint')"
      :before-start="beforeRunStart"
      @close="showRun = false"
      @view-run="onViewRun"
      @save-draft="saveRunDraftClick"
      @started="onRunStarted"
      @update:loading="running = $event"
    />

    <AppDrawer :open="showOverview" :title="t('pages.workflowEditor.overview.title')" :width="400" @close="showOverview = false">
      <div class="space-y-4 p-4">
        <div>
          <label class="label" for="wf-desc">{{ t('pages.workflowEditor.descLabel') }}</label>
          <textarea
            id="wf-desc"
            v-model="wf.description"
            rows="3"
            class="input resize-y text-[12.5px]"
            :placeholder="t('pages.workflowEditor.descPlaceholder')"
            data-testid="editor-description"
          />
        </div>
        <div
          class="flex items-start gap-2 rounded-md border px-3 py-2 text-[12px]"
          :class="graphError ? 'border-err/30 bg-err/10 text-err' : 'border-ok/30 bg-ok/10 text-ok'"
        >
          <Icon :name="graphError ? 'alert' : 'check'" :size="14" class="mt-0.5 shrink-0" />
          <span>{{ graphError || t('pages.workflowEditor.overview.valid') }}</span>
        </div>
        <div class="flex gap-2 text-center text-[12px]">
          <div class="flex-1 rounded-md border border-line bg-base/40 py-2">
            <div class="text-[17px] font-semibold text-txt">{{ wf.nodes.length }}</div>
            <div class="text-txt3">{{ t('pages.workflowEditor.overview.nodes') }}</div>
          </div>
          <div class="flex-1 rounded-md border border-line bg-base/40 py-2">
            <div class="text-[17px] font-semibold text-txt">{{ wf.edges.length }}</div>
            <div class="text-txt3">{{ t('pages.workflowEditor.overview.edges') }}</div>
          </div>
        </div>
        <div class="space-y-2">
          <div class="text-[11px] uppercase tracking-wider text-txt3">{{ t('pages.workflowEditor.overview.nodeList') }}</div>
          <button
            v-for="n in wf.nodes"
            :key="n.id"
            type="button"
            class="w-full rounded-md border border-line bg-base/40 p-2.5 text-left transition hover:border-line-strong"
            @click="jumpToIssue(n.id); showOverview = false"
          >
            <div class="flex items-center gap-2">
              <div class="flex h-6 w-6 shrink-0 items-center justify-center rounded bg-elevated">
                <Icon :name="NODE_ICONS[n.type] || 'alert'" :size="14" class="text-accent-2" />
              </div>
              <span class="flex-1 truncate text-[13px] font-medium text-txt">{{ n.label }}</span>
              <span class="chip text-txt3">{{ NODE_DEFS[n.type]?.label || n.type }}</span>
            </div>
            <div v-if="nodeChips(n).length" class="mt-1.5 flex flex-wrap gap-1 pl-8">
              <span v-for="(chip, i) in nodeChips(n)" :key="i" class="chip border-line text-txt3">{{ chip }}</span>
            </div>
          </button>
        </div>
      </div>
    </AppDrawer>

    <AppModal
      :open="restoreAsk !== null"
      :title="t('canvas.versions.restoreTitle', { n: restoreAsk ?? 0 })"
      :width="420"
      @close="restoringVersion === null && (restoreAsk = null)"
    >
      <p class="text-sm text-txt2">{{ t('canvas.versions.restoreBody', { n: restoreAsk ?? 0, next: wf.version + 1 }) }}</p>
      <p v-if="saver.dirty.value" class="mt-2 text-sm text-warn" data-testid="editor-restore-dirty">{{ t('canvas.versions.restoreDirty') }}</p>
      <template #footer>
        <AppButton variant="ghost" :disabled="restoringVersion !== null" @click="restoreAsk = null">{{ t('common.buttons.cancel') }}</AppButton>
        <AppButton variant="primary" icon="refresh" :disabled="restoringVersion !== null" data-testid="editor-restore-confirm" @click="confirmRestore">
          {{ restoringVersion !== null ? t('canvas.versions.restoring') : t('canvas.versions.restoreConfirm') }}
        </AppButton>
      </template>
    </AppModal>

    <AppModal :open="!!leavePrompt" :title="t('canvas.leave.title')" :width="440" :close-on-backdrop="false" close-on-esc @close="chooseLeave('cancel')">
      <p class="text-sm text-txt2">{{ t('canvas.leave.body', { name: wf.name }) }}</p>
      <template #footer>
        <AppButton variant="ghost" data-testid="editor-leave-cancel" @click="chooseLeave('cancel')">{{ t('canvas.leave.cancel') }}</AppButton>
        <AppButton variant="outline" data-testid="editor-leave-discard" @click="chooseLeave('discard')">{{ t('canvas.leave.discard') }}</AppButton>
        <AppButton variant="primary" icon="check" data-testid="editor-leave-save" @click="chooseLeave('save')">{{ t('canvas.leave.save') }}</AppButton>
      </template>
    </AppModal>

    <ExportVersionModal
      v-if="showExport && wf.id"
      :open="showExport"
      :workflow-id="wf.id"
      :workflow-name="wf.name"
      :description="wf.description"
      :needs-repo="wf.needsRepo"
      :status="wf.status"
      :local-draft="{ nodes: wf.nodes, edges: wf.edges }"
      @close="showExport = false"
    />

    <CopyWorkflowModal
      v-if="copyModal"
      :open="!!copyModal"
      :source-id="copyModal.sourceId"
      :source-name="copyModal.sourceName"
      :suggested-name="copyModal.suggestedName"
      :existing-names="copyModal.existing"
      @close="copyModal = null"
      @copied="onCopied"
    />

    <AppModal :open="showDelete" :title="t('canvas.topbar.deleteTitle', { name: wf.name })" :width="420" @close="!deleteBusy && (showDelete = false)">
      <p class="text-sm text-txt2">{{ t('canvas.topbar.deleteBody') }}</p>
      <template #footer>
        <AppButton variant="ghost" :disabled="deleteBusy" @click="showDelete = false">{{ t('common.buttons.cancel') }}</AppButton>
        <AppButton variant="danger" icon="trash" :disabled="deleteBusy" data-testid="editor-delete-confirm" @click="confirmDelete">{{ t('common.buttons.delete') }}</AppButton>
      </template>
    </AppModal>

    <AppModal :open="showDiscardConfirm" :title="t('pages.workflowIO.import.discardTitle')" :width="420" @close="onDiscardCancel">
      <p class="text-sm text-txt2">{{ t('pages.workflowIO.import.discardBody') }}</p>
      <template #footer>
        <AppButton variant="ghost" @click="onDiscardCancel">{{ t('common.buttons.cancel') }}</AppButton>
        <AppButton variant="primary" @click="onDiscardConfirm">{{ t('pages.workflowIO.import.discardConfirm') }}</AppButton>
      </template>
    </AppModal>

    <input ref="fileInput" type="file" accept=".json" class="hidden" @change="handleFileChange" />
  </div>
</template>

<style scoped>
.app-tabs-indicator {
  transition:
    transform var(--dur-ui) var(--ease-out-expo),
    width var(--dur-ui) var(--ease-out-expo),
    opacity var(--dur-ui) ease;
}

.editor-menu-item {
  display: flex;
  width: 100%;
  align-items: center;
  gap: 8px;
  border-radius: 6px;
  padding: 6px 10px;
  font-size: 12.5px;
  color: rgb(var(--c-txt2));
  text-align: left;
}
.editor-menu-item:hover:not(:disabled) {
  background: rgb(var(--c-elevated));
  color: rgb(var(--c-txt));
}
.editor-menu-item:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.insp-enter-active,
.insp-leave-active {
  transition:
    transform var(--dur-overlay) var(--ease-out-expo),
    opacity var(--dur-overlay) ease;
}
.insp-enter-from,
.insp-leave-to {
  transform: translateX(24px);
  opacity: 0;
}
@media (prefers-reduced-motion: reduce) {
  .insp-enter-active,
  .insp-leave-active {
    transition: none;
  }
}

.pub-enter-active,
.pub-leave-active {
  transition: opacity var(--dur-ui) ease, transform var(--dur-ui) ease;
}
.pub-enter-from {
  opacity: 0;
  transform: translateY(6px);
}
.pub-leave-to {
  opacity: 0;
  transform: translateY(-6px);
}
.pub-pop {
  animation: pub-pop var(--dur-overlay) cubic-bezier(0.16, 1, 0.3, 1);
}
@keyframes pub-pop {
  0% {
    transform: scale(0.4);
    opacity: 0;
  }
  60% {
    transform: scale(1.12);
  }
  100% {
    transform: scale(1);
    opacity: 1;
  }
}
</style>
