<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import Icon from '@/components/ui/Icon.vue'
import AppButton from '@/components/ui/AppButton.vue'
import AppModal from '@/components/ui/AppModal.vue'
import AgentSidebar from '@/components/agent/AgentSidebar.vue'
import ResourceList from '@/components/ui/ResourceList.vue'
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
import WorkspaceSharedAgentPanel from '@/components/workspace/WorkspaceSharedAgentPanel.vue'
import { api, type CreateAgentTestPayload, type SandboxView } from '@/lib/api/api'
import { useAgentStudio } from '@/lib/agent/useAgentStudio'

const props = defineProps<{ embedded?: boolean }>()
const route = useRoute()
const router = useRouter()
/** Agent 默认配置 is shown exactly when the URL says ?view=defaults (deep links, back/forward, settings nav). */
const showDefaults = computed(() => route.query.view === 'defaults')

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
  showSidebarSheet,
  openSidebarSheet,
  closeSidebarSheet,
  leaveConfirmCfg,
  agents,
  treeNodes,
  activeTreeKey,
  onTreeSelect,
  onSheetTreeSelect,
  select,
  syncStudioQuery,
  agentListCollapsed,
  toggleAgentListCollapsed,
  activeName,
  draft,
  tab,
  dataSubTab,
  draftBindingDirty,
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
  onExportWorkspace,
  cancelBundleSecrets,
  confirmBundleSecrets,
  showCreateWizard,
  showTeamWizard,
  teamBootstrapSessionId,
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
  openRenameAgent,
  confirmDeleteAgent,
  promptOk,
  confirmOk,
  onAgentNameClick,
  syncTabFade,
} = useAgentStudio({ embedded: () => !!props.embedded, pauseQuerySync: showDefaults })

/** The env tab's "Edit" link for a managed model key lands on the Runtime card. */
const focusModelTick = ref(0)
async function focusCliModel() {
  requestStudioTab('meta')
  focusModelTick.value++
  // Reset once the panel has seen it, so a later visit to the tab does not pulse again.
  await nextTick()
  focusModelTick.value = 0
}

/* ── Agent 默认配置 lives in the same list as the Agents (pinned first item), so there is no
 *    second page to switch to. The old /settings/shared-agent link redirects here. ── */
const DEFAULTS_KEY = '__workspace_defaults__'
const pinnedNodes = computed(() => [{ id: DEFAULTS_KEY, label: t('nav.sharedAgent'), icon: 'skills' }])
const sharedPanelRef = ref<{ dirty: boolean; save: () => Promise<boolean> } | null>(null)
// Leaving the defaults some other way (settings nav, back): put the Agent back in the URL.
watch(showDefaults, (now, before) => { if (before && !now) syncStudioQuery() })
function openDefaults() {
  if (!showDefaults.value) void router.replace({ query: { view: 'defaults' } })
}
function agentQuery(): Record<string, string> {
  const q: Record<string, string> = {}
  if (activeName.value) q.agent = activeName.value
  if (tab.value !== 'files') q.studioTab = tab.value
  if (tab.value === 'data') q.sub = dataSubTab.value
  return q
}
/** Defaults → Agent. The URL only changes once every confirm has passed. */
function leaveDefaultsTo(key: string) {
  const apply = () => {
    // Coming back to the same Agent keeps the tab it was on; another Agent starts on Files.
    if (key !== activeName.value) select(key)
    void router.replace({ query: agentQuery() })
  }
  // The Agent itself may be unsaved too: same prompts as switching Agents (sheet on narrow screens).
  const switchAgent = () => {
    if (key === activeName.value || !dirty.value) return apply()
    if (isMobile.value) {
      leaveConfirmCfg.value = {
        title: t('pages.agentStudio.dialogs.leaveUnsavedTitle'),
        message: t('pages.agentStudio.dialogs.leaveUnsavedSwitchMessage', { name: activeName.value }),
        saveText: t('pages.agentStudio.dialogs.saveAndSwitch'),
        discardText: t('pages.agentStudio.dialogs.discardAndSwitch'),
        onSave: async () => {
          if (!(await save())) return false
          justSaved.value = true
          apply()
          return true
        },
        onDiscard: apply,
      }
      return
    }
    confirmCfg.value = {
      title: t('pages.agentStudio.dialogs.discardTitle'),
      message: t('pages.agentStudio.dialogs.discardMessage', { name: activeName.value }),
      confirmText: t('pages.agentStudio.dialogs.discardConfirm'),
      danger: true,
      ok: apply,
    }
  }
  if (!sharedPanelRef.value?.dirty) return switchAgent()
  const name = t('nav.sharedAgent')
  if (isMobile.value) {
    leaveConfirmCfg.value = {
      title: t('pages.agentStudio.dialogs.leaveUnsavedTitle'),
      message: t('pages.agentStudio.dialogs.leaveUnsavedSwitchMessage', { name }),
      saveText: t('pages.agentStudio.dialogs.saveAndSwitch'),
      discardText: t('pages.agentStudio.dialogs.discardAndSwitch'),
      onSave: async () => {
        if (!(await sharedPanelRef.value?.save())) return false
        switchAgent()
        return true
      },
      onDiscard: switchAgent,
    }
    return
  }
  confirmCfg.value = {
    title: t('pages.agentStudio.dialogs.discardTitle'),
    message: t('pages.agentStudio.dialogs.discardMessage', { name }),
    confirmText: t('pages.agentStudio.dialogs.discardConfirm'),
    danger: true,
    ok: switchAgent,
  }
}
function onSidebarSelect(key: string) {
  if (key === DEFAULTS_KEY) return openDefaults()
  if (showDefaults.value) return leaveDefaultsTo(key)
  onTreeSelect(key)
}
// Narrow screens pick from the bottom sheet; the defaults item works the same way there.
function onSheetSelect(key: string) {
  if (key === DEFAULTS_KEY) {
    openDefaults()
    closeSidebarSheet()
    return
  }
  if (showDefaults.value) {
    closeSidebarSheet()
    leaveDefaultsTo(key)
    return
  }
  onSheetTreeSelect(key)
}

