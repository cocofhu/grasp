<script setup lang="ts">
import Icon from '@/components/ui/Icon.vue'
import AppButton from '@/components/ui/AppButton.vue'
import AppModal from '@/components/ui/AppModal.vue'
import AgentProjectSidebar from '@/components/agent/AgentProjectSidebar.vue'
import ProjectTree from '@/components/ui/ProjectTree.vue'
import AgentDataPanel, { type DataSubTab } from '@/components/agent/AgentDataPanel.vue'
import AgentFilesPanel from '@/components/agent/AgentFilesPanel.vue'
import AgentMcpPanel from '@/components/agent/AgentMcpPanel.vue'
import AgentEnvPanel from '@/components/agent/AgentEnvPanel.vue'
import AgentCapabilitiesPanel from '@/components/agent/AgentCapabilitiesPanel.vue'
import AgentMetaPanel from '@/components/agent/AgentMetaPanel.vue'
import AgentChatTester from '@/components/agent/AgentChatTester.vue'
import AgentCreateWizard from '@/components/agent/AgentCreateWizard.vue'
import CreateAgentTeamWizard from '@/components/agent/CreateAgentTeamWizard.vue'
import TeamBootstrapPanel from '@/components/agent/TeamBootstrapPanel.vue'
import { api, type CreateAgentTestPayload, type SandboxView } from '@/lib/api/api'
import { useAgentStudio } from '@/lib/agent/useAgentStudio'

const props = defineProps<{ projectId?: string; embedded?: boolean }>()

const {
  t,
  isMobile,
  cardGridStyle,
  justSaved,
  filesPanelRef,
  agentNameEl,
  tabStripEl,
  agentNameTruncated,
  showFullNameTip,
  fullNameTipStyle,
  tabFadeLeft,
  tabFadeRight,
  closeFullNameTip,
  showProjectSheet,
  openProjectSheet,
  closeProjectSheet,
  leaveConfirmCfg,
  agents,
  projects,
  treeProjects,
  treeNodes,
  activeTreeKey,
  onTreeSelect,
  onSheetTreeSelect,
  agentListCollapsed,
  toggleAgentListCollapsed,
  activeName,
  draft,
  tab,
  dataSubTab,
  savedProjectId,
  draftBindingDirty,
  projectNameById,
  loading,
  loadFailed,
  loadDenied,
  error,
  saving,
  initialLoading,
  showRefreshProgress,
  toastMsg,
  promptCfg,
  promptValue,
  promptError,
  promptOkMsg,
  promptCanSubmit,
  refreshPromptFeedback,
  confirmCfg,
  showAgentManage,
  manageFocusAgent,
  showUnsavedExport,
  exporting,
  showBundleSecrets,
  importFileInput,
  showImportDiscardConfirm,
  showImportConflict,
  showImportErrorModal,
  importErrorMessage,
  importConflictName,
  importConflictAction,
  importRenameValue,
  importRenameError,
  showBatchConflict,
  batchConflictNames,
  triggerImport,
  showImportProjectPick,
  importProjectId,
  cancelImportProjectPick,
  confirmImportProjectPick,
  onImportDiscardCancel,
  onImportDiscardConfirm,
  onImportFileChange,
  selectImportConflict,
  closeImportConflict,
  confirmImportConflict,
  closeBatchConflict,
  confirmBatchRename,
  confirmBatchOverwrite,
  dirty,
  agentNames,
  manageSearch,
  filteredManageNames,
  manageSearchActive,
  manageNameHighlight,
  clearManageSearch,
  studioTabs,
  studioTabLabel,
  showToast,
  openSettingsInFiles,
  discardUnsavedChanges,
  requestStudioTab,
  onDataSubTab,
  leaveConfirmSave,
  leaveConfirmDiscard,
  leaveConfirmCancel,
  load,
  chooseAgentFromSheet,
  openManageFromSheet,
  save,
  confirmSaveWithReason,
  showSaveReasonModal,
  saveReason,
  historyRefreshKey,
  reloadAgentFromServer,
  triggerExport,
  cancelUnsavedExport,
  discardAndExport,
  saveThenExport,
  onExportProject,
  cancelBundleSecrets,
  confirmBundleSecrets,
  onImportProject,
  showCreateWizard,
  showTeamWizard,
  teamBootstrapSessionId,
  createAgentProjectId,
  openCreateAgent,
  openCreateTeam,
  showCreateTeam,
  hideTeamCreate,
  embedded,
  chooseAgent,
  onWizardCreated,
  onTeamBootstrapStarted,
  onTeamBootstrapRefresh,
  onTeamBootstrapSelectPm,
  onTeamBootstrapOpenPm,
  onTeamBootstrapDone,
  openAgentManage,
  closeAgentManage,
  openRenameAgent,
  confirmDeleteAgent,
  promptOk,
  confirmOk,
  onAgentNameClick,
  syncTabFade,
} = useAgentStudio({ projectId: () => props.projectId, embedded: () => !!props.embedded })

