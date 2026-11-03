<script setup lang="ts">
import { nextTick, onMounted, ref, watch } from 'vue'

defineOptions({ inheritAttrs: false })
const text = defineModel<string>({ required: true })
const textarea = ref<HTMLTextAreaElement | null>(null)

function resize() {
  const el = textarea.value
  if (!el) return
  el.style.height = 'auto'
  el.style.height = `${Math.min(Math.max(el.scrollHeight, 48), 220)}px`
}

watch(text, () => void nextTick(resize))
onMounted(resize)
defineExpose({ textarea, resize })
</script>

<template>
  <textarea
    ref="textarea"
    v-model="text"
    rows="2"
    class="chat-input scroll-area block max-h-[220px] min-h-[48px] w-full min-w-0 resize-none py-1 disabled:opacity-50"
    v-bind="$attrs"
  />
</template>