async function createStudioChatTest(
  profile: string,
  payload: CreateAgentTestPayload,
): Promise<SandboxView> {
  return api.createSharedAgentTest({
    agentName: profile,
    ...(payload.repos ? { repos: payload.repos } : {}),
    ...(payload.repoUrl ? { repoUrl: payload.repoUrl } : {}),
  })
}
</script>
<template>
  <div
    class="agent-studio-document flex h-full min-h-0 flex-col overflow-hidden"
    data-testid="agent-studio-panel"
    :aria-busy="loading ? 'true' : 'false'"
  >
    <div
      v-if="error && !loadFailed && !loadDenied && agents.length"
      class="mb-3 shrink-0 border-l-2 border-err/40 px-3 py-2 text-[13px] text-err"
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
        class="agent-studio-skeleton grid min-h-0 flex-1 overflow-hidden"
        :data-mobile="isMobile"
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
        class="flex flex-1 flex-col items-center justify-center px-6 text-center"
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
        class="flex flex-1 flex-col items-center justify-center px-6 text-center"
      >
        <h3 class="text-sm font-semibold text-txt">{{ t('common.asyncState.loadFailedTitle') }}</h3>
        <p class="mt-1 max-w-md text-xs text-txt2">{{ t('common.asyncState.loadFailedDesc') }}</p>
        <AppButton class="mt-4" variant="outline" data-testid="agent-studio-retry" @click="load">
          {{ t('common.buttons.retry') }}
        </AppButton>
      </div>

      <div
        v-else-if="!agents.length && !teamBootstrapSessionId && !showDefaults"
        class="flex flex-1 flex-col items-center justify-center gap-3 px-6 text-center"
        data-testid="agent-studio-empty-team"
      >
        <template v-if="embedded">
          <h2 class="m-0 text-[18px] font-semibold text-txt">{{ t('pages.agentStudio.emptyWorkspaceTitle') }}</h2>
          <p class="m-0 max-w-md text-[13px] leading-6 text-txt3">{{ t('pages.agentStudio.emptyWorkspaceDesc') }}</p>
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
          <AppButton variant="primary" icon="users" data-testid="agent-studio-empty-create-team" @click="openCreateTeam">
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
        <!-- Agent 默认配置 stays reachable with no Agents yet (same look as the pinned list item). -->
        <button
          type="button"
          data-testid="agent-studio-empty-defaults"
          class="mt-2 flex min-h-9 items-center gap-2 rounded px-2 text-left text-[12px] text-txt2 hover:bg-overlay"
          @click="openDefaults"
        >
          <Icon name="skills" :size="14" class="shrink-0" /><span>{{ t('nav.sharedAgent') }}</span>
        </button>
      </div>

      <div
        v-else
        class="agent-studio-layout grid min-h-0 flex-1 overflow-hidden bg-surface transition-[grid-template-columns] duration-[var(--dur-ui)] ease-[var(--ease-out-expo)]"
        :class="showRefreshProgress ? 'opacity-[0.55]' : ''"
        :style="cardGridStyle"
        :data-mobile="isMobile"
        :data-directory-collapsed="agentListCollapsed"
      >
      <!-- Agent tree (hidden on narrow screens; agent name bar remains) -->
      <AgentSidebar
        class="agent-studio-directory"
        v-if="!isMobile"
        :nodes="treeNodes"
        :pinned="pinnedNodes"
        :active-key="showDefaults ? DEFAULTS_KEY : activeTreeKey"
        :collapsed="agentListCollapsed"
        :hide-create-team="hideTeamCreate"
        @select="onSidebarSelect"
        @rename="openRenameAgent"
        @delete="confirmDeleteAgent"
        @import="triggerImport"
        @create-agent="openCreateAgent"
        @create-team="openCreateTeam"
        @export-workspace="onExportWorkspace"
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

      <!-- workspace defaults (was /settings/shared-agent) -->
      <div v-else-if="showDefaults" class="flex min-h-0 min-w-0 flex-col overflow-hidden" data-testid="studio-defaults-pane">
        <div data-test="studio-name-bar" class="toolbar-inline-row flex items-center gap-2 border-b border-line px-4">
          <Icon name="skills" :size="15" class="shrink-0 text-accent-2" />
          <span class="min-w-0 flex-1 truncate text-[13px] font-medium text-txt">{{ t('nav.sharedAgent') }}</span>
          <button
            v-if="isMobile"
            type="button"
            data-test="sidebar-switch"
            class="inline-flex min-h-11 shrink-0 items-center gap-1 rounded border border-line bg-elevated px-2.5 text-[12px] text-txt2 transition hover:border-line-strong hover:text-txt"
            :title="t('pages.agentStudio.mobile.switchTitle')"
            :aria-label="t('pages.agentStudio.mobile.switchAria')"
            @click="openSidebarSheet"
          >
            <Icon name="menu" :size="14" />
            <span>{{ t('pages.agentStudio.mobile.switch') }}</span>
          </button>
        </div>
        <WorkspaceSharedAgentPanel ref="sharedPanelRef" />
      </div>

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
              data-test="sidebar-switch"
              class="inline-flex min-h-11 shrink-0 items-center gap-1 rounded border border-line bg-elevated px-2.5 text-[12px] text-txt2 transition hover:border-line-strong hover:text-txt"
              :title="t('pages.agentStudio.mobile.switchTitle')"
              :aria-label="t('pages.agentStudio.mobile.switchAria')"
              @click="openSidebarSheet"
            >
              <Icon name="menu" :size="14" />
              <span>{{ t('pages.agentStudio.mobile.switch') }}</span>
            </button>
            <span class="studio-status-slot">
              <Transition name="studio-feedback">
                <span v-if="dirty" key="dirty" class="chip border-warn/30 text-warn">{{ t('pages.agentStudio.unsaved') }}</span>
                <span
                  v-else-if="justSaved"
                  key="saved"
                  class="chip border-ok/30 bg-ok/10 text-ok"
                >{{ t('pages.agentStudio.saved') }}</span>
              </Transition>
            </span>
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
            <Transition name="studio-feedback">
              <AppButton
                v-if="dirty"
                data-test="studio-save"
                variant="primary"
                class="studio-save-button min-h-11"
                :disabled="saving"
                @click="() => save()"
              >
                <span class="studio-save-label">
                  <Transition name="studio-feedback">
                    <span :key="saving ? 'saving' : 'save'">{{ saving ? t('common.buttons.saving') : t('common.buttons.save') }}</span>
                  </Transition>
                </span>
              </AppButton>
            </Transition>
          </div>
        </div>
        <div
          v-else
          data-test="studio-name-bar"
          class="toolbar-inline-row flex items-center gap-2 border-b border-line px-4"
        >
          <Icon name="robot" :size="15" class="shrink-0 text-accent-2" />
          <span class="min-w-0 truncate text-[13px] font-medium text-txt" :title="activeName">{{ activeName }}</span>
          <span class="studio-status-slot">
            <Transition name="studio-feedback">
              <span v-if="dirty" key="dirty" class="chip border-warn/30 text-warn">{{ t('pages.agentStudio.unsaved') }}</span>
              <span
                v-else-if="justSaved"
                key="saved"
                class="chip border-ok/30 bg-ok/10 text-ok"
              >{{ t('pages.agentStudio.saved') }}</span>
            </Transition>
          </span>
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
              class="studio-save-button"
              :disabled="!dirty || saving"
              @click="() => save()"
            >
              <span class="studio-save-label">
                <Transition name="studio-feedback">
                  <span :key="saving ? 'saving' : dirty ? 'save' : 'saved'">{{ saving ? t('common.buttons.saving') : dirty ? t('common.buttons.save') : t('pages.agentStudio.saved') }}</span>
                </Transition>
              </span>
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
            @edit-model="focusCliModel"
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
            :sub-tab="dataSubTab"
            @update:sub-tab="onDataSubTab"
          />
          </div>

          <AgentMetaPanel
            v-else-if="tab === 'meta' && draft && !isMobile"
            key="meta"
            :draft="draft"
            :agent-name="activeName"
            :focus-model-tick="focusModelTick"
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

    <!-- narrow-screen Agent tree sheet -->
    <Teleport to="body">
      <div
        v-if="showSidebarSheet"
        data-test="sidebar-sheet"
        class="fixed inset-0 z-40"
        role="dialog"
        aria-modal="true"
        :aria-label="t('pages.agentStudio.mobile.sheetTitle')"
      >
        <div
          data-test="sidebar-sheet-backdrop"
          class="absolute inset-0 bg-black/50"
          @click="closeSidebarSheet"
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
              data-test="sidebar-sheet-import"
              class="flex min-h-9 min-w-9 shrink-0 items-center justify-center rounded text-txt3 hover:bg-overlay hover:text-txt"
              :title="t('pages.agentStudio.exportImport.import')"
              :aria-label="t('pages.agentStudio.exportImport.import')"
              @click="triggerImport"
            >
              <Icon name="input" :size="14" />
            </button>
            <button
              type="button"
              data-test="sidebar-sheet-create-agent"
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
              data-test="sidebar-sheet-create-team"
              class="flex min-h-9 min-w-9 shrink-0 items-center justify-center rounded text-txt3 hover:bg-overlay hover:text-txt"
              :title="t('pages.agentStudio.tree.newTeam')"
              :aria-label="t('pages.agentStudio.tree.newTeam')"
              @click="openCreateTeam"
            >
              <Icon name="users" :size="14" />
            </button>
            <button
              type="button"
              data-test="sidebar-sheet-close"
              class="flex min-h-9 min-w-9 shrink-0 items-center justify-center rounded text-txt3 hover:bg-overlay hover:text-txt"
              :aria-label="t('common.buttons.close')"
              @click="closeSidebarSheet"
            >
              <Icon name="close" :size="14" />
            </button>
          </div>
          <div class="min-h-0 flex-1 overflow-hidden">
            <ResourceList
              :nodes="treeNodes"
              :pinned="pinnedNodes"
              :active-key="showDefaults ? DEFAULTS_KEY : activeTreeKey"
              :title="t('pages.agentStudio.tree.title')"
              :total="agents.length"
              :search-placeholder="t('pages.agentStudio.tree.searchPlaceholder')"
              :empty-text="t('pages.agentStudio.tree.empty')"
              storage-key="agent-studio-sheet-tree"
              @select="onSheetSelect"
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

    <!-- workspace bundle export secrets warning -->
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

    <!-- workspace bundle name conflict -->
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
      <Transition name="studio-toast">
        <div
          v-if="toastMsg"
          data-test="studio-toast"
          class="rounded-lg fixed bottom-5 right-5 z-[10000] border border-line bg-elevated px-3.5 py-2 text-[12px] text-txt2 shadow-card"
        >
          {{ toastMsg }}
        </div>
      </Transition>
    </Teleport>
  </div>
