<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../ui/Icon.vue'
import { fmtDuration } from '@/lib/shared/format'
import { bootReadySec, bootSteps, getBootRecord } from '@/lib/run/bootRecord'
import { serverNow } from '@/lib/api/httpCore'

// phase:
//  - 'pending'  节点尚未进入澄清(队列中),静态等待提示
//  - 'starting' 已进入,正在创建沙箱 / 建立 ACP / 生成第一轮问题,展示分阶段动效
// sandboxPhase: gateway-driven lifecycle when known ('pulling' | 'creating' | …).
// When 'pulling', pin the pull-image step instead of cycling (plan g3.1 / f5).
// variant 'inline': rendered as a row inside the chat transcript, in the chat's
// folded tool-row look (使用了 N 个工具). Its three steps follow the node's boot
// record (lib/run/bootRecord.ts): they tick only on real state changes and show
// a time only when the backend exposes both ends.
// phase 'done' (inline only): the folded「环境已就绪」row ClarifyChat shows
// above the first reply, read from the same boot record.
const props = withDefaults(defineProps<{
  phase: 'pending' | 'starting' | 'done'
  sandboxPhase?: string | null
  variant?: 'center' | 'inline'
  /** runId:nodeId:iteration of the boot record (lib/run/bootRecord.ts). */
  bootKey?: string | null
  /** Done row merged into another line (the 已流转到 separator): its title, with 环境已就绪 moved to the meta. */
  title?: string | null
}>(), { sandboxPhase: null, variant: 'center', bootKey: null, title: null })

const { t } = useI18n()

const isPulling = computed(() => (props.sandboxPhase || '').trim().toLowerCase() === 'pulling')

// Center variant (stage pane): cycle through the boot stages, as on main.
const STARTING_STEPS = computed(() => [
  {
    title: t('pages.clarifyBootLoader.stepPulling.title'),
    hint: t('pages.clarifyBootLoader.stepPulling.hint'),
  },
  { title: t('pages.clarifyBootLoader.stepSandbox.title'), hint: t('pages.clarifyBootLoader.stepSandbox.hint') },
  { title: t('pages.clarifyBootLoader.stepRuntime.title'), hint: t('pages.clarifyBootLoader.stepRuntime.hint') },
  { title: t('pages.clarifyBootLoader.stepAcp.title'), hint: t('pages.clarifyBootLoader.stepAcp.hint') },
  { title: t('pages.clarifyBootLoader.stepQuestions.title'), hint: t('pages.clarifyBootLoader.stepQuestions.hint') },
])
const step = ref(1) // non-pulling default: workspace step (index 1); pulling pins 0
let timer: number | undefined
const cycles = computed(() => props.variant !== 'inline')

