<script setup lang="ts">
import Icon from '@/components/ui/Icon.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import PipelineFilter from '@/components/ui/PipelineFilter.vue'
import ProjectFilter from '@/components/ui/ProjectFilter.vue'
import TagFilter from '@/components/ui/TagFilter.vue'
import GateApproval from '@/components/run/GateApproval.vue'
import GateShareLinkPanel from '@/components/run/GateShareLinkPanel.vue'
import InboxPendingCard from '@/components/inbox/InboxPendingCard.vue'
import InboxStartFailedPane from '@/components/inbox/InboxStartFailedPane.vue'
import ReviewShell from '@/components/run/ReviewShell.vue'
import ReviewComposer from '@/components/run/ReviewComposer.vue'
import ConfirmFlowOverlay from '@/components/run/ConfirmFlowOverlay.vue'
import ArtifactLoadingPane from '@/components/run/ArtifactLoadingPane.vue'
import ClarifyProductStage from '@/components/run/ClarifyProductStage.vue'
import ReactArtifactStage from '@/components/run/ReactArtifactStage.vue'
import ReactConnectingState from '@/components/run/ReactConnectingState.vue'
import RefreshStrip from '@/components/run/RefreshStrip.vue'
import AppInlineError from '@/components/ui/AppInlineError.vue'
import Pagination from '@/components/ui/Pagination.vue'
import { computed } from 'vue'
import { useGatesInbox } from '@/lib/inbox/useGatesInbox'

const {
  addProcessingIntent,
  removeProcessingIntent,
  isProcessingIntent,
  isItemCardDisabled,
  markProcessed,
  unmarkProcessed,
  isProcessedTriple,
  abortInboxContext,
  acquireInboxContextSignal,
  releaseInboxContextSignal,
  beginProcessingIntent,
  rollbackProcessingIntent,
  endProcessingIntent,
  isActiveStillInList,
  shouldFetchActiveInboxContext,
  invalidateListLoads,
  removeListItemLocally,
  restoreListItemLocally,
  incomingTarget,
  mergeIncomingGhost,
  confirmIncomingGhostStillNeeded,
  seedIncomingIfNeeded,
  isIncomingContextPending,
  rebindActiveFromList,
  mergeFailedStarting,
  confirmStartingVanished,
  detectStartingFailures,
  dismissStartFailure,
  stopStartingPoll,
  startStartingPoll,
  selectFromQuery,
  applyHomeHandoff,
  selectFromHandoff,
  waitForQueryItem,
  ensureValidActive,
  reconcileProcessedWithList,
  selectActiveAfterRemove,
  handleLeftInboxContext,
  loadList,
  retryListLoad,
  onAppPreviewStagedPick,
  onAppPreviewReviewPick,
  mergeStagedAppPreviewPick,
  syncActiveAfterApply,
  checkProcessedWhileEditing,
  isActive,
  isClarifySoftRefreshBlocked,
  closeActiveRunWs,
  activeDialogueNodeId,
  applyOrBufferAcpFrame,
  flushPendingAcpFrames,
  seedClarifyAcpFromNodeEventsOnce,
  startBusySeedRetry,
  restoreReactSessions,
  flushPendingReviewFrames,
  applyOrBufferReviewFrame,
  projectClarifySessionAfterLoad,
  reseedAfterWsReconnect,
  connectActiveRunWs,
  patchActiveRunArtifacts,
  softRefreshActiveRun,
  loadActiveRun,
  retryActiveRun,
  applyListUpdate,
  onManualRefresh,
  dismissUpdateBanner,
  onReactRevised,
  onResolve,
  patchShareStatus,
  openSharePanel,
  selectItem,
  openDetail,
  backToList,
  onClarifySend,
  onClarifyRetryLast,
  onClarifyFinish,
  onClarifyCancel,
  onClarifyQueueRemove,
  onClarifyQueueReorder,
  onFocus,
  onVisible,
  itemTitle,
  itemSecondary,
  router,
  route,
  toast,
  PAGE_SIZE,
  SKELETON_CARDS,
  listItems,
  listTotal,
  listPage,
  listLoading,
  listErrorMessage,
  listLoadGeneration,
  active,
  homeSeed,
  incomingGhost,
  startFailedItem,
  incomingArmed,
  activeHomeSeed,
  mobileView,
  listScrollTop,
  listEl,
  gateApprovalRef,
  reviewChatRef,
  reviewShellRef,
  confirmFlowDeskHold,
  inboxConfirmFlowPhase,
  inboxConfirmFlowReduce,
  inboxConfirmFlowToken,
  pendingAcpFrames,
  projectFilterOpen,
  pipelineFilterOpen,
  tagFilterOpen,
  showUpdateBanner,
  showProcessedBanner,
  manualRefreshing,
  showListSkeleton,
  showListError,
  showListRefresh,
  listPanelBusy,
  clarifyConfirmError,
  processedTriples,
  confirmedAbsentTriples,
  inboxContextAborts,
  processingIntentKeys,
  processingLock,
  startFailedActive,
  activeStarting,
  startingSandboxPhase,
  incomingGhostConfirmInFlight,
  STARTING_POLL_MS,
  STARTING_POLL_MAX_TICKS,
  queryWaitGen,
  isGateEditing,
  lastStagedAppPreviewPick,
  isClarifyEditing,
  isEditing,
  statusPillClass,
  statusPillText,
  updateBannerDetail,
  activeRun,
  activeRunLoading,
  activeRunLoadError,
  activeRunWsRunId,
  clarifyLiveBusy,
  dialogueRailsFilled,
  dialogueLiveIncremental,
  busySeedRetry,
  activeRunWsReconnect,
  activeClarify,
  inboxAppPreviewActive,
  inboxRemoteKind,
  inboxStageNodeType,
  showClarifyReviewShell,
  clarifyComposerNodeId,
  clarifyComposerIteration,
  clarifyComposerTurns,
  clarifyComposerDone,
  clarifyComposerNodeType,
  inboxClarifyStageKind,
  inboxReviewState,
  reviewActive,
  composerMode,
  activeGate,
  clarifyInputActive,
  sharePanelOpen,
  shareTarget,
  t,
  displayedItems,
  remoteItems,
  totalCount,
  refresh,
  peek,
  applyPending,
  removeItemLocally,
  restoreItemLocally,
  hasPendingUpdate,
  pendingMeta,
  lastPeekAt,
  itemKey,
  ariaBusy,
  isMobile,
  selected,
  selectedProject,
  selectedTags,
  clarifyDraft,
  clarifyAttachments,
  clarifyAnnotations,
  isShareableInboxItem,
  isHumanGateInboxItem,
  inboxShareKind,
  REVIEW_SHELL_WIDTH_KEY_APPROVAL,
} = useGatesInbox()

