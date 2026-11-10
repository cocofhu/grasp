<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api/api'
import { soloWorkflowClient, type SoloWorkflowTask as Task } from '@/lib/api/clients/soloWorkflowClient'
import { useVisiblePolling } from '@/lib/composables/useVisiblePolling'
import type { Workflow, ClarifyImage } from '@/lib/shared/types'
import { useWorkflowFeature, setWorkflowFeature } from '@/lib/composables/useWorkflowFeature'
import { seedAskLaunchFields } from '@/lib/run/useWorkflowAskInputs'
import RunLaunchModal, { type InputField } from '@/components/workflow/RunLaunchModal.vue'
import SoloWorkflowInteraction from './SoloWorkflowInteraction.vue'
import SoloWorkflowImport, { type WorkspaceImportDraft } from './SoloWorkflowImport.vue'
import SoloWorkspaceSnapshots from './SoloWorkspaceSnapshots.vue'
import AppModal from '@/components/ui/AppModal.vue'
import AppDrawer from '@/components/ui/AppDrawer.vue'
import AppButton from '@/components/ui/AppButton.vue'
import Icon from '@/components/ui/Icon.vue'
import WorkspaceSaveBanner from '@/components/run/WorkspaceSaveBanner.vue'
import { runDisplayStatus } from '@/lib/run/workspaceSave'

const props = defineProps<{ sessionId: string; sandboxId?: number; busy?: boolean }>()
const emit = defineEmits<{ changed: []; bindingChanged: [] }>()
const { t } = useI18n()
const { enabled } = useWorkflowFeature()
const tasks = ref<Task[]>([])
const error = ref('')
const pickerOpen = ref(false)
const choices = ref<Workflow[]>([])
const query = ref('')
const loadingChoices = ref(false)
const launch = ref<Workflow | null>(null)
const fields = ref<InputField[]>([])
const inputs = ref<Record<string, string>>({})
const images = ref<Record<string, ClarifyImage[]>>({})
const selectedTask = ref<Task | null>(null)
const selectedNode = ref('')
const importTask = ref<Task | null>(null)
const snapshotTask = ref<Task | null>(null)
const importDrafts = ref<Record<string, WorkspaceImportDraft | null>>({})
const interactionDrafts = ref<Record<string, string>>({})
const instructionRequests = ref<Record<string, { clientMessageId: string; text: string; generation: string; iteration: number; attempt?: number } | null>>({})
const cancelling = ref('')
const confirmCancel = ref<Task | null>(null)
const matches = computed(() => choices.value.filter(workflow => workflow.name.toLowerCase().includes(query.value.toLowerCase())))
let disposed = false
let loading = false
async function refresh() {
  if (loading || disposed) return
  loading = true
  try {
    const value = await soloWorkflowClient.listSessionWorkflows(props.sessionId)
    if (disposed) return
    tasks.value = value.items
    setWorkflowFeature(value.workflows)
    error.value = ''
  } catch (cause) { if (!disposed) error.value = cause instanceof Error ? cause.message : String(cause) }
  finally { loading = false }
}
async function openPicker(search = '') {
  if (!enabled.value) { error.value = t('pages.solo.workflows.disabled'); return }
  pickerOpen.value = true
  query.value = search
  loadingChoices.value = true
  try { choices.value = (await api.listWorkflows()).filter(workflow => (workflow.publishedVersion || 0) > 0) }
  catch (cause) { error.value = cause instanceof Error ? cause.message : String(cause) }
  finally { loadingChoices.value = false }
}
async function select(workflow: Workflow) {
  try {
    const graph = workflow.publishedSnapshot || await api.getWorkflowVersionGraph(workflow.id, workflow.publishedVersion!)
    const frozen = { ...workflow, ...graph }
    const seeded = await seedAskLaunchFields(frozen)
    fields.value = seeded.fields
    inputs.value = seeded.inputs
    images.value = seeded.images
    launch.value = frozen
    pickerOpen.value = false
  } catch (cause) { error.value = cause instanceof Error ? cause.message : String(cause) }
}
function interact(task: Task, nodeId = '') { selectedTask.value = task; selectedNode.value = nodeId }
function onStarted() { void refresh(); emit('changed') }
async function control(task: Task, resume: boolean) {
  try {
    await soloWorkflowClient.controlRun(task.id, resume, task.controlVersion || 0)
    await refresh()
  } catch (cause) { error.value = cause instanceof Error ? cause.message : String(cause) }
}
async function cancelTask() {
  const task = confirmCancel.value
  if (!task) return
  cancelling.value = task.id
  try { await api.cancelRun(task.id); confirmCancel.value = null; await refresh(); emit('changed') }
  catch (cause) { error.value = cause instanceof Error ? cause.message : String(cause) }
  finally { cancelling.value = '' }
}
function terminal(status: string) { return ['completed', 'failed', 'cancelled'].includes(status) }
useVisiblePolling(() => refresh(), 2500)
onBeforeUnmount(() => { disposed = true })
defineExpose({ openPicker, refresh })
</script>

