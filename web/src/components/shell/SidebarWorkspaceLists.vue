<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import Icon from '@/components/ui/Icon.vue'
import { useI18n } from 'vue-i18n'
import RecentReorderList from './RecentReorderList.vue'
import { isSectionCollapsed, toggleSection } from '@/lib/shared/sidebarSections'
import { api } from '@/lib/api/api'
import { useWorkspaceContext } from '@/lib/composables/useWorkspaceContext'
import { displayRunTitle } from '@/lib/run/runTitle'
import type { Run } from '@/lib/shared/types'
import type { SoloSession } from '@/lib/api/apiTypes'
import { HISTORY_RUN_STATUSES } from '@/lib/inbox/sidebarRunRows'
import { useSoloSessions } from '@/lib/solo/useSoloSessions'
import {
  RECENT_POLL_MS,
  createRecentHold,
  historyEntries,
  onRecentActivity,
  recentRunActive,
  recentRunTarget,
  runDetailPath,
  sameRecentRows,
} from '@/lib/run/recentRuns'

/** Finished runs requested per page, and the same cap on ended Solo sessions before they merge. */
const SIDEBAR_HISTORY_RUN_LIMIT = 20
/** Same box as the 正在执行 row menu: 8px padding plus one 32px row. */
const MENU_WIDTH = 168
const MENU_HEIGHT = 8 + 32
const MENU_MARGIN = 8
/** Touch long-press, same timing as the 正在执行 rows. */
const LONG_PRESS_MS = 500
const LONG_PRESS_MOVE_PX = 8

const props = defineProps<{ runTitles?: Record<string, string> }>()
const emit = defineEmits<{ (e: 'navigate'): void }>()
const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const context = useWorkspaceContext()
const { workspace, ensureLoaded } = context
const workspaceReady = computed(() => !!workspace.value)
const historyCollapsed = computed(() => isSectionCollapsed('history'))
const runs = ref<Run[]>([])
/** Latest poll result held back while the user is on the list (see createRecentHold). */
let heldRuns: Run[] | null = null
const recentHold = createRecentHold()
const recentListEl = ref<{ $el?: HTMLElement } | null>(null)
const runsFailed = ref(false)
let runsRequest: AbortController | null = null
const runTitle = (run: Run) => displayRunTitle(props.runTitles?.[run.id] ?? run.title) || run.workflowName || run.id
const soloStore = useSoloSessions()
const solos = ref<SoloSession[]>([])
let heldSolos: SoloSession[] | null = null
const soloTitle = (session: SoloSession) => session.title?.trim() || t('pages.solo.untitled')
const soloPath = (session: SoloSession) => `/solo/${encodeURIComponent(session.id)}`
const soloActive = (session: SoloSession) => route.path === soloPath(session)
const soloSignature = (list: SoloSession[]) => list.map((s) => [s.id, soloTitle(s), s.status, s.closedAt, s.lastActiveAt, s.updatedAt].join('\u0001')).join('\u0002')
const entries = computed(() => historyEntries(runs.value, solos.value, SIDEBAR_HISTORY_RUN_LIMIT))

async function loadRuns() {
  runsRequest?.abort()
  const controller = new AbortController()
  runsRequest = controller
  try {
    if (!await ensureLoaded() || runsRequest !== controller) return
    if (!soloStore.loaded.value) void soloStore.refresh()
    const result = await api.listRuns({
      page: 1,
      pageSize: SIDEBAR_HISTORY_RUN_LIMIT,
      status: HISTORY_RUN_STATUSES,
      sort: 'last_activity',
      order: 'desc',
      signal: controller.signal,
    })
    if (runsRequest !== controller) return
    if (!Array.isArray(result?.items)) throw new Error('Invalid run list')
    applyRuns(result.items.slice(0, SIDEBAR_HISTORY_RUN_LIMIT))
    runsFailed.value = false
  } catch {
    if (runsRequest === controller) runsFailed.value = true
  } finally { if (runsRequest === controller) runsRequest = null }
}
/** Write only real changes, and never while the user is on the list. */
function applyRuns(next: Run[]) {
  if (recentHold.isHeld()) { heldRuns = next; return }
  heldRuns = null
  if (sameRecentRows(runs.value, next, runTitle)) return
  runs.value = next
}
function applySolos(next: SoloSession[]) {
  if (recentHold.isHeld()) { heldSolos = next; return }
  heldSolos = null
  if (soloSignature(solos.value) === soloSignature(next)) return
  solos.value = next
}
function flushHeld() {
  if (recentHold.isHeld()) return
  if (heldRuns) applyRuns(heldRuns)
  if (heldSolos) applySolos(heldSolos)
}
watch(() => soloStore.ended.value, (next) => applySolos([...next]), { immediate: true })
watch(recentListEl, (comp) => {
  const el = comp?.$el
  if (el instanceof HTMLElement) recentHold.attach(el, flushHeld)
  else recentHold.detach()
})

