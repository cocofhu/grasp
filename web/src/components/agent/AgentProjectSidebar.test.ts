// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import AgentProjectSidebar from './AgentProjectSidebar.vue'

const wrappers: VueWrapper[] = []
function mountSidebar(props: Record<string, unknown> = {}) {
  const wrapper = mount(AgentProjectSidebar, {
    attachTo: document.body,
    props: { nodes: [{ id: 'alpha', label: 'alpha' }, { id: 'beta', label: 'beta' }], activeKey: 'alpha', collapsed: false, workspaceId: 'workspace', ...props },
    global: { plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })], stubs: { Icon: true } },
  })
  wrappers.push(wrapper)
  return wrapper
}
afterEach(() => {
  for (const wrapper of wrappers.splice(0)) if (wrapper.exists()) wrapper.unmount()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

function menuAction(action: string): HTMLButtonElement {
  const button = document.querySelector<HTMLButtonElement>(`[data-testid="agent-tree-${action}"]`)
  if (!button) throw new Error(`Missing Agent action: ${action}`)
  return button
}

describe('Agent list sidebar', () => {
  it('lists all Agents directly and forwards selection', async () => {
    const wrapper = mountSidebar()
    expect(wrapper.findAll('[data-resource-key]')).toHaveLength(2)
    expect(wrapper.get('[data-testid="resource-list-title"]').attributes('title')).toBe('智能体')
    const header = wrapper.get('[data-testid="resource-list"]').element.firstElementChild!
    expect(header.querySelectorAll('button')).toHaveLength(3)
    expect(wrapper.get('input[type="search"]').attributes('aria-label')).toBe('搜索智能体')
    await wrapper.get('[data-resource-key="beta"]').trigger('click')
    expect(wrapper.emitted('select')).toEqual([['beta']])
    wrapper.unmount()
  })
  it('keeps management, import, export and team creation for the workspace', async () => {
    const wrapper = mountSidebar()
    expect(document.querySelector('[data-testid="agent-tree-manage"]')).toBeNull()
    await wrapper.get('[data-testid="agent-tree-create-agent"]').trigger('click')
    for (const action of ['manage', 'import', 'export', 'create-team']) {
      await wrapper.get('[data-testid="agent-tree-more"]').trigger('click')
      menuAction(action).click()
      await flushPromises()
      expect(document.querySelector('[role="menu"]')).toBeNull()
    }
    expect(wrapper.emitted('open-manage')).toEqual([[]])
    expect(wrapper.emitted('import')).toHaveLength(1)
    expect(wrapper.emitted('export-project')).toEqual([['workspace']])
    expect(wrapper.emitted('create-agent')).toEqual([[]])
    expect(wrapper.emitted('create-team')).toHaveLength(1)
    wrapper.unmount()
  })
  it('hides team creation only for embedded callers and closes the popup when collapsed', async () => {
    const wrapper = mountSidebar({ hideCreateTeam: true })
    await wrapper.get('[data-testid="agent-tree-more"]').trigger('click')
    expect(document.querySelector('[data-testid="agent-tree-create-team"]')).toBeNull()
    expect(document.querySelectorAll('[role="menuitem"]')).toHaveLength(3)
    await wrapper.get('[data-testid="agent-tree-collapse"]').trigger('click')
    expect(wrapper.emitted('toggle-collapsed')).toEqual([[]])
    await wrapper.setProps({ collapsed: true })
    expect(document.querySelector('[role="menu"]')).toBeNull()
    expect(wrapper.find('[data-testid="resource-list"]').exists()).toBe(false)
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('toggle-collapsed')).toHaveLength(2)
  })

  it('supports menu keyboard navigation and restores focus on Escape', async () => {
    const wrapper = mountSidebar()
    const trigger = wrapper.get('[data-testid="agent-tree-more"]')
    expect(trigger.attributes('aria-label')).toBe('更多操作')
    expect(trigger.attributes('aria-haspopup')).toBe('menu')
    expect(trigger.attributes('aria-expanded')).toBe('false')
    await trigger.trigger('keydown', { key: 'ArrowDown' })
    await flushPromises()
    const menu = document.querySelector<HTMLElement>('[role="menu"]')!
    expect(menu.parentElement).toBe(document.body)
    expect(menu.getAttribute('aria-labelledby')).toBe(trigger.attributes('id'))
    expect(trigger.attributes('aria-controls')).toBe(menu.id)
    expect(trigger.attributes('aria-expanded')).toBe('true')
    expect(document.activeElement).toBe(menuAction('manage'))
    const key = async (value: string) => {
      document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', { key: value, bubbles: true }))
      await flushPromises()
    }
    await key('ArrowDown')
    expect(document.activeElement).toBe(menuAction('import'))
    await key('End')
    expect(document.activeElement).toBe(menuAction('create-team'))
    await key('ArrowDown')
    expect(document.activeElement).toBe(menuAction('manage'))
    await key('ArrowUp')
    expect(document.activeElement).toBe(menuAction('create-team'))
    await key('Home')
    expect(document.activeElement).toBe(menuAction('manage'))
    await key('Escape')
    expect(document.querySelector('[role="menu"]')).toBeNull()
    expect(document.activeElement).toBe(trigger.element)
    expect(trigger.attributes('aria-expanded')).toBe('false')
    await trigger.trigger('keydown', { key: 'ArrowUp' })
    await flushPromises()
    expect(document.activeElement).toBe(menuAction('create-team'))
    const tab = new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true })
    document.activeElement!.dispatchEvent(tab)
    await flushPromises()
    expect(tab.defaultPrevented).toBe(false)
    expect(document.querySelector('[role="menu"]')).toBeNull()
    expect(document.activeElement).toBe(trigger.element)
    await trigger.trigger('click')
    await wrapper.setProps({ workspaceId: 'another-workspace' })
    expect(document.querySelector('[role="menu"]')).toBeNull()
  })

  it('closes on an outside press and clamps the teleported popup within the viewport', async () => {
    vi.stubGlobal('innerWidth', 224)
    vi.stubGlobal('innerHeight', 160)
    const wrapper = mountSidebar()
    const trigger = wrapper.get('[data-testid="agent-tree-more"]')
    vi.spyOn(trigger.element, 'getBoundingClientRect').mockReturnValue(new DOMRect(210, 145, 28, 28))
    await trigger.trigger('click')
    await flushPromises()
    const menu = document.querySelector<HTMLElement>('[role="menu"]')!
    vi.spyOn(menu, 'getBoundingClientRect').mockReturnValue(new DOMRect(0, 0, 208, 144))
    window.dispatchEvent(new Event('resize'))
    await flushPromises()
    expect(menu.style.left).toBe('8px')
    expect(menu.style.top).toBe('8px')
    expect(menu.style.width).toBe('208px')
    menuAction('manage').dispatchEvent(new Event('pointerdown', { bubbles: true }))
    await flushPromises()
    expect(document.querySelector('[role="menu"]')).not.toBeNull()
    document.body.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    await flushPromises()
    expect(document.querySelector('[role="menu"]')).toBeNull()
    expect(trigger.attributes('aria-expanded')).toBe('false')
  })
  it('opens management for the chosen Agent from its context menu', async () => {
    const wrapper = mountSidebar()
    await wrapper.get('[data-resource-key="beta"]').trigger('contextmenu')
    const rename = Array.from(document.body.querySelectorAll('button')).find((button) => button.textContent?.includes(pages.pages.agentStudio.tree.renameViaManage))!
    rename.click()
    await flushPromises()
    expect(wrapper.emitted('open-manage')).toEqual([['beta']])
    wrapper.unmount()
  })
})
