/**
 * Lock for the simplified "OpenCode provider and model" copy.
 *
 * plan coverage: g1.1 / g1.2 / g1.3 / g2.2 — the default-visible copy must be
 * plain-language and term-free, both locales must stay key-aligned, and the
 * technical details must live in the collapsible advanced note.
 */
import { describe, expect, it } from 'vitest'
import zhPages from '@/locales/zh-CN/pages.json'
import enPages from '@/locales/en/pages.json'

type Copy = Record<string, unknown>

const zh = (zhPages as { pages: { agentStudio: { openCode: Copy } } }).pages.agentStudio.openCode
const en = (enPages as { pages: { agentStudio: { openCode: Copy } } }).pages.agentStudio.openCode

// Copy a user sees without expanding any control. `providers` holds brand /
// option names, not explanatory copy, so it is out of the term scan.
const VISIBLE_KEYS = [
  'title',
  'desc',
  'providerLabel',
  'providerSearchPlaceholder',
  'providerSelfHosted',
  'modelLabel',
  'modelPlaceholder',
  'modelPlaceholderCustom',
  'modelHint',
  'modelHintTyped',
  'modelUnknown',
  'modelSearchPlaceholder',
  'modelRequired',
  'baseLabel',
  'basePlaceholder',
  'baseHint',
  'baseRequired',
  'visionLabel',
  'visionHint',
] as const

describe('openCode copy', () => {
  it('zh-CN and en expose the same openCode keys', () => {
    expect(Object.keys(zh).sort()).toEqual(Object.keys(en).sort())
  })

  it('default-visible copy drops the removed technical terms', () => {
    const forbiddenZh = ['ACP_BRIDGE_MODEL', '模型目录', 'models.dev', '厂商前缀', '聚合网关']
    const forbiddenEn = ['ACP_BRIDGE_MODEL', 'model catalog', 'models.dev', 'vendor prefix', 'aggregating']
    for (const key of VISIBLE_KEYS) {
      const zhText = String(zh[key])
      const enText = String(en[key]).toLowerCase()
      for (const term of forbiddenZh) {
        expect(zhText, `${key} should not contain ${term}`).not.toContain(term)
      }
      for (const term of forbiddenEn) {
        expect(enText, `${key} should not contain ${term}`).not.toContain(term.toLowerCase())
      }
    }
    expect(String(zh.modelLabel)).toBe('模型')
    expect(String(en.modelLabel)).toBe('Model')
  })

  it('model hint is one plain sentence plus a concrete example', () => {
    expect(String(zh.modelHint)).toContain('deepseek-flash')
    expect(String(en.modelHint)).toContain('deepseek-flash')
    expect(String(zh.modelHint)).toMatch(/选择|输入/)
    expect(String(en.modelHint)).toMatch(/pick|type/i)
  })

  it('advanced note carries the slash id and prefix details', () => {
    expect(zh.advancedSummary).toBeTruthy()
    expect(en.advancedSummary).toBeTruthy()
    expect(String(zh.advancedHint)).toContain('厂商前缀')
    expect(String(zh.advancedHint)).toContain('deepseek/deepseek-flash')
    expect(String(en.advancedHint).toLowerCase()).toContain('prefix')
    expect(String(en.advancedHint)).toContain('deepseek/deepseek-flash')
  })
})
