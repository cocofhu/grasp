<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import HomeParticleMeshBackground from '@/components/dashboard/HomeParticleMeshBackground.vue'
import HomePipelineSelect from '@/components/dashboard/HomePipelineSelect.vue'
import HomePrioritySelect from '@/components/dashboard/HomePrioritySelect.vue'
import Icon from '@/components/ui/Icon.vue'
import ChatImageThumb from '@/components/ui/ChatImageThumb.vue'
import ChatImagePreviewModal from '@/components/ui/ChatImagePreviewModal.vue'
import HomeCreateBaselineModal from '@/components/dashboard/HomeCreateBaselineModal.vue'
import RunLaunchModal from '@/components/workflow/RunLaunchModal.vue'
import { useChatImagePreview } from '@/lib/composables/useChatImagePreview'
import { useHomeApproveChat } from '@/lib/run/useHomeApproveChat'
import { attachmentDisplayName, isImageAttachment } from '@/lib/shared/attachments'
import { imgSrc } from '@/lib/shared/compositeText'
import { useBrandSettings } from '@/lib/composables/useBrandSettings'
import type { Workflow } from '@/lib/shared/types'

const { preview: imagePreview, openChatImagePreview, closeChatImagePreview } = useChatImagePreview()

const router = useRouter()
const { t, tm } = useI18n()
const { productName, homeSubtitle } = useBrandSettings()
const effectiveSubtitle = computed(() => homeSubtitle.value || String(t('pages.dashboard.title')))
const {
  projectId,
  pipelines,
  selected,
  selectedId,
  launchPriority,
  draft,
  sending,
  hidingPipelineId,
  canSend,
  loading,
  loadError,
  launchOpen,
  launchTarget,
  launchTitle,
  launchFirstMessage,
  runFields,
  runInputs,
  runImages,
  draftRestored,
  attachments,
  fileInput,
  attachNotice,
  onPickFiles,
  onPaste,
  removeAttachment,
  load,
  reloadAfterCreate,
  selectPipeline,
  selectPriority,
  hidePipelineFromHome,
  send,
  closeLaunch,
  onLaunchStarted,
} = useHomeApproveChat()

const brandVisible = ref('')
/** Keep caret in layout; hide with opacity so settle does not shift the centered brand. */
const brandCursorGone = ref(false)
const brandCursorBlink = ref(false)
const brandTimers: ReturnType<typeof setTimeout>[] = []
const composerFocused = ref(false)
const composing = ref(false)
const textareaRef = ref<HTMLTextAreaElement | null>(null)
const overflowScroll = ref(false)
const pipelineCardsEl = ref<HTMLDivElement | null>(null)
const pipelineCanScrollPrev = ref(false)
const pipelineCanScrollNext = ref(false)
const pipelineFadeLeft = ref(false)
const pipelineFadeRight = ref(false)
const pipelineOverflows = ref(false)
let pipelineStripObserver: ResizeObserver | null = null
const phVisible = ref('')
const phCursor = ref(false)
let phTimer: ReturnType<typeof setTimeout> | null = null
let phHoldTimer: ReturnType<typeof setTimeout> | null = null
const createBaselineOpen = ref(false)
const pipelineMenuOpen = ref(false)
const pipelineMenuX = ref(0)
const pipelineMenuY = ref(0)
const pipelineMenuTarget = ref<Workflow | null>(null)
let longPressTimer: ReturnType<typeof setTimeout> | null = null
let longPressStart: { x: number; y: number } | null = null
let suppressNextCardClick = false

const PIPELINE_MENU_WIDTH = 168
const PIPELINE_MENU_HEIGHT = 70
const PIPELINE_MENU_MARGIN = 8
const LONG_PRESS_MS = 500
const LONG_PRESS_MOVE_PX = 10

const placeholderLines = computed(() => {
  const raw = tm('pages.dashboard.placeholders') as unknown
  if (Array.isArray(raw) && raw.length > 0) {
    const lines = raw.filter((s): s is string => typeof s === 'string' && s.trim().length > 0)
    if (lines.length > 0) return lines
  }
  const single = String(t('pages.dashboard.placeholder')).trim()
  return single ? [single] : []
})
const placeholderPrimary = computed(() => placeholderLines.value[0] ?? '')
const showPhTypewriter = computed(() => !draft.value.trim() && !composerFocused.value)

