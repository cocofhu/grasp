import { describe, expect, it } from 'vitest'
import {
  TEAM_ENGINEER_COUNT,
  TEAM_WIZARD_STEPS,
  assembleTeamBootstrapPayload,
  freshTeamDraft,
  syncDerivedNames,
  validateTeamBasics,
} from '@/lib/agent/agentTeamWizard'

describe('agentTeamWizard', () => {
  it('requires model selection before the optional workspace settings', () => {
    expect(TEAM_WIZARD_STEPS).toHaveLength(5)
    expect(TEAM_WIZARD_STEPS.map((s) => s.id)).toEqual(['team', 'model', 'mcp', 'env', 'review'])
  })

  it('starts without MCP or env (the wizard seeds grasp-tool from server presets)', () => {
    const d = freshTeamDraft()
    expect(d.mcp).toEqual([])
    expect(d.env).toEqual([])
    expect(TEAM_ENGINEER_COUNT).toBe(4)
  })

  it('starts without a model and submits only explicit references', () => {
    const d = freshTeamDraft()
    expect(d.modelConfigurationId).toBe('')
    d.modelConfigurationId = 'model-team'
    const payload = assembleTeamBootstrapPayload(d)
    expect(payload.modelConfigurationId).toBe('model-team')
    for (const field of ['apiKey', 'acpBackend', 'openCodeProvider', 'gitCredentialType', 'gitBindings', 'gitUrl', 'repoId'])
      expect(payload).not.toHaveProperty(field)
  })

  it('derives the prefix from the team name until the prefix is edited', () => {
    const d = freshTeamDraft()
    d.teamName = 'Demo'
    syncDerivedNames(d)
    expect(d.prefix).toBe('Demo')
    d.prefixTouched = true
    d.prefix = 'Luna'
    d.teamName = 'Other'
    syncDerivedNames(d)
    expect(d.prefix).toBe('Luna')
  })

  it('validates required team fields', () => {
    const d = freshTeamDraft()
    expect(validateTeamBasics(d)).toBe('teamNameRequired')
    d.teamName = 'Demo'
    expect(validateTeamBasics(d)).toBe('prefixRequired')
    syncDerivedNames(d)
    expect(validateTeamBasics(d)).toBe('')
  })

  it('assembles bootstrap payload with mcp/env', () => {
    const d = freshTeamDraft()
    d.teamName = 'Demo'
    d.mcp.push({ name: 'grasp-tool', transport: 'url', url: '${GRASP_ARTIFACT_URL}', headers: [], command: '', args: '', env: [] })
    syncDerivedNames(d)
    const payload = assembleTeamBootstrapPayload(d)
    expect(payload).not.toHaveProperty('teamName')
    expect(payload.prefix).toBe('Demo')
    expect(payload.mcp.some((m) => m.name === 'grasp-tool')).toBe(true)
    expect(payload.env).toEqual({})
  })
})
