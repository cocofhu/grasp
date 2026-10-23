<script setup lang="ts">
/**
 * The conversation's busy line: a pulse and 「思考中…」, or the tail
 * 「输出中」once a streaming reply already has text. One component for
 * ClarifyChat and the run LLM transcript.
 */
import { useI18n } from 'vue-i18n'
import Icon from '../ui/Icon.vue'

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
    <Icon name="breath-dot" busy :size="16" class="shrink-0 text-accent-2" aria-hidden="true" />
    <span>{{ t('pages.clarify.thinkingBusy') }}</span>
  </div>
  <div
    v-else
    class="chat-meta grasp-status-host mt-1 inline-flex items-center gap-2 font-normal"
    :class="outputting ? 'clarify-outputting' : ''"
    data-testid="clarify-busy-status"
    :data-state="outputting ? 'outputting' : 'thinking'"
    role="status"
  >
    <Icon v-if="!outputting" name="breath-dot" busy :size="16" class="shrink-0 text-accent-2" aria-hidden="true" />
    <span>{{ outputting ? t('pages.clarify.outputting') : t('pages.clarify.thinkingBusy') }}</span>
  </div>
</template>

<style scoped>
.clarify-outputting { color: rgb(var(--c-txt3)); }
</style>
