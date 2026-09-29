<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import ReactArtifactStage from '@/components/run/ReactArtifactStage.vue'
import {
  clearEmbedSession,
  loadEmbedSession,
  parseEmbedThemeFromHash,
  parseEmbedThemeMessage,
  parseEmbedTicketFromHash,
  redeemEmbedTicket,
  saveEmbedSession,
} from '@/lib/inbox/embedChat'
import { publicGateApi, type PublicGatePreview } from '@/lib/inbox/gateShareLink'
import { isAbortError } from '@/lib/run/liveLogRehydrate'
import { isFeedbackArtifactName } from '@/lib/run/reactArtifactPreview'
import { setThemeOverride } from '@/lib/shared/theme'
import type { Artifact, NodeType, Run } from '@/lib/shared/types'

const PUBLIC_SHARE_RUN_ID = 'public-share'
const POLL_MS = 4000

const { t } = useI18n()
const route = useRoute()
const runId = String(route.params.runId || '')
const nodeId = String(route.params.nodeId || '')

const phase = ref<'connecting' | 'ready' | 'expired' | 'network'>('connecting')
const token = ref('')
const preview = ref<PublicGatePreview | null>(null)
const artifacts = ref<Artifact[]>([])
const runGraph = ref<Run | null>(null)
const loadErr = ref('')

let pollTimer: ReturnType<typeof setInterval> | null = null
let artifactsAbort: AbortController | null = null
let previewAbort: AbortController | null = null

const nodeType = computed(() => String(preview.value?.nodeType || '').trim())
/** Preview Artifacts window: pipeline products only — drop feedback.* / feedback_index.json. */
const stageArtifacts = computed(() => artifacts.value.filter((a) => !isFeedbackArtifactName(a.name)))
const previewPin = computed(() => {
  const pin = preview.value?.productName || preview.value?.structured?.name || ''
  if (pin && stageArtifacts.value.some((a) => a.name === pin)) return pin
  // Never fall back to a feedback file when nothing is pinned.
  return stageArtifacts.value[0]?.name || ''
})

function takeHash(): string {
  const hash = window.location.hash
  if (hash) history.replaceState(history.state, '', `${window.location.pathname}${window.location.search}`)
  return hash
}

async function connect() {
  phase.value = 'connecting'
  const hash = takeHash()
  const theme = parseEmbedThemeFromHash(hash)
  if (theme) setThemeOverride(theme)
  const ticket = parseEmbedTicketFromHash(hash)
  if (ticket) {
    try {
      const s = await redeemEmbedTicket(ticket, runId, nodeId)
      if (s) {
        saveEmbedSession(runId, nodeId, s)
        token.value = s.token
        phase.value = 'ready'
        await refresh({ silent: false })
        startPoll()
        return
      }
    } catch {
      if (!loadEmbedSession(runId, nodeId)) {
        phase.value = 'network'
        return
      }
    }
  }
  const stored = loadEmbedSession(runId, nodeId)
  if (stored) {
    token.value = stored.token
    phase.value = 'ready'
    await refresh({ silent: false })
    startPoll()
    return
  }
  phase.value = 'expired'
}

function stopPoll() {
  if (pollTimer != null) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}

function startPoll() {
  stopPoll()
  pollTimer = setInterval(() => void refresh({ silent: true }), POLL_MS)
}

function abortLoads() {
  artifactsAbort?.abort()
  artifactsAbort = null
  previewAbort?.abort()
  previewAbort = null
}

