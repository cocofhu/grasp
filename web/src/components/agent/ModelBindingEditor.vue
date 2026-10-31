<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { api, type CredentialItem, type ModelBinding, type ModelConfiguration, type ModelConnection, type ToolCapability } from '@/lib/api/api'
import { bindingProblem, useRoutingCopy } from '@/lib/agent/modelRouting'
import AppButton from '@/components/ui/AppButton.vue'
import AppSelect from '@/components/ui/AppSelect.vue'
import Icon from '@/components/ui/Icon.vue'
import SlidingTabs from '@/components/ui/SlidingTabs.vue'
import CredentialProviderLogo from '@/components/workspace/CredentialProviderLogo.vue'
import { ACP_BACKENDS } from '@/lib/shared/regionPolicy'
import { boundConfigurationIds } from '@/lib/agent/modelBindings'
import { ensureModelCatalogEvents, modelCatalogRevision } from '@/lib/api/modelCatalogEvents'

const props = withDefaults(defineProps<{ backend?: string; modelValue?: ModelBinding; compact?: boolean; disabled?: boolean; summary?: boolean; summarySource?: string; allowAllConfigurations?: boolean }>(), { compact: false, backend: '', disabled: false, summary: false, summarySource: '', allowAllConfigurations: true })
const emit = defineEmits<{ 'update:modelValue': [value: ModelBinding]; selectConfiguration: [value: ModelConfiguration]; validity: [valid: boolean] }>()
const c = useRoutingCopy()
const configurations = ref<ModelConfiguration[]>([])
const credentials = ref<CredentialItem[]>([])
const connections = ref<ModelConnection[]>([])
const capabilities = ref<ToolCapability[]>([])
const loading = ref(true)
const error = ref('')
const configurationId = computed(() => props.modelValue?.configurationId || '')
const configurationIds = computed(() => boundConfigurationIds(props.modelValue))
const allConfigurations = computed(() => !!props.modelValue?.allConfigurations)
let manualConfigurationIds: string[] | undefined
watch(() => props.modelValue, (value) => {
  if (!value?.allConfigurations) manualConfigurationIds = value ? boundConfigurationIds(value) : undefined
})
const extraOptions = computed(() => configurations.value.filter(row => row.state === 'active' && !configurationIds.value.includes(row.id)).map(row => ({ value: row.id, label: `${row.name} · ${ACP_BACKENDS.find(tool => tool.id === row.backend)?.label || row.backend}` })))
const available = computed(() => configurations.value.filter((row) => (!props.backend || row.backend === props.backend) && row.state === 'active'))
const selected = computed(() => configurations.value.find((row) => row.id === configurationId.value))
const summaryLabel = computed(() => props.summarySource && selected.value ? ACP_BACKENDS.find((row) => row.id === selected.value?.backend)?.label || selected.value.backend : selected.value?.name || c.value.missingConfiguration)
const options = computed(() => available.value.map((row) => ({ value: row.id, label: row.name })))
const extraIds = computed(() => configurationIds.value.filter(id => id !== configurationId.value))
const selectedMeta = computed(() => {
  const row = selected.value
  if (!row) return ''
  const model = row.binding?.modelId
  return model ? `${toolLabel(row.backend)} · ${model}` : toolLabel(row.backend)
})
const primaryProblem = computed(() => bindingProblem(props.modelValue, props.backend || selected.value?.backend || '', connections.value, credentials.value, capabilities.value, configurations.value))
const problem = computed(() => {
  const primary = primaryProblem.value
  if (primary || allConfigurations.value) return primary
  for (const id of extraIds.value) {
    const row = configurations.value.find(row => row.id === id)
    const issue = bindingProblem({ kind: 'configuration', configurationId: id }, row?.backend || '', connections.value, credentials.value, capabilities.value, configurations.value)
    if (issue) return issue
  }
  return ''
})
const valid = computed(() => !loading.value && !error.value && !!configurationId.value && !problem.value)
watch(valid, (value) => emit('validity', value), { immediate: true })
async function load() {
  const generation = ++loadGeneration
  loading.value = true; error.value = ''
  try {
    const [configs, keys, services, caps] = await Promise.all([api.listModelConfigurations(), api.listCredentials(), api.listModelConnections(), api.listToolCapabilities()])
    if (generation !== loadGeneration) return
    configurations.value = configs.items || []; credentials.value = keys.items || []; connections.value = services.items || []; capabilities.value = caps.items || []
  } catch (cause) { if (generation === loadGeneration) error.value = String((cause as Error).message || cause) }
  finally { if (generation === loadGeneration) loading.value = false }
}
let loadGeneration = 0
function bindingWith(ids: string[], primary: string): ModelBinding { return { kind: 'configuration', configurationId: primary, ...(allConfigurations.value ? { allConfigurations: true } : ids.length > 1 ? { configurationIds: ids } : {}) } }
function setAllConfigurations(value: boolean) {
  if (props.disabled || props.summary || !props.allowAllConfigurations || !configurationId.value) return
  if (value) {
    manualConfigurationIds = configurationIds.value
    emit('update:modelValue', { kind: 'configuration', configurationId: configurationId.value, allConfigurations: true })
  } else {
    const ids = [...new Set([configurationId.value, ...(manualConfigurationIds || [])])]
    emit('update:modelValue', { kind: 'configuration', configurationId: configurationId.value, ...(ids.length > 1 ? { configurationIds: ids } : {}) })
  }
}
function select(id: string) {
  const row = available.value.find((row) => row.id === id)
  if (!props.disabled && !props.summary && row && id !== configurationId.value) {
    emit('update:modelValue', bindingWith(configurationIds.value.length > 1 ? [...new Set([...configurationIds.value, id])] : [id], id))
    emit('selectConfiguration', row)
  }
}
function add(id: string) {
  const row = configurations.value.find(row => row.id === id && row.state === 'active')
  if (!row || props.disabled || props.summary || allConfigurations.value) return
  const ids = [...new Set([...configurationIds.value, id])]
  emit('update:modelValue', bindingWith(ids, configurationId.value || id))
  if (!configurationId.value) emit('selectConfiguration', row)
}
function remove(id: string) {
  if (props.disabled || props.summary || allConfigurations.value || id === configurationId.value) return
  emit('update:modelValue', bindingWith(configurationIds.value.filter(value => value !== id), configurationId.value))
}
function makeDefault(id: string) {
  const row = configurations.value.find(row => row.id === id && row.state === 'active')
  if (!row || props.disabled || props.summary || id === configurationId.value) return
  emit('update:modelValue', bindingWith(configurationIds.value, id)); emit('selectConfiguration', row)
}
function setScope(all: boolean) {
  if (all !== allConfigurations.value) setAllConfigurations(all)
}
function onScopeKeydown(event: KeyboardEvent) {
  if (!['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown'].includes(event.key)) return
  event.preventDefault()
  const next = !allConfigurations.value
  if (next && (props.disabled || !configurationId.value)) return
  setScope(next)
  const group = event.currentTarget as HTMLElement
  group.querySelector<HTMLElement>(`[data-scope="${next ? 'all' : 'selected'}"]`)?.focus()
}
function backendOf(id: string) { return configurations.value.find((row) => row.id === id)?.backend || props.backend }
function nameOf(id: string) { return configurations.value.find(row => row.id === id)?.name || id }
function toolLabel(backend: string) { return ACP_BACKENDS.find(tool => tool.id === backend)?.label || backend }
onMounted(load)
ensureModelCatalogEvents()
watch(modelCatalogRevision, load)
</script>

