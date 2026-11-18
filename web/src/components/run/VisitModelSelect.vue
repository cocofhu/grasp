<script lang="ts">
import type { api as visitModelApi } from '@/lib/api/api'
import type { ScopedSessionModelCatalog } from '@/lib/solo/useSessionModelCatalog'

export type VisitModelTransport = Pick<typeof visitModelApi, 'getVisitModel' | 'setVisitModel'> & {
  modelCatalog: () => Promise<ScopedSessionModelCatalog>
}
</script>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import SoloModelSelect from '@/components/solo/SoloModelSelect.vue'
import { defaultReasoningEffort, listedEfforts } from '@/lib/solo/configuredModels'
import { api } from '@/lib/api/api'
import type { VisitModelChoice } from '@/lib/api/clients/runsClient'
import { modelRouteValue, parseModelRoute } from '@/lib/agent/modelBindings'
import { useSessionModelCatalog } from '@/lib/solo/useSessionModelCatalog'
import { soloErrorText } from '@/lib/solo/soloErrors'
import { useToast } from '@/lib/composables/useToast'

const props = withDefaults(
  defineProps<{
    runId: string
    nodeId: string
    iteration?: number
    agentName: string
    transport?: VisitModelTransport
    disabled?: boolean
    currentConfigurationId?: string
    currentModelId?: string
    currentModelSet?: boolean
    reasoningEffort?: string
    serviceTier?: string
    serviceTiers?: string[]
    modelNotice?: string
  }>(),
  { iteration: 1, currentConfigurationId: '', currentModelId: '', currentModelSet: false, reasoningEffort: '', serviceTier: '', serviceTiers: undefined, modelNotice: '' },
)

const { t, te, locale } = useI18n()
const toast = useToast()
const catalog = useSessionModelCatalog(computed(() => props.agentName), props.transport?.modelCatalog)
const transport = props.transport || api
const configurationId = ref(props.currentConfigurationId)
const modelId = ref(props.currentModelSet ? props.currentModelId : '')
const modelSet = ref(props.currentModelSet)
const effort = ref(props.reasoningEffort)
const serviceTier = ref(props.serviceTier)
const serviceTiers = ref(props.serviceTiers)
const notice = ref(props.modelNotice)
const saving = ref(false)
const localError = ref('')
let generation = 0

function apply(choice: VisitModelChoice) {
  configurationId.value = choice.currentConfigurationId || ''
  modelSet.value = !!choice.currentModelSet
  modelId.value = choice.currentModelSet ? choice.currentModelId || '' : ''
  effort.value = choice.reasoningEffort || ''
  serviceTier.value = choice.serviceTier || ''
  serviceTiers.value = choice.serviceTiers
  notice.value = choice.modelNotice || ''
}

async function reload() {
  const token = ++generation
  try {
    const choice = await transport.getVisitModel(props.runId, props.nodeId, props.iteration)
    if (token !== generation || saving.value) return
    apply(choice)
  } catch (err) {
    if (token !== generation || saving.value) return
    if ((err as { status?: number }).status === 404) apply({ currentModelSet: false })
  }
}

watch(() => [props.runId, props.nodeId, props.iteration], () => void reload(), { immediate: true })
watch(
  () => [props.currentModelSet, props.currentModelId, props.currentConfigurationId, props.reasoningEffort, props.serviceTier, props.serviceTiers, props.modelNotice],
  () => {
    if (saving.value) return
    apply({
      currentConfigurationId: props.currentConfigurationId,
      currentModelId: props.currentModelId,
      currentModelSet: props.currentModelSet,
      reasoningEffort: props.reasoningEffort,
      serviceTier: props.serviceTier,
      serviceTiers: props.serviceTiers,
      modelNotice: props.modelNotice,
    })
  },
)

const currentGroup = computed(() => configurationId.value || catalog.defaultConfigurationId.value || '')
const currentSelection = computed(() => {
  const raw = modelSet.value ? modelId.value : ''
  const group = currentGroup.value
  const choices = catalog.choices.value
  const selected = choices.find(choice => (!group || choice.configurationId === group) && ((choice.modelId ?? choice.id) === raw || !raw))
  if (selected) return selected.id
  return group && raw ? modelRouteValue(group, raw) : raw
})
const shownModels = computed(() => {
  const selectedId = currentSelection.value
  const choices = catalog.choices.value.map(choice => choice.id === selectedId && modelSet.value && configurationId.value && serviceTiers.value !== undefined ? { ...choice, serviceTiers: serviceTiers.value } : choice)
  if (!selectedId || choices.some(choice => choice.id === selectedId)) return choices
  return [...choices, {
    id: selectedId,
    name: modelId.value || selectedId,
    hint: modelId.value,
    vendor: '',
    backend: false,
    unavailable: false,
  }]
})
const noticeText = computed(() => notice.value ? soloErrorText({ code: notice.value, message: notice.value }, t, te, locale.value) : '')
const errorText = computed(() => localError.value || noticeText.value)
const catalogFailedDefault = computed(() => catalog.blocked.value && !modelSet.value)

