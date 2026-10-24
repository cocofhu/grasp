<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getRegionPolicy, normalizeRegions, setRegion, switchBackendRegions, type BackendId } from '@/lib/shared/regionPolicy'
import { DEFAULT_WORKSPACE_DIR, defaultConfigRootFor, kvToRec, recToKV, type AgentStudioDraft } from '@/lib/agent/agentStudioDraft'
import CodingToolPicker from './CodingToolPicker.vue'
import AgentGitCredentialPicker from './AgentGitCredentialPicker.vue'
import EnvCredentialHelpModal from './EnvCredentialHelpModal.vue'
import ModelBindingEditor from './ModelBindingEditor.vue'
import AgentCredentialBindings from './AgentCredentialBindings.vue'
import { setDraftGitBindings } from '@/lib/agent/gitBindings'
const props = defineProps<{ draft: AgentStudioDraft; agentName: string; focusModelTick?: number }>()
const { t } = useI18n()
const gitHelpOpen = ref(false)
let configRootTouched = false
watch(() => props.agentName, () => { configRootTouched = false })
function selectBackend(id: BackendId) {
  if (id === props.draft.acpBackend) return
  props.draft.acpBackend = id
  props.draft.modelBinding = { kind: 'configuration', configurationId: '' }
  if (!configRootTouched) props.draft.layout.configRoot = defaultConfigRootFor(id)
  props.draft.env = recToKV(switchBackendRegions(kvToRec(props.draft.env), id))
}
const regionPolicy = computed(() => getRegionPolicy(props.draft.acpBackend))
const region = computed(() => normalizeRegions(kvToRec(props.draft.env), props.draft.acpBackend, 'preserve-special'))
function selectRegion(value: string) { props.draft.env = recToKV(setRegion(kvToRec(props.draft.env), props.draft.acpBackend, value)) }
</script>

<template>
  <div class="scroll-area min-h-0 flex-1 overflow-auto p-4">
    <div class="max-w-3xl space-y-4">
      <section><h3 class="mb-3 mt-0 text-[13px] font-semibold text-txt">{{ t('pages.agentStudio.toolPicker.title') }}</h3><CodingToolPicker :model-value="draft.acpBackend" :region-policy="regionPolicy" :current-region="region.special ? '' : region.region" :special-region="region.special ? region.region : ''" @update:model-value="selectBackend" @select-region="selectRegion" /></section>
      <section><h3 class="mb-3 mt-0 text-[13px] font-semibold text-txt">{{ t('pages.agentStudio.wizard.steps.apiKey') }}</h3><ModelBindingEditor :key="agentName" :backend="draft.acpBackend" :model-value="draft.modelBinding" @update:model-value="draft.modelBinding = $event" /></section>
      <div class="border-t border-line pt-4"><AgentGitCredentialPicker :model-value="draft.gitBindings" @update:model-value="setDraftGitBindings(draft, $event)" @help="gitHelpOpen = true" /></div>
      <label class="block"><span class="text-[12px] font-medium text-txt2">{{ t('pages.agentStudio.meta.configRoot') }}</span><input v-model="draft.layout.configRoot" class="mt-1.5 w-full rounded-md border border-line bg-base px-3 py-2 font-mono text-[13px] text-txt outline-none focus:border-accent" data-testid="meta-config-root" @input="configRootTouched = true" /></label>
      <label class="block"><span class="text-[12px] font-medium text-txt2">{{ t('pages.agentStudio.meta.workspaceDir') }}</span><input v-model="draft.layout.workspaceDir" :placeholder="DEFAULT_WORKSPACE_DIR" class="mt-1.5 w-full rounded-md border border-line bg-base px-3 py-2 font-mono text-[13px] text-txt outline-none focus:border-accent" data-testid="meta-workspace-dir" /></label>
    </div>
    <EnvCredentialHelpModal :open="gitHelpOpen" section="git" :backend="draft.acpBackend" @close="gitHelpOpen = false" />
    <AgentCredentialBindings :draft="draft" />
    <p class="mt-3 max-w-3xl text-[11px] leading-5 text-txt3">{{ t('pages.agentStudio.meta.capabilitiesNote') }}</p>
  </div>
</template>
