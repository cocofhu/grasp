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
const { pins, pin: pinRow, unpin: unpinRow, isPinned } = useSidebarPins()
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
  const name = sidebarRunTitle(it)
  const node = pendingNodeLabel(it).trim()
  return node ? `${name} · ${node}` : name
}

const pendingRows = computed(() => {
  const shown = new Set(displayedItems.value.map(itemKey))
  const extras = sidebarExtras.value.filter((it) => !shown.has(itemKey(it)))
  return [...extras, ...displayedItems.value]
})
const pendingRunTitles = computed(() => {
  const titles: Record<string, string> = {}
  for (const item of pendingRows.value) {
    if (typeof item.runTitle === 'string') titles[item.runId] = item.runTitle
  }
  return titles
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
function pendingTypeMarks(it: InboxItem): Array<{ name: string; toneClass: string }> {
  if (rowMotion(it) || startFailedKeys.value.includes(itemKey(it))) return []
  const tone = inboxBadgeTone(it)
  if (tone === 'replying') return []
  if (tone === 'gate') return [{ name: 'pending-approval', toneClass: 'nav-row-type-icon--approval' }]
  if (tone === 'review') return [{ name: 'pending-review', toneClass: 'nav-row-type-icon--review' }]
  return [{ name: 'pending-chat', toneClass: 'nav-row-type-icon--chat' }]
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
    },
)

function isActive(to: string) {
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
        <span>{{ t('nav.pendingApprovals') }}</span>
        <span class="sidebar-section-count">{{ pendingRows.length }}</span>
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
