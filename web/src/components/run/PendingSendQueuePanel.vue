<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../ui/Icon.vue'
import type { ClarifyImage, ReactAnnotation } from '@/lib/shared/types'

export type PendingQueueRow = {
  id?: string
  text: string
  images: ClarifyImage[]
  annotations: ReactAnnotation[]
  /** Not yet acknowledged: shown, but not draggable, editable or a drop target. */
  locked?: boolean
}

const props = defineProps<{
  items: PendingQueueRow[]
  notice?: string | null
  toast?: string | null
  panelTestId?: string
  /** Messages the platform sends on its own (no edit / cancel / reorder). */
  readonly?: boolean
}>()

const emit = defineEmits<{
  (e: 'cancel', index: number): void
  (e: 'edit', index: number): void
  (e: 'reorder', fromIndex: number, toIndex: number): void
}>()

const { t } = useI18n()
const dragIndex = ref<number | null>(null)

function locked(index: number) {
  return !!props.items[index]?.locked
}

function onDragStart(index: number, e: DragEvent) {
  if (locked(index)) {
    e.preventDefault()
    return
  }
  dragIndex.value = index
  if (e.dataTransfer) {
    e.dataTransfer.effectAllowed = 'move'
    e.dataTransfer.setData('text/plain', String(index))
  }
}

function onDragEnd() {
  dragIndex.value = null
}

function onDragOver(e: DragEvent) {
  e.preventDefault()
  if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
}

function onDrop(toIndex: number, e: DragEvent) {
  e.preventDefault()
  const from =
    dragIndex.value ??
  Number.parseInt(e.dataTransfer?.getData('text/plain') || '', 10)
  dragIndex.value = null
  if (!Number.isFinite(from) || from === toIndex || from < 0 || from >= props.items.length) return
  if (locked(from) || locked(toIndex)) return
  emit('reorder', from, toIndex)
}
</script>

<template>
  <div
    v-if="items.length"
    class="mb-2 rounded-md border border-line bg-base/40 px-2.5 py-2"
    :data-testid="panelTestId || 'clarify-review-queue'"
  >
    <div class="mb-1 flex items-center gap-1.5 text-[11px] text-txt3">
      <Icon name="clock" :size="11" />
      {{ t('pages.agentChatTester.queue', { n: items.length }) }}
    </div>
    <div
      v-if="notice"
      class="mb-2 rounded border border-err/30 bg-err/10 px-2 py-1.5 text-[12px] text-err"
      role="alert"
      data-testid="clarify-queue-notice"
    >
      {{ notice }}
    </div>
    <div class="space-y-1">
      <div
        v-for="(q, qi) in items"
        :key="q.id || qi"
        data-testid="clarify-queue-item"
        class="flex min-w-0 flex-wrap items-center gap-2 rounded border border-line bg-surface px-2 py-1 text-[12px] text-txt2 transition-colors"
        :class="dragIndex === qi ? 'opacity-60 shadow-sm' : ''"
        :draggable="!readonly && !q.locked"
        @dragstart="onDragStart(qi, $event)"
        @dragend="onDragEnd"
        @dragover="onDragOver"
        @drop="onDrop(qi, $event)"
      >
        <button
          v-if="!readonly && !q.locked"
          type="button"
          class="flex shrink-0 cursor-grab items-center p-0.5 text-txt3 active:cursor-grabbing"
          :title="t('pages.clarify.queueDragTitle')"
          :aria-label="t('pages.clarify.queueDragAria')"
          @mousedown.stop
        >
          <Icon name="grip" :size="12" :disabled="dragIndex !== null" />
        </button>
        <span
          class="flex h-4 w-4 shrink-0 items-center justify-center rounded-full border border-line text-[9px] text-txt3"
        >{{ qi + 1 }}</span>
        <span class="min-w-0 flex-1 truncate" :title="q.text">{{ q.text || (q.images.length || q.annotations.length ? '…' : '') }}</span>
        <slot name="row-extra" :item="q" :index="qi" />
        <span v-if="q.images.length" class="shrink-0 rounded-full border border-line px-1.5 text-[9px] text-txt3">
          {{ t('pages.clarify.queueAttachmentBadge', { n: q.images.length }) }}
        </span>
        <span v-if="q.annotations.length" class="shrink-0 rounded-full border border-line px-1.5 text-[9px] text-txt3">
          {{ t('pages.clarify.queueAnnotationBadge') }}
        </span>
        <span v-if="!readonly && !q.locked" class="flex shrink-0 gap-0.5">
          <button
            type="button"
            class="flex h-6 w-6 items-center justify-center rounded-md text-txt3 hover:bg-base hover:text-txt"
            :title="t('pages.clarify.queueEditTitle')"
            :aria-label="t('pages.clarify.queueEditAria')"
            data-testid="clarify-queue-edit"
            @click.stop="emit('edit', qi)"
          >
            <Icon name="edit" :size="13" />
          </button>
          <button
            type="button"
            class="flex h-6 w-6 items-center justify-center rounded-md text-txt3 hover:bg-err/10 hover:text-err"
            :title="t('pages.clarify.queueCancelTitle')"
            :aria-label="t('pages.clarify.queueCancelAria')"
            data-testid="clarify-queue-cancel"
            @click.stop="emit('cancel', qi)"
          >
            <Icon name="close" :size="13" />
          </button>
        </span>
      </div>
    </div>
    <div
      v-if="toast"
      class="pointer-events-none fixed bottom-7 left-1/2 z-20 -translate-x-1/2 rounded-full bg-txt px-3.5 py-2 text-[12px] text-surface shadow-lg"
      role="status"
      data-testid="clarify-queue-toast"
    >
      {{ toast }}
    </div>
  </div>
</template>
