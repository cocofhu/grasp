<script setup lang="ts">
import StatsPieChart from './StatsPieChart.vue'
import Icon from '../ui/Icon.vue'
import TruncatedTextTooltip from '../ui/TruncatedTextTooltip.vue'
import TokenUsageByModelTable from './TokenUsageByModelTable.vue'

import { useExecutionStats } from '@/lib/run/useExecutionStats'
import type { ExecutionStatsProps, ExecutionStatsEmit } from '@/lib/run/useExecutionStats'

const props = withDefaults(defineProps<ExecutionStatsProps>(), {
  statsTab: undefined,
  unknownModelDisplayName: null,
})
const emit = defineEmits<ExecutionStatsEmit>()

const {
  t,
  localTab,
  tabControlled,
  statsTab,
  singleDim,
  multiDim,
  labelFn,
  typeLabel,
  localizeItemLabel,
  singleSummary,
  singleItems,
  singleBottleneck,
  processAtoms,
  mergedUsage,
  modelUsageParts,
  tipUsageParts,
  durationTipText,
  compactTokensMain,
  compactRateMain,
  multiRateMain,
  tokenRateTipText,
  openTip,
  tipUid,
  tipDomId,
  showTip,
  hideTip,
  candidates,
  selectedIds,
  detailCache,
  detailErrors,
  candidatesLoading,
  detailsLoading,
  candidatesError,
  selectionInitialized,
  pickerOpen,
  loadGen,
  pad2,
  localYmd,
  startedAtYmd,
  fmtStartHm,
  fmtCandidateTime,
  fmtOlderDateLabel,
  dayGroupLabel,
  wallSourceForId,
  candidateWallSec,
  durationWithPrefix,
  isCurrentRun,
  defaultSelectedIds,
  loadCandidates,
  toggleRun,
  togglePicker,
  selectedCandidates,
  selectedRunsTitle,
  candidateGroups,
  multiInputs,
  multiSummary,
  multiAvgWallSec,
  multiItems,
  pendingDetailCount,
  multiReady,
  selectedCountDisplay,
  pieFootnote,
  dimOptionsSingle,
  dimOptionsMulti,
  singleNote,
  pieCenterSub,
  maxMultiAvg,
  fmtTokensOrDash,
  singleTokenHint,
  singleRateHint,
  multiSumHint,
  multiAvgHint,
  multiRateHint,
  fmtCompactDuration,
  fmtDuration,
  fmtMultiAvgDuration,
  fmtTokenCount,
  fmtCompactTokenCount,
} = useExecutionStats(props, emit)
</script>

<style scoped>
.stats-panel {
  --stats-token: #0b6e99;
  --stats-time: #b45309;
  /* Rich tip highlight pair: dark defaults, light overrides below (Demo「修复后」). */
  --stats-tip-hl-bg: rgba(139, 92, 246, 0.28);
  --stats-tip-hl-txt: #e9d5ff;
  --stats-tip-line: rgba(248, 250, 252, 0.14);
}
html.light .stats-panel {
  --stats-tip-hl-bg: #ede9fe;
  --stats-tip-hl-txt: #4c1d95;
  --stats-tip-line: rgba(17, 24, 39, 0.12);
}
.stats-kpi-time {
  color: var(--stats-time);
}
.stats-kpi-token {
  color: var(--stats-token);
}
.stats-tok-val {
  color: var(--stats-token);
}
.stats-tok-na {
  color: inherit;
}
.stats-kpi-value-wrap {
  position: relative;
  display: inline-flex;
  max-width: 100%;
  align-self: flex-start;
}
.stats-kpi-value {
  cursor: help;
  border-radius: 2px;
  border-bottom: 1px dotted color-mix(in srgb, currentColor 35%, transparent);
  outline: none;
}
.stats-kpi-value:hover,
.stats-kpi-value:focus-visible {
  background: color-mix(in srgb, currentColor 8%, transparent);
}
.stats-kpi-tip {
  position: absolute;
  top: calc(100% + 6px);
  z-index: 30;
  max-width: min(280px, 70vw);
  border: 1px solid rgb(var(--c-line-strong, 203 213 225));
  background: rgb(var(--c-overlay, 15 23 42));
  color: rgb(var(--c-txt2, 226 232 240));
  padding: 6px 10px;
  font-size: 11px;
  font-weight: 500;
  line-height: 1.4;
  white-space: nowrap;
  box-shadow: 0 8px 20px rgba(0, 0, 0, 0.22);
  pointer-events: none;
}
.stats-kpi-tip-left {
  left: 0;
}
.stats-kpi-tip-center {
  left: 50%;
  transform: translateX(-50%);
}
.stats-kpi-tip-right {
  right: 0;
  left: auto;
}
.stats-kpi-tip-rich {
  white-space: normal;
  width: 248px;
  max-width: min(248px, 70vw);
  padding: 10px 11px 9px;
  text-align: left;
}
.stats-kpi-tip-total {
  margin-bottom: 8px;
  padding-bottom: 7px;
  border-bottom: 1px solid var(--stats-tip-line);
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
  font-weight: 700;
  color: rgb(var(--c-txt));
}
.stats-kpi-tip-parts {
  display: grid;
  gap: 5px;
}
.stats-kpi-tip-part {
  display: grid;
  grid-template-columns: 52px 1fr auto;
  gap: 8px;
  align-items: center;
  font-size: 11.5px;
}
.stats-kpi-tip-part-k {
  /* Prefer --c-txt2 over --c-txt3: latter is too light on overlay in light theme. */
  color: rgb(var(--c-txt2));
}
.stats-kpi-tip-part-k-hl {
  color: var(--stats-tip-hl-txt);
  font-weight: 650;
}
.stats-kpi-tip-part-n {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-weight: 650;
  color: rgb(var(--c-txt));
  text-align: right;
}
.stats-kpi-tip-part-c {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 10.5px;
  color: rgb(var(--c-txt2));
  min-width: 42px;
}
.stats-kpi-tip-part-hl {
  margin: 0 -6px;
  padding: 4px 6px;
  border-radius: 5px;
  background: var(--stats-tip-hl-bg);
}
.stats-kpi-tip-part-hl .stats-kpi-tip-part-n,
.stats-kpi-tip-part-hl .stats-kpi-tip-part-c {
  color: var(--stats-tip-hl-txt);
  font-weight: 650;
}
</style>

