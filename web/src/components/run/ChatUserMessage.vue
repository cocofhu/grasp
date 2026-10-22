<script setup lang="ts">
/**
 * The conversation's user bubble. ClarifyChat and the run LLM transcript
 * both render user text through this one element. Same chat markdown as
 * assistant replies, so tags stay visible and never execute.
 */
import { renderChatMarkdown } from '@/lib/shared/markdown'

defineProps<{
  text: string
  /** Omit on history rows that are not the live human bubble. */
  testId?: string
}>()
</script>

<template>
  <div
    class="chat-user-message break-words [overflow-wrap:anywhere]"
    :data-testid="testId || undefined"
  >
    <div class="chat-prose md" v-html="renderChatMarkdown(text)" />
  </div>
</template>
