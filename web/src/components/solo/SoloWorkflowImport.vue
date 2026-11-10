<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { soloWorkflowClient as client, type WorkspaceImportInventory as Inventory, type WorkspaceImportReceipt as Preview, type WorkspaceImportRepository as Repository, type WorkspaceImportSource as Source, type WorkspaceImportSourceRef } from '@/lib/api/clients/soloWorkflowClient'
import { randomUuid } from '@/lib/shared/randomUuid'
import AppButton from '@/components/ui/AppButton.vue'

export type WorkspaceImportDraft = { sourceKey: string; inventory: Inventory | null; selections: Record<string, { checked: boolean; commit: string }>; preview: Preview | null; previewRequestId: string; confirmRequestId: string; uncertain: boolean }
const emptyDraft = (): WorkspaceImportDraft => ({ sourceKey: '', inventory: null, selections: {}, preview: null, previewRequestId: '', confirmRequestId: '', uncertain: false })
const props = defineProps<{ sessionId: string; runId: string; nodes?: { id: string; label: string }[] }>()
const draft = defineModel<WorkspaceImportDraft | null>('draft', { default: null })
const emit = defineEmits<{ changed: [] }>()
const { t, te, locale } = useI18n()
// A bound model emits synchronously but its prop updates on the next render.
// Keep an immediate reactive value so the first drawer render is never null.
const state = ref<WorkspaceImportDraft>(draft.value || emptyDraft())
draft.value = state.value
const sources = ref<Source[]>([])
const previous = ref<Preview[]>([])
const busy = ref(false)
const error = ref('')
let disposed = false
const sourceKey = (source: WorkspaceImportSourceRef) => JSON.stringify([source.nodeId, source.iteration, source.attempt])
const selectedSource = computed(() => sources.value.find(source => sourceKey(source) === state.value.sourceKey))
const selected = computed(() => Object.entries(state.value.selections).filter(([, value]) => value.checked).map(([path, value]) => ({ path, commit: value.commit.trim() })))
const valid = computed(() => selected.value.length > 0 && selected.value.every(repo => /^[0-9a-f]{40}$/.test(repo.commit)))
const unsettled = (state = '') => state === 'applying' || state === 'unknown'
const uncertain = computed(() => state.value.uncertain || unsettled(state.value.preview?.state))
const status = computed(() => uncertain.value ? 'unknown' : state.value.preview?.state || 'prepared')
function repositoryReason(repo: Repository) {
  const key = `pages.solo.workspaceImport.reasons.${repo.reason}`
  return repo.reason && te(key) ? t(key) : t('pages.solo.workspaceImport.repositoryUnsupported')
}
function label(source: WorkspaceImportSourceRef) {
  const node = props.nodes?.find(node => node.id === source.nodeId)?.label || source.nodeId
  return t('pages.solo.workspaceImport.sourceLabel', { node, iteration: source.iteration, attempt: source.attempt })
}
function date(value: string) { return new Date(value).toLocaleString(locale.value) }
const nonce = randomUuid
function failed(cause: unknown) { if (!disposed) error.value = cause instanceof Error ? cause.message : String(cause) }
function remember(receipt: Preview) { previous.value = [receipt, ...previous.value.filter(value => value.id !== receipt.id)].slice(0, 20) }
function editSource() { state.value.inventory = null; state.value.selections = {}; state.value.previewRequestId = ''; error.value = '' }
function editSelection() { state.value.previewRequestId = ''; error.value = '' }
async function inventory() {
  if (!selectedSource.value?.available || busy.value) return
  busy.value = true; error.value = ''
  const source = selectedSource.value
  try {
    const result = await client.importInventory(props.sessionId, props.runId, source)
    if (disposed) return
    state.value.inventory = result
    state.value.selections = Object.fromEntries(result.repositories.map(repo => [repo.path, { checked: repo.supported, commit: repo.commit }]))
  } catch (cause) { failed(cause) } finally { busy.value = false }
}
async function preview() {
  if (!selectedSource.value || !valid.value || busy.value) return
  busy.value = true; error.value = ''
  if (!state.value.previewRequestId) state.value.previewRequestId = nonce()
  const source = selectedSource.value
  try {
    const result = await client.previewImport(props.sessionId, props.runId, source, selected.value, state.value.previewRequestId)
    if (disposed) return
    state.value.preview = result; state.value.confirmRequestId = ''; state.value.uncertain = false
    remember(result)
  } catch (cause) { failed(cause) } finally { busy.value = false }
}
async function confirm() {
  const receipt = state.value.preview
  if (!receipt || receipt.state !== 'prepared' || uncertain.value || busy.value) return
  if (!state.value.confirmRequestId) state.value.confirmRequestId = nonce()
  const confirmation = state.value.confirmRequestId
  busy.value = true; error.value = ''
  // Preserve this before sending: closing the drawer or a lost response cannot
  // turn an uncertain write into a new automatic filesystem operation.
  state.value.uncertain = true
  try {
    const result = await client.confirmImport(props.sessionId, receipt.id, confirmation)
    if (disposed) return
    state.value.preview = result; state.value.uncertain = result.state !== 'applied'
    remember(result)
    if (result.state === 'applied') emit('changed')
  } catch (cause) {
    const rejection = cause as { status?: number; code?: string; params?: Record<string, unknown> } | null
    // Error codes alone are ambiguous: target-unavailable can also occur
    // after admission. Only the server's exact pre-admission proof permits a
    // retry, using the original nonce even if another request has since won.
    if (!disposed && rejection && typeof rejection.status === 'number' && rejection.status >= 400 && rejection.status < 500
      && rejection.params?.confirmationAccepted === false
      && rejection.params.importId === receipt.id && rejection.params.clientRequestId === confirmation
      && state.value.preview?.id === receipt.id && state.value.confirmRequestId === confirmation) {
      state.value.uncertain = false
      if (rejection.code === 'workspace_import_expired') { state.value.preview.state = 'expired'; remember(state.value.preview) }
    }
    failed(cause)
  } finally { busy.value = false }
}
async function adopt(receipt: Preview) {
  if (busy.value) return
  busy.value = true; error.value = ''
  try {
    const result = await client.getImport(props.sessionId, receipt.id)
    if (disposed) return
    const same = state.value.preview?.id === result.id
    state.value.preview = result; state.value.uncertain = unsettled(result.state)
    remember(result)
    if (!same) state.value.confirmRequestId = ''
    if (result.state === 'applied') emit('changed')
  } catch (cause) { failed(cause) } finally { busy.value = false }
}
async function reconcile() {
  const receipt = state.value.preview
  if (!receipt || busy.value) return
  busy.value = true; error.value = ''
  try {
    // First read authoritative SQL. Only this explicit user action may then
    // ask the runtime for its completion receipt; it never retries Import.
    let result = await client.getImport(props.sessionId, receipt.id)
    if (unsettled(result.state)) result = await client.reconcileImport(props.sessionId, receipt.id)
    if (disposed) return
    state.value.preview = result; state.value.uncertain = unsettled(result.state)
    remember(result)
    if (result.state === 'applied') emit('changed')
  } catch (cause) { failed(cause) } finally { busy.value = false }
}
function restart() { state.value = emptyDraft(); draft.value = state.value; error.value = '' }
onMounted(async () => {
  const saved = state.value.preview
  const results = await Promise.allSettled([
    client.importSources(props.sessionId, props.runId),
    client.listImports(props.sessionId, props.runId),
  ])
  if (disposed) return
  if (results[0].status === 'fulfilled') sources.value = results[0].value
  else failed(results[0].reason)
  if (results[1].status === 'fulfilled') previous.value = results[1].value
  else failed(results[1].reason)
  if (saved) await adopt(saved)
})
onBeforeUnmount(() => { disposed = true })
</script>

