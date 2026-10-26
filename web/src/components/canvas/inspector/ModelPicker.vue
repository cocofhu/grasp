<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import CredentialProviderLogo from '@/components/workspace/CredentialProviderLogo.vue'
import { api, type ConcreteModelBinding, type CredentialItem, type ModelConfiguration, type ModelConnection } from '@/lib/api/api'
import { credentialUsable, isModelCredential, isToolCredential, useRoutingCopy } from '@/lib/agent/modelRouting'
import type { CanvasAgent } from '../composables/outlets'

const props = defineProps<{
  agent: CanvasAgent | null
  /** null while workspace credentials are loading. */
  credentials: CredentialItem[] | null
}>()
const { t } = useI18n()
const c = useRoutingCopy()
const summaryBinding = ref<ConcreteModelBinding | null>(null)
const summaryConfiguration = ref<ModelConfiguration | null>(null)
const summaryConnection = ref<ModelConnection | null>(null)
const summaryLoading = ref(false)
const summaryError = ref('')
const inheritedBackend = ref('')
watch(
  () => [props.agent?.modelBinding, props.agent?.acpBackend, props.agent?.inheritModel] as const,
  async ([localBinding, localToolId, inheritModel], _previous, onCleanup) => {
    let stale = false
    onCleanup(() => { stale = true })
    summaryBinding.value = null
    summaryConfiguration.value = null
    summaryConnection.value = null
    summaryError.value = ''
    inheritedBackend.value = ''
    summaryLoading.value = true
    try {
      let binding = localBinding
      let toolId = localToolId
      if (inheritModel) {
        const defaults = await api.getSharedAgentConfig()
        if (stale) return
        binding = defaults.modelBinding
        toolId = defaults.acpBackend
        inheritedBackend.value = toolId || ''
      }
      if (!binding?.configurationId) { summaryError.value = c.value.missingConfiguration; return }
      const response = await api.listModelConfigurations()
      if (stale) return
      const configuration = response.items.find((row) => row.id === binding.configurationId)
      if (!configuration || configuration.state !== 'active') { summaryError.value = c.value.missingConfiguration; return }
      if (configuration.backend !== toolId && !(inheritModel && !toolId)) { summaryError.value = c.value.configurationBackendMismatch; return }
      summaryConfiguration.value = configuration
      const resolved = configuration.binding
      if (resolved.kind === 'connection') {
        const response = await api.listModelConnections()
        if (stale) return
        const connection = response.items.find((row) => row.id === resolved.connectionId)
        if (!connection || connection.state !== 'active') { summaryError.value = c.value.missingConnection; return }
        if (!connection.models.some((model) => model.id === resolved.modelId)) { summaryError.value = c.value.missingModel; return }
        summaryConnection.value = connection
      }
      summaryBinding.value = resolved
    } catch (e) {
      if (!stale) summaryError.value = String((e as Error).message || e)
    } finally { if (!stale) summaryLoading.value = false }
  },
  { immediate: true, deep: true },
)
const summaryCredential = computed(() => {
  const binding = summaryBinding.value
  const id = binding?.kind === 'platform' ? binding.credentialId : summaryConnection.value?.credentialId
  return (props.credentials || []).find((item) => item.id === id)
})
const summaryProblem = computed(() => {
  if (summaryError.value) return summaryError.value
  if (!summaryBinding.value || props.credentials === null) return ''
  if (!summaryCredential.value || !credentialUsable(summaryCredential.value)) return c.value.missingCredential
  if (summaryBinding.value.kind === 'platform' && !isToolCredential(summaryCredential.value, backend.value)) return c.value.missingCredential
  if (summaryBinding.value.kind === 'connection' && !isModelCredential(summaryCredential.value)) return c.value.missingCredential
  return ''
})

const backend = computed(() => props.agent?.inheritModel ? summaryConfiguration.value?.backend || inheritedBackend.value : String(props.agent?.acpBackend || ''))
const studioMetaLink = computed(() => ({
  path: '/agents',
  query: props.agent?.name ? { agent: props.agent.name, studioTab: 'meta' } : {},
}))
const backendLabel = computed(() => (backend.value ? t(`pages.workspace.credentials.kinds.${backend.value}`) : ''))

</script>

<template>
  <div class="relative" data-testid="model-picker" :data-state="!agent ? 'no-agent' : 'binding'">
    <p v-if="!agent" class="text-[12px] text-txt3" data-testid="model-picker-no-agent">{{ t('canvas.inspector.model.needAgent') }}</p>

    <section v-else class="rounded-lg border border-line bg-base/50 p-3" data-testid="model-picker-binding-summary">
      <div class="flex items-center justify-between gap-3"><p class="m-0 text-[12px] font-semibold text-txt">{{ backendLabel }} · {{ summaryConfiguration ? c.configuration : c.source }}</p><RouterLink :to="studioMetaLink" class="shrink-0 text-[11px] text-accent-2" data-testid="model-picker-binding-edit">{{ c.edit }}</RouterLink></div>
      <p v-if="summaryLoading || credentials === null" class="mb-0 mt-2 text-xs text-txt3">{{ c.loading }}</p>
      <p v-else-if="summaryProblem" role="status" class="mb-0 mt-2 text-xs leading-5 text-warn" data-testid="model-picker-binding-error">{{ summaryProblem }}</p>
      <div v-else-if="summaryBinding" class="mt-2 space-y-1.5 text-xs text-txt2">
        <p v-if="summaryConfiguration" class="m-0 break-words font-medium" data-testid="model-picker-binding-configuration">{{ summaryConfiguration.name }}</p>
        <p class="m-0 break-words" data-testid="model-picker-binding-source">{{ summaryBinding.kind === 'platform' ? c.platform : summaryConnection?.name }} · {{ summaryCredential?.name }}</p>
        <p class="m-0 break-all font-mono" data-testid="model-picker-binding-model">{{ summaryBinding.modelId || c.autoModel }}</p>
        <p v-if="summaryConnection" class="m-0 break-all text-[11px] text-txt3">{{ summaryConnection.protocol }} · {{ summaryConnection.baseUrl }}</p>
        <p v-for="(model, role) in summaryBinding.toolSettings?.modelRoles || {}" :key="role" class="m-0 break-all text-[11px] text-txt3">{{ c[`role_${role}` as keyof typeof c] || role }} · {{ model }}</p>
      </div>
    </section>

  </div>
</template>
