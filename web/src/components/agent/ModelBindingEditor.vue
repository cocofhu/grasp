<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { api, type CredentialItem, type ModelBinding, type ModelConfiguration, type ModelConnection, type ModelRoles, type ToolCapability, type WorkspaceModelDefaults } from '@/lib/api/api'
import { ACP_BACKENDS } from '@/lib/shared/regionPolicy'
import AppButton from '@/components/ui/AppButton.vue'
import AppSelect from '@/components/ui/AppSelect.vue'
import CredentialProviderLogo from '@/components/workspace/CredentialProviderLogo.vue'
import CliModelSelect from './CliModelSelect.vue'
import { bindingProblem, connectionCapabilities, credentialUsable, isToolCredential, useRoutingCopy } from '@/lib/agent/modelRouting'

const props = withDefaults(defineProps<{ backend: string; modelValue?: ModelBinding; allowInherit?: boolean; directOnly?: boolean; compact?: boolean; configurationsOnly?: boolean }>(), { allowInherit: true, directOnly: false, compact: false, configurationsOnly: false })
const emit = defineEmits<{ (e: 'update:modelValue', value: ModelBinding): void; (e: 'validity', valid: boolean): void }>()
const c = useRoutingCopy()
const credentials = ref<CredentialItem[]>([])
const connections = ref<ModelConnection[]>([])
const configurations = ref<ModelConfiguration[]>([])
const capabilities = ref<ToolCapability[]>([])
const defaults = ref<WorkspaceModelDefaults>({ bindingsByBackend: {} })
const loading = ref(true)
const error = ref('')
const binding = computed<ModelBinding>(() => props.modelValue || { kind: 'inherit' })
const mode = computed(() => binding.value.kind)
const direct = computed(() => mode.value === 'platform' || mode.value === 'connection')
const availableConfigurations = computed(() => configurations.value.filter((row) => row.backend === props.backend && row.state === 'active'))
const selectedConfiguration = computed(() => { const value = binding.value; return value.kind === 'configuration' ? configurations.value.find((row) => row.id === value.configurationId) : undefined })
const missingSelection = computed(() => { const value = binding.value; return value.kind === 'configuration' && value.configurationId && !availableConfigurations.value.some((row) => row.id === value.configurationId) })
const platformKeys = computed(() => credentials.value.filter((item) => credentialUsable(item) && isToolCredential(item, props.backend)))
const connectionCaps = computed(() => connectionCapabilities(capabilities.value, props.backend))
const platformAvailable = computed(() => capabilities.value.some((cap) => cap.toolId === props.backend && cap.adapterState === 'existing' && cap.modes.includes('platform')))
const eligible = computed(() => connections.value.filter((connection) => connection.state === 'active' && connectionCapabilities(capabilities.value, props.backend, connection).length))
const selected = computed(() => { const current = binding.value; return current.kind === 'connection' ? connections.value.find((row) => row.id === current.connectionId) : undefined })
const inheritedConfiguration = computed(() => { const value = inherited.value; return value?.kind === 'configuration' ? configurations.value.find((row) => row.id === value.configurationId) : undefined })
const inheritedSummary = computed(() => inherited.value?.kind === 'configuration' ? inheritedConfiguration.value ? configurationSummary(inheritedConfiguration.value) : inherited.value.configurationId : concreteSummary(inherited.value))
const selectedCaps = computed(() => connectionCapabilities(capabilities.value, props.backend, selected.value))
const roles = computed(() => [...new Set(selectedCaps.value.flatMap((cap) => cap.roleNames || []))].filter((role) => role !== 'main'))
const routeProfiles = computed(() => {
  if (!selected.value) return []
  const row = selected.value
  try { const url = new URL(row.baseUrl); if (row.providerId !== 'deepseek' || row.protocol !== 'anthropic-messages' || row.auth.scheme !== 'x-api-key' || url.protocol !== 'https:' || url.host !== 'api.deepseek.com' || url.pathname.replace(/\/$/, '') !== '/anthropic') return [] } catch { return [] }
  return [...new Set(selectedCaps.value.flatMap((cap) => cap.routeProfiles || []))]
})
const configurationId = computed(() => binding.value.kind === 'configuration' ? binding.value.configurationId : '')
const credentialId = computed(() => binding.value.kind === 'platform' ? binding.value.credentialId : '')
const connectionId = computed(() => binding.value.kind === 'connection' ? binding.value.connectionId : '')
const configurationChoice = computed(() => !props.configurationsOnly && props.compact && props.allowInherit && mode.value === 'inherit' ? '__inherit' : configurationId.value)
const configurationOptions = computed(() => [
  ...(props.configurationsOnly ? [] : props.compact ? props.allowInherit ? [{ value: '__inherit', label: c.value.inherit }] : [] : [{ value: '', label: c.value.selectConfiguration }]),
  ...availableConfigurations.value.map((row) => ({ value: row.id, label: row.name })),
])
const credentialOptions = computed(() => [{ value: '', label: c.value.selectCredential }, ...platformKeys.value.map((row) => ({ value: row.id, label: row.name, hint: row.masked }))])
const connectionOptions = computed(() => [{ value: '', label: c.value.selectConnection }, ...eligible.value.map((row) => ({ value: row.id, label: row.name, hint: row.protocol }))])
const modelOptions = computed(() => (selected.value?.models || []).map((model) => ({ value: model.id, label: model.name || model.id, ...(model.name && model.name !== model.id ? { hint: model.id } : {}) })))
const mainModelOptions = computed(() => [{ value: '', label: c.value.selectModel }, ...modelOptions.value])
const roleOptions = computed(() => [{ value: '', label: c.value.followMain }, ...modelOptions.value])
const routeOptions = computed(() => [{ value: '', label: c.value.standardRoute }, ...routeProfiles.value.map((profile) => ({ value: profile, label: profile }))])
const routeProfile = computed(() => binding.value.kind === 'connection' ? binding.value.toolSettings?.routeProfile || '' : '')
function missingOption(value: string, options: { value: string }[]) { return !!value && !options.some((option) => option.value === value) }
const missingCredential = computed(() => missingOption(credentialId.value, credentialOptions.value))
const missingConnection = computed(() => missingOption(connectionId.value, connectionOptions.value))
const missingModel = computed(() => missingOption(modelId.value, mainModelOptions.value))
function roleValue(role: string) { return binding.value.kind === 'connection' ? binding.value.toolSettings?.modelRoles?.[role as keyof ModelRoles] || '' : '' }
function connectionProvider(id: string) { return connections.value.find((row) => row.id === id)?.providerId || '' }
function setRoute(value: string) { if (binding.value.kind === 'connection' && value !== (binding.value.toolSettings?.routeProfile || '')) emit('update:modelValue', { ...binding.value, toolSettings: { ...binding.value.toolSettings, routeProfile: value || undefined } }) }

