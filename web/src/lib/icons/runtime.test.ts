// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest'
import definitions from './definitions.json'
import { hydrateRuntimeIcons, installRuntimeIconMotion, runtimeIcon } from './runtime'

const cleanups: Array<() => void> = []
function mount(markup: string) {
  const host = document.createElement('div')
  document.body.appendChild(host)
  const shadow = host.attachShadow({ mode: 'open' })
  shadow.innerHTML = markup
  cleanups.push(() => host.remove(), installRuntimeIconMotion(shadow))
  return shadow
}
function over(el: Element, relatedTarget: EventTarget | null = null) { el.dispatchEvent(new PointerEvent('pointerover', { bubbles: true, pointerType: 'mouse', relatedTarget })) }
function out(el: Element, relatedTarget: EventTarget | null = null) { el.dispatchEvent(new PointerEvent('pointerout', { bubbles: true, pointerType: 'mouse', relatedTarget })) }

afterEach(() => {
  cleanups.splice(0).reverse().forEach((cleanup) => cleanup())
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('native icon runtime', () => {
  it('shares catalog paths, leaves completed checks still, and hydrates templates', () => {
    const template = document.createElement('template')
    template.innerHTML = '<span class="cc-theme-moon" data-runtime-icon="moon" data-icon-size="18"></span>'
    hydrateRuntimeIcons(template.content)
    const moon = template.content.querySelector('svg')!
    expect(runtimeIcon('moon')).toContain(definitions.moon.body)
    expect(moon.querySelector('path')).not.toBeNull()
    expect(moon.classList.contains('cc-theme-moon')).toBe(true)
    expect(moon.getAttribute('width')).toBe('18')
    expect(runtimeIcon('check-draw')).not.toContain('<circle')
    expect(runtimeIcon('check-draw')).not.toContain('data-icon-motion')
    expect(runtimeIcon('check-draw', 16, { enter: true })).not.toContain('data-icon-motion')
    expect(runtimeIcon('chat-stop')).toContain('stroke="currentColor"')
    expect(runtimeIcon('grip')).toContain('stroke="none"')
    expect(runtimeIcon('arc-spinner', 16, { busy: true })).toContain('data-icon-motion="busy"')
  })

  it('plays once on the nearest owner, remains still while held, and replays after reentry', () => {
    vi.useFakeTimers()
    const root = mount(`<div role="button" id="outer">${runtimeIcon('info')}<button id="inner">${runtimeIcon('close')}</button></div>`)
    const outer = root.querySelector('#outer')!
    const inner = root.querySelector('#inner')!
    const [outerIcon, innerIcon] = root.querySelectorAll('svg')
    over(inner)
    expect(innerIcon.getAttribute('data-icon-motion')).toBe('once')
    expect(outerIcon.hasAttribute('data-icon-motion')).toBe(false)
    vi.advanceTimersByTime(definitions.close.duration + 50)
    expect(innerIcon.hasAttribute('data-icon-motion')).toBe(false)
    over(inner)
    expect(innerIcon.hasAttribute('data-icon-motion')).toBe(false)
    out(inner)
    over(inner)
    expect(innerIcon.getAttribute('data-icon-motion')).toBe('once')
    out(inner)
    over(outer)
    expect(outerIcon.getAttribute('data-icon-motion')).toBe('once')
    expect(innerIcon.hasAttribute('data-icon-motion')).toBe(false)
  })

  it('hands pointer ownership between nested hosts in both directions', () => {
    const root = mount(`<div role="button" id="outer">${runtimeIcon('info')}<button id="inner">${runtimeIcon('close')}</button></div>`)
    const outer = root.querySelector('#outer')!
    const inner = root.querySelector('#inner')!
    const [outerIcon, innerIcon] = root.querySelectorAll('svg')
    over(outer)
    expect(outerIcon.getAttribute('data-icon-motion')).toBe('once')
    out(outer, inner)
    over(inner, outer)
    expect(outerIcon.hasAttribute('data-icon-motion')).toBe(false)
    expect(innerIcon.getAttribute('data-icon-motion')).toBe('once')
    // An unrelated late outer leave must not cancel the inner owner's glyph.
    out(outer)
    expect(innerIcon.getAttribute('data-icon-motion')).toBe('once')
    out(inner, outer)
    over(outer, inner)
    expect(innerIcon.hasAttribute('data-icon-motion')).toBe(false)
    expect(outerIcon.getAttribute('data-icon-motion')).toBe('once')
  })

  it('uses the safe fallback for inherited object property names', () => {
    for (const name of ['toString', 'constructor', '__proto__']) {
      expect(runtimeIcon(name)).toBe(runtimeIcon('help'))
      expect(runtimeIcon(name)).not.toContain('undefined')
    }
  })

  it('keeps disabled, busy, reduced-motion and dragged glyphs stable', () => {
    const root = mount(`<button disabled>${runtimeIcon('close')}</button><button aria-busy="true">${runtimeIcon('chat-send')}</button><button>${runtimeIcon('arc-spinner', 16, { busy: true })}</button><span data-icon-host>${runtimeIcon('grip')}</span>`)
    const buttons = root.querySelectorAll('button')
    buttons.forEach((button) => over(button))
    expect(buttons[0].querySelector('svg')!.hasAttribute('data-icon-motion')).toBe(false)
    expect(buttons[1].querySelector('svg')!.hasAttribute('data-icon-motion')).toBe(false)
    expect(buttons[2].querySelector('svg')!.getAttribute('data-icon-motion')).toBe('busy')
    out(buttons[2])
    expect(buttons[2].querySelector('svg')!.getAttribute('data-icon-motion')).toBe('busy')
    const grip = root.querySelector('[data-icon-host]')!
    over(grip)
    expect(grip.querySelector('svg')!.getAttribute('data-icon-motion')).toBe('once')
    grip.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true }))
    expect(grip.querySelector('svg')!.hasAttribute('data-icon-motion')).toBe(false)
    vi.stubGlobal('matchMedia', () => ({ matches: true, addEventListener() {}, removeEventListener() {} }))
    const reduced = mount(`<button>${runtimeIcon('check')}</button>`)
    over(reduced.querySelector('button')!)
    expect(reduced.querySelector('svg')!.hasAttribute('data-icon-motion')).toBe(false)
  })
})
