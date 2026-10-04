import { describe, expect, it } from 'vitest'
import {
  cloneAnnotations,
  cloneImages,
  dropGhostItems,
  isAuthoritativeIdle,
  reconcileQueue,
  takeTurnBeginItem,
  type SessionQueueItem,
} from './sessionQueue'

const q = (text: string, id?: string, extra: Partial<SessionQueueItem> = {}): SessionQueueItem => ({
  id,
  text,
  images: [],
  annotations: [],
  ...extra,
})

describe('sessionQueue', () => {
  it('detects authoritative idle', () => {
    expect(isAuthoritativeIdle(0, false, null)).toBe(true)
    expect(isAuthoritativeIdle(0, undefined, undefined)).toBe(true)
    expect(isAuthoritativeIdle(1, false, null)).toBe(false)
    expect(isAuthoritativeIdle(0, true, null)).toBe(false)
    expect(isAuthoritativeIdle(0, false, { id: 'a' })).toBe(false)
  })

  it('matches by id, then text for optimistic rows, and prefers frame attachments', () => {
    const img = { data: 'x', mimeType: 'image/png' }
    const local = [q('first', 'a', { images: [img] }), q('second')]
    const out = reconcileQueue(local, [{ id: 'a', text: 'first' }, { id: 'b', text: 'second', images: [] }], true)
    expect(out.map((r) => r.id)).toEqual(['a', 'b'])
    expect(out[0]!.images).toEqual([img])
    expect(out[0]!.images[0]).not.toBe(img)
    expect(out[1]!.images).toEqual([])
  })

  it('keeps at most one optimistic row ahead when nothing is in flight', () => {
    const local = [q('a', 'a'), q('b'), q('c')]
    expect(reconcileQueue(local, [{ id: 'a', text: 'a' }], false).map((r) => r.text)).toEqual(['a', 'b'])
    expect(reconcileQueue(local, [{ id: 'a', text: 'a' }], true).map((r) => r.text)).toEqual(['a'])
    expect(reconcileQueue([q('a')], [{ id: 'a', text: 'a' }, { id: 'b', text: 'b' }], false)).toHaveLength(2)
    expect(reconcileQueue([q('a', 'a'), q('x')], [{ id: 'a', text: 'a' }], false).map((r) => r.text)).toEqual([
      'a',
      'x',
    ])
  })

  it('turn_begin removes the named item without stealing a same-text waiter', () => {
    const local = [q('same', 'a'), q('same', 'b')]
    const byId = takeTurnBeginItem(local, { id: 'b', text: 'same' })
    expect(byId.taken?.id).toBe('b')
    expect(byId.queue.map((r) => r.id)).toEqual(['a'])

    const gone = takeTurnBeginItem([q('same', 'a')], { id: 'zz', text: 'same' })
    expect(gone.taken).toBeUndefined()
    expect(gone.queue).toHaveLength(1)

    const byText = takeTurnBeginItem([q('hi')], { text: 'hi' })
    expect(byText.taken?.text).toBe('hi')
    expect(byText.queue).toHaveLength(0)

    expect(takeTurnBeginItem([q('hi')], null).queue).toHaveLength(1)
  })

  it('drops only optimistic rows after a turn ends', () => {
    const real = [q('a', 'a')]
    expect(dropGhostItems(real)).toBe(real)
    expect(dropGhostItems([q('a', 'a'), q('ghost')]).map((r) => r.text)).toEqual(['a'])
  })

  it('clones attachment lists element-wise', () => {
    const ann = { kind: 'element' } as never
    expect(cloneAnnotations([ann])[0]).not.toBe(ann)
    expect(cloneAnnotations(null)).toEqual([])
    expect(cloneImages(undefined)).toEqual([])
  })
})
