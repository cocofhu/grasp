import { computed, nextTick, ref, watch, type Ref } from 'vue'
import type { MessageReference } from '@/lib/api/apiTypes'
import { MAX_REFERENCES, activeTrigger, applyEdit, insertToken, insideToken, parseGitHubReference, referenceLabel, removeToken, type ReferenceToken } from './referenceTokens'
import type { ReferenceTab } from './useReferenceSearch'

/** What the composer needs from a mounted ReferencePicker. */
export type ReferencePickerHandle = {
  move: (delta: number) => void
  confirm: () => boolean
  cycleTab: (delta: number) => void
}

/**
 * @ references in one textarea. Each picked reference owns its `@label` span;
 * any edit touching the span drops the reference, so what is sent always
 * matches what is shown.
 */
export function useReferenceComposer(opts: {
  input: Ref<string>
  textarea: Ref<HTMLTextAreaElement | null>
  composing: Ref<boolean>
  defaultRepo: Ref<string>
  /** False turns the composer back into plain text (e.g. workflow mode). */
  enabled?: Ref<boolean>
}) {
  const { input, textarea, composing, defaultRepo } = opts
  const tokens = ref<ReferenceToken[]>([])
  const trigger = ref<{ start: number; query: string } | null>(null)
  const pickerTab = ref<ReferenceTab>('all')
  const picker = ref<ReferencePickerHandle | null>(null)
  let dismissedAt = -1
  let tracked = input.value
  const refs = computed(() => tokens.value.map((token) => token.ref))
  const targets = computed(() => refs.value.map((ref) => ref.target))
  const full = computed(() => tokens.value.length >= MAX_REFERENCES)
  const enabled = () => opts.enabled?.value !== false

  watch(input, (next) => {
    tokens.value = applyEdit(tokens.value, tracked, next)
    tracked = next
  })

  function set(text: string, next: ReferenceToken[], caret?: number) {
    tracked = text
    input.value = text
    tokens.value = next
    if (caret === undefined) return
    void nextTick(() => {
      const el = textarea.value
      if (!el) return
      el.focus()
      el.setSelectionRange(caret, caret)
      updateTrigger()
    })
  }

  function clear() {
    trigger.value = null
    set('', [])
  }

  function updateTrigger() {
    const el = textarea.value
    if (!enabled() || !el || composing.value || el.selectionStart !== el.selectionEnd || insideToken(tokens.value, el.selectionStart)) {
      if (!composing.value) trigger.value = null
      return
    }
    const next = activeTrigger(input.value, el.selectionStart)
    if (!next || next.start !== dismissedAt) dismissedAt = -1
    trigger.value = next && next.start !== dismissedAt ? next : null
    if (!trigger.value) pickerTab.value = 'all'
  }

  function addToken(start: number, end: number, ref: MessageReference) {
    if (full.value) return
    const res = insertToken(input.value, start, end, ref, referenceLabel(ref, defaultRepo.value))
    const kept = applyEdit(tokens.value, input.value, res.text)
    set(res.text, [...kept, res.token].sort((a, b) => a.start - b.start), res.caret)
  }

  function pick(ref: MessageReference) {
    const at = trigger.value
    if (!at) return
    trigger.value = null
    addToken(at.start, at.start + 1 + at.query.length, ref)
  }

  function drop(index: number) {
    const res = removeToken(input.value, tokens.value, index)
    set(res.text, res.tokens)
  }

  /** Menu entries open the picker on a tab by typing the @ for the user. */
  function open(tab: ReferenceTab) {
    const el = textarea.value
    const caret = el ? el.selectionEnd : input.value.length
    const text = input.value
    const lead = caret > 0 && !/\s/.test(text[caret - 1]!) ? ' ' : ''
    const next = text.slice(0, caret) + lead + '@' + text.slice(caret)
    pickerTab.value = tab
    set(next, applyEdit(tokens.value, text, next), caret + lead.length + 1)
  }

  /** Call after attachment handling; a pasted GitHub link becomes a reference. */
  function onPaste(e: ClipboardEvent) {
    if (e.defaultPrevented || !enabled()) return
    const parsed = parseGitHubReference(e.clipboardData?.getData('text/plain') || '')
    const el = textarea.value
    if (!parsed || !el || full.value || insideToken(tokens.value, el.selectionStart)) return
    e.preventDefault()
    addToken(el.selectionStart, el.selectionEnd, { ...parsed, title: '' })
  }

  /** True when the key was handled here and the caller must ignore it. */
  function onKeydown(e: KeyboardEvent): boolean {
    if (composing.value || e.isComposing || e.keyCode === 229) return false
    const p = picker.value
    if (trigger.value && p) {
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') p.move(e.key === 'ArrowDown' ? 1 : -1)
      else if (e.key === 'Tab') p.cycleTab(e.shiftKey ? -1 : 1)
      else if (e.key === 'Escape') {
        dismissedAt = trigger.value.start
        trigger.value = null
      } else if (!(e.key === 'Enter' && !e.shiftKey && p.confirm())) return backspace(e)
      e.preventDefault()
      return true
    }
    return backspace(e)
  }

  function backspace(e: KeyboardEvent): boolean {
    const el = textarea.value
    if (e.key !== 'Backspace' || !el || el.selectionStart !== el.selectionEnd) return false
    const index = tokens.value.findIndex((token) => token.end === el.selectionStart)
    if (index < 0) return false
    e.preventDefault()
    const start = tokens.value[index]!.start
    const rest = tokens.value.filter((_, i) => i !== index)
    const text = input.value.slice(0, start) + input.value.slice(el.selectionStart)
    set(text, applyEdit(rest, input.value, text), start)
    return true
  }

  /** Snapshot for restoring the composer when a send fails. */
  function snapshot() {
    const text = input.value
    const saved = tokens.value.slice()
    return () => set(text, saved)
  }

  return { tokens, trigger, pickerTab, picker, refs, targets, full, set, clear, updateTrigger, pick, drop, open, onPaste, onKeydown, snapshot }
}
