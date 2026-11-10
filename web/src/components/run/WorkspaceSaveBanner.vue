<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppButton from '@/components/ui/AppButton.vue'
import type { Run, RunWorkspaceSave } from '@/lib/shared/types'
import { retryWorkspaceSave, workspaceSaveReasonKeys } from '@/lib/run/workspaceSave'

/**
 * Explains a run held between nodes by its workspace save: saving (it moves
 * on by itself) or failed (the cause, and a retry that only saves again).
 */
const props = defineProps<{
  run: Pick<Run, 'id' | 'controlVersion'>
  save: RunWorkspaceSave
  nodeLabel?: string
}>()
const emit = defineEmits<{ retried: [] }>()
const { t } = useI18n()
const busy = ref(false)
const error = ref('')
const node = computed(() => props.nodeLabel || props.save.nodeId)
const reasons = computed(() => workspaceSaveReasonKeys(props.save).map(key => t(key)))

async function retry() {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    await retryWorkspaceSave(props.run, props.save)
    emit('retried')
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div
    v-if="save.state === 'saving'"
    data-testid="workspace-save-banner"
    data-state="saving"
    class="rounded-md border border-info/30 bg-info/8 px-3.5 py-2.5 text-[13px] leading-relaxed"
    role="status"
  >
    <strong class="block text-[12px] font-semibold text-info">{{ t('workspaceSnapshots.runSave.savingTitle') }}</strong>
    <p class="m-0 text-txt">{{ t('workspaceSnapshots.runSave.savingBody', { node }) }}</p>
  </div>
  <div
    v-else
    data-testid="workspace-save-banner"
    data-state="failed"
    class="rounded-md border border-err/35 bg-err/8 px-3.5 py-2.5 text-[13px] leading-relaxed"
    role="alert"
  >
    <strong class="block text-[12px] font-semibold text-err">{{ t('workspaceSnapshots.runSave.failedTitle') }}</strong>
    <p class="m-0 text-txt">{{ t('workspaceSnapshots.runSave.failedBody', { node }) }}</p>
    <p v-for="reason in reasons" :key="reason" class="m-0 mt-1 text-txt" data-testid="workspace-save-reason">{{ reason }}</p>
    <div class="mt-2 flex flex-wrap items-center gap-2">
      <AppButton
        v-if="save.action"
        variant="primary"
        size="sm"
        icon="refresh"
        data-testid="workspace-save-retry"
        :disabled="busy"
        :loading="busy"
        @click="retry"
      >{{ busy ? t('workspaceSnapshots.runSave.retrying') : t(save.action === 'reconcile' ? 'workspaceSnapshots.runSave.reconcile' : 'workspaceSnapshots.runSave.retry') }}</AppButton>
      <span v-else class="text-[12px] text-txt2">{{ t('workspaceSnapshots.runSave.noAction') }}</span>
      <span v-if="error" class="text-[12px] text-err" data-testid="workspace-save-retry-error">{{ t('workspaceSnapshots.runSave.retryFailed', { error }) }}</span>
    </div>
  </div>
</template>
