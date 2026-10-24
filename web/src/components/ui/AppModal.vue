<script lang="ts">
// Track all open dialogs so a focus trap never competes with a later modal.
const openDialogs: symbol[] = []
</script>

<script setup lang="ts">
import { nextTick, ref, watch, onBeforeUnmount, useId } from 'vue'
import Icon from './Icon.vue'

const titleId = `app-modal-title-${useId().replace(/:/g, '')}`

const props = withDefaults(
  defineProps<{
    open: boolean
    title?: string
    width?: number
    hideBody?: boolean
    bodyOverflow?: 'auto' | 'hidden'
    bodyMinHeight?: number
    /**
     * When false, clicking the backdrop does not emit close (default true).
     */
    closeOnBackdrop?: boolean
    /**
     * Opt-in Escape-to-close. Default false so existing closeOnBackdrop=false
     * callers (and other modals) are not surprised by a new Esc path.
     */
    closeOnEsc?: boolean
    /** Opt-in keyboard focus management for multi-step dialogs. */
    trapFocus?: boolean
    closeLabel?: string
  }>(),
  { bodyOverflow: 'auto', closeOnBackdrop: true, closeOnEsc: false, trapFocus: false, closeLabel: 'Close' },
)
const emit = defineEmits<{ (e: 'close'): void }>()
const dialogEl = ref<HTMLElement | null>(null)
const dialogToken = Symbol('app-modal')
let restoreTarget: HTMLElement | null = null
let focusManaged = false
let focusGeneration = 0

function isTopDialog() { return openDialogs.at(-1) === dialogToken }

function removeFromStack() {
  const index = openDialogs.indexOf(dialogToken)
  if (index !== -1) openDialogs.splice(index, 1)
}

function focusableElements(): HTMLElement[] {
  const dialog = dialogEl.value
  if (!dialog) return []
  return [...dialog.querySelectorAll<HTMLElement>('a[href], button, input, select, textarea, summary, [tabindex], [contenteditable="true"]')].filter((element) => {
    if (element.tabIndex < 0 || element.matches(':disabled') || element.closest('[hidden], [inert], [aria-hidden="true"]')) return false
    for (let current: HTMLElement | null = element; current && current !== dialog; current = current.parentElement) {
      const style = window.getComputedStyle(current)
      if (style.display === 'none' || style.visibility === 'hidden') return false
      if (current.tagName === 'DETAILS' && !current.hasAttribute('open')) {
        const summary = current.querySelector(':scope > summary')
        if (!summary?.contains(element)) return false
      }
    }
    return true
  })
}

function restoreFocus() {
  if (restoreTarget?.isConnected && restoreTarget !== document.body) restoreTarget.focus({ preventScroll: true })
  restoreTarget = null
  focusManaged = false
}

watch(
  () => [props.open, props.trapFocus] as const,
  async ([open, trapFocus]) => {
    const generation = ++focusGeneration
    if (open && !openDialogs.includes(dialogToken)) openDialogs.push(dialogToken)
    if (!open || !trapFocus) {
      const wasTop = isTopDialog()
      if (!open) removeFromStack()
      if (focusManaged) {
        if (wasTop) restoreFocus()
        else { restoreTarget = null; focusManaged = false }
      }
      return
    }
    if (!focusManaged) {
      restoreTarget = document.activeElement instanceof HTMLElement ? document.activeElement : null
      focusManaged = true
    }
    await nextTick()
    if (generation !== focusGeneration || !props.open || !props.trapFocus || !isTopDialog()) return
    const available = focusableElements()
    const target = available.find((element) => element.hasAttribute('autofocus')) || available[0] || dialogEl.value
    target?.focus({ preventScroll: true })
  },
  { immediate: true },
)

function onBackdropClick() {
  if (props.closeOnBackdrop) emit('close')
}

function showEnteringOverlay(element: Element) {
  element.removeAttribute('aria-hidden')
  ;(element as HTMLElement).inert = false
}

