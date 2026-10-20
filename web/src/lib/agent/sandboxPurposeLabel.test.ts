import { describe, expect, it } from 'vitest'
import { createI18n } from 'vue-i18n'
import zhCommon from '@/locales/zh-CN/common.json'
import zhPages from '@/locales/zh-CN/pages.json'
import enCommon from '@/locales/en/common.json'
import enPages from '@/locales/en/pages.json'
import { sandboxPurposeLabelKey, sandboxSourceTextKey } from './sandboxPurposeLabel'

describe('sandboxPurposeLabel', () => {
  it('maps agent to Workspace assistant keys, test to chat-test, run separate', () => {
    expect(sandboxPurposeLabelKey('agent')).toBe('pages.sandboxes.purpose.agent')
    expect(sandboxPurposeLabelKey('test')).toBe('pages.sandboxes.purpose.test')
    expect(sandboxPurposeLabelKey('run')).toBe('pages.sandboxes.purpose.run')

    expect(sandboxSourceTextKey('agent')).toBe('pages.sandboxes.source.pmConsult')
    expect(sandboxSourceTextKey('test')).toBe('pages.sandboxes.source.chatTest')
    expect(sandboxSourceTextKey('run')).toBeNull()
  })

  it('zh/en labels distinguish Workspace assistant from Agent chat test', () => {
    const zh = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...zhCommon, ...zhPages } },
    })
    const en = createI18n({
      legacy: false,
      locale: 'en',
      messages: { en: { ...enCommon, ...enPages } },
    })

    expect(zh.global.t(sandboxPurposeLabelKey('agent'))).toBe('工作区助手')
    expect(zh.global.t(sandboxPurposeLabelKey('test'))).toBe('测试')
    expect(zh.global.t(sandboxSourceTextKey('agent')!)).toBe('工作区助手咨询')
    expect(zh.global.t(sandboxSourceTextKey('test')!)).toBe('对话测试')

    expect(en.global.t(sandboxPurposeLabelKey('agent'))).toBe('Workspace assistant')
    expect(en.global.t(sandboxPurposeLabelKey('test'))).toBe('Test')
    expect(en.global.t(sandboxSourceTextKey('agent')!)).toBe('Workspace assistant consult')
    expect(en.global.t(sandboxSourceTextKey('test')!)).toBe('Chat test')

    expect(zh.global.t(sandboxPurposeLabelKey('agent'))).not.toMatch(/\bPM\b/)
    expect(en.global.t(sandboxPurposeLabelKey('agent'))).not.toMatch(/\bPM\b/)
  })
})
