<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/ui/Icon.vue'
import { messagePreviewText } from '@/lib/chat/messagePreview'

export interface ChatMessageRailItem {
  id: string
  title: string
  preview?: string
}

const props = withDefaults(defineProps<{
  items: ChatMessageRailItem[]
  scroller: HTMLElement | null
  scopeKey: string
  hasMore?: boolean
  loadingEarlier?: boolean
}>(), { hasMore: false, loadingEarlier: false })
const emit = defineEmits<{ navigate: [id: string]; loadEarlier: [] }>()
const { t } = useI18n()
const tooltipId = `chat-message-rail-tooltip-${useId().replace(/:/g, '')}`
const activeId = ref('')
const hoverId = ref('')
const focusId = ref('')
const tooltip = ref<HTMLElement | null>(null)
const tooltipPosition = ref({ left: 0, top: 0, width: 300 })
const buttons = new Map<string, HTMLButtonElement>()
const titleTextCache = new Map<string, { source: string; text: string }>()
let previewTextSource = ''
let previewText = ''
const previewId = computed(() => hoverId.value || focusId.value)
const displayItems = computed(() => {
  const ids = new Set(props.items.map(item => item.id))
  for (const id of titleTextCache.keys()) if (!ids.has(id)) titleTextCache.delete(id)
  return props.items.map(item => {
    let title = titleTextCache.get(item.id)
    if (title?.source !== item.title) {
      title = { source: item.title, text: messagePreviewText(item.title).slice(0, 240) }
      titleTextCache.set(item.id, title)
    }
    return { ...item, title: title.text }
  })
})
const previewItem = computed(() => {
  const item = displayItems.value.find(item => item.id === previewId.value)
  if (!item) return undefined
  const source = item.preview || ''
  if (source !== previewTextSource) {
    previewTextSource = source
    previewText = messagePreviewText(source).slice(0, 480)
  }
  return { ...item, preview: previewText }
})
let frame = 0
let highlighted: HTMLElement | null = null
let highlightTimer: ReturnType<typeof setTimeout> | undefined
let resize: ResizeObserver | null = null

function messageTargets(): Map<string, HTMLElement> {
  const targets = new Map<string, HTMLElement>()
  for (const element of props.scroller?.querySelectorAll<HTMLElement>('[data-chat-message-id], [data-msg-id]') || []) {
    const id = element.dataset.chatMessageId || element.dataset.msgId
    if (id) targets.set(id, element)
  }
  return targets
}
function targetFor(id: string): HTMLElement | null { return messageTargets().get(id) ?? null }


function clearHighlight() {
  if (highlightTimer) clearTimeout(highlightTimer)
  highlightTimer = undefined
  highlighted?.classList.remove('chat-message-located')
  highlighted = null
}

function updateActive() {
  frame = 0
  const scroller = props.scroller
  if (!scroller || !props.items.length) return
  const top = scroller.getBoundingClientRect().top + Math.min(80, scroller.clientHeight * .2)
  let found = props.items[0]!.id
  const targets = messageTargets()
  for (const item of props.items) {
    const target = targets.get(item.id)
    if (!target) continue
    if (target.getBoundingClientRect().top <= top) found = item.id
    else break
  }
  activeId.value = found
}

function scheduleActive() {
  if (!frame) frame = requestAnimationFrame(updateActive)
}

async function positionTooltip() {
  const button = buttons.get(previewId.value)
  if (!button) return
  const rect = button.getBoundingClientRect()
  const width = Math.min(320, Math.max(180, window.innerWidth - 24))
  const left = Math.max(12, Math.min(rect.right + 12, window.innerWidth - width - 12))
  tooltipPosition.value = { left, top: Math.max(12, rect.top - 12), width }
  await nextTick()
  if (!tooltip.value) return
  const height = tooltip.value.getBoundingClientRect().height
  tooltipPosition.value = { left, width, top: Math.max(12, Math.min(rect.top - 12, window.innerHeight - height - 12)) }
}

function setButton(id: string, element: unknown) {
  if (element instanceof HTMLButtonElement) buttons.set(id, element)
  else buttons.delete(id)
}

