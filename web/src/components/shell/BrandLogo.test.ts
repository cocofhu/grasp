// @vitest-environment happy-dom
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import BrandLogo from './BrandLogo.vue'
import { setBrandSettings } from '@/lib/composables/useBrandSettings'

describe('BrandLogo', () => {
  it('renders app name and tagline by default', () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const wrapper = mount(BrandLogo, {
      props: { size: 'md', align: 'center' },
      global: { plugins: [i18n] },
    })
    expect(wrapper.find('.brand-logo__name').text().length).toBeGreaterThan(0)
    expect(wrapper.find('.brand-logo__tagline').exists()).toBe(true)
    expect(wrapper.find('.brand-logo__mark').exists()).toBe(false)
    wrapper.unmount()
  })

  it('hides tagline when showTagline is false (sidebar compact wordmark)', () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const wrapper = mount(BrandLogo, {
      props: { size: 'md', showTagline: false },
      global: { plugins: [i18n] },
    })
    const name = wrapper.find('.brand-logo__name')
    expect(name.text()).toBe('Grasp')
    expect(wrapper.find('.brand-logo__tagline').exists()).toBe(false)
    expect(wrapper.find('.brand-logo__mark').exists()).toBe(false)
    expect(wrapper.classes()).not.toContain('brand-logo--with-mark')
    wrapper.unmount()
  })

  it('uses custom product name only when authenticated shell opts in', () => {
    setBrandSettings({ product_name: 'Acme Flow' })
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const shellLogo = mount(BrandLogo, {
      props: { useCustomBrand: true },
      global: { plugins: [i18n] },
    })
    const loginLogo = mount(BrandLogo, { global: { plugins: [i18n] } })
    expect(shellLogo.get('.brand-logo__name').text()).toBe('Acme Flow')
    expect(loginLogo.get('.brand-logo__name').text()).toBe('Grasp')
    shellLogo.unmount()
    loginLogo.unmount()
    setBrandSettings(null)
  })

  it('does not leak mark markup to login, boot shell, or mobile drawer (g3.2)', () => {
    const dir = dirname(fileURLToPath(import.meta.url))
    const sidebar = readFileSync(join(dir, 'AppSidebar.vue'), 'utf8')
    const shell = readFileSync(join(dir, 'AppShell.vue'), 'utf8')
    const login = readFileSync(join(dir, '../../views/LoginView.vue'), 'utf8')
    const boot = readFileSync(join(dir, '../../../index.html'), 'utf8')
    expect(sidebar).not.toMatch(/show-mark|showMark/)
    expect(sidebar).toMatch(/show-tagline="false"/)
    expect(shell).not.toMatch(/show-mark|showMark|brand-logo__mark/)
    expect(login).not.toMatch(/show-mark|showMark|brand-logo__mark/)
    expect(boot).not.toMatch(/brand-logo__mark/)
  })
})
