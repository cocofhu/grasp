<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { api, type ConcreteModelBinding, type CredentialItem, type ModelConfiguration, type ModelConfigurationBody, type ModelConnection, type ModelRoles, type ToolCapability } from '@/lib/api/api'
import { bindingProblem, connectionCapabilities, credentialUsable, isModelCredential, isToolCredential, useRoutingCopy } from '@/lib/agent/modelRouting'
import { ACP_BACKENDS } from '@/lib/shared/regionPolicy'
import { useToast } from '@/lib/composables/useToast'
import AppButton from '@/components/ui/AppButton.vue'
import AppModal from '@/components/ui/AppModal.vue'
import AppSelect from '@/components/ui/AppSelect.vue'
import Icon from '@/components/ui/Icon.vue'
import CliModelSelect from '@/components/agent/CliModelSelect.vue'
import CredentialProviderLogo from './CredentialProviderLogo.vue'
import ModelRecordActions from './ModelRecordActions.vue'

withDefaults(defineProps<{ hideCreateButton?: boolean }>(), { hideCreateButton: false })
const emit = defineEmits<{ (e: 'manageCredentials', backend?: string): void; (e: 'changed'): void }>()
const c = useRoutingCopy()
const toast = useToast()
const rows = ref<ModelConfiguration[]>([])
const connections = ref<ModelConnection[]>([])
const credentials = ref<CredentialItem[]>([])
const capabilities = ref<ToolCapability[]>([])
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
const saving = ref(false)
const editingId = ref('')
const awaitingCredential = ref(false)
const form = reactive({ name: '', backend: 'cursor', kind: 'platform' as 'platform' | 'connection', credentialId: '', connectionId: '', modelId: '', state: 'active' as ModelConfiguration['state'], roles: {} as ModelRoles, routeProfile: '' })
const toolCaps = computed(() => capabilities.value.filter((item) => item.toolId === form.backend && item.adapterState === 'existing'))
const platformAvailable = computed(() => toolCaps.value.some((item) => item.modes.includes('platform')))
const connectionAvailable = computed(() => connectionCapabilities(capabilities.value, form.backend).length > 0)
const platformKeys = computed(() => credentials.value.filter((item) => credentialUsable(item) && isToolCredential(item, form.backend)))
const services = computed(() => connections.value.filter((item) => item.state === 'active' && connectionCapabilities(capabilities.value, form.backend, item).length && credentials.value.some((key) => key.id === item.credentialId && credentialUsable(key) && isModelCredential(key))))
const selectedService = computed(() => services.value.find((item) => item.id === form.connectionId))
const credentialOptions = computed(() => [
  ...(platformAvailable.value ? platformKeys.value.map((key) => ({ value: `platform:${key.id}`, label: key.name, hint: toolLabel(form.backend), provider: form.backend })) : []),
  ...services.value.map((service) => ({ value: `connection:${service.id}`, label: credentials.value.find((key) => key.id === service.credentialId)?.name || service.name, hint: service.name, provider: service.providerId })),
])
const credentialChoice = computed<string>({
  get: () => form.kind === 'platform' ? (form.credentialId ? `platform:${form.credentialId}` : '') : (form.connectionId ? `connection:${form.connectionId}` : ''),
  set(value: string) {
    if (value === credentialChoice.value || !credentialOptions.value.some((option) => option.value === value)) return
    form.kind = value.startsWith('platform:') ? 'platform' : 'connection'
    form.credentialId = form.kind === 'platform' ? value.slice('platform:'.length) : ''
    form.connectionId = form.kind === 'connection' ? value.slice('connection:'.length) : ''
    form.modelId = ''; form.roles = {}; form.routeProfile = ''
  },
})
const selectedCredential = computed(() => credentialOptions.value.find((option) => option.value === credentialChoice.value))
const modelOptions = computed(() => (selectedService.value?.models || []).map((model) => ({ value: model.id, label: model.name || model.id, ...(model.name && model.name !== model.id ? { hint: model.id } : {}) })))
const wizardSteps = computed(() => [c.value.chooseTool, c.value.chooseCredential, c.value.selectModel])
const roleNames = computed(() => [...new Set(toolCaps.value.flatMap((item) => item.roleNames || []))].filter((role) => role !== 'main') as (keyof ModelRoles)[])
const nativeRoute = computed(() => {
  const service = selectedService.value
  if (form.backend !== 'deepseek_harness' || service?.providerId !== 'deepseek' || service.protocol !== 'anthropic-messages' || service.auth.scheme !== 'x-api-key') return false
  try { const url = new URL(service.baseUrl); return url.protocol === 'https:' && url.hostname === 'api.deepseek.com' && url.pathname.replace(/\/$/, '') === '/anthropic' } catch { return false }
})
const sourceValid = computed(() => form.kind === 'platform' ? platformAvailable.value && platformKeys.value.some((item) => item.id === form.credentialId) : connectionAvailable.value && !!selectedService.value)
const modelValid = computed(() => sourceValid.value && (form.kind === 'platform' || !!form.modelId.trim()))
const valid = computed(() => !!form.name.trim() && !!form.backend && modelValid.value)
function toolLabel(id: string) { return ACP_BACKENDS.find((tool) => tool.id === id)?.label || id }
function credentialProvider(value: string) { return credentialOptions.value.find((option) => option.value === value)?.provider || '' }
function problem(row: ModelConfiguration) { return bindingProblem(row.binding, row.backend, connections.value, credentials.value, capabilities.value) }
function roleLabel(role: keyof ModelRoles) { return c.value[`role_${role}`] }
async function load() {
  loading.value = true
  error.value = ''
  try {
    const [configs, servicesResult, keys, caps] = await Promise.all([api.listModelConfigurations(), api.listModelConnections(), api.listCredentials(), api.listToolCapabilities()])
    rows.value = configs.items; connections.value = servicesResult.items; credentials.value = keys.items; capabilities.value = caps.items
  } catch (e) { error.value = String((e as Error).message || e) } finally { loading.value = false }
}
function resetSource() {
  if (form.kind === 'platform' && !platformAvailable.value) form.kind = 'connection'
  if (form.kind === 'connection' && !connectionAvailable.value && platformAvailable.value) form.kind = 'platform'
  form.credentialId = ''; form.connectionId = ''; form.modelId = ''; form.roles = {}; form.routeProfile = ''
}
function chooseTool(backend: string) {
  if (editingId.value || form.backend === backend) return
  form.backend = backend
  resetSource()
}
function toolKeydown(event: KeyboardEvent, index: number) {
  if (editingId.value) return
  const next = event.key === 'ArrowLeft' || event.key === 'ArrowUp' ? (index + ACP_BACKENDS.length - 1) % ACP_BACKENDS.length : event.key === 'ArrowRight' || event.key === 'ArrowDown' ? (index + 1) % ACP_BACKENDS.length : event.key === 'Home' ? 0 : event.key === 'End' ? ACP_BACKENDS.length - 1 : -1
  if (next < 0) return
  event.preventDefault()
  chooseTool(ACP_BACKENDS[next]!.id)
  const group = (event.currentTarget as HTMLElement).parentElement
  group?.querySelectorAll<HTMLButtonElement>('[role="radio"]')[next]?.focus()
}
function start(row?: ModelConfiguration) {
  editingId.value = row?.id || ''
  Object.assign(form, { name: row?.name || '', backend: row?.backend || 'cursor', kind: row?.binding.kind || 'platform', credentialId: row?.binding.kind === 'platform' ? row.binding.credentialId : '', connectionId: row?.binding.kind === 'connection' ? row.binding.connectionId : '', modelId: row?.binding.modelId || '', state: row?.state || 'active', roles: { ...row?.binding.toolSettings?.modelRoles }, routeProfile: row?.binding.toolSettings?.routeProfile || '' })
  if (!row) resetSource()
  step.value = 1; error.value = ''; open.value = true
}
async function openForTool(backend: string, connectionId?: string) {
  if (!ACP_BACKENDS.some((tool) => tool.id === backend)) return
  await load()
  if (error.value) return
  start()
  form.backend = backend
  resetSource()
  if (connectionId && services.value.some((service) => service.id === connectionId)) credentialChoice.value = `connection:${connectionId}`
}
async function openCreate() {
  await load()
  if (!error.value) start()
}
function addCredential() {
  awaitingCredential.value = true
  open.value = false
  emit('manageCredentials', form.backend)
}
async function resumeWithCredential(credentialId?: string, connectionId?: string) {
  if (!awaitingCredential.value) return
  awaitingCredential.value = false
  await load()
  if (connectionId && credentialOptions.value.some((option) => option.value === `connection:${connectionId}`)) credentialChoice.value = `connection:${connectionId}`
  else if (credentialId && credentialOptions.value.some((option) => option.value === `platform:${credentialId}`)) credentialChoice.value = `platform:${credentialId}`
  step.value = 2
  open.value = true
}
function concreteBinding(): ConcreteModelBinding {
  const roles = Object.fromEntries(Object.entries(form.roles).filter(([, value]) => value?.trim()).map(([role, value]) => [role, value!.trim()])) as ModelRoles
  if (roles.main) roles.main = form.modelId.trim()
  const toolSettings = { ...(Object.keys(roles).length ? { modelRoles: roles } : {}), ...(form.routeProfile ? { routeProfile: form.routeProfile } : {}) }
  const settings = Object.keys(toolSettings).length ? { toolSettings } : {}
  return form.kind === 'platform' ? { kind: 'platform', credentialId: form.credentialId, ...(form.modelId ? { modelId: form.modelId } : {}), ...settings } : { kind: 'connection', connectionId: form.connectionId, modelId: form.modelId.trim(), ...settings }
}
async function ensureModels(binding: ConcreteModelBinding) {
  if (binding.kind !== 'connection') return
  const latest = (await api.listModelConnections()).items.find((item) => item.id === binding.connectionId)
  if (!latest || latest.state !== 'active' || !connectionCapabilities(capabilities.value, form.backend, latest).length) throw new Error(c.value.missingConnection)
  const required = [...new Set([binding.modelId, ...Object.values(binding.toolSettings?.modelRoles || {})])].filter(Boolean) as string[]
  const missing = required.filter((id) => !latest.models.some((model) => model.id === id))
  if (!missing.length) return
  const updated = await api.updateModelConnection(latest.id, { name: latest.name, providerId: latest.providerId, protocol: latest.protocol, baseUrl: latest.baseUrl, credentialId: latest.credentialId, auth: latest.auth, service: latest.service, state: latest.state, models: [...latest.models, ...missing.map((id) => ({ id, source: 'manual' as const }))] })
  connections.value = connections.value.map((item) => item.id === updated.id ? updated : item)
}
async function save() {
  if (!valid.value || saving.value) return
  saving.value = true; error.value = ''
  try {
    const binding = concreteBinding()
    await ensureModels(binding)
    const body: ModelConfigurationBody = { name: form.name.trim(), backend: form.backend, binding, state: form.state }
    if (editingId.value) await api.updateModelConfiguration(editingId.value, body)
    else await api.createModelConfiguration(body)
    open.value = false; await load(); toast.success(c.value.configurationSaved); emit('changed')
  } catch (e) { error.value = String((e as Error).message || e) } finally { saving.value = false }
}
async function toggle(row: ModelConfiguration) {
  try { await api.updateModelConfiguration(row.id, { name: row.name, backend: row.backend, binding: row.binding, state: row.state === 'active' ? 'disabled' : 'active' }); await load(); emit('changed') } catch (e) { error.value = String((e as Error).message || e) }
}
async function remove(row: ModelConfiguration) {
  if (!window.confirm(c.value.configurationDeleteConfirm)) return
  try { await api.deleteModelConfiguration(row.id); await load(); toast.success(c.value.configurationRemoved); emit('changed') } catch (e) { error.value = String((e as Error).message || e) }
}
onMounted(load)
defineExpose({ openForTool, resumeWithCredential, openCreate })
</script>

