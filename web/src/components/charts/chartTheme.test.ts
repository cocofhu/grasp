// @vitest-environment happy-dom
import { beforeEach, describe, expect, it } from 'vitest'
import { setTheme } from '@/lib/shared/theme'
import {
  axisTooltip,
  chartTone,
  pieChartOption,
  pieLegend,
  pieTooltipFormatter,
  statsAxis,
  statsLegend,
  statsTooltip,
  TREND_CHART_GRID,
  CHART_GRID,
} from './chartTheme'

describe('chartTheme shared chrome (g1.1)', () => {
  beforeEach(() => {
    localStorage.clear()
    document.documentElement.classList.remove('light')
    setTheme('dark')
  })

  it('dark tone uses zinc tooltip and low-contrast split lines', () => {
    setTheme('dark')
    const tone = chartTone()
    expect(tone.tooltipBg).toBe('#27272a')
    expect(tone.tooltipText).toBe('#e4e4e7')
    expect(tone.splitLine).toBe('rgba(255,255,255,0.08)')
    expect(tone.axisLabel).toBe('#a1a1aa')
    const tip = statsTooltip()
    expect(tip.borderRadius).toBe(12)
    expect(tip.backgroundColor).toBe('#27272a')
    expect(tip.appendToBody).toBe(true)
    expect(tip.confine).toBe(false)
    expect(JSON.stringify(tip)).not.toContain('#1a1d23')
  })

  it('light tone uses white tooltip and light-gray split lines', () => {
    setTheme('light')
    const tone = chartTone()
    expect(tone.tooltipBg).toBe('#ffffff')
    expect(tone.tooltipText).toBe('#27272a')
    expect(tone.splitLine).toBe('#eef0f3')
    expect(tone.axisLabel).toBe('#71717a')
    const tip = statsTooltip()
    expect(tip.backgroundColor).toBe('#ffffff')
    expect(tip.textStyle.color).toBe('#27272a')
    expect(tip.borderRadius).toBe(12)
    expect(statsAxis().splitLine.lineStyle.color).toBe('#eef0f3')
  })

  it('pie legend is vertical on the right; axis legend is top-left', () => {
    const pie = pieLegend()
    expect(pie.orient).toBe('vertical')
    expect(pie.right).toBe(0)
    expect(pie.show).toBe(true)
    const axis = statsLegend()
    expect(axis.top).toBe(0)
    expect(axis.left).toBe(0)
  })

  it('pie option puts name+percent in a truncating legend (no outer labels) and keeps the compact tooltip', () => {
    const option = pieChartOption(
      [
        { name: '输入', value: 396_400, color: '#3b82f6' },
        { name: '输出', value: 100, color: '#8b5cf6' },
      ],
      false,
    )
    expect(option).toBeTruthy()
    expect(option!.legend.orient).toBe('vertical')
    expect(option!.legend.right).toBe(0)
    // Outer labels overlapped the legend; share now lives in the legend text.
    expect(option!.series[0]!.label.show).toBe(false)
    expect(option!.series[0]!.labelLine.show).toBe(false)
    expect(option!.legend.formatter('输入')).toBe('输入  100.0%')
    expect(option!.legend.formatter('输出')).toBe('输出  0.0%')
    expect(option!.legend.formatter('missing')).toBe('missing  0.0%')
    expect(option!.legend.textStyle).toMatchObject({ width: 132, overflow: 'truncate' })
    expect(option!.series[0]!.center[0]).toBe('26%')
    expect(option!.series[0]!.radius).toBe('66%')
    expect(pieChartOption([{ name: 'a', value: 1 }], true)!.series[0]!.radius).toEqual(['44%', '66%'])
    expect(option!.tooltip.borderRadius).toBe(12)
    expect(option!.tooltip.appendToBody).toBe(true)
    expect(typeof option!.tooltip.formatter).toBe('function')
    expect(pieTooltipFormatter({ name: '输入', value: 396_400, percent: 99.9 })).toBe('输入: 396.4K (99.9%)')
    expect(axisTooltip().valueFormatter?.(2_080_982_825)).toBe('2.08B')
  })

  it('board trend grid keeps first category in the left-edge hover band (g2.2)', () => {
    expect(TREND_CHART_GRID.left).toBe(CHART_GRID.left)
    expect(TREND_CHART_GRID.left).toBe(42)
    expect(TREND_CHART_GRID.containLabel).toBe(false)
    expect(TREND_CHART_GRID.top).toBe(40)
  })
})
