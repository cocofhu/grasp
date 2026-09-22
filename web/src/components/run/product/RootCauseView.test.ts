// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import commonZh from '@/locales/zh-CN/common.json'
import pagesZh from '@/locales/zh-CN/pages.json'
import RootCauseView, { type RootCauseDoc } from './RootCauseView.vue'

const mermaidRender = vi.fn()
const mermaidParse = vi.fn()

vi.mock('mermaid', () => ({
  default: {
    initialize: vi.fn(),
    parse: (...args: unknown[]) => mermaidParse(...args),
    render: (...args: unknown[]) => mermaidRender(...args),
  },
}))

function mountRootCause(doc: RootCauseDoc, accent?: string) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...commonZh, ...pagesZh } },
  })
  return mount(RootCauseView, {
    props: { doc, accent },
    global: { plugins: [i18n], stubs: { Icon: true, AnnotateBtn: true } },
  })
}

const baseDoc: RootCauseDoc = {
  title: '登录失败',
  summary: '概述正文',
  symptom: '点击登录无响应',
  expected: '应跳转首页',
  actual: '页面卡住',
  reproduction: ['打开登录页', '输入账号密码', '点击登录'],
  impact: '所有用户无法登录',
  root_cause: '会话 cookie 未写入',
  evidence: [{ title: '日志', detail: 'Set-Cookie 缺失' }],
}

describe('RootCauseView', () => {
  beforeEach(() => {
    mermaidParse.mockReset()
    mermaidRender.mockReset()
    mermaidParse.mockResolvedValue(true)
    mermaidRender.mockResolvedValue({ svg: '<svg data-ok="1"></svg>' })
  })

  it('renders summary, expected, actual, and root_cause', () => {
    const wrapper = mountRootCause(baseDoc)
    expect(wrapper.text()).toContain('概述正文')
    expect(wrapper.text()).toContain('应跳转首页')
    expect(wrapper.text()).toContain('页面卡住')
    expect(wrapper.text()).toContain('会话 cookie 未写入')
    expect(wrapper.find('[data-json-path="expected"]').exists()).toBe(true)
    expect(wrapper.find('[data-json-path="actual"]').exists()).toBe(true)
    expect(wrapper.find('[data-json-path="root_cause"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('mounts MermaidDiagram for a single diagram', async () => {
    const doc: RootCauseDoc = {
      ...baseDoc,
      diagrams: [{ format: 'mermaid', source: 'flowchart LR\n  A-->B', title: '因果图' }],
    }
    const wrapper = mountRootCause(doc)
    await flushPromises()
    expect(wrapper.find('[data-testid="plan-diagram"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="root-cause-diagram-tabs"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows in-section tabs when there are two or more diagrams', async () => {
    const doc: RootCauseDoc = {
      ...baseDoc,
      diagrams: [
        { source: 'flowchart LR\n  A-->B', title: '图一' },
        { source: 'flowchart LR\n  C-->D', title: '图二' },
      ],
    }
    const wrapper = mountRootCause(doc)
    await flushPromises()
    expect(wrapper.find('[data-testid="root-cause-diagram-tabs"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="root-cause-diagram-tab-0"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="root-cause-diagram-tab-1"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('falls back to source when mermaid render fails', async () => {
    mermaidRender.mockRejectedValue(new Error('boom'))
    const doc: RootCauseDoc = {
      ...baseDoc,
      diagrams: [{ source: 'flowchart LR\n  FAIL-->HERE' }],
    }
    const wrapper = mountRootCause(doc)
    await flushPromises()
    expect(wrapper.find('[data-testid="plan-diagram-fallback"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('FAIL-->HERE')
    wrapper.unmount()
  })
})
