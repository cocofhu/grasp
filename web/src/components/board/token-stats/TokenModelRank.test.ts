// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import enCommon from '@/locales/en/common.json'
import enPages from '@/locales/en/pages.json'
import TokenModelRank from './TokenModelRank.vue'
import { colorForModel } from './tokenModelColors'
import { setTheme } from '@/lib/shared/theme'
import { RANK_TRACK_DARK, RANK_TRACK_LIGHT } from '@/components/charts/chartTheme'

vi.mock('vue-echarts', () => ({
  default: {
    name: 'VChart',
    template: '<div data-testid="mock-vchart" class="h-full w-full"><canvas /></div>',
    props: ['option'],
  },
}))

vi.mock('@/components/charts/echartsSetup', () => ({
  registerECharts: () => {},
}))

const i18nZh = () =>
  createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })

const i18nEn = () =>
  createI18n({
    legacy: false,
    locale: 'en',
    messages: { en: { ...enCommon, ...enPages } },
  })

function expectNoFilledTagCopy(text: string) {
  expect(text).not.toContain('含补全')
  expect(text).not.toContain('includes filled data')
}

function barColor(vm: unknown, idx: number) {
  const opts = (vm as { rowOptions: { series: { itemStyle: { color: string } }[] }[] }).rowOptions
  return opts[idx]?.series[0]?.itemStyle?.color
}

function trackColor(vm: unknown, idx: number) {
  const opts = (vm as { rowOptions: { series: { backgroundStyle?: { color?: string } }[] }[] }).rowOptions
  return String(opts[idx]?.series[0]?.backgroundStyle?.color ?? '')
}

function expectResolvedRgb(color: string) {
  expect(color).toMatch(/^rgb\(\d+,\s*\d+,\s*\d+\)$/)
  expect(color).not.toContain('var(')
}

const rankModels = [
  { modelKey: 'cursor-grok-4.5-high-fast', name: 'cursor-grok-4.5-high-fast', total: 800, filled: true },
  { modelKey: 'gpt-5.6-sol-medium', name: 'gpt-5.6-sol-medium', total: 600, filled: true },
  { modelKey: '未知/未分桶', name: '未知模型', total: 400, unknown: true },
  { modelKey: 'other', name: 'other', total: 200, other: true },
]

