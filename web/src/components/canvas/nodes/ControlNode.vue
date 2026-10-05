<script setup lang="ts">
import Icon from '../../ui/Icon.vue'
import NodeShell from './NodeShell.vue'
import { NODE_WIDTH } from '../composables/useAutoLayout'
import type { CanvasNodeData } from '../composables/canvasContext'

defineProps<{ id: string; data: CanvasNodeData; selected?: boolean }>()
</script>

<template>
  <NodeShell v-memo="[data, selected]" :id="id" :data="data" :selected="selected" :width="NODE_WIDTH">
    <template #icon>
      <span class="cnode-icon" aria-hidden="true"><Icon :name="data.icon" :size="15" /></span>
    </template>
    <template #sub>{{ data.subtitle || data.typeLabel }}</template>
  </NodeShell>
</template>
