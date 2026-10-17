<script setup lang="ts">
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { computed, ref, watch, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../ui/Icon.vue'
import InboxMotionMark from '@/components/inbox/InboxMotionMark.vue'
import { sidebarNavGroups } from '@/data/sidebarNav'
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
import type { InboxItem } from '@/lib/shared/types'
import {
  isSettingsChrome,
  settingsItemActive,
  settingsItemKey,
  settingsNavItems,
  type SettingsNavItem,
} from '@/data/settingsNav'
import { usePendingGates } from '@/lib/inbox/usePendingGates'
import { useRunTerminalNotifications } from '@/lib/run/useRunTerminalNotifications'
import { useWorkflowFavorites } from '@/lib/run/useWorkflowFavorites'
import { useWorkflowRunLaunch } from '@/lib/run/useWorkflowRunLaunch'
import { useToast } from '@/lib/composables/useToast'
import { useBreakpoint } from '@/lib/composables/useBreakpoint'

defineProps<{ drawer?: boolean }>()
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
const { pins, pin: pinRow, unpin: unpinRow, isPinned } = useSidebarPins()
// Same unreadCount singleton as shell chrome bell — keep the settings notification badge in sync.
const { unreadCount } = useRunTerminalNotifications()
const { displayItems, hydrateDisplay, unfavorite, getFavoriteWorkflow, reorderFavorites } = useWorkflowFavorites()
const { openLaunch } = useWorkflowRunLaunch()
const { isMobile } = useBreakpoint()

const DRAG_THRESHOLD_PX = 4
const quickList = ref<HTMLElement>()
const suppressQuickItemClick = ref(false)
const dragState = ref<{
  workflowId: string
  from: number
  placeholder: number
  x: number
  y: number
  offsetX: number
  offsetY: number
  width: number
}>()
const dragItems = computed(() =>
  dragState.value
    ? displayItems.value.filter((item) => item.workflowId !== dragState.value?.workflowId)
    : displayItems.value,
)

let timer: number | undefined
function badgeFor(to: string): number {
  if (to === '/notifications') return unreadCount.value
  return 0
}

function pendingNodeLabel(it: InboxItem): string {
  return it.type === 'gate' ? it.title : it.label
}

function pendingTooltip(it: InboxItem): string {
  const name = sidebarRunTitle(it)
  const node = pendingNodeLabel(it).trim()
  return node ? `${name} · ${node}` : name
}

const pendingRows = computed(() => {
  const shown = new Set(displayedItems.value.map(itemKey))
  const extras = sidebarExtras.value.filter((it) => !shown.has(itemKey(it)))
  return [...extras, ...displayedItems.value]
})

const orderedPendingRows = computed(() => orderWithPins(pendingRows.value, itemKey, pins.value))

const LONG_PRESS_MS = 500
const LONG_PRESS_MOVE_PX = 8
const MENU_WIDTH = 168
/** Blocked share copy is longer than the two-item menu, so the clamp must follow it. */
const MENU_WIDTH_BLOCKED = 280
/** Two items used to be a flat 72: 8px of menu padding plus 32px per row. */
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
  return pendingRows.value.find((it) => itemKey(it) === key) ?? null
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
  const rows = isShareableInboxItem(it) ? 3 : 2
  return MENU_CHROME + rows * MENU_ROW_HEIGHT
}

function openMenu(it: InboxItem, x: number, y: number) {
  if (editingKey.value) return
  const height = pendingMenuHeight(it)
  const width = shareBlockReason(it) ? MENU_WIDTH_BLOCKED : MENU_WIDTH
  const left = Math.max(MENU_MARGIN, Math.min(x, window.innerWidth - width))
  const top = Math.max(MENU_MARGIN, Math.min(y, window.innerHeight - height))
  menu.value = { key: itemKey(it), x: left, y: top }
}

