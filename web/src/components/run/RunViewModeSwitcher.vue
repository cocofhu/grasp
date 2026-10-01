<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { RunViewMode } from '@/lib/run/useRunDetail'

const props = defineProps<{
  viewMode: RunViewMode
  isMobile: boolean
}>()

const emit = defineEmits<{
  'update:viewMode': [mode: RunViewMode]
}>()

const { t } = useI18n()

const viewModeTrack = ref<HTMLElement | null>(null)
const viewModeIndicator = ref<Record<string, string>>({
  opacity: '0',
  transform: 'translateX(0)',
  width: '0px',
})

function updateViewModeIndicator() {
  const root = viewModeTrack.value
  if (!root) return
  const active = root.querySelector<HTMLElement>('[data-view-mode-active="true"]')
  if (!active) {
    viewModeIndicator.value = { opacity: '0', transform: 'translateX(0)', width: '0px' }
    return
  }
  viewModeIndicator.value = {
    opacity: '1',
    transform: `translateX(${active.offsetLeft}px)`,
    width: `${active.offsetWidth}px`,
  }
}

watch(
  () => props.viewMode,
  () => {
    void nextTick(updateViewModeIndicator)
  },
)
onMounted(() => {
  void nextTick(updateViewModeIndicator)
  window.addEventListener('resize', updateViewModeIndicator)
})
onBeforeUnmount(() => {
  window.removeEventListener('resize', updateViewModeIndicator)
})
</script>

<template>
  <div
    ref="viewModeTrack"
    class="relative inline-flex rounded-lg border border-line bg-surface/90 p-0.5 text-[12px] backdrop-blur"
  >
    <span
      class="seg-indicator pointer-events-none absolute inset-y-0.5 left-0 rounded-md bg-accent-dim"
      data-testid="run-view-mode-indicator"
      :style="viewModeIndicator"
      aria-hidden="true"
    />
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
  </div>
</template>

<style scoped>
.seg-indicator {
  transition:
    transform var(--dur-ui) var(--ease-out-expo),
    width var(--dur-ui) var(--ease-out-expo),
    opacity var(--dur-ui) ease;
}
</style>
