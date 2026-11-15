import { nextTick, ref, type Ref } from 'vue'

/** Input idle time after which a focused, emptied field stops holding list auto-apply. */
export const TYPING_IDLE_MS = 2000

type InboxInputGuard = {
  /**
   * The focused text field (gate form, clarify composer, filters…) holds a
   * non-empty draft. Counted as editing for list auto-apply so the caret and the
   * typed text are never disturbed; an empty focused field (e.g. the composer
   * right after sending) does not freeze the list.
   */
  focusedFieldHasDraft: Ref<boolean>
  /**
   * The user typed into the focused field within the last TYPING_IDLE_MS. Clearing
   * a draft (Ctrl+A, Backspace) and typing on must not count as "done editing":
   * the hold lasts until the field blurs or input has been idle for a while.
   */
  typingRecently: Ref<boolean>
  /** Start tracking document focus and input. */
  attach: () => void
  /** Stop tracking and drop the typing hold. */
  detach: () => void
  /**
   * Snapshot the focused text field so a silent list reload can never take the
   * caret away. If the field is still connected nothing happens; if a re-render
   * replaced it, focus moves to its replacement (same data-testid) with the same
   * selection — but only when focus fell to <body>, never away from a field the
   * user moved to themselves.
   */
  captureFocus: () => (() => void) | null
}

function isTextEntry(el: Element | null): el is HTMLElement {
  if (!(el instanceof HTMLElement)) return false
  if (el.isContentEditable) return true
  if (el instanceof HTMLTextAreaElement) return true
  if (el instanceof HTMLInputElement) {
    return !['button', 'checkbox', 'radio', 'submit', 'reset', 'file', 'image', 'range', 'color'].includes(el.type)
  }
  return false
}

function fieldHasDraft(el: HTMLElement): boolean {
  if (el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement) return el.value.trim().length > 0
  return (el.textContent || '').trim().length > 0
}

/** Input protection for the inbox page: owns the focus/draft/typing state and its document listeners. */
export function createInboxInputGuard(): InboxInputGuard {
  const focusedFieldHasDraft = ref(false)
  const typingRecently = ref(false)
  let typingIdleTimer: ReturnType<typeof setTimeout> | null = null

  function clearTypingIdle() {
    if (typingIdleTimer) clearTimeout(typingIdleTimer)
    typingIdleTimer = null
    typingRecently.value = false
  }
  function syncFocusedFieldDraft() {
    const el = typeof document !== 'undefined' ? document.activeElement : null
    const entry = isTextEntry(el)
    focusedFieldHasDraft.value = entry && fieldHasDraft(el)
    if (!entry) clearTypingIdle()
  }
  function onFieldInput(e: Event) {
    syncFocusedFieldDraft()
    const el = typeof document !== 'undefined' ? document.activeElement : null
    if (!isTextEntry(el) || (e.target instanceof Node && !el.contains(e.target))) return
    typingRecently.value = true
    if (typingIdleTimer) clearTimeout(typingIdleTimer)
    typingIdleTimer = setTimeout(() => {
      typingIdleTimer = null
      typingRecently.value = false
    }, TYPING_IDLE_MS)
  }
  function onFocusOut() {
    // activeElement is still the old field during focusout; read it next tick.
    void nextTick(syncFocusedFieldDraft)
  }

  function attach() {
    syncFocusedFieldDraft()
    document.addEventListener('focusin', syncFocusedFieldDraft)
    document.addEventListener('focusout', onFocusOut)
    document.addEventListener('input', onFieldInput)
  }
  function detach() {
    document.removeEventListener('focusin', syncFocusedFieldDraft)
    document.removeEventListener('focusout', onFocusOut)
    document.removeEventListener('input', onFieldInput)
    clearTypingIdle()
  }

  function captureFocus(): (() => void) | null {
    if (typeof document === 'undefined') return null
    const el = document.activeElement
    if (!isTextEntry(el)) return null
    const tid = el.getAttribute('data-testid')
    const sel =
      el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement
        ? { start: el.selectionStart, end: el.selectionEnd }
        : null
    return () => {
      if (el.isConnected || !tid) return
      const cur = document.activeElement
      if (cur && cur !== document.body) return
      const next = document.querySelector(`[data-testid="${CSS.escape(tid)}"]`)
      if (!(next instanceof HTMLElement) || !isTextEntry(next)) return
      next.focus({ preventScroll: true })
      if (sel && (next instanceof HTMLInputElement || next instanceof HTMLTextAreaElement)) {
        const max = next.value.length
        next.setSelectionRange(Math.min(sel.start ?? max, max), Math.min(sel.end ?? max, max))
      }
    }
  }

  return { focusedFieldHasDraft, typingRecently, attach, detach, captureFocus }
}