function prefersReducedMotion(): boolean {
  if (typeof window === 'undefined' || !window.matchMedia) return false
  return window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

function clearBrandTimers() {
  while (brandTimers.length) {
    const id = brandTimers.pop()
    if (id != null) clearTimeout(id)
  }
}

function scheduleBrand(fn: () => void, ms: number) {
  brandTimers.push(setTimeout(fn, ms))
}

/** Monospace brand: type once, soft blink caret, then opacity-hide caret (keep box). */
function runBrandTypewriter() {
  clearBrandTimers()
  brandCursorBlink.value = false
  brandCursorGone.value = false
  if (prefersReducedMotion()) {
    brandVisible.value = productName.value
    brandCursorGone.value = true
    return
  }
  brandVisible.value = ''
  let i = 0
  const typeNext = () => {
    if (i < productName.value.length) {
      i += 1
      brandVisible.value = productName.value.slice(0, i)
      scheduleBrand(typeNext, 78)
      return
    }
    brandCursorBlink.value = true
    scheduleBrand(() => {
      brandCursorBlink.value = false
      brandCursorGone.value = true
    }, 850 * 3)
  }
  scheduleBrand(typeNext, 220)
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
}

function onComposerKeydown(e: KeyboardEvent) {
  // plan g1.1 — Ctrl/⌘+Enter sends; bare Enter / Shift+Enter insert newline
  if (e.key !== 'Enter') return
  if (composing.value || e.isComposing || e.keyCode === 229) return
  if (!(e.ctrlKey || e.metaKey)) return
  e.preventDefault()
  void send()
}

function onComposerFocus() {
  composerFocused.value = true
}

function onComposerBlur() {
  composerFocused.value = false
}

function goProjects() {
  void router.push('/projects')
}

function openCreateBaseline(e?: Event) {
  e?.preventDefault()
  e?.stopPropagation()
  closePipelineMenu()
  clearLongPress()
  createBaselineOpen.value = true
}

/** plan g2.1 — HomePipelineSelect create footer → same baseline modal as rail card */
function onCreateFromSelect() {
  openCreateBaseline()
}

async function onBaselineCreated(payload?: { id?: string }) {
  createBaselineOpen.value = false
  await reloadAfterCreate(payload?.id)
}

function closePipelineMenu() {
  pipelineMenuOpen.value = false
  pipelineMenuTarget.value = null
}

function positionPipelineMenu(x: number, y: number) {
  const maxX = Math.max(PIPELINE_MENU_MARGIN, window.innerWidth - PIPELINE_MENU_WIDTH - PIPELINE_MENU_MARGIN)
  const maxY = Math.max(PIPELINE_MENU_MARGIN, window.innerHeight - PIPELINE_MENU_HEIGHT - PIPELINE_MENU_MARGIN)
  pipelineMenuX.value = Math.min(Math.max(PIPELINE_MENU_MARGIN, x), maxX)
  pipelineMenuY.value = Math.min(Math.max(PIPELINE_MENU_MARGIN, y), maxY)
}

function openPipelineMenu(pipeline: Workflow, x: number, y: number) {
  pipelineMenuTarget.value = pipeline
  positionPipelineMenu(x, y)
  pipelineMenuOpen.value = true
}

function onPipelineContextMenu(e: MouseEvent, pipeline: Workflow) {
  e.preventDefault()
  e.stopPropagation()
  clearLongPress()
  openPipelineMenu(pipeline, e.clientX, e.clientY)
}

function onPipelineMore(e: MouseEvent, pipeline: Workflow) {
  e.stopPropagation()
  const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
  openPipelineMenu(pipeline, rect.right, rect.bottom + 4)
}

function clearLongPress() {
  if (longPressTimer != null) {
    clearTimeout(longPressTimer)
    longPressTimer = null
  }
  longPressStart = null
}

function onPipelinePointerDown(e: PointerEvent, pipeline: Workflow) {
  if (e.pointerType !== 'touch') return
  clearLongPress()
  longPressStart = { x: e.clientX, y: e.clientY }
  longPressTimer = setTimeout(() => {
    longPressTimer = null
    suppressNextCardClick = true
    openPipelineMenu(pipeline, e.clientX, e.clientY)
  }, LONG_PRESS_MS)
}

function onPipelinePointerMove(e: PointerEvent) {
  if (!longPressStart) return
  if (
    Math.hypot(e.clientX - longPressStart.x, e.clientY - longPressStart.y)
    > LONG_PRESS_MOVE_PX
  ) {
    clearLongPress()
  }
}

function onPipelineCardClick(id: string) {
  clearLongPress()
  if (suppressNextCardClick) {
    suppressNextCardClick = false
    return
  }
  selectPipeline(id)
}

async function hideMenuPipeline() {
  const target = pipelineMenuTarget.value
  if (!target) return
  closePipelineMenu()
  await hidePipelineFromHome(target)
}

function editMenuPipeline() {
  const target = pipelineMenuTarget.value
  if (!target) return
  closePipelineMenu()
  void router.push(`/workflows/${target.id}/edit`)
}

function onWindowPointerDown() {
  if (pipelineMenuOpen.value) closePipelineMenu()
}

function onWindowKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') closePipelineMenu()
}

