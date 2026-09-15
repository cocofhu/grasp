<script setup lang="ts">
import { toRefs } from 'vue'
import { useGateApprovalCtx } from './gateApprovalContext'
import Icon from '../../ui/Icon.vue'
import ParagraphInput from '../../ui/ParagraphInput.vue'
import HtmlPreview from '../../ui/HtmlPreview.vue'
import RefreshStrip from '../RefreshStrip.vue'
import UpstreamRequirementContext from '../UpstreamRequirementContext.vue'
import ReviewShell from '../ReviewShell.vue'
import PreviewFeedbackChat from '../PreviewFeedbackChat.vue'
import GateProductEditor from '../GateProductEditor.vue'
import GateHotUnifiedActions from './GateHotUnifiedActions.vue'
import GateApprovalColdActions from './GateApprovalColdActions.vue'
import CommentArtifactSidebar from '../CommentArtifactSidebar.vue'

const ctx = useGateApprovalCtx()
const s = ctx.s
const productEditorRef = ctx.productEditorRef
const feedbackChatRef = ctx.feedbackChatRef
const {
  gate,
  run,
  isMobile,
  formText,
  formImages,
  resolved,
  formError,
  actionSubmitting,
  productDirty,
  savedProductContent,
  savedProductMeta,
  primaryProducts,
  excludedProduces,
  productName,
  canEditProducts,
  isVisualBody,
  isProposalSelect,
  isAppPreview,
  bodyTemplate,
  usesPreviewIssues,
  openPreviewIssueCount,
  previewIssuesLoading,
  previewIssuesError,
  pickedSelector,
  pickedElementImage,
  commentPins,
  commentPinBadges,
  commentPinSelectedId,
  commentArtifactCommitted,
  commentArtifactWriting,
  commentArtifactWriteError,
  annotateDraft,
  proposalsDoc,
  proposalsLoading,
  planDoc,
  planLoading,
  productDoc,
  productHtml,
  productLoading,
  productLoadError,
  productHasSavedContent,
  reviewingIteration,
  previewFromArtifactFallback,
  shouldFillPreview,
  shouldFitStructured,
  shouldFillAppPreview,
  useFillLayout,
  useUnifiedPreviewBudget,
  contentFitChromeOffsetPx,
  CONTENT_FIT_PREVIEW_MAX_VH,
  REVIEW_SHELL_WIDTH_KEY_APPROVAL,
  shouldHideGateForm,
  footerActions,
  isColdSession,
  helpColdText,
  helpReviseDetailNoIssuesText,
  helpReviseWithIssuesText,
  canReactRevise,
  canRecordIssue,
  canSubmitReact,
  showHotPass,
  showHotReject,
  hotRejectAllowEmpty,
  composerRejectLabel,
  composerPassDisabled,
  passAction,
  reactText,
  reactImages,
  reactAnnotations,
  reactSending,
  reactError,
  reactQueued,
  reactThinking,
  reactStreamText,
  reactStreamThought,
  reactInterrupted,
  reactStreamCompletedAt,
} = toRefs(s)
const {
  t,
  isActionDisabled,
  actionPendingLabel,
  actionButtonTitle,
  onProductSaved,
  onProductRefresh,
  retryLoadProduct,
  onHtmlPreviewPick,
  onAppPreviewPick,
  clearHtmlPreviewPick,
  onAnnotateSave,
  onAnnotateSendChat,
  onAnnotateClose,
  onCommentPinSelect,
  onCommentPinDelete,
  onWriteCommentArtifact,
  loadPreviewIssues,
  choose,
  recordFeedbackIssue,
  sendHotReject,
  onComposerPass,
  onComposerReject,
  cancelReactRevise,
  onSidebarAction,
  renderMarkdown,
} = s
</script>

