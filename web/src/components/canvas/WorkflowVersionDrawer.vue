<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../ui/Icon.vue'
import { api } from '@/lib/api/api'
import { fmtTime } from '@/lib/shared/format'
import type { WorkflowVersion } from '@/lib/shared/types'

const props = defineProps<{
  workflowId: string
  /** Latest saved version (the editor's base). */
  latestVersion: number
  publishedVersion: number
  /** Version shown read-only on the canvas, if any. */
  previewVersion?: number | null
  /** Version being restored, if any. */
  restoring?: number | null
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'preview', version: number): void
  (e: 'restore', version: number): void
}>()

const { t } = useI18n()
const versions = ref<WorkflowVersion[]>([])
const loading = ref(false)
const failed = ref(false)
let abort: AbortController | null = null

async function load() {
  if (!props.workflowId) return
  abort?.abort()
  const ctrl = (abort = new AbortController())
  loading.value = true
  failed.value = false
  try {
    const list = (await api.listWorkflowVersions(props.workflowId, { signal: ctrl.signal })) || []
    if (ctrl.signal.aborted) return
    versions.value = [...list].sort((a, b) => b.version - a.version)
  } catch {
    if (ctrl.signal.aborted) return
    failed.value = true
    versions.value = []
  } finally {
    if (abort === ctrl) loading.value = false
  }
}

watch(() => [props.workflowId, props.latestVersion, props.publishedVersion] as const, load, { immediate: true })
onBeforeUnmount(() => abort?.abort())

function sourceLabel(v: WorkflowVersion): string {
  if (v.source === 'restore' && v.restoredFrom) return t('canvas.versions.source.restoredFrom', { n: v.restoredFrom })
  return t(`canvas.versions.source.${v.source}`)
}

defineExpose({ reload: load })
</script>

<template>
  <aside
    class="cchrome absolute bottom-3 right-3 top-3 z-30 flex w-[320px] max-w-[calc(100%-24px)] flex-col overflow-hidden !p-0"
    role="dialog"
    :aria-label="t('canvas.versions.title')"
    data-testid="version-drawer"
    @keydown.esc.stop="emit('close')"
  >
    <header class="flex h-12 shrink-0 items-center gap-2 border-b border-line px-3">
      <Icon name="history" :size="15" class="text-txt3" />
      <h2 class="flex-1 text-[13px] font-semibold text-txt">{{ t('canvas.versions.title') }}</h2>
      <button type="button" class="cchrome-btn" :aria-label="t('common.buttons.close')" data-testid="version-drawer-close" @click="emit('close')">
        <Icon name="close" :size="14" />
      </button>
    </header>
    <p class="shrink-0 border-b border-line px-3 py-2 text-[11.5px] leading-5 text-txt3">{{ t('canvas.versions.intro') }}</p>
    <div class="scroll-area min-h-0 flex-1 overflow-y-auto p-2">
      <div v-if="loading && !versions.length" class="py-8 text-center text-[12px] text-txt3">{{ t('common.buttons.loading') }}</div>
      <div v-else-if="failed" class="flex flex-col items-center gap-2 py-8 text-center text-[12px] text-err">
        {{ t('canvas.versions.loadFailed') }}
        <button type="button" class="underline" @click="load">{{ t('common.loading.retry') }}</button>
      </div>
      <div v-else-if="!versions.length" class="py-8 text-center text-[12px] text-txt3">{{ t('canvas.versions.empty') }}</div>
      <ul v-else class="space-y-1">
        <li v-for="v in versions" :key="v.version">
          <div
            role="button"
            tabindex="0"
            class="group flex w-full cursor-pointer items-start gap-2.5 rounded-lg border px-2.5 py-2 text-left transition-colors"
            :class="previewVersion === v.version ? 'border-accent/60 bg-accent-dim' : 'border-transparent hover:border-line hover:bg-elevated'"
            :aria-pressed="previewVersion === v.version"
            :data-testid="`version-item-${v.version}`"
            @click="emit('preview', v.version)"
            @keydown.enter.prevent="emit('preview', v.version)"
          >
            <span class="mt-0.5 flex h-7 min-w-7 shrink-0 items-center justify-center rounded-md bg-elevated px-1 font-mono text-[11.5px] font-semibold text-accent-2">v{{ v.version }}</span>
            <span class="min-w-0 flex-1">
              <span class="flex flex-wrap items-center gap-1">
                <span class="chip text-txt3" :data-testid="`version-source-${v.version}`">{{ sourceLabel(v) }}</span>
                <span v-if="v.version === latestVersion" class="chip border-accent/40 text-accent-2" data-testid="version-latest">{{ t('canvas.versions.current') }}</span>
                <span v-if="v.version === publishedVersion" class="chip border-ok/40 text-ok" data-testid="version-published">{{ t('canvas.versions.published') }}</span>
              </span>
              <span class="mt-1 block text-[11px] text-txt3">
                {{ fmtTime(v.createdAt) }} · {{ t('canvas.versions.nodes', { n: v.nodeCount }) }}
              </span>
            </span>
            <button
              v-if="v.version !== latestVersion"
              type="button"
              class="shrink-0 self-center rounded-md px-2 py-1 text-[11.5px] text-txt2 opacity-0 transition-opacity hover:bg-base hover:text-txt focus:opacity-100 group-hover:opacity-100"
              :class="{ '!opacity-100': previewVersion === v.version || restoring === v.version }"
              :disabled="!!restoring"
              :data-testid="`version-restore-${v.version}`"
              @click.stop="emit('restore', v.version)"
            >
              {{ restoring === v.version ? t('canvas.versions.restoring') : t('canvas.versions.restore') }}
            </button>
          </div>
        </li>
      </ul>
    </div>
  </aside>
</template>
