<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../../ui/Icon.vue'
import AnnotateBtn from './AnnotateBtn.vue'
import MermaidDiagram, { type PlanDiagram } from '../MermaidDiagram.vue'
import type { Artifact } from '@/lib/shared/types'

export type RootCauseEvidence = { title?: string; detail?: string }
export type RootCauseDoc = {
  title?: string
  summary?: string
  symptom?: string
  expected?: string
  actual?: string
  reproduction?: string[]
  impact?: string
  root_cause?: string
  evidence?: RootCauseEvidence[]
  diagrams?: PlanDiagram[]
  ruled_out?: string[]
  contributing_factors?: string[]
  affected_scope?: string
  causal_chain?: string
}

const props = defineProps<{ doc: RootCauseDoc; accent?: string; artifacts?: Artifact[] }>()

const { t } = useI18n()
const hex = computed(() => props.accent || '#818CF8')

const diagrams = computed(() =>
  (props.doc.diagrams || []).filter((d) => (d.source || '').trim()),
)

const activeDiagramTab = reactive({ index: 0 })

watch(
  diagrams,
  (list) => {
    if (list.length <= 0) activeDiagramTab.index = 0
    else if (activeDiagramTab.index < 0 || activeDiagramTab.index >= list.length) activeDiagramTab.index = 0
  },
  { immediate: true },
)

function kindLabel(kind?: string) {
  const k = (kind || '').trim().toLowerCase()
  if (k === 'activity' || k === 'flowchart' || k === 'sequence' || k === 'er' || k === 'chart') {
    return t(`pages.plan.diagramKinds.${k === 'chart' ? 'other' : k}`)
  }
  if (!k) return ''
  return t('pages.plan.diagramKinds.other')
}

function diagramTabLabel(d: PlanDiagram, index: number) {
  const title = (d.title || '').trim()
  if (title) return title
  const kind = kindLabel(d.kind)
  if (kind) return kind
  return `${index + 1}`
}

function currentDiagram(): PlanDiagram | undefined {
  const list = diagrams.value
  if (!list.length) return undefined
  const i = Math.min(Math.max(activeDiagramTab.index, 0), list.length - 1)
  return list[i]
}

function diagramJsonPath(active: PlanDiagram | undefined): string {
  if (!active) return 'diagrams[0]'
  const idx = (props.doc.diagrams || []).findIndex(
    (d) => (d.source || '').trim() === (active.source || '').trim(),
  )
  return idx >= 0 ? `diagrams[${idx}]` : 'diagrams[0]'
}

function selectDiagramTab(index: number) {
  activeDiagramTab.index = index
}
</script>