<template>
  <section class="min-w-0" data-testid="model-binding-editor">
    <p v-if="loading" class="text-xs text-txt3">{{ c.loading }}</p>
    <p v-else-if="error" role="alert" class="text-xs text-err">{{ error }} <AppButton size="sm" @click="load">{{ c.retry }}</AppButton></p>
    <template v-else>
      <div v-if="summary" class="flex min-w-0 items-center gap-2.5 py-1 text-[13px] text-txt2" data-testid="model-binding-inherited-summary">
        <span v-if="allConfigurations" class="grid h-8 w-8 shrink-0 place-items-center rounded-lg bg-accent-dim text-accent"><Icon name="layers" :size="18" /></span>
        <CredentialProviderLogo v-else :provider="selected?.backend || backend" match="provider" :selected="!!summarySource" aria-hidden="true" />
        <div class="min-w-0">
          <div class="truncate" :class="summarySource ? 'text-[12px] font-semibold text-txt' : ''" :title="allConfigurations ? c.allConfigurations : selected?.name" :data-testid="allConfigurations ? 'model-binding-all-summary' : undefined">{{ allConfigurations ? c.allConfigurations : summaryLabel }}</div>
          <div v-if="allConfigurations" class="truncate text-[11px] text-txt3">{{ [summarySource, `${c.startupDefault}：${selected?.name || c.missingConfiguration}`].filter(Boolean).join(' · ') }}</div>
          <template v-else>
            <div v-if="summarySource" class="text-[11px] text-txt3">{{ summarySource }}</div>
            <div v-if="configurationIds.length > 1" class="text-[11px] text-txt3">{{ configurationIds.map(nameOf).join(' · ') }}</div>
          </template>
        </div>
      </div>
      <div v-else class="model-binding-fields min-w-0">
        <div :data-testid="allowAllConfigurations ? 'model-binding-startup-default' : undefined">
          <div v-if="allowAllConfigurations" class="mb-2 min-w-0">
            <div class="text-xs font-medium text-txt">{{ c.startupDefault }}</div>
            <div class="mt-px text-[11px] text-txt3">{{ c.startupDefaultHint }}</div>
          </div>
          <AppSelect :model-value="configurationId" :options="options" :aria-label="allowAllConfigurations ? c.startupDefault : c.configuration" :placeholder="configurationId && primaryProblem ? `${selected?.name || configurationId} · ${c.configurationUnavailable}` : available.length ? c.selectConfiguration : c.noConfigurations" :empty-text="c.noConfigurations" :invalid="!!configurationId && !!primaryProblem" :disabled="disabled" searchable data-testid="model-binding-configuration-select" @update:model-value="select">
            <template #leading><CredentialProviderLogo :provider="selected?.backend || backend" match="provider" aria-hidden="true" /></template>
            <template v-if="selectedMeta && !primaryProblem" #trailing><span class="model-binding-configuration-meta min-w-0 max-w-[45%] shrink truncate text-[11px] text-txt3" data-testid="model-binding-configuration-meta">{{ selectedMeta }}</span></template>
            <template #option-leading="{ option }"><CredentialProviderLogo :provider="backendOf(option.value)" match="provider" aria-hidden="true" /></template>
          </AppSelect>
          <p v-if="configurationId && primaryProblem" role="status" class="mb-0 mt-2 flex items-start gap-1.5 text-xs leading-5 text-warn" data-testid="model-binding-problem"><Icon name="warning" :size="13" :interactive="false" class="mt-1 shrink-0" /><span>{{ c[primaryProblem as keyof typeof c] || primaryProblem }}</span></p>
        </div>
        <div v-if="allowAllConfigurations" class="mt-[18px] min-w-0" data-testid="model-binding-available">
          <div class="model-binding-scope-head mb-2 flex min-h-7 items-end justify-between gap-3">
            <div class="min-w-0">
              <div class="text-xs font-medium text-txt">{{ c.availableGroups }}</div>
              <div class="mt-px text-[11px] text-txt3">{{ c.availableGroupsHint }}</div>
            </div>
            <SlidingTabs :value="allConfigurations" indicator-class="rounded-[4px] bg-surface shadow-sm ring-1 ring-line" class="model-binding-scope grid h-7 shrink-0 grid-cols-2 rounded-md border border-line bg-base p-0.5" role="radiogroup" :aria-label="c.scopeLabel" :aria-disabled="disabled || undefined" data-testid="model-binding-scope" @keydown="onScopeKeydown">
              <button type="button" role="radio" data-scope="all" :aria-checked="allConfigurations" :tabindex="allConfigurations ? 0 : -1" :disabled="disabled || !configurationId" :title="!configurationId ? c.needDefaultFirst : undefined" class="model-binding-scope-option" data-testid="model-binding-all-configurations" @click="setScope(true)">{{ c.scopeAll }}</button>
              <button type="button" role="radio" data-scope="selected" :aria-checked="!allConfigurations" :tabindex="allConfigurations ? -1 : 0" :disabled="disabled" class="model-binding-scope-option" data-testid="model-binding-scope-selected" @click="setScope(false)">{{ c.scopeSelected }}</button>
            </SlidingTabs>
          </div>
        </div>
        <div v-if="allowAllConfigurations || configurationId" class="overflow-hidden rounded-lg border border-line bg-surface" :class="allowAllConfigurations ? '' : 'mt-2'" data-testid="model-binding-scope-panel">
          <div v-if="allowAllConfigurations && allConfigurations" class="flex items-start gap-3 px-3.5 py-3" data-testid="model-binding-all-groups">
            <span class="grid h-8 w-8 shrink-0 place-items-center rounded-lg bg-accent-dim text-accent"><Icon name="layers" :size="18" :interactive="false" /></span>
            <div class="min-w-0">
              <div class="text-xs font-semibold text-txt">{{ c.allConfigurations }}</div>
              <p class="m-0 mt-0.5 text-[11px] leading-[1.6] text-txt3" data-testid="model-binding-all-hint">{{ c.allConfigurationsHint }}</p>
            </div>
          </div>
          <p v-else-if="!configurationId" class="m-0 px-3.5 py-3 text-[11px] text-txt3" data-testid="model-binding-no-default">{{ c.noDefaultPanel }}</p>
          <div v-else data-testid="model-binding-groups">
            <div class="flex min-w-0 items-center gap-2.5 py-2 pl-3 pr-2.5 text-xs text-txt3" data-testid="model-binding-default-row">
              <CredentialProviderLogo :provider="backendOf(configurationId)" match="provider" class="shrink-0" aria-hidden="true" />
              <span class="min-w-0 flex-1 truncate">{{ nameOf(configurationId) }}</span>
              <span class="shrink-0 rounded-full bg-accent-dim px-1.5 py-px text-[10px] text-accent-2" data-testid="model-binding-startup-tag">{{ c.startupTag }}</span>
            </div>
            <div v-for="id in extraIds" :key="id" class="model-binding-row group flex min-w-0 items-center gap-2.5 border-t border-line py-2 pl-3 pr-2.5 text-xs text-txt" :data-testid="`model-binding-row-${id}`">
              <CredentialProviderLogo :provider="backendOf(id)" match="provider" class="shrink-0" aria-hidden="true" />
              <span class="min-w-0 flex-1 truncate">{{ nameOf(id) }}</span>
              <span class="shrink-0 text-[11px] text-txt3">{{ toolLabel(backendOf(id)) }}</span>
              <span class="model-binding-row-actions flex shrink-0 gap-0.5 opacity-0 transition-opacity group-hover:opacity-100 group-focus-within:opacity-100">
                <button type="button" class="rounded px-1.5 py-0.5 text-[11px] text-txt3 hover:bg-elevated hover:text-txt disabled:cursor-not-allowed" :disabled="disabled" :data-testid="`model-binding-default-${id}`" @click="makeDefault(id)">{{ c.makeDefault }}</button>
                <button type="button" class="rounded px-1.5 py-0.5 text-[11px] text-txt3 hover:bg-elevated hover:text-err disabled:cursor-not-allowed" :disabled="disabled" :data-testid="`model-binding-remove-${id}`" @click="remove(id)">{{ c.removeModel }}</button>
              </span>
            </div>
            <div v-if="extraOptions.length" class="border-t border-line bg-base p-1">
              <AppSelect model-value="" :options="extraOptions" :disabled="disabled" :placeholder="c.addGroup" :aria-label="c.addGroup" variant="ghost" size="sm" searchable data-testid="model-binding-add-select" @update:model-value="add">
                <template #leading><Icon name="plus" :size="14" :interactive="false" class="shrink-0" /></template>
                <template #option-leading="{ option }"><CredentialProviderLogo :provider="backendOf(option.value)" match="provider" aria-hidden="true" /></template>
              </AppSelect>
            </div>
          </div>
        </div>
        <p v-if="configurationId && problem && !primaryProblem" role="status" class="mb-0 mt-2 flex items-start gap-1.5 text-xs leading-5 text-warn" data-testid="model-binding-problem"><Icon name="warning" :size="13" :interactive="false" class="mt-1 shrink-0" /><span>{{ c[problem as keyof typeof c] || problem }}</span></p>
      </div>
      <p v-if="summary && configurationId && problem" role="status" class="mb-0 mt-2 text-xs leading-5 text-warn" data-testid="model-binding-problem">{{ c[problem as keyof typeof c] || problem }}</p>
    </template>
  </section>
</template>

<style scoped>
.model-binding-fields {
  container-type: inline-size;
}
.model-binding-scope-option {
  min-width: 0;
  border-radius: 4px;
  padding: 0 12px;
  font-size: 12px;
  white-space: nowrap;
  color: rgb(var(--c-txt3));
  transition: color 150ms ease;
}
.model-binding-scope-option:hover:not(:disabled) {
  color: rgb(var(--c-txt));
}
.model-binding-scope-option[aria-checked="true"] {
  color: rgb(var(--c-txt));
  font-weight: 600;
}
.model-binding-scope-option:disabled {
  cursor: not-allowed;
  opacity: .45;
}
.model-binding-scope-option:focus-visible {
  outline: 2px solid rgb(var(--c-accent));
  outline-offset: 1px;
}
@container (max-width: 479px) {
  .model-binding-scope-head {
    flex-direction: column;
    align-items: stretch;
  }
  .model-binding-scope {
    width: 100%;
  }
  .model-binding-configuration-meta {
    display: none;
  }
}
@media (hover: none) {
  .model-binding-row-actions {
    opacity: 1;
  }
}
</style>
