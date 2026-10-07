// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import enCommon from '@/locales/en/common.json'
import enPages from '@/locales/en/pages.json'
import { markAuthReady, useAuth } from '@/lib/composables/useAuth'

const authApiMocks = vi.hoisted(() => ({
  login: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    authApi: {
      ...actual.authApi,
      login: authApiMocks.login,
    },
  }
})

import LoginView from './LoginView.vue'

async function mountLogin(locale: 'zh-CN' | 'en' = 'zh-CN', redirect?: string) {
  const i18n =
    locale === 'zh-CN'
      ? createI18n({
          legacy: false,
          locale: 'zh-CN',
          messages: { 'zh-CN': { ...common, ...pages } },
        })
      : createI18n({
          legacy: false,
          locale: 'en',
          messages: { en: { ...enCommon, ...enPages } },
        })
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { path: '/login', component: LoginView },
    ],
  })
  await router.push(redirect === undefined ? '/login' : { path: '/login', query: { redirect } })
  await router.isReady()
  return mount(LoginView, {
    global: {
      plugins: [i18n, router],
      stubs: { BrandLogo: true, Icon: true, AppButton: { template: '<button type="submit"><slot /></button>' } },
    },
  })
}

beforeEach(() => {
  vi.clearAllMocks()
  useAuth().clearUser()
  markAuthReady()
})

describe('LoginView copy', () => {
  it('hides demo account hint, divider, and collapsible footer (plan g1.1 / g1.2)', async () => {
    const wrapper = await mountLogin('zh-CN')
    await flushPromises()
    expect(wrapper.text()).not.toContain('demo1234')
    expect(wrapper.text()).not.toMatch(/Demo\s+admin/)
    expect(wrapper.find('kbd').exists()).toBe(false)
    expect(wrapper.find('details').exists()).toBe(false)
    expect(wrapper.find('form').element.nextElementSibling).toBeNull()
    expect(wrapper.find('#username').exists()).toBe(true)
    expect(wrapper.find('#password').exists()).toBe(true)
    expect(wrapper.find('button[type="submit"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('shows account subtitle without internal 静态账号 wording', async () => {
    const wrapper = await mountLogin('zh-CN')
    await flushPromises()
    expect(wrapper.text()).toContain('使用账号登录管理界面')
    expect(wrapper.text()).not.toContain('静态账号')
    wrapper.unmount()
  })

  it('shows bad-credentials error without raw HTTP status phrase', async () => {
    authApiMocks.login.mockRejectedValue(new Error('401 Unauthorized'))
    const wrapper = await mountLogin('zh-CN')
    await flushPromises()
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain('用户名或密码错误')
    expect(wrapper.text()).not.toContain('Unauthorized')
    expect(wrapper.text()).not.toMatch(/401/)
    wrapper.unmount()
  })

  it('switches subtitle to English', async () => {
    const wrapper = await mountLogin('en')
    await flushPromises()
    expect(wrapper.text()).toContain('Sign in with your account to manage the console')
    expect(wrapper.text()).not.toContain('静态账号')
    wrapper.unmount()
  })

  it('sanitizes login response redirect before navigating', async () => {
    authApiMocks.login.mockResolvedValue({
      username: 'admin',
      expires_at: '2099-01-01T00:00:00Z',
      redirect: 'https://evil.example/phish',
    })
    const wrapper = await mountLogin('zh-CN')
    await flushPromises()
    await wrapper.find('#username').setValue('admin')
    await wrapper.find('#password').setValue('x')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(wrapper.vm.$route.path).toBe('/')
    wrapper.unmount()
  })
})

describe('LoginView redirect hint (plan g1.1 / g1.2 / g1.3)', () => {
  class ResizeObserverStub {
    observe() {}
    unobserve() {}
    disconnect() {}
  }

  beforeEach(() => {
    vi.stubGlobal('ResizeObserver', ResizeObserverStub)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('keeps the hint on one text node and truncates a long path (plan g1.1 / g1.2 / g1.3)', async () => {
    const longPath = `/${'1'.repeat(80)}`
    const wrapper = await mountLogin('zh-CN', longPath)
    await flushPromises()

    const hint = wrapper.get('[data-testid="login-redirect-hint"]')
    const label = hint.get('span.shrink-0.whitespace-nowrap')
    expect(label.text()).toBe('登录后将回跳至')
    expect(label.element.childNodes).toHaveLength(1)
    expect(label.element.childNodes[0]?.nodeType).toBe(Node.TEXT_NODE)
    expect(label.classes()).toEqual(expect.arrayContaining(['shrink-0', 'whitespace-nowrap']))

    const path = hint.get('[data-testid="login-redirect-path"]')
    expect(path.classes()).toEqual(
      expect.arrayContaining(['min-w-0', 'flex-1', 'overflow-hidden', 'text-ellipsis', 'whitespace-nowrap']),
    )
    expect(path.classes()).toEqual(expect.arrayContaining(['font-mono', 'text-[11px]', 'text-txt2']))
    expect(path.text()).toBe(longPath)
    wrapper.unmount()
  })

  it('shows a short redirect path in full on the same row (plan g1.3)', async () => {
    const wrapper = await mountLogin('zh-CN', '/dashboard')
    await flushPromises()

    const hint = wrapper.get('[data-testid="login-redirect-hint"]')
    expect(hint.get('span.shrink-0.whitespace-nowrap').text()).toBe('登录后将回跳至')
    expect(hint.get('[data-testid="login-redirect-path"]').text()).toBe('/dashboard')
    wrapper.unmount()
  })

  it('does not render the redirect hint without a redirect query (plan g1.3)', async () => {
    const wrapper = await mountLogin('zh-CN')
    await flushPromises()
    expect(wrapper.find('[data-testid="login-redirect-hint"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('登录后将回跳至')
    wrapper.unmount()
  })

  it('keeps the English hint on one text node for a long path (plan g1.1 / g1.3)', async () => {
    const longPath = `/${'a'.repeat(80)}`
    const wrapper = await mountLogin('en', longPath)
    await flushPromises()

    const label = wrapper.get('[data-testid="login-redirect-hint"] span.shrink-0.whitespace-nowrap')
    expect(label.text()).toBe('After sign-in you will return to')
    expect(label.element.childNodes).toHaveLength(1)
    expect(label.element.childNodes[0]?.nodeType).toBe(Node.TEXT_NODE)
    expect(wrapper.get('[data-testid="login-redirect-path"]').text()).toBe(longPath)
    wrapper.unmount()
  })
})
