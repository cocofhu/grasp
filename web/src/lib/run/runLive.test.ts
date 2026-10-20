import { describe, expect, it } from 'vitest'
import type { NodeRun, Run } from '@/lib/shared/types'
import { decideLive, runningAutoNodeId, takeoverErrorCode, takeoverRaceLost, waitingNodeId } from './runLive'

const nr = (status: NodeRun['status'], extra: Partial<NodeRun> = {}) => ({ status, iteration: 1, ...extra }) as NodeRun
const run = (status: Run['status'], nodeRuns: Record<string, NodeRun>) => ({ id: 'r1', status, nodeRuns }) as unknown as Run

describe('decideLive', () => {
  it('observes a running auto node', () => {
    expect(decideLive(run('running', { impl: nr('running', { iteration: 2 }) }), 'impl')).toEqual({ kind: 'observe', nodeId: 'impl', iteration: 2 })
  })
  it('keeps observing (stopping) a taken-over node that is still winding down', () => {
    expect(decideLive(run('running', { impl: nr('running', { mode: 'manual' }) }), 'impl')).toEqual({ kind: 'observe', nodeId: 'impl', iteration: 1, stopping: true })
  })
  it('goes to the chat once the taken-over node waits for a person', () => {
    expect(decideLive(run('waiting_human', { impl: nr('waiting_human', { mode: 'manual' }) }), 'impl')).toEqual({ kind: 'chat', nodeId: 'impl' })
  })
  it('follows the run to the next auto node after confirm', () => {
    const r = run('running', { impl: nr('completed', { mode: 'manual' }), test: nr('running') })
    expect(decideLive(r, 'impl')).toEqual({ kind: 'follow', nodeId: 'test' })
    expect(decideLive(r, '')).toEqual({ kind: 'observe', nodeId: 'test', iteration: 1 })
  })
  it('goes to a node parked elsewhere, else run detail', () => {
    expect(decideLive(run('waiting_human', { impl: nr('completed'), gate: nr('waiting_human') }), 'impl')).toEqual({ kind: 'chat', nodeId: 'gate' })
    expect(decideLive(run('completed', { impl: nr('completed') }), 'impl')).toEqual({ kind: 'detail' })
  })
  it('stays put while the engine routes between nodes', () => {
    expect(decideLive(run('running', { impl: nr('completed') }), 'impl')).toEqual({ kind: 'observe', nodeId: 'impl', iteration: 1 })
  })
  it('never treats a manual running node as the next auto node', () => {
    expect(runningAutoNodeId(run('running', { impl: nr('running', { mode: 'manual' }) }))).toBe('')
    expect(waitingNodeId(run('waiting_human', { a: nr('completed'), b: nr('waiting_human') }))).toBe('b')
  })
})

describe('takeover error codes', () => {
  it('maps server codes and flags the races that should refresh instead of fail', () => {
    expect(takeoverErrorCode(new Error('node_finishing'))).toBe('node_finishing')
    expect(takeoverErrorCode(new Error('boom'))).toBeNull()
    expect(takeoverRaceLost('node_finishing')).toBe(true)
    expect(takeoverRaceLost('node_not_current')).toBe(true)
    expect(takeoverRaceLost('not_auto_node')).toBe(false)
    expect(takeoverRaceLost(null)).toBe(false)
  })
})
