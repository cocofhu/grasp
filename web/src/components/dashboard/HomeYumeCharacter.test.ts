// @vitest-environment happy-dom
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import HomeYumeCharacter from './HomeYumeCharacter.vue'

function stubReducedMotion(matches: boolean) {
  vi.stubGlobal(
    'matchMedia',
    vi.fn((query: string) => ({
      matches: query.includes('prefers-reduced-motion') ? matches : false,
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  )
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
    await root.trigger('pointerup', { pointerType: 'mouse', button: 0 })
    expect(root.classes()).not.toContain('jump')
    wrapper.unmount()
  })
})
