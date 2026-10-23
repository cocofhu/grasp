// @vitest-environment happy-dom
import { describe, expect, it } from 'vitest'
import { applyAcpBackend, applyWizardStartPath, assembleCreatePayload, freshDraft } from './agentCreateWizard'
import { fromDraft, toDraft } from './agentStudioDraft'
import { launchCommand } from './cliModelView'
import { applyTeamAcpBackend, applyTeamStartPath, freshTeamDraft } from './agentTeamWizard'
import { applyOnboardingBackend, applyStartPath, assembleBootstrapBody, freshOnboardingDraft } from '@/lib/pm/onboardingWizard'
import { authGuideFor } from './backendAuthGuide'
import { backendKind, isModelVendorCredential, kindOfItem } from '@/lib/workspace/credentialKinds'
import { APIKEY_BACKENDS, CLI_BACKENDS, startPathForBackend } from '@/lib/shared/startPath'

const harnessCredential = {
  id: 'dsh-team-key', name: 'DeepSeek team', provider: 'deepseek_harness',
  envKey: 'GRASP_DEEPSEEK_API_KEY', type: 'ai', configured: true,
}

describe('DeepSeek Harness setup', () => {
  it('is a coding tool with a dedicated credential kind and auth guide', () => {
    expect(APIKEY_BACKENDS.map((b) => b.id)).toEqual(['opencode'])
    expect(CLI_BACKENDS.some((b) => b.id === 'deepseek_harness')).toBe(true)
    expect(startPathForBackend('deepseek_harness')).toBe('cli')
    expect(backendKind('deepseek_harness')).toMatchObject({ envKey: 'GRASP_DEEPSEEK_API_KEY', secret: 'apiKey', logoProvider: 'deepseek' })
    expect(kindOfItem(harnessCredential)?.id).toBe('deepseek_harness')
    expect(isModelVendorCredential(harnessCredential)).toBe(false)
    expect(authGuideFor('deepseek_harness').keys).toEqual([{ key: 'GRASP_DEEPSEEK_API_KEY' }])
    expect(launchCommand('deepseek_harness', 'deepseek-flash')).toBe('dsh --profile acp')
  })

  it('remembers the native dsh tool when returning from the model API-key path', () => {
    const draft = freshDraft()
    draft.authMode = 'customConfig'
    applyAcpBackend(draft, 'deepseek_harness')
    expect(draft.authMode).toBe('apiKey')
    expect(draft.configRoot).toBe('/root/.dsh')
    expect(draft.startPath).toBe('cli')
    applyWizardStartPath(draft, 'apiKey')
    expect(draft.acpBackend).toBe('opencode')
    applyWizardStartPath(draft, 'cli')
    expect(draft.acpBackend).toBe('deepseek_harness')
    expect(draft.startPath).toBe('cli')
    expect(draft.authMode).toBe('apiKey')
    expect(draft.configRoot).toBe('/root/.dsh')
  })

  it('restores dsh as the coding tool in onboarding and team setup', () => {
    const onboarding = freshOnboardingDraft()
    applyOnboardingBackend(onboarding, 'deepseek_harness')
    applyStartPath(onboarding, 'apiKey')
    expect(onboarding.acpBackend).toBe('opencode')
    applyStartPath(onboarding, 'cli')
    expect(onboarding).toMatchObject({ startPath: 'cli', acpBackend: 'deepseek_harness' })

    const team = freshTeamDraft()
    applyTeamAcpBackend(team, 'deepseek_harness')
    applyTeamStartPath(team, 'apiKey')
    expect(team.acpBackend).toBe('opencode')
    applyTeamStartPath(team, 'cli')
    expect(team).toMatchObject({ startPath: 'cli', acpBackend: 'deepseek_harness', configRoot: '/root/.dsh', authMode: 'apiKey' })
  })

  it('keeps the model API-key path on OpenCode for drafts from the previous dsh classification', () => {
    const agent = freshDraft()
    const onboarding = freshOnboardingDraft()
    const team = freshTeamDraft()
    for (const draft of [agent, onboarding, team]) {
      draft.acpBackend = 'deepseek_harness'
      draft.apiKeyBackend = 'deepseek_harness'
      draft.cliBackend = 'cursor'
      draft.startPath = 'apiKey'
    }
    applyWizardStartPath(agent, 'apiKey')
    applyStartPath(onboarding, 'apiKey')
    applyTeamStartPath(team, 'apiKey')
    for (const draft of [agent, onboarding, team]) {
      expect(draft).toMatchObject({ startPath: 'apiKey', acpBackend: 'opencode', apiKeyBackend: 'opencode' })
    }
  })

  it('clears per-role model overrides when onboarding switches to a different backend', () => {
    const draft = freshOnboardingDraft()
    draft.cliModel = 'gpt-model'
    for (const member of draft.team) member.model = 'anthropic/claude-model'
    applyOnboardingBackend(draft, 'deepseek_harness')
    expect(draft.cliModel).toBe('')
    expect(draft.team.every((member) => member.model === '')).toBe(true)
    const payload = assembleBootstrapBody(draft)
    expect(payload.acpBackend).toBe('deepseek_harness')
    expect(payload.agents?.every((member) => !('model' in member))).toBe(true)
    expect(payload).not.toHaveProperty('cliModel')
  })

  it('retains onboarding models and credentials when reselecting the same backend', () => {
    const draft = freshOnboardingDraft()
    applyOnboardingBackend(draft, 'deepseek_harness')
    draft.apiKey = 'dsh-test-key'
    draft.cliModel = 'deepseek-flash'
    for (const member of draft.team) member.model = 'deepseek-v4-pro'
    applyOnboardingBackend(draft, 'deepseek_harness')
    expect(assembleBootstrapBody(draft)).toMatchObject({
      acpBackend: 'deepseek_harness', apiKey: 'dsh-test-key', cliModel: 'deepseek-flash',
      agents: draft.team.map((member) => expect.objectContaining({ templateId: member.templateId, model: 'deepseek-v4-pro' })),
    })
  })

  it('clears the team wizard model on backend changes and preserves it on reselection', () => {
    const draft = freshTeamDraft()
    draft.env.push({ k: 'ACP_BRIDGE_MODEL', v: 'openai/gpt-model' })
    applyTeamAcpBackend(draft, 'deepseek_harness')
    expect(draft.env.some((row) => row.k === 'ACP_BRIDGE_MODEL')).toBe(false)
    draft.env.push({ k: 'ACP_BRIDGE_MODEL', v: 'deepseek-flash' })
    applyTeamAcpBackend(draft, 'deepseek_harness')
    expect(draft.env).toContainEqual({ k: 'ACP_BRIDGE_MODEL', v: 'deepseek-flash' })
  })

  it('creates and round-trips a native model and selected credential without putting keys in env', () => {
    const draft = freshDraft()
    draft.name = 'deepseek-agent'
    applyAcpBackend(draft, 'deepseek_harness')
    draft.aiCredentialId = harnessCredential.id
    draft.openCodeCredentialId = 'stale-opencode-key'
    draft.env = [
      { k: 'ACP_BRIDGE_MODEL', v: 'deepseek-v4-pro' },
      { k: 'GRASP_DEEPSEEK_API_KEY', v: 'must-not-save' },
      { k: 'DEEPSEEK_API_KEY', v: 'must-not-save' },
      { k: 'ACP_DEEPSEEK_API_KEY', v: 'must-not-save' },
      { k: 'ACP_DEEPSEEK_HARNESS_API_KEY', v: 'must-not-save' },
    ]
    applyAcpBackend(draft, 'deepseek_harness')
    const payload = assembleCreatePayload(draft)
    expect(payload).toMatchObject({ acpBackend: 'deepseek_harness', aiCredentialId: 'dsh-team-key', env: { ACP_BRIDGE_MODEL: 'deepseek-v4-pro' }, layout: { configRoot: '/root/.dsh' } })
    expect(payload).not.toHaveProperty('openCodeCredentialId')
    expect(payload.env).not.toHaveProperty('GRASP_DEEPSEEK_API_KEY')
    expect(payload.env).not.toHaveProperty('DEEPSEEK_API_KEY')
    expect(payload.env).not.toHaveProperty('ACP_DEEPSEEK_API_KEY')
    expect(payload.env).not.toHaveProperty('ACP_DEEPSEEK_HARNESS_API_KEY')
    expect(fromDraft(toDraft(payload))).toMatchObject({ acpBackend: 'deepseek_harness', aiCredentialId: 'dsh-team-key', env: { ACP_BRIDGE_MODEL: 'deepseek-v4-pro' } })
  })
})
