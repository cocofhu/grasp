<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'

const props = defineProps<{ disabled?: boolean }>()
const emit = defineEmits<{ files: [files: File[]] }>()
const { t } = useI18n()
const depth = ref(0)
const notice = ref('')
const reset = () => { depth.value = 0 }

function hasFiles(e: DragEvent) {
  return Array.from(e.dataTransfer?.types || []).includes('Files')
}
function enter(e: DragEvent) {
  if (!hasFiles(e)) return
  e.preventDefault()
  if (!props.disabled) depth.value++
}
function over(e: DragEvent) {
  if (!hasFiles(e)) return
  e.preventDefault()
  if (e.dataTransfer) e.dataTransfer.dropEffect = props.disabled ? 'none' : 'copy'
  if (!props.disabled) depth.value = Math.max(1, depth.value)
}
function leave() { depth.value = Math.max(0, depth.value - 1) }
function drop(e: DragEvent) {
  if (!hasFiles(e)) return
  e.preventDefault()
  e.stopPropagation()
  reset()
  if (props.disabled) return
  notice.value = ''
  const items = Array.from(e.dataTransfer?.items || [])
  const directories = items.filter(item => item.kind === 'file' && item.webkitGetAsEntry?.()?.isDirectory)
  const fileItems = items.filter(item => item.kind === 'file')
  const files = fileItems.length
    ? fileItems.filter(item => !item.webkitGetAsEntry?.()?.isDirectory).map(item => item.getAsFile()).filter((file): file is File => !!file)
    : Array.from(e.dataTransfer?.files || [])
  if (directories.length) notice.value = t('common.attachments.noDirectories')
  if (files.length) emit('files', files)
}
function keydown(e: KeyboardEvent) { if (e.key === 'Escape') reset() }
watch(() => props.disabled, reset)
onMounted(() => {
  window.addEventListener('dragend', reset)
  window.addEventListener('drop', reset, true)
  window.addEventListener('blur', reset)
  window.addEventListener('keydown', keydown)
})
onBeforeUnmount(() => {
  window.removeEventListener('dragend', reset)
  window.removeEventListener('drop', reset, true)
  window.removeEventListener('blur', reset)
  window.removeEventListener('keydown', keydown)
})
</script>

<template>
  <div class="relative min-w-0" data-attachment-drop-zone @dragenter="enter" @dragover="over" @dragleave="leave" @drop="drop">
    <slot />
    <p v-if="notice" class="mt-1 text-xs text-err" role="alert">{{ notice }}</p>
    <div
      v-if="depth > 0 && !disabled"
      class="pointer-events-none absolute inset-0 z-20 flex items-center justify-center gap-2 rounded-xl border-2 border-dashed border-accent bg-surface/95 px-3 py-4 text-sm font-medium text-accent"
      role="status"
      data-testid="attachment-drop-hint"
    >
      <Icon name="paperclip" :size="20" />{{ t('common.attachments.dropHere') }}
    </div>
  </div>
</template>
