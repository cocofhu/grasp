<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import RunChatView from './RunChatView.vue'
import RunDetailView from './RunDetailView.vue'
import { isRunDetailsQuery } from '@/lib/run/runViewMode'

const route = useRoute()
// Keep existing node/artifact deep links and explicit details views working.
const details = computed(() => isRunDetailsQuery(route.query))
</script>

<template>
  <RunDetailView v-if="details" />
  <RunChatView v-else />
</template>
