<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import VendorMark from '@/components/agent/VendorMark.vue'
import SoloPickerShell from './SoloPickerShell.vue'
import type { ConfiguredModelChoice } from '@/lib/solo/configuredModels'

const props = withDefaults(
  defineProps<{ models: ConfiguredModelChoice[]; modelValue: string; disabled?: boolean; emptyAsLabel?: boolean }>(),
  { disabled: false, emptyAsLabel: false },
)
const emit = defineEmits<{ (e: 'update:modelValue', v: string): void }>()
const { t } = useI18n()

const current = computed(() => props.models.find((model) => model.id === props.modelValue) ?? props.models[0] ?? null)
const label = computed(() => {
  if (!props.models.length && props.emptyAsLabel) return t('pages.solo.modelSelect.empty')
  return current.value?.name || t('pages.solo.modelSelect.placeholder')
})

function groups(q: string) {
  const query = q.trim().toLowerCase()
  return props.models.filter((model) => !query || model.name.toLowerCase().includes(query) || model.hint.toLowerCase().includes(query) || model.id.toLowerCase().includes(query))
}

function choose(model: ConfiguredModelChoice, close: () => void) {
  if (model.unavailable) return
  const primary = props.models[0]
  emit('update:modelValue', primary && model.id === primary.id ? '' : model.id)
  close()
}

function selected(model: ConfiguredModelChoice) {
  if (!props.modelValue) return model.id === (props.models[0]?.id || '')
  return model.id === props.modelValue
}
</script>

<template>
  <SoloPickerShell
    :label="label"
    :placeholder="!current"
    :disabled="disabled || !models.length"
    :search-placeholder="t('pages.solo.modelSelect.search')"
    test-id="solo-model-select"
  >
    <template #trigger>
      <VendorMark v-if="current" :vendor="current.vendor" :backend="current.backend" :auto="current.backend && !current.id" :size="20" />
      <Icon v-else name="sparkles" :size="14" class="shrink-0 text-accent" />
    </template>
    <template #default="{ query, close }">
      <div v-if="!models.length" class="solo-picker-empty">{{ t('pages.solo.modelSelect.empty') }}</div>
      <template v-else>
        <button
          v-for="model in groups(query)"
          :key="model.id || 'auto'"
          type="button"
          role="option"
          data-picker-opt
          :disabled="model.unavailable"
          :aria-selected="selected(model)"
          :data-testid="`solo-model-option-${model.id || 'auto'}`"
          @click="choose(model, close)"
        >
          <VendorMark :vendor="model.vendor" :backend="model.backend" :auto="model.backend && !model.id" :size="20" :unknown="model.unavailable" />
          <span class="min-w-0 flex-1">
            <span class="block truncate">{{ model.name }}</span>
            <span v-if="model.hint && model.hint !== model.name" class="block truncate font-mono text-[11px] text-txt3">{{ model.hint }}</span>
          </span>
          <span v-if="model.unavailable" class="shrink-0 text-[11px] text-txt3">{{ t('pages.solo.modelSelect.unavailable') }}</span>
          <Icon v-else-if="selected(model)" name="check" :size="14" class="shrink-0 text-accent" />
        </button>
        <div v-if="!groups(query).length" class="solo-picker-empty">{{ t('pages.solo.modelSelect.noMatch') }}</div>
      </template>
    </template>
  </SoloPickerShell>
</template>
