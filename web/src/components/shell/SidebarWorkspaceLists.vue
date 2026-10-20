<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import { api, type WorkflowCatalogItem } from '@/lib/api/api'
import { workflowCatalogRevision } from '@/lib/api/workflowCatalogEvents'
import { useWorkspaceContext } from '@/lib/composables/useWorkspaceContext'
import { useWorkflowFavorites } from '@/lib/run/useWorkflowFavorites'
import { useWorkflowRunLaunch } from '@/lib/run/useWorkflowRunLaunch'
import { useToast } from '@/lib/composables/useToast'
import { displayRunTitle } from '@/lib/run/runTitle'
import type { Run } from '@/lib/shared/types'

const props = defineProps<{ runTitles?: Record<string, string> }>()
const emit = defineEmits<{ (e: 'navigate'): void }>()
const { t } = useI18n()
const route = useRoute()
const toast = useToast()
const context = useWorkspaceContext()
const { workspace, ensureLoaded } = context
const workspaceReady = computed(() => !!workspace.value)
const { entries, isFavorite, toggleFavorite } = useWorkflowFavorites()
const { openLaunch } = useWorkflowRunLaunch()
const workflows = ref<WorkflowCatalogItem[]>([])
const runs = ref<Run[]>([])
const loading = ref(false)
const loadFailed = ref(false)
const runsFailed = ref(false)
const launchingId = ref('')
const catalogBusy = computed(() => loading.value || (!workspaceReady.value && !!context.loading?.value))
const catalogFailed = computed(() => loadFailed.value || !!context.error?.value)
let request: AbortController | null = null
let runsRequest: AbortController | null = null
let launchGeneration = 0
const favorites = computed(() => new Map(entries.value.map((entry, index) => [entry.workflowId, index])))
const orderedWorkflows = computed(() => {
  return workflows.value
    .slice().sort((a, b) => (favorites.value.get(a.id) ?? Infinity) - (favorites.value.get(b.id) ?? Infinity) || b.updatedAt.localeCompare(a.updatedAt) || a.id.localeCompare(b.id))
})
const duplicates = computed(() => {
  const counts = new Map<string, number>()
  for (const item of workflows.value) counts.set(item.name, (counts.get(item.name) || 0) + 1)
  return counts
})
const editorPath = (id: string) => `/workflows/${encodeURIComponent(id)}/edit`
const runPath = (id: string) => `/runs/${encodeURIComponent(id)}`
const runTitle = (run: Run) => displayRunTitle(props.runTitles?.[run.id] ?? run.title) || run.workflowName || run.id
function workflowActive(id: string) {
  return route.path === editorPath(id) || ((route.path === '/runs' || route.path === '/gates') && route.query.wf === id)
}
async function loadCatalog() {
  request?.abort()
  const controller = new AbortController()
  request = controller
  loading.value = true
  loadFailed.value = false
  try {
    if (!await ensureLoaded() || request !== controller) return
    const result = await api.listWorkflowCatalog({ signal: controller.signal })
    if (request !== controller) return
    if (!Array.isArray(result)) throw new Error('Invalid workflow directory')
    workflows.value = result
  } catch {
    if (request === controller) loadFailed.value = true
  } finally {
    if (request === controller) { loading.value = false; request = null }
  }
}
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
async function launch(item: WorkflowCatalogItem) {
  const current = ++launchGeneration
  launchingId.value = item.id
  try {
    // The directory has no graph. Manual launches always load and run the saved head.
    const workflow = await api.getWorkflow(item.id)
    if (current !== launchGeneration) return
    await openLaunch(workflow)
    if (current === launchGeneration) emit('navigate')
  } catch (cause: unknown) {
    if (current === launchGeneration) toast.error(cause instanceof Error ? cause.message : t('nav.workspaceLoadFailed'))
  } finally { if (current === launchGeneration) launchingId.value = '' }
}
watch([workspaceReady, workflowCatalogRevision], () => {
  if (workspaceReady.value) void loadCatalog()
  else { request?.abort(); request = null; runsRequest?.abort(); runsRequest = null; workflows.value = []; runs.value = []; loading.value = false; loadFailed.value = false; runsFailed.value = false; launchGeneration++; launchingId.value = '' }
}, { immediate: true })
watch([workspaceReady, () => route.path], () => { if (workspaceReady.value) void loadRuns() }, { immediate: true })
onBeforeUnmount(() => { request?.abort(); runsRequest?.abort(); launchGeneration++ })
</script>

