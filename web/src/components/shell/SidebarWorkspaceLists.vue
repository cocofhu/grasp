<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import Icon from '@/components/ui/Icon.vue'
import { useI18n } from 'vue-i18n'
import RecentReorderList from './RecentReorderList.vue'
import SidebarSectionHeading from './SidebarSectionHeading.vue'
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
  sameRecentRows,
} from '@/lib/run/recentRuns'

/** Finished runs requested per page, and the same cap on ended Solo sessions before they merge. */
const SIDEBAR_HISTORY_RUN_LIMIT = 20

const props = defineProps<{ runTitles?: Record<string, string> }>()
const emit = defineEmits<{
  (e: 'navigate'): void
  (e: 'solo-contextmenu', event: MouseEvent, session: SoloSession): void
  (e: 'solo-menu-keydown', event: KeyboardEvent, session: SoloSession): void
  (e: 'solo-pointerdown', event: PointerEvent, session: SoloSession): void
  (e: 'solo-click', event: MouseEvent): void
}>()
const { t } = useI18n()
const route = useRoute()
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

function onSoloClick(event: MouseEvent) {
  emit('solo-click', event)
  if (!event.defaultPrevented) emit('navigate')
}

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
const endedBadly = (run: Run) => run.status === 'failed' || run.status === 'cancelled'
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
onBeforeUnmount(() => { runsRequest?.abort(); window.clearInterval(recentTimer); offRecentActivity(); recentHold.detach() })
</script>

<template>
  <div class="workspace-lists" data-testid="sidebar-workspace-lists">
    <section class="workspace-recents" data-testid="sidebar-recent">
      <SidebarSectionHeading icon="history-back" :label="t('nav.runHistory')" :collapsed="historyCollapsed" toggle-test-id="sidebar-recent-toggle" data-testid="sidebar-recent-heading" @toggle="toggleSection('history')" />
      <div v-show="!historyCollapsed" data-testid="sidebar-recent-body">
      <div v-if="runsFailed" class="workspace-list-state" role="alert"><span>{{ t('nav.workspaceLoadFailed') }}</span><button type="button" @click="loadRuns">{{ t('nav.workspaceRetry') }}</button></div>
      <p v-else-if="!entries.length" class="workspace-list-state">{{ t('nav.workspaceNoRuns') }}</p>
      <div v-else class="sidebar-tree">
        <RecentReorderList ref="recentListEl">
          <div v-for="entry in entries" :key="entry.key" class="sidebar-tree-item"><RouterLink v-hover-ink v-if="entry.kind === 'solo'" :to="soloPath(entry.session)" class="workspace-recent-row" :class="{ 'is-active': soloActive(entry.session) }" :aria-current="soloActive(entry.session) ? 'page' : undefined" :title="soloTitle(entry.session)" data-testid="sidebar-recent-solo" :data-solo-id="entry.session.id" :data-status="entry.session.status" @contextmenu="emit('solo-contextmenu', $event, entry.session)" @keydown="emit('solo-menu-keydown', $event, entry.session)" @pointerdown="emit('solo-pointerdown', $event, entry.session)" @click.capture="onSoloClick($event)"><span class="workspace-list-title">{{ soloTitle(entry.session) }}</span><Icon v-if="entry.session.status === 'error'" name="alert" :size="14" :interactive="false" class="workspace-recent-ended is-failed" data-testid="sidebar-recent-ended" :title="t('common.status.failed')" aria-hidden="true" /><svg v-else class="workspace-recent-ended is-cancelled" data-testid="sidebar-recent-ended" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" :title="t('common.status.cancelled')"><circle cx="12" cy="12" r="8.25" /><path d="M6.17 6.17 17.83 17.83" /></svg></RouterLink><RouterLink v-hover-ink v-else :to="recentRunTarget(entry.run)" class="workspace-recent-row" :class="{ 'is-active': recentRunActive(entry.run, route) }" :aria-current="recentRunActive(entry.run, route) ? 'page' : undefined" :title="runTitle(entry.run)" data-testid="sidebar-recent-run" :data-run-id="entry.run.id" :data-status="entry.run.status" @click="emit('navigate')"><span class="workspace-list-title">{{ runTitle(entry.run) }}</span><Icon v-if="entry.run.status === 'failed' || entry.run.workspaceSave?.state === 'failed'" name="alert" :size="14" :interactive="false" class="workspace-recent-ended is-failed" data-testid="sidebar-recent-ended" :title="t(entry.run.status === 'failed' ? 'common.status.failed' : 'common.status.save_failed')" aria-hidden="true" /><svg v-else-if="entry.run.status === 'cancelled'" class="workspace-recent-ended is-cancelled" data-testid="sidebar-recent-ended" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" :title="t('common.status.cancelled')"><circle cx="12" cy="12" r="8.25" /><path d="M6.17 6.17 17.83 17.83" /></svg></RouterLink></div>
        </RecentReorderList>
      </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.workspace-lists { min-width: 0; font-size: 13px; }
.workspace-list-title { min-width: 0; flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 13px; font-weight: 400; }
.workspace-list-state { display: flex; flex-wrap: wrap; gap: 6px; padding: 9px 8px; color: rgb(var(--c-txt3)); font-size: 11px; }
.workspace-list-state button { color: rgb(var(--c-accent-2)); }
.workspace-recent-row { display: flex; align-items: center; gap: 10px; width: 100%; min-width: 0; min-height: 36px; margin-bottom: 2px; padding: 8px 10px; border-radius: 10px; font-size: 13px; color: light-dark(#27272a, rgb(var(--c-txt2))); --hover-ink-color: rgb(var(--c-txt) / .04); }
.sidebar-tree-item > .workspace-recent-row { padding-left: 6px; }
.workspace-recent-row:hover { color: rgb(var(--c-txt)); }
.workspace-recent-row.is-active, .workspace-recent-row.is-active:hover { color: rgb(var(--c-txt)); background: light-dark(#f1f1f1, rgb(var(--c-txt) / .065)); }
.workspace-recent-ended { flex: none; transform-box: view-box; transform-origin: center; transition: color 160ms ease; }
.workspace-recent-ended.is-failed { color: light-dark(#d17a7a, #e08a8a); }
.workspace-recent-ended.is-cancelled { color: light-dark(#b4b4bb, #6f6f78); }
.workspace-recent-row:hover .workspace-recent-ended.is-failed,
.workspace-recent-row.is-active .workspace-recent-ended.is-failed { color: light-dark(#be5555, #f0a0a0); }
.workspace-recent-row:hover .workspace-recent-ended.is-cancelled,
.workspace-recent-row.is-active .workspace-recent-ended.is-cancelled { color: light-dark(#71717a, #a1a1aa); }
.workspace-recent-row:hover .workspace-recent-ended { animation: workspace-recent-ended-sway 1600ms linear 1; }
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
