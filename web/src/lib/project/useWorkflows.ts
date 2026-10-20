import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import type { InputField } from '@/components/workflow/RunLaunchModal.vue'
import type { RepoRow } from '@/components/ui/ReposEditor.vue'
import { api } from '@/lib/api/api'
import { workflowCatalogRevision } from '@/lib/api/workflowCatalogEvents'
import { useWorkspaceContext } from '@/lib/composables/useWorkspaceContext'
import { useToast } from '@/lib/composables/useToast'
import { fmtTime } from '@/lib/shared/format'
import { clearRunDraft, mergeRunDraft, saveRunDraft } from '@/lib/run/runDraft'
import { useBreakpoint } from '@/lib/composables/useBreakpoint'
import { useWorkflowImport } from '@/lib/run/useWorkflowImport'
import { useWorkflowFavorites } from '@/lib/run/useWorkflowFavorites'
import type { ClarifyImage, Workflow, WorkflowNotifyPolicy } from '@/lib/shared/types'

export function useWorkflows() {
const router = useRouter()
const { t } = useI18n()
const toast = useToast()
const { workspaceId: projectId, ensureLoaded } = useWorkspaceContext()
const { isFavorite, toggleFavorite } = useWorkflowFavorites()
const { isMobile } = useBreakpoint()
const workflows = ref<Workflow[]>([])
const loading = ref(false)
const loadError = ref('')
const search = ref('')
const visibleWorkflows = computed(() => {
 const query = search.value.trim().toLocaleLowerCase()
 return workflows.value.filter((w) => !query || `${w.name} ${w.description} ${w.id}`.toLocaleLowerCase().includes(query))
})
let sequence = 0
let workspaceGeneration = 0
function toggleWorkflowFavorite(w: Workflow) { toggleFavorite(w.id, { name: w.name }) }
let runGeneration = 0
const runTarget = ref<Workflow | null>(null)
const runFields = ref<InputField[]>([])
const runInputs = ref<Record<string, string>>({})
const runImages = ref<Record<string, ClarifyImage[]>>({})
const draftRestored = ref(false)
const openMenuId = ref<string | null>(null)
const menuPosition = ref<{ top: string; left: string } | null>(null)
const newWorkflowMenuOpen = ref(false)
const baselineModalOpen = ref(false)
const baselineName = ref('')
const baselineRepos = ref<RepoRow[]>([{ name: '', url: '', branch: '' }])
const creatingBaseline = ref(false)
const baselineCreateError = ref('')

const deleteWfTarget = ref<Workflow | null>(null)
const deletingWf = ref(false)
const deleteWfError = ref('')

const copyPreviewLoading = ref<string | null>(null)
const copyModal = ref<{ sourceId: string; sourceName: string; suggestedName: string } | null>(null)
const exportTarget = ref<Workflow | null>(null)
const { fileInput, triggerImport, handleFileChange } = useWorkflowImport({
  projectId: () => projectId.value,
  onImported: async (wf) => {
    workflows.value = [wf, ...workflows.value.filter((x) => x.id !== wf.id)]
  },
})

const savingNotifyWfId = ref<string | null>(null)

function wfNotifyMode(w: Workflow): 'off' | 'inherit' | 'custom' {
  const m = w.notifyPolicy?.mode
  if (m === 'off' || m === 'custom') return m
  return 'inherit'
}

function wfNotifyHas(w: Workflow, ev: string): boolean {
  return (w.notifyPolicy?.events || []).includes(ev)
}

async function persistWorkflowNotify(w: Workflow, policy: WorkflowNotifyPolicy) {
  const current = workspaceGeneration
  savingNotifyWfId.value = w.id
  try {
    // Notify-only PATCH: never send cached nodes/edges (review v1).
    const saved = await api.patchWorkflowNotifyPolicy(w.id, policy)
    if (current !== workspaceGeneration) return
    workflows.value = workflows.value.map((x) => (x.id === w.id ? { ...x, ...saved } : x))
    toast.success(t('pages.projectDetail.notify.wfUpdated'))
  } catch (e: any) {
    if (current !== workspaceGeneration) return
    toast.error(String(e?.message || e))
  } finally {
    if (current === workspaceGeneration) savingNotifyWfId.value = null
  }
}

async function setWorkflowNotifyMode(w: Workflow, mode: 'off' | 'inherit' | 'custom') {
  const events =
    mode === 'custom'
      ? w.notifyPolicy?.events?.length
        ? [...(w.notifyPolicy.events || [])]
        : ['waiting_human', 'failed']
      : [...(w.notifyPolicy?.events || [])]
  await persistWorkflowNotify(w, { mode, events })
}

async function toggleWorkflowNotifyEvent(w: Workflow, ev: 'waiting_human' | 'failed' | 'completed') {
  const cur = new Set(w.notifyPolicy?.events || [])
  if (cur.has(ev)) cur.delete(ev)
  else cur.add(ev)
  await persistWorkflowNotify(w, {
    mode: 'custom',
    events: (['waiting_human', 'failed', 'completed'] as const).filter((k) => cur.has(k)),
  })
}

const savingHomeWfId = ref<string | null>(null)

/** Optimistic Home-visibility toggle with rollback (plan g2.1). */
async function toggleWorkflowShowOnHome(w: Workflow, next: boolean) {
  const current = workspaceGeneration
  const prev = !!w.showOnHome
  if (prev === next) return
  workflows.value = workflows.value.map((x) => (x.id === w.id ? { ...x, showOnHome: next } : x))
  savingHomeWfId.value = w.id
  try {
    const saved = await api.patchWorkflowHomeVisibility(w.id, next)
    if (current !== workspaceGeneration) return
    workflows.value = workflows.value.map((x) => (x.id === w.id ? { ...x, ...saved } : x))
    toast.success(t('pages.projectDetail.homeVisibility.updated'))
  } catch (e: any) {
    if (current !== workspaceGeneration) return
    workflows.value = workflows.value.map((x) => (x.id === w.id ? { ...x, showOnHome: prev } : x))
    toast.error(String(e?.message || e) || t('pages.projectDetail.homeVisibility.updateFailed'))
  } finally {
    if (current === workspaceGeneration) savingHomeWfId.value = null
  }
}

const existingNames = () => workflows.value.map((w) => w.name)

function askFields(w: Workflow): InputField[] {
  const input = (w.nodes || []).find((n) => n.type === 'input')
  const vars = ((input?.config?.variables as any[]) || []).filter((v) => v && v.name && v.ask)
  return vars.map((v) => ({
    key: v.name,
    desc: v.desc,
    type: v.type === 'string' ? 'text' : v.type,
    required: v.required,
    default:
      v.type === 'repos'
        ? JSON.stringify(Array.isArray(v.value) ? v.value : [])
        : v.value == null
          ? ''
          : String(v.value),
    editable: v.editable,
    options: v.options,
  }))
}

function fieldOptions(f: InputField): string[] {
  return String(f.options || '')
    .split(/[,，]/)
    .map((s) => s.trim())
    .filter(Boolean)
}

function closeMenu() {
  openMenuId.value = null
  menuPosition.value = null
  newWorkflowMenuOpen.value = false
}

function toggleMenu(id: string, event?: MouseEvent) {
  newWorkflowMenuOpen.value = false
  openMenuId.value = openMenuId.value === id ? null : id
  const button = event?.currentTarget as HTMLElement | undefined
  if (openMenuId.value && button) {
    const bounds = button.getBoundingClientRect()
    menuPosition.value = {
      top: `${Math.max(12, Math.min(bounds.bottom + 5, window.innerHeight - 220))}px`,
      left: `${Math.max(12, Math.min(bounds.right - 168, window.innerWidth - 180))}px`,
    }
  } else menuPosition.value = null
}

function toggleNewWorkflowMenu() {
  openMenuId.value = null
  newWorkflowMenuOpen.value = !newWorkflowMenuOpen.value
}

function menuIdFor(id: string) {
  return 'workflow-more-menu-' + id
}

function onDocClick(e: MouseEvent) {
  const el = e.target as Element | null
  if (!el?.closest?.('[data-wf-menu]')) closeMenu()
  if (!el?.closest?.('[data-new-workflow-menu]')) newWorkflowMenuOpen.value = false
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    closeMenu()
    if (baselineModalOpen.value && !creatingBaseline.value) closeBaselineModal()
  }
}

