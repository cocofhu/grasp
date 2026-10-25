<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { identityIcon } from '@/lib/agent/identityIcons'

const props = withDefaults(defineProps<{ iconId: string; size?: number }>(), { size: 18 })
const el = ref<SVGSVGElement | null>(null)

function paint() {
  const spec = identityIcon(props.iconId)
  if (el.value) el.value.innerHTML = spec?.body ?? ''
}

onMounted(paint)
watch(() => props.iconId, paint)
</script>

<template>
  <svg
    ref="el"
    class="identity-glyph"
    viewBox="0 0 24 24"
    :width="size"
    :height="size"
    fill="none"
    stroke="currentColor"
    stroke-width="1.6"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  />
</template>

<style scoped>
.identity-glyph {
  display: block;
  flex: none;
}
</style>
