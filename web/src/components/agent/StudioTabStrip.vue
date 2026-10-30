<script setup lang="ts" generic="K extends string">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'

const props = defineProps<{
  tabs: { k: K; l: string; n?: number }[]
  modelValue: K
  /** Narrow screens: 44px touch targets. */
  touch?: boolean
  /** Shares a padded toolbar row with action buttons. */
  dense?: boolean
  /** Per-tab data-testid as `${testidPrefix}${k}`. */
  testidPrefix?: string
}>()
const emit = defineEmits<{ (e: 'update:modelValue', k: K): void }>()

const track = ref<HTMLElement | null>(null)
const indicator = ref<{ transform: string; width: string } | null>(null)
let observer: ResizeObserver | undefined
/** The underline spans the label, not the button padding. */
const INSET = 10

function syncIndicator() {
  const active = track.value?.querySelector<HTMLElement>('[aria-selected="true"]')
  if (!active || !active.offsetWidth) {
    indicator.value = null
    return
  }
  indicator.value = {
    transform: `translateX(${active.offsetLeft + INSET}px)`,
    width: `${Math.max(0, active.offsetWidth - INSET * 2)}px`,
  }
}

watch(() => [props.modelValue, props.tabs], () => void nextTick(syncIndicator), { deep: true })
onMounted(() => {
  if (typeof ResizeObserver !== 'undefined' && track.value) {
    observer = new ResizeObserver(syncIndicator)
    observer.observe(track.value)
  }
  void nextTick(syncIndicator)
})
onBeforeUnmount(() => observer?.disconnect())
</script>

<template>
  <div ref="track" role="tablist" class="relative flex min-w-max items-stretch gap-0.5">
    <button
      v-for="item in tabs"
      :key="item.k"
      type="button"
      role="tab"
      :aria-selected="modelValue === item.k"
      :data-studio-tab="item.k"
      :data-testid="testidPrefix ? `${testidPrefix}${item.k}` : undefined"
      class="group relative inline-flex shrink-0 items-center gap-1.5 whitespace-nowrap px-2.5 text-[13px] transition-colors"
      :class="[
        touch ? 'min-h-11' : dense ? 'h-8' : 'h-10',
        modelValue === item.k ? 'font-medium text-txt' : 'text-txt3 hover:text-txt2',
      ]"
      @click="emit('update:modelValue', item.k)"
    >
      <span
        v-if="modelValue !== item.k"
        class="pointer-events-none absolute inset-x-2.5 bottom-0 h-0.5 rounded-full bg-transparent transition-colors group-hover:bg-line-strong"
        data-testid="studio-tab-hover-line"
        aria-hidden="true"
      />
      <span>{{ item.l }}</span>
      <span
        v-if="item.n !== undefined"
        class="min-w-[18px] rounded-full px-1.5 text-center text-[11px] font-medium leading-[18px] tabular-nums transition-colors"
        :class="modelValue === item.k ? 'bg-accent-dim text-accent-2' : 'bg-overlay text-txt3 group-hover:text-txt2'"
      >{{ item.n }}</span>
    </button>
    <span
      v-show="indicator"
      class="studio-tab-indicator pointer-events-none absolute bottom-0 left-0 h-0.5 rounded-full bg-accent"
      data-testid="studio-tab-indicator"
      :style="indicator ?? undefined"
      aria-hidden="true"
    />
  </div>
</template>

<style scoped>
.studio-tab-indicator {
  transition:
    transform var(--dur-ui) var(--ease-out-expo),
    width var(--dur-ui) var(--ease-out-expo);
}
@media (prefers-reduced-motion: reduce) {
  .studio-tab-indicator {
    transition: none;
  }
}
</style>
