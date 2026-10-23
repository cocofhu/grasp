import { describe, expect, it } from 'vitest'
import {
  findInboxItemByKey,
  findInboxItemForHandoff,
  findInboxItemForQuery,
  inboxItemKey,
  inboxQueryKey,
  inboxTripleKey,
  isInboxLeftPending,
  pickNextActiveAfterRemove,
} from './inboxActiveSelection'

type Item = { runId: string; nodeId: string; iteration?: number; label: string }

function item(id: string, iteration = 1): Item {
  return { runId: `run-${id}`, nodeId: `node-${id}`, iteration, label: id }
}

describe('pickNextActiveAfterRemove', () => {
  it('middle item → next neighbor', () => {
    const list = [item('a'), item('b'), item('c')]
    const next = pickNextActiveAfterRemove(list, inboxItemKey(list[1]))
    expect(next?.label).toBe('c')
  })

  it('last item → previous neighbor', () => {
    const list = [item('a'), item('b'), item('c')]
    const next = pickNextActiveAfterRemove(list, inboxItemKey(list[2]))
    expect(next?.label).toBe('b')
  })

  it('only item → null', () => {
    const list = [item('solo')]
    expect(pickNextActiveAfterRemove(list, inboxItemKey(list[0]))).toBeNull()
  })

  it('first item → next neighbor', () => {
    const list = [item('a'), item('b')]
    const next = pickNextActiveAfterRemove(list, inboxItemKey(list[0]))
    expect(next?.label).toBe('b')
  })

  it('unknown key falls back to first remaining', () => {
    const list = [item('a'), item('b')]
    const next = pickNextActiveAfterRemove(list, 'missing')
    expect(next?.label).toBe('a')
  })
})

describe('inbox keys', () => {
  it('builds item and triple keys', () => {
    const it = item('x', 2)
    expect(inboxItemKey(it)).toBe('run-x:node-x')
    expect(inboxTripleKey(it)).toBe('run-x:node-x:2')
    expect(inboxTripleKey({ runId: 'r', nodeId: 'n' })).toBe('r:n:1')
  })

  it('builds query keys and finds the matching card', () => {
    expect(inboxQueryKey(' run-x ', ' node-x ')).toBe('run-x:node-x')
    expect(inboxQueryKey('', 'node-x')).toBe('')
    expect(inboxQueryKey('run-x', '')).toBe('')
    const list = [item('a'), item('b')]
    expect(findInboxItemByKey(list, 'run-b:node-b')?.label).toBe('b')
    expect(findInboxItemByKey(list, '')).toBeUndefined()
  })

  it('finds a handoff card by run+node, or by run when node id is empty', () => {
    const list = [item('a'), item('b')]
    expect(findInboxItemForHandoff(list, { runId: 'run-b', nodeId: 'node-b' })?.label).toBe('b')
    expect(findInboxItemForHandoff(list, { runId: 'run-a', nodeId: '' })?.label).toBe('a')
    expect(findInboxItemForHandoff(list, { runId: 'missing', nodeId: '' })).toBeUndefined()
  })

  it('finds a query card by run+node, or by run when the node id does not match', () => {
    const list = [item('a'), item('b')]
    expect(findInboxItemForQuery(list, 'run-b', 'node-b')?.label).toBe('b')
    expect(findInboxItemForQuery(list, 'run-a', 'guessed')?.label).toBe('a')
    expect(findInboxItemForQuery(list, 'run-a', '')?.label).toBe('a')
    expect(findInboxItemForQuery(list, '', 'node-a')).toBeUndefined()
    expect(findInboxItemForQuery(list, 'missing', 'node-x')).toBeUndefined()
  })
})

describe('isInboxLeftPending', () => {
  it('matches the success leave marker and ignores error text', () => {
    expect(isInboxLeftPending({ pending: false })).toBe(true)
    expect(isInboxLeftPending({ pending: false, type: 'gate' })).toBe(false)
    expect(isInboxLeftPending({ type: 'clarify', status: 'waiting_human' })).toBe(false)
    expect(isInboxLeftPending(null)).toBe(false)
    expect(isInboxLeftPending(new Error('no pending inbox item'))).toBe(false)
    expect(isInboxLeftPending('no pending inbox item')).toBe(false)
  })
})
