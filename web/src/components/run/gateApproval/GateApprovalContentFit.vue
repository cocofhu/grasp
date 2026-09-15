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
import StructuredArtifactView from '../StructuredArtifactView.vue'
import GateApprovalComposer from './GateApprovalComposer.vue'
import CommentArtifactSidebar from '../CommentArtifactSidebar.vue'

const ctx = useGateApprovalCtx()
const s = ctx.s
const productEditorRef = ctx.productEditorRef
const feedbackChatRef = ctx.feedbackChatRef
const gateStageEl = ctx.gateStageEl
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
    data-testid="content-fit-scroll"
  >
      <ReviewShell
        class="min-h-0 flex-1"
        :mobile="isMobile"
        :sidebar-width="400"
        :storage-key="REVIEW_SHELL_WIDTH_KEY_APPROVAL"
      >
        <template #stage>
          <div class="flex h-full min-h-0 flex-col overflow-hidden">
            <!-- Inbox: budget fills stage so upstream sits on card bottom (no 60vh free-space void). -->
            <div
              :class="
                useUnifiedPreviewBudget
                  ? 'flex min-h-0 flex-1 flex-col overflow-hidden'
                  : 'contents'
              "
              :data-testid="useUnifiedPreviewBudget ? 'content-fit-budget' : undefined"
            >
              <div
                ref="gateStageEl"
                class="rounded-lg border border-line"
                :class="
                  isMobile
                    ? 'scroll-area min-h-0 overflow-x-hidden overflow-y-auto'
                    : 'flex min-h-0 flex-1 flex-col overflow-hidden'
                "
                data-testid="content-fit-preview"
                data-review-annotate-stage
                :style="
                  useUnifiedPreviewBudget
                    ? undefined
                    : { maxHeight: `${CONTENT_FIT_PREVIEW_MAX_VH}vh` }
                "
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
                  :class="isMobile ? undefined : 'min-h-0 flex-1'"
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
                  :fill-parent="!isMobile"
                  :fit-content="false"
                  :max-content-height-vh="
                    isMobile || useUnifiedPreviewBudget ? undefined : CONTENT_FIT_PREVIEW_MAX_VH
                  "
                  :content-height-offset-px="isMobile ? 0 : contentFitChromeOffsetPx"
                  :enlargeable="!isMobile"
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
                  class="flex flex-col items-center justify-center gap-2.5 px-6 py-8 text-center"
                  :class="isMobile ? 'min-h-[200px]' : 'min-h-0 flex-1'"
                  data-testid="content-fit-product-error"
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
                    data-testid="content-fit-product-retry"
                    @click="retryLoadProduct"
                  >
                    {{ t('pages.gateApproval.previewRetry') }}
                  </button>
                </div>
                <HtmlPreview
                  v-else-if="shouldFillPreview"
                  :class="isMobile ? undefined : 'min-h-0 flex-1'"
                  :html="productHtml"
                  :mode="isMobile ? 'inline' : 'default'"
                  :enlargeable="!isMobile"
                  :fill-parent="!isMobile"
                  :fit-content="false"
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
                  v-else-if="shouldFitStructured && productName"
                  class="p-4"
                  :class="isMobile ? undefined : 'min-h-0 flex-1 overflow-y-auto'"
                >
                  <StructuredArtifactView
                    :name="productName"
                    :doc="productDoc"
                    :artifacts="run?.artifacts"
                    :run-id="run?.id"
                    :run-status="run?.status"
                  />
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
          </div>
        </template>
        <template #sidebar>
          <div
            class="flex h-full min-h-0 flex-col overflow-hidden border-t border-line p-3 safe-area-bottom"
            data-testid="content-fit-form"
          >
            <div v-if="gate.form?.length && !shouldHideGateForm" class="mb-3 shrink-0 space-y-3">
              <div v-for="f in gate.form" :key="f.key">
                <label class="label">{{ f.label }} <span v-if="f.required" class="text-err">*</span></label>
                <ParagraphInput
                  v-model:text="formText[f.key]"
                  v-model:images="formImages[f.key]"
                  :text-only="isMobile"
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
              data-testid="content-fit-preview-issues-loading"
            >
              <Icon name="spinner" :size="14" class="animate-spin text-accent" />
              {{ t('pages.gateApproval.loadingPreviewIssues') }}
            </div>
            <div v-else class="flex min-h-0 flex-1 flex-col">
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
              <p v-else-if="canEditProducts" class="mb-2 min-w-0 shrink-0 text-[11px] leading-relaxed text-txt3 [overflow-wrap:anywhere]">
                <b class="font-medium text-txt2">{{ t('pages.clarify.confirmFlow') }}</b>
                {{ t('pages.gateApproval.helpApproveDetail') }}
                <span class="mx-1">·</span>
                <b class="font-medium text-txt2">{{ t('pages.reviewComposer.send') }}</b>
                {{ t('pages.gateApproval.helpReviseDetail') }}
              </p>
              <div
                v-if="previewIssuesError"
                class="mb-2 shrink-0 rounded-md border border-warn/30 bg-warn/10 px-2.5 py-2 text-[11px] text-warn"
                data-testid="content-fit-preview-issues-error"
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
              <!-- CommentPin MVP: 评论|产物 tabs (parallel to PreviewIssue chat below). -->
              <CommentArtifactSidebar
                v-if="isVisualBody"
                class="mb-2 min-h-[200px] max-h-[48%] shrink-0"
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
              <!-- Preview path: unified feedback in sidebar (hot hides built-in submit). -->
              <div
                v-if="usesPreviewIssues && run"
                class="flex min-h-0 flex-1 flex-col"
                data-testid="content-fit-feedback"
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
              <!-- Hot unified actions (no ReviewComposer input): 记入 + 发送 + 确认并流转. -->
              <div
                v-if="canReactRevise && usesPreviewIssues"
                class="mt-auto shrink-0 pt-2"
                data-testid="review-composer-gate"
              >
              
                <GateHotUnifiedActions layout="content-fit" />
            
              </div>
              <!-- Non-preview hot (structured etc.): ReviewComposer review semantics. -->
              <GateApprovalComposer v-else-if="canReactRevise" class="min-h-0 flex-1" />
              <div
                v-else
                class="mt-auto flex shrink-0 flex-col gap-2 pt-2"
              >
                <GateApprovalColdActions layout="content-fit" />
              </div>
            </div>
          </div>
        </template>
      </ReviewShell>
  </div>
</template>
