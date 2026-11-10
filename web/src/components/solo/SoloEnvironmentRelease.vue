<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { req } from '@/lib/api/httpCore'
import { randomUuid } from '@/lib/shared/randomUuid'
import AppButton from '@/components/ui/AppButton.vue'

type ReleaseReceipt = { id: string; state: string; wholeEnvironment: boolean; chatsRetained: boolean; snapshotsRetained: number; workspacesAffected: number; returnPointsRetained: number; lossConfirmationRequired: boolean; requiresReconciliation: boolean; blockers?: string[]; error?: string; createdAt: string }
export type EnvironmentReleaseDraft = { sessionId: string; previewRequestId: string; confirmRequestId: string; receipt: ReleaseReceipt | null; uncertain: boolean }
const props = defineProps<{ sessionId: string }>()
const draft = defineModel<EnvironmentReleaseDraft | null>('draft', { default: null })
const emit = defineEmits<{ changed: [] }>()
const { t, locale } = useI18n()
const emptyDraft = (): EnvironmentReleaseDraft => ({ sessionId: props.sessionId, previewRequestId: '', confirmRequestId: '', receipt: null, uncertain: false })
const state = ref<EnvironmentReleaseDraft>(draft.value?.sessionId === props.sessionId ? draft.value : emptyDraft())
draft.value = state.value
const lossAccepted = ref(false)
const busy = ref(false)
const error = ref('')
const previous = ref<ReleaseReceipt[]>([])
const base = computed(() => `/solo/sessions/${encodeURIComponent(props.sessionId)}/environment-releases`)
const needsCheck = computed(() => state.value.uncertain || ['confirmed', 'dispatching', 'unknown'].includes(state.value.receipt?.state || ''))
const canConfirm = computed(() => state.value.receipt?.state === 'prepared' && !needsCheck.value && lossAccepted.value && !busy.value)
let disposed = false
const nonce = randomUuid
const status = (value: string) => t(`workspaceRelease.states.${['prepared', 'confirmed', 'dispatching', 'unknown', 'released'].includes(value) ? value : 'unknown'}`)
function failed(cause: unknown) {
  const code = cause && typeof cause === 'object' && 'code' in cause ? String(cause.code) : ''
  const keys: Record<string, string> = { workspace_release_unsupported: 'unsupported', workspace_release_blocked: 'blocked', workspace_release_scope_changed: 'scopeChanged', workspace_release_execution_blocked: 'authorityBlocked', workspace_release_not_found: 'notFound', workspace_release_reconcile_required: 'unknownHint' }
  error.value = t(`workspaceRelease.${keys[code] || 'unavailable'}`)
}
function remember(receipt: ReleaseReceipt) {
  previous.value = [receipt, ...previous.value.filter(item => item.id !== receipt.id)]
}
function accept(receipt: ReleaseReceipt, checked = false) {
  if (disposed) return
  state.value.receipt = receipt
  if (checked) state.value.uncertain = false
  remember(receipt)
  if (receipt.state === 'released') { state.value.uncertain = false; emit('changed') }
}
async function preview() {
  if (busy.value || state.value.receipt) return
  busy.value = true; error.value = ''
  if (!state.value.previewRequestId) state.value.previewRequestId = nonce()
  try {
    const receipt = await req<ReleaseReceipt>(base.value, { method: 'POST', body: JSON.stringify({ clientRequestId: state.value.previewRequestId }) })
    if (disposed) return
    accept(receipt); lossAccepted.value = false
  } catch (cause) { if (!disposed) failed(cause) } finally { busy.value = false }
}
async function confirm() {
  const receipt = state.value.receipt
  if (!receipt || !canConfirm.value) return
  if (!state.value.confirmRequestId) state.value.confirmRequestId = nonce()
  busy.value = true; error.value = ''
  // Persist this draft before sending. Closing the drawer or losing the reply
  // must lead to reading the original intent, never another automatic write.
  state.value.uncertain = true
  try {
    const result = await req<ReleaseReceipt>(`${base.value}/${encodeURIComponent(receipt.id)}/confirm`, { method: 'POST', body: JSON.stringify({ clientRequestId: state.value.confirmRequestId, confirmed: true, lossAccepted: true }) })
    accept(result, true)
  } catch (cause) { if (!disposed) failed(cause) } finally { busy.value = false }
}
async function check(receipt = state.value.receipt) {
  if (!receipt || busy.value) return
  busy.value = true; error.value = ''
  try {
    let result = await req<ReleaseReceipt>(`${base.value}/${encodeURIComponent(receipt.id)}`)
    // Only this explicit click may inspect the physical completion receipt.
    // The reconcile endpoint performs GETs; it cannot repeat native deletion.
    if (result.requiresReconciliation && ['dispatching', 'unknown'].includes(result.state)) {
      result = await req<ReleaseReceipt>(`${base.value}/${encodeURIComponent(receipt.id)}/reconcile`, { method: 'POST' })
    }
    const same = state.value.receipt?.id === result.id
    if (!same) { state.value.confirmRequestId = ''; lossAccepted.value = false }
    accept(result, true)
  } catch (cause) { if (!disposed) failed(cause) } finally { busy.value = false }
}
function restart() { if (busy.value || needsCheck.value) return; state.value = emptyDraft(); draft.value = state.value; lossAccepted.value = false; error.value = '' }
const date = (value: string) => new Date(value).toLocaleString(locale.value)
onMounted(async () => {
  try {
    const result = await req<{ items: ReleaseReceipt[] }>(base.value)
    if (disposed) return
    previous.value = result.items
    const saved = state.value.receipt
    if (saved) {
      // A remount may only read SQL metadata. Reconciliation always needs a
      // fresh explicit click, even if the retained draft is uncertain.
      const current = await req<ReleaseReceipt>(`${base.value}/${encodeURIComponent(saved.id)}`)
      accept(current, true)
    } else {
      const pending = result.items.find(item => ['confirmed', 'dispatching', 'unknown'].includes(item.state))
      if (pending) accept(pending, true)
    }
  } catch (cause) { if (!disposed) failed(cause) }
})
onBeforeUnmount(() => { disposed = true })
</script>

