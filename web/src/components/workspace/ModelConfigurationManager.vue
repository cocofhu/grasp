<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, type CliCatalogModel, type ConcreteModelBinding, type CredentialItem, type ModelConfiguration, type ModelConfigurationBody, type ModelConnection, type ModelRoles, type ToolCapability } from '@/lib/api/api'
import { loadCliModels } from '@/lib/agent/cliCatalog'
import { modelVendor } from '@/lib/agent/cliModelView'
import { officialModelFamily } from '@/lib/agent/fixedEffort'
import { bindingProblem, connectionCapabilities, credentialUsable, isModelCredential, isToolCredential, useRoutingCopy } from '@/lib/agent/modelRouting'
import { effortChoices, validEffortToken } from '@/lib/agent/fixedEffort'
import { mergeModelMetadata, modelInputMode, setModelInputMode, type ModelInputMode } from '@/lib/agent/modelCapabilities'
import { ACP_BACKENDS } from '@/lib/shared/regionPolicy'
import { useToast } from '@/lib/composables/useToast'
import AppButton from '@/components/ui/AppButton.vue'
import CredentialEditorSurface from './CredentialEditorSurface.vue'
import AppSelect from '@/components/ui/AppSelect.vue'
import Icon from '@/components/ui/Icon.vue'
import CliModelSelect from '@/components/agent/CliModelSelect.vue'
import VendorMark from '@/components/agent/VendorMark.vue'
import CredentialProviderLogo from './CredentialProviderLogo.vue'
import ModelRecordActions from './ModelRecordActions.vue'
import ModelEffortPicker from './ModelEffortPicker.vue'

