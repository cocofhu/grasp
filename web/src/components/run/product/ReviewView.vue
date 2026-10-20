<script setup lang="ts">
import { computed } from 'vue'
import ArtifactReportDocument from './ArtifactReportDocument.vue'
import { useI18n } from 'vue-i18n'
import AnnotateBtn from './AnnotateBtn.vue'

export type ReviewFinding = {
  id?: string
  severity?: string
  file?: string
  line?: number
  title?: string
  detail?: string
  suggestion?: string
}
export type ReviewDoc = {
  summary?: string
  verdict?: string
  findings?: ReviewFinding[]
  action_items?: string[]
}

const props = defineProps<{ doc: ReviewDoc }>()

const { t } = useI18n()

const VERDICT: Record<string, { labelKey: string; cls: string; icon: string }> = {
  approve: { labelKey: 'pages.product.review.verdictLabels.approve', cls: 'text-ok', icon: '✓' },
  approve_with_comments: { labelKey: 'pages.product.review.verdictLabels.approve_with_comments', cls: 'text-info', icon: '✓' },
  request_changes: { labelKey: 'pages.product.review.verdictLabels.request_changes', cls: 'text-warn', icon: '↻' },
  reject: { labelKey: 'pages.product.review.verdictLabels.reject', cls: 'text-err', icon: '✕' },
}
const verdict = computed(() => {
  const v = VERDICT[props.doc.verdict || '']
  if (v) return { label: t(v.labelKey), cls: v.cls, icon: v.icon }
  return { label: props.doc.verdict || '—', cls: 'text-txt2', icon: '?' }
})

const SEV: Record<string, { label: string; cls: string }> = {
  critical: { label: 'CRITICAL', cls: 'bg-err/20 text-err' },
  high: { label: 'HIGH', cls: 'bg-err/15 text-err' },
  medium: { label: 'MEDIUM', cls: 'bg-warn/15 text-warn' },
  low: { label: 'LOW', cls: 'bg-info/15 text-info' },
}
function sev(s?: string) {
  return SEV[s || 'medium'] || SEV.medium
}
</script>

<template>
  <ArtifactReportDocument>
    <header class="mb-5" data-testid="review-conclusion">
      <p class="mb-1.5 text-[11px] text-txt3">{{ t('pages.product.review.verdict') }}</p>
      <h2 class="report-title flex items-center gap-2"><span aria-hidden="true" class="text-base" :class="verdict.cls">{{ verdict.icon }}</span>{{ verdict.label }}</h2>
    </header>

    <div v-if="doc.summary" class="report-summary group flex items-start gap-2">
      <p class="min-w-0 flex-1" data-json-path="summary" data-label="概述">{{ doc.summary }}</p>
      <AnnotateBtn json-path="summary" label="概述" />
    </div>

    <section v-if="doc.findings?.length" class="report-section">
      <h3 class="report-heading">{{ t('pages.product.review.findings', { n: doc.findings.length }) }}</h3>
      <div>
        <div v-for="(f, i) in doc.findings" :key="f.id || i" class="report-item group" data-testid="review-finding">
          <div class="flex flex-wrap items-center gap-2">
            <span class="shrink-0 rounded px-1.5 py-0.5 text-[11px] font-medium" :class="sev(f.severity).cls">{{ sev(f.severity).label }}</span>
            <h4 class="min-w-0 flex-1" :data-json-path="`findings[${f.id || i}]`" :data-label="f.title || `意见 ${i + 1}`">{{ f.title }}</h4>
            <AnnotateBtn :json-path="`findings[${f.id || i}]`" :label="f.title || `意见 ${i + 1}`" />
          </div>
          <code v-if="f.file" class="mt-1 block break-all font-mono text-[11px] text-txt3">{{ f.file }}<span v-if="f.line">:{{ f.line }}</span></code>
          <p v-if="f.detail" class="mt-2 text-[13px] leading-6 text-txt2" :data-json-path="`findings[${f.id || i}].detail`" :data-label="`${f.id || i} 详情`">{{ f.detail }}</p>
          <p v-if="f.suggestion" class="report-note text-txt2" :data-json-path="`findings[${f.id || i}].suggestion`" :data-label="`${f.id || i} 建议`"><span class="mr-1.5 font-medium text-txt">{{ t('pages.product.review.suggestion') }}</span>{{ f.suggestion }}</p>
        </div>
      </div>
    </section>

    <section v-if="doc.action_items?.length" class="report-section">
      <h3 class="report-heading">{{ t('pages.product.review.actionItems') }}</h3>
      <ul class="space-y-2">
        <li v-for="(a, i) in doc.action_items" :key="i" class="group flex items-start gap-2 text-[13px] leading-6 text-txt2">
          <span class="shrink-0 text-txt3" aria-hidden="true">☐</span>
          <span class="min-w-0 flex-1" :data-json-path="`action_items[${i}]`" :data-label="a">{{ a }}</span>
          <AnnotateBtn :json-path="`action_items[${i}]`" :label="a" />
        </li>
      </ul>
    </section>
  </ArtifactReportDocument>
</template>
