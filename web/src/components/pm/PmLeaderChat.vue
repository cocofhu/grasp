<script setup lang="ts">
import AppButton from '@/components/ui/AppButton.vue'
import Icon from '@/components/ui/Icon.vue'
import ChatImageThumb from '@/components/ui/ChatImageThumb.vue'
import ChatImagePreviewModal from '@/components/ui/ChatImagePreviewModal.vue'
import CitationCard from '@/components/pm/CitationCard.vue'
import ArtifactLoadingPane from '@/components/run/ArtifactLoadingPane.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import TokenUsageByModelTip from '@/components/ui/TokenUsageByModelTip.vue'
import { usePmLeaderChat } from '@/lib/pm/usePmLeaderChat'
import type { PmLeaderBinding } from '@/lib/shared/types'

const props = defineProps<{
  projectId: string
  binding: PmLeaderBinding | null
  restoreMobileChat?: boolean
  unknownModelDisplayName?: string | null
}>()
const emit = defineEmits<{ openSettings: []; restoredMobileChat: [] }>()

const {
  t,
  toast,
  isMobile,
  mobileView,
  threads,
  activeId,
  messages,
  input,
  loading,
  messagesLoading,
  messagesLoadFailed,
  finalizingRefetchFailed,
  finalizing,
  sending,
  streaming,
  streamText,
  streamHtml,
  streamPreview,
  unsubStreamHtml,
  syncStreamText,
  appendStreamText,
  clearStreamText,
  resuming,
  scroller,
  STICK_THRESHOLD,
  TOP_THRESHOLD,
  PAGE_SIZE,
  stickToBottom,
  hasMoreEarlier,
  historyLoading,
  historyLoadFailed,
  isNearBottom,
  onScrollerScroll,
  scrollBottom,
  mergeMessagesKeepPrefix,
  resetHistoryWindowState,
  failedPartialByUserMsgId,
  activeUserMessageId,
  attachments,
  fileInput,
  attachNotice,
  onPickFiles,
  onPaste,
  removeAttachment,
  imagePreview,
  openChatImagePreview,
  closeChatImagePreview,
  enabled,
  turnBusy,
  busy,
  canSend,
  showStreamBubble,
  mainViewState,
  busyHint,
  isPullingBoot,
  suggestions,
  showStreamTypingDots,
  copyAssistantText,
  showThreadsAside,
  showChatSection,
  channelTypeOf,
  isChannelThread,
  channelBadgeLabel,
  channelBadgeClass,
  channelReadonlyTitle,
  channelReadonlyHint,
  threadDisplayTitle,
  channelSourceLine,
  activeThread,
  activeIsChannel,
  activeThreadTitle,
  showEmptyHint,
  showHistoryTip,
  historyTipText,
  historyTipClass,
  showIdleSuggestions,
  channelCtx,
  channelDetailOpen,
  channelDetailTitle,
  channelDetailSource,
  closeChannelCtx,
  openChannelCtx,
  openChannelDetail,
  closeChannelDetail,
  onChannelCtxAction,
  FAIL_KIND_KEYS,
  failMeta,
  applyRestoreMobileChat,
  isFailedUser,
  isChannelHint,
  loadThreads,
  activateThread,
  selectThread,
  backToThreads,
  ensureActiveThread,
  loadMessages,
  loadEarlier,
  retryLoadMessages,
  setFailedPartial,
  clearFailedPartial,
  newThread,
  removeThread,
  closeWs,
  resetTurnLocal,
  patchLocalMessage,
  handleAcp,
  clearFinalizingStream,
  refetchAfterTurnDone,
  onTurnDone,
  onTurnError,
  send,
  stop,
  retryTurn,
  attachmentDisplayName,
  relTime,
  imgSrc,
  renderMarkdown,
  isImageAttachment
} = usePmLeaderChat(props, emit)
</script>

