<script setup lang="ts">
/**
 * The conversation's busy line: a pulse and 「思考中…」, or the tail
 * 「输出中」once a streaming reply already has text. One component for
 * ClarifyChat and the run LLM transcript.
 */
import { useI18n } from 'vue-i18n'
import InboxMotionMark from '@/components/inbox/InboxMotionMark.vue'

withDefaults(
  defineProps<{
    /** placeholder = empty in-flight turn; status = tail under steps that already started. */
    variant?: 'placeholder' | 'status'
    outputting?: boolean
  }>(),
  { variant: 'placeholder', outputting: false },
)

const { t } = useI18n()
</script>

<template>
  <div
    v-if="variant === 'placeholder'"
    class="chat-meta inline-flex items-center gap-2.5 py-2"
    data-testid="clarify-busy-placeholder"
  >
    <InboxMotionMark kind="pulse" />
    <span>{{ t('pages.clarify.thinkingBusy') }}</span>
  </div>
  <div
    v-else
    class="chat-meta mt-1 inline-flex items-center gap-2 font-normal"
    :class="outputting ? 'clarify-outputting' : ''"
    data-testid="clarify-busy-status"
    :data-state="outputting ? 'outputting' : 'thinking'"
    role="status"
  >
    <InboxMotionMark v-if="!outputting" kind="pulse" />
    <span>{{ outputting ? t('pages.clarify.outputting') : t('pages.clarify.thinkingBusy') }}</span>
  </div>
</template>

<style scoped>
.clarify-outputting { color: rgb(var(--c-txt3)); }
</style>