<template>
  <section class="min-w-0" data-testid="model-configurations-manager">
    <div class="configuration-heading">
      <div class="min-w-0">
        <div class="flex items-center gap-2.5">
          <h3 class="m-0 text-[13px] font-semibold text-txt">{{ c.modelsTab }}</h3>
          <span v-if="!loading" class="configuration-count">{{ rows.length }}</span>
        </div>
      </div>
      <AppButton v-if="!hideCreateButton" icon="plus" size="sm" variant="primary" data-testid="model-configuration-add" :disabled="loading" @click="start()">{{ c.addConfiguration }}</AppButton>
    </div>
    <p v-if="error && !open" role="alert" class="text-xs text-err">{{ error }} <AppButton size="sm" @click="load">{{ c.retry }}</AppButton></p>
    <p v-if="loading" class="text-xs text-txt3">{{ c.loading }}</p>
    <div v-else-if="!rows.length" class="configuration-empty">
      <p class="m-0 text-xs leading-6 text-txt3">{{ c.noConfigurations }}</p>
      <AppButton size="sm" variant="ghost" data-testid="model-configuration-manage-credentials" @click="emit('manageCredentials')">{{ c.configureKeys }}</AppButton>
    </div>
    <div v-else class="configuration-list">
      <article v-for="row in rows" :key="row.id" class="configuration-row" :data-testid="`model-configuration-${row.id}`">
        <div class="configuration-identity">
          <CredentialProviderLogo :provider="row.backend" match="provider" />
          <div class="min-w-0 flex-1">
            <div class="flex min-w-0 items-center gap-1.5">
              <h4 class="m-0 truncate text-[13px] font-medium leading-5 text-txt" :title="row.name">{{ row.name }}</h4>
              <span v-if="problem(row)" class="inline-flex shrink-0 text-warn" :title="c.configurationUnavailable">
                <Icon name="warning" :size="12" :interactive="false" />
                <span class="sr-only">{{ c.configurationUnavailable }}</span>
              </span>
            </div>
            <p class="configuration-model" :class="row.binding.modelId ? 'font-mono' : ''" :title="row.binding.modelId || c.autoModel">{{ row.binding.modelId || c.autoModel }}</p>
          </div>
        </div>
        <div class="configuration-actions">
          <ModelRecordActions :name="row.name" :active="row.state === 'active'" prefix="model-configuration" :record-id="row.id" @edit="start(row)" @toggle="toggle(row)" @remove="remove(row)" />
        </div>
      </article>
    </div>
    <AppModal :open="open" :title="editingId ? c.editConfiguration : c.addConfiguration" :width="660" :close-on-esc="!saving" trap-focus :close-label="c.close" @close="!saving && (open = false)">
      <ol class="configuration-steps">
        <li v-for="(label, index) in wizardSteps" :key="index" :aria-current="step === index + 1 ? 'step' : undefined" :class="{ 'configuration-step--current': step === index + 1, 'configuration-step--done': step > index + 1 }">
          <span class="configuration-step-number" aria-hidden="true"><Icon v-if="step > index + 1" name="check" :size="11" :interactive="false" /><template v-else>{{ index + 1 }}</template></span>
          <span>{{ label }}</span>
        </li>
      </ol>
      <div class="space-y-4" ref="formEl" data-testid="model-configuration-form" :data-step="step">
        <template v-if="step === 1">
          <label class="block"><span class="label">{{ c.configurationName }}</span><input v-model="form.name" class="input w-full" autofocus data-testid="model-configuration-name" /></label>
          <div>
            <span id="configuration-tool-label" class="label">{{ c.chooseTool }}</span>
            <div class="configuration-tool-grid" role="radiogroup" aria-labelledby="configuration-tool-label" :aria-disabled="!!editingId" data-testid="model-configuration-backend">
              <button v-for="(tool, index) in ACP_BACKENDS" :key="tool.id" type="button" role="radio" :aria-checked="form.backend === tool.id" :disabled="!!editingId" :tabindex="form.backend === tool.id ? 0 : -1" class="configuration-tool-option" :class="{ 'configuration-tool-option--selected': form.backend === tool.id }" :data-testid="`model-configuration-backend-${tool.id}`" @click="chooseTool(tool.id)" @keydown="toolKeydown($event, index)">
                <CredentialProviderLogo :provider="tool.id" match="provider" :selected="form.backend === tool.id" />
                <span class="min-w-0 text-center text-xs leading-4">{{ tool.label }}</span>
                <Icon v-if="form.backend === tool.id" name="check" :size="12" :interactive="false" class="configuration-tool-check" />
              </button>
            </div>
            <p v-if="editingId" class="mb-0 mt-2 text-xs leading-5 text-txt3">{{ c.immutableTool }}</p>
          </div>
        </template>
        <template v-else-if="step === 2">
          <div v-if="credentialOptions.length || credentialChoice">
            <span class="label">{{ c.chooseCredential }}</span>
            <AppSelect v-model="credentialChoice" :options="credentialOptions" :aria-label="c.chooseCredential" :placeholder="c.choose" :invalid="!!credentialChoice && !sourceValid" searchable data-testid="model-configuration-credential-choice">
              <template #leading><CredentialProviderLogo v-if="credentialChoice" :provider="credentialProvider(credentialChoice) || form.backend" match="provider" /></template>
              <template #option-leading="{ option }"><CredentialProviderLogo :provider="credentialProvider(option.value)" match="provider" /></template>
            </AppSelect>
            <p v-if="credentialChoice && !sourceValid" class="mb-0 mt-2 text-xs leading-5 text-warn">{{ c.missingCredential }}</p>
          </div>
          <div v-else class="configuration-credential-empty" data-testid="model-configuration-credentials-empty">
            <CredentialProviderLogo :provider="form.backend" match="provider" />
            <p class="m-0 text-xs leading-5 text-txt3">{{ c.noCredentialsForTool }}</p>
          </div>
          <AppButton size="sm" variant="ghost" icon="plus" data-testid="model-configuration-add-credential" @click="addCredential">{{ c.addCredential }}</AppButton>
        </template>
        <template v-else>
          <div class="flex min-w-0 items-center gap-3 border-b border-line/70 pb-4">
            <CredentialProviderLogo :provider="form.backend" match="provider" />
            <div class="min-w-0"><p class="m-0 truncate text-[13px] font-medium text-txt" :title="form.name">{{ form.name }}</p><p class="mb-0 mt-1 truncate text-xs text-txt3">{{ selectedCredential?.label }}</p></div>
          </div>
          <template v-if="form.kind === 'platform'"><CliModelSelect v-model="form.modelId" :backend="form.backend" variant="compact" hide-command hide-auto-sub /></template>
          <template v-else>
            <div>
              <span class="label">{{ c.selectModel }}</span>
              <AppSelect v-model="form.modelId" :options="modelOptions" :aria-label="c.selectModel" :placeholder="c.selectModel" searchable allow-custom data-testid="model-configuration-model-id">
                <template #leading><CredentialProviderLogo :provider="selectedService?.providerId" match="provider" /></template>
                <template #option-leading><CredentialProviderLogo :provider="selectedService?.providerId" match="provider" /></template>
              </AppSelect>
              <p class="mb-0 mt-2 text-xs leading-5 text-txt3">{{ c.manualModelHint }}</p>
            </div>
            <datalist id="configuration-model-catalog"><option v-for="model in selectedService?.models || []" :key="model.id" :value="model.id">{{ model.name || model.id }}</option></datalist>
            <details v-if="roleNames.length" class="rounded-lg border border-line p-3"><summary class="cursor-pointer text-xs text-txt2">{{ c.auxiliary }}</summary><div class="mt-3 grid gap-3 sm:grid-cols-2"><label v-for="role in roleNames" :key="role" class="block"><span class="label">{{ roleLabel(role) }}</span><input v-model="form.roles[role]" class="input w-full font-mono" list="configuration-model-catalog" :placeholder="c.followMain" :data-testid="`model-configuration-role-${role}`" /></label></div></details>
            <label v-if="nativeRoute" class="block"><span class="label">{{ c.routeProfile }}</span><AppSelect v-model="form.routeProfile" :options="[{ value: '', label: c.standardRoute }, { value: 'deepseek-native', label: 'DeepSeek native' }]" :aria-label="c.routeProfile" searchable data-testid="model-configuration-route-profile" /></label>
          </template>
        </template>
        <p v-if="error" role="alert" class="break-words text-xs text-err" data-testid="model-configuration-error">{{ error }}</p>
      </div>
      <template #footer><AppButton variant="ghost" :disabled="saving" data-testid="model-configuration-back" @click="step > 1 ? step-- : (open = false)">{{ step > 1 ? c.back : c.cancel }}</AppButton><AppButton v-if="step < 3" variant="primary" :disabled="step === 1 ? !form.name.trim() : !sourceValid" data-testid="model-configuration-next" @click="step++">{{ c.next }}</AppButton><AppButton v-else variant="primary" :loading="saving" :disabled="!valid || saving" data-testid="model-configuration-save" @click="save">{{ c.save }}</AppButton></template>
    </AppModal>
  </section>
