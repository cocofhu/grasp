<script setup lang="ts">
import SlidingTabs from '@/components/ui/SlidingTabs.vue'
import { useI18n } from 'vue-i18n'

export type MarkdownViewMode = 'edit' | 'split' | 'preview'

defineProps<{ modelValue: MarkdownViewMode }>()
const emit = defineEmits<{ (e: 'update:modelValue', v: MarkdownViewMode): void }>()

const { t } = useI18n()

const modes: { value: MarkdownViewMode; label: string }[] = [
  { value: 'edit', label: 'pages.markdownEditor.edit' },
  { value: 'split', label: 'pages.markdownEditor.split' },
  { value: 'preview', label: 'pages.markdownEditor.preview' },
]
</script>

<template>
  <SlidingTabs :value="modelValue" indicator-class="rounded bg-accent-dim" class="flex items-center gap-0.5" role="radiogroup" data-testid="md-view-toggle">
    <button
      v-for="m in modes"
      :key="m.value"
      type="button"
      role="radio"
      :aria-checked="modelValue === m.value"
      :data-testid="`md-view-${m.value}`"
      class="rounded px-2 py-0.5 text-[12px] leading-5 outline-none transition-colors focus:outline-none focus-visible:shadow-[inset_0_0_0_2px_rgba(99,102,241,0.35)]"
      :class="modelValue === m.value ? 'text-txt' : 'text-txt3 hover:text-txt2'"
      @click="emit('update:modelValue', m.value)"
    >{{ t(m.label) }}</button>
  </SlidingTabs>
</template>