function navigate(id: string) {
  const target = targetFor(id)
  const scroller = props.scroller
  if (!target || !scroller) return
  // The host pauses its own follow state before the first scroll event can fire.
  emit('navigate', id)
  hoverId.value = ''
  focusId.value = ''
  const top = Math.max(0, scroller.scrollTop + target.getBoundingClientRect().top - scroller.getBoundingClientRect().top - 16)
  // An immediate jump also avoids a streaming host re-enabling follow during a smooth-scroll's first frames.
  if (typeof scroller.scrollTo === 'function') scroller.scrollTo({ top, behavior: 'auto' })
  else scroller.scrollTop = top
  activeId.value = id
  clearHighlight()
  highlighted = target
  target.classList.add('chat-message-located')
  highlightTimer = setTimeout(clearHighlight, 1600)
}

function onKeydown(event: KeyboardEvent, index: number) {
  let next = index
  if (event.key === 'ArrowDown' || event.key === 'ArrowRight') next = Math.min(props.items.length - 1, index + 1)
  else if (event.key === 'ArrowUp' || event.key === 'ArrowLeft') next = Math.max(0, index - 1)
  else if (event.key === 'Home') next = 0
  else if (event.key === 'End') next = props.items.length - 1
  else if (event.key === 'Escape') {
    hoverId.value = ''
    focusId.value = ''
    return
  } else return
  event.preventDefault()
  buttons.get(props.items[next]?.id || '')?.focus()
}

watch(previewId, () => { void positionTooltip() })
watch(() => props.scroller, (scroller, _, onCleanup) => {
  if (!scroller) return
  scroller.addEventListener('scroll', scheduleActive, { passive: true })
  if (typeof ResizeObserver !== 'undefined') {
    resize = new ResizeObserver(() => { scheduleActive(); void positionTooltip() })
    resize.observe(scroller)
  }
  scheduleActive()
  onCleanup(() => { scroller.removeEventListener('scroll', scheduleActive); resize?.disconnect(); resize = null })
}, { immediate: true, flush: 'post' })
watch(() => props.items.map(item => item.id).join('\n'), async () => {
  if (!props.items.some(item => item.id === hoverId.value)) hoverId.value = ''
  if (!props.items.some(item => item.id === focusId.value)) focusId.value = ''
  await nextTick()
  scheduleActive()
})
watch(() => props.scopeKey, () => {
  hoverId.value = ''
  focusId.value = ''
  activeId.value = ''
  clearHighlight()
  void nextTick(scheduleActive)
})
onBeforeUnmount(() => { if (frame) cancelAnimationFrame(frame); clearHighlight(); resize?.disconnect() })
</script>

<template>
  <nav v-if="items.length || hasMore" class="chat-message-rail" :aria-label="t('pages.chatNavigation.ariaLabel')" data-testid="chat-message-rail">
    <button v-if="hasMore" type="button" class="chat-message-rail-earlier" :disabled="loadingEarlier" :aria-label="t(loadingEarlier ? 'pages.chatNavigation.loadingEarlier' : 'pages.chatNavigation.loadEarlier')" :title="t('pages.chatNavigation.loadedHistory')" data-testid="chat-message-rail-earlier" @click="emit('loadEarlier')">
      <Icon :key="loadingEarlier ? 'loading' : 'earlier'" :name="loadingEarlier ? 'spinner' : 'arrow-up'" :size="13" :class="loadingEarlier ? 'animate-spin' : ''" />
    </button>
    <div class="chat-message-rail-track" data-testid="chat-message-rail-track" @scroll.passive="positionTooltip">
      <button v-for="(item, index) in displayItems" :key="item.id" :ref="el => setButton(item.id, el)" type="button" class="chat-message-rail-mark" :class="{ 'is-active': item.id === activeId }" :aria-current="item.id === activeId ? 'step' : undefined" :aria-label="t('pages.chatNavigation.jumpTo', { title: item.title })" :aria-describedby="previewId === item.id ? tooltipId : undefined" :tabindex="item.id === (focusId || activeId || items[0]?.id) ? 0 : -1" :data-message-id="item.id" data-testid="chat-message-rail-mark" @mouseenter="hoverId = item.id" @mouseleave="hoverId = ''" @focus="focusId = item.id" @blur="focusId = ''" @keydown="onKeydown($event, index)" @click="navigate(item.id)">
        <span aria-hidden="true" />
      </button>
    </div>
    <Teleport to="body">
      <div v-if="previewItem" :id="tooltipId" ref="tooltip" role="tooltip" class="chat-message-rail-tooltip" :style="{ left: `${tooltipPosition.left}px`, top: `${tooltipPosition.top}px`, width: `${tooltipPosition.width}px` }" data-testid="chat-message-rail-tooltip">
        <p class="chat-message-rail-question">{{ previewItem.title }}</p>
        <p v-if="previewItem.preview" class="chat-message-rail-preview">{{ previewItem.preview }}</p>
        <p v-if="hasMore" class="chat-message-rail-history">{{ t('pages.chatNavigation.loadedHistory') }}</p>
      </div>
    </Teleport>
  </nav>
