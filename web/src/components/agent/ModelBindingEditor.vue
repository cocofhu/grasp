<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { api, type CredentialItem, type ModelBinding, type ModelConfiguration, type ModelConnection, type ToolCapability } from '@/lib/api/api'
import { bindingProblem, useRoutingCopy } from '@/lib/agent/modelRouting'
import AppButton from '@/components/ui/AppButton.vue'
import AppSelect from '@/components/ui/AppSelect.vue'
import CredentialProviderLogo from '@/components/workspace/CredentialProviderLogo.vue'

const props = withDefaults(defineProps<{ backend?: string; modelValue?: ModelBinding; compact?: boolean }>(), { compact: false, backend: '' })
const emit = defineEmits<{ 'update:modelValue': [value: ModelBinding]; validity: [valid: boolean] }>()
const c = useRoutingCopy()
const configurations = ref<ModelConfiguration[]>([])
const credentials = ref<CredentialItem[]>([])
const connections = ref<ModelConnection[]>([])
const capabilities = ref<ToolCapability[]>([])
const loading = ref(true)
const error = ref('')
const configurationId = computed(() => props.modelValue?.configurationId || '')
const available = computed(() => configurations.value.filter((row) => (!props.backend || row.backend === props.backend) && row.state === 'active'))
const selected = computed(() => configurations.value.find((row) => row.id === configurationId.value))
const options = computed(() => available.value.map((row) => ({ value: row.id, label: row.name })))
const problem = computed(() => bindingProblem(props.modelValue, props.backend || selected.value?.backend || '', connections.value, credentials.value, capabilities.value, configurations.value))
const valid = computed(() => !loading.value && !error.value && !!configurationId.value && !problem.value)
watch(valid, (value) => emit('validity', value), { immediate: true })
async function load() {
  loading.value = true; error.value = ''
  try {
    const [configs, keys, services, caps] = await Promise.all([api.listModelConfigurations(), api.listCredentials(), api.listModelConnections(), api.listToolCapabilities()])
    configurations.value = configs.items || []; credentials.value = keys.items || []; connections.value = services.items || []; capabilities.value = caps.items || []
  } catch (cause) { error.value = String((cause as Error).message || cause) }
  finally { loading.value = false }
}
function select(id: string) {
  if (id !== configurationId.value && available.value.some((row) => row.id === id)) emit('update:modelValue', { kind: 'configuration', configurationId: id })
}
function backendOf(id: string) { return configurations.value.find((row) => row.id === id)?.backend || props.backend }
onMounted(load)
</script>

<template>
  <section class="min-w-0" data-testid="model-binding-editor">
    <p v-if="loading" class="text-xs text-txt3">{{ c.loading }}</p>
    <p v-else-if="error" role="alert" class="text-xs text-err">{{ error }} <AppButton size="sm" @click="load">{{ c.retry }}</AppButton></p>
    <template v-else>
      <AppSelect :model-value="configurationId" :options="options" :aria-label="c.configuration" :placeholder="configurationId && problem ? `${selected?.name || configurationId} · ${c.configurationUnavailable}` : available.length ? c.selectConfiguration : c.noConfigurations" :empty-text="c.noConfigurations" :invalid="!!configurationId && !!problem" searchable data-testid="model-binding-configuration-select" @update:model-value="select">
        <template #leading><CredentialProviderLogo :provider="selected?.backend || backend" match="provider" aria-hidden="true" /></template>
        <template #option-leading="{ option }"><CredentialProviderLogo :provider="backendOf(option.value)" match="provider" aria-hidden="true" /></template>
      </AppSelect>
      <p v-if="configurationId && problem" role="status" class="mb-0 mt-2 text-xs leading-5 text-warn" data-testid="model-binding-problem">{{ c[problem as keyof typeof c] || problem }}</p>
    </template>
  </section>
</template>