watch([workspaceReady, () => route.path], () => {
  if (workspaceReady.value) void loadRuns()
  else { runsRequest?.abort(); runsRequest = null; heldRuns = null; runs.value = []; runsFailed.value = false }
}, { immediate: true })
// No global run channel: poll on the pending-row cadence, plus local relays
// from pages that hold a run WebSocket (live view, inbox chat).
const recentTimer = window.setInterval(() => { if (workspaceReady.value && !document.hidden) void loadRuns() }, RECENT_POLL_MS)
const offRecentActivity = onRecentActivity(() => { if (workspaceReady.value) void loadRuns() })

/** Right-click menu on a workflow run row (Solo sessions have no workflow). */
const menu = ref<{ runId: string; x: number; y: number } | null>(null)
const menuEl = ref<HTMLElement | null>(null)
function openMenu(runId: string, clientX: number, clientY: number) {
  const x = Math.max(MENU_MARGIN, Math.min(clientX, window.innerWidth - MENU_WIDTH))
  const y = Math.max(MENU_MARGIN, Math.min(clientY, window.innerHeight - MENU_HEIGHT))
  menu.value = { runId, x, y }
}
function onRunContextMenu(event: MouseEvent, run: Run) {
  event.preventDefault()
  event.stopPropagation()
  clearRowPress()
  openMenu(run.id, event.clientX, event.clientY)
}
let rowPress: { x: number; y: number; timer: number } | null = null
/** The click that ends a long-press must not also follow the row link. */
let suppressClick = false
function clearRowPress() {
  if (!rowPress) return
  window.clearTimeout(rowPress.timer)
  window.removeEventListener('pointermove', onRowPressMove)
  window.removeEventListener('pointerup', clearRowPress)
  window.removeEventListener('pointercancel', clearRowPress)
  rowPress = null
}
function onRowPressMove(event: PointerEvent) {
  if (rowPress && (Math.abs(event.clientX - rowPress.x) > LONG_PRESS_MOVE_PX || Math.abs(event.clientY - rowPress.y) > LONG_PRESS_MOVE_PX)) clearRowPress()
}
function onRunPointerDown(event: PointerEvent, run: Run) {
  if (event.pointerType === 'mouse') return
  clearRowPress()
  const timer = window.setTimeout(() => {
    if (!rowPress) return
    const { x, y } = rowPress
    clearRowPress()
    suppressClick = true
    window.setTimeout(() => { suppressClick = false }, 400)
    openMenu(run.id, x, y)
  }, LONG_PRESS_MS)
  rowPress = { x: event.clientX, y: event.clientY, timer }
  window.addEventListener('pointermove', onRowPressMove)
  window.addEventListener('pointerup', clearRowPress)
  window.addEventListener('pointercancel', clearRowPress)
}
function onRunClickCapture(event: MouseEvent) {
  if (!suppressClick) return
  suppressClick = false
  event.preventDefault()
  event.stopImmediatePropagation()
}
function closeMenu() { menu.value = null }
function onDocMouseDown(event: MouseEvent) {
  const target = event.target
  if (target instanceof Node && menuEl.value?.contains(target)) return
  closeMenu()
}
function onDocKeydown(event: KeyboardEvent) {
  if (event.key !== 'Escape') return
  event.preventDefault()
  closeMenu()
}
watch(menu, (open, was) => {
  if (open && !was) {
    document.addEventListener('mousedown', onDocMouseDown)
    document.addEventListener('keydown', onDocKeydown)
  } else if (!open && was) {
    document.removeEventListener('mousedown', onDocMouseDown)
    document.removeEventListener('keydown', onDocKeydown)
  }
})
function onMenuWorkflowView() {
  const m = menu.value
  if (!m) return
  closeMenu()
  const query: Record<string, string> = { view: 'canvas' }
  for (const key of ['wf', 'tag'] as const) {
    const raw = route.query[key]
    const value = Array.isArray(raw) ? raw[0] : raw
    if (typeof value === 'string' && value) query[key] = value
  }
  const path = runDetailPath(m.runId)
  if (route.path === path) void router.replace({ path, query })
  else void router.push({ path, query })
  emit('navigate')
}