function onComposerSubmit(e: Event) {
  e.preventDefault()
  void send()
}

const PIPELINE_SCROLL_EPS = 2

function pipelineCardStep(): number {
  const rail = pipelineCardsEl.value
  if (!rail) return 204
  const card = rail.querySelector('.home-shell__card')
  if (!card) return 204
  const styles = getComputedStyle(rail)
  const gap = parseFloat(styles.columnGap || styles.gap || '12') || 12
  return card.getBoundingClientRect().width + gap
}

function syncPipelineNav() {
  const rail = pipelineCardsEl.value
  if (!rail) {
    pipelineCanScrollPrev.value = false
    pipelineCanScrollNext.value = false
    pipelineFadeLeft.value = false
    pipelineFadeRight.value = false
    pipelineOverflows.value = false
    return
  }
  const max = Math.max(0, rail.scrollWidth - rail.clientWidth)
  const left = rail.scrollLeft
  const atStart = left <= PIPELINE_SCROLL_EPS
  const atEnd = left >= max - PIPELINE_SCROLL_EPS
  const overflow = max > PIPELINE_SCROLL_EPS
  pipelineOverflows.value = overflow
  pipelineCanScrollPrev.value = overflow && !atStart
  pipelineCanScrollNext.value = overflow && !atEnd
  pipelineFadeLeft.value = overflow && !atStart
  pipelineFadeRight.value = overflow && !atEnd
}

function scrollPipelineByDir(dir: number) {
  const rail = pipelineCardsEl.value
  if (!rail) return
  const delta = pipelineCardStep() * dir
  if (prefersReducedMotion()) {
    rail.classList.add('home-pipeline-rail--instant')
    rail.scrollLeft += delta
    requestAnimationFrame(() => rail.classList.remove('home-pipeline-rail--instant'))
  } else {
    rail.scrollBy({ left: delta, behavior: 'smooth' })
  }
}

function onPipelineWheel(e: WheelEvent) {
  closePipelineMenu()
  const rail = pipelineCardsEl.value
  if (!rail) return
  if (Math.abs(e.deltaY) > Math.abs(e.deltaX) && !e.shiftKey) return
  const dx = e.shiftKey ? e.deltaY : e.deltaX
  if (!dx) return
  e.preventDefault()
  rail.scrollLeft += dx
}

function bindPipelineStripObserver() {
  if (typeof ResizeObserver === 'undefined') return
  pipelineStripObserver?.disconnect()
  pipelineStripObserver = null
  if (!pipelineCardsEl.value) return
  pipelineStripObserver = new ResizeObserver(() => syncPipelineNav())
  pipelineStripObserver.observe(pipelineCardsEl.value)
}

function openFilePicker() {
  fileInput.value?.click()
}

watch(draft, () => nextTick(autoGrow))
watch(productName, () => runBrandTypewriter(), { immediate: true })
watch(showPhTypewriter, () => runPlaceholderTypewriter(), { immediate: true })
watch(placeholderLines, () => {
  if (showPhTypewriter.value) runPlaceholderTypewriter()
})

watch(
  () => pipelines.value.length,
  () => nextTick(() => {
    syncPipelineNav()
    bindPipelineStripObserver()
  }),
)

onMounted(() => {
  nextTick(() => {
    autoGrow()
    syncPipelineNav()
    bindPipelineStripObserver()
  })
  window.addEventListener('resize', syncPipelineNav)
  window.addEventListener('pointerdown', onWindowPointerDown)
  window.addEventListener('keydown', onWindowKeydown)
  window.addEventListener('scroll', closePipelineMenu, true)
})

