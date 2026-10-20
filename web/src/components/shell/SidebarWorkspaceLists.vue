<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import { api } from '@/lib/api/api'
import { useWorkspaceContext } from '@/lib/composables/useWorkspaceContext'
import { displayRunTitle } from '@/lib/run/runTitle'
import type { Run } from '@/lib/shared/types'

const props = defineProps<{ runTitles?: Record<string, string> }>()
const emit = defineEmits<{ (e: 'navigate'): void }>()
const { t } = useI18n()
const route = useRoute()
const context = useWorkspaceContext()
const { workspace, ensureLoaded } = context
const workspaceReady = computed(() => !!workspace.value)
const runs = ref<Run[]>([])
const runsFailed = ref(false)
let runsRequest: AbortController | null = null
const runPath = (id: string) => `/runs/${encodeURIComponent(id)}`
const runTitle = (run: Run) => displayRunTitle(props.runTitles?.[run.id] ?? run.title) || run.workflowName || run.id

async function loadRuns() {
  runsRequest?.abort()
  const controller = new AbortController()
  runsRequest = controller
  try {
    if (!await ensureLoaded() || runsRequest !== controller) return
    const result = await api.listRuns({ page: 1, pageSize: 6, sort: 'started_at', order: 'desc', signal: controller.signal })
    if (runsRequest !== controller) return
    if (!Array.isArray(result?.items)) throw new Error('Invalid run list')
    runs.value = result.items.slice(0, 6)
    runsFailed.value = false
  } catch {
    if (runsRequest === controller) runsFailed.value = true
  } finally { if (runsRequest === controller) runsRequest = null }
}

watch([workspaceReady, () => route.path], () => {
  if (workspaceReady.value) void loadRuns()
  else { runsRequest?.abort(); runsRequest = null; runs.value = []; runsFailed.value = false }
}, { immediate: true })
onBeforeUnmount(() => { runsRequest?.abort() })
</script>

<template>
  <div class="workspace-lists" data-testid="sidebar-workspace-lists">
    <section class="workspace-recents" data-testid="sidebar-recent">
      <h3 class="workspace-list-heading"><RouterLink to="/runs" @click="emit('navigate')">{{ t('nav.recent') }}</RouterLink></h3>
      <div v-if="runsFailed" class="workspace-list-state" role="alert"><span>{{ t('nav.workspaceLoadFailed') }}</span><button type="button" @click="loadRuns">{{ t('nav.workspaceRetry') }}</button></div>
      <p v-else-if="!runs.length" class="workspace-list-state">{{ t('nav.workspaceNoRuns') }}</p>
      <RouterLink v-for="run in runs" :key="run.id" :to="runPath(run.id)" class="workspace-recent-row" :class="{ 'is-active': route.path === runPath(run.id) }" :aria-current="route.path === runPath(run.id) ? 'page' : undefined" :title="runTitle(run)" data-testid="sidebar-recent-run" @click="emit('navigate')"><Icon name="chat" :size="14" /><span class="workspace-list-title">{{ runTitle(run) }}</span></RouterLink>
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
a:focus-visible, button:focus-visible { outline: 2px solid rgb(var(--c-accent) / .6); outline-offset: -2px; }
@media (prefers-reduced-motion: reduce) { * { animation: none !important; transition: none !important; } }
</style>
