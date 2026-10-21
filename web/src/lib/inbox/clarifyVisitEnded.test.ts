import { describe, expect, it } from 'vitest'
import { createI18n } from 'vue-i18n'
import zhPages from '@/locales/zh-CN/pages.json'
import enPages from '@/locales/en/pages.json'
import { clarifyVisitEndedNotice } from './clarifyVisitEnded'

const ended = (autoRetryOnResume?: boolean) =>
  Object.assign(new Error('运行已结束，这条消息没有发送'), { status: 409, code: 'clarify_visit_ended', autoRetryOnResume })

describe('clarifyVisitEndedNotice', () => {
  const zh = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': zhPages } }).global
  const en = createI18n({ legacy: false, locale: 'en', messages: { en: enPages } }).global

  it('asks to resend an ordinary message after resume', () => {
    expect(clarifyVisitEndedNotice(ended(false), zh.t)).toBe('运行已结束，这条消息没有发送；续跑后请重新发送')
    expect(clarifyVisitEndedNotice(ended(), zh.t)).toBe('运行已结束，这条消息没有发送；续跑后请重新发送')
    expect(clarifyVisitEndedNotice(ended(false), en.t)).toBe(
      'The run has ended, so this message was not sent. Send it again after resuming.',
    )
  })

  it('says the system first message is sent again automatically', () => {
    expect(clarifyVisitEndedNotice(ended(true), zh.t)).toBe('运行已结束，这条消息没有发送；续跑后会自动发送')
    expect(clarifyVisitEndedNotice(ended(true), en.t)).toBe(
      'The run has ended, so this message was not sent. It will be sent automatically after resuming.',
    )
  })

  it('ignores other errors', () => {
    expect(clarifyVisitEndedNotice(new Error('x'), zh.t)).toBeNull()
    expect(clarifyVisitEndedNotice(Object.assign(new Error('b'), { code: 'sandbox_busy' }), zh.t)).toBeNull()
  })
})
