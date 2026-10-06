import { describe, expect, it } from 'vitest'
import {
  buildEnvelope,
  sanitizeFilename,
  collectAgentProfiles,
  agentProfileIssues,
  getAgentProfile,
  setAgentProfile,
} from './workflowIO'
import type { WFNode } from '../shared/types'

describe('sanitizeFilename', () => {
  it('replaces illegal chars and adds .json', () => {
    expect(sanitizeFilename('CI/CD 工作流')).toBe('CI_CD 工作流.json')
  })
})

describe('buildEnvelope', () => {
  it('strips runtime fields and lifts variables from input config', () => {
    const nodes: WFNode[] = [
      {
        id: 'in',
        type: 'input',
        label: 'Start',
        position: { x: 0, y: 0 },
        config: { variables: [{ name: 'repo_url', type: 'string' }] },
      },
      { id: 'out', type: 'output', label: 'End', position: { x: 0, y: 0 }, config: {} },
    ]
    const env = buildEnvelope(
      { name: 'Demo', description: 'd', needsRepo: true },
      { nodes, edges: [{ id: 'e1', source: 'in', target: 'out' }] },
    )
    expect(env.schemaVersion).toBe(1)
    expect(env.name).toBe('Demo')
    expect(env.graph.variables).toHaveLength(1)
    expect(env.graph.nodes[0].config.variables).toBeUndefined()
    expect(env.exportedAt).toBeTruthy()
  })
})

describe('agent profile helpers', () => {
  const nodes: WFNode[] = [
    { id: 'a', type: 'agent', label: 'I', position: { x: 0, y: 0 }, config: { agent_profile: 'ImplementAgent' } },
    { id: 'b', type: 'input', label: 'In', position: { x: 0, y: 0 }, config: {} },
    { id: 'c', type: 'agent', label: 'P', position: { x: 0, y: 0 }, config: { agent_profile: 'PreviewAgent' } },
  ]

  it('collects agent node profiles', () => {
    expect(collectAgentProfiles(nodes).sort()).toEqual(['ImplementAgent', 'PreviewAgent'].sort())
  })

  it('reports missing profiles by name via agentProfileIssues', () => {
    const issues = agentProfileIssues(nodes, [{ name: 'Other', projectId: 'p' }], 'p')
    expect(issues.filter((i) => i.reason === 'missing').map((i) => i.name).sort()).toEqual(
      ['ImplementAgent', 'PreviewAgent'].sort(),
    )
  })

  it('reports missing or foreign agent profiles for import warn', () => {
    const agents = [
      { name: 'ImplementAgent', projectId: 'alpha' },
      { name: 'PreviewAgent', projectId: 'beta' },
    ]
    const withGhost: WFNode[] = [
      ...nodes,
      { id: 'e', type: 'agent', label: 'Pl', position: { x: 0, y: 0 }, config: { agent_profile: 'ghost' } },
    ]
    const issues = agentProfileIssues(withGhost, agents, 'alpha')
    expect(issues).toEqual([
      { name: 'PreviewAgent', reason: 'foreign' },
      { name: 'ghost', reason: 'missing' },
    ])
    // same-project only — no false positive
    expect(agentProfileIssues(
      [{ id: 'a', type: 'agent', label: 'I', position: { x: 0, y: 0 }, config: { agent_profile: 'ImplementAgent' } }],
      agents,
      'alpha',
    )).toEqual([])
  })

  it('setAgentProfile writes agent_profile', () => {
    const cfg: Record<string, unknown> = {}
    setAgentProfile(cfg, 'New')
    expect(cfg.agent_profile).toBe('New')
    expect(getAgentProfile(cfg)).toBe('New')
  })
})
