<script setup lang="ts">
import { ref, reactive, computed, onMounted, nextTick, onBeforeUnmount, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import AppButton from '@/components/ui/AppButton.vue'
import StatusPill from '@/components/ui/StatusPill.vue'
import WorkflowCanvas from '@/components/canvas/WorkflowCanvas.vue'
import NodePalette from '@/components/canvas/NodePalette.vue'
import NodeInspector from '@/components/canvas/NodeInspector.vue'
import EdgeInspector from '@/components/canvas/EdgeInspector.vue'
import AppDrawer from '@/components/ui/AppDrawer.vue'
import AppModal from '@/components/ui/AppModal.vue'
import RunLaunchModal, { type InputField } from '@/components/workflow/RunLaunchModal.vue'
import ExportVersionModal from '@/components/workflow/ExportVersionModal.vue'
import WorkflowApiTab from '@/components/workflow/WorkflowApiTab.vue'
import WorkflowRunHistoryTab from '@/components/workflow/WorkflowRunHistoryTab.vue'
import HardLoadLayer from '@/components/run/HardLoadLayer.vue'
import { useWorkflowAskInputs } from '@/lib/run/useWorkflowAskInputs'
import { api } from '@/lib/api/api'
import { useWorkflowImport } from '@/lib/run/useWorkflowImport'
import { getAgentProfile } from '@/lib/run/workflowIO'
import { cleanOutputConfigForSave, migrateAndCleanOutputNodes } from '@/lib/shared/migrateOutputConfig'
import { fmtTime } from '@/lib/shared/format'
import { clearRunDraft, mergeRunDraft, saveRunDraft } from '@/lib/run/runDraft'
import { useToast } from '@/lib/composables/useToast'
import { useWorkflowFavorites } from '@/lib/run/useWorkflowFavorites'
import {
  isGraphDirty,
  isMetaDirty,
  saveDraftBranch,
  shouldSaveBeforeRun,
  snapshotGraph,
  snapshotMeta,
  type GraphBaseline,
  type MetaBaseline,
} from '@/lib/run/workflowEditorDirty'
import { NODE_DEFS, syncHumanGateFormDefaults } from '@/data/nodeRegistry'
import type { ClarifyImage, NodeType, WFNode, WFEdge, Workflow, WorkflowVersion } from '@/lib/shared/types'
import { readStoredProjectId } from '@/lib/composables/useProjectContext'
import { useBreakpoint } from '@/lib/composables/useBreakpoint'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const toast = useToast()
const { isFavorite, toggleFavorite } = useWorkflowFavorites()
const { isMobile } = useBreakpoint()
const showFlowPeek = ref(false)
const routeId = route.params.id as string

function toggleCurrentFavorite() {
  if (!wf.id) return
  toggleFavorite(wf.id, { name: wf.name })
}

type EditorTab = 'canvas' | 'runs' | 'api'
const activeTab = ref<EditorTab>('canvas')
const editorTabTrack = ref<HTMLElement | null>(null)
const editorTabIndicator = ref<Record<string, string>>({
  opacity: '0',
  transform: 'translateX(0)',
  width: '0px',
})

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
  editorTabIndicator.value = {
    opacity: '1',
    transform: `translateX(${left}px)`,
    width: `${width}px`,
  }
}

watch(activeTab, () => {
  void nextTick(updateEditorTabIndicator)
})
onMounted(() => {
  void nextTick(updateEditorTabIndicator)
  window.addEventListener('resize', updateEditorTabIndicator)
})
onBeforeUnmount(() => {
  window.removeEventListener('resize', updateEditorTabIndicator)
})

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
  updatedAt: '',
  needsRepo: false,
  nodes: [],
  edges: [],
})
const { fields: askFieldsComputed } = useWorkflowAskInputs(wf)

/** Project-detail Tab query key for the pipelines list (see PROJECT_TABS in useProjectDetail). */
const PROJECT_WORKFLOWS_TAB = 'workflows'

/** Back to the owning project's pipelines Tab, or the project list when unowned. */
function goBackToProject() {
  if (wf.projectId) {
    router.push(`/projects/${wf.projectId}?tab=${PROJECT_WORKFLOWS_TAB}`)
    return
  }
  router.push('/projects')
}

const saving = ref(false)
const running = ref(false)
const errorMsg = ref('')
/** Non-new route hydrate gate — HardLoadLayer until getWorkflow settles (plan g3). */
const hydrating = ref(routeId !== 'new')
const hydrateFailed = ref(false)

const selectedNode = ref<string | null>(null)
const selectedEdge = ref<string | null>(null)
const outputMigrated = ref(false)