onBeforeUnmount(() => {
  clearBrandTimers()
  clearPhTimers()
  clearLongPress()
  pipelineStripObserver?.disconnect()
  pipelineStripObserver = null
  window.removeEventListener('resize', syncPipelineNav)
  window.removeEventListener('pointerdown', onWindowPointerDown)
  window.removeEventListener('keydown', onWindowKeydown)
  window.removeEventListener('scroll', closePipelineMenu, true)
})
</script>

<template>
  <div data-testid="dashboard-view" class="home-shell relative flex h-full min-h-0 flex-col">
    <HomeParticleMeshBackground />
    <div
      class="home-shell__content relative z-[1] mx-auto flex w-full max-w-3xl min-h-0 flex-1 flex-col items-center justify-center overflow-y-auto px-4 py-10"
    >
      <h1 class="home-brand" data-testid="home-brand" :aria-label="productName">
        <span class="home-brand__text" data-testid="home-brand-text">{{ brandVisible }}</span>
        <span
          class="home-brand__cursor"
          :class="{
            'home-brand__cursor--blink': brandCursorBlink,
            'home-brand__cursor--gone': brandCursorGone,
          }"
          data-testid="home-brand-cursor"
          aria-hidden="true"
        />
      </h1>
      <p class="home-hint mt-[18px] text-center" data-testid="home-title">
        {{ effectiveSubtitle }}
      </p>

      <div class="mt-[30px] w-full">
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
          class="mb-2 flex flex-wrap gap-2"
          data-testid="home-attach-chips"
        >
          <div v-for="(im, ii) in attachments" :key="ii" class="relative">
            <ChatImageThumb
              v-if="isImageAttachment(im)"
              mode="previewable"
              size="sm"
              thumb-class="rounded"
              :src="imgSrc(im)"
              :label="attachmentDisplayName(im, ii)"
              :alt="attachmentDisplayName(im, ii)"
              test-id="home-draft-image-thumb"
              @preview="openChatImagePreview(imgSrc(im), attachmentDisplayName(im, ii))"
            />
            <div
              v-else
              class="flex h-9 max-w-[160px] items-center gap-1.5 rounded border border-line bg-elevated px-2.5"
              :title="attachmentDisplayName(im, ii)"
              data-testid="home-pending-file-chip"
            >
              <span class="shrink-0 text-[9px] font-semibold uppercase text-info">DOC</span>
              <span class="min-w-0 truncate text-[11px] text-txt2">{{
                attachmentDisplayName(im, ii)
              }}</span>
            </div>
            <button
              type="button"
              class="absolute -right-1.5 -top-1.5 flex h-4 w-4 items-center justify-center rounded bg-err text-white"
              data-testid="home-attach-remove"
              @click.stop="removeAttachment(ii)"
            >
              <Icon name="close" :size="9" />
            </button>
          </div>
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
              :disabled="sending"
              autocomplete="off"
              @input="onTextInput"
              @keydown="onComposerKeydown"
              @compositionstart="composing = true"
              @compositionend="composing = false"
              @paste="onPaste"
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
          <div class="home-composer__toolbar flex items-center gap-2 border-t px-3 py-2.5">
            <button
              type="button"
              class="home-composer__plus flex h-8 w-8 shrink-0 items-center justify-center border text-txt2 hover:text-txt disabled:opacity-40"
              :disabled="sending"
              :title="t('pages.clarify.addImage')"
              data-testid="home-composer-plus"
              @click="openFilePicker"
            >
              <Icon name="plus" :size="16" />
            </button>
            <label class="sr-only" for="home-pipeline-select">{{ t('pages.dashboard.pickPipeline') }}</label>
            <HomePipelineSelect
              :pipelines="pipelines"
              :model-value="selectedId"
              :disabled="sending"
              @update:model-value="selectPipeline"
              @create="onCreateFromSelect"
            />
            <HomePrioritySelect
              :model-value="launchPriority"
              :disabled="!pipelines.length || sending"
              @update:model-value="selectPriority"
            />
            <div class="flex-1" />
            <button
              type="submit"
              class="home-composer__send flex h-8 w-8 shrink-0 items-center justify-center text-base disabled:opacity-[0.28]"
              data-testid="home-composer-send"
              :disabled="sending || !canSend"
              :aria-label="t('pages.dashboard.send')"
              :title="t('pages.dashboard.sendShortcut')"
            >
              <Icon name="arrow-up" :size="16" />
            </button>
          </div>
        </form>
      </div>

      <div
        v-if="loadError"
        class="mt-6 flex w-full flex-wrap items-center justify-between gap-2 rounded border border-err/40 bg-err/10 px-3 py-2 text-[13px] text-err"
        data-testid="dashboard-load-error"
      >
        <span>{{ t('pages.board.loadFailed') }}</span>
        <button
          type="button"
          class="rounded-md border border-err/40 px-2.5 py-1 text-xs text-err hover:bg-err/10"
          data-testid="dashboard-retry"
          @click="load()"
        >
          {{ t('pages.board.retry') }}
        </button>
      </div>

      <div v-else-if="loading" class="mt-10 text-sm text-txt3" data-testid="home-pipelines-loading">
        {{ t('pages.board.loading') }}
      </div>

      <div v-else-if="!pipelines.length" class="mt-10 text-center" data-testid="home-pipelines-empty">
        <p class="text-sm text-txt3">{{ t('pages.dashboard.noPipelines') }}</p>
        <button
          type="button"
          class="mt-3 rounded-md border border-line px-3 py-1.5 text-[13px] text-txt2 hover:bg-elevated"
          data-testid="home-go-projects"
          @click="goProjects"
        >
          {{ t('pages.dashboard.goProjects') }}
        </button>
      </div>

      <div
        v-if="!loadError"
        class="home-pipeline-rail-wrap w-full"
        :class="{
          'mt-10': loading || pipelines.length > 0,
          'mt-4': !loading && pipelines.length === 0,
          'home-pipeline-rail-wrap--has-left': pipelineFadeLeft,
          'home-pipeline-rail-wrap--has-right': pipelineFadeRight,
        }"
        data-testid="home-pipeline-rail-wrap"
      >
        <button
          type="button"
          class="home-pipeline-nav home-pipeline-nav--prev"
          data-testid="home-pipeline-scroll-prev"
          :disabled="!pipelineCanScrollPrev"
          :aria-label="t('pages.dashboard.scrollLeft')"
          :title="t('pages.dashboard.scrollLeft')"
          @click="scrollPipelineByDir(-1)"
        >
          <Icon name="chevron-left" :size="16" />
        </button>
        <div class="home-pipeline-fade home-pipeline-fade--left" aria-hidden="true" />
        <div class="home-pipeline-fade home-pipeline-fade--right" aria-hidden="true" />

        <div
          ref="pipelineCardsEl"
          class="home-pipeline-rail flex w-full gap-3 pb-1"
          :class="{ 'home-pipeline-rail--overflow': pipelineOverflows }"
          data-testid="home-pipeline-cards"
          tabindex="0"
          role="list"
          @scroll.passive="syncPipelineNav"
          @wheel="onPipelineWheel"
        >
          <div
            v-for="p in pipelines"
            :key="p.id"
            tabindex="0"
            role="listitem"
            class="home-shell__card w-48 shrink-0 overflow-hidden rounded-lg border border-line p-0 text-left"
            :class="p.id === selected?.id ? 'home-shell__card--selected' : 'hover:border-line-strong'"
            :data-testid="`home-pipeline-card-${p.id}`"
            @click="onPipelineCardClick(p.id)"
            @keydown.enter.space.prevent="selectPipeline(p.id)"
            @contextmenu="onPipelineContextMenu($event, p)"
            @pointerdown="onPipelinePointerDown($event, p)"
            @pointermove="onPipelinePointerMove"
            @pointerup="clearLongPress"
            @pointercancel="clearLongPress"
            @pointerleave="clearLongPress"
          >
            <button
              type="button"
              class="home-pipeline-more absolute right-2 top-2 z-[1] flex h-7 w-7 items-center justify-center rounded-lg text-txt2"
              :aria-label="t('pages.dashboard.pipelineMenu.more')"
              :title="t('pages.dashboard.pipelineMenu.more')"
              :data-testid="`home-pipeline-more-${p.id}`"
              @click="onPipelineMore($event, p)"
            >
              <Icon name="more" :size="16" />
            </button>
            <div class="home-shell__card-top flex h-20 items-center justify-center">
              <span class="flex items-center gap-1.5">
                <span class="h-2 w-2 bg-txt3" />
                <span class="h-px w-6 bg-line-strong" />
                <span class="h-2.5 w-2.5 bg-accent" />
                <span class="h-px w-6 bg-line-strong" />
                <span class="h-2 w-2 bg-txt3" />
              </span>
            </div>
            <div class="px-3 py-2.5">
              <div
                class="truncate text-[13px] font-medium text-txt"
                :title="p.name"
                data-testid="home-pipeline-card-name"
              >{{ p.name }}</div>
              <div
                v-if="p.projectName"
                class="mt-0.5 truncate text-[11px] text-txt2"
                :title="p.projectName"
                :data-testid="`home-pipeline-card-project-${p.id}`"
              >{{ p.projectName }}</div>
              <div
                class="mt-0.5 line-clamp-2 text-[11px] text-txt3"
                :title="p.description || t('pages.dashboard.cardFallback')"
              >
                {{ p.description || t('pages.dashboard.cardFallback') }}
              </div>
            </div>
          </div>
          <button
            type="button"
            role="listitem"
            class="home-shell__card home-shell__card--add w-48 shrink-0"
            data-testid="home-new-workflow"
            @click="openCreateBaseline"
            @contextmenu.prevent
            @pointerdown.stop
          >
            <span class="home-shell__card-plus" aria-hidden="true">
              <Icon name="plus" :size="20" />
            </span>
            <span class="home-shell__card-add-label">{{ t('pages.dashboard.create.addCard') }}</span>
          </button>
        </div>

        <button
          type="button"
          class="home-pipeline-nav home-pipeline-nav--next"
          data-testid="home-pipeline-scroll-next"
          :disabled="!pipelineCanScrollNext"
          :aria-label="t('pages.dashboard.scrollRight')"
          :title="t('pages.dashboard.scrollRight')"
          @click="scrollPipelineByDir(1)"
        >
          <Icon name="chevron-right" :size="16" />
        </button>
      </div>
    </div>

    <Teleport to="body">
      <div
        v-if="pipelineMenuOpen && pipelineMenuTarget"
        class="home-pipeline-menu fixed z-[9999] min-w-[168px] rounded-lg border border-line bg-elevated py-1 shadow-card"
        role="menu"
        data-testid="home-pipeline-menu"
        :style="{ left: pipelineMenuX + 'px', top: pipelineMenuY + 'px' }"
        @pointerdown.stop
        @click.stop
      >
        <button
          type="button"
          class="home-pipeline-menu__item"
          role="menuitem"
          data-testid="home-pipeline-menu-hide"
          :disabled="hidingPipelineId === pipelineMenuTarget.id"
          @click="hideMenuPipeline"
        >
          <Icon name="eye-off" :size="14" aria-hidden="true" />
          <span>{{ t('pages.dashboard.pipelineMenu.hide') }}</span>
        </button>
        <button
          type="button"
          class="home-pipeline-menu__item"
          role="menuitem"
          data-testid="home-pipeline-menu-edit"
          @click="editMenuPipeline"
        >
          <Icon name="edit" :size="14" aria-hidden="true" />
          <span>{{ t('pages.dashboard.pipelineMenu.edit') }}</span>
        </button>
      </div>
    </Teleport>

    <HomeCreateBaselineModal
      :open="createBaselineOpen"
      @close="createBaselineOpen = false"
      @created="onBaselineCreated"
    />

    <RunLaunchModal
      :open="launchOpen"
      :workflow-id="launchTarget?.id || ''"
      :project-id="projectId"
      :workflow-name="launchTarget?.name || ''"
      :fields="runFields"
      :run-inputs="runInputs"
      :run-images="runImages"
      :draft-restored="draftRestored"
      :run-title="launchTitle"
      :first-message="launchFirstMessage"
      :initial-priority="launchPriority"
      @close="closeLaunch()"
      @stayed="closeLaunch()"
      @started="onLaunchStarted($event)"
    />

    <ChatImagePreviewModal
      :open="!!imagePreview"
      :src="imagePreview?.src || ''"
      :label="imagePreview?.label || ''"
      test-id-prefix="home-image-preview"
      @close="closeChatImagePreview"
    />
  </div>
