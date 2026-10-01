<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../../ui/Icon.vue'
import LlmPromptBubble from './LlmPromptBubble.vue'
import LlmAnswerBubble from './LlmAnswerBubble.vue'
import LlmSystemRow from './LlmSystemRow.vue'
import { api } from '@/lib/api/api'
import type { LlmTranscriptResponse } from '@/lib/api/apiTypes'
import { buildLlmTranscript, summarizeLlmTranscript } from '@/lib/run/llmTranscript'
import { resolveNodeDisplayLabel } from '@/lib/run/resolveNodeDisplayLabel'
import { fmtTokenCount } from '@/lib/run/tokenUsage'
import type { AcpEvent, Run, WFNode } from '@/lib/shared/types'

const PREFS_KEY = 'grasp.llmTranscript.prefs'
const POLL_MS = 4000
const REFETCH_DEBOUNCE_MS = 600
const FOLLOW_SLACK_PX = 120

const props = defineProps<{
  run: Run
  nodes: WFNode[]
  liveEvents?: Record<string, AcpEvent[] | undefined>
}>()

const emit = defineEmits<{
  locate: [nodeId: string, execIdx: number]
  'open-artifacts': []
}>()

const { t, locale } = useI18n()

interface Prefs {
  expandAll: boolean
  showThought: boolean
  follow: boolean
}

function readPrefs(): Prefs {
  const base: Prefs = { expandAll: false, showThought: true, follow: true }
  try {
    const raw = localStorage.getItem(PREFS_KEY)
    return raw ? { ...base, ...JSON.parse(raw) } : base
  } catch {
    return base
  }
}

const prefs = ref<Prefs>(readPrefs())
watch(
  prefs,
  (v) => {
    try {
      localStorage.setItem(PREFS_KEY, JSON.stringify(v))
    } catch {
      /* storage unavailable */
    }
  },
  { deep: true },
)

// ---- data ----
const transcript = ref<LlmTranscriptResponse | null>(null)
const loading = ref(false)
const loadError = ref(false)
let ctrl: AbortController | null = null

async function load() {
  const runId = props.run?.id
  if (!runId) return
  ctrl?.abort()
  const mine = new AbortController()
  ctrl = mine
  loading.value = !transcript.value
  try {
    const res = await api.llmTranscript(runId, { signal: mine.signal })
    if (ctrl !== mine) return
    transcript.value = res
    loadError.value = false
  } catch {
    if (mine.signal.aborted) return
    loadError.value = true
  } finally {
    if (ctrl === mine) loading.value = false
  }
}

const runActive = computed(() => ['running', 'waiting_human', 'queued'].includes(props.run?.status))

watch(
  () => props.run?.id,
  () => {
    transcript.value = null
    loadError.value = false
    void load()
  },
  { immediate: true },
)

// Run detail reloads on WS trace/status frames; follow it with a debounced refetch.
const runSignature = computed(() => {
  const r = props.run
  if (!r) return ''
  let events = 0
  for (const list of Object.values(r.nodeExecutions || {})) {
    for (const ex of list) events += ex.events?.length || 0
  }
  return `${r.status}|${r.trace?.length || 0}|${events}`
})
let debounce: ReturnType<typeof setTimeout> | undefined
watch(runSignature, () => {
  if (debounce) clearTimeout(debounce)
  debounce = setTimeout(() => void load(), REFETCH_DEBOUNCE_MS)
})

// Inflight prompts change without a run reload (a new turn in the same node).
let poll: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  poll = setInterval(() => {
    if (runActive.value && document.visibilityState !== 'hidden') void load()
  }, POLL_MS)
})
onBeforeUnmount(() => {
  ctrl?.abort()
  if (poll) clearInterval(poll)
  if (debounce) clearTimeout(debounce)
})

// ---- view model ----
const nodeById = computed<Record<string, WFNode>>(() => {
  const m: Record<string, WFNode> = {}
  for (const n of props.nodes || []) m[n.id] = n
  return m
})

function nodeInfo(id: string) {
  const n = nodeById.value[id]
  return {
    label: n ? resolveNodeDisplayLabel(n.label, n.type, t, { nodeId: id }) : id,
    type: n?.type || 'agent',
  }
}

const items = computed(() => {
  void locale.value
  return buildLlmTranscript({ run: props.run, transcript: transcript.value, liveEvents: props.liveEvents, nodeInfo })
})
const summary = computed(() => summarizeLlmTranscript(items.value))
const nodeItems = computed(() => items.value.filter((i) => i.type === 'node'))
const hasTurns = computed(() => items.value.some((i) => i.type === 'turn'))
const previewOnly = computed(() => !transcript.value)

// ---- scrolling ----
const scroller = ref<HTMLElement | null>(null)
const nearBottom = ref(true)

function onScroll() {
  const el = scroller.value
  if (!el) return
  nearBottom.value = el.scrollHeight - el.scrollTop - el.clientHeight < FOLLOW_SLACK_PX
}

function scrollToBottom() {
  const el = scroller.value
  if (el) el.scrollTop = el.scrollHeight
}

const contentSignature = computed(() => {
  const last = items.value[items.value.length - 1]
  const tail = last?.type === 'turn' ? last.turn.answers.reduce((n, a) => n + (a.text?.length || 0) + a.tools.length, 0) : 0
  return `${items.value.length}|${tail}`
})
watch(
  contentSignature,
  async () => {
    if (!prefs.value.follow || !nearBottom.value) return
    await nextTick()
    scrollToBottom()
  },
  { flush: 'post' },
)
onMounted(async () => {
  await nextTick()
  if (prefs.value.follow && runActive.value) scrollToBottom()
})

