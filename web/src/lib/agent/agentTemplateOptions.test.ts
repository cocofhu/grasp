import { describe, expect, it } from 'vitest'
import { createI18n } from 'vue-i18n'
import enPages from '@/locales/en/pages.json'
import zhPages from '@/locales/zh-CN/pages.json'
import { buildTemplateOptions } from './agentTemplateOptions'
import {
  assembleCreatePayload,
  freshDraft,
  hasRoleTemplate,
} from './agentCreateWizard'

const ROWS = [
  { id: 'clarify', embedName: 'ClarifyAgent', roleLabelZh: '需求澄清', summary: '多轮对话澄清需求', capabilities: { interaction: 'clarify' as const, tools: ['ask_question' as const] } },
  { id: 'implement', embedName: 'ImplementAgent', roleLabelZh: '实现', summary: 'x' },
  { id: 'blank', embedName: 'Blank', roleLabelZh: '', summary: '' },
  { id: 'test_review', embedName: 'TestReviewAgent', roleLabelZh: '测试评审', summary: 'y' },
  { id: 'custom_pack', embedName: 'CustomAgent', roleLabelZh: '', summary: 'server summary' },
]

function translator(locale: 'zh-CN' | 'en') {
  const i18n = createI18n({ legacy: false, locale, messages: { [locale]: locale === 'en' ? enPages : zhPages } })
  return { t: (k: string) => i18n.global.t(k), te: (k: string) => i18n.global.te(k) }
}

describe('agentTemplateOptions', () => {
  it('puts blank first and keeps the server template order with capabilities', () => {
    const { t, te } = translator('zh-CN')
    const opts = buildTemplateOptions(ROWS, t, te)
    expect(opts.map((o) => o.id)).toEqual(['blank', 'clarify', 'implement', 'test_review', 'custom_pack'])
    expect(opts[0]!.name).toBe(t('pages.agentStudio.wizard.basics.templateBlank'))
    expect(opts[1]).toMatchObject({ name: '需求澄清', capabilities: ROWS[0]!.capabilities })
    expect(opts[4]).toMatchObject({ name: 'CustomAgent', description: 'server summary' })
  })

  it('shows localized titles and descriptions in English, without internal names', () => {
    const { t, te } = translator('en')
    const opts = buildTemplateOptions(ROWS, t, te)
    for (const o of opts.slice(0, 4)) {
      expect(`${o.name} ${o.subtitle ?? ''} ${o.description ?? ''}`).not.toMatch(/[\u3400-\u9fff]/)
      expect(o.subtitle ?? '').not.toMatch(/Agent$/)
    }
    expect(opts.map((o) => o.name).slice(1, 4)).toEqual(['Clarify', 'Implement', 'Test & review'])
  })
})

describe('assembleCreatePayload templateId (plan g1.4 / g2.1)', () => {
  it('blank omits templateId and writes default rule file', () => {
    const d = freshDraft()
    d.name = 'qa-1'
    d.templateId = 'blank'
    const payload = assembleCreatePayload(d)
    expect(payload.templateId).toBeUndefined()
    expect(payload.files?.some((f) => f.path.includes('rules/'))).toBe(true)
  })

  it('test templateId does not rewrite name and skips default rule files', () => {
    const d = freshDraft()
    d.name = 'qa-1'
    d.templateId = 'test'
    expect(hasRoleTemplate(d)).toBe(true)
    const payload = assembleCreatePayload(d)
    expect(payload.name).toBe('qa-1')
    expect(payload.templateId).toBe('test')
    expect(payload.files?.some((f) => f.path.startsWith('rules/'))).toBe(false)
  })
})
