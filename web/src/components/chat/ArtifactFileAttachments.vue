<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ArtifactAttachment } from '@/lib/shared/types'
import { api } from '@/lib/api/api'
import AppModal from '@/components/ui/AppModal.vue'
import Icon from '@/components/ui/Icon.vue'
import ArtifactFilePreview from '@/components/run/ArtifactFilePreview.vue'

defineProps<{ files?: ArtifactAttachment[] }>()
const { t } = useI18n()
const selected = ref<ArtifactAttachment | null>(null)
const open = computed(() => selected.value !== null)
function sizeLabel(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1048576) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1048576).toFixed(1)} MB`
}
</script>

<template>
  <div v-if="files?.length" class="flex min-w-0 flex-col gap-2" data-testid="artifact-file-attachments">
    <div v-for="file in files" :key="`${file.artifactId}:${file.revision}`" class="flex min-w-0 items-center gap-3 rounded-lg border border-line bg-elevated/40 p-3">
      <Icon name="artifact" :size="20" class="shrink-0 text-txt3" />
      <div class="min-w-0 flex-1">
        <p class="truncate text-sm font-medium text-txt" :title="file.name">{{ file.name }}</p>
        <p class="mt-1 text-xs text-txt3">{{ sizeLabel(file.sizeBytes) }} · v{{ file.revision }}</p>
        <p v-if="file.caption" class="mt-1 break-words text-xs leading-5 text-txt2">{{ file.caption }}</p>
      </div>
      <button v-if="file.kind !== 'file'" type="button" class="min-h-10 shrink-0 px-2 text-xs text-accent" @click="selected = file">{{ t('pages.artifactFiles.preview') }}</button>
      <a :href="api.artifactDownloadUrl(file.artifactId, file.revision)" class="inline-flex min-h-10 shrink-0 items-center px-2 text-xs text-accent">{{ t('pages.artifactFiles.download') }}</a>
    </div>
    <AppModal :open="open" :title="selected?.name" :width="960" close-on-esc trap-focus @close="selected = null">
      <ArtifactFilePreview v-if="selected" :file="selected" />
    </AppModal>
  </div>
</template>
