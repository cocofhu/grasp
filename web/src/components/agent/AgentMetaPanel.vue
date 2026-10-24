<script lang="ts">
type ToolRuntimeDraft = Pick<import('@/lib/agent/agentStudioDraft').AgentStudioDraft, 'modelBinding' | 'aiCredentialId' | 'openCodeCredentialId'> & { env: Record<string, string> }
// Studio unmounts this panel when changing tabs; keep unsaved tool choices with
// the draft, and let them be collected when that draft is discarded/reloaded.
const previousToolDrafts = new WeakMap<object, Map<string, ToolRuntimeDraft>>()
</script>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getRegionPolicy, isManagedRegionKey, normalizeRegions, setRegion, switchBackendRegions, type BackendId } from '@/lib/shared/regionPolicy'
import { DEFAULT_WORKSPACE_DIR, defaultConfigRootFor, kvToRec, recToKV, type AgentStudioDraft } from '@/lib/agent/agentStudioDraft'
import { api, type ModelBinding, type AgentGitBindings } from '@/lib/api/api'
import CodingToolPicker from './CodingToolPicker.vue'
import AgentGitCredentialPicker from './AgentGitCredentialPicker.vue'
import EnvCredentialHelpModal from './EnvCredentialHelpModal.vue'
import ModelBindingEditor from './ModelBindingEditor.vue'
import AppModal from '@/components/ui/AppModal.vue'
import AppButton from '@/components/ui/AppButton.vue'
import AgentCredentialBindings from './AgentCredentialBindings.vue'
import RepoAccessPicker from './RepoAccessPicker.vue'
import { legacyGitBindings, setDraftGitBindings } from '@/lib/agent/gitBindings'
import { useRoutingCopy } from '@/lib/agent/modelRouting'
const props = defineProps<{ draft: AgentStudioDraft; agentName: string; focusModelTick?: number }>()
const { t } = useI18n()
const c = useRoutingCopy()
let configRootTouched = false
const sharedRepoIds = ref<string[]>([])
const sharedGitBindings = ref<AgentGitBindings>()
const gitHelpOpen = ref(false)
const pendingBackend = ref<BackendId | null>(null)
const modelEnvKeys = new Set(['ACP_BRIDGE_MODEL', 'ACP_BRIDGE_REASONING_EFFORT', 'GRASP_OPENCODE_PROVIDER', 'GRASP_OPENCODE_BASE_URL', 'GRASP_OPENCODE_MODEL_VISION'])
watch(() => props.agentName, () => { configRootTouched = false; pendingBackend.value = null })
onMounted(async () => { try { const shared = await api.getSharedAgentConfig(); sharedRepoIds.value = shared.repoIds || []; sharedGitBindings.value = shared.gitBindings } catch { /* Repository defaults are optional. */ } })
function selectAcpBackend(id: BackendId) { if (id !== props.draft.acpBackend) pendingBackend.value = id }
function confirmBackend() {
  const id = pendingBackend.value
  if (!id) return
  let previousBindings = previousToolDrafts.get(props.draft)
  if (!previousBindings) { previousBindings = new Map(); previousToolDrafts.set(props.draft, previousBindings) }
  const currentEnv = kvToRec(props.draft.env)
  previousBindings.set(props.draft.acpBackend, {
    modelBinding: props.draft.modelBinding ? JSON.parse(JSON.stringify(props.draft.modelBinding)) : undefined,
    aiCredentialId: props.draft.aiCredentialId,
    openCodeCredentialId: props.draft.openCodeCredentialId,
    env: Object.fromEntries(Object.entries(currentEnv).filter(([key]) => modelEnvKeys.has(key) || isManagedRegionKey(key))),
  })
  const previous = previousBindings.get(id)
  props.draft.acpBackend = id
  if (!configRootTouched) props.draft.layout.configRoot = defaultConfigRootFor(id)
  // An absent binding is a meaningful legacy configuration, not inheritance.
  props.draft.modelBinding = previous ? previous.modelBinding : { kind: 'inherit' }
  props.draft.aiCredentialId = previous?.aiCredentialId || ''
  props.draft.openCodeCredentialId = previous?.openCodeCredentialId || ''
  const env = switchBackendRegions(currentEnv, id)
  for (const key of modelEnvKeys) delete env[key]
  props.draft.env = recToKV({ ...env, ...previous?.env })
  pendingBackend.value = null
}
function setBinding(binding: ModelBinding) { props.draft.modelBinding = binding; props.draft.aiCredentialId = ''; props.draft.openCodeCredentialId = '' }
const currentRegionPolicy = computed(() => getRegionPolicy(props.draft.acpBackend))
const displayRegion = computed(() => { const normalized = normalizeRegions(kvToRec(props.draft.env), props.draft.acpBackend, 'preserve-special'); return normalized.special ? '' : normalized.region })
const specialRegion = computed(() => { const normalized = normalizeRegions(kvToRec(props.draft.env), props.draft.acpBackend, 'preserve-special'); return normalized.special ? normalized.region : '' })
function selectRegion(id: string) { props.draft.env = recToKV(setRegion(kvToRec(props.draft.env), props.draft.acpBackend, id)) }
</script>

