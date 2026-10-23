<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import Icon from '@/components/ui/Icon.vue'
import { useI18n } from 'vue-i18n'
import RecentReorderList from './RecentReorderList.vue'
import { isSectionCollapsed, toggleSection } from '@/lib/shared/sidebarSections'
import { api } from '@/lib/api/api'
import { useWorkspaceContext } from '@/lib/composables/useWorkspaceContext'
import { displayRunTitle } from '@/lib/run/runTitle'
import type { Run } from '@/lib/shared/types'
import { HISTORY_RUN_STATUSES } from '@/lib/inbox/sidebarRunRows'
import {
  RECENT_POLL_MS,
  createRecentHold,
  onRecentActivity,
  recentRunActive,
  recentRunTarget,
  sameRecentRows,
} from '@/lib/run/recentRuns'

const props = defineProps<{ runTitles?: Record<string, string> }>()
const emit = defineEmits<{ (e: 'navigate'): void }>()
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

async function loadRuns() {
  runsRequest?.abort()
  const controller = new AbortController()
  runsRequest = controller
  try {
    if (!await ensureLoaded() || runsRequest !== controller) return
    const result = await api.listRuns({
      page: 1,
      pageSize: 6,
      status: HISTORY_RUN_STATUSES,
      sort: 'last_activity',
      order: 'desc',
      signal: controller.signal,
    })
    if (runsRequest !== controller) return
    if (!Array.isArray(result?.items)) throw new Error('Invalid run list')
    applyRuns(result.items.slice(0, 6))
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
function flushHeldRuns() {
  if (heldRuns && !recentHold.isHeld()) applyRuns(heldRuns)
}
watch(recentListEl, (comp) => {
  const el = comp?.$el
  if (el instanceof HTMLElement) recentHold.attach(el, flushHeldRuns)
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
      <h3 class="workspace-list-heading grasp-status-host"><RouterLink to="/runs" class="grasp-status-host" data-testid="sidebar-recent-heading" @click="emit('navigate')"><Icon name="history-back" :size="14" class="workspace-list-heading-icon grasp-status" aria-hidden="true" /><span>{{ t('nav.runHistory') }}</span></RouterLink><button type="button" class="workspace-list-toggle" data-testid="sidebar-recent-toggle" :aria-expanded="!historyCollapsed" :aria-label="t(historyCollapsed ? 'nav.sectionExpand' : 'nav.sectionCollapse', { name: t('nav.runHistory') })" @click="toggleSection('history')"><Icon name="chevron-down" :size="12" class="workspace-list-chevron" :class="{ 'is-collapsed': historyCollapsed }" aria-hidden="true" /></button></h3>
      <div v-show="!historyCollapsed" data-testid="sidebar-recent-body">
      <div v-if="runsFailed" class="workspace-list-state" role="alert"><span>{{ t('nav.workspaceLoadFailed') }}</span><button type="button" @click="loadRuns">{{ t('nav.workspaceRetry') }}</button></div>
      <p v-else-if="!runs.length" class="workspace-list-state">{{ t('nav.workspaceNoRuns') }}</p>
      <div v-else class="sidebar-tree">
        <RecentReorderList ref="recentListEl">
          <div v-for="run in runs" :key="run.id" class="sidebar-tree-item"><RouterLink :to="recentRunTarget(run)" class="workspace-recent-row" :class="{ 'is-active': recentRunActive(run, route) }" :aria-current="recentRunActive(run, route) ? 'page' : undefined" :title="runTitle(run)" data-testid="sidebar-recent-run" :data-run-id="run.id" :data-status="run.status" @click="emit('navigate')"><span class="workspace-list-title">{{ runTitle(run) }}</span><svg v-if="run.status === 'failed'" class="workspace-recent-ended is-failed" data-testid="sidebar-recent-ended" width="14" height="14" viewBox="0 0 14 14" fill="none" aria-hidden="true"><title>{{ t('common.status.failed') }}</title><circle cx="7" cy="7" r="5.6" stroke="currentColor" stroke-width="1.3" /><path d="M7 4.2v3.4" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" /><circle cx="7" cy="9.8" r=".8" fill="currentColor" /></svg><span v-else-if="run.status === 'cancelled'" class="workspace-recent-ended is-cancelled" data-testid="sidebar-recent-ended" :title="t('common.status.cancelled')" aria-hidden="true" /></RouterLink></div>
        </RecentReorderList>
      </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.workspace-lists { min-width: 0; font-size: 13px; }
.workspace-list-heading { display: flex; align-items: center; justify-content: flex-start; gap: 1px; margin: 0 0 4px; padding: 0 10px; font-size: 11.5px; font-weight: 600; color: color-mix(in srgb, rgb(var(--c-txt2)) 70%, rgb(var(--c-txt3))); }
.workspace-list-heading > a:first-child { display: flex; align-items: center; gap: 6px; min-width: 0; min-height: 30px; transition: color 160ms ease; }
.workspace-list-heading > a:first-child:hover { color: rgb(var(--c-txt)); }
.workspace-list-heading-icon { flex: none; }
.workspace-list-toggle { display: inline-flex; align-items: center; justify-content: center; width: 18px; height: 18px; border-radius: 5px; color: inherit; }
.workspace-list-toggle:hover { color: rgb(var(--c-txt)); background: rgb(var(--c-txt) / .05); }
.workspace-list-chevron { opacity: .7; transition: transform 160ms ease, opacity 160ms ease; }
.workspace-list-toggle:hover .workspace-list-chevron { opacity: 1; }
.workspace-list-chevron.is-collapsed { transform: rotate(-90deg); }
.workspace-list-title { min-width: 0; flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 13px; }
.workspace-list-state { display: flex; flex-wrap: wrap; gap: 6px; padding: 9px 8px; color: rgb(var(--c-txt3)); font-size: 11px; }
.workspace-list-state button { color: rgb(var(--c-accent-2)); }
.workspace-recent-row { display: flex; align-items: center; gap: 8px; min-height: 32px; padding: 6px 10px; border-radius: 8px; color: rgb(var(--c-txt2)); }
.sidebar-tree-item > .workspace-recent-row { padding-left: 6px; }
.workspace-recent-row:hover { color: rgb(var(--c-txt)); background: rgb(var(--c-txt) / .04); }
.workspace-recent-row.is-active, .workspace-recent-row.is-active:hover { color: rgb(var(--c-txt)); background: light-dark(#f1f1f1, rgb(var(--c-txt) / .065)); }
.workspace-recent-ended { flex: none; }
.workspace-recent-ended.is-failed { color: light-dark(#d17a7a, #e08a8a); }
.workspace-recent-ended.is-cancelled { width: 6px; height: 6px; margin-inline: 4px; border-radius: 9999px; background: light-dark(#b4b4bb, #6f6f78); }
a:focus-visible, button:focus-visible { outline: 2px solid rgb(var(--c-accent) / .6); outline-offset: -2px; }
@media (prefers-reduced-motion: reduce) { * { animation: none !important; transition: none !important; } }
</style>
