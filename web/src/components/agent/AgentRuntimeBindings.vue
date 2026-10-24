<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, type AgentGitBindings, type CredentialItem, type ModelBinding, type ModelConfiguration, type SharedAgentConfig } from '@/lib/api/api'
import { ACP_BACKENDS, getRegionPolicy, normalizeRegions, setRegion, switchBackendRegions, type BackendId } from '@/lib/shared/regionPolicy'
import { defaultConfigRootFor, kvToRec, recToKV } from '@/lib/agent/agentStudioDraft'
import AppSwitch from '@/components/ui/AppSwitch.vue'
import AppSelect from '@/components/ui/AppSelect.vue'
import AppButton from '@/components/ui/AppButton.vue'
import ModelBindingEditor from './ModelBindingEditor.vue'
import AgentGitCredentialPicker from './AgentGitCredentialPicker.vue'

type BindingDraft = {
  acpBackend: BackendId
  modelBinding?: ModelBinding
  gitBindings?: AgentGitBindings
  inheritModel?: boolean
  inheritGit?: boolean
  env: { k: string; v: string }[]
  layout?: { configRoot: string }
  configRoot?: string
}
const props = withDefaults(defineProps<{
  draft: BindingDraft
  inheritance?: boolean
  section?: 'all' | 'model' | 'git'
  defaults?: SharedAgentConfig
  items?: CredentialItem[]
  disabled?: boolean
}>(), { inheritance: false, section: 'all', disabled: false })
const emit = defineEmits<{ modelValidity: [value: boolean]; gitValidity: [value: boolean]; defaultsLoaded: [value: SharedAgentConfig]; help: [] }>()
const { t, locale } = useI18n()
const shared = ref<SharedAgentConfig>()
const loading = ref(false)
const error = ref('')
const baseline = computed(() => props.defaults || shared.value)
const inheritModel = computed(() => props.inheritance && !!props.draft.inheritModel)
const inheritGit = computed(() => props.inheritance && !!props.draft.inheritGit)
const model = computed(() => inheritModel.value ? baseline.value?.modelBinding : props.draft.modelBinding)
const git = computed(() => inheritGit.value ? baseline.value?.gitBindings || { items: [] } : props.draft.gitBindings)
const modelValid = ref(false)
const gitValid = ref(true)
const waiting = computed(() => loading.value || !baseline.value)
watch([modelValid, inheritModel, waiting], () => emit('modelValidity', modelValid.value && (!inheritModel.value || !waiting.value)), { immediate: true })
watch([gitValid, inheritGit, waiting], () => emit('gitValidity', gitValid.value && (!inheritGit.value || !waiting.value)), { immediate: true })
const policy = computed(() => getRegionPolicy(props.draft.acpBackend))
const region = computed(() => normalizeRegions(kvToRec(props.draft.env), props.draft.acpBackend, 'preserve-special'))
const regionOptions = computed(() => policy.value?.options.map((option) => ({ value: option.id, label: t(option.labelKey) })) || [])
const unavailable = computed(() => locale.value.startsWith('en') ? 'Default configuration is unavailable.' : '默认配置不可用。')
const missingDefaultModel = computed(() => locale.value.startsWith('en') ? 'Select a model in Agent defaults first.' : '请先在 Agent 默认配置中选择模型。')

async function loadDefaults() {
  if (!props.inheritance || props.defaults) return
  loading.value = true; error.value = ''
  try { shared.value = await api.getSharedAgentConfig(); emit('defaultsLoaded', shared.value) }
  catch (cause) { error.value = String((cause as Error).message || cause) }
  finally { loading.value = false }
}
function selectModel(configuration: ModelConfiguration) {
  if (props.disabled || inheritModel.value || !ACP_BACKENDS.some((row) => row.id === configuration.backend)) return
  const backend = configuration.backend as BackendId
  const oldRoot = defaultConfigRootFor(props.draft.acpBackend)
  if (props.draft.layout?.configRoot === oldRoot) props.draft.layout.configRoot = defaultConfigRootFor(backend)
  if (props.draft.configRoot === oldRoot) props.draft.configRoot = defaultConfigRootFor(backend)
  props.draft.env = recToKV(switchBackendRegions(kvToRec(props.draft.env), backend))
  props.draft.acpBackend = backend
}
function selectRegion(value: string) {
  props.draft.env = recToKV(setRegion(kvToRec(props.draft.env), props.draft.acpBackend, value))
}
onMounted(loadDefaults)
</script>

