<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'

defineProps<{ open: boolean }>()

const emit = defineEmits<{
  (e: 'scratch'): void
  (e: 'baseline'): void
}>()

const { t } = useI18n()
</script>

<template>
  <div
    v-if="open"
    class="new-workflow-menu absolute right-0 top-[calc(100%+8px)] z-30 rounded-lg border border-line bg-surface p-1.5"
    role="menu"
    data-testid="new-workflow-menu"
  >
    <button
      type="button"
      role="menuitem"
      class="flex w-full items-start gap-2.5 rounded-md p-2 text-left transition hover:bg-elevated"
      data-testid="new-workflow-scratch"
      @click="emit('scratch')"
    >
      <span class="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-accent-dim text-accent-2">
        <Icon name="plus" :size="16" />
      </span>
      <span class="min-w-0">
        <span class="block text-[13px] font-semibold text-txt">{{ t('pages.workspace.newWorkflow.scratch') }}</span>
        <span class="mt-0.5 block text-xs leading-snug text-txt3">{{ t('pages.workspace.newWorkflow.scratchDesc') }}</span>
      </span>
    </button>
    <button
      type="button"
      role="menuitem"
      class="flex w-full items-start gap-2.5 rounded-md p-2 text-left transition hover:bg-elevated"
      data-testid="new-workflow-baseline"
      @click="emit('baseline')"
    >
      <span class="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-warn/15 text-warn">
        <Icon name="trigger" :size="16" />
      </span>
      <span class="min-w-0">
        <span class="block text-[13px] font-semibold text-txt">{{ t('pages.workspace.newWorkflow.baseline') }}</span>
        <span class="mt-0.5 block text-xs leading-snug text-txt3">{{ t('pages.workspace.newWorkflow.baselineDesc') }}</span>
      </span>
    </button>
  </div>
</template>

<style scoped>
.new-workflow-menu {
  width: min(300px, calc(100vw - 32px));
  box-shadow: 0 16px 40px -18px rgba(0, 0, 0, 0.72);
}
:global(html.light .new-workflow-menu) {
  box-shadow: 0 16px 40px -20px rgba(16, 24, 40, 0.35);
}
</style>
