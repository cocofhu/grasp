import { describe, expect, it } from 'vitest'
import {
  applyDeleteGroup,
  applyMoveAgent,
  applyRemoveAgentFromGroup,
  assignNeedsDraftConfirm,
  allGroupCollapseIds,
  ancestorGroupIdsForAgent,
  buildDefaultCollapsedSet,
  buildOrgTreeRows,
  classifyAssignTargets,
  groupPath,
  groupProjectLabel,
  mergeCollapsedWithOrgChange,
  pruneOrgToAgentGroups,
  recursiveMemberNames,
  shouldSyncDraftAfterAssign,
  unifiedProjectId,
  wouldCreateGroupCycle,
  setAgentMembership,
  UNGROUPED_ID,
} from './agentOrg'
import type { AgentOrg } from '../api/api'

const sample: AgentOrg = {
  revision: 1,
  groups: [
    { id: 'eng', name: '工程部' },
    { id: 'des', name: '设计组', parentGroupId: 'eng' },
    { id: 'prod', name: '产品部' },
    { id: 'des2', name: '设计组', parentGroupId: 'prod' },
  ],
  agents: {
    alice: { groupIds: ['eng', 'des'] },
    bob: { groupIds: ['des'] },
    carol: {},
  },
}

describe('agentOrg helpers', () => {
  it('builds path for duplicate names', () => {
    expect(groupPath(sample, 'des')).toBe('工程部 / 设计组')
    expect(groupPath(sample, 'des2')).toBe('产品部 / 设计组')
  })

  it('detects group cycles', () => {
    expect(wouldCreateGroupCycle(sample, 'eng', 'des')).toBe(true)
    expect(wouldCreateGroupCycle(sample, 'des', 'prod')).toBe(false)
  })

  it('applies move semantics and ungrouped clear', () => {
    let o = applyMoveAgent(sample, 'alice', 'eng', 'prod')
    expect(o.agents.alice.groupIds?.sort()).toEqual(['des', 'prod'])
    o = applyMoveAgent(o, 'alice', 'prod', '')
    expect(o.agents.alice).toBeUndefined()
  })

  it('removes from one group only', () => {
    let o = applyRemoveAgentFromGroup(sample, 'alice', 'eng')
    expect(o.agents.alice.groupIds).toEqual(['des'])
    o = applyRemoveAgentFromGroup(sample, 'bob', 'des')
    expect(o.agents.bob).toBeUndefined()
    expect(applyRemoveAgentFromGroup(sample, 'carol', '__ungrouped__')).toBe(sample)
  })

  it('deletes group with promote rules', () => {
    const o = applyDeleteGroup(sample, 'eng')
    expect(o.groups.find((g) => g.id === 'eng')).toBeUndefined()
    expect(o.groups.find((g) => g.id === 'des')?.parentGroupId).toBeFalsy()
  })

  it('renders multi-group and ungrouped rows', () => {
    const rows = buildOrgTreeRows(sample, ['alice', 'bob', 'carol'], new Set())
    expect(rows.some((r) => r.kind === 'agent' && r.name === 'alice' && r.multi)).toBe(true)
    expect(rows.some((r) => r.kind === 'ungrouped-header')).toBe(true)
    expect(rows.some((r) => r.kind === 'agent' && r.name === 'carol' && r.groupId === '__ungrouped__')).toBe(true)
  })

  it('default collapsed set folds all groups; selected agent expands ancestor path', () => {
    const all = allGroupCollapseIds(sample)
    expect(all.has('eng')).toBe(true)
    expect(all.has(UNGROUPED_ID)).toBe(true)

    const ancestors = ancestorGroupIdsForAgent(sample, 'bob')
    expect(ancestors.has('des')).toBe(true)
    expect(ancestors.has('eng')).toBe(true)

    const collapsed = buildDefaultCollapsedSet(sample, ['bob'])
    expect(collapsed.has('eng')).toBe(false)
    expect(collapsed.has('des')).toBe(false)
    expect(collapsed.has('prod')).toBe(true)

    const rows = buildOrgTreeRows(sample, ['alice', 'bob', 'carol'], collapsed)
    expect(rows.some((r) => r.kind === 'agent' && r.name === 'bob')).toBe(true)
    expect(rows.some((r) => r.kind === 'agent' && r.name === 'carol')).toBe(false)
    expect(rows.some((r) => r.kind === 'group' && r.id === 'prod' && r.collapsed)).toBe(true)
  })

  it('mergeCollapsedWithOrgChange keeps expanded known ids and folds new groups', () => {
    const known = new Set(['eng', 'des', 'prod', UNGROUPED_ID])
    const prevCollapsed = new Set(['eng', 'prod', UNGROUPED_ID])
    const expandedOrg: AgentOrg = {
      ...sample,
      groups: [...sample.groups!, { id: 'new_root', name: '新组' }],
    }
    const merged = mergeCollapsedWithOrgChange(expandedOrg, prevCollapsed, known)
    expect(merged.has('eng')).toBe(true)
    expect(merged.has('des')).toBe(false)
    expect(merged.has('new_root')).toBe(true)
    expect(merged.has(UNGROUPED_ID)).toBe(true)
  })
})

