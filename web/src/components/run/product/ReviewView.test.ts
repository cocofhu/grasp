// @vitest-environment happy-dom
import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import { provideReviewAnnotate } from '@/lib/inbox/reviewAnnotate'
import ReviewView, { type ReviewDoc } from './ReviewView.vue'

const doc: ReviewDoc = {
  verdict: 'approve_with_comments',
  summary: '核心交付已完成，合并前补充一项验证。',
  findings: [{ id: 'retry', severity: 'medium', title: '补充失败重试覆盖', file: 'src/workflow.ts', line: 42, detail: '当前仅验证成功路径。', suggestion: '模拟超时并验证重试。' }],
  action_items: ['补充超时用例'],
}

function mountReport(value = doc, annotate?: ReturnType<typeof vi.fn>) {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
  const Host = defineComponent({ setup() {
    if (annotate) provideReviewAnnotate({ enabled: true, annotate })
    return () => h(ReviewView, { doc: value })
  } })
  return mount(Host, { global: { plugins: [i18n], stubs: { Icon: true } } })
}

describe('review report document', () => {
  it('presents the verdict, findings and actions as readable report headings and prose', () => {
    const wrapper = mountReport()
    expect(wrapper.get('h2').text()).toContain(pages.pages.product.review.verdictLabels.approve_with_comments)
    expect(wrapper.findAll('h3')).toHaveLength(2)
    expect(wrapper.get('h4').text()).toBe('补充失败重试覆盖')
    for (const text of ['核心交付已完成，合并前补充一项验证。', 'src/workflow.ts:42', '当前仅验证成功路径。', '模拟超时并验证重试。', '补充超时用例']) expect(wrapper.text()).toContain(text)
    expect(wrapper.find('button').exists()).toBe(false)
    wrapper.unmount()
  })

  it('keeps annotation targets for summary, findings, details, suggestions and actions', async () => {
    const annotate = vi.fn()
    const wrapper = mountReport(doc, annotate)
    for (const path of ['summary', 'findings[retry]', 'findings[retry].detail', 'findings[retry].suggestion', 'action_items[0]']) expect(wrapper.find(`[data-json-path="${path}"]`).exists()).toBe(true)
    await wrapper.get('button[title="标注 findings[retry]"]').trigger('click')
    expect(annotate).toHaveBeenLastCalledWith({ jsonPath: 'findings[retry]', label: '补充失败重试覆盖' })
    await wrapper.get('button[title="标注 action_items[0]"]').trigger('click')
    expect(annotate).toHaveBeenLastCalledWith({ jsonPath: 'action_items[0]', label: '补充超时用例' })
    wrapper.unmount()
  })

  it('retains an unknown verdict instead of dropping the report conclusion', () => {
    const wrapper = mountReport({ verdict: 'needs another look', summary: '等待下一轮复核' })
    expect(wrapper.get('h2').text()).toContain('needs another look')
    expect(wrapper.text()).toContain('等待下一轮复核')
    expect(wrapper.find('section').exists()).toBe(false)
    wrapper.unmount()
  })
})
