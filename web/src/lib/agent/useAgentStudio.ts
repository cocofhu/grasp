/**
 * Agent Studio view: Agent tree / draft / selection / panel orchestration.
 */
import { ref, computed, watch, nextTick, onMounted, onBeforeUnmount, toValue, type MaybeRefOrGetter } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import type AgentFilesPanel from '@/components/agent/AgentFilesPanel.vue'
import type { DataSubTab } from '@/components/agent/AgentDataPanel.vue'
import { api, type Agent, type WorkspaceAgentsImportResult, type TeamBootstrapSession } from '@/lib/api/api'
import { createListRequestSeq, httpStatusOf } from '@/lib/shared/listRequestSeq'
import { useBreakpoint } from '@/lib/composables/useBreakpoint'
import { useWorkspaceContext } from '@/lib/composables/useWorkspaceContext'
import { downloadZip, validateAgentName, normalizeAgentName } from '@/lib/agent/agentIO'
import { agentConfigRelPath } from '@/lib/agent/backendAuthGuide'
import { useAgentImport } from '@/lib/agent/useAgentImport'
import { isManagedRegionKey } from '@/lib/shared/regionPolicy'
import {
  fromDraft, hydrateStudioDraft, draftPayloadJson,
  type AgentStudioDraft as Draft,
} from '@/lib/agent/agentStudioDraft'
import { capabilityIssueKey, validateCapabilities } from '@/lib/workflow/agentCapabilities'
import { isProtectedOnboardingAgentName } from '@/lib/pm/onboardingWizard'

export type StudioTab = 'files' | 'mcp' | 'env' | 'capabilities' | 'meta' | 'data' | 'test'
const STUDIO_TABS: StudioTab[] = ['files', 'mcp', 'env', 'capabilities', 'meta', 'data', 'test']

function isStudioTab(q: unknown): q is StudioTab {
  return typeof q === 'string' && (STUDIO_TABS as readonly string[]).includes(q)
}

function parseDataSub(q: unknown): DataSubTab {
  if (q === 'context' || q === 'jobs' || q === 'memory') return q
  return 'memory'
}


