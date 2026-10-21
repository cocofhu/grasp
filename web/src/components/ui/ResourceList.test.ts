// @vitest-environment happy-dom
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ResourceList from './ResourceList.vue'

const base = { title: '智能体', nodes: [{ id: 'alpha', label: 'alpha' }], emptyText: '暂无工作区，请先创建工作区。' }

describe('ResourceList empty copy', () => {
  it('shows the empty text when there are no items and no search', () => {
    const w = mount(ResourceList, { props: { ...base, nodes: [] }, global: { stubs: { Icon: true } } })
    expect(w.get('[data-testid="resource-list-empty"]').text()).toBe('暂无工作区，请先创建工作区。')
  })

  it('a search with no match shows the no-match text, not the empty text', async () => {
    const w = mount(ResourceList, { props: { ...base, noMatchText: '无匹配 Agent', pinned: [{ id: 'd', label: 'Agent 默认配置' }] }, global: { stubs: { Icon: true } } })
    await w.get('input[type="search"]').setValue('zzz')
    expect(w.get('[data-testid="resource-list-pinned"]').text()).toContain('Agent 默认配置')
    expect(w.get('[data-testid="resource-list-empty"]').text()).toBe('无匹配 Agent')
  })

  it('without no-match copy, a pinned list shows nothing under the pinned item while searching', async () => {
    const w = mount(ResourceList, { props: { ...base, pinned: [{ id: 'd', label: 'Agent 默认配置' }] }, global: { stubs: { Icon: true } } })
    await w.get('input[type="search"]').setValue('zzz')
    expect(w.find('[data-testid="resource-list-empty"]').exists()).toBe(false)
  })

  it('lists without pinned items keep their empty text while searching', async () => {
    const w = mount(ResourceList, { props: base, global: { stubs: { Icon: true } } })
    await w.get('input[type="search"]').setValue('zzz')
    expect(w.get('[data-testid="resource-list-empty"]').text()).toBe('暂无工作区，请先创建工作区。')
  })
})