const props = withDefaults(defineProps<{ hideCreateButton?: boolean; inline?: boolean; allowCredentialCreation?: boolean; setup?: boolean }>(), { hideCreateButton: false, inline: false, allowCredentialCreation: true, setup: false })
const emit = defineEmits<{ (e: 'manageCredentials', backend?: string): void; (e: 'changed'): void; (e: 'saved', configuration: ModelConfiguration): void; (e: 'editing', active: boolean): void }>()
const c = useRoutingCopy()
const { t } = useI18n()
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
watch(() => open.value || awaitingCredential.value || saving.value, (active) => emit('editing', active))
const form = reactive({ name: '', backend: 'cursor', kind: 'platform' as 'platform' | 'connection', credentialId: '', connectionId: '', modelId: '', auxiliaryModelIds: [] as string[], state: 'active' as ModelConfiguration['state'], roles: {} as ModelRoles, routeProfile: '', efforts: {} as Record<string, string> })
const cliModels = ref<CliCatalogModel[]>([])
const cliReady = ref(false)
const savedModelIds = ref<string[]>([])
const imageInputDrafts = reactive(new Map<string, ModelInputMode>())
const toolCaps = computed(() => capabilities.value.filter((item) => item.toolId === form.backend && item.adapterState === 'existing'))
const platformAvailable = computed(() => toolCaps.value.some((item) => item.modes.includes('platform')))
const connectionAvailable = computed(() => connectionCapabilities(capabilities.value, form.backend).length > 0)
const platformKeys = computed(() => credentials.value.filter((item) => credentialUsable(item) && isToolCredential(item, form.backend)))
const services = computed(() => connections.value.filter((item) => item.state === 'active' && connectionCapabilities(capabilities.value, form.backend, item).length && credentials.value.some((key) => key.id === item.credentialId && credentialUsable(key) && isModelCredential(key))))
const selectedService = computed(() => services.value.find((item) => item.id === form.connectionId))
const discoveredModels = ref<ModelConnection['models']>([])
const catalogLoading = ref(false)
const catalogError = ref(false)
let catalogRequest = 0
const serviceModels = computed(() => {
  const discovered = new Map(discoveredModels.value.map(model => [model.id, model]))
  const saved = (selectedService.value?.models || []).map(model => mergeModelMetadata(model, discovered.get(model.id)))
  const savedIds = new Set(saved.map(model => model.id))
  return [...saved, ...discoveredModels.value.filter(model => !savedIds.has(model.id))]
})
async function loadServiceModels() {
  if (!open.value || step.value !== 3 || form.kind !== 'connection' || !selectedService.value) return
  const generation = ++catalogRequest
  const id = selectedService.value.id
  const current = () => generation === catalogRequest && open.value && step.value === 3 && form.kind === 'connection' && selectedService.value?.id === id
  catalogLoading.value = true
  catalogError.value = false
  try {
    const response = await api.listModelConnectionModels(id)
    if (!current()) return
    if (response.error || !Array.isArray(response.models)) throw new Error('Model catalog unavailable')
    discoveredModels.value = response.models
  } catch {
    if (current()) catalogError.value = true
  } finally {
    if (current()) catalogLoading.value = false
  }
}
watch(() => [open.value, step.value, form.kind, selectedService.value?.id, selectedService.value?.revision] as const, () => {
  catalogRequest++
  discoveredModels.value = []
  catalogLoading.value = false
  catalogError.value = false
  void loadServiceModels()
})
onBeforeUnmount(() => { catalogRequest++ })
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
    form.modelId = ''; form.auxiliaryModelIds = []; form.roles = {}; form.routeProfile = ''; form.efforts = {}
    imageInputDrafts.clear()
  },
})
const selectedCredential = computed(() => credentialOptions.value.find((option) => option.value === credentialChoice.value))
type SlotOption = { value: string; label: string; hint?: string; vendor: string }
const slotCatalog = computed<SlotOption[]>(() => {
  if (form.kind === 'platform') return cliModels.value.map((model) => ({ value: model.id, label: model.name || model.id, ...(model.name && model.name !== model.id ? { hint: model.id } : {}), vendor: model.vendor }))
  const vendor = selectedService.value?.providerId || ''
  return serviceModels.value.map((model) => ({ value: model.id, label: model.name || model.id, ...(model.name && model.name !== model.id ? { hint: model.id } : {}), vendor: modelVendor(model.id, model.name, vendor) }))
})
const wizardSteps = computed(() => [c.value.chooseTool, c.value.chooseCredential, c.value.selectModel])
const roleNames = computed(() => [...new Set(toolCaps.value.flatMap((item) => item.roleNames || []))].filter((role) => role !== 'main') as (keyof ModelRoles)[])
const configurableImageInput = computed(() => form.kind === 'connection' && ['codebuddy', 'opencode', 'deepseek_harness'].includes(form.backend))
const imageInputOptions = computed(() => [
  { value: 'default', label: c.value.imageInputDefault },
  { value: 'text', label: c.value.imageInputText },
  { value: 'image', label: c.value.imageInputImage },
])
function imageInputMode(id: string): ModelInputMode {
  const key = id.trim()
  return imageInputDrafts.get(key) ?? modelInputMode(serviceModels.value.find(model => model.id === key))
}
function changeImageInput(id: string, mode: string) {
  if (id.trim() && mode !== imageInputMode(id) && (mode === 'default' || mode === 'text' || mode === 'image')) imageInputDrafts.set(id.trim(), mode)
}
function showRoleImageInput(role: keyof ModelRoles) {
  if (!configurableImageInput.value) return false
  const id = form.roles[role]?.trim()
  if (!id || id === form.modelId.trim() || form.auxiliaryModelIds.some(model => model.trim() === id)) return false
  return roleNames.value.find(name => form.roles[name]?.trim() === id) === role
}
function modelEffortLevels(id: string) {
  return effortChoices(form.backend, form.kind, selectedService.value?.providerId, selectedService.value?.baseUrl, id)
}
function modelSlotLabel(id: string) {
  const value = id.trim()
  return optionsFor(value).find((option) => option.value === value)?.label || value
}
function showEffort(id: string) {
  return form.backend === 'opencode' && !!id.trim()
}
function setEffort(id: string, value: string) {
  const key = id.trim()
  if (!value) delete form.efforts[key]
  else form.efforts[key] = value
}
const nativeRoute = computed(() => {
  const service = selectedService.value
  if (form.backend !== 'deepseek_harness' || service?.providerId !== 'deepseek' || service.protocol !== 'anthropic-messages' || service.auth.scheme !== 'x-api-key') return false
  try { const url = new URL(service.baseUrl); return url.protocol === 'https:' && url.hostname === 'api.deepseek.com' && url.pathname.replace(/\/$/, '') === '/anthropic' } catch { return false }
})
const sourceValid = computed(() => form.kind === 'platform' ? platformAvailable.value && platformKeys.value.some((item) => item.id === form.credentialId) : connectionAvailable.value && !!selectedService.value)
const auxiliaryProblem = computed(() => {
  const ids = form.auxiliaryModelIds.map((id) => id.trim())
  if (ids.some((id) => !id)) return c.value.duplicateModel
  const all = [form.modelId.trim(), ...ids].filter(Boolean)
  return new Set(all).size === all.length ? '' : c.value.duplicateModel
})
const lockedFamily = computed(() => {
  if (form.backend !== 'opencode' || form.kind !== 'platform') return ''
  for (const id of [form.modelId, ...form.auxiliaryModelIds]) {
    const family = officialModelFamily(id)
    if (family) return family
  }
  return ''
})
const familyCatalog = computed(() => {
  const family = lockedFamily.value
  if (!family) return slotCatalog.value
  return slotCatalog.value.filter((option) => option.vendor === family || officialModelFamily(option.value) === family)
})
const officialProblem = computed(() => {
  if (form.backend !== 'opencode' || form.kind !== 'platform') return ''
  const ids = [form.modelId, ...form.auxiliaryModelIds].map((id) => id.trim()).filter(Boolean)
  if (!ids.length) return ''
  const families = ids.map((id) => officialModelFamily(id))
  if (families.some((family) => !family)) return c.value.officialModelInvalid
  return new Set(families).size > 1 ? c.value.officialFamilyMixed : ''
})
const modelValid = computed(() => sourceValid.value && (form.kind === 'platform' || !!form.modelId.trim()) && !auxiliaryProblem.value && !officialProblem.value)
const valid = computed(() => !!form.name.trim() && !!form.backend && modelValid.value)
function takenIds(except: string) {
  const skip = except.trim()
  const ids = new Set<string>()
  const primary = form.modelId.trim()
  if (primary && primary !== skip) ids.add(primary)
  for (const id of form.auxiliaryModelIds) {
    const trimmed = id.trim()
    if (trimmed && trimmed !== skip) ids.add(trimmed)
  }
  return ids
}
function optionsFor(current: string): SlotOption[] {
  const value = current.trim()
  const taken = takenIds(value)
  const options = familyCatalog.value.filter((option) => option.value === value || !taken.has(option.value))
  if (value && !options.some((option) => option.value === value)) options.unshift({ value, label: value, vendor: modelVendor(value, '', form.kind === 'platform' ? '' : (selectedService.value?.providerId || '')) })
  return options
}
const canAddModel = computed(() => familyCatalog.value.some((option) => !takenIds('').has(option.value)))
const primaryExcludeIds = computed(() => form.auxiliaryModelIds.map((id) => id.trim()).filter(Boolean))
function addAuxiliary() {
  const next = familyCatalog.value.find((option) => !takenIds('').has(option.value))
  if (!next) return
  form.auxiliaryModelIds.push(next.value)
}
function removeAuxiliary(index: number) { form.auxiliaryModelIds.splice(index, 1) }
function slotVendor(id: string) { return optionsFor(id).find((option) => option.value === id)?.vendor || modelVendor(id, '', form.kind === 'connection' ? selectedService.value?.providerId || '' : '') }
function slotUnavailable(id: string) {
  const value = id.trim()
  if (!value || !savedModelIds.value.includes(value)) return false
  if (form.kind === 'platform' && !cliReady.value) return false
  return !slotCatalog.value.some((option) => option.value === value)
}
function auxiliaryCount(row: ModelConfiguration) { return row.binding.auxiliaryModelIds?.length || 0 }
function auxiliaryCountLabel(row: ModelConfiguration) { return c.value.auxiliaryCount.replace('{n}', String(auxiliaryCount(row))) }
function toolLabel(id: string) { return ACP_BACKENDS.find((tool) => tool.id === id)?.label || id }
function credentialProvider(value: string) { return credentialOptions.value.find((option) => option.value === value)?.provider || '' }
function problem(row: ModelConfiguration) { return bindingProblem(row.binding, row.backend, connections.value, credentials.value, capabilities.value) }
function roleLabel(role: keyof ModelRoles) { return c.value[`role_${role}`] }
function roleOptions(current: string) {
  const value = current.trim()
  const options = [...slotCatalog.value]
  if (value && !options.some((option) => option.value === value)) options.unshift({ value, label: value, vendor: modelVendor(value, '', selectedService.value?.providerId || '') })
  return [{ value: '', label: c.value.followMain }, ...options]
}
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
  form.credentialId = ''; form.connectionId = ''; form.modelId = ''; form.auxiliaryModelIds = []; form.roles = {}; form.routeProfile = ''; form.efforts = {}
  imageInputDrafts.clear()
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
  imageInputDrafts.clear()
  editingId.value = row?.id || ''
  savedModelIds.value = [row?.binding.modelId || '', ...(row?.binding.auxiliaryModelIds || [])].map((id) => id.trim()).filter(Boolean)
  Object.assign(form, { name: row?.name || '', backend: row?.backend || 'cursor', kind: row?.binding.kind || 'platform', credentialId: row?.binding.kind === 'platform' ? row.binding.credentialId : '', connectionId: row?.binding.kind === 'connection' ? row.binding.connectionId : '', modelId: row?.binding.modelId || '', auxiliaryModelIds: [...(row?.binding.auxiliaryModelIds || [])], state: row?.state || 'active', roles: { ...row?.binding.toolSettings?.modelRoles }, routeProfile: row?.binding.toolSettings?.routeProfile || '', efforts: { ...(row?.binding.toolSettings?.modelEfforts || {}) } })
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
async function openSetup(configurationId = '') {
  await load()
  if (!error.value) start(rows.value.find(row => row.id === configurationId))
}
async function openCreate() {
  await load()
  if (!error.value) start()
}
function addCredential() {
  if (!props.allowCredentialCreation) return
  awaitingCredential.value = true
  open.value = false
  emit('manageCredentials', form.backend)
}
async function resumeWithCredential(credentialId?: string, connectionId?: string) {
  if (!awaitingCredential.value) return
  await load()
  if (connectionId && credentialOptions.value.some((option) => option.value === `connection:${connectionId}`)) credentialChoice.value = `connection:${connectionId}`
  else if (credentialId && credentialOptions.value.some((option) => option.value === `platform:${credentialId}`)) credentialChoice.value = `platform:${credentialId}`
  step.value = 2
  open.value = true
  awaitingCredential.value = false
}
function concreteBinding(): ConcreteModelBinding {
  const roles = Object.fromEntries(Object.entries(form.roles).filter(([, value]) => value?.trim()).map(([role, value]) => [role, value!.trim()])) as ModelRoles
  if (roles.main) roles.main = form.modelId.trim()
  const selectedIds = [form.modelId.trim(), ...form.auxiliaryModelIds.map((id) => id.trim())].filter(Boolean)
  const effortEntries = form.backend === 'opencode' ? selectedIds.map((id) => [id, (form.efforts[id] || '').trim()] as const).filter(([, effort]) => validEffortToken(effort)) : []
  const modelEfforts = effortEntries.length ? Object.fromEntries(effortEntries) : undefined
  const toolSettings = { ...(Object.keys(roles).length ? { modelRoles: roles } : {}), ...(form.routeProfile ? { routeProfile: form.routeProfile } : {}), ...(modelEfforts ? { modelEfforts } : {}) }
  const settings = Object.keys(toolSettings).length ? { toolSettings } : {}
  const auxiliaryModelIds = form.auxiliaryModelIds.map((id) => id.trim()).filter(Boolean)
  const aux = auxiliaryModelIds.length ? { auxiliaryModelIds } : {}
  return form.kind === 'platform' ? { kind: 'platform', credentialId: form.credentialId, ...(form.modelId ? { modelId: form.modelId } : {}), ...aux, ...settings } : { kind: 'connection', connectionId: form.connectionId, modelId: form.modelId.trim(), ...aux, ...settings }
}
async function ensureModels(binding: ConcreteModelBinding) {
  if (binding.kind !== 'connection') return
  const latest = (await api.listModelConnections()).items.find((item) => item.id === binding.connectionId)
  if (!latest || latest.state !== 'active' || !connectionCapabilities(capabilities.value, form.backend, latest).length) throw new Error(c.value.missingConnection)
  const required = [...new Set([binding.modelId, ...(binding.auxiliaryModelIds || []), ...Object.values(binding.toolSettings?.modelRoles || {})])].filter(Boolean) as string[]
  const requiredIds = new Set(required)
  const discovered = new Map(discoveredModels.value.map(model => [model.id, model]))
  const mergeRequired = (model: ModelConnection['models'][number]) => {
    const merged = mergeModelMetadata(model, discovered.get(model.id))
    const draft = imageInputDrafts.get(model.id)
    return draft === undefined ? merged : setModelInputMode(merged, draft)
  }
  const models = latest.models.map(model => requiredIds.has(model.id) ? mergeRequired(model) : model)
  for (const id of required) {
    if (!models.some(model => model.id === id)) models.push(mergeRequired(serviceModels.value.find(model => model.id === id) || { id, source: 'manual' }))
  }
  const metadataKey = (model: ModelConnection['models'][number]) => [model.id, model.name || null, model.source, model.capabilities?.toolCalls ?? null, model.capabilities?.vision ?? null, model.capabilities?.reasoning ?? null, model.input?.length ? [...model.input].sort() : null, model.contextWindow ?? null, model.maxTokens ?? null, model.reasoningEfforts?.length ? model.reasoningEfforts : null]
  if (JSON.stringify(models.map(metadataKey)) === JSON.stringify(latest.models.map(metadataKey))) return
  const updated = await api.updateModelConnection(latest.id, { name: latest.name, providerId: latest.providerId, protocol: latest.protocol, baseUrl: latest.baseUrl, credentialId: latest.credentialId, auth: latest.auth, service: latest.service, state: latest.state, models })
  connections.value = connections.value.map((item) => item.id === updated.id ? updated : item)
}
async function save() {
  if (!valid.value || saving.value) return
  saving.value = true; error.value = ''
  try {
    const binding = concreteBinding()
    await ensureModels(binding)
    const body: ModelConfigurationBody = { name: form.name.trim(), backend: form.backend, binding, state: form.state }
    const saved = editingId.value ? await api.updateModelConfiguration(editingId.value, body) : await api.createModelConfiguration(body)
    open.value = false; await load(); toast.success(c.value.configurationSaved); emit('saved', saved); emit('changed')
  } catch (e) { error.value = String((e as Error).message || e) } finally { saving.value = false }
}
async function toggle(row: ModelConfiguration) {
  try { await api.updateModelConfiguration(row.id, { name: row.name, backend: row.backend, binding: row.binding, state: row.state === 'active' ? 'disabled' : 'active' }); await load(); emit('changed') } catch (e) { error.value = String((e as Error).message || e) }
}
async function remove(row: ModelConfiguration) {
  if (!window.confirm(c.value.configurationDeleteConfirm)) return
  try { await api.deleteModelConfiguration(row.id); await load(); toast.success(c.value.configurationRemoved); emit('changed') } catch (e) { error.value = String((e as Error).message || e) }
}
watch(() => [form.backend, open.value] as const, async ([backend, isOpen]) => {
  if (!isOpen) return
  cliReady.value = false
  const list = await loadCliModels(backend)
  if (backend !== form.backend || !open.value) return
  cliModels.value = list
  cliReady.value = true
})
onMounted(() => { if (!props.setup) void load() })
defineExpose({ openForTool, resumeWithCredential, openCreate, openSetup })
</script>

