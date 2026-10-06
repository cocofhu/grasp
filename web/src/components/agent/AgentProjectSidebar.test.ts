// @vitest-environment happy-dom
import { afterEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import AgentProjectSidebar from './AgentProjectSidebar.vue'
import type { ProjectTreeNode } from '@/components/ui/projectTree'

const nodes: ProjectTreeNode[] = [
  { id: 'p1', label: 'Alpha', count: 2, children: [{ id: 'a1', label: 'a1' }, { id: 'a2', label: 'a2' }] },
  { id: 'p2', label: 'Beta', count: 0, children: [] },
]

const ProjectTreeStub = {
  name: 'ProjectTree',
  props: ['nodes', 'activeKey', 'title', 'total', 'searchPlaceholder', 'emptyText', 'storageKey'],
  emits: ['select', 'contextmenu'],
  template: `<div data-testid="tree-stub" :data-total="total" :data-title="title">
    <slot name="actions" />
    <button data-testid="row-p1" @click="$emit('select', 'p:p1')" @contextmenu="$emit('contextmenu', $event, 'p:p1')" />
    <button data-testid="row-a1" @click="$emit('select', 'c:p1:a1')" @contextmenu="$emit('contextmenu', $event, 'c:p1:a1')" />
  </div>`,
}

function mountSidebar(props: Partial<{ collapsed: boolean; hideCreateTeam: boolean }> = {}) {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
  return mount(AgentProjectSidebar, {
    attachTo: document.body,
    props: { nodes, activeKey: 'c:p1:a1', collapsed: false, ...props },
    global: { plugins: [i18n], stubs: { Icon: true, ProjectTree: ProjectTreeStub } },
  })
}

function ctxButton(action: string) {
  return document.body.querySelector(`[data-tree-ctx-action="${action}"]`) as HTMLButtonElement | null
}

afterEach(() => {
  document.body.innerHTML = ''
})

describe('AgentProjectSidebar', () => {
  it('renders the project tree with the agent total and forwards selection', async () => {
    const w = mountSidebar()
    const tree = w.get('[data-testid="tree-stub"]')
    expect(tree.attributes('data-total')).toBe('2')
    expect(tree.attributes('data-title')).toBe(pages.pages.agentStudio.tree.title)
    await w.get('[data-testid="row-a1"]').trigger('click')
    expect(w.emitted('select')).toEqual([['c:p1:a1']])
    w.unmount()
  })

  it('header actions emit manage / import / create / team / collapse', async () => {
    const w = mountSidebar()
    await w.get('[data-testid="agent-tree-manage"]').trigger('click')
    await w.get('[data-testid="agent-tree-import"]').trigger('click')
    await w.get('[data-testid="agent-tree-create-agent"]').trigger('click')
    await w.get('[data-testid="agent-tree-create-team"]').trigger('click')
    expect(w.emitted('open-manage')).toEqual([[]])
    expect(w.emitted('import')).toHaveLength(1)
    expect(w.emitted('create-agent')).toEqual([[]])
    expect(w.emitted('create-team')).toHaveLength(1)
    w.unmount()
  })

  it('hides create team when requested and shows only the expand button when collapsed', () => {
    const hidden = mountSidebar({ hideCreateTeam: true })
    expect(hidden.find('[data-testid="agent-tree-create-team"]').exists()).toBe(false)
    hidden.unmount()
    const collapsed = mountSidebar({ collapsed: true })
    expect(collapsed.find('[data-testid="tree-stub"]').exists()).toBe(false)
    collapsed.unmount()
  })

  it('project context menu creates, exports, imports, and clears sensitive config for that project', async () => {
    const w = mountSidebar()
    const open = async () => {
      await w.get('[data-testid="row-p1"]').trigger('contextmenu')
      expect(document.body.querySelector('[data-tree-ctx-menu]')?.getAttribute('data-tree-ctx-kind')).toBe('project')
    }
    await open()
    expect(ctxButton('renameViaManage')).toBeNull()
    ctxButton('createAgent')!.click()
    await w.vm.$nextTick()
    expect(document.body.querySelector('[data-tree-ctx-menu]')).toBeNull()
    await open()
    ctxButton('export')!.click()
    await open()
    ctxButton('import')!.click()
    await open()
    expect(ctxButton('clearSensitive')).toBeNull()
    expect(w.emitted('create-agent')).toEqual([['p1']])
    expect(w.emitted('export-project')).toEqual([['p1']])
    expect(w.emitted('import-project')).toEqual([['p1']])
    w.unmount()
  })

  it('agent context menu only offers rename via management', async () => {
    const w = mountSidebar()
    await w.get('[data-testid="row-a1"]').trigger('contextmenu')
    expect(document.body.querySelector('[data-tree-ctx-menu]')?.getAttribute('data-tree-ctx-kind')).toBe('agent')
    expect(ctxButton('export')).toBeNull()
    ctxButton('renameViaManage')!.click()
    expect(w.emitted('open-manage')).toEqual([['a1']])
    w.unmount()
  })

  it('closes the context menu from the backdrop', async () => {
    const w = mountSidebar()
    await w.get('[data-testid="row-p1"]').trigger('contextmenu')
    ;(document.body.querySelector('[data-tree-ctx-backdrop]') as HTMLElement).click()
    await w.vm.$nextTick()
    expect(document.body.querySelector('[data-tree-ctx-menu]')).toBeNull()
    w.unmount()
  })
})
