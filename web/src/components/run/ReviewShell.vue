<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ConfirmFlowOverlay from '@/components/run/ConfirmFlowOverlay.vue'
import { provideConfirmFlowCeremony } from '@/lib/inbox/confirmFlowCeremony'

/**
 * Shared review layout: annotatable product stage + review sidebar.
 * Desktop: horizontal (stage flex-1 | sash | sidebar); sidebar width is
 * internal state (default via sidebarWidth; Run Detail review passes 300),
 * draggable via sash, clamped to [240, min(480, shell−stageMin−sash)],
 * optionally persisted per storageKey.
 * Narrow: vertical with a bottom drawer (draggable height); no horizontal sash.
 * Mobile drawer clamps to [handle hit min, shell height] so it can fill top/bottom.
 * Hosts ConfirmFlowOverlay over stage+sidebar only (not app chrome / node rail).
 */

const SIDEBAR_MIN = 240
const SIDEBAR_MAX = 480
/** Desktop stage floor only — mobile drawer no longer reserves STAGE_MIN. */
const STAGE_MIN = 160
const SASH_WIDTH = 4
const DRAG_THRESHOLD_PX = 3

/** Visual handle bar is thin; hit target / drawer floor is this minimum. */
const HANDLE_HIT_MIN = 44

const props = withDefaults(
  defineProps<{
    /** Narrow / mobile: stage on top, sidebar as bottom drawer. */
    mobile?: boolean
    /** Desktop sidebar default width in px (also used for double-click reset). */
    sidebarWidth?: number
    /** Initial drawer height hint on mobile (clamped to shell + stage budget). */
    drawerHeight?: number
    /** localStorage key for scene-isolated width persistence. */
    storageKey?: string
    /**
     * When false, parent hosts ConfirmFlowOverlay (GateApproval).
     * Default true for clarify/review shells.
     */
    hostConfirmFlow?: boolean
    /**
     * Run Detail: give the stage (「流水线产物」) and sidebar (「Agent交互」)
     * the shared floating-card look (shell radius + border + shadow + gap).
     * Inbox/GateApproval keep the flush layout.
     */
    cardPanes?: boolean
  }>(),
  {
    mobile: false,
    sidebarWidth: 400,
    drawerHeight: 280,
    storageKey: '',
    hostConfirmFlow: true,
    cardPanes: false,
  },
)

const { t } = useI18n()

const confirmFlow = props.hostConfirmFlow ? provideConfirmFlowCeremony() : null
const confirmFlowPhase = confirmFlow?.phase
const confirmFlowReduce = confirmFlow?.reduceMotion
const confirmFlowToken = confirmFlow?.playToken

defineExpose({
  playConfirmCeremony: () => confirmFlow?.play() ?? Promise.resolve(),
  confirmFlowCeremony: confirmFlow,
})

const shellRef = ref<HTMLElement | null>(null)
const sashDragging = ref(false)
const drawerDragging = ref(false)

const height = ref(props.drawerHeight)
let startY = 0
let startH = 0

let sashStartX = 0
let sashStartW = 0
let sashDidDrag = false

function effectiveMax(): number {
  const shellW = shellRef.value?.getBoundingClientRect().width
  const available =
    shellW && shellW > 0
      ? shellW
      : typeof window !== 'undefined'
        ? window.innerWidth
        : SIDEBAR_MAX + STAGE_MIN + SASH_WIDTH
  const room = Math.floor(available - STAGE_MIN - SASH_WIDTH)
  // When the shell is too narrow to fit SIDEBAR_MIN + STAGE_MIN + sash,
  // prefer protecting the stage: allow the sidebar below SIDEBAR_MIN.
  if (room < SIDEBAR_MIN) return Math.max(0, room)
  return Math.min(SIDEBAR_MAX, room)
}

function clampSidebar(px: number): number {
  const max = effectiveMax()
  const min = Math.min(SIDEBAR_MIN, max)
  return Math.max(min, Math.min(max, Math.round(px)))
}

/** Shell container height — prefer parent box over window (iframe / plugin embed). */
function effectiveShellHeight(): number {
  const shellH = shellRef.value?.getBoundingClientRect().height
  if (shellH && shellH > 0) return shellH
  return typeof window !== 'undefined' ? window.innerHeight : 600
}

function effectiveDrawerMax(shellH = effectiveShellHeight()): number {
  return Math.max(0, Math.round(shellH))
}

/** Floor is the visible handle hit target; never exceed shell on short viewports. */
function effectiveDrawerMin(shellH = effectiveShellHeight()): number {
  const max = effectiveDrawerMax(shellH)
  return Math.min(HANDLE_HIT_MIN, max)
}

/** Clamp drawer height to [handleMin, shellH] — full top / full bottom allowed. */
function clampDrawerHeight(px: number, shellH = effectiveShellHeight()): number {
  const min = effectiveDrawerMin(shellH)
  const max = effectiveDrawerMax(shellH)
  return Math.max(min, Math.min(max, Math.round(px)))
}