const baselineGraph = ref<GraphBaseline | null>(null)
const baselineMeta = ref<MetaBaseline | null>(null)

function applyOutputMigration() {
  // Migrate + clean before baseline so meta-only Save does not invent a
  // client-side graph dirty from prepareWorkflowForSave stripping result.
  outputMigrated.value = migrateAndCleanOutputNodes(wf.nodes)
}

function captureBaseline() {
  // Snapshot after migrate so legacy output migrate does not look like user dirty.
  baselineGraph.value = snapshotGraph(wf)
  baselineMeta.value = snapshotMeta(wf)
}

const graphDirty = computed(() => isGraphDirty(wf, baselineGraph.value))
const metaDirty = computed(() => isMetaDirty(wf, baselineMeta.value))
const anyDirty = computed(() => graphDirty.value || metaDirty.value)

async function hydrate(fn: () => Promise<Partial<Workflow>>) {
  Object.assign(wf, await fn())
  applyOutputMigration()
  await nextTick()
  captureBaseline()
}

function prepareWorkflowForSave() {
  for (const n of wf.nodes) {
    if (n.type === 'output' && n.config) {
      n.config = cleanOutputConfigForSave(n.config)
    }
  }
}

async function loadExistingWorkflow() {
  hydrating.value = true
  hydrateFailed.value = false
  errorMsg.value = ''
  try {
    await hydrate(() => api.getWorkflow(routeId))
    hydrateFailed.value = false
  } catch {
    hydrateFailed.value = true
    // Do not captureBaseline as an editable empty graph on hydrate failure (plan g3.2).
    errorMsg.value = t('pages.workflowEditor.loadFailed')
  } finally {
    hydrating.value = false
  }
}

onMounted(async () => {
  if (routeId === 'new') {
    hydrating.value = false
    if (!wf.projectId) {
      // No project context — send user to pick/create a project first.
      errorMsg.value = t('pages.workflowEditor.projectRequired')
      await nextTick()
      captureBaseline()
      return
    }
    await nextTick()
    captureBaseline()
    return
  }
  await loadExistingWorkflow()
})

async function saveDraft() {
  const branch = saveDraftBranch(graphDirty.value, metaDirty.value)
  if (branch === 'noop') {
    toast.success(t('common.toast.noChanges'))
    return
  }
  saving.value = true
  errorMsg.value = ''
  try {
    prepareWorkflowForSave()
    await hydrate(() => api.saveWorkflow(wf))
    outputMigrated.value = false
    toast.success(t('common.toast.saved'))
  } catch (e: any) {
    errorMsg.value = String(e?.message || e)
  } finally {
    saving.value = false
  }
}
// --- publish (confirm before freezing an immutable version snapshot) ------
const showPublish = ref(false)
const showExport = ref(false)
const publishError = ref('')
const published = ref(false)

const {
  fileInput,
  showDiscardConfirm,
  triggerImport,
  onDiscardCancel,
  onDiscardConfirm,
  handleFileChange,
} = useWorkflowImport({ dirty: () => anyDirty.value })

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
  saving.value = true
  publishError.value = ''
  errorMsg.value = ''
  try {
    // Save first so a new (id-less) workflow gets its server-assigned id before
    // we publish; absorb the response so wf.id is populated.
    prepareWorkflowForSave()
    await hydrate(() => api.saveWorkflow(wf))
    await hydrate(() => api.publishWorkflow(wf.id))
    // Brief success animation before dismissing the dialog.
    published.value = true
    setTimeout(() => { showPublish.value = false }, 1100)
  } catch (e: any) {
    publishError.value = t('pages.workflowEditor.publishFailed') + String(e?.message || e)
  } finally {
    saving.value = false
  }
}
// --- run launch (collect declared inputs before starting) ----------------
const showRun = ref(false)
const runFields = ref<InputField[]>([])
const runInputs = ref<Record<string, string>>({})
const runImages = ref<Record<string, ClarifyImage[]>>({})
const draftRestored = ref(false)

function fieldOptions(f: InputField): string[] {
  return String(f.options || '').split(/[,，]/).map((s) => s.trim()).filter(Boolean)
}

// Run-launch inputs are the global variables marked ask=true.
function askFields(): InputField[] {
  return askFieldsComputed.value
}

async function openRun() {
  if (graphError.value) {
    errorMsg.value = graphError.value
    return
  }
  draftRestored.value = false
  runFields.value = askFields()
  const seed: Record<string, string> = {}
  const imgSeed: Record<string, ClarifyImage[]> = {}
  for (const f of runFields.value) {
    seed[f.key] = f.default || (f.type === 'select' ? fieldOptions(f)[0] || '' : '')
    imgSeed[f.key] = []
  }
  const keys = runFields.value.map((f) => f.key)
  const merged = await mergeRunDraft(wf.id, seed, imgSeed, keys)
  runInputs.value = merged.inputs
  runImages.value = merged.images
  draftRestored.value = merged.restored
  showRun.value = true
}

