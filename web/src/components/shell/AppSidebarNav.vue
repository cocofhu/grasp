<script setup lang="ts">
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { computed, ref, watch, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../ui/Icon.vue'
import AppButton from '../ui/AppButton.vue'
import AppModal from '../ui/AppModal.vue'
import InboxMotionMark from '@/components/inbox/InboxMotionMark.vue'
import { isBoardSectionPath, isHomeSectionPath, sidebarNavGroups } from '@/data/sidebarNav'
import { inboxBadgeTone, isReplyingInboxItem, isStartingInboxItem, sidebarRunTitle } from '@/lib/inbox/inboxDisplay'
import { isShareableInboxItem } from '@/lib/inbox/gateShareLink'
import {
  INBOX_SHARE_INTENT_QUERY,
  INBOX_SHARE_INTENT_VALUE,
  knownInboxProcessingLock,
} from '@/lib/inbox/inboxShareIntent'
import { orderWithPins, useSidebarPins } from '@/lib/inbox/sidebarPins'
import { api } from '@/lib/api/api'
import { displayRunTitle, RUN_TITLE_MAX } from '@/lib/run/runTitle'
import type { InboxItem, Run } from '@/lib/shared/types'
import { keepRowOrder, mergeRunRows, pinRunId, RUNNING_RUN_STATUSES } from '@/lib/inbox/sidebarRunRows'
import { onRecentActivity, recentRunActive, recentRunMark, recentRunTarget } from '@/lib/run/recentRuns'
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
import SidebarWorkspaceLists from './SidebarWorkspaceLists.vue'
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
const { ensureLoaded: ensureWorkspaceLoaded } = useWorkspaceContext()

function dropCarriedRows(controller: AbortController) {
  if (runningRequest === controller) carriedRows.value = []
}

async function loadRunningRuns() {
  runningRequest?.abort()
  const controller = new AbortController()
  runningRequest = controller
  try {
    if (!(await ensureWorkspaceLoaded()) || runningRequest !== controller) {
      dropCarriedRows(controller)
      return
    }
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
    if (runningRequest === controller) runningRequest = null
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

const runPins = computed(() => pins.value.map((p) => ({ ...p, itemKey: pinRunId(p.itemKey) })))
const isRowPinned = (it: InboxItem) => runPins.value.some((p) => p.itemKey === it.runId)
const orderedPendingRows = computed(() => orderWithPins(pendingRows.value, (it) => it.runId, runPins.value))

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
const menu = ref<{ key: string; x: number; y: number } | null>(null)
const menuEl = ref<HTMLElement | null>(null)

type RowPress = { key: string; x: number; y: number; timer: number }
let rowPress: RowPress | null = null

const menuItem = computed(() => {
  const key = menu.value?.key
  if (!key) return null
  return pendingRows.value.find((it) => it.runId === key) ?? null
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
  menu.value = { key: it.runId, x: left, y: top }
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

function onMenuPin() {
  const it = menuItem.value
  if (!it) return
  if (isRowPinned(it)) {
    for (const p of pins.value) if (pinRunId(p.itemKey) === it.runId) unpinRow(p.itemKey)
  } else pinRow(it.runId)
  closeMenu()
}

function onMenuRename() {
  const it = menuItem.value
  if (!it) return
  void beginRename(it)
}

function onMenuShare() {
  const it = menuItem.value
  if (!it || !isShareableInboxItem(it) || shareBlockReason(it)) return
  closeMenu()
  void router.push({
    path: '/gates',
    query: {
      run: it.runId,
      node: it.nodeId,
      [INBOX_SHARE_INTENT_QUERY]: INBOX_SHARE_INTENT_VALUE,
    },
  })
  onNavigate()
}

const cancelTarget = ref<InboxItem | null>(null)
const cancellingRun = ref(false)
const cancelRunError = ref('')

function onMenuCancel() {
  const it = menuItem.value
  if (!it || cancellingRun.value) return
  closeMenu()
  cancelRunError.value = ''
  cancelTarget.value = it
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

async function confirmCancelRun() {
  const target = cancelTarget.value
  if (!target || cancellingRun.value) return
  cancellingRun.value = true
  cancelRunError.value = ''
  try {
    await api.cancelRun(target.runId)
    cancelTarget.value = null
    toast.success(t('pages.runDetail.cancelSuccess'))
    await loadRunningRuns()
  } catch (e) {
    cancelRunError.value = mapCancelRunError(e)
  } finally {
    cancellingRun.value = false
  }
}

watch(pendingRows, (rows) => {
  if (menu.value && !rows.some((it) => it.runId === menu.value?.key)) closeMenu()
  if (!editingKey.value) return
  if (rows.some((it) => it.runId === editingKey.value)) return
  editEpoch.value++
  editingKey.value = null
  renameDraft.value = ''
  saving.value = false
})

function rowMotion(it: InboxItem): 'spin' | 'pulse' | null {
  if (isRunOnlyRow(it)) {
    const run = runningById.value.get(it.runId)
    if (!run) return 'pulse'
    const mark = recentRunMark(run)
    if (!mark) return run.status === 'queued' ? 'spin' : 'pulse'
    return 'motion' in mark ? mark.motion : null
  }
  if (startFailedKeys.value.includes(itemKey(it))) return null
  if (isStartingInboxItem(it)) return 'spin'
  if (isReplyingInboxItem(it)) return 'pulse'
  return null
}

/** Waiting on a person: static type mark. Executing rows keep motion; start-failed rows stay blank. */
function pendingTypeMarks(it: InboxItem): Array<{ name: string; toneClass: string }> {
  if (isRunOnlyRow(it)) {
    const run = runningById.value.get(it.runId)
    const mark = run ? recentRunMark(run) : null
    return mark && 'icon' in mark ? [mark.icon] : []
  }
  if (rowMotion(it) || startFailedKeys.value.includes(itemKey(it))) return []
  const tone = inboxBadgeTone(it)
  if (tone === 'replying') return []
  if (tone === 'gate') return [{ name: 'pending-approval', toneClass: 'nav-row-type-icon--approval' }]
  if (tone === 'review') return [{ name: 'pending-review', toneClass: 'nav-row-type-icon--review' }]
  return [{ name: 'pending-chat', toneClass: 'nav-row-type-icon--chat' }]
}

function isPendingActive(it: InboxItem): boolean {
  const onInbox = route.path === '/gates' || route.path.startsWith('/gates/')
  if (onInbox && activeInboxKey.value?.startsWith(`${it.runId}:`)) return true
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
    void router.push(run ? recentRunTarget(run) : { path: '/gates', query: { run: it.runId } })
  } else {
    void router.push({ path: '/gates', query: { run: it.runId, node: it.nodeId } })
  }
  onNavigate()
}
function pollRefresh() {
  if (!document.hidden) {
    void loadRunningRuns()
  }
  return peek({ source: 'sidebar-poll' })
}
const offRunActivity = onRecentActivity(() => void loadRunningRuns())
onMounted(() => {
  refresh({ source: 'mount' })
  void loadRunningRuns()
  timer = window.setInterval(pollRefresh, 15000)
  chromeHasMounted.value = true
  document.addEventListener('mousedown', onDocMouseDown)
  document.addEventListener('keydown', onDocKeydown)
})
onBeforeUnmount(() => {
  if (timer) clearInterval(timer)
  offRunActivity()
  runningRequest?.abort()
  clearRowPress()
  document.removeEventListener('mousedown', onDocMouseDown)
  document.removeEventListener('keydown', onDocKeydown)
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

    <!-- Workspace primary + quick workflows slide together (plan g2.2) -->
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

    <div v-if="pendingRows.length" class="mb-3" data-testid="nav-pending-approvals">
      <div class="sidebar-section-heading px-3 pb-1 pt-2 text-[10px] font-semibold uppercase tracking-wider text-txt3">
        <span>{{ t('nav.runningRuns') }}</span>
        <span class="sidebar-section-count">{{ pendingRows.length }}</span>
      </div>
      <template v-for="it in orderedPendingRows" :key="it.runId">
        <div
          v-if="editingKey === it.runId"
          class="nav-item mb-0.5 w-full min-w-0 cursor-text text-start"
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
              :size="14"
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
          class="nav-item mb-0.5 w-full min-w-0 text-start"
          :class="{ active: isPendingActive(it) }"
          data-testid="nav-pending-row"
          :data-key="it.runId"
          :data-node-id="it.nodeId"
          :data-motion="rowMotion(it) || undefined"
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
          <span v-if="rowTitle(it)" class="nav-row-title min-w-0 flex-1 truncate text-start">{{ rowTitle(it) }}</span>
          <span v-else class="nav-row-title-skeleton min-w-0 flex-1" data-testid="nav-pending-title-skeleton" aria-hidden="true"><span /></span>
          <InboxMotionMark v-if="rowMotion(it) === 'spin'" kind="spin" />
          <InboxMotionMark v-else-if="rowMotion(it) === 'pulse'" kind="pulse" />
          <template v-else>
            <Icon
              v-for="mark in pendingTypeMarks(it)"
              :key="mark.name"
              :name="mark.name"
              :size="14"
              class="nav-row-type-icon"
              :class="mark.toneClass"
              data-testid="nav-pending-type-icon"
              aria-hidden="true"
            />
          </template>
        </button>
      </template>
    </div>
    <SidebarWorkspaceLists v-if="workspaceLists" :run-titles="pendingRunTitles" @navigate="onNavigate" />
    </div>
    </Transition>
    </div>

  </nav>
  <Teleport to="body">
    <div
      v-if="menu && menuItem"
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
        <Icon :name="isRowPinned(menuItem) ? 'close' : 'pin'" :size="14" />
        <span>{{ isRowPinned(menuItem) ? t('nav.pendingUnpin') : t('nav.pendingPin') }}</span>
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
  background: rgb(var(--c-txt) / .055);
  font-weight: 600;
}
.nav-create-action > svg {
  color: rgb(var(--c-txt2));
}
.sidebar-workspace-nav [data-testid="nav-workspace-chrome"] {
  margin-bottom: 16px;
}
.sidebar-workspace-nav [data-testid="nav-pending-approvals"] {
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
