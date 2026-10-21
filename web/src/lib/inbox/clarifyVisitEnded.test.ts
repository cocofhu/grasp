// @vitest-environment happy-dom
import { describe, expect, it } from 'vitest'
import { createI18n } from 'vue-i18n'
import zhPages from '@/locales/zh-CN/pages.json'
import enPages from '@/locales/en/pages.json'
import {
  beginClarifySend,
  clarifyVisitEndedNotice,
  failClarifySend,
  handleClarifyVisitEnded,
  readDropped,
} from './clarifyVisitEnded'
import { readClarifyStash } from './clarifyDraftStash'
import { vi, afterEach } from 'vitest'

/** Stash / outbox keys only (the tab id and heartbeat live alongside). */
function countKeys(st: Storage, prefix: string): number {
  let n = 0
  for (let i = 0; i < st.length; i++) if (st.key(i)?.startsWith(prefix)) n++
  return n
}


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

describe('beginClarifySend / 409 outbox', () => {
  const zh = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': zhPages } }).global
  afterEach(() => {
    localStorage.clear()
    sessionStorage.clear()
  })

  it('gives ordinary sends an id + outbox entry; force/retryLast get none', () => {
    const sent = { text: 'hi', images: [], annotations: [] }
    expect(beginClarifySend('r', 'n', sent, { force: true })).toBeUndefined()
    expect(beginClarifySend('r', 'n', sent, { retryLast: true })).toBeUndefined()
    expect(countKeys(sessionStorage, 'grasp-clarify-outbox:')).toBe(0)
    const id = beginClarifySend('r', 'n', sent, {})!
    expect(id).toMatch(/\S/)
    failClarifySend('r', 'n', id)
    expect(countKeys(sessionStorage, 'grasp-clarify-outbox:')).toBe(0)
  })

  it('409: local copy first, then the outbox, then dropped (ref-only only as a last resort)', () => {
    const local = { data: 'aGk=', mimeType: 'image/png', name: 'a.png' }
    const id = beginClarifySend('r', 'n', { text: '原文', images: [local], annotations: [] }, {})
    const restore = vi.fn()
    const err = Object.assign(ended(false), {
      dropped: { clientMessageId: id, text: '原文', attachments: [{ ref: 'blob:1', mimeType: 'image/png' }] },
    })
    handleClarifyVisitEnded(err, zh.t, {
      chat: { restoreUnsentQueued: restore },
      runId: 'r',
      nodeId: 'n',
      sent: {},
      clientMessageId: id,
    })
    expect(restore.mock.calls[0][0]).toMatchObject({ text: '原文', images: [local] })
    // Consumed by the 409: no later frame can refill it again.
    expect(countKeys(sessionStorage, 'grasp-clarify-outbox:')).toBe(0)

    const restore2 = vi.fn()
    handleClarifyVisitEnded(
      Object.assign(ended(false), { dropped: { text: '服务端', attachments: [{ ref: 'blob:2', mimeType: 'image/png' }] } }),
      zh.t,
      { chat: { restoreUnsentQueued: restore2 }, runId: 'r', nodeId: 'n', sent: {} },
    )
    expect(restore2.mock.calls[0][0]).toMatchObject({ text: '服务端', images: [{ ref: 'blob:2', mimeType: 'image/png' }] })
  })

  it('no chat mounted: the toast also says which images could not be kept (S21)', () => {
    const big = { data: 'x'.repeat(300 * 1024), mimeType: 'image/png' }
    const r = handleClarifyVisitEnded(ended(false), zh.t, {
      chat: null,
      runId: 'r',
      nodeId: 'n',
      sent: { text: '带大图', images: [big] },
    })
    expect(r!.notice).toBe('运行已结束，这条消息没有发送；续跑后请重新发送\n有 1 张图片太大，没能一起保留，续跑后请重新添加')
  })
})
