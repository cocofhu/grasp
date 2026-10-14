// @vitest-environment happy-dom
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import HomeLogoEasterEgg from './HomeLogoEasterEgg.vue'

describe('HomeLogoEasterEgg', () => {
  beforeEach(() => {
    vi.stubGlobal('matchMedia', (query: string) => ({
      matches: query.includes('prefers-reduced-motion: reduce') ? false : false,
      media: query,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn(),
      onchange: null,
    }))
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('renders mark, brand typewriter slots, and respects reduced-motion disable', async () => {
    const wrapper = mount(HomeLogoEasterEgg, {
      props: {
        productName: 'Grasp',
        brandVisible: 'Grasp',
        brandCursorBlink: false,
        brandCursorGone: true,
      },
      slots: { default: '<div data-testid=\"egg-slot\">ui</div>' },
      attachTo: document.body,
    })
    await flushPromises()
    expect(wrapper.get('[data-testid=\"home-logo-easter-egg\"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid=\"home-logo-mark\"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid=\"home-brand-text\"]').text()).toBe('Grasp')
    expect(wrapper.get('[data-testid=\"egg-slot\"]').text()).toBe('ui')
    expect(wrapper.get('[data-testid=\"home-logo-mark\"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('disables mark under reduced motion', async () => {
    vi.stubGlobal('matchMedia', (query: string) => ({
      matches: String(query).includes('prefers-reduced-motion') ,
      media: query,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn(),
      onchange: null,
    }))
    const wrapper = mount(HomeLogoEasterEgg, {
      props: {
        productName: 'Grasp',
        brandVisible: 'Grasp',
        brandCursorBlink: false,
        brandCursorGone: true,
      },
    })
    await flushPromises()
    expect(wrapper.get('[data-testid=\"home-logo-mark\"]').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })
})
