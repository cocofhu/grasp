<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { onBeforeRouteLeave, useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api/api'
import { useWorkspaceContext } from '@/lib/composables/useWorkspaceContext'
import type { PmLeaderBinding } from '@/lib/shared/types'
import WorkspacePage from '@/components/shell/WorkspacePage.vue'
import AppButton from '@/components/ui/AppButton.vue'
import BoardView from '@/views/BoardView.vue'
import PmLeaderChat from '@/components/pm/PmLeaderChat.vue'
import PmSettingsPanel from '@/components/pm/PmSettingsPanel.vue'
import PmCronJobsPanel from '@/components/pm/PmCronJobsPanel.vue'
import RequirementDraftsPanel from '@/components/project/RequirementDraftsPanel.vue'
import ProjectNotifyPanel from '@/components/project/ProjectNotifyPanel.vue'
import ProjectExternalMcpPanel from '@/components/project/ProjectExternalMcpPanel.vue'
import ProjectSharedAgentPanel from '@/components/project/ProjectSharedAgentPanel.vue'
import ProjectCredentialsPanel from '@/components/project/ProjectCredentialsPanel.vue'
import ProjectAuditPanel from '@/components/project/ProjectAuditPanel.vue'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const { workspace, workspaceId, reload } = useWorkspaceContext()
const tool = computed(() => String(route.meta.workspaceTool || 'board'))
const title = computed(() => String(route.meta.workspaceTitle || 'nav.board'))
const documentTool = computed(() => ['assistantSettings', 'notificationPolicy', 'externalMcp'].includes(tool.value))
const binding = ref<PmLeaderBinding | null>(null)
const bindingLoading = ref(false)
const bindingError = ref('')
const drafts = ref<{ isDirty: boolean; requestLeave: () => Promise<boolean> } | null>(null)
let bindingGeneration = 0
async function loadBinding() {
  if (!workspaceId.value || tool.value !== 'assistant') return
  const current = ++bindingGeneration
  bindingLoading.value = true
  bindingError.value = ''
  try {
    const result = await api.getPmLeader(workspaceId.value)
    if (current === bindingGeneration) binding.value = result
  } catch (cause: unknown) {
    if (current === bindingGeneration) bindingError.value = cause instanceof Error ? cause.message : String(cause)
  } finally { if (current === bindingGeneration) bindingLoading.value = false }
}
watch([workspaceId, tool], () => {
  bindingGeneration++
  binding.value = null
  void loadBinding()
}, { immediate: true })
onBeforeRouteLeave(() => drafts.value?.isDirty ? drafts.value.requestLeave() : true)
</script>

<template>
  <WorkspacePage :title-key="title" :layout="documentTool ? 'settings' : 'workspace'">
    <template #actions>
      <AppButton v-if="tool === 'assistantSettings'" icon="chat" @click="router.push('/assistant')">{{ t('nav.openAssistant') }}</AppButton>
    </template>
    <div v-if="workspace" :key="tool" class="flex min-w-0 flex-col" :class="documentTool ? 'gap-8' : ['min-h-0 flex-1', { 'scroll-area overflow-y-auto': tool === 'schedules' }]" :data-testid="`workspace-tool-${tool}`">
      <BoardView v-if="tool === 'board'" :project-id="workspaceId" embedded class="min-h-0 flex-1" />
      <RequirementDraftsPanel v-else-if="tool === 'requirements'" ref="drafts" :project-id="workspaceId" />
      <template v-else-if="tool === 'assistant'">
        <p v-if="bindingLoading" role="status" class="p-6 text-sm text-txt3">{{ t('nav.workspaceLoading') }}</p>
        <div v-else-if="bindingError" class="flex flex-col items-start gap-3 p-6" role="alert"><p>{{ bindingError }}</p><AppButton @click="loadBinding">{{ t('nav.workspaceRetry') }}</AppButton></div>
        <PmLeaderChat v-else :project-id="workspaceId" :binding="binding" :unknown-model-display-name="workspace.unknownModelDisplayName" @open-settings="router.push('/settings/assistant')" />
      </template>
      <PmSettingsPanel v-else-if="tool === 'assistantSettings'" :project-id="workspaceId" :project="workspace" @changed="reload" @project-updated="workspace = $event" />
      <PmCronJobsPanel v-else-if="tool === 'schedules'" :project-id="workspaceId" />
      <ProjectNotifyPanel v-else-if="tool === 'notificationPolicy'" :project-id="workspaceId" :project="workspace" @updated="workspace = $event" @open-channel-settings="router.push('/settings/assistant')" />
      <ProjectExternalMcpPanel v-else-if="tool === 'externalMcp'" :project-id="workspaceId" />
      <ProjectSharedAgentPanel v-else-if="tool === 'sharedAgent'" :project-id="workspaceId" />
      <ProjectCredentialsPanel v-else-if="tool === 'credentials'" :project-id="workspaceId" />
      <ProjectAuditPanel v-else-if="tool === 'audit'" :project-id="workspaceId" />
    </div>
  </WorkspacePage>
</template>