</template>

<style scoped>
.configuration-heading {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
}
.configuration-count {
  color: rgb(var(--c-txt3));
  font-size: 11px;
  font-variant-numeric: tabular-nums;
}
.configuration-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  padding: 28px 16px;
  border-block: 1px solid rgb(var(--c-line) / .7);
  text-align: center;
}
.configuration-list {
  border-top: 1px solid rgb(var(--c-line) / .7);
}
.configuration-row {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
  min-height: 64px;
  padding: 10px 0;
  border-bottom: 1px solid rgb(var(--c-line) / .7);
}
.configuration-identity {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
  flex: 1;
}
.configuration-model {
  margin: 2px 0 0;
  min-width: 0;
  overflow: hidden;
  color: rgb(var(--c-txt3));
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 12px;
  line-height: 18px;
}
.configuration-actions {
  flex-shrink: 0;
}
.configuration-steps {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
  margin: 0 0 24px;
  padding: 0;
  list-style: none;
}
.configuration-steps li {
  display: flex;
  align-items: center;
  gap: 7px;
  min-width: 0;
  color: rgb(var(--c-txt3));
  font-size: 12px;
  line-height: 18px;
}
.configuration-step-number {
  display: inline-flex;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  border: 1px solid rgb(var(--c-line));
  border-radius: 50%;
  font-size: 11px;
}
.configuration-steps .configuration-step--current,
.configuration-steps .configuration-step--done {
  color: rgb(var(--c-accent-2));
}
.configuration-step--current .configuration-step-number {
  border-color: rgb(var(--c-accent) / .35);
  background: rgb(var(--c-accent) / .07);
}
.configuration-step--done .configuration-step-number {
  border-color: transparent;
  background: rgb(var(--c-accent) / .07);
}
.configuration-tool-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 8px;
}
.configuration-tool-option {
  position: relative;
  display: flex;
  min-width: 0;
  min-height: 84px;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  border: 1px solid rgb(var(--c-line));
  border-radius: 8px;
  padding: 12px 8px;
  background: rgb(var(--c-surface));
  color: rgb(var(--c-txt2));
  cursor: pointer;
}
.configuration-tool-option:hover:not(:disabled) {
  border-color: rgb(var(--c-line-strong));
  background: rgb(var(--c-elevated));
}
.configuration-tool-option--selected {
  border-color: rgb(var(--c-accent) / .5);
  background: rgb(var(--c-accent) / .04);
  color: rgb(var(--c-txt));
}
.configuration-tool-option:focus-visible {
  outline: 2px solid rgb(var(--c-accent) / .4);
  outline-offset: 2px;
}
.configuration-tool-option:disabled {
  cursor: default;
}
.configuration-tool-option:disabled:not(.configuration-tool-option--selected) {
  opacity: .45;
}
.configuration-tool-check {
  position: absolute;
  top: 7px;
  right: 7px;
  color: rgb(var(--c-accent-2));
}
.configuration-credential-empty {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 16px 0;
}
@media (max-width: 420px) {
  .configuration-row {
    gap: 8px;
  }
  .configuration-identity {
    gap: 8px;
  }
  .configuration-steps {
    gap: 8px;
  }
  .configuration-steps li {
    flex-direction: column;
    gap: 5px;
    align-items: flex-start;
    font-size: 11px;
  }
  .configuration-tool-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
</style>
