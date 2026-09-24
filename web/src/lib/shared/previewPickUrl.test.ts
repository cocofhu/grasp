// @vitest-environment node
import { describe, expect, it } from 'vitest'
import {
  PICK_HTML_MAX,
  PICK_TEXT_MAX,
  previewPickAnnotation,
  previewPickLabel,
  previewPickPath,
} from './previewPickUrl'

describe('previewPickPath', () => {
  it('extracts pathname+search+hash', () => {
    expect(previewPickPath('http://127.0.0.1:5173/settings?tab=1#x')).toBe('/settings?tab=1#x')
  })

  it('returns / for root', () => {
    expect(previewPickPath('http://127.0.0.1:3000/')).toBe('/')
  })

  it('accepts already-relative paths', () => {
    expect(previewPickPath('/app/home')).toBe('/app/home')
  })

  it('returns empty for blank or opaque', () => {
    expect(previewPickPath('')).toBe('')
    expect(previewPickPath('not a url')).toBe('')
  })
})

describe('previewPickLabel', () => {
  it('joins path and selector', () => {
    expect(previewPickLabel('http://127.0.0.1:5173/settings', '#hero', 'div')).toBe(
      '/settings · #hero',
    )
  })

  it('falls back to selector when url missing', () => {
    expect(previewPickLabel('', '#hero', 'div')).toBe('#hero')
  })
})

describe('previewPickAnnotation', () => {
  it('carries the element context the agent needs', () => {
    expect(
      previewPickAnnotation({
        selector: 'main > h2',
        tagName: 'H2',
        text: ' Choose \n your plan ',
        outerHTML: '<h2>Choose your plan</h2>',
        url: ' http://10.0.0.5:5173/pricing ',
      }),
    ).toEqual({
      selector: 'main > h2',
      url: 'http://10.0.0.5:5173/pricing',
      label: '/pricing · main > h2',
      tagName: 'h2',
      text: 'Choose your plan',
      outerHTML: '<h2>Choose your plan</h2>',
    })
  })

  it('omits empty fields and clips long ones', () => {
    const ann = previewPickAnnotation({
      selector: '#big',
      tagName: 'div',
      text: 'x'.repeat(PICK_TEXT_MAX + 5),
      outerHTML: 'y'.repeat(PICK_HTML_MAX + 5),
    })
    expect(ann.url).toBeUndefined()
    expect(ann.text).toBe('x'.repeat(PICK_TEXT_MAX) + '…')
    expect(ann.outerHTML).toBe('y'.repeat(PICK_HTML_MAX) + '…')
    expect(previewPickAnnotation({ selector: '#a', tagName: '', outerHTML: '' })).toEqual({
      selector: '#a',
      label: '#a',
    })
  })
})