async function saveRunDraftClick() {
  if (!wf.id) return
  const images: Record<string, ClarifyImage[]> = {}
  for (const [k, v] of Object.entries(runImages.value)) {
    images[k] = v ? [...v] : []
  }
  const result = await saveRunDraft(wf.id, { ...runInputs.value }, images)
  if (result === 'ok') toast.success(t('common.toast.draftSaved'))
  else if (result === 'quota_exceeded' || result === 'partial') {
    // Text already on disk / fields persisted — warn per F4 (review v3), not error.
    toast.warn(t('common.toast.draftTooLarge'))
  } else toast.error(t('common.toast.draftSaveFailed'))
}

async function beforeRunStart() {
  // Only persist when the graph is dirty (or the workflow has no id yet).
  // Meta-only edits are intentionally skipped so published stays published.
  errorMsg.value = ''
  if (!shouldSaveBeforeRun(!!wf.id, graphDirty.value)) return
  prepareWorkflowForSave()
  await hydrate(() => api.saveWorkflow(wf))
}

function onRunStarted() {
  void clearRunDraft(wf.id)
}

function onViewRun(runId: string) {
  router.push('/runs/' + runId)
}

function onConnect({ source, target, sourceHandle }: { source: string; target: string; sourceHandle?: string | null }) {
  if (source === target) return
  // A connection dragged from a branch node's per-case handle (`case-<i>`) is
  // stored as that rule's `goto`, not as a real edge (the engine routes by goto).
  const m = sourceHandle?.match(/^case-(\d+)$/)
  if (m) {
    const node = wf.nodes.find((n) => n.id === source)
    const idx = Number(m[1])
    const cases = (node?.config?.cases as any[]) || []
    if (cases[idx]) cases[idx].goto = target
    return
  }
  // A connection dragged from a human_gate / structured-exit action handle
  // (`action-<id>`) is stored as that action's `goto` (branch-style routing),
  // not a real edge. app_preview no longer exposes action handles.
  const am = sourceHandle?.match(/^action-(.+)$/)
  if (am) {
    const node = wf.nodes.find((n) => n.id === source)
    if (node?.type === 'app_preview') {
      // Fold into a normal success edge (single-exit semantics).
      if (hasUnconditionalSuccessEdge(source)) {
        errorMsg.value = t('pages.workflowEditor.duplicateSuccessEdge')
        return
      }
      const id = `e_${Math.random().toString(36).slice(2, 7)}`
      wf.edges.push({ id, source, target, kind: 'success' })
      return
    }
    if (node?.type === 'test' || node?.type === 'review') {
      if (!node.config.exits) node.config.exits = { pass: { goto: '' }, fail: { goto: '' } }
      const exit = am[1]
      if (exit === 'pass' || exit === 'fail') {
        node.config.exits[exit].goto = target
      }
      return
    }
    if (node?.type !== 'human_gate') return
    const actions = (node?.config?.actions as any[]) || []
    const act = actions.find((a) => String(a?.id ?? '') === am[1])
    if (act) act.goto = target
    return
  }
  // Reject an ambiguous success fan-out: the FSM takes exactly one outgoing
  // edge per node (it never forks), so a second unconditional success edge from
  // the same node would silently never run. Branch nodes route via config, and
  // guarded (when) / failure / rollback edges are unaffected.
  const srcNode = wf.nodes.find((n) => n.id === source)
  if (srcNode?.type !== 'branch' && hasUnconditionalSuccessEdge(source)) {
    errorMsg.value = t('pages.workflowEditor.duplicateSuccessEdge')
    return
  }
  const id = `e_${Math.random().toString(36).slice(2, 7)}`
  wf.edges.push({ id, source, target, kind: 'success' })
}

// hasUnconditionalSuccessEdge reports whether the given node already has an
// outgoing success edge with no `when` guard — the one edge that always fires.
function hasUnconditionalSuccessEdge(source: string): boolean {
  return wf.edges.some(
    (e) => e.source === source && (e.kind === 'success' || !e.kind) && !String(e.when ?? '').trim(),
  )
}
function onMoveNode({ id, x, y }: { id: string; x: number; y: number }) {
  const n = wf.nodes.find((nn) => nn.id === id)
  if (n) n.position = { x, y }
}

