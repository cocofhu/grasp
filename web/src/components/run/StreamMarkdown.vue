<script setup lang="ts">
/**
 * Streaming markdown as one v-html per top-level block (renderMarkdownBlocks).
 * Settled blocks keep the same HTML string, so Vue never re-sets their
 * innerHTML while the reply grows; only the block being written re-renders.
 */
defineProps<{ blocks: string[] }>()
</script>

<template>
  <span class="stream-md" data-testid="stream-md">
    <span v-for="(b, i) in blocks" :key="i" class="stream-md-block" data-testid="stream-md-block" v-html="b" />
  </span>
</template>

<style scoped>
.stream-md,
.stream-md-block {
  display: contents;
}
</style>
