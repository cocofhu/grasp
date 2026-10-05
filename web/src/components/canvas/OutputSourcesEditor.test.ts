// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { WFEdge, WFNode } from '@/lib/shared/types'
import OutputSourcesEditor from './OutputSourcesEditor.vue'

const EMPTY_AVAILABLE =
  '连接上游节点后可选。将分支、测试/评审门禁或人工门禁连到本输出节点后，可映射的结构化产物会出现在此列表。'

function mountEditor(opts?: { withUpstream?: boolean; results?: string[] }) {
  const node: WFNode = {
    id: 'output',
    type: 'output',
    label: '输出',
    position: { x: 0, y: 0 },
    config: { results: opts?.results ?? [] },
  }
  const upstream: WFNode = {
    id: 'research',
    type: 'agent',
    label: '调研',
    position: { x: -200, y: 0 },
    config: { agent_profile: 'researcher', prompt: '' },
  }
  const edges: WFEdge[] =
    opts?.withUpstream === false
      ? []
      : [{ id: 'e1', source: 'research', target: 'output', kind: 'success' }]
  const allNodes = opts?.withUpstream === false ? [node] : [node, upstream]
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(OutputSourcesEditor, {
    props: { node, allNodes, edges },
    global: { plugins: [i18n], stubs: { Icon: true } },
  })
}

describe('OutputSourcesEditor', () => {
  it('initializes results array and renders editor', () => {
    const wrapper = mountEditor()
    expect(Array.isArray(wrapper.props('node').config.results)).toBe(true)
    expect(wrapper.text().length).toBeGreaterThan(0)
    wrapper.unmount()
  })

  it('shows Demo empty-available guide when no mappable options', () => {
    // plan_coverage: g2.2 / g3.2 — availableOptions 为空时展示 Demo 长文案
    const wrapper = mountEditor({ withUpstream: false })
    const empty = wrapper.find('[data-testid="output-sources-empty-available"]')
    expect(empty.exists()).toBe(true)
    expect(empty.text()).toBe(EMPTY_AVAILABLE)
    expect(wrapper.text()).toContain('未选择任何来源')
    wrapper.unmount()
  })

  it('hides empty-available guide when options exist', () => {
    // plan_coverage: g2.2 / g3.2 — 非空时不展示空态引导
    const wrapper = mountEditor({ withUpstream: true })
    expect(wrapper.find('[data-testid="output-sources-empty-available"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain(EMPTY_AVAILABLE)
    expect(wrapper.text()).toContain('调研')
    wrapper.unmount()
  })

  it('uses fixed-width index badges for 10+ selected sources', () => {
    // plan_coverage: g1.1 / g2.1 — 序号徽章等宽，避免一位/两位数字挤开同行控件
    const results = Array.from({ length: 12 }, (_, i) => `custom.source.${i + 1}`)
    const wrapper = mountEditor({ withUpstream: false, results })
    const badges = wrapper.findAll('.inline-flex.w-\\[22px\\].tabular-nums')
    expect(badges).toHaveLength(12)
    for (const badge of badges) {
      expect(badge.classes()).toContain('w-[22px]')
      expect(badge.classes()).toContain('shrink-0')
      expect(badge.classes()).not.toContain('min-w-[18px]')
    }
    expect(badges[8]?.text()).toBe('9')
    expect(badges[9]?.text()).toBe('10')
    wrapper.unmount()
  })
})