<template>
  <section class="space-y-4 p-5" data-testid="workflow-code-import">
    <p class="text-sm leading-6 text-txt2">{{ t('pages.solo.workspaceImport.intro') }}</p>
    <div class="rounded-lg border border-line bg-elevated/50 p-3 text-xs leading-5 text-txt3">
      <p>{{ t('pages.solo.workspaceImport.boundaries') }}</p>
      <details class="mt-2" data-testid="code-import-limits">
        <summary class="cursor-pointer text-txt2">{{ t('pages.solo.workspaceImport.details') }}</summary>
        <p class="mt-2">{{ t('pages.solo.workspaceImport.exclusions') }}</p>
        <p class="mt-1">{{ t('pages.solo.workspaceImport.limits') }}</p>
        <p class="mt-1">{{ t('pages.solo.workspaceImport.retention') }}</p>
      </details>
    </div>
    <p v-if="error" class="text-xs text-err" role="alert">{{ error }}</p>
    <template v-if="!state.preview">
      <label class="block space-y-2 text-xs text-txt2"><span>{{ t('pages.solo.workspaceImport.source') }}</span>
        <select v-model="state.sourceKey" class="input w-full" :disabled="busy" data-testid="code-import-source" @change="editSource">
          <option value="">{{ t('pages.solo.workspaceImport.chooseSource') }}</option>
          <option v-for="source in sources" :key="sourceKey(source)" :value="sourceKey(source)" :disabled="!source.available">{{ label(source) }}{{ source.shared ? ` · ${t('pages.solo.workspaceImport.shared')}` : '' }}{{ !source.available ? ` · ${t('pages.solo.workspaceImport.unavailable')}` : '' }}</option>
        </select>
      </label>
      <p v-if="!sources.length" class="text-xs leading-5 text-txt3">{{ t('pages.solo.workspaceImport.noSources') }}</p>
      <AppButton :disabled="!selectedSource?.available || busy" :loading="busy" data-testid="code-import-inventory" @click="inventory">{{ t('pages.solo.workspaceImport.readRepositories') }}</AppButton>
      <div v-if="state.inventory" class="space-y-3">
        <p v-if="!state.inventory.repositories.length" class="text-xs text-txt3">{{ t('pages.solo.workspaceImport.noRepositories') }}</p>
        <div v-for="repo in state.inventory.repositories" :key="repo.path" class="rounded-lg border border-line p-3">
          <label class="flex items-center gap-2 text-sm text-txt"><input v-model="state.selections[repo.path]!.checked" type="checkbox" :disabled="!repo.supported || busy" :aria-label="repo.path" @change="editSelection" /><span class="font-mono">{{ repo.path }}</span></label>
          <p v-if="!repo.supported" class="mt-2 text-xs text-txt3">{{ repositoryReason(repo) }}</p>
          <template v-else><label class="mt-3 block space-y-1 text-xs text-txt3"><span>{{ t('pages.solo.workspaceImport.commit') }}</span><input v-model="state.selections[repo.path]!.commit" class="input w-full font-mono text-xs" :disabled="busy" :aria-label="`${repo.path} ${t('pages.solo.workspaceImport.commit')}`" @input="editSelection" /></label><p v-if="repo.dirty || repo.untracked" class="mt-2 text-xs text-warn">{{ t('pages.solo.workspaceImport.dirtyExcluded') }}</p></template>
        </div>
        <AppButton variant="primary" :disabled="!valid || busy" :loading="busy" data-testid="code-import-preview" @click="preview">{{ t('pages.solo.workspaceImport.preview') }}</AppButton>
      </div>
    </template>
    <div v-else class="space-y-4 rounded-lg border border-line p-4" data-testid="code-import-receipt">
      <strong class="block text-sm text-txt">{{ t(`pages.solo.workspaceImport.states.${status}`) }}</strong>
      <p class="text-xs text-txt3">{{ label(state.preview.source) }}</p>
      <div v-for="repo in state.preview.repositories" :key="repo.path" class="space-y-1 text-xs"><strong class="font-mono text-txt">{{ repo.path }}</strong><p class="break-all font-mono text-txt3">{{ repo.commit }}</p><p class="text-txt3">{{ t('pages.solo.workspaceImport.files', { count: repo.files, size: (repo.bytes / 1024).toFixed(1) }) }}</p><p v-if="repo.dirty || repo.untracked" class="text-warn">{{ t('pages.solo.workspaceImport.dirtyExcluded') }}</p></div>
      <template v-if="state.preview.state === 'applied'"><p class="text-xs text-txt2">{{ t('pages.solo.workspaceImport.result') }}</p><code v-for="path in state.preview.paths" :key="path" class="block break-all rounded bg-elevated p-2 text-xs text-txt" data-testid="code-import-result-path">{{ path }}</code></template>
      <p v-else-if="uncertain" class="text-xs leading-5 text-txt3">{{ t('pages.solo.workspaceImport.unknownHint') }}</p>
      <p v-else-if="state.preview.state === 'expired'" class="text-xs leading-5 text-txt3">{{ t('pages.solo.workspaceImport.expiredHint') }}</p>
      <p v-else class="text-xs leading-5 text-txt3">{{ t('pages.solo.workspaceImport.confirmHint') }}</p>
      <p v-if="state.preview.state === 'prepared' && state.preview.expiresAt" class="text-xs text-txt3">{{ t('pages.solo.workspaceImport.expiresAt', { time: date(state.preview.expiresAt) }) }}</p>
      <div v-if="state.preview.state !== 'applied' && state.preview.destination" class="space-y-2"><p class="text-xs text-txt3">{{ t('pages.solo.workspaceImport.destination') }}</p><code class="block break-all rounded bg-elevated p-2 text-xs text-txt" data-testid="code-import-destination">{{ state.preview.destination }}</code></div>
      <div class="flex flex-wrap gap-2"><AppButton v-if="state.preview.state === 'prepared' && !uncertain" variant="primary" :loading="busy" data-testid="code-import-confirm" @click="confirm">{{ t('pages.solo.workspaceImport.confirm') }}</AppButton><AppButton v-if="uncertain" :loading="busy" data-testid="code-import-reconcile" @click="reconcile">{{ t('pages.solo.workspaceImport.reconcile') }}</AppButton><AppButton variant="ghost" :disabled="busy" data-testid="code-import-restart" @click="restart">{{ t('pages.solo.workspaceImport.newPreview') }}</AppButton></div>
    </div>
    <div v-if="previous.length" class="space-y-2 border-t border-line pt-4"><p class="text-xs text-txt3">{{ t('pages.solo.workspaceImport.previous') }}</p><button v-for="receipt in previous" :key="receipt.id" type="button" class="flex w-full items-center justify-between gap-3 rounded-lg border border-line p-2 text-left text-xs text-txt2 hover:border-accent/50 disabled:opacity-50" :disabled="busy" data-testid="code-import-previous" @click="adopt(receipt)"><span>{{ label(receipt.source) }} · {{ date(receipt.createdAt) }}</span><span class="shrink-0 text-txt3">{{ t(`pages.solo.workspaceImport.states.${receipt.state}`) }}</span></button></div>
  </section>
</template>
