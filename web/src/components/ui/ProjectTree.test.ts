// @vitest-environment happy-dom
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { createI18n } from 'vue-i18n'
import ProjectTree from './ProjectTree.vue'
import {
  childKey,
  filterProjectTree,
  parseTreeKey,
  projectKey,
  type ProjectTreeNode,
} from './projectTree'

const i18n = createI18n({ legacy: false, locale: 'en', missingWarn: false, fallbackWarn: false })

const nodes: ProjectTreeNode[] = [
  {
    id: 'alpha',
    label: 'Alpha',
    count: 5,
    children: [
      { id: 'wf1', label: 'Build pipeline', count: 3 },
      { id: 'wf2', label: 'Release', count: 2 },
    ],
  },
  { id: 'beta', label: 'Beta', count: 1, children: [{ id: 'wf3', label: 'Nightly', count: 1 }] },
  { id: 'gamma', label: 'Gamma', count: 0 },
]

const STORAGE = 'test.projectTree'

function mountTree(props: Partial<InstanceType<typeof ProjectTree>['$props']> = {}) {
  return mount(ProjectTree, {
    props: { nodes, activeKey: '', title: 'Projects', storageKey: STORAGE, ...props },
    global: { plugins: [i18n] },
    attachTo: document.body,
  })
}

function keys(wrapper: ReturnType<typeof mountTree>) {
  return wrapper.findAll('[data-tree-key]').map((el) => el.attributes('data-tree-key'))
}

beforeEach(() => localStorage.clear())
afterEach(() => {
  document.body.innerHTML = ''
})

describe('projectTree helpers', () => {
  it('builds and parses keys', () => {
    expect(projectKey('a')).toBe('p:a')
    expect(childKey('a', 'b')).toBe('c:a:b')
    expect(parseTreeKey('p:a')).toEqual({ projectId: 'a' })
    expect(parseTreeKey('c:a:b')).toEqual({ projectId: 'a', childId: 'b' })
    expect(parseTreeKey('c:a:b:c')).toEqual({ projectId: 'a', childId: 'b:c' })
    expect(parseTreeKey('')).toBeNull()
    expect(parseTreeKey('p:')).toBeNull()
    expect(parseTreeKey('c:a')).toBeNull()
    expect(parseTreeKey('x:a')).toBeNull()
  })

  it('filters by project and child labels', () => {
    const byProject = filterProjectTree(nodes, 'alp')
    expect(byProject.nodes.map((n) => n.id)).toEqual(['alpha'])
    expect(byProject.nodes[0].children).toHaveLength(2)
    expect(byProject.forcedOpen.size).toBe(0)

    const byChild = filterProjectTree(nodes, 'night')
    expect(byChild.nodes.map((n) => n.id)).toEqual(['beta'])
    expect([...byChild.forcedOpen]).toEqual(['beta'])
  })
})

