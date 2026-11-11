<script setup lang="ts">
/**
 * In-progress status for ClarifyChat and the run LLM transcript.
 * Always the thinking lamp and 「思考中…」/ Thinking…, including while
 * visible message text is still streaming. Callers omit the row once the
 * turn ends. `outputting` is ignored so message text is not restyled as
 * 「输出中」/ Writing.
 */
import { useI18n } from 'vue-i18n'
import ThinkingBusyCue from './ThinkingBusyCue.vue'

const props = withDefaults(
  defineProps<{
    /** placeholder = empty in-flight turn; status = tail under steps that already started. */
    variant?: 'placeholder' | 'status'
    /** Ignored. A running turn never switches this row to 输出中. */
    outputting?: boolean
  }>(),
  { variant: 'placeholder', outputting: false },
)

void props.outputting

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
    class="chat-meta grasp-status-host mt-1 inline-flex items-center gap-2 font-normal clarify-thinking"
    data-testid="clarify-busy-status"
    data-state="thinking"
    role="status"
  >
    <ThinkingBusyCue :label="t('pages.clarify.thinkingBusy')" />
  </div>
</template>

<style scoped>
.clarify-thinking { padding-inline: 4px; }
.clarify-thinking :deep(.thinking-busy) { gap: 6px; }
</style>