</template>

<style scoped>
/* g1 — clean shell aligned to page.html design tokens */
.home-shell {
  isolation: isolate;
}

/* g1.1 — monospace brand; solid color; letter-spacing matches design */
.home-brand {
  display: inline-flex;
  align-items: baseline;
  margin: 0;
  min-height: 1.1em;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace;
  font-size: clamp(2.5rem, 8.5vw, 4.25rem);
  font-weight: 600;
  letter-spacing: 0.04em;
  line-height: 1.05;
  color: rgb(var(--c-txt));
}

.home-brand__text {
  white-space: pre;
}

.home-brand__cursor {
  display: inline-block;
  width: 0.08em;
  height: 0.92em;
  margin-left: 0.06em;
  vertical-align: -0.06em;
  flex-shrink: 0;
  background: rgb(var(--c-accent));
  opacity: 1;
  transition: opacity 0.2s ease;
}

.home-brand__cursor--blink {
  animation: home-brand-caret 0.85s steps(1) 3;
}

.home-brand__cursor--gone {
  opacity: 0;
}

.home-hint {
  font-size: 14px;
  font-weight: 500;
  letter-spacing: 0.01em;
  color: rgb(var(--c-txt2));
  opacity: 0;
  animation: home-hint-in 0.45s ease-out 0.15s forwards;
}