// Workflow overview drawer: a per-node summary of the graph (type + key config),
// edge count and structural validation — the "details" companion to the
// per-node inspector.
const showOverview = ref(false)
function nodeChips(n: WFNode): string[] {
  const c = (n.config || {}) as Record<string, any>
  const out: string[] = []
  const profile = getAgentProfile(c)
  if (profile) out.push(t('pages.workflowEditor.nodeChips.agent', { name: profile }))
  if (c.produces) out.push(t('pages.workflowEditor.nodeChips.artifact', { name: c.produces }))
  if (n.type === 'branch') out.push(t('pages.workflowEditor.nodeChips.routes', { n: c.cases?.length || 0 }))
  if (n.type === 'input') out.push(t('pages.workflowEditor.nodeChips.variables', { n: (c.variables || []).filter((v: any) => v?.name).length }))
  if (n.type === 'human_gate') out.push(t('pages.workflowEditor.nodeChips.actions', { n: (c.actions || []).length }))
  return out
}

const showVersions = ref(false)
const versions = ref<WorkflowVersion[]>([])
const loadingVersions = ref(false)
const restoring = ref(0)

async function openVersions() {
  if (!wf.id) return
  showVersions.value = true
  loadingVersions.value = true
  try {
    versions.value = await api.listWorkflowVersions(wf.id)
  } catch {
    versions.value = []
  } finally {
    loadingVersions.value = false
  }
}
async function rollback(version: number) {
  if (!wf.id) return
  restoring.value = version
  try {
    await hydrate(() => api.restoreWorkflowVersion(wf.id, version))
    clearSel()
    showVersions.value = false
  } catch {
    errorMsg.value = t('pages.workflowEditor.rollbackFailed')
  } finally {
    restoring.value = 0
  }
}

const curNode = computed(() => wf.nodes.find((n) => n.id === selectedNode.value) || null)
const curEdge = computed(() => wf.edges.find((e) => e.id === selectedEdge.value) || null)

function selectNode(id: string) {
  selectedNode.value = id
  selectedEdge.value = null
}
function selectEdge(id: string) {
  selectedEdge.value = id
  selectedNode.value = null
}
function clearSel() {
  selectedNode.value = null
  selectedEdge.value = null
}

// Structural contract: a runnable pipeline has exactly one input (start) and
// at least one output (end); the input has no incoming edges and outputs no
// outgoing edges. Returns an error message, or '' when valid.
function graphErrorMsg(): string {
  const inputs = wf.nodes.filter((n) => n.type === 'input')
  const outputs = wf.nodes.filter((n) => n.type === 'output')
  if (!inputs.length) return t('pages.workflowEditor.graphErrors.missingInput')
  if (inputs.length > 1) return t('pages.workflowEditor.graphErrors.tooManyInputs')
  if (!outputs.length) return t('pages.workflowEditor.graphErrors.missingOutput')
  const incoming = new Set(wf.edges.map((e) => e.target))
  const outgoing = new Set(wf.edges.map((e) => e.source))
  if (incoming.has(inputs[0].id)) return t('pages.workflowEditor.graphErrors.inputHasIncoming')
  if (outputs.some((o) => outgoing.has(o.id))) return t('pages.workflowEditor.graphErrors.outputHasOutgoing')
  // Ambiguous success fan-out: a non-branch node with 2+ unconditional success
  // edges — only the first ever runs (the FSM never forks).
  const uncondSuccess = new Map<string, number>()
  for (const e of wf.edges) {
    if ((e.kind && e.kind !== 'success') || String(e.when ?? '').trim()) continue
    const src = wf.nodes.find((n) => n.id === e.source)
    if (src?.type === 'branch') continue
    uncondSuccess.set(e.source, (uncondSuccess.get(e.source) ?? 0) + 1)
  }
  for (const [src, n] of uncondSuccess) {
    if (n > 1) {
      const node = wf.nodes.find((nn) => nn.id === src)
      return t('pages.workflowEditor.graphErrors.duplicateSuccessFanOut', { label: node?.label || src })
    }
  }
  return ''
}
const graphError = computed(graphErrorMsg)

function dropNode({ type, x, y }: { type: NodeType; x: number; y: number }) {
  // Enforce the single-input rule at the source: only one input node allowed.
  if (type === 'input' && wf.nodes.some((n) => n.type === 'input')) {
    errorMsg.value = t('pages.workflowEditor.singleInputOnly')
    return
  }
  const def = NODE_DEFS[type]
  const id = `${type}_${Math.random().toString(36).slice(2, 6)}`
  const config = JSON.parse(JSON.stringify(def.defaults))
  if (type === 'human_gate') syncHumanGateFormDefaults(config)
  const node: WFNode = { id, type, label: def.label, position: { x, y }, config }
  wf.nodes.push(node)
  selectNode(id)
}

