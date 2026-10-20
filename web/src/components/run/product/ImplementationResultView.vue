<script setup lang="ts">
import ArtifactReportDocument from './ArtifactReportDocument.vue'
import { useI18n } from 'vue-i18n'
import Icon from '../../ui/Icon.vue'
import AnnotateBtn from './AnnotateBtn.vue'

export type ImplChangedArea = { title?: string; detail?: string }
export type ImplementationResultDoc = {
  summary?: string
  change_type?: string
  changed_areas?: ImplChangedArea[]
  tests?: string[]
  breaking_changes?: string[]
  follow_ups?: string[]
}

defineProps<{ doc: ImplementationResultDoc; accent?: string }>()

const { t } = useI18n()
</script>

<template>
  <ArtifactReportDocument>

  <div class="space-y-4">
    <div class="report-summary group">
      <span v-if="doc.change_type" class="mb-1.5 inline-block rounded-md bg-accent/15 px-2 py-0.5 text-[11px] font-medium text-accent-2">{{ doc.change_type }}</span>
      <div v-if="doc.summary" class="flex items-start gap-1 text-[13px] leading-relaxed text-txt2">
        <span class="min-w-0 flex-1">{{ doc.summary }}</span>
        <AnnotateBtn json-path="summary" label="概述" />
      </div>
    </div>

    <section v-if="doc.changed_areas?.length" class="report-section">
      <h3 class="report-heading mb-1.5 flex items-center gap-1.5 font-semibold text-txt3">
        <Icon name="edit" :size="12" :style="{ color: accent }" />{{ t('pages.product.implementationResult.changedAreas') }}
      </h3>
      <div class="space-y-2">
        <div v-for="(c, i) in doc.changed_areas" :key="i" class="report-item group">
          <div class="flex items-center gap-1 text-[13px] font-semibold text-txt">
            <span class="min-w-0 flex-1">{{ c.title }}</span>
            <AnnotateBtn :json-path="`changed_areas[${i}]`" :label="c.title || `改动 ${i + 1}`" />
          </div>
          <div v-if="c.detail" class="mt-1 text-[13px] leading-relaxed text-txt2">{{ c.detail }}</div>
        </div>
      </div>
    </section>

    <section v-if="doc.tests?.length" class="report-section">
      <h3 class="report-heading mb-1.5 font-semibold text-txt3">{{ t('pages.product.implementationResult.tests') }}</h3>
      <ul class="space-y-1">
        <li v-for="(t, i) in doc.tests" :key="i" class="group flex items-start gap-1.5 text-[13px] leading-5 text-txt2">
          <Icon name="check" :size="12" class="mt-0.5 shrink-0 text-ok" />
          <span class="min-w-0 flex-1">{{ t }}</span>
          <AnnotateBtn :json-path="`tests[${i}]`" :label="t" />
        </li>
      </ul>
    </section>

    <section v-if="doc.breaking_changes?.length" class="report-section">
      <h3 class="report-heading mb-1 flex items-center gap-1.5 font-semibold text-err">
        <Icon name="gate" :size="12" />{{ t('pages.product.implementationResult.breakingChanges') }}
      </h3>
      <ul class="space-y-1">
        <li v-for="(b, i) in doc.breaking_changes" :key="i" class="group flex items-start gap-1.5 text-[13px] leading-5 text-txt2">
          <span class="mt-0.5 shrink-0 text-err">⚠</span>
          <span class="min-w-0 flex-1">{{ b }}</span>
          <AnnotateBtn :json-path="`breaking_changes[${i}]`" :label="b" />
        </li>
      </ul>
    </section>

    <section v-if="doc.follow_ups?.length" class="report-section">
      <h3 class="report-heading mb-1.5 font-semibold text-txt3">{{ t('pages.product.implementationResult.followUps') }}</h3>
      <ul class="space-y-1">
        <li v-for="(u, i) in doc.follow_ups" :key="i" class="group flex items-start gap-1.5 text-[13px] leading-5 text-txt2">
          <span class="mt-0.5 shrink-0 text-txt3">→</span>
          <span class="min-w-0 flex-1">{{ u }}</span>
          <AnnotateBtn :json-path="`follow_ups[${i}]`" :label="u" />
        </li>
      </ul>
    </section>
  </div>

  </ArtifactReportDocument>
</template>
