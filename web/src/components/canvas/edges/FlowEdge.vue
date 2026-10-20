<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { EdgeLabelRenderer, useVueFlow, type GraphNode, type Position } from '@vue-flow/core'
import Icon from '../../ui/Icon.vue'
import { renderMarkdown } from '@/lib/shared/markdown'
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

const handoffOpen = ref(false)
const handoffIndex = ref(0)
const handoffItems = computed(() => {
  const list = props.data.handoffs?.length ? props.data.handoffs : props.data.handoff ? [props.data.handoff] : []
  return list
    .map((item) => ({
      text: (item?.text ?? '').trim(),
      source: item?.source === 'platform' ? ('platform' as const) : ('agent' as const),
    }))
    .filter((item) => item.text.length > 0)
})
const hasHandoff = computed(() => handoffItems.value.length > 0)
const handoffMany = computed(() => handoffItems.value.length > 1)
const activeHandoff = computed(() => handoffItems.value[handoffIndex.value] ?? handoffItems.value[handoffItems.value.length - 1])
const handoffPlatform = computed(() => activeHandoff.value?.source === 'platform')
/** Parse Markdown only while the card is open so collapsed edges stay cheap (LRU-cached). */
const handoffHtml = computed(() =>
  handoffOpen.value && activeHandoff.value ? renderMarkdown(activeHandoff.value.text) : '',
)

function lastHandoffIndex() {
  return Math.max(0, handoffItems.value.length - 1)
}

watch(
  handoffItems,
  () => {
    const last = lastHandoffIndex()
    if (!handoffOpen.value || handoffIndex.value > last) handoffIndex.value = last
  },
  { immediate: true },
)

function toggleHandoff() {
  if (handoffOpen.value) {
    handoffOpen.value = false
    handoffIndex.value = lastHandoffIndex()
    return
  }
  handoffIndex.value = lastHandoffIndex()
  handoffOpen.value = true
}

function selectHandoff(index: number) {
  handoffIndex.value = index
}

function closeHandoff() {
  handoffOpen.value = false
  handoffIndex.value = lastHandoffIndex()
}

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
      v-if="data.label || showControls || hasHandoff"
      class="cedge-mid nodrag nopan"
      :class="{ 'is-handoff-open': hasHandoff && handoffOpen }"
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
        v-if="hasHandoff"
        type="button"
        class="cedge-handoff"
        :class="{ 'is-platform': handoffPlatform, 'is-open': handoffOpen }"
        :aria-expanded="handoffOpen"
        :aria-label="
          handoffMany
            ? t('canvas.handoff.ariaCount', { source: data.sourceLabel, target: data.targetLabel, n: handoffItems.length })
            : t('canvas.handoff.aria', { source: data.sourceLabel, target: data.targetLabel })
        "
        data-testid="canvas-edge-handoff"
        @click.stop="toggleHandoff"
      >
        <Icon class="cedge-handoff-ico" name="swap" :size="12" />
        <span>{{ t('canvas.handoff.chip') }}</span>
      </button>
      <div
        v-if="hasHandoff && handoffOpen"
        class="cedge-handoff-pop"
        role="dialog"
        :aria-label="t('canvas.handoff.aria', { source: data.sourceLabel, target: data.targetLabel })"
        data-testid="canvas-edge-handoff-pop"
        @click.stop
        @wheel.stop
      >
        <div class="cedge-handoff-head">
          <span class="cedge-handoff-badge" :class="{ 'is-platform': handoffPlatform }">
            <Icon name="swap" :size="14" />
          </span>
          <div class="cedge-handoff-headtext">
            <div class="cedge-handoff-title">{{ t('canvas.handoff.title') }}</div>
            <div class="cedge-handoff-route">
              <span class="cedge-handoff-node">{{ data.sourceLabel }}</span>
              <Icon name="arrow-right" :size="11" />
              <span class="cedge-handoff-node">{{ data.targetLabel }}</span>
            </div>
          </div>
          <button
            type="button"
            class="cedge-handoff-x"
            :aria-label="t('canvas.handoff.close')"
            data-testid="canvas-edge-handoff-close"
            @click.stop="closeHandoff"
          >
            <Icon name="close" :size="13" />
          </button>
        </div>
        <div v-if="handoffMany" class="cedge-handoff-tabs" role="tablist" data-testid="canvas-edge-handoff-tabs">
          <button
            v-for="(item, index) in handoffItems"
            :key="index"
            type="button"
            class="cedge-handoff-tab"
            :class="{ 'is-on': index === handoffIndex }"
            role="tab"
            :aria-selected="index === handoffIndex"
            :data-visit="index + 1"
            data-testid="canvas-edge-handoff-tab"
            @click.stop="selectHandoff(index)"
          >
            {{ t('canvas.handoff.visit', { n: index + 1 }) }}
          </button>
        </div>
        <div class="cedge-handoff-body md" data-testid="canvas-edge-handoff-body" v-html="handoffHtml" />
        <div class="cedge-handoff-foot">
          <span v-if="handoffPlatform" class="cedge-handoff-tag" data-testid="canvas-edge-handoff-platform">
            {{ t('canvas.handoff.platformNote') }}
          </span>
          <span v-else data-testid="canvas-edge-handoff-source">
            {{
              handoffMany
                ? t('canvas.handoff.fromNodeVisit', { source: data.sourceLabel, n: handoffIndex + 1 })
                : t('canvas.handoff.fromNode', { source: data.sourceLabel })
            }}
          </span>
        </div>
      </div>
    </div>
  </EdgeLabelRenderer>
</template>