</template>

<style scoped>
.agent-studio-document {
  container: agent-studio / inline-size;
}

.studio-status-slot,
.studio-save-label {
  display: inline-grid;
  flex-shrink: 0;
  align-items: center;
}

.studio-status-slot {
  min-inline-size: 4.75em;
}

.studio-status-slot > .chip,
.studio-save-label > span {
  grid-area: 1 / 1;
}

.studio-status-slot > .chip {
  justify-self: start;
}

.studio-save-button {
  min-inline-size: 6.5em;
  transition: opacity 200ms ease, background-color 200ms ease, color 200ms ease, box-shadow 200ms ease;
}

.studio-feedback-enter-active,
.studio-feedback-leave-active,
.studio-toast-enter-active,
.studio-toast-leave-active {
  transition: opacity 200ms ease, transform 200ms ease;
}

.studio-feedback-enter-from,
.studio-feedback-leave-to {
  opacity: 0;
  transform: translateY(3px);
}

.studio-toast-enter-from,
.studio-toast-leave-to {
  opacity: 0;
  transform: translateY(6px);
}

@media (prefers-reduced-motion: reduce) {
  .studio-save-button,
  .studio-feedback-enter-active,
  .studio-feedback-leave-active,
  .studio-toast-enter-active,
  .studio-toast-leave-active {
    transition: none;
  }

  .studio-feedback-enter-from,
  .studio-feedback-leave-to,
  .studio-toast-enter-from,
  .studio-toast-leave-to {
    transform: none;
  }
}

/* Account for the Settings directory as well as the browser width. */
@container agent-studio (max-width: 780px) {
  .agent-studio-layout[data-mobile="false"],
  .agent-studio-skeleton[data-mobile="false"] {
    grid-template-columns: minmax(0, 1fr) !important;
    grid-template-rows: 160px minmax(0, 1fr);
  }

  .agent-studio-layout[data-mobile="false"][data-directory-collapsed="true"] {
    grid-template-rows: 28px minmax(0, 1fr);
  }

  .agent-studio-directory {
    border-right: 0;
    border-bottom: 1px solid rgb(var(--c-line));
  }
}
</style>
