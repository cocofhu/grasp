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
const cardId = `chat-message-rail-card-${useId().replace(/:/g, '')}`
const activeId = ref('')
const hoverId = ref('')
const focusId = ref('')
const open = ref(false)
const track = ref<HTMLElement | null>(null)
const card = ref<HTMLElement | null>(null)
const cardPosition = ref({ left: 0, top: 0, width: 300 })
const buttons = new Map<string, HTMLButtonElement>()
const rows = new Map<string, HTMLElement>()
const titleTextCache = new Map<string, { source: string; text: string }>()
let previewTextSource = ''
let previewText = ''
// The card stays open while the pointer is over the rail or the card, so the gap between them never closes it.
let pointerInside = false
let closeTimer: ReturnType<typeof setTimeout> | undefined
const pointedId = computed(() => hoverId.value || focusId.value)
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
  const id = pointedId.value || activeId.value
  const item = displayItems.value.find(item => item.id === id)
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

/** Touch screens have no hover: a tap on a mark jumps straight to the message. */
function hoverless() {
  return typeof window.matchMedia === 'function' && window.matchMedia('(hover: none)').matches
}

function cancelClose() {
  if (closeTimer) clearTimeout(closeTimer)
  closeTimer = undefined
}

function openCard() {
  cancelClose()
  if (open.value || hoverless() || !props.items.length) return
  open.value = true
  void positionCard()
}

function closeCard() {
  cancelClose()
  open.value = false
  hoverId.value = ''
}

function scheduleClose() {
  cancelClose()
  closeTimer = setTimeout(() => {
    closeTimer = undefined
    if (!pointerInside && !focusId.value) closeCard()
  }, 120)
}

function onPointerEnter() {
  pointerInside = true
  openCard()
}

function onPointerLeave() {
  pointerInside = false
  scheduleClose()
}

/** Anchor the card beside the mark the user reached for, then keep it still while they move along the list. */
async function positionCard() {
  if (!open.value) return
  const anchor = buttons.get(pointedId.value || activeId.value) ?? track.value
  if (!anchor) return
  const rail = (track.value ?? anchor).getBoundingClientRect()
  const rect = anchor.getBoundingClientRect()
  const width = Math.min(320, Math.max(200, window.innerWidth - 24))
  const left = Math.max(12, Math.min(rail.right + 6, window.innerWidth - width - 12))
  const center = rect.top + rect.height / 2
  cardPosition.value = { left, width, top: Math.max(12, center - 20) }
  await nextTick()
  if (!card.value) return
  const height = card.value.getBoundingClientRect().height
  cardPosition.value = { left, width, top: Math.max(12, Math.min(center - height / 2, window.innerHeight - height - 12)) }
  rows.get(pointedId.value || activeId.value)?.scrollIntoView?.({ block: 'nearest' })
}

function setButton(id: string, element: unknown) {
  if (element instanceof HTMLButtonElement) buttons.set(id, element)
  else buttons.delete(id)
}

function setRow(id: string, element: unknown) {
  if (element instanceof HTMLElement) rows.set(id, element)
  else rows.delete(id)
}

