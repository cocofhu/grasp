<script setup lang="ts">
/**
 * Visible Token detail tip (Demo .detail-tip aligned).
 * Renders only when totalTokens is a number (incl. 0); null/undefined → nothing.
 * Prefer sourceRows (workflow/pm/total) when provided; else fall back to usage
 * four-component breakdown (progressive; never invent rows).
 */
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { fmtTokenCount } from '@/lib/run/tokenUsage'
import type { TokenUsage } from '@/lib/shared/types'

const props = defineProps<{
  totalTokens: number
  usage?: TokenUsage | null
  /** Source split for the workspace Token card (workflow / pm / total). */
  workflowTokens?: number | null
  pmTokens?: number | null
  /** Optional id for aria-describedby on the hover host. */
  tipId?: string
}>()

const { t } = useI18n()

const exact = computed(() => fmtTokenCount(props.totalTokens))

const hasSourceSplit = computed(
  () => props.workflowTokens != null || props.pmTokens != null,
)

const hasBreakdown = computed(() => hasSourceSplit.value || props.usage != null)

const rows = computed(() => {
  if (hasSourceSplit.value) {
    return [
      {
        key: 'workflow',
        label: t('pages.workspace.tokenTipWorkflow'),
        value: fmtTokenCount(props.workflowTokens ?? 0),
      },
      {
        key: 'pm',
        label: t('pages.workspace.tokenTipPm'),
        value: fmtTokenCount(props.pmTokens ?? 0),
        cache: true,
      },
      {
        key: 'total',
        label: t('pages.workspace.tokenTipTotal'),
        value: exact.value,
        total: true,
      },
    ]
  }
  if (!props.usage) return []
  const u = props.usage
  return [
    { key: 'input', label: t('pages.executionTimeline.partInput'), value: fmtTokenCount(u.inputTokens || 0) },
    { key: 'output', label: t('pages.executionTimeline.partOutput'), value: fmtTokenCount(u.outputTokens || 0) },
    {
      key: 'cacheRead',
      label: t('pages.executionTimeline.partCacheRead'),
      value: fmtTokenCount(u.cacheReadTokens || 0),
      cache: true,
    },
    {
      key: 'cacheWrite',
      label: t('pages.executionTimeline.partCacheWrite'),
      value: fmtTokenCount(u.cacheWriteTokens || 0),
      cache: true,
    },
    {
      key: 'total',
      label: t('pages.workspace.tokenTipTotal'),
      value: exact.value,
      total: true,
    },
  ]
})
</script>

<template>
  <div
    :id="tipId"
    role="tooltip"
    data-testid="token-detail-tip"
    class="pointer-events-none absolute top-[calc(100%+10px)] z-20 w-[min(260px,calc(100vw-24px))] rounded-[10px] bg-[#111827] px-3.5 py-3 text-left text-[#f9fafb] opacity-0 shadow-[0_12px_32px_rgba(15,23,42,0.28)] invisible translate-y-1 group-hover:opacity-100 group-hover:visible group-hover:translate-y-0 group-focus-within:opacity-100 group-focus-within:visible group-focus-within:translate-y-0 right-0 max-[720px]:right-auto max-[720px]:left-0 token-usage-tip"
  >
    <div class="mb-2 text-[11px] font-semibold tracking-wide text-[#9ca3af]">
      {{ t('pages.workspace.tokenTipTitle') }}
    </div>
    <div class="mb-2.5 font-mono text-lg font-bold tracking-tight tabular-nums" data-testid="token-detail-tip-exact">
      {{ exact }}
      <span class="ml-1.5 text-[11px] font-medium text-[#9ca3af]">tokens</span>
    </div>
    <div v-if="hasBreakdown" class="grid gap-1.5" data-testid="token-detail-tip-breakdown">
      <div
        v-for="row in rows"
        :key="row.key"
        class="flex items-center justify-between gap-3 rounded-[6px] px-2 py-1 text-xs"
        :class="
          row.total
            ? 'bg-[rgba(167,139,250,0.18)]'
            : 'bg-white/5'
        "
        :data-tip-row="row.key"
      >
        <span
          :class="
            row.total
              ? 'font-bold text-[#ddd6fe]'
              : row.cache
                ? 'text-[#93c5fd]'
                : 'text-[#d1d5db]'
          "
        >{{ row.label }}</span>
        <span
          class="font-mono font-semibold tabular-nums"
          :class="
            row.total
              ? 'font-bold text-[#ddd6fe]'
              : row.cache
                ? 'text-[#93c5fd]'
                : ''
          "
        >{{ row.value }}</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.token-usage-tip {
  transition:
    opacity var(--dur-ui) var(--ease-out-expo),
    visibility var(--dur-ui) var(--ease-out-expo),
    transform var(--dur-ui) var(--ease-out-expo);
}
@media (prefers-reduced-motion: reduce) {
  .token-usage-tip {
    transition: none;
    transform: none;
  }
}
</style>
