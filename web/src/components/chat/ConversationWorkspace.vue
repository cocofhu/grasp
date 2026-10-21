<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, provide, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ArtifactUnreadMark from '@/components/chat/ArtifactUnreadMark.vue'
import Icon from '@/components/ui/Icon.vue'
import {
  artifactButtonAriaLabel,
  nextRunArtifactUnread,
  onRunArtifactEdit,
  runArtifactUnreadOnPanel,
} from '@/lib/chat/artifactUnread'
import { CONVERSATION_PANEL_SHOWN_EVENT, conversationPanelVisibleKey } from '@/lib/chat/conversationArtifacts'

const props = withDefaults(defineProps<{
  title?: string
  scopeKey: string
  storageKey?: string
  panelWidth?: number
  panelEnabled?: boolean
  showToolbar?: boolean
  /** Sandbox stage: icon-only resource and artifact toggles share this drawer. */
  splitModes?: boolean
  /** Keep the open flag across scope changes. The parent owns that session switch. */
  retainOpen?: boolean
}>(), { title: '', storageKey: 'grasp.conversation.panelWidth', panelWidth: 440, panelEnabled: true, showToolbar: true, splitModes: false, retainOpen: false })
const open = defineModel<boolean>('open', { default: false })
const mode = defineModel<'resources' | 'artifacts' | null>('mode', { default: null })
provide(conversationPanelVisibleKey, computed(() => open.value && props.panelEnabled))
const { t } = useI18n()
const root = ref<HTMLElement | null>(null)
const main = ref<HTMLElement | null>(null)
const panel = ref<HTMLElement | null>(null)
const toggle = ref<HTMLButtonElement | null>(null)
const resourceToggle = ref<HTMLButtonElement | null>(null)
const artifactToggle = ref<HTMLButtonElement | null>(null)
const pressedToggle = ref<'resources' | 'artifacts' | null>(null)
const closeButton = ref<HTMLButtonElement | null>(null)
const canvasWidth = ref(0)
const preferredWidth = ref(props.panelWidth)
const dragging = ref(false)
const compact = computed(() => canvasWidth.value > 0 && canvasWidth.value < 800)
const maxWidth = computed(() => Math.max(280, Math.min(880, canvasWidth.value - 364)))
const width = computed(() => Math.max(280, Math.min(maxWidth.value, preferredWidth.value)))
const panelTitle = computed(() => {
  if (props.splitModes) {
    return mode.value === 'resources'
      ? t('pages.conversationWorkspace.resources')
      : t('pages.conversationWorkspace.artifacts')
  }
  return props.title || t('pages.conversationWorkspace.artifacts')
})
const headingIcon = computed(() => {
  if (!props.splitModes) return 'artifact'
  return mode.value === 'resources' ? 'stage-resources' : 'doc'
})
const artifactPanelOpen = computed(() => props.splitModes ? open.value && mode.value === 'artifacts' : open.value)
const unreadCount = ref(0)
const artifactToggleLabel = computed(() => artifactButtonAriaLabel(artifactPanelOpen.value, unreadCount.value, {
  open: t('pages.conversationWorkspace.openArtifacts'),
  close: t('pages.conversationWorkspace.closeArtifacts'),
  unread: (count) => t('pages.conversationWorkspace.openArtifactsUnread', { n: count }),
  cap: t('pages.conversationWorkspace.openArtifactsUnreadCap'),
}))
const stopArtifactEdit = onRunArtifactEdit(() => {
  if (!props.showToolbar || !props.panelEnabled) return
  unreadCount.value = nextRunArtifactUnread(unreadCount.value, artifactPanelOpen.value, 'artifact')
})
let observer: ResizeObserver | undefined
let returnFocus: HTMLElement | null = null
let pointerStart = 0
let pointerWidth = 0

function readWidth() {
  try {
    const raw = localStorage.getItem(props.storageKey)
    const value = Number(raw)
    preferredWidth.value = raw && Number.isFinite(value) && value >= 280 && value <= 880 ? value : props.panelWidth
  } catch { preferredWidth.value = props.panelWidth }
}
function saveWidth() {
  try { localStorage.setItem(props.storageKey, String(width.value)) } catch { /* private storage */ }
}

