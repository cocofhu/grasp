<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import Icon from '@/components/ui/Icon.vue'
import AppButton from '@/components/ui/AppButton.vue'
import AppModal from '@/components/ui/AppModal.vue'
import AppSwitch from '@/components/ui/AppSwitch.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import StatusPill from '@/components/ui/StatusPill.vue'
import RunLaunchModal, { type InputField } from '@/components/workflow/RunLaunchModal.vue'
import ReposEditor from '@/components/ui/ReposEditor.vue'
import NewWorkflowMenu from '@/components/workflow/NewWorkflowMenu.vue'
import CopyWorkflowModal from '@/components/workflow/CopyWorkflowModal.vue'
import ExportVersionModal from '@/components/workflow/ExportVersionModal.vue'
import BoardView from '@/views/BoardView.vue'
import PmLeaderChat from '@/components/pm/PmLeaderChat.vue'
import PmCronJobsPanel from '@/components/pm/PmCronJobsPanel.vue'
import PmSettingsPanel from '@/components/pm/PmSettingsPanel.vue'
import TokenUsageHoverTip from '@/components/ui/TokenUsageHoverTip.vue'
import ProjectAuditPanel from '@/components/project/ProjectAuditPanel.vue'
import ProjectNotifyPanel from '@/components/project/ProjectNotifyPanel.vue'
import ProjectSharedAgentPanel from '@/components/project/ProjectSharedAgentPanel.vue'
import ProjectAgentsPanel from '@/components/project/ProjectAgentsPanel.vue'
import RequirementDraftsPanel from '@/components/project/RequirementDraftsPanel.vue'
import ProjectExternalMcpPanel from '@/components/project/ProjectExternalMcpPanel.vue'
import { useProjectDetail } from '@/lib/project/useProjectDetail'
import { DEFAULT_PROJECT_ID } from '@/lib/pm/onboardingWizard'

const {
  PROJECT_TABS,
  LEGACY_PM_SETTINGS_TAB,
  LEGACY_PM_MEMORY_TAB,
  isProjectTab,
  parseProjectTab,
  route,
  router,
  t,
  locale,
  toast,
  isFavorite,
  toggleFavorite,
  favoriteBtnMinWidth,
  toggleWorkflowFavorite,
  isMobile,
  projectId,
  project,
  workflows,
  loading,
  hasInitialLoaded,
  loadFailed,
  loadDenied,
  notFound,
  wfRefreshing,
  projectSeq,
  workflowSeq,
  initialLoading,
  showRefreshProgress,
  initialLegacyPmSettings,
  initialLegacyPmMemory,
  tab,
  draftsPanelRef,
  confirmDraftsLeave,
  pmView,
  showPmMemoryMigration,
  setTab,
  pmRestoreMobileChat,
  openPmSettings,
  backToPmChat,
  resetPmViewForProjectContext,
  rewriteLegacyPmSettingsQuery,
  rewriteLegacyPmMemoryQuery,
  syncTabFromRoute,
  ensureTabQuery,
  dismissPmMemoryMigration,
  goStudioMemory,
  savingMeta,
  savingVars,
  editName,
  editDesc,
  editUnknownModelDisplayName,
  unknownModelDisplayNameError,
  varRows,
  showDelete,
  deleting,
  deleteError,
  runTarget,
  runFields,
  runInputs,
  runImages,
  draftRestored,
  openMenuId,
  newWorkflowMenuOpen,
  baselineModalOpen,
  baselineName,
  baselineRepos,
  creatingBaseline,
  baselineCreateError,
  hasValidBaselineRepo,
  deleteWfTarget,
  deletingWf,
  deleteWfError,
  copyPreviewLoading,
  copyModal,
  exportTarget,
  projectAgents,
  isOnboardingEmpty,
  fileInput,
  triggerImport,
  handleFileChange,
  tabs,
  auditForceDenied,
  pmBinding,
  loadPmBinding,
  onPmBindingChanged,
  onNotifyProjectUpdated,
  openNotifyChannelSettings,
  savingNotifyWfId,
  wfNotifyMode,
  wfNotifyHas,
  persistWorkflowNotify,
  setWorkflowNotifyMode,
  toggleWorkflowNotifyEvent,
  savingHomeWfId,
  toggleWorkflowShowOnHome,
  VAR_TYPES,
  existingNames,
  askFields,
  fieldOptions,
  closeMenu,
  toggleMenu,
  toggleNewWorkflowMenu,
  menuIdFor,
  onDocClick,
  onKeydown,
  onScrollClose,
  load,
  openOnboarding,
  reloadWorkflows,
  saveMeta,
  clearUnknownModelDisplayName,
  saveVars,
  SECRET_MASK,
  addVarRow,
  removeVarRow,
  onVarSecretChange,
  onVarNameChange,
  onVarTypeChange,
  selectOptions,
  isBoolTrue,
  setBoolValue,
  onVarValueInput,
  newWorkflow,
  openBaselineModal,
  closeBaselineModal,
  createFromBaseline,
  openWorkflow,
  openEdit,
  openRun,
  saveRunDraftClick,
  closeRunModal,
  onRunStayed,
  onRunStarted,
  onViewRun,
  openCopy,
  closeCopyModal,
  onCopied,
  openExport,
  openDeleteWf,
  confirmDeleteWf,
  confirmDelete,
  fmtTime,
  fmtCompactTokenCount
} = useProjectDetail()

const projectTabTrack = ref<HTMLElement | null>(null)
const projectTabIndicator = ref<Record<string, string>>({
  opacity: '0',
  transform: 'translateX(0)',
  width: '0px',
})

function updateProjectTabIndicator() {
  const root = projectTabTrack.value
  if (!root) return
  const active = root.querySelector<HTMLElement>('[data-project-tab-active="true"]')
  if (!active) {
    projectTabIndicator.value = { opacity: '0', transform: 'translateX(0)', width: '0px' }
    return
  }
  projectTabIndicator.value = {
    opacity: '1',
    transform: `translateX(${active.offsetLeft}px)`,
    width: `${active.offsetWidth}px`,
  }
}

watch(tab, () => {
  void nextTick(updateProjectTabIndicator)
})
onMounted(() => {
  void nextTick(updateProjectTabIndicator)
  window.addEventListener('resize', updateProjectTabIndicator)
})
onBeforeUnmount(() => {
  window.removeEventListener('resize', updateProjectTabIndicator)
})

const onboardingEmptyDesc = computed(() =>
  projectId.value === DEFAULT_PROJECT_ID
    ? t('pages.onboarding.emptyDesc')
    : t('pages.onboarding.emptyDescCreate'),
)

</script>

