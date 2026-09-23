<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppSelect from '@/components/ui/AppSelect.vue'
import { loadOpenCodeModels, loadOpenCodeProviders } from '@/lib/agent/openCodeCatalog'
import {
  OPENCODE_CUSTOM_PROVIDER,
  OPENCODE_FALLBACK_PROVIDERS,
  openCodeModelFromCatalog,
  openCodeModelWithProvider,
  type OpenCodeProviderId,
} from '@/lib/agent/openCodeProvider'
import type { OpenCodeCatalogModel, OpenCodeCatalogProvider } from '@/lib/api/apiTypes'

const props = defineProps<{
  provider: OpenCodeProviderId
  baseUrl: string
  model: string
  vision?: boolean
  requireBase?: boolean
  requireModel?: boolean
}>()

const emit = defineEmits<{
  'update:provider': [value: OpenCodeProviderId]
  'update:baseUrl': [value: string]
  'update:model': [value: string]
  'update:vision': [value: boolean]
}>()

const { t } = useI18n()

const catalog = ref<OpenCodeCatalogProvider[]>([])
const catalogLoading = ref(true)
const models = ref<OpenCodeCatalogModel[]>([])
const modelsLoading = ref(false)

const custom = computed(() => props.provider === OPENCODE_CUSTOM_PROVIDER)

/**
 * Vendors come from the catalog OpenCode itself resolves ids against. When it is
 * unreachable the shipped shortlist stands in, and `custom` is appended either
 * way because a private gateway is never in the catalog.
 */
const providerOptions = computed(() => {
  const customOption = {
    value: OPENCODE_CUSTOM_PROVIDER,
    label: t('pages.agentStudio.openCode.providers.custom'),
    hint: '',
  }
  if (!catalog.value.length) {
    return OPENCODE_FALLBACK_PROVIDERS.map((p) =>
      p.id === OPENCODE_CUSTOM_PROVIDER
        ? customOption
        : { value: p.id, label: t(p.labelKey), hint: p.id },
    )
  }
  // hint carries the id so both "Anthropic" and "anthropic" find the vendor.
  const fromCatalog = catalog.value.map((p) => ({
    value: p.id,
    label: p.name || p.id,
    hint: p.id,
  }))
  return [...fromCatalog, customOption]
})

/**
 * Models of the picked vendor, as the `provider/model` id OpenCode expects.
 *
 * The vendor is prefixed even when the id already starts with the vendor's name:
 * the catalog really does list `openrouter/auto` under `openrouter`, and dropping
 * the prefix there would send OpenCode looking for a model called `auto`.
 */
const modelOptions = computed(() =>
  models.value.map((m) => ({
    value: `${props.provider}/${m.id}`,
    label: m.id,
    hint: m.name && m.name !== m.id ? m.name : '',
  })),
)

/** A hand-typed bare id names the same model as the prefixed one. */
const modelValue = computed(() => openCodeModelWithProvider(props.model, props.provider))

const modelPlaceholder = computed(() =>
  ownEndpoint.value
    ? t('pages.agentStudio.openCode.modelPlaceholderCustom')
    : t('pages.agentStudio.openCode.modelPlaceholder'),
)

/**
 * A model the vendor's catalog does not list is the one mistake that costs a whole
 * sandbox: OpenCode resolves it, fails, and `opencode run` exits 1 with no stderr.
 */
const modelUnknown = computed(() => {
  if (!modelValue.value || ownEndpoint.value || modelsLoading.value || !modelOptions.value.length) {
    return false
  }
  return !modelOptions.value.some((o) => o.value === modelValue.value)
})

/**
 * A vendor the catalog does not list is treated as a self-hosted OpenAI-compatible
 * endpoint: the server writes the adapter and declares the model, so a typed-in
 * vendor id works like a picked one. It does need a base URL of its own.
 */
const selfHostedVendor = computed(() => {
  if (custom.value || catalogLoading.value || !catalog.value.length) return false
  const id = props.provider.trim().toLowerCase()
  if (!id) return false
  return !catalog.value.some((p) => p.id.trim().toLowerCase() === id)
})

/** Vendors whose endpoint and model list are ours to state, not the catalog's. */
const ownEndpoint = computed(() => custom.value || selfHostedVendor.value)

/** A pick from this vendor's catalog listing. Those already declare their own capabilities. */
const listedModel = computed(
  () => !!modelValue.value && modelOptions.value.some((o) => o.value === modelValue.value),
)

/**
 * Image input is opted in here whenever OpenCode cannot look the model up:
 * a private gateway, a typed-in vendor, or a catalog vendor with a hand-typed id.
 * Wait out the vendor's listing so a catalog pick does not flash the switch.
 */
const showVision = computed(() => {
  if (ownEndpoint.value) return true
  if (modelsLoading.value) return false
  return !listedModel.value
})

/** A gateway absent from the catalog has no list to offer; ids are typed in. */
const modelHint = computed(() =>
  ownEndpoint.value || (!modelsLoading.value && !models.value.length)
    ? t('pages.agentStudio.openCode.modelHintTyped')
    : t('pages.agentStudio.openCode.modelHint'),
)

