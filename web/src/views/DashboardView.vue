<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AttachmentDropZone from '@/components/chat/AttachmentDropZone.vue'
import ChatFileAttachment from '@/components/chat/ChatFileAttachment.vue'
import ChatAttachmentDetails from '@/components/chat/ChatAttachmentDetails.vue'
import HomeParticleMeshBackground from '@/components/dashboard/HomeParticleMeshBackground.vue'
import HomeYumeCharacter from '@/components/dashboard/HomeYumeCharacter.vue'
import HomeModeSwitch from '@/components/dashboard/HomeModeSwitch.vue'
import HomeWorkflowSelect from '@/components/dashboard/HomeWorkflowSelect.vue'
import HomePrioritySelect from '@/components/dashboard/HomePrioritySelect.vue'
import HomeWorkflowRail from '@/components/dashboard/HomeWorkflowRail.vue'
import HomeCreateBaselineModal from '@/components/dashboard/HomeCreateBaselineModal.vue'
import RunLaunchModal from '@/components/workflow/RunLaunchModal.vue'
import AppButton from '@/components/ui/AppButton.vue'
import AppModal from '@/components/ui/AppModal.vue'
import Icon from '@/components/ui/Icon.vue'
import ChatImageThumb from '@/components/ui/ChatImageThumb.vue'
import ChatImagePreviewModal from '@/components/ui/ChatImagePreviewModal.vue'
import SoloAgentSelect from '@/components/solo/SoloAgentSelect.vue'
import SoloProjectSelect from '@/components/solo/SoloProjectSelect.vue'
import SoloModelSelect from '@/components/solo/SoloModelSelect.vue'
import HomeSoloPanel from '@/components/solo/HomeSoloPanel.vue'
import ProjectDrawer from '@/components/solo/ProjectDrawer.vue'
import SoloLimitModal from '@/components/solo/SoloLimitModal.vue'
import SoloCreationRecovery from '@/components/solo/SoloCreationRecovery.vue'
import ReferencePicker from '@/components/solo/ReferencePicker.vue'
import ReferenceChip from '@/components/solo/ReferenceChip.vue'
import ReferencePlusMenu from '@/components/solo/ReferencePlusMenu.vue'
import { useChatImagePreview } from '@/lib/composables/useChatImagePreview'
import { useBrandSettings } from '@/lib/composables/useBrandSettings'
import { useWorkflowFeature } from '@/lib/composables/useWorkflowFeature'
import { useImageAttachments } from '@/lib/composables/useImageAttachments'
import { useSoloLauncher } from '@/lib/solo/useSoloLauncher'
import { soloPrefs, type HomeMode } from '@/lib/solo/soloPrefs'
import { useHomeApproveChat } from '@/lib/run/useHomeApproveChat'
import { api } from '@/lib/api/api'
import { useToast } from '@/lib/composables/useToast'
import { openRetryOnboarding } from '@/lib/pm/firstInstall'
import { missingOnboardingNames, onboardingAgentsReady } from '@/lib/pm/onboardingWizard'
import type { AppLocale } from '@/lib/shared/loadLocaleMessages'
import { useReferenceComposer } from '@/lib/solo/useReferenceComposer'
import { githubRepoOf } from '@/lib/solo/referenceDisplay'
import { attachmentDisplayName, isImageAttachment } from '@/lib/shared/attachments'
import { imgSrc } from '@/lib/shared/compositeText'
import { clearHomeComposerDraft, loadHomeComposerDraft, saveHomeComposerDraft } from '@/lib/run/homeComposerDraft'
import type { ReferenceScope, ReferenceTab } from '@/lib/solo/useReferenceSearch'

const { t, tm, locale } = useI18n()
const router = useRouter()
const toast = useToast()
const solo = useSoloLauncher()
const { agents: soloAgents, projects: soloProjects, agentName: soloAgentName, projectIds: soloProjectIds,
  branches: soloBranches, initialModelId: soloInitialModelId, reasoningEffort: soloReasoningEffort,
  modelChoices: soloModelChoices, limitOpen: soloLimitOpen } = solo
