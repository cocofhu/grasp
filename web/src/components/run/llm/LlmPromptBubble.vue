<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../../ui/Icon.vue'
import { copyToClipboard } from '@/lib/shared/copyToClipboard'
import { imgSrc } from '@/lib/shared/compositeText'
import { renderMarkdown } from '@/lib/shared/markdown'
import { fmtClock, type LlmPrompt } from '@/lib/run/llmTranscript'

const COLLAPSED_LINES = 6

const props = defineProps<{
  prompt: LlmPrompt
  expandAll: boolean
  /** Full text not loaded yet (run detail only carries a preview). */
  previewOnly?: boolean
}>()

const { t } = useI18n()

const expanded = ref(props.expandAll)
watch(
  () => props.expandAll,
  (v) => {
    expanded.value = v
  },
)

const lines = computed(() => props.prompt.text.split('\n'))
const collapsible = computed(() => lines.value.length > COLLAPSED_LINES || props.prompt.text.length > 600)
const shown = computed(() => {
  if (expanded.value || !collapsible.value) return props.prompt.text
  return lines.value.slice(0, COLLAPSED_LINES).join('\n').slice(0, 600)
})
/** Collapsed summary and the expanded body share renderMarkdown (GFM + DOMPurify). */
const promptHtml = computed(() => renderMarkdown(shown.value))

const isHuman = computed(() => props.prompt.source === 'human')
const sourceCls = computed(() => {
  switch (props.prompt.source) {
    case 'human':
      return 'border-warn/40 bg-warn/10 text-warn'
    case 'instruction':
      return 'border-accent/40 bg-accent-dim text-accent-2'
    default:
      return 'border-line bg-elevated text-txt2'
  }
})

const copied = ref(false)
async function copy() {
  if (await copyToClipboard(props.prompt.text)) {
    copied.value = true
    setTimeout(() => (copied.value = false), 1500)
  }
}
</script>

<template>
  <div class="flex min-w-0 w-full justify-end gap-2.5" data-testid="llm-prompt">
    <div class="flex min-w-0 w-full max-w-full flex-1 flex-col items-end">
      <div class="mb-1 flex items-center gap-1.5 text-[11px] text-txt3">
        <span class="rounded-full border px-1.5 py-px text-[10px]" :class="sourceCls" data-testid="llm-prompt-source">
          {{ t(`pages.llmTranscript.source.${prompt.source}`) }}
        </span>
        <span v-if="prompt.imageCount" class="inline-flex items-center gap-0.5">
          <Icon name="paperclip" :size="11" />{{ t('pages.llmTranscript.images', { n: prompt.imageCount }) }}
        </span>
        <span v-if="prompt.at" class="tabular-nums">{{ fmtClock(prompt.at) }}</span>
        <button
          type="button"
          class="rounded px-1 text-txt3 transition-colors hover:text-txt"
          :title="copied ? t('pages.llmTranscript.copied') : t('pages.llmTranscript.copy')"
          :aria-label="t('pages.llmTranscript.copy')"
          data-testid="llm-prompt-copy"
          @click="copy"
        >
          <Icon :name="copied ? 'check' : 'copy'" :size="11" />
        </button>
      </div>
      <div
        class="rounded-xl min-w-0 w-full max-w-full overflow-hidden rounded-tr-sm border px-3 py-2"
        :class="isHuman ? 'border-warn/30 bg-warn/[0.07]' : 'border-accent/25 bg-accent-dim/60'"
      >
        <div v-if="prompt.images?.length" class="mb-2 flex flex-wrap justify-end gap-1.5">
          <img
            v-for="(img, i) in prompt.images"
            :key="i"
            :src="imgSrc(img)"
            alt=""
            class="h-16 w-16 rounded-md border border-line object-cover"
          />
        </div>
        <div
          class="md min-w-0 max-w-full overflow-x-auto break-words text-[13px] leading-6 text-txt"
          data-testid="llm-prompt-text"
        >
          <div v-html="promptHtml" />
          <span v-if="collapsible && !expanded" class="text-txt3">…</span>
        </div>
        <div
          v-if="collapsible || prompt.truncated || previewOnly"
          class="mt-1.5 flex flex-wrap items-center justify-end gap-2 border-t border-line/60 pt-1.5 text-[11px]"
        >
          <span v-if="previewOnly" class="text-txt3">{{ t('pages.llmTranscript.promptPreview') }}</span>
          <span v-else-if="prompt.truncated" class="text-warn">{{ t('pages.llmTranscript.promptTruncated') }}</span>
          <button
            v-if="collapsible"
            type="button"
            class="inline-flex items-center gap-0.5 text-accent-2 hover:text-accent"
            data-testid="llm-prompt-toggle"
            @click="expanded = !expanded"
          >
            {{ expanded ? t('pages.llmTranscript.collapsePrompt') : t('pages.llmTranscript.expandPrompt') }}
            <Icon name="chevron-down" :size="11" :class="expanded ? 'rotate-180' : ''" />
          </button>
        </div>
      </div>
    </div>
    <span
      class="mt-5 flex h-7 w-7 shrink-0 items-center justify-center rounded-full border"
      :class="isHuman ? 'border-warn/40 bg-warn/10 text-warn' : 'border-accent/40 bg-accent-dim text-accent-2'"
      :title="isHuman ? t('pages.llmTranscript.speaker.human') : t('pages.llmTranscript.speaker.platform')"
    >
      <Icon :name="isHuman ? 'user' : 'workflow'" :size="14" />
    </span>
  </div>
</template>
