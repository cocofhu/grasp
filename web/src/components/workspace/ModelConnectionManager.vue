<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { api, type CredentialItem, type ModelConnection, type ModelConnectionBody, type ModelProtocol } from '@/lib/api/api'
import AppButton from '@/components/ui/AppButton.vue'
import AppModal from '@/components/ui/AppModal.vue'
import AppSelect from '@/components/ui/AppSelect.vue'
import Icon from '@/components/ui/Icon.vue'
import CredentialProviderLogo from './CredentialProviderLogo.vue'
import ModelRecordActions from './ModelRecordActions.vue'
import ModelProviderSelect from './ModelProviderSelect.vue'
import { credentialUsable, isModelCredential, modelProviderPreset, MODEL_PROTOCOLS, MODEL_PROVIDER_PRESETS, useRoutingCopy } from '@/lib/agent/modelRouting'
import { useToast } from '@/lib/composables/useToast'

const props = defineProps<{ credentials: CredentialItem[] }>()
const emit = defineEmits<{ (e: 'addCredential'): void; (e: 'changed'): void }>()
const c = useRoutingCopy()
const toast = useToast()
const rows = ref<ModelConnection[]>([])
const loading = ref(true)
const error = ref('')
const open = ref(false)
const step = ref(1)
const formEl = ref<HTMLElement | null>(null)
watch(step, async () => {
  await nextTick()
  if (!open.value || !formEl.value) return
  const target = formEl.value.querySelector<HTMLElement>('input:not([disabled]), textarea:not([disabled]), select:not([disabled]), button:not([disabled])') || formEl.value
  if (target === formEl.value) target.tabIndex = -1
  target.focus({ preventScroll: true })
})
const editingId = ref('')
const saving = ref(false)
const customProviderId = ref('')
const form = reactive<ModelConnectionBody>({ name: '', providerId: 'custom', protocol: 'openai-chat-completions', baseUrl: '', credentialId: '', auth: { scheme: 'bearer' }, models: [], state: 'active' })
const effectiveProviderId = computed(() => form.providerId === 'custom' ? customProviderId.value.trim() : form.providerId.trim())
const providerReady = computed(() => /^[a-zA-Z0-9][a-zA-Z0-9._-]*$/.test(effectiveProviderId.value))
const keys = computed(() => props.credentials.filter((item) => isModelCredential(item) && credentialUsable(item)))
const credentialOptions = computed(() => keys.value.map((key) => ({ value: key.id, label: key.name, hint: modelProviderPreset(key.issuer || key.provider || '')?.label || key.issuer || key.provider })))
const wizardSteps = computed(() => [c.value.chooseService, c.value.details, c.value.review])
function credentialProvider(id: string) { const key = keys.value.find((item) => item.id === id); return key?.issuer || key?.provider || '' }
const protocols = MODEL_PROTOCOLS
const protocolOptions = protocols.map((protocol) => ({ value: protocol, label: protocol }))
const authOptions = [{ value: 'bearer', label: 'Authorization: Bearer' }, { value: 'x-api-key', label: 'x-api-key' }]
function setProtocol(value: string) { if (protocols.includes(value as ModelProtocol)) form.protocol = value as ModelProtocol }
function setAuth(value: string) { if (value === 'bearer' || value === 'x-api-key') form.auth = { scheme: value } }
const providers = computed(() => [...new Set(['custom', ...MODEL_PROVIDER_PRESETS.map((preset) => preset.id), form.providerId, ...rows.value.map((row) => row.providerId), ...props.credentials.map((item) => item.issuer || '')])].filter(Boolean))
const valid = computed(() => {
  try { const url = new URL(form.baseUrl); if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password) return false } catch { return false }
  if (!['bearer', 'x-api-key'].includes(form.auth.scheme)) return false
  return !!(form.name.trim() && providerReady.value && keys.value.some((item) => item.id === form.credentialId) && (form.auth.scheme !== 'custom-header' || /^[A-Za-z0-9-]+$/.test(form.auth.headerName || '')))
})
async function load() {
  loading.value = true
  error.value = ''
  try { rows.value = (await api.listModelConnections()).items } catch (e) { error.value = String((e as Error).message || e) } finally { loading.value = false }
}
function providerLabel(row: ModelConnection) { return modelProviderPreset(row.providerId)?.label || row.providerId }
function start(row?: ModelConnection) {
  editingId.value = row?.id || ''
  Object.assign(form, row ? JSON.parse(JSON.stringify(row)) : { name: '', providerId: 'custom', protocol: 'openai-chat-completions', baseUrl: '', credentialId: '', auth: { scheme: 'bearer' }, service: { region: '', product: '' }, models: [], state: 'active' })
  customProviderId.value = row && !modelProviderPreset(row.providerId) ? row.providerId : ''
  if (!modelProviderPreset(form.providerId)) form.providerId = 'custom'
  form.state = row?.state === 'disabled' ? 'disabled' : 'active'
  form.service ||= { region: '', product: '' }
  step.value = 1
  error.value = ''
  open.value = true
}
function preset(provider: string) {
  if (provider === form.providerId || (form.providerId === 'custom' && provider === effectiveProviderId.value)) return
  const next = modelProviderPreset(provider)
  form.providerId = next ? provider : 'custom'
  if (next) { form.protocol = next.protocol; form.baseUrl = next.baseUrl; form.auth = { scheme: next.scheme } }
  else {
    if (provider !== 'custom') customProviderId.value = provider
    form.baseUrl = ''
    form.protocol = 'openai-chat-completions'
    form.auth = { scheme: 'bearer' }
  }
}
async function save() {
  if (!valid.value || saving.value) return
  saving.value = true
  error.value = ''
  try {
    const models = editingId.value ? (await latestConnection(editingId.value)).models : []
    const body: ModelConnectionBody = { name: form.name.trim(), providerId: effectiveProviderId.value, protocol: form.protocol, baseUrl: form.baseUrl.trim(), credentialId: form.credentialId, auth: { ...form.auth }, service: { ...form.service }, models, state: form.state }
    if (editingId.value) await api.updateModelConnection(editingId.value, body)
    else await api.createModelConnection(body)
    open.value = false; await load(); toast.success(c.value.connectionSaved); emit('changed')
  } catch (e) { error.value = String((e as Error).message || e) } finally { saving.value = false }
}
async function latestConnection(id: string) {
  const row = (await api.listModelConnections()).items.find((item) => item.id === id)
  if (!row) throw new Error(c.value.missingConnection)
  return row
}
async function toggle(row: ModelConnection) {
  try { const latest = await latestConnection(row.id); await api.updateModelConnection(row.id, { ...latest, state: row.state === 'active' ? 'disabled' : 'active' }); await load(); emit('changed') } catch (e) { error.value = String((e as Error).message || e) }
}
async function remove(row: ModelConnection) {
  if (!window.confirm(c.value.deleteConfirm)) return
  try { await api.deleteModelConnection(row.id); await load(); toast.success(c.value.connectionRemoved); emit('changed') } catch (e) { error.value = String((e as Error).message || e) }
}
function openForCredential(id: string) { start(); form.credentialId = id }
function useCredential(id: string) { form.credentialId = id }
async function editConnection(id: string) { if (!rows.value.some((row) => row.id === id)) await load(); const row = rows.value.find((row) => row.id === id); if (row) start(row) }
defineExpose({ openForCredential, useCredential, editConnection, refresh: load })
onMounted(load)
</script>

