<script setup lang="ts">
import { computed, defineAsyncComponent, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import SlidingTabs from '@/components/ui/SlidingTabs.vue'
import HtmlPreview from '@/components/ui/HtmlPreview.vue'
import { jsonErrorLocation } from '@/lib/run/artifactEdit'

const CodeEditor = defineAsyncComponent(() => import('@/components/ui/CodeEditor.vue'))

interface DecoratableEditor {
  deltaDecorations(old: string[], next: unknown[]): string[]
  revealLineInCenter?(line: number): void
}

const props = withDefaults(
  defineProps<{
    kind: string
    name: string
    content: string
    disabled?: boolean
  }>(),
  { disabled: false },
)
const emit = defineEmits<{ (e: 'update:content', v: string): void }>()
const { t } = useI18n()

const LANGUAGES: Record<string, string> = {
  json: 'json',
  yaml: 'yaml',
  markdown: 'markdown',
  html: 'html',
  text: 'plaintext',
}
const language = computed(() => LANGUAGES[props.kind] ?? 'plaintext')
const isHtml = computed(() => props.kind === 'html')
const htmlView = ref<'code' | 'preview'>('code')

const jsonError = computed(() => (props.kind === 'json' ? jsonErrorLocation(props.content) : null))

let editor: DecoratableEditor | null = null
let decorations: string[] = []

function applyErrorDecoration() {
  if (!editor) return
  const err = jsonError.value
  decorations = editor.deltaDecorations(
    decorations,
    err
      ? [{
          range: { startLineNumber: err.line, startColumn: 1, endLineNumber: err.line, endColumn: 1 },
          options: { isWholeLine: true, className: 'ed-code-error-line', glyphMarginClassName: 'ed-code-error-glyph' },
        }]
      : [],
  )
}

function onReady(e: unknown) {
  editor = e as DecoratableEditor
  applyErrorDecoration()
}

watch(jsonError, applyErrorDecoration)
</script>

<template>
  <div class="ed-code-editor" data-testid="artifact-code-editor">
    <SlidingTabs v-if="isHtml" class="ed-switch" :value="htmlView" role="tablist">
      <button
        type="button"
        role="tab"
        class="ed-switch__btn"
        :aria-selected="htmlView === 'code'"
        data-testid="artifact-editor-html-code"
        @click="htmlView = 'code'"
      >
        {{ t('pages.artifactEditor.modeCode') }}
      </button>
      <button
        type="button"
        role="tab"
        class="ed-switch__btn"
        :aria-selected="htmlView === 'preview'"
        data-testid="artifact-editor-html-preview"
        @click="htmlView = 'preview'"
      >
        {{ t('pages.artifactEditor.modePreview') }}
      </button>
    </SlidingTabs>
    <p
      v-if="jsonError"
      class="ed-banner ed-banner--err"
      role="alert"
      data-testid="artifact-editor-json-error"
    >
      {{ t('pages.artifactEditor.jsonError', { line: jsonError.line, column: jsonError.column }) }}
    </p>
    <div v-if="isHtml && htmlView === 'preview'" class="ed-code-editor__preview">
      <HtmlPreview :html="content" mode="inline" :modal-title="name" />
    </div>
    <div v-else class="ed-code-editor__host">
      <CodeEditor
        :model-value="content"
        :language="language"
        :readonly="disabled"
        @update:model-value="emit('update:content', $event)"
        @ready="onReady"
      />
    </div>
  </div>
</template>
