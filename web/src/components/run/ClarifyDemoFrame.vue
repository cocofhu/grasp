<script setup lang="ts">
import HtmlPreview from '../ui/HtmlPreview.vue'
import { useI18n } from 'vue-i18n'

defineProps<{
  label: string
  html: string
  highlighted?: boolean
  selected?: boolean
}>()

const { t } = useI18n()
</script>

<template>
  <div
    class="overflow-hidden rounded-lg border bg-elevated transition-colors"
    :class="highlighted ? 'border-accent shadow-[0_0_0_1px_rgba(123,97,255,0.3)]' : 'border-line'"
    data-testid="clarify-demo-frame"
  >
    <div class="flex items-center gap-1.5 border-b border-line px-2 py-1 text-[10px] text-txt3">
      <span class="min-w-0 flex-1 truncate font-medium text-txt2">
        {{ label }}<span v-if="selected" class="text-accent"> ✓ {{ t('pages.clarify.demoSelected') }}</span>
      </span>
    </div>
    <HtmlPreview :html="html" mode="demo" :enlargeable="false" :modal-title="label" />
  </div>
</template>