const { homeSubtitle } = useBrandSettings()
const { enabled: workflowsEnabled } = useWorkflowFeature()
const heroCopy = computed(() => homeSubtitle.value || t('pages.dashboard.heroSubtitle'))
const { preview: imagePreview, openChatImagePreview, closeChatImagePreview } = useChatImagePreview()
const attach = useImageAttachments()
const { attachments, fileInput, notice: attachNotice, blockSendIfOversized, addFiles, onPickFiles, onPaste, removeAttachment } = attach
const draft = ref('')
const projectDrawerOpen = ref(false)
const homeMode = ref<HomeMode>(soloPrefs.homeMode())
watch(homeMode, (mode) => { soloPrefs.setHomeMode(mode); solo.error.value = '' })
/** The workflow side of the home composer shares this draft and these attachments. */
const workflow = useHomeApproveChat({ draft, attach })
const {
  homeWorkflows, unpublishedHomeWorkflows, selected: selectedWorkflow, selectedId: selectedWorkflowId,
  launchPriority, showLaunchModel, launchModelId, launchReasoningEffort, launchModelChoices, launchModelForModal,
  sending: workflowSending, hidingWorkflowId, loading: workflowsLoading, loadError: workflowsLoadError,
  launchOpen, launchTarget, launchTitle, launchFirstMessage, runFields, runInputs, runImages, draftRestored,
} = workflow
const workflowMode = computed(() => workflowsEnabled.value && homeMode.value === 'workflow')
const composerBusy = computed(() => (workflowMode.value ? workflowSending.value : solo.creating.value))
const composerCanSend = computed(() => (workflowMode.value ? workflow.canSend.value : solo.canStart.value))
/** plan g1.2 — the rail waits blank and reveals on the first successful load. */
const workflowRailRevealed = ref(false)
watch(workflowsLoading, (now, prev) => {
  if (prev === true && now === false && !workflowsLoadError.value) workflowRailRevealed.value = true
})
const createBaselineOpen = ref(false)
const homeContentEl = ref<HTMLDivElement | null>(null)
const homeBrandBlockEl = ref<HTMLDivElement | null>(null)
const homeModeTopInset = ref<number | null>(null)
/** Keep the hero's top position stable while switching the mode-specific tail. */
watch(homeMode, () => {
  const content = homeContentEl.value
  const brand = homeBrandBlockEl.value
  if (!content || !brand) return
  const inset = brand.getBoundingClientRect().top - content.getBoundingClientRect().top + content.scrollTop
  if (Number.isFinite(inset) && inset >= 0) homeModeTopInset.value = inset
})
// Attachments use the flexible inset budget so the first thumbnail row fits without a scrollbar.
watch(() => attachments.value.length, () => { homeModeTopInset.value = null })
function onHomeResize() { homeModeTopInset.value = null }
const onboardingGateOpen = ref(false)
const onboardingGateMissing = ref<string[]>([])
const composerFocused = ref(false)
const composing = ref(false)
const textareaRef = ref<HTMLTextAreaElement | null>(null)
const typingTick = ref(0)
const cheerTick = ref(0)
const overflowScroll = ref(false)
// @ references. Solo searches the picked agent and repositories; workflow
// mode searches the selected workflow's default repos and clarify Agent.
const mentionEnabled = computed(() => (workflowMode.value ? !!selectedWorkflow.value?.id : !!soloAgentName.value))
const mentionDefaultRepo = computed(() =>
  workflowMode.value ? '' : githubRepoOf(soloProjects.value.find(p => p.id === soloProjectIds.value[0])?.repoUrl),
)
const mentionScope = computed<ReferenceScope>(() =>
  workflowMode.value ? { workflowId: selectedWorkflow.value?.id || '' } : { agent: soloAgentName.value, projects: soloProjectIds.value },
)
const mention = useReferenceComposer({ input: draft, textarea: textareaRef, composing, defaultRepo: mentionDefaultRepo, enabled: mentionEnabled })
const mentionPicker = mention.picker
const plusOpen = ref(false)
let draftLoaded = false
let draftRevision = 0
let draftTimer: ReturnType<typeof setTimeout> | null = null
const submitting = ref(false)
watch([draft, attachments, mention.tokens], () => {
  draftRevision++
  if (!draftLoaded || submitting.value) return
  if (draftTimer) clearTimeout(draftTimer)
  draftTimer = setTimeout(() => void saveHomeComposerDraft(draft.value, attachments.value, draftWorkflowId(), mention.tokens.value), 400)
}, { deep: true })
watch(selectedWorkflowId, () => {
  if (!draftLoaded || submitting.value || !workflowMode.value) return
  if (draftTimer) clearTimeout(draftTimer)
  draftTimer = setTimeout(() => void saveHomeComposerDraft(draft.value, attachments.value, draftWorkflowId(), mention.tokens.value), 400)
})
function draftWorkflowId() { return workflowMode.value ? selectedWorkflowId.value : '' }

async function submitWorkflow() {
  if (composerCanSend.value && !composerBusy.value) cheerTick.value++
  if (draftTimer) clearTimeout(draftTimer)
  await workflow.send(mention.refs.value)
  // send clears the shared draft once the run starts.
  if (!draft.value && !attachments.value.length) {
    mention.clear()
    await clearHomeComposerDraft()
  }
}

async function onWorkflowLaunchStarted(runId: string) {
  await workflow.onLaunchStarted(runId)
  mention.clear()
  await clearHomeComposerDraft()
}

