import { describe, expect, it } from 'vitest'
import type { Run, RunCurrentNode } from '@/lib/shared/types'
import { decideLive, runningAutoNode, takeoverConflictRunStatus, takeoverErrorCode, takeoverRaceLost } from './runLive'

const cur = (id: string, kind: RunCurrentNode['kind'], state: RunCurrentNode['state'], iteration = 1): RunCurrentNode =>
  ({ id, name: id, iteration, kind, mode: kind === 'agent' ? 'auto' : 'manual', state })
// nodeRuns is deliberately contradictory: the decision must come from currentNode only.
const run = (status: Run['status'], currentNode?: RunCurrentNode) =>
  ({ id: 'r1', status, currentNode, nodeRuns: { stale: { status: 'running', iteration: 9 } } }) as unknown as Run

describe('decideLive (server currentNode)', () => {
  it('observes a running auto node with its iteration', () => {
    expect(decideLive(run('running', cur('impl', 'agent', 'running', 2)), 'impl')).toEqual({ kind: 'observe', nodeId: 'impl', iteration: 2 })
  })
  it('keeps observing (stopping) a taken-over node that is still winding down', () => {
    expect(decideLive(run('running', cur('impl', 'takeover', 'stopping')), 'impl')).toEqual({ kind: 'observe', nodeId: 'impl', iteration: 1, stopping: true })
  })
  it('goes to the chat once the taken-over node waits for a person (or is busy in a manual turn)', () => {
    expect(decideLive(run('waiting_human', cur('impl', 'takeover', 'waiting_human')), 'impl')).toEqual({ kind: 'chat', nodeId: 'impl' })
    expect(decideLive(run('waiting_human', cur('impl', 'takeover', 'busy')), 'impl')).toEqual({ kind: 'chat', nodeId: 'impl' })
  })
  it('follows the run to the next auto node after confirm', () => {
    const r = run('running', cur('test', 'agent', 'running'))
    expect(decideLive(r, 'impl')).toEqual({ kind: 'follow', nodeId: 'test' })
    expect(decideLive(r, '')).toEqual({ kind: 'observe', nodeId: 'test', iteration: 1 })
  })
  it('goes to a gate / review parked elsewhere, else run detail', () => {
    expect(decideLive(run('waiting_human', cur('gate', 'gate', 'waiting_human')), 'impl')).toEqual({ kind: 'chat', nodeId: 'gate' })
    expect(decideLive(run('waiting_human', cur('impl', 'review', 'waiting_human')), 'impl')).toEqual({ kind: 'chat', nodeId: 'impl' })
    expect(decideLive(run('completed', cur('impl', 'agent', 'running')), 'impl')).toEqual({ kind: 'detail' })
    expect(decideLive(run('cancelled'), 'impl')).toEqual({ kind: 'detail' })
  })
  it('stays put (keeping its iteration) while the engine routes between nodes', () => {
    expect(decideLive(run('running'), 'impl')).toEqual({ kind: 'observe', nodeId: 'impl' })
    expect(decideLive(run('running'), '')).toEqual({ kind: 'detail' })
  })
  it('runningAutoNode is only an auto agent that is running', () => {
    expect(runningAutoNode(run('running', cur('impl', 'agent', 'running')))?.id).toBe('impl')
    expect(runningAutoNode(run('running', cur('impl', 'takeover', 'stopping')))).toBeNull()
    expect(runningAutoNode(run('waiting_human', cur('gate', 'gate', 'waiting_human')))).toBeNull()
    expect(runningAutoNode(run('running'))).toBeNull()
  })
  it('does not observe an automatic successor that has not started', () => {
    const entering = cur('impl', 'agent', 'entering')
    expect(runningAutoNode(run('waiting_human', entering))).toBeNull()
    expect(runningAutoNode(run('running', entering))).toBeNull()
    expect(decideLive(run('waiting_human', entering), 'clarify')).toEqual({ kind: 'detail' })
    expect(decideLive(run('running', entering), 'impl')).toEqual({ kind: 'detail' })
  })
})

describe('takeover error codes', () => {
  it('maps server codes and flags the races that should refresh instead of fail', () => {
    expect(takeoverErrorCode(new Error('node_finishing'))).toBe('node_finishing')
    expect(takeoverErrorCode(new Error('run_not_running'))).toBe('run_not_running')
    expect(takeoverErrorCode(new Error('boom'))).toBeNull()
    expect(takeoverRaceLost('node_finishing')).toBe(true)
    expect(takeoverRaceLost('node_not_current')).toBe(true)
    expect(takeoverRaceLost('run_not_running')).toBe(false)
    expect(takeoverRaceLost('not_auto_node')).toBe(false)
    expect(takeoverRaceLost(null)).toBe(false)
  })
  it('reads current.runStatus from a 409', () => {
    expect(takeoverConflictRunStatus(Object.assign(new Error('run_not_running'), { current: { runStatus: 'cancelled' } }))).toBe('cancelled')
    expect(takeoverConflictRunStatus(new Error('run_not_running'))).toBe('')
    expect(takeoverConflictRunStatus(null)).toBe('')
  })
})
