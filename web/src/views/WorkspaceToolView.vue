<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useWorkspaceContext } from '@/lib/composables/useWorkspaceContext'
import WorkspacePage from '@/components/shell/WorkspacePage.vue'
import BoardView from '@/views/BoardView.vue'
import WorkspaceCredentialsPanel from '@/components/workspace/WorkspaceCredentialsPanel.vue'
import WorkspaceAuditPanel from '@/components/workspace/WorkspaceAuditPanel.vue'

const route = useRoute()
const { workspace } = useWorkspaceContext()
const tool = computed(() => String(route.meta.workspaceTool || 'board'))
const title = computed(() => String(route.meta.workspaceTitle || 'nav.board'))
const SETTINGS_HEADERS: Record<string, { icon: string; descriptionKey: string }> = {
  audit: { icon: 'history', descriptionKey: 'pages.settingsOverview.desc.audit' },
}
const settingsHeader = computed(() => SETTINGS_HEADERS[tool.value])
</script>

<template>
  <WorkspacePage :title-key="title" :icon="settingsHeader?.icon" :description-key="settingsHeader?.descriptionKey" :hide-header="tool === 'credentials'">
    <div v-if="workspace" :key="tool" class="flex min-h-0 min-w-0 flex-1 flex-col" :data-testid="`workspace-tool-${tool}`">
      <BoardView v-if="tool === 'board'" embedded class="min-h-0 flex-1" />
      <WorkspaceCredentialsPanel v-else-if="tool === 'credentials'" />
      <WorkspaceAuditPanel v-else-if="tool === 'audit'" />
    </div>
  </WorkspacePage>
</template>
