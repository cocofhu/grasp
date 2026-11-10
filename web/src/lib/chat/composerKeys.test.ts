// @vitest-environment happy-dom
import { describe, expect, it } from 'vitest'
import { isComposingKey, isPlainEnter } from './composerKeys'

const key = (init: KeyboardEventInit & { keyCode?: number }) => {
  const event = new KeyboardEvent('keydown', init)
  if (init.keyCode !== undefined) Object.defineProperty(event, 'keyCode', { value: init.keyCode })
  return event
}

describe('composer keys', () => {
  it('treats composition and the Safari commit Enter as composing', () => {
    expect(isComposingKey(key({ key: 'Enter', isComposing: true }))).toBe(true)
    expect(isComposingKey(key({ key: 'Enter', keyCode: 229 }))).toBe(true)
    expect(isComposingKey(key({ key: 'Enter' }), true)).toBe(true)
    expect(isComposingKey(key({ key: 'Enter', keyCode: 13 }))).toBe(false)
  })

  it('sends only on a bare Enter outside composition', () => {
    expect(isPlainEnter(key({ key: 'Enter', keyCode: 13 }))).toBe(true)
    for (const init of [{ shiftKey: true }, { ctrlKey: true }, { altKey: true }, { metaKey: true }, { isComposing: true }, { keyCode: 229 }]) {
      expect(isPlainEnter(key({ key: 'Enter', ...init }))).toBe(false)
    }
    expect(isPlainEnter(key({ key: 'Enter' }), true)).toBe(false)
    expect(isPlainEnter(key({ key: 'a' }))).toBe(false)
  })
})
