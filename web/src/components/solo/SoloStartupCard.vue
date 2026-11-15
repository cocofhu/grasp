<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BootChecklist from '@/components/run/BootChecklist.vue'
import { fmtBootSec } from '@/lib/run/bootRecord'
import { isSoloSandboxPulling, soloBootPhase, soloBootStepStates } from '@/lib/solo/soloBoot'
import { serverNow } from '@/lib/api/httpCore'
import type { SoloSession } from '@/lib/api/apiTypes'

/** Boot progress of a session's sandbox, in the workflow boot checklist's look. */
const props = defineProps<{ session: SoloSession }>()
const { t } = useI18n()

// Polls can briefly lose the live container status; never step backwards
// within one sandbox.
const phase = ref(0)
watch(
  () => [props.session.sandbox?.id ?? props.session.sandboxId ?? 0, soloBootPhase(props.session)] as const,
  ([id, derived], prev) => {
    phase.value = prev && prev[0] === id ? Math.max(phase.value, derived) : derived
  },
  { immediate: true },
)

const pulling = computed(() => phase.value === 0 && isSoloSandboxPulling(props.session))
const steps = computed(() =>
  soloBootStepStates(phase.value).map((s) => ({ ...s, label: t(`pages.clarifyBootLoader.checklist.${s.id}`) })),
)
const context = computed(() => {
  if (phase.value >= 2) return t('pages.clarifyBootLoader.stepAcp.title')
  return pulling.value ? t('pages.clarifyBootLoader.stepPulling.title') : t('pages.clarifyBootLoader.stepSandbox.title')
})
const hint = computed(() => t(pulling.value ? 'pages.solo.startup.hintPulling' : 'pages.solo.startup.hint'))

// Elapsed since the sandbox row was created, on the server clock (serverNow).
const nowMs = ref(serverNow())
let tick: ReturnType<typeof setInterval> | undefined
const elapsed = computed(() => {
  const start = Date.parse(props.session.sandbox?.createdAt || '')
  if (!Number.isFinite(start) || start <= 0) return ''
  const sec = Math.floor((nowMs.value - start) / 1000)
  return sec >= 0 ? fmtBootSec(sec) : ''
})
onMounted(() => {
  tick = setInterval(() => { nowMs.value = serverNow() }, 1000)
})
onBeforeUnmount(() => tick && clearInterval(tick))
</script>

<template>
  <BootChecklist
    data-testid="solo-startup-card"
    :data-phase="phase"
    testid-prefix="solo-startup"
    :title="t('pages.clarifyBootLoader.checklist.title')"
    :steps="steps"
    :context="context"
    :elapsed="elapsed"
    :hint="hint"
  />
</template>
