<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  effectiveModelUsageRows,
  fmtCompactTokenCount,
  fmtTokenCount,
  mergeTokenUsageByModel,
  shouldShowUnknownVisual,
  TOKEN_USAGE_SOURCE_BRIDGE,
  tokenUsageTotal,
  unknownDisplayName,
} from '@/lib/run/tokenUsage'
import type { TokenUsage, TokenUsageByModel } from '@/lib/shared/types'
import UnknownModelBadge from '@/components/ui/UnknownModelBadge.vue'

/** Demo-aligned segment colors for composition bar + quad dots. */
const PART_COLORS = {
  input: '#60A5FA',
  output: '#34D399',
  cacheRead: '#7B61FF',
  cacheWrite: '#FBBF24',
} as const

const props = defineProps<{
  /** Merged run total (optional; derived from by-model when omitted). */
  usage?: TokenUsage | null
  usageByModel?: TokenUsageByModel | null
  /** Merge multiple process-level by-model maps (StateRun rows). */
  parts?: Array<{ usage?: TokenUsage | null; usageByModel?: TokenUsageByModel | null }>
  /** Workspace-level alias for the unknown token bucket. */
  unknownModelDisplayName?: string | null
}>()

const { t } = useI18n()

const mergedByModel = computed(() => {
  if (props.parts?.length) {
    return mergeTokenUsageByModel(...props.parts.map((p) => p.usageByModel))
  }
  return props.usageByModel ?? null
})

const mergedUsage = computed(() => {
  if (props.usage != null) return props.usage
  if (props.parts?.length) {
    let has = false
    const u: TokenUsage = { inputTokens: 0, outputTokens: 0, cacheReadTokens: 0, cacheWriteTokens: 0 }
    for (const p of props.parts) {
      if (p.usage == null) continue
      has = true
      u.inputTokens += p.usage.inputTokens || 0
      u.outputTokens += p.usage.outputTokens || 0
      u.cacheReadTokens += p.usage.cacheReadTokens || 0
      u.cacheWriteTokens += p.usage.cacheWriteTokens || 0
    }
    return has ? u : null
  }
  return null
})

const rows = computed(() => effectiveModelUsageRows(mergedUsage.value, mergedByModel.value))
const total = computed(() => (mergedUsage.value != null ? tokenUsageTotal(mergedUsage.value) : null))
const rowsSum = computed(() => rows.value.reduce((s, r) => s + r.total, 0))

function sourceLabel(source: string, filled: boolean): string {
  if (filled || source === TOKEN_USAGE_SOURCE_BRIDGE) return t('pages.tokenByModel.sourceBridge')
  if (source === 'unknown') return t('pages.tokenByModel.sourceUnknown')
  return t('pages.tokenByModel.sourceUpstream')
}

function modelLabel(modelKey: string, unknown: boolean): string {
  if (!unknown) return modelKey
  return unknownDisplayName(modelKey, props.unknownModelDisplayName)
}

function showUnknownVisual(modelKey: string, unknown: boolean): boolean {
  return shouldShowUnknownVisual(unknown, modelLabel(modelKey, unknown))
}

function partPct(value: number, rowTotal: number): number {
  if (rowTotal <= 0 || value <= 0) return 0
  return (value / rowTotal) * 100
}

function barTitle(row: { inputTokens: number; outputTokens: number; cacheReadTokens: number; cacheWriteTokens: number }): string {
  return `input ${fmtTokenCount(row.inputTokens)} / output ${fmtTokenCount(row.outputTokens)} / cacheRead ${fmtTokenCount(row.cacheReadTokens)} / cacheWrite ${fmtTokenCount(row.cacheWriteTokens)}`
}
</script>

