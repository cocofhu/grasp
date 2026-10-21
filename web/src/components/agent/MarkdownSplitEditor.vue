<script setup lang="ts">
import { ref, computed, watch, nextTick, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import CodeEditor from '@/components/ui/CodeEditor.vue'
import Icon from '@/components/ui/Icon.vue'
import { renderMarkdown } from '@/lib/shared/markdown'
import {
  parseFrontmatter,
  buildFrontmatter,
  frontmatterTypeForPath,
  hasFrontmatter,
  type FrontmatterFields,
  type FrontmatterType,
} from '@/lib/shared/frontmatter'
import type * as monaco from 'monaco-editor/esm/vs/editor/editor.api'

const props = withDefaults(
  defineProps<{
    modelValue: string
    filePath: string
    readonly?: boolean
    /** split = desktop FM form + side-by-side; stack = mobile unified source + edit/preview toggle */
    variant?: 'split' | 'stack'
    /** Host renders the preview toggle in its own toolbar (no in-pane header/rail). */
    externalToolbar?: boolean
  }>(),
  { variant: 'split' },
)
const emit = defineEmits<{ (e: 'update:modelValue', v: string): void }>()

const { t } = useI18n()

const isStack = computed(() => props.variant === 'stack')
const stackMode = ref<'edit' | 'preview'>('edit')

const SPLIT_KEY = 'agent-md-split-ratio'
const PREVIEW_COLLAPSED_KEY = 'agent-md-preview-collapsed'
/** Align with AgentStudioView SIDEBAR_COLLAPSED_W */
const PREVIEW_RAIL_W = '28px'
const collapseBtnClass =
  'flex shrink-0 items-center justify-center rounded w-[22px] h-[22px] text-txt3 transition hover:bg-elevated hover:text-accent-2 focus-visible:shadow-[inset_0_0_0_2px_rgba(99,102,241,0.35)] outline-none'

const splitRatio = ref(0.5)
const fmCollapsed = ref(true)
const previewManual = ref(false)
const syncing = ref(false)
const previewScroll = ref<HTMLElement | null>(null)
const splitBody = ref<HTMLElement | null>(null)
const dragging = ref(false)
const editorRef = ref<InstanceType<typeof CodeEditor> | null>(null)

const fmType = computed<FrontmatterType | null>(() => frontmatterTypeForPath(props.filePath))
const showFmPanel = computed(() => !isStack.value && fmType.value !== null && hasFrontmatter(props.modelValue))

const fmFields = ref<FrontmatterFields>({})

const previewHtml = computed(() => {
  const { body } = parseFrontmatter(props.modelValue)
  return renderMarkdown(body)
})

/** Raw YAML between --- fences for stack preview info block (prototype-aligned). */
const stackFmRaw = computed(() => {
  const m = props.modelValue.match(/^---\r?\n([\s\S]*?)\r?\n---\r?\n/)
  return m ? m[1] : null
})

function loadSplitRatio() {
  try {
    const v = parseFloat(localStorage.getItem(SPLIT_KEY) || '0.5')
    if (!Number.isNaN(v)) splitRatio.value = Math.min(0.72, Math.max(0.28, v))
  } catch {
    /* ignore */
  }
}

function saveSplitRatio() {
  try {
    localStorage.setItem(SPLIT_KEY, String(splitRatio.value))
  } catch {
    /* ignore */
  }
}

/** Sync read so first paint matches stored preference (avoid expand→collapse flash). */
function loadPreviewCollapsed(): boolean {
  try {
    const v = localStorage.getItem(PREVIEW_COLLAPSED_KEY)
    if (v === null) return false
    return v === 'true'
  } catch {
    return false
  }
}

/** Desktop split only: false = preview expanded (default). */
const previewCollapsed = ref(loadPreviewCollapsed())

function savePreviewCollapsed() {
  try {
    localStorage.setItem(PREVIEW_COLLAPSED_KEY, String(previewCollapsed.value))
  } catch {
    /* ignore quota / private mode */
  }
}

function relayoutEditor() {
  nextTick(() => {
    try {
      editorRef.value?.getEditor()?.layout()
    } catch {
      /* ignore */
    }
  })
}

function setPreviewCollapsed(next: boolean) {
  previewCollapsed.value = next
  savePreviewCollapsed()
  // Do not rewrite SPLIT_KEY on collapse; expand reuses in-memory/local splitRatio.
  relayoutEditor()
}

function collapsePreview() {
  setPreviewCollapsed(true)
}

function expandPreview() {
  setPreviewCollapsed(false)
}

function syncFormFromContent() {
  if (!fmType.value) return
  syncing.value = true
  const { fm } = parseFrontmatter(props.modelValue)
  if (fmType.value === 'skill') {
    fmFields.value = { name: fm?.name || '', description: fm?.description || '' }
  } else {
    fmFields.value = {
      description: fm?.description || '',
      alwaysApply: !!fm?.alwaysApply,
    }
  }
  syncing.value = false
}

function syncContentFromForm() {
  if (syncing.value || !fmType.value) return
  const { body } = parseFrontmatter(props.modelValue)
  const built = buildFrontmatter(fmFields.value, fmType.value) + body
  emit('update:modelValue', built)
}

function onEditorInput(v: string) {
  emit('update:modelValue', v)
  syncFormFromContent()
}

function onEditorScroll(payload: { scrollTop: number; scrollHeight: number; clientHeight: number }) {
  if (previewManual.value || !previewScroll.value) return
  const { scrollTop, scrollHeight, clientHeight } = payload
  const pb = previewScroll.value
  const ratio = scrollTop / (scrollHeight - clientHeight || 1)
  pb.scrollTop = ratio * (pb.scrollHeight - pb.clientHeight)
}

function onPreviewWheel() {
  previewManual.value = true
}

let previewScrollTimer: ReturnType<typeof setTimeout> | null = null
function onPreviewScroll() {
  if (previewScrollTimer) clearTimeout(previewScrollTimer)
  previewScrollTimer = setTimeout(() => {
    previewManual.value = false
  }, 2000)
}

function onEditorReady(editor: monaco.editor.IStandaloneCodeEditor) {
  editor.onDidScrollChange(() => {
    previewManual.value = false
  })
}

function onSashMouseDown(e: MouseEvent) {
  if (previewCollapsed.value) return
  dragging.value = true
  e.preventDefault()
}

function onMouseMove(e: MouseEvent) {
  if (!dragging.value || !splitBody.value || previewCollapsed.value) return
  const rect = splitBody.value.getBoundingClientRect()
  // Clamp only — never auto-collapse at ratio extremes.
  splitRatio.value = Math.min(0.72, Math.max(0.28, (e.clientX - rect.left) / rect.width))
}

function onMouseUp() {
  if (dragging.value) {
    dragging.value = false
    saveSplitRatio()
  }
}

watch(() => props.modelValue, () => syncFormFromContent(), { immediate: true })
watch(() => props.filePath, () => {
  fmCollapsed.value = true
  stackMode.value = 'edit'
  // Keep previewCollapsed across file switches (shared preference, not per-file).
  syncFormFromContent()
})
watch(showFmPanel, (visible, wasVisible) => {
  if (visible && wasVisible === false) fmCollapsed.value = true
})
watch(isStack, (stack) => {
  if (stack) stackMode.value = 'edit'
})

onMounted(() => {
  loadSplitRatio()
  document.addEventListener('mousemove', onMouseMove)
  document.addEventListener('mouseup', onMouseUp)
})

defineExpose({ previewCollapsed, collapsePreview, expandPreview })

onBeforeUnmount(() => {
  document.removeEventListener('mousemove', onMouseMove)
  document.removeEventListener('mouseup', onMouseUp)
  if (previewScrollTimer) clearTimeout(previewScrollTimer)
})
</script>

<template>
  <div class="flex h-full min-h-0 flex-col bg-surface">
    <!-- frontmatter panel (desktop split only) -->
    <div v-if="showFmPanel" class="border-b border-line bg-elevated">
      <button
        type="button"
        class="flex w-full items-center gap-1.5 px-3 py-1.5 text-[11px] font-semibold uppercase tracking-wider text-txt2 hover:bg-elevated"
        @click="fmCollapsed = !fmCollapsed"
      >
        <Icon
          name="chevron-down"
          :size="12"
          class="text-txt3 transition-transform"
          :class="fmCollapsed ? '-rotate-90' : ''"
        />
        Frontmatter
        <span class="ml-1 rounded border border-line bg-base px-1.5 py-0 font-mono text-[10px] font-normal normal-case tracking-normal text-accent-2">
          {{ fmType === 'skill' ? 'skills/SKILL.md' : 'rules/*.md' }}
        </span>
      </button>
      <div v-if="!fmCollapsed" class="border-t border-line px-3 py-2">
        <template v-if="fmType === 'rules'">
          <div class="grid grid-cols-[140px_1fr] items-start gap-2 border-b border-line/50 py-1.5">
            <label class="pt-1 text-[12px] text-txt2">description</label>
            <textarea
              v-model="fmFields.description"
              rows="2"
              :disabled="readonly"
              class="rounded-md w-full resize-y border border-line bg-base px-2 py-1 text-[12px] text-txt outline-none focus:border-accent disabled:opacity-60"
              @input="syncContentFromForm"
            />
          </div>
          <div class="grid grid-cols-[140px_1fr] items-center gap-2 py-1.5">
            <label class="text-[12px] text-txt2">alwaysApply</label>
            <div class="flex items-center gap-2">
              <button
                type="button"
                class="rounded-lg relative h-[18px] w-8 border transition-colors"
                :class="fmFields.alwaysApply ? 'border-accent bg-accent' : 'border-line-strong bg-overlay'"
                :disabled="readonly"
                aria-label="alwaysApply"
                @click="fmFields.alwaysApply = !fmFields.alwaysApply; syncContentFromForm()"
              >
                <span
                  class="always-apply-knob absolute top-0.5 h-3.5 w-3.5 bg-white shadow-[0_1px_3px_rgba(0,0,0,0.35)] transition-all"
                  :class="fmFields.alwaysApply ? 'left-4' : 'left-0.5'"
                />
              </button>
              <span class="font-mono text-[11px] text-txt3">{{ fmFields.alwaysApply ? 'true' : 'false' }}</span>
            </div>
          </div>
        </template>
        <template v-else-if="fmType === 'skill'">
          <div class="grid grid-cols-[140px_1fr] items-start gap-2 border-b border-line/50 py-1.5">
            <label class="pt-1 text-[12px] text-txt2">name</label>
            <input
              v-model="fmFields.name"
              type="text"
              :disabled="readonly"
              class="rounded-md w-full border border-line bg-base px-2 py-1 text-[12px] text-txt outline-none focus:border-accent disabled:opacity-60"
              @input="syncContentFromForm"
            />
          </div>
          <div class="grid grid-cols-[140px_1fr] items-start gap-2 py-1.5">
            <label class="pt-1 text-[12px] text-txt2">description</label>
            <textarea
              v-model="fmFields.description"
              rows="2"
              :disabled="readonly"
              class="rounded-md w-full resize-y border border-line bg-base px-2 py-1 text-[12px] text-txt outline-none focus:border-accent disabled:opacity-60"
              @input="syncContentFromForm"
            />
          </div>
        </template>
        <p class="mt-1 pl-[148px] text-[10.5px] text-txt3">{{ t('pages.markdownEditor.syncHint') }}</p>
      </div>
    </div>

    <!-- stack: edit / preview toggle -->
    <div v-if="isStack" class="rounded-md overflow-hidden mx-2.5 mb-2.5 mt-2 flex shrink-0 border border-line" role="tablist">
      <button
        type="button"
        class="min-h-11 flex-1 text-[12px] transition"
        :class="stackMode === 'edit' ? 'bg-accent-dim text-txt shadow-[inset_0_-2px_0_var(--color-accent,#7B61FF)]' : 'bg-base text-txt3'"
        role="tab"
        :aria-selected="stackMode === 'edit'"
        @click="stackMode = 'edit'"
      >
        {{ t('pages.markdownEditor.edit') }}
      </button>
      <button
        type="button"
        class="min-h-11 flex-1 text-[12px] transition"
        :class="stackMode === 'preview' ? 'bg-accent-dim text-txt shadow-[inset_0_-2px_0_var(--color-accent,#7B61FF)]' : 'bg-base text-txt3'"
        role="tab"
        :aria-selected="stackMode === 'preview'"
        @click="stackMode = 'preview'"
      >
        {{ t('pages.markdownEditor.preview') }}
      </button>
    </div>

    <!-- stack body: single-column editor or preview -->
    <div v-if="isStack" class="flex min-h-0 flex-1 flex-col">
      <div v-show="stackMode === 'edit'" class="flex min-h-0 flex-1 flex-col">
        <CodeEditor
          ref="editorRef"
          :model-value="modelValue"
          language="markdown"
          :minimap="false"
          :readonly="readonly"
          @update:model-value="onEditorInput"
        />
      </div>
      <div
        v-show="stackMode === 'preview'"
        class="scroll-area min-h-0 flex-1 overflow-auto bg-base px-4 py-3.5"
      >
        <pre
          v-if="stackFmRaw"
          class="rounded-md mb-3 whitespace-pre-wrap border border-line bg-elevated px-2.5 py-2 font-mono text-[11px] leading-relaxed text-txt3"
        >FRONTMATTER
{{ stackFmRaw }}</pre>
        <div class="md" v-html="previewHtml" />
      </div>
    </div>

    <!-- split body (desktop) -->
    <div v-else ref="splitBody" class="flex min-h-0 flex-1">
      <div
        class="flex h-full min-h-0 min-w-0 flex-col"
        :style="previewCollapsed ? { flex: '1 1 auto' } : { flex: `0 0 ${splitRatio * 100}%` }"
      >
        <CodeEditor
          ref="editorRef"
          :model-value="modelValue"
          language="markdown"
          :minimap="true"
          :readonly="readonly"
          @update:model-value="onEditorInput"
          @scroll="onEditorScroll"
          @ready="onEditorReady"
        />
      </div>
      <div
        v-if="!previewCollapsed"
        class="w-1 shrink-0 cursor-col-resize bg-line transition-colors hover:bg-accent"
        :class="dragging ? 'bg-accent' : ''"
        :title="t('pages.markdownEditor.resizeSash')"
        data-testid="md-preview-sash"
        @mousedown="onSashMouseDown"
      />
      <div
        v-if="!previewCollapsed"
        class="flex min-w-0 flex-1 flex-col bg-surface"
        data-testid="md-preview-panel"
      >
        <div v-if="!externalToolbar" class="flex shrink-0 items-center justify-between gap-2 border-b border-line bg-base/35 px-3 py-1 text-[10.5px] uppercase tracking-wider text-txt3">
          <span>{{ t('pages.markdownEditor.preview') }}</span>
          <span class="flex items-center gap-1.5">
            <span class="flex items-center gap-1 text-[10px] normal-case text-ok">
              <span class="h-1.5 w-1.5 rounded-full bg-ok" />{{ t('pages.markdownEditor.live') }}
            </span>
            <button
              type="button"
              :class="collapseBtnClass"
              :title="t('pages.markdownEditor.collapsePreview')"
              :aria-label="t('pages.markdownEditor.collapsePreview')"
              :aria-expanded="true"
              data-testid="md-preview-collapse"
              @click="collapsePreview"
            >
              <Icon name="chevron-right" :size="14" />
            </button>
          </span>
        </div>
        <div
          ref="previewScroll"
          class="scroll-area min-h-0 flex-1 overflow-auto px-5 py-4"
          @wheel="onPreviewWheel"
          @scroll="onPreviewScroll"
        >
          <div class="md" v-html="previewHtml" />
        </div>
      </div>
      <button
        v-if="previewCollapsed && !externalToolbar"
        type="button"
        class="flex shrink-0 select-none items-center justify-center gap-2 border-l border-line bg-base py-2.5 text-[10.5px] font-semibold uppercase tracking-[0.12em] text-txt3 transition hover:bg-elevated hover:text-accent-2 focus-visible:shadow-[inset_0_0_0_2px_rgba(99,102,241,0.35)] outline-none"
        :style="{ width: PREVIEW_RAIL_W, writingMode: 'vertical-rl', textOrientation: 'mixed' }"
        :title="t('pages.markdownEditor.expandPreview')"
        :aria-label="t('pages.markdownEditor.expandPreview')"
        :aria-expanded="false"
        data-testid="md-preview-expand-rail"
        @click="expandPreview"
      >
        <Icon name="chevron-right" :size="12" class="shrink-0 rotate-180" style="writing-mode: horizontal-tb" />
        <span>{{ t('pages.markdownEditor.preview') }}</span>
      </button>
    </div>
  </div>
</template>

<style scoped>
html.light .always-apply-knob {
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.2);
}
</style>
