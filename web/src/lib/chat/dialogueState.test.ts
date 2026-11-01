import { readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import {
  addOptimistic,
  applyFrame,
  prefixDigest,
  rejectOptimistic,
  render,
  versionLess,
  type DialogueState,
  type OptimisticSend,
} from './dialogueState'

type Fixture = { scenario: string; about: string; frames: DialogueState[]; final: DialogueState }

const dir = join(__dirname, '__fixtures__/dialogue')
const fixtures: Fixture[] = readdirSync(dir)
  .filter((f) => f.endsWith('.json'))
  .sort()
  .map((f) => JSON.parse(readFileSync(join(dir, f), 'utf8')) as Fixture)

function full(s: DialogueState): DialogueState {
  const { prefixLen: _l, prefixDigest: _d, ...rest } = s
  return { ...rest, mode: 'full' }
}

function mulberry32(seed: number) {
  return () => {
    seed |= 0
    seed = (seed + 0x6d2b79f5) | 0
    let t = Math.imul(seed ^ (seed >>> 15), 1 | seed)
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

function state(rows: { id: string; rev: number }[], seq: number, extra: Partial<DialogueState> = {}): DialogueState {
  return {
    type: 'dialogue',
    runId: 'r',
    nodeId: 'n',
    iteration: 1,
    epoch: 1,
    seq,
    mode: 'full',
    rows: rows.map((r) => ({ ...r, phase: 'sealed', role: 'human', text: r.id, at: '' })),
    turns: [],
    busy: false,
    done: false,
    ...extra,
  }
}

describe('dialogue fixtures (server contract)', () => {
  for (const fx of fixtures) {
    it(`${fx.scenario}: frames in order rebuild the final state without a resync`, () => {
      let cur: DialogueState | null = null
      for (const frame of fx.frames) {
        if (frame.mode === 'tail') expect(prefixDigest(cur!.rows.slice(0, frame.prefixLen))).toBe(frame.prefixDigest)
        const r = applyFrame(cur, frame)
        expect(r.resync).toBe(false)
        expect(r.applied).toBe(true)
        cur = r.state
      }
      expect(cur).toEqual(full(fx.final))
    })
  }

  for (const fx of fixtures) {
    it(`${fx.scenario}: shuffled, duplicated and dropped delivery converges to the latest version`, () => {
      const want = render(full(fx.final))
      for (let seed = 1; seed <= 200; seed++) {
        const rnd = mulberry32(seed * 7919 + fx.scenario.length)
        const sent: DialogueState[] = []
        for (const f of fx.frames) {
          const roll = rnd()
          if (roll < 0.2) continue
          sent.push(f)
          if (roll > 0.8) sent.push(f)
        }
        for (let i = sent.length - 1; i > 0; i--) {
          const j = Math.floor(rnd() * (i + 1))
          ;[sent[i], sent[j]] = [sent[j]!, sent[i]!]
        }
        sent.push(fx.frames[fx.frames.length - 1]!)
        let cur: DialogueState | null = null
        let shownSeq = -1
        for (const f of sent) {
          let r = applyFrame(cur, f)
          // REST returns the server's current state, never older than the frame.
          if (r.resync) r = applyFrame(r.state, full(fx.final))
          cur = r.state
          if (cur) {
            expect(cur.seq).toBeGreaterThanOrEqual(shownSeq)
            shownSeq = cur.seq
          }
        }
        expect(render(cur), `seed ${seed}`).toEqual(want)
      }
    })
  }
})

describe('applyFrame', () => {
  it('ignores frames that are not newer', () => {
    const a = state([{ id: 'x', rev: 1 }], 5)
    expect(applyFrame(a, state([], 5)).applied).toBe(false)
    expect(applyFrame(a, state([], 4)).state).toBe(a)
    expect(applyFrame(a, state([], 1, { epoch: 0 })).applied).toBe(false)
    expect(applyFrame(a, state([], 1, { epoch: 2 })).applied).toBe(true)
  })

  it('places a tail only on a matching prefix', () => {
    const a = state([{ id: 'a', rev: 1 }, { id: 'b', rev: 2 }], 2)
    const tail = state([{ id: 'c', rev: 3 }], 3, { mode: 'tail', prefixLen: 2, prefixDigest: prefixDigest(a.rows) })
    const ok = applyFrame(a, tail)
    expect(ok.state!.rows.map((r) => r.id)).toEqual(['a', 'b', 'c'])
    expect(ok.state!.mode).toBe('full')
    expect(ok.state!.prefixLen).toBeUndefined()

    const stale = state([{ id: 'a', rev: 1 }, { id: 'b', rev: 1 }], 2)
    const r = applyFrame(stale, tail)
    expect(r).toEqual({ state: stale, resync: true, applied: false })
    expect(applyFrame(null, tail).resync).toBe(true)
    expect(applyFrame(state([{ id: 'a', rev: 1 }, { id: 'b', rev: 2 }], 2, { iteration: 2 }), tail).resync).toBe(true)
  })

  it('hashes UTF-8 like the server', () => {
    expect(prefixDigest([])).toBe('0:811c9dc5')
    expect(prefixDigest([{ id: '轮:h', rev: 1 }])).toMatch(/^1:[0-9a-f]{8}$/)
    expect(prefixDigest([{ id: '轮:h', rev: 1 }])).not.toBe(prefixDigest([{ id: '轮:h', rev: 2 }]))
  })

  it('orders versions by epoch then seq', () => {
    expect(versionLess({ epoch: 1, seq: 9 }, { epoch: 2, seq: 0 })).toBe(true)
    expect(versionLess({ epoch: 2, seq: 1 }, { epoch: 2, seq: 1 })).toBe(false)
  })
})

describe('render', () => {
  const send = (id: string): OptimisticSend => ({ clientMessageId: id, text: id, images: [], annotations: [], at: 't' })

  it('shows an idle send as a pending request and queues the rest', () => {
    const v = render(state([], 1), [send('c1'), send('c2')])
    expect(v.rows.map((r) => [r.text, r.localPending])).toEqual([['c1', true]])
    expect(v.thinking).toBe(true)
    expect(v.queue.map((q) => [q.text, q.local])).toEqual([['c2', true]])
  })

  it('queues sends while busy and drops them once the server lists them', () => {
    const busy = state([], 1, {
      busy: true,
      turns: [
        { turnId: 't1', phase: 'running' },
        { turnId: 't2', phase: 'queued', position: 0, clientMessageId: 'c1', item: { text: 'c1' } },
      ],
    })
    let list = addOptimistic([], send('c1'))
    list = addOptimistic(list, send('c3'))
    const v = render(busy, list)
    expect(v.queue.map((q) => [q.id, q.text, !!q.local])).toEqual([['t2', 'c1', false], [undefined, 'c3', true]])
    expect(v.thinking).toBe(true)
    expect(render(busy, rejectOptimistic(list, 'c3')).queue).toHaveLength(1)
  })

  it('finds the streaming reply and turns ended without rows', () => {
    const s = state([], 1, { busy: true, turns: [{ turnId: 'gone', phase: 'ended', outcome: 'dropped' }] })
    s.rows = [
      { id: 't:h', phase: 'sealed', rev: 1, role: 'human', text: 'q', at: '', turnId: 't' },
      { id: 't:a0', phase: 'running', rev: 1, role: 'agent', text: 'a', at: '', turnId: 't', streaming: true },
    ]
    const v = render(s)
    expect(v.streamingIndex).toBe(1)
    expect(v.endedTurns.map((t) => t.turnId)).toEqual(['gone'])
  })
})
