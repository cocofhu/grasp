<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, toRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink, useRouter } from 'vue-router'
import Icon from '@/components/ui/Icon.vue'
import AppButton from '@/components/ui/AppButton.vue'
import AppModal from '@/components/ui/AppModal.vue'
import ChatImageThumb from '@/components/ui/ChatImageThumb.vue'
import ChatImagePreviewModal from '@/components/ui/ChatImagePreviewModal.vue'
import AgentTimeline from '@/components/run/AgentTimeline.vue'
import ChatUserMessage from '@/components/run/ChatUserMessage.vue'
import ThinkingBusyCue from '@/components/run/ThinkingBusyCue.vue'
import ReactArtifactStage from '@/components/run/ReactArtifactStage.vue'
import ReviewShell from '@/components/run/ReviewShell.vue'
import ChatMessageRail from '@/components/chat/ChatMessageRail.vue'
import SoloStartupCard from './SoloStartupCard.vue'
import SoloLimitModal from './SoloLimitModal.vue'
import SoloModelSelect from './SoloModelSelect.vue'
import { api } from '@/lib/api/api'
import { attachmentDisplayName, isImageAttachment } from '@/lib/shared/attachments'
import { chatImageSrc } from '@/lib/shared/compositeText'
import { lastMessageText } from '@/lib/chat/messagePreview'
import { useImageAttachments } from '@/lib/composables/useImageAttachments'
import { useChatImagePreview } from '@/lib/composables/useChatImagePreview'
import { partsOf } from '@/lib/solo/acpTurn'
import { useSoloChat } from '@/lib/solo/useSoloChat'
import { modelRouteValue } from '@/lib/agent/modelBindings'
import { useSessionModelCatalog } from '@/lib/solo/useSessionModelCatalog'
import { useSoloSessions } from '@/lib/solo/useSoloSessions'
import { sessionRepos } from '@/lib/solo/sessionRepos'
import { isSoloLimitError, soloErrorText } from '@/lib/solo/soloErrors'
import type { Artifact } from '@/lib/shared/types'
import { useExecutionMode } from '@/lib/composables/useBrandSettings'

const props = defineProps<{ sessionId: string }>()
const emit = defineEmits<{ (e: 'back'): void }>()
const { t, te, locale } = useI18n()
const { worktreeMode } = useExecutionMode()
const router = useRouter()
const store = useSoloSessions()
const chat = useSoloChat(toRef(props, 'sessionId'))
const { session, turns, queued, busy, phase, link, loading, loadError, actionError, status, closed, canSend, currentModelId, currentReasoningEffort, modelChangePending, setCurrentModel, setCurrentReasoningEffort } = chat
const modelCatalog = useSessionModelCatalog(computed(() => session.value?.agentName || ''))
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
const attach = useImageAttachments()
const fileInput = attach.fileInput
const { preview: imagePreview, openChatImagePreview, closeChatImagePreview } = useChatImagePreview()

const input = ref('')
const composing = ref(false)
const scroller = ref<HTMLElement | null>(null)
const textarea = ref<HTMLTextAreaElement | null>(null)
const followBottom = ref(true)
const confirmClose = ref(false)
const confirmDelete = ref(false)
const moreOpen = ref(false)
const limitOpen = ref(false)
const closing = ref(false)
const restarting = ref(false)
const soloArtifacts = ref<Artifact[]>([])
let artifactPoll: number | undefined

watch(session, (s) => s && store.upsert(s))

const title = computed(() => session.value?.title || t('pages.solo.untitled'))
const repos = computed(() => sessionRepos(session.value))
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
function scrollDown(force = false) {
  void nextTick(() => {
    const el = scroller.value
    if (el && (force || followBottom.value)) el.scrollTop = el.scrollHeight
  })
}
function onScroll() {
  const el = scroller.value
  if (el) followBottom.value = el.scrollHeight - el.scrollTop - el.clientHeight < 80
}
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
    const title = turn.text || turn.images?.map((im, ii) => attachmentDisplayName(im, ii)).join(', ') || ''
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
    input.value = ''
    attach.clearAttachments()
    soloArtifacts.value = []
    scrollDown(true)
  },
)