@keyframes home-hint-in {
  to {
    opacity: 1;
  }
}

/* plan g1.1 — shell/hero composer 16px; overflow+bg+radius same layer */
.home-composer {
  border-color: rgb(var(--c-line));
  background: rgb(var(--c-surface));
  border-radius: 16px;
  overflow: hidden;
}

:global(html.light) .home-composer {
  border-color: rgb(var(--c-line));
  background: rgb(var(--c-elevated));
}

.home-composer__toolbar {
  border-color: rgb(var(--c-line) / 0.55);
}

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
  background: transparent;
  border-radius: 8px;
  transition: border-color 0.15s ease, color 0.15s ease, background-color 0.15s ease;
}

.home-composer__plus:hover:not(:disabled) {
  border-color: rgb(var(--c-line-strong));
  color: rgb(var(--c-txt));
  background: rgb(var(--c-accent) / 0.12);
}

.home-composer__send {
  background: rgb(var(--c-txt));
  color: rgb(var(--c-base));
  border-radius: 8px;
}

:global(html.light) .home-composer__send {
  background: rgb(var(--c-txt));
  color: #fff;
}

/* Card role 12px; selected = accent inset border */
.home-shell__card {
  position: relative;
  background: rgb(var(--c-surface));
  border-radius: 12px;
  box-shadow: none;
  cursor: pointer;
  user-select: none;
  -webkit-touch-callout: none;
}