onBeforeUnmount(() => {
  runsRequest?.abort(); window.clearInterval(recentTimer); offRecentActivity(); recentHold.detach(); clearRowPress()
  document.removeEventListener('mousedown', onDocMouseDown)
  document.removeEventListener('keydown', onDocKeydown)
})
</script>

<template>
  <div class="workspace-lists" data-testid="sidebar-workspace-lists">
    <section class="workspace-recents" data-testid="sidebar-recent">
      <h3 class="workspace-list-heading grasp-status-host" data-icon-motion-span="history-title"><button type="button" class="workspace-list-label grasp-status-host" data-testid="sidebar-recent-toggle" :aria-expanded="!historyCollapsed" :aria-label="t(historyCollapsed ? 'nav.sectionExpand' : 'nav.sectionCollapse', { name: t('nav.runHistory') })" @click="toggleSection('history')"><Icon name="history-back" :size="14" class="workspace-list-heading-icon grasp-status" aria-hidden="true" /><span>{{ t('nav.runHistory') }}</span><Icon :interactive="false" name="chevron-down" :size="12" class="workspace-list-chevron" :class="{ 'is-collapsed': historyCollapsed }" aria-hidden="true" /></button></h3>
      <div v-show="!historyCollapsed" data-testid="sidebar-recent-body">
      <div v-if="runsFailed" class="workspace-list-state" role="alert"><span>{{ t('nav.workspaceLoadFailed') }}</span><button type="button" @click="loadRuns">{{ t('nav.workspaceRetry') }}</button></div>
      <p v-else-if="!entries.length" class="workspace-list-state">{{ t('nav.workspaceNoRuns') }}</p>
      <div v-else class="sidebar-tree">
        <RecentReorderList ref="recentListEl">
          <div v-for="entry in entries" :key="entry.key" class="sidebar-tree-item">
            <RouterLink
              v-hover-ink
              v-if="entry.kind === 'solo'"
              :to="soloPath(entry.session)"
              class="workspace-recent-row"
              :class="{ 'is-active': soloActive(entry.session) }"
              :aria-current="soloActive(entry.session) ? 'page' : undefined"
              :title="soloTitle(entry.session)"
              data-testid="sidebar-recent-solo"
              :data-solo-id="entry.session.id"
              :data-status="entry.session.status"
              @click="emit('navigate')"
            >
              <span class="workspace-list-title">{{ soloTitle(entry.session) }}</span>
              <Icon v-if="entry.session.status === 'error'" name="alert" :size="14" :interactive="false" class="workspace-recent-ended is-failed" data-testid="sidebar-recent-ended" :title="t('common.status.failed')" />
              <Icon v-else name="stop" :size="14" :interactive="false" class="workspace-recent-ended is-cancelled" data-testid="sidebar-recent-ended" :title="t('common.status.cancelled')" />
            </RouterLink>
            <RouterLink
              v-hover-ink
              v-else
              :to="recentRunTarget(entry.run)"
              class="workspace-recent-row"
              :class="{ 'is-active': recentRunActive(entry.run, route) }"
              :aria-current="recentRunActive(entry.run, route) ? 'page' : undefined"
              :title="runTitle(entry.run)"
              data-testid="sidebar-recent-run"
              :data-run-id="entry.run.id"
              :data-status="entry.run.status"
              @click.capture="onRunClickCapture"
              @click="emit('navigate')"
              @contextmenu="onRunContextMenu($event, entry.run)"
              @pointerdown="onRunPointerDown($event, entry.run)"
            >
              <span class="workspace-list-title">{{ runTitle(entry.run) }}</span>
              <Icon v-if="entry.run.status === 'failed'" name="alert" :size="14" :interactive="false" class="workspace-recent-ended is-failed" data-testid="sidebar-recent-ended" :title="t('common.status.failed')" />
              <Icon v-else-if="entry.run.status === 'cancelled'" name="stop" :size="14" :interactive="false" class="workspace-recent-ended is-cancelled" data-testid="sidebar-recent-ended" :title="t('common.status.cancelled')" />
            </RouterLink>
          </div>
        </RecentReorderList>
      </div>
      </div>
    </section>
    <Teleport to="body">
      <div v-if="menu" ref="menuEl" class="recent-row-menu fixed z-[9999] min-w-[148px] rounded-lg border border-line bg-elevated py-1 shadow-card" role="menu" data-testid="sidebar-recent-menu" :style="{ left: `${menu.x}px`, top: `${menu.y}px` }" @mousedown.stop @click.stop @contextmenu.prevent>
        <button type="button" role="menuitem" class="recent-row-menu__item" data-testid="sidebar-recent-menu-workflow" @click="onMenuWorkflowView"><Icon name="workflow" :size="14" /><span>{{ t('nav.pendingWorkflowView') }}</span></button>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.workspace-lists { min-width: 0; font-size: 13px; }
