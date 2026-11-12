<script setup lang="ts">
import type { AgentStudioDraft } from '@/lib/agent/agentStudioDraft'
import AgentRuntimeBindings from './AgentRuntimeBindings.vue'
defineProps<{ draft: AgentStudioDraft; agentName: string; focusModelTick?: number }>()
</script>

<template>
  <div class="scroll-area min-h-0 flex-1 overflow-auto p-4">
    <div class="max-w-3xl space-y-4">
      <AgentRuntimeBindings :key="agentName" :draft="draft" inheritance collapse-inherited />
    </div>
  </div>
</template>
