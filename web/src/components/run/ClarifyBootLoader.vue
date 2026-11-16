<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../ui/Icon.vue'
import BootChecklist from './BootChecklist.vue'
import { bootReadySec, bootSteps, fmtBootSec as fmtSec, getBootRecord } from '@/lib/run/bootRecord'
import { serverNow } from '@/lib/api/httpCore'

// A node's sandbox boot as a row inside the chat transcript, in the chat's
// folded tool-row look (使用了 N 个工具). Its three steps follow the node's boot
// record (lib/run/bootRecord.ts): they tick only on real state changes and show
// a time only when the backend exposes both ends.
//  - 'starting' the live checklist while the sandbox / ACP comes up
//  - 'done'     the folded「环境已就绪」row ClarifyChat shows above the first
//               reply, read from the same boot record
// sandboxPhase: gateway-driven lifecycle when known ('pulling' | 'creating' | …).
const props = withDefaults(defineProps<{
  phase: 'starting' | 'done'
  sandboxPhase?: string | null
  /** runId:nodeId:iteration of the boot record (lib/run/bootRecord.ts). */
  bootKey?: string | null
  /** Done row merged into another line (the 已流转到 separator): its title, with 环境已就绪 moved to the meta. */
  title?: string | null
}>(), { sandboxPhase: null, bootKey: null, title: null })

const { t } = useI18n()

const record = computed(() => getBootRecord(props.bootKey))
const pulling = computed(() => (props.sandboxPhase || '').trim().toLowerCase() === 'pulling' || record.value?.sandboxStatus === 'pulling')
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
  if (currentCheck.value.id === 'agent') return t('pages.clarifyBootLoader.stepAcp.title')
  return pulling.value ? t('pages.clarifyBootLoader.stepPulling.title') : t('pages.clarifyBootLoader.stepSandbox.title')
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
  if (props.phase === 'starting') tick = window.setInterval(() => { nowMs.value = serverNow() }, 1000)
})
onBeforeUnmount(() => {
  if (tick) clearInterval(tick)
})

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
    Starting: one head line with spinner, title, elapsed and the step in
    progress, opened onto a three-step list that ticks off as boot advances.
  -->
  <BootChecklist
    v-if="phase === 'starting'"
    data-testid="clarify-boot-loader"
    :data-sandbox-phase="sandboxPhase || ''"
    :title="t('pages.clarifyBootLoader.checklist.title')"
    :steps="checklist"
    :context="currentTitle"
    :elapsed="elapsedLabel"
    :hint="pulling && currentCheck?.id === 'sandbox' ? t('pages.clarifyBootLoader.stepPulling.hint') : ''"
  />
  <!-- Done (inside ClarifyChat): same row, folded, check icon. -->
  <Transition v-else name="startfade" appear>
    <div v-if="doneReady" class="clarify-boot-done agent-tools chat-meta w-full min-w-0" :class="title ? '' : 'mb-2'" data-testid="clarify-boot-done">
      <button
        type="button"
        class="agent-tools-head grasp-status-host"
        :aria-expanded="doneOpen"
        data-testid="clarify-boot-done-head"
        @click="doneOpen = !doneOpen"
      >
        <Icon name="check-draw" :size="16" class="agent-tool-icon text-ok" aria-hidden="true" />
        <span class="agent-tools-count" data-testid="clarify-boot-done-title">{{ title || t('pages.clarifyBootLoader.checklist.done') }}</span>
        <span class="tabular-nums text-txt3" data-testid="clarify-boot-done-meta">{{ doneMeta }}</span>
        <span class="agent-tools-context text-txt3" />
        <Icon :interactive="false" name="chevron-down" :size="12" class="agent-tool-chevron" :class="{ 'rotate-180': doneOpen }" aria-hidden="true" />
      </button>
      <ul v-if="doneOpen" class="agent-tool-list">
        <li v-for="c in checklist" :key="c.id" class="agent-tool-row-main" data-state="done" data-testid="clarify-boot-done-step">
          <Icon name="check-draw" :size="16" class="agent-tool-icon text-ok" aria-hidden="true" />
          <span class="agent-tool-label">{{ c.label }}</span>
          <span v-if="c.sec" class="tabular-nums text-[11px] text-txt3">{{ c.sec }}</span>
        </li>
      </ul>
    </div>
  </Transition>
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