async function loadSoloArtifacts() {
  const runId = session.value?.artifactRunId
  if (!runId) {
    soloArtifacts.value = []
    return
  }
  try {
    const next = await api.runArtifacts(runId)
    if (session.value?.artifactRunId === runId) soloArtifacts.value = next
  } catch {
    if (session.value?.artifactRunId === runId) soloArtifacts.value = []
  }
}

watch(
  [() => session.value?.artifactRunId, status],
  ([runId, currentStatus], _, onCleanup) => {
    if (artifactPoll) window.clearInterval(artifactPoll)
    artifactPoll = undefined
    void loadSoloArtifacts()
    if (!runId || currentStatus === 'closed') return
    artifactPoll = window.setInterval(() => void loadSoloArtifacts(), 2500)
    onCleanup(() => {
      if (artifactPoll) window.clearInterval(artifactPoll)
      artifactPoll = undefined
    })
  },
  { immediate: true },
)

// --- composer ---------------------------------------------------------------
function autoGrow() {
  const el = textarea.value
  if (!el) return
  el.style.height = 'auto'
  el.style.height = `${Math.min(Math.max(el.scrollHeight, 48), 220)}px`
}
watch(input, () => nextTick(autoGrow))

async function send() {
  const text = input.value.trim()
  const images = attach.attachments.value.slice()
  if ((!text && !images.length) || !canSend.value || selectionInvalid.value) return
  if (attach.blockSendIfOversized(images)) return
  input.value = ''
  attach.clearAttachments()
  followBottom.value = true
  const ok = await chat.send(text, images)
  if (!ok) {
    input.value = text
    attach.attachments.value = images
  }
  scrollDown(true)
}

function onKeydown(e: KeyboardEvent) {
  if (e.key !== 'Enter' || e.shiftKey) return
  if (composing.value || e.isComposing || e.keyCode === 229) return
  e.preventDefault()
  void send()
}

async function doClose() {
  closing.value = true
  const ok = await chat.close()
  closing.value = false
  if (ok) confirmClose.value = false
}

