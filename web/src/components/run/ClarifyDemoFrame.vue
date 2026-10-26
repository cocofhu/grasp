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
      expanded ? 'clarify-demo-expanded' : 'clarify-demo-inline',
    ]"
    data-testid="clarify-demo-frame"
  >
    <div class="flex shrink-0 items-center gap-1.5 border-b border-line px-2 py-1 text-[10px] text-txt3">
      <span class="min-w-0 flex-1 truncate font-medium text-txt2" data-testid="clarify-demo-label">{{ label }}</span>
      <span
        v-if="selected"
        class="shrink-0 whitespace-nowrap font-medium text-accent"
        data-testid="clarify-demo-selected"
      ><Icon name="check" :size="11" :interactive="false" class="inline align-middle" /> {{ t('pages.clarify.demoSelected') }}</span>
      <button
        v-if="enlargeable"
        type="button"
        class="inline-flex shrink-0 items-center rounded border border-line bg-surface px-1.5 py-0.5 text-[10px] text-txt2 transition-colors hover:text-txt"
        data-testid="clarify-demo-enlarge"
        :aria-label="t('pages.clarify.demoEnlarge')"
        :title="t('pages.clarify.demoEnlarge')"
        @click.stop="emit('enlarge')"
      >
        <Icon name="expand" :size="11" aria-hidden="true" />
      </button>
    </div>
    <HtmlPreview :html="html" mode="demo" :enlargeable="false" :modal-title="label" />
  </div>
</template>

<style scoped>
/* Inline body only. HtmlPreview's demo iframe stays 140px for artifact thumbnails. */
.clarify-demo-inline :deep(iframe) {
  height: 168px;
}
.clarify-demo-expanded {
  display: flex;
  height: 100%;
  min-height: 0;
  flex-direction: column;
}
.clarify-demo-expanded :deep(iframe) {
  flex: 1 1 auto;
  width: 100%;
  height: auto;
  min-height: 0;
}
</style>
