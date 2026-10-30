<script setup lang="ts">
import Icon from '@/components/ui/Icon.vue'
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useBreakpoint } from '@/lib/composables/useBreakpoint'
import {
  placeFixedOverlayAbove,
  useFixedOverlayAboveListeners,
  type FixedOverlayAboveStyle,
} from '@/lib/composables/useFixedOverlayAbove'
import { usePlatformStatusMetrics } from '@/lib/composables/usePlatformStatusMetrics'
import { fmtCompactTokenCount } from '@/lib/run/tokenUsage'

const props = withDefaults(
  defineProps<{
    /** auto = breakpoint; compact forces narrow strip (sidebar/drawer). */
    variant?: 'auto' | 'full' | 'compact'
  }>(),
  { variant: 'auto' },
)

const { t } = useI18n()
const router = useRouter()
const { isMobile } = useBreakpoint()
const { metrics, stale } = usePlatformStatusMetrics()

const useCompact = computed(() => {
  if (props.variant === 'compact') return true
  if (props.variant === 'full') return false
  return isMobile.value
})

/** Sidebar/drawer compact tips Teleport above the trigger to escape overflow-hidden. */
const usePortaledCompactTip = computed(() => props.variant === 'compact')

type CompactZone = 'token' | 'run'

const activeCompactZone = ref<CompactZone | null>(null)
const compactTokenTrigger = ref<HTMLElement | null>(null)
const compactRunTrigger = ref<HTMLElement | null>(null)
const compactTip = ref<HTMLElement | null>(null)
/** Hide Teleport tip after click until pointer leaves (plan g1.3). */
const suppressCompactTip = ref(false)
const compactTipStyle = ref<FixedOverlayAboveStyle | null>(null)

const compactAnchor = computed(() => {
  if (activeCompactZone.value === 'token') return compactTokenTrigger.value
  if (activeCompactZone.value === 'run') return compactRunTrigger.value
  return null
})

const compactTipVisible = computed(
  () =>
    usePortaledCompactTip.value &&
    !suppressCompactTip.value &&
    activeCompactZone.value != null,
)

async function repositionCompactTip() {
  if (!compactTipVisible.value) return
  await nextTick()
  compactTipStyle.value = await placeFixedOverlayAbove(compactAnchor.value, compactTip.value, {
    align: 'center',
    gap: 8,
  })
}

const { start: startCompactTipListeners, stop: stopCompactTipListeners } =
  useFixedOverlayAboveListeners(compactTipVisible, repositionCompactTip)

watch([compactTipVisible, activeCompactZone], async ([visible]) => {
  if (visible) {
    startCompactTipListeners()
    await repositionCompactTip()
  } else {
    stopCompactTipListeners()
    compactTipStyle.value = null
  }
})

function fmtFull(n: number | null | undefined): string {
  if (n === null || n === undefined) return '—'
  return n.toLocaleString('en-US')
}

const cumulative = computed(() => metrics.value?.cumulativeTokens ?? null)
const today = computed(() => metrics.value?.todayTokens ?? null)
const running = computed(() => metrics.value?.runningCount ?? 0)
const queued = computed(() => metrics.value?.queuedCount ?? 0)

function todayAria(): string {
  return `${t('shell.statusMetrics.today')}: ${fmtFull(today.value)} · ${t('shell.statusMetrics.openStats')}`
}

function statsAria(label: string): string {
  return `${label} · ${t('shell.statusMetrics.openStats')}`
}

function runsAria(label: string): string {
  return `${label} · ${t('shell.statusMetrics.openRuns')}`
}

function tokenZoneAria(): string {
  return `${t('shell.statusMetrics.tokens')}: ${fmtFull(cumulative.value)} · ${t('shell.statusMetrics.today')}: ${fmtFull(today.value)} · ${t('shell.statusMetrics.openStats')}`
}

function runZoneAria(): string {
  return `${t('shell.statusMetrics.running')}: ${running.value} · ${t('shell.statusMetrics.queued')}: ${queued.value} · ${t('shell.statusMetrics.openRuns')}`
}

function goToStats() {
  suppressCompactTip.value = true
  activeCompactZone.value = null
  void router.push({ name: 'stats' })
}

