<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api/api'
import { ingestDialogues, ingestDialogue } from '@/lib/chat/dialogueStore'
import type { Run, ClarifyImage, ReactAnnotation } from '@/lib/shared/types'
import type { ReferenceInput } from '@/lib/api/apiTypes'
import ClarifyChat from '@/components/run/ClarifyChat.vue'
import RunGatePanel from '@/components/run/RunGatePanel.vue'
import AppButton from '@/components/ui/AppButton.vue'

const props = defineProps<{ runId: string; nodeId?: string }>()
const { t } = useI18n()
const run = ref<Run | null>(null)
const error = ref('')
const draft = ref('')
const attachments = ref<ClarifyImage[]>([])
const annotations = ref<ReactAnnotation[]>([])
const conversation = computed(() => props.nodeId ? run.value?.clarifyByNode?.[props.nodeId] : run.value?.clarify)
const nodeId = computed(() => props.nodeId || conversation.value?.nodeId || '')
const label = computed(() => run.value?.nodes?.find(node => node.id === nodeId.value)?.label || nodeId.value)
const active = computed(() => !!run.value && ['queued', 'running', 'waiting_human'].includes(run.value.status))
let disposed = false
let timer: ReturnType<typeof setInterval> | undefined
let loading = false
async function refresh() {
  if (loading || disposed) return
  loading = true
  try {
    const value = await api.getRun(props.runId)
    if (disposed) return
    run.value = value
    ingestDialogues(value.dialogueByNode, props.runId)
    if (nodeId.value) ingestDialogue(await api.nodeDialogue(props.runId, nodeId.value))
    error.value = ''
  } catch (cause) { if (!disposed) error.value = cause instanceof Error ? cause.message : String(cause) }
  finally { loading = false }
}
async function act(action: () => Promise<unknown>) {
  try { await action(); await refresh() } catch (cause) { error.value = cause instanceof Error ? cause.message : String(cause) }
}
async function send(text: string, images: ClarifyImage[], picks: ReactAnnotation[], clientId?: string, refs: ReferenceInput[] = []) {
  try { await api.reactReply(props.runId, nodeId.value, text, images, false, picks, false, false, clientId, refs); await refresh(); return true }
  catch (cause) { error.value = cause instanceof Error ? cause.message : String(cause); return false }
}
onMounted(() => { void refresh(); timer = setInterval(() => void refresh(), 1500) })
onBeforeUnmount(() => { disposed = true; clearInterval(timer) })
</script>

<template>
  <div class="flex min-h-[420px] flex-col">
    <p class="border-b border-line px-4 py-2 text-xs text-txt3">{{ props.runId }}<template v-if="label"> · {{ label }} · {{ t('pages.solo.workflows.iteration', { n: conversation?.iteration || run?.gate?.iteration || 1 }) }}</template></p>
    <p v-if="error" class="px-4 py-2 text-xs text-err" role="alert">{{ error }}</p>
    <RunGatePanel v-if="run?.gate && !props.nodeId" :gate="run.gate" :run="run" :submit-error="error" @resolve="(action, form) => act(() => api.resumeGate(props.runId, run!.gate!.nodeId, action, form))" />
    <ClarifyChat v-else-if="conversation && nodeId" :key="`${props.runId}:${nodeId}:${conversation.iteration}`" v-model:draft="draft" v-model:attachments="attachments" v-model:annotations="annotations" :run-id="props.runId" :node-id="nodeId" :iteration="conversation.iteration || 1" :done="conversation.done" :active="active" :node-label="label" :send-request="send" :confirm-error="error" :review-mode="!!conversation.takeover" annotate-enabled references-enabled @finish="act(() => api.reactReply(props.runId, nodeId, '', [], true))" @cancel="act(() => api.reactCancel(props.runId, nodeId))" @retry-last="act(() => api.reactReply(props.runId, nodeId, '', [], false, [], true))" @queue-remove="(id) => id && act(() => api.reactQueueRemove(props.runId, nodeId, id))" @queue-reorder="ids => act(() => api.reactQueueReorder(props.runId, nodeId, ids))" />
    <div v-else class="flex flex-1 flex-col items-center justify-center gap-3 p-8 text-sm text-txt3"><span>{{ t('pages.solo.workflows.noPendingInput') }}</span><AppButton size="sm" variant="outline" @click="refresh">{{ t('common.buttons.refresh') }}</AppButton></div>
  </div>
</template>