function onScrollClose() {
  if (openMenuId.value) closeMenu()
}

async function reloadWorkflows() {
  const current = ++sequence
  loading.value = true
  loadError.value = ''
  try {
    if (!await ensureLoaded()) return
    const result = await api.listWorkflows()
    if (current === sequence) workflows.value = result
  } catch (cause: unknown) {
    if (current === sequence) loadError.value = cause instanceof Error ? cause.message : String(cause)
  } finally { if (current === sequence) loading.value = false }
}
function newWorkflow() {
  newWorkflowMenuOpen.value = false
  router.push('/workflows/new/edit')
}

function openBaselineModal() {
  newWorkflowMenuOpen.value = false
  baselineName.value = ''
  baselineRepos.value = [{ name: '', url: '', branch: '' }]
  baselineCreateError.value = ''
  baselineModalOpen.value = true
}

function closeBaselineModal() {
  if (creatingBaseline.value) return
  baselineModalOpen.value = false
  baselineCreateError.value = ''
}

const hasValidBaselineRepo = computed(() =>
  baselineName.value.trim() !== '' &&
  baselineRepos.value.some((repo) => repo.url.trim() !== ''),
)

async function createFromBaseline() {
  if (!hasValidBaselineRepo.value || creatingBaseline.value) return
  creatingBaseline.value = true
  const current = workspaceGeneration
  baselineCreateError.value = ''
  try {
    const created = await api.createWorkflowFromBaseline(
      projectId.value,
      baselineName.value.trim(),
      baselineRepos.value,
    )
    if (current !== workspaceGeneration) return
    workflows.value = [created, ...workflows.value.filter((workflow) => workflow.id !== created.id)]
    baselineModalOpen.value = false
    toast.success(t('pages.projectDetail.newWorkflow.created', { name: created.name }))
  } catch (e: any) {
    if (current !== workspaceGeneration) return
    baselineCreateError.value = String(e?.message || e)
  } finally {
    if (current === workspaceGeneration) creatingBaseline.value = false
  }
}

