<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '../../ui/Icon.vue'
import AnnotateBtn from './AnnotateBtn.vue'

export type MergeRequestItem = {
  repo?: string
  sourceBranch?: string
  targetBranch?: string
  url?: string
  provider?: string
  state?: string
  note?: string
}
export type MergeRequestDoc = {
  summary?: string
  items?: MergeRequestItem[]
}

defineProps<{ doc: MergeRequestDoc; accent?: string }>()

const { t, te } = useI18n()

const STATE_CLS: Record<string, string> = {
  created: 'bg-ok/15 text-ok border-ok/40',
  reused: 'bg-info/15 text-info border-info/40',
  merged: 'bg-accent/15 text-accent-2 border-accent/40',
  unsupported: 'bg-warn/15 text-warn border-warn/40',
}

function stateLabel(s?: string) {
  const key = `pages.product.mergeRequest.states.${s || ''}`
  return s && te(key) ? t(key) : s || '—'
}

function stateCls(s?: string) {
  return STATE_CLS[s || ''] || 'bg-base text-txt3 border-line'
}

function linkLabel(provider?: string) {
  const key = `pages.product.mergeRequest.open.${provider || 'other'}`
  return te(key) ? t(key) : t('pages.product.mergeRequest.open.other')
}
</script>

<template>
  <div class="space-y-4">
    <div v-if="doc.summary" class="group flex items-start gap-1 rounded-lg border border-line bg-base/40 p-3 text-[12px] leading-relaxed text-txt2">
      <span class="min-w-0 flex-1">{{ doc.summary }}</span>
      <AnnotateBtn json-path="summary" label="概述" />
    </div>

    <section v-if="doc.items?.length">
      <div class="mb-1.5 flex items-center gap-1.5 text-[10px] font-semibold uppercase tracking-wider text-txt3">
        <Icon name="git" :size="12" :style="{ color: accent }" />{{ t('pages.product.mergeRequest.items', { n: doc.items.length }) }}
      </div>
      <div class="space-y-2">
        <div
          v-for="(it, i) in doc.items"
          :key="`${it.repo}-${i}`"
          class="group rounded-lg border border-line bg-base/40 p-2.5"
          data-testid="merge-request-item"
        >
          <div class="flex flex-wrap items-center gap-2">
            <span class="min-w-0 truncate text-[12px] font-semibold text-txt" :title="it.repo">{{ it.repo }}</span>
            <span
              class="shrink-0 rounded border px-1.5 py-0.5 text-[10px] font-medium"
              :class="stateCls(it.state)"
              data-testid="merge-request-state"
            >{{ stateLabel(it.state) }}</span>
            <AnnotateBtn :json-path="`items[${i}]`" :label="it.repo || `仓库 ${i + 1}`" />
            <a
              v-if="it.url"
              :href="it.url"
              target="_blank"
              rel="noopener noreferrer"
              class="ml-auto inline-flex shrink-0 items-center gap-1 text-[11px] font-medium text-accent-2 hover:underline"
              data-testid="merge-request-link"
            >
              <Icon name="globe" :size="12" />{{ linkLabel(it.provider) }}
            </a>
          </div>
          <div class="mt-1 flex min-w-0 items-center gap-1.5 font-mono text-[11px] text-txt3" data-testid="merge-request-branches">
            <Icon name="branch" :size="12" class="shrink-0" />
            <span class="truncate" :title="it.sourceBranch">{{ it.sourceBranch }}</span>
            <span class="shrink-0">→</span>
            <span class="truncate" :title="it.targetBranch">{{ it.targetBranch }}</span>
          </div>
          <div v-if="it.note" class="mt-1 text-[11px] leading-relaxed text-txt3">{{ it.note }}</div>
        </div>
      </div>
    </section>
  </div>
</template>
