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
  <section class="space-y-5" data-testid="solo-workspace-snapshots">
    <div class="space-y-3">
      <div class="flex flex-wrap items-center gap-3">
        <div class="flex min-w-0 flex-1 items-center gap-2 text-xs text-txt3">
          <Icon name="folder" :size="16" />
          <span>{{ binding?.revision ? t('workspaceSnapshots.workspaceVersion', { revision: binding.revision }) : t('workspaceSnapshots.current') }}</span>
        </div>
        <AppButton size="sm" variant="primary" icon="plus" :glow="false" :disabled="!canCapture" data-testid="workspace-capture" @click="state.capture">{{ t('workspaceSnapshots.capture') }}</AppButton>
      </div>
      <p v-if="binding && !binding.available" class="text-xs leading-5 text-txt3">{{ t('workspaceSnapshots.environmentUnavailable') }}</p>
      <p v-else-if="binding?.pendingOperationId" class="text-xs leading-5 text-txt3">{{ t('workspaceSnapshots.switchPending') }}</p>
      <p v-else-if="binding?.canCapture === false && binding.canRestore === false" class="text-xs leading-5 text-txt3">{{ t('workspaceSnapshots.filesUnavailable') }}</p>
      <template v-if="retained.length">
        <div class="flex items-center gap-2">
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
      <p v-if="notReceived" class="text-xs leading-5 text-txt3">{{ t('workspaceSnapshots.notReceived') }}</p>
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
      <div class="flex items-center gap-2">
        <h3 class="text-xs font-medium text-txt2">{{ t(props.runId ? 'workspaceSnapshots.workflowHistory' : 'workspaceSnapshots.history') }}</h3>
        <span v-if="snapshots.length" class="text-[11px] tabular-nums text-txt3">{{ snapshots.length }}{{ nextCursor ? '+' : '' }}</span>
        <AppButton size="sm" variant="ghost" class="ml-auto !w-7 !px-0" :disabled="operationBusy || loading" :aria-label="t('workspaceSnapshots.refresh')" :title="t('workspaceSnapshots.refresh')" data-testid="workspace-refresh" @click="state.refresh(true)"><Icon :name="loading ? 'spinner' : 'refresh'" :size="14" /></AppButton>
      </div>
      <p v-if="loading && !snapshots.length" class="flex items-center justify-center gap-2 py-8 text-xs text-txt3" role="status"><Icon name="spinner" :size="13" />{{ t('workspaceSnapshots.loading') }}</p>
      <div v-else-if="!snapshots.length" class="flex flex-col items-center gap-3 rounded-lg bg-elevated/40 py-10 text-xs text-txt3"><Icon name="history" :size="24" :interactive="false" /><p>{{ t('workspaceSnapshots.empty') }}</p></div>
      <div v-if="snapshots.length" class="divide-y divide-line border-y border-line">
        <article v-for="row in snapshots" :key="row.id" class="flex items-center gap-3 py-3" :data-testid="`workspace-snapshot-${row.id}`">
          <div class="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-elevated text-txt3"><Icon name="history" :size="16" :interactive="false" /></div>
          <div class="min-w-0 flex-1 space-y-1">
            <time :datetime="row.createdAt" class="block text-xs font-medium tabular-nums text-txt">{{ date(row.createdAt) }}</time>
            <p v-if="row.nodeId" class="break-words text-[11px] text-txt2">{{ t('workspaceSnapshots.visit', { node: nodeLabel(row.nodeId), iteration: row.iteration || 0, attempt: row.runAttempt || 0 }) }}</p>
            <p v-if="row.state === 'ready'" class="text-[11px] text-txt3">{{ t('workspaceSnapshots.fileCount', { count: row.entries, size: bytes(row.bytes) }) }}</p>
            <p v-else class="text-[11px]" :class="row.state === 'failed' ? 'text-err' : 'text-txt3'" role="status">{{ status(row.state) }}</p>
          </div>
          <AppButton v-if="row.state === 'unknown'" size="sm" variant="ghost" class="shrink-0" :disabled="operationBusy" @click="state.checkReceipt('capture', row.id, row.state)">{{ t('workspaceSnapshots.check') }}</AppButton>
          <AppButton v-if="row.state === 'ready'" size="sm" variant="outline" class="shrink-0" :disabled="!canRestore" data-testid="workspace-restore-copy" @click="state.restore(row.id)">{{ t('workspaceSnapshots.restoreCopy') }}</AppButton>
        </article>
      </div>
      <AppButton v-if="nextCursor" size="sm" variant="ghost" :disabled="operationBusy" @click="state.earlier">{{ t('workspaceSnapshots.earlier') }}</AppButton>
    </div>

    <div v-if="restores.length" class="space-y-3">
      <h3 class="text-sm font-semibold text-txt">{{ t('workspaceSnapshots.restoredCopies') }}</h3>
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
    <AppModal :open="!!confirmation" :title="t(confirmation?.action === 'return' ? 'workspaceSnapshots.return' : 'workspaceSnapshots.activate')" :width="440" :close-label="t('workspaceSnapshots.cancel')" close-on-esc trap-focus @close="confirmation = null"><p class="text-sm leading-6 text-txt2">{{ t('workspaceSnapshots.switchConfirm') }}</p><template #footer><AppButton variant="ghost" @click="confirmation = null">{{ t('workspaceSnapshots.cancel') }}</AppButton><AppButton variant="primary" :disabled="!canMutate" @click="confirmSwitch">{{ t('workspaceSnapshots.confirmSwitch') }}</AppButton></template></AppModal>
  </section>
</template>