// Keep the same passage visible when opening a pane changes line wrapping.
function capturePassage(): (() => void) | undefined {
  const scroll = main.value?.querySelector<HTMLElement>('[data-conversation-scroll]')
  if (!scroll) return
  const nearBottom = scroll.scrollHeight - scroll.scrollTop - scroll.clientHeight < 56
  const viewportTop = scroll.getBoundingClientRect().top
  const anchor = Array.from(scroll.querySelectorAll<HTMLElement>('[data-chat-anchor]')).find(el => el.getBoundingClientRect().bottom > viewportTop)
  const offset = anchor ? anchor.getBoundingClientRect().top - viewportTop : 0
  const top = scroll.scrollTop
  return () => {
    if (!scroll.isConnected) return
    if (nearBottom) scroll.scrollTop = scroll.scrollHeight
    else if (anchor?.isConnected) scroll.scrollTop += anchor.getBoundingClientRect().top - scroll.getBoundingClientRect().top - offset
    else scroll.scrollTop = top
  }
}
function restorePassage(restore?: () => void) { void nextTick(() => restore?.()) }
function close() {
  open.value = false
  if (props.splitModes) mode.value = null
}
function toggleMode(next: 'resources' | 'artifacts') {
  pressedToggle.value = next
  if (mode.value === next) close()
  else {
    mode.value = next
    open.value = true
  }
}
function onPanelKeydown(event: KeyboardEvent) {
  if (event.defaultPrevented) return
  if (event.key === 'Escape') {
    event.preventDefault()
    event.stopPropagation()
    close()
  } else if (compact.value && event.key === 'Tab') {
    const controls = Array.from(panel.value?.querySelectorAll<HTMLElement>('button:not(:disabled), a[href], input:not(:disabled), textarea:not(:disabled), select:not(:disabled), iframe, [tabindex="0"]') || []).filter(el => el.getClientRects().length)
    const first = controls[0]
    const last = controls.at(-1)
    if (first && event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus() }
    else if (last && !event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus() }
  }
}
function startResize(event: PointerEvent) {
  if (event.button !== 0) return
  pointerStart = event.clientX
  pointerWidth = width.value
  dragging.value = true
  ;(event.currentTarget as HTMLElement).setPointerCapture?.(event.pointerId)
  event.preventDefault()
}
function resize(event: PointerEvent) {
  if (!dragging.value) return
  const restore = capturePassage()
  preferredWidth.value = Math.max(280, Math.min(maxWidth.value, pointerWidth + pointerStart - event.clientX))
  restorePassage(restore)
}
function endResize() {
  if (!dragging.value) return
  dragging.value = false
  saveWidth()
}
function resizeByKey(event: KeyboardEvent) {
  const next = event.key === 'ArrowLeft' ? width.value + 24 : event.key === 'ArrowRight' ? width.value - 24 : event.key === 'Home' ? 280 : event.key === 'End' ? maxWidth.value : event.key === 'Enter' ? props.panelWidth : null
  if (next === null) return
  event.preventDefault()
  const restore = capturePassage()
  preferredWidth.value = Math.max(280, Math.min(maxWidth.value, next))
  saveWidth()
  restorePassage(restore)
}
watch(artifactPanelOpen, value => { unreadCount.value = runArtifactUnreadOnPanel(unreadCount.value, value) }, { flush: 'sync' })
watch(open, async value => {
  const restore = capturePassage()
  if (value) returnFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
  await nextTick()
  restore?.()
  if (value && compact.value) closeButton.value?.focus()
  if (!value) {
    const fallback = props.splitModes
      ? (pressedToggle.value === 'resources' ? resourceToggle.value : artifactToggle.value)
      : toggle.value
    const target = returnFocus?.isConnected ? returnFocus : fallback
    target?.focus({ preventScroll: true })
  }
})
watch(open, (value, prev) => {
  if (!value || prev !== false) return
  // Drawer was display:none while the preview stayed mounted. Tell it to repaint after layout.
  void nextTick(() => window.dispatchEvent(new CustomEvent(CONVERSATION_PANEL_SHOWN_EVENT)))
})
watch(open, (value) => {
  if (!props.splitModes) return
  if (!value) {
    if (mode.value) mode.value = null
  } else if (!mode.value) mode.value = 'artifacts'
})
watch(mode, (value) => {
  if (!props.splitModes) return
  const shouldOpen = value != null
  if (open.value !== shouldOpen) open.value = shouldOpen
})
watch(() => props.scopeKey, () => {
  if (!props.retainOpen) {
    open.value = false
    if (props.splitModes) mode.value = null
  }
  unreadCount.value = 0
  dragging.value = false
  returnFocus = null
})
watch(() => props.storageKey, readWidth)
watch(() => props.panelEnabled, enabled => { if (!enabled) { open.value = false; unreadCount.value = 0 } })
watch(compact, async value => { if (value && open.value) { await nextTick(); closeButton.value?.focus() } })
onMounted(() => {
  readWidth()
  canvasWidth.value = root.value?.clientWidth || window.innerWidth
  if (typeof ResizeObserver !== 'undefined' && root.value) {
    observer = new ResizeObserver(([entry]) => {
      if (!entry || entry.contentRect.width === canvasWidth.value) return
      const restore = capturePassage()
      canvasWidth.value = entry.contentRect.width
      restorePassage(restore)
    })
    observer.observe(root.value)
  }
})
onBeforeUnmount(() => { observer?.disconnect(); stopArtifactEdit() })
</script>

