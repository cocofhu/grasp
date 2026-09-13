// @vitest-environment happy-dom
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { createApp, nextTick, ref } from 'vue'
import { isHoverInkBlocked, vHoverInk } from './hoverInkDirective'

function mountButton(opts: { enabled?: boolean; disabled?: boolean; busy?: boolean } = {}) {
  const host = document.createElement('div')
  document.body.appendChild(host)
  const enabled = ref(opts.enabled !== false)
  let template = `<button type="button" v-hover-ink="{ enabled }">Go</button>`
  if (opts.disabled) {
    template = `<button type="button" v-hover-ink="{ enabled }" disabled>Go</button>`
  } else if (opts.busy) {
    template = `<button type="button" v-hover-ink="{ enabled }" aria-busy="true">Go</button>`
  }
  const app = createApp({
    setup() {
      return { enabled }
    },
    template,
  })
  app.directive('hover-ink', vHoverInk)
  app.mount(host)
  const btn = host.querySelector('button') as HTMLButtonElement
  return { host, app, btn, enabled }
}

describe('vHoverInk directive (plan g1.2 / g1.3)', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'matchMedia',
      vi.fn((query: string) => ({
        matches: query.includes('hover: hover') && query.includes('pointer: fine'),
        media: query,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
      })),
    )
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    document.body.innerHTML = ''
  })

  it('injects ink layer under content and expands from landing point', async () => {
    const { host, app, btn } = mountButton()
    await nextTick()
    const ink = btn.querySelector(':scope > .hover-ink') as HTMLElement
    expect(ink).toBeTruthy()
    expect(btn.classList.contains('hover-ink-host')).toBe(true)
    // review v1 / plan g1.2: stylesheet puts ink at z-index:-1 under isolation
    const { readFileSync } = await import('node:fs')
    const { dirname, join } = await import('node:path')
    const { fileURLToPath } = await import('node:url')
    const css = readFileSync(
      join(dirname(fileURLToPath(import.meta.url)), '../../styles/global.css'),
      'utf8',
    )
    expect(css).toMatch(/\.hover-ink-host\s*>\s*\.hover-ink\s*\{[^}]*z-index:\s*-1/s)
    // plan g1.1: center with translate, never negative-margin layout box
    expect(css).toMatch(
      /\.hover-ink-host\s*>\s*\.hover-ink\s*\{[^}]*transform:\s*translate\(-50%,\s*-50%\)\s*scale\(0\)/s,
    )
    expect(css).not.toMatch(/margin:\s*calc\(\s*var\(--ink-d/)

    Object.defineProperty(btn, 'getBoundingClientRect', {
      value: () => ({
        left: 10,
        top: 20,
        width: 100,
        height: 40,
        right: 110,
        bottom: 60,
        x: 10,
        y: 20,
        toJSON: () => ({}),
      }),
    })

    btn.dispatchEvent(
      new PointerEvent('pointerenter', { clientX: 30, clientY: 30, pointerType: 'mouse', bubbles: true }),
    )
    expect(btn.classList.contains('is-hover-ink')).toBe(true)
    expect(btn.style.getPropertyValue('--ink-x')).toBe('20px')
    expect(btn.style.getPropertyValue('--ink-y')).toBe('10px')
    expect(btn.style.getPropertyValue('--ink-d')).toBe(`${Math.hypot(80, 30) * 2}px`)
    // Bare text node remains in the host; ink does not remove/replace content.
    expect(btn.textContent).toContain('Go')

    btn.dispatchEvent(new PointerEvent('pointerleave', { bubbles: true }))
    expect(btn.classList.contains('is-hover-ink')).toBe(false)
    expect(btn.classList.contains('is-leave-ink')).toBe(true)

    app.unmount()
    host.remove()
  })

  it('collapses --ink-d after leave so the layout box cannot stick (plan g1.2)', async () => {
    vi.useFakeTimers()
    const { host, app, btn } = mountButton()
    await nextTick()
    Object.defineProperty(btn, 'getBoundingClientRect', {
      value: () => ({
        left: 0,
        top: 0,
        width: 100,
        height: 40,
        right: 100,
        bottom: 40,
        x: 0,
        y: 0,
        toJSON: () => ({}),
      }),
    })

    btn.dispatchEvent(
      new PointerEvent('pointerenter', { clientX: 2, clientY: 20, pointerType: 'mouse', bubbles: true }),
    )
    const d = btn.style.getPropertyValue('--ink-d')
    expect(Number.parseFloat(d)).toBeGreaterThan(100)

    btn.dispatchEvent(new PointerEvent('pointerleave', { bubbles: true }))
    // Still large during leave animation…
    expect(btn.style.getPropertyValue('--ink-d')).toBe(d)
    // …then collapses after HOVER_INK_DURATION_MS (plan g1.2).
    const { HOVER_INK_DURATION_MS } = await import('./hoverInkGeometry')
    vi.advanceTimersByTime(HOVER_INK_DURATION_MS)
    expect(btn.style.getPropertyValue('--ink-d')).toBe('0px')

    app.unmount()
    host.remove()
    vi.useRealTimers()
  })

  it('retracts on window blur and visibility hidden (review v4 / edge_cases)', async () => {
    const { host, app, btn } = mountButton()
    await nextTick()
    Object.defineProperty(btn, 'getBoundingClientRect', {
      value: () => ({
        left: 0,
        top: 0,
        width: 80,
        height: 32,
        right: 80,
        bottom: 32,
        x: 0,
        y: 0,
        toJSON: () => ({}),
      }),
    })
    btn.dispatchEvent(
      new PointerEvent('pointerenter', { clientX: 10, clientY: 10, pointerType: 'mouse', bubbles: true }),
    )
    expect(btn.classList.contains('is-hover-ink')).toBe(true)

    window.dispatchEvent(new Event('blur'))
    expect(btn.classList.contains('is-hover-ink')).toBe(false)
    expect(btn.classList.contains('is-leave-ink')).toBe(true)

    btn.dispatchEvent(
      new PointerEvent('pointerenter', { clientX: 10, clientY: 10, pointerType: 'mouse', bubbles: true }),
    )
    expect(btn.classList.contains('is-hover-ink')).toBe(true)
    Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'hidden' })
    document.dispatchEvent(new Event('visibilitychange'))
    expect(btn.classList.contains('is-hover-ink')).toBe(false)

    app.unmount()
    host.remove()
  })

  it('ignores disabled, loading (aria-busy), and touch pointers', async () => {
    const disabled = mountButton({ disabled: true })
    await nextTick()
    disabled.btn.dispatchEvent(
      new PointerEvent('pointerenter', { clientX: 1, clientY: 1, pointerType: 'mouse', bubbles: true }),
    )
    expect(disabled.btn.classList.contains('is-hover-ink')).toBe(false)
    disabled.app.unmount()
    disabled.host.remove()

    const busy = mountButton({ busy: true })
    await nextTick()
    busy.btn.dispatchEvent(
      new PointerEvent('pointerenter', { clientX: 1, clientY: 1, pointerType: 'mouse', bubbles: true }),
    )
    expect(busy.btn.classList.contains('is-hover-ink')).toBe(false)
    busy.app.unmount()
    busy.host.remove()

    const touch = mountButton()
    await nextTick()
    touch.btn.dispatchEvent(
      new PointerEvent('pointerenter', { clientX: 1, clientY: 1, pointerType: 'touch', bubbles: true }),
    )
    expect(touch.btn.classList.contains('is-hover-ink')).toBe(false)
    touch.app.unmount()
    touch.host.remove()
  })

  it('isHoverInkBlocked reads disabled / aria-busy / binding', () => {
    const el = document.createElement('button')
    el.disabled = true
    expect(isHoverInkBlocked(el)).toBe(true)
    el.disabled = false
    el.setAttribute('aria-busy', 'true')
    expect(isHoverInkBlocked(el)).toBe(true)
    el.removeAttribute('aria-busy')
    expect(isHoverInkBlocked(el, { value: false } as never)).toBe(true)
    expect(isHoverInkBlocked(el, { value: { enabled: false } } as never)).toBe(true)
    expect(isHoverInkBlocked(el)).toBe(false)
  })

  it('clips ink without scroll-origin shift on left-edge landing (plan g1.1 / g1.2 / g2.2)', async () => {
    const { readFileSync } = await import('node:fs')
    const { dirname, join } = await import('node:path')
    const { fileURLToPath } = await import('node:url')
    const css = readFileSync(
      join(dirname(fileURLToPath(import.meta.url)), '../../styles/global.css'),
      'utf8',
    )
    // Host must use overflow:clip so a left-overflow ink circle cannot pull scrollLeft.
    expect(css).toMatch(/\.hover-ink-host\s*\{[^}]*overflow:\s*clip/s)
    expect(css).not.toMatch(/\.hover-ink-host\s*\{[^}]*overflow:\s*hidden/s)
    // plan g1.1: no negative-margin centering (that left-extends the layout box).
    expect(css).not.toMatch(/margin:\s*calc\(\s*var\(--ink-d/)
    expect(css).toMatch(/translate\(-50%,\s*-50%\)\s*scale\(0\)/)

    const { host, app, btn } = mountButton()
    await nextTick()
    Object.defineProperty(btn, 'getBoundingClientRect', {
      value: () => ({
        left: 40,
        top: 10,
        width: 120,
        height: 36,
        right: 160,
        bottom: 46,
        x: 40,
        y: 10,
        toJSON: () => ({}),
      }),
    })

    const beforeLeft = btn.getBoundingClientRect().left
    // Max left overflow: pointer near the host's left edge.
    btn.dispatchEvent(
      new PointerEvent('pointerenter', { clientX: 42, clientY: 28, pointerType: 'mouse', bubbles: true }),
    )
    expect(btn.classList.contains('is-hover-ink')).toBe(true)
    expect(btn.style.getPropertyValue('--ink-x')).toBe('2px')
    const d = Number.parseFloat(btn.style.getPropertyValue('--ink-d'))
    expect(d).toBeGreaterThan(120)
    expect(btn.scrollLeft).toBe(0)
    expect(btn.getBoundingClientRect().left).toBe(beforeLeft)

    app.unmount()
    host.remove()
  })
})
