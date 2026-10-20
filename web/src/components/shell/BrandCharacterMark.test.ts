// @vitest-environment happy-dom
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import BrandCharacterMark from './BrandCharacterMark.vue'

function stubReducedMotion(matches: boolean) {
  let changeListener: (() => void) | undefined
  const queryList = {
    matches,
    media: '(prefers-reduced-motion: reduce)',
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn((_event: string, listener: () => void) => { changeListener = listener }),
    removeEventListener: vi.fn((_event: string, listener: () => void) => {
      if (changeListener === listener) changeListener = undefined
    }),
    dispatchEvent: vi.fn(),
  }
  vi.stubGlobal(
    'matchMedia',
    vi.fn(() => queryList),
  )
  return {
    queryList,
    setMatches(value: boolean) {
      queryList.matches = value
      changeListener?.()
    },
  }
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
    expect(root.attributes('aria-hidden')).toBe('true')
    expect(root.attributes('role')).toBeUndefined()
    expect(root.attributes('tabindex')).toBeUndefined()
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

  it('stops an active response when motion preference changes and clears timers on unmount', async () => {
    const motion = stubReducedMotion(false)
    const wrapper = mount(BrandCharacterMark, { props: { size: 28 } })
    const root = wrapper.get('[data-testid="brand-character-mark"]')
    const baseline = vi.getTimerCount()
    await root.trigger('pointerdown', { pointerType: 'mouse', button: 0 })
    expect(root.classes()).toContain('jump')
    expect(vi.getTimerCount()).toBe(baseline + 1)

    motion.setMatches(true)
    await wrapper.vm.$nextTick()
    expect(root.classes()).toContain('brand-character--static')
    expect(root.classes()).not.toContain('jump')
    expect(vi.getTimerCount()).toBe(baseline)
    await root.trigger('pointerdown', { pointerType: 'touch', button: 0 })
    expect(root.classes()).not.toContain('jump')

    motion.setMatches(false)
    await wrapper.vm.$nextTick()
    expect(root.classes()).not.toContain('brand-character--static')
    await root.trigger('pointerdown', { pointerType: 'touch', button: 0 })
    expect(root.classes()).toContain('jump')
    wrapper.unmount()
    expect(vi.getTimerCount()).toBe(baseline)
    expect(motion.queryList.removeEventListener).toHaveBeenCalledWith('change', expect.any(Function))
  })

  it('ignores secondary mouse input and keeps the fixed size during the response', async () => {
    const wrapper = mount(BrandCharacterMark, { props: { size: 48 } })
    const root = wrapper.get('[data-testid="brand-character-mark"]')
    const baseline = vi.getTimerCount()
    await root.trigger('pointerdown', { pointerType: 'mouse', button: 2 })
    expect(root.classes()).not.toContain('jump')
    expect(vi.getTimerCount()).toBe(baseline)
    await root.trigger('pointerdown', { pointerType: 'pen', button: 0 })
    expect(root.classes()).toContain('jump')
    expect(root.attributes('style')).toContain('width: 48px; height: 48px')
    vi.advanceTimersByTime(360)
    await wrapper.vm.$nextTick()
    expect(root.classes()).not.toContain('jump')
    expect(vi.getTimerCount()).toBe(baseline)
    wrapper.unmount()
  })
})
