// @vitest-environment happy-dom
import { describe, expect, it, beforeEach, beforeAll, vi } from 'vitest'
import {
  ONBOARDING_SUPPRESS_KEY,
  ONBOARDING_AGENT_NAMES,
  ONBOARDING_REQUIRED_TEMPLATE_IDS,
  ONBOARDING_STEPS,
  ONBOARDING_WORKFLOW_NAMES,
  applyDefaultTeamNames,
  assembleBootstrapBody,
  buildOnboardingWorkflowPreview,
  deriveOnboardingAgentNames,
  onboardingRoleNames,
  detectSystemLocale,
  suppressOnboarding,
  freshOnboardingDraft,
  freshOnboardingTeam,
  gitConfigured,
  hasDefaultWorkflow,
  isOnboardingSuppressed,
  isProtectedOnboardingAgentName,
  isRequiredTemplate,
  missingOnboardingNames,
  needsOnboarding,
  onboardingAgentsReady,
  setTeamMemberEnabled,
  teamNameIssues,
  teamValid,
  gitIdentityConfigured,
  repoConfigured,
  repoNameFromUrl,
  sanitizeOnboardingPrefix,
  shouldAutoOpenOnboarding,
} from './onboardingWizard'
import { i18n } from '@/lib/shared/i18n'
import { validateAgentName } from '@/lib/agent/agentIO'
import { loadLocaleMessages } from '@/lib/shared/loadLocaleMessages'
import { locale } from '@/lib/shared/locale'

beforeAll(async () => {
  const [zh, en] = await Promise.all([loadLocaleMessages('zh-CN'), loadLocaleMessages('en')])
  i18n.global.setLocaleMessage('zh-CN', zh)
  i18n.global.setLocaleMessage('en', en)
})