function removeNodeById(id: string) {
  if (!wf.nodes.some((n) => n.id === id)) return
  wf.nodes = wf.nodes.filter((n) => n.id !== id)
  wf.edges = wf.edges.filter((e) => e.source !== id && e.target !== id)
  // Clear structured gate gotos pointing at the removed node.
  for (const n of wf.nodes) {
    if (n.type !== 'test' && n.type !== 'review') continue
    const exits = n.config?.exits as any
    if (!exits) continue
    if (exits.pass?.goto === id) exits.pass.goto = ''
    if (exits.fail?.goto === id) exits.fail.goto = ''
  }
  if (selectedNode.value === id) clearSel()
}
function clearStructuredGoto({ edgeId }: { edgeId: string }) {
  const parts = edgeId.split(':')
  if (parts.length !== 3) return
  const [, nodeId, exit] = parts
  const node = wf.nodes.find((n) => n.id === nodeId)
  if (!node?.config?.exits) return
  if (exit === 'pass' || exit === 'fail') {
    node.config.exits[exit].goto = ''
  }
}
function removeEdgeById(id: string) {
  if (!wf.edges.some((e) => e.id === id)) return
  wf.edges = wf.edges.filter((e) => e.id !== id)
  if (selectedEdge.value === id) clearSel()
}
function deleteNode() {
  if (curNode.value) removeNodeById(curNode.value.id)
}
function deleteEdge() {
  if (curEdge.value) removeEdgeById(curEdge.value.id)
}
</script>

