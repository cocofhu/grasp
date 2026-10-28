import { describe, expect, it } from 'vitest'
import { mergePersistedAndLiveTurns, persistedCompletedLiveHuman } from './mergeClarifyLiveTurns'
import type { ClarifyTurn } from '@/lib/shared/types'

describe('mergePersistedAndLiveTurns', () => {
  it('appends live when persisted has not caught up', () => {
    const persisted: ClarifyTurn[] = [{ role: 'agent', text: '问', at: 't0' }]
    const live: ClarifyTurn[] = [
      { role: 'human', text: '答', at: 't1' },
      { role: 'agent', text: '流', at: 't2', streaming: true },
    ]
    expect(mergePersistedAndLiveTurns(persisted, live)).toEqual([...persisted, ...live])
  })

  it('dedupes human when props.turns catches up while agent still streaming', () => {
    const persisted: ClarifyTurn[] = [
      { role: 'agent', text: '问', at: 't0' },
      { role: 'human', text: '澄清意见甲', at: 't1' },
    ]
    const live: ClarifyTurn[] = [
      { role: 'human', text: '澄清意见甲', at: 't1-live' },
      { role: 'agent', text: '流式中…', at: 't2', streaming: true },
    ]
    const merged = mergePersistedAndLiveTurns(persisted, live)
    const humans = merged.filter((t) => t.role === 'human' && t.text === '澄清意见甲')
    expect(humans).toHaveLength(1)
    expect(merged[merged.length - 1].streaming).toBe(true)
    expect(merged[merged.length - 1].text).toBe('流式中…')
  })

  it('returns persisted only when live empty', () => {
    const persisted: ClarifyTurn[] = [{ role: 'agent', text: 'x', at: 't0' }]
    expect(mergePersistedAndLiveTurns(persisted, [])).toEqual(persisted)
  })

  it('drops live when persisted already completed the same human turn', () => {
    const persisted: ClarifyTurn[] = [
      { role: 'human', text: '改成绿的', at: 't1' },
      { role: 'agent', text: '标题已改为绿色（#16a34a）', at: 't2' },
    ]
    const live: ClarifyTurn[] = [
      { role: 'human', text: '改成绿的', at: 't1-live' },
      { role: 'agent', text: '', at: 't2-live', streaming: true },
    ]
    expect(persistedCompletedLiveHuman(persisted, '改成绿的')).toBe(true)
    const merged = mergePersistedAndLiveTurns(persisted, live)
    expect(merged.filter((t) => t.role === 'human' && t.text === '改成绿的')).toHaveLength(1)
    expect(merged.some((t) => t.streaming)).toBe(false)
    expect(merged[merged.length - 1].text).toContain('标题已改为绿色')
  })

  it('keeps live stream when a newer human is still in flight', () => {
    const persisted: ClarifyTurn[] = [
      { role: 'human', text: '改成绿的', at: 't1' },
      { role: 'agent', text: '已改绿', at: 't2' },
      { role: 'human', text: '改成绿的', at: 't3' },
    ]
    const live: ClarifyTurn[] = [
      { role: 'human', text: '改成绿的', at: 't3-live' },
      { role: 'agent', text: '流式中', at: 't4', streaming: true },
    ]
    expect(persistedCompletedLiveHuman(persisted, '改成绿的')).toBe(false)
    const merged = mergePersistedAndLiveTurns(persisted, live)
    expect(merged.filter((t) => t.role === 'human' && t.text === '改成绿的')).toHaveLength(2)
    expect(merged[merged.length - 1].streaming).toBe(true)
  })

  it('cover retry replaces trailing empty/failure agent with live stream (plan g2.1)', () => {
    const persisted: ClarifyTurn[] = [
      { role: 'human', text: '做登录', at: 't1' },
      { role: 'agent', text: '', at: 't2' },
    ]
    const live: ClarifyTurn[] = [
      { role: 'human', text: '做登录', at: 't1-live' },
      { role: 'agent', text: '', at: 't3', streaming: true },
    ]
    expect(persistedCompletedLiveHuman(persisted, '做登录')).toBe(true)
    const merged = mergePersistedAndLiveTurns(persisted, live)
    expect(merged.filter((t) => t.role === 'human')).toHaveLength(1)
    expect(merged.filter((t) => t.role === 'agent')).toHaveLength(1)
    expect(merged[merged.length - 1].streaming).toBe(true)
  })

  it('keeps a newer idle snapshot when persisted already completed the same human', () => {
    const persisted: ClarifyTurn[] = [
      { role: 'human', text: '继续', at: 't1' },
      { role: 'agent', text: '半截', at: 't2', thought: '旧思考' },
    ]
    const live: ClarifyTurn[] = [
      { role: 'human', text: '继续', at: 't1-live' },
      {
        role: 'agent',
        text: '已经写完',
        thought: '新思考',
        at: 't3',
        streaming: true,
        tools: [{ title: 'read_file', status: 'completed' }],
      },
    ]
    const merged = mergePersistedAndLiveTurns(persisted, live)
    expect(merged.filter((t) => t.role === 'human')).toHaveLength(1)
    expect(merged[merged.length - 1]).toMatchObject({
      text: '已经写完',
      thought: '新思考',
      streaming: true,
      tools: [{ title: 'read_file', status: 'completed' }],
    })
  })

  it('does not let a shorter live cover the persisted reply on the same human turn', () => {
    const persisted: ClarifyTurn[] = [
      { role: 'human', text: '继续', at: 't1' },
      { role: 'agent', text: '已落盘正文', at: 't2', thought: '落盘思考' },
    ]
    const live: ClarifyTurn[] = [
      { role: 'human', text: '继续', at: 't1-live' },
      { role: 'agent', text: '短', at: 't3', thought: '少', streaming: true },
    ]
    const merged = mergePersistedAndLiveTurns(persisted, live)
    expect(merged).toEqual(persisted)
    expect(merged.some((t) => t.text === '短')).toBe(false)
  })

  it('keeps one in-progress bubble when the live text matches the persisted reply', () => {
    const persisted: ClarifyTurn[] = [
      { role: 'human', text: '继续', at: 't1' },
      { role: 'agent', text: '短回复', at: 't2', thought: '旧思考' },
    ]
    const live: ClarifyTurn[] = [
      { role: 'human', text: '继续', at: 't1-live' },
      { role: 'agent', text: '短回复', at: 't3', thought: '旧思考', streaming: true },
    ]
    const merged = mergePersistedAndLiveTurns(persisted, live)
    expect(merged.filter((t) => t.text === '短回复')).toHaveLength(1)
    expect(merged[merged.length - 1]?.streaming).toBe(true)
    expect(merged.some((t) => t.role === 'human')).toBe(true)
  })

  it.each([
    ['更短的新回复', '旧回合的完整回复内容'],
    ['等长新回复', '旧回合的另一条回复'],
    ['一个明显比旧回复更长的新回合回复', '旧回复'],
  ])('keeps a new same-text turn before persistence catches up: %s', (reply, oldReply) => {
    const persisted: ClarifyTurn[] = [
      { role: 'human', text: '继续', at: 't1' },
      { role: 'agent', text: oldReply, at: 't2' },
    ]
    const live: ClarifyTurn[] = [
      { role: 'human', text: '继续', at: 't3' },
      { role: 'agent', text: reply, at: 't4', streaming: true },
    ]
    const merged = mergePersistedAndLiveTurns(persisted, live, persisted.length)
    expect(merged).toHaveLength(4)
    expect(merged.filter((t) => t.role === 'human' && t.text === '继续')).toHaveLength(2)
    expect(merged.at(-1)).toMatchObject({ text: reply, streaming: true })
  })

  it('keeps a later accepted row after the snapshot settles the in-flight turn', () => {
    const persisted: ClarifyTurn[] = [
      { role: 'human', text: '澄清意见甲', at: 't1' },
      { role: 'agent', text: '收到', at: 't2' },
    ]
    const live: ClarifyTurn[] = [
      { role: 'human', text: '澄清意见甲', at: 't1-live', localPending: true },
      { role: 'agent', text: '', at: 't2-live', streaming: true, localPending: true },
      { role: 'human', text: '澄清意见乙', at: 't3-live', localPending: true },
    ]
    const merged = mergePersistedAndLiveTurns(persisted, live)
    expect(merged.filter((t) => t.text === '澄清意见甲')).toHaveLength(1)
    expect(merged.filter((t) => t.text === '收到')).toHaveLength(1)
    expect(merged.filter((t) => t.text === '澄清意见乙')).toHaveLength(1)
    expect(merged.at(-1)?.role).toBe('human')
  })

  it('does not paint a later turn onto the settled reply', () => {
    const persisted: ClarifyTurn[] = [
      { role: 'human', text: '澄清意见甲', at: 't1' },
      { role: 'agent', text: '收到', at: 't2' },
    ]
    const live: ClarifyTurn[] = [
      { role: 'human', text: '澄清意见甲', at: 't1-live', localPending: true },
      { role: 'agent', text: '收到', at: 't2-live', streaming: true, localPending: true },
      { role: 'human', text: '澄清意见乙', at: 't3-live', localPending: true },
      { role: 'agent', text: '这是第二条更长的回复', at: 't4-live', streaming: true, localPending: true },
    ]
    const merged = mergePersistedAndLiveTurns(persisted, live)
    expect(merged.map((t) => t.text)).toEqual(['澄清意见甲', '收到', '澄清意见乙', '这是第二条更长的回复'])
  })

  it('keeps a later same-text row after the first copy is already persisted', () => {
    const persisted: ClarifyTurn[] = [
      { role: 'human', text: '继续', at: 't1' },
      { role: 'agent', text: '第一轮', at: 't2' },
    ]
    const live: ClarifyTurn[] = [
      { role: 'human', text: '继续', at: 't1-live', localPending: true },
      { role: 'agent', text: '', at: 't2-live', streaming: true, localPending: true },
      { role: 'human', text: '继续', at: 't3-live', localPending: true },
    ]
    const merged = mergePersistedAndLiveTurns(persisted, live)
    expect(merged.filter((t) => t.role === 'human' && t.text === '继续')).toHaveLength(2)
    expect(merged.filter((t) => t.text === '第一轮')).toHaveLength(1)
  })

  it('dedupes only the snapshot suffix belonging to the anchored live turn', () => {
    const oldTurns: ClarifyTurn[] = [
      { role: 'human', text: '继续', at: 't1' },
      { role: 'agent', text: '旧回复', at: 't2' },
    ]
    const persisted: ClarifyTurn[] = [
      ...oldTurns,
      { role: 'human', text: '继续', at: 't3' },
      { role: 'agent', text: '新回复', at: 't4' },
    ]
    const live: ClarifyTurn[] = [
      { role: 'human', text: '继续', at: 't3-live' },
      { role: 'agent', text: '新回复', at: 't4-live', streaming: true },
    ]
    const merged = mergePersistedAndLiveTurns(persisted, live, oldTurns.length)
    expect(merged).toHaveLength(4)
    expect(merged.filter((t) => t.role === 'human')).toHaveLength(2)
    expect(merged.filter((t) => t.text === '新回复')).toHaveLength(1)
  })
})

