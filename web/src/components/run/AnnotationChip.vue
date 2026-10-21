<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../ui/Icon.vue'
import type { ReactAnnotation } from '@/lib/shared/types'
import { isQuoteAnnotation } from '@/lib/inbox/reviewQuote'

const props = withDefaults(
  defineProps<{
    ann: ReactAnnotation
    removable?: boolean
    testId?: string
  }>(),
  { removable: false },
)

defineEmits<{ remove: [] }>()

const { t } = useI18n()

/** Collapse newlines so a quote stays one paragraph before the single-line ellipsis. */
function oneLine(value: string | undefined): string {
  return String(value || '').replace(/\s+/g, ' ').trim()
}

const kind = computed<'field' | 'quote' | 'unbound'>(() => {
  if (!isQuoteAnnotation(props.ann)) return 'field'
  return props.ann.jsonPath || props.ann.selector ? 'quote' : 'unbound'
})

const kindLabel = computed(() => {
  if (kind.value === 'field') return t('pages.reviewComposer.chipField')
  if (kind.value === 'quote') return t('pages.reviewComposer.chipQuote')
  return t('pages.reviewComposer.chipUnbound')
})

const path = computed(() => oneLine(props.ann.jsonPath || props.ann.selector || ''))
const quoteText = computed(() => (isQuoteAnnotation(props.ann) ? oneLine(props.ann.quote) : ''))
const labelText = computed(() => oneLine(props.ann.label))

/** Excerpt when present; otherwise the path/selector, and the label only if there is no path. */
const body = computed(() => quoteText.value || path.value || labelText.value)
const bodyMono = computed(() => !quoteText.value && !!path.value)
const showTruncated = computed(() => !!props.ann.truncated && !!quoteText.value)

/**
 * Kind, full body, a label/path that differs from the body, and soft-truncation
 * live in the tooltip and accessible name — not as a second line or a badge.
 * Identical strings are kept once (path === label).
 */
const hoverTitle = computed(() => {
  const parts: string[] = []
  const push = (value: string) => {
    if (!value || parts.includes(value)) return
    parts.push(value)
  }
  push(kindLabel.value)
  push(body.value)
  push(path.value)
  push(labelText.value)
  if (showTruncated.value) push(t('pages.reviewComposer.chipTruncated'))
  return parts.join(' · ')
})
</script>

<template>
  <span
    class="chip max-w-full min-w-0"
    role="group"
    :title="hoverTitle"
    :aria-label="hoverTitle"
    :data-testid="testId"
    :data-chip-kind="kind"
  >
    <Icon v-if="kind === 'field'" name="crosshair" :size="12" class="shrink-0" aria-hidden="true" />
    <span v-else class="shrink-0 leading-none" aria-hidden="true">❝</span>
    <span class="min-w-0 truncate" :class="bodyMono ? 'font-mono' : undefined">{{ body }}</span>
    <button
      v-if="removable"
      type="button"
      class="shrink-0 text-txt3 hover:text-txt"
      :aria-label="t('pages.reviewComposer.removeChip')"
      @click="$emit('remove')"
    >
      <Icon name="close" :size="12" />
    </button>
  </span>
</template>
