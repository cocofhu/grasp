<script setup lang="ts">
import Icon from '../ui/Icon.vue'
import ChatImageThumb from '../ui/ChatImageThumb.vue'
import ChatImagePreviewModal from '../ui/ChatImagePreviewModal.vue'
import ComposerShell from './ComposerShell.vue'
import ClarifyDemoFrame from './ClarifyDemoFrame.vue'
import ThoughtSummaryStatus from './ThoughtSummaryStatus.vue'
import AnnotationChip from './AnnotationChip.vue'
import PendingSendQueuePanel from './PendingSendQueuePanel.vue'
import type {
  ClarifyTurn,
  ClarifyImage,
  ReactAnnotation,
} from '@/lib/shared/types'
import { isImageAttachment, attachmentDisplayName } from '@/lib/shared/attachments'
import { imgSrc } from '@/lib/shared/compositeText'
import { useClarifyChat } from '@/lib/inbox/useClarifyChat'

const props = withDefaults(
  defineProps<{
    runId: string
    nodeId: string
    iteration: number
    turns?: ClarifyTurn[] | null
    done: boolean
    active?: boolean
    reviewMode?: boolean
    annotateEnabled?: boolean
    hideFinish?: boolean
    /** Session history plus confirm only; input/send stay unmounted. */
    coldSession?: boolean
    /** Host-level lock for the final confirmation action. */
    finishDisabled?: boolean
    /** Public adapters may expose confirm for a live react node. */
    forceConfirmFlow?: boolean
    sendLabel?: string
    confirmError?: string | null
    nodeType?: string
    seedHumanText?: string
    seedHumanImages?: ClarifyImage[]
  }>(),
  {
    active: true,
    iteration: 1,
    reviewMode: false,
    annotateEnabled: false,
    hideFinish: false,
    coldSession: false,
    finishDisabled: false,
    forceConfirmFlow: false,
    confirmError: null,
    nodeType: '',
    turns: () => [],
    seedHumanText: '',
    seedHumanImages: () => [],
  },
)

const emit = defineEmits<{
  (e: 'send', text: string, images: ClarifyImage[], annotations: ReactAnnotation[]): void
  (e: 'retry-last'): void
  (e: 'finish'): void
  (e: 'cancel'): void
  (e: 'queue-remove', itemId: string | undefined, index: number): void
  (e: 'queue-reorder', itemIds: string[]): void
}>()

const draft = defineModel<string>('draft', { default: '' })
const attachments = defineModel<ClarifyImage[]>('attachments', { default: () => [] })
const annotations = defineModel<ReactAnnotation[]>('annotations', { default: () => [] })

const chat = useClarifyChat(props, emit, { draft, attachments, annotations })

defineExpose({
  applyReviewFrame: chat.applyReviewFrame,
  applyAcpEvents: chat.applyAcpEvents,
  applyQueueState: chat.applyQueueState,
  cancelReview: chat.cancelReview,
  discardLastQueued: chat.discardLastQueued,
  isSessionBusy: chat.isSessionBusy,
  reorderQueuedItems: chat.reorderQueuedItems,
  playConfirmCeremony: chat.playConfirmCeremony,
})

const {
  humanMatchesSeed,
  prependSeedHuman,
  isNearBottom,
  onScrollerScroll,
  scrollBottom,
  enterStickSequence,
  onUnreadFabClick,
  autoGrow,
  onTextInput,
  textHasChoicePrefix,
  stripChoicePrefix,
  isChoiceReply,
  latestQuestionTurnIndex,
  hasHumanReplyAfter,
  imagePreviewLabel,
  openImagePreview,
  turnsSemanticKey,
  addFiles,
  onPickFiles,
  onPaste,
  removeAttachment,
  sendMessage,
  sendFromComposer,
  onComposerKeydown,
  removeAnnotation,
  isActiveTurn,
  isActiveFormTurn,
  isSelected,
  toggle,
  answered,
  autoPickId,
  applyRecommended,
  submitRecommended,
  prevCard,
  nextCard,
  pick,
  submitChoices,
  parseChoiceSummary,
  submitForms,
  parseFormSummary,
  formFieldKey,
  fieldWhy,
  formInputType,
  choiceRowsForAgentTurn,
  selectedLabelsForQuestion,
  selectedDemoForInteractive,
  send,
  finishEarly,
  playConfirmCeremony,
  showDoneChrome,
  confirmFlowPlaying,
  cancelReview,
  discardLastQueued,
  forceAuthoritativeIdle,
  settleAfterTurnEnd,
  applyQueueState,
  applyReviewFrame,
  applyAcpEvents,
  agentHasMessage,
  agentThoughtDisplay,
  isThoughtOpen,
  onThoughtToggle,
  showTurnCompleted,
  isRetryableFailedAgent,
  emptyFailDisplayText,
  showFailRetry,
  failRetryDisabled,
  retryLastFailed,
  showAnnotationChips,
  persistedTurns,
  inputPlaceholder,
  thinking,
  validating,
  queued,
  liveTurns,
  showApproveEmptyHint,
  useConfirmFlowAction,
  seedHumanTurn,
  liveAgentIdx,
  liveStreamHtml,
  streamPreview,
  unsubStream,
  liveThoughtText,
  thoughtPreview,
  unsubThought,
  syncReveal,
  messageReveal,
  thoughtReveal,
  thoughtOpenOverride,
  scroller,
  fileInput,
  textareaRef,
  overflowScroll,
  composing,
  STICK_THRESHOLD,
  stickToBottom,
  unreadCount,
  showUnreadFab,
  unreadFabLabel,
  sessionBusy,
  AUTO_GROW_MIN,
  AUTO_GROW_MAX,
  LEGACY_CHOICE_PREFIX,
  choicePrefix,
  latestQuestionIdx,
  latestQuestions,
  latestQuestionAnswered,
  displayTurns,
  attachNotice,
  sel,
  other,
  otherChecked,
  isOtherSelected,
  toggleOther,
  activeQuestions,
  someAnswered,
  hasRecommended,
  activeForms,
  formValues,
  formNotice,
  formCanSubmit,
  step,
  curQuestion,
  isFirstCard,
  isLastCard,
  confirmDisabled,
  translate,
  locale,
  imagePreview,
  openChatImagePreview,
  closeImagePreview,
  renderMarkdown,
  relTime,
  demoGridColsClass,
  demoOptionsOf,
  selectedDemoOption,
  useSideBySide,
  queueNotice,
  queueToast,
  cancelQueuedItem,
  reorderQueuedItems,
  editQueuedItem,
} = chat
</script>

