<script setup lang="ts">
/**
 * The question a node starts from, as a user bubble: the note the node before
 * it handed over, else the node's own task. Keeps a chained transcript Q/A/Q/A.
 * Body uses the same chat markdown as assistant replies; the source line does not.
 */
import { useI18n } from 'vue-i18n'
import { renderChatMarkdown } from '@/lib/shared/markdown'
import ChatUserMessage from './ChatUserMessage.vue'

export type ClarifyNodeAsk =
  | { kind: 'handoff'; text: string; from: string; source: 'agent' | 'platform' }
  | { kind: 'task'; text: string }

defineProps<{ ask: ClarifyNodeAsk }>()

const { t } = useI18n()
</script>

<template>
  <div
    v-if="ask.kind === 'handoff'"
    class="flex justify-end"
    data-turn-role="human"
    data-testid="clarify-incoming-handoff"
    :data-source="ask.source"
  >
    <span class="sr-only">{{ t('pages.clarify.me') }}</span>
    <div class="chat-user-message chat-response-summary min-w-0 max-w-[88%]">
      <div class="chat-response-heading flex items-center gap-2 text-txt2" data-testid="clarify-incoming-handoff-from">
        {{ ask.source === 'platform'
          ? t('pages.clarify.handoffFromPlatform', { name: ask.from })
          : t('pages.clarify.handoffFrom', { name: ask.from }) }}
      </div>
      <div
        class="chat-prose md break-words [overflow-wrap:anywhere]"
        data-testid="clarify-incoming-handoff-text"
        v-html="renderChatMarkdown(ask.text)"
      />
    </div>
  </div>
  <div
    v-else
    class="flex justify-end"
    data-turn-role="human"
    data-testid="clarify-node-task"
  >
    <span class="sr-only">{{ t('pages.clarify.me') }}</span>
    <div class="min-w-0 max-w-[88%]">
      <ChatUserMessage :text="ask.text" />
    </div>
  </div>
</template>

<style scoped>
.chat-response-heading { margin-bottom: 10px; font-size: 12px; font-weight: 500; }
</style>