const problem = computed(() => bindingProblem(props.modelValue, props.backend, connections.value, credentials.value, capabilities.value, defaults.value.bindingsByBackend[props.backend], configurations.value))
const modelId = computed(() => binding.value.kind === 'platform' || binding.value.kind === 'connection' ? binding.value.modelId || '' : '')
const inherited = computed(() => defaults.value.bindingsByBackend[props.backend])
function toolLabel(backend: string) { return ACP_BACKENDS.find((row) => row.id === backend)?.label || backend }
function concreteSummary(value?: ModelBinding) {
  if (!value || (value.kind !== 'platform' && value.kind !== 'connection')) return ''
  const connection = value.kind === 'connection' ? connections.value.find((row) => row.id === value.connectionId) : undefined
  const keyId = value.kind === 'platform' ? value.credentialId : connection?.credentialId
  const key = credentials.value.find((row) => row.id === keyId)
  return [value.kind === 'platform' ? c.value.platform : connection?.name || (value.kind === 'connection' ? value.connectionId : ''), key?.name || keyId, value.modelId || c.value.autoModel].filter(Boolean).join(' · ')
}
function configurationSummary(value: ModelConfiguration) { return [toolLabel(value.backend), concreteSummary(value.binding)].join(' · ') }
async function load() {
  loading.value = true
  error.value = ''
  try { const [keys, rows, caps, baseline, configs] = await Promise.all([api.listCredentials(), api.listModelConnections(), api.listToolCapabilities(), api.getModelDefaults(), api.listModelConfigurations()]); credentials.value = keys.items || []; connections.value = rows.items || []; capabilities.value = caps.items || []; defaults.value = { bindingsByBackend: baseline.bindingsByBackend || {} }; configurations.value = configs.items || [] } catch (e) { error.value = String((e as Error).message || e) } finally { loading.value = false }
}
function choose(kind: ModelBinding['kind']) {
  if (kind === 'inherit') emit('update:modelValue', { kind })
  else if (kind === 'configuration') emit('update:modelValue', { kind, configurationId: '' })
  else if (kind === 'platform') emit('update:modelValue', { kind, credentialId: '' })
  else emit('update:modelValue', { kind, connectionId: '', modelId: '' })
}
function setConfiguration(value: string) {
  if (props.configurationsOnly && !availableConfigurations.value.some((row) => row.id === value)) return
  if (!props.configurationsOnly && props.compact && props.allowInherit && value === '__inherit') { if (mode.value !== 'inherit') emit('update:modelValue', { kind: 'inherit' }); return }
  if (mode.value !== 'configuration' || value !== configurationId.value) emit('update:modelValue', { kind: 'configuration', configurationId: value })
}
function setCredential(value: string) { if (value !== credentialId.value) emit('update:modelValue', { kind: 'platform', credentialId: value, modelId: modelId.value || undefined }) }
function setConnection(value: string) { if (value !== connectionId.value) emit('update:modelValue', { kind: 'connection', connectionId: value, modelId: '' }) }
function setModel(value: string) {
  if ((binding.value.kind !== 'platform' && binding.value.kind !== 'connection') || value === modelId.value) return
  const previous = binding.value.toolSettings?.modelRoles || {}
  const modelRoles: ModelRoles = { ...previous }
  if (modelRoles.main) modelRoles.main = value
  emit('update:modelValue', { ...binding.value, modelId: value, ...(Object.keys(modelRoles).length ? { toolSettings: { ...binding.value.toolSettings, modelRoles } } : {}) })
}
function setRole(role: string, value: string) {
  if (binding.value.kind !== 'connection' || value === roleValue(role)) return
  const modelRoles = { ...binding.value.toolSettings?.modelRoles, [role]: value }
  if (!value) delete modelRoles[role as keyof ModelRoles]
  emit('update:modelValue', { ...binding.value, toolSettings: { ...binding.value.toolSettings, modelRoles } })
}
const valid = computed(() => !loading.value && !error.value && !problem.value && (!props.configurationsOnly || (mode.value === 'configuration' && !!configurationId.value)))
watch(valid, (value) => emit('validity', value), { immediate: true })
onMounted(load)
</script>

