<script setup lang="ts">
import HtmlPreview from '../ui/HtmlPreview.vue'
import Icon from '../ui/Icon.vue'
import { useI18n } from 'vue-i18n'

withDefaults(
  defineProps<{
    label: string
    html: string
    highlighted?: boolean
    selected?: boolean
    /** Card-level enlarge. Off inside the page window so it does not nest another layer. */
    enlargeable?: boolean
    /** Taller frame for the page-level window. Inline cards keep the demo height. */
    expanded?: boolean
  }>(),
  { enlargeable: true, expanded: false },
)

const emit = defineEmits<{ enlarge: [] }>()

const { t } = useI18n()
</script>

<template>
  <div
    class="overflow-hidden rounded-lg border bg-elevated transition-colors"
    :class="[
      highlighted ? 'border-accent shadow-[0_0_0_1px_rgba(123,97,255,0.3)]' : 'border-line',
      expanded ? 'clarify-demo-expanded' : '',
    ]"
    data-testid="clarify-demo-frame"
  >
    <div class="flex items-center gap-1.5 border-b border-line px-2 py-1 text-[10px] text-txt3">
      <span class="min-w-0 flex-1 truncate font-medium text-txt2" data-testid="clarify-demo-label">{{ label }}</span>
      <span
        v-if="selected"
        class="shrink-0 whitespace-nowrap font-medium text-accent"
        data-testid="clarify-demo-selected"
      >✓ {{ t('pages.clarify.demoSelected') }}</span>
      <button
        v-if="enlargeable"
        type="button"
        class="inline-flex shrink-0 items-center gap-0.5 whitespace-nowrap rounded border border-line bg-surface px-1.5 py-0.5 text-[10px] text-txt2 transition-colors hover:text-txt"
        data-testid="clarify-demo-enlarge"
        @click.stop="emit('enlarge')"
      >
        <Icon name="expand" :size="11" />{{ t('pages.clarify.demoEnlarge') }}
      </button>
    </div>
    <HtmlPreview :html="html" mode="demo" :enlargeable="false" :modal-title="label" />
  </div>
</template>

<style scoped>
.clarify-demo-expanded :deep(iframe) {
  height: 70vh;
}
</style>
