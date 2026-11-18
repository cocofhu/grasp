// @vitest-environment happy-dom
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import FileTypeIcon from './FileTypeIcon.vue'

describe('FileTypeIcon', () => {
  it('draws a decorative folded page with the label in the tone colour', () => {
    const w = mount(FileTypeIcon, { props: { label: 'PDF', tone: 'pdf' } })
    const svg = w.get('svg')
    expect(svg.attributes('aria-hidden')).toBe('true')
    expect(svg.attributes('data-tone')).toBe('pdf')
    expect(w.findAll('path')).toHaveLength(2)
    expect(w.get('text').text()).toBe('PDF')
    const badge = w.get('[data-testid="file-type-icon-badge"]')
    expect(badge.attributes('fill')).toBe('#e5483d')
    expect(badge.attributes('width')).toBe('20')
  })

  it('widens the badge for four-character labels', () => {
    const w = mount(FileTypeIcon, { props: { label: 'JSON', tone: 'data' } })
    const badge = w.get('[data-testid="file-type-icon-badge"]')
    expect(badge.attributes('width')).toBe('24')
    expect(badge.attributes('fill')).toBe('#1f9d55')
    expect(w.get('text').attributes('x')).toBe('12')
  })
})
