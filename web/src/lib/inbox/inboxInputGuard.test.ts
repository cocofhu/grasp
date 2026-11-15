// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import { createInboxInputGuard, TYPING_IDLE_MS } from './inboxInputGuard'

afterEach(() => {
  document.body.innerHTML = ''
  vi.useRealTimers()
})

function field(testId: string, value = ''): HTMLTextAreaElement {
  const el = document.createElement('textarea')
  el.setAttribute('data-testid', testId)
  el.value = value
  document.body.appendChild(el)
  return el
}

describe('createInboxInputGuard', () => {
  it('holds while the focused field has a draft or was typed into recently', async () => {
    vi.useFakeTimers()
    const guard = createInboxInputGuard()
    const el = field('composer', 'draft')
    el.focus()
    guard.attach()
    expect(guard.focusedFieldHasDraft.value).toBe(true)

    el.value = ''
    el.dispatchEvent(new Event('input', { bubbles: true }))
    expect(guard.focusedFieldHasDraft.value).toBe(false)
    expect(guard.typingRecently.value).toBe(true)
    vi.advanceTimersByTime(TYPING_IDLE_MS - 1)
    expect(guard.typingRecently.value).toBe(true)
    vi.advanceTimersByTime(1)
    expect(guard.typingRecently.value).toBe(false)

    el.dispatchEvent(new Event('input', { bubbles: true }))
    el.blur()
    document.dispatchEvent(new FocusEvent('focusout'))
    await nextTick()
    expect(guard.typingRecently.value).toBe(false)
    guard.detach()
  })

  it('stops listening and drops the typing hold on detach', () => {
    const guard = createInboxInputGuard()
    const el = field('composer')
    el.focus()
    guard.attach()
    el.dispatchEvent(new Event('input', { bubbles: true }))
    expect(guard.typingRecently.value).toBe(true)
    guard.detach()
    expect(guard.typingRecently.value).toBe(false)
    el.value = 'x'
    el.dispatchEvent(new Event('input', { bubbles: true }))
    expect(guard.typingRecently.value).toBe(false)
    expect(guard.focusedFieldHasDraft.value).toBe(false)
  })

  it('ignores non-text controls', () => {
    const guard = createInboxInputGuard()
    const box = document.createElement('input')
    box.type = 'checkbox'
    document.body.appendChild(box)
    box.focus()
    guard.attach()
    box.dispatchEvent(new Event('input', { bubbles: true }))
    expect(guard.typingRecently.value).toBe(false)
    expect(guard.captureFocus()).toBeNull()
    guard.detach()
  })

  it('moves focus and selection to a re-rendered field with the same test id', () => {
    const guard = createInboxInputGuard()
    const old = field('composer', 'hello')
    old.focus()
    old.setSelectionRange(2, 4)
    const restore = guard.captureFocus()!
    old.remove()
    const replacement = field('composer', 'hello')
    restore()
    expect(document.activeElement).toBe(replacement)
    expect([replacement.selectionStart, replacement.selectionEnd]).toEqual([2, 4])
  })

  it('leaves focus alone when the field survived or the user moved elsewhere', () => {
    const guard = createInboxInputGuard()
    const kept = field('composer', 'hi')
    kept.focus()
    guard.captureFocus()!()
    expect(document.activeElement).toBe(kept)

    const restore = guard.captureFocus()!
    kept.remove()
    field('composer', 'hi')
    const other = field('search')
    other.focus()
    restore()
    expect(document.activeElement).toBe(other)
  })
})