async function createStudioChatTest(
  profile: string,
  payload: CreateAgentTestPayload,
): Promise<SandboxView> {
  return api.createProjectSharedAgentTest(savedProjectId.value, {
    agentName: profile,
    ...(payload.repos ? { repos: payload.repos } : {}),
    ...(payload.repoUrl ? { repoUrl: payload.repoUrl } : {}),
  })
}
</script>
<template>
  <div
    class="flex h-full min-h-0 flex-col overflow-hidden"
    data-testid="agent-studio-panel"
    :aria-busy="loading ? 'true' : 'false'"
  >
    <div
      v-if="error && !loadFailed && !loadDenied && agents.length"
      class="card mb-3 shrink-0 border-err/40 p-3 text-[13px] text-err"
    >{{ t('pages.agentStudio.errorPrefix') }}{{ error }}</div>

    <div class="flex min-h-0 flex-1 flex-col">
      <div
        v-if="showRefreshProgress"
        class="mb-2 h-[2px] overflow-hidden bg-line"
        data-testid="agent-studio-thin-progress"
        aria-hidden="true"
      >
        <i class="admin-list-thin-bar bg-accent" />
      </div>

      <div
        v-if="initialLoading"
        class="card grid min-h-0 flex-1 overflow-hidden"
        data-testid="agent-studio-skeleton"
        aria-hidden="true"
        :style="isMobile ? { gridTemplateColumns: '1fr' } : { gridTemplateColumns: '280px 1fr' }"
      >
        <div v-if="!isMobile" class="border-r border-line bg-base p-3">
          <div class="mb-3 h-3 w-16 bg-elevated animate-pulse" />
          <div v-for="n in 6" :key="'tree-skel-' + n" class="mb-1.5 h-8 bg-elevated animate-pulse" />
        </div>
        <div class="space-y-3 p-4">
          <div class="h-8 w-48 bg-elevated animate-pulse" />
          <div class="h-10 w-full bg-elevated animate-pulse" />
          <div class="h-40 w-full bg-elevated animate-pulse" />
        </div>
      </div>

      <div
        v-else-if="loadDenied"
        role="status"
        data-testid="agent-studio-denied"
        class="card flex flex-1 flex-col items-center justify-center border-warn/40 bg-warn/10 px-6 text-center"
      >
        <Icon name="lock" :size="22" class="mb-3 text-warn" />
        <h3 class="text-sm font-semibold text-txt">{{ t('common.asyncState.permissionDeniedTitle') }}</h3>
        <p class="mt-1 max-w-md text-xs text-txt2">{{ t('common.asyncState.permissionDeniedDesc') }}</p>
        <AppButton class="mt-4" variant="outline" data-testid="agent-studio-retry" @click="load">
          {{ t('common.buttons.retry') }}
        </AppButton>
      </div>

      <div
        v-else-if="loadFailed"
        role="status"
        data-testid="agent-studio-failed"
        class="card flex flex-1 flex-col items-center justify-center border-err/40 bg-err/10 px-6 text-center"
      >
        <h3 class="text-sm font-semibold text-txt">{{ t('common.asyncState.loadFailedTitle') }}</h3>
        <p class="mt-1 max-w-md text-xs text-txt2">{{ t('common.asyncState.loadFailedDesc') }}</p>
        <AppButton class="mt-4" variant="outline" data-testid="agent-studio-retry" @click="load">
          {{ t('common.buttons.retry') }}
        </AppButton>
      </div>

      <div
        v-else-if="!agents.length && !teamBootstrapSessionId"
        class="card flex flex-1 flex-col items-center justify-center gap-3 px-6 text-center"
        data-testid="agent-studio-empty-team"
      >
        <template v-if="embedded">
          <h2 class="m-0 text-[18px] font-semibold text-txt">{{ t('pages.agentStudio.emptyProjectTitle') }}</h2>
          <p class="m-0 max-w-md text-[13px] leading-6 text-txt3">{{ t('pages.agentStudio.emptyProjectDesc') }}</p>
          <AppButton variant="primary" icon="plus" data-testid="agent-studio-empty-create" @click="openCreateAgent()">
            {{ t('common.buttons.newAgent') }}
          </AppButton>
          <button
            type="button"
            data-testid="agent-studio-empty-import"
            class="text-[12px] text-accent-2 hover:underline"
            @click="triggerImport"
          >
            {{ t('pages.agentStudio.exportImport.import') }}
          </button>
        </template>
        <template v-else>
          <h2 class="m-0 text-[18px] font-semibold text-txt">{{ t('pages.agentStudio.emptyTeamTitle') }}</h2>
          <p class="m-0 max-w-md text-[13px] leading-6 text-txt3">{{ t('pages.agentStudio.emptyTeamDesc') }}</p>
          <AppButton variant="primary" icon="skills" data-testid="agent-studio-empty-create-team" @click="openCreateTeam">
            {{ t('pages.agentStudio.emptyTeamCta') }}
          </AppButton>
          <button type="button" class="text-[12px] text-accent-2 hover:underline" @click="openCreateAgent()">
            {{ t('pages.agentStudio.emptyTeamOrSingle') }}
          </button>
          <button
            type="button"
            data-testid="agent-studio-empty-import"
            class="text-[12px] text-accent-2 hover:underline"
            @click="triggerImport"
          >
            {{ t('pages.agentStudio.exportImport.import') }}
          </button>
        </template>
      </div>

      <div
        v-else
        class="card grid min-h-0 flex-1 overflow-hidden transition-[grid-template-columns] duration-[var(--dur-ui)] ease-[var(--ease-out-expo)]"
        :class="showRefreshProgress ? 'opacity-[0.55]' : ''"
        :style="cardGridStyle"
      >
      <!-- project → Agent tree (hidden on narrow screens; agent name bar remains) -->
      <AgentProjectSidebar
        v-if="!isMobile"
        :nodes="treeNodes"
        :active-key="activeTreeKey"
        :collapsed="agentListCollapsed"
        :hide-create-team="hideTeamCreate"
        @select="onTreeSelect"
        @open-manage="openAgentManage"
        @import="triggerImport"
        @create-agent="openCreateAgent"
        @create-team="openCreateTeam"
        @export-project="onExportProject"
        @import-project="onImportProject"
        @toggle-collapsed="toggleAgentListCollapsed"
      />

      <TeamBootstrapPanel
        v-if="teamBootstrapSessionId"
        class="min-h-0 min-w-0"
        :session-id="teamBootstrapSessionId"
        @open-pm="onTeamBootstrapOpenPm"
        @select-pm="onTeamBootstrapSelectPm"
        @done="onTeamBootstrapDone"
        @refresh="onTeamBootstrapRefresh"
      />

      <!-- editor -->
      <div v-else-if="draft" class="flex min-h-0 min-w-0 flex-col overflow-hidden">
        <div
          v-if="isMobile"
          data-test="studio-name-bar"
          class="toolbar-inline-row flex flex-col gap-2 border-b border-line px-4"
        >
          <div data-test="studio-name-row-top" class="flex min-w-0 items-center gap-2">
            <Icon name="robot" :size="15" class="shrink-0 text-accent-2" />
            <button
              ref="agentNameEl"
              type="button"
              data-test="agent-name"
              class="min-w-0 flex-1 truncate text-left text-[13px] font-medium text-txt"
              :class="agentNameTruncated ? 'cursor-pointer' : 'cursor-default'"
              :title="activeName"
              :aria-label="agentNameTruncated ? t('pages.agentStudio.mobile.fullNameAria') : undefined"
              @click="onAgentNameClick"
            >{{ activeName }}</button>
            <button
              type="button"
              data-test="project-switch"
              class="inline-flex min-h-11 shrink-0 items-center gap-1 rounded border border-line bg-elevated px-2.5 text-[12px] text-txt2 transition hover:border-line-strong hover:text-txt"
              :title="t('pages.agentStudio.mobile.switchTitle')"
              :aria-label="t('pages.agentStudio.mobile.switchAria')"
              @click="openProjectSheet"
            >
              <Icon name="menu" :size="14" />
              <span>{{ t('pages.agentStudio.mobile.switch') }}</span>
            </button>
            <span v-if="dirty" class="chip shrink-0 border-warn/30 text-warn">{{ t('pages.agentStudio.unsaved') }}</span>
            <span
              v-else-if="justSaved"
              class="chip shrink-0 border-ok/30 bg-ok/10 text-ok"
            >{{ t('pages.agentStudio.saved') }}</span>
          </div>
          <div data-test="studio-name-row-bottom" class="flex items-center gap-2">
            <AppButton
              data-test="studio-export"
              variant="outline"
              icon="download"
              class="min-h-11"
              :disabled="exporting"
              @click="triggerExport"
            >
              {{ t('pages.agentStudio.exportImport.export') }}
            </AppButton>
            <AppButton
              v-if="dirty"
              data-test="studio-save"
              variant="primary"
              class="min-h-11"
              :disabled="saving"
              @click="() => save()"
            >
              {{ saving ? t('common.buttons.saving') : t('common.buttons.save') }}
            </AppButton>
          </div>
        </div>
        <div
          v-else
          data-test="studio-name-bar"
          class="toolbar-inline-row flex items-center gap-2 border-b border-line px-4"
        >
          <Icon name="robot" :size="15" class="shrink-0 text-accent-2" />
          <span class="min-w-0 truncate text-[13px] font-medium text-txt" :title="activeName">{{ activeName }}</span>
          <span v-if="dirty" class="chip shrink-0 border-warn/30 text-warn">{{ t('pages.agentStudio.unsaved') }}</span>
          <span
            v-else-if="justSaved"
            class="chip shrink-0 border-ok/30 bg-ok/10 text-ok"
          >{{ t('pages.agentStudio.saved') }}</span>
          <div class="ml-auto flex shrink-0 items-center gap-2">
            <AppButton
              data-test="studio-export"
              size="sm"
              variant="outline"
              icon="download"
              :disabled="exporting"
              @click="triggerExport"
            >
              {{ t('pages.agentStudio.exportImport.export') }}
            </AppButton>
            <AppButton
              data-test="studio-save"
              size="sm"
              variant="primary"
              :disabled="!dirty || saving"
              @click="() => save()"
            >
              {{ saving ? t('common.buttons.saving') : dirty ? t('common.buttons.save') : t('pages.agentStudio.saved') }}
            </AppButton>
          </div>
        </div>

        <div data-test="studio-tabs" class="relative min-w-0 border-b border-line">
          <div
            ref="tabStripEl"
            data-test="studio-tab-strip"
            class="scroll-area flex min-w-0 gap-1 overflow-x-auto px-3 pt-2 [-webkit-overflow-scrolling:touch]"
            @scroll="syncTabFade"
          >
            <button
              v-for="tabItem in studioTabs"
              :key="tabItem.k"
              class="shrink-0 whitespace-nowrap px-3 text-[12px] transition"
              :class="[
                isMobile ? 'min-h-11 py-2.5' : 'rounded-t py-1.5',
                tab === tabItem.k ? 'border-b-2 border-accent text-txt' : 'text-txt3 hover:text-txt2',
              ]"
              @click="requestStudioTab(tabItem.k)"
            >{{ tabItem.l }}</button>
          </div>
          <div
            v-if="isMobile && tabFadeLeft"
            data-test="tab-fade-left"
            class="pointer-events-none absolute inset-y-0 left-0 flex w-10 items-center justify-center bg-gradient-to-r from-surface to-transparent text-txt2"
            aria-hidden="true"
          >
            <Icon name="chevron-left" :size="14" />
          </div>
          <div
            v-if="isMobile && tabFadeRight"
            data-test="tab-fade-right"
            class="pointer-events-none absolute inset-y-0 right-0 flex w-10 items-center justify-center bg-gradient-to-l from-surface to-transparent text-txt2"
            aria-hidden="true"
          >
            <Icon name="chevron-right" :size="14" />
          </div>
        </div>

        <AgentFilesPanel
          v-if="draft"
          v-show="tab === 'files'"
          ref="filesPanelRef"
          :draft="draft"
          :dirty="dirty"
          :is-mobile="isMobile"
          :agent-name="activeName"
          :history-refresh-key="historyRefreshKey"
          :save="save"
          @error="error = $event"
          @toast="showToast"
          @update:just-saved="justSaved = $event"
          @discard="discardUnsavedChanges"
          @restored="reloadAgentFromServer(activeName)"
        />

        <!-- narrow-screen: non-whitelist tabs show desktop-only tip (files+data+test allowed) -->
        <Transition name="ui-fade" mode="out-in">
          <div
            v-if="tab !== 'files' && isMobile && tab !== 'data' && tab !== 'test'"
            key="mobile-desktop-only"
            class="flex min-h-0 flex-1 flex-col items-center justify-center gap-3 px-5 py-8 text-center"
          >
          <div class="rounded-md flex h-10 w-10 items-center justify-center border border-info/35 bg-info/10 text-info">
            <Icon name="alert" :size="20" />
          </div>
          <h3 class="text-[14px] font-semibold text-txt">{{ t('pages.agentStudio.mobile.desktopOnlyTitle') }}</h3>
          <p class="max-w-[28ch] text-[12.5px] leading-relaxed text-txt2">
            {{ t('pages.agentStudio.mobile.desktopOnlyDesc', { tab: studioTabLabel }) }}
          </p>
          <button
            type="button"
            class="rounded-md min-h-11 border border-line bg-transparent px-4 text-[13px] text-txt2 hover:border-accent hover:text-txt"
            data-testid="studio-mobile-back-files"
            @click="requestStudioTab('files')"
          >
            {{ t('pages.agentStudio.mobile.backToFiles') }}
          </button>
          </div>

          <AgentMcpPanel
            v-else-if="tab === 'mcp' && draft && !isMobile"
            key="mcp"
            :draft="draft"
            @toast="showToast"
          />

          <AgentEnvPanel
            v-else-if="tab === 'env' && draft && !isMobile"
            key="env"
            :draft="draft"
            context="agent"
            @toast="showToast"
            @open-settings-file="openSettingsInFiles"
          />

          <AgentCapabilitiesPanel
            v-else-if="tab === 'capabilities' && draft && !isMobile"
            key="capabilities"
            :draft="draft"
          />

          <!-- data: Agent-scoped memory / context / cron-job management (whitelist on mobile) -->
          <div v-else-if="tab === 'data' && draft" key="data" class="min-h-0 flex-1 overflow-hidden">
          <div v-if="draftBindingDirty" class="border-b border-warn/30 bg-warn/10 px-4 py-2 text-[12px] text-warn">
            {{ t('pages.agentStudio.data.unsavedBinding') }}
          </div>
          <AgentDataPanel
            :agent-name="activeName"
            :project-name="projectNameById(savedProjectId)"
            :sub-tab="dataSubTab"
            @update:sub-tab="onDataSubTab"
          />
          </div>

          <AgentMetaPanel
            v-else-if="tab === 'meta' && draft && !isMobile"
            key="meta"
            :draft="draft"
            :agent-name="activeName"
            :projects="projects"
          />

          <div
            v-else-if="tab === 'test' && draft"
            key="test"
            class="flex min-h-0 flex-1 flex-col overflow-hidden"
            data-testid="studio-chat-test"
          >
            <AgentChatTester
              :key="activeName"
              :profile="activeName"
              :home-project-id="savedProjectId"
              :create-test="createStudioChatTest"
            />
          </div>
        </Transition>

      </div>
      <div
        v-else
        class="flex min-h-0 min-w-0 flex-col items-center justify-center gap-2 text-[13px] text-txt3"
      >
        <Icon name="robot" :size="24" class="opacity-50" />
        <span>{{ t('pages.agentStudio.tree.noSelection') }}</span>
      </div>
    </div>
    </div>

    <!-- Agent management (rename + hard delete) -->
    <AppModal
      :open="showAgentManage"
      :title="t('pages.agentStudio.tree.manageTitle')"
      :width="520"
      @close="closeAgentManage"
    >
      <p class="mb-3 text-[12.5px] leading-relaxed text-txt2">{{ t('pages.agentStudio.tree.manageIntro') }}</p>
      <div v-if="!agentNames.length" class="text-[13px] text-txt3">{{ t('pages.agentStudio.tree.manageEmpty') }}</div>
      <template v-else>
        <div class="relative mb-2">
          <Icon name="search" :size="15" class="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-txt3" />
          <input
            v-model="manageSearch"
            type="text"
            autocomplete="off"
            :placeholder="t('pages.agentStudio.tree.manageSearchPlaceholder')"
            class="rounded-md w-full border border-line bg-base py-2 pl-8 pr-8 text-[13px] text-txt outline-none transition focus:border-accent"
            data-test="manage-search"
          />
          <button
            v-if="manageSearch"
            type="button"
            class="absolute right-1.5 top-1/2 flex h-6 w-6 -translate-y-1/2 items-center justify-center text-txt3 hover:bg-elevated hover:text-txt"
            :aria-label="t('pages.agentStudio.tree.manageClearSearch')"
            data-test="manage-search-clear"
            @click="clearManageSearch"
          >
            <Icon name="close" :size="14" />
          </button>
        </div>
        <p class="mb-2 text-[12px] tabular-nums text-txt3" data-test="manage-search-count">
          {{
            manageSearchActive
              ? t('pages.agentStudio.tree.manageSearchCount', { matched: filteredManageNames.length, total: agentNames.length })
              : t('pages.agentStudio.tree.manageTotalCount', { total: agentNames.length })
          }}
        </p>
        <div v-if="manageSearchActive && !filteredManageNames.length" class="rounded-lg border border-dashed border-line px-4 py-8 text-center">
          <Icon name="search" :size="20" class="mx-auto mb-2 text-txt3" />
          <p class="text-[13px] font-medium text-txt">{{ t('pages.agentStudio.tree.manageNoMatchTitle') }}</p>
          <p class="mt-1 text-[12px] text-txt3">{{ t('pages.agentStudio.tree.manageNoMatchDesc') }}</p>
          <AppButton class="mt-3" size="sm" variant="outline" @click="clearManageSearch">
            {{ t('pages.agentStudio.tree.manageClearSearch') }}
          </AppButton>
        </div>
        <div v-else class="flex flex-col gap-0.5">
        <div
          v-for="name in filteredManageNames"
          :key="name"
          :data-manage-agent="name"
          class="rounded-lg flex items-center gap-2 border px-2.5 py-2 transition"
          :class="
            manageFocusAgent === name
              ? 'border-accent/40 bg-accent-dim shadow-[inset_0_0_0_1px_rgba(99,102,241,0.35)]'
              : 'border-transparent hover:bg-elevated'
          "
        >
          <Icon name="robot" :size="14" class="shrink-0 text-accent-2" />
          <span class="min-w-0 flex-1 truncate text-[13px] text-txt">
            <template v-if="manageNameHighlight(name).hit">
              {{ manageNameHighlight(name).before }}<mark class="bg-warn/25 px-0 text-inherit">{{ manageNameHighlight(name).hit }}</mark>{{ manageNameHighlight(name).after }}
            </template>
            <template v-else>{{ name }}</template>
          </span>
          <div class="flex shrink-0 gap-1.5">
            <AppButton size="sm" variant="outline" icon="edit" @click="openRenameAgent(name)">
              {{ t('pages.agentStudio.tree.manageRename') }}
            </AppButton>
            <AppButton size="sm" variant="danger" icon="trash" @click="confirmDeleteAgent(name)">
              {{ t('pages.agentStudio.dialogs.delete') }}
            </AppButton>
          </div>
        </div>
        </div>
      </template>
      <template #footer>
        <AppButton size="sm" variant="ghost" @click="closeAgentManage">{{ t('common.buttons.close') }}</AppButton>
      </template>
    </AppModal>

    <!-- prompt modal (create / rename) -->
    <AppModal :open="!!promptCfg" :title="promptCfg?.title" :width="420" @close="promptCfg = null">
      <label class="mb-1 block text-[12px] text-txt2">{{ promptCfg?.label }}</label>
      <input
        v-model="promptValue"
        :placeholder="promptCfg?.placeholder"
        class="w-full rounded-md border border-line bg-base px-3 py-2 text-[13px] text-txt outline-none focus:border-accent"
        :class="{
          'border-err': !!promptError,
          'border-ok/55': !!promptOkMsg && !promptError,
        }"
        @input="refreshPromptFeedback"
        @keyup.enter="promptOk"
      />
      <p v-if="promptError" class="mt-2 text-[12px] text-err">{{ promptError }}</p>
      <p v-else-if="promptOkMsg" class="mt-2 text-[12px] text-ok">{{ promptOkMsg }}</p>
      <p v-if="promptCfg?.hint" class="mt-3 text-[12px] leading-relaxed text-txt2">{{ promptCfg.hint }}</p>
      <template #footer>
        <AppButton size="sm" variant="ghost" @click="promptCfg = null">{{ t('common.buttons.cancel') }}</AppButton>
        <AppButton size="sm" variant="primary" :disabled="!promptCanSubmit" @click="promptOk">{{ t('pages.agentStudio.dialogs.confirm') }}</AppButton>
      </template>
    </AppModal>

    <!-- confirm modal (delete / discard) -->
    <AppModal :open="!!confirmCfg" :title="confirmCfg?.title" :width="420" @close="confirmCfg = null">
      <p class="text-[13px] leading-6 text-txt2">{{ confirmCfg?.message }}</p>
      <template #footer>
        <AppButton size="sm" variant="ghost" @click="confirmCfg = null">{{ t('common.buttons.cancel') }}</AppButton>
        <AppButton size="sm" :variant="confirmCfg?.danger ? 'danger' : 'primary'" @click="confirmOk">{{ confirmCfg?.confirmText || t('pages.agentStudio.dialogs.confirm') }}</AppButton>
      </template>
    </AppModal>

    <!-- narrow-screen project tree sheet -->
    <Teleport to="body">
      <div
        v-if="showProjectSheet"
        data-test="project-sheet"
        class="fixed inset-0 z-40"
        role="dialog"
        aria-modal="true"
        :aria-label="t('pages.agentStudio.mobile.sheetTitle')"
      >
        <div
          data-test="project-sheet-backdrop"
          class="absolute inset-0 bg-black/50"
          @click="closeProjectSheet"
        />
        <div
          class="absolute inset-x-0 bottom-0 flex h-[70vh] max-h-[70vh] flex-col overflow-hidden rounded-t-xl border-t border-line bg-elevated shadow-card"
        >
          <div class="mx-auto mt-2 h-1 w-10 shrink-0 rounded-full bg-line-strong/70" aria-hidden="true" />
          <div class="flex shrink-0 items-center gap-1.5 border-b border-line px-3 py-2.5">
            <h3 class="min-w-0 flex-1 truncate text-[14px] font-semibold text-txt">
              {{ t('pages.agentStudio.mobile.sheetTitle') }}
            </h3>
            <button
              type="button"
              data-test="project-sheet-import"
              class="flex min-h-9 min-w-9 shrink-0 items-center justify-center rounded text-txt3 hover:bg-overlay hover:text-txt"
              :title="t('pages.agentStudio.exportImport.import')"
              :aria-label="t('pages.agentStudio.exportImport.import')"
              @click="triggerImport"
            >
              <Icon name="input" :size="14" />
            </button>
            <button
              type="button"
              data-test="project-sheet-create-agent"
              class="flex min-h-9 min-w-9 shrink-0 items-center justify-center rounded text-txt3 hover:bg-overlay hover:text-txt"
              :title="t('common.buttons.newAgent')"
              :aria-label="t('common.buttons.newAgent')"
              @click="openCreateAgent()"
            >
              <Icon name="plus" :size="14" />
            </button>
            <button
              v-if="showCreateTeam"
              type="button"
              data-test="project-sheet-create-team"
              class="flex min-h-9 min-w-9 shrink-0 items-center justify-center rounded text-txt3 hover:bg-overlay hover:text-txt"
              :title="t('pages.agentStudio.tree.newTeam')"
              :aria-label="t('pages.agentStudio.tree.newTeam')"
              @click="openCreateTeam"
            >
              <Icon name="skills" :size="14" />
            </button>
            <AppButton
              size="sm"
              variant="outline"
              data-test="project-sheet-manage"
              class="min-h-9 shrink-0"
              @click="openManageFromSheet"
            >{{ t('pages.agentStudio.tree.manage') }}</AppButton>
            <button
              type="button"
              data-test="project-sheet-close"
              class="flex min-h-9 min-w-9 shrink-0 items-center justify-center rounded text-txt3 hover:bg-overlay hover:text-txt"
              :aria-label="t('common.buttons.close')"
              @click="closeProjectSheet"
            >
              <Icon name="close" :size="14" />
            </button>
          </div>
          <div class="min-h-0 flex-1 overflow-hidden">
            <ProjectTree
              :nodes="treeNodes"
              :active-key="activeTreeKey"
              :title="t('pages.agentStudio.tree.title')"
              :total="agents.length"
              :search-placeholder="t('pages.agentStudio.tree.searchPlaceholder')"
              :empty-text="t('pages.agentStudio.tree.empty')"
              storage-key="agent-studio-sheet-tree"
              @select="onSheetTreeSelect"
            />
          </div>
        </div>
      </div>
    </Teleport>

    <!-- leave edit: save / discard / cancel -->
    <AppModal
      :open="!!leaveConfirmCfg"
      :title="leaveConfirmCfg?.title"
      :width="420"
      @close="leaveConfirmCancel"
    >
      <p class="text-[13px] leading-6 text-txt2">{{ leaveConfirmCfg?.message }}</p>
      <template #footer>
        <div class="flex w-full flex-col gap-2 sm:flex-row sm:justify-end">
          <AppButton
            size="sm"
            variant="primary"
            class="min-h-11 sm:min-h-0"
            :disabled="saving"
            @click="leaveConfirmSave"
          >{{ leaveConfirmCfg?.saveText }}</AppButton>
          <AppButton
            size="sm"
            variant="outline"
            class="min-h-11 border-warn/40 text-warn sm:min-h-0"
            @click="leaveConfirmDiscard"
          >{{ leaveConfirmCfg?.discardText }}</AppButton>
          <AppButton
            size="sm"
            variant="ghost"
            class="min-h-11 sm:min-h-0"
            @click="leaveConfirmCancel"
          >{{ t('common.buttons.cancel') }}</AppButton>
        </div>
      </template>
    </AppModal>

    <!-- project bundle export secrets warning -->
    <AppModal
      :open="showBundleSecrets"
      :title="t('pages.agentStudio.exportImport.bundleSecrets.title')"
      :width="460"
      close-on-esc
      @close="cancelBundleSecrets"
    >
      <p class="text-[13px] leading-6 text-txt2">{{ t('pages.agentStudio.exportImport.bundleSecrets.body') }}</p>
      <template #footer>
        <AppButton size="sm" variant="ghost" @click="cancelBundleSecrets">{{ t('common.buttons.cancel') }}</AppButton>
        <AppButton size="sm" variant="primary" :disabled="exporting" @click="confirmBundleSecrets">
          {{ t('pages.agentStudio.exportImport.bundleSecrets.confirm') }}
        </AppButton>
      </template>
    </AppModal>

    <!-- header import: pick the target project -->
    <AppModal
      :open="showImportProjectPick"
      :title="t('pages.agentStudio.exportImport.pickProject.title')"
      :width="420"
      data-test="import-project-pick"
      close-on-esc
      @close="cancelImportProjectPick"
    >
      <p class="mb-3 text-[13px] leading-6 text-txt2">{{ t('pages.agentStudio.exportImport.pickProject.intro') }}</p>
      <select
        v-model="importProjectId"
        data-test="import-project-select"
        class="w-full rounded border border-line bg-surface px-2 py-1.5 text-[13px] text-txt outline-none focus:border-accent"
      >
        <option v-for="p in projects" :key="p.id" :value="p.id">{{ p.name }}</option>
      </select>
      <template #footer>
        <AppButton size="sm" variant="ghost" @click="cancelImportProjectPick">{{ t('common.buttons.cancel') }}</AppButton>
        <AppButton
          size="sm"
          variant="primary"
          data-test="import-project-confirm"
          :disabled="!importProjectId"
          @click="confirmImportProjectPick"
        >{{ t('pages.agentStudio.exportImport.pickProject.confirm') }}</AppButton>
      </template>
    </AppModal>

    <!-- project bundle name conflict -->
    <AppModal
      :open="showBatchConflict"
      :title="t('pages.agentStudio.exportImport.batchConflict.title')"
      :width="480"
      close-on-esc
      @close="closeBatchConflict"
    >
      <p class="text-[13px] leading-6 text-txt2">{{ t('pages.agentStudio.exportImport.batchConflict.intro') }}</p>
      <ul class="rounded-md mt-2 max-h-32 overflow-auto border border-line bg-base px-3 py-2 text-[12px] text-txt">
        <li v-for="n in batchConflictNames" :key="n" class="font-mono">{{ n }}</li>
      </ul>
      <div class="mt-3 flex flex-col gap-2">
        <button
          type="button"
          class="rounded-lg w-full border border-accent/50 bg-accent-dim px-3 py-2.5 text-left"
          @click="confirmBatchRename"
        >
          <div class="text-[13px] font-medium text-txt">{{ t('pages.agentStudio.exportImport.batchConflict.rename') }}</div>
          <div class="text-[11.5px] text-txt3">{{ t('pages.agentStudio.exportImport.batchConflict.renameDesc') }}</div>
        </button>
        <button
          type="button"
          class="rounded-lg w-full border border-err/40 bg-base px-3 py-2.5 text-left hover:bg-err/10"
          @click="confirmBatchOverwrite"
        >
          <div class="text-[13px] font-medium text-err">{{ t('pages.agentStudio.exportImport.batchConflict.overwrite') }}</div>
          <div class="text-[11.5px] text-txt3">{{ t('pages.agentStudio.exportImport.batchConflict.overwriteDesc') }}</div>
        </button>
        <button
          type="button"
          class="rounded-md w-full border border-line bg-base px-3 py-2.5 text-left hover:bg-elevated"
          @click="closeBatchConflict"
        >
          <div class="text-[13px] font-medium text-txt">{{ t('pages.agentStudio.exportImport.batchConflict.cancel') }}</div>
          <div class="text-[11.5px] text-txt3">{{ t('pages.agentStudio.exportImport.batchConflict.cancelDesc') }}</div>
        </button>
      </div>
    </AppModal>

    <!-- unsaved export guide -->
    <AppModal :open="showUnsavedExport" :title="t('pages.agentStudio.exportImport.unsavedExport.title')" :width="420" @close="cancelUnsavedExport">
      <p class="text-[13px] leading-6 text-txt2">{{ t('pages.agentStudio.exportImport.unsavedExport.message', { name: activeName }) }}</p>
      <template #footer>
        <AppButton size="sm" variant="ghost" @click="cancelUnsavedExport">{{ t('common.buttons.cancel') }}</AppButton>
        <AppButton size="sm" variant="outline" :disabled="exporting" @click="discardAndExport">{{ t('pages.agentStudio.exportImport.unsavedExport.discard') }}</AppButton>
        <AppButton size="sm" variant="primary" :disabled="exporting || saving" @click="saveThenExport">{{ t('pages.agentStudio.exportImport.unsavedExport.saveThenExport') }}</AppButton>
      </template>
    </AppModal>

    <!-- import discard confirm -->
    <AppModal :open="showImportDiscardConfirm" :title="t('pages.agentStudio.exportImport.discardImport.title')" :width="420" @close="onImportDiscardCancel">
      <p class="text-[13px] leading-6 text-txt2">{{ t('pages.agentStudio.exportImport.discardImport.message', { name: activeName }) }}</p>
      <template #footer>
        <AppButton size="sm" variant="ghost" @click="onImportDiscardCancel">{{ t('common.buttons.cancel') }}</AppButton>
        <AppButton size="sm" variant="danger" @click="onImportDiscardConfirm">{{ t('pages.agentStudio.exportImport.discardImport.confirm') }}</AppButton>
      </template>
    </AppModal>

    <!-- import name conflict -->
    <AppModal :open="showImportConflict" :title="t('pages.agentStudio.exportImport.conflict.title')" :width="460" @close="closeImportConflict">
      <p class="text-[13px] leading-6 text-txt2">{{ t('pages.agentStudio.exportImport.conflict.intro', { name: importConflictName }) }}</p>
      <div class="mt-3 flex flex-col gap-2">
        <button
          type="button"
          class="rounded-lg w-full border px-3 py-2.5 text-left transition"
          :class="importConflictAction === 'overwrite' ? 'border-accent/50 bg-accent-dim' : 'border-line bg-base hover:bg-elevated'"
          @click="selectImportConflict('overwrite')"
        >
          <div class="text-[13px] font-medium text-txt">{{ t('pages.agentStudio.exportImport.conflict.overwrite') }}</div>
          <div class="text-[11.5px] text-txt3">{{ t('pages.agentStudio.exportImport.conflict.overwriteDesc') }}</div>
        </button>
        <button
          type="button"
          class="rounded-lg w-full border px-3 py-2.5 text-left transition"
          :class="importConflictAction === 'rename' ? 'border-accent/50 bg-accent-dim' : 'border-line bg-base hover:bg-elevated'"
          @click="selectImportConflict('rename')"
        >
          <div class="text-[13px] font-medium text-txt">{{ t('pages.agentStudio.exportImport.conflict.rename') }}</div>
          <div class="text-[11.5px] text-txt3">{{ t('pages.agentStudio.exportImport.conflict.renameDesc') }}</div>
        </button>
        <button
          type="button"
          class="rounded-lg w-full border px-3 py-2.5 text-left transition"
          :class="importConflictAction === 'cancel' ? 'border-accent/50 bg-accent-dim' : 'border-line bg-base hover:bg-elevated'"
          @click="selectImportConflict('cancel')"
        >
          <div class="text-[13px] font-medium text-txt">{{ t('pages.agentStudio.exportImport.conflict.cancel') }}</div>
          <div class="text-[11.5px] text-txt3">{{ t('pages.agentStudio.exportImport.conflict.cancelDesc') }}</div>
        </button>
      </div>
      <div v-if="importConflictAction === 'rename'" class="mt-3.5">
        <label class="mb-1.5 block text-[12px] text-txt2">{{ t('pages.agentStudio.exportImport.conflict.newName') }}</label>
        <input
          v-model="importRenameValue"
          class="w-full rounded-md border border-line bg-base px-3 py-2 font-mono text-[13px] text-txt outline-none focus:border-accent"
          @keyup.enter="confirmImportConflict"
        />
        <p v-if="importRenameError" class="mt-2 text-[12px] text-err">{{ importRenameError }}</p>
      </div>
      <template #footer>
        <AppButton size="sm" variant="ghost" @click="closeImportConflict">{{ t('common.buttons.cancel') }}</AppButton>
        <AppButton size="sm" variant="primary" @click="confirmImportConflict">{{ t('pages.agentStudio.exportImport.conflict.confirm') }}</AppButton>
      </template>
    </AppModal>

    <!-- import error -->
    <AppModal :open="showImportErrorModal" :title="t('pages.agentStudio.exportImport.importError.title')" :width="420" @close="showImportErrorModal = false">
      <p class="text-[13px] leading-6 text-txt2">{{ importErrorMessage || t('pages.agentStudio.exportImport.importError.invalidZip') }}</p>
      <template #footer>
        <AppButton size="sm" variant="primary" @click="showImportErrorModal = false">{{ t('pages.agentStudio.exportImport.importError.ok') }}</AppButton>
      </template>
    </AppModal>

    <input ref="importFileInput" type="file" accept=".zip" class="hidden" @change="onImportFileChange" />

    <Teleport to="body">
      <div
        v-if="isMobile && showFullNameTip"
        data-test="agent-name-tip-backdrop"
        class="fixed inset-0 z-[9998]"
        @click="closeFullNameTip"
      />
      <div
        v-if="isMobile && showFullNameTip"
        data-test="agent-name-tip"
        class="rounded-lg fixed z-[9999] border border-line bg-elevated px-3 py-2.5 text-[12.5px] text-txt shadow-card"
        :style="fullNameTipStyle"
        @click.stop
      >
        <small class="mb-1 block text-[11px] text-txt3">{{ t('pages.agentStudio.mobile.fullNameLabel') }}</small>
        <b class="break-all font-semibold">{{ activeName }}</b>
      </div>
    </Teleport>

    <AgentCreateWizard
      :open="showCreateWizard"
      :existing-names="agents.map((a) => a.name)"
      :projects="treeProjects"
      :project-id="createAgentProjectId"
      @close="showCreateWizard = false"
      @created="onWizardCreated"
    />

    <CreateAgentTeamWizard
      v-if="!embedded"
      :open="showTeamWizard"
      :existing-names="agents.map((a) => a.name)"
      @close="showTeamWizard = false"
      @started="onTeamBootstrapStarted"
    />

    <AppModal
      :open="showSaveReasonModal"
      :title="t('pages.agentStudio.workspaceHistory.saveTitle')"
      :width="420"
      @close="showSaveReasonModal = false"
    >
      <p class="text-[13px] leading-6 text-txt2">{{ t('pages.agentStudio.workspaceHistory.saveHint') }}</p>
      <label class="mb-1 mt-2 block text-[12px] text-txt2">{{ t('pages.agentStudio.workspaceHistory.saveReasonLabel') }}</label>
      <input
        v-model="saveReason"
        class="w-full rounded border border-line bg-surface px-2 py-1.5 text-[13px] text-txt outline-none focus:border-accent"
      />
      <template #footer>
        <AppButton size="sm" variant="ghost" @click="showSaveReasonModal = false">{{ t('common.buttons.cancel') }}</AppButton>
        <AppButton size="sm" variant="primary" :disabled="saving" @click="confirmSaveWithReason">
          {{ saving ? t('common.buttons.saving') : t('common.buttons.save') }}
        </AppButton>
      </template>
    </AppModal>


    <Teleport to="body">
      <div
        v-if="toastMsg"
        data-test="studio-toast"
        class="rounded-lg fixed bottom-5 right-5 z-[10000] border border-line bg-elevated px-3.5 py-2 text-[12px] text-txt2 shadow-card"
      >
        {{ toastMsg }}
      </div>
    </Teleport>
  </div>
</template>