const menuShareVisible = computed(() => !!menuItem.value && isShareableInboxItem(menuItem.value))
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
  const key = itemKey(it)
  const timer = window.setTimeout(() => {
    if (!rowPress || rowPress.key !== key) return
    const x = rowPress.x
    const y = rowPress.y
    suppressPendingClick.value = true
    window.setTimeout(() => {
      suppressPendingClick.value = false
    }, 400)
    clearRowPress()
    const row = pendingRows.value.find((item) => itemKey(item) === key)
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
  editingKey.value = itemKey(it)
  renameDraft.value = displayRunTitle(it.runTitle)
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
  if (saving.value || editingKey.value !== itemKey(it)) return
  if (!pendingRows.value.some((row) => itemKey(row) === itemKey(it))) {
    cancelEdit()
    return
  }
  const next = renameDraft.value.trim()
  const prev = displayRunTitle(it.runTitle)
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
  const key = itemKey(it)
  window.setTimeout(() => {
    if (saving.value || editingKey.value !== key) return
    if (document.activeElement === renameInput.value) return
    void commitRename(it)
  }, 0)
}

function onMenuPin() {
  const it = menuItem.value
  if (!it) return
  const key = itemKey(it)
  if (isPinned(key)) unpinRow(key)
  else pinRow(key)
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

watch(pendingRows, (rows) => {
  if (menu.value && !rows.some((it) => itemKey(it) === menu.value?.key)) closeMenu()
  if (!editingKey.value) return
  if (rows.some((it) => itemKey(it) === editingKey.value)) return
  editEpoch.value++
  editingKey.value = null
  renameDraft.value = ''
  saving.value = false
})

function rowMotion(it: InboxItem): 'spin' | 'pulse' | null {
  if (startFailedKeys.value.includes(itemKey(it))) return null
  if (isStartingInboxItem(it)) return 'spin'
  if (isReplyingInboxItem(it)) return 'pulse'
  return null
}

/** Waiting on a person: static type mark. Executing rows keep motion; start-failed rows stay blank. */
function pendingTypeMarks(it: InboxItem): Array<{ name: 'chat' | 'gate'; toneClass: string }> {
  if (rowMotion(it) || startFailedKeys.value.includes(itemKey(it))) return []
  const tone = inboxBadgeTone(it)
  if (tone === 'replying') return []
  if (tone === 'gate') return [{ name: 'gate', toneClass: 'text-warn' }]
  if (tone === 'review') return [{ name: 'chat', toneClass: 'text-n-review' }]
  return [{ name: 'chat', toneClass: 'text-n-clarify' }]
}

function isPendingActive(it: InboxItem): boolean {
  if (route.path !== '/gates' && !route.path.startsWith('/gates/')) return false
  return activeInboxKey.value === itemKey(it)
}

function onPendingRow(it: InboxItem) {
  if (editingKey.value) return
  if (suppressPendingClick.value) {
    suppressPendingClick.value = false
    return
  }
  void router.push({ path: '/gates', query: { run: it.runId, node: it.nodeId } })
  onNavigate()
}
function pollRefresh() {
  return peek({ source: 'sidebar-poll' })
}
onMounted(() => {
  refresh({ source: 'mount' })
  void hydrateDisplay()
  timer = window.setInterval(pollRefresh, 15000)
  chromeHasMounted.value = true
  document.addEventListener('mousedown', onDocMouseDown)
  document.addEventListener('keydown', onDocKeydown)
})
onBeforeUnmount(() => {
  if (timer) clearInterval(timer)
  clearRowPress()
  document.removeEventListener('mousedown', onDocMouseDown)
  document.removeEventListener('keydown', onDocKeydown)
})
// Refresh promptly when navigating (e.g. right after approving a gate).
watch(
  () => route.path,
  () => {
    refresh({ source: 'navigate' })
    void hydrateDisplay()
  },
)

function isActive(to: string) {
  return route.path === to || route.path.startsWith(to + '/')
}

const settingsChrome = computed(() => isSettingsChrome(route.path, route.meta.full === true))

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

function onUnfavorite(workflowId: string, name: string, ev: Event) {
  ev.stopPropagation()
  ev.preventDefault()
  if (suppressQuickItemClick.value) return
  unfavorite(workflowId, { name })
}

async function onLaunch(workflowId: string) {
  if (suppressQuickItemClick.value) return
  try {
    const wf = await getFavoriteWorkflow(workflowId)
    if (!wf) return
    openLaunch(wf)
    onNavigate()
  } catch {
    toast.error(t('common.toast.favoriteLaunchFailed'))
  }
}

function updatePlaceholder(clientY: number) {
  if (!dragState.value || !quickList.value) return
  const rows = Array.from(quickList.value.querySelectorAll<HTMLElement>('[data-sortable-row]'))
  const slot = rows.findIndex((row) => clientY < row.getBoundingClientRect().top + row.offsetHeight / 2)
  dragState.value.placeholder = slot === -1 ? rows.length : slot
}

function onHandlePointerDown(workflowId: string, event: PointerEvent) {
  if (isMobile.value || event.button !== 0) return
  const source = (event.currentTarget as HTMLElement).closest<HTMLElement>('[data-sortable-row]')
  const from = displayItems.value.findIndex((item) => item.workflowId === workflowId)
  if (!source || from < 0) return

  event.preventDefault()
  const handle = event.currentTarget as HTMLElement
  const startX = event.clientX
  const startY = event.clientY
  let activated = false
  let offsetX = 0
  let offsetY = 0

  try {
    handle.setPointerCapture(event.pointerId)
  } catch {
    // Pointer capture is a progressive enhancement for the drag session.
  }

  const activate = (moveEvent: PointerEvent) => {
    const rect = source.getBoundingClientRect()
    offsetX = moveEvent.clientX - rect.left
    offsetY = moveEvent.clientY - rect.top
    activated = true
    suppressQuickItemClick.value = true
    dragState.value = {
      workflowId,
      from,
      placeholder: from,
      x: moveEvent.clientX,
      y: moveEvent.clientY,
      offsetX,
      offsetY,
      width: rect.width,
    }
    document.body.classList.add('quick-workflow-dragging')
    updatePlaceholder(moveEvent.clientY)
  }

  const onMove = (moveEvent: PointerEvent) => {
    if (!activated) {
      const dx = moveEvent.clientX - startX
      const dy = moveEvent.clientY - startY
      if (Math.hypot(dx, dy) < DRAG_THRESHOLD_PX) return
      activate(moveEvent)
    }
    if (!dragState.value) return
    dragState.value.x = moveEvent.clientX
    dragState.value.y = moveEvent.clientY
    updatePlaceholder(moveEvent.clientY)
    moveEvent.preventDefault()
  }

  const finish = (cancelled: boolean) => {
    handle.removeEventListener('pointermove', onMove)
    handle.removeEventListener('pointerup', onUp)
    handle.removeEventListener('pointercancel', onCancel)
    window.removeEventListener('pointermove', onMove)
    window.removeEventListener('pointerup', onUp)
    window.removeEventListener('pointercancel', onCancel)
    try {
      handle.releasePointerCapture(event.pointerId)
    } catch {
      // Capture may already be released by the browser.
    }
    if (!activated || !dragState.value) return

    const { from: dragFrom, placeholder } = dragState.value
    dragState.value = undefined
    document.body.classList.remove('quick-workflow-dragging')
    if (!cancelled) reorderFavorites(dragFrom, placeholder)
    window.setTimeout(() => {
      suppressQuickItemClick.value = false
    }, 0)
  }
  const onUp = () => finish(false)
  const onCancel = () => finish(true)

  handle.addEventListener('pointermove', onMove)
  handle.addEventListener('pointerup', onUp)
  handle.addEventListener('pointercancel', onCancel)
  // Once activation removes the source row from the rendered list, some browsers
  // release element-level pointer capture. Window listeners keep the session intact.
  window.addEventListener('pointermove', onMove)
  window.addEventListener('pointerup', onUp)
  window.addEventListener('pointercancel', onCancel)
}

const primaryGroup = sidebarNavGroups[0]
const settingsItems = settingsNavItems
</script>

<template>
  <nav
    class="scroll-area flex-1 overflow-y-auto px-3 py-2"
    data-testid="app-sidebar-nav"
    :data-nav-mode="settingsChrome ? 'settings' : 'workspace'"
  >
    <div class="nav-chrome-viewport">
    <Transition :name="chromeSlideName" :css="chromeUseCss">
    <!-- Settings chrome: back to home + category list (plan g2.2) -->
    <div v-if="settingsChrome" key="settings" class="nav-chrome-pane mb-3" data-testid="nav-settings-chrome">
      <RouterLink
        v-hover-ink
        to="/dashboard"
        class="nav-item mb-2 text-txt3"
        data-testid="nav-back-home"
        @click="onNavigate"
      >
        <Icon name="arrow-left" :size="16" />
        <span class="flex-1 text-xs">{{ t('nav.backHome') }}</span>
      </RouterLink>
      <template v-for="(item, index) in settingsItems" :key="settingsItemKey(item, index)">
        <div
          v-if="item.groupKey"
          class="px-3 pb-1 pt-2 text-[10px] font-semibold uppercase tracking-wider text-txt3"
        >
          {{ t(item.groupKey) }}
        </div>
        <RouterLink
          v-hover-ink
          :to="settingsLinkTo(item)"
          class="nav-item mb-0.5"
          :class="{ active: isSettingsItemActive(item) }"
          :data-testid="item.query?.integrations ? 'nav-settings-integrations' : undefined"
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
        v-for="item in primaryGroup.items"
        :key="item.to"
        v-hover-ink
        :to="item.to"
        class="nav-item mb-0.5"
        :class="{ active: isActive(item.to) }"
        @click="onNavigate"
      >
        <Icon :name="item.icon" :size="17" />
        <span class="flex-1">{{ t(item.labelKey) }}</span>
        <span
          v-if="badgeFor(item.to)"
          class="force-radius-full flex h-5 min-w-5 items-center justify-center rounded-full bg-accent px-1.5 text-[11px] font-semibold text-white"
        >{{ badgeFor(item.to) }}</span>
      </RouterLink>
    </div>

    <!-- Quick workflows: workspace only, and only when favorites exist (plan g1.2) -->
    <div v-if="displayItems.length" class="mb-3" data-testid="nav-quick-workflows">
      <div class="px-3 pb-1 pt-2 text-[10px] font-semibold uppercase tracking-wider text-txt3">
        {{ t('nav.quickWorkflows') }}
      </div>
      <div ref="quickList" class="quick-workflows-list">
        <template v-for="(item, index) in dragItems" :key="item.workflowId">
          <div
            v-if="dragState && dragState.placeholder === index"
            class="quick-workflow-placeholder"
            data-testid="nav-quick-workflow-placeholder"
          />
          <div
            class="mb-0.5 grid w-full grid-cols-[28px_1fr_28px] items-center gap-0.5 px-2 py-1.5 text-left text-txt2 transition hover:bg-elevated hover:text-txt"
            data-sortable-row
            data-testid="nav-quick-workflow-item"
            role="button"
            tabindex="0"
            @click="onLaunch(item.workflowId)"
            @keydown.enter.prevent="onLaunch(item.workflowId)"
            @keydown.space.prevent="onLaunch(item.workflowId)"
          >
            <button
              v-if="!isMobile"
              type="button"
              class="quick-workflow-handle flex h-7 w-7 items-center justify-center text-txt3 hover:bg-overlay hover:text-txt"
              data-testid="nav-quick-workflow-drag-handle"
              aria-label="拖动调整顺序"
              title="拖动调整顺序"
              @pointerdown="onHandlePointerDown(item.workflowId, $event)"
            ><span aria-hidden="true">⠿</span></button>
            <div v-else aria-hidden="true" />
            <div class="min-w-0">
              <div class="truncate text-[13px] text-txt" :title="item.name">{{ item.name }}</div>
              <div class="mt-0.5 flex min-w-0 items-center gap-1.5">
                <span class="truncate text-[11px] text-txt3" :title="item.projectName">{{ item.projectName }}</span>
                <span
                  v-if="item.status === 'draft'"
                  class="rounded-md shrink-0 border border-warn/35 bg-warn/10 px-1.5 py-px text-[10px] text-warn"
                >{{ t('common.status.draft') }}</span>
              </div>
            </div>
            <button
              type="button"
              class="flex h-7 w-7 items-center justify-center text-warn hover:text-warn"
              data-testid="nav-quick-workflow-unfavorite"
              :aria-label="t('common.buttons.unfavorite')"
              :title="t('common.buttons.unfavorite')"
              @click="onUnfavorite(item.workflowId, item.name, $event)"
            >
              <Icon name="star-filled" :size="14" />
            </button>
          </div>
        </template>
        <div
          v-if="dragState && dragState.placeholder === dragItems.length"
          class="quick-workflow-placeholder"
          data-testid="nav-quick-workflow-placeholder"
        />
      </div>
    </div>

    <div v-if="pendingRows.length" class="mb-3" data-testid="nav-pending-approvals">
      <div class="px-3 pb-1 pt-2 text-[10px] font-semibold uppercase tracking-wider text-txt3">
        {{ t('nav.pendingApprovals') }}
      </div>
      <template v-for="it in orderedPendingRows" :key="itemKey(it)">
        <div
          v-if="editingKey === itemKey(it)"
          class="nav-item mb-0.5 w-full min-w-0 cursor-text text-start"
          :class="{ active: isPendingActive(it) }"
          data-testid="nav-pending-row"
          :data-key="itemKey(it)"
          :data-motion="rowMotion(it) || undefined"
          data-editing="true"
          :aria-current="isPendingActive(it) ? 'true' : undefined"
          @click.stop
          @contextmenu.prevent
        >
          <span
            v-if="isPinned(itemKey(it))"
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
          :data-key="itemKey(it)"
          :data-motion="rowMotion(it) || undefined"
          :title="pendingTooltip(it)"
          :aria-current="isPendingActive(it) ? 'true' : undefined"
          @click="onPendingRow(it)"
          @contextmenu="onPendingContextMenu($event, it)"
          @pointerdown="onPendingPointerDown($event, it)"
          @keydown.f2.prevent="beginRename(it)"
        >
          <span
            v-if="isPinned(itemKey(it))"
            class="flex shrink-0 text-accent"
            data-testid="nav-pending-pin"
            aria-hidden="true"
          >
            <Icon name="pin" :size="12" />
          </span>
          <span class="nav-row-title min-w-0 flex-1 truncate text-start">{{ sidebarRunTitle(it) }}</span>
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
        <Icon :name="isPinned(itemKey(menuItem)) ? 'close' : 'pin'" :size="14" />
        <span>{{ isPinned(itemKey(menuItem)) ? t('nav.pendingUnpin') : t('nav.pendingPin') }}</span>
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
    </div>
  </Teleport>
  <div
    v-if="dragState"
    class="quick-workflow-drag-float grid grid-cols-[28px_1fr_28px] items-center gap-0.5 px-2 py-1.5"
    :style="{
      width: `${dragState.width}px`,
      left: `${dragState.x - dragState.offsetX}px`,
      top: `${dragState.y - dragState.offsetY}px`,
    }"
    aria-hidden="true"
  >
    <span class="flex h-7 w-7 items-center justify-center text-txt">⠿</span>
    <div class="min-w-0">
      <div class="truncate text-[13px] text-txt">{{ displayItems.find((item) => item.workflowId === dragState?.workflowId)?.name }}</div>
      <div class="truncate text-[11px] text-txt3">{{ displayItems.find((item) => item.workflowId === dragState?.workflowId)?.projectName }}</div>
    </div>
    <span class="flex h-7 w-7 items-center justify-center text-warn"><Icon name="star-filled" :size="14" /></span>
  </div>
</template>

<style scoped>
/* Buttons default to centered text. Pending titles start at the row edge.
   Weight stays 400 so the active nav rule (600) does not bold this title. */
.nav-row-title {
  text-align: start;
  font-weight: 400;
}

.nav-row-type-icon {
  flex: none;
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

.quick-workflow-handle {
  cursor: grab;
  touch-action: none;
}

.quick-workflow-handle:active {
  cursor: grabbing;
}

.quick-workflow-placeholder {
  height: 46px;
  margin-bottom: 2px;
  border: 1px dashed rgb(var(--c-accent) / 70%);
  background: rgb(var(--c-accent) / 12%);
}

.quick-workflow-drag-float {
  position: fixed;
  z-index: 9999;
  pointer-events: none;
  border: 1px solid rgb(var(--c-accent));
  background: rgb(var(--c-elevated));
  box-shadow: 0 12px 28px rgb(0 0 0 / 55%), 0 0 0 1px rgb(var(--c-accent) / 35%);
  opacity: 0.96;
  transform: scale(1.02);
}

:global(body.quick-workflow-dragging),
:global(body.quick-workflow-dragging *) {
  cursor: grabbing !important;
  user-select: none;
}
</style>