/** Whole-list fade when filter/page changes (g3.2). */
const listFadeKey = computed(() =>
  [listPage.value, selectedProject.value || '', selectedTags.value.slice().sort().join(',')].join('|'),
)
</script>

<template>
  <div
    class="flex h-full min-h-0 flex-col"
    data-testid="gates-inbox-view"
    :aria-busy="listPanelBusy ? 'true' : 'false'"
  >
    <!-- Mobile detail header -->
    <div v-if="isMobile && mobileView === 'detail'" class="mb-3 flex shrink-0 items-center gap-2">
      <button
        class="flex h-11 w-11 shrink-0 items-center justify-center rounded-md text-txt2 hover:bg-elevated hover:text-txt"
        :aria-label="t('shell.aria.backToList')"
        @click="backToList"
      >
        <Icon name="arrow-left" :size="18" />
      </button>
      <div class="min-w-0 flex-1">
        <h2 class="truncate text-base font-semibold text-txt">{{ active ? itemTitle(active) : '' }}</h2>
        <p class="truncate text-[11px] text-txt3" :title="active ? itemSecondary(active) : undefined">
          {{ active ? itemSecondary(active) : '' }}
        </p>
      </div>
    </div>

    <!-- Desktop / mobile list header -->
    <div v-else class="mb-5 flex shrink-0 flex-col gap-2.5 md:flex-row md:items-start md:justify-between">
      <div class="min-w-0">
        <h2 class="text-lg font-semibold text-txt">{{ t('pages.gatesInbox.title') }}</h2>
        <p class="text-sm text-txt3" v-html="t('pages.gatesInbox.subtitleHtml')" />
      </div>
      <div class="flex w-full shrink-0 flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-center md:w-auto">
        <TagFilter
          v-model="selectedTags"
          v-model:open="tagFilterOpen"
          :project-id="selectedProject"
        />
        <div class="flex flex-wrap items-center gap-2">
          <span
            class="toolbar-control inline-flex items-center gap-1.5 border text-[11px] font-medium"
            data-testid="gates-inbox-status-pill"
            :class="{
              'border-info/40 bg-info/10 text-info': statusPillClass === 'pending',
              'border-accent/40 bg-accent-dim/50 text-accent-2': statusPillClass === 'editing',
              'border-ok/35 bg-ok/10 text-ok': statusPillClass === 'idle',
            }"
          >
            <span
              class="h-1.5 w-1.5 rounded-full"
              :class="{
                'bg-info animate-pulse': statusPillClass === 'pending',
                'bg-accent': statusPillClass === 'editing',
                'bg-ok': statusPillClass === 'idle',
              }"
            />
            {{ statusPillText }}
          </span>
          <button
            class="toolbar-control inline-flex items-center gap-1.5 border border-line bg-surface text-xs font-medium text-txt transition hover:border-line-strong hover:bg-elevated disabled:opacity-45"
            data-testid="gates-inbox-refresh"
            :disabled="manualRefreshing || processingLock"
            :aria-busy="processingLock || undefined"
            @click="onManualRefresh"
          >
            <Icon name="refresh" :size="14" :class="{ 'animate-spin': manualRefreshing }" />
            {{ t('common.buttons.refresh') }}
          </button>
          <ProjectFilter
            v-model="selectedProject"
            v-model:open="projectFilterOpen"
            :count="listTotal"
          />
          <PipelineFilter
            v-model="selected"
            v-model:open="pipelineFilterOpen"
            :count="listTotal"
          />
        </div>
      </div>
    </div>

    <!-- f6: processed while editing -->
    <div
      v-if="showProcessedBanner && isEditing"
      class="mb-3 flex shrink-0 items-center gap-2.5 rounded-md border border-warn/35 bg-warn/10 px-3.5 py-2.5 text-sm text-warn"
    >
      <Icon name="alert" :size="18" class="shrink-0" />
      <span v-html="t('pages.gatesInbox.processedBanner')" />
    </div>

    <!-- f2: pending update banner -->
    <div
      v-if="showUpdateBanner && hasPendingUpdate"
      class="mb-3 flex shrink-0 flex-wrap items-center justify-between gap-3 rounded-md border border-info/35 bg-info/10 px-3.5 py-2.5 text-sm animate-[slideDown_0.25s_ease]"
    >
      <div class="flex min-w-0 flex-1 items-center gap-2.5 text-info">
        <Icon name="bell" :size="18" class="shrink-0" />
        <div class="min-w-0">
          <strong class="font-semibold text-txt">{{ t('pages.gatesInbox.updateBannerTitle') }}</strong>
          <span class="text-txt2">{{ updateBannerDetail }}</span>
        </div>
      </div>
      <div class="flex shrink-0 gap-2">
        <button
          class="rounded-md bg-accent px-3 py-1.5 text-xs font-medium text-white transition hover:bg-accent-hover disabled:opacity-45"
          :disabled="processingLock"
          @click="applyListUpdate()"
        >
          {{ t('common.buttons.applyRefresh') }}
        </button>
        <button
          class="rounded-md border border-line bg-surface px-3 py-1.5 text-xs font-medium text-txt transition hover:bg-elevated"
          @click="dismissUpdateBanner"
        >
          {{ t('common.buttons.later') }}
        </button>
      </div>
    </div>

    <!-- Mobile list view -->
    <template v-if="isMobile && mobileView === 'list'">
      <RefreshStrip v-if="showListRefresh" data-testid="gates-inbox-refresh-strip" />
      <div
        v-if="showListSkeleton"
        class="flex min-h-0 flex-1 flex-col gap-2"
        data-testid="inbox-list-skeleton"
        aria-hidden="true"
      >
        <div
          v-for="n in SKELETON_CARDS"
          :key="'skel-m-' + n"
          class="flex w-full shrink-0 flex-col gap-2 rounded-lg border border-line bg-surface p-3"
          data-testid="inbox-pending-card-skeleton"
        >
          <div class="flex items-start gap-3">
            <div class="h-9 w-9 shrink-0 bg-elevated animate-pulse" />
            <div class="min-w-0 flex-1 space-y-2">
              <div class="h-3.5 w-2/3 bg-elevated animate-pulse" />
              <div class="h-2.5 w-full bg-elevated animate-pulse" />
            </div>
          </div>
        </div>
      </div>
      <div
        v-else-if="showListError"
        class="card flex min-h-0 flex-1 flex-col items-stretch justify-center overflow-auto p-4"
        data-testid="inbox-list-failed"
      >
        <AppInlineError
          :title="t('common.asyncState.loadFailedTitle')"
          :message="listErrorMessage"
          retry-testid="inbox-list-retry"
          @retry="retryListLoad"
        />
      </div>
      <div
        v-else-if="listItems.length"
        class="flex min-h-0 flex-1 flex-col"
        :class="showListRefresh ? 'opacity-[0.55]' : ''"
      >
        <!-- plan g2.1: py-0.5 so list-card-lift hover translateY(-1px) top border is not clipped -->
        <div ref="listEl" class="scroll-area flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto py-0.5">
        <Transition name="ui-fade" mode="out-in">
        <div :key="listFadeKey" class="flex flex-col gap-2" data-testid="inbox-list-fade">
        <InboxPendingCard
          v-for="it in listItems"
          :key="itemKey(it)"
          :item="it"
          :active="isActive(it)"
          :disabled="isItemCardDisabled(it)"
          show-chevron
          @select="openDetail(it)"
          @open-share="openSharePanel(it, true)"
        />
        </div>
        </Transition>
        </div>
        <Pagination v-if="listTotal > PAGE_SIZE" v-model:page="listPage" :page-size="PAGE_SIZE" :total="listTotal" />
      </div>
      <div v-else class="card flex min-h-0 flex-1 flex-col items-center justify-center overflow-auto">
        <EmptyState
          icon="gate"
          :title="listTotal ? t('common.empty.noPendingGatesForPipeline') : t('common.empty.noPendingGates')"
          :desc="
            listTotal
              ? t('common.empty.noPendingGatesPipelineDesc')
              : t('common.empty.noPendingGatesDesc')
          "
        />
      </div>
    </template>

    <!-- Mobile detail view -->
    <div v-else-if="isMobile && mobileView === 'detail' && (active || confirmFlowDeskHold)" class="card relative flex min-h-0 flex-1 flex-col overflow-hidden">
      <div v-if="active" class="flex shrink-0 items-center justify-end gap-3 border-b border-line px-4 py-2">
        <button
          v-if="isShareableInboxItem(active)"
          type="button"
          class="inline-flex min-h-11 items-center text-xs text-accent-2 hover:underline"
          data-testid="gate-share-copy-btn-detail"
          :aria-label="t('pages.gatesInbox.share.copyLinkAria')"
          @click="openSharePanel(active)"
        >
          {{ t('pages.gatesInbox.share.copyLink') }}
        </button>
        <button class="text-xs text-accent-2 hover:underline" @click="router.push('/runs/' + active.runId)">
          {{ t('common.buttons.openRunDetail') }}
        </button>
      </div>
      <div class="relative flex min-h-0 flex-1 flex-col">
        <template v-if="active">
        <ArtifactLoadingPane v-if="activeRunLoading && active.type === 'gate'" message-key="pages.gatesInbox.loadingRun" />
            <GateApproval
          v-else-if="active.type === 'gate'"
          ref="gateApprovalRef"
          :key="active.runId + active.nodeId"
          :gate="activeGate!"
          :run="activeRun || undefined"
          :fill-preview="true"
          :share-link="isHumanGateInboxItem(active) ? active.shareLink ?? { state: 'none' } : null"
          @resolve="onResolve"
          @react-revised="onReactRevised"
          @open-share="openSharePanel(active)"
        />
        <InboxStartFailedPane v-else-if="startFailedActive" @dismiss="dismissStartFailure" />
        <ReviewShell
          v-else-if="showClarifyReviewShell"
          ref="reviewShellRef"
          :key="active.runId + active.nodeId"
          class="min-h-0 flex-1"
          mobile
          :sidebar-width="400"
          :storage-key="REVIEW_SHELL_WIDTH_KEY_APPROVAL"
          :host-confirm-flow="false"
        >
          <template #stage>
            <ReactConnectingState
              v-if="activeStarting"
              mode="stage"
              :sandbox-phase="startingSandboxPhase"
            />
            <ArtifactLoadingPane v-else-if="activeRunLoading" message-key="pages.gatesInbox.loadingRun" />
            <ReactArtifactStage
              v-else
              :artifacts="activeRun?.artifacts || []"
              :preview-artifact="activeClarify?.previewArtifact"
              :run-id="active.runId"
              :run="activeRun || undefined"
              :node-id="active.nodeId"
              :node-type="inboxStageNodeType"
              :annotatable="clarifyInputActive"
              :remote-kind="inboxRemoteKind"
              @pick="onAppPreviewReviewPick"
              @staged-pick="onAppPreviewStagedPick"
            />
          </template>
          <template #sidebar>
            <ReactConnectingState
              v-if="activeStarting"
              :show-confirm="composerMode === 'review'"
              :sandbox-phase="startingSandboxPhase"
            />
            <ReviewComposer
              v-else
              ref="reviewChatRef"
              :mode="composerMode"
              :run-id="active.runId"
              :node-id="clarifyComposerNodeId"
              :iteration="clarifyComposerIteration"
              v-model:draft="clarifyDraft"
              v-model:attachments="clarifyAttachments"
              v-model:annotations="clarifyAnnotations"
              :turns="clarifyComposerTurns"
              :node-type="clarifyComposerNodeType"
              :seed-human-text="activeHomeSeed?.text"
              :seed-human-images="activeHomeSeed?.images ?? []"
              :done="clarifyComposerDone"
              :active="clarifyInputActive"
              :confirm-error="clarifyConfirmError"
              @send="onClarifySend"
              @retry-last="onClarifyRetryLast"
              @finish="onClarifyFinish"
              @cancel="onClarifyCancel"
              @queue-remove="(itemId) => onClarifyQueueRemove(itemId)"
              @queue-reorder="onClarifyQueueReorder"
            />
          </template>
        </ReviewShell>
        <ClarifyProductStage
          v-else-if="active.type === 'clarify'"
          :product-nodes="[]"
          :selected-product-id="null"
          :stage-kind="inboxClarifyStageKind"
          :selected-node="null"
          :selected-node-run="null"
          :run="null"
          :loading="activeRunLoading"
          @retry="retryActiveRun"
        />
        </template>
        <ConfirmFlowOverlay
          :phase="inboxConfirmFlowPhase"
          :reduce-motion="inboxConfirmFlowReduce"
          :play-token="inboxConfirmFlowToken"
        />
      </div>
    </div>

    <!-- Desktop three-zone: list | product stage + review sidebar (via GateApproval/ReviewShell).
         items-stretch so detail card + review sidebar fill remaining viewport height (no page void under card). -->
    <div
      v-else-if="!isMobile && (listItems.length || confirmFlowDeskHold)"
      class="grid min-h-0 flex-1 grid-cols-[320px_1fr] items-stretch gap-4"
      :class="showListRefresh ? 'opacity-[0.55]' : ''"
    >
      <div class="flex h-full min-h-0 flex-col overflow-hidden">
        <RefreshStrip v-if="showListRefresh" data-testid="gates-inbox-refresh-strip" />
        <!-- plan g2.1: py-0.5 so list-card-lift hover translateY(-1px) top border is not clipped -->
        <div class="scroll-area flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto py-0.5">
          <Transition name="ui-fade" mode="out-in">
          <div :key="listFadeKey" class="flex flex-col gap-2" data-testid="inbox-list-fade-desktop">
          <InboxPendingCard
            v-for="it in listItems"
            :key="itemKey(it)"
            :item="it"
            :active="isActive(it)"
            :disabled="isItemCardDisabled(it)"
            @select="selectItem(it)"
            @open-share="openSharePanel(it)"
          />
          </div>
          </Transition>
        </div>
        <Pagination v-if="listTotal > PAGE_SIZE" v-model:page="listPage" :page-size="PAGE_SIZE" :total="listTotal" />
      </div>

      <div v-if="active || confirmFlowDeskHold" class="relative flex h-full min-h-0 min-w-0 flex-col">
        <div class="card relative flex h-full min-h-0 w-full flex-col overflow-hidden">
          <template v-if="active">
          <div class="flex shrink-0 items-center justify-between border-b border-line px-4 py-2.5">
            <span class="text-xs text-txt3">Run #{{ active.runId.replace('run-', '') }} · {{ active.nodeId }}</span>
            <button class="text-xs text-accent-2 hover:underline" @click="router.push('/runs/' + active.runId)">
              {{ t('common.buttons.openRunDetail') }}
            </button>
          </div>
          <div class="flex min-h-0 flex-1 flex-col overflow-hidden">
            <ArtifactLoadingPane v-if="activeRunLoading && active.type === 'gate'" message-key="pages.gatesInbox.loadingRun" />
            <GateApproval
              v-else-if="active.type === 'gate'"
              ref="gateApprovalRef"
              :key="active.runId + active.nodeId"
              :gate="activeGate!"
              :run="activeRun || undefined"
              :fill-preview="true"
              :unified-preview-budget="true"
              class="min-h-0 flex-1"
              :share-link="isHumanGateInboxItem(active) ? active.shareLink ?? { state: 'none' } : null"
              @resolve="onResolve"
              @react-revised="onReactRevised"
              @open-share="openSharePanel(active)"
            />
            <InboxStartFailedPane v-else-if="startFailedActive" @dismiss="dismissStartFailure" />
            <ReviewShell
              v-else-if="showClarifyReviewShell"
              ref="reviewShellRef"
              :key="active.runId + active.nodeId"
              class="min-h-0 flex-1"
              :sidebar-width="400"
              :storage-key="REVIEW_SHELL_WIDTH_KEY_APPROVAL"
              :host-confirm-flow="false"
            >
              <template #stage>
                <ReactConnectingState
              v-if="activeStarting"
              mode="stage"
              :sandbox-phase="startingSandboxPhase"
            />
                <ArtifactLoadingPane v-else-if="activeRunLoading" message-key="pages.gatesInbox.loadingRun" />
                <ReactArtifactStage
                  v-else
                  :artifacts="activeRun?.artifacts || []"
                  :preview-artifact="activeClarify?.previewArtifact"
                  :run-id="active.runId"
                  :run="activeRun || undefined"
                  :node-id="active.nodeId"
                  :node-type="inboxStageNodeType"
                  :annotatable="clarifyInputActive"
                  :remote-kind="inboxRemoteKind"
                  @pick="onAppPreviewReviewPick"
                  @staged-pick="onAppPreviewStagedPick"
                />
              </template>
              <template #sidebar>
                <ReactConnectingState
              v-if="activeStarting"
              :show-confirm="composerMode === 'review'"
              :sandbox-phase="startingSandboxPhase"
            />
                <ReviewComposer
                  v-else
                  ref="reviewChatRef"
                  :mode="composerMode"
                  :run-id="active.runId"
                  :node-id="clarifyComposerNodeId"
                  :iteration="clarifyComposerIteration"
                  v-model:draft="clarifyDraft"
                  v-model:attachments="clarifyAttachments"
                  v-model:annotations="clarifyAnnotations"
                  :turns="clarifyComposerTurns"
                  :node-type="clarifyComposerNodeType"
                  :seed-human-text="activeHomeSeed?.text"
                  :seed-human-images="activeHomeSeed?.images ?? []"
                  :done="clarifyComposerDone"
                  :active="clarifyInputActive"
                  :confirm-error="clarifyConfirmError"
                  @send="onClarifySend"
                  @retry-last="onClarifyRetryLast"
                  @finish="onClarifyFinish"
                  @cancel="onClarifyCancel"
                  @queue-remove="(itemId) => onClarifyQueueRemove(itemId)"
                  @queue-reorder="onClarifyQueueReorder"
                />
              </template>
            </ReviewShell>
            <ClarifyProductStage
              v-else-if="active.type === 'clarify'"
              :product-nodes="[]"
              :selected-product-id="null"
              :stage-kind="inboxClarifyStageKind"
              :selected-node="null"
              :selected-node-run="null"
              :run="null"
              :loading="activeRunLoading"
              @retry="retryActiveRun"
            />
          </div>
          </template>
          <ConfirmFlowOverlay
            :phase="inboxConfirmFlowPhase"
            :reduce-motion="inboxConfirmFlowReduce"
            :play-token="inboxConfirmFlowToken"
          />
        </div>
      </div>
    </div>

    <!-- Desktop loading∧empty: keep the 320px list rail + a weak detail hint (no EmptyState flash). -->
    <div
      v-else-if="!isMobile && showListSkeleton"
      class="grid min-h-0 flex-1 grid-cols-[320px_1fr] items-stretch gap-4"
      data-testid="inbox-list-skeleton"
    >
      <div class="flex h-full min-h-0 flex-col gap-2 overflow-hidden" aria-hidden="true">
        <div
          v-for="n in SKELETON_CARDS"
          :key="'skel-d-' + n"
          class="flex w-full shrink-0 flex-col gap-2 rounded-lg border border-line bg-surface p-3"
          data-testid="inbox-pending-card-skeleton"
        >
          <div class="flex items-start gap-3">
            <div class="h-9 w-9 shrink-0 bg-elevated animate-pulse" />
            <div class="min-w-0 flex-1 space-y-2">
              <div class="h-3.5 w-2/3 bg-elevated animate-pulse" />
              <div class="h-2.5 w-full bg-elevated animate-pulse" />
            </div>
          </div>
        </div>
      </div>
      <div class="flex h-full min-h-0 min-w-0 flex-col">
        <div class="card flex h-full min-h-0 w-full flex-col overflow-hidden">
          <div class="flex shrink-0 items-center justify-between border-b border-line px-4 py-2.5">
            <span class="text-xs text-txt3">{{ t('pages.gatesInbox.detailPane') }}</span>
          </div>
          <div class="flex min-h-0 flex-1 flex-col items-center justify-center px-6 text-center">
            <p class="text-sm text-txt2">{{ t('pages.gatesInbox.listLoadingHint') }}</p>
          </div>
        </div>
      </div>
    </div>

    <div
      v-else-if="!isMobile && showListError"
      class="card flex min-h-0 flex-1 flex-col items-stretch justify-center overflow-auto p-4"
      data-testid="inbox-list-failed"
    >
      <AppInlineError
        :title="t('common.asyncState.loadFailedTitle')"
        :message="listErrorMessage"
        retry-testid="inbox-list-retry"
        @retry="retryListLoad"
      />
    </div>

    <div v-else-if="!isMobile" class="card flex min-h-0 flex-1 flex-col items-center justify-center overflow-auto">
      <EmptyState
        icon="gate"
        :title="listTotal ? t('common.empty.noPendingGatesForPipeline') : t('common.empty.noPendingGates')"
        :desc="
          listTotal
            ? t('common.empty.noPendingGatesPipelineDesc')
            : t('common.empty.noPendingGatesDesc')
        "
      />
    </div>

    <GateShareLinkPanel
      :open="sharePanelOpen"
      :target="shareTarget"
      :kind="shareTarget ? inboxShareKind(shareTarget) : 'human_gate'"
      @close="sharePanelOpen = false"
      @updated="(st) => shareTarget && patchShareStatus(shareTarget, st)"
      @revoked="
        (st) => {
          if (shareTarget) patchShareStatus(shareTarget, st)
          sharePanelOpen = false
        }
      "
    />
  </div>
</template>

<style scoped>
@keyframes slideDown {
  from {
    opacity: 0;
    transform: translateY(-6px);
  }
}
</style>
