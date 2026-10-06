import { describe, expect, it } from 'vitest'
import type { Artifact } from '@/lib/shared/types'
import { groupByRun, runIdShort, runSectionTitle } from './artifactGroups'

function artifact(overrides: Partial<Artifact> & Pick<Artifact, 'id' | 'createdAt'>): Artifact {
  return {
    name: `${overrides.id}.json`,
    kind: 'json',
    nodeId: 'test',
    runId: 'run-1',
    workflowId: 'wf-a',
    workflowName: '工作流 A',
    sizeBytes: 100,
    content: '',
    ...overrides,
  }
}

describe('groupByRun', () => {
  it('sorts run sections by latest artifact time desc', () => {
    const sections = groupByRun([
      artifact({ id: 'a1', createdAt: '2026-07-01T10:00:00Z', runId: 'run-old', runTitle: '旧 Run' }),
      artifact({ id: 'a2', createdAt: '2026-07-04T08:00:00Z', runId: 'run-new', runTitle: '新 Run' }),
      artifact({ id: 'a3', createdAt: '2026-07-03T12:00:00Z', runId: 'run-mid', runTitle: '中 Run' }),
    ])
    expect(sections.map((s) => s.runId)).toEqual(['run-new', 'run-mid', 'run-old'])
  })

  it('sorts items within a run by createdAt desc', () => {
    const sections = groupByRun([
      artifact({ id: 'a1', createdAt: '2026-07-01T10:00:00Z', runId: 'run-a' }),
      artifact({ id: 'a2', createdAt: '2026-07-04T08:00:00Z', runId: 'run-a' }),
    ])
    expect(sections[0].items.map((a) => a.id)).toEqual(['a2', 'a1'])
  })

  it('uses runIdShort when runTitle is empty', () => {
    expect(runSectionTitle('', 'run-abc123')).toBe('#abc123')
    expect(runIdShort('run-abc123')).toBe('#abc123')
  })

  it('returns [] for non-array without throwing', () => {
    expect(groupByRun(null as unknown as Artifact[])).toEqual([])
    expect(groupByRun({ items: [] } as unknown as Artifact[])).toEqual([])
  })
})
