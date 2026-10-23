<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '../../ui/Icon.vue'

defineProps<{
  zoom: number
  editable: boolean
  minimap: boolean
  canUndo?: boolean
  canRedo?: boolean
}>()

const emit = defineEmits<{
  (e: 'zoom-in'): void
  (e: 'zoom-out'): void
  (e: 'fit'): void
  (e: 'layout'): void
  (e: 'toggle-minimap'): void
  (e: 'undo'): void
  (e: 'redo'): void
  (e: 'help'): void
}>()

const { t } = useI18n()
</script>

<template>
  <div class="cchrome absolute bottom-3 left-3 z-10 flex items-center gap-0.5 p-1" role="toolbar" :aria-label="t('canvas.aria.canvas')" data-testid="canvas-toolbar">
    <template v-if="editable">
      <button type="button" class="cchrome-btn" :disabled="!canUndo" :aria-label="t('canvas.toolbar.undo')" :title="t('canvas.toolbar.undo')" data-testid="canvas-undo" @click="emit('undo')">
        <Icon name="undo" :size="15" />
      </button>
      <button type="button" class="cchrome-btn" :disabled="!canRedo" :aria-label="t('canvas.toolbar.redo')" :title="t('canvas.toolbar.redo')" data-testid="canvas-redo" @click="emit('redo')">
        <Icon name="redo" :size="15" />
      </button>
      <span class="mx-0.5 h-4 w-px bg-line" aria-hidden="true" />
    </template>
    <button type="button" class="cchrome-btn" :aria-label="t('canvas.toolbar.zoomOut')" :title="t('canvas.toolbar.zoomOut')" @click="emit('zoom-out')">
      <Icon name="zoom-out" :size="15" />
    </button>
    <span class="w-10 text-center text-[11px] tabular-nums text-txt3" data-testid="canvas-zoom">{{ Math.round(zoom * 100) }}%</span>
    <button type="button" class="cchrome-btn" :aria-label="t('canvas.toolbar.zoomIn')" :title="t('canvas.toolbar.zoomIn')" @click="emit('zoom-in')">
      <Icon name="plus" :size="15" />
    </button>
    <button type="button" class="cchrome-btn" :aria-label="t('canvas.toolbar.fitView')" :title="t('canvas.toolbar.fitView')" data-testid="canvas-fit" @click="emit('fit')">
      <Icon name="expand" :size="14" />
    </button>
    <button v-if="editable" type="button" class="cchrome-btn" :aria-label="t('canvas.toolbar.autoLayout')" :title="t('canvas.toolbar.autoLayout')" data-testid="canvas-layout" @click="emit('layout')">
      <Icon name="layout" :size="15" />
    </button>
    <button type="button" class="cchrome-btn" :class="{ 'is-on': minimap }" :aria-pressed="minimap" :aria-label="t('canvas.toolbar.minimap')" :title="t('canvas.toolbar.minimap')" data-testid="canvas-minimap-toggle" @click="emit('toggle-minimap')">
      <Icon name="minimap" :size="15" />
    </button>
    <button type="button" class="cchrome-btn" :aria-label="t('canvas.toolbar.help')" :title="t('canvas.toolbar.help')" data-testid="canvas-help" @click="emit('help')">
      <Icon name="help" :size="14" />
    </button>
  </div>
</template>