const jumpTarget = ref('')
function jumpTo(key: string) {
  jumpTarget.value = ''
  if (!key) return
  const el = scroller.value?.querySelector<HTMLElement>(`[data-item-key="${CSS.escape(key)}"]`)
  if (!el) return
  prefs.value.follow = false
  el.scrollIntoView({ block: 'start', behavior: 'smooth' })
}

function toggleFollow() {
  prefs.value.follow = !prefs.value.follow
  if (prefs.value.follow) {
    nearBottom.value = true
    scrollToBottom()
  }
}
</script>

<template>
  <div class="flex h-full min-h-0 min-w-0 w-full flex-col bg-base" data-testid="run-llm-transcript">
    <div class="flex shrink-0 flex-wrap items-center gap-x-3 gap-y-2 border-b border-line bg-surface px-3 py-2 sm:px-4">
      <div class="flex min-w-0 items-center gap-2 text-[12px] text-txt2" data-testid="llm-summary">
        <Icon name="chat" :size="14" class="text-accent-2" />
        <span>{{ t('pages.llmTranscript.summary', { nodes: summary.nodes, turns: summary.turns }) }}</span>
        <span v-if="summary.totalTokens != null" class="font-mono tabular-nums text-txt3">
          {{ t('pages.llmTranscript.tokens', { n: fmtTokenCount(summary.totalTokens) }) }}
        </span>
        <Icon v-if="loading" name="spinner" :size="12" class="animate-spin text-txt3" />
      </div>
      <div class="flex-1" />
      <select
        v-if="nodeItems.length > 1"
        v-model="jumpTarget"
        class="rounded-md border border-line bg-elevated px-2 py-1 text-[12px] text-txt2"
        data-testid="llm-jump"
        :aria-label="t('pages.llmTranscript.jumpTo')"
        @change="jumpTo(jumpTarget)"
      >
        <option value="">{{ t('pages.llmTranscript.jumpTo') }}</option>
        <option v-for="n in nodeItems" :key="n.key" :value="n.key">
          {{ n.type === 'node' ? n.label : '' }}{{ n.type === 'node' && n.iteration > 1 ? ' · ' + t('common.iterationN', { n: n.iteration }) : '' }}
        </option>
      </select>
      <label class="inline-flex cursor-pointer items-center gap-1.5 text-[12px] text-txt2">
        <input v-model="prefs.showThought" type="checkbox" class="accent-[rgb(var(--c-accent))]" data-testid="llm-show-thought" />
        {{ t('pages.llmTranscript.showThought') }}
      </label>
      <button
        type="button"
        class="rounded-md border border-line px-2 py-1 text-[12px] text-txt2 hover:border-line-strong hover:text-txt"
        data-testid="llm-expand-all"
        @click="prefs.expandAll = !prefs.expandAll"
      >
        {{ prefs.expandAll ? t('pages.llmTranscript.collapseAll') : t('pages.llmTranscript.expandAll') }}
      </button>
      <button
        type="button"
        class="rounded-md inline-flex items-center gap-1 border px-2 py-1 text-[12px]"
        :class="prefs.follow ? 'border-accent/50 bg-accent-dim text-accent-2' : 'border-line text-txt2 hover:border-line-strong'"
        data-testid="llm-follow"
        :aria-pressed="prefs.follow"
        @click="toggleFollow"
      >
        <Icon name="chevrons-down" :size="12" />{{ t('pages.llmTranscript.follow') }}
      </button>
    </div>

    <div
      v-if="loadError"
      class="flex shrink-0 items-center gap-2 border-b border-warn/30 bg-warn/[0.06] px-4 py-1.5 text-[12px] text-warn"
      data-testid="llm-load-error"
    >
      <Icon name="alert" :size="13" />{{ t('pages.llmTranscript.loadFailed') }}
      <button type="button" class="ml-auto text-accent-2 hover:text-accent" @click="load">{{ t('pages.llmTranscript.retry') }}</button>
    </div>

    <div ref="scroller" class="scroll-area min-h-0 flex-1 overflow-y-auto px-3 py-4 sm:px-6" @scroll.passive="onScroll">
      <div
        v-if="loading && !hasTurns"
        class="flex flex-col items-center gap-2 py-16 text-[12px] text-txt3"
        data-testid="llm-loading"
      >
        <Icon name="spinner" :size="20" class="animate-spin text-accent" />{{ t('pages.llmTranscript.loading') }}
      </div>
      <div
        v-else-if="!items.length"
        class="flex flex-col items-center gap-2 py-16 text-[12px] text-txt3"
        data-testid="llm-empty"
      >
        <Icon name="chat" :size="20" />{{ t('pages.llmTranscript.empty') }}
      </div>
      <ol v-else class="mx-auto flex max-w-[1040px] list-none flex-col gap-3 p-0">
        <li v-for="it in items" :key="it.key" :data-item-key="it.key" :data-item-type="it.type">
          <template v-if="it.type === 'turn'">
            <div class="flex flex-col gap-2.5">
              <LlmPromptBubble
                v-if="it.turn.prompt"
                :prompt="it.turn.prompt"
                :expand-all="prefs.expandAll"
                :preview-only="previewOnly && it.turn.prompt.truncated"
              />
              <LlmAnswerBubble
                :turn="it.turn"
                :models="it.models"
                :node-label="nodeInfo(it.nodeId).label"
                :expand-all="prefs.expandAll"
                :show-thought="prefs.showThought"
                @open-artifact="emit('open-artifacts')"
              />
            </div>
          </template>
          <LlmSystemRow v-else :item="it" @locate="(nodeId, idx) => emit('locate', nodeId, idx)" />
        </li>
      </ol>
    </div>
  </div>
</template>
