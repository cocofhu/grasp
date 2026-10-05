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
  it('has 7 wizard steps', () => {
    expect(TEAM_WIZARD_STEPS).toHaveLength(7)
    expect(TEAM_WIZARD_STEPS.map((s) => s.id)).toEqual([
      'team',
      'acp',
      'apiKey',
      'git',
      'mcp',
      'env',
      'review',
    ])
  })

  it('prefills artifact-store and GIT_REPOS', () => {
    const d = freshTeamDraft()
    expect(d.mcp[0]).toMatchObject(artifactStorePreset())
    expect(d.env.some((e) => e.k === 'GIT_REPOS')).toBe(true)
    expect(TEAM_ENGINEER_COUNT).toBe(9)
  })

  it('defaults to OpenCode API Key path (g1.3)', () => {
    const d = freshTeamDraft()
    expect(d.startPath).toBe('apiKey')
    expect(d.acpBackend).toBe('opencode')
    expect(d.cliBackend).toBe('cursor')
    expect(d.configRoot).toBe('/root/.config/opencode')
  })

  it('assembles bootstrap with opencode when Agent step skipped', () => {
    const d = freshTeamDraft()
    d.projectName = 'Demo'
    syncDerivedNames(d)
    d.background = 'build a workflow team'
    const payload = assembleTeamBootstrapPayload(d)
    expect(payload.acpBackend).toBe('opencode')
  })

  it('syncs derived names from project / prefix', () => {
    const d = freshTeamDraft()
    d.projectName = 'Demo'
    syncDerivedNames(d)
    expect(d.prefix).toBe('Demo')
    expect(d.rootGroupName).toBe('Demo项目组')
    expect(d.pmName).toBe('Demo项目经理')
  })

  it('validates required team fields and pm collision', () => {
    const d = freshTeamDraft()
    expect(validateTeamBasics(d, [])).toBe('projectRequired')
    d.projectName = 'Demo'
    syncDerivedNames(d)
    d.background = 'bg'
    expect(validateTeamBasics(d, ['Demo项目经理'])).toBe('pmExists')
    expect(validateTeamBasics(d, [])).toBe('')
  })

  it('assembles bootstrap payload with mcp/env', () => {
    const d = freshTeamDraft()
    d.projectName = 'Demo'
    syncDerivedNames(d)
    d.background = 'build a workflow team'
    d.gitUrl = 'https://github.com/org/repo.git'
    const payload = assembleTeamBootstrapPayload(d)
    expect(payload.projectName).toBe('Demo')
    expect(payload.pmName).toBe('Demo项目经理')
    expect(payload.mcp.some((m) => m.name === 'artifact-store')).toBe(true)
    expect(payload.env.GIT_REPOS).toBe('${vars.repos}')
    expect(payload.gitUrl).toContain('github.com')
  })
})
