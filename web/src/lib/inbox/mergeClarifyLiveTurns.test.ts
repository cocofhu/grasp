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

  it('does not let a different same-length live cover the persisted reply', () => {
    const persisted: ClarifyTurn[] = [
      { role: 'human', text: '继续', at: 't1' },
      { role: 'agent', text: '已落盘正文', at: 't2' },
    ]
    const live: ClarifyTurn[] = [
      { role: 'human', text: '继续', at: 't1-live' },
      { role: 'agent', text: '另一段正文', at: 't3', streaming: true },
    ]
    expect(mergePersistedAndLiveTurns(persisted, live)).toEqual(persisted)
  })
})
