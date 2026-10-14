// @vitest-environment happy-dom
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import HomeParticleMeshBackground from './HomeParticleMeshBackground.vue'

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

describe('HomeParticleMeshBackground', () => {
  beforeEach(() => {
    stubReducedMotion(false)
    vi.spyOn(window, 'requestAnimationFrame').mockImplementation((cb) => {
      cb(0)
      return 1
    })
    vi.spyOn(window, 'cancelAnimationFrame').mockImplementation(() => {})
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('renders canvas layer with pointer-events none', async () => {
    const wrapper = mount(HomeParticleMeshBackground, {
      attachTo: document.body,
    })
    await flushPromises()
    const root = wrapper.get('[data-testid="home-particle-mesh-bg"]')
    expect(root.classes()).toContain('home-particle-mesh')
    const canvas = wrapper.find('canvas')
    expect(canvas.exists()).toBe(true)
    const src = readFileSync(
      join(dirname(fileURLToPath(import.meta.url)), 'HomeParticleMeshBackground.vue'),
      'utf8',
    )
    expect(src).toMatch(/pointer-events:\s*none/)
    expect(src).toMatch(/window\.addEventListener\(\s*'pointermove'/)
    expect(src).not.toMatch(/addEventListener\(\s*'mousemove'/)
    expect(src).not.toMatch(/addEventListener\(\s*'mouseleave'/)
    wrapper.unmount()
  })

  it('does not start animation loop under reduced-motion', async () => {
    stubReducedMotion(true)
    const rafSpy = vi.spyOn(window, 'requestAnimationFrame')
    const wrapper = mount(HomeParticleMeshBackground, {
      attachTo: document.body,
    })
    await flushPromises()
    expect(rafSpy).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('stops animation loop on unmount', async () => {
    const cancelSpy = vi.spyOn(window, 'cancelAnimationFrame')
    const wrapper = mount(HomeParticleMeshBackground, {
      attachTo: document.body,
    })
    await flushPromises()
    wrapper.unmount()
    expect(cancelSpy).toHaveBeenCalled()
  })
})
