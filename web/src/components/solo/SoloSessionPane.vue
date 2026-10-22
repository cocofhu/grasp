<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, toRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink, useRouter } from 'vue-router'
import Icon from '@/components/ui/Icon.vue'
import AppButton from '@/components/ui/AppButton.vue'
import AppModal from '@/components/ui/AppModal.vue'
import ChatImageThumb from '@/components/ui/ChatImageThumb.vue'
import ChatImagePreviewModal from '@/components/ui/ChatImagePreviewModal.vue'
import AcpStatusPill from '@/components/run/AcpStatusPill.vue'
import AgentTimeline from '@/components/run/AgentTimeline.vue'
import SoloStatusDot from './SoloStatusDot.vue'
import SoloStartupCard from './SoloStartupCard.vue'
import SoloEnvPanel from './SoloEnvPanel.vue'
import SoloLimitModal from './SoloLimitModal.vue'
import { api } from '@/lib/api/api'
import { agentHueVar, agentInitial } from '@/components/canvas/composables/agentAvatar'
import { attachmentDisplayName, isImageAttachment } from '@/lib/shared/attachments'
import { chatImageSrc } from '@/lib/shared/compositeText'
import { useImageAttachments } from '@/lib/composables/useImageAttachments'
import { useChatImagePreview } from '@/lib/composables/useChatImagePreview'
import { partsOf } from '@/lib/solo/acpTurn'
import { useSoloChat } from '@/lib/solo/useSoloChat'
import { useSoloSessions } from '@/lib/solo/useSoloSessions'
import { backendLabel, repoShort } from '@/lib/solo/soloPrefs'
import { isSoloLimitError, soloErrorText } from '@/lib/solo/soloErrors'

const props = defineProps<{ sessionId: string }>()
const emit = defineEmits<{ (e: 'back'): void }>()
const { t, te } = useI18n()
const router = useRouter()
const store = useSoloSessions()
const chat = useSoloChat(toRef(props, 'sessionId'))
const { session, turns, queued, busy, phase, link, loading, loadError, actionError, status, closed, canSend } = chat
const attach = useImageAttachments()
const fileInput = attach.fileInput
const { preview: imagePreview, openChatImagePreview, closeChatImagePreview } = useChatImagePreview()

const input = ref('')
const composing = ref(false)
const scroller = ref<HTMLElement | null>(null)
const textarea = ref<HTMLTextAreaElement | null>(null)
const followBottom = ref(true)
const envOpen = ref(false)
const confirmClose = ref(false)
const confirmDelete = ref(false)
const moreOpen = ref(false)
const limitOpen = ref(false)
const closing = ref(false)
const restarting = ref(false)
const editingTitle = ref(false)
const titleDraft = ref('')
const titleInput = ref<HTMLInputElement | null>(null)

watch(session, (s) => s && store.upsert(s))

const title = computed(() => session.value?.title || t('pages.solo.untitled'))
const project = computed(() => session.value?.project ?? null)
const sandboxId = computed(() => session.value?.sandboxId || session.value?.sandbox?.id || 0)
const sandboxLive = computed(() => status.value === 'running' && !!sandboxId.value)
const ideUrl = computed(() => {
  if (!sandboxLive.value || !session.value?.sandbox?.hasCodeServer) return ''
  const base = api.sandboxIdeUrl(sandboxId.value)
  const dir = project.value ? project.value.repoDirName || repoShort(project.value.repoUrl).split('/').pop() : ''
  return dir ? base.replace(/folder=[^&]*/, `folder=/root/workspace/${encodeURIComponent(dir)}`) : base
})
const branchLabel = computed(() => session.value?.branch || project.value?.defaultBranch || '')
const showStartup = computed(() => status.value === 'starting' && !!session.value)

function avatar(name: string) {
  const v = agentHueVar(name)
  return { background: `rgb(var(${v}) / 0.16)`, color: `rgb(var(${v}))` }
}

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
watch(
  () => props.sessionId,
  () => {
    followBottom.value = true
    input.value = ''
    attach.clearAttachments()
    envOpen.value = false
    editingTitle.value = false
    scrollDown(true)
  },
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
  if ((!text && !images.length) || !canSend.value) return
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

// --- header actions -----------------------------------------------------------
async function editTitle() {
  titleDraft.value = session.value?.title ?? ''
  editingTitle.value = true
  await nextTick()
  titleInput.value?.select()
}
async function commitTitle() {
  if (!editingTitle.value) return
  editingTitle.value = false
  const next = titleDraft.value.trim()
  if (next && next !== session.value?.title) await chat.rename(next)
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
    } else actionError.value = soloErrorText(e, t, te)
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
    actionError.value = soloErrorText(e, t, te)
  }
}

