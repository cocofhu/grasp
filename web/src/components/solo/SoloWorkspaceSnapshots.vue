<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppButton from '@/components/ui/AppButton.vue'
import AppModal from '@/components/ui/AppModal.vue'
import Icon from '@/components/ui/Icon.vue'
import { useWorkspaceSnapshots } from '@/lib/solo/useWorkspaceSnapshots'

const props = defineProps<{ sessionId: string; runId?: string; sandboxId?: number; busy?: boolean; nodes?: { id: string; label: string }[] }>()
const emit = defineEmits<{ changed: []; bindingChanged: [] }>()
const { t, locale, te } = useI18n()
const state = useWorkspaceSnapshots(props, (bindingChanged) => { emit('changed'); if (bindingChanged) emit('bindingChanged') })
const { snapshots, restores, operations, checkpoints, binding, pending, pendingState, notReceived, rejected, loading, busy: operationBusy, error, nextCursor, canMutate, canCapture, canRestore } = state
const confirmation = ref<{ action: 'activate' | 'return'; target: string; expected: string } | null>(null)
const returnTarget = ref('')
const retained = computed(() => (binding.value?.retainedBindings || []).filter(row => row.id !== binding.value?.id && row.state !== 'released'))
function status(value: string) { return te(`workspaceSnapshots.states.${value}`) ? t(`workspaceSnapshots.states.${value}`) : t('workspaceSnapshots.states.pending') }
function date(value: string) { return new Date(value).toLocaleString(locale.value) }
function bytes(value: number) { return new Intl.NumberFormat(locale.value, { maximumFractionDigits: 1 }).format(value / (1024 * 1024)) + ' MiB' }
function nodeLabel(id: string) { return props.nodes?.find(node => node.id === id)?.label || id }
function requestSwitch(action: 'activate' | 'return', target: string) {
  if (!binding.value?.fingerprint || !target) return
  confirmation.value = { action, target, expected: binding.value.fingerprint }
}
async function confirmSwitch() {
  const value = confirmation.value
  confirmation.value = null
  if (!value) return
  if (binding.value?.fingerprint !== value.expected) { error.value = 'changed'; return }
  await state.switchWorkspace(value.action, value.target)
}
</script>

