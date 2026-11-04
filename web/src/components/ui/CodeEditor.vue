<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, watch } from 'vue'
// Import the editor core only, then opt into the specific languages we need
// (keeps the bundle small vs. the all-languages `monaco-editor` entry).
import * as monaco from 'monaco-editor/esm/vs/editor/editor.api'
import 'monaco-editor/esm/vs/editor/contrib/find/browser/findController'
import 'monaco-editor/esm/vs/basic-languages/markdown/markdown.contribution'
import 'monaco-editor/esm/vs/basic-languages/shell/shell.contribution'
import 'monaco-editor/esm/vs/basic-languages/yaml/yaml.contribution'
import 'monaco-editor/esm/vs/basic-languages/html/html.contribution'
import 'monaco-editor/esm/vs/basic-languages/javascript/javascript.contribution'
import 'monaco-editor/esm/vs/basic-languages/typescript/typescript.contribution'
import 'monaco-editor/esm/vs/basic-languages/python/python.contribution'
import 'monaco-editor/esm/vs/basic-languages/ini/ini.contribution'
import 'monaco-editor/esm/vs/language/json/monaco.contribution'
import EditorWorker from 'monaco-editor/esm/vs/editor/editor.worker?worker'
import JsonWorker from 'monaco-editor/esm/vs/language/json/json.worker?worker'
import { theme } from '@/lib/shared/theme'

// Wire Monaco web workers for Vite (only json + the base editor worker; other
// languages fall back to the editor worker which is enough for highlighting).
;(self as any).MonacoEnvironment = {
  getWorker(_: string, label: string) {
    if (label === 'json') return new JsonWorker()
    return new EditorWorker()
  },
}

const props = withDefaults(
  defineProps<{ modelValue: string; language?: string; readonly?: boolean; minimap?: boolean }>(),
  { language: 'markdown', readonly: false, minimap: false },
)
const emit = defineEmits<{
  (e: 'update:modelValue', v: string): void
  (e: 'ready', editor: monaco.editor.IStandaloneCodeEditor): void
  (e: 'scroll', payload: { scrollTop: number; scrollHeight: number; clientHeight: number }): void
}>()

const host = ref<HTMLElement | null>(null)
let editor: monaco.editor.IStandaloneCodeEditor | null = null
let applying = false
let scrollDisposable: monaco.IDisposable | null = null

function monacoTheme() {
  return theme.value === 'light' ? 'vs' : 'vs-dark'
}

function updateMinimap() {
  editor?.updateOptions({ minimap: { enabled: props.minimap } })
}

onMounted(() => {
  if (!host.value) return
  editor = monaco.editor.create(host.value, {
    value: props.modelValue,
    language: props.language,
    theme: monacoTheme(),
    readOnly: props.readonly,
    automaticLayout: true,
    minimap: { enabled: props.minimap },
    fontSize: 12.5,
    lineNumbers: 'on',
    scrollBeyondLastLine: false,
    tabSize: 2,
    wordWrap: 'on',
    renderLineHighlight: 'line',
    fixedOverflowWidgets: true,
    padding: { top: 10, bottom: 10 },
    scrollbar: { verticalScrollbarSize: 10, horizontalScrollbarSize: 10 },
  })
  editor.onDidChangeModelContent(() => {
    if (applying) return
    emit('update:modelValue', editor!.getValue())
  })
  scrollDisposable = editor.onDidScrollChange(() => {
    if (!editor) return
    emit('scroll', {
      scrollTop: editor.getScrollTop(),
      scrollHeight: editor.getScrollHeight(),
      clientHeight: editor.getLayoutInfo().height,
    })
  })
  emit('ready', editor)
})

watch(
  () => props.modelValue,
  (v) => {
    if (!editor || v === editor.getValue()) return
    applying = true
    editor.setValue(v ?? '')
    applying = false
  },
)

watch(
  () => props.language,
  (lang) => {
    const model = editor?.getModel()
    if (model) monaco.editor.setModelLanguage(model, lang || 'plaintext')
  },
)

watch(() => props.minimap, updateMinimap)

watch(theme, () => monaco.editor.setTheme(monacoTheme()))

function getEditor() {
  return editor
}

defineExpose({ getEditor })

onBeforeUnmount(() => {
  scrollDisposable?.dispose()
  editor?.getModel()?.dispose()
  editor?.dispose()
  editor = null
})
</script>

<template>
  <div ref="host" class="h-full w-full" />
</template>
