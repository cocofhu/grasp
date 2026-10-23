// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import shell from '@/locales/zh-CN/shell.json'
import nav from '@/locales/zh-CN/nav.json'
import enCommon from '@/locales/en/common.json'
import enPages from '@/locales/en/pages.json'
import enShell from '@/locales/en/shell.json'
import enNav from '@/locales/en/nav.json'
import { __resetSidebarHiddenForTests, setSidebarHidden } from '@/lib/shared/sidebarHidden'

const routeState = { path: '/dashboard', meta: {} as Record<string, unknown> }

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() }),
  useRoute: () => routeState,
  RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' },
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: { ...actual.api, listSoloSessions: vi.fn().mockResolvedValue({ sessions: [], maxSandboxes: 0 }) },
    authApi: { logout: vi.fn().mockResolvedValue(undefined) },
  }
})

import AppSidebar from './AppSidebar.vue'
import SidebarAccountMenu from './SidebarAccountMenu.vue'

function mountSidebar() {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages, ...shell, ...nav } },
  })
  return mount(AppSidebar, {
    global: {
      plugins: [i18n],
      stubs: {
        BrandLogo: { template: '<div data-testid="brand" />' },
        AppSidebarNav: { template: '<nav data-testid="nav" />' },
        ShellChromeControls: { template: '<div data-testid="shell-chrome-controls" />' },
        Icon: true,
      },
    },
  })
}

