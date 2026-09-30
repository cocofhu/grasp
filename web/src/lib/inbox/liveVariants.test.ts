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
    const st = parseEmbedLiveMessage({ type: EMBED_LIVE_MESSAGE, op: 'state', sid: 'sid001', current: 2, mode: 'compare' })
    expect(st).toEqual({ kind: 'state', sid: 'sid001', view: { current: 2, mode: 'compare' } })
    const st2 = parseEmbedLiveMessage({ type: EMBED_LIVE_MESSAGE, op: 'state', sid: 'sid001' })
    expect(st2).toEqual({ kind: 'state', sid: 'sid001', view: { current: 0, mode: 'inplace' } })
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
})
