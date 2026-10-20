// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'

const apiMocks = vi.hoisted(() => ({
  listProjectRunTags: vi.fn(),
}))

vi.mock('@/lib/composables/useWorkspaceContext', async () => {
  const { ref } = await import('vue')
  const workspace = ref({ id: 'p1', name: '工作区', variables: [] })
  return { useWorkspaceContext: () => ({ workspace, workspaceId: ref(workspace.value.id), ensureLoaded: async () => workspace.value, reload: async () => workspace.value }) }
})


vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      listProjectRunTags: apiMocks.listProjectRunTags,
    },
  }
})

import TagFilter from './TagFilter.vue'

function mountFilter(
  props: { modelValue?: string[]; projectId?: string; open?: boolean } = {},
) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common } },
  })
  return mount(TagFilter, {
    props: {
      modelValue: props.modelValue ?? [],
      projectId: props.projectId ?? '',
      open: props.open,
    },
    global: { plugins: [i18n], stubs: { Icon: true } },
  })
}

beforeEach(() => {
  vi.clearAllMocks()
  apiMocks.listProjectRunTags.mockResolvedValue({ tags: ['prod', 'canary', 'hotfix'] })
})

describe('TagFilter', () => {
  it('trigger uses shared toolbar-control sizing (g3.3 g4.4)', () => {
    const wrapper = mountFilter()
    const trigger = wrapper.get('[data-testid="tag-filter-trigger"]')
    const cls = trigger.classes().join(' ')
    expect(trigger.classes()).toContain('toolbar-control')
    expect(cls).not.toContain('min-h-[44px]')
    expect(cls).not.toContain('px-3')
    expect(cls).not.toContain('py-1.5')
    wrapper.unmount()
  })

  it('shows label trigger without count when empty', () => {
    const wrapper = mountFilter()
    expect(wrapper.get('[data-testid="tag-filter-trigger"]').text()).toContain('标签')
    expect(wrapper.find('[data-testid="tag-filter-count"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows selected count on trigger', () => {
    const wrapper = mountFilter({ modelValue: ['prod', 'canary'] })
    expect(wrapper.get('[data-testid="tag-filter-count"]').text()).toBe('2')
    wrapper.unmount()
  })

  it('loads stock tags and toggles selection', async () => {
    const wrapper = mountFilter({ projectId: 'p1', open: true })
    await flushPromises()
    expect(apiMocks.listProjectRunTags).toHaveBeenCalledWith('p1')

    const suggestions = wrapper.get('[data-testid="tag-filter-suggestions"]').findAll('button')
    expect(suggestions.length).toBe(3)
    await suggestions[0]!.trigger('click')
    expect(wrapper.emitted('update:modelValue')?.[0]?.[0]).toEqual(['prod'])

    await wrapper.setProps({ modelValue: ['prod'] })
    await suggestions[0]!.trigger('click')
    const emitted = wrapper.emitted('update:modelValue') ?? []
    expect(emitted[emitted.length - 1]?.[0]).toEqual([])
    wrapper.unmount()
  })

  it('adds valid tag on Enter and shows selected chip', async () => {
    const wrapper = mountFilter({ projectId: 'p1', open: true, modelValue: [] })
    await flushPromises()
    const input = wrapper.get('[data-testid="tag-filter-input"]')
    await input.setValue('nightly')
    await input.trigger('keydown', { key: 'Enter' })
    expect(wrapper.emitted('update:modelValue')?.[0]?.[0]).toEqual(['nightly'])
    wrapper.unmount()
  })

  it('blocks illegal tag with red error and does not emit', async () => {
    const wrapper = mountFilter({ open: true })
    const input = wrapper.get('[data-testid="tag-filter-input"]')
    await input.setValue('owner:alice')
    await input.trigger('keydown', { key: 'Enter' })
    expect(wrapper.find('[data-testid="tag-filter-error"]').exists()).toBe(true)
    expect(wrapper.emitted('update:modelValue')).toBeFalsy()
    wrapper.unmount()
  })

  it('loads workspace tags without requiring a project prop', async () => {
    const wrapper = mountFilter({ open: true, projectId: '' })
    await flushPromises()
    expect(apiMocks.listProjectRunTags).toHaveBeenCalledWith('p1')
    expect(wrapper.text()).not.toMatch(/选择项目/)
    wrapper.unmount()
  })

  it('shows no-stock empty state when project has no tags', async () => {
    apiMocks.listProjectRunTags.mockResolvedValue({ tags: [] })
    const wrapper = mountFilter({ open: true, projectId: 'p1' })
    await flushPromises()
    expect(wrapper.get('[data-testid="tag-filter-empty"]').text()).toMatch(/暂无存量/)
    wrapper.unmount()
  })

  it('removes selected chip on click', async () => {
    const wrapper = mountFilter({ open: true, modelValue: ['prod'] })
    await wrapper.get('[data-testid="tag-filter-selected"] button').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.[0]?.[0]).toEqual([])
    wrapper.unmount()
  })

  it('emits update:open when controlled', async () => {
    const wrapper = mountFilter({ open: false })
    await wrapper.get('[data-testid="tag-filter-trigger"]').trigger('click')
    expect(wrapper.emitted('update:open')?.[0]?.[0]).toBe(true)
    wrapper.unmount()
  })

  it('filters suggestions by draft input', async () => {
    const wrapper = mountFilter({ projectId: 'p1', open: true })
    await flushPromises()
    await wrapper.get('[data-testid="tag-filter-input"]').setValue('can')
    const suggestions = wrapper.get('[data-testid="tag-filter-suggestions"]').findAll('button')
    expect(suggestions).toHaveLength(1)
    expect(suggestions[0]!.text()).toContain('canary')
    wrapper.unmount()
  })

  it('degrades to empty stock and shows error when listProjectRunTags rejects (404)', async () => {
    apiMocks.listProjectRunTags.mockRejectedValue(new Error('not found'))
    const wrapper = mountFilter({ projectId: 'proj-missing', open: true })
    await flushPromises()
    expect(apiMocks.listProjectRunTags).toHaveBeenCalledWith('p1')
    expect(wrapper.find('[data-testid="tag-filter-stock-error"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="tag-filter-suggestions"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('degrades to empty stock when tags is a non-array payload', async () => {
    apiMocks.listProjectRunTags.mockResolvedValue({ tags: { items: ['x'] } as unknown as string[] })
    const wrapper = mountFilter({ projectId: 'p1', open: true })
    await flushPromises()
    expect(wrapper.get('[data-testid="tag-filter-empty"]').text()).toMatch(/暂无存量/)
    wrapper.unmount()
  })
})
