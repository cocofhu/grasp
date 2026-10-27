<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { effortLabel, validEffortToken } from '@/lib/agent/fixedEffort'

const props = defineProps<{ modelValue: string; efforts: string[] }>()
const emit = defineEmits<{ (e: 'update:modelValue', value: string): void }>()
const { locale } = useI18n()
const zh = computed(() => locale.value.toLowerCase().startsWith('zh'))
const choices = computed(() => props.efforts.map((value) => ({ value, label: effortLabel(value, locale.value) })))
const known = computed(() => new Set(['', ...props.efforts]))
const draft = ref('')
watch(() => [props.modelValue, props.efforts.join('\0')] as const, () => {
  draft.value = known.value.has(props.modelValue) ? '' : props.modelValue
}, { immediate: true })

function choose(value: string) {
  emit('update:modelValue', value)
}
function onCustom(event: Event) {
  const value = (event.target as HTMLInputElement).value
  draft.value = value
  const trimmed = value.trim()
  if (trimmed === '' || validEffortToken(trimmed)) emit('update:modelValue', trimmed)
}
</script>

<template>
  <div class="effort">
    <div v-if="efforts.length" class="effort-menu" data-testid="model-configuration-effort">
      <span class="effort-label">{{ zh ? '思考强度' : 'Reasoning effort' }}</span>
      <div class="effort-options" role="radiogroup" :aria-label="zh ? '思考强度' : 'Reasoning effort'">
        <button type="button" role="radio" data-testid="model-effort-default" :aria-checked="modelValue === ''" @click="choose('')">{{ zh ? '默认' : 'Default' }}</button>
        <button v-for="option in choices" :key="option.value" type="button" role="radio" :data-testid="`model-effort-${option.value}`" :aria-checked="modelValue === option.value" @click="choose(option.value)">{{ option.label }}</button>
      </div>
    </div>
    <label class="effort-custom">
      <span class="effort-label">{{ zh ? '另填一档' : 'Custom level' }}</span>
      <input class="input" data-testid="model-configuration-effort-custom" :value="draft" :placeholder="zh ? '只用于这个模型' : 'This model only'" @input="onCustom" />
    </label>
  </div>
</template>

<style scoped>
.effort {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-top: 8px;
}
.effort-label {
  color: rgb(var(--c-txt3));
  font-size: 12px;
  line-height: 18px;
}
.effort-options {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 6px;
}
.effort-options button {
  border: 1px solid rgb(var(--c-line));
  border-radius: 999px;
  background: transparent;
  color: rgb(var(--c-txt2));
  padding: 2px 10px;
  font-size: 12px;
  line-height: 20px;
}
.effort-options button[aria-checked="true"] {
  border-color: rgb(var(--c-txt));
  color: rgb(var(--c-txt));
}
.effort-custom {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
</style>