<template>
  <section v-if="tasks.length || error" class="space-y-3" data-testid="solo-workflows">
    <div class="flex items-center gap-2 text-xs text-txt3"><Icon name="workflow" :size="14" /><span>{{ t('pages.solo.workflows.title') }}</span><span v-if="tasks.length" class="rounded bg-elevated px-1.5">{{ tasks.length }}</span></div>
    <p v-if="error" class="text-xs text-err" role="alert">{{ error }}</p>
    <article v-for="task in tasks" :key="task.id" class="rounded-xl border border-line bg-surface px-4 py-3 shadow-sm" :data-testid="`solo-workflow-${task.id}`">
      <div class="flex min-w-0 items-center gap-2"><span class="rounded bg-accent/10 px-1.5 py-0.5 font-mono text-[10px] text-accent">{{ task.id }}</span><strong class="min-w-0 flex-1 truncate text-[13px] text-txt">{{ task.title || task.workflowName }}</strong><span class="shrink-0 text-xs" :class="['failed', 'save_failed'].includes(runDisplayStatus(task)) ? 'text-err' : task.status === 'completed' ? 'text-ok' : 'text-accent'" data-testid="solo-workflow-status">{{ t(`common.status.${runDisplayStatus(task)}`) }}</span></div>
      <WorkspaceSaveBanner v-if="task.workspaceSave" class="mt-3" :run="task" :save="task.workspaceSave" :node-label="task.nodes.find(node => node.id === task.workspaceSave?.nodeId)?.label" @retried="refresh" />
      <div class="mt-4 flex overflow-x-auto pb-1">
        <div v-for="(node, index) in task.nodes" :key="node.id" class="relative flex min-w-[64px] flex-1 flex-col items-center gap-1.5">
          <span v-if="index" class="absolute right-1/2 top-2.5 h-px w-full bg-line" />
          <button type="button" class="relative z-[1] flex h-5 w-5 items-center justify-center rounded-full border border-line bg-surface text-txt3" :class="{ '!border-ok !bg-ok !text-white': node.status === 'completed', '!border-accent !text-accent': ['running', 'waiting_human'].includes(node.status), '!border-err !text-err': node.status === 'failed' }" :aria-label="`${node.label} · ${node.status}`" @click="interact(task, node.type === 'human_gate' ? '' : node.id)"><Icon v-if="node.status === 'completed'" name="check" :size="12" /><Icon v-else-if="node.status === 'running'" name="spinner" :size="12" /><span v-else class="h-1 w-1 rounded-full bg-current" /></button>
          <span class="max-w-[110px] truncate px-1 text-[11px] text-txt3">{{ node.label || node.id }}</span>
        </div>
      </div>
      <div class="mt-3 flex flex-wrap items-center justify-end gap-2">
        <span v-if="task.pauseRequested" class="mr-auto text-[11px] text-txt3">{{ t('pages.solo.workflows.pausePending') }}</span>
        <AppButton v-if="!terminal(task.status) && !task.workspaceSave" size="sm" variant="ghost" @click="control(task, task.status === 'paused' || !!task.pauseRequested)">{{ t(task.status === 'paused' || task.pauseRequested ? 'pages.solo.workflows.resume' : 'pages.solo.workflows.pause') }}</AppButton>
        <AppButton v-if="task.status === 'waiting_human'" size="sm" variant="primary" @click="interact(task)">{{ t('pages.solo.workflows.handle') }}</AppButton>
        <AppButton size="sm" variant="ghost" @click="interact(task)">{{ t('pages.solo.workflows.conversation') }}</AppButton>
        <AppButton v-if="terminal(task.status)" size="sm" variant="ghost" data-testid="workflow-import-code" @click="importTask = task">{{ t('pages.solo.workspaceImport.open') }}</AppButton>
        <AppButton size="sm" variant="ghost" data-testid="workflow-file-snapshots" @click="snapshotTask = task">{{ t('workspaceSnapshots.history') }}</AppButton>
        <RouterLink :to="`/runs/${encodeURIComponent(task.id)}?view=details`" class="text-xs text-txt3 hover:text-accent">{{ t('pages.solo.workflows.details') }}</RouterLink>
        <AppButton v-if="!terminal(task.status)" size="sm" variant="ghost" :loading="cancelling === task.id" @click="confirmCancel = task">{{ t('common.buttons.cancel') }}</AppButton>
      </div>
    </article>
  </section>
    <AppModal :open="pickerOpen" :title="t('pages.solo.workflows.choose')" :width="520" close-on-esc @close="pickerOpen = false">
      <input v-model="query" class="input mb-3 w-full" :placeholder="t('pages.solo.workflows.search')" :aria-label="t('pages.solo.workflows.search')" />
      <p class="mb-4 text-xs leading-5 text-txt3">{{ t('pages.solo.workflows.launchHint') }}</p>
      <div v-if="loadingChoices" class="p-5 text-center text-txt3"><Icon name="spinner" :size="16" /></div>
      <div v-else class="max-h-[420px] space-y-2 overflow-y-auto"><button v-for="workflow in matches" :key="workflow.id" type="button" class="flex w-full items-center gap-3 rounded-lg border border-line px-3 py-3 text-left hover:border-accent/50" @click="select(workflow)"><Icon name="workflow" :size="16" class="text-accent" /><span class="min-w-0 flex-1"><strong class="block truncate text-sm">{{ workflow.publishedSnapshot?.name || workflow.name }}</strong><span class="block truncate text-xs text-txt3">{{ workflow.publishedSnapshot?.description || workflow.description }}</span></span><span class="text-xs text-txt3">v{{ workflow.publishedVersion }}</span></button><p v-if="!matches.length" class="p-5 text-center text-xs text-txt3">{{ t('pages.solo.workflows.empty') }}</p></div>
    </AppModal>
    <RunLaunchModal v-if="launch" :open="!!launch" :solo-session-id="props.sessionId" :workflow-id="launch.id" :workflow-name="launch.name" :workflow-version="launch.publishedVersion" :fields="fields" :run-inputs="inputs" :run-images="images" published-snapshot @close="launch = null" @started="onStarted" />
    <AppDrawer :open="!!selectedTask" :title="selectedTask ? `${selectedTask.id} · ${selectedTask.title || selectedTask.workflowName}` : ''" :width="760" @close="selectedTask = null"><SoloWorkflowInteraction v-if="selectedTask" :key="`${selectedTask.id}:${selectedNode}`" v-model:draft="interactionDrafts[`${selectedTask.id}:${selectedNode}`]" v-model:instruction-request="instructionRequests[`${selectedTask.id}:${selectedNode}`]" :run-id="selectedTask.id" :node-id="selectedNode" /></AppDrawer>
    <AppDrawer :open="!!importTask" :title="t('pages.solo.workspaceImport.title')" :width="620" @close="importTask = null"><SoloWorkflowImport v-if="importTask" :key="`${props.sessionId}:${importTask.id}`" v-model:draft="importDrafts[importTask.id]" :session-id="props.sessionId" :run-id="importTask.id" :nodes="importTask.nodes" @changed="emit('changed')" /></AppDrawer>
    <AppModal :open="!!snapshotTask" :title="t('workspaceSnapshots.workflowHistory')" :width="560" :close-label="t('common.buttons.close')" close-on-esc trap-focus @close="snapshotTask = null"><SoloWorkspaceSnapshots v-if="snapshotTask" :key="`${props.sessionId}:${snapshotTask.id}`" :session-id="props.sessionId" :run-id="snapshotTask.id" :nodes="snapshotTask.nodes" :sandbox-id="props.sandboxId" :busy="props.busy" @changed="emit('changed')" @binding-changed="emit('bindingChanged')" /></AppModal>
    <AppModal :open="!!confirmCancel" :title="t('pages.solo.workflows.cancelTitle')" :width="420" close-on-esc @close="confirmCancel = null"><p class="text-sm text-txt2">{{ t('pages.solo.workflows.cancelBody', { task: confirmCancel?.title || confirmCancel?.id }) }}</p><template #footer><AppButton variant="ghost" @click="confirmCancel = null">{{ t('common.buttons.back') }}</AppButton><AppButton variant="primary" :loading="!!cancelling" @click="cancelTask">{{ t('pages.solo.workflows.confirmCancel') }}</AppButton></template></AppModal>
</template>
