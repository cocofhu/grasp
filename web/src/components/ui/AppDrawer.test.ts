// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import AppDrawer from './AppDrawer.vue'

function mountDrawer(open = true, extra: { flush?: boolean; width?: number } = {}) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common } },
  })
  return mount(AppDrawer, {
    props: { open, title: '抽屉', ...extra },
    slots: { default: '<div data-testid="drawer-body">正文</div>' },
    global: { plugins: [i18n], stubs: { Icon: true } },
    attachTo: document.body,
  })
}

function sheet(): HTMLElement {
  const el = document.body.querySelector('.fixed.inset-0 > div:last-child')
  if (!(el instanceof HTMLElement)) throw new Error('drawer sheet missing')
  return el
}

describe('AppDrawer', () => {
  it('teleports drawer content when open', () => {
    const wrapper = mountDrawer(true)
    expect(document.body.textContent).toContain('抽屉')
    expect(document.body.querySelector('[data-testid="drawer-body"]')).toBeTruthy()
    wrapper.unmount()
  })

  it('emits close from overlay click', async () => {
    const wrapper = mountDrawer(true)
    const overlay = document.body.querySelector('.bg-black\\/50') as HTMLElement
    overlay?.click()
    expect(wrapper.emitted('close')).toHaveLength(1)
    wrapper.unmount()
  })

  it('keeps the default sheet as a floating app-sidebar-card', () => {
    const wrapper = mountDrawer(true)
    const panel = sheet()
    expect(panel.className).toMatch(/\bapp-sidebar-card\b/)
    expect(panel.className).toMatch(/\bbottom-3\.5\b/)
    expect(panel.className).toMatch(/\bright-3\.5\b/)
    expect(panel.className).toMatch(/\btop-3\.5\b/)
    expect(panel.className).not.toMatch(/\binset-y-0\b/)
    expect(panel.style.maxWidth).toBe('calc(100vw - 28px)')
    wrapper.unmount()
  })

  it('flush sheet meets the viewport top, right, and bottom without the card shell', () => {
    const wrapper = mountDrawer(true, { flush: true, width: 420 })
    const panel = sheet()
    expect(panel.className).not.toMatch(/\bapp-sidebar-card\b/)
    expect(panel.className).not.toMatch(/\b(bottom|right|top)-3\.5\b/)
    expect(panel.className).not.toMatch(/\brounded/)
    expect(panel.className).not.toMatch(/\bshadow/)
    expect(panel.className).toMatch(/\binset-y-0\b/)
    expect(panel.className).toMatch(/\bright-0\b/)
    expect(panel.className).toMatch(/\bborder-l\b/)
    expect(panel.className).toMatch(/\bborder-line\b/)
    expect(panel.style.width).toBe('420px')
    expect(panel.style.maxWidth).toBe('100vw')
    wrapper.unmount()
  })
})