<template>
  <div
    class="flex h-full min-h-0 flex-1 flex-col overflow-hidden"
    data-testid="project-detail-panel"
    :aria-busy="loading || wfRefreshing ? 'true' : 'false'"
  >
    <div
      v-if="showRefreshProgress"
      class="mb-2 h-[2px] overflow-hidden bg-line"
      data-testid="project-detail-thin-progress"
      aria-hidden="true"
    >
      <i class="admin-list-thin-bar bg-accent" />
    </div>
    <div
      class="flex min-h-0 flex-1 flex-col"
      :class="showRefreshProgress ? 'opacity-[0.55]' : ''"
    >
    <div class="mb-1.5 shrink-0">
      <div
        class="mb-1.5 flex items-start justify-between gap-3"
        data-testid="project-detail-header-row"
      >
        <button
          type="button"
          class="inline-flex min-w-0 items-center gap-1 truncate text-xs text-txt3 hover:text-txt2"
          @click="router.push('/projects')"
        >
          <Icon name="chevron-right" :size="12" class="shrink-0 rotate-180" />
          <span class="truncate">{{ t('pages.projectDetail.back') }}</span>
        </button>
        <div
          v-if="!initialLoading && project"
          class="group relative min-w-[132px] shrink-0 rounded-[10px] border border-accent/35 bg-gradient-to-b from-accent-dim/90 to-surface px-3 py-2 text-left transition-[box-shadow,border-color] md:text-right"
          :class="project.totalTokens != null ? 'cursor-help hover:border-accent hover:shadow-[0_0_0_3px_rgba(123,97,255,0.14)]' : ''"
          data-testid="project-token-stat"
          :aria-label="
            project.totalTokens != null
              ? t('pages.projectDetail.tokenTipAria')
              : t('pages.projectDetail.tokenUsage')
          "
          :aria-describedby="project.totalTokens != null ? 'project-token-detail-tip' : undefined"
          :tabindex="project.totalTokens != null ? 0 : undefined"
        >
          <div class="text-[11px] font-semibold tracking-wide text-accent-2">
            {{ t('pages.projectDetail.tokenUsage') }}
          </div>
          <div
            class="mt-0.5 text-[22px] font-bold leading-tight tracking-tight tabular-nums"
            :class="project.totalTokens == null ? 'text-txt3' : 'text-txt'"
            data-testid="project-token-stat-value"
          >
            {{ fmtCompactTokenCount(project.totalTokens) }}
          </div>
          <TokenUsageHoverTip
            v-if="project.totalTokens != null"
            tip-id="project-token-detail-tip"
            :total-tokens="project.totalTokens"
            :workflow-tokens="project.workflowTokens"
            :pm-tokens="project.pmTokens"
          />
        </div>
      </div>
      <div
        v-if="initialLoading"
        data-testid="project-detail-title-skeleton"
        aria-hidden="true"
      >
        <div class="h-7 w-48 bg-elevated animate-pulse" />
        <div class="mt-2 h-3 w-72 bg-elevated animate-pulse" />
      </div>
      <template v-else-if="project">
        <div class="min-w-0" data-testid="project-detail-title-row">
          <h2 class="truncate text-lg font-semibold text-txt">{{ project.name }}</h2>
          <p v-if="project.description" class="mt-0.5 truncate text-sm text-txt3">
            {{ project.description }}
          </p>
        </div>
      </template>
      <div
        v-else-if="loadDenied"
        role="status"
        data-testid="project-detail-denied"
        class="rounded-lg border border-warn/40 bg-warn/10 px-5 py-10 text-center"
      >
        <Icon name="lock" :size="22" class="mx-auto mb-3 text-warn" />
        <h3 class="text-sm font-semibold text-txt">{{ t('common.asyncState.permissionDeniedTitle') }}</h3>
        <p class="mt-1 text-xs text-txt2">{{ t('common.asyncState.permissionDeniedDesc') }}</p>
        <AppButton class="mt-4" variant="outline" data-testid="project-detail-retry" @click="load">
          {{ t('common.buttons.retry') }}
        </AppButton>
      </div>
      <div
        v-else-if="loadFailed"
        role="status"
        data-testid="project-detail-failed"
        class="rounded-lg border border-err/40 bg-err/10 px-5 py-10 text-center"
      >
        <h3 class="text-sm font-semibold text-txt">{{ t('common.asyncState.loadFailedTitle') }}</h3>
        <p class="mt-1 text-xs text-txt2">{{ t('common.asyncState.loadFailedDesc') }}</p>
        <AppButton class="mt-4" variant="outline" data-testid="project-detail-retry" @click="load">
          {{ t('common.buttons.retry') }}
        </AppButton>
      </div>
      <div v-else-if="notFound" class="text-sm text-err">{{ t('pages.projectDetail.notFound') }}</div>
    </div>

    <template v-if="initialLoading || project">
      <div
        ref="projectTabTrack"
        class="scroll-area relative flex shrink-0 flex-nowrap gap-1 overflow-x-auto overflow-y-hidden border-b border-line [-webkit-overflow-scrolling:touch]"
        data-testid="project-detail-tabs"
      >
        <button
          v-for="tb in tabs"
          :key="tb.id"
          type="button"
          class="relative z-[1] shrink-0 whitespace-nowrap border-b-2 border-transparent px-3 py-1.5 text-sm transition"
          :class="tab === tb.id ? 'text-accent-2' : 'text-txt3 hover:text-txt2'"
          :data-testid="`project-tab-${tb.id}`"
          :data-project-tab-active="tab === tb.id ? 'true' : undefined"
          @click="setTab(tb.id)"
        >
          {{ t(tb.labelKey) }}
        </button>
        <span
          class="app-tabs-indicator pointer-events-none absolute bottom-0 left-0 h-0.5 rounded-full bg-accent"
          data-testid="project-tabs-indicator"
          :style="projectTabIndicator"
          aria-hidden="true"
        />
      </div>

      <Transition name="ui-fade" mode="out-in">
      <div
        :key="String(tab)"
        class="toolbar-below-tabs flex min-h-0 flex-1 flex-col overflow-hidden"
        data-testid="project-detail-tab-panel"
      >
      <div
        v-if="showPmMemoryMigration"
        data-testid="pm-memory-migration-banner"
        class="rounded-lg mb-3 flex flex-col gap-2 border border-warn/35 bg-warn/10 px-3 py-2.5 sm:flex-row sm:items-center sm:justify-between"
      >
        <div class="min-w-0">
          <div class="text-[12px] font-medium text-txt">{{ t('pages.projectDetail.pm.memoryMigratedTitle') }}</div>
          <p class="mt-0.5 text-[11px] text-txt2">{{ t('pages.projectDetail.pm.memoryMigratedDesc') }}</p>
        </div>
        <div class="flex shrink-0 flex-wrap gap-2">
          <AppButton
            size="sm"
            variant="primary"
            data-testid="pm-memory-go-studio"
            @click="goStudioMemory()"
          >
            {{ t('pages.projectDetail.pm.goStudioMemory') }}
          </AppButton>
          <AppButton size="sm" variant="ghost" data-testid="pm-memory-migration-dismiss" @click="dismissPmMemoryMigration">
            {{ t('common.buttons.close') }}
          </AppButton>
        </div>
      </div>

      <div
        v-if="initialLoading"
        data-testid="project-detail-content-skeleton"
        aria-hidden="true"
        class="min-h-[280px]"
      >
        <div v-if="tab === 'workflows' && isMobile" class="flex flex-col gap-2">
          <div
            v-for="n in 4"
            :key="'wf-skel-m-' + n"
            class="rounded-lg flex flex-col gap-3 border border-line bg-surface p-3"
          >
            <div class="flex items-start justify-between gap-3">
              <div class="min-w-0 flex-1 space-y-2">
                <div class="h-3.5 w-2/3 bg-elevated animate-pulse" />
                <div class="h-2.5 w-1/2 bg-elevated animate-pulse" />
              </div>
              <div class="h-5 w-14 bg-elevated animate-pulse" />
            </div>
            <div class="h-8 w-full bg-elevated animate-pulse" />
          </div>
        </div>
        <div v-else-if="tab === 'workflows'" class="rounded-lg overflow-hidden border border-line">
          <div class="grid grid-cols-5 gap-3 border-b border-line bg-elevated px-3 py-2">
            <div v-for="n in 5" :key="'wf-th-' + n" class="h-2.5 bg-elevated animate-pulse" />
          </div>
          <div v-for="n in 5" :key="'wf-skel-r-' + n" class="grid grid-cols-5 gap-3 border-t border-line px-3 py-3">
            <div class="h-3 bg-elevated animate-pulse" />
            <div class="h-3 w-2/3 bg-elevated animate-pulse" />
            <div class="h-3 w-1/2 bg-elevated animate-pulse" />
            <div class="h-3 w-1/3 bg-elevated animate-pulse" />
            <div class="h-3 w-1/2 bg-elevated animate-pulse" />
          </div>
        </div>
        <div v-else class="rounded-lg space-y-3 border border-line bg-surface p-4">
          <div class="h-4 w-40 bg-elevated animate-pulse" />
          <div class="h-24 w-full bg-elevated animate-pulse" />
          <div class="h-10 w-full bg-elevated animate-pulse" />
        </div>
      </div>

      <div
        v-else-if="tab === 'board'"
        class="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden"
        data-testid="project-board-panel"
      >
        <BoardView :project-id="projectId" embedded class="min-h-0 flex-1" />
      </div>

      <div
        v-else-if="tab === 'requirementDrafts'"
        class="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden"
        data-testid="project-requirement-drafts-panel"
      >
        <RequirementDraftsPanel ref="draftsPanelRef" :project-id="projectId" class="min-h-0 flex-1" />
      </div>

      <div
        v-else-if="tab === 'pmLeader'"
        class="flex min-h-0 flex-1 flex-col"
        data-testid="project-pm-leader-panel"
      >
        <PmLeaderChat
          v-if="pmView === 'chat'"
          :project-id="projectId"
          :binding="pmBinding"
          :restore-mobile-chat="pmRestoreMobileChat"
          :unknown-model-display-name="project?.unknownModelDisplayName"
          @open-settings="openPmSettings"
          @restored-mobile-chat="pmRestoreMobileChat = false"
        />
        <div
          v-else
          class="flex min-h-0 flex-1 flex-col gap-3 overflow-hidden"
          data-testid="project-pm-settings-view"
        >
          <div class="flex shrink-0 items-center border-b border-line px-3 py-2" :class="isMobile ? 'min-h-[44px]' : ''">
            <button
              v-if="isMobile"
              type="button"
              class="flex h-11 w-11 shrink-0 items-center justify-center rounded-md text-txt2 hover:bg-elevated hover:text-txt"
              data-testid="pm-settings-back"
              :aria-label="t('shell.aria.backToList')"
              @click="backToPmChat"
            >
              <Icon name="arrow-left" :size="18" />
            </button>
            <AppButton
              v-else
              variant="ghost"
              size="sm"
              data-testid="pm-settings-back"
              @click="backToPmChat"
            >
              {{ t('pages.projectDetail.pm.backToChat') }}
            </AppButton>
          </div>
          <PmSettingsPanel
            :project-id="projectId"
            :project="project"
            @changed="onPmBindingChanged"
            @project-updated="(p) => { project = p }"
          />
        </div>
      </div>

      <!-- plan g2.1: cronJobs/notify fill + scroll exit (drop 420px hard floor) -->
      <div
        v-else-if="tab === 'externalMcp'"
        class="flex min-h-0 flex-1 flex-col overflow-hidden"
        data-testid="project-external-mcp-view"
      >
        <ProjectExternalMcpPanel :project-id="projectId" />
      </div>

      <div
        v-else-if="tab === 'cronJobs'"
        class="scroll-area flex min-h-0 flex-1 flex-col overflow-y-auto"
      >
        <PmCronJobsPanel :project-id="projectId" />
      </div>

      <div
        v-else-if="tab === 'notify' && project"
        class="scroll-area min-h-0 flex-1 overflow-y-auto"
      >
        <ProjectNotifyPanel
          :project-id="projectId"
          :project="project"
          @updated="onNotifyProjectUpdated"
          @open-channel-settings="openNotifyChannelSettings"
        />
      </div>

      <!-- Workflows tab — plan g2.1: min-h-0 flex-1 + overflow-y-auto -->
      <div v-else-if="tab === 'workflows'" class="flex min-h-0 flex-1 flex-col overflow-hidden">
        <div class="mb-3 flex shrink-0 justify-end gap-2">
          <AppButton
            v-if="isOnboardingEmpty"
            variant="outline"
            icon="sparkles"
            data-testid="onboarding-cta"
            @click="openOnboarding"
          >
            {{ t('pages.onboarding.cta') }}
          </AppButton>
          <AppButton variant="outline" icon="input" @click="triggerImport">
            {{ t('common.buttons.import') }}
          </AppButton>
          <div class="relative" data-new-workflow-menu @click.stop>
            <AppButton
              variant="primary"
              icon="plus"
              data-testid="new-workflow-button"
              :aria-expanded="newWorkflowMenuOpen"
              aria-haspopup="menu"
              @click="toggleNewWorkflowMenu"
            >
              {{ t('common.buttons.newWorkflow') }}
              <Icon name="chevron-down" :size="14" />
            </AppButton>
            <NewWorkflowMenu
              :open="newWorkflowMenuOpen"
              @scratch="newWorkflow"
              @baseline="openBaselineModal"
            />
          </div>
        </div>
        <div class="scroll-area min-h-0 flex-1 overflow-y-auto">
        <div
          v-if="isOnboardingEmpty"
          class="card px-5 py-10 text-center text-[13px] text-txt3"
          data-testid="onboarding-empty"
        >
          <p class="text-[15px] font-medium text-txt">{{ t('pages.onboarding.emptyTitle') }}</p>
          <p class="mt-2 text-txt2" data-testid="onboarding-empty-desc">{{ onboardingEmptyDesc }}</p>
          <div class="mt-4 flex justify-center gap-2">
            <AppButton variant="primary" icon="sparkles" @click="openOnboarding">
              {{ t('pages.onboarding.cta') }}
            </AppButton>
          </div>
        </div>
        <div
          v-else-if="!workflows.length"
          class="card px-5 py-10 text-center text-[13px] text-txt3"
          data-testid="workflows-empty"
        >
          <p class="text-[15px] font-medium text-txt">{{ t('common.empty.noWorkflows') }}</p>
        </div>
        <!-- Mobile card list: avoids table overflow clipping the more menu -->
        <div v-else-if="isMobile" class="flex flex-col gap-2">
          <article
            v-for="w in workflows"
            :key="w.id"
            class="flex flex-col gap-3 rounded-lg border border-line bg-surface p-3 transition hover:border-line-strong hover:bg-elevated"
          >
            <button
              type="button"
              class="flex w-full items-start justify-between gap-3 text-left"
              @click="openWorkflow(w)"
            >
              <div class="min-w-0 flex-1">
                <div class="truncate text-sm font-semibold text-txt">{{ w.name }}</div>
                <div
                  v-if="w.description"
                  class="mt-0.5 truncate text-[11px] text-txt3"
                >{{ w.description }}</div>
              </div>
              <StatusPill :status="w.status" size="sm" />
            </button>

            <!-- Own row: nowrap Off/Inherit/Custom must not share flex-1 with Run/Favorite (overflow-hidden clip). -->
            <div
              class="flex min-w-0 w-full flex-col gap-1.5"
              data-testid="wf-notify-inline"
              @click.stop
            >
              <span
                v-if="savingNotifyWfId === w.id"
                class="text-[11px] text-txt3"
                data-testid="wf-notify-saving"
              >{{ t('common.buttons.saving') }}</span>
              <div class="max-w-full overflow-x-auto">
                <div class="rounded-lg inline-flex w-max max-w-none border border-line text-[11px]">
                  <button
                    type="button"
                    class="shrink-0 whitespace-nowrap px-2 py-1 transition"
                    :class="
                      wfNotifyMode(w) === 'off'
                        ? 'bg-err/15 text-err'
                        : 'bg-surface text-txt3 hover:bg-elevated hover:text-txt'
                    "
                    :disabled="savingNotifyWfId === w.id"
                    @click="setWorkflowNotifyMode(w, 'off')"
                  >
                    {{ t('pages.projectDetail.notify.modeOff') }}
                  </button>
                  <button
                    type="button"
                    class="shrink-0 whitespace-nowrap border-l border-line px-2 py-1 transition"
                    :class="
                      wfNotifyMode(w) === 'inherit'
                        ? 'bg-accent-dim text-accent-2'
                        : 'bg-surface text-txt3 hover:bg-elevated hover:text-txt'
                    "
                    :disabled="savingNotifyWfId === w.id"
                    @click="setWorkflowNotifyMode(w, 'inherit')"
                  >
                    {{ t('pages.projectDetail.notify.modeInherit') }}
                  </button>
                  <button
                    type="button"
                    class="shrink-0 whitespace-nowrap border-l border-line px-2 py-1 transition"
                    :class="
                      wfNotifyMode(w) === 'custom'
                        ? 'bg-accent-dim text-accent-2'
                        : 'bg-surface text-txt3 hover:bg-elevated hover:text-txt'
                    "
                    :disabled="savingNotifyWfId === w.id"
                    @click="setWorkflowNotifyMode(w, 'custom')"
                  >
                    {{ t('pages.projectDetail.notify.modeCustom') }}
                  </button>
                </div>
              </div>
              <div v-if="wfNotifyMode(w) === 'custom'" class="flex flex-wrap gap-2 text-[11px] text-txt2">
                <label class="inline-flex items-center gap-1">
                  <input
                    type="checkbox"
                    class="accent-accent"
                    :checked="wfNotifyHas(w, 'waiting_human')"
                    :disabled="savingNotifyWfId === w.id"
                    @change="toggleWorkflowNotifyEvent(w, 'waiting_human')"
                  />
                  <span>{{ t('pages.projectDetail.notify.segWaitingHuman') }}</span>
                </label>
                <label class="inline-flex items-center gap-1">
                  <input
                    type="checkbox"
                    class="accent-accent"
                    :checked="wfNotifyHas(w, 'failed')"
                    :disabled="savingNotifyWfId === w.id"
                    @change="toggleWorkflowNotifyEvent(w, 'failed')"
                  />
                  <span>{{ t('pages.projectDetail.notify.segFailed') }}</span>
                </label>
                <label class="inline-flex items-center gap-1">
                  <input
                    type="checkbox"
                    class="accent-accent"
                    :checked="wfNotifyHas(w, 'completed')"
                    :disabled="savingNotifyWfId === w.id"
                    @change="toggleWorkflowNotifyEvent(w, 'completed')"
                  />
                  <span>{{ t('pages.projectDetail.notify.segCompleted') }}</span>
                </label>
              </div>
            </div>
            <div
              class="flex min-w-0 items-center gap-2"
              data-testid="wf-home-visibility-inline"
              @click.stop
            >
              <AppSwitch
                :model-value="!!w.showOnHome"
                :disabled="savingHomeWfId === w.id"
                :aria-label="t('pages.projectDetail.homeVisibility.label')"
                data-testid="wf-home-visibility-switch"
                @update:model-value="toggleWorkflowShowOnHome(w, $event)"
              />
              <span class="text-[12px] text-txt2">{{ t('pages.projectDetail.homeVisibility.label') }}</span>
              <span
                v-if="savingHomeWfId === w.id"
                class="text-[11px] text-txt3"
                data-testid="wf-home-visibility-saving"
              >{{ t('common.buttons.saving') }}</span>
            </div>
            <div class="relative flex items-center gap-2" data-wf-menu @click.stop>
              <button
                type="button"
                class="inline-flex min-h-[44px] flex-1 items-center justify-center gap-1.5 rounded-md bg-accent-dim px-3.5 text-sm font-medium text-accent-2 transition hover:brightness-110"
                @click="openRun(w)"
              >
                <Icon name="play" :size="14" />
                {{ t('common.buttons.run') }}
              </button>
              <button
                type="button"
                class="inline-flex min-h-[44px] flex-1 items-center justify-center gap-1.5 rounded-md border border-line bg-surface px-3 text-sm font-medium text-txt2 transition hover:border-line-strong hover:bg-elevated hover:text-txt"
                :class="{ 'border-warn/40 text-warn': isFavorite(w.id) }"
                data-testid="workflow-favorite-btn"
                @click="toggleWorkflowFavorite(w)"
              >
                <Icon :name="isFavorite(w.id) ? 'star-filled' : 'star'" :size="14" />
                {{ isFavorite(w.id) ? t('common.buttons.unfavorite') : t('common.buttons.favorite') }}
              </button>
              <button
                type="button"
                class="inline-flex h-11 w-11 shrink-0 items-center justify-center rounded-md border border-line bg-surface text-txt2 transition hover:border-line-strong hover:bg-elevated hover:text-txt"
                :class="{ 'border-line-strong bg-elevated text-txt': openMenuId === w.id }"
                :aria-label="t('pages.workflowList.moreActions')"
                :aria-expanded="openMenuId === w.id"
                aria-haspopup="menu"
                :aria-controls="openMenuId === w.id ? menuIdFor(w.id) : undefined"
                @click="toggleMenu(w.id)"
              >
                <Icon name="more" :size="18" />
              </button>
              <div
                v-if="openMenuId === w.id"
                :id="menuIdFor(w.id)"
                class="absolute bottom-[calc(100%+6px)] right-0 z-30 min-w-[148px] rounded-md border border-line-strong bg-surface p-1 shadow-lg"
                role="menu"
              >
                <button
                  type="button"
                  role="menuitem"
                  class="flex min-h-[44px] w-full items-center gap-2 rounded-md px-3 text-left text-[13px] text-txt2 transition hover:bg-elevated hover:text-txt"
                  @click="openEdit(w)"
                >
                  <Icon name="edit" :size="14" />{{ t('common.buttons.edit') }}
                </button>
                <button
                  type="button"
                  role="menuitem"
                  class="flex min-h-[44px] w-full items-center gap-2 rounded-md px-3 text-left text-[13px] text-txt2 transition hover:bg-elevated hover:text-txt disabled:opacity-50"
                  :disabled="copyPreviewLoading === w.id"
                  @click="openCopy(w)"
                >
                  <Icon name="copy" :size="14" />{{
                    copyPreviewLoading === w.id ? t('common.buttons.copying') : t('common.buttons.copy')
                  }}
                </button>
                <button
                  type="button"
                  role="menuitem"
                  class="flex min-h-[44px] w-full items-center gap-2 rounded-md px-3 text-left text-[13px] text-txt2 transition hover:bg-elevated hover:text-txt"
                  @click="openExport(w)"
                >
                  <Icon name="download" :size="14" />{{ t('common.buttons.export') }}
                </button>
                <button
                  type="button"
                  role="menuitem"
                  class="flex min-h-[44px] w-full items-center gap-2 rounded-md px-3 text-left text-[13px] text-err transition hover:bg-err/10"
                  @click="openDeleteWf(w)"
                >
                  <Icon name="trash" :size="14" />{{ t('common.buttons.delete') }}
                </button>
              </div>
            </div>
          </article>
        </div>
        <!-- Desktop table -->
        <div v-else class="overflow-hidden rounded-lg border border-line">
          <div class="scroll-area overflow-x-auto">
            <table class="w-full min-w-[1080px] text-left text-sm" data-testid="workflows-desktop-table">
              <thead class="bg-elevated text-xs text-txt3">
                <tr>
                  <th class="px-3 py-2 font-medium whitespace-nowrap">{{ t('pages.projectDetail.colName') }}</th>
                  <th class="px-3 py-2 font-medium whitespace-nowrap">{{ t('pages.projectDetail.colStatus') }}</th>
                  <th class="px-3 py-2 font-medium whitespace-nowrap">{{ t('pages.projectDetail.notify.colPolicy') }}</th>
                  <th class="px-3 py-2 font-medium whitespace-nowrap">{{ t('pages.projectDetail.homeVisibility.col') }}</th>
                  <th class="px-3 py-2 font-medium whitespace-nowrap">{{ t('pages.projectDetail.colUpdated') }}</th>
                  <th class="px-3 py-2 text-right font-medium whitespace-nowrap">{{ t('common.table.actions') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="w in workflows"
                  :key="w.id"
                  class="cursor-pointer border-t border-line hover:bg-elevated"
                  @click="openWorkflow(w)"
                >
                  <td class="px-3 py-2.5">
                    <div class="font-medium text-txt">{{ w.name }}</div>
                    <div v-if="w.description" class="truncate text-xs text-txt3">{{ w.description }}</div>
                  </td>
                  <td class="px-3 py-2.5"><StatusPill :status="w.status" size="sm" /></td>
                  <td class="px-3 py-2.5" @click.stop data-testid="wf-notify-cell">
                    <span
                      v-if="savingNotifyWfId === w.id"
                      class="mr-2 text-[11px] text-txt3"
                      data-testid="wf-notify-saving"
                    >{{ t('common.buttons.saving') }}</span>
                    <div class="inline-flex overflow-hidden rounded-md border border-line text-[11px]">
                      <button
                        type="button"
                        class="shrink-0 whitespace-nowrap px-2 py-1 transition"
                        :class="
                          wfNotifyMode(w) === 'off'
                            ? 'bg-err/15 text-err'
                            : 'bg-surface text-txt3 hover:bg-elevated hover:text-txt'
                        "
                        :disabled="savingNotifyWfId === w.id"
                        @click="setWorkflowNotifyMode(w, 'off')"
                      >
                        {{ t('pages.projectDetail.notify.modeOff') }}
                      </button>
                      <button
                        type="button"
                        class="shrink-0 whitespace-nowrap border-l border-line px-2 py-1 transition"
                        :class="
                          wfNotifyMode(w) === 'inherit'
                            ? 'bg-accent-dim text-accent-2'
                            : 'bg-surface text-txt3 hover:bg-elevated hover:text-txt'
                        "
                        :disabled="savingNotifyWfId === w.id"
                        @click="setWorkflowNotifyMode(w, 'inherit')"
                      >
                        {{ t('pages.projectDetail.notify.modeInherit') }}
                      </button>
                      <button
                        type="button"
                        class="shrink-0 whitespace-nowrap border-l border-line px-2 py-1 transition"
                        :class="
                          wfNotifyMode(w) === 'custom'
                            ? 'bg-accent-dim text-accent-2'
                            : 'bg-surface text-txt3 hover:bg-elevated hover:text-txt'
                        "
                        :disabled="savingNotifyWfId === w.id"
                        @click="setWorkflowNotifyMode(w, 'custom')"
                      >
                        {{ t('pages.projectDetail.notify.modeCustom') }}
                      </button>
                    </div>
                    <div
                      v-if="wfNotifyMode(w) === 'custom'"
                      class="mt-1.5 flex flex-wrap gap-2 text-[11px] text-txt2"
                    >
                      <label class="inline-flex items-center gap-1">
                        <input
                          type="checkbox"
                          class="accent-accent"
                          :checked="wfNotifyHas(w, 'waiting_human')"
                          :disabled="savingNotifyWfId === w.id"
                          @change="toggleWorkflowNotifyEvent(w, 'waiting_human')"
                        />
                        <span>{{ t('pages.projectDetail.notify.segWaitingHuman') }}</span>
                      </label>
                      <label class="inline-flex items-center gap-1">
                        <input
                          type="checkbox"
                          class="accent-accent"
                          :checked="wfNotifyHas(w, 'failed')"
                          :disabled="savingNotifyWfId === w.id"
                          @change="toggleWorkflowNotifyEvent(w, 'failed')"
                        />
                        <span>{{ t('pages.projectDetail.notify.segFailed') }}</span>
                      </label>
                      <label class="inline-flex items-center gap-1">
                        <input
                          type="checkbox"
                          class="accent-accent"
                          :checked="wfNotifyHas(w, 'completed')"
                          :disabled="savingNotifyWfId === w.id"
                          @change="toggleWorkflowNotifyEvent(w, 'completed')"
                        />
                        <span>{{ t('pages.projectDetail.notify.segCompleted') }}</span>
                      </label>
                    </div>
                  </td>
                  <td class="px-3 py-2.5" @click.stop data-testid="wf-home-visibility-cell">
                    <div class="flex items-center gap-2">
                      <AppSwitch
                        :model-value="!!w.showOnHome"
                        :disabled="savingHomeWfId === w.id"
                        :aria-label="t('pages.projectDetail.homeVisibility.label')"
                        data-testid="wf-home-visibility-switch"
                        @update:model-value="toggleWorkflowShowOnHome(w, $event)"
                      />
                      <span
                        v-if="savingHomeWfId === w.id"
                        class="text-[11px] text-txt3"
                        data-testid="wf-home-visibility-saving"
                      >{{ t('common.buttons.saving') }}</span>
                    </div>
                  </td>
                  <td class="px-3 py-2.5 text-txt3">{{ fmtTime(w.updatedAt) }}</td>
                  <td class="px-3 py-2.5" @click.stop data-testid="wf-actions-cell">
                    <div class="flex items-center justify-end gap-1 whitespace-nowrap" data-testid="wf-actions-group">
                      <button
                        type="button"
                        class="whitespace-nowrap rounded-md px-2 py-1 text-xs text-txt2 hover:bg-overlay hover:text-txt"
                        @click="openEdit(w)"
                      >
                        <Icon name="edit" :size="13" class="mr-1 inline" />{{ t('common.buttons.edit') }}
                      </button>
                      <button
                        type="button"
                        class="whitespace-nowrap rounded-md px-2 py-1 text-xs text-accent-2 hover:bg-accent-dim"
                        @click="openRun(w)"
                      >
                        <Icon name="play" :size="13" class="mr-1 inline" />{{ t('common.buttons.run') }}
                      </button>
                      <button
                        type="button"
                        class="inline-flex items-center justify-center gap-1 whitespace-nowrap rounded-md px-2 py-1 text-xs text-txt2 hover:bg-overlay hover:text-txt"
                        :class="{ 'text-warn hover:bg-warn/10': isFavorite(w.id) }"
                        :style="{ minWidth: favoriteBtnMinWidth }"
                        data-testid="workflow-favorite-btn"
                        @click="toggleWorkflowFavorite(w)"
                      >
                        <Icon
                          :name="isFavorite(w.id) ? 'star-filled' : 'star'"
                          :size="13"
                        />{{ isFavorite(w.id) ? t('common.buttons.unfavorite') : t('common.buttons.favorite') }}
                      </button>
                      <button
                        type="button"
                        class="whitespace-nowrap rounded-md px-2 py-1 text-xs text-accent-2 hover:bg-accent-dim disabled:opacity-50"
                        :disabled="copyPreviewLoading === w.id"
                        @click="openCopy(w)"
                      >
                        <Icon name="copy" :size="13" class="mr-1 inline" />{{
                          copyPreviewLoading === w.id ? t('common.buttons.copying') : t('common.buttons.copy')
                        }}
                      </button>
                      <button
                        type="button"
                        class="whitespace-nowrap rounded-md px-2 py-1 text-xs text-accent-2 hover:bg-accent-dim"
                        @click="openExport(w)"
                      >
                        <Icon name="download" :size="13" class="mr-1 inline" />{{ t('common.buttons.export') }}
                      </button>
                      <button
                        type="button"
                        class="whitespace-nowrap rounded-md px-2 py-1 text-xs text-txt2 hover:bg-err/10 hover:text-err"
                        @click="openDeleteWf(w)"
                      >
                        <Icon name="trash" :size="13" class="mr-1 inline" />{{ t('common.buttons.delete') }}
                      </button>
                    </div>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
        </div>
      </div>

      <!-- Agents: embed Agent Studio (chat test is an inner Studio tab) -->
      <div v-else-if="tab === 'agents'" class="flex min-h-0 flex-1 flex-col" data-testid="project-agents-tab">
        <ProjectAgentsPanel :project-id="projectId" />
      </div>

      <!-- Shared Agent config: fill remaining main area -->
      <div v-else-if="tab === 'sharedAgent'" class="flex min-h-0 flex-1 flex-col">
        <ProjectSharedAgentPanel :project-id="projectId" />
      </div>

      <!-- Variables tab: fill remaining main area; no varsHint / merge-rules row -->
      <div v-else-if="tab === 'variables'" class="flex min-h-0 flex-1 flex-col">
        <!-- Empty: same shell as sandbox tab -->
        <div
          v-if="!varRows.length"
          class="rounded-lg flex min-h-[360px] flex-1 flex-col border border-b-0 border-line bg-surface shadow-[var(--shadow-card)]"
          data-testid="workflow-vars-empty-shell"
        >
          <div class="flex flex-1 flex-col items-center justify-center">
            <EmptyState
              icon="doc"
              :title="t('pages.projectDetail.varsEmptyTitle')"
              :desc="t('pages.projectDetail.varsEmptyDesc')"
            >
              <AppButton variant="primary" icon="plus" @click="addVarRow">
                {{ t('pages.projectDetail.addRow') }}
              </AppButton>
            </EmptyState>
          </div>
        </div>

        <!-- Data: head / scroll rows / foot stick to shell bottom -->
        <div
          v-else
          class="rounded-lg flex min-h-0 flex-1 flex-col overflow-hidden border border-b-0 border-line bg-surface shadow-[var(--shadow-card)]"
          data-testid="workflow-vars-data-panel"
        >
          <div
            class="hidden shrink-0 gap-2 border-b border-line bg-elevated/55 px-3 py-2.5 text-[11px] font-semibold uppercase tracking-wider text-txt3 sm:grid sm:grid-cols-[minmax(0,1fr)_minmax(0,1.2fr)_minmax(120px,auto)_40px]"
          >
            <span>{{ t('pages.projectDetail.colVarName') }}</span>
            <span>{{ t('pages.projectDetail.colDefault') }}</span>
            <span>{{ t('pages.projectDetail.colType') }}</span>
            <span>{{ t('common.table.actions') }}</span>
          </div>

          <div class="scroll-area flex min-h-0 flex-1 flex-col overflow-y-auto">
            <div
              v-for="(row, i) in varRows"
              :key="i"
              class="border-b border-line bg-base/40 px-3 py-2 last:border-b-0 hover:bg-elevated/35"
            >
              <div
                class="grid grid-cols-1 gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1.2fr)_minmax(120px,auto)_40px] sm:items-center"
              >
                <input
                  :value="row.name"
                  class="input min-w-0 px-2.5 py-1.5 font-mono text-xs"
                  :placeholder="t('pages.projectDetail.varName')"
                  @input="onVarNameChange(row, ($event.target as HTMLInputElement).value)"
                />

                <!-- Value control matrix by type -->
                <div
                  v-if="row.type === 'bool'"
                  class="rounded-lg flex min-w-0 overflow-hidden border border-line"
                >
                  <button
                    type="button"
                    class="flex-1 bg-base px-2.5 py-1.5 text-xs transition"
                    :class="isBoolTrue(row) ? 'bg-accent-dim text-accent-2' : 'text-txt3'"
                    @click="setBoolValue(row, true)"
                  >
                    true
                  </button>
                  <button
                    type="button"
                    class="flex-1 bg-base px-2.5 py-1.5 text-xs transition"
                    :class="!isBoolTrue(row) ? 'bg-accent-dim text-accent-2' : 'text-txt3'"
                    @click="setBoolValue(row, false)"
                  >
                    false
                  </button>
                </div>
                <input
                  v-else-if="row.type === 'number'"
                  :value="row.value == null ? '' : String(row.value)"
                  type="number"
                  class="input min-w-0 px-2.5 py-1.5 text-xs"
                  :placeholder="t('pages.projectDetail.varValue')"
                  @input="onVarValueInput(row, ($event.target as HTMLInputElement).value, true)"
                />
                <textarea
                  v-else-if="row.type === 'paragraph'"
                  :value="row.value == null ? '' : String(row.value)"
                  rows="2"
                  class="input min-h-[40px] min-w-0 px-2.5 py-1.5 text-xs"
                  :placeholder="t('pages.projectDetail.varValue')"
                  @input="onVarValueInput(row, ($event.target as HTMLTextAreaElement).value)"
                />
                <select
                  v-else-if="row.type === 'select'"
                  :value="row.value == null ? '' : String(row.value)"
                  class="input min-w-0 px-2.5 py-1.5 text-xs"
                  @change="onVarValueInput(row, ($event.target as HTMLSelectElement).value)"
                >
                  <option value="">{{ t('pages.projectDetail.selectPlaceholder') }}</option>
                  <option v-for="opt in selectOptions(row)" :key="opt" :value="opt">{{ opt }}</option>
                </select>
                <input
                  v-else
                  :value="row.value == null ? '' : String(row.value)"
                  class="input min-w-0 px-2.5 py-1.5 text-xs"
                  :placeholder="t('pages.projectDetail.varValue')"
                  :type="row.secret ? 'password' : 'text'"
                  @input="onVarValueInput(row, ($event.target as HTMLInputElement).value)"
                />

                <div class="flex min-w-0 flex-wrap items-center gap-1.5">
                  <select
                    :value="row.type"
                    class="input min-w-0 flex-1 px-2.5 py-1.5 text-xs sm:w-[100px] sm:flex-none"
                    @change="onVarTypeChange(row, ($event.target as HTMLSelectElement).value)"
                  >
                    <option v-for="vt in VAR_TYPES" :key="vt.value" :value="vt.value">
                      {{ vt.label }}
                    </option>
                  </select>
                  <button
                    type="button"
                    class="chip shrink-0"
                    :class="row.secret ? 'border-accent/50 text-accent-2' : 'text-txt3'"
                    :title="row.secret ? t('pages.projectDetail.secret') : t('pages.projectDetail.plain')"
                    @click="onVarSecretChange(row, !row.secret)"
                  >
                    {{ row.secret ? t('pages.projectDetail.secret') : t('pages.projectDetail.plain') }}
                  </button>
                </div>

                <button
                  type="button"
                  class="inline-flex h-7 w-7 shrink-0 items-center justify-center text-txt3 hover:text-err sm:justify-self-center"
                  :title="t('common.buttons.delete')"
                  :aria-label="t('common.buttons.delete')"
                  @click="removeVarRow(i)"
                >
                  <Icon name="close" :size="14" />
                </button>
              </div>

              <!-- Secondary row: desc / select options -->
              <div class="mt-2 flex flex-col gap-1.5">
                <input
                  v-model="row.desc"
                  class="input px-2.5 py-1.5 text-xs"
                  :placeholder="t('pages.projectDetail.varDescPlaceholder')"
                />
                <input
                  v-if="row.type === 'select'"
                  v-model="row.options"
                  class="input px-2.5 py-1.5 font-mono text-xs"
                  :placeholder="t('pages.projectDetail.varOptionsPlaceholder')"
                />
              </div>
            </div>
          </div>

          <div
            class="flex shrink-0 flex-wrap gap-2 border-t border-line bg-surface p-3"
            data-testid="workflow-vars-footer"
          >
            <AppButton variant="outline" icon="plus" @click="addVarRow">
              {{ t('pages.projectDetail.addRow') }}
            </AppButton>
            <AppButton variant="primary" :disabled="savingVars" @click="saveVars">
              {{ savingVars ? t('common.buttons.saving') : t('common.buttons.save') }}
            </AppButton>
          </div>
        </div>
      </div>

      <div v-else-if="tab === 'audit'" class="flex min-h-0 flex-1 flex-col" data-testid="project-audit-tab">
        <ProjectAuditPanel :project-id="projectId" :force-denied="auditForceDenied" />
      </div>

      <!-- Project info (meta) tab: fill remaining main area (no page void under card) -->
      <div v-else-if="tab === 'meta'" class="flex min-h-0 flex-1 flex-col">
        <div
          class="rounded-lg flex flex-1 flex-col overflow-hidden border border-line bg-surface shadow-[var(--shadow-card)]"
        >
          <div class="shrink-0 border-b border-line bg-elevated/55 px-4 py-3.5">
            <h2 class="m-0 text-sm font-semibold text-txt">
              {{ t('pages.projectDetail.metaTitle') }}
            </h2>
            <p class="m-0 mt-1 text-[13px] leading-snug text-txt3">
              {{ t('pages.projectDetail.metaHint') }}
            </p>
          </div>

          <div class="scroll-area flex min-h-0 flex-1 flex-col gap-3.5 overflow-y-auto p-4">
            <div>
              <label class="label" for="project-meta-name">{{ t('pages.projectList.nameLabel') }}</label>
              <input
                id="project-meta-name"
                v-model="editName"
                class="input"
                autocomplete="off"
              />
            </div>
            <div>
              <label class="label" for="project-meta-desc">{{ t('pages.projectList.descLabel') }}</label>
              <textarea
                id="project-meta-desc"
                v-model="editDesc"
                rows="5"
                class="input min-h-[120px] resize-y"
                :placeholder="t('pages.projectDetail.metaDescPlaceholder')"
              />
            </div>
            <div>
              <label class="label" for="project-meta-unknown-display">
                {{ t('pages.projectDetail.unknownModelDisplayNameLabel') }}
              </label>
              <input
                id="project-meta-unknown-display"
                v-model="editUnknownModelDisplayName"
                class="input"
                maxlength="80"
                autocomplete="off"
                data-testid="project-meta-unknown-display"
                :placeholder="t('pages.projectDetail.unknownModelDisplayNamePlaceholder')"
                @input="unknownModelDisplayNameError = ''"
              />
              <p class="m-0 mt-1.5 text-[11px] leading-snug text-txt3">
                {{ t('pages.projectDetail.unknownModelDisplayNameHelp') }}
              </p>
              <p
                v-if="unknownModelDisplayNameError"
                class="m-0 mt-1.5 text-xs text-err"
                data-testid="project-meta-unknown-display-error"
              >
                {{ unknownModelDisplayNameError }}
              </p>
            </div>
          </div>

          <div
            class="flex shrink-0 flex-wrap items-center justify-between gap-2 border-t border-line bg-surface p-3"
            data-testid="project-meta-footer"
          >
            <AppButton
              variant="outline"
              size="sm"
              class="text-err"
              data-testid="project-meta-delete"
              @click="showDelete = true"
            >
              {{ t('common.buttons.delete') }}
            </AppButton>
            <div class="flex flex-wrap items-center gap-2">
              <AppButton
                variant="outline"
                size="sm"
                data-testid="project-meta-unknown-display-clear"
                :disabled="savingMeta"
                @click="clearUnknownModelDisplayName"
              >
                {{ t('pages.projectDetail.unknownModelDisplayNameClear') }}
              </AppButton>
              <AppButton variant="primary" :disabled="savingMeta" @click="saveMeta">
                {{ savingMeta ? t('common.buttons.saving') : t('common.buttons.save') }}
              </AppButton>
            </div>
          </div>
        </div>
      </div>
      </div>
      </Transition>
    </template>
    </div>

    <input ref="fileInput" type="file" accept=".json" class="hidden" @change="handleFileChange" />

    <RunLaunchModal
      v-if="runTarget"
      :open="!!runTarget"
      :workflow-id="runTarget.id"
      :project-id="runTarget.projectId"
      :workflow-name="runTarget.name"
      :fields="runFields"
      :run-inputs="runInputs"
      :run-images="runImages"
      :draft-restored="draftRestored"
      @close="closeRunModal"
      @stayed="onRunStayed"
      @view-run="onViewRun"
      @save-draft="saveRunDraftClick"
      @started="onRunStarted"
    />

    <CopyWorkflowModal
      v-if="copyModal"
      :open="!!copyModal"
      :source-id="copyModal.sourceId"
      :source-name="copyModal.sourceName"
      :suggested-name="copyModal.suggestedName"
      :existing-names="existingNames()"
      @close="closeCopyModal"
      @copied="onCopied"
    />

    <ExportVersionModal
      v-if="exportTarget"
      :open="!!exportTarget"
      :workflow-id="exportTarget.id"
      :workflow-name="exportTarget.name"
      :description="exportTarget.description"
      :needs-repo="exportTarget.needsRepo"
      :status="exportTarget.status"
      @close="exportTarget = null"
    />

    <AppModal
      :open="baselineModalOpen"
      :title="t('pages.projectDetail.newWorkflow.modalTitle')"
      close-on-esc
      :close-on-backdrop="!creatingBaseline"
      @close="closeBaselineModal"
    >
      <p class="mb-3 text-[13px] leading-relaxed text-txt2">
        {{ t('pages.projectDetail.newWorkflow.modalHint') }}
      </p>
      <div class="mb-4">
        <label class="label" for="baseline-workflow-name">
          {{ t('pages.projectDetail.newWorkflow.nameLabel') }}
        </label>
        <input
          id="baseline-workflow-name"
          v-model="baselineName"
          class="input"
          autocomplete="off"
          data-testid="baseline-workflow-name"
          :placeholder="t('pages.projectDetail.newWorkflow.namePlaceholder')"
        />
      </div>
      <ReposEditor
        :repos="baselineRepos"
        :min-rows="1"
        @update:repos="baselineRepos = $event"
      />
      <div
        v-if="baselineCreateError"
        class="mt-3 flex items-start gap-2 rounded-md border border-err/30 bg-err/10 px-3 py-2 text-[12px] text-err"
        role="alert"
      >
        <Icon name="alert" :size="14" class="mt-0.5 shrink-0" />
        {{ baselineCreateError }}
      </div>
      <template #footer>
        <AppButton variant="ghost" :disabled="creatingBaseline" @click="closeBaselineModal">
          {{ t('common.buttons.cancel') }}
        </AppButton>
        <AppButton
          variant="primary"
          :loading="creatingBaseline"
          :disabled="!hasValidBaselineRepo"
          data-testid="create-baseline-workflow"
          @click="createFromBaseline"
        >
          {{ creatingBaseline ? t('pages.projectDetail.newWorkflow.creating') : t('pages.projectDetail.newWorkflow.create') }}
        </AppButton>
      </template>
    </AppModal>

    <AppModal
      :open="!!deleteWfTarget"
      :title="t('pages.workflowList.deleteTitle', { name: deleteWfTarget?.name || '' })"
      @close="deleteWfTarget = null"
    >
      <div class="space-y-3 text-sm text-txt2">
        <div class="flex items-start gap-2 rounded-md border border-err/30 bg-err/10 px-3 py-2 text-[12px] text-err">
          <Icon name="alert" :size="14" class="mt-0.5 shrink-0" />
          {{ t('pages.workflowList.deleteWarning') }}
        </div>
        <p>{{ t('pages.workflowList.deleteConfirm', { name: deleteWfTarget?.name }) }}</p>
        <div
          v-if="deleteWfError"
          class="flex items-start gap-2 rounded-md border border-err/30 bg-err/10 px-3 py-2 text-[12px] text-err"
        >
          <Icon name="alert" :size="14" class="mt-0.5" />{{ deleteWfError }}
        </div>
      </div>
      <template #footer>
        <AppButton variant="ghost" @click="deleteWfTarget = null">{{ t('common.buttons.cancel') }}</AppButton>
        <AppButton variant="danger" icon="trash" :disabled="deletingWf" @click="confirmDeleteWf">
          {{ deletingWf ? t('common.buttons.deleting') : t('common.buttons.confirmDelete') }}
        </AppButton>
      </template>
    </AppModal>

    <AppModal
      :open="showDelete"
      :title="t('pages.projectDetail.deleteTitle', { name: project?.name || '' })"
      :width="420"
      @close="!deleting && (showDelete = false)"
    >
      <p class="mb-3 text-sm text-txt2">{{ t('pages.projectDetail.deleteWarning') }}</p>
      <p v-if="deleteError" class="mb-2 text-sm text-err">{{ deleteError }}</p>
      <div class="flex justify-end gap-2">
        <AppButton variant="outline" :disabled="deleting" @click="showDelete = false">
          {{ t('common.buttons.cancel') }}
        </AppButton>
        <AppButton variant="danger" :disabled="deleting" @click="confirmDelete">
          {{ t('common.buttons.delete') }}
        </AppButton>
      </div>
    </AppModal>
  </div>
</template>

<style scoped>
.app-tabs-indicator {
  transition:
    transform var(--dur-ui) var(--ease-out-expo),
    width var(--dur-ui) var(--ease-out-expo),
    opacity var(--dur-ui) ease;
}
</style>