function uiOnboardingLang(): AppLocale {
  return String(locale.value).toLowerCase().startsWith('en') ? 'en' : 'zh-CN'
}

function finishOnboardingFromGate() {
  onboardingGateOpen.value = false
  openRetryOnboarding()
}

/** Home baseline create stops until one canonical quartet exists. */
async function ensureOnboardingAgents(): Promise<boolean> {
  try {
    const agents = await api.listAgents()
    const names = (agents || []).map((agent) => agent.name || '')
    if (onboardingAgentsReady(names)) return true
    onboardingGateMissing.value = missingOnboardingNames(names, uiOnboardingLang())
    onboardingGateOpen.value = true
    return false
  } catch (cause: unknown) {
    toast.error(cause instanceof Error ? cause.message : String(cause))
    return false
  }
}

async function openCreateBaseline() {
  if (!(await ensureOnboardingAgents())) return
  createBaselineOpen.value = true
}

async function onBaselineCreated(payload?: { id?: string }) {
  createBaselineOpen.value = false
  await workflow.reloadAfterCreate(payload?.id)
}

function goEditWorkflow(id: string) {
  void router.push(`/workflows/${id}/edit`)
}

async function submitComposer() {
  if (workflowMode.value) return submitWorkflow()
  if (blockSendIfOversized() || !composerCanSend.value || composerBusy.value || submitting.value) return
  submitting.value = true
  if (draftTimer) clearTimeout(draftTimer)
  cheerTick.value++
  const started = await solo.start(draft.value, attachments.value.map(image => ({ ...image })), '', mention.refs.value)
  if (started) {
    draft.value = ''
    attachments.value = []
    mention.clear()
    await clearHomeComposerDraft()
  }
  submitting.value = false
}