/** Adaptive default: ~38% of shell (and props hint), clamped to the new range. */
function defaultDrawerHeight(shellH = effectiveShellHeight()): number {
  const preferred = Math.min(Math.round(shellH * 0.38), props.drawerHeight)
  return clampDrawerHeight(preferred, shellH)
}

/** Last shell height used for mobile drawer clamp — preserves full-top / full-bottom on resize. */
let lastDrawerShellH = 0

function initDrawerHeight() {
  if (!props.mobile) return
  height.value = defaultDrawerHeight()
  lastDrawerShellH = effectiveShellHeight()
}

/** Read stored width; returns null for missing/illegal/out-of-[240,480] values. */
function readStored(): number | null {
  if (!props.storageKey) return null
  try {
    const raw = localStorage.getItem(props.storageKey)
    if (raw == null || raw === '') return null
    const n = Number(raw)
    if (!Number.isFinite(n)) return null
    const rounded = Math.round(n)
    // Out-of-range or illegal → ignore and fall back to props default.
    if (rounded < SIDEBAR_MIN || rounded > SIDEBAR_MAX) return null
    return rounded
  } catch {
    return null
  }
}

function writeStored(px: number) {
  if (!props.storageKey) return
  try {
    localStorage.setItem(props.storageKey, String(px))
  } catch {
    /* ignore quota / private mode */
  }
}

function initWidth() {
  width.value = clampSidebar(readStored() ?? props.sidebarWidth)
}

const width = ref(clampSidebar(readStored() ?? props.sidebarWidth))

/**
 * Reclamp when the shell box changes — including parent-driven shrink
 * (Run Detail outer sash) which does not fire window.resize.
 */
function onShellSizeChange() {
  if (sashDragging.value) return
  if (props.mobile) {
    if (!drawerDragging.value) {
      const shellH = effectiveShellHeight()
      const min = effectiveDrawerMin(shellH)
      const max = effectiveDrawerMax(shellH)
      if (lastDrawerShellH > 0) {
        const prevMin = effectiveDrawerMin(lastDrawerShellH)
        const prevMax = effectiveDrawerMax(lastDrawerShellH)
        // Keep extreme semantics across shell resize (rotate / parent grow).
        if (height.value >= prevMax - 1) {
          height.value = max
        } else if (height.value <= prevMin + 1) {
          height.value = min
        } else {
          height.value = clampDrawerHeight(height.value, shellH)
        }
      } else {
        height.value = clampDrawerHeight(height.value, shellH)
      }
      lastDrawerShellH = shellH
    }
    return
  }
  width.value = clampSidebar(width.value)
}

function setDrawerDraggingUi(on: boolean) {
  if (typeof document === 'undefined') return
  document.body.classList.toggle('review-shell-drawer-dragging', on)
}

function onPointerDown(e: PointerEvent) {
  if (!props.mobile) return
  drawerDragging.value = true
  startY = e.clientY
  startH = height.value
  setDrawerDraggingUi(true)
  ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
  e.preventDefault()
}

function onPointerMove(e: PointerEvent) {
  if (!drawerDragging.value) return
  const dy = startY - e.clientY
  height.value = clampDrawerHeight(startH + dy)
  e.preventDefault()
}

function onPointerUp() {
  if (!drawerDragging.value) return
  drawerDragging.value = false
  setDrawerDraggingUi(false)
  lastDrawerShellH = effectiveShellHeight()
}

function setSashDraggingUi(on: boolean) {
  if (typeof document === 'undefined') return
  document.body.classList.toggle('review-shell-sash-dragging', on)
}

function onSashPointerDown(e: PointerEvent) {
  if (props.mobile) return
  sashDragging.value = true
  sashDidDrag = false
  sashStartX = e.clientX
  sashStartW = width.value
  setSashDraggingUi(true)
  ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
  e.preventDefault()
}

function onSashPointerMove(e: PointerEvent) {
  if (!sashDragging.value) return
  // Dragging sash left grows the sidebar (sidebar is on the right).
  const dx = sashStartX - e.clientX
  if (Math.abs(dx) > DRAG_THRESHOLD_PX) sashDidDrag = true
  width.value = clampSidebar(sashStartW + dx)
  e.preventDefault()
}

function onSashPointerUp() {
  if (!sashDragging.value) return
  sashDragging.value = false
  setSashDraggingUi(false)
  if (sashDidDrag) writeStored(width.value)
}

function onSashDblClick() {
  if (props.mobile || sashDidDrag) return
  width.value = clampSidebar(props.sidebarWidth)
  writeStored(width.value)
}

watch(
  () => [props.storageKey, props.sidebarWidth, props.mobile] as const,
  () => {
    if (props.mobile) {
      if (!drawerDragging.value) initDrawerHeight()
      return
    }
    if (sashDragging.value) return
    initWidth()
  },
)

let shellObserver: ResizeObserver | undefined

onMounted(() => {
  if (props.mobile) initDrawerHeight()
  else initWidth()
  window.addEventListener('resize', onShellSizeChange)
  if (typeof ResizeObserver !== 'undefined' && shellRef.value) {
    shellObserver = new ResizeObserver(() => onShellSizeChange())
    shellObserver.observe(shellRef.value)
  }
})