<template>
  <div
    v-if="total != null"
    data-testid="run-token-by-model"
    class="rounded-lg mt-3 overflow-x-clip border border-line bg-surface"
  >
    <div class="flex items-baseline justify-between gap-2 border-b border-line px-2.5 py-2">
      <h4 class="m-0 text-[13px] font-semibold text-txt">{{ t('pages.tokenByModel.modelTableTitle') }}</h4>
      <span class="shrink-0 font-mono text-[11px] tabular-nums text-txt3">
        Σ {{ fmtTokenCount(rowsSum) }}
        <span v-if="total != null && rowsSum === total" class="ml-1 text-ok">= {{ t('pages.executionStats.totalTokens') }}</span>
      </span>
    </div>

    <div v-if="!rows.length" class="px-2.5 py-4 text-center text-[12.5px] text-txt3">
      {{ t('pages.executionStats.empty') }}
    </div>

    <div v-else class="py-1">
      <div
        v-for="row in rows"
        :key="row.modelKey"
        class="border-b border-line px-2.5 py-2.5 last:border-b-0"
        :data-model="row.modelKey"
        :data-filled="row.filled ? '1' : '0'"
        :data-unknown="row.unknown ? '1' : '0'"
      >
        <!-- 首行：名 + 来源徽章 + 右侧小计 -->
        <div class="flex min-w-0 items-center gap-1.5">
          <span
            class="min-w-0 truncate text-[12.5px] font-semibold"
            :class="showUnknownVisual(row.modelKey, row.unknown) ? 'text-txt3' : row.filled ? 'text-ok' : 'text-txt'"
            :title="modelLabel(row.modelKey, row.unknown)"
          >{{ modelLabel(row.modelKey, row.unknown) }}</span>
          <UnknownModelBadge v-if="showUnknownVisual(row.modelKey, row.unknown)" />
          <span
            class="rounded-lg inline-flex shrink-0 items-center border px-1.5 py-px text-[10.5px] leading-none"
            :class="
              row.unknown && !row.filled
                ? 'border-warn/35 bg-warn/8 text-warn'
                : 'border-line bg-elevated text-txt2'
            "
          >{{ sourceLabel(row.source, row.filled) }}</span>
          <span
            class="ml-auto flex shrink-0 items-baseline gap-1 pl-2"
            :title="`${t('pages.tokenByModel.colSubtotal')} ${fmtTokenCount(row.total)} token`"
          >
            <b class="font-mono text-[13.5px] font-semibold tabular-nums text-txt">{{ fmtCompactTokenCount(row.total) }}</b>
            <span class="text-[10.5px] text-txt3">{{ t('pages.executionStats.tokenUnit') }}</span>
          </span>
        </div>

        <!-- 无 % 数字的四分量组成占比条 -->
        <div
          class="mt-1.5 mb-2 flex h-1 overflow-hidden bg-elevated"
          :title="barTitle(row)"
        >
          <i
            v-if="partPct(row.inputTokens, row.total) > 0"
            class="block h-full"
            :style="{ width: `${partPct(row.inputTokens, row.total)}%`, background: PART_COLORS.input }"
          />
          <i
            v-if="partPct(row.outputTokens, row.total) > 0"
            class="block h-full"
            :style="{ width: `${partPct(row.outputTokens, row.total)}%`, background: PART_COLORS.output }"
          />
          <i
            v-if="partPct(row.cacheReadTokens, row.total) > 0"
            class="block h-full"
            :style="{ width: `${partPct(row.cacheReadTokens, row.total)}%`, background: PART_COLORS.cacheRead }"
          />
          <i
            v-if="partPct(row.cacheWriteTokens, row.total) > 0"
            class="block h-full"
            :style="{ width: `${partPct(row.cacheWriteTokens, row.total)}%`, background: PART_COLORS.cacheWrite }"
          />
        </div>

        <!-- 英文全称四分量 auto-fit 网格（≤320px 自然降为 2/1 列） -->
        <div
          class="rounded-lg grid gap-px border border-line bg-line"
          style="grid-template-columns: repeat(auto-fit, minmax(96px, 1fr))"
        >
          <div class="min-w-0 bg-base px-1.5 py-1">
            <div class="flex items-center gap-1 text-[10.5px] text-txt3">
              <em class="inline-block h-1.5 w-1.5 shrink-0 not-italic" :style="{ background: PART_COLORS.input }" />
              input
            </div>
            <div
              class="mt-px truncate font-mono text-[12.5px] tabular-nums"
              :class="row.inputTokens ? 'text-txt' : 'text-txt3'"
              :title="`${fmtTokenCount(row.inputTokens)} token`"
            >{{ fmtCompactTokenCount(row.inputTokens) }}</div>
          </div>
          <div class="min-w-0 bg-base px-1.5 py-1">
            <div class="flex items-center gap-1 text-[10.5px] text-txt3">
              <em class="inline-block h-1.5 w-1.5 shrink-0 not-italic" :style="{ background: PART_COLORS.output }" />
              output
            </div>
            <div
              class="mt-px truncate font-mono text-[12.5px] tabular-nums"
              :class="row.outputTokens ? 'text-txt' : 'text-txt3'"
              :title="`${fmtTokenCount(row.outputTokens)} token`"
            >{{ fmtCompactTokenCount(row.outputTokens) }}</div>
          </div>
          <div class="min-w-0 bg-base px-1.5 py-1">
            <div class="flex items-center gap-1 text-[10.5px] text-txt3">
              <em class="inline-block h-1.5 w-1.5 shrink-0 not-italic" :style="{ background: PART_COLORS.cacheRead }" />
              cacheRead
            </div>
            <div
              class="mt-px truncate font-mono text-[12.5px] tabular-nums"
              :class="row.cacheReadTokens ? 'text-txt' : 'text-txt3'"
              :title="`${fmtTokenCount(row.cacheReadTokens)} token`"
            >{{ fmtCompactTokenCount(row.cacheReadTokens) }}</div>
          </div>
          <div class="min-w-0 bg-base px-1.5 py-1">
            <div class="flex items-center gap-1 text-[10.5px] text-txt3">
              <em class="inline-block h-1.5 w-1.5 shrink-0 not-italic" :style="{ background: PART_COLORS.cacheWrite }" />
              cacheWrite
            </div>
            <div
              class="mt-px truncate font-mono text-[12.5px] tabular-nums"
              :class="row.cacheWriteTokens ? 'text-txt' : 'text-txt3'"
              :title="`${fmtTokenCount(row.cacheWriteTokens)} token`"
            >{{ fmtCompactTokenCount(row.cacheWriteTokens) }}</div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