async function recoverSoloCreation() {
  const request = solo.pendingCreation.value?.request
  if (!request) return
  const started = await solo.retryPendingCreation()
  if (!started) return
  // Keep a separately edited draft. Clear only the composer that produced
  // this exact opening message, so returning home cannot submit it again.
  const refs = mention.refs.value.map(({ kind, target }) => ({ kind, target }))
  if (draft.value.trim() === (request.text || '') &&
      JSON.stringify(attachments.value) === JSON.stringify(request.images || []) &&
      JSON.stringify(refs) === JSON.stringify(request.references || [])) {
    draft.value = ''
    attachments.value = []
    mention.clear()
    await clearHomeComposerDraft()
  }
}
function onProjectSaved(project: Parameters<typeof solo.onProjectSaved>[0]) { projectDrawerOpen.value = false; solo.onProjectSaved(project) }
function openFilePicker() { fileInput.value?.click() }
function onPlus() { if (mentionEnabled.value) plusOpen.value = !plusOpen.value; else openFilePicker() }
function plusPickFiles() { plusOpen.value = false; openFilePicker() }
function plusReference(tab: ReferenceTab) { plusOpen.value = false; mention.open(tab) }
function onComposerPaste(event: ClipboardEvent) { onPaste(event); mention.onPaste(event) }
function updateComposerTriggers() { mention.updateTrigger() }
function onComposerKeyup(event: KeyboardEvent) { if (['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) updateComposerTriggers() }
function onComposerSubmit(event: Event) { event.preventDefault(); void submitComposer() }
function onWindowPointerDown(event: PointerEvent) { if (!(event.target instanceof Element && event.target.closest('[data-home-plus]'))) plusOpen.value = false }
// References are validated against the scope they were picked in, so a
// switch keeps the text and drops the references.
watch([homeMode, () => selectedWorkflow.value?.id || '', soloAgentName, () => soloProjectIds.value.join('|')], () => {
  mention.trigger.value = null
  if (mention.tokens.value.length) mention.set(draft.value, [])
  plusOpen.value = false
})
const phVisible = ref('')
const phCursor = ref(false)
let phTimer: ReturnType<typeof setTimeout> | null = null
let phHoldTimer: ReturnType<typeof setTimeout> | null = null
const placeholderLines = computed(() => {
  const raw = tm(workflowMode.value ? 'pages.dashboard.placeholders' : 'pages.solo.home.placeholders') as unknown
  return Array.isArray(raw) ? raw.filter((line): line is string => typeof line === 'string' && !!line.trim()) : []
})
const placeholderPrimary = computed(() => placeholderLines.value[0] || t('pages.dashboard.placeholder'))
const showPhTypewriter = computed(() => !draft.value.trim() && !composerFocused.value)
function prefersReducedMotion(): boolean {
  if (typeof window === 'undefined' || !window.matchMedia) return false
  return window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

function clearPhTimers() {
  if (phTimer != null) {
    clearTimeout(phTimer)
    phTimer = null
  }
  if (phHoldTimer != null) {
    clearTimeout(phHoldTimer)
    phHoldTimer = null
  }
}

/** g1.2 — idle placeholder typewriter (type → hold → delete → next line → repeat). */
function runPlaceholderTypewriter() {
  clearPhTimers()
  const lines = placeholderLines.value
  if (!showPhTypewriter.value) {
    phVisible.value = ''
    phCursor.value = false
    return
  }
  const first = lines[0] ?? ''
  if (prefersReducedMotion()) {
    phVisible.value = first
    phCursor.value = true
    return
  }
  phVisible.value = ''
  phCursor.value = true
  let li = 0
  let i = 0
  let deleting = false
  const tick = () => {
    if (!showPhTypewriter.value) return
    const full = lines[li] ?? ''
    if (!full) return
    if (!deleting) {
      i += 1
      phVisible.value = full.slice(0, i)
      if (i >= full.length) {
        phHoldTimer = setTimeout(() => {
          deleting = true
          tick()
        }, 1800)
        return
      }
      phTimer = setTimeout(tick, 70)
      return
    }
    i -= 1
    phVisible.value = full.slice(0, Math.max(0, i))
    if (i <= 0) {
      deleting = false
      li = (li + 1) % lines.length
      phTimer = setTimeout(tick, 400)
      return
    }
    phTimer = setTimeout(tick, 32)
  }
  phTimer = setTimeout(tick, 80)
}

function autoGrow() {
  const el = textareaRef.value
  if (!el) return
  el.style.height = 'auto'
  const h = Math.min(Math.max(el.scrollHeight, 88), 200)
  el.style.height = `${h}px`
  overflowScroll.value = el.scrollHeight > 200
}

function onTextInput() {
  autoGrow()
  typingTick.value++
  updateComposerTriggers()
}

function onComposerKeydown(e: KeyboardEvent) {
  if (mention.onKeydown(e)) return
  // plan g1.1 — Ctrl/⌘+Enter sends; bare Enter / Shift+Enter insert newline
  if (e.key !== 'Enter') return
  if (composing.value || e.isComposing || e.keyCode === 229) return
  if (!(e.ctrlKey || e.metaKey)) return
  e.preventDefault()
  void submitComposer()
}

function onComposerFocus() {
  composerFocused.value = true
  mention.updateTrigger()
}

function onComposerBlur() {
  composerFocused.value = false
  mention.trigger.value = null
}


watch(draft, () => void nextTick(autoGrow))
watch(showPhTypewriter, runPlaceholderTypewriter, { immediate: true })
watch(placeholderLines, () => { if (showPhTypewriter.value) runPlaceholderTypewriter() })
onMounted(async () => {
  void solo.load()
  const revision = draftRevision
  const saved = await loadHomeComposerDraft()
  if (revision === draftRevision && saved) {
    mention.set(saved.text, saved.referenceTokens || [])
    attachments.value = saved.attachments
    if (saved.pipelineId) workflow.selectWorkflow(saved.pipelineId)
  }
  draftLoaded = true
  void nextTick(autoGrow)
  window.addEventListener('pointerdown', onWindowPointerDown)
  window.addEventListener('resize', onHomeResize)
})
onBeforeUnmount(() => {
  clearPhTimers()
  if (draftTimer) clearTimeout(draftTimer)
  if (draftLoaded && !submitting.value && !workflowSending.value) void saveHomeComposerDraft(draft.value, attachments.value, draftWorkflowId(), mention.tokens.value)
  window.removeEventListener('pointerdown', onWindowPointerDown)
  window.removeEventListener('resize', onHomeResize)
})
</script>

<template>
  <div data-testid="dashboard-view" class="home-shell relative flex h-full min-h-0 flex-col">
    <HomeParticleMeshBackground />
    <div
      ref="homeContentEl"
      class="home-shell__content relative z-[1] mx-auto flex w-full max-w-3xl min-h-0 flex-1 flex-col items-center justify-start overflow-x-hidden overflow-y-auto px-4"
      :class="{ 'home-shell__content--mode-locked': homeModeTopInset != null }"
      :style="homeModeTopInset != null ? { '--home-mode-top-inset': `${homeModeTopInset}px` } : undefined"
    >
      <div ref="homeBrandBlockEl" class="home-brand-block" data-testid="home-brand-block">
        <HomeYumeCharacter :attentive="composerFocused" :typing-tick="typingTick" :cheer-tick="cheerTick" />
        <p class="home-hero-copy" data-testid="home-hero-copy">{{ heroCopy }}</p>
      </div>

      <AttachmentDropZone class="home-composer-wrap w-full" :disabled="composerBusy" @files="addFiles">
        <div v-if="workflowsEnabled" class="mb-3 flex justify-center">
          <HomeModeSwitch v-model="homeMode" />
        </div>
        <SoloCreationRecovery
          :request="solo.pendingCreation.value" :busy="composerBusy || solo.recovering.value" :projects="soloProjects"
          :resolved="!!solo.recoveredSession.value" :checked="solo.recoveryChecked.value"
          @check="solo.checkPendingCreation" @retry="recoverSoloCreation" @stop="solo.stopCreationRetry"
        />
        <p
          v-if="!workflowMode && solo.error.value"
          class="mb-2 rounded border border-err/40 bg-err/10 px-3 py-1.5 text-[12px] text-err"
          data-testid="home-solo-error"
          role="alert"
        >
          {{ solo.error.value }}
        </p>
        <p
          v-if="attachNotice"
          class="mb-2 rounded border border-err/40 bg-err/10 px-3 py-1.5 text-[12px] text-err"
          data-testid="home-attach-notice"
          role="alert"
        >
          {{ attachNotice.text }}
        </p>
        <div
          v-if="attachments.length"
          class="mb-2 flex w-full min-w-0 flex-wrap gap-2 pr-1.5 pt-1.5"
          data-testid="home-attach-chips"
        >
          <div v-for="(im, ii) in attachments" :key="ii" class="relative">
            <ChatImageThumb
              v-if="isImageAttachment(im) && im.readState !== 'reading'"
              mode="previewable"
              size="sm"
              thumb-class="rounded"
              :src="imgSrc(im)"
              :label="attachmentDisplayName(im, ii)"
              :alt="attachmentDisplayName(im, ii)"
              test-id="home-draft-image-thumb"
              @preview="openChatImagePreview(imgSrc(im), attachmentDisplayName(im, ii))"
            />
            <ChatFileAttachment v-else :attachment="im" compact data-testid="home-pending-file-chip" />
            <ChatAttachmentDetails v-if="isImageAttachment(im) && im.readState !== 'reading'" :attachment="im" :index="ii" />
            <button
              type="button"
              class="absolute -right-1.5 -top-1.5 flex h-4 w-4 items-center justify-center rounded pending-attach-remove"
              data-testid="home-attach-remove"
              @click.stop="removeAttachment(ii)"
              :aria-label="t('common.attachments.remove', { name: attachmentDisplayName(im, ii) })"
            >
              <Icon name="close" :size="9" />
            </button>
          </div>
        </div>
        <TransitionGroup
          v-if="mentionEnabled && mention.tokens.value.length"
          tag="div"
          name="ref-chip"
          class="mb-2 flex w-full min-w-0 flex-wrap gap-1.5"
          data-testid="home-reference-chips"
        >
          <ReferenceChip
            v-for="(tk, ti) in mention.tokens.value"
            :key="tk.ref.target"
            :reference="tk.ref"
            :default-repo="mentionDefaultRepo"
            removable
            @remove="mention.drop(ti)"
          />
        </TransitionGroup>
        <div class="relative w-full">
        <Transition name="ref-pop">
          <ReferencePicker
            v-if="mentionEnabled && mention.trigger.value && !composerBusy && !composing"
            ref="mentionPicker"
            placement="down"
            :scope="mentionScope"
            :query="mention.trigger.value.query"
            :taken="mention.targets.value"
            :full="mention.full.value"
            :initial-tab="mention.pickerTab.value"
            @pick="mention.pick"
          />
        </Transition>
        <div class="contents" data-home-plus>
          <ReferencePlusMenu :open="plusOpen" placement="down" test-prefix="home-plus" @upload="plusPickFiles" @reference="plusReference" />
        </div>
        <form
          class="home-composer flex w-full flex-col overflow-hidden border"
          data-testid="home-composer"
          @submit="onComposerSubmit"
        >
          <input
            ref="fileInput"
            type="file"
            multiple
            class="hidden"
            data-testid="home-composer-file"
            @change="onPickFiles"
          />
          <div class="home-composer__field relative px-4 pb-3 pt-4">
            <label class="sr-only" for="home-composer-input">{{ placeholderPrimary }}</label>
            <textarea
              id="home-composer-input"
              ref="textareaRef"
              v-model="draft"
              class="home-composer__input w-full min-w-0 resize-none bg-transparent text-[15px] text-txt outline-none"
              :class="overflowScroll ? 'scroll-area max-h-[200px] overflow-y-auto' : 'overflow-y-hidden'"
              data-testid="home-composer-input"
              rows="3"
              :disabled="composerBusy"
              autocomplete="off"
              @input="onTextInput"
              @keydown="onComposerKeydown"
              @compositionstart="composing = true"
              @compositionend="composing = false; updateComposerTriggers()"
              @paste="onComposerPaste"
              @click="updateComposerTriggers"
              @select="updateComposerTriggers"
              @keyup="onComposerKeyup"
              @focus="onComposerFocus"
              @blur="onComposerBlur"
            />
            <span
              v-if="showPhTypewriter"
              class="home-composer__ph pointer-events-none absolute left-4 top-4 text-[15px] text-txt3"
              data-testid="home-composer-placeholder"
              aria-hidden="true"
            >
              {{ phVisible }}<span
                v-if="phCursor"
                class="home-composer__ph-cursor"
                data-testid="home-composer-ph-cursor"
              />
            </span>
          </div>
          <div class="home-composer__toolbar flex flex-wrap items-center gap-2 border-t px-3 py-2.5">
            <button
              type="button"
              class="home-composer__plus flex h-8 w-8 shrink-0 items-center justify-center border text-txt2 hover:text-txt disabled:opacity-40"
              :disabled="composerBusy"
              :title="t('pages.clarify.addImage')"
              data-testid="home-composer-plus"
              data-home-plus
              :aria-haspopup="mentionEnabled ? 'menu' : undefined"
              :aria-expanded="mentionEnabled ? plusOpen : undefined"
              @click="onPlus"
            >
              <Icon name="plus" :size="16" />
            </button>
            <Transition name="home-mode-controls" mode="out-in">
              <div v-if="workflowMode" key="workflow" class="home-mode-controls flex min-w-0 flex-wrap items-center gap-2">
                <label class="sr-only" for="home-workflow-select">{{ t('pages.dashboard.pickWorkflow') }}</label>
                <HomeWorkflowSelect
                  :workflows="homeWorkflows"
                  :model-value="selectedWorkflowId"
                  :disabled="workflowSending"
                  @update:model-value="workflow.selectWorkflow"
                  @create="openCreateBaseline"
                />
                <SoloModelSelect
                  v-if="showLaunchModel"
                  :model-value="launchModelId"
                  :reasoning-effort="launchReasoningEffort"
                  :models="launchModelChoices"
                  :disabled="workflowSending"
                  @update:model-value="workflow.selectLaunchModel"
                  @update:reasoning-effort="workflow.selectLaunchEffort"
                />
                <HomePrioritySelect
                  :model-value="launchPriority"
                  :disabled="!homeWorkflows.length || workflowSending"
                  @update:model-value="workflow.selectPriority"
                />
              </div>
              <div v-else key="solo" class="home-mode-controls flex min-w-0 flex-wrap items-center gap-2">
                <SoloAgentSelect
                  v-model="soloAgentName"
                  :agents="soloAgents"
                  :loading="solo.loading.value"
                  :disabled="solo.creating.value"
                />
                <SoloProjectSelect
                  v-model="soloProjectIds"
                  :branches="soloBranches"
                  :projects="soloProjects"
                  :loading="solo.loading.value"
                  :disabled="solo.creating.value"
                  @update:branch="solo.setProjectBranch"
                  @create="projectDrawerOpen = true"
                />
                <SoloModelSelect
                  v-model="soloInitialModelId"
                  v-model:reasoning-effort="soloReasoningEffort"
                  :models="soloModelChoices"
                  :disabled="solo.creating.value"
                />
              </div>
            </Transition>

            <div class="flex-1" />
            <button
              type="submit"
              class="home-composer__send flex h-8 w-8 shrink-0 items-center justify-center text-base disabled:opacity-[0.28]"
              data-testid="home-composer-send"
              :disabled="composerBusy || !composerCanSend"
              :aria-label="workflowMode ? t('pages.dashboard.send') : t('pages.solo.home.start')"
              :title="t('pages.dashboard.sendShortcut')"
            >
              <Icon v-if="composerBusy && !workflowMode" name="spinner" :size="15" />
              <Icon v-else name="arrow-up" :size="16" />
            </button>
          </div>
        </form>
        </div>
      </AttachmentDropZone>

      <Transition name="home-mode-tail" mode="out-in">
        <HomeSoloPanel
          v-if="!workflowMode"
          key="solo"
          :projects="soloProjects"
          :project-ids="soloProjectIds"
          @toggle-project="solo.toggleProject"
          @new-project="projectDrawerOpen = true"
        />
        <HomeWorkflowRail
          v-else
          key="workflow"
          class="home-mode-tail"
          :workflows="homeWorkflows"
          :unpublished="unpublishedHomeWorkflows"
          :selected-id="selectedWorkflow?.id || ''"
          :revealed="workflowRailRevealed"
          :load-error="workflowsLoadError"
          :hiding-workflow-id="hidingWorkflowId"
          @select="workflow.selectWorkflow"
          @create="openCreateBaseline"
          @retry="workflow.load()"
          @hide="workflow.hideWorkflowFromHome"
          @edit="goEditWorkflow"
        />
      </Transition>
    </div>
    <ProjectDrawer :open="projectDrawerOpen" @close="projectDrawerOpen = false" @saved="onProjectSaved" />
    <SoloLimitModal :open="soloLimitOpen" @close="soloLimitOpen = false" />
    <HomeCreateBaselineModal :open="createBaselineOpen" @close="createBaselineOpen = false" @created="onBaselineCreated" />
    <AppModal
      :open="onboardingGateOpen"
      :title="t('pages.workspace.newWorkflow.agentsRequiredTitle')"
      :width="440"
      close-on-esc
      data-testid="home-onboarding-agents-gate"
      @close="onboardingGateOpen = false"
    >
      <p class="text-[13px] leading-relaxed text-txt2" data-testid="home-onboarding-agents-gate-body">
        {{ t('pages.workspace.newWorkflow.agentsRequired', { missing: onboardingGateMissing.join(t('pages.onboarding.team.listSep')) }) }}
      </p>
      <template #footer>
        <AppButton variant="ghost" data-testid="home-onboarding-agents-gate-dismiss" @click="onboardingGateOpen = false">
          {{ t('common.buttons.cancel') }}
        </AppButton>
        <AppButton variant="primary" data-testid="home-onboarding-agents-gate-open" @click="finishOnboardingFromGate">
          {{ t('pages.workspace.newWorkflow.goOnboarding') }}
        </AppButton>
      </template>
    </AppModal>
    <RunLaunchModal
      :open="launchOpen"
      :workflow-id="launchTarget?.id || ''"
      :workflow-name="launchTarget?.name || ''"
      :fields="runFields"
      :run-inputs="runInputs"
      :run-images="runImages"
      :draft-restored="draftRestored"
      :run-title="launchTitle"
      :first-message="launchFirstMessage"
      :initial-priority="launchPriority"
      :launch-model="launchModelForModal"
      published-snapshot
      @close="workflow.closeLaunch()"
      @stayed="workflow.closeLaunch()"
      @started="onWorkflowLaunchStarted($event)"
    />
    <ChatImagePreviewModal :open="!!imagePreview" :src="imagePreview?.src || ''" :label="imagePreview?.label || ''" test-id-prefix="home-image-preview" @close="closeChatImagePreview" />
  </div>
</template>

<style scoped>
/* Inherit the app palette so the home canvas and surrounding shell stay continuous. */
.home-shell {
  isolation: isolate;
  --home-shadow: 0 0 0 / 0.18;
  --home-highlight: 255 255 255 / 0.04;
}

html.light .home-shell {
  --home-shadow: 0 0 0 / 0.045;
  --home-highlight: 255 255 255 / 0.9;
}

.home-shell :deep(.home-particle-mesh__canvas) {
  opacity: 0.2;
}

html.light .home-shell :deep(.home-particle-mesh__canvas) {
  opacity: 0.1;
}

/* Top inset shrinks before the column scrolls. Basis 120px, floor 48px:
   the 72px of slack absorbs exactly one image-chip row (56px thumb + 6px
   pt-1.5 + 8px mb-2 = 70px). A second chip row or the attach notice still
   scrolls; re-check this budget if the chip thumb size changes.
   The 520px breakpoint below uses a 4.25rem basis. Its floor is 36px so the
   ear that rises ~30px above the 240×200 crop at float-high plus jump-high
   stays inside this scrolling column. */
.home-shell__content {
  padding-bottom: 40px;
  scrollbar-width: none;
}

.home-shell__content::-webkit-scrollbar {
  display: none;
  width: 0;
  height: 0;
}

.home-shell__content::before {
  content: '';
  width: 100%;
  flex: 0 1 120px;
  min-height: 48px;
}

/* Mode-specific controls and panels can have very different heights. Freeze
   the current inset for that switch so the hero stays anchored in place. */
.home-shell__content.home-shell__content--mode-locked::before {
  flex: 0 0 var(--home-mode-top-inset);
  min-height: var(--home-mode-top-inset);
}

/* brand: fill LOCKED — cropped yume + copy; no wordmark / logo-mark */
.home-brand-block {
  --yume-w: 240px;
  --yume-h: 200px;
  display: flex;
  flex-direction: column;
  align-items: center;
  flex-shrink: 0;
  width: 100%;
}

/* plan g1.3 — whole-sentence unified color/weight; no .em split highlight */
.home-hero-copy {
  margin-top: 6px; /* after crop; optical ~4-6px */
  max-width: 100%;
  font-size: 17px;
  font-weight: 600;
  letter-spacing: 0.04em;
  color: rgb(var(--c-txt));
  text-align: center;
  line-height: 1.45;
  overflow-wrap: anywhere;
  word-break: break-word;
  position: relative;
  z-index: 1;
}

/* plan g1.1 — shell/hero composer 16px; overflow+bg+radius same layer */
.home-composer {
  border-color: rgb(var(--c-line));
  background: rgb(var(--c-surface));
  border-radius: 16px;
  overflow: hidden;
  box-shadow:
    inset 0 1px 0 rgb(var(--home-highlight)),
    0 2px 3px rgb(var(--home-shadow)),
    0 10px 28px rgb(var(--home-shadow));
  transition:
    border-color 0.22s ease,
    box-shadow 0.22s ease;
}

.home-composer:hover {
  border-color: rgb(var(--c-line-strong));
}

.home-composer:focus-within {
  border-color: rgb(var(--c-accent) / 0.65);
  box-shadow: 0 0 0 3px rgb(var(--c-accent) / 0.07), 0 6px 22px rgb(var(--home-shadow));
}

.home-composer__toolbar {
  border-color: rgb(var(--c-line) / 0.55);
  background: color-mix(in srgb, rgb(var(--c-base)) 35%, rgb(var(--c-surface)));
}

html.light .home-composer__toolbar {
  background: rgb(var(--c-surface));
}

.home-mode-controls { flex: 0 1 auto; }
.home-mode-controls-enter-active, .home-mode-controls-leave-active { transition: opacity 0.18s ease, transform 0.18s var(--ease-out-expo); }
.home-mode-controls-enter-from { opacity: 0; transform: translateY(4px); }
.home-mode-controls-leave-to { opacity: 0; transform: translateY(-4px); }
.home-mode-tail-enter-active, .home-mode-tail-leave-active { transition: opacity 0.24s ease, transform 0.24s var(--ease-out-expo); }
.home-mode-tail-enter-from { opacity: 0; transform: translateY(8px); }
.home-mode-tail-leave-to { opacity: 0; transform: translateY(-5px); }

.home-composer__input {
  min-height: 88px;
  line-height: 1.55;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  word-break: break-word;
}

.home-composer__ph {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  word-break: break-word;
  line-height: 1.55;
}

.home-composer__ph-cursor {
  display: inline-block;
  width: 1.5px;
  height: 1.05em;
  margin-left: 1px;
  vertical-align: -2px;
  background: rgb(var(--c-accent));
  animation: home-ph-caret 1s steps(1) infinite;
}

.home-composer__plus {
  border-color: rgb(var(--c-line));
  background: rgb(var(--c-surface));
  border-radius: 8px;
  box-shadow: inset 0 1px 0 rgb(var(--home-highlight)), 0 1px 2px rgb(var(--home-shadow));
  transition: border-color 0.15s ease, color 0.15s ease, background-color 0.15s ease;
}

.home-composer__plus:hover:not(:disabled) {
  border-color: rgb(var(--c-line-strong));
  color: rgb(var(--c-txt));
  background: rgb(var(--c-accent) / 0.04);
}

.home-composer__send {
  background: linear-gradient(155deg, rgb(var(--c-accent) / 0.85), rgb(var(--c-accent)));
  color: rgb(var(--c-base));
  border-radius: 8px;
  box-shadow: inset 0 1px 0 rgb(var(--home-highlight)), 0 2px 4px rgb(var(--home-shadow));
  transition: transform 0.16s ease, box-shadow 0.2s ease;
}

.home-composer__send:hover:not(:disabled) {
  transform: translateY(-1px);
}

.home-composer__send:active:not(:disabled) {
  transform: scale(0.96);
}

html.light .home-composer__send {
  background: linear-gradient(155deg, rgb(var(--c-accent) / 0.85), rgb(var(--c-accent)));
  color: #fff;
}

.home-composer__send:disabled {
  background: rgb(var(--c-txt3));
  box-shadow: none;
}

html.light .home-composer__send:disabled {
  background: rgb(var(--c-line-strong));
}


.home-composer-wrap { margin-top: 22px; }
@keyframes home-ph-caret { 0%, 49% { opacity: 1; } 50%, 100% { opacity: 0; } }
@media (prefers-reduced-motion: reduce) {
  .home-composer, .home-composer__plus, .home-composer__send, .home-mode-controls, .home-mode-tail { transition: none; transform: none; }
  .home-mode-controls-enter-active, .home-mode-controls-leave-active, .home-mode-tail-enter-active, .home-mode-tail-leave-active { transition: none; }
  .home-composer__ph-cursor { animation: none; }
  .home-composer__send:hover:not(:disabled), .home-composer__send:active:not(:disabled) { transform: none; }
}
@media (max-width: 520px) {
  .home-shell__content { padding-bottom: 2.5rem; }
  .home-shell__content::before { flex-basis: 4.25rem; min-height: 36px; }
}
</style>