<template>
  <div class="space-y-4">
    <div v-if="doc.summary || doc.title" class="group rounded-lg border border-line bg-accent-dim/30 p-3">
      <div v-if="doc.title" class="mb-1 flex items-center gap-1 text-[13px] font-semibold text-txt">
        <span data-json-path="title" :data-label="doc.title">{{ doc.title }}</span>
        <AnnotateBtn json-path="title" :label="doc.title" />
      </div>
      <div v-if="doc.summary" class="flex items-start gap-1 text-[12px] leading-relaxed text-txt2">
        <span
          class="min-w-0 flex-1"
          data-json-path="summary"
          :data-label="t('pages.product.rootCause.summary')"
        >{{ doc.summary }}</span>
        <AnnotateBtn json-path="summary" :label="t('pages.product.rootCause.summary')" />
      </div>
    </div>

    <section
      v-if="doc.symptom || doc.expected || doc.actual"
      class="rounded-lg border border-line bg-base/40 p-3"
    >
      <div class="mb-2 text-[10px] font-semibold uppercase tracking-wider text-txt3">
        {{ t('pages.product.rootCause.expectedActual') }}
      </div>
      <div v-if="doc.symptom" class="mb-2 text-[12px] leading-relaxed text-txt2">
        <span class="font-medium text-txt">{{ t('pages.product.rootCause.symptom') }}:</span>
        {{ doc.symptom }}
      </div>
      <div v-if="doc.expected" class="group flex items-start gap-1 text-[12px] leading-relaxed text-txt2">
        <span class="shrink-0 font-medium text-ok">{{ t('pages.product.rootCause.expected') }}:</span>
        <span
          class="min-w-0 flex-1"
          data-json-path="expected"
          :data-label="t('pages.product.rootCause.expected')"
        >{{ doc.expected }}</span>
        <AnnotateBtn json-path="expected" :label="t('pages.product.rootCause.expected')" />
      </div>
      <div v-if="doc.actual" class="group mt-2 flex items-start gap-1 text-[12px] leading-relaxed text-txt2">
        <span class="shrink-0 font-medium text-warn">{{ t('pages.product.rootCause.actual') }}:</span>
        <span
          class="min-w-0 flex-1"
          data-json-path="actual"
          :data-label="t('pages.product.rootCause.actual')"
        >{{ doc.actual }}</span>
        <AnnotateBtn json-path="actual" :label="t('pages.product.rootCause.actual')" />
      </div>
    </section>

    <section v-if="doc.reproduction?.length">
      <div class="mb-1.5 text-[10px] font-semibold uppercase tracking-wider text-txt3">
        {{ t('pages.product.rootCause.reproduction') }}
      </div>
      <ol class="list-decimal space-y-1 pl-5">
        <li
          v-for="(step, i) in doc.reproduction"
          :key="i"
          class="group flex items-start gap-1.5 text-[11px] leading-relaxed text-txt2"
        >
          <span class="min-w-0 flex-1" :data-json-path="`reproduction[${i}]`" :data-label="step">{{ step }}</span>
          <AnnotateBtn :json-path="`reproduction[${i}]`" :label="step" />
        </li>
      </ol>
    </section>

    <section v-if="doc.impact || doc.affected_scope" class="rounded-lg border border-line bg-base/40 p-3">
      <div class="mb-1.5 text-[10px] font-semibold uppercase tracking-wider text-txt3">
        {{ t('pages.product.rootCause.impact') }}
      </div>
      <div v-if="doc.impact" class="text-[12px] leading-relaxed text-txt2">{{ doc.impact }}</div>
      <div v-if="doc.affected_scope" class="mt-2 text-[11px] leading-relaxed text-txt3">
        <span class="font-medium text-txt2">{{ t('pages.product.rootCause.affectedScope') }}:</span>
        {{ doc.affected_scope }}
      </div>
    </section>

    <section v-if="doc.root_cause" class="group rounded-lg border border-warn/40 bg-warn/5 p-3">
      <div class="mb-1 flex items-center gap-1.5 text-[10px] font-semibold uppercase tracking-wider text-warn">
        <Icon name="doc" :size="12" />{{ t('pages.product.rootCause.rootCause') }}
        <AnnotateBtn json-path="root_cause" :label="t('pages.product.rootCause.rootCause')" />
      </div>
      <div
        class="text-[12px] leading-relaxed text-txt2"
        data-json-path="root_cause"
        :data-label="t('pages.product.rootCause.rootCause')"
      >{{ doc.root_cause }}</div>
      <div v-if="doc.causal_chain" class="mt-2 text-[11px] leading-relaxed text-txt3">
        <span class="font-medium text-txt2">{{ t('pages.product.rootCause.causalChain') }}:</span>
        {{ doc.causal_chain }}
      </div>
    </section>

    <section v-if="doc.evidence?.length">
      <div class="mb-1.5 flex items-center gap-1.5 text-[10px] font-semibold uppercase tracking-wider text-txt3">
        <Icon name="doc" :size="12" :style="{ color: hex }" />{{ t('pages.product.rootCause.evidence') }}
      </div>
      <div class="space-y-2">
        <div
          v-for="(e, i) in doc.evidence"
          :key="i"
          class="rounded-lg border border-line bg-base/40 p-2.5"
        >
          <div class="text-[12px] font-semibold text-txt">{{ e.title }}</div>
          <div v-if="e.detail" class="mt-1 text-[11px] leading-relaxed text-txt3">{{ e.detail }}</div>
        </div>
      </div>
    </section>

    <section v-if="doc.ruled_out?.length">
      <div class="mb-1.5 text-[10px] font-semibold uppercase tracking-wider text-txt3">
        {{ t('pages.product.rootCause.ruledOut') }}
      </div>
      <ul class="space-y-1">
        <li v-for="(r, i) in doc.ruled_out" :key="i" class="flex items-start gap-1.5 text-[11px] text-txt2">
          <span class="mt-0.5 shrink-0 text-txt3">×</span>
          <span :data-json-path="`ruled_out[${i}]`">{{ r }}</span>
        </li>
      </ul>
    </section>

    <section v-if="doc.contributing_factors?.length">
      <div class="mb-1.5 text-[10px] font-semibold uppercase tracking-wider text-txt3">
        {{ t('pages.product.rootCause.contributingFactors') }}
      </div>
      <ul class="space-y-1">
        <li
          v-for="(f, i) in doc.contributing_factors"
          :key="i"
          class="text-[11px] leading-relaxed text-txt2"
        >{{ f }}</li>
      </ul>
    </section>

    <section v-if="diagrams.length" class="rounded-lg border border-line bg-base/40 p-3">
      <div class="mb-2 text-[10px] font-semibold uppercase tracking-wider text-txt3">
        {{ t('pages.product.rootCause.diagrams') }}
      </div>
      <div
        v-if="diagrams.length >= 2"
        class="mb-2 flex gap-1.5 overflow-x-auto"
        data-testid="root-cause-diagram-tabs"
        role="tablist"
      >
        <button
          v-for="(d, di) in diagrams"
          :key="(d.source || '') + di"
          type="button"
          role="tab"
          class="shrink-0 rounded-full px-2 py-0.5 text-[11px] leading-tight transition-colors"
          :class="activeDiagramTab.index === di ? '' : 'bg-base text-txt3'"
          :style="activeDiagramTab.index === di ? { background: hex + '22', color: hex } : undefined"
          :aria-selected="activeDiagramTab.index === di"
          :data-testid="`root-cause-diagram-tab-${di}`"
          @click="selectDiagramTab(di)"
        >
          {{ diagramTabLabel(d, di) }}
        </button>
      </div>
      <MermaidDiagram
        v-if="currentDiagram()"
        :diagram="currentDiagram()!"
        :json-path="diagramJsonPath(currentDiagram())"
        :artifacts="artifacts"
      />
    </section>
  </div>
</template>
