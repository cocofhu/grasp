// @vitest-environment happy-dom
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { type DOMWrapper, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import HomeYumeCharacter from './HomeYumeCharacter.vue'

function stubReducedMotion(matches: boolean) {
  const listeners = new Set<() => void>()
  const media = {
    matches,
    media: '(prefers-reduced-motion: reduce)',
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn((_event: string, listener: () => void) => listeners.add(listener)),
    removeEventListener: vi.fn((_event: string, listener: () => void) => listeners.delete(listener)),
    dispatchEvent: vi.fn(),
  }
  vi.stubGlobal(
    'matchMedia',
    vi.fn(() => media),
  )
  return {
    media,
    listeners,
    change(value: boolean) {
      media.matches = value
      for (const listener of listeners) listener()
    },
  }
}

describe('HomeYumeCharacter', () => {
  beforeEach(() => {
    stubReducedMotion(false)
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('renders fixed-size black-and-white yume without demo caption', async () => {
    const wrapper = mount(HomeYumeCharacter, { attachTo: document.body })
    await flushPromises()
    const root = wrapper.get('[data-testid="home-yume"]')
    expect(root.attributes('role')).toBe('button')
    expect(wrapper.find('svg.cat--yume').exists()).toBe(true)
    expect(wrapper.find('.c-eyes').exists()).toBe(true)
    expect(wrapper.find('.c-tail').exists()).toBe(true)
    expect(wrapper.find('.c-star').exists()).toBe(false)
    expect(wrapper.html()).not.toMatch(/c-star|yume-c-pop/)
    // B&W tokens from INTERACTION.md
    expect(wrapper.html()).toMatch(/fill="#FFFFFF"|fill="#ffffff"/i)
    expect(wrapper.html()).toMatch(/#1a1a1a/)
    expect(wrapper.html()).toMatch(/#c8c8c8/)
    expect(wrapper.html()).not.toMatch(/#e9e3ff/)
    expect(wrapper.text()).not.toMatch(/FLOAT|BLINK|演示|demo/i)
    wrapper.unmount()
  })

  it('presses on pointerdown and springs on pointerup', async () => {
    const wrapper = mount(HomeYumeCharacter, { attachTo: document.body })
    await flushPromises()
    const root = wrapper.get('[data-testid="home-yume"]')
    await root.trigger('pointerdown', { pointerType: 'mouse', button: 0 })
    expect(root.classes()).toContain('pressed')
    await root.trigger('pointerup', { pointerType: 'mouse', button: 0 })
    expect(root.classes()).toContain('jump')
    expect(root.classes()).not.toContain('pressed')
    vi.advanceTimersByTime(360)
    await flushPromises()
    expect(root.classes()).not.toContain('jump')
    wrapper.unmount()
  })

  it('follows pointer with eyes', async () => {
    const wrapper = mount(HomeYumeCharacter, { attachTo: document.body })
    await flushPromises()
    const root = wrapper.get('[data-testid="home-yume"]').element as HTMLElement
    Object.defineProperty(root, 'getBoundingClientRect', {
      value: () => ({
        left: 100,
        top: 100,
        width: 148,
        height: 148,
        right: 248,
        bottom: 248,
        x: 100,
        y: 100,
        toJSON: () => ({}),
      }),
    })
    window.dispatchEvent(
      new PointerEvent('pointermove', { clientX: 174, clientY: 174 }),
    )
    await flushPromises()
    const eyes = wrapper.find('.c-eyes').element as SVGGElement
    expect(eyes.style.transform).toMatch(/translate\(/)
    wrapper.unmount()
  })

  it('keeps a hidden 240×200 crop at rest and shows overflow while jumping or pressed', () => {
    const src = readFileSync(join(dirname(fileURLToPath(import.meta.url)), 'HomeYumeCharacter.vue'), 'utf8')
    const root = src.match(/\.home-yume\s*\{([^}]+)\}/)?.[1] ?? ''
    expect(root).toMatch(/width:\s*var\(--yume-w,\s*240px\)/)
    expect(root).toMatch(/height:\s*var\(--yume-h,\s*200px\)/)
    expect(root).toMatch(/overflow:\s*hidden/)
    const shown = src.match(/\.home-yume\.jump\s*,\s*\.home-yume\.pressed\s*\{([^}]+)\}/)?.[1] ?? ''
    expect(shown).toMatch(/overflow:\s*visible/)
    expect(src).toMatch(/scale\(0\.92,\s*1\.08\)\s*translateY\(-14%\)/)
    expect(src).toMatch(/translateY\(-9%\)/)
  })

  it('keeps the dark drop-shadow and uses a lighter one in light theme', () => {
    const src = readFileSync(join(dirname(fileURLToPath(import.meta.url)), 'HomeYumeCharacter.vue'), 'utf8')
    expect(src).toMatch(/filter:\s*drop-shadow\(0 12px 28px rgba\(0,\s*0,\s*0,\s*0\.45\)\)/)
    const light = src.match(/html\.light\s+\.home-yume\s*\{([^}]+)\}/)?.[1] ?? ''
    expect(light).toMatch(/drop-shadow/)
    expect(light).not.toMatch(/0\.45/)
    expect(light).not.toMatch(/28px/)
    expect(src).not.toMatch(/c-star/)
    expect(src).not.toMatch(/yume-c-pop/)
  })

  it('disables motion under prefers-reduced-motion', async () => {
    stubReducedMotion(true)
    const wrapper = mount(HomeYumeCharacter, { attachTo: document.body })
    await flushPromises()
    const root = wrapper.get('[data-testid="home-yume"]')
    expect(root.classes()).toContain('home-yume--static')
    await root.trigger('pointerdown', { pointerType: 'mouse', button: 0 })
    expect(root.classes()).not.toContain('pressed')
    await root.trigger('pointerup', { pointerType: 'mouse', button: 0 })
    expect(root.classes()).not.toContain('jump')
    wrapper.unmount()
  })

  it('stops an active interaction when motion preference changes and cleans up on unmount', async () => {
    const motion = stubReducedMotion(false)
    const removed = vi.spyOn(window, 'removeEventListener')
    const wrapper = mount(HomeYumeCharacter, { attachTo: document.body })
    await flushPromises()
    const root = wrapper.get('[data-testid="home-yume"]')
    const eyes = wrapper.get('.c-eyes').element as SVGGElement
    const baseline = vi.getTimerCount()
    eyes.style.transform = 'translate(4px, 2px)'
    await root.trigger('pointerdown', { pointerType: 'mouse', button: 0 })
    await root.trigger('pointerup', { pointerType: 'mouse', button: 0 })
    expect(root.classes()).toContain('jump')
    expect(vi.getTimerCount()).toBe(baseline + 1)

    motion.change(true)
    await flushPromises()
    expect(root.classes()).toContain('home-yume--static')
    expect(root.classes()).not.toContain('jump')
    expect(root.classes()).not.toContain('pressed')
    expect(eyes.style.transform).toBe('')
    expect(vi.getTimerCount()).toBe(0)

    motion.change(false)
    await flushPromises()
    await root.trigger('keydown', { key: 'Enter' })
    expect(root.classes()).toContain('jump')
    wrapper.unmount()
    expect(vi.getTimerCount()).toBe(0)
    expect(motion.listeners.size).toBe(0)
    expect(removed).toHaveBeenCalledWith('pointermove', expect.any(Function))
  })

  it('ignores non-primary clicks and keeps keyboard and assistive activation available', async () => {
    const wrapper = mount(HomeYumeCharacter, { attachTo: document.body })
    await flushPromises()
    const root = wrapper.get('[data-testid="home-yume"]')
    await root.trigger('pointerdown', { pointerType: 'mouse', button: 2 })
    await root.trigger('pointerup', { pointerType: 'mouse', button: 2 })
    expect(root.classes()).not.toContain('pressed')
    expect(root.classes()).not.toContain('jump')
    for (const key of ['Enter', ' ']) {
      await root.trigger('keydown', { key })
      expect(root.classes()).toContain('jump')
      // Wait out the tap-combo window so each activation stays a single jump.
      await vi.advanceTimersByTimeAsync(800)
    }
    await root.trigger('click', { detail: 0 })
    expect(root.classes()).toContain('jump')
    wrapper.unmount()
    expect(vi.getTimerCount()).toBe(0)
  })

  const idleClasses = (classes: string[]) => classes.filter((c) => c.startsWith('idle-'))

  it('plays a random idle action every 6–10s without repeating the previous one', async () => {
    vi.spyOn(Math, 'random').mockReturnValue(0)
    const wrapper = mount(HomeYumeCharacter, { attachTo: document.body })
    await flushPromises()
    const root = wrapper.get('[data-testid="home-yume"]')
    await vi.advanceTimersByTimeAsync(5999)
    expect(idleClasses(root.classes())).toEqual([])
    await vi.advanceTimersByTimeAsync(1)
    expect(idleClasses(root.classes())).toEqual(['idle-look'])
    await vi.advanceTimersByTimeAsync(1600)
    expect(idleClasses(root.classes())).toEqual([])
    await vi.advanceTimersByTimeAsync(6000 - 1600)
    expect(idleClasses(root.classes())).toEqual(['idle-ear'])
    wrapper.unmount()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('skips idle actions while hovered or while the composer is focused', async () => {
    const wrapper = mount(HomeYumeCharacter, { attachTo: document.body })
    await flushPromises()
    const root = wrapper.get('[data-testid="home-yume"]')
    await root.trigger('pointerenter')
    await vi.advanceTimersByTimeAsync(20_000)
    expect(idleClasses(root.classes())).toEqual([])
    await root.trigger('pointerleave')

    await wrapper.setProps({ attentive: true })
    expect(root.classes()).toContain('attentive')
    await vi.advanceTimersByTimeAsync(20_000)
    expect(idleClasses(root.classes())).toEqual([])
    wrapper.unmount()
  })

  it('ignores pointer tracking while attentive and resumes it after blur', async () => {
    const wrapper = mount(HomeYumeCharacter, { attachTo: document.body, props: { attentive: true } })
    await flushPromises()
    const eyes = wrapper.get('.c-eyes').element as SVGGElement
    window.dispatchEvent(new PointerEvent('pointermove', { clientX: 999, clientY: 999 }))
    expect(eyes.style.transform).toBe('')
    await wrapper.setProps({ attentive: false })
    expect(wrapper.get('[data-testid="home-yume"]').classes()).not.toContain('attentive')
    wrapper.unmount()
  })

  it('dozes after 45s without activity and wakes with a shake on input', async () => {
    const wrapper = mount(HomeYumeCharacter, { attachTo: document.body })
    await flushPromises()
    const root = wrapper.get('[data-testid="home-yume"]')
    await vi.advanceTimersByTimeAsync(44_000)
    expect(root.classes()).not.toContain('sleeping')
    await vi.advanceTimersByTimeAsync(1000)
    expect(root.classes()).toContain('sleeping')
    expect(wrapper.findAll('.c-zz')).toHaveLength(2)
    await vi.advanceTimersByTimeAsync(30_000)
    expect(idleClasses(root.classes())).toEqual([])

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'a' }))
    await flushPromises()
    expect(root.classes()).not.toContain('sleeping')
    expect(root.classes()).toContain('wake')
    expect(wrapper.findAll('.c-zz')).toHaveLength(0)
    await vi.advanceTimersByTimeAsync(400)
    expect(root.classes()).not.toContain('wake')
    wrapper.unmount()
  })

  it('postpones dozing on activity and wakes when the composer is focused', async () => {
    const wrapper = mount(HomeYumeCharacter, { attachTo: document.body })
    await flushPromises()
    const root = wrapper.get('[data-testid="home-yume"]')
    await vi.advanceTimersByTimeAsync(30_000)
    window.dispatchEvent(new PointerEvent('pointermove', { clientX: 1, clientY: 1 }))
    await vi.advanceTimersByTimeAsync(30_000)
    expect(root.classes()).not.toContain('sleeping')
    await vi.advanceTimersByTimeAsync(15_000)
    expect(root.classes()).toContain('sleeping')

    await wrapper.setProps({ attentive: true })
    expect(root.classes()).not.toContain('sleeping')
    expect(root.classes()).toContain('wake')
    await vi.advanceTimersByTimeAsync(120_000)
    expect(root.classes()).not.toContain('sleeping')
    wrapper.unmount()
  })

  it('pauses idle and dozing while the page is hidden', async () => {
    let hidden = false
    vi.spyOn(document, 'hidden', 'get').mockImplementation(() => hidden)
    const wrapper = mount(HomeYumeCharacter, { attachTo: document.body })
    await flushPromises()
    const root = wrapper.get('[data-testid="home-yume"]')
    hidden = true
    document.dispatchEvent(new Event('visibilitychange'))
    expect(vi.getTimerCount()).toBe(0)
    await vi.advanceTimersByTimeAsync(60_000)
    expect(root.classes()).not.toContain('sleeping')
    expect(idleClasses(root.classes())).toEqual([])

    hidden = false
    document.dispatchEvent(new Event('visibilitychange'))
    document.dispatchEvent(new Event('visibilitychange'))
    expect(vi.getTimerCount()).toBe(2)
    wrapper.unmount()
  })

  async function tap(root: DOMWrapper<Element>) {
    await root.trigger('pointerdown', { pointerType: 'mouse', button: 0 })
    await root.trigger('pointerup', { pointerType: 'mouse', button: 0 })
  }

  it('escalates rapid taps: jump, flip, then hearts that clean themselves up', async () => {
    const wrapper = mount(HomeYumeCharacter, { attachTo: document.body })
    await flushPromises()
    const root = wrapper.get('[data-testid="home-yume"]')
    await tap(root)
    expect(root.classes()).toContain('jump')
    expect(root.classes()).not.toContain('flip')
    await vi.advanceTimersByTimeAsync(400)
    await tap(root)
    expect(root.classes()).toContain('flip')
    await vi.advanceTimersByTimeAsync(400)
    await tap(root)
    expect(root.classes()).toContain('jump')
    expect(root.classes()).toContain('hearts')
    const hearts = wrapper.findAll('.c-heart')
    expect(hearts.length).toBeGreaterThanOrEqual(3)
    expect(hearts.length).toBeLessThanOrEqual(5)
    expect(hearts[0].html()).toMatch(/fill="#c8c8c8"/)
    expect(hearts[0].html()).toMatch(/stroke="#1a1a1a"/)

    await vi.advanceTimersByTimeAsync(1400)
    expect(wrapper.findAll('.c-heart')).toHaveLength(0)
    expect(root.classes()).not.toContain('hearts')
    expect(root.classes()).not.toContain('flip')

    await tap(root)
    expect(root.classes()).toContain('jump')
    expect(root.classes()).not.toContain('flip')
    wrapper.unmount()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('counts keyboard and assistive activation toward the combo but not right clicks', async () => {
    const wrapper = mount(HomeYumeCharacter, { attachTo: document.body })
    await flushPromises()
    const root = wrapper.get('[data-testid="home-yume"]')
    await root.trigger('keydown', { key: 'Enter' })
    await root.trigger('pointerdown', { pointerType: 'mouse', button: 2 })
    await root.trigger('pointerup', { pointerType: 'mouse', button: 2 })
    expect(root.classes()).not.toContain('flip')
    await root.trigger('click', { detail: 0 })
    expect(root.classes()).toContain('flip')
    wrapper.unmount()
  })

  it('nods on typing with a throttle and cheers with an open mouth on submit', async () => {
    const wrapper = mount(HomeYumeCharacter, { attachTo: document.body, props: { attentive: true } })
    await flushPromises()
    const root = wrapper.get('[data-testid="home-yume"]')
    await wrapper.setProps({ typingTick: 1 })
    expect(root.classes()).toContain('nod')
    await wrapper.setProps({ typingTick: 2 })
    await vi.advanceTimersByTimeAsync(220)
    expect(root.classes()).not.toContain('nod')
    await wrapper.setProps({ typingTick: 3 })
    expect(root.classes()).not.toContain('nod')
    await vi.advanceTimersByTimeAsync(150)
    await wrapper.setProps({ typingTick: 4 })
    expect(root.classes()).toContain('nod')

    expect(wrapper.find('.c-mouth').exists()).toBe(true)
    expect(wrapper.find('.c-mouth-open').exists()).toBe(true)
    await wrapper.setProps({ cheerTick: 1 })
    expect(root.classes()).toContain('cheer')
    await vi.advanceTimersByTimeAsync(650)
    expect(root.classes()).not.toContain('cheer')
    wrapper.unmount()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('runs no new motion or timers under prefers-reduced-motion', async () => {
    stubReducedMotion(true)
    const wrapper = mount(HomeYumeCharacter, { attachTo: document.body })
    await flushPromises()
    const root = wrapper.get('[data-testid="home-yume"]')
    expect(vi.getTimerCount()).toBe(0)
    await wrapper.setProps({ attentive: true, typingTick: 1, cheerTick: 1 })
    await root.trigger('keydown', { key: 'Enter' })
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'a' }))
    await vi.advanceTimersByTimeAsync(60_000)
    expect(vi.getTimerCount()).toBe(0)
    for (const cls of ['attentive', 'nod', 'cheer', 'jump', 'flip', 'sleeping', 'wake']) {
      expect(root.classes()).not.toContain(cls)
    }
    expect(idleClasses(root.classes())).toEqual([])
    wrapper.unmount()
  })

  it('resets dozing, hearts and combos immediately when reduced motion turns on', async () => {
    const motion = stubReducedMotion(false)
    const wrapper = mount(HomeYumeCharacter, { attachTo: document.body })
    await flushPromises()
    const root = wrapper.get('[data-testid="home-yume"]')
    await tap(root)
    await tap(root)
    await tap(root)
    expect(wrapper.findAll('.c-heart').length).toBeGreaterThan(0)
    motion.change(true)
    await flushPromises()
    expect(wrapper.findAll('.c-heart')).toHaveLength(0)
    expect(root.classes()).not.toContain('flip')
    expect(vi.getTimerCount()).toBe(0)

    motion.change(false)
    await vi.advanceTimersByTimeAsync(45_000)
    expect(root.classes()).toContain('sleeping')
    motion.change(true)
    await flushPromises()
    expect(root.classes()).not.toContain('sleeping')
    expect(vi.getTimerCount()).toBe(0)

    motion.change(false)
    await flushPromises()
    await tap(root)
    expect(root.classes()).toContain('jump')
    expect(root.classes()).not.toContain('flip')
    wrapper.unmount()
  })

  it('removes every global listener on unmount', async () => {
    const removedWindow = vi.spyOn(window, 'removeEventListener')
    const removedDocument = vi.spyOn(document, 'removeEventListener')
    const wrapper = mount(HomeYumeCharacter, { attachTo: document.body })
    await flushPromises()
    wrapper.unmount()
    for (const type of ['pointermove', 'pointerdown', 'keydown']) {
      expect(removedWindow).toHaveBeenCalledWith(type, expect.any(Function))
    }
    expect(removedDocument).toHaveBeenCalledWith('visibilitychange', expect.any(Function))
    expect(vi.getTimerCount()).toBe(0)
  })

  it('animates with separate layers and transform/opacity only', () => {
    const src = readFileSync(join(dirname(fileURLToPath(import.meta.url)), 'HomeYumeCharacter.vue'), 'utf8')
    for (const layer of ['class="c-act"', 'class="k c-ear-l"', 'class="k c-ear-r"', 'class="k c-mouth-open"']) {
      expect(src).toContain(layer)
    }
    const shown = src.match(/\.home-yume\.flip\s*,\s*\.home-yume\.hearts\s*,\s*\.home-yume\.cheer\s*\{([^}]+)\}/)?.[1] ?? ''
    expect(shown).toMatch(/overflow:\s*visible/)
    expect(src).not.toMatch(/\bd:\s*path\(/)
    const keyframes = [...src.matchAll(/@keyframes[^{]+\{((?:[^{}]*\{[^}]*\})+)\s*\}/g)].map((m) => m[1]).join('\n')
    const props = [...keyframes.matchAll(/([a-z-]+)\s*:/g)].map((m) => m[1])
    expect(new Set(props)).toEqual(new Set(['transform', 'opacity']))
  })
})
