<script setup lang="ts">
import { computed } from 'vue'
import type { FileBadgeTone } from '@/lib/shared/artifactBadge'

const props = defineProps<{ label: string; tone: FileBadgeTone }>()

const TONE_COLOR: Record<FileBadgeTone, string> = {
  pdf: '#e5483d', doc: '#3b7ddd', data: '#1f9d55', audio: '#d6408c', media: '#7B61FF', archive: '#c98410',
}
const color = computed(() => TONE_COLOR[props.tone] ?? TONE_COLOR.archive)
const width = computed(() => (props.label.length > 3 ? 24 : 20))
</script>

<template>
  <svg class="file-type-icon" aria-hidden="true" width="28" height="34" viewBox="0 0 28 34" :data-tone="tone" data-testid="file-type-icon">
    <path d="M4 1h14l9 9v20a3 3 0 0 1-3 3H4a3 3 0 0 1-3-3V4a3 3 0 0 1 3-3z" class="file-type-icon__paper" />
    <path d="M18 1v6a3 3 0 0 0 3 3h6" class="file-type-icon__fold" />
    <rect x="0" y="18" :width="width" height="10" rx="2" :fill="color" data-testid="file-type-icon-badge" />
    <text :x="width / 2" y="25.6" text-anchor="middle" font-size="6.4" font-weight="700" fill="#fff" letter-spacing=".3">{{ label }}</text>
  </svg>
</template>

<style scoped>
.file-type-icon {
  --paper: #e6e6eb;
  --fold: #c3c3cc;
  --paper-stroke: #8e8e98;
  --paper-shadow: rgba(0, 0, 0, 0.5);
  flex-shrink: 0;
  filter: drop-shadow(0 1px 1.5px var(--paper-shadow));
  font-family: -apple-system, 'Segoe UI', sans-serif;
}
:global(html.light .file-type-icon) {
  --paper: #fff;
  --fold: #ececf0;
  --paper-stroke: #d0d0d8;
  --paper-shadow: rgba(0, 0, 0, 0.08);
}
.file-type-icon__paper { fill: var(--paper); stroke: var(--paper-stroke); }
.file-type-icon__fold { fill: var(--fold); stroke: var(--paper-stroke); }
</style>
