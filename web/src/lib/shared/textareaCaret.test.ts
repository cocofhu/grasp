// @vitest-environment happy-dom
import { afterEach, describe, expect, it } from 'vitest'
import { textareaCaretRect } from './textareaCaret'

afterEach(() => {
  document.body.innerHTML = ''
})

describe('textareaCaretRect', () => {
  it('offsets by the textarea scroll, uses its line height and leaves no mirror behind', () => {
    const el = document.createElement('textarea')
    el.style.lineHeight = '22px'
    el.value = 'hello @world'
    document.body.appendChild(el)
    el.scrollTop = 0
    const before = document.body.children.length
    const rect = textareaCaretRect(el, 6)
    expect(rect.height).toBe(22)
    expect(Number.isFinite(rect.left)).toBe(true)
    expect(Number.isFinite(rect.top)).toBe(true)
    expect(document.body.children.length).toBe(before)
  })

  it('falls back to the font size when the line height is not a length', () => {
    const el = document.createElement('textarea')
    el.style.lineHeight = 'normal'
    el.style.fontSize = '10px'
    document.body.appendChild(el)
    expect(textareaCaretRect(el, 0).height).toBe(14)
  })
})
