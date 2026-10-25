<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import AppButton from '@/components/ui/AppButton.vue'
import Icon from '@/components/ui/Icon.vue'
import ChatImageThumb from '@/components/ui/ChatImageThumb.vue'
import ChatImagePreviewModal from '@/components/ui/ChatImagePreviewModal.vue'
import CitationCard from '@/components/pm/CitationCard.vue'
import ChatMessageRail from '@/components/chat/ChatMessageRail.vue'
import ArtifactUnreadMark from '@/components/chat/ArtifactUnreadMark.vue'
import ConversationWorkspace from '@/components/chat/ConversationWorkspace.vue'
import ConversationArtifactBrowser from '@/components/chat/ConversationArtifactBrowser.vue'
import { provideConversationArtifacts, type ConversationArtifactTarget } from '@/lib/chat/conversationArtifacts'
import {
  artifactButtonAriaLabel,
  createCitationUnreadState,
  reduceArtifactCitationUnread,
} from '@/lib/chat/artifactUnread'
import ArtifactLoadingPane from '@/components/run/ArtifactLoadingPane.vue'
import TokenUsageByModelTip from '@/components/ui/TokenUsageByModelTip.vue'
import { usePmLeaderChat } from '@/lib/pm/usePmLeaderChat'
import type { PmLeaderBinding } from '@/lib/shared/types'

