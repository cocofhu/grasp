<script setup lang="ts">
import { ref, computed, watch, nextTick, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import CodeEditor from '@/components/ui/CodeEditor.vue'
import Icon from '@/components/ui/Icon.vue'
import MarkdownViewToggle, { type MarkdownViewMode } from '@/components/agent/MarkdownViewToggle.vue'
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
    /** Host renders the edit/split/preview toggle in its own toolbar (no in-pane header). */
    externalToolbar?: boolean
  }>(),
  { variant: 'split' },
)
const emit = defineEmits<{ (e: 'update:modelValue', v: string): void }>()

const { t } = useI18n()

const isStack = computed(() => props.variant === 'stack')
const stackMode = ref<'edit' | 'preview'>('edit')

const SPLIT_KEY = 'agent-md-split-ratio'
const VIEW_MODE_KEY = 'agent-md-view-mode'
const VIEW_MODES: MarkdownViewMode[] = ['edit', 'split', 'preview']

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

/** Sync read so first paint matches stored preference (avoid layout flash). */
function loadViewMode(): MarkdownViewMode {
  try {
    const v = localStorage.getItem(VIEW_MODE_KEY) as MarkdownViewMode | null
    return v && VIEW_MODES.includes(v) ? v : 'edit'
  } catch {
    return 'edit'
  }
}

/** Desktop split variant only; single-column editing by default. */
const viewMode = ref<MarkdownViewMode>(loadViewMode())

function relayoutEditor() {
  nextTick(() => {
    try {
      editorRef.value?.getEditor()?.layout()
    } catch {
      /* ignore */
    }
  })
}

function setViewMode(next: MarkdownViewMode) {
  viewMode.value = next
  try {
    localStorage.setItem(VIEW_MODE_KEY, next)
  } catch {
    /* ignore quota / private mode */
  }
  // Do not rewrite SPLIT_KEY on mode change; split reuses in-memory/local splitRatio.
  relayoutEditor()
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
  if (viewMode.value !== 'split') return
  dragging.value = true
  e.preventDefault()
}

function onMouseMove(e: MouseEvent) {
  if (!dragging.value || !splitBody.value || viewMode.value !== 'split') return
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
  // Keep viewMode across file switches (shared preference, not per-file).
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

defineExpose({ viewMode, setViewMode })

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

    <!-- split body (desktop): edit / split / preview -->
    <template v-else>
      <div v-if="!externalToolbar" class="flex shrink-0 justify-end border-b border-line px-2 py-1">
        <MarkdownViewToggle :model-value="viewMode" @update:model-value="setViewMode" />
      </div>
      <div ref="splitBody" class="flex min-h-0 flex-1">
        <div
          v-show="viewMode !== 'preview'"
          class="flex h-full min-h-0 min-w-0 flex-col"
          :style="viewMode === 'split' ? { flex: `0 0 ${splitRatio * 100}%` } : { flex: '1 1 auto' }"
        >
          <CodeEditor
            ref="editorRef"
            :model-value="modelValue"
            language="markdown"
            :minimap="false"
            :readonly="readonly"
            @update:model-value="onEditorInput"
            @scroll="onEditorScroll"
            @ready="onEditorReady"
          />
        </div>
        <div
          v-if="viewMode === 'split'"
          class="w-1 shrink-0 cursor-col-resize bg-line transition-colors hover:bg-accent"
          :class="dragging ? 'bg-accent' : ''"
          :title="t('pages.markdownEditor.resizeSash')"
          data-testid="md-preview-sash"
          @mousedown="onSashMouseDown"
        />
        <div
          v-if="viewMode !== 'edit'"
          ref="previewScroll"
          class="scroll-area min-w-0 flex-1 overflow-auto bg-surface"
          data-testid="md-preview-panel"
          @wheel="onPreviewWheel"
          @scroll="onPreviewScroll"
        >
          <div class="md" :class="viewMode === 'preview' ? 'mx-auto max-w-[780px] px-8 py-6' : 'px-5 py-4'" v-html="previewHtml" />
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
html.light .always-apply-knob {
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.2);
}
</style>
