<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import AppSelect from '@/components/ui/AppSelect.vue'

const props = withDefaults(
  defineProps<{
    options: readonly string[]
    required?: boolean
    disabled?: boolean
    ariaLabel?: string
  }>(),
  { required: false, disabled: false, ariaLabel: undefined },
)
const model = defineModel<string>({ default: '' })
const { t } = useI18n()

const items = computed(() => {
  const opts = props.options.map((v) => ({ value: v, label: v }))
  // Keep a stored value the schema does not list so it is never silently replaced.
  if (model.value && !props.options.includes(model.value)) opts.unshift({ value: model.value, label: model.value })
  return props.required ? opts : [{ value: '', label: t('pages.artifactEditor.enumUnset') }, ...opts]
})
</script>

<template>
  <AppSelect
    :model-value="model"
    :options="items"
    :disabled="disabled"
    :aria-label="ariaLabel"
    size="sm"
    class="ed-enum"
    @update:model-value="model = $event"
  />
</template>
