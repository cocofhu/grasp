// @vitest-environment happy-dom
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import nav from '@/locales/zh-CN/nav.json'
import shell from '@/locales/zh-CN/shell.json'
import SidebarAccountMenu from './SidebarAccountMenu.vue'

const mounted: Array<ReturnType<typeof mount>> = []

async function render(username = 'preview') {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/dashboard', component: { template: '<div />' } },
      { path: '/settings', component: { template: '<div />' } },
    ],
  })
  await router.push('/dashboard')
  const host = document.createElement('div')
  document.body.appendChild(host)
  const wrapper = mount(SidebarAccountMenu, {
    props: { username, initials: 'PR' },
    attachTo: host,
    global: {
      plugins: [
        router,
        createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...nav, ...shell } } }),
      ],
      stubs: { Icon: true },
    },
  })
  mounted.push(wrapper)
  return { wrapper, router, trigger: wrapper.get('[data-testid="sidebar-account-trigger"]') }
}

function panel() {
  return document.body.querySelector<HTMLElement>('[data-testid="sidebar-account-menu"]')
}

function item(testId: string) {
  return document.body.querySelector<HTMLElement>(`[data-testid="${testId}"]`)!
}

async function key(target: HTMLElement | Document, value: string) {
  target.dispatchEvent(new KeyboardEvent('keydown', { key: value, bubbles: true, cancelable: true }))
  await flushPromises()
}

function bounds(left: number, top: number, width: number, height: number): DOMRect {
  return { x: left, y: top, left, top, width, height, right: left + width, bottom: top + height, toJSON: () => ({}) } as DOMRect
}

afterEach(() => {
  mounted.splice(0).forEach((wrapper) => wrapper.unmount())
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('SidebarAccountMenu', () => {
  it('keeps the username behind an accessible avatar trigger until opened', async () => {
    const username = 'a-very-long-preview-account-name'
    const { wrapper, trigger } = await render(username)
    expect(trigger.attributes('aria-label')).toBe(username)
    expect(trigger.attributes('aria-haspopup')).toBe('menu')
    expect(trigger.attributes('aria-expanded')).toBe('false')
    expect(wrapper.get('[data-testid="sidebar-user-avatar"]').text()).toBe('PR')
    expect(wrapper.text()).not.toContain(username)
    expect(panel()).toBeNull()

    await trigger.trigger('click')
    await flushPromises()
    expect(panel()?.id).toBe(trigger.attributes('aria-controls'))
    expect(panel()?.getAttribute('aria-labelledby')).toBe(trigger.attributes('id'))
    expect(panel()?.querySelector('.sidebar-account-name')?.getAttribute('title')).toBe(username)
    expect(panel()?.textContent).toContain(username)
    expect(document.activeElement).toBe(item('sidebar-account-settings'))
    expect(trigger.attributes('aria-expanded')).toBe('true')
  })

  it('portals the first panel above the trigger and clamps it inside a narrow viewport', async () => {
    vi.stubGlobal('innerWidth', 320)
    vi.stubGlobal('innerHeight', 600)
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
      return this.dataset.testid === 'sidebar-account-trigger'
        ? bounds(270, 550, 44, 44)
        : bounds(0, 0, 220, 144)
    })
    const { wrapper, trigger } = await render()
    await trigger.trigger('click')
    await flushPromises()
    const menu = panel()!
    expect(wrapper.element.contains(menu)).toBe(false)
    expect(menu.classList.contains('fixed')).toBe(true)
    expect(menu.style.position).toBe('fixed')
    expect(menu.style.width).toBe('220px')
    expect(menu.style.left).toBe('92px')
    expect(menu.style.top).toBe('398px')
  })

  it('moves through the actions with arrows, Home, and End, then returns focus on Escape', async () => {
    const { trigger } = await render()
    await trigger.trigger('keydown', { key: 'ArrowDown' })
    await flushPromises()
    const settings = item('sidebar-account-settings')
    const logout = item('sidebar-logout')
    expect(document.activeElement).toBe(settings)
    await key(settings, 'ArrowDown')
    expect(document.activeElement).toBe(logout)
    await key(logout, 'ArrowDown')
    expect(document.activeElement).toBe(settings)
    await key(settings, 'ArrowUp')
    expect(document.activeElement).toBe(logout)
    await key(logout, 'Home')
    expect(document.activeElement).toBe(settings)
    await key(settings, 'End')
    expect(document.activeElement).toBe(logout)
    await key(logout, 'Escape')
    expect(panel()).toBeNull()
    expect(document.activeElement).toBe(trigger.element)
    expect(trigger.attributes('aria-expanded')).toBe('false')

    await trigger.trigger('keydown', { key: 'ArrowUp' })
    await flushPromises()
    expect(document.activeElement).toBe(item('sidebar-logout'))
  })

  it('restores focus when toggled closed and leaves outside-click focus alone', async () => {
    const { trigger } = await render()
    await trigger.trigger('click')
    await flushPromises()
    await trigger.trigger('click')
    await flushPromises()
    expect(panel()).toBeNull()
    expect(document.activeElement).toBe(trigger.element)

    await trigger.trigger('click')
    await flushPromises()
    const outside = document.createElement('button')
    document.body.appendChild(outside)
    outside.focus()
    outside.click()
    await flushPromises()
    expect(panel()).toBeNull()
    expect(document.activeElement).toBe(outside)
  })

  it('keeps settings as a real route and closes when navigating', async () => {
    const { router, trigger } = await render()
    await trigger.trigger('click')
    await flushPromises()
    const settings = item('sidebar-account-settings')
    expect(settings.getAttribute('href')).toBe('/settings')
    expect(settings.textContent).toBe('设置')
    settings.click()
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/settings')
    expect(panel()).toBeNull()
  })

  it('closes before emitting logout without performing authentication itself', async () => {
    const { wrapper, trigger } = await render()
    await trigger.trigger('click')
    await flushPromises()
    expect(item('sidebar-logout').textContent).toBe('登出')
    item('sidebar-logout').click()
    await flushPromises()
    expect(panel()).toBeNull()
    expect(trigger.attributes('aria-expanded')).toBe('false')
    expect(wrapper.emitted('logout')).toEqual([[]])
    expect(document.activeElement).toBe(trigger.element)
  })

  it('allows Tab to dismiss the menu and closes on an external route change', async () => {
    const { router, trigger } = await render()
    await trigger.trigger('click')
    await flushPromises()
    await key(item('sidebar-account-settings'), 'Tab')
    expect(panel()).toBeNull()
    expect(document.activeElement).toBe(trigger.element)

    await trigger.trigger('click')
    await flushPromises()
    await router.push('/settings')
    await flushPromises()
    expect(panel()).toBeNull()
  })

  it('closes its portal when the parent hides the sidebar and blocks hidden opens', async () => {
    const { wrapper, trigger } = await render()
    await trigger.trigger('click')
    await flushPromises()
    expect(panel()).not.toBeNull()
    await wrapper.setProps({ visible: false })
    await flushPromises()
    expect(panel()).toBeNull()
    expect(trigger.attributes('aria-expanded')).toBe('false')
    expect(document.activeElement).not.toBe(trigger.element)
    await trigger.trigger('click')
    await flushPromises()
    expect(panel()).toBeNull()

    await wrapper.setProps({ visible: true })
    await trigger.trigger('click')
    await flushPromises()
    expect(panel()).not.toBeNull()
  })
})