async function save(nextConfiguration: string, nextModel: string, nextEffort: string, nextServiceTier = serviceTier.value) {
  const previous = { configurationId: configurationId.value, modelId: modelId.value, modelSet: modelSet.value, effort: effort.value, serviceTier: serviceTier.value, notice: notice.value }
  configurationId.value = nextConfiguration
  modelId.value = nextModel
  modelSet.value = true
  effort.value = nextEffort
  serviceTier.value = nextServiceTier
  localError.value = ''
  notice.value = ''
  const token = ++generation
  saving.value = true
  try {
    const choice = await transport.setVisitModel(props.runId, props.nodeId, {
      iteration: props.iteration,
      configurationId: nextConfiguration,
      modelId: nextModel,
      reasoningEffort: nextEffort,
      ...(nextServiceTier || previous.serviceTier || serviceTiers.value?.length ? { serviceTier: nextServiceTier } : {}),
    })
    if (token === generation) apply(choice)
    return token === generation
  } catch (err) {
    if (token !== generation) return false
    configurationId.value = previous.configurationId
    modelId.value = previous.modelId
    modelSet.value = previous.modelSet
    effort.value = previous.effort
    serviceTier.value = previous.serviceTier
    notice.value = previous.notice
    localError.value = soloErrorText(err, t, te, locale.value)
    return false
  } finally {
    if (token === generation) saving.value = false
  }
}

function chooseModel(value: string) {
  const choice = catalog.choices.value.find(item => item.id === value) || (!value ? catalog.choices.value[0] : undefined)
  const grouped = catalog.choices.value.some(item => item.id.startsWith('@group:'))
  const changesGroup = !!choice?.configurationId && !!currentGroup.value && choice.configurationId !== currentGroup.value
  const encoded = choice?.configurationId && (grouped || changesGroup) ? modelRouteValue(choice.configurationId, choice.modelId || '') : value
  const route = parseModelRoute(encoded)
  const levels = listedEfforts(choice?.efforts)
  const nextEffort = levels.includes(effort.value) ? effort.value : defaultReasoningEffort(levels)
  void save(route.configurationId || choice?.configurationId || currentGroup.value, route.modelId || '', nextEffort, choice?.serviceTiers?.includes(serviceTier.value) ? serviceTier.value : '')
}

function chooseEffort(value: string) {
  void save(configurationId.value || currentGroup.value, modelId.value, value)
}
async function chooseServiceTier(value: string) {
  const choice = catalog.choices.value.find(item => item.id === currentSelection.value)
  const levels = listedEfforts(choice?.efforts)
  const nextEffort = levels.length && !levels.includes(effort.value) ? defaultReasoningEffort(levels) : effort.value
  if (await save(configurationId.value || currentGroup.value, modelId.value, nextEffort, value)) {
    toast.show(t('pages.solo.modelSelect.fastModeNextTurn'))
  }
}
</script>

<template>
  <div class="min-w-0" data-testid="visit-model-select">
    <SoloModelSelect
      flat
      :models="shownModels"
      :model-value="currentSelection"
      :reasoning-effort="effort"
      :service-tier="serviceTier"
      :disabled="disabled || saving || !catalog.ready.value"
      :empty-as-label="catalog.ready.value && !shownModels.length && !modelSet"
      @update:model-value="chooseModel"
      @update:reasoning-effort="chooseEffort"
      @update:service-tier="chooseServiceTier"
    />
    <p v-if="catalogFailedDefault" class="mt-1 text-[11px] text-txt2" role="status" data-testid="visit-model-catalog-notice">{{ t('pages.solo.modelSelect.catalogFailedDefault') }}</p>
    <p v-if="errorText" class="mt-1 text-[11px] text-err" data-testid="visit-model-notice">{{ errorText }}</p>
  </div>
</template>