function prefersReducedMotion() {
  return typeof window !== 'undefined'
    && typeof window.matchMedia === 'function'
    && window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

function startCycling() {
  step.value = isPulling.value ? 0 : 1
  if (timer) clearInterval(timer)
  if (!cycles.value || prefersReducedMotion() || isPulling.value) {
    timer = undefined
    return
  }
  timer = window.setInterval(() => {
    // Advance but hold on the last step so it doesn't loop back to the start.
    if (step.value < STARTING_STEPS.value.length - 1) step.value++
  }, 2600)
}
function stopCycling() {
  if (timer) clearInterval(timer)
  timer = undefined
}

watch(
  () => props.phase,
  (p) => (p === 'starting' ? startCycling() : stopCycling()),
)
watch(isPulling, () => {
  if (props.phase === 'starting') startCycling()
})

// Inline checklist: driven by the boot record only (no pacing timer).
const record = computed(() => getBootRecord(props.bootKey))
const pulling = computed(() => isPulling.value || record.value?.sandboxStatus === 'pulling')
function fmtSec(sec: number) {
  return sec < 60 ? `${sec}s` : fmtDuration(sec)
}
const checklist = computed(() =>
  bootSteps(record.value).map((s) => ({
    id: s.id,
    label: t(`pages.clarifyBootLoader.checklist.${s.id}`),
    state: s.state,
    sec: s.state === 'done' && s.sec != null ? fmtSec(s.sec) : '',
  })),
)
const currentCheck = computed(() => checklist.value.find((c) => c.state === 'running') || null)
/** What the backend is doing now, from the real lifecycle status. */
const currentTitle = computed(() => {
  if (!currentCheck.value) return ''
  if (currentCheck.value.id === 'agent') return STARTING_STEPS.value[3]!.title
  return pulling.value ? STARTING_STEPS.value[0]!.title : STARTING_STEPS.value[1]!.title
})
// Elapsed since the node started (server timestamp), measured on the server
// clock (serverNow) so client clock skew does not shift it. Hidden when unknown.
const nowMs = ref(serverNow())
let tick: number | undefined
const elapsedLabel = computed(() => {
  const start = record.value?.nodeStartedAt
  if (start == null) return ''
  const sec = Math.floor((nowMs.value - start) / 1000)
  return sec >= 0 ? fmtSec(sec) : ''
})

onMounted(() => {
  if (props.phase === 'starting') startCycling()
  if (props.phase === 'starting' && props.variant === 'inline') {
    tick = window.setInterval(() => { nowMs.value = serverNow() }, 1000)
  }
})
onBeforeUnmount(() => {
  stopCycling()
  if (tick) clearInterval(tick)
})
const checklistOpen = ref(true)

// Done row (ClarifyChat): folded by default, expands to the three steps.
const doneOpen = ref(false)
const doneReady = computed(() => !!record.value?.live)
const doneMeta = computed(() => {
  const n = checklist.value.length
  const sec = bootReadySec(record.value)
  const meta = sec == null
    ? t('pages.clarifyBootLoader.checklist.doneMetaNoTime', { n })
    : t('pages.clarifyBootLoader.checklist.doneMeta', { n, d: fmtSec(sec) })
  return props.title ? `${t('pages.clarifyBootLoader.checklist.done')} · ${meta}` : meta
})
</script>

<template>
  <!--
    Starting: the chat's folded tool row (使用了 N 个工具) look — one head line
    with spinner, title, elapsed and the step in progress, opened onto a
    three-step list that ticks off as boot advances.
  -->
  <div
    v-if="variant === 'inline' && phase === 'starting'"
    class="agent-tools chat-meta w-full min-w-0"
    data-testid="clarify-boot-loader"
    data-variant="inline"
    :data-sandbox-phase="sandboxPhase || ''"
  >
    <!-- Live region for the step in progress; the toggle keeps plain button semantics. -->
    <span class="sr-only" role="status" aria-live="polite" data-testid="clarify-boot-status">{{ currentTitle }}</span>
    <button
      type="button"
      class="agent-tools-head"
      :aria-expanded="checklistOpen"
      data-testid="clarify-boot-head"
      @click="checklistOpen = !checklistOpen"
    >
      <Icon name="spinner" :size="16" class="agent-tool-icon agent-tool-running animate-spin" aria-hidden="true" />
      <span class="agent-tools-count" data-testid="clarify-boot-title">{{ t('pages.clarifyBootLoader.checklist.title') }}</span>
      <span v-if="elapsedLabel" class="tabular-nums text-txt3" data-testid="clarify-boot-elapsed">{{ elapsedLabel }}</span>
      <span class="agent-tools-context text-txt3" data-testid="clarify-boot-context">{{ currentTitle }}</span>
      <Icon name="chevron-down" :size="16" class="agent-tool-chevron" :class="{ 'rotate-180': checklistOpen }" aria-hidden="true" />
    </button>
    <ul v-if="checklistOpen" class="agent-tool-list" data-testid="clarify-boot-checklist">
      <li
        v-for="c in checklist"
        :key="c.id"
        class="agent-tool-row-main"
        :data-state="c.state"
        data-testid="clarify-boot-step"
      >
        <!-- type=transition: the spinner's infinite spin must not hold the swap to ✓. -->
        <Transition name="startfade" mode="out-in" type="transition">
          <Icon v-if="c.state === 'running'" key="r" name="spinner" :size="16" class="agent-tool-icon agent-tool-running animate-spin" aria-hidden="true" />
          <Icon v-else-if="c.state === 'done'" key="d" name="check" :size="16" class="agent-tool-icon" aria-hidden="true" />
          <Icon v-else key="p" name="dot" :size="16" class="agent-tool-icon text-txt3/60" aria-hidden="true" />
        </Transition>
        <span class="agent-tool-label" :class="c.state === 'pending' ? 'text-txt3' : ''">{{ c.label }}</span>
        <span v-if="c.sec" class="tabular-nums text-[11px] text-txt3">{{ c.sec }}</span>
      </li>
    </ul>
    <p v-if="pulling && currentCheck?.id === 'sandbox'" class="chat-meta m-0 pl-[34px] text-txt3">{{ STARTING_STEPS[0].hint }}</p>
  </div>
  <!-- Done (inside ClarifyChat): same row, folded, check icon. -->
  <Transition v-else-if="variant === 'inline' && phase === 'done'" name="startfade" appear>
    <div v-if="doneReady" class="agent-tools chat-meta w-full min-w-0" :class="title ? '' : 'mb-2'" data-testid="clarify-boot-done">
      <button
        type="button"
        class="agent-tools-head"
        :aria-expanded="doneOpen"
        data-testid="clarify-boot-done-head"
        @click="doneOpen = !doneOpen"
      >
        <Icon name="check" :size="16" class="agent-tool-icon" aria-hidden="true" />
        <span class="agent-tools-count" data-testid="clarify-boot-done-title">{{ title || t('pages.clarifyBootLoader.checklist.done') }}</span>
        <span class="tabular-nums text-txt3" data-testid="clarify-boot-done-meta">{{ doneMeta }}</span>
        <span class="agent-tools-context text-txt3" />
        <Icon name="chevron-down" :size="16" class="agent-tool-chevron" :class="{ 'rotate-180': doneOpen }" aria-hidden="true" />
      </button>
      <ul v-if="doneOpen" class="agent-tool-list">
        <li v-for="c in checklist" :key="c.id" class="agent-tool-row-main" data-state="done" data-testid="clarify-boot-done-step">
          <Icon name="check" :size="16" class="agent-tool-icon" aria-hidden="true" />
          <span class="agent-tool-label">{{ c.label }}</span>
          <span v-if="c.sec" class="tabular-nums text-[11px] text-txt3">{{ c.sec }}</span>
        </li>
      </ul>
    </div>
  </Transition>
  <div
    v-else
    class="flex h-full flex-col items-center justify-center gap-3 px-6 text-center"
    data-testid="clarify-boot-loader"
    :data-sandbox-phase="sandboxPhase || ''"
  >
    <template v-if="phase === 'pending'">
      <Icon name="dot" :size="22" class="animate-pulseglow text-n-clarify" />
      <p class="text-[13px] text-txt2">{{ t('pages.clarifyBootLoader.pending') }}</p>
      <p class="max-w-md text-[11px] text-txt3/70">{{ t('pages.clarifyBootLoader.pendingHint') }}</p>
    </template>
    <template v-else>
      <Icon name="spinner" :size="30" class="animate-spin text-n-clarify" />
      <Transition name="startfade" mode="out-in">
        <p :key="step" class="text-[13px] text-txt2" data-testid="clarify-boot-title">{{ STARTING_STEPS[step].title }}</p>
      </Transition>
      <Transition name="startfade" mode="out-in">
        <p :key="'h' + step" class="max-w-md text-[12px] text-txt3">{{ STARTING_STEPS[step].hint }}</p>
      </Transition>
      <div class="mt-1 flex items-center gap-1.5" data-testid="clarify-boot-progress">
        <span
          v-for="(_, i) in STARTING_STEPS"
          :key="i"
          class="h-1.5 rounded-full transition-all duration-300"
          :class="i === step ? 'w-5 bg-n-clarify' : i < step ? 'w-1.5 bg-n-clarify/50' : 'w-1.5 bg-line'"
        />
      </div>
    </template>
  </div>
</template>

<style scoped>
.startfade-enter-active,
.startfade-leave-active {
  transition: opacity 0.25s ease, transform 0.25s ease;
}
.startfade-enter-from {
  opacity: 0;
  transform: translateY(4px);
}
.startfade-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}

@media (prefers-reduced-motion: reduce) {
  .startfade-enter-active,
  .startfade-leave-active {
    transition: none;
  }
}
</style>
<style scoped src="./agentToolGroup.css"></style>
