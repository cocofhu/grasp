// @vitest-environment happy-dom
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'
import type { GlobalTokenStatsTreeNode } from '@/lib/shared/types'
import TokenUsageStructure from './TokenUsageStructure.vue'

function mountStructure(tree: GlobalTokenStatsTreeNode[], total = tree.reduce((sum, node) => sum + node.value, 0)) {
  const i18n = createI18n({
    legacy: false,
    locale: 'en',
    messages: {
      en: {
        pages: {
          board: { tokenStats: { workflow: 'Workflow', pm: 'Assistant' } },
          tokenAnalytics: {
            sourceStudio: 'Studio',
            structure: { shareLabel: 'Share', showMore: 'Show all ({count})', showLess: 'Show less' },
          },
        },
      },
    },
  })
  return mount(TokenUsageStructure, { props: { tree, total }, global: { plugins: [i18n] } })
}

describe('TokenUsageStructure', () => {
  it('keeps tiny categories readable beside a dominant workflow and sorts by usage', () => {
    const wrapper = mountStructure([
      { kind: 'studio', key: 'studio', name: 'studio', value: 9 },
      { kind: 'pm', key: 'pm', name: 'pm', value: 1 },
      { kind: 'workflow', key: 'w1', name: 'Main workflow', value: 999_990 },
      { kind: 'workflow', key: 'empty', name: 'Empty', value: 0 },
      { kind: 'workflow', key: 'negative', name: 'Negative', value: -2 },
    ], 1_000_000)
    const rows = wrapper.findAll('[data-testid="token-structure-row"]')
    expect(rows.map((row) => row.attributes('data-key'))).toEqual(['w1', 'studio', 'pm'])
    expect(rows[0].find('[data-testid="token-structure-total"]').attributes('title')).toBe('999,990')
    expect(rows[1].text()).toContain('Studio')
    expect(rows[1].find('[data-testid="token-structure-total"]').text()).toBe('9')
    expect(rows[1].find('[data-testid="token-structure-share"]').text()).toBe('<0.1%')
    expect(rows[2].text()).toContain('Assistant')
    expect(rows[2].find('[data-testid="token-structure-total"]').text()).toBe('1')
    wrapper.unmount()
  })

  it('drills into a workflow and retains its scope when selecting a node type', async () => {
    const wrapper = mountStructure([{
      kind: 'workflow', key: 'w1', name: 'Main workflow', value: 100,
      children: [{ kind: 'nodeType', key: 'agent', name: 'Agent', value: 100 }],
    }])
    const source = wrapper.find('[data-testid="token-structure-source"]')
    expect(source.element.tagName).toBe('BUTTON')
    await source.trigger('click')
    await wrapper.find('[data-testid="token-structure-node"]').trigger('click')
    expect(wrapper.emitted('drill')).toEqual([
      [[{ dim: 'workflow', key: 'w1', name: 'Main workflow' }]],
      [[{ dim: 'workflow', key: 'w1', name: 'Main workflow' }, { dim: 'nodeType', key: 'agent', name: 'Agent' }]],
    ])
    wrapper.unmount()
  })

  it('retains the source scope for assistant and studio node types', async () => {
    const wrapper = mountStructure(['pm', 'studio'].map((kind) => ({
      kind, key: kind, name: kind, value: 100,
      children: [{ kind: 'nodeType', key: 'llm', name: 'LLM', value: 100 }],
    })))
    for (const button of wrapper.findAll('[data-testid="token-structure-node"]')) await button.trigger('click')
    expect(wrapper.emitted('drill')).toEqual([
      [[{ dim: 'source', key: 'pm', name: 'Assistant' }, { dim: 'nodeType', key: 'llm', name: 'LLM' }]],
      [[{ dim: 'source', key: 'studio', name: 'Studio' }, { dim: 'nodeType', key: 'llm', name: 'LLM' }]],
    ])
    wrapper.unmount()
  })

  it('keeps aggregate and unknown buckets visible without emitting invalid filters', async () => {
    const wrapper = mountStructure([
      {
        kind: 'workflow', key: '_other', name: 'Other workflows', value: 100,
        children: [{ kind: 'nodeType', key: 'unknown', name: 'Unknown', value: 100 }],
      },
      {
        kind: 'workflow', key: 'w1', name: 'Main workflow', value: 50,
        children: [{ kind: 'nodeType', key: 'unknown', name: 'Unknown', value: 50 }],
      },
    ])
    const rows = wrapper.findAll('[data-testid="token-structure-row"]')
    expect(rows[0].find('[data-testid="token-structure-source"]').element.tagName).toBe('DIV')
    expect(rows[0].find('[data-testid="token-structure-node"]').element.tagName).toBe('SPAN')
    await rows[0].find('[data-testid="token-structure-source"]').trigger('click')
    await rows[0].find('[data-testid="token-structure-node"]').trigger('click')
    expect(wrapper.emitted('drill')).toBeUndefined()
    await rows[1].find('[data-testid="token-structure-node"]').trigger('click')
    expect(wrapper.emitted('drill')).toEqual([[[{ dim: 'workflow', key: 'w1', name: 'Main workflow' }]]])
    wrapper.unmount()
  })

  it('expands long rankings without losing the remaining categories', async () => {
    const wrapper = mountStructure(Array.from({ length: 7 }, (_, i) => ({ kind: 'workflow', key: `w${i}`, name: `Workflow ${i}`, value: 10 - i })))
    expect(wrapper.findAll('[data-testid="token-structure-row"]')).toHaveLength(5)
    const toggle = wrapper.find('[data-testid="token-structure-expand"]')
    expect(toggle.text()).toBe('Show all (7)')
    expect(toggle.attributes('aria-expanded')).toBe('false')
    await toggle.trigger('click')
    expect(wrapper.findAll('[data-testid="token-structure-row"]')).toHaveLength(7)
    expect(toggle.text()).toBe('Show less')
    expect(toggle.attributes('aria-expanded')).toBe('true')
    await toggle.trigger('click')
    expect(wrapper.findAll('[data-testid="token-structure-row"]')).toHaveLength(5)
    wrapper.unmount()
  })
})
