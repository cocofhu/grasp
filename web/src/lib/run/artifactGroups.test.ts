import { describe, expect, it } from 'vitest'
import type { Artifact } from '@/lib/shared/types'
import {
  UNNAMED_GROUP_KEY,
  UNNAMED_WORKFLOW,
  buildGroups,
  countUnnamedArtifacts,
  groupByRun,
  groupKey,
  isUnnamed,
  resolveDefaultGroup,
  runIdShort,
  runSectionTitle,
  visibleGroups,
} from './artifactGroups'

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

describe('isUnnamed', () => {
  it('does not treat empty workflowName as unnamed when workflowId exists', () => {
    expect(isUnnamed(artifact({ id: 'a1', createdAt: '2026-01-01T00:00:00Z', workflowName: '' }))).toBe(false)
  })

  it('does not treat literal 未命名工作流 as unnamed when workflowId exists', () => {
    expect(
      isUnnamed(
        artifact({ id: 'a1', createdAt: '2026-01-01T00:00:00Z', workflowName: UNNAMED_WORKFLOW }),
      ),
    ).toBe(false)
  })

  it('treats missing workflowId as unnamed', () => {
    expect(
      isUnnamed(artifact({ id: 'a1', createdAt: '2026-01-01T00:00:00Z', workflowId: undefined })),
    ).toBe(true)
  })

  it('accepts named workflows', () => {
    expect(isUnnamed(artifact({ id: 'a1', createdAt: '2026-01-01T00:00:00Z' }))).toBe(false)
  })
})

describe('groupKey', () => {
  it('uses workflowId even when workflowName snapshot is empty', () => {
    expect(groupKey(artifact({ id: 'a1', createdAt: '2026-01-01T00:00:00Z', workflowName: '' }))).toBe(
      'wf-a',
    )
  })

  it('merges truly unnamed artifacts into __unnamed__', () => {
    expect(
      groupKey(artifact({ id: 'a1', createdAt: '2026-01-01T00:00:00Z', workflowId: undefined })),
    ).toBe(UNNAMED_GROUP_KEY)
  })

  it('uses workflowId for named artifacts', () => {
    expect(groupKey(artifact({ id: 'a1', createdAt: '2026-01-01T00:00:00Z', workflowId: 'wf-x' }))).toBe(
      'wf-x',
    )
  })
})

