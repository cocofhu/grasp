import { describe, expect, it } from 'vitest'
import { sandboxDisplayState, sandboxLifecycle, type SandboxDisplayInput } from './sandboxDisplayState'

const base: SandboxDisplayInput = { status: 'running', paused: false, waking: false, busy: false, queued: 0 }

describe('sandboxDisplayState', () => {
  it.each([
    [{ status: 'starting', sandboxStatus: 'pulling' }, { kind: 'preparing', tone: 'pending', action: null, step: 'pulling' }],
    [{ status: 'starting', sandboxStatus: 'creating' }, { kind: 'preparing', step: 'creating' }],
    [{ status: 'starting', sandboxStatus: 'running' }, { kind: 'preparing', action: null }],
    [{}, { kind: 'running', tone: 'ok', action: 'stop', actionDisabled: false }],
    [{ busy: true }, { kind: 'busy', tone: 'ok', action: 'stop', actionDisabled: true }],
    [{ queued: 2 }, { kind: 'busy', actionDisabled: true }],
    [{ paused: true }, { kind: 'paused', tone: 'idle', action: 'wake' }],
    [{ paused: true, waking: true }, { kind: 'waking', tone: 'pending', action: null }],
    [{ sandboxStatus: 'error' }, { kind: 'error', tone: 'err', action: 'restart' }],
    [{ status: 'stopping' }, { kind: 'stopping', tone: 'pending', action: null }],
    [{ status: 'stopped' }, { kind: 'stopped', tone: 'idle', action: 'resume' }],
    [{ status: 'closed' }, { kind: 'closed', tone: 'idle', action: 'restart' }],
    [{ status: 'error' }, { kind: 'error', tone: 'err', action: 'restart' }],
  ])('%o maps to %o', (patch, want) => {
    const got = sandboxDisplayState({ ...base, ...patch } as SandboxDisplayInput)
    expect(got).toMatchObject(want)
  })

  it('names a preparing step only while pulling or creating', () => {
    expect(sandboxDisplayState({ ...base, status: 'starting', sandboxStatus: 'running' })).not.toHaveProperty('step')
  })
})

describe('sandboxLifecycle', () => {
  const destroyAt = '2026-11-25T20:00:00Z'

  it('counts down an idle E2B sandbox', () => {
    const got = sandboxLifecycle({ sandbox: { id: 1, name: 'a', status: 'running', connected: true, hasCodeServer: true, recoveryKind: 'snapshot', destroyAt, modelActiveAt: '2026-11-25T14:00:00Z' } })
    expect(got).toMatchObject({ kind: 'snapshot', reclaim: 'countdown', connections: 0 })
    expect(got.destroyAt?.toISOString()).toBe('2026-11-25T20:00:00.000Z')
    expect(got.modelActiveAt?.toISOString()).toBe('2026-11-25T14:00:00.000Z')
  })

  it('reports why there is no countdown', () => {
    const sb = { id: 1, name: 'a', status: 'running', connected: true, hasCodeServer: true }
    expect(sandboxLifecycle({ sandbox: { ...sb, recoveryKind: 'files' } }).reclaim).toBe('exempt')
    expect(sandboxLifecycle({ sandbox: sb, idleReclaimExempt: true })).toMatchObject({ kind: 'files', reclaim: 'exempt' })
    expect(sandboxLifecycle({ sandbox: { ...sb, recoveryKind: 'snapshot' }, keepAlive: true }).reclaim).toBe('keepAlive')
    expect(sandboxLifecycle({ sandbox: { ...sb, recoveryKind: 'snapshot', externalSessions: 2 } })).toMatchObject({ reclaim: 'connected', connections: 2 })
    expect(sandboxLifecycle({ sandbox: { ...sb, destroyAt: 'not a date' } })).toMatchObject({ kind: 'unknown', reclaim: 'none' })
    expect(sandboxLifecycle({})).toMatchObject({ kind: 'unknown', reclaim: 'none', connections: 0 })
  })
})