function onDocPointer(e: Event) {
  if (!moreOpen.value) return
  const el = e.target as HTMLElement
  if (!el.closest?.('[data-solo-more]')) moreOpen.value = false
}
onMounted(() => document.addEventListener('pointerdown', onDocPointer))
onBeforeUnmount(() => document.removeEventListener('pointerdown', onDocPointer))

function turnInterrupted(turn: { status?: string; error?: string; streaming: boolean }) {
  return !turn.streaming && (turn.status === 'stopped' || turn.status === 'failed' || !!turn.error)
}
</script>

<template>
  <div class="solo-pane flex min-h-0 min-w-0 flex-1 flex-col" data-testid="solo-session-pane">
    <div v-if="loading && !session" class="flex flex-1 items-center justify-center text-txt3">
      <Icon name="spinner" :size="18" class="animate-spin" />
    </div>
    <div v-else-if="loadError" class="flex flex-1 flex-col items-center justify-center gap-3 p-6 text-center">
      <Icon name="alert" :size="22" class="text-err" />
      <p class="text-[13px] text-txt2">{{ loadError }}</p>
      <AppButton size="sm" @click="chat.load()">{{ t('common.buttons.retry') }}</AppButton>
    </div>

    <template v-else-if="session">
      <!-- header -->
      <header class="solo-head">
        <button type="button" class="solo-head__back" :aria-label="t('pages.solo.backToList')" @click="emit('back')">
          <Icon name="arrow-left" :size="16" />
        </button>
        <SoloStatusDot :status="session.status" :busy="busy" />
        <div class="min-w-0 flex-1">
          <input
            v-if="editingTitle"
            ref="titleInput"
            v-model="titleDraft"
            class="solo-head__title-input"
            maxlength="80"
            :aria-label="t('pages.solo.header.rename')"
            @keydown.enter.prevent="commitTitle"
            @keydown.esc.prevent="editingTitle = false"
            @blur="commitTitle"
          />
          <button v-else type="button" class="solo-head__title" :title="t('pages.solo.header.rename')" data-testid="solo-title" @click="editTitle">
            <span class="truncate">{{ title }}</span>
            <Icon name="edit" :size="12" class="solo-head__title-edit" />
          </button>
          <div class="solo-head__meta">
            <span class="solo-head__chip">
              <span class="solo-head__avatar" :style="avatar(session.agentName)">{{ agentInitial(session.agentName) }}</span>
              {{ session.agentName }}
              <span class="text-txt3">· {{ backendLabel(session.backend) }}</span>
            </span>
            <span v-if="project" class="solo-head__chip" :title="project.repoUrl">
              <Icon name="git" :size="12" class="text-accent" />
              <span class="truncate">{{ project.name }}</span>
              <template v-if="branchLabel">
                <Icon name="branch" :size="11" class="text-txt3" />
                <span class="font-mono text-txt3">{{ branchLabel }}</span>
              </template>
            </span>
            <span v-else class="solo-head__chip text-txt3">
              <Icon name="folder" :size="12" />{{ t('pages.solo.noProject') }}
            </span>
          </div>
        </div>
        <div class="solo-head__actions">
          <AcpStatusPill v-if="status === 'running'" :busy="busy" :connected="link === 'open'" class="hidden md:inline-flex" />
          <RouterLink
            v-if="sandboxLive"
            :to="`/sandboxes/${sandboxId}/console`"
            class="solo-head__btn"
            :title="t('pages.solo.header.console')"
            data-testid="solo-console-link"
          >
            <Icon name="terminal" :size="15" /><span class="hidden lg:inline">{{ t('pages.solo.header.console') }}</span>
          </RouterLink>
          <a v-if="ideUrl" :href="ideUrl" target="_blank" rel="noopener" class="solo-head__btn" :title="t('pages.solo.header.vscode')">
            <Icon name="stage-ide" :size="15" /><span class="hidden lg:inline">{{ t('pages.solo.header.vscode') }}</span>
          </a>
          <button type="button" class="solo-head__btn" :title="t('pages.solo.header.env')" data-testid="solo-env-toggle" @click="envOpen = true">
            <Icon name="variable" :size="15" /><span class="hidden lg:inline">{{ t('pages.solo.header.env') }}</span>
          </button>
          <button
            v-if="!closed && status !== 'error'"
            type="button"
            class="solo-head__btn solo-head__btn--danger"
            data-testid="solo-close"
            @click="confirmClose = true"
          >
            <Icon name="stop" :size="14" /><span class="hidden sm:inline">{{ t('pages.solo.header.close') }}</span>
          </button>
          <button
            v-else
            type="button"
            class="solo-head__btn solo-head__btn--primary"
            :disabled="restarting"
            data-testid="solo-restart"
            @click="doRestart"
          >
            <Icon :name="restarting ? 'spinner' : 'refresh'" :size="14" :class="{ 'animate-spin': restarting }" />
            <span class="hidden sm:inline">{{ t('pages.solo.header.restart') }}</span>
          </button>
          <div class="relative" data-solo-more>
            <button type="button" class="solo-head__btn" :aria-label="t('pages.solo.header.more')" :aria-expanded="moreOpen" @click="moreOpen = !moreOpen">
              <Icon name="more" :size="15" />
            </button>
            <div v-if="moreOpen" class="solo-menu" role="menu">
              <button type="button" role="menuitem" class="solo-menu__item" @click="moreOpen = false; editTitle()">
                <Icon name="edit" :size="14" />{{ t('pages.solo.header.rename') }}
              </button>
              <RouterLink v-if="session.projectId" :to="`/settings/projects?edit=${session.projectId}`" role="menuitem" class="solo-menu__item" @click="moreOpen = false">
                <Icon name="settings" :size="14" />{{ t('pages.solo.header.editProject') }}
              </RouterLink>
              <button type="button" role="menuitem" class="solo-menu__item solo-menu__item--danger" data-testid="solo-delete" @click="moreOpen = false; confirmDelete = true">
                <Icon name="trash" :size="14" />{{ t('pages.solo.header.delete') }}
              </button>
            </div>
          </div>
        </div>
      </header>

      <div v-if="link === 'reconnecting'" class="solo-banner" role="status">
        <Icon name="spinner" :size="12" class="animate-spin" />{{ t('pages.solo.link.reconnecting') }}
      </div>

      <!-- transcript -->
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
              <RouterLink v-if="sandboxId" :to="`/sandboxes/${sandboxId}/console`" class="solo-head__btn">
                <Icon name="terminal" :size="14" />{{ t('pages.solo.errorCard.logs') }}
              </RouterLink>
              <RouterLink v-if="session.projectId" :to="`/settings/projects?edit=${session.projectId}`" class="solo-head__btn">
                <Icon name="settings" :size="14" />{{ t('pages.solo.errorCard.edit') }}
              </RouterLink>
            </div>
          </div>

          <div v-if="!turns.length && status === 'running'" class="solo-empty">
            <span class="solo-empty__avatar" :style="avatar(session.agentName)">{{ agentInitial(session.agentName) }}</span>
            <p class="text-[13px] text-txt2">{{ t('pages.solo.chat.empty', { agent: session.agentName }) }}</p>
            <p v-if="project" class="font-mono text-[11.5px] text-txt3">{{ repoShort(project.repoUrl) }}<template v-if="branchLabel"> @ {{ branchLabel }}</template></p>
          </div>

          <template v-for="turn in turns" :key="turn.id">
            <div v-if="turn.role === 'system'" class="solo-system">
              <span>{{ turn.text }}</span>
            </div>
            <div v-else-if="turn.role === 'user'" class="flex justify-end">
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
                <div v-if="turn.text" class="chat-user-message whitespace-pre-wrap break-words px-4 py-2.5 [overflow-wrap:anywhere]">{{ turn.text }}</div>
              </div>
            </div>
            <div v-else class="min-w-0 space-y-3">
              <AgentTimeline
                v-if="turn.steps.length"
                :parts="partsOf(turn)"
                :streaming="turn.streaming"
                :completed="!turn.streaming && !turnInterrupted(turn)"
                :interrupted="turnInterrupted(turn)"
                :started-at="turn.startedAt"
                :ended-at="turn.endedAt"
                message-test-id="solo-agent-message"
              />
              <div v-if="turn.plan.length" class="border-l border-line/70 pl-3 text-[12px] leading-6">
                <div class="chat-meta mb-1">{{ t('pages.solo.chat.plan') }}</div>
                <div v-for="(p, pi) in turn.plan" :key="pi" class="flex items-center gap-2 text-txt2">
                  <Icon :name="p.status === 'completed' ? 'check' : 'clock'" :size="13" :class="p.status === 'completed' ? 'text-ok' : 'text-txt3'" />
                  <span :class="p.status === 'completed' ? 'text-txt3 line-through' : ''">{{ p.content }}</span>
                </div>
              </div>
              <div v-if="turn.streaming && !turn.steps.length" class="chat-meta flex items-center gap-1.5 py-1">
                <Icon name="spinner" :size="13" class="animate-spin" />
                {{ phase === 'preparing' ? t('pages.solo.phase.preparing') : t('pages.solo.chat.generating') }}
              </div>
              <div v-if="turn.status === 'stopped' && !turn.streaming" class="chat-meta">{{ t('pages.solo.chat.stopped') }}</div>
              <div v-else-if="turn.error" class="rounded-md border border-err/30 bg-err/10 px-3 py-2 text-[12px] text-err">{{ turn.error }}</div>
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
              <button type="button" class="absolute -right-1.5 -top-1.5 flex h-4 w-4 items-center justify-center rounded-full bg-err text-white" @click="attach.removeAttachment(ii)">
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
                  :disabled="!canSend || (!input.trim() && !attach.attachments.value.length)"
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

      <SoloEnvPanel :open="envOpen" :session="session" @close="envOpen = false" />

      <AppModal :open="confirmClose" :title="t('pages.solo.close.title')" :width="460" close-on-esc @close="confirmClose = false">
        <div class="space-y-3 text-[13px] text-txt2" data-testid="solo-close-modal">
          <div class="flex items-start gap-2 rounded-md border border-warn/30 bg-warn/10 px-3 py-2 text-[12px] text-warn">
            <Icon name="alert" :size="14" class="mt-0.5 shrink-0" />
            <span>{{ t('pages.solo.close.body') }}</span>
          </div>
          <p v-if="busy">{{ t('pages.solo.close.busy') }}</p>
          <button
            v-if="project && status === 'running'"
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
  </div>
