import { describe, expect, it } from 'vitest'
import type { ArtifactTreeProject } from '@/lib/shared/types'
import {
  SESSION_CHILD_ID,
  buildArtifactTreeNodes,
  describeSelection,
  resolveArtifactSelection,
  selectionFromQuery,
  selectionFromTreeKey,
  selectionScope,
  selectionToQuery,
  selectionTreeKey,
  type ArtifactSelection,
} from './artifactTree'

const labels = { session: 'Agent 会话产物', unnamedWorkflow: '未命名工作流' }

const tree: ArtifactTreeProject[] = [
  {
    projectId: 'p1',
    projectName: 'Alpha',
    count: 5,
    sessionCount: 2,
    workflows: [
      { workflowId: 'w1', workflowName: 'Build', count: 2 },
      { workflowId: 'w2', workflowName: ' ', count: 1 },
    ],
  },
  { projectId: 'p2', projectName: '', count: 1, sessionCount: 0, workflows: [{ workflowId: 'w3', workflowName: 'Ship', count: 1 }] },
]

describe('buildArtifactTreeNodes', () => {
  it('maps workflows to children and appends the session bucket only when non-empty', () => {
    const nodes = buildArtifactTreeNodes(tree, labels)
    expect(nodes.map((n) => [n.id, n.label, n.count])).toEqual([
      ['p1', 'Alpha', 5],
      ['p2', 'p2', 1],
    ])
    expect(nodes[0].children).toEqual([
      { id: 'w1', label: 'Build', count: 2, icon: 'workflow' },
      { id: 'w2', label: '未命名工作流', count: 1, icon: 'workflow' },
      { id: SESSION_CHILD_ID, label: 'Agent 会话产物', count: 2, icon: 'robot' },
    ])
    expect(nodes[1].children?.map((c) => c.id)).toEqual(['w3'])
  })
})

describe('selection <-> query / tree key', () => {
  const cases: [ArtifactSelection, Record<string, string>, string][] = [
    [{ kind: 'project', projectId: 'p1' }, { project: 'p1' }, 'p:p1'],
    [{ kind: 'workflow', projectId: 'p1', workflowId: 'w1' }, { project: 'p1', workflow: 'w1' }, 'c:p1:w1'],
    [{ kind: 'session', projectId: 'p1' }, { project: 'p1', session: '1' }, `c:p1:${SESSION_CHILD_ID}`],
  ]

  it.each(cases)('round-trips %j', (sel, query, key) => {
    expect(selectionToQuery(sel)).toEqual(query)
    expect(selectionFromQuery(query)).toEqual(sel)
    expect(selectionTreeKey(sel)).toBe(key)
    expect(selectionFromTreeKey(key)).toEqual(sel)
  })

  it('ignores queries without a project and prefers workflow over session', () => {
    expect(selectionFromQuery({})).toBeNull()
    expect(selectionFromQuery({ workflow: 'w1' })).toBeNull()
    expect(selectionFromQuery({ project: 'p1', workflow: 'w1', session: '1' })).toEqual({
      kind: 'workflow',
      projectId: 'p1',
      workflowId: 'w1',
    })
    expect(selectionFromQuery({ project: ['p2', 'p1'] })).toEqual({ kind: 'project', projectId: 'p2' })
    expect(selectionTreeKey(null)).toBe('')
    expect(selectionFromTreeKey('bogus')).toBeNull()
  })

  it('maps selections to mutually exclusive list scopes', () => {
    expect(selectionScope({ kind: 'project', projectId: 'p1' })).toEqual({ projectId: 'p1' })
    expect(selectionScope({ kind: 'workflow', projectId: 'p1', workflowId: 'w1' })).toEqual({
      projectId: 'p1',
      workflowId: 'w1',
    })
    expect(selectionScope({ kind: 'session', projectId: 'p1' })).toEqual({ projectId: 'p1', session: true })
  })
})

describe('resolveArtifactSelection', () => {
  it('defaults to the first project and returns null for an empty tree', () => {
    expect(resolveArtifactSelection(tree, null)).toEqual({ kind: 'project', projectId: 'p1' })
    expect(resolveArtifactSelection([], { kind: 'project', projectId: 'p1' })).toBeNull()
  })

  it('keeps valid selections', () => {
    const wf: ArtifactSelection = { kind: 'workflow', projectId: 'p2', workflowId: 'w3' }
    expect(resolveArtifactSelection(tree, wf)).toEqual(wf)
    expect(resolveArtifactSelection(tree, { kind: 'session', projectId: 'p1' })).toEqual({
      kind: 'session',
      projectId: 'p1',
    })
  })

  it('falls back to the project when the child is unknown or empty', () => {
    expect(resolveArtifactSelection(tree, { kind: 'workflow', projectId: 'p2', workflowId: 'w1' })).toEqual({
      kind: 'project',
      projectId: 'p2',
    })
    expect(resolveArtifactSelection(tree, { kind: 'session', projectId: 'p2' })).toEqual({
      kind: 'project',
      projectId: 'p2',
    })
  })

  it('falls back to the first project when the project is unknown', () => {
    expect(resolveArtifactSelection(tree, { kind: 'workflow', projectId: 'gone', workflowId: 'w1' })).toEqual({
      kind: 'project',
      projectId: 'p1',
    })
  })
})

describe('describeSelection', () => {
  it('returns breadcrumb labels and the bucket count', () => {
    expect(describeSelection(tree, { kind: 'project', projectId: 'p1' }, labels)).toMatchObject({
      projectLabel: 'Alpha',
      childLabel: '',
      count: 5,
    })
    expect(
      describeSelection(tree, { kind: 'workflow', projectId: 'p1', workflowId: 'w1' }, labels),
    ).toMatchObject({ projectLabel: 'Alpha', childLabel: 'Build', count: 2 })
    expect(describeSelection(tree, { kind: 'session', projectId: 'p1' }, labels)).toMatchObject({
      childLabel: 'Agent 会话产物',
      count: 2,
    })
    expect(describeSelection(tree, null, labels)).toBeNull()
    expect(describeSelection(tree, { kind: 'project', projectId: 'gone' }, labels)).toBeNull()
  })
})