describe('buildGroups', () => {
  it('keeps empty-snapshot artifacts in separate groups per workflowId', () => {
    const artifacts = [
      artifact({ id: 'a1', createdAt: '2026-07-01T12:00:00Z', workflowId: 'wf-rand-a', workflowName: '' }),
      artifact({
        id: 'a2',
        createdAt: '2026-06-28T09:00:00Z',
        workflowId: 'wf-rand-b',
        workflowName: UNNAMED_WORKFLOW,
      }),
    ]
    const groups = buildGroups(artifacts)
    expect(groups).toHaveLength(2)
    expect(groups.map((g) => g.key).sort()).toEqual(['wf-rand-a', 'wf-rand-b'])
    expect(groups.every((g) => !g.isUnnamed)).toBe(true)
  })

  it('merges renamed workflow artifacts by workflowId with API title', () => {
    const wfId = 'wf-self-iter'
    const artifacts = [
      artifact({
        id: 'a1',
        createdAt: '2026-07-04T14:20:00Z',
        workflowId: wfId,
        workflowName: '自我迭代',
      }),
      artifact({
        id: 'a2',
        createdAt: '2026-07-03T09:10:00Z',
        workflowId: wfId,
        workflowName: UNNAMED_WORKFLOW,
      }),
      artifact({
        id: 'a3',
        createdAt: '2026-07-03T09:05:00Z',
        workflowId: wfId,
        workflowName: '',
      }),
      artifact({
        id: 'a4',
        createdAt: '2026-07-01T18:00:00Z',
        workflowId: undefined,
        workflowName: '',
      }),
    ]
    const groups = buildGroups(artifacts, { [wfId]: { name: '自我迭代' } })
    expect(groups).toHaveLength(2)
    const named = groups.find((g) => g.workflowId === wfId)
    const unnamed = groups.find((g) => g.isUnnamed)
    expect(named?.title).toBe('自我迭代')
    expect(named?.count).toBe(3)
    expect(unnamed?.count).toBe(1)
    expect(countUnnamedArtifacts(artifacts)).toBe(1)
  })

  it('groups orphan workflowId separately with latest workflowName snapshot', () => {
    const artifacts = [
      artifact({
        id: 'a1',
        createdAt: '2026-06-20T08:00:00Z',
        workflowId: 'wf-deleted',
        workflowName: '已删除工作流(旧名)',
      }),
      artifact({
        id: 'a2',
        createdAt: '2026-06-25T08:00:00Z',
        workflowId: 'wf-deleted',
        workflowName: '已删除工作流',
      }),
    ]
    const groups = buildGroups(artifacts)
    expect(groups).toHaveLength(1)
    expect(groups[0].workflowId).toBe('wf-deleted')
    expect(groups[0].title).toBe('已删除工作流')
  })

  it('sorts artifacts within group by createdAt desc', () => {
    const artifacts = [
      artifact({ id: 'a1', createdAt: '2026-01-01T00:00:00Z', workflowId: 'wf-a' }),
      artifact({ id: 'a2', createdAt: '2026-02-01T00:00:00Z', workflowId: 'wf-a' }),
    ]
    const groups = buildGroups(artifacts)
    expect(groups[0].artifacts.map((a) => a.id)).toEqual(['a2', 'a1'])
  })

  it('places unnamed group at bottom and named groups by count desc', () => {
    const artifacts = [
      artifact({ id: 'a1', createdAt: '2026-01-01T00:00:00Z', workflowId: 'wf-a', workflowName: 'A' }),
      artifact({ id: 'a2', createdAt: '2026-01-02T00:00:00Z', workflowId: 'wf-b', workflowName: 'B' }),
      artifact({ id: 'a3', createdAt: '2026-01-03T00:00:00Z', workflowId: 'wf-b', workflowName: 'B' }),
      artifact({ id: 'a4', createdAt: '2026-01-04T00:00:00Z', workflowId: 'wf-c', workflowName: '' }),
      artifact({ id: 'a5', createdAt: '2026-01-05T00:00:00Z', workflowId: undefined, workflowName: '' }),
    ]
    const groups = buildGroups(artifacts)
    expect(groups.map((g) => g.key)).toEqual(['wf-b', 'wf-a', 'wf-c', UNNAMED_GROUP_KEY])
  })

  it('prefers workflow API name over artifact snapshot', () => {
    const artifacts = [
      artifact({
        id: 'a1',
        createdAt: '2026-06-20T08:00:00Z',
        workflowId: 'wf-a',
        workflowName: '旧名称快照',
      }),
    ]
    const groups = buildGroups(artifacts, { 'wf-a': { name: 'API 当前名' } })
    expect(groups[0].title).toBe('API 当前名')
  })

  it('falls back to snapshot when workflow is deleted from API list', () => {
    const artifacts = [
      artifact({
        id: 'a1',
        createdAt: '2026-06-20T08:00:00Z',
        workflowId: 'wf-deleted',
        workflowName: '已删除工作流',
      }),
    ]
    const groups = buildGroups(artifacts, {})
    expect(groups[0].title).toBe('已删除工作流')
  })
})

describe('visibleGroups', () => {
  const allGroups = buildGroups([
    artifact({ id: 'a1', createdAt: '2026-01-01T00:00:00Z', workflowId: 'wf-a' }),
    artifact({ id: 'a2', createdAt: '2026-01-02T00:00:00Z', workflowId: 'wf-b' }),
  ])

  it('returns all groups when wfParam is empty', () => {
    expect(visibleGroups(allGroups, '')).toHaveLength(2)
  })

  it('returns only matching workflowId when wfParam is set', () => {
    const visible = visibleGroups(allGroups, 'wf-a')
    expect(visible).toHaveLength(1)
    expect(visible[0].workflowId).toBe('wf-a')
  })
})

