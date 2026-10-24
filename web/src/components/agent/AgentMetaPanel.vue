<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { DEFAULT_WORKSPACE_DIR, type AgentStudioDraft } from '@/lib/agent/agentStudioDraft'
import AgentRuntimeBindings from './AgentRuntimeBindings.vue'
import EnvCredentialHelpModal from './EnvCredentialHelpModal.vue'
import AgentCredentialBindings from './AgentCredentialBindings.vue'
defineProps<{ draft: AgentStudioDraft; agentName: string; focusModelTick?: number }>()
const { t } = useI18n()
const gitHelpOpen = ref(false)
</script>

<template>
  <div class="scroll-area min-h-0 flex-1 overflow-auto p-4">
    <div class="max-w-3xl space-y-4">
      <AgentRuntimeBindings :key="agentName" :draft="draft" inheritance @help="gitHelpOpen = true" />
      <label class="block"><span class="text-[12px] font-medium text-txt2">{{ t('pages.agentStudio.meta.configRoot') }}</span><input v-model="draft.layout.configRoot" class="mt-1.5 w-full rounded-md border border-line bg-base px-3 py-2 font-mono text-[13px] text-txt outline-none focus:border-accent" data-testid="meta-config-root" /></label>
      <label class="block"><span class="text-[12px] font-medium text-txt2">{{ t('pages.agentStudio.meta.workspaceDir') }}</span><input v-model="draft.layout.workspaceDir" :placeholder="DEFAULT_WORKSPACE_DIR" class="mt-1.5 w-full rounded-md border border-line bg-base px-3 py-2 font-mono text-[13px] text-txt outline-none focus:border-accent" data-testid="meta-workspace-dir" /></label>
    </div>
    <EnvCredentialHelpModal :open="gitHelpOpen" section="git" :backend="draft.acpBackend" @close="gitHelpOpen = false" />
    <AgentCredentialBindings :draft="draft" />
    <p class="mt-3 max-w-3xl text-[11px] leading-5 text-txt3">{{ t('pages.agentStudio.meta.capabilitiesNote') }}</p>
  </div>
</template>