describe('onboardingWizard', () => {
  beforeEach(() => {
    localStorage.clear()
    i18n.global.locale.value = 'zh-CN'
    locale.value = 'zh-CN'
  })

  it('has one topic per step and defaults language from the system', () => {
    expect(ONBOARDING_STEPS.map((s) => s.id)).toEqual(['prefs', 'model', 'git', 'team', 'workflow', 'done'])
    vi.stubGlobal('navigator', { language: 'zh-CN' })
    expect(detectSystemLocale()).toBe('zh-CN')
    expect(freshOnboardingDraft().language).toBe('zh-CN')
    vi.stubGlobal('navigator', { language: 'en-US' })
    expect(detectSystemLocale()).toBe('en')
    expect(freshOnboardingDraft().language).toBe('en')
    vi.unstubAllGlobals()
  })

  it('defaults theme from the current app theme', () => {
    expect(freshOnboardingDraft().theme).toBe('dark')
  })

  it('submits model configuration and a detached Git selection without secrets', () => {
    const d = freshOnboardingDraft()
    d.modelConfigurationId = 'model-main'
    d.gitBindings = { items: [{ credentialId: 'git-team', scope: 'github.com' }], preferredCredentialId: 'git-team' }
    const body = assembleBootstrapBody(d)
    expect(body.modelConfigurationId).toBe('model-main')
    expect(body.gitBindings).toEqual(d.gitBindings)
    d.gitBindings.items[0]!.scope = 'other.example'
    expect(body.gitBindings.items[0]!.scope).toBe('github.com')
    for (const field of [
      'apiKey',
      'acpBackend',
      'cliModel',
      'openCodeProvider',
      'githubToken',
      'gitCredentialType',
      'herokuRepos',
      'featureHint',
    ])
      expect(body).not.toHaveProperty(field)
  })

  it('sends the wizard language so the server names the workflow in it', () => {
    const d = freshOnboardingDraft()
    d.language = 'en'
    expect(assembleBootstrapBody(d).language).toBe('en')
    d.language = 'zh-CN'
    expect(assembleBootstrapBody(d).language).toBe('zh-CN')
  })

  it('sends git identity and can turn preview flags off', () => {
    const d = freshOnboardingDraft()
    expect(gitIdentityConfigured(d)).toBe(false)
    d.gitUserName = ' Ada Lovelace '
    d.gitUserEmail = ' ada@example.com '
    d.vncPreview = false
    d.browserMcp = false
    expect(gitIdentityConfigured(d)).toBe(true)
    const body = assembleBootstrapBody(d)
    expect(body.gitUserName).toBe('Ada Lovelace')
    expect(body.gitUserEmail).toBe('ada@example.com')
    expect(body.vncPreview).toBe(false)
    expect(body.browserMcp).toBe(false)
  })

  it('sends the repo only when a URL is given, branch only alongside it', () => {
    const d = freshOnboardingDraft()
    expect(assembleBootstrapBody(d)).not.toHaveProperty('repoUrl')

    d.repoBranch = 'develop'
    expect(assembleBootstrapBody(d)).not.toHaveProperty('repoBranch')

    d.repoUrl = '  https://github.com/org/web.git  '
    const body = assembleBootstrapBody(d)
    expect(body.repoUrl).toBe('https://github.com/org/web.git')
    expect(body.repoBranch).toBe('develop')
  })

  it('skipping Git drops repo and credentials but keeps identity', () => {
    const d = freshOnboardingDraft()
    d.repoUrl = 'https://github.com/org/web.git'
    d.gitBindings = { items: [{ credentialId: 'git-team' }], preferredCredentialId: 'git-team' }
    d.gitUserName = 'Ada'
    d.gitUserEmail = 'ada@example.com'
    expect(gitConfigured(d)).toBe(true)
    d.gitSkipped = true
    expect(gitConfigured(d)).toBe(false)
    expect(repoConfigured(d)).toBe(false)
    const body = assembleBootstrapBody(d)
    expect(body).not.toHaveProperty('repoUrl')
    expect(body).not.toHaveProperty('gitCredentialType')
    expect(body).not.toHaveProperty('githubToken')
    expect(body.gitUserName).toBe('Ada')
    expect(body.gitBindings).toEqual({ items: [] })
  })

  it('sends every canonical name and a per-agent model', () => {
    const d = freshOnboardingDraft()
    d.language = 'zh-CN'
    applyDefaultTeamNames(d.team, deriveOnboardingAgentNames('zh-CN'))
    expect(d.team.map((m) => m.name)).toEqual(['需求澄清', '实现', '测试评审', '交付'])
    d.team[1]!.modelConfigurationId = 'model-review'
    d.team[1]!.name = '编码'
    setTeamMemberEnabled(d.team, 'test_review', false)
    expect(assembleBootstrapBody(d).agents).toEqual([
      { templateId: 'clarify', name: '需求澄清' },
      { templateId: 'implement', name: '实现', modelConfigurationId: 'model-review' },
      { templateId: 'test_review', name: '测试评审' },
      { templateId: 'deliver', name: '交付' },
    ])
  })

  it('keeps every template required and resets names to the canonical defaults', () => {
    const team = freshOnboardingTeam()
    setTeamMemberEnabled(team, 'test_review', false)
    setTeamMemberEnabled(team, 'deliver', false)
    expect(team.every((m) => m.enabled)).toBe(true)
    expect(isRequiredTemplate('test_review')).toBe(true)
    expect(isRequiredTemplate('deliver')).toBe(true)
    expect(ONBOARDING_REQUIRED_TEMPLATE_IDS).toEqual(['clarify', 'implement', 'test_review', 'deliver'])

    applyDefaultTeamNames(team, ['需求澄清', '实现', '测试评审', '交付'])
    team[0]!.name = '澄清官'
    team[0]!.nameEdited = true
    applyDefaultTeamNames(team, ['Clarify', 'Implement', 'TestReview', 'Deliver'])
    expect(team.map((m) => m.name)).toEqual(['Clarify', 'Implement', 'TestReview', 'Deliver'])
  })

  it('validates team names: required, invalid, duplicate; unchecked members are ignored', () => {
    const team = freshOnboardingTeam()
    expect(teamNameIssues(team).clarify).toBe('required')
    expect(teamValid(team)).toBe(false)
    applyDefaultTeamNames(team, [...ONBOARDING_AGENT_NAMES])
    expect(teamValid(team)).toBe(true)
    team[0]!.name = 'a b'
    team[2]!.name = '实现'
    const issues = teamNameIssues(team)
    expect(issues.clarify).toBe('invalid')
    expect(issues.implement).toBe('duplicate')
    expect(issues.test_review).toBe('duplicate')
    team[2]!.enabled = false
    expect(teamNameIssues(team).implement).toBe('')
  })

  it('treats either complete quartet as ready and lists the current language gaps', () => {
    expect(isProtectedOnboardingAgentName('需求澄清')).toBe(true)
    expect(isProtectedOnboardingAgentName(' Clarify ')).toBe(true)
    expect(isProtectedOnboardingAgentName('自建')).toBe(false)
    expect(onboardingAgentsReady(['需求澄清', '实现'])).toBe(false)
    expect(onboardingAgentsReady(['需求澄清', '实现', '测试评审', '交付', 'extra'])).toBe(true)
    expect(onboardingAgentsReady(['Clarify', 'Implement', 'TestReview', 'Deliver'])).toBe(true)
    expect(missingOnboardingNames(['需求澄清', '实现'], 'zh-CN')).toEqual(['测试评审', '交付'])
    expect(missingOnboardingNames(['需求澄清'], 'en')).toEqual(['Clarify', 'Implement', 'TestReview', 'Deliver'])
  })

  it('previews the default workflow with every role and the fail loop', () => {
    const team = freshOnboardingTeam()
    applyDefaultTeamNames(team, [...ONBOARDING_AGENT_NAMES])
    setTeamMemberEnabled(team, 'test_review', false)
    const full = buildOnboardingWorkflowPreview(team)
    expect(full.nodes.map((n) => n.id)).toEqual(['input', 'clarify', 'implement', 'test_review', 'deliver', 'output'])
    expect(full.nodes.find((n) => n.id === 'implement')?.name).toBe('实现')
    expect(full.edges).toEqual([
      { from: 'input', to: 'clarify' },
      { from: 'clarify', to: 'implement' },
      { from: 'implement', to: 'test_review' },
      { from: 'test_review', to: 'deliver', handle: 'pass' },
      { from: 'deliver', to: 'output' },
      { from: 'test_review', to: 'implement', handle: 'fail' },
    ])
  })

  it('derives the clone dir the same way the server does', () => {
    expect(repoNameFromUrl('https://github.com/org/web.git')).toBe('web')
    expect(repoNameFromUrl('https://git.host.cc/org/web')).toBe('web')
    expect(repoNameFromUrl('git@github.com:org/api.git')).toBe('api')
    expect(repoNameFromUrl('ssh://git@host/org/infra.git/')).toBe('infra')
    expect(repoNameFromUrl('   ')).toBe('')
  })

  it('repoConfigured tracks a non-blank URL', () => {
    const d = freshOnboardingDraft()
    expect(repoConfigured(d)).toBe(false)
    d.repoUrl = '   '
    expect(repoConfigured(d)).toBe(false)
    d.repoUrl = 'https://github.com/org/web.git'
    expect(repoConfigured(d)).toBe(true)
  })

  it('Git availability uses selected credential references and respects skip', () => {
    const d = freshOnboardingDraft()
    expect(gitConfigured(d)).toBe(false)
    d.gitBindings = { items: [{ credentialId: 'git-team' }], preferredCredentialId: 'git-team' }
    expect(gitConfigured(d)).toBe(true)
    d.gitSkipped = true
    expect(gitConfigured(d)).toBe(false)
    expect(assembleBootstrapBody(d).gitBindings).toEqual({ items: [] })
  })

  it('treats only a workspace without workflows or agents as needing onboarding', () => {
    expect(needsOnboarding([], [])).toBe(true)
    expect(needsOnboarding([{ name: '我的流程' }], [])).toBe(false)
    expect(needsOnboarding([], [{ name: '实现' }])).toBe(false)
  })

  it('derives role names independently of the workspace name', () => {
    expect(ONBOARDING_AGENT_NAMES).toEqual(['需求澄清', '实现', '测试评审', '交付'])
    expect(sanitizeOnboardingPrefix('支付中台')).toBe('支付中台')
    expect(deriveOnboardingAgentNames()).toEqual([...ONBOARDING_AGENT_NAMES])
    expect(deriveOnboardingAgentNames('zh-CN')).toEqual([...ONBOARDING_AGENT_NAMES])
    expect(Array.from(sanitizeOnboardingPrefix('长'.repeat(80))).length).toBe(54)
  })

  it('localizes default role names and keeps every one a valid Agent name', () => {
    expect(onboardingRoleNames('zh-CN')).toEqual([...ONBOARDING_AGENT_NAMES])
    expect(onboardingRoleNames('en')).toEqual(['Clarify', 'Implement', 'TestReview', 'Deliver'])
    expect(deriveOnboardingAgentNames('en')).toEqual(['Clarify', 'Implement', 'TestReview', 'Deliver'])
    for (const name of [
      ...deriveOnboardingAgentNames('en'),
      ...onboardingRoleNames('en'),
      ...onboardingRoleNames('zh-CN'),
    ]) {
      expect(validateAgentName(name)).toBe('')
    }
  })

  it('new draft can inherit app locale, not browser language (g1.2)', () => {
    locale.value = 'zh-CN'
    i18n.global.locale.value = 'zh-CN'
    vi.stubGlobal('navigator', { language: 'en-US' })
    expect(detectSystemLocale()).toBe('en')
    expect(freshOnboardingDraft().language).toBe('en')
    expect(freshOnboardingDraft({ inheritAppLocale: true }).language).toBe('zh-CN')
    vi.unstubAllGlobals()
  })

  it('auto-opens only in an empty workspace', () => {
    expect(shouldAutoOpenOnboarding([], [])).toBe(true)
    expect(shouldAutoOpenOnboarding([{ name: ONBOARDING_WORKFLOW_NAMES[0] }], [])).toBe(false)
    // Any existing workflow keeps automatic prompting closed.
    expect(shouldAutoOpenOnboarding([{ name: '我的流程' }], [])).toBe(false)
    // Existing agents (including default role names) keep it closed too.
    expect(shouldAutoOpenOnboarding([], [{ name: '实现' }])).toBe(false)
    expect(shouldAutoOpenOnboarding([], [{ name: 'Clarify' }])).toBe(false)
  })

  it('an English first install also counts as finished', () => {
    expect(hasDefaultWorkflow([{ name: ' Default Workflow ' }])).toBe(true)
    expect(hasDefaultWorkflow([{ name: '默认工作流' }])).toBe(true)
    expect(hasDefaultWorkflow([{ name: '我的流程' }])).toBe(false)
    expect(shouldAutoOpenOnboarding([{ name: 'Default Workflow' }], [])).toBe(false)
  })

  it('the storage escape hatch suppresses auto-open for tests and debugging', () => {
    expect(isOnboardingSuppressed()).toBe(false)
    suppressOnboarding()
    expect(isOnboardingSuppressed()).toBe(true)
    expect(ONBOARDING_SUPPRESS_KEY).toBe('grasp-onboarding-suppress')
    expect(localStorage.getItem(ONBOARDING_SUPPRESS_KEY)).toBe('1')
    expect(shouldAutoOpenOnboarding([], [])).toBe(false)
  })
})