<template>
  <div ref="root" class="conversation-workspace chat-thread" :class="{ 'is-compact': compact, 'is-resizing': dragging }" data-testid="conversation-workspace" :data-panel-open="open && panelEnabled ? 'true' : 'false'">
    <header v-if="showToolbar" class="conversation-toolbar">
      <slot name="toolbar">
        <span class="conversation-heading"><Icon name="chat" :size="15" aria-hidden="true" />{{ t('pages.conversationWorkspace.conversation') }}</span>
        <div class="ml-auto flex min-w-0 items-center gap-2">
          <slot name="actions" />
          <div v-if="panelEnabled && splitModes" class="flex items-center gap-0.5">
            <button ref="resourceToggle" type="button" class="conversation-mode-toggle" data-testid="conversation-resource-toggle" :aria-pressed="mode === 'resources'" :aria-expanded="mode === 'resources'" :aria-label="mode === 'resources' ? t('pages.conversationWorkspace.closeResources') : t('pages.conversationWorkspace.openResources')" :title="t('pages.conversationWorkspace.resourceTip')" @click="toggleMode('resources')"><Icon name="stage-resources" :size="16" aria-hidden="true" /></button>
            <button ref="artifactToggle" type="button" class="conversation-mode-toggle" data-testid="conversation-panel-toggle" :aria-pressed="mode === 'artifacts'" :aria-expanded="mode === 'artifacts'" :aria-label="artifactToggleLabel" :title="t('pages.conversationWorkspace.artifacts')" @click="toggleMode('artifacts')"><ArtifactUnreadMark :count="unreadCount" :panel-open="artifactPanelOpen"><Icon name="doc" :size="16" aria-hidden="true" /></ArtifactUnreadMark></button>
          </div>
          <button v-else-if="panelEnabled" ref="toggle" type="button" class="conversation-panel-toggle" data-testid="conversation-panel-toggle" :aria-expanded="open" :aria-label="artifactToggleLabel" @click="open = !open"><ArtifactUnreadMark :count="unreadCount" :panel-open="open"><Icon name="chat-artifacts" :size="16" aria-hidden="true" /></ArtifactUnreadMark>{{ t('pages.conversationWorkspace.artifacts') }}</button>
        </div>
      </slot>
    </header>
    <div class="conversation-body">
      <div ref="main" class="conversation-main" :inert="compact && open && panelEnabled ? true : undefined" :aria-hidden="compact && open && panelEnabled ? 'true' : undefined" data-testid="conversation-main"><slot /></div>
      <div v-if="panelEnabled && open && !compact" role="separator" tabindex="0" aria-orientation="vertical" :aria-valuemin="280" :aria-valuemax="maxWidth" :aria-valuenow="width" :aria-label="t('pages.conversationWorkspace.resizePanel')" class="conversation-sash" data-testid="conversation-panel-sash" @pointerdown="startResize" @pointermove="resize" @pointerup="endResize" @pointercancel="endResize" @keydown="resizeByKey" />
      <aside v-if="panelEnabled" v-show="open" ref="panel" class="conversation-panel" :style="compact ? undefined : { width: width + 'px' }" :role="compact ? 'dialog' : 'complementary'" :aria-modal="compact && open ? 'true' : undefined" :aria-label="panelTitle" :data-drawer-mode="splitModes ? (mode || 'closed') : undefined" data-testid="conversation-artifact-panel" @keydown="onPanelKeydown">
        <header class="conversation-panel-heading">
          <Icon v-if="!compact" :name="headingIcon" :size="15" class="shrink-0 text-txt3" />
          <span class="min-w-0 flex-1 truncate text-xs font-medium text-txt2" :title="panelTitle">{{ panelTitle }}</span>
          <button ref="closeButton" type="button" class="conversation-close" data-testid="conversation-panel-close" :aria-label="compact ? t('pages.conversationWorkspace.backToChat') : (splitModes && mode === 'resources' ? t('pages.conversationWorkspace.closeResources') : t('pages.conversationWorkspace.closeArtifacts'))" @click="close"><Icon :key="compact ? 'back' : 'close'" :name="compact ? 'arrow-left' : 'close'" :size="16" /><span v-if="compact" class="text-xs">{{ t('pages.conversationWorkspace.backToChat') }}</span></button>
        </header>
        <div class="conversation-panel-content"><slot name="panel" /></div>
      </aside>
    </div>
  </div>