function navigate(id: string) {
  const target = targetFor(id)
  const scroller = props.scroller
  if (!target || !scroller) return
  // The host pauses its own follow state before the first scroll event can fire.
  emit('navigate', id)
  focusId.value = ''
  closeCard()
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

function onFocus(id: string) {
  focusId.value = id
  openCard()
  rows.get(id)?.scrollIntoView?.({ block: 'nearest' })
}

function onBlur() {
  focusId.value = ''
  scheduleClose()
}

function onKeydown(event: KeyboardEvent, index: number) {
  let next = index
  if (event.key === 'ArrowDown' || event.key === 'ArrowRight') next = Math.min(props.items.length - 1, index + 1)
  else if (event.key === 'ArrowUp' || event.key === 'ArrowLeft') next = Math.max(0, index - 1)
  else if (event.key === 'Home') next = 0
  else if (event.key === 'End') next = props.items.length - 1
  else if (event.key === 'Escape') {
    focusId.value = ''
    closeCard()
    return
  } else return
  event.preventDefault()
  buttons.get(props.items[next]?.id || '')?.focus()
}

watch(() => props.scroller, (scroller, _, onCleanup) => {
  if (!scroller) return
  scroller.addEventListener('scroll', scheduleActive, { passive: true })
  if (typeof ResizeObserver !== 'undefined') {
    resize = new ResizeObserver(() => { scheduleActive(); void positionCard() })
    resize.observe(scroller)
  }
  scheduleActive()
  onCleanup(() => { scroller.removeEventListener('scroll', scheduleActive); resize?.disconnect(); resize = null })
}, { immediate: true, flush: 'post' })
watch(() => props.items.map(item => item.id).join('\n'), async () => {
  if (!props.items.some(item => item.id === hoverId.value)) hoverId.value = ''
  if (!props.items.some(item => item.id === focusId.value)) focusId.value = ''
  if (!props.items.length) closeCard()
  await nextTick()
  scheduleActive()
})
watch(() => props.scopeKey, () => {
  focusId.value = ''
  activeId.value = ''
  pointerInside = false
  closeCard()
  clearHighlight()
  void nextTick(scheduleActive)
})
onBeforeUnmount(() => { if (frame) cancelAnimationFrame(frame); cancelClose(); clearHighlight(); resize?.disconnect() })
</script>

<template>
  <nav v-if="items.length || hasMore" class="chat-message-rail" :aria-label="t('pages.chatNavigation.ariaLabel')" data-testid="chat-message-rail">
    <button v-if="hasMore" type="button" class="chat-message-rail-earlier" :disabled="loadingEarlier" :aria-label="t(loadingEarlier ? 'pages.chatNavigation.loadingEarlier' : 'pages.chatNavigation.loadEarlier')" :title="t('pages.chatNavigation.loadedHistory')" data-testid="chat-message-rail-earlier" @click="emit('loadEarlier')">
      <Icon :key="loadingEarlier ? 'loading' : 'earlier'" :name="loadingEarlier ? 'spinner' : 'arrow-up'" :size="13" :class="loadingEarlier ? 'animate-spin' : ''" />
    </button>
    <div ref="track" class="chat-message-rail-track" data-testid="chat-message-rail-track" @mouseenter="onPointerEnter" @mouseleave="onPointerLeave">
      <button v-for="(item, index) in displayItems" :key="item.id" :ref="el => setButton(item.id, el)" type="button" class="chat-message-rail-mark" :class="{ 'is-active': item.id === activeId, 'is-pointed': item.id === pointedId }" :aria-current="item.id === activeId ? 'step' : undefined" :aria-label="t('pages.chatNavigation.jumpTo', { title: item.title })" :aria-controls="open ? cardId : undefined" :tabindex="item.id === (focusId || activeId || items[0]?.id) ? 0 : -1" :data-message-id="item.id" data-testid="chat-message-rail-mark" @mouseenter="hoverId = item.id; onPointerEnter()" @focus="onFocus(item.id)" @blur="onBlur" @keydown="onKeydown($event, index)" @click="navigate(item.id)">
        <span aria-hidden="true" />
      </button>
    </div>
    <Teleport to="body">
      <Transition name="chat-message-rail-card">
        <div v-if="open && displayItems.length" :id="cardId" ref="card" class="chat-message-rail-card" :style="{ left: `${cardPosition.left}px`, top: `${cardPosition.top}px`, width: `${cardPosition.width}px` }" data-testid="chat-message-rail-card" @mouseenter="onPointerEnter" @mouseleave="onPointerLeave(); hoverId = ''">
          <button v-if="hasMore" type="button" class="chat-message-rail-card-earlier" tabindex="-1" :disabled="loadingEarlier" data-testid="chat-message-rail-card-earlier" @click="emit('loadEarlier')">
            <Icon :name="loadingEarlier ? 'spinner' : 'arrow-up'" :size="12" :class="loadingEarlier ? 'animate-spin' : ''" />
            {{ t(loadingEarlier ? 'pages.chatNavigation.loadingEarlier' : 'pages.chatNavigation.loadEarlier') }}
          </button>
          <div class="chat-message-rail-list" role="listbox" :aria-label="t('pages.chatNavigation.ariaLabel')">
            <div v-for="item in displayItems" :key="item.id" :ref="el => setRow(item.id, el)" role="option" class="chat-message-rail-row" :class="{ 'is-active': item.id === activeId, 'is-pointed': item.id === pointedId }" :aria-selected="item.id === activeId" :data-message-id="item.id" data-testid="chat-message-rail-row" @mouseenter="hoverId = item.id" @click="navigate(item.id)">
              {{ item.title }}
            </div>
          </div>
          <div v-if="previewItem?.preview || hasMore" class="chat-message-rail-detail" data-testid="chat-message-rail-detail">
            <p v-if="previewItem?.preview" class="chat-message-rail-preview">{{ previewItem.preview }}</p>
            <p v-if="hasMore" class="chat-message-rail-history">{{ t('pages.chatNavigation.loadedHistory') }}</p>
          </div>
        </div>
      </Transition>
    </Teleport>
  </nav>
</template>

<style scoped>
.chat-message-rail { position: absolute; z-index: 5; inset-block: 18px; left: 0; width: 26px; display: flex; flex-direction: column; align-items: center; justify-content: center; pointer-events: none; }
.chat-message-rail-track { display: flex; min-height: 0; max-height: min(420px, 100%); flex-direction: column; gap: 2px; overflow-y: auto; padding-block: 5px; scrollbar-width: none; pointer-events: auto; }
.chat-message-rail-track::-webkit-scrollbar { display: none; }
.chat-message-rail-mark { display: flex; width: 26px; min-height: 18px; flex: none; align-items: center; justify-content: flex-start; padding-left: 7px; outline: none; }
.chat-message-rail-mark > span { width: 8px; height: 1.5px; border-radius: 1px; background: rgb(var(--c-txt3) / .4); transition: width 160ms ease, background 160ms ease; }
.chat-message-rail-mark.is-pointed > span { width: 12px; background: rgb(var(--c-txt2)); }
.chat-message-rail-mark.is-active > span { width: 16px; background: rgb(var(--c-txt)); }
.chat-message-rail-mark:focus-visible { outline: 2px solid rgb(var(--c-accent) / .45); outline-offset: -2px; border-radius: 4px; }
.chat-message-rail-earlier { display: flex; width: 26px; min-height: 26px; flex: none; align-items: center; justify-content: center; color: rgb(var(--c-txt3)); pointer-events: auto; }
.chat-message-rail-earlier:hover { color: rgb(var(--c-txt)); }
.chat-message-rail-earlier:disabled { opacity: .5; }
.chat-message-rail-card { position: fixed; z-index: 90; display: flex; max-height: calc(100vh - 24px); flex-direction: column; overflow: hidden; padding: 6px; border: 1px solid rgb(var(--c-line) / .8); border-radius: 12px; background: rgb(var(--c-surface)); color: rgb(var(--c-txt)); box-shadow: 0 8px 28px rgb(var(--c-txt) / .08), 0 2px 6px rgb(var(--c-txt) / .04); }
.chat-message-rail-card-earlier { display: flex; flex: none; align-items: center; gap: 6px; margin-bottom: 2px; padding: 6px 10px; border-radius: 8px; font-size: 12px; color: rgb(var(--c-txt3)); }
.chat-message-rail-card-earlier:hover { background: rgb(var(--c-elevated)); color: rgb(var(--c-txt)); }
.chat-message-rail-card-earlier:disabled { opacity: .6; }
.chat-message-rail-list { min-height: 0; max-height: min(60vh, 480px); overflow-y: auto; overscroll-behavior: contain; scrollbar-width: thin; }
.chat-message-rail-row { position: relative; overflow: hidden; padding: 6px 10px 6px 12px; border-radius: 8px; cursor: pointer; white-space: nowrap; text-overflow: ellipsis; font-size: 13px; line-height: 20px; color: rgb(var(--c-txt2)); transition: background 120ms ease, color 120ms ease; }
.chat-message-rail-row.is-pointed { background: rgb(var(--c-elevated)); color: rgb(var(--c-txt)); }
.chat-message-rail-row.is-active { font-weight: 600; color: rgb(var(--c-txt)); }
.chat-message-rail-row.is-active::before { content: ''; position: absolute; left: 3px; top: 8px; bottom: 8px; width: 2px; border-radius: 1px; background: rgb(var(--c-txt)); }
.chat-message-rail-detail { flex: none; margin: 4px 4px 2px; padding: 8px 6px 2px; border-top: 1px solid rgb(var(--c-line) / .7); overflow-wrap: anywhere; }
.chat-message-rail-preview { display: -webkit-box; -webkit-box-orient: vertical; -webkit-line-clamp: 3; overflow: hidden; white-space: pre-line; font-size: 12px; line-height: 1.65; color: rgb(var(--c-txt3)); }
.chat-message-rail-history { margin-top: 4px; font-size: 10px; line-height: 1.5; color: rgb(var(--c-txt3)); }
.chat-message-rail-history:first-child { margin-top: 0; }
.chat-message-rail-card-enter-active, .chat-message-rail-card-leave-active { transition: opacity 140ms ease, transform 140ms ease; }
.chat-message-rail-card-enter-from, .chat-message-rail-card-leave-to { opacity: 0; transform: translateX(-4px); }
:global(.chat-message-located) { outline: 2px solid rgb(var(--c-accent) / .28); outline-offset: 6px; border-radius: 7px; animation: chat-located 1.6s ease-out; }
@keyframes chat-located { from { outline-color: rgb(var(--c-accent) / .55); } to { outline-color: rgb(var(--c-accent) / .15); } }
@media (prefers-reduced-motion: reduce) { .chat-message-rail-mark > span, .chat-message-rail-row { transition: none; } .chat-message-rail-card-enter-active, .chat-message-rail-card-leave-active { transition: none; } :global(.chat-message-located) { animation: none; } }
</style>
