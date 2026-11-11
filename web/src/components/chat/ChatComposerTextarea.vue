<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'

defineOptions({ inheritAttrs: false })
const text = defineModel<string>({ required: true })
const textarea = ref<HTMLTextAreaElement | null>(null)
let observer: ResizeObserver | undefined
let width = 0

function resize() {
  const el = textarea.value
  if (!el) return
  // Measuring while hidden or mid-layout wraps the placeholder per character and locks in a huge height.
  if (!text.value || !el.clientWidth) {
    el.style.height = ''
    return
  }
  el.style.height = 'auto'
  el.style.height = `${Math.min(Math.max(el.scrollHeight, 48), 220)}px`
}

watch(text, () => void nextTick(resize))
onMounted(() => {
  resize()
  if (typeof ResizeObserver === 'undefined' || !textarea.value) return
  observer = new ResizeObserver(([entry]) => {
    const next = Math.round(entry?.contentRect.width ?? 0)
    if (next === width) return
    width = next
    resize()
  })
  observer.observe(textarea.value)
})
onBeforeUnmount(() => observer?.disconnect())
defineExpose({ textarea, resize })
</script>

<template>
  <textarea
    ref="textarea"
    v-model="text"
    rows="2"
    class="chat-input scroll-area block max-h-[min(220px,30dvh)] min-h-[48px] w-full min-w-0 resize-none py-1 disabled:opacity-50"
    v-bind="$attrs"
  />
</template>
