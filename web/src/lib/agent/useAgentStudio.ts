/**
 * Agent Studio view: org/draft/selection/panel orchestration.
 */
import { ref, computed, watch, nextTick, onMounted, onBeforeUnmount, toValue, type MaybeRefOrGetter } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import type AgentFilesPanel from '@/components/agent/AgentFilesPanel.vue'
import type { DataSubTab } from '@/components/agent/AgentDataPanel.vue'
import { api, type Agent, type AgentOrg, type TeamBootstrapSession } from '@/lib/api/api'
import { createListRequestSeq, httpStatusOf } from '@/lib/shared/listRequestSeq'
import { useBreakpoint } from '@/lib/composables/useBreakpoint'
import {
  emptyOrg, groupPath, newGroupId, applyDeleteGroup, applyMoveAgent,
  applyRemoveAgentFromGroup, wouldCreateGroupCycle, buildOrgTreeRows, pruneOrgToAgentGroups,
  recursiveMemberNames, classifyAssignTargets, assignNeedsDraftConfirm,
  shouldSyncDraftAfterAssign, isAgentInGroupSubtree, UNGROUPED_ID,
  allGroupCollapseIds, ancestorGroupIdsForAgent, buildDefaultCollapsedSet,
  mergeCollapsedWithOrgChange,
} from '@/lib/agent/agentOrg'
import { downloadZip, validateAgentName, normalizeAgentName } from '@/lib/agent/agentIO'
import { agentConfigRelPath } from '@/lib/agent/backendAuthGuide'
import { useAgentImport } from '@/lib/agent/useAgentImport'
import { isManagedRegionKey } from '@/lib/shared/regionPolicy'
import {
  fromDraft, hydrateStudioDraft, draftPayloadJson,
  type AgentStudioDraft as Draft,
} from '@/lib/agent/agentStudioDraft'
import { capabilityIssueKey, validateCapabilities } from '@/lib/workflow/agentCapabilities'

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
  projectId?: MaybeRefOrGetter<string | undefined>
  embedded?: boolean | MaybeRefOrGetter<boolean>
}) {
const { t } = useI18n()
const { isMobile } = useBreakpoint()
const route = useRoute()
const router = useRouter()

const scopedProjectId = computed(() => {
  const raw = toValue(opts?.projectId)
  return typeof raw === 'string' ? raw.trim() : ''
})
const embedded = computed(() => {
  const e = toValue(opts?.embedded)
  return e === true || !!scopedProjectId.value
})
const hideTeamCreate = computed(() => embedded.value)
const showCreateTeam = computed(() => !hideTeamCreate.value)

function filterAgentsByScope(list: Agent[] | null | undefined): Agent[] {
  const all = list || []
  const pid = scopedProjectId.value
  if (!pid) return all
  return all.filter((a) => a.projectId === pid)
}

const AGENT_LIST_COLLAPSED_KEY = 'agent-studio-agent-list-collapsed'
const ORG_SIDEBAR_EXPANDED_W = '280px'
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
    gridTemplateColumns: `${agentListCollapsed.value ? SIDEBAR_COLLAPSED_W : ORG_SIDEBAR_EXPANDED_W} 1fr`,
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

/** Narrow-screen agent org tree bottom sheet (≈70vh). */
const showOrgSheet = ref(false)
const orgSheetCollapsed = ref<Set<string>>(new Set())
const orgSheetKnownIds = ref<Set<string>>(new Set())

function expandAgentsForOrgSheet(names: string[]): string[] {
  const out = names.filter(Boolean)
  if (manageFocusAgent.value && !out.includes(manageFocusAgent.value)) {
    out.push(manageFocusAgent.value)
  }
  return out
}

function syncOrgSheetCollapsedFromOrg(expandNames?: string[]) {
  const names = expandAgentsForOrgSheet(expandNames || [activeName.value])
  if (orgSheetKnownIds.value.size === 0) {
    orgSheetCollapsed.value = buildDefaultCollapsedSet(org.value, names)
  } else {
    orgSheetCollapsed.value = mergeCollapsedWithOrgChange(
      org.value,
      orgSheetCollapsed.value,
      orgSheetKnownIds.value,
      names,
    )
  }
  orgSheetKnownIds.value = new Set([...allGroupCollapseIds(org.value)])
}

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
const projects = ref<{ id: string; name: string }[]>([])
const org = ref<AgentOrg>(emptyOrg())
const orgBaseline = ref('')
const activeName = ref('')
const draft = ref<Draft | null>(null)
const originalJson = ref('')
const tab = ref<StudioTab>('files')
const dataSubTab = ref<DataSubTab>('memory')
const orgSaving = ref(false)
/** Skip one URL write while applying deep-link query on load. */
let applyingStudioQuery = false

function syncStudioQuery() {
  if (applyingStudioQuery) return
  const useStudioTabKey = embedded.value
  const next: Record<string, unknown> = useStudioTabKey ? { ...route.query } : {}
  if (activeName.value) next.agent = activeName.value
  else if (useStudioTabKey) delete next.agent

  if (useStudioTabKey) {
    if (tab.value !== 'files') next.studioTab = tab.value
    else delete next.studioTab
    // Never overwrite project detail `tab=agents`.
  } else if (tab.value !== 'files') {
    next.tab = tab.value
  }

  if (tab.value === 'data') next.sub = dataSubTab.value
  else if (useStudioTabKey) delete next.sub

  const curAgent = typeof route.query.agent === 'string' ? route.query.agent : ''
  const curTabKey = useStudioTabKey ? 'studioTab' : 'tab'
  const curTab = typeof route.query[curTabKey] === 'string' ? (route.query[curTabKey] as string) : ''
  const curSub = typeof route.query.sub === 'string' ? route.query.sub : ''
  const nextTab = useStudioTabKey
    ? typeof next.studioTab === 'string'
      ? next.studioTab
      : ''
    : typeof next.tab === 'string'
      ? next.tab
      : ''
  if (
    curAgent === (typeof next.agent === 'string' ? next.agent : '') &&
    curTab === nextTab &&
    curSub === (typeof next.sub === 'string' ? next.sub : '')
  ) {
    return
  }
  void router.replace({ query: next as typeof route.query })
}

// Data / chat tester follow the last-saved binding so unsaved draft edits
// cannot hit APIs under the wrong (or unbound) project.
const savedProjectId = computed(() => {
  const a = agents.value.find((x) => x.name === activeName.value)
  return a?.projectId?.trim() || ''
})
const isProjectBound = computed(() => !!savedProjectId.value)
const draftBindingDirty = computed(() => {
  const draftPid = draft.value?.projectId?.trim() || ''
  return draftPid !== savedProjectId.value
})
function projectNameById(id: string): string {
  return projects.value.find((p) => p.id === id)?.name || id
}
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
/** 侧栏铅笔阻断弹窗 */
const showRenameBlocked = ref(false)
const renameBlockedTarget = ref('')

const showUnsavedExport = ref(false)
const exporting = ref(false)
const showFolderSecrets = ref(false)
const pendingFolderExportGroupId = ref('')

type SensitiveKeyHit = { key: string; agentCount: number }
const showClearSensitive = ref(false)
const clearSensitiveBusy = ref(false)
const clearSensitiveGroupId = ref('')
const clearSensitiveGroupName = ref('')
const clearSensitiveAgentCount = ref(0)
const clearSensitiveHits = ref<SensitiveKeyHit[]>([])
const clearSensitiveSelected = ref<Set<string>>(new Set())
const clearSensitiveSelectedCount = computed(() => clearSensitiveSelected.value.size)

const agentImport = useAgentImport({
  dirty: () => agentDirty.value,
  agentDirty: () => agentDirty.value,
  orgDirty: () => orgDirty.value,
  persistOrg: () => persistOrg(org.value),
  agentNames: () => agents.value.map((a) => a.name),
  onImported: async (agent) => {
    const i = agents.value.findIndex((a) => a.name === agent.name)
    if (i >= 0) agents.value[i] = agent
    else {
      agents.value.push(agent)
      agents.value.sort((a, b) => a.name.localeCompare(b.name))
    }
    // ZIP import carries no org → agent stays ungrouped.
    await reloadOrg()
    select(agent.name)
  },
  onFolderImported: async (importedOrg) => {
    org.value = importedOrg
    orgBaseline.value = orgSnapshot(importedOrg)
    try {
      const list = await api.listAgents()
      agents.value = list || []
      if (activeName.value && agents.value.some((a) => a.name === activeName.value)) {
        const a = agents.value.find((x) => x.name === activeName.value)!
        const loaded = hydrateStudioDraft(a)
        originalJson.value = draftPayloadJson(loaded)
        draft.value = loaded
      } else if (agents.value.length) {
        select(agents.value[0].name)
      }
    } catch (e: any) {
      error.value = String(e?.message || e)
    }
  },
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
  triggerGroupImport,
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

function orgSnapshot(o: AgentOrg): string {
  return JSON.stringify({
    revision: o.revision,
    groups: o.groups || [],
    agents: o.agents || {},
  })
}

const agentDirty = computed(
  () => !!draft.value && draftPayloadJson(draft.value) !== originalJson.value,
)
const orgDirty = computed(() => orgSnapshot(org.value) !== orgBaseline.value)
const dirty = computed(() => agentDirty.value || orgDirty.value)

watch(dirty, (d) => {
  if (d) justSaved.value = false
})

watch(isMobile, (mobile) => {
  if (!mobile) {
    showOrgSheet.value = false
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

const displayOrg = computed(() =>
  embedded.value ? pruneOrgToAgentGroups(org.value, agentNames.value) : org.value,
)

const orgSheetRows = computed(() =>
  buildOrgTreeRows(displayOrg.value, agentNames.value, orgSheetCollapsed.value, agents.value, projects.value),
)

type AssignFailItem = { name: string; reason: string }
const showAssignPick = ref(false)
const showAssignCover = ref(false)
const showAssignDraft = ref(false)
const assignApplying = ref(false)
const assignGroupName = ref('')
const assignMembers = ref<string[]>([])
const assignTargetId = ref('')
const assignDiffBound = ref<{ name: string; oldProjectId: string }[]>([])
const assignFail = ref<AssignFailItem[]>([])
const assignOkCount = ref(0)

const assignTargetLabel = computed(() =>
  assignTargetId.value ? projectNameById(assignTargetId.value) : '',
)
const assignMemberList = computed(() => assignMembers.value.join('、'))
const assignAffectedList = computed(() =>
  assignDiffBound.value
    .map((item) => `${item.name}（${projectNameById(item.oldProjectId)}）`)
    .join('、'),
)

function closeAssignModals() {
  showAssignPick.value = false
  showAssignCover.value = false
  showAssignDraft.value = false
  assignApplying.value = false
}

function onAssignProject(groupId: string) {
  if (embedded.value) return
  const group = (org.value.groups || []).find((g) => g.id === groupId)
  if (!group) return
  assignFail.value = []
  assignOkCount.value = 0
  const members = recursiveMemberNames(org.value, groupId, agentNames.value)
  if (!members.length) {
    showToast(t('pages.agentStudio.org.assignEmpty'))
    return
  }
  if (!projects.value.length) {
    showToast(t('pages.agentStudio.org.assignNoProjects'))
    return
  }
  assignGroupName.value = group.name
  assignMembers.value = members
  assignTargetId.value = projects.value[0]?.id || ''
  assignDiffBound.value = []
  showAssignCover.value = false
  showAssignDraft.value = false
  showAssignPick.value = true
}

function onAssignPickNext() {
  const target = assignTargetId.value.trim()
  if (!target) {
    showToast(t('pages.agentStudio.org.assignNoProjects'))
    return
  }
  const classified = classifyAssignTargets(assignMembers.value, agents.value, target)
  if (classified.already.length === assignMembers.value.length) {
    closeAssignModals()
    showToast(t('pages.agentStudio.org.assignAlready'))
    return
  }
  assignDiffBound.value = classified.diffBound
  if (classified.diffBound.length) {
    showAssignPick.value = false
    showAssignCover.value = true
    return
  }
  maybeAssignDraftThenApply()
}

function cancelAssignCover() {
  closeAssignModals()
  showToast(t('pages.agentStudio.org.assignCancelled'))
}

function maybeAssignDraftThenApply() {
  const needsDraft = assignNeedsDraftConfirm({
    activeName: activeName.value,
    memberNames: assignMembers.value,
    draftBindingDirty: draftBindingDirty.value,
  })
  if (needsDraft) {
    showAssignPick.value = false
    showAssignCover.value = false
    showAssignDraft.value = true
    return
  }
  void applyAssign(assignMembers.value.includes(activeName.value))
}

function keepAssignDraft() {
  closeAssignModals()
  showToast(t('pages.agentStudio.org.assignDraftKept'))
}

function syncDraftProjectId(target: string) {
  if (!draft.value) return
  draft.value.projectId = target
  try {
    const snap = JSON.parse(originalJson.value || '{}') as Agent
    snap.projectId = target
    originalJson.value = JSON.stringify(snap)
  } catch {
    /* ignore malformed snapshot */
  }
}

async function applyAssign(syncDraftRequested: boolean) {
  const target = assignTargetId.value.trim()
  if (!target || assignApplying.value) return
  assignApplying.value = true
  assignFail.value = []
  assignOkCount.value = 0
  const ok: string[] = []
  const fail: AssignFailItem[] = []
  for (const name of assignMembers.value) {
    try {
      await api.patchAgentProject(name, target)
      ok.push(name)
    } catch (e: any) {
      fail.push({ name, reason: String(e?.message || e) })
    }
  }
  assignOkCount.value = ok.length
  assignFail.value = fail
  try {
    const list = await api.listAgents()
    agents.value = list || []
  } catch {
    /* keep local list; brackets refresh on next load */
  }
  if (
    shouldSyncDraftAfterAssign({
      activeName: activeName.value,
      memberNames: assignMembers.value,
      failNames: fail.map((f) => f.name),
      syncDraftRequested,
    })
  ) {
    syncDraftProjectId(target)
  }
  closeAssignModals()
  if (fail.length) {
    showToast(t('pages.agentStudio.org.assignPartialToast', { ok: ok.length, fail: fail.length }))
  } else {
    showToast(
      t('pages.agentStudio.org.assignOkToast', { n: ok.length, project: projectNameById(target) }),
    )
  }
}

function openOrgSheet() {
  closeMobileChromeOverlays()
  showOrgSheet.value = true
}

function closeOrgSheet() {
  showOrgSheet.value = false
}

function toggleOrgSheetNode(id: string) {
  const next = new Set(orgSheetCollapsed.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  orgSheetCollapsed.value = next
}

function orgSheetPadStyle(depth: number) {
  return { paddingLeft: `${6 + depth * 14}px` }
}

async function persistOrg(next: AgentOrg): Promise<boolean> {
  orgSaving.value = true
  error.value = ''
  try {
    const saved = await api.saveAgentsOrg({
      revision: org.value.revision,
      groups: next.groups || [],
      agents: next.agents || {},
    })
    org.value = saved
    orgBaseline.value = orgSnapshot(saved)
    return true
  } catch (e: any) {
    error.value = String(e?.message || e)
    // Reload authoritative org on conflict / validation failure.
    try {
      const fresh = await api.getAgentsOrg()
      org.value = fresh
      orgBaseline.value = orgSnapshot(fresh)
    } catch {
      /* ignore */
    }
    return false
  } finally {
    orgSaving.value = false
  }
}

async function reloadOrg() {
  try {
    const o = (await api.getAgentsOrg()) || emptyOrg()
    if (!o.agents) o.agents = {}
    if (!o.groups) o.groups = []
    org.value = o
    orgBaseline.value = orgSnapshot(o)
    syncOrgSheetCollapsedFromOrg()
  } catch (e: any) {
    error.value = String(e?.message || e)
  }
}

function openCreateRootGroup() {
  promptValue.value = ''
  promptError.value = ''
  promptOkMsg.value = ''
  promptCfg.value = {
    title: t('pages.agentStudio.org.newRootGroup'),
    label: t('pages.agentStudio.org.groupNameLabel'),
    placeholder: t('pages.agentStudio.org.groupNamePlaceholder'),
    validate: (v) => (!v ? t('pages.agentStudio.dialogs.nameRequired') : ''),
    submit: async (v) => {
      const next = {
        ...org.value,
        groups: [...(org.value.groups || []), { id: newGroupId(), name: v }],
      }
      if (!(await persistOrg(next))) throw new Error(error.value || 'org save failed')
    },
  }
}

function openCreateChildGroup(parentId: string) {
  promptValue.value = ''
  promptError.value = ''
  promptOkMsg.value = ''
  promptCfg.value = {
    title: t('pages.agentStudio.org.newChildGroup'),
    label: t('pages.agentStudio.org.groupNameLabel'),
    placeholder: t('pages.agentStudio.org.groupNamePlaceholder'),
    validate: (v) => (!v ? t('pages.agentStudio.dialogs.nameRequired') : ''),
    submit: async (v) => {
      const next = {
        ...org.value,
        groups: [...(org.value.groups || []), { id: newGroupId(), name: v, parentGroupId: parentId }],
      }
      if (!(await persistOrg(next))) throw new Error(error.value || 'org save failed')
    },
  }
}

function openRenameGroup(groupId: string) {
  const g = (org.value.groups || []).find((x) => x.id === groupId)
  if (!g) return
  promptValue.value = g.name
  promptError.value = ''
  promptOkMsg.value = ''
  promptCfg.value = {
    title: t('pages.agentStudio.org.renameGroup'),
    label: t('pages.agentStudio.org.groupNameLabel'),
    validate: (v) => (!v ? t('pages.agentStudio.dialogs.nameRequired') : ''),
    submit: async (v) => {
      const next = {
        ...org.value,
        groups: (org.value.groups || []).map((x) => (x.id === groupId ? { ...x, name: v } : x)),
      }
      if (!(await persistOrg(next))) throw new Error(error.value || 'org save failed')
    },
  }
}

function confirmDeleteGroup(groupId: string) {
  const g = (org.value.groups || []).find((x) => x.id === groupId)
  if (!g) return
  confirmCfg.value = {
    title: t('pages.agentStudio.org.deleteGroupTitle'),
    message: t('pages.agentStudio.org.deleteGroupMessage', { name: g.name }),
    confirmText: t('pages.agentStudio.dialogs.delete'),
    danger: true,
    ok: async () => {
      const next = applyDeleteGroup(org.value, groupId)
      await persistOrg(next)
    },
  }
}

async function onMoveGroup(groupId: string, newParentId: string) {
  if (wouldCreateGroupCycle(org.value, groupId, newParentId)) {
    error.value = t('pages.agentStudio.org.groupCycle')
    return
  }
  const next = {
    ...org.value,
    groups: (org.value.groups || []).map((g) =>
      g.id === groupId ? { ...g, parentGroupId: newParentId || undefined } : g,
    ),
  }
  await persistOrg(next)
}

async function onMoveAgent(agentName: string, sourceGroupId: string, targetGroupId: string) {
  const next = applyMoveAgent(org.value, agentName, sourceGroupId, targetGroupId)
  await persistOrg(next)
}

async function onRemoveFromGroup(agentName: string, groupId: string) {
  const next = applyRemoveAgentFromGroup(org.value, agentName, groupId)
  if (await persistOrg(next)) {
    showToast(t('pages.agentStudio.org.removeFromGroupToast'))
  }
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
  resetOrgFromBaseline()
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
  leaveConfirmCfg.value = null
}

function leaveConfirmDiscard() {
  const c = leaveConfirmCfg.value
  if (!c) return
  c.onDiscard()
  leaveConfirmCfg.value = null
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
    const [list, o, projList] = await Promise.all([api.listAgents(), api.getAgentsOrg(), api.listProjects()])
    if (!studioSeq.isCurrentSeq(localSeq)) return
    agents.value = filterAgentsByScope(list)
    projects.value = (projList || []).map((p) => ({ id: p.id, name: p.name }))
    org.value = o?.groups ? o : { revision: o?.revision || 0, groups: o?.groups || [], agents: o?.agents || {} }
    if (!org.value.agents) org.value.agents = {}
    if (!org.value.groups) org.value.groups = []
    orgBaseline.value = orgSnapshot(org.value)
    syncOrgSheetCollapsedFromOrg()
    applyingStudioQuery = true
    try {
      const qAgent = typeof route.query.agent === 'string' ? route.query.agent.trim() : ''
      // Embedded: studioTab only. Standalone: studioTab, else legacy tab (bookmarks).
      const qTabRaw = embedded.value
        ? route.query.studioTab
        : route.query.studioTab ?? route.query.tab
      const qTab = isStudioTab(qTabRaw) ? qTabRaw : null
      const qSub = parseDataSub(route.query.sub)
      if (qAgent && agents.value.some((a) => a.name === qAgent)) {
        select(qAgent, { tab: qTab || 'files', dataSub: qSub, skipQuerySync: true })
      } else if (agents.value.length && !activeName.value) {
        select(agents.value[0].name, { skipQuerySync: true })
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

function resetOrgFromBaseline() {
  try {
    org.value = JSON.parse(orgBaseline.value || orgSnapshot(emptyOrg()))
  } catch {
    /* keep current */
  }
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
      ok: () => {
        resetOrgFromBaseline()
        select(name)
      },
    }
    return
  }
  select(name)
}

/** Narrow-screen sheet switch: leaveConfirm-style save / discard / cancel. */
function chooseAgentFromSheet(name: string) {
  if (name === activeName.value) {
    closeOrgSheet()
    return
  }
  if (!dirty.value) {
    select(name)
    closeOrgSheet()
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
      closeOrgSheet()
      return true
    },
    onDiscard: () => {
      resetOrgFromBaseline()
      select(name)
      closeOrgSheet()
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
    if (draft.value && draftPayloadJson(draft.value) !== originalJson.value) {
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
    if (orgSnapshot(org.value) !== orgBaseline.value) {
      const ok = await persistOrg(org.value)
      if (!ok) return false
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
  pendingFolderExportGroupId.value = ''
  if (dirty.value) {
    showUnsavedExport.value = true
    return
  }
  void doExport(activeName.value)
}

function cancelUnsavedExport() {
  showUnsavedExport.value = false
  pendingFolderExportGroupId.value = ''
}

async function discardAndExport() {
  showUnsavedExport.value = false
  if (pendingFolderExportGroupId.value) {
    showFolderSecrets.value = true
    return
  }
  if (!activeName.value) return
  await doExport(activeName.value)
}

async function saveThenExport() {
  const ok = await save()
  if (!ok) return
  showUnsavedExport.value = false
  if (pendingFolderExportGroupId.value) {
    showFolderSecrets.value = true
    return
  }
  if (activeName.value) await doExport(activeName.value)
}

async function onExportGroup(groupId: string) {
  if (exporting.value) return
  if (orgDirty.value) {
    if (!(await persistOrg(org.value))) return
  }
  const inSubtree =
    !!activeName.value && isAgentInGroupSubtree(org.value, activeName.value, groupId)
  if (inSubtree && agentDirty.value) {
    pendingFolderExportGroupId.value = groupId
    showUnsavedExport.value = true
    return
  }
  pendingFolderExportGroupId.value = groupId
  showFolderSecrets.value = true
}

function cancelFolderSecrets() {
  showFolderSecrets.value = false
  pendingFolderExportGroupId.value = ''
}

async function confirmFolderSecrets() {
  const groupId = pendingFolderExportGroupId.value
  showFolderSecrets.value = false
  pendingFolderExportGroupId.value = ''
  if (!groupId || exporting.value) return
  exporting.value = true
  error.value = ''
  try {
    const { blob, filename } = await api.exportOrgFolder(groupId)
    downloadZip(blob, filename)
    const base = filename.replace(/\.zip$/i, '')
    showToast(t('pages.agentStudio.exportImport.folderExportSuccess', { name: base }))
  } catch (e: any) {
    error.value = String(e?.message || e)
  } finally {
    exporting.value = false
  }
}

function onImportGroup(groupId: string) {
  triggerGroupImport(groupId)
}

function isClearSensitiveKeySelected(key: string): boolean {
  return clearSensitiveSelected.value.has(key)
}

function toggleClearSensitiveKey(key: string, checked: boolean) {
  const next = new Set(clearSensitiveSelected.value)
  if (checked) next.add(key)
  else next.delete(key)
  clearSensitiveSelected.value = next
}

function selectAllClearSensitiveKeys() {
  clearSensitiveSelected.value = new Set(clearSensitiveHits.value.map((h) => h.key))
}

function clearAllClearSensitiveKeys() {
  clearSensitiveSelected.value = new Set()
}

function cancelClearSensitive() {
  showClearSensitive.value = false
  clearSensitiveBusy.value = false
  clearSensitiveGroupId.value = ''
  clearSensitiveGroupName.value = ''
  clearSensitiveAgentCount.value = 0
  clearSensitiveHits.value = []
  clearSensitiveSelected.value = new Set()
}

async function onClearSensitiveConfig(groupId: string) {
  if (clearSensitiveBusy.value) return
  if (orgDirty.value) {
    if (!(await persistOrg(org.value))) return
  }
  clearSensitiveBusy.value = true
  error.value = ''
  try {
    const group = (org.value.groups || []).find((g) => g.id === groupId)
    const names = recursiveMemberNames(org.value, groupId, agentNames.value)
    const { keys } = await api.scanOrgSensitiveKeys(groupId)
    if (!keys.length) {
      showToast(t('pages.agentStudio.org.clearSensitive.empty'))
      return
    }
    clearSensitiveGroupId.value = groupId
    clearSensitiveGroupName.value = group?.name || groupId
    clearSensitiveAgentCount.value = names.length
    clearSensitiveHits.value = keys
    clearSensitiveSelected.value = new Set(keys.map((k) => k.key))
    showClearSensitive.value = true
  } catch (e: any) {
    error.value = String(e?.message || e)
  } finally {
    clearSensitiveBusy.value = false
  }
}

async function confirmClearSensitive() {
  const groupId = clearSensitiveGroupId.value
  const keys = [...clearSensitiveSelected.value]
  if (!groupId || !keys.length || clearSensitiveBusy.value) return
  clearSensitiveBusy.value = true
  error.value = ''
  try {
    const result = await api.stripOrgSensitiveKeys(groupId, keys)
    const failed = result.failed || []
    if (failed.length) {
      showToast(
        t('pages.agentStudio.org.clearSensitive.partialFail', {
          cleared: result.cleared,
          failed: failed.join(', '),
        }),
      )
    } else {
      showToast(
        t('pages.agentStudio.org.clearSensitive.success', {
          agents: result.cleared,
          keys: (result.strippedKeys || keys).length,
        }),
      )
    }
    const affected = new Set(result.agentNames || [])
    if (activeName.value && affected.has(activeName.value)) {
      await reloadAgentFromServer(activeName.value)
    }
    await refreshAgentsList()
    cancelClearSensitive()
  } catch (e: any) {
    error.value = String(e?.message || e)
  } finally {
    clearSensitiveBusy.value = false
  }
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
  // New agents default to ungrouped / no parent (no org entry).
  select(created.name)
  showToast(t('pages.agentStudio.wizard.createdToast', { name: created.name }))
}

function onTeamBootstrapStarted(session: TeamBootstrapSession) {
  teamBootstrapSessionId.value = session.id
  void reloadOrg()
  void refreshAgentsList()
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

async function onTeamBootstrapRefresh() {
  await Promise.all([reloadOrg(), refreshAgentsList()])
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

/** 侧栏铅笔：阻断改名并引导至 Agent 管理（不调用 api.renameAgent） */
function onSidebarRenameBlocked(name: string) {
  renameBlockedTarget.value = name
  showRenameBlocked.value = true
}

function closeRenameBlocked() {
  showRenameBlocked.value = false
  renameBlockedTarget.value = ''
}

function gotoManageFromBlocked() {
  const target = renameBlockedTarget.value
  closeRenameBlocked()
  openAgentManage(target)
}

function openRenameAgent(name: string) {
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
      await reloadOrg()
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
  confirmCfg.value = {
    title: t('pages.agentStudio.dialogs.deleteAgentTitle'),
    message: t('pages.agentStudio.dialogs.deleteAgentMessage', { name }),
    confirmText: t('pages.agentStudio.dialogs.delete'),
    danger: true,
    ok: async () => {
      await api.deleteAgent(name)
      agents.value = agents.value.filter((a) => a.name !== name)
      await reloadOrg()
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
  confirmCfg.value = null
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

watch(
  () => (org.value.groups || []).map((g) => g.id).join(','),
  () => syncOrgSheetCollapsedFromOrg(),
)

watch([activeName, manageFocusAgent], () => {
  const next = new Set(orgSheetCollapsed.value)
  for (const name of expandAgentsForOrgSheet([activeName.value])) {
    for (const id of ancestorGroupIdsForAgent(org.value, name)) next.delete(id)
  }
  orgSheetCollapsed.value = next
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
  ORG_SIDEBAR_EXPANDED_W,
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
  showOrgSheet,
  orgSheetCollapsed,
  leaveConfirmCfg,
  agents,
  projects,
  org,
  displayOrg,
  orgBaseline,
  activeName,
  draft,
  originalJson,
  tab,
  dataSubTab,
  orgSaving,
  applyingStudioQuery,
  syncStudioQuery,
  savedProjectId,
  isProjectBound,
  draftBindingDirty,
  projectNameById,
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
  showRenameBlocked,
  renameBlockedTarget,
  showUnsavedExport,
  exporting,
  showFolderSecrets,
  pendingFolderExportGroupId,
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
  triggerGroupImport,
  onImportDiscardCancel,
  onImportDiscardConfirm,
  onImportFileChange,
  selectImportConflict,
  closeImportConflict,
  confirmImportConflict,
  closeBatchConflict,
  confirmBatchRename,
  confirmBatchOverwrite,
  orgSnapshot,
  agentDirty,
  orgDirty,
  dirty,
  agentNames,
  manageSearch,
  manageSearchQuery,
  filteredManageNames,
  manageSearchActive,
  manageNameHighlight,
  clearManageSearch,
  orgSheetRows,
  showAssignPick,
  showAssignCover,
  showAssignDraft,
  assignApplying,
  assignGroupName,
  assignMembers,
  assignTargetId,
  assignDiffBound,
  assignFail,
  assignOkCount,
  assignTargetLabel,
  assignMemberList,
  assignAffectedList,
  closeAssignModals,
  onAssignProject,
  onAssignPickNext,
  cancelAssignCover,
  maybeAssignDraftThenApply,
  keepAssignDraft,
  syncDraftProjectId,
  applyAssign,
  openOrgSheet,
  closeOrgSheet,
  toggleOrgSheetNode,
  orgSheetPadStyle,
  persistOrg,
  reloadOrg,
  openCreateRootGroup,
  openCreateChildGroup,
  openRenameGroup,
  confirmDeleteGroup,
  onMoveGroup,
  onMoveAgent,
  onRemoveFromGroup,
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
  resetOrgFromBaseline,
  chooseAgent,
  chooseAgentFromSheet,
  openManageFromSheet,
  save,
  doExport,
  triggerExport,
  cancelUnsavedExport,
  discardAndExport,
  saveThenExport,
  onExportGroup,
  cancelFolderSecrets,
  confirmFolderSecrets,
  onImportGroup,
  onClearSensitiveConfig,
  showClearSensitive,
  clearSensitiveBusy,
  clearSensitiveGroupName,
  clearSensitiveAgentCount,
  clearSensitiveHits,
  clearSensitiveSelectedCount,
  isClearSensitiveKeySelected,
  toggleClearSensitiveKey,
  selectAllClearSensitiveKeys,
  clearAllClearSensitiveKeys,
  cancelClearSensitive,
  confirmClearSensitive,
  showCreateWizard,
  showTeamWizard,
  teamBootstrapSessionId,
  openCreateAgent,
  openCreateTeam,
  showCreateTeam,
  hideTeamCreate,
  embedded,
  scopedProjectId,
  onWizardCreated,
  onTeamBootstrapStarted,
  refreshAgentsList,
  onTeamBootstrapRefresh,
  onTeamBootstrapSelectPm,
  onTeamBootstrapOpenPm,
  onTeamBootstrapDone,
  openAgentManage,
  closeAgentManage,
  onSidebarRenameBlocked,
  closeRenameBlocked,
  gotoManageFromBlocked,
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
  UNGROUPED_ID,
  }
}
