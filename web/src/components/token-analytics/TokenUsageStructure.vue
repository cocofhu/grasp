<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { GlobalTokenStatsTreeNode } from '@/lib/shared/types'
import { fmtCompactTokenCount, fmtTokenCount } from '@/lib/run/tokenUsage'
import { sourceLabel } from './tokenAnalyticsCharts'
import type { DrillTarget } from './tokenAnalyticsShared'

const props = defineProps<{
  tree: GlobalTokenStatsTreeNode[]
  total: number
}>()
const emit = defineEmits<{ (e: 'drill', path: DrillTarget[]): void }>()
const { t } = useI18n()
const expanded = ref(false)
const INITIAL_ROWS = 5

function drillPath(node: GlobalTokenStatsTreeNode, parents: DrillTarget[] = []): DrillTarget[] {
  let step: DrillTarget | undefined
  if (node.kind === 'workflow' && node.key && !node.key.startsWith('_')) {
    step = { dim: 'workflow', key: node.key, name: node.name }
  } else if (node.kind === 'pm' || node.kind === 'studio') {
    step = { dim: 'source', key: node.kind, name: sourceLabel(t, node.kind) }
  } else if (node.kind === 'nodeType' && node.key && node.key !== 'unknown' && !node.key.startsWith('_')) {
    step = { dim: 'nodeType', key: node.key, name: node.name }
  }
  return step ? [...parents, step] : parents
}

function positiveNodes(nodes: GlobalTokenStatsTreeNode[]) {
  return nodes.filter((node) => Number.isFinite(node.value) && node.value > 0).sort((a, b) => b.value - a.value)
}

const rows = computed(() => positiveNodes(props.tree).map((node) => {
  const drill = drillPath(node)
  return {
    ...node,
    name: node.kind === 'pm' || node.kind === 'studio' ? sourceLabel(t, node.kind) : node.name,
    drill,
    children: positiveNodes(node.children || []).map((child) => ({ ...child, drill: drillPath(child, drill) })),
  }
}))
const visibleRows = computed(() => expanded.value ? rows.value : rows.value.slice(0, INITIAL_ROWS))
const denominator = computed(() => props.total > 0 ? props.total : rows.value.reduce((sum, row) => sum + row.value, 0))

function share(value: number): number {
  return denominator.value > 0 ? Math.min(100, value / denominator.value * 100) : 0
}

function shareLabel(value: number): string {
  const percent = share(value)
  return percent > 0 && percent < 0.1 ? '<0.1%' : `${percent.toFixed(1)}%`
}

function drill(path: DrillTarget[]) {
  if (path.length) emit('drill', path)
}
</script>

<template>
  <div class="mt-5 min-w-0" data-testid="token-analytics-structure">
    <ol class="m-0 grid list-none gap-5 p-0">
      <li v-for="row in visibleRows" :key="`${row.kind}:${row.key}`" class="min-w-0" data-testid="token-structure-row" :data-key="row.key">
        <component
          :is="row.drill.length ? 'button' : 'div'"
          :type="row.drill.length ? 'button' : undefined"
          class="group grid min-h-7 w-full grid-cols-[minmax(0,1fr)_auto] items-start gap-x-4 rounded-md text-left outline-none"
          :class="row.drill.length ? 'focus-visible:ring-2 focus-visible:ring-accent/40 focus-visible:ring-offset-4 focus-visible:ring-offset-surface' : ''"
          data-testid="token-structure-source"
          :data-key="row.key"
          @click="drill(row.drill)"
        >
          <span class="flex min-w-0 flex-wrap items-baseline gap-x-2 gap-y-0.5">
            <span class="min-w-0 max-w-full text-[13px] font-medium leading-5 text-txt [overflow-wrap:anywhere]" :class="row.drill.length ? 'group-hover:text-accent-2' : ''">{{ row.name }}</span>
            <span v-if="row.kind === 'workflow'" class="text-[11px] leading-5 text-txt3">{{ sourceLabel(t, 'workflow') }}</span>
          </span>
          <span class="flex shrink-0 items-baseline gap-3 tabular-nums sm:gap-5">
            <span class="text-[13px] font-semibold leading-5 text-txt" data-testid="token-structure-total" :title="fmtTokenCount(row.value)">{{ fmtCompactTokenCount(row.value) }}</span>
            <span class="w-12 text-right text-xs leading-5 text-txt3" data-testid="token-structure-share" :aria-label="`${t('pages.tokenAnalytics.structure.shareLabel')} ${shareLabel(row.value)}`">{{ shareLabel(row.value) }}</span>
          </span>
        </component>
        <div class="mt-2 h-1 overflow-hidden rounded-full bg-elevated" aria-hidden="true">
          <div class="h-full min-w-[3px] rounded-full bg-accent/65" :style="{ width: `${share(row.value)}%` }" />
        </div>
        <div v-if="row.children.length" class="mt-2 flex flex-wrap items-center gap-x-1.5 gap-y-1">
          <component
            :is="child.drill.length ? 'button' : 'span'"
            v-for="child in row.children"
            :key="`${child.kind}:${child.key}`"
            :type="child.drill.length ? 'button' : undefined"
            class="inline-flex min-h-7 max-w-full items-baseline gap-1.5 rounded-md px-1.5 py-0.5 text-[11px] leading-4 outline-none"
            :class="child.drill.length ? 'text-txt2 transition-colors hover:bg-elevated hover:text-accent-2 focus-visible:ring-2 focus-visible:ring-accent/40 motion-reduce:transition-none' : 'text-txt3'"
            data-testid="token-structure-node"
            :data-key="child.key"
            @click="drill(child.drill)"
          >
            <span class="min-w-0 [overflow-wrap:anywhere]">{{ child.name }}</span>
            <span class="shrink-0 tabular-nums text-txt3" :title="fmtTokenCount(child.value)">{{ fmtCompactTokenCount(child.value) }}</span>
          </component>
        </div>
      </li>
    </ol>
    <button
      v-if="rows.length > INITIAL_ROWS"
      type="button"
      class="mt-4 rounded-md px-1.5 py-1 text-xs text-txt2 outline-none transition-colors hover:bg-elevated hover:text-txt focus-visible:ring-2 focus-visible:ring-accent/40 motion-reduce:transition-none"
      :aria-expanded="expanded"
      data-testid="token-structure-expand"
      @click="expanded = !expanded"
    >{{ expanded ? t('pages.tokenAnalytics.structure.showLess') : t('pages.tokenAnalytics.structure.showMore', { count: rows.length }) }}</button>
  </div>
</template>
