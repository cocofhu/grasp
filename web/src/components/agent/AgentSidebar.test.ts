// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import AgentSidebar from './AgentSidebar.vue'

const wrappers: VueWrapper[] = []
function mountSidebar(props: Record<string, unknown> = {}) {
  const wrapper = mount(AgentSidebar, {
    attachTo: document.body,
    props: { nodes: [{ id: 'alpha', label: 'alpha' }, { id: 'beta', label: 'beta' }], activeKey: 'alpha', ...props },
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
    expect(header.querySelectorAll('button')).toHaveLength(2)
    expect(wrapper.get('input[type="search"]').attributes('aria-label')).toBe('搜索智能体')
    await wrapper.get('[data-resource-key="beta"]').trigger('click')
    expect(wrapper.emitted('select')).toEqual([['beta']])
    wrapper.unmount()
  })
  it('keeps import, export and team creation, without an Agent management entry', async () => {
    const wrapper = mountSidebar()
    expect(document.querySelector('[data-testid="agent-tree-manage"]')).toBeNull()
    await wrapper.get('[data-testid="agent-tree-create-agent"]').trigger('click')
    for (const action of ['import', 'export', 'create-team']) {
      await wrapper.get('[data-testid="agent-tree-more"]').trigger('click')
      expect(document.querySelector('[data-testid="agent-tree-manage"]')).toBeNull()
      menuAction(action).click()
      await flushPromises()
      expect(document.querySelector('[role="menu"]')).toBeNull()
    }
    expect(wrapper.emitted('open-manage')).toBeUndefined()
    expect(wrapper.emitted('import')).toHaveLength(1)
    expect(wrapper.emitted('export-workspace')).toEqual([[]])
    expect(wrapper.emitted('create-agent')).toEqual([[]])
    expect(wrapper.emitted('create-team')).toHaveLength(1)
    wrapper.unmount()
  })
  it('hides team creation only for embedded callers and closes its menus on request', async () => {
    const wrapper = mountSidebar({ hideCreateTeam: true })
    await wrapper.get('[data-testid="agent-tree-more"]').trigger('click')
    expect(document.querySelector('[data-testid="agent-tree-create-team"]')).toBeNull()
    expect(document.querySelector('[data-testid="agent-tree-manage"]')).toBeNull()
    expect(document.querySelectorAll('[role="menuitem"]')).toHaveLength(2)
    ;(wrapper.vm as unknown as { closeMenus: () => void }).closeMenus()
    await flushPromises()
    expect(document.querySelector('[role="menu"]')).toBeNull()
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
    expect(document.activeElement).toBe(menuAction('import'))
    const key = async (value: string) => {
      document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', { key: value, bubbles: true }))
      await flushPromises()
    }
    await key('ArrowDown')
    expect(document.activeElement).toBe(menuAction('export'))
    await key('End')
    expect(document.activeElement).toBe(menuAction('create-team'))
    await key('ArrowDown')
    expect(document.activeElement).toBe(menuAction('import'))
    await key('ArrowUp')
    expect(document.activeElement).toBe(menuAction('create-team'))
    await key('Home')
    expect(document.activeElement).toBe(menuAction('import'))
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
    menuAction('import').dispatchEvent(new Event('pointerdown', { bubbles: true }))
    await flushPromises()
    expect(document.querySelector('[role="menu"]')).not.toBeNull()
    document.body.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    await flushPromises()
    expect(document.querySelector('[role="menu"]')).toBeNull()
    expect(trigger.attributes('aria-expanded')).toBe('false')
  })
  it('shows a lock and no menu for a canonical Agent', async () => {
    const wrapper = mountSidebar({
      nodes: [
        { id: '需求澄清', label: '需求澄清', icon: 'robot', locked: true, lockTitle: '工作流需要此 Agent，不能删除或重命名。' },
        { id: '自建', label: '自建', icon: 'robot' },
      ],
    })
    expect(wrapper.get('[data-resource-key="需求澄清"]').find('[data-testid="resource-lock"]').attributes('title')).toBe('工作流需要此 Agent，不能删除或重命名。')
    expect(wrapper.get('[data-resource-key="自建"]').find('[data-testid="resource-lock"]').exists()).toBe(false)
    const lockedEvent = new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 40, clientY: 40 })
    wrapper.get('[data-resource-key="需求澄清"]').element.dispatchEvent(lockedEvent)
    await flushPromises()
    expect(lockedEvent.defaultPrevented).toBe(true)
    expect(document.querySelector('[data-testid="agent-row-menu"]')).toBeNull()
    expect(wrapper.emitted('rename')).toBeUndefined()
    expect(wrapper.emitted('delete')).toBeUndefined()
    expect(wrapper.emitted('select')).toBeUndefined()
    wrapper.unmount()
  })

  it('right-click on an operable row offers only rename and delete without changing selection', async () => {
    const wrapper = mountSidebar({ pinned: [{ id: '__workspace_defaults__', label: 'Agent 默认配置' }] })
    const pinnedEvent = new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 12, clientY: 12 })
    wrapper.get('[data-resource-key="__workspace_defaults__"]').element.dispatchEvent(pinnedEvent)
    await flushPromises()
    expect(document.querySelector('[data-testid="agent-row-menu"]')).toBeNull()

    const event = new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 48, clientY: 64 })
    wrapper.get('[data-resource-key="beta"]').element.dispatchEvent(event)
    await flushPromises()
    expect(event.defaultPrevented).toBe(true)
    expect(wrapper.emitted('select')).toBeUndefined()
    const menu = document.querySelector<HTMLElement>('[data-testid="agent-row-menu"]')!
    expect(menu).toBeTruthy()
    expect(menu.className).toContain('rounded-lg')
    expect(menu.className).toContain('border-line')
    expect(menu.className).toContain('bg-elevated')
    const items = Array.from(menu.querySelectorAll<HTMLButtonElement>('[role="menuitem"]'))
    expect(items.map((item) => item.textContent?.trim())).toEqual(['重命名', '删除'])
    expect(items[1]?.className).toContain('agent-row-menu__item--danger')
    expect(document.body.textContent).not.toContain('重命名（前往管理）')
    expect(document.body.textContent).not.toContain('Agent 管理')
    wrapper.unmount()
  })

  it('emits rename or delete for the row that was right-clicked', async () => {
    const wrapper = mountSidebar()
    await wrapper.get('[data-resource-key="beta"]').trigger('contextmenu', { clientX: 30, clientY: 40 })
    document.querySelector<HTMLButtonElement>('[data-testid="agent-row-rename"]')!.click()
    await flushPromises()
    expect(wrapper.emitted('rename')).toEqual([['beta']])
    expect(wrapper.emitted('select')).toBeUndefined()
    expect(document.querySelector('[data-testid="agent-row-menu"]')).toBeNull()

    await wrapper.get('[data-resource-key="alpha"]').trigger('contextmenu', { clientX: 30, clientY: 40 })
    document.querySelector<HTMLButtonElement>('[data-testid="agent-row-delete"]')!.click()
    await flushPromises()
    expect(wrapper.emitted('delete')).toEqual([['alpha']])
    expect(wrapper.emitted('select')).toBeUndefined()
    wrapper.unmount()
  })

  it('closes the row menu on outside press, Escape, opening more, or scrolling the list', async () => {
    const wrapper = mountSidebar()
    const open = async () => {
      await wrapper.get('[data-resource-key="beta"]').trigger('contextmenu', { clientX: 30, clientY: 40 })
      await flushPromises()
      expect(document.querySelector('[data-testid="agent-row-menu"]')).not.toBeNull()
    }
    await open()
    document.body.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    await flushPromises()
    expect(document.querySelector('[data-testid="agent-row-menu"]')).toBeNull()

    await open()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushPromises()
    expect(document.querySelector('[data-testid="agent-row-menu"]')).toBeNull()

    await open()
    await wrapper.get('[data-testid="agent-tree-more"]').trigger('click')
    await flushPromises()
    expect(document.querySelector('[data-testid="agent-row-menu"]')).toBeNull()
    expect(document.querySelector('[data-testid="agent-tree-more-menu"]')).not.toBeNull()

    await open()
    wrapper.get('.scroll-area').element.dispatchEvent(new Event('scroll'))
    await flushPromises()
    expect(document.querySelector('[data-testid="agent-row-menu"]')).toBeNull()
    wrapper.unmount()
  })

  it('clamps the row menu inside the viewport', async () => {
    vi.stubGlobal('innerWidth', 224)
    vi.stubGlobal('innerHeight', 160)
    const wrapper = mountSidebar()
    await wrapper.get('[data-resource-key="beta"]').trigger('contextmenu', { clientX: 210, clientY: 150 })
    await flushPromises()
    const menu = document.querySelector<HTMLElement>('[data-testid="agent-row-menu"]')!
    vi.spyOn(menu, 'getBoundingClientRect').mockReturnValue(new DOMRect(0, 0, 148, 72))
    window.dispatchEvent(new Event('resize'))
    await flushPromises()
    expect(menu.style.left).toBe('68px')
    expect(menu.style.top).toBe('80px')
    wrapper.unmount()
  })
})
