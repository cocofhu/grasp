<script lang="ts">
export type FeedbackIndexRound = {
  seq?: number
  kind?: string
  node?: string
  iteration?: number
  round?: number
  at?: string
  actor?: string
  action?: string
  summary?: string
  attachments?: number
  annotations?: number
  interrupted?: boolean
  artifact?: string
}

export type FeedbackIndexDoc = {
  runId?: string
  generatedAt?: string
  totalRounds?: number
  counts?: Record<string, number>
  rounds?: FeedbackIndexRound[]
}
</script>

<script setup lang="ts">
import ArtifactReportDocument from './ArtifactReportDocument.vue'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../../ui/Icon.vue'
import FeedbackRoundCard, { type FeedbackRoundDoc } from './FeedbackRoundCard.vue'
import { api } from '@/lib/api/api'
import { fmtTime } from '@/lib/shared/format'
import type { Artifact } from '@/lib/shared/types'

// Renders the human-feedback ledger. The same component handles both shapes the
// ledger produces: feedback_index.json (a timeline over every round) and a
// single feedback product. New ReAct products use
// feedback.<kind>.<node>.i<n>.json and show a cumulative conclusion; legacy
// and gate/preview products may retain the i<n>r<n> form. The index stays the
// expandable per-round audit timeline.
const props = defineProps<{
  name: string
  doc: any
  runId?: string
  artifacts?: Artifact[]
}>()

const { t } = useI18n()

const isIndex = computed(() => Array.isArray(props.doc?.rounds))
const index = computed<FeedbackIndexDoc>(() => (props.doc || {}) as FeedbackIndexDoc)
const rounds = computed<FeedbackIndexRound[]>(() => index.value.rounds || [])

const KIND_META: Record<string, { icon: string; cls: string; labelKey: string }> = {
  clarify: { icon: 'chat', cls: 'text-info border-info/40 bg-info/10', labelKey: 'pages.product.feedback.kind.clarify' },
  review: { icon: 'edit', cls: 'text-accent-2 border-accent/40 bg-accent-dim', labelKey: 'pages.product.feedback.kind.review' },
  gate: { icon: 'gate', cls: 'text-ok border-ok/40 bg-ok/10', labelKey: 'pages.product.feedback.kind.gate' },
  preview: { icon: 'dashboard', cls: 'text-warn border-warn/40 bg-warn/10', labelKey: 'pages.product.feedback.kind.preview' },
}

function kindMeta(kind?: string) {
  return KIND_META[kind || ''] || KIND_META.review
}

/** Gate rejections read as errors, not approvals. */
function badgeClass(r: FeedbackIndexRound): string {
  if (r.kind === 'gate' && r.action && !['pass', 'approve'].includes(r.action)) {
    return 'text-err border-err/40 bg-err/10'
  }
  return kindMeta(r.kind).cls
}

const expanded = ref<Set<number>>(new Set())
const loaded = ref<Record<string, FeedbackRoundDoc>>({})
const loading = ref<Record<string, boolean>>({})
const failed = ref<Record<string, boolean>>({})

function artifactId(name: string): string | null {
  return (props.artifacts || []).find((a) => a.name === name)?.id ?? null
}

async function loadRound(name: string) {
  if (loaded.value[name] || loading.value[name]) return
  const id = artifactId(name)
  if (!id) {
    failed.value = { ...failed.value, [name]: true }
    return
  }
  loading.value = { ...loading.value, [name]: true }
  try {
    const art = await api.artifactContent(id)
    loaded.value = { ...loaded.value, [name]: JSON.parse(art.content || '{}') as FeedbackRoundDoc }
    failed.value = { ...failed.value, [name]: false }
  } catch {
    failed.value = { ...failed.value, [name]: true }
  } finally {
    loading.value = { ...loading.value, [name]: false }
  }
}

function toggle(i: number, r: FeedbackIndexRound) {
  const next = new Set(expanded.value)
  if (next.has(i)) next.delete(i)
  else {
    next.add(i)
    if (r.artifact) void loadRound(r.artifact)
  }
  expanded.value = next
}

// A re-render for a different product must not keep the previous one's rows open.
watch(
  () => props.name,
  () => {
    expanded.value = new Set()
  },
)

const countEntries = computed(() =>
  Object.entries(index.value.counts || {}).filter(([, n]) => n > 0),
)
</script>