describe('accepted waiting turns during transcript refresh', () => {
  it('keeps every same-text waiter after the first request is persisted', () => {
    const persisted: ClarifyTurn[] = [
      { role: 'human', text: '继续', at: '1' },
      { role: 'agent', text: '第一轮回复', at: '2' },
    ]
    const live: ClarifyTurn[] = [
      { role: 'human', text: '继续', at: '1-live', localPending: true },
      { role: 'agent', text: '第一轮回复', at: '2-live' },
      { role: 'human', text: '继续', at: '3-live', localPending: true },
      { role: 'human', text: '继续', at: '4-live', localPending: true },
    ]
    const merged = mergePersistedAndLiveTurns(persisted, live)
    expect(merged.map((turn) => turn.text)).toEqual(['继续', '第一轮回复', '继续', '继续'])
  })

  it('deduplicates a persisted waiting user without dropping the later accepted waiter', () => {
    const persisted: ClarifyTurn[] = [
      { role: 'human', text: '当前请求', at: '1' },
      { role: 'agent', text: '当前回复', at: '2' },
      { role: 'human', text: '等待请求', at: '3' },
    ]
    const live: ClarifyTurn[] = [
      { role: 'human', text: '当前请求', at: '1-live', localPending: true },
      { role: 'agent', text: '当前回复', at: '2-live' },
      { role: 'human', text: '等待请求', at: '3-live', localPending: true },
      { role: 'human', text: '尚未落盘的请求', at: '4-live', localPending: true },
    ]
    const merged = mergePersistedAndLiveTurns(persisted, live)
    expect(merged.map((turn) => turn.text)).toEqual(['当前请求', '当前回复', '等待请求', '尚未落盘的请求'])
  })

  it('keeps different image-only requests when only the first image was persisted', () => {
    const firstImage = { data: 'Zmlyc3Q=', mimeType: 'image/png', name: 'first.png' }
    const secondImage = { data: 'c2Vjb25k', mimeType: 'image/png', name: 'second.png' }
    const persisted: ClarifyTurn[] = [
      { role: 'human', text: '', images: [firstImage], at: '1' },
      { role: 'agent', text: '第一张图的回复', at: '2' },
    ]
    const live: ClarifyTurn[] = [
      { role: 'human', text: '', images: [firstImage], at: '1-live', localPending: true },
      { role: 'agent', text: '第一张图的回复', at: '2-live' },
      { role: 'human', text: '', images: [secondImage], at: '3-live', localPending: true },
    ]
    const merged = mergePersistedAndLiveTurns(persisted, live)
    expect(merged.filter((turn) => turn.role === 'human').map((turn) => turn.images?.[0]?.name))
      .toEqual(['first.png', 'second.png'])
    expect(merged.filter((turn) => turn.role === 'agent')).toHaveLength(1)
  })
})