<template>
  <section class="space-y-4 p-5" data-testid="environment-release">
    <p class="text-sm leading-6 text-txt2">{{ t('workspaceRelease.intro') }}</p>
    <div class="rounded-lg border border-line bg-elevated/50 p-3 text-xs leading-5 text-txt3">
      <p>{{ t('workspaceRelease.retained') }}</p>
      <details class="mt-2" data-testid="environment-release-details">
        <summary class="cursor-pointer text-txt2">{{ t('workspaceRelease.scopeDetails') }}</summary>
        <p class="mt-2">{{ t('workspaceRelease.wholeEnvironment') }}</p>
        <p class="mt-2">{{ t('workspaceRelease.snapshotBoundary') }}</p>
        <p class="mt-2">{{ t('workspaceRelease.capability') }}</p>
      </details>
    </div>
    <p v-if="error" class="text-xs leading-5 text-err" role="alert">{{ error }}</p>
    <AppButton v-if="!state.receipt" :loading="busy" data-testid="environment-release-preview" @click="preview">{{ t('workspaceRelease.preview') }}</AppButton>
    <div v-else class="space-y-4 rounded-lg border border-line p-4" data-testid="environment-release-receipt">
      <strong class="block text-sm text-txt">{{ status(state.receipt.state) }}</strong>
      <template v-if="state.receipt.state !== 'released'">
        <p class="text-xs leading-5 text-warn">{{ t('workspaceRelease.lossScope', { count: state.receipt.workspacesAffected }) }}</p>
        <p class="text-xs leading-5 text-txt3">{{ t('workspaceRelease.retainedCount', { snapshots: state.receipt.snapshotsRetained, returns: state.receipt.returnPointsRetained }) }}</p>
      </template>
      <p v-if="needsCheck" class="text-xs leading-5 text-txt3">{{ t('workspaceRelease.unknownHint') }}</p>
      <p v-else-if="state.receipt.state === 'released'" class="text-xs leading-5 text-txt2">{{ t('workspaceRelease.releasedHint') }}</p>
      <template v-else-if="state.receipt.state === 'prepared'">
        <label class="flex items-start gap-2 rounded-lg bg-elevated p-3 text-xs leading-5 text-txt2"><input v-model="lossAccepted" type="checkbox" class="mt-1 shrink-0" :disabled="busy" data-testid="environment-release-loss" /><span>{{ t('workspaceRelease.lossAcceptance') }}</span></label>
        <AppButton variant="danger" :disabled="!canConfirm" :loading="busy" data-testid="environment-release-confirm" @click="confirm">{{ t('workspaceRelease.confirm') }}</AppButton>
      </template>
      <AppButton v-if="needsCheck" :loading="busy" data-testid="environment-release-check" @click="check()">{{ t('workspaceRelease.check') }}</AppButton>
      <AppButton v-if="!needsCheck" variant="ghost" :disabled="busy" data-testid="environment-release-restart" @click="restart">{{ t('workspaceRelease.newPreview') }}</AppButton>
    </div>
    <div v-if="previous.length" class="space-y-2 border-t border-line pt-4">
      <p class="text-xs text-txt3">{{ t('workspaceRelease.previous') }}</p>
      <button v-for="receipt in previous" :key="receipt.id" type="button" class="flex w-full items-center justify-between gap-3 rounded-lg border border-line p-2 text-left text-xs text-txt2 hover:border-accent/50 disabled:opacity-50" :disabled="busy" data-testid="environment-release-previous" @click="check(receipt)"><span>{{ date(receipt.createdAt) }}</span><span class="shrink-0 text-txt3">{{ status(receipt.state) }}</span></button>
    </div>
  </section>
</template>
