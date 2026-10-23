<script setup lang="ts">
import SlidingTabs from '@/components/ui/SlidingTabs.vue'
import { useI18n } from 'vue-i18n'
import type { RunViewMode } from '@/lib/run/useRunDetail'

defineProps<{
  viewMode: RunViewMode
  isMobile: boolean
}>()

const emit = defineEmits<{
  'update:viewMode': [mode: RunViewMode]
}>()

const { t } = useI18n()

</script>

<template>
  <SlidingTabs
    :value="viewMode"
    active-selector='[data-view-mode-active="true"]'
    indicator-class="rounded-md bg-accent-dim"
    indicator-testid="run-view-mode-indicator"
    class="relative inline-flex rounded-lg border border-line bg-surface/90 p-0.5 text-[12px] backdrop-blur"
  >
    <button
      v-if="!isMobile"
      data-testid="view-mode-canvas"
      class="relative z-[1] rounded-md px-2.5 py-1 font-medium transition-colors"
      :class="viewMode === 'canvas' ? 'text-accent' : 'text-txt3 hover:text-txt2'"
      :data-view-mode-active="viewMode === 'canvas' ? 'true' : undefined"
      @click="emit('update:viewMode', 'canvas')"
    >
      {{ t('pages.runDetail.canvas') }}
    </button>
    <button
      data-testid="view-mode-timeline"
      class="relative z-[1] rounded-md px-2.5 py-1 font-medium transition-colors"
      :class="viewMode === 'timeline' ? 'text-accent' : 'text-txt3 hover:text-txt2'"
      :data-view-mode-active="viewMode === 'timeline' ? 'true' : undefined"
      @click="emit('update:viewMode', 'timeline')"
    >
      {{ t('pages.runDetail.timeline') }}
    </button>
    <button
      data-testid="view-mode-stats"
      class="relative z-[1] rounded-md px-2.5 py-1 font-medium transition-colors"
      :class="viewMode === 'stats' ? 'text-accent' : 'text-txt3 hover:text-txt2'"
      :data-view-mode-active="viewMode === 'stats' ? 'true' : undefined"
      @click="emit('update:viewMode', 'stats')"
    >
      {{ t('pages.runDetail.stats') }}
    </button>
    <button
      data-testid="view-mode-llm"
      class="relative z-[1] rounded-md px-2.5 py-1 font-medium transition-colors"
      :class="viewMode === 'llm' ? 'text-accent' : 'text-txt3 hover:text-txt2'"
      :data-view-mode-active="viewMode === 'llm' ? 'true' : undefined"
      @click="emit('update:viewMode', 'llm')"
    >
      {{ t('pages.runDetail.llm') }}
    </button>
  </SlidingTabs>
</template>
