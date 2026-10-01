<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { TokenUsageEventRow } from '@/lib/shared/types'
import { fmtCompactTokenCount, fmtTokenCount } from '@/lib/run/tokenUsage'
import { displayRunTitle } from '@/lib/run/runTitle'
import { truncateText } from '@/lib/shared/format'
import { TOKEN_LEDGER_STATUS_COLORS, fmtCost, type DrillTarget } from './tokenAnalyticsShared'
import { phaseLabel, sourceLabel, statusLabel } from './tokenAnalyticsCharts'

defineProps<{
  items: TokenUsageEventRow[]
  currency?: string
  sort: 'time' | 'total' | 'cost'
  loading?: boolean
}>()
const emit = defineEmits<{
  (e: 'update:sort', v: 'time' | 'total' | 'cost'): void
  (e: 'drill', v: DrillTarget): void
}>()

const { t } = useI18n()

function fmtAt(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

function runLabel(r: TokenUsageEventRow): string {
  if (r.runTitle) return truncateText(displayRunTitle(r.runTitle).replace(/\s+/g, ' ').trim(), 36)
  return r.runId || ''
}

const SORTS = ['time', 'total', 'cost'] as const
</script>

<template>
  <div class="flex min-h-0 flex-col" data-testid="token-events-table">
    <div class="mb-2 flex items-center gap-1 text-xs text-txt3">
      <span>{{ t('pages.tokenAnalytics.events.sortBy') }}</span>
      <button
        v-for="s in SORTS"
        :key="s"
        type="button"
        class="rounded-md px-2 py-0.5"
        :class="sort === s ? 'bg-elevated font-semibold text-txt' : 'hover:text-txt2'"
        :data-testid="`token-events-sort-${s}`"
        @click="emit('update:sort', s)"
      >
        {{ t(`pages.tokenAnalytics.events.sort.${s}`) }}
      </button>
    </div>
    <div class="token-analytics-table-scroll min-h-0 overflow-auto" :class="loading ? 'opacity-60' : ''">
      <table class="w-full min-w-[960px] border-collapse text-xs">
        <thead class="sticky top-0 bg-surface">
          <tr class="text-txt3">
            <th class="border-b border-line py-1.5 pr-2 text-left font-semibold">{{ t('pages.tokenAnalytics.events.colTime') }}</th>
            <th class="border-b border-line py-1.5 pr-2 text-left font-semibold">{{ t('pages.tokenAnalytics.events.colSource') }}</th>
            <th class="border-b border-line py-1.5 pr-2 text-left font-semibold">{{ t('pages.tokenAnalytics.tables.colProject') }}</th>
            <th class="border-b border-line py-1.5 pr-2 text-left font-semibold">{{ t('pages.tokenAnalytics.events.colScope') }}</th>
            <th class="border-b border-line py-1.5 pr-2 text-left font-semibold">{{ t('pages.tokenAnalytics.tables.colModel') }}</th>
            <th class="border-b border-line py-1.5 pl-2 text-right font-semibold">{{ t('pages.tokenAnalytics.tables.colTotal') }}</th>
            <th class="border-b border-line py-1.5 pl-2 text-right font-semibold">{{ t('pages.tokenAnalytics.tables.colInput') }}</th>
            <th class="border-b border-line py-1.5 pl-2 text-right font-semibold">{{ t('pages.tokenAnalytics.tables.colOutput') }}</th>
            <th class="border-b border-line py-1.5 pl-2 text-right font-semibold">{{ t('pages.tokenAnalytics.tables.colCache') }}</th>
            <th class="border-b border-line py-1.5 pl-2 text-right font-semibold">{{ t('pages.tokenAnalytics.kpiCost') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="!items.length">
            <td colspan="10" class="py-8 text-center text-txt3">{{ t('pages.tokenAnalytics.events.empty') }}</td>
          </tr>
          <tr v-for="r in items" :key="r.id" class="hover:bg-accent-dim/40">
            <td class="whitespace-nowrap border-b border-line/60 py-1.5 pr-2 tabular-nums text-txt2">{{ fmtAt(r.at) }}</td>
            <td class="whitespace-nowrap border-b border-line/60 py-1.5 pr-2">
              <span>{{ sourceLabel(t, r.source) }}</span>
              <span v-if="r.phase" class="ml-1 text-txt3">· {{ phaseLabel(t, r.phase) }}</span>
              <span
                v-if="r.status !== 'ok'"
                class="ml-1 rounded px-1 py-px text-[10px] text-white"
                :style="{ background: TOKEN_LEDGER_STATUS_COLORS[r.status] || '#94a3b8' }"
              >{{ statusLabel(t, r.status) }}</span>
            </td>
            <td class="max-w-[160px] truncate border-b border-line/60 py-1.5 pr-2">
              <button
                v-if="r.projectId"
                type="button"
                class="truncate text-accent-2 hover:underline"
                @click="emit('drill', { dim: 'project', key: r.projectId, name: r.projectName || r.projectId })"
              >{{ r.projectName || r.projectId }}</button>
              <span v-else class="text-txt3">{{ r.projectName || t('pages.tokenAnalytics.unassigned') }}</span>
            </td>
            <td class="max-w-[260px] truncate border-b border-line/60 py-1.5 pr-2">
              <button
                v-if="r.runId"
                type="button"
                class="truncate text-accent-2 hover:underline"
                :title="r.runTitle || r.runId"
                @click="emit('drill', { dim: 'run', key: r.runId, name: runLabel(r) })"
              >{{ runLabel(r) }}</button>
              <span v-else-if="r.threadId" class="text-txt3">{{ t('pages.tokenAnalytics.events.thread') }}</span>
              <span v-if="r.nodeType" class="ml-1 text-txt3">· {{ r.nodeType }}</span>
            </td>
            <td class="max-w-[160px] truncate border-b border-line/60 py-1.5 pr-2">
              <button
                v-if="r.modelKey"
                type="button"
                class="truncate hover:text-accent-2 hover:underline"
                @click="emit('drill', { dim: 'model', key: r.modelKey, name: r.modelKey })"
              >{{ r.modelKey }}</button>
              <span v-else class="text-txt3">{{ t('pages.tokenAnalytics.unknownModel') }}</span>
            </td>
            <td class="whitespace-nowrap border-b border-line/60 py-1.5 pl-2 text-right font-semibold tabular-nums" :title="fmtTokenCount(r.total)">{{ fmtCompactTokenCount(r.total) }}</td>
            <td class="whitespace-nowrap border-b border-line/60 py-1.5 pl-2 text-right tabular-nums">{{ fmtCompactTokenCount(r.inputTokens) }}</td>
            <td class="whitespace-nowrap border-b border-line/60 py-1.5 pl-2 text-right tabular-nums">{{ fmtCompactTokenCount(r.outputTokens) }}</td>
            <td class="whitespace-nowrap border-b border-line/60 py-1.5 pl-2 text-right tabular-nums">
              {{ fmtCompactTokenCount(r.cacheReadTokens) }} / {{ fmtCompactTokenCount(r.cacheWriteTokens) }}
            </td>
            <td class="whitespace-nowrap border-b border-line/60 py-1.5 pl-2 text-right tabular-nums">
              <span v-if="r.priced">{{ fmtCost(r.cost, currency) }}</span>
              <span v-else class="text-txt3">—</span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