const props = defineProps<{
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
  threadsLoadFailed,
  canCreateThread,
  canContinueThread,
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

// Keep old conversations readable even when sending is disabled or the default Agent is missing.
const setupRequired = computed(() => !loading.value && !threadsLoadFailed.value && !threads.value.length && !canCreateThread.value)
const suggestionIcons = ['chart', 'gate', 'alert']
const artifactOpen = ref(false)
const artifactTarget = ref<ConversationArtifactTarget | null>(null)
const conversationScope = computed(() => `pm:${activeId.value || 'new'}`)
provideConversationArtifacts({ open(target) { artifactTarget.value = target; artifactOpen.value = true } })
watch(conversationScope, () => { artifactOpen.value = false; artifactTarget.value = null })
const citationUnread = ref(createCitationUnreadState())
watch(
  () => ({
    threadId: activeId.value || '',
    panelOpen: artifactOpen.value,
    loading: messagesLoading.value,
    messages: messages.value,
  }),
  (event) => {
    citationUnread.value = reduceArtifactCitationUnread(citationUnread.value, event)
  },
)
const artifactButtonLabel = computed(() => artifactButtonAriaLabel(artifactOpen.value, citationUnread.value.count, {
  open: t('pages.conversationWorkspace.openArtifacts'),
  close: t('pages.conversationWorkspace.closeArtifacts'),
  unread: (count) => t('pages.conversationWorkspace.openArtifactsUnread', { n: count }),
  cap: t('pages.conversationWorkspace.openArtifactsUnreadCap'),
}))
const workspace = ref<InstanceType<typeof ConversationWorkspace> | null>(null)
function toggleArtifacts() { workspace.value?.toggleFromArtifactIcon() }

const messageNavigation = computed(() => {
  const items: Array<{ id: string; title: string; preview: string }> = []
  for (const message of messages.value) {
    if (message.role === 'user' && !isChannelHint(message)) {
      items.push({ id: message.id, title: message.content?.trim() ? message.content : t('pages.chatNavigation.attachmentMessage'), preview: '' })
    } else if (message.role === 'assistant' && items.length) {
      const item = items[items.length - 1]!
      if (message.content?.trim()) item.preview = message.content
    }
  }
  const latest = items[items.length - 1]
  if (latest && showStreamBubble.value && streamText.value.trim()) latest.preview = streamText.value
  return items
})
function onNavigateMessage() { stickToBottom.value = false }
function showConversationHistory() { workspace.value?.close() }

</script>

<template>
  <div v-if="loading && !threads.length" class="flex min-h-0 flex-1 items-start justify-center pt-12" role="status">
    <Icon name="spinner" :size="20" class="text-txt3" />
    <span class="sr-only">{{ t('pages.workspace.pm.loadingHistory') }}</span>
  </div>

  <div v-else-if="threadsLoadFailed && !threads.length" class="scroll-area min-h-0 flex-1 overflow-y-auto" data-testid="pm-threads-load-failed">
    <div class="pm-entry-content">
      <h2 class="text-xl font-semibold tracking-tight text-txt">{{ t('pages.workspace.pm.threadsLoadFailed') }}</h2>
      <p class="mb-5 mt-2 text-sm leading-6 text-txt2">{{ t('pages.workspace.pm.threadsLoadFailedDesc') }}</p>
      <AppButton icon="refresh" @click="loadThreads">{{ t('pages.workspace.pm.retry') }}</AppButton>
    </div>
  </div>

  <section v-else-if="setupRequired" data-testid="pm-setup-state" class="scroll-area min-h-0 flex-1 overflow-y-auto">
    <div class="pm-entry-content">
      <p class="mb-3 text-xs font-medium text-txt3">{{ t('pages.workspace.pm.scopeLabel') }}</p>
      <h2 class="text-xl font-semibold tracking-tight text-txt">{{ t(enabled ? 'pages.workspace.pm.unavailableTitle' : 'pages.workspace.pm.disabledTitle') }}</h2>
      <p class="mt-3 max-w-lg text-sm leading-6 text-txt2">{{ t('pages.workspace.pm.disabledHint') }}</p>
      <p v-if="binding?.agentError" class="mt-4 text-sm leading-6 text-err" role="alert">{{ binding.agentError }}</p>
      <AppButton class="mt-6" variant="primary" icon="settings" data-testid="pm-setup-settings" @click="emit('openSettings')">{{ t('pages.workspace.pm.goSettings') }}</AppButton>
    </div>
  </section>

  <div v-else class="pm-chat-layout flex min-h-0 flex-1 overflow-hidden" :class="{ 'pm-desktop-layout': !isMobile }" data-testid="pm-chat-layout">
    <!-- left rail -->
    <aside
      v-if="threads.length && showThreadsAside && (isMobile || !artifactOpen)"
      data-testid="pm-threads-aside"
      class="pm-threads-aside flex min-h-0 shrink-0 flex-col"
      :class="isMobile ? 'w-full' : 'w-56'"
    >
      <div class="flex min-h-12 items-center justify-between gap-2 py-2">
        <span class="text-xs font-medium text-txt3">{{ t('pages.workspace.pm.threads') }}</span>
        <div class="flex items-center gap-1">
        <AppButton
          size="sm"
          variant="ghost"
          :disabled="turnBusy || !canCreateThread"
          :class="isMobile ? 'min-h-[44px] min-w-[44px]' : ''"
          @click="newThread"
        >{{ t('pages.workspace.pm.newThread') }}</AppButton>
        <AppButton v-if="isMobile" size="sm" variant="ghost" icon="settings" class="min-h-[44px]" @click="emit('openSettings')">{{ t('pages.workspace.pm.settings') }}</AppButton>
        </div>
      </div>
      <div class="pm-thread-list scroll-area min-h-0 flex-1 space-y-1 overflow-y-auto pb-3">
        <button
          v-for="th in threads"
          :key="th.id"
          type="button"
          class="pm-thread-item group flex w-full items-center gap-1 rounded-md px-2 text-left text-sm text-txt2 hover:bg-elevated hover:text-txt disabled:cursor-not-allowed disabled:opacity-50"
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
            <span class="min-w-0 truncate text-[13px]">{{ threadDisplayTitle(th) }}</span>
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
            >{{ t('pages.workspace.pm.unspoken') }}</span>
            <span
              v-else
              class="rounded-md inline-flex shrink-0 items-center border border-line bg-elevated px-1 text-[9px] font-bold tracking-wide leading-4 text-txt3"
              data-testid="pm-web-tag"
            >{{ t('pages.workspace.pm.channelBadgeWeb') }}</span>
          </span>
          <span
            v-if="!turnBusy && !isChannelThread(th)"
            class="hidden text-xs text-txt3 group-hover:inline"
            data-testid="pm-thread-delete"
            data-icon-motion-host
            :title="t('common.buttons.delete')"
            @click.stop="removeThread(th.id)"
          ><Icon name="close" :size="12" /></span>
        </button>
        <p v-if="!threads.length && !loading" class="px-2 py-4 text-xs text-txt3">
          {{ t('pages.workspace.pm.noThreads') }}
        </p>
      </div>
    </aside>

    <!-- main chat -->
    <ConversationWorkspace
      v-if="!threads.length || showChatSection"
      ref="workspace"
      v-model:open="artifactOpen"
      :title="t('pages.artifacts.title')"
      :scope-key="conversationScope"
      :show-toolbar="false"
      class="min-h-0 min-w-0 flex-1"
    >
    <template #masthead>
      <div
        class="flex min-h-12 shrink-0 items-center gap-2 border-b border-line/60 py-2"
        :class="isMobile ? 'min-h-[44px]' : ''"
      >
        <button
          v-if="isMobile && threads.length"
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
            >{{ t('pages.workspace.pm.unspoken') }}</span>
          </div>
          <div
            v-if="activeIsChannel"
            class="truncate text-[11px] text-txt3"
            data-testid="pm-channel-subtitle"
          >
            {{ channelSourceLine(activeThread) }}
          </div>
        </div>
        <span v-else class="min-w-0 flex-1 text-xs text-txt3">{{ t('pages.workspace.pm.scopeLabel') }}</span>
        <AppButton v-if="!isMobile && artifactOpen && threads.length" size="sm" variant="ghost" icon="chat" data-testid="pm-chat-show-threads" @click="showConversationHistory">{{ t('pages.workspace.pm.threads') }}</AppButton>
        <AppButton class="pm-header-action" size="sm" variant="ghost" data-testid="pm-chat-artifacts" :aria-expanded="artifactOpen" :aria-label="artifactButtonLabel" @click="toggleArtifacts"><span class="inline-flex items-center gap-[7px]"><ArtifactUnreadMark :count="citationUnread.count" :panel-open="artifactOpen"><Icon name="chat-artifacts" :size="16" aria-hidden="true" /></ArtifactUnreadMark>{{ t('pages.artifacts.title') }}</span></AppButton>
        <AppButton
          size="sm"
          variant="ghost"
          icon="settings"
          class="pm-header-action"
          data-testid="pm-chat-open-settings"
          :class="isMobile ? 'min-h-[44px] shrink-0' : ''"
          @click="emit('openSettings')"
        >
          {{ t('pages.workspace.pm.settings') }}
        </AppButton>
      </div>
    </template>
    <section
      data-testid="pm-chat-section"
      class="pm-chat-panel chat-thread flex min-h-0 min-w-0 flex-col"
      :class="isMobile ? 'w-full flex-1' : 'flex-1'"
    >
      <p v-if="!canCreateThread" class="shrink-0 border-b border-line/60 py-3 text-xs leading-5 text-txt2" data-testid="pm-availability-notice" role="status">
        {{ t(!enabled ? 'pages.workspace.pm.pausedHistoryHint' : 'pages.workspace.pm.defaultUnavailableHint') }}
      </p>
      <div class="pm-message-area relative flex min-h-0 flex-1 flex-col" :class="{ 'pm-message-area-empty': showEmptyHint && canContinueThread }">
      <ChatMessageRail :items="messageNavigation" :scroller="scroller" :scope-key="conversationScope" :has-more="hasMoreEarlier" :loading-earlier="historyLoading" @navigate="onNavigateMessage" @load-earlier="loadEarlier" />
      <div
        ref="scroller"
        data-conversation-scroll
        data-testid="pm-message-scroller"
        class="pm-message-scroller scroll-area min-h-0 flex-1 overflow-y-auto"
        :class="{ 'pm-message-scroller-empty': showEmptyHint && canContinueThread }"
        :aria-hidden="showEmptyHint && canContinueThread ? 'true' : undefined"
        @scroll="onScrollerScroll"
      >
        <ArtifactLoadingPane
          v-if="mainViewState === 'messagesLoading'"
          message-key="pages.workspace.pm.loadingHistory"
          data-testid="pm-messages-loading"
        />

        <div v-else-if="mainViewState === 'errorEmpty'" class="mx-auto w-full max-w-3xl py-10" data-testid="pm-messages-load-failed" role="status">
          <h2 class="text-base font-semibold text-txt">{{ t('pages.workspace.pm.loadFailed') }}</h2>
          <p class="mb-4 mt-2 text-sm leading-6 text-txt2">{{ t('pages.workspace.pm.loadFailedDesc') }}</p>
          <AppButton size="sm" icon="refresh" @click="retryLoadMessages">{{ t('pages.workspace.pm.retry') }}</AppButton>
        </div>

        <template v-else>
        <div class="mx-auto flex w-full max-w-[760px] flex-col gap-8">

        <div
          v-if="showHistoryTip"
          data-testid="pm-history-tip"
          class="min-h-[28px] py-1.5 text-center text-[11.5px]"
          :class="historyTipClass"
        >
          <span>{{ historyTipText }}</span>
          <button
            v-if="historyLoadFailed"
            type="button"
            class="ml-2 inline-flex min-h-9 items-center px-1 text-xs font-medium text-accent-2 underline-offset-4 hover:underline disabled:opacity-50 sm:min-h-0"
            :disabled="historyLoading"
            data-testid="pm-history-retry"
            @click="loadEarlier"
          >{{ t('pages.workspace.pm.retry') }}</button>
        </div>

        <template v-for="m in messages" :key="m.id">
          <div
            v-if="isChannelHint(m)"
            class="rounded-lg mx-auto flex max-w-[92%] items-start gap-2 self-center border border-warn/35 bg-warn/[0.08] px-3 py-2 text-[12px] text-txt"
            :data-msg-id="m.id"
            :data-chat-anchor="m.id"
            data-testid="pm-channel-hint"
          >
            <span
              class="rounded-md shrink-0 border border-warn/35 px-1.5 py-px text-[10px] font-bold tracking-wide text-warn"
            >
              {{ t('pages.workspace.pm.channelHintLabel') }}
            </span>
            <div class="min-w-0">
              <div>{{ m.content }}</div>
              <div class="mt-0.5 text-[11px] text-txt2">
                {{ t('pages.workspace.pm.channelHintMeta') }}
              </div>
            </div>
          </div>
          <div v-else-if="m.role === 'user'" class="flex gap-2.5 flex-row-reverse" :data-msg-id="m.id" :data-chat-message-id="m.id" :data-chat-anchor="m.id">
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
                    class="chat-file-chip rounded-lg flex max-w-[200px] items-center gap-2 border border-line bg-elevated px-2 py-1.5"
                    data-testid="pm-history-file-chip"
                    :title="attachmentDisplayName(im, ii)"
                  >
                    <Icon name="artifact" :size="16" class="shrink-0 text-txt2" aria-hidden="true" />
                    <span class="min-w-0 truncate text-[12px] text-txt">{{ attachmentDisplayName(im, ii) }}</span>
                  </div>
                </template>
              </div>
              <div
                v-if="m.content"
                class="chat-user-message px-4 py-2.5 whitespace-pre-wrap break-words"
              >
                {{ m.content }}
              </div>
              <div
                v-else-if="m.images?.length"
                class="chat-user-message px-4 py-2.5"
              >
                {{ t('pages.workspace.pm.imagesOnly') }}
              </div>
              <div class="chat-meta msg-time mt-1.5 text-right">{{ relTime(m.createdAt) }}</div>
            </div>
          </div>
          <div v-else-if="m.role === 'assistant'" class="flex gap-2.5" :data-msg-id="m.id" :data-chat-message-id="m.id" :data-chat-anchor="m.id">
            <div class="min-w-0 flex-1">
              <div
                data-assistant-bubble
                class="min-w-0"
              >
                <div class="md chat-prose" v-html="renderMarkdown(m.content)" />
                <div
                  v-if="m.content?.trim() || m.usage != null"
                  class="chat-meta msg-actions mt-2.5 flex flex-wrap items-center gap-3"
                >
                  <time class="msg-time" :datetime="m.createdAt">{{ relTime(m.createdAt) }}</time>
                  <TokenUsageByModelTip
                    v-if="m.usage != null"
                    :usage="m.usage"
                    :usage-by-model="m.usageByModel"
                    :unknown-model-display-name="unknownModelDisplayName"
                  />
                  <button
                    v-if="m.content?.trim()"
                    type="button"
                    class="pm-message-action inline-flex items-center gap-1 py-0.5"
                    @click="copyAssistantText"
                  >
                    <Icon name="copy" :size="12" />
                    {{ t('pages.workspace.pm.copyFull') }}
                  </button>
                </div>
              </div>
              <div v-if="m.citations?.length" class="mt-3 space-y-1">
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
            <div class="min-w-0 max-w-[85%]">
              <div class="rounded-lg border border-err/35 bg-err/[0.06] px-3 py-2 text-sm leading-6 text-txt">
                <div class="mb-1.5 flex items-center gap-1.5 text-[11px] font-medium text-err">
                  <Icon name="alert" :size="14" class="shrink-0" />
                  {{ t('pages.workspace.pm.failPartialKeptMeta') }}
                </div>
                <div class="md chat-prose" v-html="renderMarkdown(failedPartialByUserMsgId[m.id])" />
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
                  :disabled="busy || !canContinueThread"
                  data-testid="pm-fail-retry"
                  @click="retryTurn(m.id)"
                >
                  {{ t('pages.workspace.pm.retry') }}
                </button>
              </div>
            </div>
          </div>
        </template>

        <div v-if="showStreamBubble" class="flex gap-2.5" data-testid="pm-stream-bubble">
          <div class="min-w-0 flex-1">
            <div>
              <div v-if="resuming && !streamText && !finalizing" class="flex items-center gap-1.5 text-txt3">
                <Icon name="spinner" :size="13" class="text-accent-2" />
                {{ t('pages.workspace.pm.resuming') }}
              </div>
              <div v-else-if="streamText" class="md chat-prose" v-html="streamHtml" />
              <div v-else-if="showStreamTypingDots" class="chat-typing py-1" role="status">
                <i aria-hidden="true" /><i aria-hidden="true" /><i aria-hidden="true" />
                <span class="sr-only">{{ t('pages.workspace.pm.generating') }}</span>
              </div>
              <div v-else class="flex items-center gap-1.5 text-txt3">
                <Icon name="spinner" :size="13" class="text-accent-2" />
                {{ t('pages.workspace.pm.generating') }}
              </div>
              <div
                v-if="finalizing"
                class="mt-2 flex flex-wrap items-center gap-1.5 border-t border-line pt-2 text-[11px] text-warn"
                data-testid="pm-stream-finalizing-meta"
              >
                <template v-if="finalizingRefetchFailed">
                  <Icon name="alert" :size="13" class="text-warn" />
                  <span>{{ t('pages.workspace.pm.loadFailed') }}</span>
                  <button
                    type="button"
                    class="ml-1 text-accent underline-offset-2 hover:underline"
                    data-testid="pm-finalizing-retry"
                    @click="refetchAfterTurnDone"
                  >
                    {{ t('pages.workspace.pm.retry') }}
                  </button>
                </template>
                <template v-else>
                  <Icon name="spinner" :size="13" class="text-warn" />
                  {{ t('pages.workspace.pm.finalizing') }}
                </template>
              </div>
              <div
                v-else-if="resuming"
                class="mt-2 flex items-center gap-1.5 border-t border-line pt-2 text-[11px] text-accent-2"
                data-testid="pm-stream-resuming-meta"
              >
                <Icon name="spinner" :size="13" class="text-accent-2" />
                {{ t('pages.workspace.pm.resuming') }}
              </div>
            </div>
          </div>
        </div>

        <div
          v-if="showIdleSuggestions && canContinueThread"
          class="mt-1 flex flex-wrap gap-x-4 gap-y-2"
          data-testid="pm-idle-suggestions"
        >
          <button
            v-for="s in suggestions"
            :key="s"
            type="button"
            class="py-1 text-xs text-txt2 underline-offset-4 hover:text-txt hover:underline disabled:opacity-50"
            :disabled="busy"
            @click="send(s)"
          >
            {{ s }}
          </button>
        </div>
        </div>
        </template>
      </div>
      </div>

      <div v-if="activeIsChannel" class="shrink-0 border-t border-line/60 py-4" data-testid="pm-channel-readonly">
        <div class="mx-auto flex max-w-3xl items-start gap-2.5">
          <div
            class="rounded-full flex h-7 w-7 shrink-0 items-center justify-center border border-accent-2/35 bg-accent/15 text-accent-2"
            aria-hidden="true"
          >
            <Icon name="lock" :size="14" :interactive="false" />
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
      <div v-else-if="!canContinueThread" class="flex shrink-0 flex-wrap items-center justify-center gap-3 border-t border-line/60 py-4 text-center text-xs leading-5 text-txt2" data-testid="pm-history-readonly">
        <span>{{ t(enabled ? 'pages.workspace.pm.threadUnavailableHint' : 'pages.workspace.pm.pausedComposerHint') }}</span>
        <AppButton v-if="turnBusy" size="sm" icon="close" data-testid="pm-paused-stop" @click="stop">{{ t('pages.workspace.pm.stop') }}</AppButton>
      </div>
      <div v-else class="pm-composer-zone mx-auto w-full max-w-[760px] shrink-0" :class="showEmptyHint ? 'pm-composer-zone-welcome scroll-area' : ''" data-testid="pm-composer-zone">
        <div v-if="showEmptyHint" class="mb-5" data-testid="pm-welcome-state">
          <h2 class="text-2xl font-semibold tracking-tight text-txt">{{ t('pages.workspace.pm.welcomeTitle') }}</h2>
          <p class="mt-2 text-[13px] leading-6 text-txt2">{{ t('pages.workspace.pm.emptyHint') }}</p>
        </div>
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
              class="chat-file-chip rounded-lg flex h-14 max-w-[160px] items-center gap-1.5 border border-line bg-elevated px-2"
              data-testid="pm-pending-file-chip"
              :title="attachmentDisplayName(im, ii)"
            >
              <Icon name="artifact" :size="16" class="shrink-0 text-txt2" aria-hidden="true" />
              <span class="min-w-0 truncate text-[11px] text-txt2">{{ attachmentDisplayName(im, ii) }}</span>
            </div>
            <button
              type="button"
              class="absolute -right-1.5 -top-1.5 flex h-4 w-4 items-center justify-center pending-attach-remove"
              :disabled="busy"
              @click.stop="removeAttachment(ii)"
            >
              <Icon name="close" :size="9" />
            </button>
          </div>
        </div>
        <div class="chat-composer flex flex-col gap-2 p-2.5" data-testid="pm-composer">
          <input
            ref="fileInput"
            type="file"
            multiple
            class="hidden"
            @change="onPickFiles"
          />
          <textarea
            v-model="input"
            rows="2"
            class="chat-input scroll-area max-h-32 min-h-[40px] w-full min-w-0 resize-none px-2 py-1.5 outline-none disabled:opacity-50"
            :placeholder="t('pages.workspace.pm.inputPh')"
            :aria-label="t('pages.workspace.pm.inputPh')"
            :disabled="busy"
            @keydown.enter.exact.prevent="send()"
            @paste="onPaste"
          />
          <div class="chat-toolbar flex items-center justify-between gap-2">
          <button
            type="button"
            class="chat-action-button"
            :disabled="busy"
            data-testid="pm-chat-attach"
            :aria-label="t('pages.workspace.pm.images')"
            :title="t('pages.workspace.pm.images')"
            @click="fileInput?.click()"
          >
            <Icon name="plus" :size="18" />
            <span class="sr-only">{{ t('pages.workspace.pm.images') }}</span>
          </button>
          <button
            v-if="turnBusy"
            type="button"
            class="chat-action-button chat-action-stop"
            :aria-label="t('pages.workspace.pm.stop')"
            :title="t('pages.workspace.pm.stop')"
            @click="stop"
          >
            <Icon name="chat-stop" :size="16" />
            <span class="sr-only">{{ t('pages.workspace.pm.stop') }}</span>
          </button>
          <button
            v-else
            type="button"
            class="chat-action-button chat-action-primary"
            :disabled="!canSend"
            data-testid="pm-chat-send"
            :aria-label="t('pages.workspace.pm.send')"
            :title="t('pages.workspace.pm.send')"
            @click="send()"
          >
            <Icon name="chat-send" :size="18" />
            <span class="sr-only">{{ t('pages.workspace.pm.send') }}</span>
          </button>
          </div>
        </div>
        <ul v-if="showEmptyHint" class="mt-4 space-y-1" data-testid="pm-welcome-suggestions">
          <li v-for="(suggestion, index) in suggestions" :key="suggestion">
            <button
              type="button"
              class="flex min-h-10 w-full items-center gap-3 rounded-md px-2 py-2 text-left text-[13px] leading-5 text-txt2 transition-colors hover:bg-elevated/50 hover:text-txt focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent disabled:opacity-50"
              :disabled="busy"
              @click="send(suggestion)"
            >
              <Icon :name="suggestionIcons[index]!" :size="16" class="shrink-0 text-txt3" />
              <span class="min-w-0 flex-1">{{ suggestion }}</span>
              <Icon name="chevron-right" :size="14" class="shrink-0 text-txt3" />
            </button>
          </li>
        </ul>
        <p
          v-if="turnBusy"
          class="mt-2 text-[11px]"
          :class="isPullingBoot ? 'text-warn' : 'text-txt2'"
          data-testid="pm-busy-hint"
          :data-pulling="isPullingBoot ? '1' : '0'"
        >
          {{ busyHint }}
        </p>
      </div>
    </section>
    <template #panel>
      <ConversationArtifactBrowser :target="artifactTarget" />
    </template>
    </ConversationWorkspace>
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
      {{ t('pages.workspace.pm.channelViewDetail') }}
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
        <span>{{ t('pages.workspace.pm.channelDetailTitle') }}</span>
        <button
          type="button"
          class="px-2 text-txt2 hover:text-txt"
          :aria-label="t('pages.workspace.pm.channelDetailClose')"
          data-testid="pm-channel-detail-close"
          @click="closeChannelDetail"
        ><Icon name="close" :size="12" /></button>
      </div>
      <div class="flex flex-col gap-3 p-3.5">
        <div>
          <div class="mb-1 text-[11px] text-txt3">{{ t('pages.workspace.pm.channelDetailLabelTitle') }}</div>
          <div class="rounded-lg border border-line bg-base px-2.5 py-2 text-[13px] text-txt" data-testid="pm-channel-detail-title">
            {{ channelDetailTitle }}
          </div>
        </div>
        <div>
          <div class="mb-1 text-[11px] text-txt3">{{ t('pages.workspace.pm.channelDetailLabelSource') }}</div>
          <div class="rounded-lg border border-line bg-base px-2.5 py-2 text-[13px] text-txt" data-testid="pm-channel-detail-source">
            {{ channelDetailSource }}
          </div>
        </div>
      </div>
      <div class="flex justify-end border-t border-line px-3.5 py-2.5">
        <AppButton size="sm" variant="outline" data-testid="pm-channel-detail-ok" @click="closeChannelDetail">
          {{ t('pages.workspace.pm.channelDetailClose') }}
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
.pm-entry-content { width: 100%; max-width: 640px; margin-inline: auto; padding: 36px 0 48px; }
.pm-chat-layout { gap: 24px; }
.pm-header-action { min-height: 34px; border: 1px solid transparent; border-radius: 9px; padding-inline: 10px; }
.pm-header-action :deep(.app-icon) { width: 16px; height: 16px; }
.pm-header-action[aria-expanded="true"] { border-color: rgb(var(--c-accent) / .12); background: rgb(var(--c-accent-dim)); color: rgb(var(--c-accent-2)); }
.pm-desktop-layout .pm-threads-aside { padding-right: 16px; border-right: 1px solid rgb(var(--c-line) / .6); }
.pm-message-area-empty { display: none; }
.pm-message-scroller { padding: 28px 8px 28px 34px; scrollbar-gutter: stable; }
.pm-message-scroller-empty { display: none; }
.pm-composer-zone { padding: 12px 0 8px; }
.pm-message-action { color: inherit; border-radius: 4px; transition: color 140ms ease; }
.pm-message-action:hover { color: rgb(var(--c-accent-2)); }
.pm-message-action:focus-visible { outline: 2px solid rgb(var(--c-accent) / .4); outline-offset: 3px; }
.pm-composer-zone-welcome { min-height: 0; flex: 1 1 0%; max-width: 680px; padding-block: clamp(32px, 8vh, 80px) 32px; overflow-y: auto; scrollbar-gutter: stable; }
@media (min-width: 768px) and (max-width: 1199px) {
  .pm-desktop-layout { flex-direction: column; gap: 16px; }
  .pm-desktop-layout .pm-threads-aside { width: 100%; max-height: 148px; padding-right: 0; border-right: 0; border-bottom: 1px solid rgb(var(--c-line) / .6); }
  .pm-desktop-layout .pm-thread-list { display: flex; flex: none; flex-direction: row; gap: 4px; overflow-x: auto; overflow-y: hidden; }
  .pm-desktop-layout .pm-thread-item { width: 190px; flex: none; margin-top: 0; }
}
@media (max-width: 767px) {
  .pm-header-action { min-height: 44px; }
  .pm-chat-layout { gap: 0; }
  .pm-entry-content { padding-block: 24px 40px; }
  .pm-composer-zone-welcome { padding-top: 28px; }
  /* Keep the active composer above the shell's floating navigation control. */
  .pm-composer-zone:not(.pm-composer-zone-welcome) { padding-bottom: calc(64px + env(safe-area-inset-bottom, 0px)); }
}

</style>