<template>
  <div
    class="flex min-h-0 flex-1 flex-col overflow-hidden"
    data-testid="mobile-fill-remaining"
  >
      <ReviewShell
        class="min-h-0 flex-1"
        mobile
        :sidebar-width="400"
        :drawer-height="320"
        :storage-key="REVIEW_SHELL_WIDTH_KEY_APPROVAL"
      >
        <template #stage>
          <div
            class="flex h-full min-h-0 flex-col overflow-hidden"
            data-testid="mobile-fill-scroll"
          >
            <div
              class="rounded-lg flex min-h-0 flex-1 flex-col overflow-hidden border border-line"
              data-testid="mobile-fill-preview"
            >
              <div
                v-if="reviewingIteration != null"
                class="shrink-0 border-b border-line bg-elevated/60 px-3 py-1.5 text-[11px] text-txt3"
              >
                {{ t('pages.gateApproval.reviewingUpstream', { n: reviewingIteration }) }}
                <span v-if="previewFromArtifactFallback" class="ml-2 text-warn">
                  {{ t('pages.gateApproval.reviewingUpstreamFallback') }}
                </span>
              </div>
              <RefreshStrip v-if="productLoading && productHasSavedContent" />
              <GateProductEditor
                v-if="canEditProducts && run"
                ref="productEditorRef"
                class="min-h-0 flex-1"
                :run-id="run.id"
                :gate-node-id="gate.nodeId"
                :products="primaryProducts"
                :saved-content="savedProductContent"
                :saved-meta="savedProductMeta"
                :artifacts="run.artifacts"
                :run-status="run.status"
                :can-edit="canEditProducts"
                :content-loading="productLoading"
                :load-error="productLoadError"
                :excluded-names="excludedProduces"
                fill-parent
                :enlargeable="false"
                :inspectable="isVisualBody"
                :comment-pins="isVisualBody ? commentPinBadges : []"
                :annotate-draft="isVisualBody ? annotateDraft : null"
                @saved="onProductSaved"
                @dirty-change="productDirty = $event"
                @refresh-request="onProductRefresh"
                @retry-load="retryLoadProduct"
                @pick="onHtmlPreviewPick"
                @pin-select="onCommentPinSelect"
                @annotate-save="onAnnotateSave"
                @annotate-send-chat="onAnnotateSendChat"
                @annotate-close="onAnnotateClose"
              />
              <div
                v-else-if="productLoadError"
                class="flex min-h-0 flex-1 flex-col items-center justify-center gap-2.5 px-6 py-8 text-center"
                data-testid="mobile-fill-product-error"
                role="alert"
              >
                <p class="text-[13px] font-medium text-txt">{{ t('pages.gateApproval.previewLoadFailedTitle') }}</p>
                <p class="max-w-[36ch] text-[12px] text-txt3">
                  {{ t('pages.gateApproval.previewLoadFailedBody') }}
                </p>
                <p class="max-w-[42ch] text-[11px] text-err">{{ productLoadError }}</p>
                <button
                  type="button"
                  class="mt-1 bg-accent px-3.5 py-1.5 text-xs font-medium text-white hover:bg-accent-hover"
                  data-testid="mobile-fill-product-retry"
                  @click="retryLoadProduct"
                >
                  {{ t('pages.gateApproval.previewRetry') }}
                </button>
              </div>
              <HtmlPreview
                v-else-if="shouldFillPreview"
                class="min-h-0 flex-1"
                :html="productHtml"
                mode="inline"
                :enlargeable="false"
                fill-parent
                inspectable
                :comment-pins="commentPinBadges"
                :annotate-draft="annotateDraft"
                @pick="onHtmlPreviewPick"
                @pin-select="onCommentPinSelect"
                @annotate-save="onAnnotateSave"
                @annotate-send-chat="onAnnotateSendChat"
                @annotate-close="onAnnotateClose"
              />
              <div
                v-else
                class="flex min-h-0 flex-1 items-center justify-center text-[12px] text-txt3"
                data-testid="mobile-fill-product-loading"
              >
                <Icon name="spinner" :size="20" class="mr-2 animate-spin text-accent" />
                {{ t('pages.gateApproval.loadingArtifact') }}
              </div>
            </div>
            <UpstreamRequirementContext
              :artifacts="run?.artifacts"
              :run-id="run?.id"
              :run-status="run?.status"
              :product-name="productName"
              :body-template="bodyTemplate"
            />
          </div>
        </template>
        <template #sidebar>
          <div
            class="flex h-full min-h-0 flex-col overflow-hidden p-3 safe-area-bottom"
            data-testid="mobile-fill-sticky-actions"
          >
            <!-- Cold gate.form only (hidden when preview feedback / hot unified input owns the draft). -->
            <div v-if="gate.form?.length && !shouldHideGateForm" class="mb-3 shrink-0 space-y-3">
              <div v-for="f in gate.form" :key="f.key">
                <label class="label">{{ f.label }} <span v-if="f.required" class="text-err">*</span></label>
                <ParagraphInput
                  v-model:text="formText[f.key]"
                  v-model:images="formImages[f.key]"
                  text-only
                  :placeholder="t('pages.gateApproval.formPlaceholder', { label: f.label })"
                />
              </div>
            </div>
            <div v-if="formError" class="mb-2 shrink-0 rounded-md border border-err/30 bg-err/10 px-3 py-2 text-xs text-err" role="alert">
              {{ formError }}
            </div>
            <div v-if="resolved && !actionSubmitting" class="rounded-md border border-ok/30 bg-ok/10 px-3 py-2 text-xs text-ok">
              {{ t('pages.gateApproval.submitted', { label: gate.actions.find((a) => a.id === resolved)?.label }) }}
            </div>
            <div
              v-else-if="usesPreviewIssues && previewIssuesLoading"
              class="flex items-center justify-center gap-2 py-2 text-xs text-txt3"
              data-testid="mobile-fill-preview-issues-loading"
            >
              <Icon name="spinner" :size="14" class="animate-spin text-accent" />
              {{ t('pages.gateApproval.loadingPreviewIssues') }}
            </div>
            <div v-else class="flex min-h-0 flex-1 flex-col">
              <!--
                Help + pins + review history scroll on their own so the decision
                bar below stays pinned inside the drawer on short viewports.
              -->
              <div class="scroll-area flex min-h-0 flex-1 flex-col overflow-y-auto">
                <p
                  v-if="isColdSession"
                  class="mb-2 min-w-0 shrink-0 text-[11px] leading-relaxed text-txt3 [overflow-wrap:anywhere]"
                  data-testid="gate-cold-help"
                >
                  {{ helpColdText }}
                </p>
                <p
                  v-else-if="usesPreviewIssues && openPreviewIssueCount === 0"
                  class="mb-2 min-w-0 shrink-0 text-[11px] leading-relaxed text-txt3 [overflow-wrap:anywhere]"
                >
                  <b class="font-medium text-txt2">{{ t('pages.clarify.confirmFlow') }}</b>
                  {{ t('pages.gateApproval.helpApproveDetail') }}
                  <span class="mx-1">·</span>
                  <b class="font-medium text-txt2">{{ t('pages.reviewComposer.send') }}</b>
                  {{ helpReviseDetailNoIssuesText }}
                </p>
                <p
                  v-else-if="usesPreviewIssues && openPreviewIssueCount >= 1"
                  class="mb-2 min-w-0 shrink-0 text-[11px] leading-relaxed text-txt3 [overflow-wrap:anywhere]"
                >
                  <template v-if="canReactRevise">
                    <b class="font-medium text-txt2">{{ t('pages.reviewComposer.send') }}</b>
                    {{ t('pages.gateApproval.helpReviseWithIssuesDetail') }}
                    <span class="mx-1">·</span>
                    {{ t('pages.reviewComposer.openIssuesConfirmHint') }}
                  </template>
                  <template v-else>{{ helpReviseWithIssuesText }}</template>
                </p>
                <div
                  v-if="previewIssuesError"
                  class="mb-2 shrink-0 rounded-md border border-warn/30 bg-warn/10 px-2.5 py-2 text-[11px] text-warn"
                  data-testid="mobile-fill-preview-issues-error"
                >
                  {{ t('pages.gateApproval.loadPreviewIssuesFailed') }}
                  <button
                    type="button"
                    class="ml-2 underline"
                    @click="loadPreviewIssues()"
                  >
                    {{ t('pages.gateApproval.previewRetry') }}
                  </button>
                </div>
                <!-- Nothing is actionable without pins, and the drawer needs the rows. -->
                <CommentArtifactSidebar
                  v-if="isVisualBody && (commentPins.length > 0 || commentArtifactCommitted)"
                  class="mb-2 max-h-[36%] shrink-0"
                  :pins="commentPins"
                  :selected-id="commentPinSelectedId"
                  :artifact-committed="commentArtifactCommitted"
                  :writing="commentArtifactWriting"
                  :write-error="commentArtifactWriteError"
                  @select="onCommentPinSelect"
                  @edit="onCommentPinSelect"
                  @delete="onCommentPinDelete"
                  @write="onWriteCommentArtifact"
                />
                <!-- min-h floor: flex-1 alone collapses to 0 once the composer shell grows. -->
                <div
                  v-if="usesPreviewIssues && run"
                  class="flex min-h-[52px] flex-1 flex-col"
                  data-testid="mobile-fill-feedback"
                >
                  <PreviewFeedbackChat
                    ref="feedbackChatRef"
                    class="min-h-0 flex-1"
                    :run-id="run.id"
                    :node-id="gate.nodeId"
                    :selector="pickedSelector"
                    :element-image="pickedElementImage"
                    v-model:text="reactText"
                    v-model:images="reactImages"
                    copy-variant="review"
                    fill-sidebar
                    :hide-submit="canReactRevise"
                    @clear-selector="clearHtmlPreviewPick"
                    @issues-changed="loadPreviewIssues()"
                  />
                </div>
              </div>
              <div
                v-if="canReactRevise && usesPreviewIssues"
                class="mt-2 shrink-0"
                data-testid="review-composer-gate"
              >
                <GateHotUnifiedActions layout="mobile" />
              </div>
              <div v-else class="mt-2 flex shrink-0 flex-col gap-2">
                <GateApprovalColdActions layout="mobile" />
              </div>
            </div>
          </div>
        </template>
      </ReviewShell>
  </div>
</template>