<template>
  <section class="space-y-5 p-5" data-testid="solo-workspace-snapshots">
    <p class="text-sm leading-6 text-txt2">{{ t('workspaceSnapshots.intro') }}</p>
    <div class="rounded-xl border border-line bg-elevated/40 p-4">
      <div class="flex items-center gap-2 text-sm font-medium text-txt"><Icon name="folder" :size="16" />{{ t('workspaceSnapshots.current') }}<span v-if="binding?.revision" class="ml-auto text-xs font-normal text-txt3">{{ t('workspaceSnapshots.workspaceVersion', { revision: binding.revision }) }}</span></div>
      <p v-if="!binding?.available" class="mt-2 text-xs leading-5 text-txt3">{{ t('workspaceSnapshots.environmentUnavailable') }}</p>
      <p v-else-if="binding.pendingOperationId" class="mt-2 text-xs leading-5 text-txt3">{{ t('workspaceSnapshots.switchPending') }}</p>
      <p v-else-if="binding.canCapture === false && binding.canRestore === false" class="mt-2 text-xs leading-5 text-txt3">{{ t('workspaceSnapshots.filesUnavailable') }}</p>
      <div class="mt-3 flex flex-wrap items-center gap-2">
        <AppButton size="sm" variant="outline" icon="save" :disabled="!canCapture" data-testid="workspace-capture" @click="state.capture">{{ t('workspaceSnapshots.capture') }}</AppButton>
        <AppButton size="sm" variant="ghost" icon="refresh" :disabled="operationBusy" @click="state.refresh(true)">{{ t('workspaceSnapshots.refresh') }}</AppButton>
      </div>
      <p class="mt-2 text-[11px] leading-5 text-txt3">{{ t('workspaceSnapshots.captureNote') }}</p>
      <template v-if="retained.length">
        <div class="mt-4 flex items-center gap-2 border-t border-line pt-3">
          <select v-model="returnTarget" class="input min-w-0 flex-1 text-xs" :aria-label="t('workspaceSnapshots.returnTarget')" :disabled="operationBusy || !!pending">
            <option value="">{{ t('workspaceSnapshots.returnTarget') }}</option>
            <option v-for="row in retained" :key="row.id" :value="row.id">{{ t('workspaceSnapshots.workspaceVersion', { revision: row.revision }) }}</option>
          </select>
          <AppButton size="sm" variant="outline" :disabled="!canMutate || !binding?.canReturn || !returnTarget" data-testid="workspace-return" @click="requestSwitch('return', returnTarget)">{{ t('workspaceSnapshots.return') }}</AppButton>
        </div>
      </template>
    </div>

    <p v-if="error" class="text-xs leading-5 text-err" role="alert">{{ t(`workspaceSnapshots.errors.${error}`) }}</p>
    <div v-if="pending" class="space-y-3 rounded-lg border border-accent/25 bg-accent/5 p-3" data-testid="workspace-pending-request">
      <div class="flex items-center gap-2 text-xs text-txt2"><Icon name="spinner" :size="13" /><span>{{ t(`workspaceSnapshots.actions.${pending.request.action}`) }} · {{ status(pendingState || 'pending') }}</span></div>
      <p class="text-xs leading-5 text-txt3">{{ t(notReceived ? 'workspaceSnapshots.notReceived' : 'workspaceSnapshots.originalRequest') }}</p>
      <div class="flex flex-wrap gap-2"><AppButton size="sm" variant="outline" :disabled="operationBusy" @click="state.observe(true)">{{ t('workspaceSnapshots.check') }}</AppButton><AppButton v-if="notReceived" size="sm" variant="ghost" :disabled="operationBusy" data-testid="workspace-retry-original" @click="state.postOriginal">{{ t('workspaceSnapshots.retryOriginal') }}</AppButton><AppButton v-if="notReceived && rejected" size="sm" variant="ghost" :disabled="operationBusy" @click="state.dismissRejected">{{ t('workspaceSnapshots.editRejected') }}</AppButton></div>
    </div>

    <div class="space-y-3">
      <template v-if="checkpoints.length">
        <h3 class="text-sm font-semibold text-txt">{{ t('workspaceSnapshots.checkpoints') }}</h3>
        <article v-for="row in checkpoints" :key="row.id" class="space-y-2 rounded-xl border border-line px-4 py-3" :data-testid="`workspace-checkpoint-${row.id}`">
          <div class="flex items-center justify-between gap-2 text-xs"><span class="text-txt2">{{ t('workspaceSnapshots.visit', { node: nodeLabel(row.nodeId), iteration: row.iteration, attempt: row.runAttempt }) }}</span><span :class="row.fileState === 'ready' ? 'text-ok' : 'text-txt3'">{{ status(row.fileState) }}</span></div>
          <p v-if="row.reason" class="text-xs leading-5 text-txt3">{{ t(te(`workspaceSnapshots.checkpointReasons.${row.reason}`) ? `workspaceSnapshots.checkpointReasons.${row.reason}` : 'workspaceSnapshots.checkpointUnknown') }}</p>
          <AppButton v-if="row.snapshotId && ['unknown', 'blocked', 'pending'].includes(row.fileState)" size="sm" variant="ghost" :disabled="operationBusy" data-testid="workspace-checkpoint-check" @click="state.checkCheckpoint(row.id)">{{ t('workspaceSnapshots.check') }}</AppButton>
        </article>
      </template>
      <h3 class="text-sm font-semibold text-txt">{{ t(props.runId ? 'workspaceSnapshots.workflowHistory' : 'workspaceSnapshots.history') }}</h3>
      <p v-if="loading" class="flex items-center gap-2 text-xs text-txt3"><Icon name="spinner" :size="13" />{{ t('workspaceSnapshots.loading') }}</p>
      <p v-else-if="!snapshots.length" class="rounded-lg border border-dashed border-line p-4 text-xs leading-6 text-txt3">{{ t('workspaceSnapshots.empty') }}</p>
      <article v-for="row in snapshots" :key="row.id" class="space-y-2 rounded-xl border border-line px-4 py-3" :data-testid="`workspace-snapshot-${row.id}`">
        <div class="flex items-center justify-between gap-2 text-xs"><time class="text-txt2">{{ date(row.createdAt) }}</time><span :class="row.state === 'ready' ? 'text-ok' : row.state === 'failed' ? 'text-err' : 'text-txt3'">{{ status(row.state) }}</span></div>
        <p v-if="row.nodeId" class="text-xs text-txt2">{{ t('workspaceSnapshots.visit', { node: nodeLabel(row.nodeId), iteration: row.iteration || 0, attempt: row.runAttempt || 0 }) }}</p>
        <p v-if="row.state === 'ready'" class="text-[11px] text-txt3">{{ t('workspaceSnapshots.fileCount', { count: row.entries, size: bytes(row.bytes) }) }}</p>
        <div class="flex justify-end gap-2"><AppButton v-if="row.state === 'unknown'" size="sm" variant="ghost" :disabled="operationBusy" @click="state.checkReceipt('capture', row.id, row.state)">{{ t('workspaceSnapshots.check') }}</AppButton><AppButton v-if="row.state === 'ready'" size="sm" variant="outline" :disabled="!canRestore" data-testid="workspace-restore-copy" @click="state.restore(row.id)">{{ t('workspaceSnapshots.restoreCopy') }}</AppButton></div>
      </article>
      <AppButton v-if="nextCursor" size="sm" variant="ghost" :disabled="operationBusy" @click="state.earlier">{{ t('workspaceSnapshots.earlier') }}</AppButton>
    </div>

    <div v-if="restores.length" class="space-y-3">
      <h3 class="text-sm font-semibold text-txt">{{ t('workspaceSnapshots.restoredCopies') }}</h3>
      <p class="text-xs leading-5 text-txt3">{{ t('workspaceSnapshots.restoreNote') }}</p>
      <article v-for="row in restores" :key="row.id" class="space-y-3 rounded-xl border border-line px-4 py-3">
        <div class="flex items-center justify-between gap-2 text-xs"><time class="text-txt2">{{ date(row.createdAt) }}</time><span :class="row.state === 'restored' ? 'text-ok' : 'text-txt3'">{{ status(row.state) }}</span></div>
        <p v-if="row.state === 'restored' && !binding?.returnSnapshotId" class="text-xs text-txt3">{{ t('workspaceSnapshots.saveBeforeSwitch') }}</p>
        <p v-else-if="row.state === 'restored' && !binding?.canActivate" class="text-xs text-txt3">{{ t('workspaceSnapshots.switchUnavailable') }}</p>
        <div class="flex justify-end gap-2"><AppButton v-if="row.state === 'unknown'" size="sm" variant="ghost" :disabled="operationBusy" @click="state.checkReceipt('restore', row.id, row.state)">{{ t('workspaceSnapshots.check') }}</AppButton><AppButton v-if="row.state === 'restored'" size="sm" variant="primary" :disabled="!canMutate || !binding?.canActivate" data-testid="workspace-activate" @click="requestSwitch('activate', row.id)">{{ t('workspaceSnapshots.activate') }}</AppButton></div>
      </article>
    </div>
    <div v-if="operations.length" class="space-y-2">
      <h3 class="text-sm font-semibold text-txt">{{ t('workspaceSnapshots.operations') }}</h3>
      <article v-for="row in operations" :key="row.id" class="flex items-center gap-2 rounded-lg border border-line px-3 py-2 text-xs text-txt2"><span>{{ t(row.action === 'return' ? 'workspaceSnapshots.actions.return' : 'workspaceSnapshots.actions.activate') }}</span><span class="ml-auto text-txt3">{{ status(row.state) }}</span><AppButton v-if="row.state === 'unknown' || row.requiresReconciliation" size="sm" variant="ghost" :disabled="operationBusy" @click="state.checkReceipt('binding', row.id, row.state)">{{ t('workspaceSnapshots.check') }}</AppButton></article>
    </div>
    <AppModal :open="!!confirmation" :title="t(confirmation?.action === 'return' ? 'workspaceSnapshots.return' : 'workspaceSnapshots.activate')" :width="440" close-on-esc @close="confirmation = null"><p class="text-sm leading-6 text-txt2">{{ t('workspaceSnapshots.switchConfirm') }}</p><template #footer><AppButton variant="ghost" @click="confirmation = null">{{ t('workspaceSnapshots.cancel') }}</AppButton><AppButton variant="primary" :disabled="!canMutate" @click="confirmSwitch">{{ t('workspaceSnapshots.confirmSwitch') }}</AppButton></template></AppModal>
  </section>
</template>