/** plan g1.1: run-zone hotspots navigate to name=runs (not stats). */
function goToRuns() {
  suppressCompactTip.value = true
  activeCompactZone.value = null
  void router.push({ name: 'runs' })
}

function onActivateKey(ev: KeyboardEvent, dest: 'stats' | 'runs' = 'stats') {
  if (ev.key !== 'Enter' && ev.key !== ' ') return
  ev.preventDefault()
  if (dest === 'runs') goToRuns()
  else goToStats()
}

function onCompactZoneEnter(zone: CompactZone) {
  suppressCompactTip.value = false
  activeCompactZone.value = zone
}

function onCompactStripLeave() {
  activeCompactZone.value = null
  suppressCompactTip.value = false
}

function onCompactStripFocusOut(ev: FocusEvent) {
  const root = ev.currentTarget as HTMLElement
  if (root.contains(ev.relatedTarget as Node | null)) return
  activeCompactZone.value = null
}
</script>

<template>
  <div
    class="status-metrics flex select-none items-center font-mono text-txt2 tabular-nums"
    data-testid="status-metrics"
    :aria-label="t('shell.statusMetrics.aria')"
    :data-stale="stale ? 'true' : 'false'"
  >
    <!-- Desktop ≥md (or variant=full): four icon+value items -->
    <template v-if="!useCompact">
      <button
        type="button"
        class="sm-item relative inline-flex items-center gap-1.5 border-0 bg-transparent px-1.5 py-1 text-inherit hover:bg-elevated hover:text-txt focus-visible:bg-elevated focus-visible:text-txt focus-visible:outline-none"
        data-testid="status-metrics-tokens"
        :aria-label="statsAria(t('shell.statusMetrics.tokens'))"
        @click="goToStats"
        @keydown="onActivateKey"
      >
        <Icon name="token" :size="14" class="sm-ico block h-3.5 w-3.5 shrink-0 text-txt3" aria-hidden="true" />
        <span class="sm-val text-xs leading-none text-txt">{{ fmtCompactTokenCount(cumulative) }}</span>
        <span
          class="sm-tip sm-kpi pointer-events-none absolute left-1/2 top-[calc(100%+6px)] z-40 hidden min-w-[200px] -translate-x-1/2 border border-line-strong bg-surface px-3 py-2.5 text-left font-sans shadow-card"
          role="tooltip"
        >
          <div class="sm-kpi-title">{{ t('shell.statusMetrics.tokens') }}</div>
          <div class="sm-kpi-row">
            <span class="sm-kpi-lab">{{ t('shell.statusMetrics.tokens') }}</span>
            <span class="sm-kpi-num font-mono tabular-nums">{{ fmtFull(cumulative) }}</span>
          </div>
        </span>
      </button>
      <span class="mx-0.5 h-3.5 w-px shrink-0 bg-line-strong" aria-hidden="true" />

      <button
        type="button"
        class="sm-item relative inline-flex items-center gap-1.5 border-0 bg-transparent px-1.5 py-1 text-inherit hover:bg-elevated hover:text-txt focus-visible:bg-elevated focus-visible:text-txt focus-visible:outline-none"
        data-testid="status-metrics-today"
        :aria-label="todayAria()"
        @click="goToStats"
        @keydown="onActivateKey"
      >
        <Icon name="calendar" :size="14" class="sm-ico block h-3.5 w-3.5 shrink-0 text-txt3" aria-hidden="true" />
        <span class="sm-val text-xs leading-none text-txt">{{ fmtCompactTokenCount(today) }}</span>
        <span
          class="sm-tip sm-kpi pointer-events-none absolute left-1/2 top-[calc(100%+6px)] z-40 hidden min-w-[200px] -translate-x-1/2 border border-line-strong bg-surface px-3 py-2.5 text-left font-sans shadow-card"
          role="tooltip"
        >
          <div class="sm-kpi-title">{{ t('shell.statusMetrics.today') }}</div>
          <div class="sm-kpi-row">
            <span class="sm-kpi-lab">{{ t('shell.statusMetrics.today') }}</span>
            <span class="sm-kpi-num font-mono tabular-nums">{{ fmtFull(today) }}</span>
          </div>
        </span>
      </button>
      <span class="mx-0.5 h-3.5 w-px shrink-0 bg-line-strong" aria-hidden="true" />

      <button
        type="button"
        class="sm-item relative inline-flex items-center gap-1.5 border-0 bg-transparent px-1.5 py-1 text-inherit hover:bg-elevated hover:text-txt focus-visible:bg-elevated focus-visible:text-txt focus-visible:outline-none"
        data-testid="status-metrics-running"
        :aria-label="runsAria(t('shell.statusMetrics.running'))"
        @click="goToRuns"
        @keydown="onActivateKey($event, 'runs')"
      >
        <Icon name="runs" :size="14" class="sm-ico block h-3.5 w-3.5 shrink-0 text-txt3" aria-hidden="true" />
        <span class="sm-val text-xs leading-none text-txt">{{ running }}</span>
        <span
          class="sm-tip sm-kpi pointer-events-none absolute left-1/2 top-[calc(100%+6px)] z-40 hidden min-w-[160px] -translate-x-1/2 border border-line-strong bg-surface px-3 py-2.5 text-left font-sans shadow-card"
          role="tooltip"
        >
          <div class="sm-kpi-title">{{ t('shell.statusMetrics.running') }}</div>
          <div class="sm-kpi-row">
            <span class="sm-kpi-lab">{{ t('shell.statusMetrics.running') }}</span>
            <span class="sm-kpi-num font-mono tabular-nums">{{ running }}</span>
          </div>
        </span>
      </button>
      <span class="mx-0.5 h-3.5 w-px shrink-0 bg-line-strong" aria-hidden="true" />

      <button
        type="button"
        class="sm-item relative inline-flex items-center gap-1.5 border-0 bg-transparent px-1.5 py-1 text-inherit hover:bg-elevated hover:text-txt focus-visible:bg-elevated focus-visible:text-txt focus-visible:outline-none"
        data-testid="status-metrics-queued"
        :aria-label="runsAria(t('shell.statusMetrics.queued'))"
        @click="goToRuns"
        @keydown="onActivateKey($event, 'runs')"
      >
        <Icon name="queue" :size="14" class="sm-ico block h-3.5 w-3.5 shrink-0 text-txt3" aria-hidden="true" />
        <span class="sm-val text-xs leading-none text-txt">{{ queued }}</span>
        <span
          class="sm-tip sm-kpi pointer-events-none absolute left-1/2 top-[calc(100%+6px)] z-40 hidden min-w-[160px] -translate-x-1/2 border border-line-strong bg-surface px-3 py-2.5 text-left font-sans shadow-card"
          role="tooltip"
        >
          <div class="sm-kpi-title">{{ t('shell.statusMetrics.queued') }}</div>
          <div class="sm-kpi-row">
            <span class="sm-kpi-lab">{{ t('shell.statusMetrics.queued') }}</span>
            <span class="sm-kpi-num font-mono tabular-nums">{{ queued }}</span>
          </div>
        </span>
      </button>
    </template>

    <!-- Narrow &lt;md / sidebar: Token zone | run zone; partitioned KPI tips -->
    <div
      v-else
      class="sm-compact relative inline-flex w-full items-center gap-0 rounded-md border-0 bg-elevated px-1 py-0.5 text-[11px] text-inherit"
      :class="suppressCompactTip ? 'tip-suppressed' : ''"
      data-testid="status-metrics-compact"
      @mouseleave="onCompactStripLeave"
      @focusout="onCompactStripFocusOut"
    >
      <button
        ref="compactTokenTrigger"
        type="button"
        class="sm-item sm-zone relative inline-flex shrink-0 items-baseline gap-1.5 rounded-md border-0 bg-transparent px-2 py-1.5 text-inherit hover:bg-surface hover:text-txt focus-visible:bg-surface focus-visible:text-txt focus-visible:outline-none"
        data-testid="status-metrics-compact-token"
        :aria-label="tokenZoneAria()"
        @click="goToStats"
        @keydown="onActivateKey"
        @mouseenter="onCompactZoneEnter('token')"
        @focus="onCompactZoneEnter('token')"
      >
        <span
          class="sm-val text-xs font-semibold leading-none"
          :class="cumulative == null ? 'text-txt3' : 'text-txt'"
        >{{ fmtCompactTokenCount(cumulative) }}</span>
        <span class="sm-lab font-sans text-[11px] leading-none text-txt3">{{ t('shell.statusMetrics.cardTokenTitle') }}</span>
        <span
          v-if="!usePortaledCompactTip"
          class="sm-tip sm-kpi pointer-events-none absolute left-1/2 top-[calc(100%+6px)] z-40 hidden min-w-[220px] -translate-x-1/2 border border-line-strong bg-surface px-3 py-2.5 text-left font-sans shadow-card"
          role="tooltip"
          data-testid="status-metrics-compact-token-tip"
        >
          <div class="sm-kpi-title">
            <Icon name="token" :size="14" class="sm-kpi-ico" :interactive="false" aria-hidden="true" />
            {{ t('shell.statusMetrics.cardTokenTitle') }}
          </div>
          <div class="sm-kpi-row">
            <span class="sm-kpi-lab">{{ t('shell.statusMetrics.tokens') }}</span>
            <span class="sm-kpi-num font-mono tabular-nums">{{ fmtFull(cumulative) }}</span>
          </div>
          <div class="sm-kpi-row">
            <span class="sm-kpi-lab">{{ t('shell.statusMetrics.today') }}</span>
            <span class="sm-kpi-num font-mono tabular-nums">{{ fmtFull(today) }}</span>
          </div>
        </span>
      </button>
      <span class="mx-1 h-3.5 w-px shrink-0 bg-line-strong" aria-hidden="true" />
      <button
        ref="compactRunTrigger"
        type="button"
        class="sm-item sm-zone relative inline-flex min-w-0 flex-1 items-center gap-1.5 rounded-md border-0 bg-transparent px-2 py-1.5 text-inherit hover:bg-surface hover:text-txt focus-visible:bg-surface focus-visible:text-txt focus-visible:outline-none"
        data-testid="status-metrics-compact-run"
        :data-active="running > 0 ? 'true' : 'false'"
        :aria-label="runZoneAria()"
        @click="goToRuns"
        @keydown="onActivateKey($event, 'runs')"
        @mouseenter="onCompactZoneEnter('run')"
        @focus="onCompactZoneEnter('run')"
      >
        <span class="relative inline-flex h-1.5 w-1.5 shrink-0" aria-hidden="true">
          <span
            v-if="running > 0"
            class="absolute inset-0 animate-ping rounded-full bg-ok opacity-60 motion-reduce:animate-none"
          />
          <span
            class="relative h-1.5 w-1.5 rounded-full"
            :class="running > 0 ? 'bg-ok' : 'bg-txt3/40'"
          />
        </span>
        <span class="inline-flex items-baseline gap-1 truncate">
          <span
            class="sm-val text-xs font-semibold leading-none"
            :class="running > 0 ? 'text-txt' : 'text-txt2'"
          >{{ running }}</span>
          <span class="sm-lab font-sans text-[11px] leading-none text-txt3">{{ t('shell.statusMetrics.running') }}</span>
        </span>
        <span class="text-txt3/60" aria-hidden="true">·</span>
        <span class="inline-flex items-baseline gap-1 truncate">
          <span
            class="sm-val text-xs font-semibold leading-none"
            :class="queued > 0 ? 'text-txt' : 'text-txt2'"
          >{{ queued }}</span>
          <span class="sm-lab font-sans text-[11px] leading-none text-txt3">{{ t('shell.statusMetrics.queued') }}</span>
        </span>
        <span
          v-if="!usePortaledCompactTip"
          class="sm-tip sm-kpi pointer-events-none absolute left-1/2 top-[calc(100%+6px)] z-40 hidden min-w-[180px] -translate-x-1/2 border border-line-strong bg-surface px-3 py-2.5 text-left font-sans shadow-card"
          role="tooltip"
          data-testid="status-metrics-compact-run-tip"
        >
          <div class="sm-kpi-title">
            <Icon name="runs" :size="14" class="sm-kpi-ico" :interactive="false" aria-hidden="true" />
            {{ t('shell.statusMetrics.cardRunTitle') }}
          </div>
          <div class="sm-kpi-row">
            <span class="sm-kpi-lab">{{ t('shell.statusMetrics.running') }}</span>
            <span class="sm-kpi-num font-mono tabular-nums">{{ running }}</span>
          </div>
          <div class="sm-kpi-row">
            <span class="sm-kpi-lab">{{ t('shell.statusMetrics.queued') }}</span>
            <span class="sm-kpi-num font-mono tabular-nums">{{ queued }}</span>
          </div>
        </span>
      </button>
    </div>

    <Teleport v-if="usePortaledCompactTip" to="body">
      <div
        v-show="compactTipVisible"
        ref="compactTip"
        class="sm-tip sm-kpi pointer-events-none fixed z-[60] min-w-[220px] border border-line-strong bg-surface px-3 py-2.5 text-left font-sans shadow-card"
        role="tooltip"
        data-testid="status-metrics-compact-tip"
        :data-zone="activeCompactZone ?? undefined"
        data-placement="above"
        :style="compactTipStyle ?? undefined"
      >
        <template v-if="activeCompactZone === 'token'">
          <div class="sm-kpi-title">
            <Icon name="token" :size="14" class="sm-kpi-ico" :interactive="false" aria-hidden="true" />
            {{ t('shell.statusMetrics.cardTokenTitle') }}
          </div>
          <div class="sm-kpi-row">
            <span class="sm-kpi-lab">{{ t('shell.statusMetrics.tokens') }}</span>
            <span class="sm-kpi-num font-mono tabular-nums">{{ fmtFull(cumulative) }}</span>
          </div>
          <div class="sm-kpi-row">
            <span class="sm-kpi-lab">{{ t('shell.statusMetrics.today') }}</span>
            <span class="sm-kpi-num font-mono tabular-nums">{{ fmtFull(today) }}</span>
          </div>
        </template>
        <template v-else-if="activeCompactZone === 'run'">
          <div class="sm-kpi-title">
            <Icon name="runs" :size="14" class="sm-kpi-ico" :interactive="false" aria-hidden="true" />
            {{ t('shell.statusMetrics.cardRunTitle') }}
          </div>
          <div class="sm-kpi-row">
            <span class="sm-kpi-lab">{{ t('shell.statusMetrics.running') }}</span>
            <span class="sm-kpi-num font-mono tabular-nums">{{ running }}</span>
          </div>
          <div class="sm-kpi-row">
            <span class="sm-kpi-lab">{{ t('shell.statusMetrics.queued') }}</span>
            <span class="sm-kpi-num font-mono tabular-nums">{{ queued }}</span>
          </div>
        </template>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.sm-item:hover .sm-ico,