<template>
  <section class="connection-section" data-testid="model-connections-manager">
    <div class="connection-heading">
      <div class="flex min-w-0 items-center gap-2.5"><h3 class="m-0 text-[13px] font-semibold text-txt">{{ c.connections }}</h3><span v-if="!loading" class="text-[11px] tabular-nums text-txt3">{{ rows.length }}</span></div>
      <AppButton icon="plus" size="sm" variant="outline" data-testid="model-connection-add" @click="start()">{{ c.addConnection }}</AppButton>
    </div>
    <p v-if="error && !open" role="alert" class="text-xs text-err">{{ error }} <AppButton size="sm" @click="load">{{ c.retry }}</AppButton></p>
    <p v-if="loading" class="text-xs text-txt3">{{ c.loading }}</p>
    <p v-else-if="!rows.length" class="connection-empty">{{ c.emptyConnections }}</p>
    <div v-else class="connection-list">
      <article v-for="row in rows" :key="row.id" class="connection-row" :data-testid="`model-connection-${row.id}`">
        <div class="connection-identity">
          <CredentialProviderLogo :provider="row.providerId" match="provider" />
          <div class="min-w-0">
            <h4 class="m-0 truncate text-[13px] font-medium leading-5 text-txt" :title="row.name">{{ row.name }}</h4>
            <p class="connection-meta">{{ providerLabel(row) }}</p>
          </div>
        </div>
        <div class="connection-actions"><ModelRecordActions :name="row.name" :active="row.state === 'active'" prefix="model-connection" :record-id="row.id" @edit="start(row)" @toggle="toggle(row)" @remove="remove(row)" /></div>
      </article>
    </div>
    <AppModal :open="open" :title="editingId ? c.editConnection : c.addConnection" :width="650" :close-on-esc="!saving" trap-focus :close-label="c.close" @close="!saving && (open = false)">
      <ol class="connection-steps">
        <li v-for="(label, index) in wizardSteps" :key="index" :aria-current="step === index + 1 ? 'step' : undefined" :class="{ 'connection-step--current': step === index + 1, 'connection-step--done': step > index + 1 }">
          <span class="connection-step-number" aria-hidden="true"><Icon v-if="step > index + 1" name="check" :size="11" :interactive="false" /><template v-else>{{ index + 1 }}</template></span>
          <span>{{ label }}</span>
        </li>
      </ol>
      <div class="space-y-4" ref="formEl" data-testid="model-connection-form" :data-step="step">
        <template v-if="step === 1"><label class="block"><span class="label">{{ c.provider }}</span><ModelProviderSelect :model-value="form.providerId" :extra-providers="providers" data-testid="model-connection-provider" @update:model-value="preset" /></label><label v-if="form.providerId === 'custom'" class="block"><span class="label">{{ c.providerId }}</span><input v-model="customProviderId" class="input w-full" data-testid="model-connection-custom-provider" placeholder="my-provider" autocomplete="off" /></label><label class="block"><span class="label">{{ c.protocol }}</span><AppSelect :model-value="form.protocol" :options="protocolOptions" :aria-label="c.protocol" searchable data-testid="model-connection-protocol" @update:model-value="setProtocol" /></label><label class="block"><span class="label">{{ c.name }}</span><input v-model="form.name" class="input w-full" autofocus data-testid="model-connection-name" /></label></template>
        <template v-else-if="step === 2"><label class="block"><span class="label">{{ c.baseUrl }}</span><input v-model="form.baseUrl" type="url" class="input w-full font-mono" data-testid="model-connection-base-url" placeholder="https://…" /></label><div><span class="label">{{ c.credential }}</span><AppSelect v-model="form.credentialId" :options="credentialOptions" :aria-label="c.credential" :placeholder="c.selectKey" searchable data-testid="model-connection-credential"><template #leading><CredentialProviderLogo v-if="form.credentialId" :provider="credentialProvider(form.credentialId)" match="provider" /></template><template #option-leading="{ option }"><CredentialProviderLogo :provider="credentialProvider(option.value)" match="provider" /></template></AppSelect></div><p v-if="!keys.length" class="text-xs text-warn">{{ c.noKey }} <AppButton size="sm" @click="emit('addCredential')">{{ c.addKey }}</AppButton></p><label class="block"><span class="label">{{ c.auth }}</span><AppSelect :model-value="form.auth.scheme" :options="authOptions" :aria-label="c.auth" :placeholder="form.auth.scheme === 'custom-header' ? 'Custom header · unsupported' : c.auth" :invalid="form.auth.scheme === 'custom-header'" searchable data-testid="model-connection-auth" @update:model-value="setAuth" /></label><label v-if="form.auth.scheme === 'custom-header'" class="block"><span class="label">{{ c.headerName }}</span><input v-model="form.auth.headerName" class="input w-full" /></label><div class="grid gap-3 sm:grid-cols-2"><label class="block"><span class="label">{{ c.region }}</span><input v-model="form.service!.region" class="input w-full" /></label><label class="block"><span class="label">{{ c.product }}</span><input v-model="form.service!.product" class="input w-full" /></label></div></template>
        <template v-else><p class="text-xs leading-5 text-txt3">{{ c.modelCatalogHint }}</p><div class="break-all rounded-lg border border-line p-3 text-xs leading-6 text-txt2">{{ form.name }} · {{ effectiveProviderId }}<br />{{ form.protocol }} · {{ form.baseUrl }}<br />{{ keys.find((key) => key.id === form.credentialId)?.name }} · {{ c.unverified }}</div></template>
        <p v-if="error" role="alert" class="text-xs text-err">{{ error }}</p>
      </div>
      <template #footer><AppButton variant="ghost" :disabled="saving" @click="step > 1 ? step-- : (open = false)">{{ step > 1 ? c.back : c.cancel }}</AppButton><AppButton v-if="step < 3" variant="primary" :disabled="step === 1 ? !form.name.trim() || !providerReady : !valid" data-testid="model-connection-next" @click="step++">{{ c.next }}</AppButton><AppButton v-else variant="primary" :loading="saving" :disabled="!valid || saving" data-testid="model-connection-save" @click="save">{{ c.save }}</AppButton></template>
    </AppModal>
  </section>
