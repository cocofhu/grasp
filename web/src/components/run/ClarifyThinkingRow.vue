<script setup lang="ts">
/**
 * The conversation's busy line: a small lamp and 「思考中…」, or the tail
 * 「输出中」once a streaming reply already has text. One component for
 * ClarifyChat and the run LLM transcript.
 */
import { useI18n } from 'vue-i18n'
import ThinkingBusyCue from './ThinkingBusyCue.vue'

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
    class="chat-meta grasp-status-host inline-flex items-center gap-2.5 py-2"
    data-testid="clarify-busy-placeholder"
  >
    <ThinkingBusyCue :label="t('pages.clarify.thinkingBusy')" />
  </div>
  <div
    v-else
    class="chat-meta grasp-status-host mt-1 inline-flex items-center gap-2 font-normal"
    :class="outputting ? 'clarify-outputting' : 'clarify-thinking'"
    data-testid="clarify-busy-status"
    :data-state="outputting ? 'outputting' : 'thinking'"
    role="status"
  >
    <ThinkingBusyCue v-if="!outputting" :label="t('pages.clarify.thinkingBusy')" />
    <span v-else>{{ t('pages.clarify.outputting') }}</span>
  </div>
</template>

<style scoped>
.clarify-thinking { padding-inline: 4px; }
.clarify-thinking :deep(.thinking-busy) { gap: 6px; }
.clarify-outputting { color: rgb(var(--c-txt3)); }
</style>
