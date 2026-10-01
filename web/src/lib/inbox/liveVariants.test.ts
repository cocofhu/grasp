import { describe, expect, it } from 'vitest'
import {
  EMBED_LIVE_MESSAGE,
  createLiveStore,
  isLiveBusy,
  isLiveOpen,
  parseEmbedLiveMessage,
  parseLiveRef,
  parseLiveSession,
  validLiveSid,
} from './liveVariants'

describe('liveVariants protocol', () => {
  it('validates sids', () => {
    expect(validLiveSid('abc123')).toBe(true)
    expect(validLiveSid('abc')).toBe(false)
    expect(validLiveSid('bad sid!')).toBe(false)
    expect(validLiveSid(42)).toBe(false)
  })

  it('classifies states', () => {
    expect(isLiveOpen('ready')).toBe(true)
    expect(isLiveOpen('failed')).toBe(true)
    expect(isLiveOpen('accepted')).toBe(false)
    expect(isLiveOpen(undefined)).toBe(false)
    expect(isLiveBusy('generating')).toBe(true)
    expect(isLiveBusy('ready')).toBe(false)
  })

  it('parses a generate request and drops junk', () => {
    const msg = parseEmbedLiveMessage({
      type: EMBED_LIVE_MESSAGE,
      op: 'generate',
      sid: 'sid001',
      reqId: 'r1',
      action: 'bolder',
      count: 3,
      prompt: 'bolder',
      url: 'http://app/',
      position: 'middle',
      notes: ['title', 7],
      params: { gap: 24 },
      element: {
        selector: 'main > section',
        tagName: 'section',
        id: 'hero',
        classes: ['card', 3],
        text: 'Dispatch',
        outerHTML: '<section>',
        styles: { color: '#fff', size: 3 },
      },
    })
    expect(msg?.kind).toBe('request')
    if (msg?.kind !== 'request') return
    expect(msg.reqId).toBe('r1')
    expect(msg.event).toMatchObject({ op: 'generate', sid: 'sid001', action: 'bolder', count: 3, url: 'http://app/', notes: ['title'] })
    expect(msg.event.position).toBeUndefined()
    expect(msg.event.element).toEqual({
      selector: 'main > section',
      tagName: 'section',
      id: 'hero',
      classes: ['card'],
      text: 'Dispatch',
      outerHTML: '<section>',
      styles: { color: '#fff' },
    })
    expect(msg.event.params).toEqual({ gap: 24 })
  })

  it('parses insert, accept, mount_failed and state', () => {
    const ins = parseEmbedLiveMessage({ type: EMBED_LIVE_MESSAGE, op: 'insert', sid: 'sid001', position: 'after', element: { selector: 'x' } })
    expect(ins?.kind === 'request' && ins.event.position).toBe('after')
    const acc = parseEmbedLiveMessage({ type: EMBED_LIVE_MESSAGE, op: 'accept', sid: 'sid001', variant: 2 })
    expect(acc?.kind === 'request' && acc.event.variant).toBe(2)
    const mf = parseEmbedLiveMessage({ type: EMBED_LIVE_MESSAGE, op: 'mount_failed', sid: 'sid001', error: 'boom' })
    expect(mf?.kind === 'request' && mf.event.error).toBe('boom')
    expect(mf?.kind === 'request' && mf.event.auto).toBeUndefined()
    const auto = parseEmbedLiveMessage({ type: EMBED_LIVE_MESSAGE, op: 'mount_failed', sid: 'sid001', error: 'boom', auto: true })
    expect(auto?.kind === 'request' && auto.event.auto).toBe(true)
    const notMf = parseEmbedLiveMessage({ type: EMBED_LIVE_MESSAGE, op: 'accept', sid: 'sid001', variant: 1, auto: true })
    expect(notMf?.kind === 'request' && notMf.event.auto).toBeUndefined()
    expect(parseLiveSession({ sid: 'sid001', state: 'ready', mountAutoReported: true })?.mountAutoReported).toBe(true)
    const st = parseEmbedLiveMessage({ type: EMBED_LIVE_MESSAGE, op: 'state', sid: 'sid001', current: 2, mode: 'compare' })
    expect(st).toEqual({ kind: 'state', sid: 'sid001', view: { current: 2, mode: 'compare' } })
    const st2 = parseEmbedLiveMessage({ type: EMBED_LIVE_MESSAGE, op: 'state', sid: 'sid001' })
    expect(st2).toEqual({ kind: 'state', sid: 'sid001', view: { current: 0, mode: 'inplace' } })
    const orig = parseEmbedLiveMessage({ type: EMBED_LIVE_MESSAGE, op: 'state', sid: 'sid001', current: 0, original: true })
    expect(orig).toEqual({ kind: 'state', sid: 'sid001', view: { current: 0, mode: 'inplace', original: true } })
    const bogus = parseEmbedLiveMessage({ type: EMBED_LIVE_MESSAGE, op: 'state', sid: 'sid001', current: 2, original: true })
    expect(bogus?.kind === 'state' && bogus.view.original).toBeUndefined()
  })

  it('rejects bad messages', () => {
    expect(parseEmbedLiveMessage(null)).toBeNull()
    expect(parseEmbedLiveMessage('x')).toBeNull()
    expect(parseEmbedLiveMessage({ type: 'other', op: 'generate', sid: 'sid001' })).toBeNull()
    expect(parseEmbedLiveMessage({ type: EMBED_LIVE_MESSAGE, op: 'generate', sid: 'x' })).toBeNull()
    expect(parseEmbedLiveMessage({ type: EMBED_LIVE_MESSAGE, op: 'explode', sid: 'sid001' })).toBeNull()
    const noEl = parseEmbedLiveMessage({ type: EMBED_LIVE_MESSAGE, op: 'generate', sid: 'sid001', element: { selector: ' ' } })
    expect(noEl?.kind === 'request' && noEl.event.element).toBeUndefined()
  })

  it('carries only scalar candidate params in the page state snapshot', () => {
    const message = parseEmbedLiveMessage({ type: EMBED_LIVE_MESSAGE, op: 'state', sid: 'sid001', current: 2, params: { gap: '32px', tone: 'strong', Bad: 'no', nested: { x: 1 } } })
    expect(message).toEqual({ kind: 'state', sid: 'sid001', view: { current: 2, mode: 'inplace', params: { gap: '32px', tone: 'strong' } } })
  })

  it('carries bounded visual annotations through the drawer and rejects malformed geometry', () => {
    const request = { type: EMBED_LIVE_MESSAGE, op: 'generate', sid: 'sid001', action: 'animate' }
    const marks = [
      { kind: 'draw', points: [{ x: 0.1, y: 0.2 }, { x: 0.8, y: 0.9 }], targets: [{ selector: 'button#book', text: 'Book now' }] },
      { kind: 'note', points: [{ x: 0.4, y: 0.6 }], text: 'Match the suites below' },
    ]
    const message = parseEmbedLiveMessage({ ...request, marks })
    expect(message?.kind === 'request' && message.event).toMatchObject({ action: 'animate', marks })
    marks[0].points[0].x = 0.9
    expect(message?.kind === 'request' && message.event.marks?.[0].points[0].x).toBe(0.1)
    for (const bad of [
      null, {}, Array(9).fill(marks[1]),
      [{ kind: 'script', points: [{ x: 0, y: 0 }] }],
      [{ kind: 'draw', points: [{ x: 0, y: 0 }] }],
      [{ kind: 'note', points: [{ x: Infinity, y: 0 }] }],
      [{ kind: 'note', points: [{ x: -0.1, y: 0 }] }],
      [{ kind: 'draw', points: Array(81).fill({ x: 0.2, y: 0.2 }) }],
      [{ ...marks[1], targets: [{ text: 'missing selector' }] }],
    ]) expect(parseEmbedLiveMessage({ ...request, marks: bad })).toBeNull()
  })

  it('parses sessions and refs', () => {
    expect(parseLiveSession(null)).toBeNull()
    expect(parseLiveSession({ sid: 'sid001' })).toBeNull()
    const s = parseLiveSession({ sid: 'sid001', state: 'ready', variants: [{ n: 1, label: 'a' }, { x: 1 }, null], selected: 1.5 })
    expect(s?.variants).toEqual([{ n: 1, label: 'a' }])
    expect(s?.mode).toBe('replace')
    expect(s?.selected).toBeUndefined()
    expect(parseLiveRef({ sid: 'sid001', op: 'accept', variant: 2 })).toEqual({ sid: 'sid001', op: 'accept', variant: 2 })
    expect(parseLiveRef({ sid: 'sid001', op: 'generate', variant: 0 })).toEqual({ sid: 'sid001', op: 'generate' })
    expect(parseLiveRef({ sid: 'x', op: 'accept' })).toBeUndefined()
    expect(parseLiveRef(undefined)).toBeUndefined()
    expect(parseLiveSession({ sid: 'sid001', state: 'failed', selected: 2, retryAccept: true })?.retryAccept).toBe(true)
    expect(parseLiveSession({ sid: 'sid001', state: 'failed', retryAccept: 'true' })?.retryAccept).toBe(false)
  })
})

