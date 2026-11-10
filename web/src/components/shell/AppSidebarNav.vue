<script setup lang="ts">
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { computed, ref, watch, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../ui/Icon.vue'
import AppButton from '../ui/AppButton.vue'
import AppModal from '../ui/AppModal.vue'
import InboxMotionMark from '@/components/inbox/InboxMotionMark.vue'
import { isBoardSectionPath, isHomeSectionPath, sidebarNavGroups } from '@/data/sidebarNav'
import { isNodeConversationPath, nodeConversationPath, nodeConversationRunId } from '@/lib/run/nodeConversation'
import { isReplyingInboxItem, isStartingInboxItem, sidebarRunTitle } from '@/lib/inbox/inboxDisplay'
import { isShareableInboxItem } from '@/lib/inbox/gateShareLink'
import {
  beginInboxCancelDeparture,
  endInboxCancelDeparture,
} from '@/lib/inbox/inboxCancelDeparture'
import {
  INBOX_SHARE_INTENT_QUERY,
  INBOX_SHARE_INTENT_VALUE,
  knownInboxProcessingLock,
} from '@/lib/inbox/inboxShareIntent'
import { orderWithPins, useSidebarPins } from '@/lib/inbox/sidebarPins'
import { api } from '@/lib/api/api'
import { displayRunTitle, RUN_TITLE_MAX } from '@/lib/run/runTitle'
import type { InboxItem, Run } from '@/lib/shared/types'
import { isAutoProcessNode, keepRowOrder, mergeRunRows, pinRunId, RUNNING_RUN_STATUSES, sidebarAutoNodeName, sidebarLandedOnHuman, sidebarRunGroup } from '@/lib/inbox/sidebarRunRows'
import {
  clearSidebarHandoff,
  consumeSidebarHandoff,
  isSidebarConfirmWindowHeld,
  isSidebarRunDismissed,
  readSidebarHandoff,
  sidebarHandoffEpoch,
  sidebarTrackedRunIds,
  type SidebarRunGroup,
} from '@/lib/inbox/sidebarHandoff'
import { onRecentActivity, recentCurrentNode, recentRunActive, recentRunMark, recentRunTarget, runDetailPath, runLivePath } from '@/lib/run/recentRuns'
import { useWorkspaceContext } from '@/lib/composables/useWorkspaceContext'
import {
  settingsItemActive,
  settingsItemKey,
  settingsNavItems,
  type SettingsNavItem,
} from '@/data/settingsNav'
import { useExecutionMode } from '@/lib/composables/useBrandSettings'
import { usePendingGates } from '@/lib/inbox/usePendingGates'
import { useRunTerminalNotifications } from '@/lib/run/useRunTerminalNotifications'
import { useToast } from '@/lib/composables/useToast'
import { useBreakpoint } from '@/lib/composables/useBreakpoint'
import { isSoloActive, useSoloSessions } from '@/lib/solo/useSoloSessions'
import { soloErrorText } from '@/lib/solo/soloErrors'
import type { SoloSession } from '@/lib/api/apiTypes'
import SidebarSectionHeading from './SidebarSectionHeading.vue'
import SidebarWorkspaceLists from './SidebarWorkspaceLists.vue'
import { isSectionCollapsed, toggleSection } from '@/lib/shared/sidebarSections'
import { useSidebarNavigation } from '@/lib/composables/useSidebarNavigation'

const props = defineProps<{ drawer?: boolean; rail?: boolean; workspaceLists?: boolean }>()
const emit = defineEmits<{ (e: 'navigate'): void }>()

const { t, te, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const toast = useToast()

// Shared singleton source so approving a gate elsewhere updates the lists immediately.
const {
  displayedItems,
  sidebarExtras,
  startFailedKeys,
  activeInboxKey,
  peek,
  refresh,
  itemKey,
  patchRunTitle,
} = usePendingGates()
const { pins, pin: pinRow, unpin: unpinRow } = useSidebarPins()
// Same unreadCount singleton as shell chrome bell — keep the settings notification badge in sync.
const { unreadCount } = useRunTerminalNotifications()
const { isMobile } = useBreakpoint()
const soloStore = useSoloSessions()
const soloCollapsed = computed(() => isSectionCollapsed('solo'))
const runningCollapsed = computed(() => isSectionCollapsed('running'))
const queuedCollapsed = computed(() => isSectionCollapsed('queued'))
const navRoot = ref<HTMLElement | null>(null)

let timer: number | undefined
function badgeFor(to: string): number {
  if (to === '/notifications') return unreadCount.value
  return 0
}

function pendingNodeLabel(it: InboxItem): string {
  return it.type === 'gate' ? it.title : it.label
}

function pendingTooltip(it: InboxItem): string {
  const name = rowTitle(it)
  const node = pendingNodeLabel(it).trim()
  return node ? `${name} · ${node}` : name
}

const pendingItems = computed(() => {
  const shown = new Set(displayedItems.value.map(itemKey))
  const extras = sidebarExtras.value.filter((it) => !shown.has(itemKey(it)))
  return [...extras, ...displayedItems.value]
})
const pendingRunIds = computed(() => new Set(pendingItems.value.map((it) => it.runId)))

/** Unfinished runs (queued / running / waiting_human) for「工作流」「排队中」. */
const runningRuns = ref<Run[]>([])
const runningById = computed(() => new Map(runningRuns.value.map((r) => [r.id, r])))
/** A row is named after its run: the run's own title wins over whatever the pending item carries. */
function rowRunTitle(it: InboxItem): string | undefined {
  const run = runningById.value.get(it.runId)
  return run?.title?.trim() ? run.title : it.runTitle
}
function rowTitle(it: InboxItem): string {
  const run = runningById.value.get(it.runId)
  return sidebarRunTitle({ runTitle: rowRunTitle(it), workflowName: run?.workflowName || it.workflowName })
}
/**
 * A run whose pending item just left (hand-off confirmed) can stay in place
 * until the next unfinished-run read, but only if that run was already in the
 * previous unfinished list. A read that is aborted, skipped, or fails drops
 * these placeholders instead of leaving them in the count.
 */
const carriedRows = ref<InboxItem[]>([])
let runningRequest: AbortController | null = null
/** An activity notice arrived while a list read was still in flight. */
let runningFollowUp = false
let navAlive = true
const { workspace, ensureLoaded: ensureWorkspaceLoaded } = useWorkspaceContext()
const workspaceReady = computed(() => !!workspace.value)

function dropCarriedRows(controller: AbortController) {
  if (runningRequest === controller) carriedRows.value = []
}

async function loadRunningRuns(opts?: { follow?: boolean }) {
  // Execution pages notify about every 300ms. Aborting the read already on
  // the wire would drop it before it can paint, so remember one follow-up
  // and let this request land. A failed read still keeps the last success.
  if (opts?.follow && runningRequest) {
    runningFollowUp = true
    return
  }
  runningFollowUp = false
  runningRequest?.abort()
  const controller = new AbortController()
  runningRequest = controller
  try {
    if (!(await ensureWorkspaceLoaded()) || runningRequest !== controller) {
      dropCarriedRows(controller)
      return
    }
    // Solo sessions are resident sandboxes; active ones fill「正在执行」.
    void soloStore.refresh()
    const result = await api.listRuns({
      page: 1,
      pageSize: 50,
      status: RUNNING_RUN_STATUSES,
      sort: 'last_activity',
      order: 'desc',
      signal: controller.signal,
    })
    if (runningRequest !== controller || !Array.isArray(result?.items)) {
      dropCarriedRows(controller)
      return
    }
    runningRuns.value = result.items
    carriedRows.value = []
  } catch {
    // Keep the last unfinished list. Placeholders from a read that did not
    // land must not stay in the count; the next poll retries.
    dropCarriedRows(controller)
  } finally {
    if (runningRequest === controller) {
      runningRequest = null
      if (runningFollowUp && navAlive) {
        runningFollowUp = false
        void loadRunningRuns()
      }
    }
  }
}

/** Run order last shown; see keepRowOrder. */
let shownRunOrder: string[] = []
/** Group last painted for each run. A confirm window freezes a row that was waiting on a person. */
let paintedGroups = new Map<string, SidebarRunGroup>()
const pendingRows = computed(() => {
  const rows = mergeRunRows(pendingItems.value, runningRuns.value, activeInboxKey.value)
  const listed = new Set(rows.map((r) => r.runId))
  const unfinished = new Set(runningRuns.value.map((r) => r.id))
  const merged = [
    ...rows,
    ...carriedRows.value.filter((c) => !listed.has(c.runId) && unfinished.has(c.runId)),
  ]
  const ordered = keepRowOrder(merged, shownRunOrder)
  shownRunOrder = ordered.map((r) => r.runId)
  return ordered
})
watch(pendingItems, (next, prev) => {
  const still = new Set(next.map((it) => it.runId))
  const unfinished = new Set(runningRuns.value.map((r) => r.id))
  const left = (prev ?? []).filter(
    (it, i, arr) =>
      !still.has(it.runId) &&
      unfinished.has(it.runId) &&
      arr.findIndex((x) => x.runId === it.runId) === i,
  )
  if (!left.length) return
  const carried = carriedRows.value.slice()
  for (const it of left) {
    if (startFailedKeys.value.includes(itemKey(it)) || carried.some((c) => c.runId === it.runId)) continue
    carried.push({ ...it, state: undefined } as InboxItem)
  }
  carriedRows.value = carried
  void loadRunningRuns()
})

/** Row with no pending item: a run that is executing on its own (or just handed off). */
function isRunOnlyRow(it: InboxItem): boolean {
  return !pendingRunIds.value.has(it.runId)
}

const pendingRunTitles = computed(() => {
  const titles: Record<string, string> = {}
  for (const item of pendingRows.value) {
    if (typeof item.runTitle === 'string') titles[item.runId] = item.runTitle
  }
  return titles
})

const runPins = computed(() => pins.value.filter((p) => !p.itemKey.startsWith('solo:')).map((p) => ({ ...p, itemKey: pinRunId(p.itemKey) })))
const soloPins = computed(() => pins.value.filter((p) => p.itemKey.startsWith('solo:')))
const isRowPinned = (it: InboxItem) => runPins.value.some((p) => p.itemKey === it.runId)
type RowGroup = 'running' | 'auto' | 'queued' | 'left'
function rowGroup(runId: string): RowGroup {
  if (isSidebarRunDismissed(runId)) return 'left'
  // This confirm's window is still up. Keep the row painted as waiting on a person,
  // even when the handoff or the snapshot already says automatic. A row already
  // painted as automatic is not this handoff and must not be pulled back.
  if (isSidebarConfirmWindowHeld(runId) && paintedGroups.get(runId) !== 'auto') return 'running'
  const run = runningById.value.get(runId)
  if (run?.status === 'queued') return 'queued'
  return sidebarRunGroup(run, readSidebarHandoff(runId))
}
const rowGroups = computed(() => {
  void sidebarHandoffEpoch.value
  const groups = new Map<string, RowGroup>()
  for (const it of pendingRows.value) groups.set(it.runId, rowGroup(it.runId))
  return groups
})
function autoNodeName(it: InboxItem): string {
  if (rowGroup(it.runId) !== 'auto') return ''
  return sidebarAutoNodeName(runningById.value.get(it.runId), readSidebarHandoff(it.runId))
}
const isWorkflowGroup = (group: RowGroup | undefined): group is SidebarRunGroup => group === 'running' || group === 'auto'
const workflowPendingRows = computed(() => pendingRows.value.filter((it) => isWorkflowGroup(rowGroups.value.get(it.runId))))
const queuedPendingRows = computed(() => pendingRows.value.filter((it) => rowGroups.value.get(it.runId) === 'queued'))
const orderedWorkflowRows = computed(() => orderWithPins(workflowPendingRows.value, (it) => it.runId, runPins.value))
const orderedQueuedRows = computed(() => orderWithPins(queuedPendingRows.value, (it) => it.runId, runPins.value))
/**
 * Active Solo sandboxes sit in「正在执行」; every executing workflow run, waiting or automatic, sits in「工作流」.
 * Ordered by start time (newest first) so activity never reshuffles the rows.
 */
const soloRows = computed(() => [...soloStore.active.value].sort((a, b) => {
  const aAt = Date.parse(a.createdAt)
  const bAt = Date.parse(b.createdAt)
  const diff = (Number.isFinite(bAt) ? bAt : 0) - (Number.isFinite(aAt) ? aAt : 0)
  return diff || (a.id < b.id ? -1 : a.id > b.id ? 1 : 0)
}))
const orderedSoloRows = computed(() => orderWithPins(soloRows.value, soloRowKey, soloPins.value))
const soloSectionCount = computed(() => orderedSoloRows.value.length)
const runningSectionCount = computed(() => orderedWorkflowRows.value.length)

type SidebarSectionEntry =
  | { kind: 'run'; key: string; row: InboxItem }
  | { kind: 'solo'; key: string; session: SoloSession }
const runEntry = (row: InboxItem): SidebarSectionEntry => ({ kind: 'run', key: row.runId, row })
const soloEntry = (session: SoloSession): SidebarSectionEntry => ({ kind: 'solo', key: soloRowKey(session), session })
const queuedSectionCount = computed(() => orderedQueuedRows.value.length)

type RunSectionId = 'solo' | 'running' | 'queued'
const runSections = computed(() => {
  const sections: Array<{
    id: RunSectionId
    testId: string
    headingTestId: string
    toggleTestId: string
    rowsTestId: string
    labelKey: string
    icon: 'run-live' | 'workflow' | 'queue'
    count: number
    collapsed: boolean
    entries: SidebarSectionEntry[]
  }> = []
  if (soloSectionCount.value > 0) {
    sections.push({
      id: 'solo',
      testId: 'nav-solo-running',
      headingTestId: 'nav-solo-heading',
      toggleTestId: 'nav-solo-toggle',
      rowsTestId: 'nav-solo-rows',
      labelKey: 'nav.runningRuns',
      icon: 'run-live',
      count: soloSectionCount.value,
      collapsed: soloCollapsed.value,
      entries: orderedSoloRows.value.map(soloEntry),
    })
  }
  if (runningSectionCount.value > 0) {
    sections.push({
      id: 'running',
      testId: 'nav-pending-approvals',
      headingTestId: 'nav-running-heading',
      toggleTestId: 'nav-running-toggle',
      rowsTestId: 'nav-running-rows',
      labelKey: 'nav.workflows',
      icon: 'workflow',
      count: runningSectionCount.value,
      collapsed: runningCollapsed.value,
      entries: orderedWorkflowRows.value.map(runEntry),
    })
  }
  if (queuedSectionCount.value > 0) {
    sections.push({
      id: 'queued',
      testId: 'nav-queued',
      headingTestId: 'nav-queued-heading',
      toggleTestId: 'nav-queued-toggle',
      rowsTestId: 'nav-queued-rows',
      labelKey: 'nav.queuedRuns',
      icon: 'queue',
      count: queuedSectionCount.value,
      collapsed: queuedCollapsed.value,
      entries: orderedQueuedRows.value.map(runEntry),
    })
  }
  return sections
})

function soloRowKey(session: SoloSession): string {
  return `solo:${session.id}`
}

function soloRowTitle(session: SoloSession): string {
  return session.title?.trim() || t('pages.solo.untitled')
}

function soloRowMotion(session: SoloSession): 'spin' | 'pulse' | null {
  if (soloStore.closing.value.has(session.id)) return 'spin'
  if (session.status === 'starting') return 'spin'
  return session.busy ? 'pulse' : null
}

function isSoloRowActive(session: SoloSession): boolean {
  return route.path === `/solo/${encodeURIComponent(session.id)}`
}

function onSoloRow(session: SoloSession) {
  if (suppressPendingClick.value || editingKey.value) return
  void router.push(`/solo/${encodeURIComponent(session.id)}`)
  onNavigate()
}

const LONG_PRESS_MS = 500
const LONG_PRESS_MOVE_PX = 8
const MENU_WIDTH = 168
/** Blocked share copy is longer than the two-item menu, so the clamp must follow it. */
const MENU_WIDTH_BLOCKED = 280
/** 8px of menu padding plus 32px per row (workflow view, pin, rename, cancel, optional share). */
const MENU_CHROME = 8
const MENU_ROW_HEIGHT = 32
const MENU_MARGIN = 8

const editingKey = ref<string | null>(null)
const renameDraft = ref('')
const renameInput = ref<HTMLInputElement | null>(null)
const saving = ref(false)
const editEpoch = ref(0)
const suppressPendingClick = ref(false)
let rowClickReleaseTimer: number | undefined
type MenuTarget = { kind: 'run' | 'solo'; key: string; x: number; y: number }
const menu = ref<MenuTarget | null>(null)
const menuEl = ref<HTMLElement | null>(null)

type RowPress = { key: string; x: number; y: number; timer: number }
let rowPress: RowPress | null = null

const menuItem = computed(() => {
  const key = menu.value?.key
  if (!key || menu.value?.kind !== 'run') return null
  return pendingRows.value.find((it) => it.runId === key) ?? null
})
const menuSolo = computed(() => {
  const key = menu.value?.key
  if (!key || menu.value?.kind !== 'solo') return null
  return soloStore.sessions.value.find((it) => soloRowKey(it) === key) ?? null
})
const menuSoloActive = computed(() => !!menuSolo.value && isSoloActive(menuSolo.value))
const menuSoloClosing = computed(() => !!menuSolo.value && soloStore.closing.value.has(menuSolo.value.id))
const menuSoloRelease = computed(() => menuSolo.value?.status === 'closed' && !!(menuSolo.value.sandboxId || menuSolo.value.sandbox?.id))
const changingSoloEnvironments = ref(new Set<string>())
const menuSoloEnvironmentDisabled = computed(() => {
  const session = menuSolo.value
  return !session || menuSoloClosing.value || changingSoloEnvironments.value.has(session.id) || (session.status !== 'stopped' && (session.busy || session.waiting > 0 || session.status === 'starting'))
})
const menuSoloEnvironmentLabel = computed(() => {
  const session = menuSolo.value
  if (session?.status === 'stopped') return t('pages.solo.environment.resume')
  return t(session?.sandbox?.desiredState === 'stopped' ? 'pages.solo.environment.retryStop' : 'pages.solo.environment.stop')
})
const menuPinned = computed(() => {
  if (menuSolo.value) return soloPins.value.some((p) => p.itemKey === soloRowKey(menuSolo.value!))
  return menuItem.value ? isRowPinned(menuItem.value) : false
})

function setRenameInput(el: unknown) {
  renameInput.value = el instanceof HTMLInputElement ? el : null
}

function closeMenu() {
  menu.value = null
}

function shareLinkState(it: InboxItem): string | undefined {
  if (!('shareLink' in it) || !it.shareLink) return undefined
  return it.shareLink.state
}

/** Missing shareLink is not "used". A missing lock is not a lock. */
function shareBlockReason(it: InboxItem): 'starting' | 'locked' | 'used' | null {
  if (!isShareableInboxItem(it)) return null
  if (isStartingInboxItem(it)) return 'starting'
  if (knownInboxProcessingLock.value) return 'locked'
  if (shareLinkState(it) === 'used') return 'used'
  return null
}

function pendingMenuHeight(it: InboxItem): number {
  const shareRow = !isRunOnlyRow(it) && isShareableInboxItem(it) ? 1 : 0
  const rows = 4 + shareRow
  return MENU_CHROME + rows * MENU_ROW_HEIGHT
}

function openMenu(it: InboxItem, x: number, y: number) {
  if (editingKey.value) return
  const height = pendingMenuHeight(it)
  const width = !isRunOnlyRow(it) && shareBlockReason(it) ? MENU_WIDTH_BLOCKED : MENU_WIDTH
  const left = Math.max(MENU_MARGIN, Math.min(x, window.innerWidth - width))
  const top = Math.max(MENU_MARGIN, Math.min(y, window.innerHeight - height))
  menu.value = { kind: 'run', key: it.runId, x: left, y: top }
}

function openSoloMenu(session: SoloSession, x: number, y: number) {
  if (editingKey.value) return
  const width = MENU_WIDTH
  const active = isSoloActive(session)
  const environment = active || (session.status === 'closed' && !!(session.sandboxId || session.sandbox?.id))
  const rows = (active ? 3 : 0) + (worktreeMode.value ? 0 : 1 + Number(environment))
  const separators = active && !worktreeMode.value ? 2 : 0
  const height = MENU_CHROME + rows * MENU_ROW_HEIGHT + separators * 9
  const left = Math.max(MENU_MARGIN, Math.min(x, window.innerWidth - width))
  const top = Math.max(MENU_MARGIN, Math.min(y, window.innerHeight - height))
  menu.value = { kind: 'solo', key: soloRowKey(session), x: left, y: top }
}

function onMenuSoloWorkspace(panel: 'snapshots' | 'release') {
  const session = menuSolo.value
  if (!session || worktreeMode.value || (panel === 'release' && (!menuSoloRelease.value || menuSoloClosing.value))) return
  closeMenu()
  const path = `/solo/${encodeURIComponent(session.id)}`
  const query = { ...(route.path === path ? route.query : {}), workspace: panel }
  if (route.path === path) void router.replace({ path, query })
  else void router.push({ path, query })
  onNavigate()
}

async function onMenuSoloEnvironment() {
  const session = menuSolo.value
  if (!session || !menuSoloActive.value || worktreeMode.value || menuSoloEnvironmentDisabled.value) return
  changingSoloEnvironments.value.add(session.id)
  closeMenu()
  try {
    const saved = await (session.status === 'stopped' ? api.resumeSoloEnvironment(session.id) : api.stopSoloEnvironment(session.id))
    soloStore.upsert(saved)
  } catch (cause) {
    toast.error(soloErrorText(cause, t, te, locale.value))
  } finally {
    changingSoloEnvironments.value.delete(session.id)
  }
}

const menuShareVisible = computed(
  () => !!menuItem.value && !isRunOnlyRow(menuItem.value) && isShareableInboxItem(menuItem.value),
)
const menuShareReason = computed(() => (menuItem.value ? shareBlockReason(menuItem.value) : null))
const menuShareDisabled = computed(() => menuShareReason.value !== null)
const menuShareLabel = computed(() => {
  if (menuShareReason.value === 'starting') {
    return t('nav.pendingShareBlocked', { reason: t('nav.pendingShareStarting') })
  }
  if (menuShareReason.value === 'locked') {
    return t('nav.pendingShareBlocked', { reason: t('nav.pendingShareLocked') })
  }
  if (menuShareReason.value === 'used') {
    return t('nav.pendingShareBlocked', { reason: t('nav.pendingShareUsed') })
  }
  return t('pages.gatesInbox.share.shareApproval')
})

function clearRowPress() {
  if (!rowPress) return
  window.clearTimeout(rowPress.timer)
  window.removeEventListener('pointermove', onRowPressMove)
  rowPress = null
}

function onRowPressMove(event: PointerEvent) {
  if (!rowPress) return
  if (
    Math.abs(event.clientX - rowPress.x) > LONG_PRESS_MOVE_PX ||
    Math.abs(event.clientY - rowPress.y) > LONG_PRESS_MOVE_PX
  ) {
    clearRowPress()
  }
}

function onRowPressEnd() {
  clearRowPress()
  if (!suppressPendingClick.value) return
  window.clearTimeout(rowClickReleaseTimer)
  // Keep suppressing while the finger is held, then cover compatibility clicks.
  rowClickReleaseTimer = window.setTimeout(() => { suppressPendingClick.value = false }, 400)
}

function startRowPress(event: PointerEvent, key: string, open: (x: number, y: number) => void) {
  if (event.pointerType === 'mouse' || editingKey.value) return
  clearRowPress()
  const timer = window.setTimeout(() => {
    if (!rowPress || rowPress.key !== key) return
    const x = rowPress.x
    const y = rowPress.y
    window.clearTimeout(rowClickReleaseTimer)
    suppressPendingClick.value = true
    clearRowPress()
    if (!editingKey.value) open(x, y)
  }, LONG_PRESS_MS)
  rowPress = { key, x: event.clientX, y: event.clientY, timer }
  window.addEventListener('pointermove', onRowPressMove)
}

function onPendingPointerDown(event: PointerEvent, it: InboxItem) {
  startRowPress(event, it.runId, (x, y) => {
    const row = pendingRows.value.find(item => item.runId === it.runId)
    if (row) openMenu(row, x, y)
  })
}

function onSoloPointerDown(event: PointerEvent, session: SoloSession) {
  if (worktreeMode.value && !isSoloActive(session)) return
  startRowPress(event, soloRowKey(session), (x, y) => {
    const row = soloStore.sessions.value.find(item => item.id === session.id)
    if (row) openSoloMenu(row, x, y)
  })
}

function onSoloHistoryClick(event: MouseEvent) {
  if (!suppressPendingClick.value) return
  event.preventDefault()
  event.stopPropagation()
}

function onPendingContextMenu(event: MouseEvent, it: InboxItem) {
  event.preventDefault()
  event.stopPropagation()
  if (editingKey.value) return
  clearRowPress()
  openMenu(it, event.clientX, event.clientY)
}

function onSoloContextMenu(event: MouseEvent, session: SoloSession) {
  if (worktreeMode.value && !isSoloActive(session)) return
  event.preventDefault()
  event.stopPropagation()
  if (editingKey.value) return
  clearRowPress()
  openSoloMenu(session, event.clientX, event.clientY)
}

function onSoloMenuKeydown(event: KeyboardEvent, session: SoloSession) {
  if (event.key !== 'ContextMenu' && !(event.shiftKey && event.key === 'F10')) return
  if (worktreeMode.value && !isSoloActive(session)) return
  event.preventDefault()
  const rect = (event.currentTarget as HTMLElement).getBoundingClientRect()
  openSoloMenu(session, rect.left + 12, rect.bottom)
  void nextTick(() => menuEl.value?.querySelector<HTMLButtonElement>('button:not(:disabled)')?.focus())
}

function onDocMouseDown(event: MouseEvent) {
  if (!menu.value) return
  const target = event.target
  if (target instanceof Node && menuEl.value?.contains(target)) return
  // Touch release can synthesize a mouse down on the row that opened the menu.
  if (suppressPendingClick.value && target instanceof Element && target.closest('[data-testid="nav-pending-row"], [data-testid="sidebar-recent-solo"]')) return
  closeMenu()
}

function onDocKeydown(event: KeyboardEvent) {
  if (event.key !== 'Escape' || !menu.value) return
  event.preventDefault()
  closeMenu()
}

function cancelEdit() {
  editingKey.value = null
  renameDraft.value = ''
}

async function beginRename(it: InboxItem) {
  if (saving.value) return
  closeMenu()
  editingKey.value = it.runId
  renameDraft.value = displayRunTitle(rowRunTitle(it))
  await nextTick()
  const el = renameInput.value
  if (!el) return
  el.focus()
  const end = el.value.length
  el.setSelectionRange(end, end)
}

function refocusRename() {
  void nextTick(() => {
    renameInput.value?.focus()
  })
}

async function commitRename(it: InboxItem) {
  if (saving.value || editingKey.value !== it.runId) return
  if (!pendingRows.value.some((row) => row.runId === it.runId)) {
    cancelEdit()
    return
  }
  const next = renameDraft.value.trim()
  const prev = displayRunTitle(rowRunTitle(it))
  if (next === prev) {
    cancelEdit()
    return
  }
  if (!next) {
    toast.error(t('nav.pendingNameEmpty'))
    refocusRename()
    return
  }
  if ([...next].length > RUN_TITLE_MAX) {
    toast.error(t('nav.pendingNameTooLong'))
    refocusRename()
    return
  }
  const epoch = editEpoch.value
  const runId = it.runId
  saving.value = true
  try {
    const saved = await api.updateRunTitle(runId, next)
    const title = saved.title?.trim() || next
    patchRunTitle(runId, title)
    runningRuns.value = runningRuns.value.map((r) => (r.id === runId ? { ...r, title } : r))
    carriedRows.value = carriedRows.value.map((r) => (r.runId === runId ? { ...r, runTitle: title } : r))
    if (epoch !== editEpoch.value) return
    editingKey.value = null
    renameDraft.value = ''
  } catch {
    if (epoch !== editEpoch.value) return
    toast.error(t('nav.pendingRenameFailed'))
    refocusRename()
  } finally {
    if (epoch === editEpoch.value) saving.value = false
  }
}

function onRenameEscape() {
  if (saving.value) return
  cancelEdit()
}

function onRenameBlur(it: InboxItem) {
  const key = it.runId
  window.setTimeout(() => {
    if (saving.value || editingKey.value !== key) return
    if (document.activeElement === renameInput.value) return
    void commitRename(it)
  }, 0)
}

async function commitSoloRename(session: SoloSession) {
  const key = soloRowKey(session)
  if (saving.value || editingKey.value !== key) return
  const next = renameDraft.value.trim()
  if (!next) {
    toast.error(t('nav.pendingNameEmpty'))
    refocusRename()
    return
  }
  if ([...next].length > RUN_TITLE_MAX) {
    toast.error(t('nav.pendingNameTooLong'))
    refocusRename()
    return
  }
  saving.value = true
  try {
    const saved = await api.renameSoloSession(session.id, next)
    soloStore.upsert(saved)
    editingKey.value = null
    renameDraft.value = ''
  } catch {
    toast.error(t('nav.pendingRenameFailed'))
    refocusRename()
  } finally {
    saving.value = false
  }
}

function onSoloRenameBlur(session: SoloSession) {
  const key = soloRowKey(session)
  window.setTimeout(() => {
    if (saving.value || editingKey.value !== key) return
    if (document.activeElement === renameInput.value) return
    void commitSoloRename(session)
  }, 0)
}

function onMenuPin() {
  if (menuSolo.value) {
    const key = soloRowKey(menuSolo.value)
    if (soloPins.value.some((p) => p.itemKey === key)) unpinRow(key)
    else pinRow(key)
    closeMenu()
    return
  }
  const it = menuItem.value
  if (!it) return
  if (isRowPinned(it)) {
    for (const p of pins.value) if (pinRunId(p.itemKey) === it.runId) unpinRow(p.itemKey)
  } else pinRow(it.runId)
  closeMenu()
}

function onMenuRename() {
  if (menuSolo.value) {
    const s = menuSolo.value
    closeMenu()
    renameDraft.value = s.title || ''
    editingKey.value = soloRowKey(s)
    void nextTick(() => renameInput.value?.focus())
    return
  }
  const it = menuItem.value
  if (!it) return
  void beginRename(it)
}

function preservedListFilters(): Record<string, string> {
  const query: Record<string, string> = {}
  for (const key of ['wf', 'tag'] as const) {
    const raw = route.query[key]
    const value = Array.isArray(raw) ? raw[0] : raw
    if (typeof value === 'string' && value) query[key] = value
  }
  return query
}

function onMenuWorkflowView() {
  const it = menuItem.value
  if (!it) return
  closeMenu()
  const query: Record<string, string> = { ...preservedListFilters(), view: 'canvas' }
  const path = runDetailPath(it.runId)
  const location = { path, query }
  if (route.path === path) void router.replace(location)
  else void router.push(location)
  onNavigate()
}

function pushConversation(runId: string, nodeId?: string, extra?: Record<string, string>) {
  const query = { ...preservedListFilters(), ...(extra ?? {}) }
  const path = nodeConversationPath(runId, nodeId)
  void router.push(Object.keys(query).length ? { path, query } : { path })
}

function onMenuShare() {
  const it = menuItem.value
  if (!it || !isShareableInboxItem(it) || shareBlockReason(it)) return
  closeMenu()
  pushConversation(it.runId, it.nodeId, { [INBOX_SHARE_INTENT_QUERY]: INBOX_SHARE_INTENT_VALUE })
  onNavigate()
}

const cancelTarget = ref<InboxItem | null>(null)
const soloCloseTarget = ref<SoloSession | null>(null)
const cancellingRun = ref(false)
const cancelRunError = ref('')

function onMenuCancel() {
  if (menuSolo.value) {
    const s = menuSolo.value
    if (soloStore.closing.value.has(s.id) || changingSoloEnvironments.value.has(s.id)) return
    closeMenu()
    soloCloseTarget.value = s
    return
  }
  const it = menuItem.value
  if (!it || cancellingRun.value) return
  closeMenu()
  cancelRunError.value = ''
  cancelTarget.value = it
}

function closeSoloConfirm() {
  soloCloseTarget.value = null
}

async function confirmCloseSolo() {
  const target = soloCloseTarget.value
  if (!target || soloStore.closing.value.has(target.id)) return
  soloCloseTarget.value = null
  const notice = toast.showSticky(t('pages.solo.close.pending', { title: soloRowTitle(target) }))
  try {
    await soloStore.close(target.id)
    toast.success(t('pages.solo.close.success', { title: soloRowTitle(target) }))
  } catch (e) {
    toast.error(soloErrorText(e, t, te, locale.value) || t('pages.solo.close.failed'))
  } finally {
    toast.dismiss(notice)
  }
}

function closeCancelConfirm() {
  if (cancellingRun.value) return
  cancelTarget.value = null
  cancelRunError.value = ''
}

function mapCancelRunError(e: unknown): string {
  const status = (e as { status?: number })?.status
  const msg = e instanceof Error ? e.message : String(e || '')
  if (status === 404 || /not found/i.test(msg)) return t('pages.runDetail.cancelErrorNotFound')
  if (status === 400 || /already finished|cannot cancel/i.test(msg)) {
    return t('pages.runDetail.cancelErrorNotCancellable')
  }
  return msg || t('pages.runDetail.cancelErrorGeneric')
}

/** The main area is this run's node conversation, not another page or another run. */
function isCurrentInboxConversation(it: InboxItem): boolean {
  if (!isNodeConversationPath(route.path)) return false
  const runId = nodeConversationRunId(route.path)
  if (runId) return runId === it.runId
  return !!activeInboxKey.value?.startsWith(`${it.runId}:`)
}

async function confirmCancelRun() {
  const target = cancelTarget.value
  if (!target || cancellingRun.value) return
  cancellingRun.value = true
  cancelRunError.value = ''
  const leavingInbox = isCurrentInboxConversation(target)
  // Hold the open conversation for the whole request, so a list refresh that
  // lands before the response cannot paint the empty inbox or open the next row.
  if (leavingInbox) beginInboxCancelDeparture(target.runId)
  let cancelled = false
  try {
    await api.cancelRun(target.runId)
    cancelled = true
  } catch (e) {
    cancelRunError.value = mapCancelRunError(e)
  }
  if (!cancelled) {
    if (leavingInbox) endInboxCancelDeparture(target.runId)
    cancellingRun.value = false
    return
  }
  cancelTarget.value = null
  toast.success(t('pages.runDetail.cancelSuccess'))
  // Leave the conversation before the running-list refresh settles. A later refresh must
  // not pull the new-request page back onto the empty inbox, and must not open
  // another running row in its place.
  if (leavingInbox) {
    try {
      await router.push({ path: '/dashboard' })
    } catch {
      /* navigation aborted; the hold ends after the list refresh */
    }
  }
  try {
    await loadRunningRuns()
  } finally {
    if (leavingInbox) endInboxCancelDeparture(target.runId)
    cancellingRun.value = false
  }
}

watch(pendingRows, (rows) => {
  if (menu.value?.kind === 'run' && !rows.some((it) => it.runId === menu.value?.key)) closeMenu()
  if (!editingKey.value) return
  if (editingKey.value.startsWith('solo:')) return
  if (rows.some((it) => it.runId === editingKey.value)) return
  editEpoch.value++
  editingKey.value = null
  renameDraft.value = ''
  saving.value = false
})

watch(soloRows, (rows) => {
  if (menu.value?.kind === 'solo' && !soloStore.sessions.value.some((s) => soloRowKey(s) === menu.value?.key)) closeMenu()
  if (!editingKey.value?.startsWith('solo:')) return
  if (rows.some((s) => soloRowKey(s) === editingKey.value)) return
  editingKey.value = null
  renameDraft.value = ''
  saving.value = false
})

function rowMotion(it: InboxItem): 'spin' | 'pulse' | null {
  if (!isRunOnlyRow(it)) return null
  const run = runningById.value.get(it.runId)
  if (!run) return null
  const mark = recentRunMark(run)
  if (!mark) {
    const handingOff = carriedRows.value.some((row) => row.runId === it.runId)
    return run.status === 'running' || handingOff ? 'pulse' : null
  }
  return null
}

const ARC_MARK = { name: 'arc-spinner', toneClass: 'nav-row-type-icon--accent' }
const BREATH_MARK = { name: 'breath-dot', toneClass: 'nav-row-type-icon--accent' }
const HOLD_MARK = { name: 'user-hold', toneClass: 'nav-row-type-icon--hold' }
const QUEUE_MARK = { name: 'queue', toneClass: 'text-txt3' }

/**
 * One icon decision for every executing row. Inbox starting still wins. A
 * current node counts only when this row is that node (or the row has no
 * pending item); a newer waiting item is not painted with the previous node's mark.
 */
function pendingTypeMarks(it: InboxItem): Array<{ name: string; toneClass: string }> {
  const pending = !isRunOnlyRow(it)
  if (pending && startFailedKeys.value.includes(itemKey(it))) return []
  if (isStartingInboxItem(it)) return [ARC_MARK]
  const run = runningById.value.get(it.runId)
  const node = run ? recentCurrentNode(run) : null
  const sameNode = !pending || !node || node.id === it.nodeId
  const mark = run && sameNode ? recentRunMark(run) : null
  if (mark?.icon.name === 'arc-spinner') return [mark.icon]
  if (isReplyingInboxItem(it) || mark?.icon.name === 'breath-dot') return [BREATH_MARK]
  if (run?.status === 'queued') return [QUEUE_MARK]
  if (mark || run?.status === 'waiting_human' || pending) return [HOLD_MARK]
  return []
}

function isPendingActive(it: InboxItem): boolean {
  if (isNodeConversationPath(route.path) && activeInboxKey.value?.startsWith(`${it.runId}:`)) return true
  const run = runningById.value.get(it.runId)
  return isRunOnlyRow(it) && !!run && recentRunActive(run, route)
}

function onPendingRow(it: InboxItem) {
  if (editingKey.value) return
  if (suppressPendingClick.value) {
    suppressPendingClick.value = false
    return
  }
  // An automatic row always opens the live conversation, including a visit that has
  // not started and a confirmed handoff the snapshot has not caught up to.
  // A row waiting on a person still opens that node's own chat. Right-click stays on the menu.
  const auto = rowGroup(it.runId) === 'auto'
  if (auto || isRunOnlyRow(it)) {
    const run = runningById.value.get(it.runId)
    if (run) void router.push(recentRunTarget(run, preservedListFilters(), auto ? { awaitAuto: true } : undefined))
    else if (auto) void router.push(runLivePath(it.runId))
    else pushConversation(it.runId)
  } else {
    pushConversation(it.runId, it.nodeId)
  }
  onNavigate()
}
function pollRefresh() {
  if (!document.hidden) {
    void loadRunningRuns()
  }
  return peek({ source: 'sidebar-poll' })
}
function onDocumentVisibility() {
  if (!document.hidden) void loadRunningRuns()
}
const offRunActivity = onRecentActivity(() => void loadRunningRuns({ follow: true }))
// History already reloads when the workspace appears. Running rows used to
// skip that moment, so a refresh that mounted the sidebar before login left
// the section blank until the next poll.
watch(workspaceReady, (ready) => {
  if (ready) void loadRunningRuns()
})
onMounted(() => {
  refresh({ source: 'mount' })
  void loadRunningRuns()
  timer = window.setInterval(pollRefresh, 15000)
  chromeHasMounted.value = true
  document.addEventListener('mousedown', onDocMouseDown)
  document.addEventListener('keydown', onDocKeydown)
  document.addEventListener('visibilitychange', onDocumentVisibility)
  window.addEventListener('pointerup', onRowPressEnd)
  window.addEventListener('pointercancel', onRowPressEnd)
})
onBeforeUnmount(() => {
  navAlive = false
  runningFollowUp = false
  if (timer) clearInterval(timer)
  offRunActivity()
  runningRequest?.abort()
  clearRowPress()
  window.clearTimeout(rowClickReleaseTimer)
  window.removeEventListener('pointerup', onRowPressEnd)
  window.removeEventListener('pointercancel', onRowPressEnd)
  document.removeEventListener('mousedown', onDocMouseDown)
  document.removeEventListener('keydown', onDocKeydown)
  document.removeEventListener('visibilitychange', onDocumentVisibility)
})
// Refresh promptly when navigating (e.g. right after approving a gate).
watch(
  () => route.path,
  () => {
    refresh({ source: 'navigate' })
    void loadRunningRuns()
  },
)

function isActive(to: string) {
  if (to === '/dashboard') return isHomeSectionPath(route.path)
  if (to === '/board') return isBoardSectionPath(route.path)
  return route.path === to || route.path.startsWith(to + '/')
}

const { settingsChrome, requestMode } = useSidebarNavigation()

watch(settingsChrome, (on) => {
  if (!on) return
  closeMenu()
  if (editingKey.value && !saving.value) cancelEdit()
})

function prefersReducedMotion(): boolean {
  return (
    typeof window !== 'undefined' &&
    !!window.matchMedia?.('(prefers-reduced-motion: reduce)')?.matches
  )
}

function releaseHandoffIfHuman(runId: string, group: SidebarRunGroup) {
  if (group !== 'running') return
  const mark = readSidebarHandoff(runId)
  if (!mark) return
  const node = runningById.value.get(runId)?.currentNode
  // Confirmed automatic: only a snapshot that has landed on a person ends it.
  // Still on the old node, missing, or not yet running must not clear the claim.
  if (mark.confirmedAuto) {
    if (!sidebarLandedOnHuman(node, mark.fromNodeId)) return
    clearSidebarHandoff(runId)
    return
  }
  if (!node?.id || node.id === mark.fromNodeId) return
  if (node.kind === 'agent' && node.mode === 'auto') return
  if (isAutoProcessNode(node)) return
  clearSidebarHandoff(runId)
}

function settleRunGroups() {
  const listed = new Set(pendingRows.value.map((it) => it.runId))
  for (const runId of sidebarTrackedRunIds()) {
    if (listed.has(runId)) continue
    const mark = readSidebarHandoff(runId)
    // A row can vanish for one poll while the run is between the inbox item
    // and the unfinished list. That gap is not the end of a confirmed automatic handoff.
    if (mark?.confirmedAuto && !isSidebarRunDismissed(runId)) continue
    clearSidebarHandoff(runId)
  }
  const next = new Map<string, SidebarRunGroup>()
  for (const it of orderedWorkflowRows.value) {
    const group = rowGroups.value.get(it.runId)
    if (isWorkflowGroup(group)) next.set(it.runId, group)
  }
  for (const [runId, group] of next) {
    releaseHandoffIfHuman(runId, group)
    const mark = readSidebarHandoff(runId)
    // Keep the claim until the list itself shows the automatic node, so the row does not jump back.
    if (group === 'auto' && mark && isAutoProcessNode(runningById.value.get(runId)?.currentNode)) consumeSidebarHandoff(runId)
  }
  paintedGroups = next
}

const placementKey = computed(() => {
  void sidebarHandoffEpoch.value
  const workflow = orderedWorkflowRows.value.map((it) => `${it.runId}\t${rowGroups.value.get(it.runId)}\t${autoNodeName(it)}`).join('\n')
  const queued = orderedQueuedRows.value.map((it) => it.runId).join('\n')
  return `${workflow}\u0000${queued}`
})
watch(placementKey, () => { settleRunGroups() }, { flush: 'post', immediate: true })

/** First paint (direct /agents etc.) must not play the push animation. */
const chromeHasMounted = ref(false)
const chromeSlideName = ref('chrome-none')

watch(settingsChrome, (now) => {
  if (!chromeHasMounted.value || prefersReducedMotion()) {
    chromeSlideName.value = 'chrome-none'
    return
  }
  chromeSlideName.value = now ? 'chrome-slide-left' : 'chrome-slide-right'
})

const chromeUseCss = computed(() => chromeSlideName.value !== 'chrome-none')

function isSettingsItemActive(item: SettingsNavItem) {
  return settingsItemActive(item, route.path, route.query)
}

function settingsLinkTo(item: SettingsNavItem) {
  if (item.query) return { path: item.to, query: item.query }
  return item.to
}

function onNavigate() {
  emit('navigate')
}

const primaryGroup = sidebarNavGroups[0]
const primaryItems = computed(() => props.rail
  ? primaryGroup?.items.filter((item) => item.to === '/dashboard') ?? []
  : primaryGroup?.items ?? [])
const { worktreeMode } = useExecutionMode()
const settingsItems = computed(() => (
  worktreeMode.value ? settingsNavItems.filter((item) => item.to !== '/sandboxes') : settingsNavItems
))
</script>

<template>
  <nav
    ref="navRoot"
    class="sidebar-workspace-nav scroll-area min-h-0 flex-1 overflow-y-auto px-3 py-2"
    data-testid="app-sidebar-nav"
    :data-nav-mode="settingsChrome ? 'settings' : 'workspace'"
  >
    <div class="nav-chrome-viewport">
    <Transition :name="chromeSlideName" :css="chromeUseCss">
    <!-- Settings chrome: back to home + category list (plan g2.2) -->
    <div v-if="settingsChrome" key="settings" class="nav-chrome-pane mb-3 settings-directory" data-testid="nav-settings-chrome">
      <RouterLink
        v-hover-ink
        to="/dashboard"
        class="nav-item settings-back-link"
        data-testid="nav-back-home"
        @click.capture="requestMode($event, '/dashboard', 'workspace')"
        @click="onNavigate"
      >
        <Icon name="arrow-left" :size="16" />
        <span class="flex-1">{{ t('nav.backWorkspace') }}</span>
      </RouterLink>
      <template v-for="(item, index) in settingsItems" :key="settingsItemKey(item, index)">
        <div
          v-if="item.groupKey"
          class="settings-group-heading"
          data-testid="settings-nav-group"
        >
          {{ t(item.groupKey) }}
        </div>
        <RouterLink
          v-hover-ink
          :to="settingsLinkTo(item)"
          class="nav-item mb-0.5"
          :class="{ active: isSettingsItemActive(item) }"
          :aria-current="isSettingsItemActive(item) ? 'page' : undefined"
          :data-testid="item.query?.integrations ? 'nav-settings-integrations' : undefined"
          @click.capture="requestMode($event, settingsLinkTo(item), 'settings')"
          @click="onNavigate"
        >
          <Icon :name="item.icon" :size="17" />
          <span class="flex-1">{{ t(item.labelKey) }}</span>
          <span
            v-if="badgeFor(item.to)"
            class="force-radius-full flex h-5 min-w-5 items-center justify-center rounded-full bg-accent px-1.5 text-[11px] font-semibold text-white"
            :data-testid="item.to === '/notifications' ? 'nav-notifications-badge' : undefined"
          >{{ badgeFor(item.to) }}</span>
        </RouterLink>
      </template>
    </div>

    <!-- Workspace primary nav. Starred workflows are not listed here. -->
    <div v-else-if="primaryGroup" key="workspace" class="nav-chrome-pane">
    <div class="mb-3" data-testid="nav-workspace-chrome">
      <RouterLink
        v-for="item in primaryItems"
        :key="item.to"
        v-hover-ink
        :to="item.to"
        class="nav-item mb-0.5"
        :class="{ active: isActive(item.to), 'nav-create-action': item.to === '/dashboard' }"
        @click.capture="requestMode($event, item.to, item.to === '/settings' ? 'settings' : 'workspace')"
        @click="onNavigate"
      >
        <Icon :name="item.to === '/dashboard' ? 'new-request' : item.icon" :size="17" />
        <span class="flex-1">{{ t(item.labelKey) }}</span>
        <span
          v-if="badgeFor(item.to)"
          class="force-radius-full flex h-5 min-w-5 items-center justify-center rounded-full bg-accent px-1.5 text-[11px] font-semibold text-white"
        >{{ badgeFor(item.to) }}</span>
      </RouterLink>
    </div>

    <div v-for="section in runSections" :key="section.id" class="sidebar-run-section mb-3" :data-testid="section.testId">
      <SidebarSectionHeading
        :icon="section.icon"
        :label="t(section.labelKey)"
        :collapsed="section.collapsed"
        :count="section.count"
        :toggle-test-id="section.toggleTestId"
        :data-testid="section.headingTestId"
        @toggle="toggleSection(section.id)"
      />
      <div v-show="!section.collapsed" class="sidebar-tree" :data-testid="section.rowsTestId">
      <div v-for="entry in section.entries" :key="entry.key" class="sidebar-tree-item">
        <template v-if="entry.kind === 'run'">
        <div
          v-if="editingKey === entry.row.runId"
          class="nav-item grasp-status-host mb-0.5 w-full min-w-0 cursor-text text-start"
          :class="{ active: isPendingActive(entry.row) }"
          data-testid="nav-pending-row"
          :data-key="entry.row.runId"
          :data-node-id="entry.row.nodeId"
          :data-motion="rowMotion(entry.row) || undefined"
          data-editing="true"
          :aria-current="isPendingActive(entry.row) ? 'true' : undefined"
          @click.stop
          @contextmenu.prevent
        >
          <span
            v-if="isRowPinned(entry.row)"
            class="flex shrink-0 text-accent"
            data-testid="nav-pending-pin"
            aria-hidden="true"
          >
            <Icon name="pin" :size="12" />
          </span>
          <input
            :ref="setRenameInput"
            v-model="renameDraft"
            type="text"
            class="h-[26px] min-w-0 flex-1 rounded-md border border-accent bg-base px-1.5 text-start text-sm font-normal text-txt outline-none"
            data-testid="nav-pending-rename-input"
            :aria-label="t('nav.pendingRenameLabel')"
            :readonly="saving"
            @click.stop
            @keydown.enter.prevent="commitRename(entry.row)"
            @keydown.esc.prevent.stop="onRenameEscape"
            @blur="onRenameBlur(entry.row)"
          />
          <InboxMotionMark v-if="rowMotion(entry.row) === 'spin'" kind="spin" />
          <InboxMotionMark v-else-if="rowMotion(entry.row) === 'pulse'" kind="pulse" />
          <template v-else>
            <Icon
              v-for="mark in pendingTypeMarks(entry.row)"
              :key="mark.name"
              :name="mark.name"
              :busy="mark.name === 'arc-spinner' || mark.name === 'breath-dot'"
              :breath-ping="mark.name === 'breath-dot'"
              :size="16"
              class="nav-row-type-icon"
              :class="mark.toneClass"
              data-testid="nav-pending-type-icon"
              aria-hidden="true"
            />
          </template>
        </div>
        <button
          v-else
          v-hover-ink
          type="button"
          class="nav-item grasp-status-host mb-0.5 w-full min-w-0 text-start"
          :class="{ active: isPendingActive(entry.row) }"
          data-testid="nav-pending-row"
          :data-key="entry.row.runId"
          :data-node-id="entry.row.nodeId"
          :data-group="rowGroups.get(entry.row.runId)"
          :data-motion="rowMotion(entry.row) || undefined"
          :title="pendingTooltip(entry.row)"
          :aria-current="isPendingActive(entry.row) ? 'true' : undefined"
          @click="onPendingRow(entry.row)"
          @contextmenu="onPendingContextMenu($event, entry.row)"
          @pointerdown="onPendingPointerDown($event, entry.row)"
          @keydown.f2.prevent="beginRename(entry.row)"
        >
          <span
            v-if="isRowPinned(entry.row)"
            class="flex shrink-0 text-accent"
            data-testid="nav-pending-pin"
            aria-hidden="true"
          >
            <Icon name="pin" :size="12" />
          </span>
          <span v-if="rowTitle(entry.row)" class="nav-row-title min-w-0 flex-1 truncate text-start">{{ rowTitle(entry.row) }}</span>
          <span v-else class="nav-row-title-skeleton min-w-0 flex-1" data-testid="nav-pending-title-skeleton" aria-hidden="true"><span /></span>
          <span v-if="autoNodeName(entry.row)" class="nav-auto-node-name min-w-0 truncate" data-testid="nav-auto-node-name">{{ autoNodeName(entry.row) }}</span>
          <InboxMotionMark v-if="rowMotion(entry.row) === 'spin'" kind="spin" />
          <InboxMotionMark v-else-if="rowMotion(entry.row) === 'pulse'" kind="pulse" />
          <template v-else>
            <Icon
              v-for="mark in pendingTypeMarks(entry.row)"
              :key="mark.name"
              :name="mark.name"
              :busy="mark.name === 'arc-spinner' || mark.name === 'breath-dot'"
              :breath-ping="mark.name === 'breath-dot'"
              :size="16"
              class="nav-row-type-icon"
              :class="mark.toneClass"
              data-testid="nav-pending-type-icon"
              aria-hidden="true"
            />
          </template>
        </button>
        </template>
        <template v-else>
      <div
        v-if="editingKey === soloRowKey(entry.session)"
        class="nav-item grasp-status-host mb-0.5 w-full min-w-0 cursor-text text-start"
        :class="{ active: isSoloRowActive(entry.session) }"
        data-testid="nav-pending-row"
        :data-key="entry.session.id"
        data-kind="solo"
        data-editing="true"
        @click.stop
        @contextmenu.prevent
      >
        <span v-if="soloPins.some((p) => p.itemKey === soloRowKey(entry.session))" class="flex shrink-0 text-accent" data-testid="nav-pending-pin" aria-hidden="true"><Icon name="pin" :size="12" /></span>
        <input
          :ref="setRenameInput"
          v-model="renameDraft"
          type="text"
          class="h-[26px] min-w-0 flex-1 rounded-md border border-accent bg-base px-1.5 text-start text-sm font-normal text-txt outline-none"
          data-testid="nav-pending-rename-input"
          :aria-label="t('nav.pendingRenameLabel')"
          :readonly="saving"
          @click.stop
          @keydown.enter.prevent="commitSoloRename(entry.session)"
          @keydown.esc.prevent.stop="onRenameEscape"
          @blur="onSoloRenameBlur(entry.session)"
        />
        <InboxMotionMark v-if="soloRowMotion(entry.session) === 'spin'" kind="spin" />
        <InboxMotionMark v-else-if="soloRowMotion(entry.session) === 'pulse'" kind="pulse" />
      </div>
      <button
        v-else
        v-hover-ink
        type="button"
        class="nav-item grasp-status-host mb-0.5 w-full min-w-0 text-start"
        :class="{ active: isSoloRowActive(entry.session) }"
        data-testid="nav-pending-row"
        :data-key="entry.session.id"
        data-kind="solo"
        :data-status="entry.session.status"
        :data-motion="soloRowMotion(entry.session) || undefined"
        :title="soloRowTitle(entry.session)"
        :aria-current="isSoloRowActive(entry.session) ? 'page' : undefined"
        @click="onSoloRow(entry.session)"
        @contextmenu="onSoloContextMenu($event, entry.session)"
        @keydown="onSoloMenuKeydown($event, entry.session)"
        @pointerdown="onSoloPointerDown($event, entry.session)"
      >
        <span v-if="soloPins.some((p) => p.itemKey === soloRowKey(entry.session))" class="flex shrink-0 text-accent" data-testid="nav-pending-pin" aria-hidden="true"><Icon name="pin" :size="12" /></span>
        <span class="nav-row-title min-w-0 flex-1 truncate text-start">{{ soloRowTitle(entry.session) }}</span>
        <span v-if="soloStore.closing.value.has(entry.session.id)" class="shrink-0 text-[11px] text-txt3" data-testid="nav-solo-closing">{{ t('pages.solo.close.closing') }}</span>
        <InboxMotionMark v-if="soloRowMotion(entry.session) === 'spin'" kind="spin" />
        <InboxMotionMark v-else-if="soloRowMotion(entry.session) === 'pulse'" kind="pulse" />
      </button>
        </template>
      </div>
      </div>
    </div>
    <SidebarWorkspaceLists v-if="workspaceLists" :run-titles="pendingRunTitles" @navigate="onNavigate" @solo-contextmenu="onSoloContextMenu" @solo-menu-keydown="onSoloMenuKeydown" @solo-pointerdown="onSoloPointerDown" @solo-click="onSoloHistoryClick" />
    </div>
    </Transition>
    </div>

  </nav>
  <Teleport to="body">
    <div
      v-if="menu && (menuItem || menuSolo)"
      ref="menuEl"
      class="pending-row-menu fixed z-[9999] min-w-[148px] rounded-lg border border-line bg-elevated py-1 shadow-card"
      role="menu"
      data-testid="nav-pending-menu"
      :style="{ left: `${menu.x}px`, top: `${menu.y}px` }"
      @mousedown.stop
      @click.stop
      @contextmenu.prevent
    >
      <button
        v-if="menuItem"
        type="button"
        role="menuitem"
        class="pending-row-menu__item"
        data-testid="nav-pending-menu-workflow"
        @click="onMenuWorkflowView"
      >
        <Icon name="workflow" :size="14" />
        <span>{{ t('nav.pendingWorkflowView') }}</span>
      </button>
      <button
        v-if="menuItem || menuSoloActive"
        type="button"
        role="menuitem"
        class="pending-row-menu__item"
        data-testid="nav-pending-menu-pin"
        @click="onMenuPin"
      >
        <Icon :name="menuPinned ? 'close' : 'pin'" :size="14" />
        <span>{{ menuPinned ? t('nav.pendingUnpin') : t('nav.pendingPin') }}</span>
      </button>
      <button
        v-if="menuItem || menuSoloActive"
        type="button"
        role="menuitem"
        class="pending-row-menu__item"
        data-testid="nav-pending-menu-rename"
        @click="onMenuRename"
      >
        <Icon name="edit" :size="14" />
        <span>{{ t('nav.pendingRename') }}</span>
      </button>
      <template v-if="menuSolo && !worktreeMode">
        <div v-if="menuSoloActive" class="pending-row-menu__separator" role="separator" />
        <button type="button" role="menuitem" class="pending-row-menu__item" data-testid="nav-solo-menu-snapshots" @click="onMenuSoloWorkspace('snapshots')">
          <Icon name="history" :size="14" />
          <span>{{ t('workspaceSnapshots.title') }}</span>
        </button>
        <button v-if="menuSoloActive" type="button" role="menuitem" class="pending-row-menu__item" data-testid="nav-solo-menu-environment" :disabled="menuSoloEnvironmentDisabled" @click="onMenuSoloEnvironment">
          <Icon :name="menuSolo.status === 'stopped' ? 'play' : 'stop'" :size="14" />
          <span>{{ menuSoloEnvironmentLabel }}</span>
        </button>
        <button v-else-if="menuSoloRelease" type="button" role="menuitem" class="pending-row-menu__item" data-testid="nav-solo-menu-release" :disabled="menuSoloClosing" @click="onMenuSoloWorkspace('release')">
          <Icon name="trash" :size="14" />
          <span>{{ t('workspaceRelease.title') }}</span>
        </button>
        <div v-if="menuSoloActive" class="pending-row-menu__separator" role="separator" />
      </template>
      <button
        v-if="menuShareVisible && menuItem"
        type="button"
        role="menuitem"
        class="pending-row-menu__item"
        data-testid="nav-pending-menu-share"
        :data-reason="menuShareReason || undefined"
        :disabled="menuShareDisabled"
        :title="menuShareLabel"
        :aria-label="menuShareLabel"
        @click="onMenuShare"
      >
        <Icon name="copy" :size="14" />
        <span>{{ menuShareLabel }}</span>
      </button>
      <button
        v-if="menuItem || menuSoloActive"
        type="button"
        role="menuitem"
        class="pending-row-menu__item"
        data-testid="nav-pending-menu-cancel"
        :disabled="!!menuSolo && (menuSoloClosing || changingSoloEnvironments.has(menuSolo.id))"
        @click="onMenuCancel"
      >
        <Icon name="close" :size="14" />
        <span>{{ t(menuSoloClosing ? 'pages.solo.close.closing' : 'nav.pendingCancelRun') }}</span>
      </button>
    </div>
  </Teleport>
  <AppModal
    :open="!!cancelTarget"
    :title="t('pages.runDetail.cancelTitle')"
    :width="440"
    @close="closeCancelConfirm"
  >
    <div class="space-y-3 text-sm text-txt2">
      <div class="flex items-start gap-2 rounded-md border border-err/30 bg-err/10 px-3 py-2 text-[12px] text-err">
        <Icon name="alert" :size="14" class="mt-0.5 shrink-0" />
        {{ t('pages.runDetail.cancelWarning') }}
      </div>
      <p>{{ t('nav.pendingCancelConfirm') }}</p>
      <div
        v-if="cancelRunError"
        class="flex items-start gap-2 rounded-md border border-err/30 bg-err/10 px-3 py-2 text-[12px] text-err"
        data-testid="nav-pending-cancel-error"
      >
        <Icon name="alert" :size="14" class="mt-0.5" />{{ cancelRunError }}
      </div>
    </div>
    <template #footer>
      <AppButton
        variant="ghost"
        data-testid="nav-pending-cancel-dismiss"
        :disabled="cancellingRun"
        @click="closeCancelConfirm"
      >
        {{ t('common.buttons.cancel') }}
      </AppButton>
      <AppButton
        data-testid="nav-pending-cancel-confirm"
        variant="danger"
        icon="close"
        :disabled="cancellingRun"
        @click="confirmCancelRun"
      >
        {{ cancellingRun ? t('common.buttons.cancelling') : t('common.buttons.confirmCancelRun') }}
      </AppButton>
    </template>
  </AppModal>
  <AppModal
    :open="!!soloCloseTarget"
    :title="t('pages.solo.close.title')"
    :width="440"
    :close-label="t('common.buttons.close')"
    close-on-esc
    trap-focus
    @close="closeSoloConfirm"
  >
    <div class="space-y-3 text-sm text-txt2">
      <p>{{ t('pages.solo.close.body') }}</p>
      <p v-if="soloCloseTarget?.busy" class="text-[12px] text-warn">{{ t('pages.solo.close.busy') }}</p>
    </div>
    <template #footer>
      <AppButton variant="ghost" @click="closeSoloConfirm">{{ t('common.buttons.cancel') }}</AppButton>
      <AppButton variant="danger" icon="stop" data-testid="nav-solo-close-confirm" @click="confirmCloseSolo">{{ t('pages.solo.close.confirm') }}</AppButton>
    </template>
  </AppModal>
</template>

<style scoped>
.sidebar-workspace-nav {
  padding: 8px 12px 16px;
}
.sidebar-workspace-nav .nav-item {
  min-height: 36px;
  padding: 8px 10px;
  font-size: 13px;
  color: light-dark(#27272a, rgb(var(--c-txt2)));
  --hover-ink-color: rgb(var(--c-txt) / .04);
}
/* :where keeps specificity equal to the .settings-directory rules below so they still win */
.sidebar-workspace-nav .nav-item:where(:hover, :focus-visible, .active) {
  color: rgb(var(--c-txt));
}
.sidebar-workspace-nav .sidebar-tree-item > .nav-item {
  padding-left: 6px;
}
.sidebar-workspace-nav .nav-item.active {
  background: light-dark(#f1f1f1, rgb(var(--c-txt) / .065));
}
.sidebar-workspace-nav[data-nav-mode="settings"] {
  padding-top: 0;
}
.settings-directory .settings-group-heading {
  margin-top: 20px;
  margin-bottom: 6px;
  padding: 0 10px;
  color: rgb(var(--c-txt3));
  font-size: 11px;
  font-weight: 500;
  line-height: 18px;
}
.settings-directory .nav-item {
  min-height: 34px;
  padding: 7px 10px;
  gap: 10px;
  color: rgb(var(--c-txt));
  font-size: 13px;
  font-weight: 400;
  line-height: 20px;
  --hover-ink-color: rgb(var(--c-txt) / .045);
}
.settings-directory .nav-item.active {
  color: rgb(var(--c-txt));
  background: light-dark(#f1f1f1, rgb(var(--c-txt) / .065));
  font-weight: 500;
}
.settings-directory .settings-back-link {
  margin-bottom: 12px;
  color: rgb(var(--c-txt3));
}
.settings-directory .nav-item :deep(svg) {
  flex-shrink: 0;
  color: currentColor;
  stroke-width: 1.65;
}
@media (max-width: 767px) {
  .settings-directory .nav-item { min-height: 44px; }
  .settings-directory .settings-group-heading { margin-top: 18px; }
}
.sidebar-workspace-nav .nav-create-action {
  height: 40px;
  margin-bottom: 8px;
  gap: 8px;
  padding: 0 10px;
  border: 0;
  color: rgb(var(--c-txt));
  background: transparent;
  box-shadow: none;
  font-weight: 500;
  --hover-ink-color: rgb(var(--c-txt) / .045);
}
.sidebar-workspace-nav .nav-create-action.active {
  background: transparent;
  font-weight: 600;
}
.nav-create-action > svg {
  color: rgb(var(--c-txt2));
}
.sidebar-workspace-nav [data-testid="nav-workspace-chrome"] {
  margin-bottom: 16px;
}
.sidebar-workspace-nav .sidebar-run-section {
  margin-bottom: 24px;
}
/* Buttons default to centered text. Pending titles start at the row edge.
   Weight stays 400 so the active nav rule (600) does not bold this title. */
.nav-row-title {
  text-align: start;
  font-weight: 400;
}
.nav-auto-node-name {
  flex: none;
  max-width: 42%;
  color: rgb(var(--c-txt3));
  font-size: 11px;
  font-weight: 400;
}
.nav-row-title-skeleton {
  display: flex;
  align-items: center;
}
.nav-row-title-skeleton > span {
  display: block;
  width: 60%;
  height: 8px;
  border-radius: 9999px;
  background: rgb(var(--c-txt) / 0.08);
}

.nav-row-type-icon {
  flex: none;
  color: light-dark(#7b718c, #b7abc9);
  stroke-width: 1.6;
}
.nav-row-type-icon--approval { color: light-dark(#947a49, #c8b185); }
.nav-row-type-icon--accent { color: rgb(var(--c-accent-2)); }
.nav-row-type-icon--hold { color: rgb(var(--c-warn)); }

/* Row-tail breath only. The extra nav-row-type-icon class beats the shared
   busy rules in icon-motion.css. The hover ring is its own circle, so a
   pointer or keyboard focus does not restart or pause the 1.6s loop.
   Peak scale 3.2 keeps a 3.4-radius stroke inside the 24 viewBox; non-scaling
   stroke stays a hairline so the row's overflow:clip never cuts a half ring. */
.nav-row-type-icon[data-icon="breath-dot"][data-icon-motion="busy"] :deep(.grasp-heart-core) {
  transform-origin: 12px 12px;
  animation: navRowBreathCore 1.6s ease-in-out infinite;
}
.nav-row-type-icon[data-icon="breath-dot"][data-icon-motion="busy"] :deep(.grasp-heart-ring) {
  transform-origin: 12px 12px;
  vector-effect: non-scaling-stroke;
  animation: navRowBreathRing 1.6s ease-out infinite;
}
.nav-row-type-icon[data-icon="breath-dot"] :deep(.nav-breath-ping) {
  transform-origin: 12px 12px;
  vector-effect: non-scaling-stroke;
  opacity: 0;
  pointer-events: none;
}
.nav-item:hover > .nav-row-type-icon[data-icon="breath-dot"] :deep(.nav-breath-ping),
.nav-item:focus-visible > .nav-row-type-icon[data-icon="breath-dot"] :deep(.nav-breath-ping) {
  animation: navRowBreathPing 700ms ease-out 1;
}
@keyframes navRowBreathCore {
  0%, 16%, 100% { transform: scale(1); }
  7% { transform: scale(0.86); }
}
@keyframes navRowBreathRing {
  0% { transform: scale(0.75); opacity: 0; }
  12% { opacity: 0.62; }
  46%, 100% { transform: scale(3.2); opacity: 0; }
}
@keyframes navRowBreathPing {
  0% { transform: scale(0.7); opacity: 0; }
  20% { opacity: 0.95; }
  100% { transform: scale(3.2); opacity: 0; }
}
@media (prefers-reduced-motion: reduce) {
  .nav-row-type-icon[data-icon="breath-dot"][data-icon-motion="busy"] :deep(.grasp-heart-core),
  .nav-row-type-icon[data-icon="breath-dot"][data-icon-motion="busy"] :deep(.grasp-heart-ring),
  .nav-row-type-icon[data-icon="breath-dot"] :deep(.nav-breath-ping),
  .nav-item:hover > .nav-row-type-icon[data-icon="breath-dot"] :deep(.nav-breath-ping),
  .nav-item:focus-visible > .nav-row-type-icon[data-icon="breath-dot"] :deep(.nav-breath-ping) {
    animation: none !important;
  }
}

.pending-row-menu__item {
  display: flex;
  width: 100%;
  align-items: center;
  gap: 8px;
  border: 0;
  background: transparent;
  padding: 6px 12px;
  color: rgb(var(--c-txt2));
  font-size: 12px;
  text-align: start;
  white-space: nowrap;
  cursor: pointer;
}

.pending-row-menu__separator { height: 1px; margin: 4px 8px; background: rgb(var(--c-line)); }

.pending-row-menu__item:hover,
.pending-row-menu__item:focus-visible {
  color: rgb(var(--c-txt));
  background: rgb(var(--c-overlay));
  outline: none;
}

.pending-row-menu__item:disabled {
  cursor: not-allowed;
  opacity: 0.45;
}

.pending-row-menu__item:disabled:hover,
.pending-row-menu__item:disabled:focus-visible {
  color: rgb(var(--c-txt2));
  background: transparent;
}

.nav-chrome-viewport {
  position: relative;
  overflow: hidden;
}

.nav-chrome-pane {
  width: 100%;
}

.chrome-slide-left-enter-active,
.chrome-slide-left-leave-active,
.chrome-slide-right-enter-active,
.chrome-slide-right-leave-active {
  transition:
    transform 320ms cubic-bezier(0.22, 1, 0.36, 1),
    opacity 320ms ease;
}

.chrome-slide-left-leave-active,
.chrome-slide-right-leave-active {
  position: absolute;
  inset-inline: 0;
  top: 0;
}

.chrome-slide-left-enter-from {
  transform: translateX(100%);
  opacity: 0.4;
}

.chrome-slide-left-leave-to {
  transform: translateX(-100%);
  opacity: 0.4;
}

.chrome-slide-right-enter-from {
  transform: translateX(-100%);
  opacity: 0.4;
}

.chrome-slide-right-leave-to {
  transform: translateX(100%);
  opacity: 0.4;
}

@media (prefers-reduced-motion: reduce) {
  .chrome-slide-left-enter-active,
  .chrome-slide-left-leave-active,
  .chrome-slide-right-enter-active,
  .chrome-slide-right-leave-active {
    transition: none;
  }
}

</style>
