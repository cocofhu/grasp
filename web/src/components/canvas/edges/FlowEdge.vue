<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { EdgeLabelRenderer, type GraphNode, type Position } from '@vue-flow/core'
import Icon from '../../ui/Icon.vue'
import { edgeGeometry } from '../composables/edgePath'
import { useCanvasContext, type CanvasEdgeData } from '../composables/canvasContext'

defineOptions({ inheritAttrs: false })

const props = defineProps<{
  id: string
  sourceX: number
  sourceY: number
  targetX: number
  targetY: number
  sourcePosition: Position
  targetPosition: Position
  sourceNode?: GraphNode
  targetNode?: GraphNode
  data: CanvasEdgeData
  markerEnd?: string
  selected?: boolean
}>()

const { t } = useI18n()
const ctx = useCanvasContext()

function box(n?: GraphNode) {
  if (!n) return undefined
  return { y: n.computedPosition?.y ?? n.position.y, height: n.dimensions?.height || 0 }
}

const geo = computed(() =>
  edgeGeometry(
    {
      sourceX: props.sourceX,
      sourceY: props.sourceY,
      targetX: props.targetX,
      targetY: props.targetY,
      sourcePosition: props.sourcePosition,
      targetPosition: props.targetPosition,
    },
    box(props.sourceNode),
    box(props.targetNode),
  ),
)

const hovered = computed(() => ctx?.hoveredEdge.value === props.id)
const showControls = computed(() => props.data.editable && (hovered.value || props.selected))

const pathClass = computed(() => [
  `tone-${props.data.tone}`,
  {
    'is-dashed': props.data.dashed || geo.value.back,
    'is-hovered': hovered.value,
    [`run-${props.data.run}`]: !!props.data.run,
  },
])

const midStyle = computed(() => ({
  transform: `translate(-50%, -50%) translate(${geo.value.labelX}px, ${geo.value.labelY}px)`,
}))

function enter() {
  ctx?.setEdgeHover(props.id)
}
function leave() {
  ctx?.setEdgeHover(null)
}
</script>

<template>
  <path
    :d="geo.path"
    class="vue-flow__edge-path cedge-path"
    :class="pathClass"
    :marker-end="markerEnd"
    :data-testid="`canvas-edge-${id}`"
    :data-back="geo.back ? 'true' : undefined"
  />
  <path
    :d="geo.path"
    class="vue-flow__edge-interaction"
    fill="none"
    stroke="transparent"
    stroke-width="20"
    @mouseenter="enter"
    @mouseleave="leave"
  />
  <EdgeLabelRenderer>
    <div
      v-if="data.label || showControls"
      class="cedge-mid nodrag nopan"
      :style="midStyle"
      :data-testid="`canvas-edge-mid-${id}`"
      @mouseenter="enter"
      @mouseleave="leave"
    >
      <button
        v-if="showControls"
        type="button"
        class="cedge-btn"
        :aria-label="t('canvas.aria.insertNode')"
        :title="t('canvas.aria.insertNode')"
        data-testid="canvas-edge-insert"
        @click.stop="ctx?.onEdgeInsert(id, $event)"
      >
        <Icon name="plus" :size="12" />
      </button>
      <button
        v-if="data.label"
        type="button"
        class="cedge-label"
        :class="`tone-${data.tone}`"
        :title="data.label"
        :disabled="!data.editable"
        data-testid="canvas-edge-label"
        @click.stop="ctx?.onEdgeEdit(id, $event)"
      >
        {{ data.label }}
      </button>
      <button
        v-else-if="showControls"
        type="button"
        class="cedge-btn"
        :aria-label="t('canvas.aria.editCondition')"
        :title="t('canvas.aria.editCondition')"
        data-testid="canvas-edge-edit"
        @click.stop="ctx?.onEdgeEdit(id, $event)"
      >
        <Icon name="edit" :size="11" />
      </button>
      <button
        v-if="showControls"
        type="button"
        class="cedge-btn is-danger"
        :aria-label="t('canvas.aria.deleteEdge')"
        :title="t('canvas.aria.deleteEdge')"
        data-testid="canvas-edge-delete"
        @click.stop="ctx?.onEdgeDelete(id)"
      >
        <Icon name="close" :size="11" />
      </button>
    </div>
  </EdgeLabelRenderer>
</template>
