<script setup lang="ts">
import { ref, watch, onMounted, onBeforeUnmount, computed, inject, type ComputedRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { injectDemoScrollbarStyles } from '@/lib/shared/demoScrollbar'
import {
  SANDBOX_ATTR,
  INLINE_FALLBACK_HEIGHT,
  RESIZE_HEIGHT_EPSILON,
  RESIZE_TIMEOUT_MS,
  HTML_PREVIEW_DEFAULT_TOOLBAR_PX,
  contentFitPreviewCapPx,
  createInstanceId,
  injectPreviewScripts,
  isValidResizeMessage,
  parseResizeMessage,
  isValidInspectPickMessage,
  parseInspectPickMessage,
  isValidInspectCanceledMessage,
  buildInspectCommand,
  type InspectElementStyle,
} from '@/lib/shared/htmlPreviewSandbox'
import { isEmbedDrawer, postEmbedEnlarge } from '@/lib/inbox/embedChat'
import Icon from './Icon.vue'
import AppModal from './AppModal.vue'
import CommentPinInspectCard from '../run/CommentPinInspectCard.vue'

const emit = defineEmits<{
  (
    e: 'pick',
    payload: {
      selector: string
      tagName: string
      imageDataUrl: string
      bounds?: { left: number; top: number; width: number; height: number }
      currentText?: string
      style?: InspectElementStyle
    },
  ): void
  (e: 'pin-select', pinId: string): void
  (e: 'annotate-save', comment: string): void
  (e: 'annotate-send-chat', comment: string): void
  (e: 'annotate-close'): void
}>()

export type HtmlPreviewCommentPinBadge = {
  id: string
  seq: number
  bounds?: { left: number; top: number; width: number; height: number }
  active?: boolean
}

export type HtmlPreviewAnnotateDraft = {
  selector: string
  imageDataUrl?: string
  screenshotMissing?: boolean
  initialComment?: string
  bounds?: { left: number; top: number; width: number; height: number } | null
  style?: InspectElementStyle | null
}

const props = withDefaults(
  defineProps<{
    html: string
    enlargeable?: boolean
    mode?: 'default' | 'inline' | 'demo'
    modalTitle?: string
    /**
     * When true with mode=default, iframe height follows document scrollHeight
     * (toolbar kept). Default false preserves h-full / flex-1 fill behavior.
     */
    fitContent?: boolean
    /**
     * Optional vh cap for measured content height (fillPreview content-fit).
     * When measured height exceeds the cap (minus chrome), iframe height is
     * clamped and scrolling=auto so content stays readable inside the shell.
     */
    maxContentHeightVh?: number
    /**
     * Extra chrome above this preview inside the content-fit shell
     * (e.g. GateApproval reviewing-upstream strip). Subtracted from the vh cap
     * so toolbar + strip + iframe stay within the shell without nested scroll.
     */
    contentHeightOffsetPx?: number
    /**
     * When true, inject inspect script and show a pick-element control.
     * Opaque-origin iframe posts selector + element screenshot to parent.
     */
    inspectable?: boolean
    /**
     * Fill the parent box instead of sizing from document scrollHeight.
     * Used by Run-detail mobile visual gates and desktop Inbox/Run-detail
     * visual HTML (shell = flex remainder ∩ ≈60vh): iframe scrolls internally;
     * short docs leave blank space inside the frame (top-aligned).
     */
    fillParent?: boolean
    /** CommentPin badges overlaid on the iframe viewport (opaque-origin bounds). */
    commentPins?: HtmlPreviewCommentPinBadge[]
    /** Open OD annotate card over the preview (parent owns pin persistence). */
    annotateDraft?: HtmlPreviewAnnotateDraft | null
  }>(),
  {
    enlargeable: true,
    mode: 'default',
    fitContent: false,
    contentHeightOffsetPx: 0,
    inspectable: false,
    fillParent: false,
    commentPins: () => [],
    annotateDraft: null,
  },
)

const { t } = useI18n()
const gateShareOpen = inject<(() => void) | undefined>('gateShareOpen', undefined)
const gateShareEnabled = inject<ComputedRef<boolean> | undefined>('gateShareEnabled', undefined)
const showGateShare = computed(() => typeof gateShareOpen === 'function' && !!gateShareEnabled?.value)
const device = ref<'desktop' | 'mobile'>('desktop')
const big = ref(false)
const modalHtml = ref('')
/** Enlarge is shown by the preview page, so the drawer iframe does not grow. */
const embedEnlarged = ref(false)
const iframeRef = ref<HTMLIFrameElement | null>(null)
const toolbarRef = ref<HTMLElement | null>(null)
const pinHostRef = ref<HTMLElement | null>(null)
const contentHeight = ref(INLINE_FALLBACK_HEIGHT)
const contentDegraded = ref(false)
/** True when measured height was clamped to maxContentHeightVh. */
const contentCapped = ref(false)
/** Steady-state: suppress micro-resize jitter and height transition. */
const heightStable = ref(false)
/** First inline/content-fit convergence from fallback height. */
const isFirstHeightConvergence = ref(true)
const inspecting = ref(false)
const inspectToggleLabel = computed(() =>
  t(inspecting.value ? 'pages.appPreview.novnc.cancelInspect' : 'pages.appPreview.novnc.inspect'),
)
const instanceId = createInstanceId()
let resizeTimeout: ReturnType<typeof setTimeout> | undefined
let pendingResizeHeight: number | null = null
let resizeRafId: number | null = null

function pinBadgeStyle(pin: HtmlPreviewCommentPinBadge, index: number): Record<string, string> {
  const b = pin.bounds
  const offset = index * 18
  if (!b || !(b.width > 0 || b.height > 0)) {
    return { top: `${8 + offset}px`, right: '8px' }
  }
  // Anchor near top-right of the picked element rect (iframe viewport coords).
  const top = Math.max(0, b.top - 8)
  const left = Math.max(0, b.left + Math.max(b.width - 12, 0) + offset)
  return {
    top: `${top}px`,
    left: `${left}px`,
  }
}

function computeHtmlHash(html: string): string {
  let hash = 0
  for (let i = 0; i < html.length; i++) {
    hash = (Math.imul(31, hash) + html.charCodeAt(i)) | 0
  }
  return `${html.length}-${hash}`
}

const lastHtmlHash = ref(computeHtmlHash(props.html || ''))

/** Fixed sandbox policy — see htmlPreviewSandbox.ts (no allow-same-origin). */
const sandboxAttr = SANDBOX_ATTR

const needsContentHeight = computed(
  () =>
    !props.fillParent &&
    (props.mode === 'inline' || (props.mode === 'default' && props.fitContent)),
)

const needsMessageListener = computed(
  () => needsContentHeight.value || props.inspectable,
)

const iframeScrolling = computed(() => {
  if (props.fillParent) return 'auto'
  if (!needsContentHeight.value) return undefined
  return contentDegraded.value || contentCapped.value ? 'auto' : 'no'
})

/** Effective iframe pixel cap after deducting toolbar / outer chrome. */
function resolveContentHeightCapPx(): number | null {
  const vh = props.maxContentHeightVh
  if (vh == null || vh <= 0) return null
  let cap = contentFitPreviewCapPx(window.innerHeight, vh)
  if (props.mode === 'default') {
    const toolbarH = toolbarRef.value?.offsetHeight || HTML_PREVIEW_DEFAULT_TOOLBAR_PX
    cap -= toolbarH
  }
  const offset = props.contentHeightOffsetPx ?? 0
  if (offset > 0) cap -= offset
  return Math.max(INLINE_FALLBACK_HEIGHT, cap)
}

function applyMeasuredHeight(measured: number) {
  const current = contentHeight.value
  const delta = Math.abs(measured - current)

  if (heightStable.value && delta < RESIZE_HEIGHT_EPSILON) {
    return
  }

  if (
    isFirstHeightConvergence.value &&
    current === INLINE_FALLBACK_HEIGHT &&
    measured < current
  ) {
    return
  }

  const cap = resolveContentHeightCapPx()
  let next = measured
  if (cap != null && measured > cap) {
    next = cap
    contentCapped.value = true
    contentDegraded.value = false
  } else {
    contentCapped.value = false
    contentDegraded.value = false
  }

  if (heightStable.value && Math.abs(next - current) < RESIZE_HEIGHT_EPSILON) {
    return
  }

  contentHeight.value = next

  if (
    isFirstHeightConvergence.value &&
    next > INLINE_FALLBACK_HEIGHT + RESIZE_HEIGHT_EPSILON
  ) {
    window.setTimeout(() => {
      heightStable.value = true
      isFirstHeightConvergence.value = false
    }, 180)
  }
}

const deviceHint = computed(() =>
  device.value === 'mobile' ? t('common.htmlPreview.mobileWidth') : t('common.htmlPreview.adaptiveWidth'),
)

const demoModalTitle = computed(() => props.modalTitle || t('common.htmlPreview.title'))

/** Enlarge modal title:「窗口放大查看 · {file}」or enlargeTitle fallback. */
const enlargeModalTitle = computed(() => {
  const base = t('common.htmlPreview.enlargeTitle')
  const name = (props.modalTitle || '').trim()
  return name ? `${base} · ${name}` : base
})

const showInlineChrome = computed(() => props.enlargeable || props.inspectable)

const demoSrcdoc = computed(() => injectDemoScrollbarStyles(props.html))

const previewSrcdoc = computed(() =>
  injectPreviewScripts(props.html, instanceId, {
    resize: needsContentHeight.value,
    inspect: props.inspectable,
  }),
)

/** Mount key — remount iframe only on structural changes, not html content updates. */
const iframeMountKey = computed(
  () =>
    `${props.mode}-${props.fitContent}-${props.fillParent}-${props.inspectable}-${props.maxContentHeightVh ?? ''}-${instanceId}`,
)

function openEnlarge() {
  const html = injectDemoScrollbarStyles(props.html)
  if (isEmbedDrawer()) {
    embedEnlarged.value = true
    postEmbedEnlarge(true, { title: demoModalTitle.value, html })
    return
  }
  modalHtml.value = html
  big.value = true
}

function closeEnlarge() {
  if (embedEnlarged.value) {
    embedEnlarged.value = false
    postEmbedEnlarge(false)
  }
  big.value = false
  modalHtml.value = ''
}

function postInspectCommand(enabled: boolean) {
  const win = iframeRef.value?.contentWindow
  if (!win || !props.inspectable) return
  win.postMessage(buildInspectCommand(instanceId, enabled), '*')
}

/**
 * Single exit for inspect mode (toolbar off / Esc / pick). Keeps last pick emit
 * result with the parent — Esc must only clear the button/mode, not staged UI.
 */
function clearInspect(_reason: string, opts?: { syncIframe?: boolean }) {
  const syncIframe = opts?.syncIframe !== false
  if (!props.inspectable) return
  if (!inspecting.value) {
    if (syncIframe) postInspectCommand(false)
    return
  }
  inspecting.value = false
  if (syncIframe) postInspectCommand(false)
}

function setInspecting(on: boolean) {
  if (!props.inspectable) return
  if (on) {
    inspecting.value = true
    postInspectCommand(true)
    return
  }
  clearInspect('setInspecting')
}

function toggleInspect() {
  if (inspecting.value) clearInspect('toolbar-toggle')
  else setInspecting(true)
}

defineExpose({ openEnlarge, closeEnlarge, setInspecting })

function clearResizeTimeout() {
  if (resizeTimeout !== undefined) {
    clearTimeout(resizeTimeout)
    resizeTimeout = undefined
  }
}

function applyContentFallback() {
  contentDegraded.value = true
  contentCapped.value = false
  contentHeight.value = INLINE_FALLBACK_HEIGHT
}

function startResizeTimeout() {
  clearResizeTimeout()
  resizeTimeout = setTimeout(applyContentFallback, RESIZE_TIMEOUT_MS)
}

function scheduleApplyResize(height: number) {
  pendingResizeHeight = height
  if (resizeRafId != null) return
  resizeRafId = requestAnimationFrame(() => {
    resizeRafId = null
    const h = pendingResizeHeight
    pendingResizeHeight = null
    if (h != null) applyMeasuredHeight(h)
  })
}

function handlePreviewMessage(event: MessageEvent) {
  if (!iframeRef.value) return
  if (event.source !== iframeRef.value.contentWindow) return
  if (event.data?.id !== instanceId) return

  if (needsContentHeight.value && isValidResizeMessage(event.data)) {
    const parsed = parseResizeMessage(event.data)
    if (!parsed) return
    clearResizeTimeout()
    scheduleApplyResize(parsed.height)
    return
  }

  if (props.inspectable && isValidInspectCanceledMessage(event.data)) {
    // iframe Esc — sandbox already disabled inspect; sync parent button only.
    clearInspect('iframe-esc', { syncIframe: false })
    return
  }

  if (props.inspectable && isValidInspectPickMessage(event.data)) {
    const parsed = parseInspectPickMessage(event.data)
    if (!parsed) return
    // Keep inspect on after pick so the user can pin another element (OD comment mode).
    emit('pick', {
      selector: parsed.selector,
      tagName: parsed.tagName,
      imageDataUrl: parsed.imageDataUrl,
      bounds: parsed.bounds,
      currentText: parsed.currentText,
      style: parsed.style,
    })
  }
}

function onPreviewLoad() {
  if (needsContentHeight.value) startResizeTimeout()
  if (props.inspectable && inspecting.value) postInspectCommand(true)
}

function resetContentHeightState() {
  contentHeight.value = INLINE_FALLBACK_HEIGHT
  contentDegraded.value = false
  contentCapped.value = false
  heightStable.value = false
  isFirstHeightConvergence.value = true
  clearResizeTimeout()
  if (resizeRafId != null) {
    cancelAnimationFrame(resizeRafId)
    resizeRafId = null
  }
  pendingResizeHeight = null
}

onMounted(() => {
  if (needsMessageListener.value) {
    window.addEventListener('message', handlePreviewMessage)
  }
})

onBeforeUnmount(() => {
  window.removeEventListener('message', handlePreviewMessage)
  clearResizeTimeout()
  if (resizeRafId != null) cancelAnimationFrame(resizeRafId)
  if (big.value || embedEnlarged.value) postEmbedEnlarge(false)
})

watch(
  () =>
    [
      props.html,
      props.mode,
      props.fitContent,
      props.fillParent,
      props.maxContentHeightVh,
      props.contentHeightOffsetPx,
      props.inspectable,
    ] as const,
  (next, prev) => {
    const newHash = computeHtmlHash(next[0] || '')
    const htmlUnchanged = newHash === lastHtmlHash.value
    const structuralChanged =
      !prev ||
      next[1] !== prev[1] ||
      next[2] !== prev[2] ||
      next[3] !== prev[3] ||
      next[4] !== prev[4] ||
      next[5] !== prev[5] ||
      next[6] !== prev[6]

    if (structuralChanged && needsContentHeight.value) {
      resetContentHeightState()
      lastHtmlHash.value = newHash
    } else if (!htmlUnchanged) {
      lastHtmlHash.value = newHash
      // Silent srcdoc hot-update: keep iframe mounted, let reactive srcdoc apply.
    }

    if (!props.inspectable) inspecting.value = false
  },
)

watch(device, () => {
  if (props.mode === 'default' && props.fitContent) resetContentHeightState()
})

watch(needsMessageListener, (enabled, wasEnabled) => {
  if (enabled && !wasEnabled) {
    window.addEventListener('message', handlePreviewMessage)
    if (needsContentHeight.value) resetContentHeightState()
  } else if (!enabled && wasEnabled) {
    window.removeEventListener('message', handlePreviewMessage)
    clearResizeTimeout()
  }
})
</script>

<template>
  <!-- Inline/mobile: content-height shell, or fillParent (h-full + iframe scroll). -->
  <div
    v-if="mode === 'inline'"
    class="w-full overflow-x-hidden"
    :class="fillParent ? 'flex h-full min-h-0 flex-col' : ''"
    :data-fill-parent="fillParent ? '1' : '0'"
    data-testid="html-preview-inline"
  >
    <div
      v-if="showInlineChrome"
      class="flex shrink-0 items-center gap-2 border-b border-line px-3 py-2"
      data-testid="html-preview-inspect-bar"
    >
      <button
        v-if="inspectable"
        type="button"
        class="flex items-center gap-1 rounded-md border px-2 py-1 text-[11px] transition-colors"
        :class="
          inspecting
            ? 'border-accent bg-accent-dim/50 text-accent'
            : 'border-line text-txt2 hover:text-txt'
        "
        :aria-pressed="inspecting ? 'true' : 'false'"
        :title="inspectToggleLabel"
        data-testid="html-preview-inspect-toggle"
        @click="toggleInspect"
      >
        <Icon name="crosshair" :size="13" />
        {{ inspectToggleLabel }}
      </button>
      <button
        v-if="showGateShare"
        type="button"
        class="flex items-center gap-1 rounded-md border border-line px-2 py-1 text-[11px] text-txt2 transition-colors hover:text-txt"
        data-testid="html-preview-share-link"
        :aria-label="t('pages.gatesInbox.share.copyLinkAria')"
        @click="gateShareOpen?.()"
      >
        <Icon name="copy" :size="13" />{{ t('pages.gatesInbox.share.copyLink') }}
      </button>
      <button
        v-if="enlargeable"
        type="button"
        class="ml-auto flex items-center gap-1 rounded-md border border-line px-2 py-1 text-[11px] text-txt2 transition-colors hover:text-txt"
        :title="t('common.htmlPreview.enlargeTitle')"
        data-testid="html-preview-enlarge"
        @click="openEnlarge"
      >
        <Icon name="expand" :size="13" />{{ t('common.htmlPreview.enlargeTitle') }}
      </button>
    </div>
    <div :class="fillParent ? 'min-h-0 flex-1' : ''">
      <div
        ref="pinHostRef"
        class="relative overflow-hidden"
        :class="fillParent ? 'h-full' : ''"
        data-testid="html-preview-pin-host"
      >
        <iframe
          :key="iframeMountKey"
          ref="iframeRef"
          :srcdoc="previewSrcdoc"
          :sandbox="sandboxAttr"
          referrerpolicy="no-referrer"
          :scrolling="iframeScrolling"
          class="w-full border-0 bg-white"
          :class="[
            fillParent ? 'h-full' : '',
            { 'html-preview-height-transition': needsContentHeight && !heightStable },
          ]"
          :style="fillParent ? undefined : { height: contentHeight + 'px' }"
          :title="t('common.htmlPreview.title')"
          @load="onPreviewLoad"
        />
        <div
          v-if="commentPins.length"
          class="pointer-events-none absolute inset-0 z-10 overflow-hidden"
          data-testid="html-preview-pin-layer"
        >
          <button
            v-for="(pin, pinIdx) in commentPins"
            :key="pin.id"
            type="button"
            class="pointer-events-auto absolute inline-flex h-5 min-w-5 items-center justify-center bg-accent px-1 font-mono text-[11px] font-semibold text-white shadow-[0_0_0_2px_rgb(10,10,11)]"
            :class="pin.active ? 'ring-2 ring-accent/60 ring-offset-1 ring-offset-base' : ''"
            :style="pinBadgeStyle(pin, pinIdx)"
            :title="'#' + pin.seq"
            :data-testid="'html-preview-pin-' + pin.seq"
            @click.stop="emit('pin-select', pin.id)"
          >
            {{ pin.seq }}
          </button>
        </div>
        <CommentPinInspectCard
          :open="!!annotateDraft"
          :selector="annotateDraft?.selector || ''"
          :image-data-url="annotateDraft?.imageDataUrl"
          :screenshot-missing="!!annotateDraft?.screenshotMissing"
          :initial-comment="annotateDraft?.initialComment"
          :anchor="annotateDraft?.bounds || null"
          :style-info="annotateDraft?.style || null"
          :container-el="pinHostRef"
          @close="emit('annotate-close')"
          @save="emit('annotate-save', $event)"
          @send-chat="emit('annotate-send-chat', $event)"
        />
      </div>
    </div>
  </div>

  <!-- Demo mode: compact card preview + simplified enlarge modal (no device toggle) -->
  <template v-else-if="mode === 'demo'">
    <iframe
      :key="iframeMountKey"
      :srcdoc="demoSrcdoc"
      :sandbox="sandboxAttr"
      referrerpolicy="no-referrer"
      class="block h-[140px] w-full border-0 bg-white"
      :title="demoModalTitle"
    />
    <AppModal
      v-if="enlargeable"
      :open="big"
      :title="demoModalTitle"
      :width="900"
      @close="closeEnlarge"
    >
      <iframe
        v-if="modalHtml"
        :key="iframeMountKey + '-modal'"
        :srcdoc="modalHtml"
        :sandbox="sandboxAttr"
        referrerpolicy="no-referrer"
        class="block h-[70vh] w-full border-0 bg-white"
        :title="demoModalTitle"
      />
    </AppModal>
  </template>

  <!-- Default desktop preview with device toggle (fillParent: h-full + iframe scroll). -->
  <div
    v-else
    class="flex flex-col"
    :class="fitContent && !fillParent ? '' : 'h-full min-h-0'"
    :data-fill-parent="fillParent ? '1' : '0'"
  >
    <div
      ref="toolbarRef"
      class="flex shrink-0 items-center gap-2 border-b border-line px-3 py-2"
      data-testid="html-preview-toolbar"
    >
      <div class="flex overflow-hidden rounded-md border border-line text-[11px]">
        <button
          class="flex items-center gap-1 px-2 py-1 transition-colors"
          :class="device === 'desktop' ? 'bg-accent-dim/50 text-accent' : 'text-txt3 hover:text-txt'"
          @click="device = 'desktop'"
        >
          <Icon name="monitor" :size="13" />{{ t('common.htmlPreview.desktop') }}
        </button>
        <button
          class="flex items-center gap-1 border-l border-line px-2 py-1 transition-colors"
          :class="device === 'mobile' ? 'bg-accent-dim/50 text-accent' : 'text-txt3 hover:text-txt'"
          @click="device = 'mobile'"
        >
          <Icon name="mobile" :size="13" />{{ t('common.htmlPreview.mobile') }}
        </button>
      </div>
      <span class="text-[10px] text-txt3">{{ deviceHint }}</span>
      <button
        v-if="inspectable"
        type="button"
        class="flex items-center gap-1 rounded-md border px-2 py-1 text-[11px] transition-colors"
        :class="
          inspecting
            ? 'border-accent bg-accent-dim/50 text-accent'
            : 'border-line text-txt2 hover:text-txt'
        "
        :aria-pressed="inspecting ? 'true' : 'false'"
        :title="inspectToggleLabel"
        data-testid="html-preview-inspect-toggle"
        @click="toggleInspect"
      >
        <Icon name="crosshair" :size="13" />
        {{ inspectToggleLabel }}
      </button>
      <button
        v-if="showGateShare"
        type="button"
        class="flex items-center gap-1 rounded-md border border-line px-2 py-1 text-[11px] text-txt2 transition-colors hover:text-txt"
        data-testid="html-preview-share-link"
        :aria-label="t('pages.gatesInbox.share.copyLinkAria')"
        @click="gateShareOpen?.()"
      >
        <Icon name="copy" :size="13" />{{ t('pages.gatesInbox.share.copyLink') }}
      </button>
      <button
        v-if="enlargeable"
        type="button"
        class="ml-auto flex items-center gap-1 rounded-md border border-line px-2 py-1 text-[11px] text-txt2 transition-colors hover:text-txt"
        :title="t('common.htmlPreview.enlargeTitle')"
        data-testid="html-preview-enlarge"
        @click="openEnlarge"
      >
        <Icon name="expand" :size="13" />{{ t('common.htmlPreview.enlargeTitle') }}
      </button>
    </div>
    <div
      class="bg-elevated"
      :class="[
        fitContent && !fillParent ? 'shrink-0' : 'min-h-0 flex-1 overflow-hidden',
        device === 'mobile' ? 'flex justify-center p-4' : '',
      ]"
    >
      <div
        ref="pinHostRef"
        class="relative overflow-hidden"
        :class="[
          device === 'desktop' ? 'h-full w-full' : '',
          device === 'mobile'
            ? fitContent && !fillParent
              ? ''
              : 'h-full'
            : '',
        ]"
        data-testid="html-preview-pin-host"
      >
        <iframe
          v-if="device === 'desktop'"
          :key="iframeMountKey + '-desktop'"
          ref="iframeRef"
          :srcdoc="inspectable || fitContent ? previewSrcdoc : html"
          :sandbox="sandboxAttr"
          referrerpolicy="no-referrer"
          :scrolling="fitContent || fillParent ? iframeScrolling : undefined"
          class="w-full border-0 bg-white"
          :class="fitContent && !fillParent ? '' : 'h-full'"
          :style="fitContent && !fillParent ? { height: contentHeight + 'px' } : undefined"
          :title="t('common.htmlPreview.title')"
          @load="onPreviewLoad"
        />
        <div
          v-else
          class="w-[390px] shrink-0 overflow-hidden rounded-lg border border-line bg-white shadow-lg"
          :class="fitContent && !fillParent ? '' : 'h-full'"
          :style="fitContent && !fillParent ? { height: contentHeight + 'px' } : undefined"
        >
          <iframe
            :key="iframeMountKey + '-mobile'"
            ref="iframeRef"
            :srcdoc="inspectable || fitContent ? previewSrcdoc : html"
            :sandbox="sandboxAttr"
            referrerpolicy="no-referrer"
            :scrolling="fitContent || fillParent ? iframeScrolling : undefined"
            class="h-full w-full border-0 bg-white"
            :title="t('common.htmlPreview.mobilePreview')"
            @load="onPreviewLoad"
          />
        </div>
        <div
          v-if="commentPins.length"
          class="pointer-events-none absolute inset-0 z-10 overflow-hidden"
          data-testid="html-preview-pin-layer"
        >
          <button
            v-for="(pin, pinIdx) in commentPins"
            :key="pin.id"
            type="button"
            class="pointer-events-auto absolute inline-flex h-5 min-w-5 items-center justify-center bg-accent px-1 font-mono text-[11px] font-semibold text-white shadow-[0_0_0_2px_rgb(10,10,11)]"
            :class="pin.active ? 'ring-2 ring-accent/60 ring-offset-1 ring-offset-base' : ''"
            :style="pinBadgeStyle(pin, pinIdx)"
            :title="'#' + pin.seq"
            :data-testid="'html-preview-pin-' + pin.seq"
            @click.stop="emit('pin-select', pin.id)"
          >
            {{ pin.seq }}
          </button>
        </div>
        <CommentPinInspectCard
          :open="!!annotateDraft"
          :selector="annotateDraft?.selector || ''"
          :image-data-url="annotateDraft?.imageDataUrl"
          :screenshot-missing="!!annotateDraft?.screenshotMissing"
          :initial-comment="annotateDraft?.initialComment"
          :anchor="annotateDraft?.bounds || null"
          :style-info="annotateDraft?.style || null"
          :container-el="pinHostRef"
          @close="emit('annotate-close')"
          @save="emit('annotate-save', $event)"
          @send-chat="emit('annotate-send-chat', $event)"
        />
      </div>
    </div>
  </div>

  <!-- Enlarge: default + inline (fillParent); nested is read-only, no inspect. -->
  <AppModal
    v-if="enlargeable && (mode === 'default' || mode === 'inline')"
    :open="big"
    :title="enlargeModalTitle"
    :width="1120"
    :close-on-esc="true"
    data-testid="html-preview-enlarge-modal"
    @close="closeEnlarge"
  >
    <div class="h-[80vh]" data-testid="html-preview-enlarge-body">
      <HtmlPreview :html="html" :enlargeable="false" :inspectable="false" />
    </div>
  </AppModal>
</template>

<style scoped>
.html-preview-height-transition {
  transition: height 180ms ease-out;
}
</style>
