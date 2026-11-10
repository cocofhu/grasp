<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { soloWorkflowClient as client, type WorkflowInstruction as Instruction, type WorkflowInstructionRequest, type WorkflowInstructionTarget as Target } from '@/lib/api/clients/soloWorkflowClient'
import { useVisiblePolling } from '@/lib/composables/useVisiblePolling'
import { randomUuid } from '@/lib/shared/randomUuid'
import AppButton from '@/components/ui/AppButton.vue'

const props = defineProps<{ runId: string; nodeId: string; label: string }>()
const { t } = useI18n()
const target = ref<Target | null>(null)
const selected = ref<Target | null>(null)
const items = ref<Instruction[]>([])
const draft = defineModel<string>('draft', { default: '' })
const request = defineModel<WorkflowInstructionRequest | null>('request', { default: null })
const error = ref('')
const submitting = ref(false)
const hasMore = ref(false)
let loading = false
let olderPending = false
let loadedOlder = false
let disposed = false
const stale = computed(() => !!selected.value && !!target.value && selected.value.generation !== target.value.generation)
const oversized = computed(() => new TextEncoder().encode(draft.value.trim()).length > 16 * 1024)
const canSend = computed(() => target.value?.state === 'open' && !!selected.value && !stale.value && !!draft.value.trim() && !oversized.value && !submitting.value)

function merge(next: Instruction[]) {
  const combined = new Map(items.value.map(item => [item.id, item]))
  for (const item of next) combined.set(item.id, item)
  items.value = [...combined.values()].sort((a, b) => a.sequence - b.sequence)
}
function adoptTarget() {
  selected.value = target.value ? { ...target.value } : null
  request.value = null
  error.value = ''
  void refresh()
}
async function refresh(older = false) {
  if (disposed) return
  if (loading) {
    if (older) olderPending = true
    return
  }
  loading = true
  const generation = selected.value?.generation || ''
  const before = older ? items.value[0]?.sequence || 0 : 0
  try {
    const value = await client.instructionHistory(props.runId, props.nodeId, before)
    if (disposed || generation !== (selected.value?.generation || '')) return
    target.value = value.target
    if (!selected.value && value.target) selected.value = { ...value.target, generation: request.value?.generation || value.target.generation, iteration: request.value?.iteration || value.target.iteration, attempt: request.value?.attempt || value.target.attempt }
    if (selected.value && value.target && selected.value.generation !== value.target.generation && !draft.value && !request.value) {
      selected.value = { ...value.target }
    }
    merge(value.items)
    if (older || !loadedOlder) hasMore.value = value.hasMore
    if (older) loadedOlder = true
  } catch (cause) {
    if (!disposed) error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    loading = false
    if (olderPending && !disposed) {
      olderPending = false
      void refresh(true)
    }
  }
}
async function submit() {
  if (!canSend.value || !selected.value) return
  const text = draft.value.trim()
  const snapshot = { ...selected.value }
  const submission = !request.value || request.value.text !== text || request.value.generation !== snapshot.generation
    ? { clientMessageId: randomUuid(), text, generation: snapshot.generation, iteration: snapshot.iteration, attempt: snapshot.attempt || 1 }
    : { ...request.value }
  request.value = submission
  submitting.value = true
  error.value = ''
  try {
    const item = await client.sendInstruction(props.runId, props.nodeId, submission)
    if (disposed) return
    merge([item])
    if (draft.value.trim() === text) draft.value = ''
    request.value = null
  } catch (cause) {
    if (!disposed) error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    submitting.value = false
    if (!disposed) void refresh()
  }
}
useVisiblePolling(() => refresh(), 1500)
onBeforeUnmount(() => { disposed = true })
</script>

<template>
  <section class="flex flex-1 flex-col gap-4 p-4" data-testid="workflow-instructions">
    <div class="text-sm text-txt"><strong>{{ t('pages.solo.workflows.instructions.title') }}</strong><p class="mt-1 text-xs leading-relaxed text-txt3">{{ t('pages.solo.workflows.instructions.hint') }}</p></div>
    <p v-if="selected" class="text-xs text-txt3">{{ props.label }} · {{ t('pages.solo.workflows.iteration', { n: selected.iteration }) }} · {{ t('pages.solo.workflows.instructions.attempt', { n: selected.attempt || 1 }) }}</p>
    <AppButton v-if="hasMore" size="sm" variant="ghost" data-testid="instruction-older" @click="refresh(true)">{{ t('pages.solo.workflows.instructions.older') }}</AppButton>
    <ol v-if="items.length" class="space-y-3" aria-live="polite">
      <li v-for="item in items" :key="item.id" class="rounded-lg border border-line bg-surface p-3" :data-testid="`instruction-${item.id}`">
        <div class="mb-1 flex items-center justify-between gap-2 text-xs text-txt3"><span>{{ item.actorId }} · {{ t('pages.solo.workflows.iteration', { n: item.iteration }) }} · {{ t('pages.solo.workflows.instructions.attempt', { n: item.attempt || 1 }) }}</span><span :class="item.state === 'unknown' || item.state === 'expired' ? 'text-warn' : item.state === 'applied' ? 'text-ok' : 'text-accent'">{{ t(`pages.solo.workflows.instructions.states.${item.state}`) }}</span></div>
        <p class="whitespace-pre-wrap break-words text-sm text-txt">{{ item.text }}</p><p v-if="item.error" class="mt-2 text-xs text-txt3">{{ item.error }}</p>
      </li>
    </ol>
    <p v-if="error" class="text-xs text-err" role="alert">{{ error }}</p>
    <div class="mt-auto space-y-2 border-t border-line pt-3">
      <p v-if="stale" class="text-xs text-warn">{{ t('pages.solo.workflows.instructions.stale') }} <AppButton size="sm" variant="ghost" data-testid="instruction-adopt" @click="adoptTarget">{{ t('pages.solo.workflows.instructions.adopt') }}</AppButton></p>
      <p v-else-if="target?.state !== 'open'" class="text-xs text-txt3">{{ t(target ? 'pages.solo.workflows.instructions.closed' : 'pages.solo.workflows.instructions.preparing') }}</p>
      <textarea v-model="draft" class="min-h-24 w-full resize-y rounded-lg border border-line bg-surface px-3 py-2 text-base text-txt outline-none focus:border-accent sm:text-sm" :placeholder="t('pages.solo.workflows.instructions.placeholder')" :aria-label="t('pages.solo.workflows.instructions.title')" :disabled="submitting" data-testid="instruction-input" />
      <p v-if="oversized" class="text-xs text-err" role="alert">{{ t('pages.solo.workflows.instructions.tooLong') }}</p>
      <div class="flex justify-end"><AppButton size="sm" variant="primary" :disabled="!canSend" :loading="submitting" data-testid="instruction-send" @click="submit">{{ t('pages.solo.workflows.instructions.send') }}</AppButton></div>
    </div>
  </section>
</template>
