import { describe, expect, it } from 'vitest'
import {
  cloneAnnotations,
  cloneImages,
  createReviewStateCursor,
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
  it('rejects a delayed enqueue snapshot after several completed turns', () => {
    const cursor = createReviewStateCursor()
    const state = (revision: number) => ({ nodeId: 'clarify', sessionGeneration: 4, revision })
    expect(cursor.accept(state(1))).toBe(true)
    // A, B and C finish before B's captured queue snapshot is published.
    expect(cursor.accept(state(9))).toBe(true)
    expect(cursor.accept(state(3))).toBe(false)
    expect(cursor.accept(state(8))).toBe(false)
    expect(cursor.accept(state(9))).toBe(true)
  })

  it('isolates node cursors and rejects frames from retired FIFO instances', () => {
    const cursor = createReviewStateCursor()
    expect(cursor.accept({ nodeId: 'a', sessionGeneration: 8, revision: 40 })).toBe(true)
    expect(cursor.accept({ nodeId: 'b', sessionGeneration: 3, revision: 1 })).toBe(true)
    expect(cursor.accept({ nodeId: 'a', sessionGeneration: 9, revision: 1 })).toBe(true)
    expect(cursor.accept({ nodeId: 'a', sessionGeneration: 8, revision: 900 })).toBe(false)
    expect(cursor.accept({ nodeId: 'a', sessionGeneration: 9, revision: 0 })).toBe(false)
  })

  it('can rebase at a new server connection without dropping legacy local frames', () => {
    const cursor = createReviewStateCursor()
    expect(cursor.accept({ sessionGeneration: 500, revision: 4 })).toBe(true)
    expect(cursor.accept({})).toBe(true)
    cursor.reset()
    expect(cursor.accept({ sessionGeneration: 1, revision: 0 })).toBe(true)
  })

  it('does not let an unversioned server idle or rejection end a versioned active turn', () => {
    const cursor = createReviewStateCursor()
    expect(cursor.acceptServer({ nodeId: 'chat', event: 'turn_begin', sessionGeneration: 2, revision: 7 })).toBe(true)
    expect(cursor.acceptServer({ nodeId: 'chat', event: 'queue_state' })).toBe(false)
    expect(cursor.acceptServer({ nodeId: 'chat', event: 'error' })).toBe(false)
    expect(cursor.acceptServer({ nodeId: 'chat', event: 'request_rejected' })).toBe(true)
    // A local optimistic placeholder has no server version.
    expect(cursor.accept({ nodeId: 'chat', event: 'turn_begin' })).toBe(true)
  })

  it('rejects a delayed retired FIFO even when a new viewer never saw its terminal frame', () => {
    const cursor = createReviewStateCursor()
    cursor.installSnapshot({}, 30)
    expect(cursor.acceptServer({ nodeId: 'chat', event: 'queue_state', sessionGeneration: 30, revision: 4 })).toBe(false)
    expect(cursor.acceptServer({ nodeId: 'chat', event: 'turn_done', sessionGeneration: 29, revision: 900 })).toBe(false)
    expect(cursor.acceptServer({ nodeId: 'chat', event: 'turn_begin', sessionGeneration: 31, revision: 1 })).toBe(true)
  })

  it('retains captured active sessions while allowing sessions created after snapshot capture', () => {
    const cursor = createReviewStateCursor()
    cursor.installSnapshot({ active: { sessionGeneration: 12, revision: 5 } }, 20)
    expect(cursor.accept({ nodeId: 'active', sessionGeneration: 12, revision: 6 })).toBe(true)
    expect(cursor.accept({ nodeId: 'new', sessionGeneration: 21, revision: 1 })).toBe(true)
    cursor.installSnapshot({}, 20)
    expect(cursor.accept({ nodeId: 'active', sessionGeneration: 12, revision: 8 })).toBe(false)
    expect(cursor.accept({ nodeId: 'new', sessionGeneration: 21, revision: 2 })).toBe(true)
  })

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

  it('never adopts another tab same-text queue item as its own optimistic send', () => {
    const ownImage = { data: 'own-image', mimeType: 'image/png' }
    const local = [q('continue', undefined, { clientMessageId: 'own-send', images: [ownImage] })]
    const remote = reconcileQueue(local, [{ id: 'remote-item', clientMessageId: 'remote-send', text: 'continue' }], true)
    expect(remote[0]).toMatchObject({ id: 'remote-item', clientMessageId: 'remote-send', images: [] })
    const own = reconcileQueue(local, [{ id: 'own-item', clientMessageId: 'own-send', text: 'continue' }], true)
    expect(own[0]).toMatchObject({ id: 'own-item', clientMessageId: 'own-send', images: [ownImage] })
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
