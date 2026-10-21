// @vitest-environment happy-dom
import { describe, expect, it } from 'vitest'
import { treemapOption } from './tokenAnalyticsCharts'

const t = (key: string) => key

describe('treemapOption layout', () => {
  it('fills the plot box on every edge instead of a fixed 100% x 86% box (no overflow at 1280)', () => {
    const option = treemapOption(
      [{ key: 'w1', name: 'main', kind: 'workflow', value: 10, children: [{ key: 'agent', name: 'agent', kind: 'nodeType', value: 10 }] }],
      'CNY',
      t,
    ) as { series: Array<Record<string, unknown>> }
    const series = option.series[0]!
    expect(series).toMatchObject({ type: 'treemap', left: 0, right: 0, top: 0, bottom: 0 })
    expect(series).not.toHaveProperty('width')
    expect(series).not.toHaveProperty('height')
  })

  it('returns null without tree data', () => {
    expect(treemapOption([], 'CNY', t)).toBeNull()
    expect(treemapOption(undefined, 'CNY', t)).toBeNull()
  })
})
