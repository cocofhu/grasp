import { describe, expect, it } from 'vitest'
import { mergePersistedAndLiveTurns, persistedCompletedLiveHuman } from './mergeClarifyLiveTurns'
import type { ClarifyTurn } from '@/lib/shared/types'

describe('mergePersistedAndLiveTurns', () => {
  it('keeps different send identities separate even when their complete text is identical', () => {
    const persisted: ClarifyTurn[] = [
      { role: 'human', text: '继续', at: '1', clientMessageId: 'tab-a' },
      { role: 'agent', text: '相同回复', at: '2', clientMessageId: 'tab-a' },
    ]
    const live: ClarifyTurn[] = [
      { role: 'human', text: '继续', at: '3', clientMessageId: 'tab-b' },
      { role: 'agent', text: '相同回复', at: '4', clientMessageId: 'tab-b', streaming: true },
    ]
    expect(persistedCompletedLiveHuman(persisted, '继续', 'tab-b')).toBe(false)
    expect(mergePersistedAndLiveTurns(persisted, live)).toEqual([...persisted, ...live])
  })

  it('reconciles by send identity after the server enriches the saved request', () => {
    const persisted: ClarifyTurn[] = [
      { role: 'human', text: 'server-rendered request', at: '1', clientMessageId: 'mine' },
      { role: 'agent', text: 'saved final answer', at: '2', clientMessageId: 'mine' },
    ]
    const live: ClarifyTurn[] = [
      { role: 'human', text: 'local request', at: '3', clientMessageId: 'mine' },
      { role: 'agent', text: 'partial', at: '4', clientMessageId: 'mine' },
    ]
    expect(mergePersistedAndLiveTurns(persisted, live)).toEqual(persisted)
  })

  it('matches the persisted active human by identity while its reply is still streaming', () => {
    const saved: ClarifyTurn = { role: 'human', text: 'server payload', at: '1', clientMessageId: 'request' }
    const human: ClarifyTurn = { role: 'human', text: 'composer payload', at: '1-local', clientMessageId: 'request' }
    const agent: ClarifyTurn = { role: 'agent', text: 'ongoing', at: '2', clientMessageId: 'request', streaming: true }
    expect(mergePersistedAndLiveTurns([saved], [human, agent])).toEqual([saved, agent])
  })
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

  it('recognizes completion only after the final agent segment of the latest human request', () => {
    const human: ClarifyTurn = { role: 'human', text: '修改统计信息', at: 't1' }
    const sealed: ClarifyTurn = { role: 'agent', text: '第一段修改完成', at: 't2', handoff: true }
    expect(persistedCompletedLiveHuman([human, sealed], human.text)).toBe(false)
    const final: ClarifyTurn = { role: 'agent', text: '正在检查', at: 't3', streaming: true }
    expect(persistedCompletedLiveHuman([human, sealed, final], human.text)).toBe(false)
    expect(persistedCompletedLiveHuman([human, sealed, { ...final, streaming: false }], human.text)).toBe(true)
    expect(persistedCompletedLiveHuman([human, sealed, { ...final, streaming: false }, { role: 'human', text: '下一条', at: 't4' }], human.text)).toBe(false)
  })

  it('keeps the active second segment before its queued user after the first segment is persisted', () => {
    const human: ClarifyTurn = { role: 'human', text: '修改统计信息', at: 't1' }
    const sealed: ClarifyTurn = { role: 'agent', text: '统计信息已修改', at: 't2', handoff: true }
    const active: ClarifyTurn = { role: 'agent', text: '', thought: '继续检查', at: 't3', streaming: true }
    const waiting: ClarifyTurn = { role: 'human', text: '调整暗色主题', at: 't4', localPending: true }
    expect(mergePersistedAndLiveTurns([human, sealed], [human, sealed, active, waiting]))
      .toEqual([human, sealed, active, waiting])
  })

  it('updates only the final persisted segment when the live final reply is longer', () => {
    const human: ClarifyTurn = { role: 'human', text: '继续', at: 't1' }
    const sealed: ClarifyTurn = { role: 'agent', text: '第一段已完成', at: 't2', handoff: true }
    const final: ClarifyTurn = { role: 'agent', text: '检查', at: 't3' }
    const active: ClarifyTurn = { ...final, text: '检查结果已更新', streaming: true }
    const waiting: ClarifyTurn = { role: 'human', text: '下一轮', at: 't4', localPending: true }
    expect(mergePersistedAndLiveTurns([human, sealed, final], [human, sealed, active, waiting]))
      .toEqual([human, sealed, active, waiting])
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

  it.each([true, false])('preserves a later server-started turn after reconnect settled an earlier partial reply (streaming=%s)', (streaming) => {
    const usage = { inputTokens: 1000, outputTokens: 200, cacheReadTokens: 300, cacheWriteTokens: 0 }
    const persisted: ClarifyTurn[] = [
      { role: 'human', text: '请补充测试结论。', at: 't1' },
      { role: 'agent', text: '全部测试通过，无需刷新。', at: 't2', usage },
    ]
    const nextHuman: ClarifyTurn = { role: 'human', text: '再检查可访问性。', at: 't3' }
    const nextAgent: ClarifyTurn = { role: 'agent', text: '继续检查可访问性。', at: 't4', streaming }
    const live: ClarifyTurn[] = [
      { role: 'human', text: '请补充测试结论。', at: 't1-live' },
      { role: 'agent', text: '正在执行验收测试。', at: 't2-live' },
      nextHuman,
      nextAgent,
    ]
    const merged = mergePersistedAndLiveTurns(persisted, live)
    expect(merged).toEqual([...persisted, nextHuman, nextAgent])
    expect(merged[1].usage).toEqual(usage)
    expect(merged[3].usage).toBeUndefined()
  })

  it('preserves a later same-text turn started by the server after an earlier copy was persisted', () => {
    const persisted: ClarifyTurn[] = [
      { role: 'human', text: '继续', at: 't1' },
      { role: 'agent', text: '第一轮已完成', at: 't2' },
    ]
    const live: ClarifyTurn[] = [
      { role: 'human', text: '继续', at: 't1-live' },
      { role: 'agent', text: '第一轮半截', at: 't2-live' },
      { role: 'human', text: '继续', at: 't3' },
      { role: 'agent', text: '正在执行第二轮', at: 't4', streaming: true },
    ]
    expect(mergePersistedAndLiveTurns(persisted, live)).toEqual([...persisted, ...live.slice(2)])
  })

  it('preserves every sealed agent segment and the active final segment of a later server turn', () => {
    const persisted: ClarifyTurn[] = [
      { role: 'human', text: '先验证功能', at: 't1' },
      { role: 'agent', text: '功能验证通过', at: 't2' },
    ]
    const live: ClarifyTurn[] = [
      { role: 'human', text: '先验证功能', at: 't1-live' },
      { role: 'agent', text: '正在验证', at: 't2-live' },
      { role: 'human', text: '再检查可访问性', at: 't3' },
      { role: 'agent', text: '开始检查键盘导航', at: 't4', handoff: true },
      { role: 'agent', text: '发现焦点需要调整', at: 't5', handoff: true },
      { role: 'agent', text: '正在验证修复结果', at: 't6', streaming: true },
      { role: 'human', text: '最后检查移动端', at: 't7', localPending: true },
    ]
    expect(mergePersistedAndLiveTurns(persisted, live)).toEqual([...persisted, ...live.slice(2)])
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
