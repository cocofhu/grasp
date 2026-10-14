// @vitest-environment happy-dom
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import BrandCharacterMark from './BrandCharacterMark.vue'

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

describe('BrandCharacterMark', () => {
  beforeEach(() => {
    stubReducedMotion(false)
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('renders the black-and-white mark at the requested size without stars', () => {
    const wrapper = mount(BrandCharacterMark, { props: { size: 28 } })
    const root = wrapper.get('[data-testid="brand-character-mark"]')
    expect(root.attributes('data-size')).toBe('28')
    expect(root.attributes('style')).toContain('width: 28px')
    expect(root.attributes('style')).toContain('height: 28px')
    expect(wrapper.find('.brand-character__eye').exists()).toBe(true)
    expect(wrapper.html()).toMatch(/#1a1a1a/)
    expect(wrapper.html()).toMatch(/#c8c8c8/)
    expect(wrapper.html()).toMatch(/fill="#fff"|fill="#ffffff"/i)
    expect(wrapper.find('.c-star').exists()).toBe(false)
    expect(wrapper.html()).not.toMatch(/c-star|drop-shadow/)
    wrapper.unmount()
  })

  it('adds the jump class on pointer down', async () => {
    const wrapper = mount(BrandCharacterMark, { props: { size: 48 }, attachTo: document.body })
    await flushPromises()
    const root = wrapper.get('[data-testid="brand-character-mark"]')
    expect(root.classes()).not.toContain('jump')
    await root.trigger('pointerdown', { pointerType: 'mouse', button: 0 })
    expect(root.classes()).toContain('jump')
    vi.advanceTimersByTime(360)
    await flushPromises()
    expect(root.classes()).not.toContain('jump')
    wrapper.unmount()
  })

  it('does not jump when reduced motion is on', async () => {
    stubReducedMotion(true)
    const wrapper = mount(BrandCharacterMark, { props: { size: 28 }, attachTo: document.body })
    await flushPromises()
    const root = wrapper.get('[data-testid="brand-character-mark"]')
    expect(root.classes()).toContain('brand-character--static')
    await root.trigger('pointerdown', { pointerType: 'mouse', button: 0 })
    expect(root.classes()).not.toContain('jump')
    wrapper.unmount()
  })
})