.sm-item:focus-visible .sm-ico,
.sm-item.tip-open .sm-ico {
  color: rgb(var(--c-txt2));
}
.sm-item:hover .sm-lab,
.sm-item:focus-visible .sm-lab {
  color: rgb(var(--c-txt2));
}
.sm-item:hover .sm-tip,
.sm-item:focus-visible .sm-tip,
.sm-item.tip-open .sm-tip {
  display: block;
}
.sm-compact.tip-suppressed .sm-tip,
.sm-compact.tip-suppressed .sm-item:hover .sm-tip,
.sm-compact.tip-suppressed .sm-item:focus-visible .sm-tip {
  display: none;
}
.sm-kpi {
  border-radius: 12px;
  box-shadow: var(--shadow-card);
}
.sm-kpi-title {
  display: flex;
  align-items: center;
  gap: 6px;
  margin: 0 0 6px;
  font-size: 11px;
  font-weight: 650;
  line-height: 1.2;
  color: rgb(var(--c-txt2));
}
.sm-kpi-ico {
  width: 13px;
  height: 13px;
  flex-shrink: 0;
  color: rgb(var(--c-txt3));
}
.sm-kpi-row {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
  padding: 5px 0;
}
.sm-kpi-row + .sm-kpi-row {
  border-top: 1px solid rgb(var(--c-line));
}
.sm-kpi-lab {
  font-size: 12px;
  color: rgb(var(--c-txt2));
}
.sm-kpi-num {
  font-size: 12.5px;
  font-weight: 650;
  color: rgb(var(--c-txt));
}
</style>