:global(html.light) .home-shell__card {
  background: rgb(var(--c-elevated));
}

.home-shell__card--selected {
  border-color: rgb(var(--c-accent));
  box-shadow: inset 0 0 0 1px rgb(var(--c-accent));
}

.home-shell__card--add {
  min-height: 148px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  border: 1.5px dashed rgb(var(--c-accent) / 0.45);
  background: linear-gradient(180deg, rgb(var(--c-accent) / 0.08), rgb(var(--c-surface)));
  cursor: pointer;
  user-select: none;
  transition:
    transform 0.2s cubic-bezier(0.16, 1, 0.3, 1),
    box-shadow 0.2s cubic-bezier(0.16, 1, 0.3, 1),
    border-color 0.2s ease;
}
.home-shell__card--add:hover {
  border-color: rgb(var(--c-accent));
  transform: translateY(-3px);
  box-shadow: 0 12px 24px rgb(var(--c-txt) / 0.1);
}
.home-shell__card--add:active {
  transform: translateY(0) scale(0.98);
}
.home-shell__card-plus {
  width: 36px;
  height: 36px;
  border-radius: 10px;
  background: rgb(var(--c-accent));
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: transform 0.24s cubic-bezier(0.16, 1, 0.3, 1);
}
.home-shell__card-plus > svg {
  display: block;
}
.home-shell__card--add:hover .home-shell__card-plus {
  transform: rotate(90deg) scale(1.06);
}
.home-shell__card--add:active .home-shell__card-plus {
  transform: rotate(90deg) scale(0.94);
}
.home-shell__card-add-label {
  font-size: 12px;
  color: rgb(var(--c-accent));
  font-weight: 600;
}

.home-shell__card-top {
  background: rgb(var(--c-elevated));
  border-bottom: 1px solid rgb(var(--c-line) / 0.55);
}

.home-pipeline-more {
  border: 0;
  background: color-mix(in srgb, rgb(var(--c-surface)) 78%, transparent);
}

.home-pipeline-more:hover,
.home-pipeline-more:focus-visible {
  color: rgb(var(--c-txt));
  background: rgb(var(--c-overlay));
  outline: none;
}

.home-pipeline-menu__item {
  display: flex;
  width: 100%;
  align-items: center;
  gap: 8px;
  border: 0;
  background: transparent;
  padding: 7px 14px;
  color: rgb(var(--c-txt2));
  font-size: 12px;
  text-align: left;
}

