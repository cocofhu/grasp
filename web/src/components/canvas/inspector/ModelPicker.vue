<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import CredentialProviderLogo from '@/components/workspace/CredentialProviderLogo.vue'
import VendorMark from '@/components/agent/VendorMark.vue'
import Icon from '@/components/ui/Icon.vue'
import type { CliCatalogModel } from '@/lib/api/apiTypes'
import { loadCliModels } from '@/lib/agent/cliCatalog'
import { readCliModel } from '@/lib/agent/cliModelEnv'
import { api, type ConcreteModelBinding, type CredentialItem, type ModelConfiguration, type ModelConnection } from '@/lib/api/api'
import { credentialUsable, isModelCredential, isToolCredential, useRoutingCopy } from '@/lib/agent/modelRouting'
import { isModelVendorCredential } from '@/lib/workspace/credentialKinds'
import type { CanvasAgent } from '../composables/outlets'
import ModelCredentialSelect from './ModelCredentialSelect.vue'
import ModelIdSelect from './ModelIdSelect.vue'
import { credentialModel, credentialVendor } from './modelCredential'

const props = defineProps<{
  /** Credential picked for this node; empty follows the Agent. */
  credentialId: string
  /** Model picked for this node; empty uses the credential's default. */
  modelId: string
  agent: CanvasAgent | null
  /** null while workspace credentials are loading. */
  credentials: CredentialItem[] | null
}>()
const emit = defineEmits<{
  (e: 'update:credentialId', v: string): void
  (e: 'update:modelId', v: string): void
}>()

