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

  it('styles the wordmark as solid Fredoka 600 ink without gradient or shimmer', () => {
    const dir = dirname(fileURLToPath(import.meta.url))
    const css = readFileSync(join(dir, '../../styles/global.css'), 'utf8')
    const rule = css.match(/\.brand-logo__name\s*\{([^}]*)\}/)?.[1] ?? ''
    expect(rule).toMatch(/font-family:\s*'Fredoka',\s*ui-rounded/)
    expect(rule).toMatch(/font-weight:\s*600/)
    expect(rule).toMatch(/letter-spacing:\s*-0\.01em/)
    expect(rule).toMatch(/color:\s*rgb\(var\(--c-txt\)\)/)
    expect(rule).not.toMatch(/background|text-fill-color|animation/)
    expect(css).toMatch(/\.brand-logo--sm \.brand-logo__name\s*\{\s*font-size:\s*18px/)
    expect(css).toMatch(/\.brand-logo--md \.brand-logo__name\s*\{\s*font-size:\s*20px/)
    expect(css).toMatch(/\.brand-logo--lg \.brand-logo__name\s*\{\s*font-size:\s*30px/)
    expect(css).not.toMatch(/--grad-logo|@keyframes shimmer\b/)
    // Long custom product names must shrink inside the sidebar lockup and ellipsize.
    expect(css).toMatch(/\.brand-logo\s*\{[^}]*min-width:\s*0/)

    const boot = readFileSync(join(dir, '../../../index.html'), 'utf8')
    const bootRule = boot.match(/\.boot-shell \.brand-logo__name\s*\{([^}]*)\}/)?.[1] ?? ''
    expect(bootRule).toMatch(/'Fredoka'/)
    expect(bootRule).not.toMatch(/gradient|background-clip/)
    expect(boot).toMatch(/rel="preload"\s+href="\/fonts\/fredoka-latin-600-normal\.woff2"/)
  })
})
