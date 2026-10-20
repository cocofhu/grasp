<script setup lang="ts">
import ArtifactReportDocument from './ArtifactReportDocument.vue'
import { useI18n } from 'vue-i18n'
import Icon from '../../ui/Icon.vue'
import AnnotateBtn from './AnnotateBtn.vue'

export type ResearchQA = { id?: string; question?: string; answer?: string }
export type ResearchFinding = { id?: string; title?: string; detail?: string }
export type ResearchDoc = {
  title?: string
  summary?: string
  questions?: ResearchQA[]
  findings?: ResearchFinding[]
  recommendation?: string
  references?: string[]
  follow_ups?: string[]
}

defineProps<{ doc: ResearchDoc; accent?: string }>()

const { t } = useI18n()
</script>

<template>
  <ArtifactReportDocument>

  <div class="space-y-4">
    <div v-if="doc.summary" class="report-summary group">
      <h2 v-if="doc.title" class="report-title mb-3 flex items-center gap-2">
        <span data-json-path="title" :data-label="doc.title">{{ doc.title }}</span>
        <AnnotateBtn json-path="title" :label="doc.title" />
      </h2>
      <div class="flex items-start gap-1 text-[13px] leading-relaxed text-txt2">
        <span class="min-w-0 flex-1" data-json-path="summary" data-label="概述">{{ doc.summary }}</span>
        <AnnotateBtn json-path="summary" label="概述" />
      </div>
    </div>

    <section v-if="doc.questions?.length" class="report-section">
      <h3 class="report-heading mb-1.5 font-semibold text-txt3">{{ t('pages.product.research.questions') }}</h3>
      <div class="space-y-2">
        <div v-for="(q, i) in doc.questions" :key="q.id || i" class="report-item group">
          <div class="flex items-start gap-1.5 text-[13px] font-medium text-txt">
            <span class="mt-0.5 shrink-0 text-accent-2">Q</span>
            <span
              class="min-w-0 flex-1"
              :data-json-path="`questions[${q.id || i}]`"
              :data-label="q.question || `Q${i + 1}`"
            >{{ q.question }}</span>
            <AnnotateBtn :json-path="`questions[${q.id || i}]`" :label="q.question || `Q${i + 1}`" />
          </div>
          <div v-if="q.answer" class="mt-1 flex items-start gap-1.5 text-[13px] leading-relaxed text-txt2">
            <span class="mt-0.5 shrink-0 text-ok">A</span>
            <span
              class="min-w-0 flex-1"
              :data-json-path="`questions[${q.id || i}].answer`"
              :data-label="`${q.id || i} 结论`"
            >{{ q.answer }}</span>
            <AnnotateBtn :json-path="`questions[${q.id || i}].answer`" :label="`${q.id || i} 结论`" />
          </div>
        </div>
      </div>
    </section>

    <section v-if="doc.findings?.length" class="report-section">
      <h3 class="report-heading mb-1.5 flex items-center gap-1.5 font-semibold text-txt3">
        <Icon name="doc" :size="12" :style="{ color: accent }" />{{ t('pages.product.research.findings') }}
      </h3>
      <div class="space-y-2">
        <div v-for="(f, i) in doc.findings" :key="f.id || i" class="report-item group">
          <div class="flex flex-wrap items-center gap-2">
            <code class="shrink-0 rounded bg-base px-1.5 py-0.5 font-mono text-[11px] text-txt3">{{ f.id }}</code>
            <span
              class="text-[13px] font-semibold text-txt"
              :data-json-path="`findings[${f.id || i}]`"
              :data-label="f.title || `发现 ${i + 1}`"
            >{{ f.title }}</span>
            <AnnotateBtn :json-path="`findings[${f.id || i}]`" :label="f.title || `发现 ${i + 1}`" />
          </div>
          <div
            v-if="f.detail"
            class="mt-1 text-[13px] leading-relaxed text-txt2"
            :data-json-path="`findings[${f.id || i}].detail`"
            :data-label="`${f.id || i} 详情`"
          >{{ f.detail }}</div>
        </div>
      </div>
    </section>

    <section v-if="doc.recommendation" class="report-section group">
      <h3 class="report-heading mb-1 flex items-center gap-1.5 font-semibold text-ok">
        <Icon name="check" :size="12" />{{ t('pages.product.research.recommendation') }}
        <AnnotateBtn json-path="recommendation" :label="t('pages.product.research.recommendation')" />
      </h3>
      <div
        class="text-[13px] leading-relaxed text-txt2"
        data-json-path="recommendation"
        :data-label="t('pages.product.research.recommendation')"
      >{{ doc.recommendation }}</div>
    </section>

    <section v-if="doc.follow_ups?.length" class="report-section">
      <h3 class="report-heading mb-1.5 font-semibold text-txt3">{{ t('pages.product.research.followUps') }}</h3>
      <ul class="space-y-1">
        <li v-for="(u, i) in doc.follow_ups" :key="i" class="group flex items-start gap-1.5 text-[13px] leading-5 text-txt2">
          <span class="mt-0.5 shrink-0 text-txt3">→</span>
          <span class="min-w-0 flex-1" :data-json-path="`follow_ups[${i}]`" :data-label="u">{{ u }}</span>
          <AnnotateBtn :json-path="`follow_ups[${i}]`" :label="u" />
        </li>
      </ul>
    </section>

    <section v-if="doc.references?.length" class="report-section">
      <h3 class="report-heading mb-1.5 font-semibold text-txt3">{{ t('pages.product.research.references') }}</h3>
      <div class="flex flex-wrap gap-1.5">
        <span
          v-for="(r, i) in doc.references"
          :key="i"
          class="group inline-flex items-center rounded-md bg-base px-2 py-0.5 text-[11px] text-txt3"
          :data-json-path="`references[${i}]`"
          :data-label="r"
        >
          {{ r }}
          <AnnotateBtn :json-path="`references[${i}]`" :label="r" />
        </span>
      </div>
    </section>
  </div>

  </ArtifactReportDocument>
</template>