describe('AppSidebar', () => {
  beforeEach(() => {
    __resetSidebarHiddenForTests()
    routeState.path = '/dashboard'
    routeState.meta = {}
  })
  afterEach(() => {
    __resetSidebarHiddenForTests()
    routeState.meta = {}
  })

  it('renders brand, nav stubs, and chrome controls (g1.2)', () => {
    const wrapper = mountSidebar()
    expect(wrapper.find('[data-testid="brand"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="nav"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="shell-chrome-controls"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it.each([
    ['/dashboard', '/dashboard'],
    ['/gates/run-1', '/dashboard'],
    ['/solo/abc', '/dashboard'],
    ['/board', '/board'],
    ['/workflows/wf-1', '/workflows'],
    ['/runs', '/board'],
    ['/runs/run-1', '/board'],
    ['/stats', '/stats'],
  ])('labels the app rail and marks the current section for %s', (path, activeHref) => {
    routeState.path = path
    const wrapper = mountSidebar()
    const rail = wrapper.get('[data-testid="sidebar-app-rail"]')
    expect(rail.attributes('aria-label')).toBe('工作区导航')
    const links = rail.findAll('a.sidebar-app-rail__item')
    expect(links.map((link) => link.attributes('href'))).toEqual(['/dashboard', '/board', '/workflows', '/stats', '/settings'])
    expect(links.map((link) => link.attributes('aria-label'))).toEqual(['新需求', '看板', '工作流', '用量统计', '设置'])
    expect(links.every((link) => link.attributes('title') === link.attributes('aria-label'))).toBe(true)
    expect(links.filter((link) => link.attributes('aria-current') === 'page').map((link) => link.attributes('href'))).toEqual([activeHref])
    expect(links.filter((link) => link.classes().includes('is-active')).map((link) => link.attributes('href'))).toEqual([activeHref])
    wrapper.unmount()
  })

  it('uses English board labels on the rail without changing order', () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'en',
      messages: { en: { ...enCommon, ...enPages, ...enShell, ...enNav } },
    })
    const wrapper = mount(AppSidebar, {
      global: {
        plugins: [i18n],
        stubs: {
          BrandLogo: { template: '<div data-testid="brand" />' },
          AppSidebarNav: { template: '<nav data-testid="nav" />' },
          ShellChromeControls: { template: '<div data-testid="shell-chrome-controls" />' },
          Icon: true,
        },
      },
    })
    const links = wrapper.get('[data-testid="sidebar-app-rail"]').findAll('a.sidebar-app-rail__item')
    expect(links.map((link) => link.attributes('href'))).toEqual(['/dashboard', '/board', '/workflows', '/stats', '/settings'])
    expect(links.map((link) => link.attributes('aria-label'))).toEqual(['New request', 'Board', 'Workflows', 'Usage', 'Settings'])
    wrapper.unmount()
  })

  it.each(['/settings', '/agents', '/notifications'])('keeps the outer rail and one settings directory on %s', (path) => {
    routeState.path = path
    const wrapper = mountSidebar()
    const sidebar = wrapper.get('[data-testid="app-desktop-sidebar"]')
    expect(sidebar.classes()).toContain('w-[360px]')
    expect(sidebar.classes()).toContain('is-settings')
    expect(sidebar.attributes('data-nav-mode')).toBe('settings')
    expect(sidebar.attributes('data-floating')).toBe('true')
    expect(wrapper.get('[data-testid="sidebar-settings-title"]').text()).toBe('设置')
    expect(wrapper.find('[data-testid="sidebar-brand-lockup"]').exists()).toBe(false)
    const rail = wrapper.get('[data-testid="sidebar-app-rail"]')
    expect(rail.findAll('a.sidebar-app-rail__item').map((link) => link.attributes('href'))).toEqual(['/dashboard', '/board', '/workflows', '/stats', '/dashboard'])
    const settingsToggle = rail.get('[data-testid="sidebar-settings-toggle"]')
    expect(settingsToggle.attributes('href')).toBe('/dashboard')
    expect(settingsToggle.attributes('aria-label')).toBe('返回工作区')
    expect(settingsToggle.attributes('title')).toBe('返回工作区')
    expect(settingsToggle.attributes('aria-current')).toBe('page')
    expect(settingsToggle.classes()).toContain('is-active')
    expect(rail.findAll('[aria-current="page"]')).toHaveLength(1)
    expect(wrapper.get('[data-testid="app-sidebar-card"]').classes()).toEqual(expect.arrayContaining(['w-[304px]', 'min-w-[304px]']))
    expect(wrapper.find('[data-testid="desktop-nav-hide"]').exists()).toBe(true)
    expect(wrapper.findAllComponents(SidebarAccountMenu)).toHaveLength(1)
    expect(rail.findComponent(SidebarAccountMenu).exists()).toBe(true)
    expect(wrapper.get('.sidebar-footer').findComponent(SidebarAccountMenu).exists()).toBe(false)
    wrapper.unmount()
  })

  it('restores the rail and settings directory after hiding them together', async () => {
    routeState.path = '/settings'
    const wrapper = mountSidebar()
    await wrapper.get('[data-testid="desktop-nav-hide"]').trigger('click')
    expect(wrapper.get('[data-testid="app-desktop-sidebar"]').attributes('aria-hidden')).toBe('true')
    setSidebarHidden(false)
    await flushPromises()
    expect(wrapper.get('[data-testid="app-desktop-sidebar"]').classes()).toContain('w-[360px]')
    expect(wrapper.get('[data-testid="sidebar-settings-title"]').text()).toBe('设置')
    expect(wrapper.find('[data-testid="sidebar-app-rail"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('puts a 44px hide-nav control on the aligned 64px brand row (g1.2 / g2.1)', () => {
    const wrapper = mountSidebar()
    const row = wrapper.find('[data-testid="sidebar-brand-row"]')
    expect(row.exists()).toBe(true)
    expect(row.classes()).toContain('h-16')
    expect(row.classes()).not.toContain('h-[72px]')
    expect(row.classes()).toContain('justify-between')
    const hide = wrapper.find('[data-testid="desktop-nav-hide"]')
    expect(hide.exists()).toBe(true)
    expect(hide.classes()).toContain('h-11')
    expect(hide.classes()).toContain('w-11')
    expect(hide.classes()).toContain('text-txt3')
    expect(hide.attributes('aria-label')).toBe('隐藏导航')
    expect(hide.attributes('aria-expanded')).toBe('true')
    expect(hide.attributes('aria-controls')).toBe('app-desktop-sidebar')
    const lockup = wrapper.get('[data-testid="sidebar-brand-lockup"]')
    expect(lockup.classes()).toEqual(expect.arrayContaining(['items-center', 'gap-2']))
    const mark = lockup.get('[data-testid="brand-character-mark"]')
    expect(mark.attributes('data-size')).toBe('28')
    expect(mark.attributes('style')).toContain('28px')
    const rowKids = [...row.element.children]
    expect(rowKids[0]?.getAttribute('data-testid')).toBe('sidebar-brand-lockup')
    expect(rowKids[rowKids.length - 1]?.getAttribute('data-testid')).toBe('desktop-nav-hide')
    wrapper.unmount()
  })

  it('does not render brand rail below brand row (g1.3)', () => {
    const wrapper = mountSidebar()
    expect(wrapper.find('[data-testid="sidebar-brand-rail"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('sidebar brand row uses compact gradient wordmark without wash (g1.1 / g2.2)', () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages, ...shell, ...nav } },
    })
    const wrapper = mount(AppSidebar, {
      global: {
        plugins: [i18n],
        stubs: {
          AppSidebarNav: { template: '<nav data-testid="nav" />' },
          ShellChromeControls: { template: '<div data-testid="shell-chrome-controls" />' },
          Icon: true,
        },
      },
    })
    const row = wrapper.find('[data-testid="sidebar-brand-row"]')
    expect(row.classes()).toContain('h-16')
    expect(row.classes()).toContain('app-sidebar-brand-row')
    const logo = wrapper.find('.brand-logo')
    expect(logo.exists()).toBe(true)
    expect(logo.find('.brand-logo__mark').exists()).toBe(false)
    expect(logo.find('.brand-logo__tagline').exists()).toBe(false)
    expect(logo.find('.brand-logo__name').exists()).toBe(true)
    wrapper.unmount()
  })

  it('collapses to width 0 with nav still mounted (g2.2 / g3.1 / g3.3)', async () => {
    const wrapper = mountSidebar()
    await wrapper.find('[data-testid="desktop-nav-hide"]').trigger('click')
    const aside = wrapper.find('[data-testid="app-desktop-sidebar"]')
    expect((aside.element as HTMLElement).style.display).not.toBe('none')
    expect(aside.classes()).toContain('w-0')
    expect(aside.classes()).toContain('border-r-0')
    expect(aside.classes()).not.toContain('w-[360px]')
    expect(aside.classes()).not.toContain('border-r')
    expect(aside.classes()).toContain('md:flex')
    expect(aside.classes()).toContain('hidden')
    expect(aside.classes()).toContain('overflow-hidden')
    expect(aside.attributes('aria-hidden')).toBe('true')
    expect(wrapper.find('[data-testid="nav"]').exists()).toBe(true)
    expect(wrapper.html()).toContain('min-w-[304px]')
    wrapper.unmount()
  })

  it('workspace desktop sidebar uses floating card shell without square clip (g2.2)', () => {
    const wrapper = mountSidebar()
    const aside = wrapper.find('[data-testid="app-desktop-sidebar"]')
    expect(aside.attributes('data-floating')).toBe('true')
    expect(aside.classes()).not.toContain('border-r')
    expect(aside.classes()).toContain('overflow-visible')
    expect(aside.classes()).not.toContain('overflow-hidden')
    const card = wrapper.find('[data-testid="app-sidebar-card"]')
    expect(card.classes()).toContain('app-sidebar-card')
    expect(card.classes()).toContain('bg-surface')
    wrapper.unmount()
  })

  it('full pages keep the same floating card (plan g1.1)', () => {
    routeState.meta = { full: true }
    const wrapper = mountSidebar()
    const aside = wrapper.find('[data-testid="app-desktop-sidebar"]')
    expect(aside.attributes('data-floating')).toBe('true')
    expect(aside.classes()).not.toContain('border-r')
    expect(aside.classes()).not.toContain('bg-surface')
    expect(aside.classes()).toContain('overflow-visible')
    const card = wrapper.find('[data-testid="app-sidebar-card"]')
    expect(card.classes()).toContain('app-sidebar-card')
    expect(card.classes()).toContain('bg-surface')
    wrapper.unmount()
  })

  it('keeps the account in the rail and footer tools in the content panel', () => {
    const wrapper = mountSidebar()
    const avatar = wrapper.find('[data-testid="sidebar-user-avatar"]')
    expect(avatar.exists()).toBe(true)
    // force-radius-full keeps avatar circular (role: full)
    expect(avatar.classes()).toContain('force-radius-full')
    expect(avatar.classes()).toContain('rounded-full')
    expect(avatar.classes()).toContain('h-7')
    expect(wrapper.get('[data-testid="sidebar-app-rail"]').findComponent(SidebarAccountMenu).exists()).toBe(true)
    // Footer keeps chrome and account rows compact with a 4px gap.
    const footer = wrapper.find('.sidebar-footer')
    expect(footer.classes()).toContain('gap-1')
    expect(footer.classes()).toContain('flex-col')
    expect(footer.findComponent(SidebarAccountMenu).exists()).toBe(false)
    expect(footer.find('[data-testid="sidebar-user-name"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('logout clears leftover embed drawer tokens', async () => {
    localStorage.setItem('grasp.embed.run-1.n1', JSON.stringify({ token: 'gse_x', expiresAt: '2099-01-01T00:00:00Z' }))
    const wrapper = mountSidebar()
    wrapper.getComponent(SidebarAccountMenu).vm.$emit('logout')
    await flushPromises()
    expect(localStorage.getItem('grasp.embed.run-1.n1')).toBeNull()
    wrapper.unmount()
  })
})
