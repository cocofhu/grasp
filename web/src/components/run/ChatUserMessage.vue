<script setup lang="ts">
/**
 * The conversation's plain-text user bubble. ClarifyChat and the run LLM
 * transcript both render user text through this one element.
 */
defineProps<{
  text: string
  /** Omit on history rows that are not the live human bubble. */
  testId?: string
}>()
</script>

<template>
  <div
    class="chat-user-message whitespace-pre-wrap break-words [overflow-wrap:anywhere]"
    :data-testid="testId || undefined"
  >{{ text }}</div>
</template>