function hideLeavingOverlay(element: Element) {
  // The fade can outlive the next dialog. Remove this whole layer from the
  // accessibility tree and tab order as soon as its close transition starts.
  element.setAttribute('aria-hidden', 'true')
  ;(element as HTMLElement).inert = true
}

function onKeydown(event: KeyboardEvent) {
  if (props.trapFocus && !isTopDialog()) return
  if (event.key === 'Escape' && props.closeOnEsc) {
    event.preventDefault()
    emit('close')
  } else if (event.key === 'Tab' && props.trapFocus) {
    const available = focusableElements()
    const first = available[0]
    const last = available.at(-1)
    const active = document.activeElement
    if (!first || !last) {
      event.preventDefault()
      dialogEl.value?.focus({ preventScroll: true })
    } else if (!dialogEl.value?.contains(active) || (event.shiftKey ? active === first : active === last)) {
      event.preventDefault()
      ;(event.shiftKey ? last : first).focus({ preventScroll: true })
    }
  }
}

watch(
  () => props.open && (props.closeOnEsc || props.trapFocus),
  (listen) => {
    if (listen) {
      window.addEventListener('keydown', onKeydown)
    } else {
      window.removeEventListener('keydown', onKeydown)
    }
  },
  { immediate: true },
)

onBeforeUnmount(() => {
  focusGeneration++
  window.removeEventListener('keydown', onKeydown)
  const wasTop = isTopDialog()
  removeFromStack()
  if (focusManaged && wasTop) restoreFocus()
})

const scrollAreaEl = ref<HTMLElement | null>(null)

defineExpose({ scrollAreaEl })
</script>

<template>
  <Teleport to="body">
    <Transition name="modal" @before-enter="showEnteringOverlay" @before-leave="hideLeavingOverlay">
      <div v-if="open" class="fixed inset-0 z-50 flex items-center justify-center p-4">
        <div class="absolute inset-0 bg-black/60 backdrop-blur-sm" @click="onBackdropClick" />
        <div
          ref="dialogEl"
          class="relative flex max-h-[88vh] w-full flex-col overflow-hidden rounded-xl border border-line bg-surface shadow-card"
          role="dialog"
          :tabindex="trapFocus ? -1 : undefined"
          aria-modal="true"
          :aria-labelledby="titleId"
          :style="{ maxWidth: (width || 560) + 'px' }"
        >
          <div class="flex h-14 shrink-0 items-center gap-2 border-b border-line px-5">
            <div v-if="$slots.header" :id="titleId" class="flex min-w-0 flex-1 items-center">
              <slot name="header" />
            </div>
            <div v-else :id="titleId" class="flex-1 text-[15px] font-semibold text-txt">{{ title }}</div>
            <button class="flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-txt3 hover:bg-elevated hover:text-txt" :aria-label="closeLabel" @click="emit('close')">
              <Icon name="close" :size="18" />
            </button>
          </div>
          <div
            v-if="!hideBody"
            ref="scrollAreaEl"
            class="scroll-area modal-scroll-area min-h-0 flex-1 p-5"
            :class="bodyOverflow === 'hidden' ? 'overflow-hidden' : 'overflow-y-auto'"
            :style="bodyMinHeight ? { minHeight: `${bodyMinHeight}px` } : undefined"
          >
            <slot />
          </div>
          <div v-if="$slots.footer" class="flex shrink-0 items-center justify-end gap-2 border-t border-line p-4">
            <slot name="footer" />
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.modal-enter-active,
.modal-leave-active {
  transition: opacity var(--dur-overlay) var(--ease-out-expo);
}
.modal-enter-active > div:last-child,
.modal-leave-active > div:last-child {
  transition: transform var(--dur-overlay) var(--ease-out-expo);
}
.modal-enter-from,
.modal-leave-to {
  opacity: 0;
}
.modal-enter-from > div:last-child,
.modal-leave-to > div:last-child {
  transform: translateY(12px) scale(0.98);
}
</style>
