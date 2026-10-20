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
import { conversationPanelVisibleKey } from '@/lib/chat/conversationArtifacts'

const props = withDefaults(defineProps<{
  title?: string
  scopeKey: string
  storageKey?: string
  panelWidth?: number
  panelEnabled?: boolean
  showToolbar?: boolean
}>(), { title: '', storageKey: 'grasp.conversation.panelWidth', panelWidth: 440, panelEnabled: true, showToolbar: true })
const open = defineModel<boolean>('open', { default: false })
provide(conversationPanelVisibleKey, computed(() => open.value && props.panelEnabled))
const { t } = useI18n()
const root = ref<HTMLElement | null>(null)
const main = ref<HTMLElement | null>(null)
const panel = ref<HTMLElement | null>(null)
const toggle = ref<HTMLButtonElement | null>(null)
const closeButton = ref<HTMLButtonElement | null>(null)
const canvasWidth = ref(0)
const preferredWidth = ref(props.panelWidth)
const dragging = ref(false)
const compact = computed(() => canvasWidth.value > 0 && canvasWidth.value < 800)
const maxWidth = computed(() => Math.max(280, Math.min(880, canvasWidth.value - 364)))
const width = computed(() => Math.max(280, Math.min(maxWidth.value, preferredWidth.value)))
const panelTitle = computed(() => props.title || t('pages.conversationWorkspace.artifacts'))
const unreadCount = ref(0)
const artifactToggleLabel = computed(() => artifactButtonAriaLabel(open.value, unreadCount.value, {
  open: t('pages.conversationWorkspace.openArtifacts'),
  close: t('pages.conversationWorkspace.closeArtifacts'),
  unread: (count) => t('pages.conversationWorkspace.openArtifactsUnread', { n: count }),
  cap: t('pages.conversationWorkspace.openArtifactsUnreadCap'),
}))
const stopArtifactEdit = onRunArtifactEdit(() => {
  if (!props.showToolbar || !props.panelEnabled) return
  unreadCount.value = nextRunArtifactUnread(unreadCount.value, open.value, 'artifact')
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
function close() { open.value = false }
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
watch(open, value => { unreadCount.value = runArtifactUnreadOnPanel(unreadCount.value, value) }, { flush: 'sync' })
watch(open, async value => {
  const restore = capturePassage()
  if (value) returnFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
  await nextTick()
  restore?.()
  if (value && compact.value) closeButton.value?.focus()
  if (!value) {
    const target = returnFocus?.isConnected ? returnFocus : toggle.value
    target?.focus({ preventScroll: true })
  }
})
watch(() => props.scopeKey, () => { open.value = false; unreadCount.value = 0; dragging.value = false; returnFocus = null })
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
          <button v-if="panelEnabled" ref="toggle" type="button" class="conversation-panel-toggle" data-testid="conversation-panel-toggle" :aria-expanded="open" :aria-label="artifactToggleLabel" @click="open = !open"><ArtifactUnreadMark :count="unreadCount" :panel-open="open"><Icon name="chat-artifacts" :size="16" aria-hidden="true" /></ArtifactUnreadMark>{{ t('pages.conversationWorkspace.artifacts') }}</button>
        </div>
      </slot>
    </header>
    <div class="conversation-body">
      <div ref="main" class="conversation-main" :inert="compact && open && panelEnabled ? true : undefined" :aria-hidden="compact && open && panelEnabled ? 'true' : undefined" data-testid="conversation-main"><slot /></div>
      <div v-if="panelEnabled && open && !compact" role="separator" tabindex="0" aria-orientation="vertical" :aria-valuemin="280" :aria-valuemax="maxWidth" :aria-valuenow="width" :aria-label="t('pages.conversationWorkspace.resizePanel')" class="conversation-sash" data-testid="conversation-panel-sash" @pointerdown="startResize" @pointermove="resize" @pointerup="endResize" @pointercancel="endResize" @keydown="resizeByKey" />
      <aside v-if="panelEnabled" v-show="open" ref="panel" class="conversation-panel" :style="compact ? undefined : { width: width + 'px' }" :role="compact ? 'dialog' : 'complementary'" :aria-modal="compact && open ? 'true' : undefined" :aria-label="panelTitle" data-testid="conversation-artifact-panel" @keydown="onPanelKeydown">
        <header class="conversation-panel-heading">
          <Icon v-if="!compact" name="artifact" :size="15" class="shrink-0 text-txt3" />
          <span class="min-w-0 flex-1 truncate text-xs font-medium text-txt2" :title="panelTitle">{{ panelTitle }}</span>
          <button ref="closeButton" type="button" class="conversation-close" data-testid="conversation-panel-close" :aria-label="compact ? t('pages.conversationWorkspace.backToChat') : t('pages.conversationWorkspace.closeArtifacts')" @click="close"><Icon :key="compact ? 'back' : 'close'" :name="compact ? 'arrow-left' : 'close'" :size="16" /><span v-if="compact" class="text-xs">{{ t('pages.conversationWorkspace.backToChat') }}</span></button>
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
.conversation-panel-toggle:focus-visible, .conversation-close:focus-visible { outline: 2px solid rgb(var(--c-accent) / .5); outline-offset: 2px; }
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
@keyframes conversation-panel-reveal { from { opacity: .4; } to { opacity: 1; } }
@media (prefers-reduced-motion: reduce) {
  .conversation-panel-toggle, .conversation-close { transition: none; }
  .conversation-panel { animation: none; }
}
</style>
