import { describe, expect, it } from 'vitest'
import type { ArtifactTree } from '@/lib/shared/types'
import {
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

const tree: ArtifactTree = {
  count: 6,
  sessionCount: 2,
  workflows: [
    { workflowId: 'w1', workflowName: 'Build', count: 2 },
    { workflowId: 'w2', workflowName: ' ', count: 1 },
    { workflowId: 'w3', workflowName: 'Ship', count: 1 },
  ],
}

describe('workspace artifact scopes', () => {
  it('lists every workflow and one combined session bucket', () => {
    expect(buildArtifactTreeNodes(tree, { ...labels, all: '全部产物' }).map((row) => [row.id, row.label, row.count])).toEqual([
      ['all', '全部产物', 6], ['workflow:w1', 'Build', 2], ['workflow:w2', '未命名工作流', 1], ['workflow:w3', 'Ship', 1], ['session', 'Agent 会话产物', 2],
    ])
  })
  it.each([
    [{ kind: 'all' }, {}, 'all'],
    [{ kind: 'workflow', workflowId: 'w3' }, { workflow: 'w3' }, 'workflow:w3'],
    [{ kind: 'session' }, { session: '1' }, 'session'],
  ] as [ArtifactSelection, Record<string, string>, string][])('round trips %j', (selection, query, key) => {
    expect(selectionToQuery(selection)).toEqual(query)
    expect(selectionFromQuery(query)).toEqual(selection)
    expect(selectionTreeKey(selection)).toBe(key)
    expect(selectionFromTreeKey(key)).toEqual(selection)
  })
  it('maps selections to list scopes', () => {
    expect(selectionScope({ kind: 'all' })).toEqual({})
    expect(selectionScope({ kind: 'workflow', workflowId: 'w3' })).toEqual({ workflowId: 'w3' })
    expect(selectionScope({ kind: 'session' })).toEqual({ session: true })
  })
  it('falls back to all artifacts for missing workflows, without losing session artifacts', () => {
    expect(resolveArtifactSelection(tree, { kind: 'workflow', workflowId: 'deleted' })).toEqual({ kind: 'all' })
    expect(resolveArtifactSelection(tree, { kind: 'session' })).toEqual({ kind: 'session' })
    expect(resolveArtifactSelection(null, null)).toBeNull()
    expect(resolveArtifactSelection({ count: 0, sessionCount: 0, workflows: [] }, { kind: 'all' })).toBeNull()
    expect(buildArtifactTreeNodes(null, labels)).toEqual([])
    expect(selectionFromTreeKey('invalid')).toBeNull()
    expect(selectionTreeKey(null)).toBe('')
  })
  it('keeps correct aggregate and workflow counts for the breadcrumb', () => {
    const copy = { ...labels, all: '全部产物' }
    expect(describeSelection(tree, { kind: 'all' }, copy)).toEqual({ rootLabel: '全部产物', childLabel: '', count: 6 })
    expect(describeSelection(tree, { kind: 'workflow', workflowId: 'w3' }, copy)).toEqual({ rootLabel: '全部产物', childLabel: 'Ship', count: 1 })
    expect(describeSelection(tree, { kind: 'session' }, copy)?.count).toBe(2)
    expect(describeSelection(tree, null, copy)).toBeNull()
    expect(describeSelection(null, { kind: 'all' }, copy)).toBeNull()
  })
})
