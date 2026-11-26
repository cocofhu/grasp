<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import TruncatedTextTooltip from '@/components/ui/TruncatedTextTooltip.vue'
import type { SoloTaskInfo } from '@/lib/api/apiTypes'
import { effectiveModelUsageRows, fmtTokenCount } from '@/lib/run/tokenUsage'

const props = defineProps<{ usage: SoloTaskInfo['usage'] }>()
const { t } = useI18n()
const breakdown = ref<HTMLElement>()

const parts = computed(() => [
  { key: 'inputTokens', label: t('pages.executionTimeline.partInput') },
  { key: 'outputTokens', label: t('pages.executionTimeline.partOutput') },
  { key: 'cacheReadTokens', label: t('pages.executionTimeline.partCacheRead') },
  { key: 'cacheWriteTokens', label: t('pages.executionTimeline.partCacheWrite') },
] as const)

const models = computed(() => effectiveModelUsageRows(undefined, props.usage.byModel).map(row => ({
  ...row,
  name: row.unknown ? t('pages.tokenAnalytics.unknownModel') : row.modelKey,
})))
const singleModel = computed(() => models.value.length === 1 ? models.value[0] : undefined)

function scrollBreakdown(event: KeyboardEvent) {
  if (!breakdown.value || (event.key !== 'ArrowUp' && event.key !== 'ArrowDown')) return
  event.preventDefault()
  breakdown.value.scrollBy(0, event.key === 'ArrowUp' ? -32 : 32)
}
</script>

<template>
  <TruncatedTextTooltip
    :text="fmtTokenCount(usage.total)"
    class="cursor-help rounded-sm underline decoration-dotted underline-offset-2 focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent/50"
    data-testid="sandbox-info-token-trigger"
    @keydown="scrollBreakdown"
  >
    <template #tooltip>
      <div
        ref="breakdown"
        class="sandbox-token-breakdown py-0.5 tabular-nums"
        :class="{ 'has-model-list': models.length > 1 }"
        data-testid="sandbox-info-token-breakdown"
      >
        <div v-if="singleModel" class="mb-2 break-all font-semibold">
          {{ singleModel.name }}
          <span v-if="singleModel.filled" class="font-normal text-txt2"> · {{ t('pages.solo.sandboxInfo.details.modelBackfill') }}</span>
        </div>
        <dl class="grid grid-cols-[max-content_auto] gap-x-5 gap-y-0.5" data-testid="sandbox-info-token-sum">
          <template v-for="part in parts" :key="part.key">
            <dt class="text-txt2">{{ part.label }}</dt>
            <dd class="text-right">{{ fmtTokenCount(usage[part.key]) }}</dd>
          </template>
        </dl>
        <section v-if="models.length > 1" class="mt-2 border-t border-line-strong pt-2">
          <h3 class="font-semibold">{{ t('pages.tokenByModel.modelTableTitle') }}</h3>
          <ul class="mt-1 divide-y divide-line-strong">
            <li v-for="model in models" :key="model.modelKey" class="py-1" data-testid="sandbox-info-token-model">
              <div class="flex justify-between gap-2 font-medium">
                <span class="min-w-0 break-all">{{ model.name }}</span>
                <span class="shrink-0">{{ fmtTokenCount(model.total) }}</span>
              </div>
              <p v-if="model.filled" class="text-[11px] text-txt2">{{ t('pages.solo.sandboxInfo.details.modelBackfill') }}</p>
              <dl class="mt-0.5 grid grid-cols-2 gap-x-3 gap-y-0.5 text-[11px] leading-4">
                <div v-for="part in parts" :key="part.key" class="flex items-baseline justify-between gap-1">
                  <dt class="text-txt2">{{ part.label }}</dt>
                  <dd class="break-all text-right">{{ fmtTokenCount(model[part.key]) }}</dd>
                </div>
              </dl>
            </li>
          </ul>
        </section>
      </div>
    </template>
  </TruncatedTextTooltip>
</template>

<style scoped>
.sandbox-token-breakdown {
  max-height: min(360px, calc(50dvh - 32px));
  overflow-y: auto;
  overscroll-behavior: contain;
}
.sandbox-token-breakdown.has-model-list { width: min(262px, calc(100vw - 40px)); }
</style>