</template>

<style scoped>
.chat-message-rail { position: absolute; z-index: 5; inset-block: 18px; left: 0; width: 26px; display: flex; flex-direction: column; align-items: center; justify-content: center; pointer-events: none; }
.chat-message-rail-track { display: flex; min-height: 0; max-height: min(420px, 100%); flex-direction: column; gap: 3px; overflow-y: auto; padding-block: 5px; scrollbar-width: none; pointer-events: auto; }
.chat-message-rail-track::-webkit-scrollbar { display: none; }
.chat-message-rail-mark { display: flex; width: 26px; min-height: 20px; flex: none; align-items: center; justify-content: center; outline: none; }
.chat-message-rail-mark > span { width: 10px; height: 2px; border-radius: 1px; background: rgb(var(--c-txt3) / .5); transition: width 160ms ease, background 160ms ease; }
.chat-message-rail-mark.is-active > span, .chat-message-rail-mark:hover > span, .chat-message-rail-mark:focus-visible > span { width: 17px; background: rgb(var(--c-accent)); }
.chat-message-rail-mark:focus-visible { outline: 2px solid rgb(var(--c-accent) / .45); outline-offset: -2px; border-radius: 4px; }
.chat-message-rail-earlier { display: flex; width: 26px; min-height: 26px; flex: none; align-items: center; justify-content: center; color: rgb(var(--c-txt3)); pointer-events: auto; }
.chat-message-rail-earlier:hover { color: rgb(var(--c-txt)); }
.chat-message-rail-earlier:disabled { opacity: .5; }
.chat-message-rail-tooltip { position: fixed; z-index: 90; pointer-events: none; padding: 14px 16px; border: 1px solid rgb(var(--c-line) / .9); border-radius: 12px; background: rgb(var(--c-surface)); color: rgb(var(--c-txt)); box-shadow: 0 8px 28px rgb(var(--c-txt) / .08), 0 2px 6px rgb(var(--c-txt) / .04); overflow-wrap: anywhere; }
.chat-message-rail-question { display: -webkit-box; -webkit-box-orient: vertical; -webkit-line-clamp: 3; overflow: hidden; font-size: 12px; line-height: 1.65; font-weight: 600; }
.chat-message-rail-preview { display: -webkit-box; -webkit-box-orient: vertical; -webkit-line-clamp: 6; overflow: hidden; margin-top: 8px; white-space: pre-line; font-size: 13px; line-height: 1.75; color: rgb(var(--c-txt2)); }
.chat-message-rail-history { margin-top: 8px; font-size: 10px; line-height: 1.5; color: rgb(var(--c-txt3)); }
:global(.chat-message-located) { outline: 2px solid rgb(var(--c-accent) / .28); outline-offset: 6px; border-radius: 7px; animation: chat-located 1.6s ease-out; }
@keyframes chat-located { from { outline-color: rgb(var(--c-accent) / .55); } to { outline-color: rgb(var(--c-accent) / .15); } }
@media (prefers-reduced-motion: reduce) { .chat-message-rail-mark > span { transition: none; } :global(.chat-message-located) { animation: none; } }
</style>
