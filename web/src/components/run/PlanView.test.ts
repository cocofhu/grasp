// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import commonZh from '@/locales/zh-CN/common.json'
import pagesZh from '@/locales/zh-CN/pages.json'
import commonEn from '@/locales/en/common.json'
import pagesEn from '@/locales/en/pages.json'
import PlanView, { type PlanDoc } from './PlanView.vue'

const mermaidRender = vi.fn()
const mermaidParse = vi.fn()

vi.mock('mermaid', () => ({
  default: {
    initialize: vi.fn(),
    parse: (...args: unknown[]) => mermaidParse(...args),
    render: (...args: unknown[]) => mermaidRender(...args),
  },
}))

const designDoc: PlanDoc = {
  title: '完整计划',
  architecture: {
    summary: 'arch summary',
    diagram: { format: 'mermaid', source: 'flowchart LR\n  A-->B', caption: '架构' },
  },
  data_design: {
    summary: 'data',
    entities: [{
      name: 'planDoc',
      fields: [
        { name: 'title', type: 'string' },
        { name: 'id', type: 'uuid', pk: true, fk: 'Other.ref' },
      ],
      relationships: ['1..* planGoal'],
    }],
    relationships: ['planDoc contains planGoal'],
    diagram: { source: 'erDiagram\n  A ||--o{ B : has' },
  },
  interfaces: [{ name: 'set_plan', summary: '写入' }],
  components: [{ name: 'plan.go', responsibility: 'parse' }],
  interaction: {
    summary: 'flow',
    diagram: { source: 'sequenceDiagram\n  A->>B: hi' },
  },
  test_design: 'S1-S7',
  goals: [
    {
      id: 'g1',
      title: 'G',
      status: 'in_progress',
      subgoals: [
        { id: 'g1.1', title: 'S1', status: 'done' },
        { id: 'g1.2', title: 'S2', status: 'pending' },
        { id: 'g1.3', title: 'S3', status: 'pending' },
      ],
    },
  ],
}

function mountPlan(doc: PlanDoc, accent?: string, locale: 'zh-CN' | 'en' = 'zh-CN') {
  const i18n =
    locale === 'zh-CN'
      ? createI18n({
          legacy: false,
          locale: 'zh-CN',
          messages: { 'zh-CN': { ...commonZh, ...pagesZh } },
        })
      : createI18n({
          legacy: false,
          locale: 'en',
          messages: { en: { ...commonEn, ...pagesEn } },
        })
  return mount(PlanView, {
    props: { doc, accent },
    global: { plugins: [i18n], stubs: { Icon: true, AnnotateBtn: true } },
  })
}

