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
  it('preserves independent runtime defaults while excluding plaintext and masked secret values', () => {
    const defaults = {
      inheritWorkspace: false,
      variables: [
        { name: 'secret', type: 'string', secret: true, value: 'private-token', required: true },
        { name: 'masked', type: 'string', secret: true, value: '****' },
        { name: 'branch', type: 'string', value: 'main' },
      ],
      notifyPolicy: { enabled: true, channelIds: ['channel-id'], defaultEvents: ['failed'] },
      gateAuto: { enabled: true, agentName: 'reviewer', variable: 'auto', prompt: 'Review this', enabledMcps: ['pm-progress'] },
    }
    const env = buildEnvelope({ name: 'Portable', description: '', needsRepo: false, defaults }, { nodes: [], edges: [], variables: [{ name: 'inputSecret', secret: true, value: 'private-input' }] })
    expect(env.defaults).toEqual({ ...defaults, variables: [
      { name: 'secret', type: 'string', secret: true, required: true },
      { name: 'masked', type: 'string', secret: true },
      { name: 'branch', type: 'string', value: 'main' },
    ] })
    expect(env.graph.variables).toEqual([{ name: 'inputSecret', secret: true }])
    const serialized = JSON.stringify(env)
    expect(serialized).not.toContain('private-token')
    expect(serialized).not.toContain('private-input')
    expect(serialized).not.toContain('****')
    env.defaults!.gateAuto!.enabledMcps.push('pm-prd-manager')
    expect(defaults.gateAuto.enabledMcps).toEqual(['pm-progress'])
    expect(defaults.variables[0].value).toBe('private-token')
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
    const issues = agentProfileIssues(nodes, [{ name: 'Other' }])
    expect(issues.filter((i) => i.reason === 'missing').map((i) => i.name).sort()).toEqual(
      ['ImplementAgent', 'PreviewAgent'].sort(),
    )
  })

  it('accepts every Agent in the workspace and only warns about missing profiles', () => {
    const agents = [
      { name: 'ImplementAgent' },
      { name: 'PreviewAgent' },
    ]
    const withGhost: WFNode[] = [
      ...nodes,
      { id: 'e', type: 'agent', label: 'Pl', position: { x: 0, y: 0 }, config: { agent_profile: 'ghost' } },
    ]
    const issues = agentProfileIssues(withGhost, agents)
    expect(issues).toEqual([
      { name: 'ghost', reason: 'missing' },
    ])
    // Historical owner IDs do not affect the workspace directory.
    expect(agentProfileIssues(
      [{ id: 'a', type: 'agent', label: 'I', position: { x: 0, y: 0 }, config: { agent_profile: 'ImplementAgent' } }],
      agents,
    )).toEqual([])
  })

  it('setAgentProfile writes agent_profile', () => {
    const cfg: Record<string, unknown> = {}
    setAgentProfile(cfg, 'New')
    expect(cfg.agent_profile).toBe('New')
    expect(getAgentProfile(cfg)).toBe('New')
  })
})
