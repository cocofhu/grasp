// @vitest-environment happy-dom
import { nextTick, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import shell from '@/locales/zh-CN/shell.json'
import {
  __resetSidebarHiddenForTests,
  setSidebarHidden,
  sidebarHidden,
} from '@/lib/shared/sidebarHidden'
import { inlineMobileNavHosts } from '@/lib/shared/mobileNav'

const isMobileRef = ref(false)

vi.mock('@/lib/composables/useBreakpoint', () => ({
  useBreakpoint: () => ({ isMobile: isMobileRef }),
}))

import FloatingNavBall from './FloatingNavBall.vue'

function mountBall(drawerOpen = false) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages, ...shell } },
  })
  return mount(FloatingNavBall, {
    props: { drawerOpen },
    global: {
      plugins: [i18n],
      stubs: { Icon: true },
    },
  })
}

describe('FloatingNavBall (g2.1–g2.5)', () => {
  beforeEach(() => {
    __resetSidebarHiddenForTests()
    isMobileRef.value = false
    vi.useFakeTimers()
  })
  afterEach(() => {
    __resetSidebarHiddenForTests()
    vi.useRealTimers()
  })

  it('is focusable with aria-label when sidebar hidden (g2.5)', () => {
    setSidebarHidden(true)
    const wrapper = mountBall()
    const btn = wrapper.find('[data-testid="floating-nav-ball"]')
    expect(btn.attributes('aria-label')).toBe('打开导航')
    expect(btn.attributes('title')).toBe('打开导航')
    expect(btn.attributes('aria-controls')).toBe('app-desktop-sidebar')
    expect(btn.attributes('tabindex')).toBe('0')
    wrapper.unmount()
  })

  it('pins on click without waiting for the exit animation (g1.1)', async () => {
    setSidebarHidden(true)
    const wrapper = mountBall()
    await wrapper.find('[data-testid="floating-nav-ball"]').trigger('click')
    expect(sidebarHidden.value).toBe(false)
    expect(wrapper.find('[data-testid="floating-nav-ball-wrap"]').attributes('data-exiting')).toBe(
      'true',
    )
    await vi.advanceTimersByTimeAsync(200)
    await nextTick()
    expect(sidebarHidden.value).toBe(false)
    expect(wrapper.find('[data-testid="floating-nav-ball-wrap"]').attributes('data-exiting')).toBe(
      'false',
    )
    wrapper.unmount()
  })

  it('ignores repeat clicks during the exit animation (g2.3)', async () => {
    setSidebarHidden(true)
    const wrapper = mountBall()
    const ball = wrapper.find('[data-testid="floating-nav-ball"]')
    await ball.trigger('click')
    await ball.trigger('click')
    expect(sidebarHidden.value).toBe(false)
    await vi.advanceTimersByTimeAsync(200)
    await nextTick()
    expect(sidebarHidden.value).toBe(false)
    wrapper.unmount()
  })

  it('reappears at once when the sidebar is hidden again mid-exit (g2.1)', async () => {
    setSidebarHidden(true)
    const wrapper = mountBall()
    await wrapper.find('[data-testid="floating-nav-ball"]').trigger('click')
    setSidebarHidden(true)
    await nextTick()
    const wrap = wrapper.find('[data-testid="floating-nav-ball-wrap"]')
    expect(wrap.attributes('data-exiting')).toBe('false')
    expect(wrap.classes()).toContain('pointer-events-auto')
    wrapper.unmount()
  })

  it('respects prefers-reduced-motion with instant pin (g2.3 / g2.5)', async () => {
    setSidebarHidden(true)
    vi.stubGlobal(
      'matchMedia',
      vi.fn().mockImplementation((query: string) => ({
        matches: query.includes('prefers-reduced-motion'),
        media: query,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        addListener: vi.fn(),
        removeListener: vi.fn(),
        dispatchEvent: vi.fn(),
        onchange: null,
      })),
    )
    const wrapper = mountBall()
    await wrapper.find('[data-testid="floating-nav-ball"]').trigger('click')
    await nextTick()
    expect(sidebarHidden.value).toBe(false)
    wrapper.unmount()
    vi.unstubAllGlobals()
  })

  it('emits open-drawer on mobile and does not pin (g2.4)', async () => {
    isMobileRef.value = true
    setSidebarHidden(true)
    const wrapper = mountBall()
    await wrapper.find('[data-testid="floating-nav-ball"]').trigger('click')
    expect(wrapper.emitted('open-drawer')).toHaveLength(1)
    expect(sidebarHidden.value).toBe(true)
    wrapper.unmount()
  })

  it('yields on mobile while a header hosts its own nav button', async () => {
    isMobileRef.value = true
    const wrapper = mountBall()
    const wrap = () => wrapper.find('[data-testid="floating-nav-ball-wrap"]')
    expect(wrap().classes()).toContain('pointer-events-auto')
    inlineMobileNavHosts.value = 1
    await nextTick()
    expect(wrap().classes()).toContain('invisible')
    inlineMobileNavHosts.value = 0
    await nextTick()
    expect(wrap().classes()).toContain('pointer-events-auto')
    wrapper.unmount()
  })

  it('hover does not pin sidebar (g2.2)', async () => {
    setSidebarHidden(true)
    const wrapper = mountBall()
    await wrapper.find('[data-testid="floating-nav-ball"]').trigger('mouseenter')
    expect(sidebarHidden.value).toBe(true)
    expect(wrapper.emitted('open-drawer')).toBeUndefined()
    wrapper.unmount()
  })
})
