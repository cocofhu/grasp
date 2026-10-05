// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { defineComponent, h } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi, beforeEach } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { Run } from '@/lib/shared/types'

const listRuns = vi.fn()

vi.mock('@/lib/api/api', () => ({
  api: {
    listRuns: (...args: unknown[]) => listRuns(...args),
  },
}))

import RunBoardStatusListModal from './RunBoardStatusListModal.vue'

const AppModalStub = defineComponent({
  name: 'AppModal',
  props: {
    open: Boolean,
    title: String,
    width: Number,
    closeOnEsc: Boolean,
  },
  setup(props, { slots }) {
    return () =>
      props.open
        ? h('div', { role: 'dialog', 'data-testid': 'stub-modal' }, [
            h('div', slots.header?.()),
            h('div', slots.default?.()),
            h('div', slots.footer?.()),
          ])
        : null
  },
})

function stubRun(partial: Partial<Run> & Pick<Run, 'id' | 'status'>): Run {
  return {
    workflowId: 'wf',
    workflowName: 'Workflow',
    trigger: 'manual',
    startedAt: '2026-07-18T12:00:00Z',
    durationSec: 60,
    progress: 40,
    currentNodeLabel: '实现',
    priority: 'normal',
    nodeRuns: {},
    artifacts: [],
    ...partial,
  }
}

function mountModal(props: Record<string, unknown> = {}) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(RunBoardStatusListModal, {
    props: {
      open: true,
      projectId: 'proj-1',
      status: 'completed',
      title: '已完成',
      ...props,
    },
    global: {
      plugins: [i18n],
      stubs: {
        AppModal: AppModalStub,
        AppButton: defineComponent({
          name: 'AppButton',
          inheritAttrs: false,
          emits: ['click'],
          setup(_, { slots, emit, attrs }) {
            return () =>
              h(
                'button',
                {
                  type: 'button',
                  ...attrs,
                  onClick: (e: MouseEvent) => emit('click', e),
                },
                slots.default?.(),
              )
          },
        }),
        PriorityBadge: { template: '<span data-testid="priority-badge" />' },
      },
    },
    attachTo: document.body,
  })
}

describe('RunBoardStatusListModal', () => {
  beforeEach(() => {
    listRuns.mockReset()
  })

  it('loads first page with pageSize 20 and shows rows + range', async () => {
    listRuns.mockResolvedValue({
      items: [
        stubRun({ id: 'run-1', status: 'completed', title: '完成-1', progress: 100 }),
        stubRun({ id: 'run-2', status: 'completed', title: '完成-2', progress: 100 }),
      ],
      total: 45,
      page: 1,
      pageSize: 20,
      hasMore: true,
    })

    const wrapper = mountModal()
    await flushPromises()

    expect(listRuns).toHaveBeenCalledWith({
      projectId: 'proj-1',
      status: 'completed',
      page: 1,
      pageSize: 20,
    })
    expect(wrapper.find('[data-testid="board-status-list-row-run-1"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('完成-1')
    expect(wrapper.text()).toContain('实现 · 100% ·')
    expect(wrapper.find('[data-testid="board-status-list-range"]').text()).toContain('已展示 2 / 45')
    expect(wrapper.find('[data-testid="board-status-list-load-more"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('appends on load more and hides button when hasMore is false', async () => {
    listRuns
      .mockResolvedValueOnce({
        items: [stubRun({ id: 'run-1', status: 'completed', title: '完成-1' })],
        total: 2,
        page: 1,
        pageSize: 20,
        hasMore: true,
      })
      .mockResolvedValueOnce({
        items: [stubRun({ id: 'run-2', status: 'completed', title: '完成-2' })],
        total: 2,
        page: 2,
        pageSize: 20,
        hasMore: false,
      })

    const wrapper = mountModal()
    await flushPromises()
    await wrapper.find('[data-testid="board-status-list-load-more"]').trigger('click')
    await flushPromises()

    expect(listRuns).toHaveBeenNthCalledWith(2, {
      projectId: 'proj-1',
      status: 'completed',
      page: 2,
      pageSize: 20,
    })
    expect(wrapper.find('[data-testid="board-status-list-row-run-2"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="board-status-list-load-more"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="board-status-list-range"]').text()).toMatch(/已全部加载/)
    wrapper.unmount()
  })

  it('shows empty state when total is 0 (not an error)', async () => {
    listRuns.mockResolvedValue({
      items: [],
      total: 0,
      page: 1,
      pageSize: 20,
      hasMore: false,
    })
    const wrapper = mountModal({ status: 'waiting_human', title: '等待人工' })
    await flushPromises()
    expect(wrapper.find('[data-testid="board-status-list-empty"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('该状态暂无运行')
    expect(wrapper.find('[data-testid="board-status-list-error"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="board-status-list-load-more"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows initial failure distinctly from empty and allows retry', async () => {
    listRuns.mockRejectedValueOnce(new Error('network')).mockResolvedValueOnce({
      items: [stubRun({ id: 'run-ok', status: 'completed', title: '恢复' })],
      total: 1,
      page: 1,
      pageSize: 20,
      hasMore: false,
    })
    const wrapper = mountModal()
    await flushPromises()
    expect(wrapper.find('[data-testid="board-status-list-error"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('加载失败')
    expect(wrapper.find('[data-testid="board-status-list-empty"]').exists()).toBe(false)

    await wrapper.find('[data-testid="board-status-list-retry"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="board-status-list-row-run-ok"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('keeps shown rows when load-more fails and retries next page', async () => {
    listRuns
      .mockResolvedValueOnce({
        items: [stubRun({ id: 'run-1', status: 'completed', title: '完成-1' })],
        total: 40,
        page: 1,
        pageSize: 20,
        hasMore: true,
      })
      .mockRejectedValueOnce(new Error('more failed'))
      .mockResolvedValueOnce({
        items: [stubRun({ id: 'run-2', status: 'completed', title: '完成-2' })],
        total: 40,
        page: 2,
        pageSize: 20,
        hasMore: false,
      })

    const wrapper = mountModal()
    await flushPromises()
    await wrapper.find('[data-testid="board-status-list-load-more"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="board-status-list-row-run-1"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="board-status-list-retry"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="board-status-list-range"]').text()).toMatch(/下一批失败/)

    await wrapper.find('[data-testid="board-status-list-retry"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="board-status-list-row-run-2"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('emits select when a row is clicked', async () => {
    listRuns.mockResolvedValue({
      items: [stubRun({ id: 'run-9', status: 'running', title: '点我' })],
      total: 1,
      page: 1,
      pageSize: 20,
      hasMore: false,
    })
    const wrapper = mountModal({ status: 'running', title: '运行中' })
    await flushPromises()
    await wrapper.find('[data-testid="board-status-list-row-run-9"]').trigger('click')
    expect(wrapper.emitted('select')?.[0]?.[0]).toMatchObject({ id: 'run-9' })
    wrapper.unmount()
  })

  it('enables Esc close and ~720 width on AppModal', async () => {
    listRuns.mockResolvedValue({ items: [], total: 0, page: 1, pageSize: 20, hasMore: false })
    const wrapper = mountModal()
    await flushPromises()
    const modal = wrapper.getComponent(AppModalStub)
    expect(modal.props('closeOnEsc')).toBe(true)
    expect(modal.props('width')).toBe(720)
    wrapper.unmount()
  })
})