<template>
  <ArtifactReportDocument>

  <div v-if="!isIndex" class="space-y-3">
    <div class="flex flex-wrap items-center gap-2">
      <span
        class="inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-[11px] font-medium"
        :class="kindMeta(doc?.kind).cls"
      >
        <Icon :name="kindMeta(doc?.kind).icon" :size="10" />
        {{ t(kindMeta(doc?.kind).labelKey) }}
      </span>
      <code v-if="doc?.node?.id" class="font-mono text-[13px] text-txt">{{ doc.node.id }}</code>
      <span class="text-[13px] text-txt3">
        {{ t('pages.product.feedback.iterRound', { i: doc?.iteration, r: doc?.latestRound ?? doc?.round }) }}
      </span>
      <span v-if="doc?.actor?.name" class="text-[13px] text-txt3">{{ doc.actor.name }}</span>
      <span v-if="doc?.at" class="ml-auto text-[11px] text-txt3">{{ fmtTime(doc.at) }}</span>
    </div>
    <FeedbackRoundCard :doc="doc as FeedbackRoundDoc" />
    <div v-if="doc?.index" class="text-[13px] text-txt3">
      {{ t('pages.product.feedback.seeIndex', { name: doc.index }) }}
    </div>
  </div>

  <div v-else class="space-y-3">
    <div class="flex flex-wrap items-center gap-2 text-[13px] text-txt3">
      <span class="text-[13px] font-medium text-txt2">
        {{ t('pages.product.feedback.total', { n: index.totalRounds ?? rounds.length }) }}
      </span>
      <span
        v-for="[kind, n] in countEntries"
        :key="kind"
        class="rounded-full border px-1.5 py-px text-[11px]"
        :class="kindMeta(kind).cls"
      >
        {{ t(kindMeta(kind).labelKey) }} {{ n }}
      </span>
    </div>

    <div v-if="!rounds.length" class="py-6 text-center text-[13px] text-txt3">
      {{ t('pages.product.feedback.empty') }}
    </div>

    <ol v-else class="relative space-y-3 border-l border-line pl-5">
      <li v-for="(r, i) in rounds" :key="r.seq ?? i" class="relative">
        <span
          class="absolute -left-[27px] flex h-5 w-5 items-center justify-center rounded-full border"
          :class="badgeClass(r)"
        >
          <Icon :name="kindMeta(r.kind).icon" :size="11" />
        </span>
        <button
          type="button"
          class="flex w-full items-start gap-2 text-left"
          :data-testid="`feedback-round-${r.seq ?? i}`"
          @click="toggle(i, r)"
        >
          <div class="min-w-0 flex-1">
            <div class="flex flex-wrap items-center gap-2">
              <span class="text-[11px] font-medium uppercase tracking-wide" :class="badgeClass(r).split(' ')[0]">
                {{ t(kindMeta(r.kind).labelKey) }}
              </span>
              <code class="font-mono text-[13px] text-txt">{{ r.node }}</code>
              <span class="text-[11px] text-txt3">
                {{ t('pages.product.feedback.iterRound', { i: r.iteration, r: r.round }) }}
              </span>
              <span v-if="r.actor" class="text-[11px] text-txt3">{{ r.actor }}</span>
              <span v-if="r.annotations" class="text-[11px] text-txt3">
                {{ t('pages.product.feedback.annotationCount', { n: r.annotations }) }}
              </span>
              <span v-if="r.attachments" class="text-[11px] text-txt3">
                {{ t('pages.product.feedback.attachmentCount', { n: r.attachments }) }}
              </span>
              <span v-if="r.interrupted" class="text-[11px] text-warn">
                {{ t('pages.product.feedback.interrupted') }}
              </span>
              <span v-if="r.at" class="ml-auto text-[11px] text-txt3">{{ fmtTime(r.at) }}</span>
            </div>
            <div class="mt-0.5 truncate text-[13px] text-txt2">{{ r.summary }}</div>
          </div>
          <Icon
            name="chevron-down"
            :size="12"
            class="mt-0.5 shrink-0 text-txt3 transition-transform"
            :class="expanded.has(i) ? '' : '-rotate-90'"
          />
        </button>

        <div v-if="expanded.has(i)" class="mt-2 border-l border-line pl-3">
          <div v-if="!r.artifact" class="text-[13px] text-txt3">
            {{ t('pages.product.feedback.indexOnly') }}
          </div>
          <div v-else-if="loading[r.artifact]" class="text-[13px] text-txt3">
            {{ t('pages.product.feedback.loading') }}
          </div>
          <div v-else-if="failed[r.artifact]" class="text-[13px] text-err">
            {{ t('pages.product.feedback.loadFailed', { name: r.artifact }) }}
          </div>
          <FeedbackRoundCard v-else-if="loaded[r.artifact]" :doc="loaded[r.artifact]" />
        </div>
      </li>
    </ol>
  </div>

  </ArtifactReportDocument>
</template>