<template>
  <section class="min-w-0" data-testid="model-configurations-manager">
    <template v-if="!inline || (!open && !awaitingCredential)">
    <div v-if="!setup" class="configuration-heading">
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
    <AppButton v-else-if="setup && !error" size="sm" variant="primary" data-testid="model-configuration-resume-setup" @click="openSetup()">{{ t('pages.onboarding.toolSetup.start') }}</AppButton>
    <div v-else-if="!setup && !rows.length" class="configuration-empty">
      <p class="m-0 text-xs leading-6 text-txt3">{{ c.noConfigurations }}</p>
      <AppButton v-if="allowCredentialCreation" size="sm" variant="ghost" data-testid="model-configuration-manage-credentials" @click="emit('manageCredentials')">{{ c.configureKeys }}</AppButton>
    </div>
    <div v-else-if="!setup" class="configuration-list">
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
            <p class="configuration-model" :title="row.binding.modelId || c.autoShort"><span :class="row.binding.modelId ? 'font-mono' : ''">{{ row.binding.modelId || c.autoShort }}</span><span v-if="auxiliaryCount(row)"> · {{ auxiliaryCountLabel(row) }}</span></p>
          </div>
        </div>
        <div class="configuration-actions">
          <AppButton v-if="inline" size="sm" variant="ghost" icon="edit" :aria-label="c.edit" :data-testid="`model-configuration-edit-${row.id}`" @click="start(row)" />
          <ModelRecordActions v-else :name="row.name" :active="row.state === 'active'" prefix="model-configuration" :record-id="row.id" @edit="start(row)" @toggle="toggle(row)" @remove="remove(row)" />
        </div>
      </article>
    </div>
    </template>
    <CredentialEditorSurface :inline="props.inline" :open="open" :hide-heading="setup" :return-focus="!saving && !awaitingCredential" :title="setup ? t('pages.onboarding.toolSetup.title') : editingId ? c.editConfiguration : c.addConfiguration" :width="660" :close-on-esc="!saving" trap-focus :close-label="c.close" @close="!saving && (open = false)">
      <ol class="configuration-steps">
        <li v-for="(label, index) in wizardSteps" :key="index" :aria-current="step === index + 1 ? 'step' : undefined" :class="{ 'configuration-step--current': step === index + 1, 'configuration-step--done': step > index + 1 }">
          <span class="configuration-step-number" aria-hidden="true"><Icon v-if="step > index + 1" name="check" :size="11" :interactive="false" /><template v-else>{{ index + 1 }}</template></span>
          <span>{{ label }}</span>
        </li>
      </ol>
      <div class="space-y-4" ref="formEl" data-testid="model-configuration-form" :data-step="step">
        <template v-if="step === 1">
          <label class="block"><span class="label">{{ setup ? t('pages.onboarding.toolSetup.name') : c.configurationName }}</span><input v-model="form.name" class="input w-full" autofocus data-testid="model-configuration-name" /></label>
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
          <AppButton v-if="allowCredentialCreation" size="sm" variant="ghost" icon="plus" data-testid="model-configuration-add-credential" @click="addCredential">{{ c.addCredential }}</AppButton>
        </template>
        <template v-else>
          <div class="flex min-w-0 items-center gap-3 border-b border-line/70 pb-4">
            <CredentialProviderLogo :provider="form.backend" match="provider" />
            <div class="min-w-0"><p class="m-0 truncate text-[13px] font-medium text-txt" :title="form.name">{{ form.name }}</p><p class="mb-0 mt-1 truncate text-xs text-txt3">{{ selectedCredential?.label }}</p></div>
          </div>
          <div v-if="form.kind === 'connection'" class="flex flex-wrap items-center justify-between gap-2 text-xs text-txt3" data-testid="model-configuration-model-catalog">
            <span role="status">{{ catalogLoading ? c.modelsLoading : catalogError ? c.modelsLoadFailed : serviceModels.length ? `${serviceModels.length} ${c.modelCount}` : c.modelsEmpty }}</span>
            <AppButton size="sm" variant="ghost" :loading="catalogLoading" :disabled="catalogLoading" data-testid="model-configuration-refresh-models" @click="loadServiceModels">{{ c.refreshModels }}</AppButton>
          </div>
          <div class="model-slot" data-testid="model-configuration-primary">
            <div class="model-slot-top"><span>{{ c.primarySlot }}</span></div>
            <CliModelSelect v-if="form.kind === 'platform'" v-model="form.modelId" :backend="form.backend" :exclude-ids="primaryExcludeIds" :allowed-vendors="lockedFamily ? [lockedFamily] : []" variant="compact" hide-command hide-auto-sub hide-reset />
            <AppSelect v-else v-model="form.modelId" :options="optionsFor(form.modelId)" :aria-label="c.primarySlot" :placeholder="c.selectModel" :loading="catalogLoading" searchable allow-custom custom-position="last" data-testid="model-configuration-model-id">
              <template #leading><VendorMark :vendor="slotVendor(form.modelId)" :size="28" :unknown="slotUnavailable(form.modelId)" /></template>
              <template #option-leading="{ option }"><VendorMark :vendor="slotVendor(option.value)" :size="28" :unknown="slotUnavailable(option.value)" /></template>
            </AppSelect>
            <label v-if="configurableImageInput && form.modelId.trim()" class="model-image-input"><span>{{ c.imageInput }}</span><AppSelect :model-value="imageInputMode(form.modelId)" :options="imageInputOptions" :aria-label="`${c.primarySlot} · ${c.imageInput}`" size="sm" data-testid="model-configuration-image-input" @update:model-value="changeImageInput(form.modelId, $event)" /></label>
            <ModelEffortPicker v-if="showEffort(form.modelId)" :model-name="modelSlotLabel(form.modelId)" :efforts="modelEffortLevels(form.modelId)" :model-value="form.efforts[form.modelId.trim()] || ''" @update:model-value="setEffort(form.modelId, $event)" />
          </div>
          <div v-for="(modelId, index) in form.auxiliaryModelIds" :key="index" class="model-slot" :data-testid="`model-configuration-aux-${index}`">
            <div class="model-slot-top">
              <span>{{ c.auxiliarySlot }}</span>
              <button type="button" class="model-slot-remove" :aria-label="c.removeModel" :data-testid="`model-configuration-aux-remove-${index}`" @click="removeAuxiliary(index)"><Icon name="close" :size="12" /></button>
            </div>
            <AppSelect :model-value="modelId" :options="optionsFor(modelId)" :aria-label="c.auxiliarySlot" :placeholder="c.selectModel" :loading="catalogLoading" searchable allow-custom custom-position="last" :data-testid="`model-configuration-aux-select-${index}`" @update:model-value="form.auxiliaryModelIds[index] = $event">
              <template #leading><VendorMark :vendor="slotVendor(modelId)" :size="28" :unknown="slotUnavailable(modelId)" /></template>
              <template #option-leading="{ option }"><VendorMark :vendor="slotVendor(option.value)" :size="28" :unknown="slotUnavailable(option.value)" /></template>
            </AppSelect>
            <p v-if="slotUnavailable(modelId)" class="mb-0 mt-1 text-xs text-warn">{{ c.modelUnavailable }}</p>
            <label v-if="configurableImageInput && modelId.trim()" class="model-image-input"><span>{{ c.imageInput }}</span><AppSelect :model-value="imageInputMode(modelId)" :options="imageInputOptions" :aria-label="`${c.auxiliarySlot} · ${c.imageInput}`" size="sm" :data-testid="`model-configuration-aux-image-input-${index}`" @update:model-value="changeImageInput(modelId, $event)" /></label>
            <ModelEffortPicker v-if="showEffort(modelId)" :model-name="modelSlotLabel(modelId)" :efforts="modelEffortLevels(modelId)" :model-value="form.efforts[modelId.trim()] || ''" @update:model-value="setEffort(modelId, $event)" />
          </div>
          <button type="button" class="model-add" data-testid="model-configuration-add-model" :disabled="!canAddModel" :title="canAddModel ? '' : c.noMoreModels" @click="addAuxiliary"><Icon name="plus" :size="14" />{{ c.addModel }}</button>
          <p v-if="form.backend === 'opencode' && form.kind === 'platform'" class="mb-0 text-xs leading-5 text-txt3" data-testid="model-configuration-official-family">{{ c.officialFamilyHint }}</p>
          <p v-if="auxiliaryProblem" class="mb-0 text-xs text-err">{{ auxiliaryProblem }}</p>
          <p v-if="officialProblem" class="mb-0 text-xs text-err" data-testid="model-configuration-official-problem">{{ officialProblem }}</p>
          <template v-if="form.kind !== 'platform'">
            <p class="mb-0 text-xs leading-5 text-txt3">{{ c.manualModelHint }}</p>
            <p v-if="configurableImageInput && form.modelId.trim()" class="mb-0 text-xs leading-5 text-txt3">{{ c.imageInputSharedHint }}</p>
            <template v-if="roleNames.length">
              <p class="mb-0 text-xs font-medium text-txt2">{{ c.auxiliary }}</p>
              <div v-for="role in roleNames" :key="role" class="model-slot" :data-testid="`model-configuration-role-${role}`">
                <div class="model-slot-top"><span>{{ roleLabel(role) }}</span></div>
                <AppSelect :model-value="form.roles[role] || ''" :options="roleOptions(form.roles[role] || '')" :aria-label="roleLabel(role)" :placeholder="c.followMain" :loading="catalogLoading" searchable allow-custom custom-position="last" @update:model-value="form.roles[role] = $event">
                  <template #leading><VendorMark v-if="form.roles[role]" :vendor="slotVendor(form.roles[role] || '')" :size="28" :unknown="slotUnavailable(form.roles[role] || '')" /></template>
                  <template #option-leading="{ option }"><VendorMark v-if="option.value" :vendor="slotVendor(option.value)" :size="28" :unknown="slotUnavailable(option.value)" /></template>
                </AppSelect>
                <label v-if="showRoleImageInput(role)" class="model-image-input"><span>{{ c.imageInput }}</span><AppSelect :model-value="imageInputMode(form.roles[role] || '')" :options="imageInputOptions" :aria-label="`${roleLabel(role)} · ${c.imageInput}`" size="sm" :data-testid="`model-configuration-role-image-input-${role}`" @update:model-value="changeImageInput(form.roles[role] || '', $event)" /></label>
                <ModelEffortPicker v-if="showEffort(form.roles[role] || '')" :model-name="modelSlotLabel(form.roles[role] || '')" :efforts="modelEffortLevels(form.roles[role] || '')" :model-value="form.efforts[(form.roles[role] || '').trim()] || ''" @update:model-value="setEffort(form.roles[role] || '', $event)" />
              </div>
            </template>
            <label v-if="nativeRoute" class="block"><span class="label">{{ c.routeProfile }}</span><AppSelect v-model="form.routeProfile" :options="[{ value: '', label: c.standardRoute }, { value: 'deepseek-native', label: 'DeepSeek native' }]" :aria-label="c.routeProfile" searchable data-testid="model-configuration-route-profile" /></label>
          </template>
        </template>
        <p v-if="error" role="alert" class="break-words text-xs text-err" data-testid="model-configuration-error">{{ error }}</p>
      </div>
      <template #footer><AppButton variant="ghost" :icon="step > 1 ? undefined : 'close'" :disabled="saving" data-testid="model-configuration-back" @click="step > 1 ? step-- : (open = false)">{{ step > 1 ? c.back : c.cancel }}</AppButton><AppButton v-if="step < 3" variant="primary" icon="arrow-right" :disabled="step === 1 ? !form.name.trim() : !sourceValid" data-testid="model-configuration-next" @click="step++">{{ c.next }}</AppButton><AppButton v-else variant="primary" :loading="saving" :disabled="!valid || saving" data-testid="model-configuration-save" @click="save">{{ c.save }}</AppButton></template>
    </CredentialEditorSurface>
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
.model-slot {
  margin: 0 0 8px;
  padding: 10px;
  border: 1px solid rgb(var(--c-line));
  border-radius: 12px;
  background: rgb(var(--c-surface));
}
.model-slot-top {
  display: flex;
  align-items: center;
  min-height: 24px;
  margin-bottom: 8px;
  color: rgb(var(--c-txt2));
  font-size: 12px;
  font-weight: 560;
}
.model-slot-remove {
  margin-left: auto;
  display: inline-flex;
  width: 24px;
  height: 24px;
  align-items: center;
  justify-content: center;
  border: 1px solid transparent;
  border-radius: 6px;
  background: transparent;
  color: rgb(var(--c-txt3));
}
.model-slot-remove:hover {
  border-color: rgb(var(--c-err) / .28);
  background: rgb(var(--c-err) / .08);
  color: rgb(var(--c-err));
}
.model-image-input {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  align-items: center;
  gap: 12px;
  margin-top: 10px;
  color: rgb(var(--c-txt3));
  font-size: 12px;
}
.model-add {
  display: flex;
  width: 100%;
  height: 36px;
  margin: 4px 0 12px;
  align-items: center;
  justify-content: center;
  gap: 6px;
  border: 1px dashed rgb(var(--c-line-strong));
  border-radius: 12px;
  background: transparent;
  color: rgb(var(--c-txt2));
  font-size: 13px;
}
.model-add:hover:not(:disabled) {
  border-color: rgb(var(--c-accent) / .55);
  background: rgb(var(--c-accent) / .08);
  color: rgb(var(--c-accent-2));
}
.model-add:disabled {
  color: rgb(var(--c-txt3));
  cursor: not-allowed;
  opacity: .55;
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
