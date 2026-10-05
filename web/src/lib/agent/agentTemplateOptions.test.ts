import { describe, expect, it } from 'vitest'
import { buildTemplateOptions } from './agentTemplateOptions'
import {
  assembleCreatePayload,
  freshDraft,
  hasRoleTemplate,
} from './agentCreateWizard'

describe('agentTemplateOptions', () => {
  it('puts blank first and keeps the server template order with capabilities', () => {
    const caps = { interaction: 'clarify' as const, tools: ['ask_question' as const] }
    const opts = buildTemplateOptions(
      [
        { id: 'clarify', embedName: 'ClarifyAgent', roleLabelZh: '需求澄清', summary: 'c', capabilities: caps },
        { id: 'implement', embedName: 'ImplementAgent', roleLabelZh: '实现', summary: 'x' },
        { id: 'blank', embedName: 'Blank', roleLabelZh: '', summary: '' },
        { id: 'test_review', embedName: 'TestReviewAgent', roleLabelZh: '', summary: 'y' },
      ],
      '空白',
      '通用身份 Rule',
    )
    expect(opts.map((o) => o.id)).toEqual(['blank', 'clarify', 'implement', 'test_review'])
    expect(opts[0]).toMatchObject({ name: '空白', subtitle: '通用身份 Rule' })
    expect(opts[1]).toMatchObject({ name: '需求澄清', subtitle: 'ClarifyAgent', capabilities: caps })
    expect(opts[3].name).toBe('TestReviewAgent')
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