async function refresh(opts: { silent: boolean }) {
  if (!token.value) return
  previewAbort?.abort()
  previewAbort = new AbortController()
  const previewSignal = previewAbort.signal
  try {
    const next = await publicGateApi.preview(token.value, previewSignal, { silent: opts.silent })
    if (previewSignal.aborted) return
    if (next.status === 'invalid' || next.status === 'expired' || next.status === 'revoked' || next.status === 'used') {
      clearEmbedSession(runId, nodeId)
      token.value = ''
      phase.value = 'expired'
      stopPoll()
      return
    }
    preview.value = next
  } catch (e) {
    if (isAbortError(e) || previewSignal.aborted) return
    if (!opts.silent) {
      loadErr.value = t('pages.embedArtifacts.loadError')
    }
  }

  if (!token.value) return
  artifactsAbort?.abort()
  artifactsAbort = new AbortController()
  const signal = artifactsAbort.signal
  try {
    const res = await publicGateApi.artifacts(token.value, signal)
    if (signal.aborted) return
    if (res.status !== 'active') {
      if (!opts.silent) artifacts.value = []
      return
    }
    artifacts.value = (res.artifacts || []).map((a) => ({
      id: a.id,
      name: a.name,
      kind: a.kind,
      nodeId: a.nodeId,
      runId: PUBLIC_SHARE_RUN_ID,
      workflowName: '',
      sizeBytes: a.sizeBytes,
      createdAt: a.createdAt,
      updatedAt: a.updatedAt,
      revision: a.revision,
    }))
    if (res.nodes?.length) {
      runGraph.value = {
        id: PUBLIC_SHARE_RUN_ID,
        workflowId: '',
        workflowName: '',
        status: 'waiting_human',
        trigger: '',
        startedAt: '',
        durationSec: 0,
        progress: 0,
        branch: '',
        nodeRuns: {},
        nodes: res.nodes.map((n) => ({
          id: n.id,
          type: n.type as NodeType,
          label: n.label || n.id,
          position: { x: 0, y: 0 },
          config: n.config || {},
        })),
        edges: [],
        artifacts: artifacts.value.filter((a) => !isFeedbackArtifactName(a.name)),
        vars: [],
        trace: [],
        priority: 'normal',
        tags: [],
      }
    } else {
      runGraph.value = null
    }
    loadErr.value = ''
  } catch (e) {
    if (isAbortError(e) || signal.aborted) return
    if (!opts.silent) {
      loadErr.value = t('pages.embedArtifacts.loadError')
      artifacts.value = []
      runGraph.value = null
    }
  }
}

function onMessage(e: MessageEvent) {
  if (window.parent === window || e.source !== window.parent) return
  const origin = window.location.ancestorOrigins?.[0]
  if (origin && e.origin !== origin) return
  const theme = parseEmbedThemeMessage(e.data)
  if (theme) setThemeOverride(theme)
}

onMounted(() => {
  window.addEventListener('message', onMessage)
  void connect()
})
onUnmounted(() => {
  window.removeEventListener('message', onMessage)
  stopPoll()
  abortLoads()
  setThemeOverride(null)
})
</script>

<template>
  <div class="flex h-screen flex-col overflow-hidden bg-base text-txt" data-testid="embed-artifacts-root">
    <ReactArtifactStage
      v-if="phase === 'ready' && token"
      class="min-h-0 flex-1"
      :artifacts="stageArtifacts"
      :preview-artifact="previewPin"
      :run="runGraph"
      :node-id="nodeId"
      :node-type="nodeType"
      remote-kind="off"
      hide-app-preview
      :token="token"
      :annotatable="false"
    />
    <div
      v-else-if="phase === 'connecting'"
      class="flex flex-1 flex-col items-center justify-center gap-3 text-center"
      role="status"
      aria-busy="true"
      data-testid="embed-artifacts-connecting"
    >
      <Icon name="spinner" :size="24" class="animate-spin text-accent" aria-hidden="true" />
      <p class="text-sm text-txt3">{{ t('pages.embedArtifacts.connecting') }}</p>
    </div>
    <div
      v-else-if="phase === 'network'"
      class="flex flex-1 flex-col items-center justify-center gap-3 px-6 text-center"
      role="alert"
      data-testid="embed-artifacts-network"
    >
      <p class="max-w-[36ch] text-sm text-txt2">{{ t('pages.embedArtifacts.networkError') }}</p>
    </div>
    <div
      v-else
      class="flex flex-1 flex-col items-center justify-center gap-3 px-6 text-center"
      role="status"
      data-testid="embed-artifacts-expired"
    >
      <h1 class="text-base font-semibold">{{ t('pages.embedArtifacts.expiredTitle') }}</h1>
      <p class="max-w-[36ch] text-sm text-txt3">{{ t('pages.embedArtifacts.expiredHint') }}</p>
    </div>
    <p
      v-if="phase === 'ready' && loadErr"
      class="shrink-0 border-t border-line px-3 py-2 text-center text-[11px] text-txt3"
      data-testid="embed-artifacts-load-error"
    >
      {{ loadErr }}
    </p>
  </div>
</template>