.home-pipeline-menu__item:hover,
.home-pipeline-menu__item:focus-visible {
  color: rgb(var(--c-txt));
  background: rgb(var(--c-overlay));
  outline: none;
}

.home-pipeline-menu__item svg {
  flex-shrink: 0;
  color: rgb(var(--c-txt3));
}

:global(html.light) .home-shell__card-top {
  background: rgb(244 244 245);
}

/* g2 — pipeline rail: hidden scrollbar + edge arrows (aligned to page.html demo) */
.home-pipeline-rail-wrap {
  position: relative;
}

.home-pipeline-rail {
  justify-content: center;
  overflow-x: auto;
  overflow-y: hidden;
  scroll-behavior: smooth;
  padding: 4px 2px 8px;
  -webkit-overflow-scrolling: touch;
  overscroll-behavior-x: contain;
  scrollbar-width: none;
  -ms-overflow-style: none;
}

.home-pipeline-rail--overflow {
  justify-content: flex-start;
}

.home-pipeline-rail::-webkit-scrollbar {
  width: 0;
  height: 0;
  display: none;
  background: transparent;
}

.home-pipeline-rail--instant {
  scroll-behavior: auto;
}

.home-pipeline-nav {
  position: absolute;
  top: 50%;
  transform: translateY(calc(-50% - 4px));
  z-index: 2;
  width: 36px;
  height: 36px;
  border: 1px solid rgb(var(--c-line));
  border-radius: 8px;
  background: color-mix(in srgb, rgb(var(--c-surface)) 92%, transparent);
  backdrop-filter: blur(6px);
  color: rgb(var(--c-txt2));
  display: grid;
  place-items: center;
  cursor: pointer;
  padding: 0;
  transition:
    opacity 0.2s ease,
    color 0.15s ease,
    border-color 0.15s ease,
    background-color 0.15s ease;
}

.home-pipeline-nav:hover:not(:disabled) {
  color: rgb(var(--c-txt));
  border-color: rgb(var(--c-line-strong));
  background: rgb(var(--c-surface));
}

.home-pipeline-nav:disabled {
  opacity: 0;
  pointer-events: none;
}

.home-pipeline-nav--prev {
  left: -6px;
}

.home-pipeline-nav--next {
  right: -6px;
}

.home-pipeline-fade {
  pointer-events: none;
  position: absolute;
  top: 0;
  bottom: 8px;
  width: 28px;
  z-index: 1;
  opacity: 0;
  transition: opacity 0.2s ease;
}

.home-pipeline-fade--left {
  left: 0;
  background: linear-gradient(90deg, rgb(var(--c-base)), transparent);
}

.home-pipeline-fade--right {
  right: 0;
  background: linear-gradient(270deg, rgb(var(--c-base)), transparent);
}

.home-pipeline-rail-wrap--has-left .home-pipeline-fade--left,
.home-pipeline-rail-wrap--has-right .home-pipeline-fade--right {
  opacity: 1;
}

@keyframes home-brand-caret {
  0%,
  49% {
    opacity: 1;
  }
  50%,
  100% {
    opacity: 0;
  }
}

@keyframes home-ph-caret {
  0%,
  49% {
    opacity: 1;
  }
  50%,
  100% {
    opacity: 0;
  }
}

/* g1.3 — prefers-reduced-motion */
@media (prefers-reduced-motion: reduce) {
  .home-hint {
    animation: none !important;
    opacity: 1;
  }

  .home-brand__cursor {
    opacity: 0 !important;
    animation: none !important;
  }

  .home-composer__ph-cursor {
    animation: none;
    opacity: 1;
  }

  .home-pipeline-rail {
    scroll-behavior: auto;
  }

  .home-shell__card--add,
  .home-shell__card--add:hover,
  .home-shell__card--add:active,
  .home-shell__card--add:hover .home-shell__card-plus,
  .home-shell__card--add:active .home-shell__card-plus {
    transform: none;
  }
}

@media (max-width: 640px) {
  .home-pipeline-nav--prev {
    left: 0;
  }

  .home-pipeline-nav--next {
    right: 0;
  }
}

@media (max-width: 520px) {
  .home-shell__content {
    padding-top: 4.25rem;
    padding-bottom: 2.5rem;
    justify-content: flex-start;
  }

}
</style>
