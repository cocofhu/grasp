import { describe, expect, it } from 'vitest'
import {
  TEAM_ENGINEER_COUNT,
  TEAM_WIZARD_STEPS,
  assembleTeamBootstrapPayload,
  artifactStorePreset,
  freshTeamDraft,
  syncDerivedNames,
  validateTeamBasics,
} from '@/lib/agent/agentTeamWizard'

describe('agentTeamWizard', () => {
  it('requires model selection before optional Git and workspace settings', () => {
    expect(TEAM_WIZARD_STEPS).toHaveLength(6)
    expect(TEAM_WIZARD_STEPS.map((s) => s.id)).toEqual(['team', 'model', 'git', 'mcp', 'env', 'review'])
  })

  it('prefills artifact-store and GIT_REPOS', () => {
    const d = freshTeamDraft()
    expect(d.mcp[0]).toMatchObject(artifactStorePreset())
    expect(d.env.some((e) => e.k === 'GIT_REPOS')).toBe(true)
    expect(TEAM_ENGINEER_COUNT).toBe(4)
  })

  it('starts without a model and submits only explicit references', () => {
    const d = freshTeamDraft()
    expect(d.modelConfigurationId).toBe('')
    expect(d.gitBindings).toEqual({ items: [] })
    d.modelConfigurationId = 'model-team'
    d.gitBindings = { items: [{ credentialId: 'git-team' }], preferredCredentialId: 'git-team' }
    const payload = assembleTeamBootstrapPayload(d)
    expect(payload.modelConfigurationId).toBe('model-team')
    expect(payload.gitBindings).toEqual(d.gitBindings)
    for (const field of ['apiKey', 'acpBackend', 'openCodeProvider', 'gitCredentialType'])
      expect(payload).not.toHaveProperty(field)
    d.gitBindings.items = []
    expect(payload.gitBindings.items).toEqual([{ credentialId: 'git-team' }])
  })

  it('syncs derived names from team name / prefix', () => {
    const d = freshTeamDraft()
    d.teamName = 'Demo'
    syncDerivedNames(d)
    expect(d.prefix).toBe('Demo')
    expect(d.pmName).toBe('Demo项目经理')
  })

  it('validates required team fields and pm collision', () => {
    const d = freshTeamDraft()
    expect(validateTeamBasics(d, [])).toBe('teamNameRequired')
    d.teamName = 'Demo'
    syncDerivedNames(d)
    d.background = 'bg'
    expect(validateTeamBasics(d, ['Demo项目经理'])).toBe('pmExists')
    expect(validateTeamBasics(d, [])).toBe('')
  })

  it('assembles bootstrap payload with mcp/env', () => {
    const d = freshTeamDraft()
    d.teamName = 'Demo'
    syncDerivedNames(d)
    d.background = 'build a workflow team'
    d.gitUrl = 'https://github.com/org/repo.git'
    const payload = assembleTeamBootstrapPayload(d)
    expect(payload).not.toHaveProperty('teamName')
    expect(payload.pmName).toBe('Demo项目经理')
    expect(payload.mcp.some((m) => m.name === 'artifact-store')).toBe(true)
    expect(payload.env.GIT_REPOS).toBe('${vars.repos}')
    expect(payload.gitUrl).toContain('github.com')
  })
})
