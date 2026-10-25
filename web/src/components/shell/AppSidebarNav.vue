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
import { autoProcessNodeName, isAutoProcessNode, isAutoProcessRun, keepRowOrder, mergeRunRows, pinRunId, RUNNING_RUN_STATUSES } from '@/lib/inbox/sidebarRunRows'
import {
  clearSidebarHandoff,
  consumeSidebarHandoff,
  decideSidebarFlight,
  readSidebarHandoff,
  SIDEBAR_FLIGHT_MS,
  sidebarHandoffEpoch,
  type SidebarRunGroup,
} from '@/lib/inbox/sidebarHandoff'
import { onRecentActivity, recentCurrentNode, recentRunActive, recentRunMark, recentRunTarget } from '@/lib/run/recentRuns'
import { useWorkspaceContext } from '@/lib/composables/useWorkspaceContext'
import {
  settingsItemActive,
  settingsItemKey,
  settingsNavItems,
  type SettingsNavItem,
} from '@/data/settingsNav'
import { usePendingGates } from '@/lib/inbox/usePendingGates'
import { useRunTerminalNotifications } from '@/lib/run/useRunTerminalNotifications'
import { useToast } from '@/lib/composables/useToast'
import { useBreakpoint } from '@/lib/composables/useBreakpoint'
import { useSoloSessions } from '@/lib/solo/useSoloSessions'
import type { SoloSession } from '@/lib/api/apiTypes'
import SidebarWorkspaceLists from './SidebarWorkspaceLists.vue'
import { isSectionCollapsed, toggleSection } from '@/lib/shared/sidebarSections'
import { useSidebarNavigation } from '@/lib/composables/useSidebarNavigation'

const props = defineProps<{ drawer?: boolean; rail?: boolean; workspaceLists?: boolean }>()
const emit = defineEmits<{ (e: 'navigate'): void }>()

const { t } = useI18n()
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
const runningCollapsed = computed(() => isSectionCollapsed('running'))
const autoCollapsed = computed(() => isSectionCollapsed('auto'))
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

/** Unfinished runs (queued / running / waiting_human) for「正在执行」. */
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
    // Solo sessions are resident sandboxes, but active ones belong in the
    // same「正在执行」section as workflow runs.
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
function autoNodeName(it: InboxItem): string {
  return autoProcessNodeName(runningById.value.get(it.runId))
}
const humanPendingRows = computed(() => pendingRows.value.filter((it) => !isAutoProcessRun(runningById.value.get(it.runId))))
const autoPendingRows = computed(() => pendingRows.value.filter((it) => isAutoProcessRun(runningById.value.get(it.runId))))
const orderedHumanRows = computed(() => orderWithPins(humanPendingRows.value, (it) => it.runId, runPins.value))
const orderedAutoRows = computed(() => orderWithPins(autoPendingRows.value, (it) => it.runId, runPins.value))
/** Active Solo sandboxes share the same running section as workflow runs. */
const soloRows = computed(() => [...soloStore.active.value].sort((a, b) => {
  const aAt = Date.parse(a.lastActiveAt || a.updatedAt || a.createdAt)
  const bAt = Date.parse(b.lastActiveAt || b.updatedAt || b.createdAt)
  return (Number.isFinite(bAt) ? bAt : 0) - (Number.isFinite(aAt) ? aAt : 0)
}))
const orderedSoloRows = computed(() => orderWithPins(soloRows.value, soloRowKey, soloPins.value))
const runningSectionCount = computed(() => orderedHumanRows.value.length + orderedSoloRows.value.length)
const autoSectionCount = computed(() => orderedAutoRows.value.length)