</template>

<style scoped>
.conversation-workspace { display: flex; position: relative; min-width: 0; min-height: 0; height: 100%; flex: 1; flex-direction: column; overflow: hidden; background: rgb(var(--c-surface)); }
.conversation-toolbar { display: flex; min-height: 44px; flex: none; align-items: center; gap: 12px; padding: 4px 16px; border-bottom: 1px solid rgb(var(--c-line) / .65); }
.conversation-heading { display: inline-flex; align-items: center; gap: 8px; color: rgb(var(--c-txt2)); font-size: 12px; font-weight: 500; }
.conversation-panel-toggle, .conversation-close { display: inline-flex; min-height: 34px; align-items: center; justify-content: center; gap: 7px; overflow: visible; border: 1px solid transparent; border-radius: 9px; padding: 0 10px; color: rgb(var(--c-txt2)); font-size: 12px; transition: background var(--dur-ui), color var(--dur-ui), border-color var(--dur-ui); }
.conversation-panel-toggle:hover, .conversation-close:hover { background: rgb(var(--c-txt) / .05); color: rgb(var(--c-txt)); }
.conversation-panel-toggle[aria-expanded="true"] { border-color: rgb(var(--c-accent) / .12); background: rgb(var(--c-accent-dim)); color: rgb(var(--c-accent-2)); }
.conversation-panel-toggle:focus-visible, .conversation-close:focus-visible, .conversation-mode-toggle:focus-visible { outline: 2px solid rgb(var(--c-accent) / .5); outline-offset: 2px; }
.conversation-mode-toggle { display: inline-flex; width: 32px; height: 32px; flex: none; align-items: center; justify-content: center; padding: 0; border: 1px solid transparent; border-radius: 8px; background: transparent; color: rgb(var(--c-txt3)); transition: background var(--dur-ui), color var(--dur-ui), border-color var(--dur-ui); }
.conversation-mode-toggle:hover { background: rgb(var(--c-txt) / .04); color: rgb(var(--c-txt)); }
.conversation-mode-toggle[aria-pressed="true"] { border-color: rgb(var(--c-line)); background: rgb(var(--c-base)); color: rgb(var(--c-txt)); }
.conversation-body { display: flex; position: relative; min-width: 0; min-height: 0; flex: 1; overflow: hidden; }
.conversation-main { display: flex; min-width: 0; min-height: 0; flex: 1; flex-direction: column; overflow: hidden; }
.conversation-panel { display: flex; min-width: 0; min-height: 0; flex: none; flex-direction: column; overflow: hidden; border-left: 1px solid rgb(var(--c-line)); background: rgb(var(--c-surface)); animation: conversation-panel-reveal 160ms ease-out; }
.conversation-panel-heading { display: flex; min-height: 44px; flex: none; align-items: center; gap: 8px; padding: 4px 10px 4px 16px; border-bottom: 1px solid rgb(var(--c-line) / .65); }
.conversation-panel-content { display: flex; min-width: 0; min-height: 0; flex: 1; flex-direction: column; overflow: hidden; }
.conversation-sash { position: relative; width: 4px; flex: none; cursor: col-resize; touch-action: none; background: transparent; }
.conversation-sash:hover, .conversation-sash:focus-visible, .is-resizing .conversation-sash { background: rgb(var(--c-accent) / .4); outline: 0; }
.is-resizing { user-select: none; }
.is-compact .conversation-panel { position: absolute; z-index: 20; inset: 0; width: 100%; border-left: 0; }
.is-compact .conversation-panel-heading { padding-inline: 12px; }
.is-compact .conversation-close, .is-compact .conversation-panel-toggle { min-height: 44px; }
.is-compact .conversation-mode-toggle { width: 44px; height: 44px; }
@keyframes conversation-panel-reveal { from { opacity: .4; } to { opacity: 1; } }
@media (prefers-reduced-motion: reduce) {
  .conversation-panel-toggle, .conversation-close, .conversation-mode-toggle { transition: none; }
  .conversation-panel { animation: none; }
}
</style>
