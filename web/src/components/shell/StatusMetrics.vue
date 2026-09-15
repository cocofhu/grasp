<script setup lang="ts">
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
        <svg class="sm-ico block h-3.5 w-3.5 shrink-0 text-txt3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <ellipse cx="12" cy="6.6" rx="7.2" ry="3.1" />
          <path d="M4.8 6.6v4.7c0 1.7 3.2 3.1 7.2 3.1s7.2-1.4 7.2-3.1V6.6" />
          <path d="M4.8 11.5v4.7c0 1.7 3.2 3.1 7.2 3.1s7.2-1.4 7.2-3.1v-4.7" />
        </svg>
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
        <svg class="sm-ico block h-3.5 w-3.5 shrink-0 text-txt3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <rect x="3.4" y="5.2" width="17.2" height="15.2" rx="2.6" />
          <path d="M8.2 3v4.2M15.8 3v4.2M3.4 10.2h17.2" />
          <circle cx="12" cy="15.2" r="2.6" />
        </svg>
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
        <svg class="sm-ico block h-3.5 w-3.5 shrink-0 text-txt3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <circle cx="12" cy="12" r="8.2" />
          <path d="M10.3 8.7l5.4 3.3-5.4 3.3z" />
        </svg>
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
        <svg class="sm-ico block h-3.5 w-3.5 shrink-0 text-txt3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <path d="M4 7.2h16M4 12h11.5M4 16.8h7" />
        </svg>
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
        class="sm-item sm-zone relative inline-flex flex-1 items-center gap-1.5 rounded-md border-0 bg-transparent px-1.5 py-1 text-inherit hover:bg-surface hover:text-txt focus-visible:bg-surface focus-visible:text-txt focus-visible:outline-none"
        data-testid="status-metrics-compact-token"
        :aria-label="tokenZoneAria()"
        @click="goToStats"
        @keydown="onActivateKey"
        @mouseenter="onCompactZoneEnter('token')"
        @focus="onCompactZoneEnter('token')"
      >
        <svg class="sm-ico block h-[13px] w-[13px] shrink-0 text-txt3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <ellipse cx="12" cy="6.6" rx="7.2" ry="3.1" />
          <path d="M4.8 6.6v4.7c0 1.7 3.2 3.1 7.2 3.1s7.2-1.4 7.2-3.1V6.6" />
          <path d="M4.8 11.5v4.7c0 1.7 3.2 3.1 7.2 3.1s7.2-1.4 7.2-3.1v-4.7" />
        </svg>
        <span class="sm-val text-[11px] font-semibold leading-none text-txt">{{ fmtCompactTokenCount(cumulative) }}</span>
        <span
          v-if="!usePortaledCompactTip"
          class="sm-tip sm-kpi pointer-events-none absolute left-1/2 top-[calc(100%+6px)] z-40 hidden min-w-[220px] -translate-x-1/2 border border-line-strong bg-surface px-3 py-2.5 text-left font-sans shadow-card"
          role="tooltip"
          data-testid="status-metrics-compact-token-tip"
        >
          <div class="sm-kpi-title">
            <svg class="sm-kpi-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <ellipse cx="12" cy="6.6" rx="7.2" ry="3.1" />
              <path d="M4.8 6.6v4.7c0 1.7 3.2 3.1 7.2 3.1s7.2-1.4 7.2-3.1V6.6" />
              <path d="M4.8 11.5v4.7c0 1.7 3.2 3.1 7.2 3.1s7.2-1.4 7.2-3.1v-4.7" />
            </svg>
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
      <span class="mx-0.5 h-3 w-px shrink-0 bg-line-strong opacity-90" aria-hidden="true" />
      <button
        ref="compactRunTrigger"
        type="button"
        class="sm-item sm-zone relative inline-flex flex-1 items-center gap-1.5 rounded-md border-0 bg-transparent px-1.5 py-1 text-inherit hover:bg-surface hover:text-txt focus-visible:bg-surface focus-visible:text-txt focus-visible:outline-none"
        data-testid="status-metrics-compact-run"
        :aria-label="runZoneAria()"
        @click="goToRuns"
        @keydown="onActivateKey($event, 'runs')"
        @mouseenter="onCompactZoneEnter('run')"
        @focus="onCompactZoneEnter('run')"
      >
        <svg class="sm-ico block h-[13px] w-[13px] shrink-0 text-txt3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <circle cx="12" cy="12" r="8.2" />
          <path d="M10.3 8.7l5.4 3.3-5.4 3.3z" />
        </svg>
        <span class="sm-val text-[11px] font-semibold leading-none text-txt">{{ running }}</span>
        <span class="text-txt3">/</span>
        <svg class="sm-ico block h-[13px] w-[13px] shrink-0 text-txt3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <path d="M4 7.2h16M4 12h11.5M4 16.8h7" />
        </svg>
        <span class="sm-val text-[11px] font-semibold leading-none text-txt">{{ queued }}</span>
        <span
          v-if="!usePortaledCompactTip"
          class="sm-tip sm-kpi pointer-events-none absolute left-1/2 top-[calc(100%+6px)] z-40 hidden min-w-[180px] -translate-x-1/2 border border-line-strong bg-surface px-3 py-2.5 text-left font-sans shadow-card"
          role="tooltip"
          data-testid="status-metrics-compact-run-tip"
        >
          <div class="sm-kpi-title">
            <svg class="sm-kpi-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <circle cx="12" cy="12" r="8.2" />
              <path d="M10.3 8.7l5.4 3.3-5.4 3.3z" />
            </svg>
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
        class="sm-tip sm-kpi pointer-events-none z-[60] min-w-[220px] border border-line-strong bg-surface px-3 py-2.5 text-left font-sans shadow-card"
        role="tooltip"
        data-testid="status-metrics-compact-tip"
        :data-zone="activeCompactZone ?? undefined"
        data-placement="above"
        :style="compactTipStyle ?? undefined"
      >
        <template v-if="activeCompactZone === 'token'">
          <div class="sm-kpi-title">
            <svg class="sm-kpi-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <ellipse cx="12" cy="6.6" rx="7.2" ry="3.1" />
              <path d="M4.8 6.6v4.7c0 1.7 3.2 3.1 7.2 3.1s7.2-1.4 7.2-3.1V6.6" />
              <path d="M4.8 11.5v4.7c0 1.7 3.2 3.1 7.2 3.1s7.2-1.4 7.2-3.1v-4.7" />
            </svg>
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
            <svg class="sm-kpi-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <circle cx="12" cy="12" r="8.2" />
              <path d="M10.3 8.7l5.4 3.3-5.4 3.3z" />
            </svg>
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
