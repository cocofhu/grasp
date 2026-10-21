// @vitest-environment happy-dom
import { describe, expect, it } from 'vitest'
import { createI18n } from 'vue-i18n'
import zhPages from '@/locales/zh-CN/pages.json'
import enPages from '@/locales/en/pages.json'
import { clarifyVisitEndedNotice, handleClarifyVisitEnded, readDropped } from './clarifyVisitEnded'
import { readClarifyStash } from './clarifyDraftStash'
import { vi, afterEach } from 'vitest'

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

describe('handleClarifyVisitEnded', () => {
  const zh = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': zhPages } }).global
  afterEach(() => localStorage.clear())

  it('hands the message to a mounted chat', () => {
    const restoreUnsentQueued = vi.fn()
    const r = handleClarifyVisitEnded(ended(false), zh.t, {
      chat: { restoreUnsentQueued },
      runId: 'r',
      nodeId: 'n',
      sent: { text: 'hi', images: [], annotations: [] },
    })
    expect(r).toEqual({ notice: '运行已结束，这条消息没有发送；续跑后请重新发送', shownInChat: true })
    expect(restoreUnsentQueued).toHaveBeenCalledWith(
      expect.objectContaining({ text: 'hi' }),
      '运行已结束，这条消息没有发送；续跑后请重新发送',
      { autoRetryOnResume: false },
    )
  })

  it('stashes by run+node when the chat is gone; nothing for autoRetryOnResume', () => {
    const r = handleClarifyVisitEnded(ended(false), zh.t, { chat: null, runId: 'r', nodeId: 'n', sent: { text: 'hi' } })
    expect(r?.shownInChat).toBe(false)
    expect(readClarifyStash('r', 'n')?.text).toBe('hi')
    localStorage.clear()
    handleClarifyVisitEnded(ended(true), zh.t, { chat: null, runId: 'r', nodeId: 'n', sent: { text: 'hi' } })
    expect(readClarifyStash('r', 'n')).toBeNull()
  })

  it('ignores other errors and reads dropped defensively', () => {
    expect(handleClarifyVisitEnded(new Error('x'), zh.t, { chat: null, runId: 'r', nodeId: 'n', sent: {} })).toBeNull()
    expect(readDropped(null)).toEqual({})
    expect(readDropped({ text: 1, clientId: '', attachments: 'x' })).toEqual({
      clientId: undefined,
      text: undefined,
      images: undefined,
      annotations: undefined,
    })
    expect(readDropped({ text: 'a', clientId: 'c', attachments: [{ data: 'x', mimeType: 'image/png' }] }).images).toHaveLength(1)
  })
})