describe('TokenModelRank ECharts bars (g1.2)', () => {
  it('renders ECharts bar hosts without HTML width bars', () => {
    const wrapper = mount(TokenModelRank, {
      props: { models: rankModels },
      global: { plugins: [i18nZh()] },
    })
    expect(wrapper.findAll('[data-testid="token-model-rank-bar"]').length).toBe(4)
    expect(wrapper.findAll('[data-testid="mock-vchart"]').length).toBe(4)
    expect(wrapper.find('.h-full.transition-\\[width\\]').exists()).toBe(false)
    const unkFill = wrapper.find('[data-unknown="1"] .h-full')
    expect(unkFill.exists()).toBe(true)
    expect((unkFill.element as HTMLElement).style.backgroundColor.replace(/\s/g, '')).toMatch(/#71717A|rgb\(113,113,122\)/i)
    wrapper.unmount()
  })
})

describe('TokenModelRank hides filledTag (g2.2)', () => {
  it('zh-CN: filled/unknown/other rows have no 含补全; data-filled keeps #34D399 / #71717A', () => {
    expect(colorForModel(rankModels[0]!, 0)).toBe('#34D399')
    expect(colorForModel(rankModels[2]!, 2)).toBe('#71717A')
    expect(colorForModel(rankModels[3]!, 3)).toBe('#A1A1AA')

    const wrapper = mount(TokenModelRank, {
      props: { models: rankModels },
      global: { plugins: [i18nZh()] },
    })
    const root = wrapper.find('[data-testid="token-model-rank"]')
    expect(root.exists()).toBe(true)
    expectNoFilledTagCopy(root.text())
    expect(wrapper.html()).not.toContain('含补全')

    const filledRows = wrapper.findAll('[data-filled="1"]')
    expect(filledRows).toHaveLength(2)
    for (const row of filledRows) {
      expectNoFilledTagCopy(row.text())
      expect(row.text()).not.toContain('含补全')
      const idx = filledRows.indexOf(row)
      expect(barColor(wrapper.vm, idx)).toBe('#34D399')
      expect(row.find('.text-ok').exists()).toBe(true)
    }

    const unknownRow = wrapper.find('[data-unknown="1"]')
    expect(unknownRow.exists()).toBe(true)
    expect(unknownRow.attributes('data-filled')).toBe('0')
    expect(unknownRow.text()).toContain('未知模型')
    expectNoFilledTagCopy(unknownRow.text())
    expect(barColor(wrapper.vm, 2)).toBe('#71717A')

    const otherRow = wrapper.find('[data-other="1"]')
    expect(otherRow.exists()).toBe(true)
    expect(otherRow.text()).toContain('other（其余模型）')
    expectNoFilledTagCopy(otherRow.text())
    expect(barColor(wrapper.vm, 3)).toBe('#A1A1AA')

    expect(root.text()).toContain('cursor-grok-4.5-high-fast')
    expect(root.text()).toContain('gpt-5.6-sol-medium')
    wrapper.unmount()
  })

  it('en locale: no includes filled data; other bucket stays unlabeled; colors unchanged', () => {
    const wrapper = mount(TokenModelRank, {
      props: { models: rankModels },
      global: { plugins: [i18nEn()] },
    })
    const root = wrapper.find('[data-testid="token-model-rank"]')
    expectNoFilledTagCopy(root.text())
    expect(wrapper.html()).not.toContain('includes filled data')
    expect(wrapper.html()).not.toContain('含补全')

    const filledRows = wrapper.findAll('[data-filled="1"]')
    expect(filledRows).toHaveLength(2)
    for (let i = 0; i < filledRows.length; i++) {
      expectNoFilledTagCopy(filledRows[i]!.text())
      expect(barColor(wrapper.vm, i)).toBe('#34D399')
    }

    expect(barColor(wrapper.vm, 2)).toBe('#71717A')

    const otherRow = wrapper.find('[data-other="1"]')
    expect(otherRow.text()).toContain('other (remaining models)')
    expectNoFilledTagCopy(otherRow.text())
    wrapper.unmount()
  })

  it('other bucket does not gain filledTag even if filled=true (g2.2 other)', () => {
    const wrapper = mount(TokenModelRank, {
      props: {
        models: [{ modelKey: 'other', name: 'other', total: 50, other: true, filled: true }],
      },
      global: { plugins: [i18nZh()] },
    })
    const otherRow = wrapper.find('[data-other="1"]')
    expect(otherRow.attributes('data-filled')).toBe('1')
    expectNoFilledTagCopy(otherRow.text())
    expect(otherRow.text()).toContain('other（其余模型）')
    wrapper.unmount()
  })
})

describe('TokenModelRank unknown vs other (g3.3)', () => {
  it('qualifying unknown keeps 未知模型, data-unknown, gray text and #71717A bar', () => {
    const models = [
      { modelKey: 'claude-sonnet-4', name: 'claude-sonnet-4', total: 100 },
      { modelKey: '未知/未分桶', name: '未知模型', total: 80, unknown: true },
      { name: 'other', total: 30, other: true },
    ]
    expect(colorForModel(models[1]!, 1)).toBe('#71717A')
    expect(colorForModel(models[2]!, 2)).toBe('#A1A1AA')

    const wrapper = mount(TokenModelRank, {
      props: { models },
      global: { plugins: [i18nZh()] },
    })

    expect(wrapper.html()).not.toMatch(/「未知」与 other 不同/)
    expect(wrapper.html()).not.toMatch(/Unknown is not the same as other/i)
    expect(wrapper.html()).not.toMatch(/相关用量按其实际消耗参与排行/)

    const unk = wrapper.find('[data-unknown="1"]')
    expect(unk.exists()).toBe(true)
    expect(unk.attributes('data-other')).toBe('0')
    expect(unk.text()).toContain('未知模型')
    expect(unk.text()).toContain('未知')
    expect(unk.find('[data-testid="unknown-model-badge"]').exists()).toBe(true)
    expect(unk.text()).not.toContain('other（其余模型）')
    expect(unk.find('.text-txt3').exists()).toBe(true)
    expect(barColor(wrapper.vm, 1)).toBe('#71717A')
    const unkFill = unk.find('.h-full')
    expect((unkFill.element as HTMLElement).style.backgroundColor).toBe('#71717A')

    const other = wrapper.find('[data-other="1"]')
    expect(other.exists()).toBe(true)
    expect(other.attributes('data-unknown')).toBe('0')
    expect(other.text()).toContain('other（其余模型）')
    expect(barColor(wrapper.vm, 2)).toBe('#A1A1AA')
    wrapper.unmount()
  })

  it('configured alias: no unknown badge and not #71717A; data-unknown keeps distinction (g4.1)', () => {
    const models = [
      { modelKey: 'gpt-5', name: 'gpt-5', total: 100 },
      { modelKey: '未知/未分桶', name: 'gpt-5', total: 80, unknown: true },
    ]
    expect(colorForModel(models[1]!, 1)).toBe('#60A5FA')
    expect(colorForModel(models[1]!, 1)).not.toBe('#71717A')
    expect(colorForModel({ name: 'gpt-5', unknown: false }, 0)).not.toBe('#71717A')

    const wrapper = mount(TokenModelRank, {
      props: { models },
      global: { plugins: [i18nZh()] },
    })
    const unk = wrapper.find('[data-unknown="1"]')
    expect(unk.text()).toContain('gpt-5')
    expect(unk.find('[data-testid="unknown-model-badge"]').exists()).toBe(false)
    const nameRow = unk.find('.flex.min-w-0.items-center')
    expect(nameRow.classes()).not.toContain('text-txt3')
    expect(nameRow.classes()).toContain('text-txt')
    expect(barColor(wrapper.vm, 1)).toBe('#60A5FA')
    expect(barColor(wrapper.vm, 1)).not.toBe('#71717A')
    expect(wrapper.findAll('[data-testid="token-model-rank"] > li')).toHaveLength(2)
    wrapper.unmount()
  })

  it('configured alias + filled: main name and bar use #34D399, no badge (g4.1)', () => {
    const models = [
      { modelKey: '未知/未分桶', name: 'Auto', total: 400, unknown: true, filled: true },
      { modelKey: 'cursor-grok-4.5-high-fast', name: 'cursor-grok-4.5-high-fast', total: 200, filled: true },
    ]
    expect(colorForModel(models[0]!, 0)).toBe('#34D399')

    const wrapper = mount(TokenModelRank, {
      props: { models },
      global: { plugins: [i18nZh()] },
    })
    const unk = wrapper.find('[data-unknown="1"]')
    expect(unk.text()).toContain('Auto')
    expect(unk.text()).not.toContain('未知模型')
    expect(unk.find('[data-testid="unknown-model-badge"]').exists()).toBe(false)
    expect(unk.find('.text-ok').exists()).toBe(true)
    expect(barColor(wrapper.vm, 0)).toBe('#34D399')
    expect(barColor(wrapper.vm, 0)).not.toBe('#71717A')
    wrapper.unmount()
  })
})

describe('TokenModelRank track color (g1.1/g1.2/g2.1)', () => {
  it('backgroundStyle is theme rgb without var(), and fills stay #34D399 / #71717A / #A1A1AA (g2.2)', () => {
    document.documentElement.style.removeProperty('--c-elevated')
    document.documentElement.classList.remove('light')
    setTheme('light')

    const wrapper = mount(TokenModelRank, {
      props: { models: rankModels },
      global: { plugins: [i18nZh()] },
    })

    const light = trackColor(wrapper.vm, 0)
    expectResolvedRgb(light)
    expect(light.replace(/\s/g, '')).toBe(RANK_TRACK_LIGHT.replace(/\s/g, ''))
    for (let i = 0; i < rankModels.length; i++) {
      expect(trackColor(wrapper.vm, i).replace(/\s/g, '')).toBe(light.replace(/\s/g, ''))
    }
    expect(barColor(wrapper.vm, 0)).toBe('#34D399')
    expect(barColor(wrapper.vm, 1)).toBe('#34D399')
    expect(wrapper.find('[data-filled="1"] .text-ok').exists()).toBe(true)
    expect(barColor(wrapper.vm, 2)).toBe('#71717A')
    expect(barColor(wrapper.vm, 3)).toBe('#A1A1AA')

    setTheme('dark')
    const dark = trackColor(wrapper.vm, 0)
    expectResolvedRgb(dark)
    expect(dark.replace(/\s/g, '')).toBe(RANK_TRACK_DARK.replace(/\s/g, ''))
    expect(dark.replace(/\s/g, '')).not.toBe(light.replace(/\s/g, ''))
    expect(barColor(wrapper.vm, 0)).toBe('#34D399')

    document.documentElement.style.setProperty('--c-elevated', '210 210 214')
    setTheme('light')
    const parsed = trackColor(wrapper.vm, 0)
    expectResolvedRgb(parsed)
    expect(parsed.replace(/\s/g, '')).toBe('rgb(210,210,214)')
    expect(parsed).not.toContain('var(')

    document.documentElement.style.removeProperty('--c-elevated')
    wrapper.unmount()
  })
})