<template>
  <div
    data-testid="execution-stats-panel"
    class="stats-panel flex h-full min-h-0 min-w-0 w-full max-w-full flex-col bg-base"
  >
    <!-- Tab bar only when parent does not control statsTab (standalone / tests). -->
    <div
      v-if="!tabControlled"
      class="flex shrink-0 border-b border-line bg-surface px-3 sm:px-4"
    >
      <button
        type="button"
        class="border-b-2 px-3 py-2.5 text-[13px] font-medium transition-colors"
        :class="
          statsTab === 'single'
            ? 'border-accent-2 text-accent-2'
            : 'border-transparent text-txt3 hover:text-txt2'
        "
        @click="statsTab = 'single'"
      >
        {{ t('pages.executionStats.tabSingle') }}
      </button>
      <button
        type="button"
        class="border-b-2 px-3 py-2.5 text-[13px] font-medium transition-colors"
        :class="
          statsTab === 'multi'
            ? 'border-accent-2 text-accent-2'
            : 'border-transparent text-txt3 hover:text-txt2'
        "
        @click="statsTab = 'multi'"
      >
        {{ t('pages.executionStats.tabMulti') }}
      </button>
    </div>

    <div
      data-testid="execution-stats-scroll"
      class="scroll-area safe-area-bottom min-h-0 min-w-0 w-full max-w-full flex-1 overflow-x-clip overflow-y-auto px-4 py-4 sm:px-5"
    >
      <!-- Single run -->
      <template v-if="statsTab === 'single'">
        <div class="mb-3.5 grid grid-cols-1 gap-2 md:grid-cols-3 xl:grid-cols-5">
          <div class="rounded-lg border border-line bg-surface px-3 py-2.5" data-testid="stats-kpi-wall">
            <div class="mb-1 text-[11px] text-txt3">{{ t('pages.executionStats.wallClock') }}</div>
            <div class="stats-kpi-value-wrap">
              <div
                class="stats-kpi-value stats-kpi-time text-[16px] font-semibold tabular-nums tracking-tight"
                tabindex="0"
                data-testid="stats-kpi-wall-value"
                :aria-describedby="openTip === 'wall' ? tipDomId('wall') : undefined"
                @mouseenter="showTip('wall')"
                @mouseleave="hideTip('wall')"
                @focus="showTip('wall')"
                @blur="hideTip('wall')"
              >
                {{ fmtCompactDuration(singleSummary.wallSec) }}
              </div>
              <span
                v-if="openTip === 'wall'"
                :id="tipDomId('wall')"
                role="tooltip"
                class="stats-kpi-tip stats-kpi-tip-left"
                data-testid="stats-kpi-wall-tip"
              >
                {{ durationTipText(singleSummary.wallSec) }}
              </span>
            </div>
            <div class="mt-0.5 text-[10px] text-txt3">{{ t('pages.executionStats.wallHint') }}</div>
          </div>
          <div class="rounded-lg border border-line bg-surface px-3 py-2.5" data-testid="stats-kpi-node-sum">
            <div class="mb-1 text-[11px] text-txt3">{{ t('pages.executionStats.nodeSum') }}</div>
            <div class="stats-kpi-value-wrap">
              <div
                class="stats-kpi-value stats-kpi-time text-[16px] font-semibold tabular-nums tracking-tight"
                tabindex="0"
                data-testid="stats-kpi-node-sum-value"
                :aria-describedby="openTip === 'nodeSum' ? tipDomId('nodeSum') : undefined"
                @mouseenter="showTip('nodeSum')"
                @mouseleave="hideTip('nodeSum')"
                @focus="showTip('nodeSum')"
                @blur="hideTip('nodeSum')"
              >
                {{ fmtCompactDuration(singleSummary.nodeSumSec) }}
              </div>
              <span
                v-if="openTip === 'nodeSum'"
                :id="tipDomId('nodeSum')"
                role="tooltip"
                class="stats-kpi-tip stats-kpi-tip-left"
                data-testid="stats-kpi-node-sum-tip"
              >
                {{ durationTipText(singleSummary.nodeSumSec) }}
              </span>
            </div>
            <div class="mt-0.5 text-[10px] text-txt3">{{ t('pages.executionStats.nodeSumHint') }}</div>
          </div>
          <div class="rounded-lg border border-line bg-surface px-3 py-2.5" data-testid="stats-kpi-gap">
            <div class="mb-1 text-[11px] text-txt3">{{ t('pages.executionStats.gap') }}</div>
            <div class="stats-kpi-value-wrap">
              <div
                class="stats-kpi-value stats-kpi-time text-[16px] font-semibold tabular-nums tracking-tight"
                tabindex="0"
                data-testid="stats-kpi-gap-value"
                :aria-describedby="openTip === 'gap' ? tipDomId('gap') : undefined"
                @mouseenter="showTip('gap')"
                @mouseleave="hideTip('gap')"
                @focus="showTip('gap')"
                @blur="hideTip('gap')"
              >
                {{ fmtCompactDuration(singleSummary.gapSec) }}
              </div>
              <span
                v-if="openTip === 'gap'"
                :id="tipDomId('gap')"
                role="tooltip"
                class="stats-kpi-tip stats-kpi-tip-center"
                data-testid="stats-kpi-gap-tip"
              >
                {{ durationTipText(singleSummary.gapSec) }}
              </span>
            </div>
            <div class="mt-0.5 text-[10px] text-txt3">{{ t('pages.executionStats.gapHint') }}</div>
          </div>
          <div class="rounded-lg border border-line bg-surface px-3 py-2.5" data-testid="stats-kpi-total-tokens">
            <div class="mb-1 text-[11px] text-txt3">{{ t('pages.executionStats.totalTokens') }}</div>
            <div class="stats-kpi-value-wrap">
              <div
                class="stats-kpi-value text-[16px] font-semibold tabular-nums tracking-tight"
                :class="singleSummary.totalTokens == null ? 'text-txt3' : 'stats-kpi-token'"
                tabindex="0"
                data-testid="stats-kpi-total-tokens-value"
                :aria-describedby="openTip === 'tokens' ? tipDomId('tokens') : undefined"
                @mouseenter="showTip('tokens')"
                @mouseleave="hideTip('tokens')"
                @focus="showTip('tokens')"
                @blur="hideTip('tokens')"
              >
                {{ compactTokensMain(singleSummary.totalTokens) }}
              </div>
              <span
                v-if="openTip === 'tokens'"
                :id="tipDomId('tokens')"
                role="tooltip"
                class="stats-kpi-tip stats-kpi-tip-right stats-kpi-tip-rich"
                data-testid="stats-kpi-total-tokens-tip"
              >
                <div class="stats-kpi-tip-total">
                  {{
                    t('pages.executionStats.tipTokenTotal', {
                      n: fmtTokenCount(singleSummary.totalTokens ?? 0),
                    })
                  }}
                </div>
                <div class="stats-kpi-tip-parts">
                  <div
                    v-for="part in tipUsageParts"
                    :key="part.key"
                    class="stats-kpi-tip-part"
                    :class="{ 'stats-kpi-tip-part-hl': part.highlight }"
                    :data-testid="`stats-kpi-token-part-${part.key}`"
                  >
                    <span
                      class="stats-kpi-tip-part-k"
                      :class="{ 'stats-kpi-tip-part-k-hl': part.highlight }"
                    >
                      {{ part.label }}
                    </span>
                    <span class="stats-kpi-tip-part-n">{{ fmtTokenCount(part.full) }}</span>
                    <span class="stats-kpi-tip-part-c">{{ fmtCompactTokenCount(part.full) }}</span>
                  </div>
                </div>
              </span>
            </div>
            <div class="mt-0.5 text-[10px] text-txt3">{{ singleTokenHint }}</div>
          </div>
          <div class="rounded-lg border border-line bg-surface px-3 py-2.5" data-testid="stats-kpi-token-rate">
            <div class="mb-1 text-[11px] text-txt3">{{ t('pages.executionStats.tokenRate') }}</div>
            <div class="stats-kpi-value-wrap">
              <div
                class="stats-kpi-value text-[16px] font-semibold tabular-nums tracking-tight"
                :class="singleSummary.totalTokens == null ? 'text-txt3' : 'stats-kpi-token'"
                tabindex="0"
                data-testid="stats-kpi-token-rate-value"
                :aria-describedby="openTip === 'rate' ? tipDomId('rate') : undefined"
                @mouseenter="showTip('rate')"
                @mouseleave="hideTip('rate')"
                @focus="showTip('rate')"
                @blur="hideTip('rate')"
              >
                {{ compactRateMain(singleSummary.totalTokens, singleSummary.wallSec) }}
              </div>
              <span
                v-if="openTip === 'rate'"
                :id="tipDomId('rate')"
                role="tooltip"
                class="stats-kpi-tip stats-kpi-tip-right"
                data-testid="stats-kpi-token-rate-tip"
              >
                {{ tokenRateTipText(singleSummary.totalTokens, singleSummary.wallSec) }}
              </span>
            </div>
            <div class="mt-0.5 text-[10px] text-txt3">{{ singleRateHint }}</div>
          </div>
        </div>

        <TokenUsageByModelTable
          v-if="statsTab === 'single' && singleSummary.totalTokens != null"
          class="mb-3"
          :parts="modelUsageParts"
          :unknown-model-display-name="unknownModelDisplayName"
        />

        <div
          v-if="singleBottleneck"
          class="rounded-lg mb-3 border border-err/35 bg-err/6 px-3 py-2.5"
        >
          <div class="mb-1.5 flex items-center justify-between gap-2">
            <span class="text-[11px] font-semibold uppercase tracking-wide text-err">
              {{ t('pages.executionStats.bottleneck') }}
            </span>
            <span
              v-if="singleBottleneck.item.hasHumanWait"
              class="rounded-md shrink-0 border border-warn/40 bg-warn/10 px-1.5 py-px text-[10px] text-warn"
            >
              {{ t('pages.executionStats.hasHumanWait') }}
            </span>
          </div>
          <div class="text-[13px] font-semibold text-txt">{{ singleBottleneck.name }}</div>
          <div class="mt-1 flex flex-wrap gap-x-3.5 gap-y-1 text-[12px] text-txt2">
            <span>
              {{ t('pages.executionStats.duration') }}
              <code class="font-mono text-accent-2">{{ fmtDuration(singleBottleneck.item.durationSec) }}</code>
            </span>
            <span>
              {{ t('pages.executionStats.share') }}
              <code class="font-mono text-accent-2">
                {{ singleBottleneck.item.sharePct == null ? '—' : singleBottleneck.item.sharePct + '%' }}
              </code>
            </span>
          </div>
        </div>

        <div class="rounded-lg mb-3.5 border border-line bg-surface p-3">
          <StatsPieChart
            :items="singleItems"
            :center-value="fmtDuration(singleSummary.nodeSumSec)"
            :center-sub="pieCenterSub"
            :empty-label="t('pages.executionStats.empty')"
            :footnote="pieFootnote"
          />
        </div>

        <div class="mb-3 flex flex-wrap items-center gap-2">
          <span class="text-[11px] text-txt3">{{ t('pages.executionStats.dimension') }}</span>
          <div class="rounded-lg inline-flex border border-line bg-surface text-[12px]">
            <button
              v-for="opt in dimOptionsSingle"
              :key="opt.id"
              type="button"
              class="px-2.5 py-1 font-medium transition-colors"
              :class="
                singleDim === opt.id
                  ? 'bg-accent-dim text-accent-2'
                  : 'text-txt3 hover:bg-elevated hover:text-txt2'
              "
              @click="singleDim = opt.id"
            >
              {{ opt.label }}
            </button>
          </div>
        </div>
        <p class="mb-3 text-[11px] leading-snug text-txt3">{{ singleNote }}</p>

        <div v-if="!singleItems.length" class="py-8 text-center text-[12px] text-txt3">
          {{ t('pages.executionStats.empty') }}
        </div>
        <div v-else class="flex flex-col gap-2">
          <div
            v-for="it in singleItems"
            :key="it.key"
            class="rounded-lg border bg-surface px-3 py-2.5"
            :class="
              singleBottleneck && it.key === singleBottleneck.item.key
                ? 'border-err/40'
                : 'border-line'
            "
          >
            <div class="mb-1.5 flex items-center gap-2">
              <TruncatedTextTooltip
                :text="it.label"
                class="min-w-0 flex-1 truncate text-[13px] font-semibold text-txt"
                data-testid="stats-ranking-label"
              >
                <span
                  v-if="it.live"
                  class="mr-1 inline-block h-1.5 w-1.5 animate-pulse bg-info align-middle"
                />
                {{ it.label }}
              </TruncatedTextTooltip>
              <span
                v-if="it.isProcess && it.iteration && it.iteration > 1"
                class="rounded-md shrink-0 border border-warn/40 bg-warn/10 px-1.5 py-px text-[10px] text-warn"
              >
                {{ t('common.iterationN', { n: it.iteration }) }}
              </span>
              <span
                v-if="!it.isProcess && it.count > 1"
                class="rounded-md shrink-0 border border-info/40 bg-info/10 px-1.5 py-px text-[10px] text-info"
              >
                {{ t('pages.executionStats.mergeCount', { n: it.count }) }}
              </span>
              <span
                v-if="it.hasHumanWait"
                class="rounded-md shrink-0 border border-warn/40 bg-warn/10 px-1.5 py-px text-[10px] text-warn"
              >
                {{ t('pages.executionStats.hasHumanWait') }}
              </span>
              <div class="flex shrink-0 items-baseline gap-2 tabular-nums">
                <span class="stats-kpi-time text-[12px] font-medium">{{ fmtDuration(it.durationSec) }}</span>
                <span class="min-w-[36px] text-right text-[12px] font-semibold text-txt">
                  {{ it.sharePct == null ? t('pages.executionStats.dash') : it.sharePct + '%' }}
                </span>
                <span
                  class="min-w-[72px] text-right text-[12px] font-semibold tabular-nums"
                  :class="it.totalTokens == null ? 'text-txt3 stats-tok-na' : 'stats-tok-val'"
                  data-testid="stats-rank-tokens"
                >
                  {{ t('pages.executionStats.tokensCol') }}
                  {{ fmtTokensOrDash(it.totalTokens) }}
                </span>
              </div>
            </div>
            <div class="h-1 overflow-hidden bg-elevated">
              <div
                class="h-full transition-all duration-300"
                :style="{
                  width: (it.sharePct == null ? 0 : Math.min(100, it.sharePct)) + '%',
                  background:
                    singleBottleneck && it.key === singleBottleneck.item.key
                      ? 'rgb(var(--c-err))'
                      : it.color,
                }"
              />
            </div>
          </div>
        </div>
      </template>

      <!-- Multi run -->
      <template v-else>
        <div class="rounded-lg mb-3 border border-line bg-surface" data-testid="stats-multi-selection">
          <div class="flex items-center justify-between gap-3 border-b border-line px-3.5 py-2.5">
            <h3
              class="min-w-0 text-[12px] font-medium text-txt3"
              data-testid="stats-selected-runs-title"
            >
              {{ selectedRunsTitle }}
            </h3>
            <div class="flex shrink-0 items-center gap-1.5">
              <button
                type="button"
                class="inline-flex items-center gap-1 px-2 py-1 text-[11px] text-txt3 hover:text-txt2"
                :disabled="candidatesLoading"
                :title="t('common.buttons.refresh')"
                data-testid="stats-multi-refresh"
                @click="loadCandidates"
              >
                <Icon name="refresh" :size="12" />
                {{ t('common.buttons.refresh') }}
              </button>
              <button
                type="button"
                class="rounded-md inline-flex items-center gap-1 border px-2.5 py-1 text-[12px] transition-colors"
                :class="
                  pickerOpen
                    ? 'border-accent/50 bg-accent-dim text-accent-2'
                    : 'border-line bg-elevated text-txt2 hover:text-txt'
                "
                :aria-expanded="pickerOpen"
                aria-controls="stats-multi-picker"
                data-testid="stats-multi-picker-toggle"
                @click="togglePicker"
              >
                {{ t('pages.executionStats.pickRuns') }}
              </button>
            </div>
          </div>
          <p v-if="candidatesError" class="px-3.5 py-3 text-[12px] text-err">{{ candidatesError }}</p>
          <div
            v-else-if="candidatesLoading && !candidates.length"
            class="px-3.5 py-4 text-[12px] text-txt3"
          >
            {{ t('pages.executionStats.loadingCandidates') }}
          </div>
          <div
            v-else
            class="flex overflow-x-auto"
            data-testid="stats-multi-compare-bar"
          >
            <div
              v-for="c in selectedCandidates"
              :key="c.id"
              class="relative min-w-[156px] max-w-[220px] flex-1 border-r border-line px-3.5 py-3 last:border-r-0"
              :class="isCurrentRun(c.id) ? 'bg-accent-dim shadow-[inset_2px_0_0_rgb(var(--c-accent))]' : ''"
              data-testid="stats-multi-compare-cell"
              :data-run-id="c.id"
              :data-current="isCurrentRun(c.id) ? 'true' : 'false'"
            >
              <span
                v-if="isCurrentRun(c.id)"
                class="mb-1 inline-block text-[10px] font-semibold tracking-wide text-accent-2"
                data-testid="stats-multi-current-badge"
              >
                {{ t('pages.executionStats.current') }}
              </span>
              <span v-else class="mb-1 block text-[11px] text-txt3">{{ dayGroupLabel(c.startedAt) }}</span>
              <button
                v-if="!isCurrentRun(c.id)"
                type="button"
                class="absolute right-2 top-2 h-5 w-5 text-[14px] leading-5 text-txt3 hover:bg-overlay hover:text-txt"
                :aria-label="t('common.buttons.close')"
                data-testid="stats-multi-remove"
                @click="toggleRun(c.id)"
              >
                <Icon name="close" :size="12" />
              </button>
              <span class="block text-[22px] font-semibold tabular-nums tracking-tight text-txt sm:text-[24px]">
                {{ fmtStartHm(c.startedAt) }}
              </span>
              <div class="mt-1 text-[12px] text-txt3">{{ fmtCandidateTime(c.startedAt) }}</div>
              <div class="stats-kpi-time mt-0.5 text-[12px]">{{ durationWithPrefix(candidateWallSec(c)) }}</div>
              <span class="mt-1.5 block font-mono text-[11px] text-txt3">{{ c.label }}</span>
            </div>
          </div>
          <div
            v-if="pickerOpen"
            id="stats-multi-picker"
            class="max-h-[280px] overflow-y-auto border-t border-line py-2"
            data-testid="stats-multi-picker"
          >
            <template v-for="g in candidateGroups" :key="g.key">
              <div class="px-3.5 pb-1 pt-2 text-[11px] tracking-wide text-txt3" data-testid="stats-multi-day-group">
                {{ g.label }}
              </div>
              <button
                v-for="c in g.items"
                :key="c.id"
                type="button"
                class="grid w-full grid-cols-[18px_44px_minmax(0,148px)_minmax(0,1fr)_auto] items-center gap-2 px-3.5 py-1.5 text-left text-[12px] text-txt2 hover:bg-elevated hover:text-txt"
                :class="selectedIds.has(c.id) ? 'bg-accent-dim/60 text-txt' : ''"
                :aria-pressed="selectedIds.has(c.id)"
                data-testid="stats-multi-picker-row"
                :data-run-id="c.id"
                :data-current="isCurrentRun(c.id) ? 'true' : 'false'"
                @click="toggleRun(c.id)"
              >
                <span
                  class="rounded-lg inline-block h-3.5 w-3.5 shrink-0 border"
                  :class="
                    selectedIds.has(c.id)
                      ? 'border-accent-2 bg-accent-2'
                      : 'border-line-strong bg-elevated'
                  "
                />
                <span class="text-[11px] font-semibold text-accent-2">
                  {{ isCurrentRun(c.id) ? t('pages.executionStats.current') : '' }}
                </span>
                <span class="truncate tabular-nums">
                  {{ fmtCandidateTime(c.startedAt) }}
                </span>
                <span class="stats-kpi-time truncate">{{ durationWithPrefix(candidateWallSec(c)) }}</span>
                <span class="font-mono text-[11px] text-txt3">{{ c.label }}</span>
              </button>
            </template>
          </div>
          <p
            v-for="(msg, id) in detailErrors"
            :key="id"
            class="px-3.5 py-1 text-[11px] text-warn"
          >
            {{ msg }}
          </p>
          <p v-if="detailsLoading || pendingDetailCount" class="px-3.5 pb-2 text-[11px] text-txt3">
            {{ t('pages.executionStats.loadingDetails') }}
          </p>
        </div>

        <div class="mb-3.5 grid grid-cols-1 gap-2 md:grid-cols-3 xl:grid-cols-6">
          <div class="rounded-lg border border-line bg-surface px-3 py-2.5" data-testid="stats-kpi-selected">
            <div class="mb-1 text-[11px] text-txt3">{{ t('pages.executionStats.selectedRuns') }}</div>
            <div
              class="text-[16px] font-semibold tabular-nums text-txt"
              data-testid="stats-kpi-selected-value"
            >
              {{ selectedCountDisplay }}
            </div>
          </div>
          <div class="rounded-lg border border-line bg-surface px-3 py-2.5" data-testid="stats-kpi-avg-wall">
            <div class="mb-1 text-[11px] text-txt3">{{ t('pages.executionStats.avgWall') }}</div>
            <div class="stats-kpi-value-wrap">
              <div
                class="stats-kpi-value stats-kpi-time text-[16px] font-semibold tabular-nums tracking-tight"
                tabindex="0"
                data-testid="stats-kpi-avg-wall-value"
                :aria-describedby="openTip === 'avgWall' ? tipDomId('avgWall') : undefined"
                @mouseenter="showTip('avgWall')"
                @mouseleave="hideTip('avgWall')"
                @focus="showTip('avgWall')"
                @blur="hideTip('avgWall')"
              >
                {{
                  multiReady && multiSummary.selectedCount
                    ? fmtMultiAvgDuration(multiAvgWallSec)
                    : t('pages.executionStats.dash')
                }}
              </div>
              <span
                v-if="openTip === 'avgWall'"
                :id="tipDomId('avgWall')"
                role="tooltip"
                class="stats-kpi-tip stats-kpi-tip-left"
                data-testid="stats-kpi-avg-wall-tip"
              >
                {{ durationTipText(Math.round(multiAvgWallSec)) }}
              </span>
            </div>
          </div>
          <div class="rounded-lg border border-line bg-surface px-3 py-2.5" data-testid="stats-kpi-process-count">
            <div class="mb-1 text-[11px] text-txt3">{{ t('pages.executionStats.processCount') }}</div>
            <div
              class="text-[16px] font-semibold tabular-nums text-txt"
              data-testid="stats-kpi-process-count-value"
            >
              {{ multiReady ? multiSummary.processCount : t('pages.executionStats.dash') }}
            </div>
          </div>
          <div class="rounded-lg border border-line bg-surface px-3 py-2.5" data-testid="stats-kpi-sum-tokens">
            <div class="mb-1 text-[11px] text-txt3">{{ t('pages.executionStats.sumTokens') }}</div>
            <div class="stats-kpi-value-wrap">
              <div
                class="stats-kpi-value text-[16px] font-semibold tabular-nums tracking-tight"
                :class="!multiReady || multiSummary.totalTokens == null ? 'text-txt3' : 'stats-kpi-token'"
                tabindex="0"
                data-testid="stats-kpi-sum-tokens-value"
                :aria-describedby="openTip === 'sumTokens' ? tipDomId('sumTokens') : undefined"
                @mouseenter="showTip('sumTokens')"
                @mouseleave="hideTip('sumTokens')"
                @focus="showTip('sumTokens')"
                @blur="hideTip('sumTokens')"
              >
                {{ multiReady ? compactTokensMain(multiSummary.totalTokens) : t('pages.executionStats.dash') }}
              </div>
              <span
                v-if="openTip === 'sumTokens'"
                :id="tipDomId('sumTokens')"
                role="tooltip"
                class="stats-kpi-tip stats-kpi-tip-right"
                data-testid="stats-kpi-sum-tokens-tip"
              >
                {{
                  t('pages.executionStats.tipTokenTotal', {
                    n: fmtTokenCount(multiSummary.totalTokens ?? 0),
                  })
                }}
              </span>
            </div>
            <div class="mt-0.5 text-[10px] text-txt3">{{ multiSumHint }}</div>
          </div>
          <div class="rounded-lg border border-line bg-surface px-3 py-2.5" data-testid="stats-kpi-avg-tokens">
            <div class="mb-1 text-[11px] text-txt3">{{ t('pages.executionStats.avgTokens') }}</div>
            <div class="stats-kpi-value-wrap">
              <div
                class="stats-kpi-value text-[16px] font-semibold tabular-nums tracking-tight"
                :class="!multiReady || multiSummary.avgTokens == null ? 'text-txt3' : 'stats-kpi-token'"
                tabindex="0"
                data-testid="stats-kpi-avg-tokens-value"
                :aria-describedby="openTip === 'avgTokens' ? tipDomId('avgTokens') : undefined"
                @mouseenter="showTip('avgTokens')"
                @mouseleave="hideTip('avgTokens')"
                @focus="showTip('avgTokens')"
                @blur="hideTip('avgTokens')"
              >
                {{ multiReady ? compactTokensMain(multiSummary.avgTokens) : t('pages.executionStats.dash') }}
              </div>
              <span
                v-if="openTip === 'avgTokens'"
                :id="tipDomId('avgTokens')"
                role="tooltip"
                class="stats-kpi-tip stats-kpi-tip-right"
                data-testid="stats-kpi-avg-tokens-tip"
              >
                {{
                  t('pages.executionStats.tipTokenTotal', {
                    n: fmtTokenCount(multiSummary.avgTokens ?? 0),
                  })
                }}
              </span>
            </div>
            <div class="mt-0.5 text-[10px] text-txt3">{{ multiAvgHint }}</div>
          </div>
          <div class="rounded-lg border border-line bg-surface px-3 py-2.5" data-testid="stats-kpi-multi-token-rate">
            <div class="mb-1 text-[11px] text-txt3">{{ t('pages.executionStats.tokenRate') }}</div>
            <div class="stats-kpi-value-wrap">
              <div
                class="stats-kpi-value text-[16px] font-semibold tabular-nums tracking-tight"
                :class="!multiReady || multiSummary.totalTokens == null ? 'text-txt3' : 'stats-kpi-token'"
                tabindex="0"
                data-testid="stats-kpi-multi-token-rate-value"
                :aria-describedby="openTip === 'multiRate' ? tipDomId('multiRate') : undefined"
                @mouseenter="showTip('multiRate')"
                @mouseleave="hideTip('multiRate')"
                @focus="showTip('multiRate')"
                @blur="hideTip('multiRate')"
              >
                {{
                  multiReady
                    ? multiRateMain(multiSummary.totalTokens, multiSummary.wallSumSec)
                    : t('pages.executionStats.dash')
                }}
              </div>
              <span
                v-if="openTip === 'multiRate'"
                :id="tipDomId('multiRate')"
                role="tooltip"
                class="stats-kpi-tip stats-kpi-tip-right"
                data-testid="stats-kpi-multi-token-rate-tip"
              >
                {{ tokenRateTipText(multiSummary.totalTokens, multiSummary.wallSumSec) }}
              </span>
            </div>
            <div class="mt-0.5 text-[10px] text-txt3">{{ multiRateHint }}</div>
          </div>
        </div>

        <p v-if="!multiReady" class="mb-3.5 py-6 text-center text-[12px] text-txt3">
          {{ t('pages.executionStats.multiPending') }}
        </p>
        <template v-else>
        <div v-if="multiItems[0]" class="rounded-lg mb-3.5 border border-err/35 bg-err/6 px-3 py-2.5">
          <div class="mb-1 text-[11px] font-semibold uppercase tracking-wide text-err">
            {{ t('pages.executionStats.topAvg') }}
          </div>
          <div class="text-[13px] font-semibold text-txt">{{ multiItems[0].label }}</div>
          <div class="mt-1 text-[12px] text-txt2">
            {{ t('pages.executionStats.avg') }}
            {{ fmtDuration(multiItems[0].avgSec) }}
            ·
            {{ t('pages.executionStats.share') }}
            {{ multiItems[0].sharePct == null ? '—' : multiItems[0].sharePct + '%' }}
          </div>
        </div>

        <div class="mb-3 flex flex-wrap items-center gap-2">
          <span class="text-[11px] text-txt3">{{ t('pages.executionStats.dimension') }}</span>
          <div class="rounded-lg inline-flex border border-line bg-surface text-[12px]">
            <button
              v-for="opt in dimOptionsMulti"
              :key="opt.id"
              type="button"
              class="px-2.5 py-1 font-medium transition-colors"
              :class="
                multiDim === opt.id
                  ? 'bg-accent-dim text-accent-2'
                  : 'text-txt3 hover:bg-elevated hover:text-txt2'
              "
              @click="multiDim = opt.id"
            >
              {{ opt.label }}
            </button>
          </div>
        </div>

        <div class="rounded-lg mb-3.5 border border-line bg-surface p-3">
          <StatsPieChart
            :items="multiItems"
            :center-value="fmtDuration(multiSummary.wallSumSec ? multiItems.reduce((a, i) => a + i.durationSec, 0) : 0)"
            :center-sub="t('pages.executionStats.pieMulti')"
            :empty-label="t('pages.executionStats.empty')"
          />
        </div>

        <h3 class="mb-2 text-[12px] font-medium text-txt3">
          {{
            multiDim === 'node'
              ? t('pages.executionStats.rankByNode')
              : t('pages.executionStats.rankByType')
          }}
        </h3>
        <p class="mb-3 text-[11px] leading-snug text-txt3">{{ t('pages.executionStats.noteMulti') }}</p>

        <div v-if="!multiItems.length" class="py-8 text-center text-[12px] text-txt3">
          {{ t('pages.executionStats.empty') }}
        </div>
        <div v-else class="flex flex-col gap-2">
          <div
            v-for="(it, i) in multiItems"
            :key="it.key"
            class="rounded-lg border bg-surface px-3 py-2.5"
            :class="i === 0 ? 'border-err/40' : 'border-line'"
          >
            <div class="mb-1.5 flex items-center gap-2">
              <span class="w-5 shrink-0 text-[12px] font-semibold tabular-nums text-txt3">{{ i + 1 }}</span>
              <TruncatedTextTooltip
                :text="it.label"
                class="min-w-0 flex-1 truncate text-[13px] font-semibold text-txt"
                data-testid="stats-ranking-label"
              >
                {{ it.label }}
              </TruncatedTextTooltip>
              <span
                v-if="it.hasHumanWait"
                class="rounded-md shrink-0 border border-warn/40 bg-warn/10 px-1.5 py-px text-[10px] text-warn"
              >
                {{ t('pages.executionStats.hasHumanWait') }}
              </span>
            </div>
            <div class="mb-1.5 flex flex-wrap items-baseline gap-x-2 gap-y-0.5 pl-7 text-[11px] text-txt2">
              <span>
                {{ t('pages.executionStats.avg') }}
                <span class="stats-kpi-time font-medium">{{ fmtDuration(it.avgSec) }}</span>
              </span>
              <span>· {{ t('pages.executionStats.rankTotal') }} {{ fmtDuration(it.durationSec) }}</span>
              <span>· {{ it.sharePct == null ? t('pages.executionStats.dash') : it.sharePct + '%' }}</span>
              <span>· {{ t('pages.executionStats.times', { n: it.count }) }}</span>
              <span
                class="font-semibold tabular-nums"
                :class="it.totalTokens == null ? 'text-txt3' : 'stats-tok-val'"
                data-testid="stats-rank-tokens"
              >
                · {{ t('pages.executionStats.tokensCol') }}
                {{ fmtTokensOrDash(it.totalTokens) }}
              </span>
            </div>
            <div class="h-1 overflow-hidden bg-elevated pl-0">
              <div
                class="ml-7 h-full transition-all duration-300"
                :style="{
                  width: (maxMultiAvg > 0 ? Math.round((it.avgSec / maxMultiAvg) * 100) : 0) + '%',
                  background: it.color,
                }"
              />
            </div>
          </div>
        </div>
        </template>
      </template>
    </div>
  </div>
</template>