describe('ProjectTree', () => {
  it('renders header, total and actions slot', () => {
    const wrapper = mount(ProjectTree, {
      props: { nodes, activeKey: '', title: 'Projects', total: 6, storageKey: STORAGE },
      slots: { actions: '<button data-test-action>+</button>' },
      global: { plugins: [i18n] },
    })
    expect(wrapper.text()).toContain('Projects')
    expect(wrapper.get('[data-testid="project-tree-total"]').text()).toBe('6')
    expect(wrapper.find('[data-test-action]').exists()).toBe(true)
  })

  it('expands only the active project by default', () => {
    const wrapper = mountTree({ activeKey: childKey('beta', 'wf3') })
    expect(keys(wrapper)).toEqual(['p:alpha', 'p:beta', 'c:beta:wf3', 'p:gamma'])
    expect(wrapper.get('[data-tree-key="c:beta:wf3"]').attributes('aria-selected')).toBe('true')
  })

  it('expands the first active project even when it arrives after mount', async () => {
    const wrapper = mountTree()
    expect(keys(wrapper)).toEqual(['p:alpha', 'p:beta', 'p:gamma'])
    await wrapper.setProps({ activeKey: projectKey('alpha') })
    expect(keys(wrapper)).toEqual(['p:alpha', 'c:alpha:wf1', 'c:alpha:wf2', 'p:beta', 'p:gamma'])
    await wrapper.setProps({ activeKey: projectKey('beta') })
    expect(keys(wrapper)).not.toContain('c:beta:wf3')
  })

  it('clicking a project selects and expands it; chevron only toggles', async () => {
    const wrapper = mountTree()
    await wrapper.get('[data-tree-key="p:alpha"]').trigger('click')
    expect(wrapper.emitted('select')).toEqual([['p:alpha']])
    expect(keys(wrapper)).toContain('c:alpha:wf1')

    await wrapper.get('[data-tree-key="p:alpha"] [data-tree-toggle]').trigger('click')
    expect(keys(wrapper)).not.toContain('c:alpha:wf1')
    expect(wrapper.emitted('select')).toHaveLength(1)
  })

  it('persists collapse state under storageKey', async () => {
    const first = mountTree()
    await first.get('[data-tree-key="p:beta"] [data-tree-toggle]').trigger('click')
    expect(JSON.parse(localStorage.getItem(STORAGE) || '[]')).toEqual(['beta'])
    first.unmount()

    const second = mountTree({ activeKey: projectKey('alpha') })
    expect(keys(second)).toEqual(['p:alpha', 'p:beta', 'c:beta:wf3', 'p:gamma'])
  })

  it('search filters and auto-expands the matching child project', async () => {
    const wrapper = mountTree()
    await wrapper.get('[data-testid="project-tree-search"]').setValue('release')
    expect(keys(wrapper)).toEqual(['p:alpha', 'c:alpha:wf2'])

    await wrapper.get('[data-testid="project-tree-search"]').setValue('zzz')
    expect(wrapper.find('[data-testid="project-tree-no-match"]').exists()).toBe(true)

    await wrapper.get('[data-testid="project-tree-clear"]').trigger('click')
    expect(keys(wrapper)).toEqual(['p:alpha', 'p:beta', 'p:gamma'])
  })

  it('selects children and forwards contextmenu', async () => {
    const wrapper = mountTree({ activeKey: projectKey('alpha') })
    await wrapper.get('[data-tree-key="c:alpha:wf2"]').trigger('click')
    expect(wrapper.emitted('select')).toEqual([['c:alpha:wf2']])
    await wrapper.get('[data-tree-key="p:beta"]').trigger('contextmenu')
    const ctx = wrapper.emitted('contextmenu')!
    expect(ctx[0][1]).toBe('p:beta')
    expect(ctx[0][0]).toBeInstanceOf(MouseEvent)
  })

  it('supports keyboard navigation', async () => {
    const wrapper = mountTree({ activeKey: projectKey('alpha') })
    const tree = wrapper.get('[role="tree"]')
    expect(wrapper.get('[data-tree-key="p:alpha"]').attributes('tabindex')).toBe('0')

    await tree.trigger('keydown', { key: 'ArrowDown' })
    expect(wrapper.get('[data-tree-key="c:alpha:wf1"]').attributes('tabindex')).toBe('0')

    await tree.trigger('keydown', { key: 'Enter' })
    expect(wrapper.emitted('select')).toEqual([['c:alpha:wf1']])

    await tree.trigger('keydown', { key: 'ArrowLeft' })
    expect(wrapper.get('[data-tree-key="p:alpha"]').attributes('tabindex')).toBe('0')
    await tree.trigger('keydown', { key: 'ArrowLeft' })
    expect(keys(wrapper)).not.toContain('c:alpha:wf1')
    await tree.trigger('keydown', { key: 'ArrowRight' })
    expect(keys(wrapper)).toContain('c:alpha:wf1')

    await tree.trigger('keydown', { key: 'End' })
    await tree.trigger('keydown', { key: 'Enter' })
    expect(wrapper.emitted('select')!.at(-1)).toEqual(['p:gamma'])
  })

  it('shows skeleton while loading and empty state when no nodes', () => {
    const loading = mountTree({ nodes: [], loading: true })
    expect(loading.findAll('.app-skeleton__row')).toHaveLength(3)
    expect(loading.find('.app-skeleton__card').exists()).toBe(false)

    const empty = mountTree({ nodes: [], emptyText: 'Nothing here' })
    expect(empty.text()).toContain('Nothing here')
  })
})
