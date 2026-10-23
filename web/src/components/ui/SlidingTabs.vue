<script setup lang="ts">
import { onBeforeUnmount, onMounted, onUpdated, ref, watch } from 'vue'

const props = withDefaults(
  defineProps<{
    value?: unknown
    as?: string
    activeSelector?: string
    indicatorClass?: string
    indicatorTestid?: string
  }>(),
  {
    as: 'div',
    activeSelector:
      '[aria-selected="true"], [aria-pressed="true"], [aria-checked="true"], [data-active="true"]',
    indicatorClass: 'rounded-md bg-surface shadow-sm',
    indicatorTestid: undefined,
    value: undefined,
  },
)

const track = ref<HTMLElement | null>(null)
const indicatorStyle = ref<{ transform: string; width: string; height: string } | null>(null)
let observer: ResizeObserver | undefined
const observed = new Set<Element>()

function syncIndicator() {
  const root = track.value
  if (!root) return

  // Observe the options too: translations, counts and conditional options can
  // move the selected button without changing the overall track size.
  const options = [...root.querySelectorAll('button')]
  const targets = new Set<Element>([root, ...options])
  for (const element of observed) {
    if (!targets.has(element)) {
      observer?.unobserve(element)
      observed.delete(element)
    }
  }
  for (const element of targets) {
    if (observer && !observed.has(element)) {
      observer.observe(element)
      observed.add(element)
    }
  }

  const selected = root.querySelector<HTMLElement>(props.activeSelector)
  if (!selected) {
    indicatorStyle.value = null
    return
  }
  const bounds = root.getBoundingClientRect()
  const button = selected.getBoundingClientRect()
  if (!button.width || !button.height) return
  // Dialogs can be scaled during their entrance. Convert viewport geometry back
  // to layout pixels so the highlight still fits once that animation finishes.
  const scaleX = root.offsetWidth ? bounds.width / root.offsetWidth : 1
  const scaleY = root.offsetHeight ? bounds.height / root.offsetHeight : 1

  const next = {
    transform: `translate3d(${(button.left - bounds.left) / scaleX - root.clientLeft + root.scrollLeft}px, ${(button.top - bounds.top) / scaleY - root.clientTop + root.scrollTop}px, 0)`,
    width: `${button.width / scaleX}px`,
    height: `${button.height / scaleY}px`,
  }
  const previous = indicatorStyle.value
  // This also makes onUpdated safe when our own measurement updates the span.
  if (
    !previous || previous.transform !== next.transform ||
    previous.width !== next.width || previous.height !== next.height
  ) indicatorStyle.value = next
}

watch(() => [props.value, props.activeSelector], syncIndicator, { flush: 'post' })
onMounted(() => {
  if (typeof ResizeObserver !== 'undefined') observer = new ResizeObserver(syncIndicator)
  syncIndicator()
})
onUpdated(syncIndicator)
onBeforeUnmount(() => {
  observer?.disconnect()
  observed.clear()
})
</script>

<template>
  <component :is="as" ref="track" class="sliding-tabs">
    <span
      v-show="indicatorStyle"
      class="sliding-tabs__indicator"
      :class="indicatorClass"
      :style="indicatorStyle"
      :data-testid="indicatorTestid"
      aria-hidden="true"
    >
      <slot name="indicator" />
    </span>
    <slot />
  </component>
</template>

<style scoped>
.sliding-tabs {
  --sliding-tabs-ease: cubic-bezier(.22, 1, .36, 1);
  position: relative;
  isolation: isolate;
}
.sliding-tabs__indicator {
  position: absolute;
  z-index: 0;
  top: 0;
  left: 0;
  pointer-events: none;
  transition:
    transform 420ms var(--sliding-tabs-ease),
    width 420ms var(--sliding-tabs-ease),
    height 420ms var(--sliding-tabs-ease),
    background-color 200ms ease;
}
.sliding-tabs :deep(> button) {
  position: relative;
  z-index: 1;
}
@supports (transition-timing-function: linear(0, 1)) {
  .sliding-tabs {
    /* Same damped spring as the home switch: mass 1, stiffness 420, damping 34. */
    --sliding-tabs-ease: linear(0, .0669, .213, .3816, .5413, .6774, .7851, .8655, .9224, .9606, .9846, .9987, 1.006, 1.0089, 1.0094, 1.0084, 1.007, 1.0054, 1.0039, 1.0027, 1.0018, 1);
  }
}
@media (prefers-reduced-motion: reduce) {
  .sliding-tabs__indicator {
    transition: none;
  }
}
</style>