export function useAgentStudio(opts?: {
  embedded?: boolean | MaybeRefOrGetter<boolean>
  /** While true (e.g. Agent 默认配置 is shown) the Studio leaves the URL alone. */
  pauseQuerySync?: MaybeRefOrGetter<boolean>
}) {
const { t } = useI18n()
const { isMobile } = useBreakpoint()
const route = useRoute()
const router = useRouter()

const { ensureLoaded: loadWorkspace, reload: reloadWorkspace } = useWorkspaceContext()
const embedded = computed(() => toValue(opts?.embedded) === true)
const hideTeamCreate = computed(() => embedded.value)
const showCreateTeam = computed(() => !hideTeamCreate.value)
function filterAgentsByScope(list: Agent[] | null | undefined): Agent[] { return list || [] }

const AGENT_LIST_COLLAPSED_KEY = 'agent-studio-agent-list-collapsed'
const SIDEBAR_EXPANDED_W = '280px'
const SIDEBAR_COLLAPSED_W = '28px'

function readCollapsedState(key: string): boolean {
  try {
    const v = localStorage.getItem(key)
    if (v === null) return false
    return v === 'true'
  } catch {
    return false
  }
}

function writeCollapsedState(key: string, collapsed: boolean) {
  try {
    localStorage.setItem(key, String(collapsed))
  } catch {
    /* ignore quota / private mode */
  }
}

const agentListCollapsed = ref(false)
function toggleAgentListCollapsed() {
  agentListCollapsed.value = !agentListCollapsed.value
  writeCollapsedState(AGENT_LIST_COLLAPSED_KEY, agentListCollapsed.value)
}

const cardGridStyle = computed(() => {
  if (isMobile.value) return { gridTemplateColumns: '1fr' }
  return {
    gridTemplateColumns: `${agentListCollapsed.value ? SIDEBAR_COLLAPSED_W : SIDEBAR_EXPANDED_W} 1fr`,
  }
})

const filesStep = ref<'list' | 'edit'>('list')
const justSaved = ref(false)
const showSaveReasonModal = ref(false)
const saveReason = ref('')
const historyRefreshKey = ref(0)
const filesPanelRef = ref<InstanceType<typeof AgentFilesPanel> | null>(null)

const TAB_FADE_THRESHOLD = 4
const agentNameEl = ref<HTMLElement | null>(null)
const tabStripEl = ref<HTMLElement | null>(null)
const agentNameTruncated = ref(false)
const showFullNameTip = ref(false)
const fullNameTipStyle = ref<Record<string, string>>({})
const tabFadeLeft = ref(false)
const tabFadeRight = ref(false)

let tabStripObserver: ResizeObserver | null = null

function closeFullNameTip() {
  showFullNameTip.value = false
}

function closeMobileChromeOverlays() {
  closeFullNameTip()
  filesPanelRef.value?.closeExplorerMore()
}

/** Narrow-screen Agent tree bottom sheet (≈70vh). */
const showSidebarSheet = ref(false)

type LeaveConfirmCfg = {
  title: string
  message: string
  saveText: string
  discardText: string
  /** Return true only when leave may proceed (e.g. save succeeded). */
  onSave: () => boolean | Promise<boolean>
  onDiscard: () => void
}
const leaveConfirmCfg = ref<LeaveConfirmCfg | null>(null)

const agents = ref<Agent[]>([])
const activeName = ref('')
const draft = ref<Draft | null>(null)
const originalJson = ref('')
const tab = ref<StudioTab>('files')
const dataSubTab = ref<DataSubTab>('memory')
/** Skip one URL write while applying deep-link query on load. */
let applyingStudioQuery = false

function syncStudioQuery() {
  if (applyingStudioQuery || toValue(opts?.pauseQuerySync)) return
  const useStudioTabKey = embedded.value
  const next: Record<string, unknown> = useStudioTabKey ? { ...route.query } : {}
  if (activeName.value) next.agent = activeName.value
  else if (useStudioTabKey) delete next.agent

  // Never overwrite the host page's `tab` query.
  if (tab.value !== 'files') next.studioTab = tab.value
  else delete next.studioTab

  if (tab.value === 'data') next.sub = dataSubTab.value
  else if (useStudioTabKey) delete next.sub

  const curAgent = typeof route.query.agent === 'string' ? route.query.agent : ''
  const curTab = typeof route.query.studioTab === 'string' ? route.query.studioTab : ''
  const curSub = typeof route.query.sub === 'string' ? route.query.sub : ''
  const nextTab = typeof next.studioTab === 'string' ? next.studioTab : ''
  if (
    curAgent === (typeof next.agent === 'string' ? next.agent : '') &&
    curTab === nextTab &&
    curSub === (typeof next.sub === 'string' ? next.sub : '')
  ) {
    return
  }
  void router.replace({ query: next as typeof route.query })
}

const draftBindingDirty = computed(() => false)
const loading = ref(true)
const hasInitialLoaded = ref(false)
const loadFailed = ref(false)
const loadDenied = ref(false)
const studioSeq = createListRequestSeq()
const error = ref('')
const saving = ref(false)
const initialLoading = computed(() => loading.value && !hasInitialLoaded.value)
const showRefreshProgress = computed(
  () => loading.value && hasInitialLoaded.value && agents.value.length > 0,
)

const toastMsg = ref('')
let toastTimer: ReturnType<typeof setTimeout> | null = null

// generic dialogs
type PromptCfg = {
  title: string
  label: string
  placeholder?: string
  /** Soft info line (e.g. rename cascade hint); not a warning. */
  hint?: string
  /** When true, show green "valid" feedback after write-rule checks pass. */
  showValidHint?: boolean
  validate: (v: string) => string
  submit: (v: string) => void | Promise<void>
}
const promptCfg = ref<PromptCfg | null>(null)
const promptValue = ref('')
const promptError = ref('')
const promptOkMsg = ref('')
const promptCanSubmit = computed(() => {
  if (!promptCfg.value) return false
  const v = promptValue.value
  if (!v.trim()) return false
  return !promptCfg.value.validate(v)
})

function refreshPromptFeedback() {
  const c = promptCfg.value
  if (!c) {
    promptError.value = ''
    promptOkMsg.value = ''
    return
  }
  const v = promptValue.value
  if (!v.trim()) {
    promptError.value = ''
    promptOkMsg.value = ''
    return
  }
  const err = c.validate(v)
  if (err) {
    promptError.value = err
    promptOkMsg.value = ''
  } else {
    promptError.value = ''
    promptOkMsg.value = c.showValidHint ? t('pages.agentStudio.dialogs.nameValid') : ''
  }
}
type ConfirmCfg = { title: string; message: string; confirmText?: string; danger?: boolean; ok: () => void | Promise<void> }
const confirmCfg = ref<ConfirmCfg | null>(null)
const showAgentManage = ref(false)
/** Agent management 打开时高亮/滚动到的目标行；关闭时清除 */
const manageFocusAgent = ref('')

const showUnsavedExport = ref(false)
const exporting = ref(false)
const showBundleSecrets = ref(false)
const pendingBundleExport = ref(false)


async function onBundleImported(_result: WorkspaceAgentsImportResult) {
  try {
    const list = await api.listAgents()
    agents.value = filterAgentsByScope(list)
    agents.value.sort((a, b) => a.name.localeCompare(b.name))
    const a = agents.value.find((x) => x.name === activeName.value)
    if (a) {
      const loaded = hydrateStudioDraft(a)
      originalJson.value = draftPayloadJson(loaded)
      draft.value = loaded
    } else {
      selectFirstAgent()
    }
  } catch (e: any) {
    error.value = String(e?.message || e)
  }
}

const agentImport = useAgentImport({
  dirty: () => dirty.value,
  agentNames: () => agents.value.map((a) => a.name),
  onImported: (agent) => {
    const i = agents.value.findIndex((a) => a.name === agent.name)
    if (i >= 0) agents.value[i] = agent
    else {
      agents.value.push(agent)
      agents.value.sort((a, b) => a.name.localeCompare(b.name))
    }
    select(agent.name)
  },
  onBundleImported,
})
const {
  fileInput: importFileInput,
  showDiscardConfirm: showImportDiscardConfirm,
  showConflict: showImportConflict,
  showImportError: showImportErrorModal,
  importError: importErrorMessage,
  conflictName: importConflictName,
  conflictAction: importConflictAction,
  renameValue: importRenameValue,
  renameError: importRenameError,
  showBatchConflict,
  batchConflictNames,
  triggerImport,
  onDiscardCancel: onImportDiscardCancel,
  onDiscardConfirm: onImportDiscardConfirm,
  handleFileChange: onImportFileChange,
  selectConflict: selectImportConflict,
  closeConflict: closeImportConflict,
  confirmConflict: confirmImportConflict,
  closeBatchConflict,
  confirmBatchRename,
  confirmBatchOverwrite,
} = agentImport

const dirty = computed(
  () => !!draft.value && draftPayloadJson(draft.value) !== originalJson.value,
)

watch(dirty, (d) => {
  if (d) justSaved.value = false
})

watch(isMobile, (mobile) => {
  if (!mobile) {
    showSidebarSheet.value = false
    closeMobileChromeOverlays()
  }
  nextTick(() => {
    measureAgentNameTruncation()
    syncTabFade()
  })
})

const agentNames = computed(() => agents.value.map((a) => a.name))
const manageSearch = ref('')
const manageSearchQuery = computed(() => manageSearch.value.trim().toLowerCase())
const filteredManageNames = computed(() => {
  const query = manageSearchQuery.value
  return query ? agentNames.value.filter((name) => name.toLowerCase().includes(query)) : agentNames.value
})
const manageSearchActive = computed(() => !!manageSearchQuery.value)

type ManageNameHighlight = { before: string; hit: string; after: string }
function manageNameHighlight(name: string): ManageNameHighlight {
  const query = manageSearchQuery.value
  const index = query ? name.toLowerCase().indexOf(query) : -1
  if (index < 0) return { before: name, hit: '', after: '' }
  return {
    before: name.slice(0, index),
    hit: name.slice(index, index + query.length),
    after: name.slice(index + query.length),
  }
}

function clearManageSearch() {
  manageSearch.value = ''
}

const treeNodes = computed(() => agents.value.map((agent) => {
  const locked = isProtectedOnboardingAgentName(agent.name)
  return {
    id: agent.name,
    label: agent.name,
    icon: 'robot',
    locked,
    lockTitle: locked ? t('pages.agentStudio.tree.lockedHint') : undefined,
  }
}))
const activeTreeKey = activeName
function onTreeSelect(key: string) { chooseAgent(key) }
function onSheetTreeSelect(key: string) { chooseAgentFromSheet(key) }

function selectFirstAgent(opts?: { skipQuerySync?: boolean }) {
  const first = treeNodes.value[0]
  if (first) select(first.id, opts)
}

function openSidebarSheet() {
  closeMobileChromeOverlays()
  showSidebarSheet.value = true
}

function closeSidebarSheet() {
  showSidebarSheet.value = false
}

const studioTabs = computed(() => {
  if (!draft.value) return []
  const d = draft.value
  return [
    { k: 'files' as const, l: t('pages.agentStudio.tabs.files', { n: d.files.length }) },
    { k: 'mcp' as const, l: t('pages.agentStudio.tabs.mcp', { n: d.mcp.length }) },
    { k: 'env' as const, l: t('pages.agentStudio.tabs.env', { n: d.env.filter((e) => !isManagedRegionKey(e.k)).length }) },
    { k: 'capabilities' as const, l: t('pages.agentStudio.tabs.capabilities') },
    { k: 'data' as const, l: t('pages.agentStudio.tabs.data') },
    { k: 'meta' as const, l: t('pages.agentStudio.tabs.meta') },
    { k: 'test' as const, l: t('pages.agentStudio.tabs.test') },
  ]
})
const studioTabLabel = computed(() => {
  const item = studioTabs.value.find((x) => x.k === tab.value)
  return item?.l || tab.value
})
function showToast(msg: string) {
  toastMsg.value = msg
  if (toastTimer) clearTimeout(toastTimer)
  toastTimer = setTimeout(() => {
    toastMsg.value = ''
  }, 2800)
}

function openSettingsInFiles() {
  if (!draft.value) return
  const backend = draft.value.acpBackend || 'cursor'
  tab.value = 'files'
  syncStudioQuery()
  nextTick(() => {
    filesPanelRef.value?.openPathOrCreate?.(agentConfigRelPath(backend))
  })
}

function discardUnsavedChanges() {
  const snap = filesPanelRef.value?.snapshot() || { path: '', openPaths: [] as string[] }
  const a = agents.value.find((x) => x.name === activeName.value)
  if (!a) return
  const loaded = hydrateStudioDraft(a)
  originalJson.value = draftPayloadJson(loaded)
  draft.value = loaded
  nextTick(() => filesPanelRef.value?.restoreAfterDiscard(snap))
  justSaved.value = false
  error.value = ''
}

function requestStudioTab(next: StudioTab) {
  if (next === tab.value) return
  const exposedStep = filesPanelRef.value?.filesStep as unknown
  const panelStep =
    exposedStep && typeof exposedStep === 'object' && exposedStep !== null && 'value' in exposedStep
      ? (exposedStep as { value: 'list' | 'edit' }).value
      : ((exposedStep as 'list' | 'edit' | undefined) ?? filesStep.value)
  const leavingDirtyEdit =
    isMobile.value && tab.value === 'files' && panelStep === 'edit' && dirty.value
  if (leavingDirtyEdit) {
    leaveConfirmCfg.value = {
      title: t('pages.agentStudio.dialogs.leaveUnsavedTitle'),
      message: t('pages.agentStudio.dialogs.leaveUnsavedTabMessage'),
      saveText: t('pages.agentStudio.dialogs.saveAndContinue'),
      discardText: t('pages.agentStudio.dialogs.discardChanges'),
      onSave: async () => {
        const ok = await save()
        if (!ok) return false
        justSaved.value = true
        tab.value = next
        syncStudioQuery()
        return true
      },
      onDiscard: () => {
        discardUnsavedChanges()
        tab.value = next
        syncStudioQuery()
      },
    }
    return
  }
  tab.value = next
  syncStudioQuery()
}

function onDataSubTab(next: DataSubTab) {
  dataSubTab.value = next
  syncStudioQuery()
}

async function leaveConfirmSave() {
  const c = leaveConfirmCfg.value
  if (!c) return
  try {
    const ok = await c.onSave()
    if (!ok) return
  } catch (e: any) {
    error.value = String(e?.message || e)
    return
  }
  // onSave/onDiscard may chain a follow-up prompt; only close the one that was answered.
  if (leaveConfirmCfg.value === c) leaveConfirmCfg.value = null
}

function leaveConfirmDiscard() {
  const c = leaveConfirmCfg.value
  if (!c) return
  c.onDiscard()
  if (leaveConfirmCfg.value === c) leaveConfirmCfg.value = null
}

function leaveConfirmCancel() {
  leaveConfirmCfg.value = null
}
async function load() {
  const localSeq = studioSeq.beginListRequest()
  const keepStale = agents.value.length > 0
  loading.value = true
  error.value = ''
  loadFailed.value = false
  loadDenied.value = false
  try {
    const [list] = await Promise.all([api.listAgents(), loadWorkspace()])
    if (!studioSeq.isCurrentSeq(localSeq)) return
    agents.value = filterAgentsByScope(list)
    applyingStudioQuery = true
    try {
      const qAgent = typeof route.query.agent === 'string' ? route.query.agent.trim() : ''
      const qTab = isStudioTab(route.query.studioTab) ? route.query.studioTab : null
      const qSub = parseDataSub(route.query.sub)
      if (qAgent && agents.value.some((a) => a.name === qAgent)) {
        select(qAgent, { tab: qTab || 'files', dataSub: qSub, skipQuerySync: true })
      } else if (!activeName.value) {
        selectFirstAgent({ skipQuerySync: true })
      }
    } finally {
      applyingStudioQuery = false
    }
    syncStudioQuery()
  } catch (e: any) {
    if (!studioSeq.isCurrentSeq(localSeq)) return
    if (keepStale) return
    error.value = String(e?.message || e)
    agents.value = []
    const status = httpStatusOf(e)
    loadDenied.value = status === 403
    loadFailed.value = status !== 403
  } finally {
    if (!studioSeq.isCurrentSeq(localSeq)) return
    loading.value = false
    hasInitialLoaded.value = true
  }
}

function select(
  name: string,
  opts?: { tab?: StudioTab; dataSub?: DataSubTab; skipQuerySync?: boolean },
) {
  const a = agents.value.find((x) => x.name === name)
  if (!a) return
  activeName.value = name
  const loaded = hydrateStudioDraft(a)
  originalJson.value = draftPayloadJson(loaded)
  draft.value = loaded
  tab.value = opts?.tab || 'files'
  if (opts?.dataSub) dataSubTab.value = opts.dataSub
  else if (!opts?.tab || opts.tab !== 'data') dataSubTab.value = 'memory'
  filesStep.value = 'list'
  justSaved.value = false
  leaveConfirmCfg.value = null
  nextTick(() => filesPanelRef.value?.resetForSelect())
  if (!opts?.skipQuerySync) syncStudioQuery()
}

// guard agent switches when there are unsaved changes
function chooseAgent(name: string) {
  if (name === activeName.value) return
  if (dirty.value) {
    confirmCfg.value = {
      title: t('pages.agentStudio.dialogs.discardTitle'),
      message: t('pages.agentStudio.dialogs.discardMessage', { name: activeName.value }),
      confirmText: t('pages.agentStudio.dialogs.discardConfirm'),
      danger: true,
      ok: () => select(name),
    }
    return
  }
  select(name)
}

/** Narrow-screen sheet switch: leaveConfirm-style save / discard / cancel. */
function chooseAgentFromSheet(name: string) {
  if (name === activeName.value) {
    closeSidebarSheet()
    return
  }
  if (!dirty.value) {
    select(name)
    closeSidebarSheet()
    return
  }
  leaveConfirmCfg.value = {
    title: t('pages.agentStudio.dialogs.leaveUnsavedTitle'),
    message: t('pages.agentStudio.dialogs.leaveUnsavedSwitchMessage', { name: activeName.value }),
    saveText: t('pages.agentStudio.dialogs.saveAndSwitch'),
    discardText: t('pages.agentStudio.dialogs.discardAndSwitch'),
    onSave: async () => {
      const ok = await save()
      if (!ok) return false
      justSaved.value = true
      select(name)
      closeSidebarSheet()
      return true
    },
    onDiscard: () => {
      select(name)
      closeSidebarSheet()
    },
  }
}

function openManageFromSheet() {
  openAgentManage()
}

async function save(reason?: string) {
  if (!dirty.value) return false
  const capsIssue = draft.value?.capabilities ? validateCapabilities(draft.value.capabilities) : null
  if (capsIssue) {
    error.value = t('pages.agentStudio.capabilities.invalid', {
      reason: t(capabilityIssueKey(capsIssue), { value: capsIssue.value ?? '' }),
    })
    tab.value = 'capabilities'
    syncStudioQuery()
    return false
  }
  saving.value = true
  error.value = ''
  try {
    if (draft.value) {
      const payload = fromDraft(draft.value)
      if (typeof reason === 'string' && reason.trim()) {
        await api.saveAgent(payload, { reason: reason.trim() })
      } else {
        await api.saveAgent(payload)
      }
      const i = agents.value.findIndex((x) => x.name === payload.name)
      if (i >= 0) agents.value[i] = payload
      originalJson.value = JSON.stringify(payload)
      historyRefreshKey.value++
    }
    justSaved.value = true
    showSaveReasonModal.value = false
    return true
  } catch (e: any) {
    error.value = String(e?.message || e)
    return false
  } finally {
    saving.value = false
  }
}

function promptSave() {
  if (!dirty.value || saving.value) return
  saveReason.value = t('pages.agentStudio.workspaceHistory.defaultSaveReason')
  showSaveReasonModal.value = true
}

async function confirmSaveWithReason() {
  return save(saveReason.value)
}

async function reloadAgentFromServer(name: string) {
  try {
    const fresh = await api.getAgent(name)
    const i = agents.value.findIndex((x) => x.name === name)
    if (i >= 0) agents.value[i] = fresh
    if (activeName.value === name) {
      const loaded = hydrateStudioDraft(fresh)
      draft.value = loaded
      originalJson.value = draftPayloadJson(loaded)
      justSaved.value = false
    }
    historyRefreshKey.value++
  } catch (e: any) {
    error.value = String(e?.message || e)
  }
}

async function doExport(name: string) {
  exporting.value = true
  error.value = ''
  try {
    const blob = await api.exportAgent(name)
    downloadZip(blob, `${name}.zip`)
    showToast(t('pages.agentStudio.exportImport.exportSuccess', { name }))
  } catch (e: any) {
    error.value = String(e?.message || e)
  } finally {
    exporting.value = false
  }
}

function triggerExport() {
  if (!activeName.value || exporting.value) return
  pendingBundleExport.value = false
  if (dirty.value) {
    showUnsavedExport.value = true
    return
  }
  void doExport(activeName.value)
}

function cancelUnsavedExport() {
  showUnsavedExport.value = false
  pendingBundleExport.value = false
}

async function discardAndExport() {
  showUnsavedExport.value = false
  if (pendingBundleExport.value) {
    showBundleSecrets.value = true
    return
  }
  if (!activeName.value) return
  await doExport(activeName.value)
}

async function saveThenExport() {
  const ok = await save()
  if (!ok) return
  showUnsavedExport.value = false
  if (pendingBundleExport.value) {
    showBundleSecrets.value = true
    return
  }
  if (activeName.value) await doExport(activeName.value)
}

function onExportWorkspace() {
  if (exporting.value) return
  pendingBundleExport.value = true
  if (dirty.value) {
    showUnsavedExport.value = true
    return
  }
  showBundleSecrets.value = true
}

function cancelBundleSecrets() {
  showBundleSecrets.value = false
  pendingBundleExport.value = false
}

async function confirmBundleSecrets() {
  const pending = pendingBundleExport.value
  showBundleSecrets.value = false
  pendingBundleExport.value = false
  if (!pending || exporting.value) return
  exporting.value = true
  error.value = ''
  try {
    const { blob, filename } = await api.exportWorkspaceAgents()
    downloadZip(blob, filename)
    const base = filename.replace(/\.zip$/i, '')
    showToast(t('pages.agentStudio.exportImport.bundleExportSuccess', { name: base }))
  } catch (e: any) {
    error.value = String(e?.message || e)
  } finally {
    exporting.value = false
  }
}

function onImportWorkspace() {
  triggerImport()
}

const showCreateWizard = ref(false)
const showTeamWizard = ref(false)
const teamBootstrapSessionId = ref('')
function openCreateAgent() {
  showCreateWizard.value = true
}

function openCreateTeam() {
  if (hideTeamCreate.value) return
  showTeamWizard.value = true
}

function onWizardCreated(created: Agent) {
  agents.value.push(created)
  agents.value.sort((a, b) => a.name.localeCompare(b.name))
  select(created.name)
  showToast(t('pages.agentStudio.wizard.createdToast', { name: created.name }))
}

function onTeamBootstrapStarted(session: TeamBootstrapSession) {
  teamBootstrapSessionId.value = session.id
  void onTeamBootstrapRefresh()
}

async function refreshAgentsList() {
  try {
    const list = await api.listAgents()
    agents.value = filterAgentsByScope(list)
    agents.value.sort((a, b) => a.name.localeCompare(b.name))
  } catch {
    /* ignore */
  }
}

async function refreshWorkspace() {
  try {
    await reloadWorkspace()
  } catch {
    /* ignore */
  }
}

async function onTeamBootstrapRefresh() {
  await Promise.all([refreshWorkspace(), refreshAgentsList()])
}

function onTeamBootstrapSelectPm(name: string) {
  void select(name)
}

function onTeamBootstrapOpenPm(name: string) {
  teamBootstrapSessionId.value = ''
  void select(name)
}

function onTeamBootstrapDone() {
  const pm = agents.value.find((a) => a.name.includes('项目经理'))?.name || agents.value[0]?.name
  teamBootstrapSessionId.value = ''
  void onTeamBootstrapRefresh().then(() => {
    if (pm) void select(pm)
  })
}

function openAgentManage(focusAgentName?: string) {
  clearManageSearch()
  manageFocusAgent.value = focusAgentName || ''
  showAgentManage.value = true
  if (focusAgentName) {
    nextTick(() => {
      const el = Array.from(document.querySelectorAll('[data-manage-agent]')).find(
        (n) => n.getAttribute('data-manage-agent') === focusAgentName,
      )
      el?.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
    })
  }
}

function closeAgentManage() {
  showAgentManage.value = false
  clearManageSearch()
  manageFocusAgent.value = ''
}

function openRenameAgent(name: string) {
  if (isProtectedOnboardingAgentName(name)) return
  promptValue.value = name
  promptError.value = ''
  promptOkMsg.value = ''
  promptCfg.value = {
    title: t('pages.agentStudio.dialogs.renameTitle'),
    label: t('pages.agentStudio.dialogs.renameLabel'),
    hint: t('pages.agentStudio.dialogs.renameCascadeHint'),
    showValidHint: true,
    validate: (v) => {
      const code = validateAgentName(v)
      if (code === 'required') return t('pages.agentStudio.dialogs.nameRequired')
      if (code === 'invalid') return t('pages.agentStudio.dialogs.nameInvalid')
      const normalized = normalizeAgentName(v)
      if (normalized !== name && agents.value.some((a) => a.name === normalized)) {
        return t('pages.agentStudio.dialogs.nameExists')
      }
      return ''
    },
    submit: async (v) => {
      const normalized = normalizeAgentName(v)
      if (normalized === name) return
      const updated = await api.renameAgent(name, normalized)
      const n = updated.updatedWorkflowCount ?? 0
      const { updatedWorkflowCount: _count, ...agent } = updated
      agents.value = agents.value.filter((a) => a.name !== name)
      agents.value.push(agent)
      agents.value.sort((a, b) => a.name.localeCompare(b.name))
      // 管理弹窗内改名后同步 focus 到新名（若仍打开）
      if (manageFocusAgent.value === name) manageFocusAgent.value = agent.name
      select(agent.name)
      if (n > 0) showToast(t('pages.agentStudio.dialogs.renameSuccessWithWorkflows', { n }))
      else showToast(t('pages.agentStudio.dialogs.renameSuccess'))
    },
  }
  refreshPromptFeedback()
}

function confirmDeleteAgent(name: string) {
  if (isProtectedOnboardingAgentName(name)) return
  confirmCfg.value = {
    title: t('pages.agentStudio.dialogs.deleteAgentTitle'),
    message: t('pages.agentStudio.dialogs.deleteAgentMessage', { name }),
    confirmText: t('pages.agentStudio.dialogs.delete'),
    danger: true,
    ok: async () => {
      await api.deleteAgent(name)
      agents.value = agents.value.filter((a) => a.name !== name)
      closeAgentManage()
      // FR-f8: clear selection; do not auto-select the next agent
      if (activeName.value === name) {
        activeName.value = ''
        draft.value = null
      }
    },
  }
}

async function promptOk() {
  const c = promptCfg.value
  if (!c) return
  const v = normalizeAgentName(promptValue.value)
  const err = c.validate(promptValue.value)
  if (err) {
    promptError.value = err
    promptOkMsg.value = ''
    return
  }
  try {
    await c.submit(v)
    promptCfg.value = null
  } catch (e: any) {
    promptError.value = String(e?.message || e)
    promptOkMsg.value = ''
  }
}

async function confirmOk() {
  const c = confirmCfg.value
  if (!c) return
  try {
    await c.ok()
  } catch (e: any) {
    error.value = String(e?.message || e)
  }
  // ok() may chain a follow-up confirm; only close the one that was confirmed.
  if (confirmCfg.value === c) confirmCfg.value = null
}

// --- working-dir file operations ------------------------------------------
function measureAgentNameTruncation() {
  const el = agentNameEl.value
  if (!el) {
    agentNameTruncated.value = false
    return
  }
  agentNameTruncated.value = el.scrollWidth > el.clientWidth + 1
}

function placeFullNameTip() {
  const el = agentNameEl.value
  if (!el || !showFullNameTip.value) return
  const rect = el.getBoundingClientRect()
  const margin = 12
  fullNameTipStyle.value = {
    top: `${Math.round(rect.bottom + 8)}px`,
    left: `${margin}px`,
    right: `${margin}px`,
  }
}

function onAgentNameClick() {
  measureAgentNameTruncation()
  if (!agentNameTruncated.value) return
  filesPanelRef.value?.closeExplorerMore()
  showFullNameTip.value = !showFullNameTip.value
  if (showFullNameTip.value) nextTick(placeFullNameTip)
}

function syncTabFade() {
  const el = tabStripEl.value
  if (!el || !isMobile.value) {
    tabFadeLeft.value = false
    tabFadeRight.value = false
    return
  }
  const max = el.scrollWidth - el.clientWidth
  tabFadeLeft.value = el.scrollLeft > TAB_FADE_THRESHOLD
  tabFadeRight.value = el.scrollLeft < max - TAB_FADE_THRESHOLD
}

function bindTabStripObserver() {
  if (typeof ResizeObserver === 'undefined') return
  tabStripObserver?.disconnect()
  tabStripObserver = null
  if (!tabStripEl.value) return
  tabStripObserver = new ResizeObserver(() => syncTabFade())
  tabStripObserver.observe(tabStripEl.value)
}

function onChromeReposition() {
  if (showFullNameTip.value) placeFullNameTip()
  syncTabFade()
}

function onChromeKeydown(e: KeyboardEvent) {
  if (e.key !== 'Escape') return
  if (showFullNameTip.value) closeMobileChromeOverlays()
}

watch(activeName, () => {
  closeFullNameTip()
  nextTick(measureAgentNameTruncation)
})

watch(tabStripEl, () => {
  bindTabStripObserver()
  nextTick(syncTabFade)
})

onMounted(() => {
  agentListCollapsed.value = readCollapsedState(AGENT_LIST_COLLAPSED_KEY)
  load()
  document.addEventListener('keydown', onChromeKeydown)
  window.addEventListener('resize', onChromeReposition)
  window.addEventListener('scroll', onChromeReposition, true)
  nextTick(() => {
    measureAgentNameTruncation()
    bindTabStripObserver()
    syncTabFade()
  })
})

onBeforeUnmount(() => {
  document.removeEventListener('keydown', onChromeKeydown)
  window.removeEventListener('resize', onChromeReposition)
  window.removeEventListener('scroll', onChromeReposition, true)
  tabStripObserver?.disconnect()
  tabStripObserver = null
  if (toastTimer) clearTimeout(toastTimer)
})

  return {
  t,
  isMobile,
  route,
  router,
  AGENT_LIST_COLLAPSED_KEY,
  SIDEBAR_EXPANDED_W,
  SIDEBAR_COLLAPSED_W,
  readCollapsedState,
  writeCollapsedState,
  agentListCollapsed,
  toggleAgentListCollapsed,
  cardGridStyle,
  filesStep,
  justSaved,
  showSaveReasonModal,
  saveReason,
  historyRefreshKey,
  promptSave,
  confirmSaveWithReason,
  reloadAgentFromServer,
  filesPanelRef,
  TAB_FADE_THRESHOLD,
  agentNameEl,
  tabStripEl,
  agentNameTruncated,
  showFullNameTip,
  fullNameTipStyle,
  tabFadeLeft,
  tabFadeRight,
  closeFullNameTip,
  closeMobileChromeOverlays,
  showSidebarSheet,
  openSidebarSheet,
  closeSidebarSheet,
  leaveConfirmCfg,
  agents,
  treeNodes,
  activeTreeKey,
  onTreeSelect,
  onSheetTreeSelect,
  activeName,
  draft,
  originalJson,
  tab,
  dataSubTab,
  syncStudioQuery,
  draftBindingDirty,
  loading,
  hasInitialLoaded,
  loadFailed,
  loadDenied,
  studioSeq,
  error,
  saving,
  initialLoading,
  showRefreshProgress,
  toastMsg,
  promptCfg,
  promptValue,
  promptError,
  promptOkMsg,
  promptCanSubmit,
  refreshPromptFeedback,
  confirmCfg,
  showAgentManage,
  manageFocusAgent,
  showUnsavedExport,
  exporting,
  showBundleSecrets,
  pendingBundleExport,
  agentImport,
  importFileInput,
  showImportDiscardConfirm,
  showImportConflict,
  showImportErrorModal,
  importErrorMessage,
  importConflictName,
  importConflictAction,
  importRenameValue,
  importRenameError,
  showBatchConflict,
  batchConflictNames,
  triggerImport,
  onImportDiscardCancel,
  onImportDiscardConfirm,
  onImportFileChange,
  selectImportConflict,
  closeImportConflict,
  confirmImportConflict,
  closeBatchConflict,
  confirmBatchRename,
  confirmBatchOverwrite,
  dirty,
  agentNames,
  manageSearch,
  manageSearchQuery,
  filteredManageNames,
  manageSearchActive,
  manageNameHighlight,
  clearManageSearch,
  studioTabs,
  studioTabLabel,
  showToast,
  openSettingsInFiles,
  discardUnsavedChanges,
  requestStudioTab,
  onDataSubTab,
  leaveConfirmSave,
  leaveConfirmDiscard,
  leaveConfirmCancel,
  load,
  select,
  chooseAgent,
  chooseAgentFromSheet,
  openManageFromSheet,
  save,
  doExport,
  triggerExport,
  cancelUnsavedExport,
  discardAndExport,
  saveThenExport,
  onExportWorkspace,
  cancelBundleSecrets,
  confirmBundleSecrets,
  onImportWorkspace,
  showCreateWizard,
  showTeamWizard,
  teamBootstrapSessionId,
  openCreateAgent,
  openCreateTeam,
  showCreateTeam,
  hideTeamCreate,
  embedded,
  onWizardCreated,
  onTeamBootstrapStarted,
  refreshAgentsList,
  onTeamBootstrapRefresh,
  onTeamBootstrapSelectPm,
  onTeamBootstrapOpenPm,
  onTeamBootstrapDone,
  openAgentManage,
  closeAgentManage,
  openRenameAgent,
  confirmDeleteAgent,
  promptOk,
  confirmOk,
  measureAgentNameTruncation,
  placeFullNameTip,
  onAgentNameClick,
  syncTabFade,
  bindTabStripObserver,
  onChromeReposition,
  onChromeKeydown,
  }
}
