<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ArtifactAttachment } from '@/lib/shared/types'
import { api } from '@/lib/api/api'
import { fileBadge, sizeLabel } from '@/lib/shared/artifactBadge'
import AppModal from '@/components/ui/AppModal.vue'
import Icon from '@/components/ui/Icon.vue'
import ArtifactFilePreview from '@/components/run/ArtifactFilePreview.vue'
import FileTypeIcon from './FileTypeIcon.vue'

defineProps<{ files?: ArtifactAttachment[] }>()
const { t } = useI18n()
const selected = ref<ArtifactAttachment | null>(null)
const open = computed(() => selected.value !== null)
const failed = reactive(new Set<string>())
const natural = reactive(new Map<string, { width: number; height: number }>())

const keyOf = (file: ArtifactAttachment) => `${file.artifactId}:${file.revision}`
const thumbKind = (file: ArtifactAttachment) =>
  (file.kind === 'image' || file.kind === 'video') && !failed.has(keyOf(file)) ? file.kind : null
const tooltip = (file: ArtifactAttachment) => (file.caption ? `${file.name}\n${file.caption}` : file.name)

function onImageLoad(file: ArtifactAttachment, event: Event) {
  const img = event.target as HTMLImageElement
  if (img.naturalWidth && img.naturalHeight) natural.set(keyOf(file), { width: img.naturalWidth, height: img.naturalHeight })
}
/** Small images may scale up to 2× their natural size, never further. */
function imageStyle(file: ArtifactAttachment) {
  const size = natural.get(keyOf(file))
  return size ? { maxWidth: `${size.width * 2}px`, maxHeight: `${size.height * 2}px` } : undefined
}
</script>

<template>
  <div v-if="files?.length" class="flex min-w-0 flex-wrap gap-1.5" data-testid="artifact-file-attachments">
    <div
      v-for="file in files"
      :key="keyOf(file)"
      class="artifact-chip"
      :title="tooltip(file)"
      :data-kind="file.kind"
      data-testid="artifact-file-chip"
    >
      <button
        type="button"
        class="artifact-chip__body"
        :disabled="file.kind === 'file'"
        :aria-label="file.kind === 'file' ? file.name : t('pages.artifactFiles.previewName', { name: file.name })"
        data-testid="artifact-file-chip-preview"
        @click="selected = file"
      >
        <span class="flex h-9 w-9 shrink-0 items-center justify-center" data-testid="artifact-file-chip-icon">
          <span v-if="thumbKind(file) === 'image'" class="artifact-chip__thumb artifact-chip__thumb--image" aria-hidden="true">
            <img
              :src="api.artifactRawUrl(file.artifactId, file.revision)"
              alt=""
              loading="lazy"
              decoding="async"
              class="h-full w-full object-contain"
              :style="imageStyle(file)"
              @load="onImageLoad(file, $event)"
              @error="failed.add(keyOf(file))"
            >
          </span>
          <span v-else-if="thumbKind(file) === 'video'" class="artifact-chip__thumb artifact-chip__thumb--video" aria-hidden="true">
            <video
              :src="api.artifactRawUrl(file.artifactId, file.revision)"
              preload="metadata"
              muted
              playsinline
              tabindex="-1"
              class="h-full w-full object-cover"
              @error="failed.add(keyOf(file))"
            />
            <span class="artifact-chip__play"><svg width="8" height="8" viewBox="0 0 24 24" fill="currentColor"><path d="M8 5v14l11-7z" /></svg></span>
          </span>
          <FileTypeIcon v-else v-bind="fileBadge(file)" />
        </span>
        <span class="min-w-0">
          <span class="block truncate text-xs font-semibold text-txt">{{ file.name }}</span>
          <span class="artifact-chip__meta block truncate text-[10px] leading-4">{{ sizeLabel(file.sizeBytes) }} · v{{ file.revision }}</span>
        </span>
      </button>
      <a
        :href="api.artifactDownloadUrl(file.artifactId, file.revision)"
        class="artifact-chip__download"
        :class="{ 'artifact-chip__download--always': file.kind === 'file' }"
        :aria-label="t('pages.artifactFiles.download')"
        :title="t('pages.artifactFiles.download')"
        data-testid="artifact-file-chip-download"
        @click.stop
      >
        <Icon name="download" :size="15" />
      </a>
    </div>
    <AppModal :open="open" :title="selected?.name" :width="960" close-on-esc trap-focus @close="selected = null">
      <ArtifactFilePreview v-if="selected" :file="selected" />
    </AppModal>
  </div>
</template>

<style scoped>
.artifact-chip {
  --chip-bg: rgb(var(--c-elevated));
  --chip-line: rgb(50 50 58);
  --chip-meta: rgb(142 142 152);
  --thumb-line: rgb(74 74 84);
  display: flex;
  align-items: center;
  gap: 4px;
  width: 100%;
  min-width: 0;
  height: 52px;
  padding: 0 4px 0 8px;
  border: 1px solid var(--chip-line);
  border-radius: 10px;
  background: var(--chip-bg);
  transition: border-color 0.15s, background-color 0.15s;
}
:global(html.light .artifact-chip) {
  --chip-bg: rgb(var(--c-surface));
  --chip-line: rgb(var(--c-line));
  --chip-meta: rgb(var(--c-txt3));
  --thumb-line: rgb(var(--c-line));
}
@media (min-width: 640px) {
  .artifact-chip { width: 228px; }
}
.artifact-chip:hover,
.artifact-chip:focus-within {
  border-color: #7b61ff;
  background: rgb(var(--c-accent-dim) / 0.6);
}
.artifact-chip__body {
  display: flex;
  flex: 1;
  align-items: center;
  gap: 10px;
  min-width: 0;
  height: 100%;
  border-radius: 8px;
  color: inherit;
  text-align: left;
}
.artifact-chip__body:disabled { cursor: default; }
.artifact-chip__body:focus-visible,
.artifact-chip__download:focus-visible {
  outline: 2px solid #7b61ff;
  outline-offset: 2px;
}
.artifact-chip__meta { color: var(--chip-meta); }
.artifact-chip__thumb {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 36px;
  height: 36px;
  overflow: hidden;
  border: 1px solid var(--thumb-line);
  border-radius: 7px;
}
/* Light in both themes: transparent grey artwork disappears on a dark checkerboard. */
.artifact-chip__thumb--image { background: repeating-conic-gradient(#ececef 0 25%, #fff 0 50%) 0 0 / 6px 6px; }
.artifact-chip__thumb--video { background: #0e0e11; }
.artifact-chip__play {
  position: absolute;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 16px;
  height: 16px;
  border: 1px solid rgba(255, 255, 255, 0.6);
  border-radius: 9999px;
  background: rgba(14, 14, 17, 0.55);
  color: #fff;
}
.artifact-chip__download {
  display: flex;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 30px;
  border-radius: 7px;
  color: rgb(var(--c-txt2));
  opacity: 0;
  transition: opacity 0.15s, background-color 0.15s, color 0.15s;
}
.artifact-chip:hover .artifact-chip__download,
.artifact-chip:focus-within .artifact-chip__download,
.artifact-chip__download--always { opacity: 1; }
@media (hover: none) {
  .artifact-chip__download { opacity: 1; }
}
.artifact-chip__download:hover {
  background: rgb(var(--c-accent-dim));
  color: #7b61ff;
}
</style>
