<script setup lang="ts">
import { ref } from 'vue'
import type { AgentStudioDraft } from '@/lib/agent/agentStudioDraft'
import AgentRuntimeBindings from './AgentRuntimeBindings.vue'
import EnvCredentialHelpModal from './EnvCredentialHelpModal.vue'
defineProps<{ draft: AgentStudioDraft; agentName: string; focusModelTick?: number }>()
const gitHelpOpen = ref(false)
</script>

<template>
  <div class="scroll-area min-h-0 flex-1 overflow-auto p-4">
    <div class="max-w-3xl space-y-4">
      <AgentRuntimeBindings :key="agentName" :draft="draft" inheritance collapse-inherited @help="gitHelpOpen = true" />
    </div>
    <EnvCredentialHelpModal :open="gitHelpOpen" section="git" :backend="draft.acpBackend" @close="gitHelpOpen = false" />
  </div>
</template>