<template>
  <div
    v-if="isMobile"
    class="flex h-full min-h-0 flex-col overflow-auto bg-base"
    data-testid="workflow-editor-mobile"
  >
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
      <div class="rounded-lg mb-3 flex h-10 w-10 items-center justify-center border border-info/35 bg-info/10 text-info">◇</div>
      <h3 class="text-[14px] font-semibold text-txt">{{ t('pages.workflowEditor.mobile.title') }}</h3>
      <p class="mt-2 max-w-[32ch] text-[12.5px] leading-relaxed text-txt2">{{ t('pages.workflowEditor.mobile.desc') }}</p>
      <button
        type="button"
        class="rounded-md mt-4 min-h-11 border border-line bg-transparent px-4 text-[13px] text-txt2 hover:border-accent hover:text-txt"
        data-testid="workflow-editor-peek"
        @click="showFlowPeek = !showFlowPeek"
      >
        {{ t('pages.workflowEditor.mobile.peek') }}
      </button>
    </div>
    <div
      v-if="showFlowPeek"
      class="rounded-lg mx-4 mb-6 border border-line bg-surface p-3 text-left"
      data-testid="workflow-editor-summary"
    >
      <div class="mb-2 text-[11px] uppercase tracking-wider text-txt3">{{ t('pages.workflowEditor.mobile.summaryLabel') }}</div>
      <p v-if="!wf.nodes.length" class="text-[12px] text-txt3">{{ t('pages.workflowEditor.mobile.empty') }}</p>
      <div
        v-for="n in wf.nodes"
        :key="n.id"
        class="flex items-center gap-2 border-b border-line/60 py-2 last:border-b-0"
        data-testid="workflow-editor-node-row"
      >
        <span class="h-2 w-2 shrink-0 bg-accent" :data-node-type="n.type" />
        <span class="min-w-0 truncate text-[13px] text-txt">{{ n.type }} · {{ n.label }}</span>
      </div>
      <p class="mt-2 text-[11px] text-txt3">{{ t('pages.workflowEditor.mobile.noEdit') }}</p>
    </div>
  </div>
  <div v-else class="flex h-full flex-col bg-base">
    <!-- toolbar -->
    <header class="flex min-h-14 shrink-0 items-center gap-3 border-b border-line bg-surface px-4 py-2">
      <button class="flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-txt2 hover:bg-elevated hover:text-txt" data-testid="workflow-editor-back" @click="goBackToProject">
        <Icon name="arrow-left" :size="18" />
      </button>
      <Icon name="workflow" :size="18" class="shrink-0 text-accent-2" />
      <div class="flex min-w-0 max-w-md flex-col gap-0.5">
        <input
          v-model="wf.name"
          class="min-w-0 rounded-md bg-transparent px-1.5 py-1 text-[15px] font-semibold text-txt outline-none hover:bg-elevated focus:bg-elevated"
        />
        <textarea
          v-model="wf.description"
          rows="2"
          class="max-h-16 w-full resize-none rounded-md bg-transparent px-1.5 py-0.5 text-[12px] leading-snug text-txt2 outline-none hover:bg-elevated focus:bg-elevated"
          :aria-label="t('pages.workflowEditor.descLabel')"
          :placeholder="t('pages.workflowEditor.descPlaceholder')"
        />
      </div>
      <StatusPill :status="wf.status" size="sm" />
      <span class="chip">v{{ wf.version }}</span>
      <span v-if="graphDirty" class="text-[11px] text-warn">{{ t('common.saved.unsaved') }}</span>
      <span v-else class="text-[11px] text-txt3">{{ t('common.saved.saved') }}</span>
      <div class="flex-1" />
      <AppButton variant="ghost" size="sm" icon="input" @click="triggerImport">{{ t('common.buttons.import') }}</AppButton>
      <AppButton variant="ghost" size="sm" icon="download" :disabled="!wf.id" @click="showExport = true">{{ t('common.buttons.export') }}</AppButton>
      <AppButton variant="ghost" size="sm" icon="doc" :disabled="!wf.nodes.length" @click="showOverview = true">{{ t('common.buttons.details') }}</AppButton>
      <AppButton variant="ghost" size="sm" icon="history" :disabled="!wf.id" @click="openVersions">{{ t('pages.workflowEditor.versions.title') }}</AppButton>
      <AppButton variant="outline" size="sm" icon="save" :disabled="saving || hydrating || hydrateFailed" @click="saveDraft">{{ t('common.buttons.saveDraft') }}</AppButton>
      <AppButton
        variant="ghost"
        size="sm"
        :icon="isFavorite(wf.id) ? 'star-filled' : 'star'"
        :disabled="!wf.id || hydrating || hydrateFailed"
        data-testid="editor-favorite-btn"
        :class="{ 'text-warn': isFavorite(wf.id) }"
        @click="toggleCurrentFavorite"
      >{{ isFavorite(wf.id) ? t('common.buttons.unfavorite') : t('common.buttons.favorite') }}</AppButton>
      <AppButton variant="ghost" size="sm" icon="play" :disabled="running || saving || hydrating || hydrateFailed" @click="openRun">{{ running ? t('common.buttons.starting') : t('common.buttons.run') }}</AppButton>
      <AppButton variant="primary" size="sm" icon="check" :disabled="saving || hydrating || hydrateFailed" @click="openPublish">{{ t('pages.workflowEditor.publish.toolbar', { version: wf.version + 1 }) }}</AppButton>
    </header>

    <div v-if="errorMsg" class="flex shrink-0 items-center gap-2 border-b border-err/30 bg-err/10 px-4 py-2 text-[12px] text-err">
      <Icon name="alert" :size="14" class="shrink-0" />{{ errorMsg }}
      <button
        v-if="hydrateFailed"
        type="button"
        class="rounded-md ml-auto border border-err/40 px-2.5 py-1 text-xs text-err hover:bg-err/10"
        data-testid="workflow-editor-hydrate-retry"
        @click="loadExistingWorkflow"
      >
        {{ t('common.loading.retry') }}
      </button>
      <button v-else class="ml-auto text-err/70 hover:text-err" @click="errorMsg = ''"><Icon name="close" :size="14" /></button>
    </div>

    <!-- editor tabs -->
    <div ref="editorTabTrack" class="relative flex shrink-0 gap-1 border-b border-line bg-surface px-4">
      <button
        class="relative px-3.5 py-2.5 text-sm font-medium transition"
        :class="activeTab === 'canvas' ? 'text-txt' : 'text-txt3 hover:text-txt2'"
        data-testid="workflow-tab-canvas"
        :data-editor-tab-active="activeTab === 'canvas' ? 'true' : undefined"
        @click="activeTab = 'canvas'"
      >
        编排
      </button>
      <button
        class="relative px-3.5 py-2.5 text-sm font-medium transition"
        :class="activeTab === 'runs' ? 'text-txt' : 'text-txt3 hover:text-txt2'"
        :disabled="!wf.id"
        data-testid="workflow-tab-runs"
        :data-editor-tab-active="activeTab === 'runs' ? 'true' : undefined"
        @click="activeTab = 'runs'"
      >
        运行记录
      </button>
      <button
        class="relative px-3.5 py-2.5 text-sm font-medium transition"
        :class="activeTab === 'api' ? 'text-txt' : 'text-txt3 hover:text-txt2'"
        :disabled="!wf.id"
        data-testid="workflow-tab-api"
        :data-editor-tab-active="activeTab === 'api' ? 'true' : undefined"
        @click="activeTab = 'api'"
      >
        访问 API
      </button>
      <span
        class="app-tabs-indicator pointer-events-none absolute bottom-0 left-0 h-0.5 rounded-full bg-accent"
        data-testid="workflow-tabs-indicator"
        :style="editorTabIndicator"
        aria-hidden="true"
      />
    </div>

    <!-- canvas tab (v-show keeps Vue Flow state; g4.1) -->
    <div v-show="activeTab === 'canvas'" class="flex min-h-0 flex-1">
      <NodePalette />
      <div class="relative min-w-0 flex-1" data-testid="workflow-editor-canvas-host">
        <WorkflowCanvas
          v-if="!hydrating && !hydrateFailed"
          :nodes="wf.nodes"
          :edges="wf.edges"
          mode="edit"
          :selected-node="selectedNode"
          :selected-edge="selectedEdge"
          @select-node="selectNode"
          @select-edge="selectEdge"
          @pane-click="clearSel"
          @drop-node="dropNode"
          @connect="onConnect"
          @move-node="onMoveNode"
          @remove-node="removeNodeById"
          @remove-edge="removeEdgeById"
          @clear-structured-goto="clearStructuredGoto"
        />
        <HardLoadLayer
          v-if="hydrating"
          :stage="t('pages.workflowEditor.loading')"
          data-testid="workflow-editor-hydrate-layer"
          :show-retry="false"
        />
        <div
          v-else-if="hydrateFailed"
          class="absolute inset-0 z-[2] flex flex-col items-center justify-center gap-3 bg-base/92 px-4 text-center"
          data-testid="workflow-editor-hydrate-failed"
          role="alert"
        >
          <p class="text-[13px] text-err">{{ t('pages.workflowEditor.loadFailed') }}</p>
          <button
            type="button"
            class="rounded-lg inline-flex min-h-11 items-center border border-line bg-surface px-3 text-[12px] font-medium text-txt hover:bg-elevated"
            data-testid="workflow-editor-hydrate-retry-canvas"
            @click="loadExistingWorkflow"
          >
            {{ t('common.loading.retry') }}
          </button>
        </div>
        <div
          v-else-if="!wf.nodes.length"
          class="pointer-events-none absolute inset-0 flex flex-col items-center justify-center text-center"
        >
          <div class="mb-3 flex h-14 w-14 items-center justify-center rounded-xl border border-dashed border-line-strong text-txt3">
            <Icon name="workflow" :size="26" />
          </div>
          <div class="text-sm font-medium text-txt2">{{ t('pages.workflowEditor.canvas.emptyTitle') }}</div>
          <div class="mt-1 text-xs text-txt3">{{ t('pages.workflowEditor.canvas.emptyDesc') }}</div>
        </div>
      </div>

      <!-- inspector panel -->
      <Transition name="panel">
        <div v-if="curNode || curEdge" class="w-[360px] shrink-0 border-l border-line bg-surface">
          <NodeInspector
            v-if="curNode"
            :node="curNode"
            :all-nodes="wf.nodes"
            :edges="wf.edges"
            :project-id="wf.projectId"
            :output-migration="outputMigrated && curNode.type === 'output'"
            @delete="deleteNode"
          />
          <EdgeInspector v-else-if="curEdge" :edge="curEdge" @delete="deleteEdge" />
        </div>
      </Transition>
    </div>

    <!-- runs / api tabs: short fade (g4.1) -->
    <Transition name="ui-fade" mode="out-in">
      <WorkflowRunHistoryTab
        v-if="activeTab === 'runs' && wf.id"
        :key="'runs'"
        :workflow-id="wf.id"
        class="min-h-0 flex-1"
      />
      <WorkflowApiTab
        v-else-if="activeTab === 'api' && wf.id"
        :key="'api'"
        :workflow="wf"
        class="min-h-0 flex-1"
      />
    </Transition>

    <AppModal :open="showPublish" :title="t('pages.workflowEditor.publish.title', { name: wf.name })" :width="440" @close="!saving && (showPublish = false)">
      <Transition name="pub" mode="out-in">
        <div v-if="published" key="done" class="flex flex-col items-center py-6 text-center">
          <div class="pub-pop mb-3 flex h-16 w-16 items-center justify-center rounded-full bg-ok/15 text-ok">
            <Icon name="check" :size="34" />
          </div>
          <div class="text-[15px] font-semibold text-txt">{{ t('pages.workflowEditor.publish.published', { version: wf.version }) }}</div>
          <div class="mt-1 text-[12px] text-txt3">{{ t('pages.workflowEditor.publish.frozen') }}</div>
        </div>
        <div v-else key="confirm" class="space-y-4">
          <div class="flex items-center justify-center gap-3 py-1">
            <span class="chip text-txt2">{{ t('pages.workflowEditor.publish.current', { version: wf.version }) }}</span>
            <Icon name="arrow-left" :size="16" class="rotate-180 text-txt3" />
            <span class="rounded-md bg-accent-dim px-2.5 py-1 text-[13px] font-semibold text-accent-2">{{ t('pages.workflowEditor.publish.next', { version: wf.version + 1 }) }}</span>
          </div>
          <p class="text-[12px] leading-5 text-txt3" v-html="t('pages.workflowEditor.publish.body', { version: wf.version + 1 })" />
          <div v-if="graphError" class="flex items-start gap-2 rounded-md border border-err/30 bg-err/10 px-3 py-2 text-[12px] text-err">
            <Icon name="alert" :size="14" class="mt-0.5 shrink-0" />{{ graphError }}
          </div>
          <div v-else-if="anyDirty" class="flex items-center gap-2 rounded-md border border-warn/30 bg-warn/10 px-3 py-2 text-[12px] text-warn">
            <Icon name="alert" :size="14" class="shrink-0" />{{ t('pages.workflowEditor.publish.dirtyWarning') }}
          </div>
          <div v-if="publishError && publishError !== graphError" class="flex items-start gap-2 rounded-md border border-err/30 bg-err/10 px-3 py-2 text-[12px] text-err">
            <Icon name="alert" :size="14" class="mt-0.5" />{{ publishError }}
          </div>
        </div>
      </Transition>
      <template v-if="!published" #footer>
        <AppButton variant="ghost" :disabled="saving" @click="showPublish = false">{{ t('common.buttons.cancel') }}</AppButton>
        <AppButton variant="primary" icon="check" :disabled="saving || !!graphError" @click="confirmPublish">{{ saving ? t('common.buttons.publishing') : t('common.buttons.confirmPublish') + ' v' + (wf.version + 1) }}</AppButton>
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
            class="w-full rounded-md border border-line bg-base/40 p-2.5 text-left transition hover:border-line-strong"
            :class="{ 'border-accent shadow-glow': selectedNode === n.id }"
            @click="selectNode(n.id); showOverview = false"
          >
            <div class="flex items-center gap-2">
              <div class="flex h-6 w-6 shrink-0 items-center justify-center rounded bg-elevated">
                <Icon :name="NODE_DEFS[n.type]?.icon || 'agent'" :size="14" class="text-accent-2" />
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

    <AppDrawer :open="showVersions" :title="t('pages.workflowEditor.versions.title')" :width="380" @close="showVersions = false">
      <div class="p-4">
        <p class="mb-3 text-[12px] leading-5 text-txt3">{{ t('pages.workflowEditor.versions.intro') }}</p>
        <div v-if="loadingVersions" class="py-8 text-center text-sm text-txt3">{{ t('common.buttons.loading') }}</div>
        <div v-else-if="!versions.length" class="py-8 text-center text-sm text-txt3">{{ t('pages.workflowEditor.versions.empty') }}</div>
        <div v-else class="space-y-2">
          <div
            v-for="v in versions"
            :key="v.version"
            class="flex items-center gap-3 rounded-md border border-line bg-base/40 px-3 py-2.5"
          >
            <div class="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-accent-dim text-accent-2 font-mono text-[12px] font-semibold">v{{ v.version }}</div>
            <div class="min-w-0 flex-1">
              <div class="flex items-center gap-1.5 text-[13px] font-medium text-txt">
                {{ t('pages.workflowEditor.versions.version', { n: v.version }) }}
                <span v-if="v.version === wf.version" class="chip border-ok/30 text-ok">{{ t('pages.workflowEditor.versions.current') }}</span>
              </div>
              <div class="text-[11px] text-txt3">{{ t('pages.workflowEditor.versions.publishedAt', { time: fmtTime(v.publishedAt) }) }}</div>
            </div>
            <AppButton
              size="sm"
              variant="outline"
              icon="refresh"
              :disabled="restoring !== 0 || v.version === wf.version"
              @click="rollback(v.version)"
            >{{ restoring === v.version ? t('common.buttons.rollingBack') : t('common.buttons.rollback') }}</AppButton>
          </div>
        </div>
      </div>
    </AppDrawer>

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

.panel-enter-active,
.panel-leave-active {
  transition: width var(--dur-overlay) ease, opacity var(--dur-overlay) ease;
  overflow: hidden;
}
.panel-enter-from,
.panel-leave-to {
  width: 0;
  opacity: 0;
}

/* publish dialog: confirm <-> success crossfade */
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

/* success check pop-in */
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