<template>
  <div class="scroll-area min-h-0 flex-1 overflow-auto p-4">
    <div class="max-w-3xl space-y-4">
      <section>
        <h3 class="mb-3 mt-0 text-[13px] font-semibold text-txt">{{ t('pages.agentStudio.toolPicker.title') }}</h3>
        <CodingToolPicker
          :model-value="draft.acpBackend"
          :region-policy="currentRegionPolicy"
          :current-region="displayRegion"
          :special-region="specialRegion"
          @update:model-value="selectAcpBackend"
          @select-region="selectRegion"
        />
      </section>
      <ModelBindingEditor :key="agentName" :backend="draft.acpBackend" :model-value="draft.modelBinding" @update:model-value="setBinding" />
      <div class="border-t border-line pt-4"><AgentGitCredentialPicker :model-value="draft.gitBindings" :legacy="legacyGitBindings(draft)" :inherited="sharedGitBindings" @update:model-value="setDraftGitBindings(draft, $event)" @help="gitHelpOpen = true" /></div>
      <RepoAccessPicker
        :key="agentName"
        :model-value="draft.repoIds"
        :inherited="sharedRepoIds"
        @update:model-value="draft.repoIds = $event"
      />
      <label  class="block">
        <span class="text-[12px] font-medium text-txt2">{{ t('pages.agentStudio.meta.configRoot') }}</span>
        <input
          v-model="draft.layout.configRoot"
          :placeholder="defaultConfigRootFor(draft.acpBackend)"
          spellcheck="false"
          data-testid="meta-config-root"
          class="mt-1.5 w-full rounded-md border border-line bg-base px-3 py-2 font-mono text-[12px] text-txt outline-none focus:border-accent"
          @input="configRootTouched = true"
        />
      </label>
      <label  class="block">
        <span class="text-[12px] font-medium text-txt2">{{ t('pages.agentStudio.meta.workspaceDir') }}</span>
        <input
          v-model="draft.layout.workspaceDir"
          :placeholder="DEFAULT_WORKSPACE_DIR"
          spellcheck="false"
          data-testid="meta-workspace-dir"
          class="mt-1.5 w-full rounded-md border border-line bg-base px-3 py-2 font-mono text-[12px] text-txt outline-none focus:border-accent"
        />
      </label>
    </div>

    <EnvCredentialHelpModal :open="gitHelpOpen" section="git" :backend="draft.acpBackend" @close="gitHelpOpen = false" />

    <AgentCredentialBindings  :draft="draft" />

    <p  class="mt-3 max-w-3xl text-[11px] leading-5 text-txt3">
      {{ t('pages.agentStudio.meta.capabilitiesNote') }}
    </p>

    <AppModal :open="!!pendingBackend" :title="t('pages.agentStudio.meta.acpBackend')" :close-on-esc="true" @close="pendingBackend = null"><p class="text-sm leading-6 text-txt2">{{ c.toolChanged }}</p><template #footer><AppButton variant="ghost" @click="pendingBackend = null">{{ c.cancel }}</AppButton><AppButton variant="primary" data-testid="confirm-model-backend-switch" @click="confirmBackend">{{ c.next }}</AppButton></template></AppModal>
  </div>
</template>

<style scoped>
.model-block {
  transition: background-color 600ms ease;
}
.model-block.is-pulse {
  animation: model-pulse 1.4s var(--ease-out-expo);
}
@keyframes model-pulse {
  0%,
  30% {
    background-color: rgb(var(--c-accent) / 0.12);
    box-shadow: inset 3px 0 0 rgb(var(--c-accent));
  }
  100% {
    background-color: transparent;
    box-shadow: inset 3px 0 0 transparent;
  }
}
@media (prefers-reduced-motion: reduce) {
  .model-block.is-pulse {
    animation: none;
    background-color: rgb(var(--c-accent) / 0.08);
  }
}
</style>