type RunSectionId = 'running' | 'auto'
const runSections = computed(() => {
  const sections: Array<{
    id: RunSectionId
    testId: string
    headingTestId: string
    toggleTestId: string
    rowsTestId: string
    labelKey: string
    icon: 'run-live' | 'arrow-flow'
    count: number
    collapsed: boolean
    rows: InboxItem[]
    solos: SoloSession[]
  }> = []
  if (runningSectionCount.value > 0) {
    sections.push({
      id: 'running',
      testId: 'nav-pending-approvals',
      headingTestId: 'nav-running-heading',
      toggleTestId: 'nav-running-toggle',
      rowsTestId: 'nav-running-rows',
      labelKey: 'nav.runningRuns',
      icon: 'run-live',
      count: runningSectionCount.value,
      collapsed: runningCollapsed.value,
      rows: orderedHumanRows.value,
      solos: orderedSoloRows.value,
    })
  }
  if (autoSectionCount.value > 0) {
    sections.push({
      id: 'auto',
      testId: 'nav-auto-process',
      headingTestId: 'nav-auto-heading',
      toggleTestId: 'nav-auto-toggle',
      rowsTestId: 'nav-auto-rows',
      labelKey: 'nav.autoProcess',
      icon: 'arrow-flow',
      count: autoSectionCount.value,
      collapsed: autoCollapsed.value,
      rows: orderedAutoRows.value,
      solos: [],
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
  if (session.status === 'starting') return 'spin'
  return session.busy ? 'pulse' : null
}

function isSoloRowActive(session: SoloSession): boolean {
  return route.path === `/solo/${encodeURIComponent(session.id)}`
}

function onSoloRow(session: SoloSession) {
  void router.push(`/solo/${encodeURIComponent(session.id)}`)
  onNavigate()
}

const LONG_PRESS_MS = 500
const LONG_PRESS_MOVE_PX = 8
const MENU_WIDTH = 168
/** Blocked share copy is longer than the two-item menu, so the clamp must follow it. */
const MENU_WIDTH_BLOCKED = 280
/** 8px of menu padding plus 32px per row (pin, rename, cancel, optional share). */
const MENU_CHROME = 8
const MENU_ROW_HEIGHT = 32
const MENU_MARGIN = 8

const editingKey = ref<string | null>(null)
const renameDraft = ref('')
const renameInput = ref<HTMLInputElement | null>(null)
const saving = ref(false)
const editEpoch = ref(0)
const suppressPendingClick = ref(false)
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
  return soloStore.active.value.find((it) => soloRowKey(it) === key) ?? null
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
  const rows = !isRunOnlyRow(it) && isShareableInboxItem(it) ? 4 : 3
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
  const height = MENU_CHROME + 3 * MENU_ROW_HEIGHT
  const left = Math.max(MENU_MARGIN, Math.min(x, window.innerWidth - width))
  const top = Math.max(MENU_MARGIN, Math.min(y, window.innerHeight - height))
  menu.value = { kind: 'solo', key: soloRowKey(session), x: left, y: top }
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
  window.removeEventListener('pointerup', onRowPressEnd)
  window.removeEventListener('pointercancel', onRowPressEnd)
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
}

function onPendingPointerDown(event: PointerEvent, it: InboxItem) {
  if (event.pointerType === 'mouse' || editingKey.value) return
  clearRowPress()
  const key = it.runId
  const timer = window.setTimeout(() => {
    if (!rowPress || rowPress.key !== key) return
    const x = rowPress.x
    const y = rowPress.y
    suppressPendingClick.value = true
    window.setTimeout(() => {
      suppressPendingClick.value = false
    }, 400)
    clearRowPress()
    const row = pendingRows.value.find((item) => item.runId === key)
    if (!row || editingKey.value) return
    openMenu(row, x, y)
  }, LONG_PRESS_MS)
  rowPress = { key, x: event.clientX, y: event.clientY, timer }
  window.addEventListener('pointermove', onRowPressMove)
  window.addEventListener('pointerup', onRowPressEnd)
  window.addEventListener('pointercancel', onRowPressEnd)
}

function onPendingContextMenu(event: MouseEvent, it: InboxItem) {
  event.preventDefault()
  event.stopPropagation()
  if (editingKey.value) return
  clearRowPress()
  openMenu(it, event.clientX, event.clientY)
}

function onSoloContextMenu(event: MouseEvent, session: SoloSession) {
  event.preventDefault()
  event.stopPropagation()
  if (editingKey.value) return
  clearRowPress()
  openSoloMenu(session, event.clientX, event.clientY)
}

function onDocMouseDown(event: MouseEvent) {
  if (!menu.value) return
  const target = event.target
  if (target instanceof Node && menuEl.value?.contains(target)) return
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
  if (cancellingRun.value) return
  soloCloseTarget.value = null
}

async function confirmCloseSolo() {
  const target = soloCloseTarget.value
  if (!target || cancellingRun.value) return
  cancellingRun.value = true
  try {
    const saved = await api.closeSoloSession(target.id)
    soloStore.upsert(saved)
    soloCloseTarget.value = null
    toast.success(t('pages.solo.header.close'))
  } catch (e) {
    toast.error(e instanceof Error ? e.message : t('nav.pendingRenameFailed'))
  } finally {
    cancellingRun.value = false
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
  if (menu.value?.kind === 'solo' && !rows.some((s) => soloRowKey(s) === menu.value?.key)) closeMenu()
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
  if (isRunOnlyRow(it)) {
    const run = runningById.value.get(it.runId)
    if (run) {
      void router.push(recentRunTarget(run, preservedListFilters()))
    } else {
      pushConversation(it.runId)
    }
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
})
onBeforeUnmount(() => {
  navAlive = false
  runningFollowUp = false
  if (timer) clearInterval(timer)
  offRunActivity()
  runningRequest?.abort()
  clearRowPress()
  takeFlightMotion()
  for (const timer of flightTimers.values()) window.clearTimeout(timer)
  flightTimers.clear()
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

const rowRects = new Map<string, DOMRect>()
let paintedGroups = new Map<string, SidebarRunGroup>()
const flights = ref<Record<string, { kind: 'in' | 'back'; landing: 'row' | 'heading' }>>({})
const flightGhost = ref<{
  runId: string
  title: string
  kind: 'in' | 'back'
  landing: 'row' | 'heading'
  style: Record<string, string>
} | null>(null)
const flightGhostEl = ref<HTMLElement | null>(null)
const flightTimers = new Map<string, number>()
/** Bumped to drop a move that was scheduled but not yet painted. */
let flightMotion = 0
const flightFrames: number[] = []

function takeFlightMotion() {
  flightMotion += 1
  for (const id of flightFrames) cancelAnimationFrame(id)
  flightFrames.length = 0
  return flightMotion
}

function queueFlightFrame(motion: number, cb: () => void) {
  const id = requestAnimationFrame(() => {
    const at = flightFrames.indexOf(id)
    if (at >= 0) flightFrames.splice(at, 1)
    if (motion !== flightMotion) return
    cb()
  })
  flightFrames.push(id)
}

function rowFlight(runId: string) {
  return flights.value[runId]?.kind
}
function rowFlightLanding(runId: string) {
  return flights.value[runId]?.landing
}
function rowFlightHidden(runId: string): boolean {
  return flightGhost.value?.runId === runId && flights.value[runId]?.landing === 'row'
}

function captureRowRects() {
  rowRects.clear()
  const root = navRoot.value
  if (!root) return
  for (const el of root.querySelectorAll<HTMLElement>('[data-testid="nav-pending-row"]')) {
    if (el.dataset.kind === 'solo') continue
    const key = el.dataset.key
    if (!key) continue
    const rect = el.getBoundingClientRect()
    if (rect.width > 0 && rect.height > 0) rowRects.set(key, rect)
  }
}

function clearFlightVisual(runId: string) {
  if (!(runId in flights.value) && flightGhost.value?.runId !== runId) return
  const next = { ...flights.value }
  delete next[runId]
  flights.value = next
  if (flightGhost.value?.runId === runId) flightGhost.value = null
}

function scheduleFlightClear(runId: string) {
  const prev = flightTimers.get(runId)
  if (prev) window.clearTimeout(prev)
  flightTimers.set(runId, window.setTimeout(() => {
    flightTimers.delete(runId)
    clearFlightVisual(runId)
  }, SIDEBAR_FLIGHT_MS))
}

function beginFlight(runId: string, kind: 'in' | 'back', landing: 'row' | 'heading') {
  const motion = takeFlightMotion()
  flights.value = { ...flights.value, [runId]: { kind, landing } }
  scheduleFlightClear(runId)
  const from = rowRects.get(runId)
  const root = navRoot.value
  if (!from || !root) return
  const headingId = kind === 'in' ? 'nav-auto-heading' : 'nav-running-heading'
  const target = landing === 'heading'
    ? root.querySelector<HTMLElement>(`[data-testid="${headingId}"]`)
    : root.querySelector<HTMLElement>(`[data-key="${runId}"]`)
  const to = target?.getBoundingClientRect()
  if (!to || to.width < 1 || to.height < 1) return
  const dx = to.left - from.left
  const dy = to.top - from.top
  if (landing === 'row' && Math.abs(dx) < 1 && Math.abs(dy) < 1) return
  const it = pendingRows.value.find((row) => row.runId === runId)
  // Position only. Transform stays on the element so a later render cannot
  // snap it to the destination before the origin has been painted.
  flightGhost.value = {
    runId,
    title: it ? rowTitle(it) : '',
    kind,
    landing,
    style: {
      left: `${from.left}px`,
      top: `${from.top}px`,
      width: `${from.width}px`,
      height: `${from.height}px`,
    },
  }
  void nextTick(() => {
    if (motion !== flightMotion) return
    const el = flightGhostEl.value ?? document.querySelector<HTMLElement>('[data-testid="nav-flight-ghost"]')
    if (!el || flightGhost.value?.runId !== runId) return
    // Commit the origin, then let that frame paint before writing the end
    // transform. A nextTick write still lands before the first paint, so the
    // transition has nothing to interpolate from.
    el.style.transition = 'none'
    el.style.transform = 'translate3d(0px, 0px, 0px)'
    void el.offsetHeight
    queueFlightFrame(motion, () => {
      queueFlightFrame(motion, () => {
        const live = flightGhostEl.value ?? document.querySelector<HTMLElement>('[data-testid="nav-flight-ghost"]')
        if (!live || flightGhost.value?.runId !== runId || motion !== flightMotion) return
        live.style.transition = `transform ${SIDEBAR_FLIGHT_MS}ms var(--ease-out-expo)`
        void live.offsetHeight
        live.style.transform = `translate3d(${dx}px, ${dy}px, 0px)`
        scheduleFlightClear(runId)
      })
    })
  })
}

function releaseHandoffIfHuman(runId: string, group: SidebarRunGroup) {
  if (group !== 'running') return
  const mark = readSidebarHandoff(runId)
  if (!mark) return
  const node = runningById.value.get(runId)?.currentNode
  if (!node?.id || node.id === mark.fromNodeId || isAutoProcessNode(node)) return
  clearSidebarHandoff(runId)
}

/** Move a row only after its group actually changes. The click mark alone does not. */
function navIsShown(): boolean {
  const root = navRoot.value
  if (!root?.isConnected) return false
  let el: HTMLElement | null = root
  while (el) {
    if (typeof window.getComputedStyle === 'function' && window.getComputedStyle(el).display === 'none') return false
    el = el.parentElement
  }
  return true
}

function headingIsFlightTarget(id: RunSectionId): boolean {
  return Object.values(flights.value).some((flight) =>
    flight.landing === 'heading' && ((id === 'auto' && flight.kind === 'in') || (id === 'running' && flight.kind === 'back')),
  )
}

function settleRunGroups() {
  const next = new Map<string, SidebarRunGroup>()
  for (const it of orderedHumanRows.value) next.set(it.runId, 'running')
  for (const it of orderedAutoRows.value) next.set(it.runId, 'auto')
  if (!navIsShown()) {
    paintedGroups = next
    return
  }
  const reduced = prefersReducedMotion()
  for (const [runId, group] of next) {
    const previous = paintedGroups.get(runId) ?? null
    releaseHandoffIfHuman(runId, group)
    const mark = readSidebarHandoff(runId)
    const run = runningById.value.get(runId)
    const decision = decideSidebarFlight({
      previous,
      next: group,
      handoffArmed: !!mark,
      confirmedAuto: !!mark && (mark.confirmedAuto || isAutoProcessNode(run?.currentNode)),
      reducedMotion: reduced,
      targetCollapsed: isSectionCollapsed(group),
    })
    if (decision.type === 'fly-in' || decision.type === 'fly-back') {
      beginFlight(runId, decision.type === 'fly-in' ? 'in' : 'back', decision.landing)
    }
    if (group === 'auto' && mark) consumeSidebarHandoff(runId)
  }
  for (const runId of paintedGroups.keys()) {
    if (!next.has(runId) && readSidebarHandoff(runId)) clearSidebarHandoff(runId)
  }
  paintedGroups = next
}

const placementKey = computed(() => {
  void sidebarHandoffEpoch.value
  const human = orderedHumanRows.value.map((it) => it.runId).join('\n')
  const auto = orderedAutoRows.value.map((it) => `${it.runId}\t${autoNodeName(it)}`).join('\n')
  return `${human}\u0000${auto}`
})
watch(placementKey, () => { captureRowRects() }, { flush: 'pre' })
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
const settingsItems = settingsNavItems
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
      <div class="sidebar-section-heading grasp-status-host px-3 pb-1 pt-2 text-[10px] font-semibold uppercase tracking-wider text-txt3" :data-testid="section.headingTestId" :data-flight-target="headingIsFlightTarget(section.id) ? 'true' : undefined">
        <button
          type="button"
          class="sidebar-section-label"
          :data-testid="section.toggleTestId"
          :aria-expanded="!section.collapsed"
          :aria-label="t(section.collapsed ? 'nav.sectionExpand' : 'nav.sectionCollapse', { name: t(section.labelKey) })"
          @click="toggleSection(section.id)"
        >
          <Icon :name="section.icon" :size="14" class="sidebar-section-icon" aria-hidden="true" />
          <span>{{ t(section.labelKey) }}</span>
          <Icon :interactive="false" name="chevron-down" :size="12" class="sidebar-section-chevron" :class="{ 'is-collapsed': section.collapsed }" aria-hidden="true" />
        </button>
        <span class="sidebar-section-count">{{ section.count }}</span>
      </div>
      <div v-show="!section.collapsed" class="sidebar-tree" :data-testid="section.rowsTestId">
      <div v-for="it in section.rows" :key="it.runId" class="sidebar-tree-item">
        <div
          v-if="editingKey === it.runId"
          class="nav-item grasp-status-host mb-0.5 w-full min-w-0 cursor-text text-start"
          :class="{ active: isPendingActive(it) }"
          data-testid="nav-pending-row"
          :data-key="it.runId"
          :data-node-id="it.nodeId"
          :data-motion="rowMotion(it) || undefined"
          data-editing="true"
          :aria-current="isPendingActive(it) ? 'true' : undefined"
          @click.stop
          @contextmenu.prevent
        >
          <span
            v-if="isRowPinned(it)"
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
            @keydown.enter.prevent="commitRename(it)"
            @keydown.esc.prevent.stop="onRenameEscape"
            @blur="onRenameBlur(it)"
          />
          <InboxMotionMark v-if="rowMotion(it) === 'spin'" kind="spin" />
          <InboxMotionMark v-else-if="rowMotion(it) === 'pulse'" kind="pulse" />
          <template v-else>
            <Icon
              v-for="mark in pendingTypeMarks(it)"
              :key="mark.name"
              :name="mark.name"
              :busy="mark.name === 'arc-spinner' || mark.name === 'breath-dot'"
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
          :class="{ active: isPendingActive(it), 'sidebar-flight-source': rowFlightHidden(it.runId) }"
          data-testid="nav-pending-row"
          :data-key="it.runId"
          :data-node-id="it.nodeId"
          :data-section="section.id"
          :data-motion="rowMotion(it) || undefined"
          :data-flight="rowFlight(it.runId)"
          :data-flight-landing="rowFlightLanding(it.runId)"
          :title="pendingTooltip(it)"
          :aria-current="isPendingActive(it) ? 'true' : undefined"
          @click="onPendingRow(it)"
          @contextmenu="onPendingContextMenu($event, it)"
          @pointerdown="onPendingPointerDown($event, it)"
          @keydown.f2.prevent="beginRename(it)"
        >
          <span
            v-if="isRowPinned(it)"
            class="flex shrink-0 text-accent"
            data-testid="nav-pending-pin"
            aria-hidden="true"
          >
            <Icon name="pin" :size="12" />
          </span>
          <span v-if="rowTitle(it)" class="nav-row-title min-w-0 truncate text-start" :class="autoNodeName(it) ? 'max-w-[62%] shrink' : 'flex-1'">{{ rowTitle(it) }}</span>
          <span v-else class="nav-row-title-skeleton min-w-0 flex-1" data-testid="nav-pending-title-skeleton" aria-hidden="true"><span /></span>
          <span v-if="autoNodeName(it)" class="nav-auto-node-name min-w-0 truncate" data-testid="nav-auto-node-name">{{ autoNodeName(it) }}</span>
          <span v-if="autoNodeName(it)" class="min-w-0 flex-1" aria-hidden="true" />
          <InboxMotionMark v-if="rowMotion(it) === 'spin'" kind="spin" />
          <InboxMotionMark v-else-if="rowMotion(it) === 'pulse'" kind="pulse" />
          <template v-else>
            <Icon
              v-for="mark in pendingTypeMarks(it)"
              :key="mark.name"
              :name="mark.name"
              :busy="mark.name === 'arc-spinner' || mark.name === 'breath-dot'"
              :size="16"
              class="nav-row-type-icon"
              :class="mark.toneClass"
              data-testid="nav-pending-type-icon"
              aria-hidden="true"
            />
          </template>
        </button>
      </div>
      <div v-for="session in section.solos" :key="soloRowKey(session)" class="sidebar-tree-item">
      <div
        v-if="editingKey === soloRowKey(session)"
        class="nav-item grasp-status-host mb-0.5 w-full min-w-0 cursor-text text-start"
        :class="{ active: isSoloRowActive(session) }"
        data-testid="nav-pending-row"
        :data-key="session.id"
        data-kind="solo"
        data-editing="true"
        @click.stop
        @contextmenu.prevent
      >
        <span v-if="soloPins.some((p) => p.itemKey === soloRowKey(session))" class="flex shrink-0 text-accent" data-testid="nav-pending-pin" aria-hidden="true"><Icon name="pin" :size="12" /></span>
        <input
          :ref="setRenameInput"
          v-model="renameDraft"
          type="text"
          class="h-[26px] min-w-0 flex-1 rounded-md border border-accent bg-base px-1.5 text-start text-sm font-normal text-txt outline-none"
          data-testid="nav-pending-rename-input"
          :aria-label="t('nav.pendingRenameLabel')"
          :readonly="saving"
          @click.stop
          @keydown.enter.prevent="commitSoloRename(session)"
          @keydown.esc.prevent.stop="onRenameEscape"
          @blur="onSoloRenameBlur(session)"
        />
        <InboxMotionMark v-if="soloRowMotion(session) === 'spin'" kind="spin" />
        <InboxMotionMark v-else-if="soloRowMotion(session) === 'pulse'" kind="pulse" />
      </div>
      <button
        v-else
        v-hover-ink
        type="button"
        class="nav-item grasp-status-host mb-0.5 w-full min-w-0 text-start"
        :class="{ active: isSoloRowActive(session) }"
        data-testid="nav-pending-row"
        :data-key="session.id"
        data-kind="solo"
        :data-status="session.status"
        :data-motion="soloRowMotion(session) || undefined"
        :title="soloRowTitle(session)"
        :aria-current="isSoloRowActive(session) ? 'page' : undefined"
        @click="onSoloRow(session)"
        @contextmenu="onSoloContextMenu($event, session)"
      >
        <span v-if="soloPins.some((p) => p.itemKey === soloRowKey(session))" class="flex shrink-0 text-accent" data-testid="nav-pending-pin" aria-hidden="true"><Icon name="pin" :size="12" /></span>
        <span class="nav-row-title min-w-0 flex-1 truncate text-start">{{ soloRowTitle(session) }}</span>
        <InboxMotionMark v-if="soloRowMotion(session) === 'spin'" kind="spin" />
        <InboxMotionMark v-else-if="soloRowMotion(session) === 'pulse'" kind="pulse" />
      </button>
      </div>
      </div>
    </div>
    <SidebarWorkspaceLists v-if="workspaceLists" :run-titles="pendingRunTitles" @navigate="onNavigate" />
    </div>
    </Transition>
    </div>

  </nav>
  <Teleport to="body">
    <div
      v-if="flightGhost"
      ref="flightGhostEl"
      class="sidebar-flight-ghost"
      data-testid="nav-flight-ghost"
      :data-flight="flightGhost.kind"
      :data-landing="flightGhost.landing"
      :style="flightGhost.style"
    >{{ flightGhost.title }}</div>
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
        type="button"
        role="menuitem"
        class="pending-row-menu__item"
        data-testid="nav-pending-menu-rename"
        @click="onMenuRename"
      >
        <Icon name="edit" :size="14" />
        <span>{{ t('nav.pendingRename') }}</span>
      </button>
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
        type="button"
        role="menuitem"
        class="pending-row-menu__item"
        data-testid="nav-pending-menu-cancel"
        @click="onMenuCancel"
      >
        <Icon name="close" :size="14" />
        <span>{{ t('nav.pendingCancelRun') }}</span>
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
    @close="closeSoloConfirm"
  >
    <div class="space-y-3 text-sm text-txt2">
      <p>{{ t('pages.solo.close.body') }}</p>
      <p v-if="soloCloseTarget?.busy" class="text-[12px] text-warn">{{ t('pages.solo.close.busy') }}</p>
    </div>
    <template #footer>
      <AppButton variant="ghost" :disabled="cancellingRun" @click="closeSoloConfirm">{{ t('common.buttons.cancel') }}</AppButton>
      <AppButton variant="danger" icon="stop" :disabled="cancellingRun" @click="confirmCloseSolo">{{ t('pages.solo.close.confirm') }}</AppButton>
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
  --hover-ink-color: rgb(var(--c-txt) / .04);
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
  color: rgb(var(--c-txt2));
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
  color: rgb(var(--c-txt2));
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
.sidebar-workspace-nav .sidebar-section-heading {
  display: flex;
  height: 26px;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 6px;
  padding: 0 10px;
  color: color-mix(in srgb, rgb(var(--c-txt2)) 70%, rgb(var(--c-txt3)));
  font-size: 11.5px;
  font-weight: 600;
  letter-spacing: 0;
  text-transform: none;
}
.sidebar-section-label {
  display: inline-flex;
  min-width: 0;
  align-items: center;
  gap: 6px;
  border-radius: 6px;
  color: inherit;
  font: inherit;
  transition: color 160ms ease;
}
.sidebar-section-label:hover {
  color: rgb(var(--c-txt));
}
.sidebar-section-icon {
  flex: none;
}
.sidebar-section-chevron {
  flex: none;
  margin-left: -1px;
  opacity: .7;
  transition: transform 160ms ease, opacity 160ms ease;
}
.sidebar-section-label:hover .sidebar-section-chevron {
  opacity: 1;
}
.sidebar-section-chevron.is-collapsed {
  transform: rotate(-90deg);
}
@media (prefers-reduced-motion: reduce) {
  .sidebar-section-chevron {
    transition: none;
  }
}
.sidebar-section-count {
  display: inline-flex;
  min-width: 18px;
  height: 18px;
  align-items: center;
  justify-content: center;
  border-radius: 6px;
  color: rgb(var(--c-txt3));
  background: rgb(var(--c-txt) / .04);
  font-size: 10px;
  font-variant-numeric: tabular-nums;
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
.sidebar-flight-source {
  visibility: hidden;
  pointer-events: none;
}
.sidebar-flight-ghost {
  position: fixed;
  z-index: 80;
  display: flex;
  align-items: center;
  overflow: hidden;
  padding: 0 10px;
  border-radius: 10px;
  background: light-dark(#fff, rgb(var(--c-surface)));
  color: rgb(var(--c-txt));
  font-size: 13px;
  font-weight: 400;
  pointer-events: none;
  box-shadow: 0 10px 28px rgb(0 0 0 / .14);
}
@media (prefers-reduced-motion: reduce) {
  .sidebar-flight-ghost {
    display: none;
  }
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
