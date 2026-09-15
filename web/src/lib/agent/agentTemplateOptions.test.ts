import { describe, expect, it } from 'vitest'
import { buildTemplateOptions } from './agentTemplateOptions'
import {
  assembleCreatePayload,
  freshDraft,
  hasRoleTemplate,
} from './agentCreateWizard'

describe('agentTemplateOptions (plan g1 / g2)', () => {
  it('orders blank → test → preflight → rest', () => {
    const opts = buildTemplateOptions(
      [
        { id: 'implement', embedName: 'ImplementAgent', roleLabelZh: '实现工程师', summary: 'x' },
        { id: 'test', embedName: 'TestAgent', roleLabelZh: '测试工程师', summary: 'y' },
        { id: 'preflight', embedName: 'PreflightAgent', roleLabelZh: '环境确认工程师', summary: 'z' },
        { id: 'clarify', embedName: 'ClarifyAgent', roleLabelZh: '澄清工程师', summary: 'c' },
      ],
      '空白',
      '通用身份 Rule',
    )
    expect(opts.map((o) => o.id)).toEqual(['blank', 'test', 'preflight', 'implement', 'clarify'])
    expect(opts[1].subtitle).toContain('set_test_result')
    expect(opts[2].subtitle).toContain('set_preflight')
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
