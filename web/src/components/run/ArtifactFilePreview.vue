<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api/api'
import type { ArtifactAttachment } from '@/lib/shared/types'
import { renderMarkdown } from '@/lib/shared/markdown'
import AppButton from '@/components/ui/AppButton.vue'
import HtmlPreview from '@/components/ui/HtmlPreview.vue'

const props = defineProps<{ file: ArtifactAttachment }>()
const { t } = useI18n()
const state = ref<'loading' | 'ready' | 'error'>('loading')
const text = ref('')
const truncated = ref(false)
const source = computed(() => api.artifactRawUrl(props.file.artifactId, props.file.revision))
const download = computed(() => api.artifactDownloadUrl(props.file.artifactId, props.file.revision))
const textKind = computed(() => ['text', 'markdown', 'json', 'yaml', 'html'].includes(props.file.kind))
let generation = 0
let abort: AbortController | null = null

async function load() {
  const current = ++generation
  abort?.abort()
  abort = new AbortController()
  state.value = 'loading'
  text.value = ''
  truncated.value = false
  try {
    const response = await fetch(source.value, {
      method: textKind.value ? 'GET' : 'HEAD', credentials: 'include', signal: abort.signal,
      ...(textKind.value ? { headers: { Range: 'bytes=0-2097151' } } : {}),
    })
    if (!response.ok) throw new Error('file unavailable')
    const body = textKind.value ? await response.text() : ''
    if (current !== generation) return
    text.value = body
    truncated.value = props.file.sizeBytes > 2097152 && textKind.value
    state.value = 'ready'
  } catch {
    if (current === generation && !abort.signal.aborted) state.value = 'error'
  }
}

watch(() => `${props.file.artifactId}:${props.file.revision}`, () => void load(), { immediate: true })
onBeforeUnmount(() => { generation++; abort?.abort() })
</script>

<template>
  <div class="flex h-full min-h-64 min-w-0 flex-col gap-3" data-testid="artifact-file-preview">
    <div v-if="state === 'loading'" class="flex flex-1 items-center justify-center text-sm text-txt3" role="status">{{ t('pages.artifactFiles.loading') }}</div>
    <div v-else-if="state === 'error'" class="flex flex-1 flex-col items-center justify-center gap-3 py-8" role="status" data-testid="artifact-file-error">
      <p class="text-sm text-txt2">{{ t('pages.artifactFiles.unavailable') }}</p>
      <AppButton size="sm" variant="outline" @click="load">{{ t('pages.artifactFiles.retry') }}</AppButton>
    </div>
    <template v-else>
      <img v-if="file.kind === 'image'" :src="source" :alt="file.caption || file.name" class="max-h-[70vh] max-w-full object-contain" @error="state = 'error'">
      <video v-else-if="file.kind === 'video'" :key="source" :src="source" controls playsinline preload="metadata" class="max-h-[70vh] w-full" @error="state = 'error'"><a :href="download">{{ t('pages.artifactFiles.download') }}</a></video>
      <audio v-else-if="file.kind === 'audio'" :key="source" :src="source" controls preload="metadata" class="my-auto w-full" @error="state = 'error'"><a :href="download">{{ t('pages.artifactFiles.download') }}</a></audio>
      <iframe v-else-if="file.kind === 'pdf'" :key="source" :src="source" :title="file.name" class="min-h-[65vh] w-full flex-1 border-0" @error="state = 'error'" />
      <HtmlPreview v-else-if="file.kind === 'html'" :html="text" />
      <div v-else-if="file.kind === 'markdown'" class="md-body min-w-0 overflow-auto" v-html="renderMarkdown(text)" />
      <pre v-else-if="textKind" class="min-w-0 flex-1 overflow-auto whitespace-pre-wrap break-words p-3 text-xs text-txt">{{ text }}</pre>
      <p v-else class="py-8 text-center text-sm text-txt3">{{ t('pages.artifactFiles.downloadOnly') }}</p>
      <p v-if="truncated" class="text-xs text-txt3">{{ t('pages.artifactFiles.truncated') }}</p>
    </template>
    <a :href="download" class="mt-auto inline-flex min-h-10 items-center justify-center rounded-md border border-line px-3 text-sm text-accent" data-testid="artifact-file-download">{{ t('pages.artifactFiles.download') }}</a>
  </div>
</template>