<template>
  <div v-if="!enabled" class="flex min-h-[420px] flex-col items-center justify-center gap-3 text-center">
    <h3 class="text-lg font-semibold text-txt">{{ t('pages.projectDetail.pm.disabledTitle') }}</h3>
    <p class="max-w-md text-sm text-txt3">{{ t('pages.projectDetail.pm.disabledHint') }}</p>
    <p v-if="binding?.agentError" class="text-sm text-err">{{ binding.agentError }}</p>
    <AppButton @click="emit('openSettings')">{{ t('pages.projectDetail.pm.goSettings') }}</AppButton>
  </div>

  <div v-else class="rounded-lg flex min-h-0 flex-1 overflow-hidden border border-line bg-base">
    <!-- left rail -->
    <aside
      v-if="showThreadsAside"
      data-testid="pm-threads-aside"
      class="flex min-h-0 shrink-0 flex-col border-r border-line bg-surface"
      :class="isMobile ? 'w-full border-r-0' : 'w-56'"
    >
      <div class="flex items-center justify-between border-b border-line px-2 py-2">
        <span class="text-xs font-medium text-txt3">{{ t('pages.projectDetail.pm.threads') }}</span>
        <AppButton
          size="sm"
          variant="ghost"
          :disabled="turnBusy"
          :class="isMobile ? 'min-h-[44px] min-w-[44px]' : ''"
          @click="newThread"
        >{{ t('pages.projectDetail.pm.newThread') }}</AppButton>
      </div>
      <div class="scroll-area flex-1 overflow-y-auto">
        <button
          v-for="th in threads"
          :key="th.id"
          type="button"
          class="group flex w-full items-center gap-1 border-b border-line px-2 text-left text-sm text-txt2 hover:bg-elevated hover:text-txt disabled:cursor-not-allowed disabled:opacity-50"
          :class="[
            th.id === activeId ? 'bg-elevated font-medium text-txt' : '',
            isMobile ? 'min-h-[44px] py-3' : 'py-2',
          ]"
          :data-channel="isChannelThread(th) ? '1' : '0'"
          :disabled="turnBusy && th.id !== activeId"
          @click="selectThread(th.id)"
          @contextmenu="openChannelCtx($event, th)"
        >
          <span class="flex min-w-0 flex-1 items-center gap-1.5 overflow-hidden">
            <span class="min-w-0 truncate font-mono text-[12px]">{{ threadDisplayTitle(th) }}</span>
            <span
              v-if="isChannelThread(th)"
              class="rounded-md inline-flex shrink-0 items-center border px-1 text-[9px] font-bold tracking-wide leading-4"
              :class="channelBadgeClass(th)"
              data-testid="pm-qq-tag"
              :data-channel-kind="channelTypeOf(th)"
              :title="channelSourceLine(th)"
            >{{ channelBadgeLabel(th) }}</span>
            <span
              v-if="th.unspoken"
              class="rounded-md inline-flex shrink-0 items-center border border-warn/45 px-1 text-[9px] leading-4 text-warn"
              data-testid="pm-unspoken-tag"
            >{{ t('pages.projectDetail.pm.unspoken') }}</span>
            <span
              v-else
              class="rounded-md inline-flex shrink-0 items-center border border-line bg-elevated px-1 text-[9px] font-bold tracking-wide leading-4 text-txt3"
              data-testid="pm-web-tag"
            >{{ t('pages.projectDetail.pm.channelBadgeWeb') }}</span>
          </span>
          <span
            v-if="!turnBusy && !isChannelThread(th)"
            class="hidden text-xs text-txt3 group-hover:inline"
            data-testid="pm-thread-delete"
            @click.stop="removeThread(th.id)"
          >×</span>
        </button>
        <p v-if="!threads.length && !loading" class="px-2 py-4 text-xs text-txt3">
          {{ t('pages.projectDetail.pm.noThreads') }}
        </p>
      </div>
    </aside>

    <!-- main chat -->
    <section
      v-if="showChatSection"
      data-testid="pm-chat-section"
      class="flex min-h-0 min-w-0 flex-col bg-base"
      :class="isMobile ? 'w-full flex-1' : 'flex-1'"
    >
      <div
        class="flex shrink-0 items-center gap-2 border-b border-line px-3 py-2"
        :class="isMobile ? 'min-h-[44px]' : ''"
      >
        <button
          v-if="isMobile"
          type="button"
          class="flex h-11 w-11 shrink-0 items-center justify-center rounded-md text-txt2 hover:bg-elevated hover:text-txt disabled:cursor-not-allowed disabled:opacity-50"
          data-testid="pm-mobile-back-to-threads"
          :aria-label="t('shell.aria.backToList')"
          :disabled="turnBusy"
          @click="backToThreads"
        >
          <Icon name="arrow-left" :size="18" />
        </button>
        <div
          v-if="activeId"
          class="flex min-w-0 flex-1 flex-col overflow-hidden"
          data-testid="pm-chat-title"
        >
          <div class="flex min-w-0 items-center gap-2 overflow-hidden">
            <span class="min-w-0 truncate text-sm font-medium text-txt">{{ activeThreadTitle }}</span>
            <span
              v-if="activeIsChannel"
              class="rounded-md inline-flex shrink-0 items-center border px-1 text-[9px] font-bold tracking-wide leading-4"
              :class="channelBadgeClass(activeThread)"
              data-testid="pm-qq-tag-header"
              :data-channel-kind="channelTypeOf(activeThread)"
              :title="channelSourceLine(activeThread)"
            >{{ channelBadgeLabel(activeThread) }}</span>
            <span
              v-if="activeThread?.unspoken"
              class="rounded-md inline-flex shrink-0 items-center border border-warn/45 px-1 text-[9px] leading-4 text-warn"
              data-testid="pm-unspoken-tag-header"
            >{{ t('pages.projectDetail.pm.unspoken') }}</span>
          </div>
          <div
            v-if="activeIsChannel"
            class="truncate text-[11px] text-txt3"
            data-testid="pm-channel-subtitle"
          >
            {{ channelSourceLine(activeThread) }}
          </div>
        </div>
        <span v-else class="min-w-0 flex-1" />
        <AppButton
          size="sm"
          variant="ghost"
          icon="settings"
          data-testid="pm-chat-open-settings"
          :class="isMobile ? 'min-h-[44px] shrink-0' : ''"
          @click="emit('openSettings')"
        >
          {{ t('pages.projectDetail.pm.settings') }}
        </AppButton>
      </div>
      <div
        ref="scroller"
        data-testid="pm-message-scroller"
        class="scroll-area min-h-0 flex-1 space-y-3 overflow-y-auto p-4"
        @scroll="onScrollerScroll"
      >
        <ArtifactLoadingPane
          v-if="mainViewState === 'messagesLoading'"
          message-key="pages.projectDetail.pm.loadingHistory"
          data-testid="pm-messages-loading"
        />

        <EmptyState
          v-else-if="mainViewState === 'errorEmpty'"
          icon="alert"
          :title="t('pages.projectDetail.pm.loadFailed')"
          :desc="t('pages.projectDetail.pm.loadFailedDesc')"
          data-testid="pm-messages-load-failed"
        >
          <AppButton variant="primary" size="sm" icon="refresh" @click="retryLoadMessages">
            {{ t('pages.projectDetail.pm.retry') }}
          </AppButton>
        </EmptyState>

        <template v-else>
        <div class="mx-auto flex max-w-3xl flex-col gap-3.5">
        <div v-if="showEmptyHint" class="space-y-3 py-8 text-center">
          <p class="text-sm text-txt3">{{ t('pages.projectDetail.pm.emptyHint') }}</p>
          <div class="flex flex-wrap justify-center gap-2">
            <button
              v-for="s in suggestions"
              :key="s"
              type="button"
              class="rounded-md border border-line bg-surface px-3 py-1 text-sm text-txt2 hover:bg-elevated hover:text-txt disabled:opacity-50"
              :disabled="busy"
              @click="send(s)"
            >
              {{ s }}
            </button>
          </div>
        </div>

        <div
          v-if="showHistoryTip"
          data-testid="pm-history-tip"
          class="min-h-[28px] py-1.5 text-center text-[11.5px]"
          :class="historyTipClass"
        >
          {{ historyTipText }}
        </div>

        <template v-for="m in messages" :key="m.id">
          <div
            v-if="isChannelHint(m)"
            class="rounded-lg mx-auto flex max-w-[92%] items-start gap-2 self-center border border-warn/35 bg-warn/[0.08] px-3 py-2 text-[12px] text-txt"
            :data-msg-id="m.id"
            data-testid="pm-channel-hint"
          >
            <span
              class="rounded-md shrink-0 border border-warn/35 px-1.5 py-px text-[10px] font-bold tracking-wide text-warn"
            >
              {{ t('pages.projectDetail.pm.channelHintLabel') }}
            </span>
            <div class="min-w-0">
              <div>{{ m.content }}</div>
              <div class="mt-0.5 text-[11px] text-txt2">
                {{ t('pages.projectDetail.pm.channelHintMeta') }}
              </div>
            </div>
          </div>
          <div v-else-if="m.role === 'user'" class="flex gap-2.5 flex-row-reverse" :data-msg-id="m.id">
            <div
              class="rounded-full flex h-7 w-7 shrink-0 items-center justify-center border border-accent/20 bg-accent-dim text-[11px] font-semibold text-accent-2"
            >
              {{ t('pages.projectDetail.pm.me') }}
            </div>
            <div class="min-w-0 max-w-[85%]">
              <div v-if="m.images?.length" class="mb-1.5 flex flex-wrap justify-end gap-1.5">
                <template v-for="(im, ii) in m.images" :key="ii">
                  <ChatImageThumb
                    v-if="isImageAttachment(im)"
                    mode="previewable"
                    size="md"
                    :src="imgSrc(im)"
                    :label="attachmentDisplayName(im, ii)"
                    :alt="attachmentDisplayName(im, ii)"
                    test-id="pm-history-image-thumb"
                    @preview="openChatImagePreview(imgSrc(im), attachmentDisplayName(im, ii))"
                  />
                  <div
                    v-else
                    class="rounded-lg flex max-w-[200px] items-center gap-2 border border-line bg-elevated px-2 py-1.5"
                    data-testid="pm-history-file-chip"
                    :title="attachmentDisplayName(im, ii)"
                  >
                    <span class="shrink-0 text-[10px] font-medium uppercase tracking-wide text-info">DOC</span>
                    <span class="min-w-0 truncate text-[12px] text-txt">{{ attachmentDisplayName(im, ii) }}</span>
                  </div>
                </template>
              </div>
              <div
                v-if="m.content"
                class="rounded-lg border border-accent/35 bg-accent px-3 py-2 text-sm leading-6 text-white whitespace-pre-wrap"
              >
                {{ m.content }}
              </div>
              <div
                v-else-if="m.images?.length"
                class="rounded-lg border border-accent/35 bg-accent px-3 py-2 text-sm leading-6 text-white/80"
              >
                {{ t('pages.projectDetail.pm.imagesOnly') }}
              </div>
              <div class="msg-time mt-1 text-right text-[10px] text-txt3">{{ relTime(m.createdAt) }}</div>
            </div>
          </div>
          <div v-else-if="m.role === 'assistant'" class="flex gap-2.5" :data-msg-id="m.id">
            <div
              class="rounded-full flex h-7 w-7 shrink-0 items-center justify-center border border-accent/25 bg-accent/10 text-accent-2"
            >
              <Icon name="robot" :size="15" />
            </div>
            <div class="min-w-0 max-w-[85%]">
              <div
                data-assistant-bubble
                class="rounded-md border border-line bg-elevated px-3 py-2 text-sm leading-6 text-txt"
              >
                <div class="md" v-html="renderMarkdown(m.content)" />
                <div
                  v-if="m.content?.trim() || m.usage != null"
                  class="msg-actions mt-1.5 flex flex-wrap items-center justify-end gap-2 border-t border-line pt-1"
                >
                  <TokenUsageByModelTip
                    v-if="m.usage != null"
                    :usage="m.usage"
                    :usage-by-model="m.usageByModel"
                    :unknown-model-display-name="unknownModelDisplayName"
                  />
                  <button
                    v-if="m.content?.trim()"
                    type="button"
                    class="inline-flex items-center gap-1 px-2 py-0.5 text-[11px] text-txt3 hover:bg-surface hover:text-txt2"
                    @click="copyAssistantText"
                  >
                    <Icon name="copy" :size="12" />
                    {{ t('pages.projectDetail.pm.copyFull') }}
                  </button>
                </div>
              </div>
              <div class="msg-time mt-1 text-right text-[10px] text-txt3">{{ relTime(m.createdAt) }}</div>
              <div v-if="m.citations?.length" class="mt-1.5 space-y-1">
                <CitationCard v-for="(c, i) in m.citations" :key="i" :citation="c" />
              </div>
            </div>
          </div>

          <!-- S2: failed partial bubble (session-only), then independent fail card. -->
          <div
            v-if="isFailedUser(m) && failedPartialByUserMsgId[m.id]"
            class="flex gap-2.5"
            data-testid="pm-failed-partial"
          >
            <div
              class="rounded-full flex h-7 w-7 shrink-0 items-center justify-center border border-accent/25 bg-accent/10 text-accent-2"
            >
              <Icon name="robot" :size="15" />
            </div>
            <div class="min-w-0 max-w-[85%]">
              <div class="rounded-lg border border-err/35 bg-err/[0.06] px-3 py-2 text-sm leading-6 text-txt">
                <div class="mb-1.5 flex items-center gap-1.5 text-[11px] font-semibold text-red-400">
                  <Icon name="alert" :size="14" class="shrink-0" />
                  {{ t('pages.projectDetail.pm.failPartialKeptMeta') }}
                </div>
                <div class="md" v-html="renderMarkdown(failedPartialByUserMsgId[m.id])" />
              </div>
            </div>
          </div>

          <!-- Failure card hangs beside the user turn (after the bubble / partial). -->
          <div v-if="isFailedUser(m)" class="flex justify-start">
            <div
              class="rounded-lg fail-card ml-[38px] max-w-[85%] flex flex-col gap-2 border border-err/35 bg-err/10 px-3 py-2.5"
              role="alert"
            >
              <div class="flex items-start gap-2">
                <Icon name="alert" :size="16" class="mt-0.5 shrink-0 text-err" />
                <div>
                  <div class="text-[13px] font-semibold text-err">
                    {{ failMeta(m.failKind || 'connection').title }}
                  </div>
                  <div class="mt-0.5 text-xs text-txt2">
                    {{ failMeta(m.failKind || 'connection').desc }}
                  </div>
                </div>
              </div>
              <div>
                <button
                  type="button"
                  class="rounded-md border border-err/40 bg-transparent px-2.5 py-1 text-xs text-err hover:bg-err/15 disabled:opacity-50"
                  :disabled="busy"
                  data-testid="pm-fail-retry"
                  @click="retryTurn(m.id)"
                >
                  {{ t('pages.projectDetail.pm.retry') }}
                </button>
              </div>
            </div>
          </div>
        </template>

        <div v-if="showStreamBubble" class="flex gap-2.5" data-testid="pm-stream-bubble">
          <div
            class="rounded-full flex h-7 w-7 shrink-0 items-center justify-center border border-accent/25 bg-accent/10 text-accent-2"
          >
            <Icon name="robot" :size="15" />
          </div>
          <div class="min-w-0 max-w-[85%]">
            <div
              class="rounded-lg border bg-elevated px-3 py-2 text-sm text-txt"
              :class="
                finalizing
                  ? 'border-warn/35 shadow-[inset_0_0_0_1px_rgb(var(--color-warn)/0.08)]'
                  : resuming
                    ? 'border-accent-2/40 shadow-[inset_0_0_0_1px_rgb(var(--color-accent-2)/0.08)]'
                    : 'border-line'
              "
            >
              <div v-if="resuming && !streamText && !finalizing" class="flex items-center gap-1.5 text-txt3">
                <Icon name="spinner" :size="13" class="animate-spin text-accent-2" />
                {{ t('pages.projectDetail.pm.resuming') }}
              </div>
              <div v-else-if="streamText" class="md" v-html="streamHtml" />
              <div v-else-if="showStreamTypingDots" class="typing-dots py-1">
                <i /><i /><i />
              </div>
              <div v-else class="flex items-center gap-1.5 text-txt3">
                <Icon name="spinner" :size="13" class="animate-spin text-accent-2" />
                {{ t('pages.projectDetail.pm.generating') }}
              </div>
              <div
                v-if="finalizing"
                class="mt-2 flex flex-wrap items-center gap-1.5 border-t border-line pt-2 text-[11px] text-warn"
                data-testid="pm-stream-finalizing-meta"
              >
                <template v-if="finalizingRefetchFailed">
                  <Icon name="alert" :size="13" class="text-warn" />
                  <span>{{ t('pages.projectDetail.pm.loadFailed') }}</span>
                  <button
                    type="button"
                    class="ml-1 text-accent underline-offset-2 hover:underline"
                    data-testid="pm-finalizing-retry"
                    @click="refetchAfterTurnDone"
                  >
                    {{ t('pages.projectDetail.pm.retry') }}
                  </button>
                </template>
                <template v-else>
                  <Icon name="spinner" :size="13" class="animate-spin text-warn" />
                  {{ t('pages.projectDetail.pm.finalizing') }}
                </template>
              </div>
              <div
                v-else-if="resuming"
                class="mt-2 flex items-center gap-1.5 border-t border-line pt-2 text-[11px] text-accent-2"
                data-testid="pm-stream-resuming-meta"
              >
                <Icon name="spinner" :size="13" class="animate-spin text-accent-2" />
                {{ t('pages.projectDetail.pm.resuming') }}
              </div>
            </div>
            <div
              class="msg-time mt-1 text-right text-[10px] text-txt3"
              :class="streamText || finalizing ? '' : 'invisible'"
            >
              —
            </div>
          </div>
        </div>

        <div
          v-if="showIdleSuggestions"
          class="mt-1 flex flex-wrap gap-1.5 pl-[38px]"
          data-testid="pm-idle-suggestions"
        >
          <button
            v-for="s in suggestions"
            :key="s"
            type="button"
            class="rounded-md border border-line bg-surface px-2.5 py-1 text-xs text-txt2 hover:bg-elevated hover:text-txt disabled:opacity-50"
            :disabled="busy"
            @click="send(s)"
          >
            {{ s }}
          </button>
        </div>
        </div>
        </template>
      </div>

      <div v-if="activeIsChannel" class="shrink-0 border-t border-line p-3" data-testid="pm-channel-readonly">
        <div class="rounded-lg flex items-center gap-2.5 border border-accent-2/30 bg-accent/10 px-3.5 py-3">
          <div
            class="rounded-full flex h-7 w-7 shrink-0 items-center justify-center border border-accent-2/35 bg-accent/15 text-accent-2"
            aria-hidden="true"
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <rect x="3" y="11" width="18" height="11" />
              <path d="M7 11V7a5 5 0 0 1 10 0v4" />
            </svg>
          </div>
          <div class="min-w-0">
            <strong class="block text-[13px] font-semibold text-txt">
              {{ channelReadonlyTitle(activeThread) }}
            </strong>
            <p class="mt-0.5 text-xs text-txt3">
              {{ channelReadonlyHint(activeThread) }}
            </p>
          </div>
        </div>
      </div>
      <div v-else class="shrink-0 border-t border-line p-3">
        <div v-if="attachments.length" class="mb-2 flex flex-wrap gap-1.5">
          <div v-for="(im, ii) in attachments" :key="ii" class="relative">
            <ChatImageThumb
              v-if="isImageAttachment(im)"
              mode="previewable"
              size="sm"
              :src="imgSrc(im)"
              :label="attachmentDisplayName(im, ii)"
              :alt="attachmentDisplayName(im, ii)"
              test-id="pm-draft-image-thumb"
              @preview="openChatImagePreview(imgSrc(im), attachmentDisplayName(im, ii))"
            />
            <div
              v-else
              class="rounded-lg flex h-14 max-w-[160px] items-center gap-1.5 border border-line bg-elevated px-2"
              data-testid="pm-pending-file-chip"
              :title="attachmentDisplayName(im, ii)"
            >
              <span class="shrink-0 text-[9px] font-medium uppercase text-info">DOC</span>
              <span class="min-w-0 truncate text-[11px] text-txt2">{{ attachmentDisplayName(im, ii) }}</span>
            </div>
            <button
              type="button"
              class="absolute -right-1.5 -top-1.5 flex h-4 w-4 items-center justify-center bg-err text-white"
              :disabled="busy"
              @click.stop="removeAttachment(ii)"
            >
              <Icon name="close" :size="9" />
            </button>
          </div>
        </div>
        <div class="flex flex-wrap items-end gap-2">
          <input
            ref="fileInput"
            type="file"
            multiple
            class="hidden"
            @change="onPickFiles"
          />
          <AppButton
            size="sm"
            variant="outline"
            icon="paperclip"
            :disabled="busy"
            data-testid="pm-chat-attach"
            :class="isMobile ? 'min-h-[44px] min-w-[44px]' : ''"
            @click="fileInput?.click()"
          >
            {{ t('pages.projectDetail.pm.images') }}
          </AppButton>
          <textarea
            v-model="input"
            rows="2"
            class="rounded-md scroll-area max-h-32 min-h-[40px] min-w-0 flex-1 resize-none border border-line bg-base px-3 py-2 text-[13px] text-txt outline-none focus:border-accent disabled:opacity-50"
            :placeholder="t('pages.projectDetail.pm.inputPh')"
            :disabled="busy"
            @keydown.enter.exact.prevent="send()"
            @paste="onPaste"
          />
          <AppButton
            v-if="turnBusy"
            size="sm"
            variant="outline"
            icon="close"
            :class="isMobile ? 'min-h-[44px] min-w-[44px]' : ''"
            @click="stop"
          >
            {{ t('pages.projectDetail.pm.stop') }}
          </AppButton>
          <AppButton
            v-else
            size="sm"
            variant="primary"
            icon="send"
            :disabled="!canSend"
            data-testid="pm-chat-send"
            :class="isMobile ? 'min-h-[44px] min-w-[44px]' : ''"
            @click="send()"
          >
            {{ t('pages.projectDetail.pm.send') }}
          </AppButton>
        </div>
        <p
          v-if="turnBusy"
          class="mt-2 text-[11px]"
          :class="isPullingBoot ? 'text-warn' : 'text-warn'"
          data-testid="pm-busy-hint"
          :data-pulling="isPullingBoot ? '1' : '0'"
        >
          {{ busyHint }}
        </p>
      </div>
    </section>
  </div>

  <!-- Channel context menu -->
  <div
    v-if="channelCtx"
    class="fixed inset-0 z-40"
    data-testid="pm-channel-ctx-backdrop"
    @click="closeChannelCtx"
    @contextmenu.prevent="closeChannelCtx"
  />
  <div
    v-if="channelCtx"
    class="rounded-lg fixed z-50 min-w-[160px] border border-line bg-elevated py-1 shadow-card"
    data-testid="pm-channel-ctx-menu"
    role="menu"
    :style="{ left: `${channelCtx.x}px`, top: `${channelCtx.y}px` }"
  >
    <button
      type="button"
      role="menuitem"
      class="flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs text-txt2 hover:bg-overlay hover:text-txt"
      data-testid="pm-channel-ctx-detail"
      @click="onChannelCtxAction"
    >
      <Icon name="doc" :size="13" />
      {{ t('pages.projectDetail.pm.channelViewDetail') }}
    </button>
  </div>

  <!-- Channel detail modal -->
  <div
    v-if="channelDetailOpen"
    class="fixed inset-0 z-[60] flex items-center justify-center bg-black/60 p-4"
    data-testid="pm-channel-detail-backdrop"
    role="dialog"
    aria-modal="true"
    @click.self="closeChannelDetail"
  >
    <div class="rounded-xl w-full max-w-[360px] border border-line bg-surface shadow-card">
      <div class="flex items-center justify-between border-b border-line px-3.5 py-3 text-[13px] font-semibold text-txt">
        <span>{{ t('pages.projectDetail.pm.channelDetailTitle') }}</span>
        <button
          type="button"
          class="px-2 text-txt2 hover:text-txt"
          :aria-label="t('pages.projectDetail.pm.channelDetailClose')"
          data-testid="pm-channel-detail-close"
          @click="closeChannelDetail"
        >×</button>
      </div>
      <div class="flex flex-col gap-3 p-3.5">
        <div>
          <div class="mb-1 text-[11px] text-txt3">{{ t('pages.projectDetail.pm.channelDetailLabelTitle') }}</div>
          <div class="rounded-lg border border-line bg-base px-2.5 py-2 text-[13px] text-txt" data-testid="pm-channel-detail-title">
            {{ channelDetailTitle }}
          </div>
        </div>
        <div>
          <div class="mb-1 text-[11px] text-txt3">{{ t('pages.projectDetail.pm.channelDetailLabelSource') }}</div>
          <div class="rounded-lg border border-line bg-base px-2.5 py-2 text-[13px] text-txt" data-testid="pm-channel-detail-source">
            {{ channelDetailSource }}
          </div>
        </div>
      </div>
      <div class="flex justify-end border-t border-line px-3.5 py-2.5">
        <AppButton size="sm" variant="outline" data-testid="pm-channel-detail-ok" @click="closeChannelDetail">
          {{ t('pages.projectDetail.pm.channelDetailClose') }}
        </AppButton>
      </div>
    </div>
  </div>

  <ChatImagePreviewModal
    :open="!!imagePreview"
    :src="imagePreview?.src || ''"
    :label="imagePreview?.label || ''"
    test-id-prefix="pm-image-preview"
    @close="closeChatImagePreview"
  />
</template>

<style scoped>
.typing-dots {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}
.typing-dots i {
  width: 5px;
  height: 5px;
  border-radius: 9999px;
  background: rgb(var(--c-accent-2));
  animation: pm-typing-bounce 1.2s infinite ease-in-out both;
}
.typing-dots i:nth-child(2) {
  animation-delay: 0.16s;
}
.typing-dots i:nth-child(3) {
  animation-delay: 0.32s;
}
@keyframes pm-typing-bounce {
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
</style>
