// @vitest-environment happy-dom
import { afterEach, describe, expect, it } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import AgentSwitcher from './AgentSwitcher.vue'

const wrappers: VueWrapper[] = []
function mountSwitcher() {
  const wrapper = mount(AgentSwitcher, {
    attachTo: document.body,
    props: {
      label: 'alpha',
      nodes: [{ id: 'alpha', label: 'alpha' }, { id: 'beta', label: 'beta' }],
      pinned: [{ id: '__workspace_defaults__', label: 'Agent 默认配置' }],
      activeKey: 'alpha',
    },
    global: { plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })], stubs: { Icon: true } },
  })
  wrappers.push(wrapper)
  return wrapper
}
afterEach(() => {
  for (const wrapper of wrappers.splice(0)) if (wrapper.exists()) wrapper.unmount()
  document.body.innerHTML = ''
})

const panelVisible = (wrapper: VueWrapper) => (wrapper.get('[data-testid="agent-switcher-panel"]').element as HTMLElement).style.display !== 'none'

describe('Agent switcher', () => {
  it('opens from the title, focuses search, and closes after picking an Agent', async () => {
    const wrapper = mountSwitcher()
    const trigger = wrapper.get('[data-testid="agent-switcher-trigger"]')
    expect(trigger.text()).toBe('alpha')
    expect(trigger.attributes('aria-label')).toBe('alpha · 切换智能体')
    expect(trigger.attributes('aria-expanded')).toBe('false')
    expect(panelVisible(wrapper)).toBe(false)

    await trigger.trigger('click')
    await flushPromises()
    expect(panelVisible(wrapper)).toBe(true)
    expect(trigger.attributes('aria-expanded')).toBe('true')
    expect(document.activeElement).toBe(wrapper.get('input[type="search"]').element)

    await wrapper.get('[data-resource-key="beta"]').trigger('click')
    expect(wrapper.emitted('select')).toEqual([['beta']])
    expect(panelVisible(wrapper)).toBe(false)

    await trigger.trigger('click')
    await trigger.trigger('click')
    expect(panelVisible(wrapper)).toBe(false)
  })

  it('closes on an outside press or Escape, but not for presses in its own menus', async () => {
    const wrapper = mountSwitcher()
    const trigger = wrapper.get('[data-testid="agent-switcher-trigger"]')
    await trigger.trigger('click')
    await wrapper.get('[data-testid="agent-tree-more"]').trigger('click')
    await flushPromises()
    const more = document.querySelector('[data-testid="agent-tree-more-menu"]')!
    more.querySelector('button')!.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    await flushPromises()
    expect(panelVisible(wrapper)).toBe(true)

    // The first Escape closes the list's own menu; the next one closes the switcher.
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    await flushPromises()
    expect(document.querySelector('[data-testid="agent-tree-more-menu"]')).toBeNull()
    expect(panelVisible(wrapper)).toBe(true)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    await flushPromises()
    expect(panelVisible(wrapper)).toBe(false)
    expect(document.activeElement).toBe(trigger.element)

    await trigger.trigger('click')
    await wrapper.get('[data-testid="agent-tree-more"]').trigger('click')
    await flushPromises()
    document.body.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    await flushPromises()
    expect(panelVisible(wrapper)).toBe(false)
    expect(document.querySelector('[data-testid="agent-tree-more-menu"]')).toBeNull()
  })

  it('closes before forwarding list actions', async () => {
    const wrapper = mountSwitcher()
    const trigger = wrapper.get('[data-testid="agent-switcher-trigger"]')
    const openMenuAction = async (action: string) => {
      await trigger.trigger('click')
      await wrapper.get('[data-testid="agent-tree-more"]').trigger('click')
      await flushPromises()
      document.querySelector<HTMLButtonElement>(`[data-testid="agent-tree-${action}"]`)!.click()
      await flushPromises()
      expect(panelVisible(wrapper)).toBe(false)
    }
    await openMenuAction('import')
    await openMenuAction('export')
    await openMenuAction('create-team')

    await trigger.trigger('click')
    await wrapper.get('[data-testid="agent-tree-create-agent"]').trigger('click')
    expect(panelVisible(wrapper)).toBe(false)

    for (const action of ['rename', 'delete']) {
      await trigger.trigger('click')
      await wrapper.get('[data-resource-key="beta"]').trigger('contextmenu', { clientX: 30, clientY: 40 })
      await flushPromises()
      document.querySelector<HTMLButtonElement>(`[data-testid="agent-row-${action}"]`)!.click()
      await flushPromises()
      expect(panelVisible(wrapper)).toBe(false)
    }

    expect(wrapper.emitted('import')).toHaveLength(1)
    expect(wrapper.emitted('export-workspace')).toHaveLength(1)
    expect(wrapper.emitted('create-team')).toHaveLength(1)
    expect(wrapper.emitted('create-agent')).toHaveLength(1)
    expect(wrapper.emitted('rename')).toEqual([['beta']])
    expect(wrapper.emitted('delete')).toEqual([['beta']])
  })
})