onMounted(async () => {
  catalog.value = await loadOpenCodeProviders()
  catalogLoading.value = false
})

watch(
  () => props.provider,
  async (provider, previous) => {
    // A model picked from the old vendor's listing does not exist on the new one.
    if (
      previous !== undefined &&
      props.model &&
      openCodeModelFromCatalog(props.model, previous, models.value)
    ) {
      emit('update:model', '')
    }
    if (!provider || provider === OPENCODE_CUSTOM_PROVIDER) {
      models.value = []
      modelsLoading.value = false
      return
    }
    modelsLoading.value = true
    const loaded = await loadOpenCodeModels(provider)
    // A slower answer for a vendor the user already left must not land.
    if (props.provider !== provider) return
    models.value = loaded
    modelsLoading.value = false
  },
  { immediate: true },
)
</script>

<template>
  <div class="space-y-3" data-test="opencode-provider-fields">
    <div class="block">
      <span class="mb-1.5 block text-[12px] font-medium text-txt2">
        {{ t('pages.agentStudio.openCode.providerLabel') }}
      </span>
      <AppSelect
        :model-value="provider"
        :options="providerOptions"
        size="sm"
        searchable
        allow-custom
        :loading="catalogLoading"
        :search-placeholder="t('pages.agentStudio.openCode.providerSearchPlaceholder')"
        :aria-label="t('pages.agentStudio.openCode.providerLabel')"
        data-test="opencode-provider"
        @update:model-value="emit('update:provider', $event)"
      />
      <p v-if="selfHostedVendor" class="mt-1 text-[11px] text-txt3" data-test="opencode-provider-self-hosted">
        {{ t('pages.agentStudio.openCode.providerSelfHosted') }}
      </p>
    </div>
    <div class="block">
      <span class="mb-1.5 block text-[12px] font-medium text-txt2">
        {{ t('pages.agentStudio.openCode.modelLabel') }}
        <span class="text-err">*</span>
      </span>
      <AppSelect
        :model-value="modelValue"
        :options="modelOptions"
        size="sm"
        searchable
        allow-custom
        :loading="modelsLoading"
        :invalid="requireModel"
        :placeholder="modelPlaceholder"
        :search-placeholder="t('pages.agentStudio.openCode.modelSearchPlaceholder')"
        :aria-label="t('pages.agentStudio.openCode.modelLabel')"
        data-test="opencode-model"
        @update:model-value="emit('update:model', $event)"
      />
      <p class="mt-1 text-[11px] text-txt3">{{ modelHint }}</p>
      <p v-if="modelUnknown" class="mt-1 text-[11px] text-warn" data-test="opencode-model-unknown">
        {{ t('pages.agentStudio.openCode.modelUnknown') }}
      </p>
      <p v-if="requireModel" class="mt-1 text-[11px] text-err" data-test="opencode-model-required">
        {{ t('pages.agentStudio.openCode.modelRequired') }}
      </p>
      <details
        class="mt-1 rounded-md border border-dashed border-line bg-base px-3 py-2"
        data-test="opencode-advanced"
      >
        <summary class="cursor-pointer text-[11px] text-accent">
          {{ t('pages.agentStudio.openCode.advancedSummary') }}
        </summary>
        <p class="mt-1.5 text-[11px] leading-5 text-txt3">
          {{ t('pages.agentStudio.openCode.advancedHint') }}
        </p>
      </details>
    </div>
    <label class="block">
      <span class="mb-1.5 block text-[12px] font-medium text-txt2">
        {{ t('pages.agentStudio.openCode.baseLabel') }}
        <span v-if="ownEndpoint" class="text-err">*</span>
      </span>
      <input
        :value="baseUrl"
        type="url"
        spellcheck="false"
        class="w-full rounded-md border bg-base px-3 py-2 font-mono text-[12px] text-txt outline-none focus:border-accent"
        :class="requireBase ? 'border-err' : 'border-line'"
        :placeholder="t('pages.agentStudio.openCode.basePlaceholder')"
        data-test="opencode-base-url"
        @input="emit('update:baseUrl', ($event.target as HTMLInputElement).value)"
      />
      <p class="mt-1 text-[11px] text-txt3">{{ t('pages.agentStudio.openCode.baseHint') }}</p>
      <p v-if="requireBase" class="mt-1 text-[11px] text-err" data-test="opencode-base-required">
        {{ t('pages.agentStudio.openCode.baseRequired') }}
      </p>
    </label>
    <label
      v-if="showVision"
      class="flex items-start gap-2.5 rounded-lg border border-line bg-base px-3 py-2.5"
      data-test="opencode-model-vision"
    >
      <input
        type="checkbox"
        class="mt-0.5"
        :checked="!!vision"
        data-test="opencode-model-vision-input"
        @change="emit('update:vision', ($event.target as HTMLInputElement).checked)"
      />
      <span>
        <span class="block text-[12px] font-medium text-txt">
          {{ t('pages.agentStudio.openCode.visionLabel') }}
        </span>
        <span class="mt-0.5 block text-[11px] leading-5 text-txt3">
          {{ t('pages.agentStudio.openCode.visionHint') }}
        </span>
      </span>
    </label>
  </div>
</template>