async function pushThenStay() {
  confirmClose.value = false
  input.value = ''
  await chat.send(t('pages.solo.close.pushPrompt'))
  scrollDown(true)
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

async function doDelete() {
  confirmDelete.value = false
  try {
    await api.deleteSoloSession(props.sessionId)
    store.remove(props.sessionId)
    void router.push('/dashboard')
  } catch (e) {
    actionError.value = soloErrorText(e, t, te, locale.value)
  }
}

function onDocPointer(e: Event) {
  if (!moreOpen.value) return
  const el = e.target as HTMLElement
  if (!el.closest?.('[data-solo-more]')) moreOpen.value = false
}
onMounted(() => document.addEventListener('pointerdown', onDocPointer))
onBeforeUnmount(() => document.removeEventListener('pointerdown', onDocPointer))
onBeforeUnmount(() => {
  if (artifactPoll) window.clearInterval(artifactPoll)
  artifactPoll = undefined
})

function turnInterrupted(turn: { status?: string; error?: string; streaming: boolean }) {
  return !turn.streaming && (turn.status === 'stopped' || turn.status === 'failed' || !!turn.error)
}
</script>

<template>
  <div class="solo-pane flex min-h-0 min-w-0 flex-1 flex-col" data-testid="solo-session-pane">
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
      split-drawer
      remember-artifact-panel
      :panel-memory-key="`solo:${props.sessionId}`"
      class="solo-workspace min-h-0 min-w-0 flex-1"
    >
      <template #stage>
        <ReactArtifactStage
          :artifacts="soloArtifacts"
          :run-id="session.artifactRunId || ''"
          :sandbox="sandboxResource"
          :sandbox-loading="sandboxResourceLoading"
          :sandbox-log-id="sandboxId"
          class="min-h-0 flex-1"
        />
      </template>

      <template #sidebar>
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
      <div ref="scroller" class="solo-transcript scroll-area min-h-0 flex-1 overflow-y-auto" data-testid="solo-transcript" @scroll.passive="onScroll">
        <div class="solo-column space-y-6">
          <SoloStartupCard v-if="showStartup" :session="session" />

          <div v-if="status === 'error'" class="solo-errcard" data-testid="solo-error-card">
            <div class="flex items-start gap-2">
              <Icon name="alert" :size="15" class="mt-0.5 shrink-0 text-err" />
              <div class="min-w-0 flex-1">
                <div class="text-[13px] font-medium text-txt">{{ t('pages.solo.errorCard.title') }}</div>
                <p class="mt-1 whitespace-pre-wrap break-words text-[12px] text-txt2">{{ session.error || t('pages.solo.errorCard.unknown') }}</p>
              </div>
            </div>
            <div class="mt-3 flex flex-wrap gap-2">
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

          <template v-for="turn in turns" :key="turn.id">
            <div v-if="turn.role === 'system'" class="solo-system">
              <span>{{ turn.text }}</span>
            </div>
            <div v-else-if="turn.role === 'user'" class="flex justify-end" :data-chat-message-id="turn.id">
              <div class="min-w-0 max-w-[88%] space-y-1.5">
                <div v-if="turn.images?.length" class="flex flex-wrap justify-end gap-1.5">
                  <template v-for="(im, ii) in turn.images" :key="ii">
                    <ChatImageThumb
                      v-if="isImageAttachment(im)"
                      mode="previewable"
                      size="md"
                      thumb-class="rounded-md"
                      :src="chatImageSrc(im)"
                      :label="attachmentDisplayName(im, ii)"
                      :alt="attachmentDisplayName(im, ii)"
                      @preview="openChatImagePreview(chatImageSrc(im), attachmentDisplayName(im, ii))"
                    />
                    <div v-else class="chat-file-chip flex max-w-[200px] items-center gap-2 rounded-md border border-line bg-elevated px-2 py-1.5">
                      <Icon name="artifact" :size="15" class="shrink-0 text-txt2" />
                      <span class="min-w-0 truncate text-[12px] text-txt">{{ attachmentDisplayName(im, ii) }}</span>
                    </div>
                  </template>
                </div>
                <ChatUserMessage v-if="turn.text" :text="turn.text" :created-at="turn.createdAt" />
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
                <ThinkingBusyCue :label="phase === 'preparing' && !turn.steps.length ? t('pages.solo.phase.preparing') : t('pages.solo.phase.running')" />
              </div>
              <div v-if="turn.status === 'stopped' && !turn.streaming" class="chat-meta">{{ t('pages.solo.chat.stopped') }}</div>
              <div v-else-if="turn.error || turn.status === 'failed'" class="rounded-md border border-err/30 bg-err/10 px-3 py-2 text-[12px] text-err">{{ turn.error || t('pages.solo.chat.turnFailed') }}</div>
            </div>
          </template>
        </div>
      </div>

      <!-- queue -->
      <div v-if="queued.length && !closed" class="solo-bottom border-t border-line py-2" data-testid="solo-queue">
        <div class="solo-column">
          <div class="chat-meta mb-1 flex items-center gap-1.5"><Icon name="clock" :size="13" />{{ t('pages.solo.chat.queue', { n: queued.length }) }}</div>
          <div v-for="(q, qi) in queued" :key="q.id" class="group flex min-w-0 items-center gap-2 py-1 text-[12px] text-txt2">
            <span class="chat-meta flex h-5 w-5 shrink-0 items-center justify-center rounded-full border border-line">{{ qi + 1 }}</span>
            <span class="min-w-0 flex-1 truncate">{{ q.text || t('pages.solo.chat.imagesOnly') }}</span>
            <button
              v-if="!q.local"
              type="button"
              class="shrink-0 text-txt3 opacity-0 transition group-hover:opacity-100 hover:text-err focus-visible:opacity-100"
              :aria-label="t('pages.solo.chat.removeQueued')"
              :title="t('pages.solo.chat.removeQueued')"
              @click="chat.removeQueued(q.id)"
            >
              <Icon name="close" :size="13" />
            </button>
          </div>
        </div>
      </div>
      </div>

      <!-- composer / closed bar -->
      <div v-if="closed" class="solo-bottom py-3" data-testid="solo-closed-bar">
        <div class="solo-column solo-closed">
          <Icon name="lock" :size="15" class="mt-0.5 shrink-0 text-txt3" />
          <p class="min-w-0 flex-1 text-[12.5px] leading-relaxed text-txt2">{{ t('pages.solo.closedBar.text') }}</p>
          <AppButton size="sm" variant="primary" icon="refresh" :loading="restarting" @click="doRestart">{{ t('pages.solo.closedBar.restart') }}</AppButton>
        </div>
      </div>
      <div v-else class="solo-bottom shrink-0 py-3">
        <div class="solo-column">
          <p v-if="actionError" class="mb-2 text-[12px] text-err" role="alert">{{ actionError }}</p>
          <p v-if="selectionInvalid && canSend" class="mb-2 text-[12px] text-warn" role="status" data-testid="solo-model-invalid-notice">{{ t('pages.solo.modelSelect.unavailableSelection') }}</p>
          <p v-if="modelCatalogBlocked && canSend" class="mb-2 text-[12px] text-txt2" role="status" data-testid="solo-model-catalog-notice">{{ t('pages.solo.modelSelect.catalogFailed') }}</p>
          <p v-if="attach.notice.value" class="mb-2 text-[12px]" :class="attach.notice.value.kind === 'error' ? 'text-err' : 'text-ok'">{{ attach.notice.value.text }}</p>
          <div v-if="attach.attachments.value.length" class="mb-2 flex flex-wrap gap-1.5">
            <div v-for="(im, ii) in attach.attachments.value" :key="ii" class="relative">
              <ChatImageThumb
                v-if="isImageAttachment(im)"
                mode="previewable"
                size="sm"
                thumb-class="rounded-md"
                :src="chatImageSrc(im)"
                :label="attachmentDisplayName(im, ii)"
                :alt="attachmentDisplayName(im, ii)"
                @preview="openChatImagePreview(chatImageSrc(im), attachmentDisplayName(im, ii))"
              />
              <div v-else class="chat-file-chip flex h-14 max-w-[160px] items-center gap-1.5 rounded-lg border border-line bg-elevated px-2">
                <Icon name="artifact" :size="15" class="shrink-0 text-txt2" />
                <span class="min-w-0 truncate text-[12px] text-txt2">{{ attachmentDisplayName(im, ii) }}</span>
              </div>
              <button type="button" class="absolute -right-1.5 -top-1.5 flex h-4 w-4 items-center justify-center rounded-full pending-attach-remove" @click="attach.removeAttachment(ii)">
                <Icon name="close" :size="9" />
              </button>
            </div>
          </div>
          <div class="chat-composer flex min-w-0 flex-col gap-2 p-2.5">
            <input ref="fileInput" type="file" multiple class="hidden" @change="attach.onPickFiles" />
            <textarea
              ref="textarea"
              v-model="input"
              rows="2"
              :disabled="!canSend"
              :placeholder="t('pages.solo.chat.placeholder', { agent: session.agentName })"
              :aria-label="t('pages.solo.chat.placeholder', { agent: session.agentName })"
              class="chat-input scroll-area max-h-[220px] min-h-[48px] min-w-0 flex-1 resize-none px-1 py-2 disabled:opacity-50"
              data-testid="solo-input"
              @keydown="onKeydown"
              @compositionstart="composing = true"
              @compositionend="composing = false"
              @paste="attach.onPaste"
            />
            <div class="chat-toolbar flex items-center justify-between gap-2">
              <div class="flex min-w-0 items-center gap-2">
                <button type="button" class="chat-action-button" :disabled="!canSend" :aria-label="t('pages.solo.chat.attach')" :title="t('pages.solo.chat.attach')" @click="fileInput?.click()">
                  <Icon name="plus" :size="18" />
                </button>
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
              </div>
              <div class="flex items-center gap-2">
                <button
                  v-if="busy"
                  type="button"
                  class="chat-action-button chat-action-stop"
                  :aria-label="t('pages.solo.chat.stop')"
                  :title="t('pages.solo.chat.stop')"
                  data-testid="solo-stop"
                  @click="chat.cancel()"
                >
                  <Icon name="chat-stop" :size="16" />
                </button>
                <button
                  type="button"
                  class="chat-action-button chat-action-primary"
                  :disabled="!canSend || selectionInvalid || (!input.trim() && !attach.attachments.value.length)"
                  :aria-label="t('pages.solo.chat.send')"
                  :title="t('pages.solo.chat.send')"
                  data-testid="solo-send"
                  @click="send"
                >
                  <Icon name="chat-send" :size="18" />
                </button>
              </div>
            </div>
          </div>
        </div>
      </div>

      <AppModal :open="confirmClose" :title="t('pages.solo.close.title')" :width="460" close-on-esc @close="confirmClose = false">
        <div class="space-y-3 text-[13px] text-txt2" data-testid="solo-close-modal">
          <div class="flex items-start gap-2 rounded-md border border-warn/30 bg-warn/10 px-3 py-2 text-[12px] text-warn">
            <Icon name="alert" :size="14" class="mt-0.5 shrink-0" />
            <span>{{ t('pages.solo.close.body') }}</span>
          </div>
          <p v-if="busy">{{ t('pages.solo.close.busy') }}</p>
          <button
            v-if="repos.length && status === 'running'"
            type="button"
            class="solo-push-hint"
            data-testid="solo-close-push"
            @click="pushThenStay"
          >
            <Icon name="git" :size="15" class="shrink-0 text-accent" />
            <span class="min-w-0 flex-1 text-left">
              <span class="block font-medium text-txt">{{ t('pages.solo.close.pushFirst') }}</span>
              <span class="block text-[11.5px] text-txt3">{{ t('pages.solo.close.pushFirstHint') }}</span>
            </span>
            <Icon name="chevron-right" :size="14" class="shrink-0 text-txt3" />
          </button>
        </div>
        <template #footer>
          <AppButton variant="ghost" @click="confirmClose = false">{{ t('common.buttons.cancel') }}</AppButton>
          <AppButton variant="danger" icon="stop" :loading="closing" data-testid="solo-close-confirm" @click="doClose">{{ t('pages.solo.close.confirm') }}</AppButton>
        </template>
      </AppModal>

      <AppModal :open="confirmDelete" :title="t('pages.solo.delete.title')" :width="420" close-on-esc @close="confirmDelete = false">
        <p class="text-[13px] text-txt2">{{ closed ? t('pages.solo.delete.body') : t('pages.solo.delete.bodyActive') }}</p>
        <template #footer>
          <AppButton variant="ghost" @click="confirmDelete = false">{{ t('common.buttons.cancel') }}</AppButton>
          <AppButton variant="danger" icon="trash" data-testid="solo-delete-confirm" @click="doDelete">{{ t('pages.solo.delete.confirm') }}</AppButton>
        </template>
      </AppModal>

      <SoloLimitModal :open="limitOpen" @close="limitOpen = false" @freed="limitOpen = false; doRestart()" />
      <ChatImagePreviewModal :open="!!imagePreview" :src="imagePreview?.src || ''" :label="imagePreview?.label || ''" @close="closeChatImagePreview" />
      </template>
    </ReviewShell>
  </div>
</template>

<style scoped>
.solo-pane {
  container: solo-pane / inline-size;
}
.solo-workspace {
  background: rgb(var(--c-surface));
}
.solo-column {
  width: 100%;
  max-width: 780px;
  min-width: 0;
  margin-inline: auto;
}
.solo-menu {
  position: absolute;
  right: 0;
  top: calc(100% + 6px);
  z-index: 30;
  min-width: 170px;
  border: 1px solid rgb(var(--c-line));
  border-radius: 10px;
  background: rgb(var(--c-elevated));
  padding: 4px;
  box-shadow: 0 12px 32px rgb(0 0 0 / 0.18);
}
.solo-menu__item {
  display: flex;
  width: 100%;
  align-items: center;
  gap: 8px;
  border-radius: 7px;
  padding: 7px 9px;
  color: rgb(var(--c-txt));
  font-size: 12.5px;
  text-align: left;
}
.solo-menu__item:hover {
  background: rgb(var(--c-accent) / 0.07);
}
.solo-menu__item--danger {
  color: rgb(var(--c-err));
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
.solo-transcript {
  padding: 24px 20px 32px;
  overflow-anchor: none;
}
.solo-bottom {
  padding-inline: 20px;
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
.solo-push-hint {
  display: flex;
  width: 100%;
  align-items: center;
  gap: 10px;
  border: 1px solid rgb(var(--c-line));
  border-radius: 10px;
  padding: 10px 12px;
  transition: border-color 0.15s ease, background 0.15s ease;
}
.solo-push-hint:hover {
  border-color: rgb(var(--c-accent) / 0.4);
  background: rgb(var(--c-accent) / 0.05);
}
@container solo-pane (max-width: 640px) {
  .solo-transcript {
    padding: 18px 12px 24px;
  }
  .solo-bottom {
    padding-inline: 12px;
  }
}
</style>
