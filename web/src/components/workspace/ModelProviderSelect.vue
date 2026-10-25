<script setup lang="ts">
import { computed } from 'vue'
import AppSelect from '@/components/ui/AppSelect.vue'
import Icon from '@/components/ui/Icon.vue'
import CredentialProviderLogo from './CredentialProviderLogo.vue'
import { MODEL_PROVIDER_PRESETS, useRoutingCopy } from '@/lib/agent/modelRouting'

const props = withDefaults(defineProps<{ modelValue: string; disabled?: boolean; extraProviders?: string[] }>(), { disabled: false, extraProviders: () => [] })
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const c = useRoutingCopy()
const options = computed(() => {
  const known = MODEL_PROVIDER_PRESETS.map((preset) => ({ value: preset.id, label: preset.label }))
  const custom = [...new Set([...props.extraProviders, props.modelValue])].filter((id) => id && id !== 'custom' && !known.some((entry) => entry.value === id))
  return [...known, ...custom.map((id) => ({ value: id, label: id })), { value: 'custom', label: c.value.customProvider }]
})
</script>

<template>
  <AppSelect :model-value="modelValue" :options="options" :disabled="disabled" :aria-label="c.provider" searchable @update:model-value="emit('update:modelValue', $event)">
    <template #leading>
      <Icon v-if="modelValue === 'custom'" name="server" :size="20" class="mx-0.5 shrink-0 text-txt3" />
      <CredentialProviderLogo v-else :provider="modelValue" match="provider" class="!h-6 !w-6 !rounded !border-0" aria-hidden="true" />
    </template>
    <template #option-leading="{ option }">
      <Icon v-if="option.value === 'custom'" name="server" :size="20" class="mx-0.5 shrink-0 text-txt3" />
      <CredentialProviderLogo v-else :provider="option.value" match="provider" class="!h-6 !w-6 !rounded !border-0" aria-hidden="true" />
    </template>
  </AppSelect>
</template>
