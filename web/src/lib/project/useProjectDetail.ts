import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { onBeforeRouteLeave, onBeforeRouteUpdate, useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import type { InputField } from '@/components/workflow/RunLaunchModal.vue'
import type { RepoRow } from '@/components/ui/ReposEditor.vue'
import { api } from '@/lib/api/api'
import { createListRequestSeq, httpStatusOf } from '@/lib/shared/listRequestSeq'
import { writeStoredProjectId } from '@/lib/composables/useProjectContext'
import { useToast } from '@/lib/composables/useToast'
import { fmtTime } from '@/lib/shared/format'
import { fmtCompactTokenCount, normalizeUnknownModelDisplayNameInput } from '@/lib/run/tokenUsage'
import { clearRunDraft, mergeRunDraft, saveRunDraft } from '@/lib/run/runDraft'
import { useBreakpoint } from '@/lib/composables/useBreakpoint'
import { useWorkflowImport } from '@/lib/run/useWorkflowImport'
import { useWorkflowFavorites } from '@/lib/run/useWorkflowFavorites'
import type {
  ClarifyImage,
  PmLeaderBinding,
  Project,
  ProjectVariable,
  Workflow,
  WorkflowNotifyPolicy,
} from '@/lib/shared/types'
import { isEmptyProjectForOnboarding } from '@/lib/pm/onboardingWizard'
import { firstInstallCompletedAt, openRetryOnboarding } from '@/lib/pm/firstInstall'

export function useProjectDetail() {
const PROJECT_TABS = [
  'board',
  'workflows',
  'requirementDrafts',
  'notify',
  'pmLeader',
  'externalMcp',
  'cronJobs',
  'agents',
  'sharedAgent',
  'credentials',
  'variables',
  'audit',
  'meta',
] as const
type Tab = (typeof PROJECT_TABS)[number]
type PmView = 'chat' | 'settings'

function isProjectTab(q: unknown): q is Tab {
  return typeof q === 'string' && (PROJECT_TABS as readonly string[]).includes(q)
}

function parseProjectTab(q: unknown): Tab {
  if (isProjectTab(q)) return q
  return 'board'
}

const route = useRoute()
const router = useRouter()
const { t, locale } = useI18n()
const toast = useToast()
const { isFavorite, toggleFavorite } = useWorkflowFavorites()

/** Desktop ops-column favorite btn: stable min-width by longer label (Demo: zh 4.75rem / en 5.75rem). */
const favoriteBtnMinWidth = computed(() =>
  String(locale.value).startsWith('zh') ? '4.75rem' : '5.75rem',
)

function toggleWorkflowFavorite(w: Workflow) {
  toggleFavorite(w.id, { name: w.name })
}
const { isMobile } = useBreakpoint()
const projectId = computed(() => route.params.id as string)

const project = ref<Project | null>(null)
const workflows = ref<Workflow[]>([])
const loading = ref(true)
const hasInitialLoaded = ref(false)
const loadFailed = ref(false)
const loadDenied = ref(false)
const notFound = ref(false)
const wfRefreshing = ref(false)
const projectSeq = createListRequestSeq()
const workflowSeq = createListRequestSeq()
const initialLoading = computed(() => loading.value && !project.value)
const showRefreshProgress = computed(
  () => (loading.value && !!project.value) || wfRefreshing.value,
)
const tab = ref<Tab>(parseProjectTab(route.query.tab))
const draftsPanelRef = ref<{ isDirty: boolean; requestLeave: () => Promise<boolean> } | null>(null)

async function confirmDraftsLeave(): Promise<boolean> {
  const panel = draftsPanelRef.value
  if (!panel?.isDirty) return true
  return panel.requestLeave()
}
/** Inline sub-view inside PM Leader; page-local, not a shareable URL param. */
const pmView = ref<PmView>('chat')

async function setTab(id: Tab) {
  if (tab.value === 'requirementDrafts' && id !== 'requirementDrafts') {
    const ok = await confirmDraftsLeave()
    if (!ok) return
  }
  if (id !== 'pmLeader') {
    pmView.value = 'chat'
  }
  tab.value = id
  const nextQuery: Record<string, unknown> = { ...route.query, tab: id }
  if (id !== 'agents') {
    delete nextQuery.agent
    delete nextQuery.studioTab
    delete nextQuery.sub
  }
  if (route.query.tab === id) return
  void router.replace({ query: nextQuery as typeof route.query })
}

/** After settings, remount chat on mobile in conversation view (not thread list). */
const pmRestoreMobileChat = ref(false)

function openPmSettings() {
  pmView.value = 'settings'
}

function backToPmChat() {
  pmView.value = 'chat'
  pmRestoreMobileChat.value = true
}

/** Reset inline PM view when project context changes. */
function resetPmViewForProjectContext() {
  pmView.value = 'chat'
  pmRestoreMobileChat.value = false
}

function syncTabFromRoute() {
  const next = parseProjectTab(route.query.tab)
  if (tab.value !== next) {
    if (tab.value === 'requirementDrafts' && next !== 'requirementDrafts') {
      void confirmDraftsLeave().then((ok) => {
        if (!ok) {
          if (route.query.tab !== 'requirementDrafts') {
            void router.replace({ query: { ...route.query, tab: 'requirementDrafts' } })
          }
          return
        }
        if (next !== 'pmLeader') {
          pmView.value = 'chat'
        }
        tab.value = next
      })
      return
    }
    if (next !== 'pmLeader') {
      pmView.value = 'chat'
    }
    tab.value = next
  }
}

function ensureTabQuery() {
  if (isProjectTab(route.query.tab)) {
    return
  }
  void router.replace({ query: { ...route.query, tab: tab.value } })
}

const savingMeta = ref(false)
const savingVars = ref(false)
const editName = ref('')
const editDesc = ref('')
const editUnknownModelDisplayName = ref('')
const unknownModelDisplayNameError = ref('')
const varRows = ref<ProjectVariable[]>([])
const showDelete = ref(false)
const deleting = ref(false)
const deleteError = ref('')
const runTarget = ref<Workflow | null>(null)
const runFields = ref<InputField[]>([])
const runInputs = ref<Record<string, string>>({})
const runImages = ref<Record<string, ClarifyImage[]>>({})
const draftRestored = ref(false)
const openMenuId = ref<string | null>(null)
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
const projectAgents = ref<{ name: string; projectId?: string }[]>([])

const isOnboardingEmpty = computed(() =>
  isEmptyProjectForOnboarding(
    workflows.value.length,
    projectAgents.value,
    projectId.value,
    project.value?.name,
  ),
)

const { fileInput, triggerImport, handleFileChange } = useWorkflowImport({
  projectId: () => projectId.value,
  onImported: async (wf) => {
    workflows.value = [wf, ...workflows.value.filter((x) => x.id !== wf.id)]
  },
})

const tabs: { id: Tab; labelKey: string }[] = [
  { id: 'board', labelKey: 'pages.projectDetail.tabBoard' },
  { id: 'workflows', labelKey: 'pages.projectDetail.tabWorkflows' },
  { id: 'requirementDrafts', labelKey: 'pages.projectDetail.tabRequirementDrafts' },
  { id: 'notify', labelKey: 'pages.projectDetail.tabNotify' },
  { id: 'pmLeader', labelKey: 'pages.projectDetail.tabPmLeader' },
  { id: 'externalMcp', labelKey: 'pages.projectDetail.tabExternalMcp' },
  { id: 'cronJobs', labelKey: 'pages.projectDetail.tabCronJobs' },
  { id: 'agents', labelKey: 'pages.projectDetail.tabAgents' },
  { id: 'sharedAgent', labelKey: 'pages.projectDetail.tabSharedAgent' },
  { id: 'credentials', labelKey: 'pages.projectDetail.tabCredentials' },
  { id: 'variables', labelKey: 'pages.projectDetail.tabVariables' },
  { id: 'audit', labelKey: 'pages.projectDetail.tabAudit' },
  { id: 'meta', labelKey: 'pages.projectDetail.tabMeta' },
]

/** Demo/test hook: ?auditDenied=1 forces the read-only denial UI. */
const auditForceDenied = computed(() => route.query.auditDenied === '1')

const pmBinding = ref<PmLeaderBinding | null>(null)

async function loadPmBinding() {
  try {
    pmBinding.value = await api.getPmLeader(projectId.value)
  } catch {
    pmBinding.value = null
  }
}

function onPmBindingChanged(b: PmLeaderBinding) {
  pmBinding.value = b
  // Save succeeded → return to chat / empty state with refreshed binding.
  pmView.value = 'chat'
}

function onNotifyProjectUpdated(p: Project) {
  project.value = p
}

function openNotifyChannelSettings() {
  pmView.value = 'settings'
  setTab('pmLeader')
}

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
  savingNotifyWfId.value = w.id
  try {
    // Notify-only PATCH: never send cached nodes/edges (review v1).
    const saved = await api.patchWorkflowNotifyPolicy(w.id, policy)
    workflows.value = workflows.value.map((x) => (x.id === w.id ? { ...x, ...saved } : x))
    toast.success(t('pages.projectDetail.notify.wfUpdated'))
  } catch (e: any) {
    toast.error(String(e?.message || e))
  } finally {
    savingNotifyWfId.value = null
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
  const prev = !!w.showOnHome
  if (prev === next) return
  workflows.value = workflows.value.map((x) => (x.id === w.id ? { ...x, showOnHome: next } : x))
  savingHomeWfId.value = w.id
  try {
    const saved = await api.patchWorkflowHomeVisibility(w.id, next)
    workflows.value = workflows.value.map((x) => (x.id === w.id ? { ...x, ...saved } : x))
    toast.success(t('pages.projectDetail.homeVisibility.updated'))
  } catch (e: any) {
    workflows.value = workflows.value.map((x) => (x.id === w.id ? { ...x, showOnHome: prev } : x))
    toast.error(String(e?.message || e) || t('pages.projectDetail.homeVisibility.updateFailed'))
  } finally {
    savingHomeWfId.value = null
  }
}

const VAR_TYPES = computed(() => [
  { value: 'string', label: t('common.varTypes.string') },
  { value: 'paragraph', label: t('common.varTypes.paragraph') },
  { value: 'number', label: t('common.varTypes.number') },
  { value: 'bool', label: t('common.varTypes.bool') },
  { value: 'select', label: t('common.varTypes.select') },
])

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
  newWorkflowMenuOpen.value = false
}

function toggleMenu(id: string) {
  newWorkflowMenuOpen.value = false
  openMenuId.value = openMenuId.value === id ? null : id
}

function toggleNewWorkflowMenu() {
  openMenuId.value = null
  newWorkflowMenuOpen.value = !newWorkflowMenuOpen.value
}

function menuIdFor(id: string) {
  return 'pd-wf-more-menu-' + id
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

async function load() {
  resetPmViewForProjectContext()
  const localSeq = projectSeq.beginListRequest()
  const keepStale = !!project.value
  loading.value = true
  loadFailed.value = false
  loadDenied.value = false
  notFound.value = false
  try {
    const [p, wfs, agents] = await Promise.all([
      api.getProject(projectId.value),
      api.listWorkflows({ projectId: projectId.value }),
      api.listAgents().catch(() => [] as { name: string; projectId?: string }[]),
    ])
    if (!projectSeq.isCurrentSeq(localSeq)) return
    project.value = p
    void loadPmBinding()
    writeStoredProjectId(p.id)
    editName.value = p.name
    editDesc.value = p.description || ''
    editUnknownModelDisplayName.value = p.unknownModelDisplayName || ''
    unknownModelDisplayNameError.value = ''
    // Spread-copy preserves server-side ask/required/editable (and options/desc).
    varRows.value = (p.variables || []).map((v) => ({ ...v }))
    workflows.value = wfs
    projectAgents.value = agents.map((a) => ({ name: a.name, projectId: a.projectId }))
  } catch (e: unknown) {
    if (!projectSeq.isCurrentSeq(localSeq)) return
    if (keepStale && project.value) return
    project.value = null
    workflows.value = []
    const status = httpStatusOf(e)
    if (status === 403) loadDenied.value = true
    else if (status === 404) notFound.value = true
    else loadFailed.value = true
  } finally {
    if (!projectSeq.isCurrentSeq(localSeq)) return
    loading.value = false
    hasInitialLoaded.value = true
  }
}

function openOnboarding() {
  openRetryOnboarding(projectId.value)
}

async function refreshAfterOnboarding() {
  await reloadWorkflows()
  try {
    const agents = await api.listAgents()
    projectAgents.value = agents.map((a) => ({ name: a.name, projectId: a.projectId }))
  } catch {
    /* ignore */
  }
}

// The wizard lives in App.vue, so a completed bootstrap reaches this view as a signal.
watch(firstInstallCompletedAt, (at) => {
  if (at) void refreshAfterOnboarding()
})

async function reloadWorkflows() {
  const localSeq = workflowSeq.beginListRequest()
  if (workflows.value.length > 0) wfRefreshing.value = true
  try {
    const wfs = await api.listWorkflows({ projectId: projectId.value })
    if (!workflowSeq.isCurrentSeq(localSeq)) return
    workflows.value = wfs
  } catch {
    if (!workflowSeq.isCurrentSeq(localSeq)) return
    // keep current list on refresh failure
  } finally {
    if (!workflowSeq.isCurrentSeq(localSeq)) return
    wfRefreshing.value = false
  }
}

async function saveMeta() {
  if (!project.value) return
  unknownModelDisplayNameError.value = ''
  const normalized = normalizeUnknownModelDisplayNameInput(editUnknownModelDisplayName.value)
  if (normalized.error) {
    unknownModelDisplayNameError.value = t('pages.projectDetail.unknownModelDisplayNameTooLong')
    return
  }
  savingMeta.value = true
  try {
    project.value = await api.updateProject(project.value.id, {
      name: editName.value.trim(),
      description: editDesc.value,
      unknownModelDisplayName: normalized.value,
    })
    editUnknownModelDisplayName.value = project.value.unknownModelDisplayName || ''
    toast.success(t('pages.projectDetail.saved'))
  } catch (e: any) {
    toast.error(String(e?.message || e))
  } finally {
    savingMeta.value = false
  }
}

function clearUnknownModelDisplayName() {
  editUnknownModelDisplayName.value = ''
  unknownModelDisplayNameError.value = ''
  void saveMeta()
}

async function saveVars() {
  if (!project.value) return
  savingVars.value = true
  try {
    // Submit full row objects (incl. hidden ask/required/editable) so mergeProjectVars
    // whole-table replace does not wipe server-side values.
    project.value = await api.updateProject(project.value.id, {
      variables: varRows.value.filter((v) => v.name.trim()),
    })
    varRows.value = (project.value.variables || []).map((v) => ({ ...v }))
    toast.success(t('pages.projectDetail.saved'))
  } catch (e: any) {
    toast.error(String(e?.message || e))
  } finally {
    savingVars.value = false
  }
}

const SECRET_MASK = '****'

function addVarRow() {
  varRows.value.push({
    name: '',
    type: 'string',
    value: '',
    secret: false,
    desc: '',
    options: '',
    ask: false,
    required: false,
    editable: false,
  })
}
function removeVarRow(i: number) {
  varRows.value.splice(i, 1)
}

/** Clear mask when un-secreting or renaming so **** is never treated as plaintext. */
function onVarSecretChange(row: ProjectVariable, secret: boolean) {
  row.secret = secret
  if (!secret && row.value === SECRET_MASK) {
    row.value = ''
  }
}
function onVarNameChange(row: ProjectVariable, name: string) {
  if (row.name !== name && row.value === SECRET_MASK) {
    row.value = ''
  }
  row.name = name
}

/** In-place type switch — never rebuild the row object (preserves ask/required/editable). */
function onVarTypeChange(row: ProjectVariable, type: string) {
  row.type = type
  if (type === 'bool') {
    row.value = row.value === true || row.value === 'true'
  } else if (type === 'number') {
    const n = Number(row.value)
    row.value = Number.isFinite(n) ? n : 0
  } else if (type === 'select') {
    if (row.options == null) row.options = ''
    row.value = row.value == null ? '' : String(row.value)
  } else {
    row.value = row.value == null ? '' : String(row.value)
  }
}

function selectOptions(row: ProjectVariable): string[] {
  return String(row.options || '')
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
}

function isBoolTrue(row: ProjectVariable): boolean {
  return row.value === true || row.value === 'true'
}

function setBoolValue(row: ProjectVariable, v: boolean) {
  row.value = v
}

function onVarValueInput(row: ProjectVariable, raw: string, asNumber = false) {
  row.value = asNumber ? Number(raw) : raw
}

function newWorkflow() {
  newWorkflowMenuOpen.value = false
  router.push({ path: '/workflows/new/edit', query: { projectId: projectId.value } })
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
  baselineCreateError.value = ''
  try {
    const created = await api.createWorkflowFromBaseline(
      projectId.value,
      baselineName.value.trim(),
      baselineRepos.value,
    )
    workflows.value = [created, ...workflows.value.filter((workflow) => workflow.id !== created.id)]
    baselineModalOpen.value = false
    toast.success(t('pages.projectDetail.newWorkflow.created', { name: created.name }))
  } catch (e: any) {
    baselineCreateError.value = String(e?.message || e)
  } finally {
    creatingBaseline.value = false
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
  runTarget.value = w
  draftRestored.value = false
  runFields.value = askFields(w)
  const seed: Record<string, string> = {}
  const imgSeed: Record<string, ClarifyImage[]> = {}
  for (const f of runFields.value) {
    seed[f.key] = f.default || (f.type === 'select' ? fieldOptions(f)[0] || '' : '')
    imgSeed[f.key] = []
  }
  const keys = runFields.value.map((f) => f.key)
  const merged = await mergeRunDraft(w.id, seed, imgSeed, keys)
  runInputs.value = merged.inputs
  runImages.value = merged.images
  draftRestored.value = merged.restored
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
  runTarget.value = null
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
  closeMenu()
  copyPreviewLoading.value = w.id
  try {
    const preview = await api.copyPreviewWorkflow(w.id)
    copyModal.value = {
      sourceId: preview.sourceId,
      sourceName: preview.sourceName,
      suggestedName: preview.suggestedName,
    }
  } catch {
    toast.error(t('common.toast.copyNameFailed'))
  } finally {
    copyPreviewLoading.value = null
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
  deleteWfError.value = ''
  try {
    await api.deleteWorkflow(target.id)
    workflows.value = workflows.value.filter((w) => w.id !== target.id)
    deleteWfTarget.value = null
  } catch (e: any) {
    deleteWfError.value = String(e?.message || e)
  } finally {
    deletingWf.value = false
  }
}

async function confirmDelete() {
  if (!project.value) return
  deleting.value = true
  deleteError.value = ''
  try {
    await api.deleteProject(project.value.id)
    writeStoredProjectId('')
    toast.success(t('pages.projectDetail.deleted'))
    router.push('/projects')
  } catch (e: any) {
    deleteError.value = String(e?.message || e)
  } finally {
    deleting.value = false
  }
}

watch(projectId, () => {
  resetPmViewForProjectContext()
  project.value = null
  workflows.value = []
  hasInitialLoaded.value = false
  loadFailed.value = false
  loadDenied.value = false
  notFound.value = false
  void load()
})
watch(
  () => route.query.tab,
  () => {
    syncTabFromRoute()
  },
)
onMounted(() => {
  ensureTabQuery()
  void load()
  document.addEventListener('click', onDocClick)
  document.addEventListener('keydown', onKeydown)
  document.addEventListener('scroll', onScrollClose, true)
})
onUnmounted(() => {
  document.removeEventListener('click', onDocClick)
  document.removeEventListener('keydown', onKeydown)
  document.removeEventListener('scroll', onScrollClose, true)
})

onBeforeRouteLeave(async () => {
  if (tab.value !== 'requirementDrafts') return true
  return confirmDraftsLeave()
})

onBeforeRouteUpdate(async (to, from) => {
  if (String(to.params.id) === String(from.params.id)) return true
  if (tab.value !== 'requirementDrafts') return true
  return confirmDraftsLeave()
})

  return {
  PROJECT_TABS,
  isProjectTab,
  parseProjectTab,
  route,
  router,
  t,
  locale,
  toast,
  isFavorite,
  toggleFavorite,
  favoriteBtnMinWidth,
  toggleWorkflowFavorite,
  isMobile,
  projectId,
  project,
  workflows,
  loading,
  hasInitialLoaded,
  loadFailed,
  loadDenied,
  notFound,
  wfRefreshing,
  projectSeq,
  workflowSeq,
  initialLoading,
  showRefreshProgress,
  tab,
  draftsPanelRef,
  confirmDraftsLeave,
  pmView,
  setTab,
  pmRestoreMobileChat,
  openPmSettings,
  backToPmChat,
  resetPmViewForProjectContext,
  syncTabFromRoute,
  ensureTabQuery,
  savingMeta,
  savingVars,
  editName,
  editDesc,
  editUnknownModelDisplayName,
  unknownModelDisplayNameError,
  varRows,
  showDelete,
  deleting,
  deleteError,
  runTarget,
  runFields,
  runInputs,
  runImages,
  draftRestored,
  openMenuId,
  newWorkflowMenuOpen,
  baselineModalOpen,
  baselineName,
  baselineRepos,
  creatingBaseline,
  baselineCreateError,
  hasValidBaselineRepo,
  deleteWfTarget,
  deletingWf,
  deleteWfError,
  copyPreviewLoading,
  copyModal,
  exportTarget,
  projectAgents,
  isOnboardingEmpty,
  fileInput,
  triggerImport,
  handleFileChange,
  tabs,
  auditForceDenied,
  pmBinding,
  loadPmBinding,
  onPmBindingChanged,
  onNotifyProjectUpdated,
  openNotifyChannelSettings,
  savingNotifyWfId,
  wfNotifyMode,
  wfNotifyHas,
  persistWorkflowNotify,
  setWorkflowNotifyMode,
  toggleWorkflowNotifyEvent,
  savingHomeWfId,
  toggleWorkflowShowOnHome,
  VAR_TYPES,
  existingNames,
  askFields,
  fieldOptions,
  closeMenu,
  toggleMenu,
  toggleNewWorkflowMenu,
  menuIdFor,
  onDocClick,
  onKeydown,
  onScrollClose,
  load,
  openOnboarding,
  reloadWorkflows,
  saveMeta,
  clearUnknownModelDisplayName,
  saveVars,
  SECRET_MASK,
  addVarRow,
  removeVarRow,
  onVarSecretChange,
  onVarNameChange,
  onVarTypeChange,
  selectOptions,
  isBoolTrue,
  setBoolValue,
  onVarValueInput,
  newWorkflow,
  openBaselineModal,
  closeBaselineModal,
  createFromBaseline,
  openWorkflow,
  openEdit,
  openRun,
  saveRunDraftClick,
  closeRunModal,
  onRunStayed,
  onRunStarted,
  onViewRun,
  openCopy,
  closeCopyModal,
  onCopied,
  openExport,
  openDeleteWf,
  confirmDeleteWf,
  confirmDelete,
  fmtTime,
  fmtCompactTokenCount
  }
}
