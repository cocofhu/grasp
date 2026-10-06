// @vitest-environment happy-dom
import { describe, expect, it, beforeEach, beforeAll, vi } from 'vitest'
import {
  DEFAULT_PROJECT_ID,
  ONBOARDING_AGENT_NAMES,
  ONBOARDING_CLI_BACKENDS,
  ONBOARDING_REQUIRED_TEMPLATE_IDS,
  ONBOARDING_STEPS,
  ONBOARDING_WORKFLOW_NAMES,
  applyDefaultTeamNames,
  applyOnboardingBackend,
  applyStartPath,
  assembleBootstrapBody,
  buildOnboardingWorkflowPreview,
  deriveOnboardingAgentNames,
  onboardingRoleNames,
  startPathForBackend,
  detectSystemLocale,
  suppressOnboarding,
  freshOnboardingDraft,
  freshOnboardingTeam,
  gitConfigured,
  isEmptyProjectForOnboarding,
  isOnboardingSuppressed,
  isRequiredTemplate,
  onboardingSuppressKey,
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
    expect(ONBOARDING_STEPS.map((s) => s.id)).toEqual(['prefs', 'model', 'key', 'git', 'team', 'workflow', 'done'])
    vi.stubGlobal('navigator', { language: 'zh-CN' })
    expect(detectSystemLocale()).toBe('zh-CN')
    expect(freshOnboardingDraft().language).toBe('zh-CN')
    vi.stubGlobal('navigator', { language: 'en-US' })
    expect(detectSystemLocale()).toBe('en')
    expect(freshOnboardingDraft().language).toBe('en')
    vi.unstubAllGlobals()
  })

  it('starts on the API-key path with OpenCode selected', () => {
    const d = freshOnboardingDraft()
    expect(d.startPath).toBe('apiKey')
    expect(d.acpBackend).toBe('opencode')
    expect(d.cliBackend).toBe('cursor')
    expect(d.openCodeProvider).toBe('openai')
    expect(ONBOARDING_CLI_BACKENDS.map((b) => b.id)).toEqual([
      'cursor',
      'claude_code',
      'codebuddy',
      'trae',
    ])
    expect(startPathForBackend('opencode')).toBe('apiKey')
    expect(startPathForBackend('trae')).toBe('cli')
  })

  it('switching to the API-key path selects OpenCode and clears the CLI key', () => {
    const d = freshOnboardingDraft()
    applyStartPath(d, 'cli')
    d.apiKey = 'crsr_cursor_key'
    applyStartPath(d, 'apiKey')
    expect(d.acpBackend).toBe('opencode')
    expect(d.apiKey).toBe('')
    expect(d.openCodeProvider).toBe('openai')
    expect(d.region).toBe('')
  })

  it('returning to the CLI path keeps the previously chosen CLI backend', () => {
    const d = freshOnboardingDraft()
    applyOnboardingBackend(d, 'trae')
    expect(d.startPath).toBe('cli')
    expect(d.region).toBe('intl')
    applyStartPath(d, 'apiKey')
    expect(d.acpBackend).toBe('opencode')
    applyStartPath(d, 'cli')
    expect(d.acpBackend).toBe('trae')
    expect(d.region).toBe('intl')
  })

  it('defaults theme from the current app theme', () => {
    expect(freshOnboardingDraft().theme).toBe('dark')
  })

  it('assembles bootstrap body without heroku repos or featureHint', () => {
    const d = freshOnboardingDraft()
    d.acpBackend = 'codebuddy'
    d.region = 'public'
    d.apiKey = 'cb-key'
    d.gitCredentialType = 'github_https'
    d.githubToken = 'ghp_x'
    const body = assembleBootstrapBody(d)
    expect(body.apiKey).toBe('cb-key')
    expect(body.region).toBe('public')
    expect(body.gitCredentialType).toBe('github_https')
    expect(body.githubToken).toBe('ghp_x')
    expect(body).not.toHaveProperty('repos')
    expect(body).not.toHaveProperty('featureHint')
    expect(body.vncPreview).toBe(true)
    expect(body.browserMcp).toBe(true)
  })

  it('includes OpenCode vendor fields on bootstrap', () => {
    const d = freshOnboardingDraft()
    d.acpBackend = 'opencode'
    d.apiKey = 'sk-oc'
    d.openCodeProvider = 'custom'
    d.openCodeBaseURL = 'https://llm.example/v1'
    d.openCodeModel = 'custom/my-model'
    d.openCodeModelVision = true
    const body = assembleBootstrapBody(d)
    expect(body.openCodeProvider).toBe('custom')
    expect(body.openCodeBaseURL).toBe('https://llm.example/v1')
    expect(body.openCodeModel).toBe('custom/my-model')
    expect(body.openCodeModelVision).toBe(true)
  })

  it('sends the wizard language so the server names the workflow in it', () => {
    const d = freshOnboardingDraft()
    d.language = 'en'
    expect(assembleBootstrapBody(d).language).toBe('en')
    d.language = 'zh-CN'
    expect(assembleBootstrapBody(d).language).toBe('zh-CN')
  })

  it('defaults a custom OpenCode model to text-only until vision is opted in', () => {
    const d = freshOnboardingDraft()
    expect(d.openCodeModelVision).toBe(false)
    d.acpBackend = 'opencode'
    expect(assembleBootstrapBody(d).openCodeModelVision).toBe(false)
  })

  it('sends git identity and can turn preview flags off', () => {
    const d = freshOnboardingDraft()
    d.apiKey = 'k'
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
    d.apiKey = 'k'
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
    d.apiKey = 'k'
    d.repoUrl = 'https://github.com/org/web.git'
    d.gitCredentialType = 'github_https'
    d.githubToken = 'ghp_x'
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
  })

  it('sends the chosen team with names and per-agent models', () => {
    const d = freshOnboardingDraft()
    d.apiKey = 'k'
    applyDefaultTeamNames(d.team, deriveOnboardingAgentNames('p1', '支付'))
    expect(d.team.map((m) => m.name)).toEqual(['支付需求澄清', '支付实现', '支付测试评审', '支付交付'])
    d.team[1]!.model = ' gpt-5 '
    setTeamMemberEnabled(d.team, 'test_review', false)
    expect(assembleBootstrapBody(d).agents).toEqual([
      { templateId: 'clarify', name: '支付需求澄清' },
      { templateId: 'implement', name: '支付实现', model: 'gpt-5' },
      { templateId: 'deliver', name: '支付交付' },
    ])
  })

  it('keeps required templates checked and leaves edited names alone on refresh', () => {
    const team = freshOnboardingTeam()
    setTeamMemberEnabled(team, 'clarify', false)
    setTeamMemberEnabled(team, 'implement', false)
    expect(team.every((m) => m.enabled)).toBe(true)
    expect(isRequiredTemplate('test_review')).toBe(false)
    expect(isRequiredTemplate('deliver')).toBe(false)
    expect(ONBOARDING_REQUIRED_TEMPLATE_IDS).toEqual(['clarify', 'implement'])

    applyDefaultTeamNames(team, ['A需求澄清', 'A实现', 'A测试评审', 'A交付'])
    team[0]!.name = '澄清官'
    team[0]!.nameEdited = true
    applyDefaultTeamNames(team, ['B需求澄清', 'B实现', 'B测试评审', 'B交付'])
    expect(team.map((m) => m.name)).toEqual(['澄清官', 'B实现', 'B测试评审', 'B交付'])
  })

  it('validates team names: required, invalid, duplicate; unchecked members are ignored', () => {
    const team = freshOnboardingTeam()
    expect(teamNameIssues(team).clarify).toBe('required')
    expect(teamValid(team, { allowBlank: true })).toBe(true)
    applyDefaultTeamNames(team, [...ONBOARDING_AGENT_NAMES])
    expect(teamValid(team)).toBe(true)
    team[0]!.name = 'a b'
    team[2]!.name = '实现'
    const issues = teamNameIssues(team)
    expect(issues.clarify).toBe('invalid')
    expect(issues.implement).toBe('duplicate')
    expect(issues.test_review).toBe('duplicate')
    setTeamMemberEnabled(team, 'test_review', false)
    expect(teamNameIssues(team).implement).toBe('')
  })

  it('previews the default workflow with the fail loop, and without it when test_review is off', () => {
    const team = freshOnboardingTeam()
    applyDefaultTeamNames(team, [...ONBOARDING_AGENT_NAMES])
    team[1]!.name = '编码'
    const full = buildOnboardingWorkflowPreview(team)
    expect(full.nodes.map((n) => n.id)).toEqual(['input', 'clarify', 'implement', 'test_review', 'deliver', 'output'])
    expect(full.nodes.find((n) => n.id === 'implement')?.name).toBe('编码')
    expect(full.edges).toEqual([
      { from: 'input', to: 'clarify' },
      { from: 'clarify', to: 'implement' },
      { from: 'implement', to: 'test_review' },
      { from: 'test_review', to: 'deliver', handle: 'pass' },
      { from: 'deliver', to: 'output' },
      { from: 'test_review', to: 'implement', handle: 'fail' },
    ])

    setTeamMemberEnabled(team, 'test_review', false)
    const noReview = buildOnboardingWorkflowPreview(team)
    expect(noReview.nodes.map((n) => n.id)).toEqual(['input', 'clarify', 'implement', 'deliver', 'output'])
    expect(noReview.edges).toContainEqual({ from: 'implement', to: 'deliver' })
    expect(noReview.edges.some((e) => e.handle)).toBe(false)

    setTeamMemberEnabled(team, 'deliver', false)
    const trimmed = buildOnboardingWorkflowPreview(team)
    expect(trimmed.nodes.map((n) => n.id)).toEqual(['input', 'clarify', 'implement', 'output'])
    expect(trimmed.edges.at(-1)).toEqual({ from: 'implement', to: 'output' })
    expect(trimmed.edges.some((e) => e.handle)).toBe(false)
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

  it('gitConfigured requires type and matching secret', () => {
    const d = freshOnboardingDraft()
    expect(gitConfigured(d)).toBe(false)
    d.gitCredentialType = 'github_https'
    expect(gitConfigured(d)).toBe(false)
    d.githubToken = 'tok'
    expect(gitConfigured(d)).toBe(true)
  })

  it('treats empty projects as eligible for onboarding CTA, including non-default', () => {
    expect(isEmptyProjectForOnboarding(0, [], DEFAULT_PROJECT_ID)).toBe(true)
    expect(isEmptyProjectForOnboarding(0, [], 'p1', '支付中台')).toBe(true)
    expect(isEmptyProjectForOnboarding(0, [], 'p1', '')).toBe(false)
    expect(isEmptyProjectForOnboarding(1, [], DEFAULT_PROJECT_ID)).toBe(false)
    expect(
      isEmptyProjectForOnboarding(0, [{ name: '实现', projectId: DEFAULT_PROJECT_ID }], DEFAULT_PROJECT_ID),
    ).toBe(false)
  })

  it('treats cross-project first-install agent names as non-empty', () => {
    expect(isEmptyProjectForOnboarding(0, [{ name: '需求澄清', projectId: 'other' }], DEFAULT_PROJECT_ID)).toBe(false)
  })

  it('derives agent names from the template labels with the project prefix', () => {
    expect(ONBOARDING_AGENT_NAMES).toEqual(['需求澄清', '实现', '测试评审', '交付'])
    expect(sanitizeOnboardingPrefix('支付中台')).toBe('支付中台')
    expect(deriveOnboardingAgentNames(DEFAULT_PROJECT_ID, 'ignored')).toEqual([...ONBOARDING_AGENT_NAMES])
    expect(deriveOnboardingAgentNames('p1', '支付中台')).toEqual(['支付中台需求澄清', '支付中台实现', '支付中台测试评审', '支付中台交付'])
    expect(deriveOnboardingAgentNames('p1', '...')).toEqual([])
    expect(Array.from(sanitizeOnboardingPrefix('长'.repeat(80))).length).toBe(54)
  })

  it('localizes default role names and keeps every one a valid Agent name', () => {
    expect(onboardingRoleNames('zh-CN')).toEqual([...ONBOARDING_AGENT_NAMES])
    expect(onboardingRoleNames('en')).toEqual(['Clarify', 'Implement', 'TestReview', 'Deliver'])
    expect(deriveOnboardingAgentNames(DEFAULT_PROJECT_ID, '', 'en')).toEqual(['Clarify', 'Implement', 'TestReview', 'Deliver'])
    expect(deriveOnboardingAgentNames('p1', 'Payments', 'en')).toEqual(['PaymentsClarify', 'PaymentsImplement', 'PaymentsTestReview', 'PaymentsDeliver'])
    const long = deriveOnboardingAgentNames('p1', 'x'.repeat(80), 'en')
    for (const name of [...long, ...onboardingRoleNames('en'), ...onboardingRoleNames('zh-CN')]) {
      expect(validateAgentName(name)).toBe('')
    }
  })

  it('treats English default names owned by another project as a conflict', () => {
    expect(isEmptyProjectForOnboarding(0, [{ name: 'Clarify', projectId: 'other' }], DEFAULT_PROJECT_ID)).toBe(false)
  })

  it('createProject draft inherits app locale, not browser language (g1.2)', () => {
    locale.value = 'zh-CN'
    i18n.global.locale.value = 'zh-CN'
    vi.stubGlobal('navigator', { language: 'en-US' })
    expect(detectSystemLocale()).toBe('en')
    expect(freshOnboardingDraft().language).toBe('en')
    expect(freshOnboardingDraft({ inheritAppLocale: true }).language).toBe('zh-CN')
    vi.unstubAllGlobals()
  })

  it('auto-open keys on the default workflow, not on having been seen', () => {
    expect(shouldAutoOpenOnboarding(DEFAULT_PROJECT_ID, [], [])).toBe(true)
    expect(shouldAutoOpenOnboarding('p1', [], [])).toBe(false)
    expect(shouldAutoOpenOnboarding(DEFAULT_PROJECT_ID, [{ name: ONBOARDING_WORKFLOW_NAMES[0] }], [])).toBe(
      false,
    )
    // Unrelated workflows do not count as a finished first install.
    expect(shouldAutoOpenOnboarding(DEFAULT_PROJECT_ID, [{ name: '我的流程' }], [])).toBe(true)
    // Neither do already-bound agents: bootstrap re-upserts them.
    expect(
      shouldAutoOpenOnboarding(DEFAULT_PROJECT_ID, [], [{ name: '实现', projectId: DEFAULT_PROJECT_ID }]),
    ).toBe(true)
    // A fixed-name agent owned elsewhere would fail bootstrap, so stay closed.
    expect(shouldAutoOpenOnboarding(DEFAULT_PROJECT_ID, [], [{ name: '需求澄清', projectId: 'other' }])).toBe(false)
    expect(shouldAutoOpenOnboarding(DEFAULT_PROJECT_ID, [], [{ name: 'Clarify', projectId: 'other' }])).toBe(false)
  })

  it('an English first install also counts as finished', () => {
    expect(shouldAutoOpenOnboarding(DEFAULT_PROJECT_ID, [{ name: 'Default Workflow' }], [])).toBe(false)
  })

  it('the storage escape hatch suppresses auto-open for tests and debugging', () => {
    suppressOnboarding(DEFAULT_PROJECT_ID)
    expect(isOnboardingSuppressed(DEFAULT_PROJECT_ID)).toBe(true)
    expect(localStorage.getItem(onboardingSuppressKey(DEFAULT_PROJECT_ID))).toBe('1')
    expect(shouldAutoOpenOnboarding(DEFAULT_PROJECT_ID, [], [])).toBe(false)
  })
})