describe('recursive members + unique project label', () => {
  const nested: AgentOrg = {
    revision: 1,
    groups: [
      { id: 'root', name: 'Grasp项目组' },
      { id: 'pipe', name: 'Workflow', parentGroupId: 'root' },
      { id: 'des', name: '设计组', parentGroupId: 'root' },
      { id: 'empty', name: '空组', parentGroupId: 'root' },
    ],
    agents: {
      pm: { groupIds: ['root'] },
      review: { groupIds: ['pipe'] },
      tester: { groupIds: ['pipe'] },
      alice: { groupIds: ['des', 'root'] },
      bob: { groupIds: ['des'] },
    },
  }
  const names = ['pm', 'review', 'tester', 'alice', 'bob', 'orphan']

  it('collects nested descendants and dedupes multi-group agents', () => {
    expect(recursiveMemberNames(nested, 'pipe', names)).toEqual(['review', 'tester'])
    expect(recursiveMemberNames(nested, 'des', names)).toEqual(['alice', 'bob'])
    expect(recursiveMemberNames(nested, 'root', names)).toEqual(['alice', 'bob', 'pm', 'review', 'tester'])
    expect(recursiveMemberNames(nested, 'empty', names)).toEqual([])
  })

  it('unifiedProjectId: empty / unbound / mixed / unique / unresolved id', () => {
    expect(unifiedProjectId([], [{ name: 'a', projectId: 'p1' }])).toBe('')
    expect(unifiedProjectId(['a', 'b'], [
      { name: 'a', projectId: '' },
      { name: 'b', projectId: '' },
    ])).toBe('')
    expect(unifiedProjectId(['a', 'b'], [
      { name: 'a', projectId: 'github' },
      { name: 'b', projectId: 'figma' },
    ])).toBe('')
    expect(unifiedProjectId(['a', 'b'], [
      { name: 'a', projectId: 'github' },
      { name: 'b', projectId: '' },
    ])).toBe('')
    expect(unifiedProjectId(['a', 'b'], [
      { name: 'a', projectId: 'github' },
      { name: 'b', projectId: 'github' },
    ])).toBe('github')
    expect(unifiedProjectId(['a', 'b'], [
      { name: 'a', projectId: 'proj_dead' },
      { name: 'b', projectId: 'proj_dead' },
    ])).toBe('proj_dead')
  })

  it('groupProjectLabel resolves name or falls back to id; empty/mixed return empty', () => {
    const projects = [
      { id: 'github', name: 'GitHub' },
      { id: 'grasp', name: 'Grasp' },
    ]
    const allGithub = [
      { name: 'pm', projectId: 'github' },
      { name: 'review', projectId: 'github' },
      { name: 'tester', projectId: 'github' },
      { name: 'alice', projectId: 'github' },
      { name: 'bob', projectId: 'github' },
    ]
    expect(groupProjectLabel(nested, 'pipe', names, allGithub, projects)).toBe('GitHub')
    expect(groupProjectLabel(nested, 'root', names, allGithub, projects)).toBe('GitHub')
    expect(groupProjectLabel(nested, 'empty', names, allGithub, projects)).toBe('')

    const mixed = [
      { name: 'alice', projectId: 'figma' },
      { name: 'bob', projectId: 'github' },
    ]
    expect(groupProjectLabel(nested, 'des', names, mixed, projects)).toBe('')

    const dead = [
      { name: 'review', projectId: 'proj_dead' },
      { name: 'tester', projectId: 'proj_dead' },
    ]
    expect(groupProjectLabel(nested, 'pipe', names, dead, projects)).toBe('proj_dead')
  })

  it('buildOrgTreeRows.projectLabel + direct count badge stay independent', () => {
    const agents = [
      { name: 'pm', projectId: 'approving' },
      { name: 'review', projectId: 'github' },
      { name: 'tester', projectId: 'github' },
      { name: 'alice', projectId: 'figma' },
      { name: 'bob', projectId: 'github' },
    ]
    const projects = [
      { id: 'github', name: 'GitHub' },
      { id: 'figma', name: 'Figma' },
      { id: 'grasp', name: 'Grasp' },
    ]
    const rows = buildOrgTreeRows(nested, names, new Set(), agents, projects)
    const root = rows.find((r) => r.kind === 'group' && r.id === 'root')
    const pipe = rows.find((r) => r.kind === 'group' && r.id === 'pipe')
    const des = rows.find((r) => r.kind === 'group' && r.id === 'des')
    const empty = rows.find((r) => r.kind === 'group' && r.id === 'empty')
    expect(root?.kind === 'group' && root.count).toBe(2)
    expect(pipe?.kind === 'group' && pipe.count).toBe(2)
    expect(pipe?.kind === 'group' && pipe.projectLabel).toBe('GitHub')
    expect(des?.kind === 'group' && des.projectLabel).toBe('')
    expect(root?.kind === 'group' && root.projectLabel).toBe('')
    expect(empty?.kind === 'group' && empty.count).toBe(0)
    expect(empty?.kind === 'group' && empty.projectLabel).toBe('')
  })

  it('pruneOrgToAgentGroups drops foreign empty install groups (embedded studio)', () => {
    const org: AgentOrg = {
      revision: 1,
      groups: [
        { id: 'g_onb_proj-a', name: 'Alpha项目组' },
        { id: 'g_onb_proj-b', name: 'Beta项目组' },
        { id: 'g_child', name: '子组', parentGroupId: 'g_onb_proj-a' },
      ],
      agents: {
        'Alpha研发工程师': { groupIds: ['g_onb_proj-a'] },
        'Alpha测试工程师': { groupIds: ['g_child'] },
      },
    }
    const pruned = pruneOrgToAgentGroups(org, ['Alpha研发工程师', 'Alpha测试工程师'])
    expect(pruned.groups?.map((g) => g.id).sort()).toEqual(['g_child', 'g_onb_proj-a'])
    const rows = buildOrgTreeRows(pruned, ['Alpha研发工程师', 'Alpha测试工程师'], new Set())
    expect(rows.some((r) => r.kind === 'group' && r.id === 'g_onb_proj-b')).toBe(false)
    expect(rows.some((r) => r.kind === 'group' && r.id === 'g_onb_proj-a')).toBe(true)
  })

  it('classifyAssignTargets + draft sync helpers', () => {
    const agents = [
      { name: 'alice', projectId: 'figma' },
      { name: 'bob', projectId: 'github' },
      { name: 'scout', projectId: '' },
    ]
    const classified = classifyAssignTargets(['alice', 'bob', 'scout'], agents, 'github')
    expect(classified.already).toEqual(['bob'])
    expect(classified.unbound).toEqual(['scout'])
    expect(classified.diffBound).toEqual([{ name: 'alice', oldProjectId: 'figma' }])

    expect(assignNeedsDraftConfirm({
      activeName: 'alice',
      memberNames: ['alice', 'bob'],
      draftBindingDirty: true,
    })).toBe(true)
    expect(assignNeedsDraftConfirm({
      activeName: 'alice',
      memberNames: ['alice', 'bob'],
      draftBindingDirty: false,
    })).toBe(false)
    expect(assignNeedsDraftConfirm({
      activeName: 'orphan',
      memberNames: ['alice', 'bob'],
      draftBindingDirty: true,
    })).toBe(false)

    expect(shouldSyncDraftAfterAssign({
      activeName: 'alice',
      memberNames: ['alice', 'bob'],
      failNames: [],
      syncDraftRequested: true,
    })).toBe(true)
    expect(shouldSyncDraftAfterAssign({
      activeName: 'alice',
      memberNames: ['alice', 'bob'],
      failNames: ['alice'],
      syncDraftRequested: true,
    })).toBe(false)
    expect(shouldSyncDraftAfterAssign({
      activeName: 'alice',
      memberNames: ['alice', 'bob'],
      failNames: [],
      syncDraftRequested: false,
    })).toBe(false)
  })
})
