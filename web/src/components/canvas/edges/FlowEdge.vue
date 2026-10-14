<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { EdgeLabelRenderer, useVueFlow, type GraphNode, type Position } from '@vue-flow/core'
import Icon from '../../ui/Icon.vue'
import { edgeGeometry, laneFloor, type EdgeEnds } from '../composables/edgePath'
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

const { getNodes } = useVueFlow()

function box(n?: GraphNode) {
  if (!n) return undefined
  return { y: n.computedPosition?.y ?? n.position.y, height: n.dimensions?.height || 0 }
}

const ends = computed<EdgeEnds>(() => ({
  sourceX: props.sourceX,
  sourceY: props.sourceY,
  targetX: props.targetX,
  targetY: props.targetY,
  sourcePosition: props.sourcePosition,
  targetPosition: props.targetPosition,
}))

const back = computed(() => props.data.backLane !== undefined)

const floor = computed(() => {
  if (!back.value) return undefined
  const cards = getNodes.value.map((n) => ({
    x: n.computedPosition?.x ?? n.position.x,
    y: n.computedPosition?.y ?? n.position.y,
    width: n.dimensions?.width || 0,
    height: n.dimensions?.height || 0,
  }))
  return laneFloor(ends.value, cards, props.data.backLane ?? 0)
})

const geo = computed(() =>
  edgeGeometry(ends.value, box(props.sourceNode), box(props.targetNode), props.data.backLane ?? 0, {
    back: back.value,
    floor: floor.value,
  }),
)

const HANDOFF_CHIP_CHARS = 24
const handoffOpen = ref(false)
const handoffChip = computed(() => {
  const text = (props.data.handoff?.text || '').replace(/\s+/g, ' ').trim()
  const chars = Array.from(text)
  return chars.length > HANDOFF_CHIP_CHARS ? chars.slice(0, HANDOFF_CHIP_CHARS).join('') + '…' : text
})
const handoffTitle = computed(() => {
  const h = props.data.handoff
  if (!h) return ''
  const head = h.source === 'platform' ? t('canvas.handoff.platformTitle') : t('canvas.handoff.title', { source: props.data.sourceLabel })
  return `${head}\n${h.text}`
})

const hovered = computed(() => ctx?.hoveredEdge.value === props.id)
const showControls = computed(() => props.data.editable && (hovered.value || props.selected))

const pathClass = computed(() => [
  `tone-${props.data.tone}`,
  {
    'is-dashed': props.data.dashed || geo.value.back,
    'is-hovered': hovered.value,
    'is-replacing': !!props.data.replacing,
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
    :data-replacing="data.replacing ? 'true' : undefined"
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
      v-if="data.label || showControls || data.handoff"
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
      <button
        v-if="data.handoff"
        type="button"
        class="cedge-handoff"
        :class="{ 'is-platform': data.handoff.source === 'platform', 'is-open': handoffOpen }"
        :title="handoffTitle"
        :aria-expanded="handoffOpen"
        :aria-label="t('canvas.handoff.aria', { source: data.sourceLabel, target: data.targetLabel })"
        data-testid="canvas-edge-handoff"
        @click.stop="handoffOpen = !handoffOpen"
      >
        {{ handoffChip }}
      </button>
      <div
        v-if="data.handoff && handoffOpen"
        class="cedge-handoff-pop"
        role="dialog"
        :aria-label="t('canvas.handoff.aria', { source: data.sourceLabel, target: data.targetLabel })"
        data-testid="canvas-edge-handoff-pop"
        @click.stop
        @wheel.stop
      >
        <div class="cedge-handoff-head">
          <span>{{ data.handoff.source === 'platform' ? t('canvas.handoff.platformTitle') : t('canvas.handoff.title', { source: data.sourceLabel }) }}</span>
          <button type="button" class="cedge-btn" :aria-label="t('canvas.handoff.close')" @click.stop="handoffOpen = false">
            <Icon name="close" :size="11" />
          </button>
        </div>
        <div class="cedge-handoff-body">{{ data.handoff.text }}</div>
      </div>
    </div>
  </EdgeLabelRenderer>
</template>
