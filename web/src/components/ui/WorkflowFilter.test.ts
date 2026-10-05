// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'

const apiMocks = vi.hoisted(() => ({
  listWorkflows: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      listWorkflows: apiMocks.listWorkflows,
    },
  }
})

import WorkflowFilter from './WorkflowFilter.vue'

beforeEach(() => {
  vi.clearAllMocks()
  apiMocks.listWorkflows.mockResolvedValue([
    { id: 'wf-1', name: '工作流 A', status: 'published', nodes: [], edges: [] },
  ])
})

describe('WorkflowFilter', () => {
  it('trigger uses shared toolbar-control sizing (g3.3 g4.4)', async () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common } },
    })
    const wrapper = mount(WorkflowFilter, {
      props: { modelValue: '' },
      global: { plugins: [i18n], stubs: { Icon: true } },
    })
    await flushPromises()
    const trigger = wrapper.get('[data-testid="workflow-filter"] button')
    const cls = trigger.classes().join(' ')
    expect(trigger.classes()).toContain('toolbar-control')
    expect(cls).not.toContain('min-h-[44px]')
    expect(cls).not.toContain('px-3')
    expect(cls).not.toContain('py-1.5')
    wrapper.unmount()
  })

  it('loads workflows and shows all label by default', async () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common } },
    })
    const wrapper = mount(WorkflowFilter, {
      props: { modelValue: '' },
      global: { plugins: [i18n], stubs: { Icon: true } },
    })
    await flushPromises()
    expect(apiMocks.listWorkflows).toHaveBeenCalled()
    expect(wrapper.text().length).toBeGreaterThan(0)
    wrapper.unmount()
  })
})