<template>
  <div class="min-w-0 space-y-5" data-testid="agent-runtime-bindings">
    <section v-if="section !== 'git'" class="min-w-0 space-y-3">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <h3 class="m-0 text-[13px] font-semibold text-txt">{{ t('pages.agentStudio.wizard.steps.apiKey') }}</h3>
        <label v-if="inheritance" class="flex items-center gap-2 text-xs text-txt2">
          <span>{{ t('pages.agentStudio.cliModel.inheritDefault') }}</span>
          <AppSwitch v-model="draft.inheritModel" :disabled="disabled" :aria-label="`${t('pages.agentStudio.wizard.steps.apiKey')} · ${t('pages.agentStudio.cliModel.inheritDefault')}`" data-testid="agent-inherit-model" />
        </label>
      </div>
      <p v-if="inheritModel && waiting" role="status" class="m-0 text-xs text-warn">{{ loading ? t('common.loading.inProgress') : unavailable }} <AppButton v-if="error" size="sm" @click="loadDefaults">{{ t('common.buttons.retry') }}</AppButton></p>
      <p v-else-if="inheritModel && !model?.configurationId" role="status" class="m-0 text-xs text-warn">{{ missingDefaultModel }}</p>
      <ModelBindingEditor :model-value="model" :disabled="disabled || inheritModel" @update:model-value="draft.modelBinding = $event" @select-configuration="selectModel" @validity="modelValid = $event" />
      <AppButton v-if="!inheritance && model?.configurationId" size="sm" variant="ghost" data-testid="default-model-reset" @click="draft.modelBinding = undefined">{{ t('common.buttons.reset') }}</AppButton>
      <label v-if="policy && !inheritModel" class="block max-w-xs">
        <span class="mb-1.5 block text-xs text-txt2">{{ t('pages.agentStudio.toolPicker.site') }}</span>
        <AppSelect :model-value="region.special ? '' : region.region" :placeholder="region.special ? region.region : ''" :options="regionOptions" :disabled="disabled" :aria-label="t('pages.agentStudio.region.title')" searchable data-testid="agent-model-region" @update:model-value="selectRegion" />
        <span v-if="region.special" class="mt-1.5 block text-[11px] text-txt3" role="status">{{ t('pages.agentStudio.region.special', { value: region.region }) }}</span>
      </label>
    </section>
    <section v-if="section !== 'model'" class="min-w-0 space-y-3" :class="section === 'all' ? 'border-t border-line pt-4' : ''">
      <p v-if="inheritGit && waiting" role="status" class="m-0 text-xs text-warn">{{ loading ? t('common.loading.inProgress') : unavailable }} <AppButton v-if="error" size="sm" @click="loadDefaults">{{ t('common.buttons.retry') }}</AppButton></p>
      <AgentGitCredentialPicker :model-value="git" :items="items" :disabled="disabled || inheritGit" @update:model-value="draft.gitBindings = $event" @validity="gitValid = $event" @help="emit('help')">
        <template v-if="inheritance" #heading-actions>
          <label class="flex shrink-0 items-center gap-2 text-xs text-txt2">
            <span>{{ t('pages.agentStudio.cliModel.inheritDefault') }}</span>
            <AppSwitch v-model="draft.inheritGit" :disabled="disabled" :aria-label="`Git · ${t('pages.agentStudio.cliModel.inheritDefault')}`" data-testid="agent-inherit-git" />
          </label>
        </template>
      </AgentGitCredentialPicker>
    </section>
  </div>
</template>
