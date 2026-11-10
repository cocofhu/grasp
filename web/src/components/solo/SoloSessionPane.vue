<script setup lang="ts">
import ArtifactFileAttachments from '@/components/chat/ArtifactFileAttachments.vue'
import ChatFileAttachment from '@/components/chat/ChatFileAttachment.vue'
import { computed, nextTick, onBeforeUnmount, ref, shallowReactive, toRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import Icon from '@/components/ui/Icon.vue'
import AppButton from '@/components/ui/AppButton.vue'
import AppDrawer from '@/components/ui/AppDrawer.vue'
import ChatImageThumb from '@/components/ui/ChatImageThumb.vue'
import ChatImagePreviewModal from '@/components/ui/ChatImagePreviewModal.vue'
import AgentTimeline from '@/components/run/AgentTimeline.vue'
import ChatUserMessage from '@/components/run/ChatUserMessage.vue'
import ThinkingBusyCue from '@/components/run/ThinkingBusyCue.vue'
import ReactArtifactStage from '@/components/run/ReactArtifactStage.vue'
import ComposerPrimaryAction from '@/components/run/ComposerPrimaryAction.vue'
import type { soloEmbedClient } from '@/lib/solo/soloEmbedClient'
import { appendPreviewPick, readPreviewMessage, type AppPreviewPickPayload } from '@/lib/shared/previewPickUrl'
import ReviewShell from '@/components/run/ReviewShell.vue'
import { useChatTailScroll } from '@/lib/chat/chatScroll'
import ChatComposer from '@/components/chat/ChatComposer.vue'
import { useChatComposerDraft } from '@/lib/chat/useChatComposerDraft'
import ChatMessageRail from '@/components/chat/ChatMessageRail.vue'
import PendingSendQueuePanel, { type PendingQueueRow } from '@/components/run/PendingSendQueuePanel.vue'
import SoloStartupCard from './SoloStartupCard.vue'
import SoloLimitModal from './SoloLimitModal.vue'
import SoloModelSelect from './SoloModelSelect.vue'
import SoloWorkflowPanel from './SoloWorkflowPanel.vue'
import SoloWorkspaceSnapshots from './SoloWorkspaceSnapshots.vue'
import SoloEnvironmentRelease, { type EnvironmentReleaseDraft } from './SoloEnvironmentRelease.vue'
import { useWorkflowFeature } from '@/lib/composables/useWorkflowFeature'
import ReferenceChip from './ReferenceChip.vue'
import { api } from '@/lib/api/api'
import { githubRepoOf } from '@/lib/solo/referenceDisplay'
import { inlineReferences, referenceLabel } from '@/lib/solo/referenceTokens'
import { attachmentDisplayName, isImageAttachment } from '@/lib/shared/attachments'
import type { ClarifyImage, ReactAnnotation } from '@/lib/shared/types'
import { chatImageSrc } from '@/lib/shared/compositeText'
import { lastMessageText } from '@/lib/chat/messagePreview'
import { useChatImagePreview } from '@/lib/composables/useChatImagePreview'
import { partsOf } from '@/lib/solo/acpTurn'
import { useSoloChat } from '@/lib/solo/useSoloChat'
import { modelRouteValue } from '@/lib/agent/modelBindings'
import { useSessionModelCatalog } from '@/lib/solo/useSessionModelCatalog'
import { useSoloSessions } from '@/lib/solo/useSoloSessions'
import { sessionRepos } from '@/lib/solo/sessionRepos'
import { isSoloLimitError, soloErrorText } from '@/lib/solo/soloErrors'
import type { SoloResources } from '@/lib/api/apiTypes'
import type { MessageReference } from '@/lib/api/apiTypes'
import { useExecutionMode } from '@/lib/composables/useBrandSettings'

const props = defineProps<{ sessionId: string; embedded?: ReturnType<typeof soloEmbedClient>; workflowIntent?: string }>()
const emit = defineEmits<{ back: []; ready: []; workflowOpened: [] }>()
const { t, te, locale } = useI18n()
const { worktreeMode } = useExecutionMode()
const store = useSoloSessions()
const chat = useSoloChat(toRef(props, 'sessionId'), props.embedded)
const workflowPanel = ref<InstanceType<typeof SoloWorkflowPanel> | null>(null)
const { enabled: workflowsEnabled } = useWorkflowFeature()
watch([() => props.workflowIntent, workflowPanel], async ([intent, panel]) => {
  if (typeof intent !== 'string' || !panel || props.embedded) return
  if (workflowsEnabled.value) await panel.openPicker(intent === '1' ? '' : intent)
  emit('workflowOpened')
})
const { session, turns, queued, displayQueued, busy, phase, link, loading, loadError, actionError, pendingConfirmation, retryingSend, retryPendingSend, stopLocalRetry, status, closed, canSend, currentModelId, currentReasoningEffort, modelChangePending, setCurrentModel, setCurrentReasoningEffort } = chat
const modelCatalog = useSessionModelCatalog(computed(() => session.value?.agentName || ''), props.embedded?.modelCatalog)
const modelChoices = modelCatalog.choices
const modelCatalogReady = modelCatalog.ready
const modelCatalogBlocked = modelCatalog.blocked
const currentGroup = computed(() => session.value?.currentConfigurationId || session.value?.initialConfigurationId || modelCatalog.defaultConfigurationId?.value || '')
const currentSelection = computed(() => {
  const raw = currentModelId.value
  const group = currentGroup.value
  const choices = modelChoices.value
  const selected = choices.find(choice => (!group || choice.configurationId === group) && ((choice.modelId ?? choice.id) === raw || !raw))
  if (selected) return selected.id
  return group && choices.length ? modelRouteValue(group, raw) : raw
})
const selectionInvalid = computed(() => {
  if (!modelCatalog.configLoaded.value) return false
  if (modelCatalog.modelInConfiguration(currentModelId.value, currentGroup.value) === false) return true
  const selected = modelChoices.value.find(choice => choice.id === currentSelection.value)
  if (selected?.unavailable) return true
  if (!modelCatalog.catalogConfirmed.value) return false
  return !selected || selected.unavailable
})
const shownModels = computed(() => {
  let choices = modelChoices.value
  const selectedId = currentSelection.value
  if (!choices.some(choice => choice.id === selectedId) && selectedId) {
    choices = [...choices, { id: selectedId, name: currentModelId.value || session.value?.model || t('pages.solo.modelSelect.placeholder'), hint: currentModelId.value, vendor: '', backend: false, unavailable: selectionInvalid.value }]
  }
  const row = session.value
  if (!row || row.reasoningEffort === undefined) return choices
  return choices.map(choice => choice.id === selectedId ? { ...choice, efforts: modelChangePending.value ? [] : row.reasoningEfforts || [] } : choice)
})
function chooseSessionModel(value: string) {
  const choice = modelChoices.value.find(choice => choice.id === value) || (!value ? modelChoices.value[0] : undefined)
  const grouped = modelChoices.value.some(choice => choice.id.startsWith('@group:'))
  const changesGroup = !!choice?.configurationId && !!currentGroup.value && choice.configurationId !== currentGroup.value
  return setCurrentModel(choice?.configurationId && (grouped || changesGroup) ? modelRouteValue(choice.configurationId, choice.modelId || '') : value)
}
const repos = computed(() => sessionRepos(session.value))
const defaultRepo = computed(() => githubRepoOf(repos.value[0]?.repoUrl))
const draft = useChatComposerDraft(defaultRepo)
const { input, attach, mention, textareaComponent } = draft
const { preview: imagePreview, openChatImagePreview, closeChatImagePreview } = useChatImagePreview()

// A preview iframe may not have the login cookie. Fetch only attachments belonging
// to this session's messages with the drawer's existing bearer.
const transcriptImageURLs = shallowReactive(new Map<string, string>())
const attachmentRequests = new Set<string>()
const attachmentController = new AbortController()
const embedded = props.embedded
if (embedded) {
  watch(turns, (rows) => {
    for (const turn of rows) {
      if (turn.role !== 'user' || !turn.id.startsWith('u:')) continue
      for (const image of turn.images || []) {
        const id = image.ref?.startsWith('blob:') ? image.ref.slice(5) : ''
        if (!id || attachmentRequests.has(id)) continue
        attachmentRequests.add(id)
        void embedded.attachment(turn.id.slice(2), id, attachmentController.signal).then(blob => {
          if (!attachmentController.signal.aborted) transcriptImageURLs.set(id, URL.createObjectURL(blob))
        }).catch(() => undefined)
      }
    }
  })
  onBeforeUnmount(() => {
    attachmentController.abort()
    for (const url of transcriptImageURLs.values()) URL.revokeObjectURL(url)
  })
}
function transcriptImageSrc(image: ClarifyImage) {
  if (embedded && image.ref?.startsWith('blob:')) return transcriptImageURLs.get(image.ref.slice(5)) || ''
  return chatImageSrc(image)
}

const { scroller, followBottom, onScroll, scrollDown } = useChatTailScroll()
async function loadEarlier() {
  const top = scroller.value?.scrollTop || 0
  const height = scroller.value?.scrollHeight || 0
  followBottom.value = false
  await chat.loadEarlier()
  await nextTick()
  if (scroller.value) scroller.value.scrollTop = top + scroller.value.scrollHeight - height
}
const limitOpen = ref(false)
const environmentBusy = ref(false)
const stoppedEnvironment = computed(() => status.value === 'stopped')
const environmentStopPending = computed(() => session.value?.sandbox?.desiredState === 'stopped' && !stoppedEnvironment.value)
async function changeEnvironment(resume: boolean) {
  environmentBusy.value = true
  actionError.value = ''
  try {
    const value = await (resume ? api.resumeSoloEnvironment(props.sessionId) : api.stopSoloEnvironment(props.sessionId))
    store.upsert(value)
    await chat.load()
  } catch (cause) {
    actionError.value = soloErrorText(cause, t, te, locale.value)
  } finally { environmentBusy.value = false }
}
const restarting = ref(false)
const resources = ref<SoloResources>()
const workspaceSnapshotsOpen = ref(false)
const environmentReleaseOpen = ref(false)
const environmentReleaseDraft = ref<EnvironmentReleaseDraft | null>(null)
const workspaceViewKey = ref(0)

watch(session, (s) => { if (s && !props.embedded) store.upsert(s) })
watch(canSend, (ready) => { if (ready) emit('ready') })

const title = computed(() => session.value?.title || t('pages.solo.untitled'))
const editTargets = computed(() => {
  const seen = new Set<string>()
  const out: { id: string; name: string }[] = []
  for (const repo of repos.value) {
    const id = repo.projectId?.trim()
    if (!id || seen.has(id)) continue
    seen.add(id)
    out.push({ id, name: repo.name })
  }
  if (!out.length) {
    const id = session.value?.projectId?.trim()
    if (id) out.push({ id, name: session.value?.project?.name || '' })
  }
  return out
})
const sandboxId = computed(() => session.value?.sandboxId || session.value?.sandbox?.id || 0)
const sandboxResource = computed(() => {
  const source = session.value?.sandbox
  const id = Number(source?.id || session.value?.sandboxId || 0)
  return id > 0 ? { id, hasCodeServer: source?.hasCodeServer } : null
})
const sandboxResourceLoading = computed(() => status.value === 'starting' && !sandboxResource.value)
const showStartup = computed(() => status.value === 'starting' && !!session.value)

// --- transcript scroll ----------------------------------------------------
watch(turns, () => scrollDown(), { deep: true })

// --- message rail: one tick per user turn, previewing the last agent reply ----
const messageRailItems = computed(() => {
  const rows = turns.value
  return rows.flatMap((turn, index) => {
    if (turn.role !== 'user') return []
    let preview = ''
    for (let i = index + 1; i < rows.length && rows[i]!.role !== 'user'; i++) {
      const reply = rows[i]!
      if (reply.role !== 'agent') continue
      const text = lastMessageText(reply.steps, reply.text)
      if (text.trim()) preview = text
    }
    const message = readPreviewMessage(turn.text, turn.annotations)
    const title = message.text || turn.images?.map((im, ii) => attachmentDisplayName(im, ii)).join(', ')
      || message.annotations.map(pick => pick.label || pick.selector).join(', ')
    return [{ id: turn.id, title, preview }]
  })
})
function onMessageNavigate() {
  followBottom.value = false
}
watch(
  () => props.sessionId,
  () => {
    followBottom.value = true
    mention.clear()
    attach.clearAttachments()
    picks.value = []
    scrollDown(true)
  },
)

watch(
  [() => props.sessionId, () => session.value?.artifactRunId, status],
  ([id, runId, currentStatus], _, onCleanup) => {
    if (resources.value?.runId !== runId) resources.value = undefined
    if (!runId || props.embedded) return
    const controller = new AbortController()
    let loading = false
    async function refresh() {
      if (loading) return
      loading = true
      try {
        const next = await api.getSoloResources(id, controller.signal)
        if (!controller.signal.aborted && next.runId === runId) resources.value = next
      } catch {
        // Keep the last successful read across transient polling failures.
      } finally {
        loading = false
      }
    }
    void refresh()
    const timer = currentStatus === 'closed' ? undefined : window.setInterval(refresh, 2500)
    onCleanup(() => {
      controller.abort()
      window.clearInterval(timer)
    })
  },
  { immediate: true },
)

// --- composer ---------------------------------------------------------------
// --- @ references -------------------------------------------------------------
/** References whose `@label` is not in the text; the bubble shows the rest in place. */
function asideReferences(turn: { text?: string; references?: MessageReference[] }) {
  const refs = turn.references || []
  if (!refs.length || !turn.text) return refs
  return inlineReferences(turn.text, refs, (r) => referenceLabel(r, defaultRepo.value)).rest
}
/** Preview elements one Solo message can carry. */
const MAX_PICKS = 8
const picks = ref<ReactAnnotation[]>([])
const canSubmit = computed(() => !!input.value.trim() || !!attach.attachments.value.length || !!mention.refs.value.length || !!picks.value.length)

function addPick(payload: AppPreviewPickPayload) {
  appendPreviewPick(picks.value, payload, MAX_PICKS)
}
defineExpose({ addPick })

async function send() {
  const text = input.value.trim()
  const command = /^\/workflow(?:\s+(.*))?$/.exec(text)
  if (command) {
    if (!workflowsEnabled.value || props.embedded) { actionError.value = t('pages.solo.workflows.disabled'); return }
    await workflowPanel.value?.openPicker(command[1] || '')
    input.value = ''
    return
  }
  const images = attach.attachments.value.slice()
  const refs = mention.refs.value
  const selected = picks.value
  if (!canSubmit.value || !canSend.value || selectionInvalid.value) return
  if (attach.blockSendIfOversized(images)) return
  const restore = mention.snapshot()
  mention.clear()
  attach.clearAttachments()
  followBottom.value = true
  picks.value = []
  const ok = await chat.send(text, images, refs, selected)
  if (!ok && !pendingConfirmation?.value.length) {
    restore()
    attach.attachments.value = images
    picks.value = [...selected, ...picks.value].slice(0, MAX_PICKS)
  }
  scrollDown(true)
}

// --- queue ------------------------------------------------------------------
const queueRows = computed<PendingQueueRow[]>(() =>
  displayQueued.value.map((q) => ({ id: q.id, ...readPreviewMessage(q.text, q.annotations), images: q.images, locked: !!q.local })),
)

function cancelQueued(index: number) {
  const item = displayQueued.value[index]
  if (item && !item.local) void chat.removeQueued(item.id)
}

let editingQueued = false
/** Withdraw a queued message into the composer; an unsent draft is queued first. */
async function editQueued(index: number) {
  const target = displayQueued.value[index]
  if (!target || target.local || editingQueued) return
  editingQueued = true
  try {
    if (canSubmit.value) {
      await send()
      // A failed send restores its draft; a draft typed meanwhile stays with its author.
      if (canSubmit.value) return
    }
    if (!queued.value.some((q) => q.id === target.id)) return
    const item = await chat.takeQueued(target.id)
    if (!item) return
    // The item is already off the server queue, so keep anything typed during removal too.
    const itemMessage = readPreviewMessage(item.text, item.annotations)
    const draft = input.value.trim()
    const text = [itemMessage.text, draft].filter(Boolean).join('\n')
    picks.value = [...itemMessage.annotations, ...picks.value].slice(0, MAX_PICKS)
    mention.load(text, [...(item.references || []), ...mention.refs.value])
    attach.attachments.value = [...item.images, ...attach.attachments.value]
    void nextTick(() => textareaComponent.value?.resize())
  } finally {
    editingQueued = false
  }
}

async function doRestart() {
  restarting.value = true
  actionError.value = ''
  try {
    const v = await api.restartSoloSession(props.sessionId)
    store.upsert(v)
    await chat.load()
  } catch (e) {
    if (isSoloLimitError(e)) {
      limitOpen.value = true
      void store.refresh()
    } else actionError.value = soloErrorText(e, t, te, locale.value)
  } finally {
    restarting.value = false
  }
}

function turnInterrupted(turn: { status?: string; error?: string; streaming: boolean }) {
  return !turn.streaming && (turn.status === 'stopped' || turn.status === 'failed' || !!turn.error)
}
</script>

<template>
  <div class="chat-pane solo-pane flex min-h-0 min-w-0 flex-1 flex-col" data-testid="solo-session-pane">
    <div v-if="loading && !session" class="flex flex-1 items-center justify-center text-txt3">
      <Icon name="spinner" :size="18" />
    </div>
    <div v-else-if="loadError" class="flex flex-1 flex-col items-center justify-center gap-3 p-6 text-center">
      <Icon name="alert" :size="22" class="text-err" />
      <p class="text-[13px] text-txt2">{{ loadError }}</p>
      <AppButton size="sm" @click="chat.load()">{{ t('common.buttons.retry') }}</AppButton>
    </div>

    <ReviewShell
      v-else-if="session"
      :scope-key="`solo:${props.sessionId}:${session.artifactRunId || 'pending'}`"
      :run-id="session.artifactRunId || ''"
      :run-name="title"
      :chat-only="!!embedded"
      split-drawer
      remember-artifact-panel
      :panel-memory-key="`solo:${props.sessionId}`"
      class="solo-workspace min-h-0 min-w-0 flex-1"
    >
      <template #stage>
        <ReactArtifactStage
          :key="`${props.sessionId}:${workspaceViewKey}`"
          :artifacts="resources?.artifacts || []"
          :preview-artifact="resources?.previewArtifact"
          :registered-previews="status === 'running' ? resources?.ports || [] : []"
          :solo-session-id="props.sessionId"
          :run-id="session.artifactRunId || ''"
          node-id="solo"
          :sandbox="sandboxResource"
          :sandbox-loading="sandboxResourceLoading"
          :sandbox-log-id="sandboxId"
          class="min-h-0 flex-1"
        />
      </template>

      <template #sidebar>
      <div v-if="!embedded && !worktreeMode" class="flex shrink-0 flex-wrap items-center justify-end gap-2 border-b border-line px-4 py-2">
		<AppButton size="sm" variant="ghost" icon="folder" data-testid="solo-workspace-history" @click="workspaceSnapshotsOpen = true">{{ t('workspaceSnapshots.title') }}</AppButton>
		<AppButton v-if="status === 'closed' && sandboxId" size="sm" variant="ghost" data-testid="solo-release-environment" @click="environmentReleaseOpen = true">{{ t('workspaceRelease.title') }}</AppButton>
        <span v-if="stoppedEnvironment" class="mr-auto text-xs text-txt3">{{ t('pages.solo.environment.stopped') }}</span>
        <span v-else-if="environmentStopPending" class="mr-auto text-xs text-txt3">{{ t('pages.solo.environment.stopPending') }}</span>
        <AppButton v-if="stoppedEnvironment" size="sm" variant="outline" icon="play" :loading="environmentBusy" @click="changeEnvironment(true)">{{ t('pages.solo.environment.resume') }}</AppButton>
        <AppButton v-else-if="status !== 'closed'" size="sm" variant="ghost" icon="stop" :disabled="busy || queued.length > 0 || status === 'starting'" :loading="environmentBusy" :title="t('pages.solo.environment.retained')" data-testid="solo-stop-environment" @click="changeEnvironment(false)">{{ t(environmentStopPending ? 'pages.solo.environment.retryStop' : 'pages.solo.environment.stop') }}</AppButton>
      </div>
      <div v-if="link === 'reconnecting'" class="solo-banner" role="status">
        <Icon name="spinner" :size="12" />{{ t('pages.solo.link.reconnecting') }}
      </div>

      <!-- transcript -->
      <div class="relative flex min-h-0 flex-1 flex-col">
      <ChatMessageRail
        :items="messageRailItems"
        :scroller="scroller"
        :scope-key="`solo:${props.sessionId}`"
        @navigate="onMessageNavigate"
      />
      <div ref="scroller" class="chat-pane-transcript scroll-area min-h-0 flex-1 overflow-y-auto" data-testid="solo-transcript" @scroll.passive="onScroll">
        <div class="chat-pane-column space-y-6">
          <SoloStartupCard v-if="showStartup" :session="session" />
          <SoloWorkflowPanel v-if="!embedded" ref="workflowPanel" :session-id="props.sessionId" :sandbox-id="sandboxId" :busy="busy || queued.length > 0" @changed="chat.refreshSession()" @binding-changed="workspaceViewKey++" />

          <div v-if="status === 'error'" class="solo-errcard" data-testid="solo-error-card">
            <div class="flex items-start gap-2">
              <Icon name="alert" :size="15" class="mt-0.5 shrink-0 text-err" />
              <div class="min-w-0 flex-1">
                <div class="text-[13px] font-medium text-txt">{{ t('pages.solo.errorCard.title') }}</div>
                <p class="mt-1 whitespace-pre-wrap break-words text-[12px] text-txt2">{{ session.error || t('pages.solo.errorCard.unknown') }}</p>
              </div>
            </div>
            <div v-if="!embedded" class="mt-3 flex flex-wrap gap-2">
              <AppButton size="sm" variant="primary" icon="refresh" :loading="restarting" @click="doRestart">{{ t('pages.solo.errorCard.retry') }}</AppButton>
              <RouterLink v-if="sandboxId && !worktreeMode" :to="`/sandboxes/${sandboxId}/console`" class="solo-error-link">
                <Icon name="terminal" :size="14" />{{ t('pages.solo.errorCard.logs') }}
              </RouterLink>
              <RouterLink
                v-for="target in editTargets"
                :key="target.id"
                :to="`/settings/projects?edit=${target.id}`"
                class="solo-error-link"
                :data-testid="`solo-error-edit-${target.id}`"
              >
                <Icon name="settings" :size="14" />
                {{ editTargets.length > 1 && target.name ? t('pages.solo.errorCard.editRepo', { name: target.name }) : t('pages.solo.errorCard.edit') }}
              </RouterLink>
            </div>
          </div>

          <div v-if="chat.hasEarlier.value" class="flex justify-center py-2">
            <AppButton size="sm" variant="ghost" icon="history" :loading="chat.loadingEarlier.value" data-testid="solo-load-earlier" @click="loadEarlier">{{ t('pages.solo.chat.loadEarlier') }}</AppButton>
          </div>
          <p v-if="chat.earlierError.value" class="text-xs text-err" role="alert">{{ chat.earlierError.value }}</p>
          <template v-for="turn in turns" :key="turn.id">
            <div v-if="turn.role === 'system'" class="solo-system">
              <span>{{ turn.text }}</span>
            </div>
            <div v-else-if="turn.role === 'user'" class="flex justify-end" :data-chat-message-id="turn.id">
              <div class="min-w-0 max-w-[88%] space-y-1.5">
                <div v-if="turn.images?.length" class="flex flex-wrap justify-end gap-1.5">
                  <template v-for="(im, ii) in turn.images" :key="ii">
                    <ChatImageThumb
                      v-if="isImageAttachment(im) && im.readState !== 'reading'"
                      mode="previewable"
                      size="md"
                      thumb-class="rounded-md"
                      :src="transcriptImageSrc(im)"
                      :label="attachmentDisplayName(im, ii)"
                      :alt="attachmentDisplayName(im, ii)"
                      @preview="openChatImagePreview(transcriptImageSrc(im), attachmentDisplayName(im, ii))"
                    />
                    <ChatFileAttachment v-else :attachment="im" :src="transcriptImageSrc(im)" />
                  </template>
                </div>
                <div v-if="asideReferences(turn).length" class="flex flex-wrap justify-end gap-1.5" data-testid="solo-turn-references">
                  <ReferenceChip v-for="r in asideReferences(turn)" :key="r.id || r.target" :reference="r" :default-repo="defaultRepo" link />
                </div>
                <ChatUserMessage v-if="turn.text || turn.annotations?.length" :text="turn.text" :annotations="turn.annotations" :created-at="turn.createdAt" :references="turn.references" :default-repo="defaultRepo" />
              </div>
            </div>
            <div v-else class="min-w-0 space-y-3">
              <AgentTimeline
                v-if="turn.steps.length || !turn.streaming"
                :parts="partsOf(turn)"
                :streaming="turn.streaming"
                :completed="!turn.streaming && !turnInterrupted(turn)"
                :interrupted="turnInterrupted(turn)"
                :started-at="turn.startedAt"
                :ended-at="turn.endedAt"
                :metadata="turn"
                quiet-thought
                message-test-id="solo-agent-message"
              />
              <div v-if="turn.historyMessageId && turn.processAvailable" class="space-y-1">
                <AppButton
                  v-if="chat.processPages.value[turn.historyMessageId]?.hasMore !== false"
                  size="sm" variant="ghost" icon="list"
                  :loading="chat.processPages.value[turn.historyMessageId]?.loading"
                  data-testid="solo-load-process"
                  @click="chat.loadProcess(turn)"
                >{{ t(chat.processPages.value[turn.historyMessageId]?.next !== undefined && chat.processPages.value[turn.historyMessageId]!.next >= 0 ? 'pages.solo.chat.loadMoreProcess' : 'pages.solo.chat.loadProcess') }}</AppButton>
                <p v-if="chat.processPages.value[turn.historyMessageId]?.error" class="text-xs text-err" role="alert">{{ chat.processPages.value[turn.historyMessageId]!.error }}</p>
                <p v-if="chat.processPages.value[turn.historyMessageId]?.legacyIncomplete" class="chat-meta">{{ t('pages.solo.chat.legacyIncomplete') }}</p>
              </div>
              <ArtifactFileAttachments :files="turn.files" />
              <div v-if="turn.plan.length" class="border-l border-line/70 pl-3 text-[12px] leading-6">
                <div class="chat-meta mb-1">{{ t('pages.solo.chat.plan') }}</div>
                <div v-for="(p, pi) in turn.plan" :key="pi" class="flex items-center gap-2 text-txt2">
                  <Icon :name="p.status === 'completed' ? 'check' : 'clock'" :size="13" :class="p.status === 'completed' ? 'text-ok' : 'text-txt3'" />
                  <span :class="p.status === 'completed' ? 'text-txt3 line-through' : ''">{{ p.content }}</span>
                </div>
              </div>
              <div
                v-if="turn.streaming"
                class="chat-meta grasp-status-host flex items-center gap-2 py-1"
                data-testid="solo-agent-activity"
                role="status"
              >
                <ThinkingBusyCue :label="pendingConfirmation?.includes(turn.id.replace(/^live:/, '')) ? t('pages.solo.phase.confirming') : phase === 'preparing' && !turn.steps.length ? t('pages.solo.phase.preparing') : t('pages.solo.phase.running')" />
              </div>
              <div v-if="turn.status === 'stopped' && !turn.streaming" class="chat-meta">{{ t('pages.solo.chat.stopped') }}</div>
              <div v-else-if="turn.error || turn.status === 'failed'" class="rounded-md border border-err/30 bg-err/10 px-3 py-2 text-[12px] text-err">{{ turn.error || t('pages.solo.chat.turnFailed') }}</div>
            </div>
          </template>
        </div>
      </div>

      <!-- queue -->
      <div v-if="queued.length && !closed" class="chat-pane-composer shrink-0 pt-3" data-testid="solo-queue">
        <div class="chat-pane-column">
          <PendingSendQueuePanel
            :items="queueRows"
            panel-test-id="solo-queue-panel"
            @cancel="cancelQueued"
            @edit="editQueued"
            @reorder="(from: number, to: number) => chat.reorderQueued(from, to)"
          >
            <template #row-extra="{ index }">
              <span v-if="displayQueued[index]?.references?.length" class="flex shrink-0 gap-1">
                <ReferenceChip v-for="r in displayQueued[index]!.references" :key="r.target" :reference="r" :default-repo="defaultRepo" />
              </span>
            </template>
          </PendingSendQueuePanel>
        </div>
      </div>
      </div>

      <!-- composer / closed bar -->
      <div v-if="closed || stoppedEnvironment" class="chat-pane-composer py-3" data-testid="solo-closed-bar">
        <div class="chat-pane-column solo-closed">
          <Icon name="lock" :size="15" class="mt-0.5 shrink-0 text-txt3" />
          <p class="min-w-0 flex-1 text-[12.5px] leading-relaxed text-txt2">{{ t(stoppedEnvironment ? 'pages.solo.environment.stopped' : 'pages.solo.closedBar.text') }}</p>
          <AppButton v-if="!embedded" size="sm" variant="primary" icon="refresh" :loading="restarting || environmentBusy" @click="stoppedEnvironment ? changeEnvironment(true) : doRestart()">{{ t(stoppedEnvironment ? 'pages.solo.environment.resume' : 'pages.solo.closedBar.restart') }}</AppButton>
        </div>
        <p v-if="actionError" class="chat-pane-column mt-2 text-xs text-err" role="alert">{{ actionError }}</p>
      </div>
      <div v-else class="chat-pane-composer shrink-0 py-3">
        <div class="chat-pane-column">
          <p v-if="actionError" class="mb-2 text-[12px] text-err" role="alert">{{ actionError }}</p>
          <AppButton v-if="pendingConfirmation?.length" size="sm" variant="outline" icon="refresh" class="mb-2" :loading="retryingSend" data-testid="solo-retry-unconfirmed" @click="retryPendingSend">{{ t('pages.solo.retryUnconfirmed') }}</AppButton>
          <AppButton v-if="pendingConfirmation?.length" size="sm" variant="ghost" class="mb-2 ml-2" :disabled="retryingSend" :title="t('pages.solo.stopLocalRetryHint')" data-testid="solo-stop-local-retry" @click="stopLocalRetry">{{ t('pages.solo.stopLocalRetry') }}</AppButton>
          <p v-if="selectionInvalid && canSend" class="mb-2 text-[12px] text-warn" role="status" data-testid="solo-model-invalid-notice">{{ t('pages.solo.modelSelect.unavailableSelection') }}</p>
          <p v-if="modelCatalogBlocked && canSend" class="mb-2 text-[12px] text-txt2" role="status" data-testid="solo-model-catalog-notice">{{ t('pages.solo.modelSelect.catalogFailed') }}</p>
          <ChatComposer
            :draft="draft"
            :picks="picks"
            :placeholder="t('pages.solo.chat.placeholder', { agent: session.agentName })"
            :scope="{ sessionId: props.sessionId }"
            :scoped-search="embedded?.searchSoloReferences"
            :disabled="!canSend"
            test-prefix="solo"
            pick-test-id="solo-preview-pick"
            @send="send"
            @remove-pick="picks.splice($event, 1)"
            @preview="openChatImagePreview"
          >
            <template #model>
              <AppButton v-if="workflowsEnabled && !embedded" size="sm" variant="ghost" class="font-mono text-xs" data-testid="solo-workflow-menu" @click="workflowPanel?.openPicker()">/workflow</AppButton>
              <SoloModelSelect
                class="min-w-0"
                flat
                :model-value="currentSelection"
                :reasoning-effort="currentReasoningEffort"
                :models="shownModels"
                :disabled="!canSend || !modelCatalogReady"
                :empty-as-label="modelCatalogReady && !modelChoices.length"
                @update:model-value="chooseSessionModel"
                @update:reasoning-effort="setCurrentReasoningEffort"
              />
              <span v-if="status === 'starting'" class="truncate text-[11.5px] text-txt3">{{ t('pages.solo.chat.waitingSandbox') }}</span>
            </template>
            <template #actions>
              <ComposerPrimaryAction
                v-if="busy"
                kind="stop"
                :label="t('pages.solo.chat.stop')"
                testid="solo-stop"
                @click="chat.cancel()"
              />
              <ComposerPrimaryAction
                kind="send"
                :disabled="!canSend || selectionInvalid || !canSubmit"
                :label="t('pages.solo.chat.send')"
                testid="solo-send"
                @click="send"
              />
            </template>
          </ChatComposer>
        </div>
      </div>

      <AppDrawer :open="workspaceSnapshotsOpen" :title="t('workspaceSnapshots.title')" :width="620" @close="workspaceSnapshotsOpen = false"><SoloWorkspaceSnapshots v-if="workspaceSnapshotsOpen" :key="props.sessionId" :session-id="props.sessionId" :sandbox-id="sandboxId" :busy="busy || queued.length > 0" @changed="chat.refreshSession()" @binding-changed="workspaceViewKey++" /></AppDrawer>
      <AppDrawer :open="environmentReleaseOpen" :title="t('workspaceRelease.title')" :width="480" @close="environmentReleaseOpen = false"><SoloEnvironmentRelease v-if="environmentReleaseOpen" :key="props.sessionId" v-model:draft="environmentReleaseDraft" :session-id="props.sessionId" @changed="chat.load()" /></AppDrawer>

      <SoloLimitModal :open="limitOpen" @close="limitOpen = false" @freed="limitOpen = false; doRestart()" />
      <ChatImagePreviewModal :open="!!imagePreview" :src="imagePreview?.src || ''" :label="imagePreview?.label || ''" @close="closeChatImagePreview" />
      </template>
    </ReviewShell>
  </div>
</template>

<style scoped>
.solo-pane {
}
.solo-workspace {
  background: rgb(var(--c-surface));
}
.solo-error-link {
  display: inline-flex;
  min-height: 30px;
  align-items: center;
  gap: 5px;
  border: 1px solid rgb(var(--c-line));
  border-radius: 8px;
  padding: 0 9px;
  color: rgb(var(--c-txt2));
  font-size: 12px;
}
.solo-error-link:hover {
  border-color: rgb(var(--c-accent) / .35);
  color: rgb(var(--c-txt));
}
.solo-banner {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  border-bottom: 1px solid rgb(var(--c-warn) / 0.3);
  background: rgb(var(--c-warn) / 0.08);
  padding: 4px 12px;
  color: rgb(var(--c-warn));
  font-size: 11.5px;
}
.solo-errcard {
  border: 1px solid rgb(var(--c-err) / 0.3);
  border-radius: 14px;
  background: rgb(var(--c-err) / 0.05);
  padding: 14px 16px;
}
.solo-system {
  display: flex;
  align-items: center;
  gap: 10px;
  color: rgb(var(--c-txt3));
  font-size: 11.5px;
}
.solo-system::before,
.solo-system::after {
  height: 1px;
  flex: 1;
  background: rgb(var(--c-line));
  content: '';
}
.solo-closed {
  display: flex;
  align-items: center;
  gap: 10px;
  border: 1px dashed rgb(var(--c-line));
  border-radius: 12px;
  padding: 12px 14px;
}
</style>