onBeforeUnmount(() => {
  drawerDragging.value = false
  sashDragging.value = false
  setSashDraggingUi(false)
  setDrawerDraggingUi(false)
  shellObserver?.disconnect()
  shellObserver = undefined
  window.removeEventListener('resize', onShellSizeChange)
})
</script>

<template>
  <div
    ref="shellRef"
    class="relative flex h-full min-h-0 overflow-hidden"
    :class="[
      mobile ? 'flex-col' : 'flex-row',
      cardPanes && !mobile ? 'gap-2 p-2' : '',
      sashDragging || drawerDragging ? 'select-none' : '',
    ]"
    data-testid="review-shell"
  >
    <section
      class="flex min-h-0 flex-1 flex-col overflow-hidden"
      :class="[
        mobile ? 'min-w-0 border-b border-line' : 'review-shell-stage',
        cardPanes && !mobile ? 'rounded-lg border border-line bg-base shadow-card' : '',
      ]"
      data-testid="review-shell-stage"
    >
      <slot name="stage" />
    </section>

    <div
      v-if="!mobile"
      class="review-shell-sash relative shrink-0 cursor-col-resize bg-line transition-colors hover:bg-accent"
      :class="sashDragging ? 'bg-accent' : ''"
      role="separator"
      aria-orientation="vertical"
      :aria-valuemin="Math.min(SIDEBAR_MIN, effectiveMax())"
      :aria-valuemax="effectiveMax()"
      :aria-valuenow="width"
      :aria-label="t('pages.reviewShell.resizeSash')"
      :title="t('pages.reviewShell.resizeSash')"
      data-testid="review-shell-sash"
      @pointerdown="onSashPointerDown"
      @pointermove="onSashPointerMove"
      @pointerup="onSashPointerUp"
      @pointercancel="onSashPointerUp"
      @dblclick="onSashDblClick"
    />

    <aside
      class="flex min-h-0 flex-col bg-surface"
      :class="[
        mobile ? 'w-full shrink-0' : 'shrink-0',
        cardPanes && !mobile ? 'rounded-lg border border-line shadow-card' : '',
      ]"
      :style="
        mobile
          ? {
              height: `${height}px`,
              maxHeight: `${effectiveDrawerMax()}px`,
              minHeight: `${effectiveDrawerMin()}px`,
            }
          : { width: `${width}px` }
      "
      data-testid="review-shell-sidebar"
    >
      <div
        v-if="mobile"
        class="review-shell-drawer-handle relative flex shrink-0 cursor-ns-resize items-center justify-center gap-2 border-b border-line text-[11px] text-txt3"
        role="separator"
        aria-orientation="horizontal"
        :aria-valuemin="effectiveDrawerMin()"
        :aria-valuemax="effectiveDrawerMax()"
        :aria-valuenow="height"
        :aria-label="t('pages.reviewShell.drawerHandleAria')"
        :title="t('pages.reviewShell.drawerHandleAria')"
        data-testid="review-shell-drawer-handle"
        @pointerdown="onPointerDown"
        @pointermove="onPointerMove"
        @pointerup="onPointerUp"
        @pointercancel="onPointerUp"
      >
        <span class="review-shell-drawer-handle-pill inline-block h-1 w-11 rounded-full bg-line-strong" />
        <span class="pointer-events-none select-none">{{ t('pages.reviewShell.drawerHandle') }}</span>
      </div>
      <div class="flex min-h-0 flex-1 flex-col overflow-hidden">
        <slot name="sidebar" />
      </div>
    </aside>

    <ConfirmFlowOverlay
      v-if="hostConfirmFlow && confirmFlowPhase"
      :phase="confirmFlowPhase"
      :reduce-motion="confirmFlowReduce"
      :play-token="confirmFlowToken"
    />
  </div>
</template>

<style scoped>
.review-shell-stage {
  /* Second line of defense: keep stage usable when sidebar hits its floor. */
  min-width: 160px;
}
.review-shell-sash {
  width: 4px;
  touch-action: none;
  z-index: 2;
}
/* Expanded hit target (~12px) without widening the visual bar. */
.review-shell-sash::before {
  content: '';
  position: absolute;
  inset: 0 -4px;
}
.review-shell-drawer-handle {
  min-height: 44px;
  touch-action: none;
  z-index: 2;
}
/* Expanded vertical hit target without a tall visual bar. */
.review-shell-drawer-handle::before {
  content: '';
  position: absolute;
  left: 0;
  right: 0;
  top: -10px;
  bottom: -10px;
}
.review-shell-drawer-handle-pill {
  pointer-events: none;
}
</style>

<style>
/* Unscoped: applied on document.body while sash is dragged. */
body.review-shell-sash-dragging {
  cursor: col-resize;
  user-select: none;
}
body.review-shell-drawer-dragging {
  cursor: ns-resize;
  user-select: none;
  touch-action: none;
}
</style>