<template>
  <section class="min-w-0 rounded-xl border border-line bg-base/30 p-4" data-testid="model-binding-editor">
    <h3 v-if="!compact && !configurationsOnly" class="m-0 text-[13px] font-semibold text-txt">{{ directOnly ? c.source : c.configuration }}</h3>
    <p v-if="!modelValue && !configurationsOnly" class="mt-2 text-xs leading-5 text-txt3">{{ c.legacy }}</p>
    <p v-if="loading" class="text-xs text-txt3">{{ c.loading }}</p>
    <p v-else-if="error" role="alert" class="text-xs text-err">{{ error }} <AppButton size="sm" @click="load">{{ c.retry }}</AppButton></p>
    <template v-else>
      <div v-if="!configurationsOnly && !compact && (!directOnly || allowInherit)" class="my-3 flex flex-wrap gap-2" role="group" :aria-label="c.source">
        <AppButton v-if="!directOnly" :variant="mode === 'configuration' ? 'primary' : 'outline'" size="sm" data-testid="model-binding-configuration" :aria-pressed="mode === 'configuration'" @click="choose('configuration')">{{ c.configuration }}</AppButton>
        <AppButton v-if="allowInherit" :variant="mode === 'inherit' ? 'primary' : 'outline'" size="sm" data-testid="model-binding-inherit" :aria-pressed="mode === 'inherit'" @click="choose('inherit')">{{ c.inherit }}</AppButton>
      </div>
      <div v-if="configurationsOnly || ((compact || mode === 'configuration') && !directOnly)" class="space-y-3" data-testid="model-binding-configuration-panel">
        <div><span v-if="!compact && !configurationsOnly" class="label">{{ c.configuration }}</span>
          <AppSelect :model-value="configurationChoice" :options="configurationOptions" :aria-label="c.configuration" :placeholder="missingSelection ? `${selectedConfiguration?.name || configurationId} · ${c.missingConfiguration}` : configurationsOnly && !availableConfigurations.length ? c.noConfigurations : c.selectConfiguration" :empty-text="configurationsOnly && !availableConfigurations.length ? c.noConfigurations : undefined" :invalid="!!missingSelection || (configurationsOnly && !!configurationId && !!problem)" searchable data-testid="model-binding-configuration-select" @update:model-value="setConfiguration">
            <template #leading><CredentialProviderLogo :provider="selectedConfiguration?.backend || backend" match="provider" aria-hidden="true" /></template>
            <template #option-leading="{ option }"><CredentialProviderLogo v-if="option.value" :provider="backend" match="provider" aria-hidden="true" /></template>
          </AppSelect>
        </div>
        <p v-if="!compact && !configurationsOnly && !availableConfigurations.length" class="text-xs leading-5 text-txt3">{{ c.noConfigurations }}</p>
        <p v-if="!configurationsOnly && selectedConfiguration && !missingSelection" class="break-words text-xs leading-6 text-txt2 [overflow-wrap:anywhere]" data-testid="model-binding-configuration-summary">{{ configurationSummary(selectedConfiguration) }}</p>
      </div>
      <div v-if="!configurationsOnly && mode === 'inherit' && (!compact || inherited)" class="text-xs leading-6 text-txt2 [overflow-wrap:anywhere]" data-testid="model-binding-inherited">
        <template v-if="inherited">{{ c.defaultSource }} · <span v-if="inheritedConfiguration">{{ inheritedConfiguration.name }} · </span>{{ inheritedSummary }}</template><template v-else>{{ c.noDefault }}</template>
      </div>
      <component :is="directOnly ? 'div' : 'details'" v-if="!configurationsOnly" :open="direct || directOnly" class="mt-3" data-testid="model-binding-advanced">
        <summary v-if="!directOnly" class="cursor-pointer text-xs text-txt3">{{ c.advancedDirect }}</summary>
        <div class="my-3 flex flex-wrap gap-2" role="group" :aria-label="c.source">
        <AppButton :variant="mode === 'platform' ? 'primary' : 'outline'" :disabled="!platformAvailable" size="sm" data-testid="model-binding-platform" :aria-pressed="mode === 'platform'" @click="choose('platform')">{{ c.platform }}</AppButton>
        <AppButton :variant="mode === 'connection' ? 'primary' : 'outline'" :disabled="!connectionCaps.length" size="sm" data-testid="model-binding-connection" :aria-pressed="mode === 'connection'" @click="choose('connection')">{{ c.connection }}</AppButton>
        </div>
      <p v-if="!connectionCaps.length" class="text-[11px] leading-5 text-txt3">{{ ['cursor', 'trae'].includes(backend) ? c.noByok : c.noCapability }}</p>
      <div v-if="mode === 'platform'" class="space-y-3">
        <p class="text-xs leading-5 text-txt3">{{ c.platformHint }}</p>
        <div><span class="label">{{ c.credential }}</span>
          <AppSelect :model-value="credentialId" :options="credentialOptions" :aria-label="c.credential" :placeholder="missingCredential ? `${credentials.find((row) => row.id === credentialId)?.name || credentialId} · ${c.missingCredential}` : c.selectCredential" :invalid="missingCredential" searchable data-testid="model-binding-credential" @update:model-value="setCredential">
            <template #leading><CredentialProviderLogo :provider="backend" match="provider" aria-hidden="true" /></template>
            <template #option-leading="{ option }"><CredentialProviderLogo v-if="option.value" :provider="backend" match="provider" aria-hidden="true" /></template>
          </AppSelect>
        </div>
        <label class="block"><span class="label">{{ c.model }}</span><CliModelSelect :backend="backend" :model-value="modelId" variant="compact" hide-command hide-auto-sub @update:model-value="setModel" /></label>
      </div>
      <div v-else-if="mode === 'connection'" class="space-y-3">
        <p class="text-xs leading-5 text-txt3">{{ c.connectionSourceHint }}</p>
        <div><span class="label">{{ c.connection }}</span>
          <AppSelect :model-value="connectionId" :options="connectionOptions" :aria-label="c.connection" :placeholder="missingConnection ? `${selected?.name || connectionId} · ${c.missingConnection}` : c.selectConnection" :invalid="missingConnection" searchable data-testid="model-binding-connection-select" @update:model-value="setConnection">
            <template #leading><CredentialProviderLogo :provider="selected?.providerId" match="provider" aria-hidden="true" /></template>
            <template #option-leading="{ option }"><CredentialProviderLogo v-if="option.value" :provider="connectionProvider(option.value)" match="provider" aria-hidden="true" /></template>
          </AppSelect>
        </div>
        <template v-if="selected">
          <p class="break-all font-mono text-[11px] text-txt3">{{ selected.providerId }} · {{ selected.baseUrl }} · {{ c.unverified }}</p>
          <div><span class="label">{{ c.model }}</span>
            <AppSelect :model-value="modelId" :options="mainModelOptions" :aria-label="c.model" :placeholder="missingModel ? `${modelId} · ${c.missingModel}` : c.selectModel" :invalid="missingModel" searchable data-testid="model-binding-model" @update:model-value="setModel">
              <template #leading><CredentialProviderLogo :provider="selected.providerId" match="provider" aria-hidden="true" /></template>
              <template #option-leading="{ option }"><CredentialProviderLogo v-if="option.value" :provider="selected.providerId" match="provider" aria-hidden="true" /></template>
            </AppSelect>
          </div>
          <div v-if="routeProfiles.length"><span class="label">{{ c.routeProfile }}</span>
            <AppSelect :model-value="routeProfile" :options="routeOptions" :aria-label="c.routeProfile" :placeholder="missingOption(routeProfile, routeOptions) ? `${routeProfile} · ${c.unsupported}` : c.standardRoute" :invalid="missingOption(routeProfile, routeOptions)" searchable data-testid="model-binding-route-profile" @update:model-value="setRoute" />
          </div>
          <fieldset v-if="roles.length" class="space-y-3 border-0 p-0">
            <legend class="mb-2 text-xs font-medium text-txt2">{{ c.auxiliary }}</legend>
            <div v-for="role in roles" :key="role"><span class="label">{{ c[`role_${role}` as keyof typeof c] || role }}</span>
              <AppSelect :model-value="roleValue(role)" :options="roleOptions" :aria-label="c[`role_${role}` as keyof typeof c] || role" :placeholder="missingOption(roleValue(role), roleOptions) ? `${roleValue(role)} · ${c.missingModel}` : c.followMain" :invalid="missingOption(roleValue(role), roleOptions)" searchable :data-testid="`model-binding-role-${role}`" @update:model-value="setRole(role, $event)">
                <template #leading><CredentialProviderLogo :provider="selected.providerId" match="provider" aria-hidden="true" /></template>
                <template #option-leading="{ option }"><CredentialProviderLogo v-if="option.value" :provider="selected.providerId" match="provider" aria-hidden="true" /></template>
              </AppSelect>
            </div>
          </fieldset>
          <div v-if="selectedCaps.some((cap) => cap.evidence?.length)" class="flex flex-wrap gap-3"><a v-for="item in selectedCaps.flatMap((cap) => cap.evidence || [])" :key="item.url" :href="item.url" target="_blank" rel="noopener noreferrer" class="text-[11px] text-accent-2">{{ c.documented }} · {{ item.kind }}</a></div>
        </template>
      </div>
      </component>
      <p v-if="!configurationsOnly && problem && modelValue" role="status" class="mb-0 mt-3 text-xs leading-5 text-warn" data-testid="model-binding-problem">{{ c[problem as keyof typeof c] || problem }}</p>
      <RouterLink v-if="!configurationsOnly" to="/settings/credentials" class="mt-3 inline-block text-xs text-accent-2">{{ c.configureKeys }}</RouterLink>
    </template>
  </section>
</template>
