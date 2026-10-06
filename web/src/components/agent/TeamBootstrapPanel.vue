<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppButton from '@/components/ui/AppButton.vue'
import { api, type TeamBootstrapSession } from '@/lib/api/api'

const props = defineProps<{
  sessionId: string
}>()

const emit = defineEmits<{
  openPm: [name: string]
  selectPm: [name: string]
  done: []
  refresh: []
}>()

const { t } = useI18n()
const session = ref<TeamBootstrapSession | null>(null)
const pollError = ref('')
const retrying = ref(false)
let timer: ReturnType<typeof setInterval> | null = null
let lastResourceCount = 0
let highlightedPm = false

const badgeClass = computed(() => {
  const s = session.value?.status
  if (s === 'ready') return 'border-ok/40 bg-ok/10 text-ok'
  if (s === 'failed') return 'border-err/40 bg-err/10 text-err'
  if (s === 'running') return 'border-ok/40 bg-ok/10 text-ok'
  if (s === 'pulling' || isPullingSandbox.value) return 'border-warn/40 bg-warn/10 text-warn'
  return 'border-warn/40 bg-warn/10 text-warn'
})

/** True when session or nested sandbox lifecycle reports image pull (g3.3). */
const isPullingSandbox = computed(() => {
  const s = session.value
  if (!s) return false
  if (s.status === 'pulling') return true
  return (s.sandboxStatus || '').trim().toLowerCase() === 'pulling'
})

const badgeText = computed(() => {
  const s = session.value?.status || 'starting'
  if (s === 'ready') {
    const n = session.value?.agentNames?.length || 0
    return t('pages.agentStudio.teamWizard.progress.ready', { n })
  }
  if (isPullingSandbox.value) {
    return t('pages.agentStudio.teamWizard.progress.pulling')
  }
  return t(`pages.agentStudio.teamWizard.progress.${s}`)
})

async function poll() {
  try {
    const s = await api.getAgentTeamBootstrap(props.sessionId)
    session.value = s
    pollError.value = ''
    if ((s.resources?.length || 0) !== lastResourceCount) {
      lastResourceCount = s.resources?.length || 0
      emit('refresh')
    }
    if (s.status === 'ready' || s.status === 'failed') {
      emit('refresh')
      if (s.status === 'ready' && s.pmAgent && !highlightedPm) {
        highlightedPm = true
        emit('selectPm', s.pmAgent)
      }
      stop()
    }
  } catch (e: any) {
    pollError.value = e?.message || String(e)
  }
}

function stop() {
  if (timer) {
    clearInterval(timer)
    timer = null
  }
}

function startPolling() {
  stop()
  void poll()
  timer = setInterval(() => void poll(), 800)
}

async function onRetry() {
  if (retrying.value) return
  retrying.value = true
  pollError.value = ''
  try {
    session.value = await api.retryAgentTeamBootstrap(props.sessionId)
    lastResourceCount = session.value.resources?.length || 0
    emit('refresh')
    startPolling()
  } catch (e: any) {
    pollError.value = e?.message || String(e)
  } finally {
    retrying.value = false
  }
}

onMounted(startPolling)

onBeforeUnmount(stop)

watch(
  () => props.sessionId,
  () => {
    lastResourceCount = 0
    highlightedPm = false
    startPolling()
  },
)

function lineClass(kind: string) {
  if (kind === 'ok') return 'text-ok'
  if (kind === 'err') return 'text-err'
  if (kind === 'warn') return 'text-warn'
  if (kind === 'mcp') return 'mcp-block'
  return 'text-txt3'
}
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col" aria-live="polite">
    <div class="flex shrink-0 items-center justify-between gap-3 border-b border-line px-4 py-3">
      <div class="min-w-0">
        <h2 class="m-0 truncate text-[15px] font-semibold text-txt">
          {{ t('pages.agentStudio.teamWizard.progress.title', { name: session?.pmAgent || '…' }) }}
        </h2>
        <p class="m-0 mt-0.5 text-[12px] text-txt3">
          {{ t('pages.agentStudio.teamWizard.progress.sub') }}
        </p>
      </div>
      <span class="rounded-md shrink-0 border px-2 py-1 text-[11px]" :class="badgeClass">{{ badgeText }}</span>
    </div>

    <div
      v-if="session?.status === 'failed' || pollError"
      class="shrink-0 border-b border-err/35 bg-err/10 px-4 py-2 text-[12px] text-err"
    >
      {{ session?.error || pollError }}
    </div>

    <div
      v-else-if="isPullingSandbox"
      class="shrink-0 border-b border-warn/35 bg-warn/10 px-4 py-2 text-[12px] text-warn"
      data-testid="team-bootstrap-pulling"
      aria-live="polite"
    >
      {{ t('pages.agentStudio.teamWizard.progress.pullingHint') }}
    </div>

    <div class="grid min-h-0 flex-1 grid-cols-1 lg:grid-cols-[1.15fr_0.85fr]">
      <div class="scroll-area min-h-0 overflow-y-auto border-r border-line p-4 font-mono text-[12px] leading-5">
        <div
          v-for="(ev, i) in session?.events || []"
          :key="i"
          class="mb-2 whitespace-pre-wrap"
          :class="lineClass(ev.kind)"
        >
          <template v-if="ev.kind === 'mcp'">
            <div class="rounded-lg mcp-block border border-dashed border-warn/45 bg-warn/10 px-3 py-2 text-warn">
              {{ ev.message }}
            </div>
          </template>
          <template v-else>{{ ev.message }}</template>
        </div>
        <div v-if="!session?.events?.length" class="text-txt3">
          {{ t('pages.agentStudio.teamWizard.progress.waiting') }}
        </div>
      </div>

      <div class="scroll-area min-h-0 overflow-y-auto p-4">
        <h3 class="mb-3 text-[13px] font-semibold text-txt2">
          {{ t('pages.agentStudio.teamWizard.progress.resources') }}
        </h3>
        <div
          v-for="(r, i) in session?.resources || []"
          :key="i"
          class="rounded-lg mb-2 border border-line bg-elevated px-3 py-2 text-[12px]"
          :class="session?.status === 'ready' ? 'border-ok/35' : ''"
        >
          <div class="font-semibold text-txt">{{ r.name }}</div>
          <div class="text-txt3">{{ r.kind }}{{ r.detail ? ' · ' + r.detail : '' }}</div>
        </div>
      </div>
    </div>

    <div class="flex shrink-0 items-center justify-end gap-2 border-t border-line px-4 py-3">
      <AppButton
        v-if="session?.status === 'failed'"
        variant="primary"
        :disabled="retrying"
        @click="onRetry"
      >
        {{ retrying ? t('pages.agentStudio.teamWizard.submitting') : t('pages.agentStudio.teamWizard.progress.retry') }}
      </AppButton>
      <AppButton
        v-if="session?.status === 'ready' && session.pmAgent"
        variant="primary"
        @click="emit('openPm', session.pmAgent)"
      >
        {{ t('pages.agentStudio.teamWizard.progress.openPm') }}
      </AppButton>
      <AppButton
        v-if="session?.status === 'ready'"
        variant="outline"
        @click="emit('done')"
      >
        {{ t('pages.agentStudio.teamWizard.progress.done') }}
      </AppButton>
      <AppButton
        v-else-if="session?.status === 'failed'"
        variant="outline"
        @click="emit('done')"
      >
        {{ t('pages.agentStudio.teamWizard.progress.keepCreated') }}
      </AppButton>
    </div>
  </div>
</template>
