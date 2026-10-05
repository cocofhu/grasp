// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { Run } from '@/lib/shared/types'
import RunBoardColumn from './RunBoardColumn.vue'

function stubRun(partial: Partial<Run> & Pick<Run, 'id' | 'status'>): Run {
  return {
    workflowId: 'wf',
    workflowName: 'Workflow',
    trigger: 'manual',
    startedAt: '2026-07-18T12:00:00Z',
    durationSec: 60,
    progress: 40,
    nodeRuns: {},
    artifacts: [],
    ...partial,
  }
}

function mountColumn(props: Record<string, unknown> = {}, slots: Record<string, string> = {}) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(RunBoardColumn, {
    props: {
      title: '已完成',
      items: [],
      emptyText: '暂无运行',
      accent: 'done',
      ...props,
    },
    slots,
    global: { plugins: [i18n], stubs: { RunBoardCard: true } },
  })
}

describe('RunBoardColumn', () => {
  it('renders column title and empty text', () => {
    const wrapper = mountColumn()
    expect(wrapper.find('[data-testid="run-board-column"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('已完成')
    expect(wrapper.text()).toContain('暂无运行')
    wrapper.unmount()
  })

  it('exposes a scroll body with overflow and half-viewport max-height', () => {
    const items = Array.from({ length: 24 }, (_, i) =>
      stubRun({ id: `run-${i}`, status: 'completed', title: `完成-${i}` }),
    )
    const wrapper = mountColumn({
      items,
      total: 24,
    }, {
      footer: '<button data-testid="col-footer">查看更多</button>',
    })

    const body = wrapper.find('[data-testid="run-board-column-body"]')
    expect(body.exists()).toBe(true)
    expect(body.classes()).toContain('scroll-area')
    expect(body.classes()).toContain('overflow-y-auto')
    expect(body.classes()).toContain('min-h-0')

    const el = body.element as HTMLElement
    expect(el.style.maxHeight).toMatch(/min\(\s*52vh\s*,\s*420px\s*\)/)
    const style = getComputedStyle(el)
    // happy-dom may not resolve Tailwind utilities; class + inline max-height are the contract.
    expect(el.className).toContain('overflow-y-auto')
    expect(style.maxHeight === '' || /vh|px|min/.test(style.maxHeight)).toBe(true)

    const titleEl = wrapper.find('[data-testid="run-board-column"] > div')
    expect(titleEl.exists()).toBe(true)
    expect(body.element.contains(titleEl.element)).toBe(false)

    const footer = wrapper.find('[data-testid="col-footer"]')
    expect(footer.exists()).toBe(true)
    expect(body.element.contains(footer.element)).toBe(false)

    expect(wrapper.text()).not.toMatch(/列内可独立滚动|↓/)
    expect(wrapper.find('.scroll-hint').exists()).toBe(false)
    expect(wrapper.find('.is-short').exists()).toBe(false)

    wrapper.unmount()
  })

  it('keeps the same max-height rule for short lists without teaching copy', () => {
    const wrapper = mountColumn({
      items: [stubRun({ id: 'run-1', status: 'completed', title: '短列' })],
      total: 1,
    })
    const body = wrapper.find('[data-testid="run-board-column-body"]')
    expect(body.exists()).toBe(true)
    expect((body.element as HTMLElement).style.maxHeight).toMatch(/min\(\s*52vh\s*,\s*420px\s*\)/)
    expect(wrapper.find('[data-testid="run-board-column"]').classes()).not.toContain('h-full')
    expect(wrapper.text()).not.toMatch(/列内可独立滚动/)
    wrapper.unmount()
  })

  it('fill mode drops max-height, stretches outer, and keeps column-body overflow-y', () => {
    const wrapper = mountColumn({
      fill: true,
      items: [
        stubRun({ id: 'run-1', status: 'completed', title: '完成-1' }),
        stubRun({ id: 'run-2', status: 'completed', title: '完成-2' }),
      ],
      total: 2,
    })

    const root = wrapper.find('[data-testid="run-board-column"]')
    expect(root.classes()).toContain('h-full')

    const body = wrapper.find('[data-testid="run-board-column-body"]')
    expect(body.exists()).toBe(true)
    expect(body.classes()).toContain('overflow-y-auto')
    expect(body.classes()).toContain('min-h-0')
    expect(body.classes()).toContain('flex-1')
    expect((body.element as HTMLElement).style.maxHeight).toBe('')

    wrapper.unmount()
  })

  it('keeps non-activatable header as a plain div (Dashboard default)', () => {
    const wrapper = mountColumn()
    expect(wrapper.find('[data-testid="run-board-column-header"]').exists()).toBe(false)
    const header = wrapper.find('[data-testid="run-board-column"] > div')
    expect(header.element.tagName.toLowerCase()).toBe('div')
    expect(header.attributes('aria-haspopup')).toBeUndefined()
    wrapper.unmount()
  })

  it('activatable header is a focusable button that emits activate-header with status', async () => {
    const wrapper = mountColumn({
      headerActivatable: true,
      status: 'completed',
      items: [stubRun({ id: 'run-1', status: 'completed', title: '完成-1' })],
      total: 1,
    })
    const header = wrapper.find('[data-testid="run-board-column-header"]')
    expect(header.exists()).toBe(true)
    expect(header.element.tagName.toLowerCase()).toBe('button')
    expect(header.attributes('type')).toBe('button')
    expect(header.attributes('aria-haspopup')).toBe('dialog')

    await header.trigger('click')
    expect(wrapper.emitted('activate-header')).toEqual([['completed']])
    expect(wrapper.emitted('select')).toBeUndefined()
    wrapper.unmount()
  })
})