describe('PlanView', () => {
  beforeEach(() => {
    mermaidParse.mockReset()
    mermaidRender.mockReset()
    mermaidParse.mockResolvedValue(true)
    mermaidRender.mockResolvedValue({ svg: '<svg data-ok="1"></svg>' })
  })

  it('renders goals and progress (legacy goals-only)', () => {
    const doc: PlanDoc = {
      title: '实施计划',
      goals: [
        {
          id: 'g1',
          title: '大目标',
          status: 'in_progress',
          subgoals: [
            { id: 'g1.1', title: '子目标 A', status: 'done' },
            { id: 'g1.2', title: '子目标 B', status: 'pending' },
          ],
        },
      ],
    }
    const wrapper = mountPlan(doc)
    expect(wrapper.text()).toContain('实施计划')
    expect(wrapper.text()).toContain('大目标')
    expect(wrapper.text()).toContain('子目标 A')
    expect(wrapper.text()).toContain('1/2')
    expect(wrapper.find('[data-testid="plan-design"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('uses default title when doc.title missing', () => {
    const wrapper = mountPlan({ goals: [{ id: 'g1', title: '仅目标', status: 'pending' }] })
    expect(wrapper.text()).toMatch(/计划|Plan/)
    expect(wrapper.text()).toContain('仅目标')
    wrapper.unmount()
  })

  it('renders full six design sections and keeps progress on goals leaves', async () => {
    const wrapper = mountPlan(designDoc)
    await flushPromises()
    expect(wrapper.find('[data-testid="plan-design"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="plan-sec-architecture"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="plan-sec-data"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="plan-sec-interfaces"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="plan-sec-components"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="plan-sec-interaction"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="plan-sec-test"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('1/3')
    expect(mermaidRender).toHaveBeenCalled()
    wrapper.unmount()
  })

  it('renders entity fields as table with badges and relationships (g1.1/g1.3)', async () => {
    const wrapper = mountPlan(designDoc)
    await flushPromises()
    const fields = wrapper.find('[data-testid="plan-entity-fields"]')
    expect(fields.exists()).toBe(true)
    expect(fields.find('table').exists()).toBe(true)
    expect(fields.findAll('tbody tr')).toHaveLength(2)
    // no left-border nested list for fields
    expect(fields.find('ul').exists()).toBe(false)
    expect(wrapper.text()).toContain('title')
    expect(wrapper.text()).toContain('string')
    expect(wrapper.text()).toContain('PK')
    expect(wrapper.text()).toContain('fk→Other.ref')
    expect(wrapper.findAll('[data-testid="plan-field-badge"]').length).toBeGreaterThanOrEqual(2)
    expect(wrapper.find('[data-testid="plan-entity-relationships"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('1..* planGoal')
    expect(wrapper.find('[data-testid="plan-data-relationships"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('planDoc contains planGoal')
    wrapper.unmount()
  })

  it('renders legacy attributes when fields absent (g2.1)', () => {
    const doc: PlanDoc = {
      data_design: {
        summary: 'legacy',
        entities: [{ name: 'Old', attributes: ['id', 'name'] }],
      },
      goals: [{ id: 'g1', title: 'G', status: 'pending' }],
    }
    const wrapper = mountPlan(doc)
    expect(wrapper.find('[data-testid="plan-entity-fields"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="plan-entity-attributes"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('legacy')
    expect(wrapper.text()).toContain('id')
    wrapper.unmount()
  })

  it('does not render empty fields table (g1.3 edge)', () => {
    const doc: PlanDoc = {
      data_design: {
        summary: 'empty fields',
        entities: [{ name: 'Empty', fields: [], description: 'no cols' }],
      },
      goals: [{ id: 'g1', title: 'G', status: 'pending' }],
    }
    const wrapper = mountPlan(doc)
    expect(wrapper.text()).toContain('Empty')
    expect(wrapper.text()).toContain('no cols')
    expect(wrapper.find('[data-testid="plan-entity-fields"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('zh-CN shows Chinese design section titles and field table headers (g1.2)', async () => {
    const wrapper = mountPlan(designDoc, undefined, 'zh-CN')
    await flushPromises()
    const text = wrapper.text()
    expect(text).toContain('架构')
    expect(text).toContain('数据设计')
    expect(text).toContain('接口')
    expect(text).toContain('组件')
    expect(text).toContain('交互')
    expect(text).toContain('测试设计')
    expect(text).toContain('字段')
    expect(text).toContain('类型')
    expect(text).toContain('约束')
    expect(text).toContain('说明')
    expect(text).not.toContain('Architecture')
    expect(text).not.toContain('Data design')
    expect(text).not.toContain('Interfaces')
    expect(text).not.toContain('Components')
    expect(text).not.toContain('Interaction')
    expect(text).not.toContain('Test design')
    expect(text).not.toContain('Constraints')
    wrapper.unmount()
  })

  it('en shows English design section titles and field table headers (g1.2)', async () => {
    const wrapper = mountPlan(designDoc, undefined, 'en')
    await flushPromises()
    const text = wrapper.text()
    expect(text).toContain('Architecture')
    expect(text).toContain('Data design')
    expect(text).toContain('Interfaces')
    expect(text).toContain('Components')
    expect(text).toContain('Interaction')
    expect(text).toContain('Test design')
    expect(text).toContain('Field')
    expect(text).toContain('Type')
    expect(text).toContain('Constraints')
    expect(text).toContain('Description')
    expect(text).not.toContain('数据设计')
    expect(text).not.toContain('测试设计')
    expect(text).not.toContain('约束')
    wrapper.unmount()
  })

  it('shows 不涉及 placeholder for NA sections', () => {
    const doc: PlanDoc = {
      architecture: { summary: '不涉及' },
      data_design: { summary: '不涉及' },
      interfaces: [{ name: '不涉及', summary: '无' }],
      components: [{ name: '不涉及' }],
      interaction: { summary: '不涉及' },
      test_design: '不涉及',
      goals: [{ id: 'g1', title: 'G', status: 'pending' }],
    }
    const wrapper = mountPlan(doc)
    expect(wrapper.find('[data-testid="plan-design"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('不涉及')
    expect(wrapper.find('[data-testid="plan-diagram"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('falls back to source when mermaid render fails', async () => {
    // Permanent rejection: MermaidDiagram retries once, then falls back.
    mermaidRender.mockRejectedValue(new Error('boom'))
    const doc: PlanDoc = {
      architecture: {
        summary: 'arch',
        diagram: { source: 'flowchart LR\n  FAIL-->HERE' },
      },
      goals: [{ id: 'g1', title: 'G', status: 'pending' }],
    }
    const wrapper = mountPlan(doc)
    await flushPromises()
    expect(wrapper.find('[data-testid="plan-diagram-fallback"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('FAIL-->HERE')
    expect(wrapper.text()).toContain('G')
    wrapper.unmount()
  })

  it('uses readable body text classes (text-txt2) for design and goal details (g1.1/g1.2/g1.3)', async () => {
    const doc: PlanDoc = {
      title: '对比度计划',
      architecture: { summary: '架构摘要可读' },
      data_design: { summary: '数据摘要可读' },
      interfaces: [{ name: 'iface', summary: '接口说明可读' }],
      components: [{ name: 'comp', responsibility: '组件职责可读' }],
      interaction: { summary: '交互摘要可读' },
      test_design: '测试设计可读',
      goals: [
        {
          id: 'g1',
          title: '大目标',
          detail: '目标说明可读',
          status: 'pending',
          subgoals: [{ id: 'g1.1', title: '小目标', detail: '小目标说明可读', status: 'pending' }],
        },
      ],
    }
    const wrapper = mountPlan(doc)
    await flushPromises()

    const bodySelectors = [
      '[data-testid="plan-body-architecture"]',
      '[data-testid="plan-body-data"]',
      '[data-testid="plan-body-interface"]',
      '[data-testid="plan-body-component"]',
      '[data-testid="plan-body-interaction"]',
      '[data-testid="plan-body-test"]',
      '[data-testid="plan-body-goal-detail"]',
      '[data-testid="plan-body-subgoal-detail"]',
    ]
    for (const sel of bodySelectors) {
      const el = wrapper.find(sel)
      expect(el.exists(), sel).toBe(true)
      expect(el.classes(), sel).toContain('text-txt2')
      expect(el.classes(), sel).not.toContain('text-txt3')
    }

    // Meta layers stay weaker: section uppercase label + goal id badge
    const designLabel = wrapper.find('[data-testid="plan-design"] > .text-txt3')
    expect(designLabel.exists()).toBe(true)
    expect(wrapper.find('code.text-txt3').exists()).toBe(true)
    wrapper.unmount()
  })

  it('keeps NA warn styling distinct from readable body text', () => {
    const doc: PlanDoc = {
      architecture: { summary: '不涉及' },
      data_design: { summary: '不涉及' },
      interaction: { summary: '不涉及' },
      test_design: '不涉及',
      goals: [{ id: 'g1', title: 'G', status: 'pending' }],
    }
    const wrapper = mountPlan(doc)
    expect(wrapper.find('[data-testid="plan-body-architecture"]').classes()).toContain('text-warn')
    expect(wrapper.find('[data-testid="plan-body-data"]').classes()).toContain('text-warn')
    expect(wrapper.find('[data-testid="plan-body-interaction"]').classes()).toContain('text-warn')
    expect(wrapper.find('[data-testid="plan-body-test"]').classes()).toContain('text-warn')
    wrapper.unmount()
  })

  // g2.1 / g2.2 / g2.3 / g2.5: section diagram tabs
  it('shows no diagram tabs for zero or one diagram (g2.3)', async () => {
    const zero = mountPlan({
      architecture: { summary: '无图' },
      goals: [{ id: 'g1', title: 'G', status: 'pending' }],
    })
    expect(zero.find('[data-testid="plan-diagram-tabs"]').exists()).toBe(false)
    expect(zero.find('[data-testid="plan-diagram"]').exists()).toBe(false)
    zero.unmount()

    const one = mountPlan({
      architecture: {
        summary: '单图',
        diagram: { source: 'flowchart LR\n  A-->B', title: '总览' },
      },
      goals: [{ id: 'g1', title: 'G', status: 'pending' }],
    })
    await flushPromises()
    expect(one.find('[data-testid="plan-diagram-tabs"]').exists()).toBe(false)
    expect(one.find('[data-testid="plan-diagram"]').exists()).toBe(true)
    // no sidebar catalog
    expect(one.find('[data-testid="plan-diagram-sidebar"]').exists()).toBe(false)
    one.unmount()
  })

  it('shows small tabs for multi diagrams and switches current figure (g2.1/g2.2)', async () => {
    const doc: PlanDoc = {
      architecture: {
        summary: '多图',
        diagrams: [
          { kind: 'flowchart', title: '写入链路', source: 'flowchart LR\n  A-->B' },
          { kind: 'activity', title: '审批活动', scope: 'approve', source: 'flowchart TD\n  S-->E' },
          { kind: 'flowchart', title: '认证模块', scope: 'auth', source: 'flowchart LR\n  L-->R' },
        ],
      },
      goals: [{ id: 'g1', title: 'G', status: 'pending' }],
    }
    const wrapper = mountPlan(doc)
    await flushPromises()
    const tabs = wrapper.find('[data-testid="plan-diagram-tabs"]')
    expect(tabs.exists()).toBe(true)
    expect(wrapper.findAll('[data-testid^="plan-diagram-tab-"]')).toHaveLength(3)
    expect(tabs.text()).toContain('写入链路')
    expect(tabs.text()).toContain('审批活动')
    expect(tabs.text()).toContain('approve')
    // only one mermaid host at a time
    expect(wrapper.findAll('[data-testid="plan-diagram"]')).toHaveLength(1)
    expect(mermaidRender).toHaveBeenCalledWith(expect.any(String), 'flowchart LR\n  A-->B')

    await wrapper.find('[data-testid="plan-diagram-tab-1"]').trigger('click')
    await flushPromises()
    expect(mermaidRender).toHaveBeenCalledWith(expect.any(String), 'flowchart TD\n  S-->E')
    // still no sidebar
    expect(wrapper.find('[data-testid="plan-diagram-sidebar"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('merges diagrams[] with singular diagram for display (g2.1)', async () => {
    const doc: PlanDoc = {
      architecture: {
        summary: '合并',
        diagrams: [{ kind: 'activity', title: '活动', source: 'flowchart TD\n  S-->E' }],
        diagram: { source: 'flowchart LR\n  A-->B', title: '兼容单图' },
      },
      goals: [{ id: 'g1', title: 'G', status: 'pending' }],
    }
    const wrapper = mountPlan(doc)
    await flushPromises()
    expect(wrapper.findAll('[data-testid^="plan-diagram-tab-"]')).toHaveLength(2)
    expect(wrapper.text()).toContain('活动')
    expect(wrapper.text()).toContain('兼容单图')
    wrapper.unmount()
  })
})
