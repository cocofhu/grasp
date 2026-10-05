<script setup lang="ts">
import { computed, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import type { OnboardingWorkflowPreview } from '@/lib/pm/onboardingWizard'

const props = defineProps<{ preview: OnboardingWorkflowPreview }>()

const { t } = useI18n()
const uid = useId()

const NODE_W = 104
const NODE_H = 52
const GAP = 32
const PAD_X = 6
const NODE_Y = 22
const LOOP_DEPTH = 46

const layout = computed(() =>
  props.preview.nodes.map((n, i) => ({ ...n, x: PAD_X + i * (NODE_W + GAP), y: NODE_Y })),
)
const width = computed(() => PAD_X * 2 + props.preview.nodes.length * NODE_W + (props.preview.nodes.length - 1) * GAP)
const hasLoop = computed(() => props.preview.edges.some((e) => e.handle === 'fail'))
const height = computed(() => NODE_Y + NODE_H + (hasLoop.value ? LOOP_DEPTH + 10 : 12))

function nodeAt(id: string) {
  return layout.value.find((n) => n.id === id)
}

type DrawnEdge = { key: string; handle?: 'pass' | 'fail'; d: string; lx: number; ly: number }

const edges = computed<DrawnEdge[]>(() =>
  props.preview.edges.flatMap((e): DrawnEdge[] => {
    const a = nodeAt(e.from)
    const b = nodeAt(e.to)
    if (!a || !b) return []
    if (e.handle === 'fail') {
      const x1 = a.x + NODE_W / 2
      const x2 = b.x + NODE_W / 2
      const y = a.y + NODE_H
      return [
        {
          key: `${e.from}-${e.to}-fail`,
          handle: e.handle,
          d: `M ${x1} ${y} C ${x1} ${y + LOOP_DEPTH}, ${x2} ${y + LOOP_DEPTH}, ${x2} ${y + 4}`,
          lx: (x1 + x2) / 2,
          ly: y + LOOP_DEPTH * 0.75 + 13,
        },
      ]
    }
    const x1 = a.x + NODE_W
    const x2 = b.x - 3
    const y = a.y + NODE_H / 2
    return [{ key: `${e.from}-${e.to}`, handle: e.handle, d: `M ${x1} ${y} L ${x2} ${y}`, lx: (x1 + x2) / 2, ly: y - 8 }]
  }),
)

function markerFor(handle?: string) {
  if (handle === 'fail') return `url(#${uid}-err)`
  if (handle === 'pass') return `url(#${uid}-ok)`
  return `url(#${uid}-line)`
}

function nodeTitle(n: (typeof layout.value)[number]) {
  if (n.kind === 'input') return t('pages.onboarding.workflow.input')
  if (n.kind === 'output') return t('pages.onboarding.workflow.output')
  return n.name || ''
}

function nodeSub(n: (typeof layout.value)[number]) {
  if (n.kind !== 'agent') return n.kind
  const title = n.templateId ? t(`pages.onboarding.team.templates.${n.templateId}.title`) : ''
  return title && title !== n.name ? title : 'agent'
}
</script>

<template>
  <div class="overflow-x-auto" data-testid="onboarding-workflow-preview">
    <div class="relative mx-auto" :style="{ width: width + 'px', height: height + 'px' }">
      <svg class="absolute inset-0" :width="width" :height="height" aria-hidden="true">
        <defs>
          <marker :id="`${uid}-line`" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="7" markerHeight="7" orient="auto">
            <path d="M0 0 L8 4 L0 8 z" class="fill-line-strong" />
          </marker>
          <marker :id="`${uid}-ok`" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="7" markerHeight="7" orient="auto">
            <path d="M0 0 L8 4 L0 8 z" class="fill-ok" />
          </marker>
          <marker :id="`${uid}-err`" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="7" markerHeight="7" orient="auto">
            <path d="M0 0 L8 4 L0 8 z" class="fill-err" />
          </marker>
        </defs>
        <g v-for="e in edges" :key="e.key" :data-testid="`onboarding-preview-edge-${e.key}`">
          <path
            :d="e.d"
            fill="none"
            stroke-width="1.5"
            :stroke-dasharray="e.handle === 'fail' ? '4 3' : undefined"
            :class="e.handle === 'fail' ? 'stroke-err' : e.handle === 'pass' ? 'stroke-ok' : 'stroke-line-strong'"
            :marker-end="markerFor(e.handle)"
          />
          <text
            v-if="e.handle"
            :x="e.lx"
            :y="e.ly"
            text-anchor="middle"
            class="text-[10px]"
            :class="e.handle === 'fail' ? 'fill-err' : 'fill-ok'"
          >
            {{ e.handle === 'fail' ? t('pages.onboarding.workflow.fail') : t('pages.onboarding.workflow.pass') }}
          </text>
        </g>
      </svg>
      <div
        v-for="n in layout"
        :key="n.id"
        class="rounded-lg absolute flex flex-col justify-center border px-2.5"
        :class="n.kind === 'agent' ? 'border-accent/50 bg-surface shadow-card' : 'border-line bg-elevated'"
        :style="{ left: n.x + 'px', top: n.y + 'px', width: NODE_W + 'px', height: NODE_H + 'px' }"
        :data-testid="`onboarding-preview-node-${n.id}`"
      >
        <strong class="block truncate text-[12px] font-medium text-txt" :title="nodeTitle(n)">{{ nodeTitle(n) }}</strong>
        <span class="mt-0.5 block truncate text-[10.5px] text-txt3">{{ nodeSub(n) }}</span>
      </div>
    </div>
  </div>
</template>
