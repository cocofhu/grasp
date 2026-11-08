<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import RunChatView from './RunChatView.vue'
import RunDetailView from './RunDetailView.vue'

const route = useRoute()
// Keep existing node/artifact deep links and explicit details views working.
const details = computed(() => ['details', 'canvas', 'timeline', 'stats', 'llm'].includes(String(route.query.view || '')) || !!route.query.node || !!route.query.tab)
</script>

<template>
  <RunDetailView v-if="details" />
  <RunChatView v-else />
</template>
