<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, type AgentGitBindings, type CredentialItem, type ModelBinding, type ModelConfiguration, type SharedAgentConfig } from '@/lib/api/api'
import { ACP_BACKENDS, getRegionPolicy, normalizeRegions, setRegion, switchBackendRegions, type BackendId } from '@/lib/shared/regionPolicy'
import { defaultConfigRootFor, kvToRec, recToKV } from '@/lib/agent/agentStudioDraft'
import { ensureModelCatalogEvents, modelCatalogRevision } from '@/lib/api/modelCatalogEvents'
import AppSwitch from '@/components/ui/AppSwitch.vue'
import AppSelect from '@/components/ui/AppSelect.vue'
import AppButton from '@/components/ui/AppButton.vue'
import Icon from '@/components/ui/Icon.vue'
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
  collapseInherited?: boolean
  hideManageLink?: boolean
  section?: 'all' | 'model' | 'git'
  defaults?: SharedAgentConfig
  items?: CredentialItem[]
  disabled?: boolean
}>(), { inheritance: false, collapseInherited: false, hideManageLink: false, section: 'all', disabled: false })
const emit = defineEmits<{ modelValidity: [value: boolean]; gitValidity: [value: boolean]; defaultsLoaded: [value: SharedAgentConfig]; help: [] }>()
const { t, locale } = useI18n()
const shared = ref<SharedAgentConfig>()
const loading = ref(false)
const error = ref('')
const baseline = computed(() => props.defaults || shared.value)
const inheritModel = computed(() => props.inheritance && !!props.draft.inheritModel)
const inheritGit = computed(() => props.inheritance && !!props.draft.inheritGit)
const collapsed = computed(() => props.collapseInherited && (inheritModel.value || inheritGit.value))
function setInheritance(value: boolean) { props.draft.inheritModel = value; props.draft.inheritGit = value }
const inheritedFrom = computed(() => {
  if (inheritModel.value && inheritGit.value) return t('pages.agentStudio.cliModel.inheritDefaultFrom')
  if (locale.value.startsWith('en')) return inheritModel.value ? 'Default model · Custom Git' : 'Custom model · Default Git'
  return inheritModel.value ? '模型使用默认配置 · Git 使用自定义配置' : '模型使用自定义配置 · Git 使用默认配置'
})
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
const customConfiguration = computed(() => locale.value.startsWith('en') ? 'Custom configuration' : '自定义配置')
const customConfigurationHint = computed(() => locale.value.startsWith('en') ? 'Settings for this Agent' : '仅用于当前 Agent')
const missingDefaultModel = computed(() => locale.value.startsWith('en') ? 'Select a model in Agent defaults first.' : '请先在默认配置中选择模型。')

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
ensureModelCatalogEvents()
watch(modelCatalogRevision, loadDefaults)
</script>