function openWorkflow(w: Workflow) {
  router.push('/workflows/' + w.id + '/edit')
}

function openEdit(w: Workflow) {
  closeMenu()
  openWorkflow(w)
}

async function openRun(w: Workflow) {
  closeMenu()
  const current = ++runGeneration
  try {
    const head = await api.getWorkflow(w.id)
    if (current !== runGeneration) return
    const fields = askFields(head)
    const seed: Record<string, string> = {}
    const images: Record<string, ClarifyImage[]> = {}
    for (const field of fields) {
      seed[field.key] = field.default || (field.type === 'select' ? fieldOptions(field)[0] || '' : '')
      images[field.key] = []
    }
    const merged = await mergeRunDraft(head.id, seed, images, fields.map((field) => field.key))
    if (current !== runGeneration) return
    runFields.value = fields
    runInputs.value = merged.inputs
    runImages.value = merged.images
    draftRestored.value = merged.restored
    runTarget.value = head
  } catch (cause: unknown) {
    if (current === runGeneration) toast.error(cause instanceof Error ? cause.message : t('nav.workspaceLoadFailed'))
  }
}

async function saveRunDraftClick() {
  const target = runTarget.value
  if (!target) return
  const images: Record<string, ClarifyImage[]> = {}
  for (const [k, v] of Object.entries(runImages.value)) {
    images[k] = v ? [...v] : []
  }
  const result = await saveRunDraft(target.id, { ...runInputs.value }, images)
  if (result === 'ok') toast.success(t('common.toast.draftSaved'))
  else if (result === 'quota_exceeded' || result === 'partial') {
    // Text already on disk / fields persisted — warn per F4 (review v3), not error.
    toast.warn(t('common.toast.draftTooLarge'))
  } else toast.error(t('common.toast.draftSaveFailed'))
}

function closeRunModal() {
  runGeneration++
  runTarget.value = null
  runFields.value = []
  runInputs.value = {}
  runImages.value = {}
  draftRestored.value = false
}

function onRunStayed() {
  void reloadWorkflows()
}

function onRunStarted() {
  if (runTarget.value) void clearRunDraft(runTarget.value.id)
}

function onViewRun(runId: string) {
  router.push('/runs/' + runId)
}