describe('resolveDefaultGroup', () => {
  const artifacts = [
    artifact({ id: 'a1', createdAt: '2026-01-01T00:00:00Z', workflowId: 'wf-a' }),
    artifact({ id: 'a2', createdAt: '2026-01-02T00:00:00Z', workflowId: 'wf-b' }),
    artifact({ id: 'a3', createdAt: '2026-01-03T00:00:00Z', workflowId: 'wf-b' }),
  ]
  const groups = buildGroups(artifacts)

  it('picks group with most artifacts when wfParam is empty', () => {
    expect(resolveDefaultGroup(groups)?.workflowId).toBe('wf-b')
  })

  it('picks first group on tie (wf-b before wf-a when equal — here wf-b wins on count)', () => {
    const tied = buildGroups([
      artifact({ id: 'a1', createdAt: '2026-01-01T00:00:00Z', workflowId: 'wf-a' }),
      artifact({ id: 'a2', createdAt: '2026-01-02T00:00:00Z', workflowId: 'wf-b' }),
    ])
    const def = resolveDefaultGroup(tied)
    expect(def?.count).toBe(1)
    expect(def?.workflowId).toBe('wf-a')
  })

  it('returns unnamed group when highlightUnnamed is true', () => {
    const mixed = buildGroups([
      artifact({ id: 'a1', createdAt: '2026-01-01T00:00:00Z', workflowId: 'wf-a' }),
      artifact({ id: 'a2', createdAt: '2026-01-02T00:00:00Z', workflowId: undefined, workflowName: '' }),
    ])
    const def = resolveDefaultGroup(mixed, { highlightUnnamed: true })
    expect(def?.isUnnamed).toBe(true)
  })

  it('returns null for empty groups', () => {
    expect(resolveDefaultGroup([])).toBeNull()
  })

  it('excludes unnamed groups when picking default among mixed groups', () => {
    const mixed = buildGroups([
      artifact({ id: 'a1', createdAt: '2026-01-01T00:00:00Z', workflowId: 'wf-a' }),
      artifact({ id: 'a2', createdAt: '2026-01-02T00:00:00Z', workflowId: undefined, workflowName: '' }),
      artifact({ id: 'a3', createdAt: '2026-01-03T00:00:00Z', workflowId: undefined, workflowName: '' }),
    ])
    expect(resolveDefaultGroup(mixed)?.workflowId).toBe('wf-a')
  })
})

describe('countUnnamedArtifacts', () => {
  it('counts only artifacts without workflowId', () => {
    const artifacts = [
      artifact({ id: 'a1', createdAt: '2026-01-01T00:00:00Z' }),
      artifact({ id: 'a2', createdAt: '2026-01-02T00:00:00Z', workflowName: '' }),
      artifact({ id: 'a3', createdAt: '2026-01-03T00:00:00Z', workflowId: undefined, workflowName: '' }),
    ]
    expect(countUnnamedArtifacts(artifacts)).toBe(1)
  })
})

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
})

/** g1.1/g1.2: non-array payloads previously threw TypeError: x is not iterable (ArtifactsView/ArtifactPreview stack). */
describe('non-array artifact list hardening', () => {
  it('buildGroups returns [] for null/undefined/object without throwing', () => {
    expect(buildGroups(null as unknown as Artifact[])).toEqual([])
    expect(buildGroups(undefined as unknown as Artifact[])).toEqual([])
    expect(buildGroups({ items: [] } as unknown as Artifact[])).toEqual([])
  })

  it('groupByRun returns [] for non-array without throwing', () => {
    expect(groupByRun(null as unknown as Artifact[])).toEqual([])
    expect(groupByRun({ items: [] } as unknown as Artifact[])).toEqual([])
  })

  it('countUnnamedArtifacts returns 0 for non-array', () => {
    expect(countUnnamedArtifacts(null as unknown as Artifact[])).toBe(0)
    expect(countUnnamedArtifacts({} as unknown as Artifact[])).toBe(0)
  })
})