<template>
  <div class="min-w-0 space-y-5" data-testid="agent-runtime-bindings">
    <section v-if="inheritance && collapseInherited" class="flex h-[120px] shrink-0 flex-col rounded-xl border border-line bg-base px-3.5 py-3" data-test="agent-inherit-default" data-testid="agent-inherit-default">
      <div class="flex shrink-0 items-center justify-between gap-4">
        <span class="text-[13px] font-medium text-txt">{{ t('pages.agentStudio.cliModel.inheritDefault') }}</span>
        <AppSwitch :model-value="collapsed" :disabled="disabled" :aria-label="t('pages.agentStudio.cliModel.inheritDefault')" data-test="agent-inherit-default-switch" data-testid="agent-inherit-default-switch" @update:model-value="setInheritance" />
      </div>
      <div class="scroll-area mt-3 flex min-h-0 flex-1 flex-col overflow-y-auto border-t border-line/70 pt-2">
        <template v-if="collapsed">
          <p v-if="waiting" role="status" class="m-0 text-xs text-warn">{{ loading ? t('common.loading.inProgress') : unavailable }} <AppButton v-if="error" size="sm" @click="loadDefaults">{{ t('common.buttons.retry') }}</AppButton></p>
          <template v-else>
            <ModelBindingEditor v-if="model?.configurationId" :model-value="model" summary :summary-source="inheritedFrom" :disabled="disabled" @validity="modelValid = $event" />
            <p v-else role="status" class="m-0 text-xs text-warn">{{ missingDefaultModel }}</p>
            <AgentGitCredentialPicker :model-value="git" :items="items" summary compact :disabled="disabled" @validity="gitValid = $event" />
          </template>
        </template>
        <div v-else class="flex min-w-0 items-center gap-2.5 py-1" data-testid="agent-inherit-custom-summary">
          <span class="grid h-8 w-8 shrink-0 place-items-center rounded-lg border border-line bg-surface text-txt2"><Icon name="settings" :size="18" /></span>
          <div class="min-w-0"><div class="truncate text-[12px] font-semibold text-txt">{{ customConfiguration }}</div><div class="truncate text-[11px] text-txt3">{{ customConfigurationHint }}</div></div>
        </div>
      </div>
    </section>
    <section v-if="!collapsed && section !== 'git'" class="min-w-0 space-y-3">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <h3 class="m-0 text-[13px] font-semibold text-txt">{{ t('pages.agentStudio.wizard.steps.apiKey') }}</h3>
        <label v-if="inheritance && !collapseInherited" class="flex items-center gap-2 text-xs text-txt2">
          <span>{{ t('pages.agentStudio.cliModel.inheritDefault') }}</span>
          <AppSwitch v-model="draft.inheritModel" :disabled="disabled" :aria-label="`${t('pages.agentStudio.wizard.steps.apiKey')} · ${t('pages.agentStudio.cliModel.inheritDefault')}`" data-testid="agent-inherit-model" />
        </label>
      </div>
      <p v-if="inheritModel && waiting" role="status" class="m-0 text-xs text-warn">{{ loading ? t('common.loading.inProgress') : unavailable }} <AppButton v-if="error" size="sm" @click="loadDefaults">{{ t('common.buttons.retry') }}</AppButton></p>
      <p v-else-if="inheritModel && !model?.configurationId" role="status" class="m-0 text-xs text-warn">{{ missingDefaultModel }}</p>
      <ModelBindingEditor :model-value="model" :summary="inheritModel" :disabled="disabled" @update:model-value="draft.modelBinding = $event" @select-configuration="selectModel" @validity="modelValid = $event" />
      <AppButton v-if="!inheritance && model?.configurationId" size="sm" variant="ghost" data-testid="default-model-reset" @click="draft.modelBinding = undefined">{{ t('common.buttons.reset') }}</AppButton>
      <label v-if="policy && !inheritModel" class="block max-w-xs">
        <span class="mb-1.5 block text-xs text-txt2">{{ t('pages.agentStudio.toolPicker.site') }}</span>
        <AppSelect :model-value="region.special ? '' : region.region" :placeholder="region.special ? region.region : ''" :options="regionOptions" :disabled="disabled" :aria-label="t('pages.agentStudio.region.title')" searchable data-testid="agent-model-region" @update:model-value="selectRegion" />
        <span v-if="region.special" class="mt-1.5 block text-[11px] text-txt3" role="status">{{ t('pages.agentStudio.region.special', { value: region.region }) }}</span>
      </label>
    </section>
    <section v-if="!collapsed && section !== 'model'" class="min-w-0 space-y-3" :class="section === 'all' ? 'border-t border-line pt-4' : ''">
      <p v-if="inheritGit && waiting" role="status" class="m-0 text-xs text-warn">{{ loading ? t('common.loading.inProgress') : unavailable }} <AppButton v-if="error" size="sm" @click="loadDefaults">{{ t('common.buttons.retry') }}</AppButton></p>
      <AgentGitCredentialPicker :model-value="git" :items="items" :summary="inheritGit" :hide-manage-link="collapseInherited || hideManageLink" :disabled="disabled" @update:model-value="draft.gitBindings = $event" @validity="gitValid = $event" @help="emit('help')">
        <template v-if="inheritance && !collapseInherited" #heading-actions>
          <label class="flex shrink-0 items-center gap-2 text-xs text-txt2">
            <span>{{ t('pages.agentStudio.cliModel.inheritDefault') }}</span>
            <AppSwitch v-model="draft.inheritGit" :disabled="disabled" :aria-label="`Git · ${t('pages.agentStudio.cliModel.inheritDefault')}`" data-testid="agent-inherit-git" />
          </label>
        </template>
      </AgentGitCredentialPicker>
    </section>
  </div>
</template>
