// @vitest-environment happy-dom
import { describe, expect, it, beforeEach, beforeAll, vi } from 'vitest'
import {
  DEFAULT_PROJECT_ID,
  ONBOARDING_AGENT_NAMES,
  ONBOARDING_CLI_BACKENDS,
  ONBOARDING_STEPS,
  ONBOARDING_WORKFLOW_NAME,
  applyOnboardingBackend,
  applyStartPath,
  assembleBootstrapBody,
  deriveOnboardingAgentNames,
  startPathForBackend,
  detectSystemLocale,
  suppressOnboarding,
  freshOnboardingDraft,
  gitConfigured,
  isEmptyProjectForOnboarding,
  isOnboardingSuppressed,
  onboardingStepsForMode,
  onboardingSuppressKey,
  gitIdentityConfigured,
  repoConfigured,
  repoNameFromUrl,
  sanitizeOnboardingPrefix,
  shouldAutoOpenOnboarding,
} from './onboardingWizard'
import { i18n } from '@/lib/shared/i18n'
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

  it('starts with language and defaults it from the system locale', () => {
    expect(ONBOARDING_STEPS[0]?.id).toBe('language')
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
      'codex',
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
      isEmptyProjectForOnboarding(0, [{ name: '综合研发工程师', projectId: DEFAULT_PROJECT_ID }], DEFAULT_PROJECT_ID),
    ).toBe(false)
  })

  it('treats cross-project first-install agent names as non-empty', () => {
    expect(
      isEmptyProjectForOnboarding(0, [{ name: '综合AI技术产品', projectId: 'other' }], DEFAULT_PROJECT_ID),
    ).toBe(false)
    expect(isEmptyProjectForOnboarding(0, [{ name: '综合AI技术产品', projectId: '' }], DEFAULT_PROJECT_ID)).toBe(true)
  })

  it('derives agent names from project prefix for non-default projects', () => {
    expect(sanitizeOnboardingPrefix('支付中台')).toBe('支付中台')
    expect(deriveOnboardingAgentNames(DEFAULT_PROJECT_ID, 'ignored')).toEqual([...ONBOARDING_AGENT_NAMES])
    expect(deriveOnboardingAgentNames('p1', '支付中台')[0]).toBe('支付中台AI技术产品')
    expect(onboardingStepsForMode('createProject')[0]?.id).toBe('projectName')
    expect(onboardingStepsForMode('createProject').map((s) => s.id)).toEqual([
      'projectName',
      'overview',
      'acp',
      'apiKey',
      'git',
      'review',
    ])
    expect(onboardingStepsForMode('createProject').some((s) => s.id === 'language')).toBe(false)
    expect(onboardingStepsForMode('firstInstall')[0]?.id).toBe('language')
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
    expect(shouldAutoOpenOnboarding(DEFAULT_PROJECT_ID, [{ name: ONBOARDING_WORKFLOW_NAME }], [])).toBe(
      false,
    )
    // Unrelated workflows do not count as a finished first install.
    expect(shouldAutoOpenOnboarding(DEFAULT_PROJECT_ID, [{ name: '我的流程' }], [])).toBe(true)
    // Neither do already-bound agents: bootstrap re-upserts them.
    expect(
      shouldAutoOpenOnboarding(DEFAULT_PROJECT_ID, [], [
        { name: '综合研发工程师', projectId: DEFAULT_PROJECT_ID },
      ]),
    ).toBe(true)
    // A fixed-name agent owned elsewhere would fail bootstrap, so stay closed.
    expect(
      shouldAutoOpenOnboarding(DEFAULT_PROJECT_ID, [], [{ name: '综合AI技术产品', projectId: 'other' }]),
    ).toBe(false)
  })

  it('the storage escape hatch suppresses auto-open for tests and debugging', () => {
    suppressOnboarding(DEFAULT_PROJECT_ID)
    expect(isOnboardingSuppressed(DEFAULT_PROJECT_ID)).toBe(true)
    expect(localStorage.getItem(onboardingSuppressKey(DEFAULT_PROJECT_ID))).toBe('1')
    expect(shouldAutoOpenOnboarding(DEFAULT_PROJECT_ID, [], [])).toBe(false)
  })
})