</template>

<style scoped>
.connection-section { min-width: 0; margin-top: 32px; }
.connection-heading { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin-bottom: 14px; }
.connection-empty { border-block: 1px solid rgb(var(--c-line) / .7); padding: 26px 12px; color: rgb(var(--c-txt3)); font-size: 12px; text-align: center; }
.connection-list { border-top: 1px solid rgb(var(--c-line) / .7); }
.connection-row { display: grid; grid-template-columns: minmax(0, 1fr) auto; align-items: center; gap: 20px; min-width: 0; padding: 12px 0; border-bottom: 1px solid rgb(var(--c-line) / .7); }
.connection-identity { display: flex; min-width: 0; align-items: center; gap: 12px; }
.connection-meta { overflow: hidden; margin: 2px 0 0; color: rgb(var(--c-txt3)); font-size: 11px; line-height: 16px; white-space: nowrap; text-overflow: ellipsis; }
.connection-actions { justify-self: end; }
.connection-steps { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 12px; margin: 0 0 24px; padding: 0; list-style: none; }
.connection-steps li { display: flex; align-items: center; gap: 7px; min-width: 0; color: rgb(var(--c-txt3)); font-size: 12px; line-height: 18px; }
.connection-step-number { display: inline-flex; flex-shrink: 0; align-items: center; justify-content: center; width: 22px; height: 22px; border: 1px solid rgb(var(--c-line)); border-radius: 50%; font-size: 11px; }
.connection-steps .connection-step--current, .connection-steps .connection-step--done { color: rgb(var(--c-accent-2)); }
.connection-step--current .connection-step-number { border-color: rgb(var(--c-accent) / .35); background: rgb(var(--c-accent) / .07); }
.connection-step--done .connection-step-number { border-color: transparent; background: rgb(var(--c-accent) / .07); }
@media (max-width: 420px) {
  .connection-row { gap: 8px; }
  .connection-identity { gap: 9px; }
  .connection-steps { gap: 8px; }
  .connection-steps li { align-items: flex-start; gap: 5px; font-size: 11px; }
}
</style>