async function openCopy(w: Workflow) {
  const current = workspaceGeneration
  closeMenu()
  copyPreviewLoading.value = w.id
  try {
    const preview = await api.copyPreviewWorkflow(w.id)
    if (current !== workspaceGeneration) return
    copyModal.value = {
      sourceId: preview.sourceId,
      sourceName: preview.sourceName,
      suggestedName: preview.suggestedName,
    }
  } catch {
    if (current !== workspaceGeneration) return
    toast.error(t('common.toast.copyNameFailed'))
  } finally {
    if (current === workspaceGeneration) copyPreviewLoading.value = null
  }
}

function closeCopyModal() {
  copyModal.value = null
}

function onCopied(wf: Workflow) {
  workflows.value = [wf, ...workflows.value.filter((x) => x.id !== wf.id)]
  copyModal.value = null
  toast.success(t('common.toast.copied', { name: wf.name }))
}

function openExport(w: Workflow) {
  closeMenu()
  exportTarget.value = w
}

function openDeleteWf(w: Workflow) {
  closeMenu()
  deleteWfTarget.value = w
  deleteWfError.value = ''
}

async function confirmDeleteWf() {
  const target = deleteWfTarget.value
  if (!target) return
  deletingWf.value = true
  const current = workspaceGeneration
  deleteWfError.value = ''
  try {
    await api.deleteWorkflow(target.id)
    if (current !== workspaceGeneration) return
    workflows.value = workflows.value.filter((w) => w.id !== target.id)
    deleteWfTarget.value = null
  } catch (e: any) {
    if (current !== workspaceGeneration) return
    deleteWfError.value = String(e?.message || e)
  } finally {
    if (current === workspaceGeneration) deletingWf.value = false
  }
}

watch(projectId, () => {
  workspaceGeneration++
  if (projectId.value) void reloadWorkflows()
  else {
    sequence++
    workflows.value = []
    loading.value = false
    loadError.value = ''
    search.value = ''
    closeRunModal()
    closeMenu()
    baselineModalOpen.value = false
    baselineName.value = ''
    baselineRepos.value = [{ name: '', url: '', branch: '' }]
    baselineCreateError.value = ''
    creatingBaseline.value = false
    copyModal.value = null
    copyPreviewLoading.value = null
    exportTarget.value = null
    deleteWfTarget.value = null
    deleteWfError.value = ''
    deletingWf.value = false
    savingNotifyWfId.value = null
    savingHomeWfId.value = null
  }
}, { flush: 'sync' })
watch(workflowCatalogRevision, () => { if (projectId.value) void reloadWorkflows() })
onMounted(() => {
  void reloadWorkflows()
  document.addEventListener('click', onDocClick)
  document.addEventListener('keydown', onKeydown)
  document.addEventListener('scroll', onScrollClose, true)
})
onUnmounted(() => {
  sequence++
  workspaceGeneration++
  closeRunModal()
  document.removeEventListener('click', onDocClick)
  document.removeEventListener('keydown', onKeydown)
  document.removeEventListener('scroll', onScrollClose, true)
})
return {
router, toast, workflows, search, visibleWorkflows, loading, loadError, toggleWorkflowFavorite, runTarget, runFields, runInputs, runImages, draftRestored, openMenuId, menuPosition, newWorkflowMenuOpen, baselineModalOpen, baselineName, baselineRepos, creatingBaseline, baselineCreateError, deleteWfTarget, deletingWf, deleteWfError, copyPreviewLoading, copyModal, exportTarget, savingNotifyWfId, wfNotifyMode, wfNotifyHas, persistWorkflowNotify, setWorkflowNotifyMode, toggleWorkflowNotifyEvent, savingHomeWfId, toggleWorkflowShowOnHome, existingNames, askFields, fieldOptions, closeMenu, toggleMenu, toggleNewWorkflowMenu, menuIdFor, onDocClick, onKeydown, onScrollClose, reloadWorkflows, newWorkflow, openBaselineModal, closeBaselineModal, hasValidBaselineRepo, createFromBaseline, openWorkflow, openEdit, openRun, saveRunDraftClick, closeRunModal, onRunStayed, onRunStarted, onViewRun, openCopy, closeCopyModal, onCopied, openExport, openDeleteWf, confirmDeleteWf, t, projectId, isFavorite, toggleFavorite, isMobile, fileInput, triggerImport, handleFileChange, fmtTime
}
}