<template>
  <div class="flex h-full flex-col" data-review-composer>
    <div class="relative flex min-h-0 flex-1 flex-col">
    <div
      ref="scroller"
      class="scroll-area flex-1 space-y-3 overflow-y-auto p-4 pb-14"
      data-testid="clarify-scroller"
      @scroll.passive="onScrollerScroll"
    >
      <div class="flex items-center gap-2 text-[11px] text-txt3">
        <Icon name="chat" :size="13" />
        {{ translate('pages.clarify.header', { n: displayTurns.length }) }}
      </div>
      <p
        v-if="showApproveEmptyHint"
        class="text-[12px] leading-relaxed text-txt2"
        data-testid="clarify-approve-empty-hint"
      >
        {{ translate('pages.clarify.approveEmptyHint') }}
      </p>
      <div v-for="(t, i) in displayTurns" :key="i" class="flex gap-2.5" :class="t.role === 'human' ? 'flex-row-reverse' : ''">
        <div
          class="flex h-7 w-7 shrink-0 items-center justify-center rounded-full text-[11px] font-semibold"
          :class="t.role === 'agent' ? 'bg-n-clarify/15 text-n-clarify' : 'bg-accent-dim text-accent-2'"
        >
          <Icon v-if="t.role === 'agent'" name="robot" :size="15" />
          <span v-else>{{ translate('pages.clarify.me') }}</span>
        </div>
        <div class="min-w-0 max-w-[80%]">
          <div v-if="t.images && t.images.length" class="mb-1.5 flex flex-wrap gap-1.5" :class="t.role === 'human' ? 'justify-end' : ''">
            <!-- human history: images → lightbox; non-images → filename chip -->
            <template v-if="t.role === 'human'">
              <template v-for="(im, ii) in t.images" :key="ii">
                <ChatImageThumb
                  v-if="isImageAttachment(im)"
                  mode="previewable"
                  size="md"
                  thumb-class="rounded-md"
                  :src="imgSrc(im)"
                  :label="imagePreviewLabel(t.images, ii)"
                  test-id="clarify-history-image-thumb"
                  @preview="openImagePreview(t.images!, ii)"
                />
                <div
                  v-else
                  class="rounded-md flex max-w-[200px] items-center gap-2 border border-line bg-elevated px-2 py-1.5"
                  data-testid="clarify-history-file-chip"
                  :title="imagePreviewLabel(t.images, ii)"
                >
                  <span class="shrink-0 text-[10px] font-medium uppercase tracking-wide text-info">DOC</span>
                  <span class="min-w-0 truncate text-[12px] text-txt">{{ imagePreviewLabel(t.images, ii) }}</span>
                </div>
              </template>
            </template>
            <!-- agent history: locked thumbs / filename chips (FR-f7) -->
            <template v-else>
              <template v-for="(im, ii) in t.images" :key="ii">
                <ChatImageThumb
                  v-if="isImageAttachment(im)"
                  mode="locked"
                  size="md"
                  thumb-class="rounded-md"
                  :src="imgSrc(im)"
                  :label="imagePreviewLabel(t.images, ii)"
                  test-id="clarify-agent-image-thumb"
                />
                <div
                  v-else
                  class="rounded-md flex max-w-[200px] items-center gap-2 border border-line bg-elevated px-2 py-1.5"
                  data-testid="clarify-agent-file-chip"
                >
                  <span class="shrink-0 text-[10px] font-medium uppercase tracking-wide text-info">DOC</span>
                  <span class="min-w-0 truncate text-[12px] text-txt">{{ imagePreviewLabel(t.images, ii) }}</span>
                </div>
              </template>
            </template>
          </div>
          <!-- annotation chips attached to this human review turn -->
          <div v-if="t.role === 'human' && t.annotations && t.annotations.length" class="mb-1.5 flex flex-wrap gap-1.5 justify-end">
            <AnnotationChip
              v-for="(a, ai) in t.annotations"
              :key="ai"
              :ann="a"
            />
          </div>
          <!-- submitted choice summary → structured cards (min-w-0 + wrap: long answers stay in bubble) -->
          <div
            v-if="t.role === 'human' && parseChoiceSummary(t.text)"
            class="min-w-0 max-w-full rounded-lg border border-accent/30 bg-accent-dim/60 px-3 py-2"
            data-testid="clarify-choice-summary"
          >
            <div class="mb-2 flex items-center gap-1.5 text-[11px] font-medium text-txt2">
              <Icon name="check" :size="12" class="text-accent" /> {{ translate('pages.clarify.myChoice') }}
            </div>
            <div class="min-w-0 max-w-full space-y-1.5">
              <div
                v-for="(row, ri) in parseChoiceSummary(t.text)!"
                :key="ri"
                class="min-w-0 max-w-full rounded-md border border-line bg-surface/70 px-2.5 py-1.5"
                data-testid="clarify-choice-row"
              >
                <div class="mb-1 min-w-0 max-w-full break-words text-[11px] leading-snug text-txt3 [overflow-wrap:anywhere]">{{ row.q }}</div>
                <div class="flex min-w-0 max-w-full flex-wrap gap-1">
                  <span
                    v-for="(a, ai) in row.answers"
                    :key="ai"
                    class="min-w-0 max-w-full break-words rounded border border-accent/30 bg-accent/10 px-1.5 py-0.5 text-[12px] text-txt [overflow-wrap:anywhere]"
                    data-testid="clarify-choice-answer"
                  >{{ a }}</span>
                </div>
              </div>
            </div>
          </div>
          <!-- submitted form summary → structured cards (plaintext values, including passwords) -->
          <div
            v-else-if="t.role === 'human' && parseFormSummary(t.text)"
            class="min-w-0 max-w-full rounded-lg border border-n-ci/30 bg-n-ci/5 px-3 py-2"
            data-testid="clarify-form-summary"
          >
            <div class="mb-2 flex items-center gap-1.5 text-[11px] font-medium text-txt2">
              <Icon name="ci" :size="12" class="text-n-ci" /> {{ translate('pages.clarify.myForm') }}
            </div>
            <div class="min-w-0 max-w-full space-y-1.5">
              <div
                v-for="(row, ri) in parseFormSummary(t.text)!"
                :key="ri"
                class="min-w-0 max-w-full rounded-md border border-line bg-surface/70 px-2.5 py-1.5"
                data-testid="clarify-form-row"
              >
                <div class="mb-1 min-w-0 max-w-full break-words text-[11px] leading-snug text-txt3 [overflow-wrap:anywhere]">{{ row.label }}</div>
                <div
                  class="min-w-0 max-w-full break-words font-mono text-[12px] text-txt [overflow-wrap:anywhere] whitespace-pre-wrap"
                  data-testid="clarify-form-value"
                >{{ row.value }}</div>
              </div>
            </div>
          </div>
          <!-- Agent busy / thought / message (Demo: four-phase + restrained done) -->
          <template v-else-if="t.role === 'agent'">
            <!-- Waiting first token: dots + 思考中… inside bubble -->
            <div
              v-if="t.streaming && !t.thought && !agentHasMessage(t)"
              class="inline-flex items-center gap-2.5 rounded-lg border border-line bg-elevated px-3 py-2 text-[13px] text-txt3"
              data-testid="clarify-busy-placeholder"
            >
              <span class="typing-dots" aria-hidden="true"><i /><i /><i /></span>
              <span>{{ translate('pages.clarify.thinkingBusy') }}</span>
            </div>
            <!-- Status: 思考中… (has thought, no message yet) -->
            <div
              v-else-if="t.streaming && t.thought && !agentHasMessage(t)"
              class="mb-1.5 text-[12px] font-normal text-txt3"
              data-testid="clarify-busy-status"
            >
              {{ translate('pages.clarify.thinkingBusy') }}
            </div>
            <!-- Status: 输出中 (shimmer) while streaming message -->
            <div
              v-else-if="t.streaming && agentHasMessage(t)"
              class="clarify-outputting mb-1.5 text-[12px] font-normal"
              data-testid="clarify-busy-status"
            >
              {{ translate('pages.clarify.outputting') }}
            </div>
            <!-- Thought: open while thought-only; default collapsed once message starts -->
            <details
              v-if="t.thought"
              class="mb-2 w-full rounded-md border border-line bg-base/60 text-[11.5px] text-txt3"
              data-testid="clarify-thought"
              :open="isThoughtOpen(i, t)"
              @toggle="onThoughtToggle(i, $event)"
            >
              <summary
                class="flex cursor-pointer select-none items-center gap-1.5 px-2.5 py-1.5 text-txt3 hover:text-txt2"
                data-testid="clarify-thought-summary"
              >
                <ThoughtSummaryStatus
                  :busy="!!t.streaming"
                  :completed="showTurnCompleted(t)"
                  :interrupted="!!t.interrupted"
                />
              </summary>
              <div class="whitespace-pre-wrap break-words border-t border-dashed border-line px-2.5 pb-2 pt-1.5 font-mono leading-5 [overflow-wrap:anywhere]">{{ agentThoughtDisplay(t, i) }}</div>
            </details>
            <!-- Message body + streaming caret -->
            <div
              v-if="agentHasMessage(t) && !isRetryableFailedAgent(t)"
              class="md rounded-lg border border-line bg-elevated px-3 py-2 text-[13px] leading-relaxed text-txt"
              data-testid="clarify-agent-message"
            >
              <span
                v-html="t.streaming ? liveStreamHtml : renderMarkdown(t.text)"
              /><span
                v-if="t.streaming"
                class="clarify-stream-caret"
                data-testid="clarify-stream-caret"
                aria-hidden="true"
              />
            </div>
            <!-- Empty / failure card + cover-retry (plan g1.1 / g1.2) -->
            <div
              v-else-if="isRetryableFailedAgent(t)"
              class="rounded-lg flex max-w-full flex-col gap-2 border border-err/35 bg-err/10 px-3 py-2.5"
              role="alert"
              data-testid="clarify-empty-fail"
            >
              <div class="flex items-start gap-2">
                <Icon name="alert" :size="16" class="mt-0.5 shrink-0 text-err" />
                <div class="min-w-0">
                  <div class="text-[13px] font-semibold text-err">
                    {{ translate('pages.clarify.emptyFailTitle') }}
                  </div>
                  <div
                    class="mt-0.5 break-words text-xs text-txt2 [overflow-wrap:anywhere]"
                    data-testid="clarify-empty-fail-desc"
                  >
                    {{
                      emptyFailDisplayText(
                        t,
                        translate('pages.clarify.emptyFailDesc'),
                      )
                    }}
                  </div>
                </div>
              </div>
              <div v-if="showFailRetry(t, i)">
                <button
                  type="button"
                  class="rounded-md border border-err/40 bg-transparent px-2.5 py-1 text-xs text-err hover:bg-err/15 disabled:opacity-50"
                  :disabled="failRetryDisabled"
                  data-testid="clarify-empty-fail-retry"
                  @click="retryLastFailed"
                >
                  {{ translate('pages.clarify.retry') }}
                </button>
              </div>
            </div>
            <!-- Restrained completion footnote (Demo); never for interrupted/error -->
            <div
              v-if="showTurnCompleted(t)"
              class="mt-1.5 flex items-center justify-between gap-2 text-[11px] text-txt3"
              data-testid="clarify-turn-completed"
            >
              <span class="text-txt2">{{ translate('pages.clarify.turnCompleted') }}</span>
              <span>{{ relTime(t.at) }}</span>
            </div>
          </template>
          <!-- Human free-text bubble (agent branch handled above; role narrowed to human) -->
          <div
            v-else-if="t.text"
            class="md rounded-lg border border-accent/30 bg-accent-dim/60 px-3 py-2 text-[13px] leading-relaxed text-txt"
            v-html="renderMarkdown(t.text)"
          />

          <!-- Structured choice questions (ask_question). The latest agent turn
               shows an interactive card deck (one question per card); earlier
               turns render read-only for context. -->
          <template v-if="t.role === 'agent' && t.questions && t.questions.length">
            <!-- interactive card deck -->
            <div v-if="isActiveTurn(i) && curQuestion" class="mt-2">
              <div>
                <div class="relative rounded-xl border border-n-clarify/25 bg-n-clarify/5 p-3">
                  <!-- progress -->
                  <div v-if="activeQuestions.length > 1" class="mb-2.5 flex items-center justify-between">
                    <div class="flex items-center gap-1">
                      <span
                        v-for="(q, qi) in activeQuestions"
                        :key="qi"
                        class="h-1.5 rounded-full transition-all"
                        :class="qi === step ? 'w-4 bg-n-clarify' : answered(q) ? 'w-1.5 bg-n-clarify/60' : 'w-1.5 bg-line-strong'"
                      />
                    </div>
                    <span class="text-[11px] text-txt3">{{ step + 1 }} / {{ activeQuestions.length }}</span>
                  </div>

                  <Transition name="deck" mode="out-in">
                    <div :key="step">
                      <div class="mb-2 flex items-center gap-1.5 text-[13px] font-medium text-txt">
                        <Icon name="chat" :size="13" class="shrink-0 text-n-clarify" />
                        <span
                          class="min-w-0 flex-1 break-words [overflow-wrap:anywhere]"
                          data-testid="clarify-question-prompt"
                        >{{ curQuestion.prompt }}</span>
                        <span class="ml-auto shrink-0 rounded border border-line px-1.5 py-0.5 text-[10px] font-normal text-txt3">{{ curQuestion.allowMultiple ? translate('pages.clarify.multiple') : translate('pages.clarify.single') }}</span>
                      </div>
                      <div class="space-y-1.5">
                        <button
                          v-for="o in curQuestion.options"
                          :key="o.id"
                          type="button"
                          class="flex w-full items-center gap-2 rounded-lg border px-2.5 py-2 text-left text-[12px] transition-colors"
                          :class="isSelected(curQuestion.id, o.id) ? 'border-accent bg-accent-dim/60 text-txt' : 'border-line bg-surface text-txt2 hover:border-line-strong'"
                          data-testid="clarify-option-btn"
                          @click="pick(curQuestion, o.id)"
                        >
                          <span
                            class="rounded-lg flex h-4 w-4 shrink-0 items-center justify-center border"
                            :class="[
                              curQuestion.allowMultiple ? 'rounded' : 'rounded-full',
                              isSelected(curQuestion.id, o.id) ? 'border-accent bg-accent text-white' : 'border-line-strong',
                            ]"
                          >
                            <Icon v-if="isSelected(curQuestion.id, o.id)" name="check" :size="10" />
                          </span>
                          <span
                            class="min-w-0 flex-1 break-words [overflow-wrap:anywhere]"
                            data-testid="clarify-option-label"
                          >{{ o.label }}</span>
                          <span
                            v-if="o.recommended"
                            class="ml-auto shrink-0 rounded-full bg-accent/15 px-1.5 py-0.5 text-[10px] font-medium text-accent-2"
                          >{{ translate('pages.clarify.recommended') }}</span>
                        </button>
                      </div>
                      <div
                        class="mt-1.5 flex w-full items-center gap-2 rounded-lg border px-2.5 py-2 text-left text-[12px] transition-colors"
                        :class="isOtherSelected(curQuestion.id) ? 'border-accent bg-accent-dim/60 text-txt' : 'border-line bg-surface text-txt2'"
                        data-testid="clarify-other-row"
                      >
                        <button
                          type="button"
                          class="rounded-lg flex h-4 w-4 shrink-0 items-center justify-center border"
                          :class="[
                            curQuestion.allowMultiple ? 'rounded' : 'rounded-full',
                            isOtherSelected(curQuestion.id) ? 'border-accent bg-accent text-white' : 'border-line-strong',
                          ]"
                          data-testid="clarify-other-checkbox"
                          :aria-label="translate('pages.clarify.otherPlaceholder')"
                          @click="toggleOther(curQuestion)"
                        >
                          <Icon v-if="isOtherSelected(curQuestion.id)" name="check" :size="10" />
                        </button>
                        <input
                          v-model="other[curQuestion.id]"
                          type="text"
                          class="input min-h-0 min-w-0 flex-1 border-0 bg-transparent p-0 text-[12px] shadow-none focus:border-transparent focus:ring-0 [overflow-wrap:anywhere]"
                          :placeholder="translate('pages.clarify.otherPlaceholder')"
                          data-testid="clarify-other-input"
                        />
                      </div>

                      <!-- Demo previews (options with demoHtml) -->
                      <div v-if="demoOptionsOf(curQuestion).length" class="mt-2.5">
                        <div
                          v-if="useSideBySide(demoOptionsOf(curQuestion))"
                          class="grid gap-2.5"
                          :class="demoGridColsClass(demoOptionsOf(curQuestion).length)"
                        >
                          <ClarifyDemoFrame
                            v-for="o in demoOptionsOf(curQuestion)"
                            :key="o.id"
                            :label="o.label"
                            :html="o.demoHtml!"
                            :highlighted="isSelected(curQuestion.id, o.id)"
                          />
                        </div>
                        <ClarifyDemoFrame
                          v-else-if="selectedDemoForInteractive(curQuestion)"
                          :label="selectedDemoForInteractive(curQuestion)!.label"
                          :html="selectedDemoForInteractive(curQuestion)!.demoHtml!"
                          highlighted
                        />
                      </div>
                    </div>
                  </Transition>

                  <!-- actions -->
                  <div class="mt-3 flex items-center justify-between">
                    <button
                      v-if="!isFirstCard"
                      class="inline-flex items-center gap-0.5 rounded-md px-2 py-1 text-[12px] text-txt2 hover:text-txt"
                      @click="prevCard"
                    >
                      <Icon name="arrow-left" :size="13" /> {{ translate('pages.clarify.prev') }}
                    </button>
                    <span v-else />

                    <button
                      v-if="!isLastCard"
                      class="inline-flex items-center gap-0.5 rounded-md bg-accent px-3 py-1.5 text-[12px] font-medium text-white hover:bg-accent-hover"
                      @click="nextCard"
                    >
                      {{ translate('pages.clarify.next') }} <Icon name="chevron-right" :size="13" />
                    </button>
                    <div v-else class="flex flex-wrap items-center gap-2">
                      <button
                        v-if="hasRecommended"
                        class="inline-flex items-center gap-1 rounded-md border border-accent/40 bg-accent/10 px-2.5 py-1.5 text-[12px] font-medium text-accent-2 hover:bg-accent/20 disabled:opacity-50"
                        :disabled="thinking"
                        :title="translate('pages.clarify.applyRecommendedTitle')"
                        @click="submitRecommended"
                      >
                        <Icon name="check" :size="12" /> {{ translate('pages.clarify.applyRecommended') }}
                      </button>
                      <button
                        class="inline-flex items-center gap-1 rounded-md bg-accent px-3 py-1.5 text-[12px] font-medium text-white hover:bg-accent-hover disabled:opacity-50"
                        :disabled="!someAnswered || thinking"
                        @click="submitChoices"
                      >
                        <Icon name="send" :size="12" /> {{ translate('pages.clarify.submitChoices') }}
                      </button>
                    </div>
                  </div>
                </div>
              </div>
            </div>

            <!-- read-only history: show demo previews when demoHtml present -->
            <div v-else class="mt-2 rounded-lg border border-n-clarify/20 bg-n-clarify/5 px-3 py-2">
              <div class="mb-1.5 flex items-center gap-1.5 text-[11px] font-medium text-txt3">
                <Icon name="chat" :size="12" class="text-n-clarify" />
                {{ translate('pages.clarify.questionsThisRound', { n: t.questions.length }) }}
                <span class="ml-1 inline-flex items-center gap-1 rounded border border-line px-1.5 py-0.5 text-[10px] font-normal text-txt3">
                  {{ translate('pages.clarify.readonly') }}
                </span>
              </div>
              <div
                v-for="(q, qi) in t.questions"
                :key="qi"
                class="mb-3 last:mb-0"
              >
                <div class="mb-1.5 min-w-0 max-w-full text-[12px] leading-snug text-txt2 [overflow-wrap:anywhere]">
                  <span class="text-txt3">{{ qi + 1 }}.</span> {{ q.prompt }}
                  <span
                    v-for="o in q.options.filter((op) => op.recommended)"
                    :key="o.id"
                    class="ml-1 inline-flex max-w-full min-w-0 break-words rounded-full bg-accent/15 px-1.5 py-0.5 text-[10px] font-medium text-accent-2 [overflow-wrap:anywhere]"
                    data-testid="clarify-recommended-chip"
                  >{{ translate('pages.clarify.recommendedLabel', { label: o.label }) }}</span>
                </div>
                <div v-if="demoOptionsOf(q).length" class="mt-1.5">
                  <div
                    v-if="useSideBySide(demoOptionsOf(q))"
                    class="grid gap-2.5"
                    :class="demoGridColsClass(demoOptionsOf(q).length)"
                  >
                    <ClarifyDemoFrame
                      v-for="o in demoOptionsOf(q)"
                      :key="o.id"
                      :label="o.label"
                      :html="o.demoHtml!"
                      :highlighted="selectedLabelsForQuestion(q, choiceRowsForAgentTurn(i)).includes(o.label)"
                      :selected="selectedLabelsForQuestion(q, choiceRowsForAgentTurn(i)).includes(o.label)"
                    />
                  </div>
                  <ClarifyDemoFrame
                    v-else-if="selectedDemoOption(q, selectedLabelsForQuestion(q, choiceRowsForAgentTurn(i)))"
                    :label="selectedDemoOption(q, selectedLabelsForQuestion(q, choiceRowsForAgentTurn(i)))!.label"
                    :html="selectedDemoOption(q, selectedLabelsForQuestion(q, choiceRowsForAgentTurn(i)))!.demoHtml!"
                    highlighted
                    selected
                  />
                </div>
              </div>
            </div>
          </template>

          <!-- Structured forms (ask_form / preflight). Latest unanswered turn is interactive;
               earlier turns render read-only. Label only (no internal name); hover shows why. -->
          <template v-if="t.role === 'agent' && t.forms && t.forms.length">
            <div v-if="isActiveFormTurn(i)" class="mt-2" data-testid="clarify-form-card">
              <div class="relative rounded-xl border border-n-ci/25 bg-n-ci/5 p-3">
                <div
                  v-for="(form, fi) in activeForms"
                  :key="fi"
                  class="mb-3 last:mb-0"
                >
                  <div v-if="form.title" class="mb-2 flex items-center gap-1.5 text-[13px] font-medium text-txt">
                    <Icon name="ci" :size="13" class="shrink-0 text-n-ci" />
                    <span>{{ form.title }}</span>
                  </div>
                  <div class="space-y-2.5">
                    <label
                      v-for="f in form.fields"
                      :key="f.name"
                      class="block"
                      data-testid="clarify-form-field"
                    >
                      <span
                        class="mb-1 block text-[12px] text-txt2"
                        :title="fieldWhy(f) || undefined"
                      >{{ f.label }}</span>
                      <input
                        v-model="formValues[formFieldKey(fi, f.name)]"
                        :type="formInputType(f)"
                        class="input h-[34px] w-full rounded-lg border border-line bg-surface px-2.5 text-[12px] text-txt placeholder:text-txt3 focus:border-n-ci/50 focus:ring-0"
                        :placeholder="f.placeholder || ''"
                        autocomplete="off"
                        data-testid="clarify-form-input"
                      />
                    </label>
                  </div>
                </div>
                <div v-if="formNotice" class="mt-2 text-[11px] text-warn" data-testid="clarify-form-notice">
                  {{ formNotice }}
                </div>
                <div class="mt-3 flex justify-end">
                  <button
                    type="button"
                    class="inline-flex items-center gap-1 rounded-md bg-accent px-3 py-1.5 text-[12px] font-medium text-white hover:bg-accent-hover disabled:opacity-50"
                    :disabled="!formCanSubmit"
                    data-testid="clarify-form-submit"
                    @click="submitForms"
                  >
                    <Icon name="send" :size="12" /> {{ translate('pages.clarify.submitForm') }}
                  </button>
                </div>
              </div>
            </div>
            <div
              v-else
              class="mt-2 rounded-lg border border-n-ci/20 bg-n-ci/5 px-3 py-2"
              data-testid="clarify-form-readonly"
            >
              <div class="mb-1.5 flex items-center gap-1.5 text-[11px] font-medium text-txt3">
                <Icon name="ci" :size="12" class="text-n-ci" />
                {{ translate('pages.clarify.formsThisRound', { n: t.forms.length }) }}
                <span class="ml-1 inline-flex items-center gap-1 rounded border border-line px-1.5 py-0.5 text-[10px] font-normal text-txt3">
                  {{ translate('pages.clarify.readonly') }}
                </span>
              </div>
              <div v-for="(form, fi) in t.forms" :key="fi" class="mb-2 last:mb-0">
                <div v-if="form.title" class="mb-1 text-[12px] text-txt2">{{ form.title }}</div>
                <div
                  v-for="f in form.fields"
                  :key="f.name"
                  class="mb-1 text-[12px] text-txt3"
                  :title="fieldWhy(f) || undefined"
                >
                  {{ f.label }}
                </div>
              </div>
            </div>
          </template>

          <div
            v-if="t.interrupted"
            class="mt-1 inline-flex items-center gap-1 rounded border border-warn/40 bg-warn/10 px-1.5 py-0.5 text-[10px] text-warn"
            data-testid="clarify-interrupted"
          >
            interrupted
          </div>
          <!-- Keep footer time; hide bottom time when completion footnote is shown (keep_footer_hide_bottom) -->
          <div
            v-if="!showTurnCompleted(t)"
            class="mt-1 text-[10px] text-txt3"
            :class="t.role === 'human' ? 'text-right' : ''"
            data-testid="clarify-turn-bottom-time"
          >{{ locale && relTime(t.at) }}</div>
        </div>
      </div>
      <div v-if="thinking && !validating && liveAgentIdx < 0" class="flex items-center gap-2 pl-9 text-[12px] text-txt3">
        <span class="typing-dots"><i /><i /><i /></span>
        {{ translate('pages.clarify.thinking') }}
      </div>
    </div>
    <button
      v-if="showUnreadFab"
      type="button"
      data-testid="clarify-unread-fab"
      class="absolute bottom-3 left-1/2 z-10 inline-flex h-[34px] -translate-x-1/2 items-center gap-1.5 rounded-full bg-surface px-3.5 pl-3 text-[14px] font-semibold text-info shadow-[0_2px_10px_rgba(26,35,50,0.12)] transition-shadow hover:shadow-[0_4px_14px_rgba(26,35,50,0.16)]"
      :aria-label="unreadFabLabel"
      :title="unreadFabLabel"
      @click="onUnreadFabClick"
    >
      <Icon name="chevrons-down" :size="16" class="shrink-0" />
      <span class="min-w-[0.7em] text-center tabular-nums">{{ unreadCount }}</span>
    </button>
    </div>

    <div v-if="showDoneChrome" class="border-t border-line p-3 text-center text-[12px] text-ok">
      <Icon name="check" :size="13" class="-mt-0.5 mr-1 inline" />{{ translate('pages.clarify.done') }}
    </div>
    <!-- Success ceremony in flight: keep composer mounted-off until hold→done (g2.3). -->
    <div
      v-else-if="done"
      class="border-t border-line p-3 text-center text-[12px] text-txt3"
      data-testid="clarify-confirm-ceremony"
      aria-live="polite"
    />
    <div v-else-if="!active && !coldSession" class="border-t border-line p-3 text-center text-[12px] text-txt3">
      <Icon name="close" :size="13" class="-mt-0.5 mr-1 inline" />{{ translate('pages.clarify.closed') }}
    </div>
    <div v-else-if="coldSession" class="shrink-0 border-t border-line p-3" data-testid="clarify-cold-actions">
      <ComposerShell :show-chrome="false" :show-footer="true">
        <template #footer>
          <button
            v-if="useConfirmFlowAction"
            type="button"
            class="inline-flex h-9 shrink-0 items-center gap-1 rounded-md bg-ok px-3.5 text-sm font-medium text-white hover:bg-ok/90 disabled:opacity-50"
            data-testid="clarify-confirm-flow"
            :disabled="confirmDisabled"
            :title="translate('pages.clarify.confirmFlowTitle')"
            @click="finishEarly"
          >
            <Icon name="check" :size="13" />
            {{ validating ? translate('pages.clarify.validating') : translate('pages.clarify.confirmFlow') }}
          </button>
        </template>
      </ComposerShell>
    </div>
    <div v-else class="border-t border-line p-3">
      <!-- pending-send queue panel (Demo / AgentChatTester): clarify + review -->
      <PendingSendQueuePanel
        :items="queued"
        :notice="queueNotice"
        :toast="queueToast"
        @cancel="cancelQueuedItem"
        @edit="editQueuedItem"
        @reorder="reorderQueuedItems"
      />
      <div v-if="showAnnotationChips && annotations.length" class="mb-2 flex flex-wrap gap-1.5">
        <AnnotationChip
          v-for="(a, ai) in annotations"
          :key="ai"
          :ann="a"
          removable
          test-id="clarify-annotation-chip"
          @remove="removeAnnotation(ai)"
        />
      </div>
      <div v-if="attachNotice" class="rounded-md mb-2 border border-err/40 bg-err/10 px-2.5 py-1.5 text-[12px] text-err" data-testid="clarify-attach-notice" role="alert">
        {{ attachNotice }}
      </div>
      <div v-if="attachments.length" class="mb-2 flex flex-wrap gap-1.5">
        <div v-for="(im, ii) in attachments" :key="ii" class="relative">
          <ChatImageThumb
            v-if="isImageAttachment(im)"
            mode="previewable"
            size="sm"
            thumb-class="rounded-md"
            :src="imgSrc(im)"
            :label="attachmentDisplayName(im, ii)"
            :alt="attachmentDisplayName(im, ii)"
            test-id="clarify-draft-image-thumb"
            @preview="openImagePreview(attachments, ii)"
          />
          <div
            v-else
            class="rounded-lg flex h-14 max-w-[160px] items-center gap-1.5 border border-line bg-elevated px-2"
            :title="attachmentDisplayName(im, ii)"
            data-testid="clarify-pending-file-chip"
          >
            <span class="shrink-0 text-[9px] font-medium uppercase text-info">DOC</span>
            <span class="min-w-0 truncate text-[11px] text-txt2">{{ attachmentDisplayName(im, ii) }}</span>
          </div>
          <button
            class="absolute -right-1.5 -top-1.5 flex h-4 w-4 items-center justify-center rounded-full bg-err text-white"
            @click.stop="removeAttachment(ii)"
          ><Icon name="close" :size="9" /></button>
        </div>
      </div>
      <ComposerShell :show-footer="!hideFinish" box-test-id="clarify-input-row" toolbar-test-id="clarify-action-row">
        <template #input>
          <input ref="fileInput" type="file" multiple class="hidden" @change="onPickFiles" />
          <textarea
            ref="textareaRef"
            v-model="draft"
            class="composer-hint-wrap min-h-[40px] min-w-0 w-full resize-none border-0 bg-transparent p-0 text-sm text-txt shadow-none outline-none focus:ring-0"
            :class="overflowScroll ? 'scroll-area max-h-[128px] overflow-y-auto' : 'overflow-y-hidden'"
            rows="1"
            :placeholder="inputPlaceholder"
            data-testid="clarify-input"
            @input="onTextInput"
            @keydown="onComposerKeydown"
            @compositionstart="composing = true"
            @compositionend="composing = false"
            @paste="onPaste"
          />
        </template>
        <template #toolbar-start>
          <button
            type="button"
            class="flex h-10 w-10 shrink-0 items-center justify-center rounded-md border border-line text-txt2 hover:border-line-strong disabled:opacity-50"
            :title="translate('pages.clarify.addImage')"
            data-testid="clarify-attach-btn"
            @click="fileInput?.click()"
          >
            <Icon name="paperclip" :size="16" />
          </button>
        </template>
        <template #toolbar-end>
          <button
            v-if="sessionBusy"
            type="button"
            class="inline-flex h-[30px] shrink-0 items-center gap-1 rounded-md border border-line bg-elevated px-2.5 text-xs font-semibold text-txt2 hover:border-line-strong"
            data-testid="clarify-review-cancel"
            title="Cancel"
            @click="cancelReview"
          >
            Cancel
          </button>
          <button
            v-if="sendLabel"
            type="button"
            class="inline-flex h-[30px] shrink-0 items-center gap-1 rounded-md bg-accent px-2.5 text-xs font-semibold text-white hover:bg-accent-hover disabled:opacity-50"
            data-testid="clarify-send-label"
            :disabled="!draft.trim() && !attachments.length && !annotations.length"
            @click="send"
          >
            <Icon name="send" :size="14" /> {{ sendLabel }}
          </button>
          <button
            v-else
            type="button"
            class="flex h-[30px] w-[30px] shrink-0 items-center justify-center rounded-md bg-accent text-white hover:bg-accent-hover disabled:opacity-50"
            data-testid="clarify-send-icon"
            :disabled="!draft.trim() && !attachments.length && !annotations.length"
            @click="send"
          >
            <Icon name="send" :size="14" />
          </button>
        </template>
        <template #hint>
          <p
            v-if="reviewMode"
            class="m-0 min-w-0 text-[11px] leading-snug text-txt3 [overflow-wrap:anywhere]"
            data-testid="clarify-confirm-hint"
          >
            {{ validating ? translate('pages.clarify.validating') : translate('pages.clarify.confirmFlowHint') }}
          </p>
        </template>
        <template #footer>
          <button
            v-if="useConfirmFlowAction"
            type="button"
            class="inline-flex h-9 shrink-0 items-center gap-1 rounded-md bg-ok px-3.5 text-sm font-medium text-white hover:bg-ok/90 disabled:opacity-50"
            data-testid="clarify-confirm-flow"
            :disabled="confirmDisabled"
            :title="translate('pages.clarify.confirmFlowTitle')"
            @click="finishEarly"
          >
            <Icon name="check" :size="13" />
            {{ validating ? translate('pages.clarify.validating') : translate('pages.clarify.confirmFlow') }}
          </button>
          <button
            v-else
            type="button"
            class="inline-flex h-9 shrink-0 items-center gap-1 rounded-md border border-line bg-elevated px-3 text-sm font-medium text-txt2 hover:border-line-strong disabled:opacity-50"
            :disabled="confirmDisabled"
            :title="translate('pages.clarify.finishEarlyTitle')"
            @click="finishEarly"
          >
            <Icon name="check" :size="13" /> {{ translate('pages.clarify.finishEarly') }}
          </button>
        </template>
      </ComposerShell>
    </div>
    <div
      v-if="confirmError && !done"
      class="flex items-center gap-1.5 border-t border-err/30 bg-err/10 px-3 py-2 text-[12px] text-err"
      data-testid="clarify-confirm-error"
      role="alert"
    >
      <Icon name="alert" :size="13" />
      <span class="min-w-0 flex-1 [overflow-wrap:anywhere]">{{ confirmError }}</span>
    </div>
  </div>

  <!-- Human history attachment image preview (single slot; no gallery / Esc) -->
  <ChatImagePreviewModal
    :open="!!imagePreview"
    :src="imagePreview?.src || ''"
    :label="imagePreview?.label || ''"
    test-id-prefix="clarify-image-preview"
    @close="closeImagePreview"
  />
