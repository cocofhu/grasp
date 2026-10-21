<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import InboxMotionMark from '@/components/inbox/InboxMotionMark.vue'
import RecentReorderList from './RecentReorderList.vue'
import { api } from '@/lib/api/api'
import { useWorkspaceContext } from '@/lib/composables/useWorkspaceContext'
import { displayRunTitle } from '@/lib/run/runTitle'
import type { Run } from '@/lib/shared/types'
import {
  RECENT_POLL_MS,
  createRecentHold,
  onRecentActivity,
  recentRunActive,
  recentRunMark,
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
    const result = await api.listRuns({ page: 1, pageSize: 6, sort: 'last_activity', order: 'desc', signal: controller.signal })
    if (runsRequest !== controller) return
    if (!Array.isArray(result?.items)) throw new Error('Invalid run list')
    applyRuns(result.items.slice(0, 6))
    runsFailed.value = false
  } catch {
    if (runsRequest === controller) runsFailed.value = true
  } finally { if (runsRequest === controller) runsRequest = null }
}
function markMotion(run: Run) {
  const m = recentRunMark(run)
  return m && 'motion' in m ? m.motion : null
}
function markIcon(run: Run) {
  const m = recentRunMark(run)
  return m && 'icon' in m ? m.icon : null
}
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
      <h3 class="workspace-list-heading"><RouterLink to="/runs" @click="emit('navigate')">{{ t('nav.recent') }}</RouterLink></h3>
      <div v-if="runsFailed" class="workspace-list-state" role="alert"><span>{{ t('nav.workspaceLoadFailed') }}</span><button type="button" @click="loadRuns">{{ t('nav.workspaceRetry') }}</button></div>
      <p v-else-if="!runs.length" class="workspace-list-state">{{ t('nav.workspaceNoRuns') }}</p>
      <RecentReorderList v-else ref="recentListEl">
        <RouterLink v-for="run in runs" :key="run.id" :to="recentRunTarget(run)" class="workspace-recent-row" :class="{ 'is-active': recentRunActive(run, route) }" :aria-current="recentRunActive(run, route) ? 'page' : undefined" :title="runTitle(run)" data-testid="sidebar-recent-run" :data-run-id="run.id" :data-kind="run.currentNode?.kind" :data-state="run.currentNode?.state" @click="emit('navigate')"><Icon name="chat" :size="14" /><span class="workspace-list-title">{{ runTitle(run) }}</span><InboxMotionMark v-if="markMotion(run)" :kind="markMotion(run)!" /><Icon v-else-if="markIcon(run)" :name="markIcon(run)!.name" :size="14" class="nav-row-type-icon" :class="markIcon(run)!.toneClass" data-testid="sidebar-recent-type-icon" aria-hidden="true" /></RouterLink>
      </RecentReorderList>
    </section>
  </div>
</template>

<style scoped>
.workspace-lists { min-width: 0; font-size: 13px; }
.workspace-list-heading { display: flex; align-items: center; justify-content: space-between; margin: 0 0 8px; padding: 0 8px; font-size: 11px; font-weight: 600; color: rgb(var(--c-txt3)); }
.workspace-list-heading > a:first-child { display: flex; align-items: center; gap: 7px; min-width: 0; min-height: 30px; }
.workspace-list-title { min-width: 0; flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 12px; }
.workspace-list-state { display: flex; flex-wrap: wrap; gap: 6px; padding: 9px 8px; color: rgb(var(--c-txt3)); font-size: 11px; }
.workspace-list-state button { color: rgb(var(--c-accent-2)); }
.workspace-recent-row { display: flex; align-items: center; gap: 9px; min-height: 35px; padding: 7px 8px; border-radius: 8px; color: rgb(var(--c-txt3)); }
.workspace-recent-row:hover, .workspace-recent-row.is-active { color: rgb(var(--c-txt)); background: rgb(var(--c-txt) / .035); }
/* Same values as the 待审批 row marks (AppSidebarNav .nav-row-type-icon). */
.nav-row-type-icon { flex: none; color: light-dark(#7b718c, #b7abc9); stroke-width: 1.6; }
.nav-row-type-icon--approval { color: light-dark(#947a49, #c8b185); }
a:focus-visible, button:focus-visible { outline: 2px solid rgb(var(--c-accent) / .6); outline-offset: -2px; }
@media (prefers-reduced-motion: reduce) { * { animation: none !important; transition: none !important; } }
</style>