</template>

<style scoped>
.solo-pane {
  container: solo-pane / inline-size;
}
.solo-column {
  width: 100%;
  max-width: 780px;
  min-width: 0;
  margin-inline: auto;
}
.solo-head {
  display: flex;
  min-height: 60px;
  flex-shrink: 0;
  align-items: center;
  gap: 10px;
  border-bottom: 1px solid rgb(var(--c-line));
  padding: 8px 16px;
}
.solo-head__back {
  display: none;
  color: rgb(var(--c-txt2));
}
.solo-head__title {
  display: flex;
  max-width: 100%;
  align-items: center;
  gap: 6px;
  color: rgb(var(--c-txt));
  font-size: 14px;
  font-weight: 600;
}
.solo-head__title-edit {
  flex-shrink: 0;
  color: rgb(var(--c-txt3));
  opacity: 0;
  transition: opacity 0.15s ease;
}
.solo-head__title:hover .solo-head__title-edit,
.solo-head__title:focus-visible .solo-head__title-edit {
  opacity: 1;
}
.solo-head__title-input {
  width: 100%;
  max-width: 420px;
  border-bottom: 1px solid rgb(var(--c-accent));
  background: transparent;
  color: rgb(var(--c-txt));
  font-size: 14px;
  font-weight: 600;
  outline: none;
}
.solo-head__meta {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 3px;
}
.solo-head__chip {
  display: inline-flex;
  max-width: 100%;
  min-width: 0;
  align-items: center;
  gap: 5px;
  color: rgb(var(--c-txt2));
  font-size: 11.5px;
}
.solo-head__avatar {
  display: inline-flex;
  width: 16px;
  height: 16px;
  align-items: center;
  justify-content: center;
  border-radius: 5px;
  font-size: 9.5px;
  font-weight: 700;
}
.solo-head__actions {
  display: flex;
  flex-shrink: 0;
  align-items: center;
  gap: 6px;
}
.solo-head__btn {
  display: inline-flex;
  height: 30px;
  align-items: center;
  gap: 5px;
  border: 1px solid rgb(var(--c-line));
  border-radius: 8px;
  padding: 0 9px;
  color: rgb(var(--c-txt2));
  font-size: 12px;
  transition: border-color 0.15s ease, color 0.15s ease;
}
.solo-head__btn:hover:not(:disabled) {
  border-color: rgb(var(--c-accent) / 0.35);
  color: rgb(var(--c-txt));
}
.solo-head__btn--danger:hover:not(:disabled) {
  border-color: rgb(var(--c-err) / 0.45);
  color: rgb(var(--c-err));
}
.solo-head__btn--primary {
  border-color: rgb(var(--c-accent) / 0.5);
  background: rgb(var(--c-accent) / 0.1);
  color: rgb(var(--c-accent-2));
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
.solo-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  padding: 48px 0 12px;
  text-align: center;
}
.solo-empty__avatar {
  display: inline-flex;
  width: 44px;
  height: 44px;
  align-items: center;
  justify-content: center;
  border-radius: 14px;
  font-size: 18px;
  font-weight: 700;
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
@media (max-width: 860px) {
  .solo-head__back {
    display: inline-flex;
  }
}
@container solo-pane (max-width: 640px) {
  .solo-head {
    padding-inline: 10px;
  }
  .solo-transcript {
    padding: 18px 12px 24px;
  }
  .solo-bottom {
    padding-inline: 12px;
  }
}
</style>