describe('createLiveStore', () => {
  it('applies frames, keeps newest, tracks views and ctx', () => {
    const live = createLiveStore()
    expect(live.apply({ bad: true })).toBeNull()
    live.apply({ sid: 'sid001', state: 'ready', updatedAt: '2026-09-30T10:00:02Z', variants: [{ n: 1 }] })
    const stale = live.apply({ sid: 'sid001', state: 'generating', updatedAt: '2026-09-30T10:00:01Z' })
    expect(stale?.state).toBe('ready')
    expect(live.activeCtx()).toBeNull()
    live.setView('sid001', { current: 1, mode: 'inplace' })
    expect(live.activeCtx()).toEqual({ sid: 'sid001', current: 1 })
    live.apply({ sid: 'steer01', state: 'generating', mode: 'steer' })
    live.setView('steer01', { current: 1, mode: 'inplace' })
    live.apply({ sid: 'sid001', state: 'accepted', updatedAt: '2026-09-30T10:00:03Z' })
    expect(live.activeCtx()).toBeNull()
    expect(live.list()).toHaveLength(2)
    live.setEnabled(true)
    expect(live.store.enabled).toBe(true)
    expect(live.replaceAll([{ sid: 'sid002', state: 'ready' }, 'junk'])).toHaveLength(1)
    expect(Object.keys(live.store.sessions)).toEqual(['sid002'])
    expect(live.replaceAll(null)).toEqual([])
  })

  it('freezes current params for a chat message and clears ended or missing views', () => {
    const live = createLiveStore()
    live.apply({ sid: 'sid001', state: 'ready' })
    live.setView('sid001', { current: 2, mode: 'inplace', params: { gap: '32px' } })
    const queuedCtx = live.activeCtx()
    live.setView('sid001', { current: 1, mode: 'inplace', params: { gap: '16px' } })
    expect(queuedCtx).toEqual({ sid: 'sid001', current: 2, params: { gap: '32px' } })
    live.replaceAll([])
    expect(live.store.views).toEqual({})
    live.apply({ sid: 'sid001', state: 'ready' })
    live.setView('sid001', { current: 2, mode: 'inplace' })
    live.apply({ sid: 'sid001', state: 'accepted' })
    expect(live.store.views).toEqual({})
  })
})
