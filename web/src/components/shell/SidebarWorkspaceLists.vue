<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import RecentReorderList from './RecentReorderList.vue'
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
      <h3 class="workspace-list-heading"><RouterLink to="/runs" @click="emit('navigate')">{{ t('nav.runHistory') }}</RouterLink></h3>
      <div v-if="runsFailed" class="workspace-list-state" role="alert"><span>{{ t('nav.workspaceLoadFailed') }}</span><button type="button" @click="loadRuns">{{ t('nav.workspaceRetry') }}</button></div>
      <p v-else-if="!runs.length" class="workspace-list-state">{{ t('nav.workspaceNoRuns') }}</p>
      <RecentReorderList v-else ref="recentListEl">
        <RouterLink v-for="run in runs" :key="run.id" :to="recentRunTarget(run)" class="workspace-recent-row" :class="{ 'is-active': recentRunActive(run, route) }" :aria-current="recentRunActive(run, route) ? 'page' : undefined" :title="runTitle(run)" data-testid="sidebar-recent-run" :data-run-id="run.id" :data-status="run.status" @click="emit('navigate')"><Icon name="history" :size="16" aria-hidden="true" /><span class="workspace-list-title">{{ runTitle(run) }}</span><span v-if="endedBadly(run)" class="workspace-recent-ended" :class="{ 'is-failed': run.status === 'failed', 'is-cancelled': run.status === 'cancelled' }" data-testid="sidebar-recent-ended" :title="t(`common.status.${run.status}`)" aria-hidden="true" /></RouterLink>
      </RecentReorderList>
    </section>
  </div>
</template>

<style scoped>
.workspace-lists { min-width: 0; font-size: 13px; }
.workspace-list-heading { display: flex; align-items: center; justify-content: space-between; margin: 0 0 8px; padding: 0 8px; font-size: 11px; font-weight: 600; color: color-mix(in srgb, rgb(var(--c-txt2)) 70%, rgb(var(--c-txt3))); }
.workspace-list-heading > a:first-child { display: flex; align-items: center; gap: 7px; min-width: 0; min-height: 30px; }
.workspace-list-title { min-width: 0; flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 12px; }
.workspace-list-state { display: flex; flex-wrap: wrap; gap: 6px; padding: 9px 8px; color: rgb(var(--c-txt3)); font-size: 11px; }
.workspace-list-state button { color: rgb(var(--c-accent-2)); }
.workspace-recent-row { display: flex; align-items: center; gap: 9px; min-height: 35px; padding: 7px 8px; border-radius: 8px; color: rgb(var(--c-txt2)); }
.workspace-recent-row :deep(.app-icon) { flex: none; color: inherit; }
.workspace-recent-row:hover { color: rgb(var(--c-txt)); background: rgb(var(--c-txt) / .04); }
.workspace-recent-row.is-active, .workspace-recent-row.is-active:hover { color: rgb(var(--c-txt)); background: light-dark(#f1f1f1, rgb(var(--c-txt) / .065)); }
.workspace-recent-ended { flex: none; width: 6px; height: 6px; border-radius: 9999px; }
.workspace-recent-ended.is-failed { background: light-dark(#d17a7a, #e08a8a); }
.workspace-recent-ended.is-cancelled { background: light-dark(#b4b4bb, #6f6f78); }
a:focus-visible, button:focus-visible { outline: 2px solid rgb(var(--c-accent) / .6); outline-offset: -2px; }
@media (prefers-reduced-motion: reduce) { * { animation: none !important; transition: none !important; } }
</style>
