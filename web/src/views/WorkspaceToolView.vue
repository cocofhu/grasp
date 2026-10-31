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
import RequirementDraftsPanel from '@/components/workspace/RequirementDraftsPanel.vue'
import WorkspaceNotifyPanel from '@/components/workspace/WorkspaceNotifyPanel.vue'
import WorkspaceExternalMcpPanel from '@/components/workspace/WorkspaceExternalMcpPanel.vue'
import WorkspaceCredentialsPanel from '@/components/workspace/WorkspaceCredentialsPanel.vue'
import WorkspaceAuditPanel from '@/components/workspace/WorkspaceAuditPanel.vue'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const { workspace, reload } = useWorkspaceContext()
const tool = computed(() => String(route.meta.workspaceTool || 'board'))
const title = computed(() => String(route.meta.workspaceTitle || 'nav.board'))
const documentTool = computed(() => ['assistantSettings', 'notificationPolicy', 'externalMcp'].includes(tool.value))
const SETTINGS_HEADERS: Record<string, { icon: string; descriptionKey: string }> = {
  assistant: { icon: 'chat', descriptionKey: 'pages.workspace.pm.scopeLabel' },
  assistantSettings: { icon: 'chat', descriptionKey: 'pages.settingsOverview.desc.assistantSettings' },
  schedules: { icon: 'clock', descriptionKey: 'pages.settingsOverview.desc.schedules' },
  notificationPolicy: { icon: 'bell-settings', descriptionKey: 'pages.settingsOverview.desc.notificationPolicy' },
  externalMcp: { icon: 'connector', descriptionKey: 'pages.workspace.externalMcp.subtitle' },
  audit: { icon: 'history', descriptionKey: 'pages.settingsOverview.desc.audit' },
}
const settingsHeader = computed(() => SETTINGS_HEADERS[tool.value])
const binding = ref<PmLeaderBinding | null>(null)
const bindingLoading = ref(false)
const bindingError = ref('')
const drafts = ref<{ isDirty: boolean; requestLeave: () => Promise<boolean> } | null>(null)
let bindingGeneration = 0
async function loadBinding() {
  if (tool.value !== 'assistant') return
  const current = ++bindingGeneration
  bindingLoading.value = true
  bindingError.value = ''
  try {
    const result = await api.getPmLeader()
    if (current === bindingGeneration) binding.value = result
  } catch (cause: unknown) {
    if (current === bindingGeneration) bindingError.value = cause instanceof Error ? cause.message : String(cause)
  } finally { if (current === bindingGeneration) bindingLoading.value = false }
}
watch(tool, () => {
  bindingGeneration++
  binding.value = null
  void loadBinding()
}, { immediate: true })
onBeforeRouteLeave(() => drafts.value?.isDirty ? drafts.value.requestLeave() : true)
</script>

<template>
  <WorkspacePage :title-key="title" :layout="documentTool ? 'settings' : 'workspace'" :icon="settingsHeader?.icon" :description-key="settingsHeader?.descriptionKey" :hide-header="tool === 'credentials'">
    <template #actions>
      <AppButton v-if="tool === 'assistantSettings'" icon="chat" @click="router.push('/assistant')">{{ t('nav.openAssistant') }}</AppButton>
    </template>
    <div v-if="workspace" :key="tool" class="flex min-w-0 flex-col" :class="documentTool ? 'gap-6' : ['min-h-0 flex-1', { 'scroll-area overflow-y-auto': tool === 'schedules' }]" :data-testid="`workspace-tool-${tool}`">
      <BoardView v-if="tool === 'board'" embedded class="min-h-0 flex-1" />
      <RequirementDraftsPanel v-else-if="tool === 'requirements'" ref="drafts" />
      <template v-else-if="tool === 'assistant'">
        <p v-if="bindingLoading" role="status" class="p-6 text-sm text-txt3">{{ t('nav.workspaceLoading') }}</p>
        <div v-else-if="bindingError" class="flex flex-col items-start gap-3 p-6" role="alert"><p>{{ bindingError }}</p><AppButton @click="loadBinding">{{ t('nav.workspaceRetry') }}</AppButton></div>
        <PmLeaderChat v-else :binding="binding" :unknown-model-display-name="workspace.unknownModelDisplayName" @open-settings="router.push('/settings/assistant')" />
      </template>
      <PmSettingsPanel v-else-if="tool === 'assistantSettings'" :workspace="workspace" @changed="reload" @workspace-updated="workspace = $event" />
      <PmCronJobsPanel v-else-if="tool === 'schedules'" />
      <WorkspaceNotifyPanel v-else-if="tool === 'notificationPolicy'" :workspace="workspace" @updated="workspace = $event" @open-channel-settings="router.push('/settings/assistant')" />
      <WorkspaceExternalMcpPanel v-else-if="tool === 'externalMcp'" />
      <WorkspaceCredentialsPanel v-else-if="tool === 'credentials'" />
      <WorkspaceAuditPanel v-else-if="tool === 'audit'" />
    </div>
  </WorkspacePage>
</template>
