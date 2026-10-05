<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../../ui/Icon.vue'
import type { EdgeKind, WFEdge } from '@/lib/shared/types'

const props = defineProps<{
  edge: WFEdge
  /** Anchor in host-relative pixels. */
  x: number
  y: number
  bounds: { width: number; height: number }
}>()

const emit = defineEmits<{
  (e: 'update', patch: Partial<Pick<WFEdge, 'when' | 'kind' | 'label'>>): void
  (e: 'delete'): void
  (e: 'close'): void
}>()

const { t } = useI18n()
const root = ref<HTMLElement | null>(null)
const whenInput = ref<HTMLInputElement | null>(null)

const KINDS: EdgeKind[] = ['success', 'failure', 'rollback']

const WIDTH = 300
const HEIGHT = 250
const style = computed(() => {
  const left = Math.max(8, Math.min(props.x - WIDTH / 2, props.bounds.width - WIDTH - 8))
  const below = props.y + 16 + HEIGHT < props.bounds.height
  const top = below ? props.y + 16 : Math.max(8, props.y - HEIGHT - 16)
  return { left: `${left}px`, top: `${top}px`, width: `${WIDTH}px` }
})

function onDocDown(ev: MouseEvent) {
  if (root.value && !root.value.contains(ev.target as Node)) emit('close')
}
onMounted(async () => {
  document.addEventListener('mousedown', onDocDown, true)
  await nextTick()
  whenInput.value?.focus()
})
onBeforeUnmount(() => document.removeEventListener('mousedown', onDocDown, true))
</script>

<template>
  <div
    ref="root"
    class="cchrome absolute z-30 p-3 text-[12px]"
    :style="style"
    role="dialog"
    :aria-label="t('canvas.edge.editorTitle')"
    data-testid="edge-label-editor"
    @keydown.esc.stop.prevent="emit('close')"
  >
    <div class="mb-2 flex items-center justify-between">
      <span class="text-[12px] font-semibold text-txt">{{ t('canvas.edge.editorTitle') }}</span>
      <button type="button" class="cchrome-btn !h-6 !min-w-6" :aria-label="t('common.buttons.close')" @click="emit('close')">
        <Icon name="close" :size="13" />
      </button>
    </div>
    <label class="mb-1 block text-[11px] text-txt3" for="edge-when">{{ t('canvas.edge.when') }}</label>
    <input
      id="edge-when"
      ref="whenInput"
      class="input mb-2 font-mono text-[12px]"
      :value="edge.when || ''"
      :placeholder="t('canvas.edge.whenPlaceholder')"
      data-testid="edge-when-input"
      @input="emit('update', { when: ($event.target as HTMLInputElement).value })"
      @keydown.enter.prevent="emit('close')"
    />
    <label class="mb-1 block text-[11px] text-txt3" for="edge-kind">{{ t('canvas.edge.kind') }}</label>
    <select
      id="edge-kind"
      class="input mb-2 text-[12px]"
      :value="edge.kind || 'success'"
      data-testid="edge-kind-select"
      @change="emit('update', { kind: ($event.target as HTMLSelectElement).value as EdgeKind })"
    >
      <option v-for="k in KINDS" :key="k" :value="k">{{ t(`common.edgeKinds.${k}.label`) }}</option>
    </select>
    <label class="mb-1 block text-[11px] text-txt3" for="edge-note">{{ t('canvas.edge.note') }}</label>
    <input
      id="edge-note"
      class="input mb-3 text-[12px]"
      :value="edge.label || ''"
      :placeholder="t('canvas.edge.notePlaceholder')"
      data-testid="edge-note-input"
      @input="emit('update', { label: ($event.target as HTMLInputElement).value })"
      @keydown.enter.prevent="emit('close')"
    />
    <div class="flex items-center justify-between">
      <button
        type="button"
        class="inline-flex items-center gap-1 rounded-md px-2 py-1 text-[12px] text-err hover:bg-err/10"
        data-testid="edge-delete"
        @click="emit('delete')"
      >
        <Icon name="trash" :size="13" />{{ t('canvas.edge.delete') }}
      </button>
      <button
        type="button"
        class="rounded-md bg-accent px-3 py-1 text-[12px] font-medium text-white hover:bg-accent-hover"
        data-testid="edge-done"
        @click="emit('close')"
      >
        {{ t('canvas.edge.done') }}
      </button>
    </div>
  </div>
</template>