<template>
  <div class="workspace-lists" data-testid="sidebar-workspace-lists">
    <section data-testid="sidebar-workflows">
      <div class="workspace-list-heading">
        <RouterLink to="/workflows" @click="emit('navigate')">{{ t('nav.workflows') }} <span class="workspace-count">{{ workflows.length }}</span></RouterLink>
        <RouterLink class="workspace-heading-action" to="/workflows/new/edit" :aria-label="t('common.buttons.newWorkflow')" :title="t('common.buttons.newWorkflow')" @click="emit('navigate')"><Icon name="plus" :size="15" /></RouterLink>
      </div>
      <div v-if="catalogFailed" class="workspace-list-state" role="alert"><span>{{ t('nav.workspaceLoadFailed') }}</span><button type="button" @click="loadCatalog">{{ t('nav.workspaceRetry') }}</button></div>
      <div v-if="catalogBusy && !workflows.length" class="workspace-catalog-skeleton" role="status"><span class="sr-only">{{ t('nav.workspaceLoading') }}</span><i v-for="index in 3" :key="index" /></div>
      <div v-else-if="workspaceReady && !catalogFailed && !workflows.length" class="workspace-empty"><Icon name="workflow" :size="24" /><p>{{ t('nav.workspaceNoWorkflows') }}</p><RouterLink to="/workflows" @click="emit('navigate')">{{ t('common.buttons.newWorkflow') }}</RouterLink></div>
      <div class="workspace-workflow-list scroll-area" :aria-busy="catalogBusy" data-testid="sidebar-workflow-list">
        <div v-for="workflow in orderedWorkflows" :key="workflow.id" class="workspace-workflow-row" :class="{ 'is-active': workflowActive(workflow.id), 'is-pinned': isFavorite(workflow.id) }" data-testid="sidebar-workflow">
          <RouterLink :to="editorPath(workflow.id)" class="workspace-workflow-link" :aria-current="workflowActive(workflow.id) ? 'page' : undefined" :title="workflow.description ? `${workflow.name}\n${workflow.description}` : workflow.name" @click="emit('navigate')">
            <Icon name="workflow" :size="15" class="workflow-icon" />
            <span class="workspace-workflow-label"><span class="workspace-list-title">{{ workflow.name }}</span><span class="workspace-workflow-meta"><span v-if="workflow.status === 'draft'" class="workspace-draft">{{ t('common.status.draft') }}</span><span v-if="(duplicates.get(workflow.name) || 0) > 1">{{ workflow.id.slice(-8) }}</span><span v-if="isFavorite(workflow.id)" class="sr-only">{{ t('nav.pendingPin') }}</span></span></span>
          </RouterLink>
          <div class="workspace-row-actions">
            <button type="button" class="workspace-row-action" :class="{ 'is-pinned': isFavorite(workflow.id) }" :aria-label="t(isFavorite(workflow.id) ? 'nav.unpinWorkflow' : 'nav.pinWorkflow', { name: workflow.name })" :title="t(isFavorite(workflow.id) ? 'nav.unpinWorkflow' : 'nav.pinWorkflow', { name: workflow.name })" :aria-pressed="isFavorite(workflow.id)" @click="toggleFavorite(workflow.id, { name: workflow.name })"><Icon :name="isFavorite(workflow.id) ? 'star-filled' : 'star'" :size="13" /></button>
            <button type="button" class="workspace-row-action workspace-row-play" :disabled="!!launchingId" :aria-label="t('nav.runWorkflow', { name: workflow.name })" :title="t('nav.runWorkflow', { name: workflow.name })" data-testid="sidebar-workflow-run" @click="launch(workflow)"><Icon :name="launchingId === workflow.id ? 'spinner' : 'play'" :size="13" :class="{ 'animate-spin': launchingId === workflow.id }" /></button>
          </div>
        </div>
      </div>
    </section>
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
.workspace-count { font-size: 10px; font-variant-numeric: tabular-nums; font-weight: 400; opacity: .7; }
.workspace-heading-action { display: flex; height: 28px; width: 28px; align-items: center; justify-content: center; border-radius: 7px; }
.workspace-heading-action:hover { background: rgb(var(--c-txt) / .05); color: rgb(var(--c-txt)); }
.workspace-workflow-list { max-height: min(42vh, 440px); overflow-y: auto; overscroll-behavior: contain; padding: 1px 0; }
.workspace-workflow-row { display: flex; align-items: center; min-height: 35px; margin: 2px 0; border-radius: 8px; color: rgb(var(--c-txt2)); transition: background-color 120ms ease; }
.workspace-workflow-row:hover { background: rgb(var(--c-txt) / .035); }
.workspace-workflow-row.is-active { background: rgb(var(--c-txt) / .055); color: rgb(var(--c-txt)); }
.workspace-workflow-link { display: flex; flex: 1; align-items: center; gap: 9px; min-width: 0; padding: 7px 8px; }
.workflow-icon { flex-shrink: 0; opacity: .65; }
.workspace-workflow-label { display: flex; min-width: 0; flex: 1; align-items: center; gap: 6px; }
.workspace-list-title { min-width: 0; flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 12px; }
.workspace-workflow-meta { flex-shrink: 0; display: flex; align-items: center; gap: 6px; font-size: 9px; color: rgb(var(--c-txt3)); }
.workspace-workflow-meta:empty { display: none; }
.workspace-draft { color: rgb(var(--c-warn)); font-size: 9px; line-height: 1.2; }
.workspace-row-actions { display: flex; flex-shrink: 0; gap: 1px; padding-right: 3px; }
.workspace-row-action { display: flex; height: 28px; width: 24px; align-items: center; justify-content: center; border-radius: 6px; color: rgb(var(--c-txt3)); opacity: 0; }
.workspace-workflow-row:hover .workspace-row-action, .workspace-workflow-row:focus-within .workspace-row-action, .workspace-row-action.is-pinned { opacity: 1; }
.workspace-row-action.is-pinned { color: rgb(var(--c-warn)); }
.workspace-row-action:hover { background: rgb(var(--c-txt) / .065); color: rgb(var(--c-txt)); }
.workspace-row-action:disabled { opacity: .4; }
.workspace-list-state { display: flex; flex-wrap: wrap; gap: 6px; padding: 9px 8px; color: rgb(var(--c-txt3)); font-size: 11px; }
.workspace-list-state button, .workspace-empty a { color: rgb(var(--c-accent-2)); }
.workspace-empty { display: flex; flex-direction: column; align-items: center; gap: 8px; padding: 22px 8px; color: rgb(var(--c-txt3)); font-size: 11px; text-align: center; }
.workspace-catalog-skeleton { display: flex; flex-direction: column; gap: 8px; padding: 8px; }
.workspace-catalog-skeleton i { height: 32px; border-radius: 6px; background: rgb(var(--c-txt) / .045); animation: pulse 1.5s infinite; }
.workspace-recents { margin-top: 22px; }
.workspace-recent-row { display: flex; align-items: center; gap: 9px; min-height: 35px; padding: 7px 8px; border-radius: 8px; color: rgb(var(--c-txt3)); }
.workspace-recent-row:hover, .workspace-recent-row.is-active { color: rgb(var(--c-txt)); background: rgb(var(--c-txt) / .035); }
a:focus-visible, button:focus-visible { outline: 2px solid rgb(var(--c-accent) / .6); outline-offset: -2px; }
@media (hover: none), (max-width: 767px) { .workspace-row-action { opacity: 1; min-width: 36px; min-height: 44px; } .workspace-workflow-link { min-height: 44px; }  }
@media (prefers-reduced-motion: reduce) { * { animation: none !important; transition: none !important; } }
</style>
