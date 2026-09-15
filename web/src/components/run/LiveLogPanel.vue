<script setup lang="ts">
import { ref, computed, watch, nextTick, onBeforeUnmount, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../ui/Icon.vue'
import AcpStatusPill from './AcpStatusPill.vue'
import type { AcpEvent, McpCall, NodeRunStatus } from '@/lib/shared/types'
import type { MergedAcpEvent } from '@/lib/run/mergeAcpEvents'
import {
  BOOT_STAGE_ORDER,
  BOOT_STAGE_TIMEOUT_MS,
  buildBootStageStates,
  deriveBootPhaseIndex,
  ratchetBootPhaseIndex,
  stageIcon,
  type BootStageId,
  type BootStageState,
  type LiveLogBootSession,
} from '@/lib/run/liveLogBootPhase'
import { allowBootEmptyState, type RehydrateStatus } from '@/lib/run/liveLogRehydrate'

// events are the canonical ACP timeline for the selected node — streamed
// incrementally over the run WebSocket while the node runs (live), or the
// persisted snapshot once it has completed. status lets the empty view explain
// *why* there's nothing yet (waiting to run vs. starting sandbox vs. done).
// busy is the authoritative queue_state.busy flag from the sandbox bridge
// (true while the agent is actively processing the turn); when absent (older
// backend) the panel falls back to the node's run status.
// mcpCalls is the authoritative in/out trace of the built-in MCP tools this
// execution called (write_artifact/read_artifact/set_*/get_*/history…).
// sandboxStatus / sandboxContainerStatus drive the boot-stage empty state.
// bootSession restores confirmed phase / dwell clock across parent remounts
// (e.g. log ↔ sandbox tab) so a timeout banner is not lost after CTA round-trip.
// rehydrateStatus is the REST timeline recovery state (independent of Boot 120s).
const props = defineProps<{
  events: (AcpEvent | MergedAcpEvent)[]
  live?: boolean
  busy?: boolean
  status?: NodeRunStatus
  mcpCalls?: McpCall[]
  hasMore?: boolean
  showConsole?: boolean
  sandboxStatus?: string | null
  sandboxContainerStatus?: string | null
  bootSession?: LiveLogBootSession | null
  rehydrateStatus?: RehydrateStatus
}>()

const emit = defineEmits<{
  'load-earlier': []
  'console-click': []
  'go-sandbox-log': []
  'boot-session': [session: LiveLogBootSession]
  'retry-rehydrate': []
}>()

const { t } = useI18n()

// Which MCP call rows are expanded to show full in/out.
const openCalls = ref<Set<number>>(new Set())
function toggleCall(i: number) {
  const s = new Set(openCalls.value)
  if (s.has(i)) {
    s.delete(i)
  } else {
    s.add(i)
  }
  openCalls.value = s
}

const hasTimelineContent = computed(
  () => props.events.length > 0 || !!(props.mcpCalls && props.mcpCalls.length),
)

// Full-page loading/error only when there is nothing displayable yet.
// With cached/WS content: keep the timeline visible (soft warn on error).
const showRehydrateLoading = computed(
  () => props.rehydrateStatus === 'loading' && !hasTimelineContent.value,
)
const showRehydrateError = computed(
  () => props.rehydrateStatus === 'error' && !hasTimelineContent.value,
)
const showRehydrateWarn = computed(
  () => props.rehydrateStatus === 'error' && hasTimelineContent.value,
)

// Boot empty-wait only after a successful rehydrate — never while loading/failed.
const showBootProgress = computed(
  () =>
    props.status === 'running' &&
    !hasTimelineContent.value &&
    allowBootEmptyState(props.rehydrateStatus) &&
    !showRehydrateLoading.value &&
    !showRehydrateError.value,
)

const confirmedPhase = ref<number | null>(props.bootSession?.confirmedPhase ?? null)
const stageTimedOut = ref(!!props.bootSession?.timedOut)
const stageEnteredAt = ref<number | null>(props.bootSession?.stageEnteredAt ?? null)
let timeoutTimer: ReturnType<typeof setTimeout> | undefined

function emitBootSession() {
  emit('boot-session', {
    confirmedPhase: confirmedPhase.value,
    stageEnteredAt: stageEnteredAt.value,
    timedOut: stageTimedOut.value,
  })
}

function clearTimeoutTimer() {
  if (timeoutTimer) {
    clearTimeout(timeoutTimer)
    timeoutTimer = undefined
  }
}

function armTimeoutTimer() {
  clearTimeoutTimer()
  if (!showBootProgress.value || confirmedPhase.value == null) {
    stageTimedOut.value = false
    stageEnteredAt.value = null
    emitBootSession()
    return
  }
  // Image pull can take many minutes — do not fire the 120s create/ACP dwell (g3.4).
  if (BOOT_STAGE_ORDER[confirmedPhase.value] === 'pulling') {
    stageTimedOut.value = false
    stageEnteredAt.value = stageEnteredAt.value ?? Date.now()
    emitBootSession()
    return
  }
  const entered = stageEnteredAt.value ?? Date.now()
  stageEnteredAt.value = entered
  const remaining = BOOT_STAGE_TIMEOUT_MS - (Date.now() - entered)
  if (remaining <= 0) {
    stageTimedOut.value = true
    emitBootSession()
    return
  }
  stageTimedOut.value = false
  emitBootSession()
  timeoutTimer = setTimeout(() => {
    if (
      showBootProgress.value &&
      confirmedPhase.value != null &&
      BOOT_STAGE_ORDER[confirmedPhase.value] !== 'pulling'
    ) {
      stageTimedOut.value = true
      emitBootSession()
    }
  }, remaining)
}

watch(
  () =>
    [
      props.status,
      props.sandboxStatus,
      props.sandboxContainerStatus,
      hasTimelineContent.value,
    ] as const,
  () => {
    const derived = deriveBootPhaseIndex(
      props.status,
      {
        status: props.sandboxStatus,
        containerStatus: props.sandboxContainerStatus,
      },
      hasTimelineContent.value,
    )
    if (derived == null) {
      confirmedPhase.value = null
      stageTimedOut.value = false
      stageEnteredAt.value = null
      clearTimeoutTimer()
      emitBootSession()
      return
    }
    const prev = confirmedPhase.value
    const next = ratchetBootPhaseIndex(prev, derived)
    if (next !== prev) {
      confirmedPhase.value = next
      stageTimedOut.value = false
      stageEnteredAt.value = Date.now()
    } else if (confirmedPhase.value == null) {
      confirmedPhase.value = next
      stageEnteredAt.value = Date.now()
    }
    armTimeoutTimer()
  },
  { immediate: true },
)

const bootStages = computed(() => {
  const idx = confirmedPhase.value
  if (idx == null) return null
  const states = buildBootStageStates(idx, stageTimedOut.value)
  return BOOT_STAGE_ORDER.map((id, i) => ({
    id,
    state: states[i] as BootStageState,
  }))
})

const activeStageId = computed<BootStageId | null>(() => {
  const idx = confirmedPhase.value
  return idx == null ? null : BOOT_STAGE_ORDER[idx]
})

const stageTitleKey: Record<BootStageId, string> = {
  pulling: 'pages.liveLog.boot.stages.pulling.title',
  creating: 'pages.liveLog.boot.stages.creating.title',
  acp_ready: 'pages.liveLog.boot.stages.acpReady.title',
  first_event: 'pages.liveLog.boot.stages.firstEvent.title',
}
const stageDescKey: Record<BootStageId, string> = {
  pulling: 'pages.liveLog.boot.stages.pulling.desc',
  creating: 'pages.liveLog.boot.stages.creating.desc',
  acp_ready: 'pages.liveLog.boot.stages.acpReady.desc',
  first_event: 'pages.liveLog.boot.stages.firstEvent.desc',
}
const stateBadgeKey: Record<BootStageState, string> = {
  done: 'pages.liveLog.boot.state.done',
  active: 'pages.liveLog.boot.state.active',
  pending: 'pages.liveLog.boot.state.pending',
  timeout: 'pages.liveLog.boot.state.timeout',
}

function stageClass(state: BootStageState): string {
  switch (state) {
    case 'done':
      return 'border-ok/50 bg-ok/10 text-ok'
    case 'active':
      return 'border-accent/60 bg-accent-dim text-accent-2 shadow-[0_0_0_3px_rgb(var(--c-accent-dim))]'
    case 'timeout':
      return 'border-err/55 bg-err/10 text-err'
    default:
      return 'border-line-strong text-txt3 opacity-70'
  }
}

function stageTitleClass(state: BootStageState): string {
  if (state === 'timeout') return 'text-err'
  if (state === 'pending') return 'text-txt3'
  return 'text-txt'
}

function badgeClass(state: BootStageState): string {
  switch (state) {
    case 'done':
      return 'border-ok/40 text-ok'
    case 'active':
      return 'border-accent/45 text-accent-2'
    case 'timeout':
      return 'border-err/45 text-err'
    default:
      return 'border-line text-txt3'
  }
}

// Message shown when there are no events yet, keyed on the node's status.
// Boot progress replaces the running empty hint when applicable.
const emptyHint = computed(() => {
  switch (props.status) {
    case 'pending':
      return { icon: 'clock', text: t('pages.liveLog.empty.pending') }
    case 'running':
      return { icon: 'spinner', text: t('pages.liveLog.empty.running'), spin: true }
    case 'waiting_human':
      return { icon: 'gate', text: t('pages.liveLog.empty.waitingHuman') }
    case 'failed':
      return { icon: 'alert', text: t('pages.liveLog.empty.failed') }
    case 'skipped':
      return { icon: 'dot', text: t('pages.liveLog.empty.skipped') }
    default:
      return { icon: 'dot', text: t('pages.liveLog.empty.default') }
  }
})

const scroller = ref<HTMLElement>()

// Snapshot chip tooltip (hover + keyboard focus).
const snapshotTipOpen = ref(false)
const snapshotTipId = `snapshot-tip-${useId().replace(/:/g, '')}`

// ACP session busy/idle indicator.
//
// The bridge's queue_state.busy is authoritative, but it can dip to false for a
// brief moment between multi-turn re-prompts (催促) while the agent is still on
// the node. So we debounce the transition to idle: busy=true shows 运行中
// immediately, while busy=false only shows 空闲中 after 20s without any further
// busy=true. Crucially this is driven by the real busy flag — NOT by "how long
// since the last event", which would wrongly read a long silent tool call as
// idle.
const IDLE_DEBOUNCE_MS = 20000
const debouncedIdle = ref(false)
let idleTimer: ReturnType<typeof setTimeout> | undefined

watch(
  () => props.busy,
  (b) => {
    if (b === undefined) return
    if (idleTimer) {
      clearTimeout(idleTimer)
      idleTimer = undefined
    }
    if (b) {
      debouncedIdle.value = false
    } else {
      idleTimer = setTimeout(() => {
        debouncedIdle.value = true
      }, IDLE_DEBOUNCE_MS)
    }
  },
  { immediate: true },
)

onBeforeUnmount(() => {
  if (idleTimer) clearTimeout(idleTimer)
  clearTimeoutTimer()
})

const busy = computed(() => {
  if (!props.live || props.status === 'waiting_human') return false
  // Authoritative bridge signal (with idle debounce) when available.
  if (props.busy !== undefined) return !debouncedIdle.value
  // Fallback for backends that don't yet send busy: treat a running node live.
  return props.status === 'running'
})

const kindMeta: Record<string, { icon: string; cls: string; tagKey: string }> = {
  message: { icon: 'chat', cls: 'text-txt', tagKey: 'pages.liveLog.eventKind.reply' },
  thought: { icon: 'sparkles', cls: 'text-txt3 italic', tagKey: 'pages.liveLog.eventKind.thought' },
  plan: { icon: 'doc', cls: 'text-accent-2', tagKey: 'pages.liveLog.eventKind.plan' },
  tool_call: { icon: 'terminal', cls: 'text-info', tagKey: 'pages.liveLog.eventKind.tool' },
  commands: { icon: 'terminal', cls: 'text-txt2', tagKey: 'pages.liveLog.eventKind.command' },
}

function eventKey(e: AcpEvent | MergedAcpEvent, i: number) {
  return 'stableKey' in e && e.stableKey ? e.stableKey : `evt:${i}:${e.kind}:${e.t}`
}

function meta(e: AcpEvent | MergedAcpEvent) {
  if (e.artifact) return { icon: 'artifact', cls: 'text-n-artifact', tag: t('pages.liveLog.eventKind.artifact') }
  if (e.kind === 'tool_call' && /^read_artifact/.test(e.title || '')) return { icon: 'doc', cls: 'text-info', tag: t('pages.liveLog.eventKind.mcpRead') }
  const m = kindMeta[e.kind]
  return m ? { icon: m.icon, cls: m.cls, tag: t(m.tagKey) } : { icon: 'dot', cls: 'text-txt2', tag: e.kind }
}

// Auto-scroll to the newest event as the live stream grows.
watch(
  () => props.events.length,
  async () => {
    await nextTick()
    if (scroller.value) scroller.value.scrollTop = scroller.value.scrollHeight
  },
)
</script>

<template>
  <div class="flex h-full flex-col">
    <div class="flex items-center gap-2 border-b border-line px-4 py-2.5">
      <Icon name="terminal" :size="14" class="text-txt2" />
      <span class="text-xs font-medium text-txt2">{{ t('pages.liveLog.title') }}</span>
      <AcpStatusPill v-if="live" :busy="busy" connected class="ml-0.5" />
      <button
        v-else-if="events.length"
        type="button"
        class="chip relative outline-none focus-visible:border-accent-2 focus-visible:ring-2 focus-visible:ring-accent-dim"
        :aria-describedby="snapshotTipOpen ? snapshotTipId : undefined"
        @mouseenter="snapshotTipOpen = true"
        @mouseleave="snapshotTipOpen = false"
        @focus="snapshotTipOpen = true"
        @blur="snapshotTipOpen = false"
      >
        {{ t('pages.liveLog.snapshot') }}
        <span
          v-if="snapshotTipOpen"
          :id="snapshotTipId"
          role="tooltip"
          class="rounded-xl absolute left-0 top-[calc(100%+6px)] z-20 min-w-[220px] max-w-[280px] border border-line-strong bg-overlay px-2.5 py-2 text-left text-[11px] font-normal leading-snug text-txt2 shadow-lg normal-case"
        >
          {{ t('pages.liveLog.snapshotTip') }}
        </span>
      </button>
      <div class="flex-1" />
      <button
        v-if="showConsole"
        type="button"
        class="rounded border border-line px-2 py-1 text-[11px] text-txt2 hover:border-line-strong"
        @click="emit('console-click')"
      >
        <Icon name="terminal" :size="12" class="-mt-0.5 mr-0.5 inline" />{{ t('common.buttons.console') }}
      </button>
    </div>
    <div v-if="hasMore" class="border-b border-line bg-base px-4 py-2 text-center">
      <button
        type="button"
        class="text-xs text-accent-2 transition hover:text-accent"
        @click="emit('load-earlier')"
      >
        {{ t('common.pagination.loadEarlier') }}
      </button>
    </div>
    <div ref="scroller" class="scroll-area min-h-0 flex-1 space-y-2 overflow-y-auto p-3 font-mono text-[12px]">
      <!-- Rehydrate loading: only when nothing displayable yet (distinct from Boot) -->
      <div
        v-if="showRehydrateLoading"
        data-testid="rehydrate-loading"
        class="flex flex-col items-center justify-center gap-2.5 px-4 py-16 font-sans text-[12px] text-txt3"
      >
        <Icon name="spinner" :size="22" class="animate-spin text-accent" />
        <div>{{ t('pages.liveLog.rehydrate.loading') }}</div>
      </div>
      <!-- True failure: zero events + REST failed — full-page error + manual retry -->
      <div
        v-else-if="showRehydrateError"
        data-testid="rehydrate-error"
        class="rounded-lg mx-4 mt-8 border border-err/40 bg-err/[0.06] px-3.5 py-3.5 font-sans"
      >
        <div class="mb-1.5 flex items-center gap-1.5 text-[12px] font-semibold text-err">
          <Icon name="alert" :size="14" />
          {{ t('pages.liveLog.rehydrate.errorTitle') }}
        </div>
        <p class="m-0 mb-3 text-[11px] leading-snug text-txt2">
          {{ t('pages.liveLog.rehydrate.errorBody') }}
        </p>
        <button
          type="button"
          data-testid="retry-rehydrate"
          class="rounded-lg inline-flex items-center gap-1 border border-transparent bg-accent px-3 py-1.5 text-[12px] font-medium text-white hover:bg-accent-hover"
          @click="emit('retry-rehydrate')"
        >
          {{ t('pages.liveLog.rehydrate.retry') }}
        </button>
      </div>
      <template v-else>
      <!-- Soft warn: rehydrate error but timeline/snapshot still readable (no retry) -->
      <div
        v-if="showRehydrateWarn"
        data-testid="rehydrate-warn"
        class="rounded-lg border border-warn/40 bg-warn/[0.06] px-3.5 py-3 font-sans"
      >
        <div class="mb-1.5 flex items-center gap-1.5 text-[12px] font-semibold text-warn">
          <Icon name="alert" :size="14" />
          {{ t('pages.liveLog.rehydrate.warnTitle') }}
        </div>
        <p class="m-0 mb-2 text-[11px] leading-snug text-txt2">
          {{ t('pages.liveLog.rehydrate.warnBody') }}
        </p>
        <p
          data-testid="rehydrate-snapshot-hint"
          class="rounded-md m-0 border border-info/30 bg-info/[0.07] px-2.5 py-2 text-[11px] leading-snug text-txt2"
        >
          <strong class="font-semibold text-info">{{ t('pages.liveLog.rehydrate.snapshotHintLabel') }}</strong>
          {{ t('pages.liveLog.rehydrate.snapshotHint') }}
        </p>
      </div>
      <div v-if="mcpCalls && mcpCalls.length" class="space-y-1 rounded-md border border-line bg-base/60 p-2">
        <div class="flex items-center gap-1.5 text-[10px] uppercase text-txt3">
          <Icon name="terminal" :size="12" class="text-info" />
          {{ t('pages.liveLog.builtinMcp', { n: mcpCalls.length }) }}
        </div>
        <div v-for="(c, i) in mcpCalls" :key="i" class="rounded border border-line/70">
          <button
            class="flex w-full items-center gap-1.5 px-1.5 py-1 text-left hover:bg-elevated"
            @click="toggleCall(i)"
          >
            <span :class="c.isError ? 'text-err' : 'text-ok'">{{ c.isError ? '✗' : '✓' }}</span>
            <span class="font-semibold" :class="c.isError ? 'text-err' : 'text-info'">{{ c.tool }}</span>
            <span class="ml-auto truncate text-[10px] text-txt3">{{ c.args }}</span>
          </button>
          <div v-if="openCalls.has(i)" class="space-y-1 border-t border-line/70 px-2 py-1.5 text-[11px]">
            <div><span class="text-txt3">{{ t('pages.liveLog.args') }}</span> <span class="whitespace-pre-wrap break-all text-txt2">{{ c.args || t('common.emptyParen') }}</span></div>
            <div><span class="text-txt3">{{ t('pages.liveLog.result') }}</span> <span class="whitespace-pre-wrap break-all" :class="c.isError ? 'text-err' : 'text-txt2'">{{ c.result || t('common.emptyParen') }}</span></div>
          </div>
        </div>
      </div>
      <div v-for="(e, i) in events" :key="eventKey(e, i)" class="flex gap-2">
        <Icon :name="meta(e).icon" :size="13" class="mt-0.5 shrink-0" :class="meta(e).cls" />
        <div class="min-w-0 flex-1">
          <span class="mr-1.5 rounded bg-base px-1 py-0.5 text-[9px] uppercase text-txt3">{{ meta(e).tag }}</span>
          <span class="whitespace-pre-wrap" :class="meta(e).cls">{{ e.title || e.text }}</span>
          <span v-if="e.status === 'running'" class="ml-1.5 text-warn">…</span>
          <span v-else-if="e.status === 'completed'" class="ml-1.5 text-ok">✓</span>
        </div>
      </div>
      <!-- Boot-stage progress: running + empty timeline -->
      <div
        v-if="showBootProgress && bootStages"
        data-testid="live-log-boot"
        class="mx-auto mt-7 flex w-full max-w-[420px] flex-col px-2 font-sans"
      >
        <div
          v-for="(stage, i) in bootStages"
          :key="stage.id"
          class="grid grid-cols-[28px_1fr] gap-x-2.5"
          :data-state="stage.state"
          :data-testid="`boot-stage-${stage.id}`"
        >
          <div class="flex flex-col items-center">
            <div
              class="rounded-lg z-[1] flex h-[22px] w-[22px] shrink-0 items-center justify-center border"
              :class="stageClass(stage.state)"
            >
              <Icon
                :name="stageIcon(stage.state)"
                :size="12"
                :class="stage.state === 'active' ? 'animate-spin' : ''"
              />
            </div>
            <div
              v-if="i < bootStages.length - 1"
              class="min-h-[18px] w-px flex-1"
              :class="stage.state === 'done' ? 'bg-ok/45' : 'bg-line'"
            />
          </div>
          <div class="pb-4 pt-0.5">
            <div class="flex flex-wrap items-center gap-2 text-[12px] font-medium" :class="stageTitleClass(stage.state)">
              <span>{{ t(stageTitleKey[stage.id]) }}</span>
              <span class="rounded-md border px-1.5 py-px text-[10px] uppercase tracking-wide" :class="badgeClass(stage.state)">
                {{ t(stateBadgeKey[stage.state]) }}
              </span>
            </div>
            <div class="mt-0.5 text-[11px] text-txt3">{{ t(stageDescKey[stage.id]) }}</div>
          </div>
        </div>
        <div
          v-if="stageTimedOut && activeStageId"
          data-testid="boot-timeout-banner"
          class="rounded-lg mt-1 border border-err/40 bg-err/[0.06] px-3 py-2.5"
        >
          <div class="mb-1 flex items-center gap-1.5 text-[12px] font-semibold text-err">
            <Icon name="alert" :size="14" />
            {{ t('pages.liveLog.boot.timeout.title') }}
          </div>
          <p class="m-0 text-[11px] text-txt2">
            {{ t('pages.liveLog.boot.timeout.body', { stage: t(stageTitleKey[activeStageId]) }) }}
          </p>
          <button
            type="button"
            data-testid="go-sandbox-log"
            class="rounded-md mt-2 inline-flex items-center gap-1 border border-accent/45 bg-accent-dim px-2.5 py-1 text-[11px] text-accent-2 hover:text-txt"
            @click="emit('go-sandbox-log')"
          >
            {{ t('pages.liveLog.boot.timeout.goSandbox') }}
          </button>
        </div>
      </div>
      <div
        v-else-if="!events.length && !(mcpCalls && mcpCalls.length)"
        class="flex flex-col items-center gap-2 px-1 py-10 text-center text-[12px] text-txt3"
      >
        <Icon :name="emptyHint.icon" :size="20" :class="emptyHint.spin ? 'animate-spin text-accent' : 'text-txt3'" />
        {{ emptyHint.text }}
      </div>
      </template>
    </div>
    <div
      v-if="live && !showRehydrateLoading && !showRehydrateError"
      class="flex shrink-0 items-center gap-2 border-t border-line px-4 py-2.5 pl-4 font-mono text-[12px] text-txt3"
    >
      <span class="h-2 w-2 animate-pulseglow rounded-full" :class="busy ? 'bg-info' : 'bg-ok'" />
      {{ busy ? t('pages.liveLog.receiving') : t('pages.liveLog.idleWaiting') }}
    </div>
  </div>
</template>