const { t } = useI18n()
const c = useRoutingCopy()
const summaryBinding = ref<ConcreteModelBinding | null>(null)
const summaryConfiguration = ref<ModelConfiguration | null>(null)
const summaryConnection = ref<ModelConnection | null>(null)
const summaryLoading = ref(false)
const summaryError = ref('')
watch(
  () => [props.agent?.modelBinding, props.agent?.acpBackend] as const,
  async ([binding, toolId], _previous, onCleanup) => {
    let stale = false
    onCleanup(() => { stale = true })
    summaryBinding.value = null
    summaryConfiguration.value = null
    summaryConnection.value = null
    summaryError.value = ''
    if (!binding) { summaryLoading.value = false; return }
    summaryLoading.value = true
    try {
      let resolved = binding.kind === 'inherit' ? (await api.getModelDefaults()).bindingsByBackend?.[toolId || ''] : binding
      if (stale) return
      if (!resolved) { summaryError.value = c.value.missingDefault; return }
      if (resolved.kind === 'configuration') {
        const configurationId = resolved.configurationId
        const response = await api.listModelConfigurations()
        if (stale) return
        const configuration = response.items.find((row) => row.id === configurationId)
        if (!configuration || configuration.state !== 'active') { summaryError.value = c.value.missingConfiguration; return }
        if (configuration.backend !== toolId) { summaryError.value = c.value.configurationBackendMismatch; return }
        summaryConfiguration.value = configuration
        resolved = configuration.binding
      }
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
  if (summaryBinding.value.kind === 'platform' && !isToolCredential(summaryCredential.value, String(props.agent?.acpBackend || ''))) return c.value.missingCredential
  if (summaryBinding.value.kind === 'connection' && !isModelCredential(summaryCredential.value)) return c.value.missingCredential
  return ''
})

const backend = computed(() => String(props.agent?.acpBackend || ''))
const usesModelKey = computed(() => backend.value === 'opencode')
const loading = computed(() => props.credentials === null)
const rows = computed(() => (props.credentials || []).filter(isModelVendorCredential))
const inherited = computed(() => rows.value.find((r) => r.id === (props.agent?.openCodeCredentialId || '')) ?? null)
/** The credential this node will run with: its own pick, else the Agent's. */
const effective = computed(() => (props.credentialId ? rows.value.find((r) => r.id === props.credentialId) ?? null : inherited.value))
const vendor = computed(() => (effective.value ? credentialVendor(effective.value) : ''))
/** CLI Agents pin their model in Agent settings; the node shows it read-only. */
const cliModelId = computed(() => (usesModelKey.value ? '' : readCliModel(props.agent?.env || {})))
const cliCatalog = ref<CliCatalogModel[]>([])
watch(
  backend,
  async (b) => {
    cliCatalog.value = []
    if (!b || b === 'opencode' || props.agent?.modelBinding) return
    const list = await loadCliModels(b)
    if (backend.value === b) cliCatalog.value = list
  },
  { immediate: true },
)
const cliModel = computed(() => cliCatalog.value.find((m) => m.id === cliModelId.value) ?? null)
const studioMetaLink = computed(() => ({
  path: '/agents',
  query: props.agent?.name ? { agent: props.agent.name, studioTab: 'meta' } : {},
}))
const backendLabel = computed(() => (backend.value ? t(`pages.workspace.credentials.kinds.${backend.value}`) : ''))

function setCredential(id: string) {
  const before = vendor.value
  emit('update:credentialId', id)
  const next = id ? rows.value.find((r) => r.id === id) : inherited.value
  if (props.modelId && (!next || credentialVendor(next) !== before)) emit('update:modelId', '')
}

function clearAll() {
  if (props.credentialId) emit('update:credentialId', '')
  if (props.modelId) emit('update:modelId', '')
}
</script>

<template>
  <div class="relative" data-testid="model-picker" :data-state="!agent ? 'no-agent' : agent.modelBinding ? 'binding' : usesModelKey ? 'select' : 'own-auth'">
    <p v-if="!agent" class="text-[12px] text-txt3" data-testid="model-picker-no-agent">{{ t('canvas.inspector.model.needAgent') }}</p>

    <section v-else-if="agent.modelBinding" class="rounded-lg border border-line bg-base/50 p-3" data-testid="model-picker-binding-summary">
      <div class="flex items-center justify-between gap-3"><p class="m-0 text-[12px] font-semibold text-txt">{{ backendLabel }} · {{ summaryConfiguration ? c.configuration : c.source }}</p><RouterLink :to="studioMetaLink" class="shrink-0 text-[11px] text-accent-2" data-testid="model-picker-binding-edit">{{ c.edit }}</RouterLink></div>
      <p v-if="summaryLoading || credentials === null" class="mb-0 mt-2 text-xs text-txt3">{{ c.loading }}</p>
      <p v-else-if="summaryProblem" role="status" class="mb-0 mt-2 text-xs leading-5 text-warn" data-testid="model-picker-binding-error">{{ summaryProblem }}</p>
      <div v-else-if="summaryBinding" class="mt-2 space-y-1.5 text-xs text-txt2">
        <p v-if="agent.modelBinding.kind === 'inherit'" class="m-0 text-[11px] text-txt3">{{ c.inherit }}</p>
        <p v-if="summaryConfiguration" class="m-0 break-words font-medium" data-testid="model-picker-binding-configuration">{{ summaryConfiguration.name }}</p>
        <p class="m-0 break-words" data-testid="model-picker-binding-source">{{ summaryBinding.kind === 'platform' ? c.platform : summaryConnection?.name }} · {{ summaryCredential?.name }}</p>
        <p class="m-0 break-all font-mono" data-testid="model-picker-binding-model">{{ summaryBinding.modelId || c.autoModel }}</p>
        <p v-if="summaryConnection" class="m-0 break-all text-[11px] text-txt3">{{ summaryConnection.protocol }} · {{ summaryConnection.baseUrl }}</p>
        <p v-for="(model, role) in summaryBinding.toolSettings?.modelRoles || {}" :key="role" class="m-0 break-all text-[11px] text-txt3">{{ c[`role_${role}` as keyof typeof c] || role }} · {{ model }}</p>
      </div>
      <div v-if="credentialId || modelId" class="mt-3 flex items-center justify-between gap-2 border-t border-line pt-2 text-[11px] text-warn"><span>{{ c.nodeBindingNotice }}</span><button type="button" class="shrink-0 text-accent-2" data-testid="model-picker-clear-ignored" @click="clearAll">{{ t('canvas.inspector.model.clear') }}</button></div>
    </section>

    <template v-else-if="!usesModelKey">
      <div class="flex items-start gap-2.5 rounded-lg border border-line bg-base/50 px-3 py-2.5" data-testid="model-picker-own-auth">
        <CredentialProviderLogo :provider="backend === 'claude_code' ? 'claude' : backend" match="provider" :configured="true" />
        <div class="min-w-0 flex-1">
          <p class="m-0 text-[12px] font-medium text-txt">{{ t('canvas.inspector.model.ownAuthTitle', { backend: backendLabel }) }}</p>
          <p class="m-0 mt-0.5 text-[11px] leading-5 text-txt3">{{ t('canvas.inspector.model.ownAuthBody') }}</p>
        </div>
      </div>
      <div class="mt-2 flex items-center gap-2 rounded-lg border border-line bg-surface px-2.5 py-2" data-testid="model-picker-cli-model">
        <VendorMark v-if="!cliModelId" :vendor="backend" backend auto :size="22" />
        <VendorMark v-else :vendor="cliModel?.vendor || ''" :unknown="!cliModel" :size="22" />
        <span class="min-w-0 flex-1">
          <span class="block truncate text-[12px] font-medium text-txt">
            {{ cliModelId ? cliModel?.name || cliModelId : t('pages.agentStudio.cliModel.auto') }}
          </span>
          <span class="block truncate font-mono text-[10.5px] text-txt3">
            {{ cliModelId ? `--model ${cliModelId}` : t('pages.agentStudio.cliModel.autoSub', { cli: backendLabel }) }}
          </span>
        </span>
        <RouterLink
          :to="studioMetaLink"
          class="inline-flex shrink-0 items-center gap-0.5 text-[11px] text-accent-2 hover:underline"
          data-testid="model-picker-cli-model-edit"
        >
          {{ t('pages.agentStudio.cliModel.inspectorEdit') }}<Icon name="chevron-right" :size="11" />
        </RouterLink>
      </div>
      <div
        v-if="credentialId || modelId"
        class="mt-2 flex items-center justify-between gap-2 rounded-lg border border-warn/35 bg-warn/[0.08] px-2.5 py-1.5 text-[12px] leading-[1.45] text-warn"
        data-testid="model-picker-ignored"
      >
        <span>{{ t('canvas.inspector.model.ignored') }}</span>
        <button type="button" class="shrink-0 text-[11px] font-medium text-accent-2 hover:underline" data-testid="model-picker-clear-ignored" @click="clearAll">
          {{ t('canvas.inspector.model.clear') }}
        </button>
      </div>
    </template>

    <div v-else class="space-y-3">
      <div>
        <p class="mb-1.5 text-[11px] font-medium text-txt3">{{ t('canvas.inspector.model.credential') }}</p>
        <ModelCredentialSelect
          :model-value="credentialId"
          :rows="rows"
          :inherited="inherited"
          :loading="loading"
          @update:model-value="setCredential"
        />
      </div>
      <div>
        <p class="mb-1.5 text-[11px] font-medium text-txt3">{{ t('canvas.inspector.model.modelLabel') }}</p>
        <ModelIdSelect
          :model-value="modelId"
          :vendor="vendor"
          :default-model="effective ? credentialModel(effective) : ''"
          @update:model-value="emit('update:modelId', $event)"
        />
      </div>
      <p class="text-[11px] leading-5 text-txt3">{{ t('canvas.inspector.model.hint') }}</p>
    </div>
  </div>
</template>
