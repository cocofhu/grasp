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
import type { CredentialItem } from '@/lib/api/api'
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
    if (!b || b === 'opencode') return
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
  <div class="relative" data-testid="model-picker" :data-state="!agent ? 'no-agent' : usesModelKey ? 'select' : 'own-auth'">
    <p v-if="!agent" class="text-[12px] text-txt3" data-testid="model-picker-no-agent">{{ t('canvas.inspector.model.needAgent') }}</p>

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