</template>

<style scoped>
/* Placeholder must wrap to the textarea width — UA default often clips the 2nd line. */
.composer-hint-wrap::placeholder {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  word-break: break-word;
  overflow: visible;
  text-overflow: unset;
}
.composer-hint-wrap::-webkit-input-placeholder {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  word-break: break-word;
  overflow: visible;
  text-overflow: unset;
}
.composer-hint-wrap::-moz-placeholder {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  word-break: break-word;
  overflow: visible;
  opacity: 1;
}

/* Card-deck step transition: subtle slide + fade as one card advances. */
.deck-enter-active,
.deck-leave-active {
  transition: opacity 0.16s ease, transform 0.16s ease;
}
.deck-enter-from {
  opacity: 0;
  transform: translateX(8px);
}
.deck-leave-to {
  opacity: 0;
  transform: translateX(-8px);
}

/* "thinking" typing indicator: three dots bouncing in sequence. */
.typing-dots {
  display: inline-flex;
  align-items: center;
  gap: 3px;
}
.typing-dots i {
  width: 5px;
  height: 5px;
  border-radius: 9999px;
  background: #22d3ee;
  animation: typing-bounce 1.2s infinite ease-in-out both;
}
.typing-dots i:nth-child(2) {
  animation-delay: 0.16s;
}
.typing-dots i:nth-child(3) {
  animation-delay: 0.32s;
}
@keyframes typing-bounce {
  0%,
  70%,
  100% {
    transform: translateY(0);
    opacity: 0.35;
  }
  35% {
    transform: translateY(-4px);
    opacity: 1;
  }
}

/* 「输出中」: same --grad-logo shimmer as .brand-logo__name */
.clarify-outputting {
  color: rgb(var(--c-accent-2));
  background: var(--grad-logo);
  background-size: var(--grad-logo-size);
  -webkit-background-clip: text;
  background-clip: text;
  -webkit-text-fill-color: transparent;
  animation: shimmer 3.5s ease-in-out infinite;
}

/* Streaming caret at message tail (Demo phase 3) */
.clarify-stream-caret {
  display: inline-block;
  width: 7px;
  height: 1em;
  margin-left: 2px;
  vertical-align: text-bottom;
  background: rgb(var(--c-accent));
  animation: clarify-caret-blink 0.9s step-end infinite;
}
@keyframes clarify-caret-blink {
  50% {
    opacity: 0;
  }
}
@media (prefers-reduced-motion: reduce) {
  .typing-dots i,
  .clarify-outputting,
  .clarify-stream-caret {
    animation: none !important;
  }
  .clarify-outputting {
    color: rgb(var(--c-accent-2));
    background: none;
    -webkit-background-clip: unset;
    background-clip: unset;
    -webkit-text-fill-color: unset;
  }
}
</style>