.recent-row-menu__item { display: flex; width: 100%; align-items: center; gap: 8px; border: 0; background: transparent; padding: 6px 12px; color: rgb(var(--c-txt2)); font-size: 12px; text-align: start; white-space: nowrap; cursor: pointer; }
.recent-row-menu__item:hover, .recent-row-menu__item:focus-visible { color: rgb(var(--c-txt)); background: rgb(var(--c-overlay)); outline: none; }
.workspace-list-heading { display: flex; align-items: center; justify-content: flex-start; gap: 1px; margin: 0 0 4px; padding: 0 10px; font-size: 11.5px; font-weight: 600; color: light-dark(#3f3f46, color-mix(in srgb, rgb(var(--c-txt2)) 70%, rgb(var(--c-txt3)))); transition: color 160ms ease; }
.workspace-list-heading:hover { color: rgb(var(--c-txt)); }
.workspace-list-label { display: flex; align-items: center; gap: 6px; min-width: 0; min-height: 30px; padding: 0; border: 0; border-radius: 6px; color: inherit; background: transparent; font: inherit; cursor: pointer; }
.workspace-list-heading-icon { flex: none; }
.workspace-list-chevron { flex: none; margin-left: -1px; opacity: .7; transition: transform 160ms ease, opacity 160ms ease; }
.workspace-list-heading:hover .workspace-list-chevron { opacity: 1; }
.workspace-list-chevron.is-collapsed { transform: rotate(-90deg); }
.workspace-list-title { min-width: 0; flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 13px; font-weight: 400; }
.workspace-list-state { display: flex; flex-wrap: wrap; gap: 6px; padding: 9px 8px; color: rgb(var(--c-txt3)); font-size: 11px; }
.workspace-list-state button { color: rgb(var(--c-accent-2)); }
.workspace-recent-row { display: flex; align-items: center; gap: 10px; width: 100%; min-width: 0; min-height: 36px; margin-bottom: 2px; padding: 8px 10px; border-radius: 10px; font-size: 13px; color: light-dark(#27272a, rgb(var(--c-txt2))); --hover-ink-color: rgb(var(--c-txt) / .04); }
.sidebar-tree-item > .workspace-recent-row { padding-left: 6px; }
.workspace-recent-row:hover { color: rgb(var(--c-txt)); }
.workspace-recent-row.is-active, .workspace-recent-row.is-active:hover { color: rgb(var(--c-txt)); background: light-dark(#f1f1f1, rgb(var(--c-txt) / .065)); }
.workspace-recent-ended { flex: none; transition: color 160ms ease; }
.workspace-recent-ended.is-failed { color: light-dark(#d17a7a, #e08a8a); transform-box: view-box; transform-origin: center; }
.workspace-recent-ended.is-cancelled { color: light-dark(#85858f, #8a8a95); }
.workspace-recent-row:hover .workspace-recent-ended.is-failed,
.workspace-recent-row.is-active .workspace-recent-ended.is-failed { color: light-dark(#be5555, #f0a0a0); }
.workspace-recent-row:hover .workspace-recent-ended.is-cancelled,
.workspace-recent-row:focus-visible .workspace-recent-ended.is-cancelled,
.workspace-recent-row.is-active .workspace-recent-ended.is-cancelled { color: light-dark(#71717a, #a1a1aa); }
.workspace-recent-row:hover .workspace-recent-ended.is-failed { animation: workspace-recent-ended-sway 1600ms linear 1; }
@keyframes workspace-recent-ended-sway {
  0% { transform: rotate(0deg); animation-timing-function: cubic-bezier(.45, 0, .55, 1); }
  16% { transform: rotate(9deg); animation-timing-function: cubic-bezier(.45, 0, .55, 1); }
  34% { transform: rotate(-6.5deg); animation-timing-function: cubic-bezier(.45, 0, .55, 1); }
  52% { transform: rotate(4deg); animation-timing-function: cubic-bezier(.45, 0, .55, 1); }
  68% { transform: rotate(-2deg); animation-timing-function: cubic-bezier(.45, 0, .55, 1); }
  84% { transform: rotate(0.6deg); animation-timing-function: cubic-bezier(.45, 0, .55, 1); }
  100% { transform: rotate(0deg); }
}
a:focus-visible, button:focus-visible { outline: 2px solid rgb(var(--c-accent) / .6); outline-offset: -2px; }
@media (prefers-reduced-motion: reduce) { * { animation: none !important; transition: none !important; } }
</style>
