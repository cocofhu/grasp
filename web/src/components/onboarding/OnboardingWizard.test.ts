// @vitest-environment happy-dom
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'
import OnboardingWizard from './OnboardingWizard.vue'
import {
  suppressOnboarding,
  isOnboardingSuppressed,
  ONBOARDING_WORKFLOW_NAMES,
  shouldAutoOpenOnboarding,
} from '@/lib/pm/onboardingWizard'

const TEMPLATES = [
  {
    id: 'clarify',
    embedName: 'ClarifyAgent',
    roleLabelZh: '需求澄清',
    summary: 's1',
    capabilities: {
      interaction: 'clarify',
      tools: ['ask_question', 'set_artifact_preview', 'set_preview'],
      writes: [
        { schema: 'clarified_requirement', required: true },
        { schema: 'plan', required: true },
        { schema: 'research' },
      ],
    },
  },
  {
    id: 'implement',
    embedName: 'ImplementAgent',
    roleLabelZh: '实现',
    summary: 's2',
    capabilities: {
      interaction: 'auto',
      review: true,
      tools: ['set_preview', 'update_plan_status'],
      writes: [{ schema: 'implementation_result', required: true }],
    },
  },
  {
    id: 'test_review',
    embedName: 'TestReviewAgent',
    roleLabelZh: '测试评审',
    summary: 's3',
    capabilities: {
      interaction: 'auto',
      review: true,
      tools: ['update_plan_status'],
      writes: [
        { schema: 'test_result', required: true },
        { schema: 'review', required: true },
      ],
    },
  },
  {
    id: 'deliver',
    embedName: 'DeliverAgent',
    roleLabelZh: '交付',
    summary: 's4',
    capabilities: {
      interaction: 'auto',
      reads: ['*'],
      writes: [{ schema: 'merge_request', required: true }],
    },
  },
]

const launchMocks = vi.hoisted(() => ({ openLaunch: vi.fn(async () => {}) }))

vi.mock('@/lib/api/api', () => ({
  api: {
    getWorkflow: vi.fn(async (id: string) => ({ id, name: '默认工作流' })),
    listAgentTeamTemplates: vi.fn(async () => ({ items: TEMPLATES })),
    bootstrapOnboarding: vi.fn(async () => ({
      agentIds: ['需求澄清', '实现', '测试评审'],
      workflowId: 'wf-1',
      published: true,
    })),
    openCodeProviders: vi.fn(async () => ({
      providers: [{ id: 'deepseek', name: 'DeepSeek', models: 1 }],
    })),
    openCodeModels: vi.fn(async () => ({ models: [{ id: 'deepseek-v4-pro' }] })),
  },
}))

const CLI_MODELS = [
  { id: 'composer-2.5', name: 'Composer 2.5', vendor: 'cursor', recommended: true, context: 200 },
  { id: 'claude-sonnet-4-5', name: 'Claude Sonnet 4.5', vendor: 'anthropic', recommended: true },
]
const cliCatalogMocks = vi.hoisted(() => ({
  load: vi.fn(async () => CLI_MODELS),
  failed: vi.fn(() => false),
}))
vi.mock('@/lib/agent/cliCatalog', () => ({
  loadCliModels: (...args: unknown[]) => cliCatalogMocks.load(...args),
  cliCatalogFailed: (...args: unknown[]) => cliCatalogMocks.failed(...args),
}))

vi.mock('@/lib/run/useWorkflowRunLaunch', () => ({
  useWorkflowRunLaunch: () => ({ openLaunch: launchMocks.openLaunch }),
}))

vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({ success: vi.fn(), error: vi.fn(), warn: vi.fn(), show: vi.fn() }),
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (k: string) => k,
      te: () => false,
    }),
  }
})

import { createMemoryHistory, createRouter } from 'vue-router'

async function mountWizard(props: Record<string, unknown> = {}) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { path: '/workflows', component: { template: '<div />' } },
      { path: '/workflows/:id/edit', component: { template: '<div />' } },
    ],
  })
  await router.push('/')
  const wrapper = mount(OnboardingWizard, {
    props: { open: true, mode: 'firstInstall', ...props },
    global: {
      plugins: [router],
      stubs: { Teleport: true, Icon: true, AppButton: true },
    },
  })
  await flushPromises()
  return Object.assign(wrapper, { router })
}

type Wrapper = Awaited<ReturnType<typeof mountWizard>>

/** Picks the vendor and model the catalog stub serves, then fills the key. */
async function fillOpenCodeAuth(wrapper: Wrapper, key = 'sk-oc-demo') {
  await wrapper
    .get('[data-test="opencode-provider"] [data-test="app-select-trigger"]')
    .trigger('click')
  await wrapper.get('[data-test="app-select-option-deepseek"]').trigger('click')
  await flushPromises()
  await wrapper.get('[data-test="opencode-model"] [data-test="app-select-trigger"]').trigger('click')
  await wrapper.get('[data-test="app-select-option-deepseek/deepseek-v4-pro"]').trigger('click')
  await wrapper.find('[data-testid="onboarding-api-key"]').setValue(key)
}

async function next(wrapper: Wrapper) {
  await wrapper.find('[data-testid="onboarding-next"]').trigger('click')
  await flushPromises()
}

async function fillIdentity(wrapper: Wrapper) {
  await wrapper.find('[data-testid="onboarding-git-user-name"]').setValue('Ada Lovelace')
  await wrapper.find('[data-testid="onboarding-git-user-email"]').setValue('ada@example.com')
}

/** Preferences → model → key → Git → team; `onGit` runs on the Git page before leaving it. */
async function walkToTeam(wrapper: Wrapper, opts: { key?: string; onGit?: () => Promise<void> } = {}) {
  await next(wrapper)
  await next(wrapper)
  await fillOpenCodeAuth(wrapper, opts.key)
  await next(wrapper)
  await fillIdentity(wrapper)
  await opts.onGit?.()
  await next(wrapper)
  expect(activeStep(wrapper)).toBe('onboarding-rail-team')
}

function activeStep(wrapper: Wrapper) {
  return wrapper.find('[data-active="1"]').attributes('data-testid')
}

/** Preferences → coding-tool path → key step. Cursor unless another backend is named. */
async function goToCliKey(wrapper: Wrapper, backend = 'cursor') {
  await next(wrapper)
  await wrapper.find('[data-testid="onboarding-path-cli"]').trigger('click')
  if (backend !== 'cursor') {
    await wrapper.find(`[data-testid="onboarding-backend-${backend}"]`).trigger('click')
  }
  await next(wrapper)
  expect(activeStep(wrapper)).toBe('onboarding-rail-key')
}

async function pickCliModel(wrapper: Wrapper, id: string) {
  await wrapper.get('[data-testid="cli-model-trigger"]').trigger('click')
  await flushPromises()
  await wrapper.get(`[data-testid="cli-model-option-${id}"]`).trigger('click')
  await flushPromises()
}

async function lastBody() {
  const { api } = await import('@/lib/api/api')
  const calls = vi.mocked(api.bootstrapOnboarding).mock.calls as unknown[][]
  const last = calls[calls.length - 1]
  expect(last).toHaveLength(1)
  return last?.[0] as Record<string, any> | undefined
}

describe('OnboardingWizard', () => {
  beforeEach(async () => {
    localStorage.clear()
    vi.clearAllMocks()
    cliCatalogMocks.load.mockReset()
    cliCatalogMocks.load.mockResolvedValue(CLI_MODELS)
    cliCatalogMocks.failed.mockReset()
    cliCatalogMocks.failed.mockReturnValue(false)
    // Chinese UI by default; English cases switch explicitly.
    const { locale } = await import('@/lib/shared/locale')
    locale.value = 'zh-CN'
    vi.stubGlobal('navigator', { ...navigator, language: 'zh-CN' })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('bootstraps native DeepSeek Harness from the coding-tool path and remembers the tool choice', async () => {
    cliCatalogMocks.load.mockResolvedValue([{ id: 'deepseek-flash', name: 'DeepSeek Flash', vendor: 'deepseek' }])
    const wrapper = await mountWizard()
    await next(wrapper)
    expect(wrapper.find('[data-testid="onboarding-backend-deepseek_harness"]').exists()).toBe(false)
    await wrapper.get('[data-testid="onboarding-path-cli"]').trigger('click')
    expect(wrapper.findAll('[data-testid^="onboarding-backend-"]')).toHaveLength(6)
    await wrapper.get('[data-testid="onboarding-backend-deepseek_harness"]').trigger('click')
    await wrapper.get('[data-testid="onboarding-path-apiKey"]').trigger('click')
    expect(wrapper.find('[data-testid="onboarding-backend-deepseek_harness"]').exists()).toBe(false)
    await wrapper.get('[data-testid="onboarding-path-cli"]').trigger('click')
    expect(wrapper.get('[data-testid="onboarding-backend-deepseek_harness"]').attributes('aria-checked')).toBe('true')
    await next(wrapper)
    expect(wrapper.find('[data-test="opencode-provider"]').exists()).toBe(false)
    expect(wrapper.get('a[href="https://platform.deepseek.com/api_keys"]').exists()).toBe(true)
    await pickCliModel(wrapper, 'deepseek-flash')
    await wrapper.get('[data-testid="onboarding-api-key"]').setValue('dsh-test-key')
    await next(wrapper)
    await fillIdentity(wrapper)
    await next(wrapper)
    await next(wrapper)
    await next(wrapper)
    const body = await lastBody()
    expect(body).toMatchObject({ acpBackend: 'deepseek_harness', apiKey: 'dsh-test-key', cliModel: 'deepseek-flash' })
    expect(body).not.toHaveProperty('openCodeProvider')
    expect(body).not.toHaveProperty('openCodeModel')
    wrapper.unmount()
  })

  it('shows one topic per step: preferences, model, key, Git, team, workflow, done', async () => {
    const wrapper = await mountWizard()
    expect(wrapper.findAll('[data-testid^="onboarding-rail-"]').map((w) => w.attributes('data-testid'))).toEqual([
      'onboarding-rail-prefs',
      'onboarding-rail-model',
      'onboarding-rail-key',
      'onboarding-rail-git',
      'onboarding-rail-team',
      'onboarding-rail-workflow',
      'onboarding-rail-done',
    ])
    const section = (id: string) => wrapper.find(`[data-testid="onboarding-section-${id}"]`).exists()
    expect(activeStep(wrapper)).toBe('onboarding-rail-prefs')
    expect(section('language')).toBe(true)
    expect(wrapper.find('[data-testid="onboarding-vnc-preview"]').exists()).toBe(true)
    expect(section('backend')).toBe(false)

    for (const [step, id] of [
      ['model', 'backend'],
      ['key', 'key'],
      ['git', 'git'],
    ]) {
      if (step === 'git') await fillOpenCodeAuth(wrapper)
      await next(wrapper)
      expect(activeStep(wrapper)).toBe(`onboarding-rail-${step}`)
      expect(section(id!)).toBe(true)
      expect(section('language')).toBe(false)
    }
  })

  it('persists language and theme switches', async () => {
    const wrapper = await mountWizard()
    await wrapper.find('[data-testid="onboarding-language-zh-CN"]').trigger('click')
    await vi.waitFor(() => {
      expect(localStorage.getItem('grasp-locale')).toBe('zh-CN')
    })
    await wrapper.find('[data-testid="onboarding-theme-light"]').trigger('click')
    expect(localStorage.getItem('grasp-theme')).toBe('light')
    expect(document.documentElement.classList.contains('light')).toBe(true)
    await wrapper.find('[data-testid="onboarding-theme-dark"]').trigger('click')
    expect(localStorage.getItem('grasp-theme')).toBe('dark')
  })

  it('backend section offers two start paths and swaps the detail block', async () => {
    const wrapper = await mountWizard()
    await next(wrapper)
    expect(wrapper.find('[data-testid="onboarding-path-apikey-detail"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="onboarding-backend-cursor"]').exists()).toBe(false)

    await wrapper.find('[data-testid="onboarding-path-cli"]').trigger('click')
    await nextTick()
    expect(wrapper.find('[data-testid="onboarding-backend-cursor"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="onboarding-backend-trae"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="onboarding-path-apikey-detail"]').exists()).toBe(false)

    await wrapper.find('[data-testid="onboarding-path-apiKey"]').trigger('click')
    await nextTick()
    expect(wrapper.find('[data-testid="onboarding-path-apikey-detail"]').exists()).toBe(true)
  })

  it('the key step requires the key and the OpenCode model; the Git step requires the identity', async () => {
    const wrapper = await mountWizard()
    await next(wrapper)
    await next(wrapper)
    await next(wrapper)
    expect(activeStep(wrapper)).toBe('onboarding-rail-key')
    expect(wrapper.find('[data-testid="onboarding-cli-model"]').exists()).toBe(false)

    await wrapper.find('[data-testid="onboarding-api-key"]').setValue('sk-oc-demo')
    await next(wrapper)
    expect(wrapper.find('[data-test="opencode-model-required"]').exists()).toBe(true)
    expect(activeStep(wrapper)).toBe('onboarding-rail-key')

    await fillOpenCodeAuth(wrapper)
    await next(wrapper)
    expect(activeStep(wrapper)).toBe('onboarding-rail-git')
    await next(wrapper)
    expect(activeStep(wrapper)).toBe('onboarding-rail-git')

    await wrapper.find('[data-testid="onboarding-git-user-name"]').setValue('Ada')
    await wrapper.find('[data-testid="onboarding-git-user-email"]').setValue('ada@example.com')
    await next(wrapper)
    expect(activeStep(wrapper)).toBe('onboarding-rail-team')
  })

  it('team step shows four template cards with capabilities', async () => {
    const { api } = await import('@/lib/api/api')
    const wrapper = await mountWizard()
    expect(api.listAgentTeamTemplates).toHaveBeenCalled()
    await walkToTeam(wrapper)

    for (const id of ['clarify', 'implement', 'test_review', 'deliver']) {
      expect(wrapper.find(`[data-testid="onboarding-team-card-${id}"]`).exists()).toBe(true)
      expect(wrapper.find(`[data-testid="onboarding-team-caps-${id}"]`).exists()).toBe(true)
    }
    const name = (id: string) =>
      (wrapper.find(`[data-testid="onboarding-team-name-${id}"]`).element as HTMLInputElement).value
    expect([name('clarify'), name('implement'), name('test_review'), name('deliver')]).toEqual(['需求澄清', '实现', '测试评审', '交付'])

    const clarifyCaps = wrapper.find('[data-testid="onboarding-team-caps-clarify"]').text()
    expect(clarifyCaps).toContain('pages.onboarding.team.interactionClarify')
    // te() is stubbed to false, so schemas fall back to manifest labels; optional ones collapse into a count.
    expect(clarifyCaps).toContain('计划')
    expect(clarifyCaps).toContain('pages.onboarding.team.moreWrites')
    expect(wrapper.find('[data-testid="onboarding-team-preview-clarify"]').text()).toBe('pages.onboarding.team.preview')
    expect(wrapper.find('[data-testid="onboarding-team-preview-test_review"]').text()).toBe(
      'pages.onboarding.team.noPreview',
    )
    expect(wrapper.find('[data-testid="onboarding-team-caps-implement"]').text()).toContain(
      'pages.onboarding.team.review',
    )

    const toggle = (id: string) => wrapper.find(`[data-testid="onboarding-team-toggle-${id}"]`)
    expect(toggle('clarify').exists()).toBe(false)
    expect(toggle('implement').exists()).toBe(false)
    expect((toggle('test_review').element as HTMLInputElement).checked).toBe(true)
  })

  it('defaults the team to English names when English is chosen, and sends them', async () => {
    const wrapper = await mountWizard()
    await wrapper.find('[data-testid="onboarding-language-en"]').trigger('click')
    await flushPromises()
    await walkToTeam(wrapper)
    const name = (id: string) =>
      (wrapper.find(`[data-testid="onboarding-team-name-${id}"]`).element as HTMLInputElement).value
    expect([name('clarify'), name('implement'), name('test_review'), name('deliver')]).toEqual([
      'Clarify',
      'Implement',
      'TestReview',
      'Deliver',
    ])
    await next(wrapper)
    expect(wrapper.find('[data-testid="onboarding-preview-node-clarify"]').text()).toContain('Clarify')
    expect(wrapper.find('[data-testid="onboarding-preview-node-clarify"]').text()).not.toMatch(/[\u3400-\u9fff]/)
    await next(wrapper)
    expect((await lastBody())?.agents).toEqual([
      { templateId: 'clarify', name: 'Clarify' },
      { templateId: 'implement', name: 'Implement' },
      { templateId: 'test_review', name: 'TestReview' },
      { templateId: 'deliver', name: 'Deliver' },
    ])
  })

  it('renames, picks a model, and unchecking test_review trims the preview and the request', async () => {
    const wrapper = await mountWizard()
    await walkToTeam(wrapper)

    await wrapper.find('[data-testid="onboarding-team-name-implement"]').setValue('编码')
    await wrapper.find('[data-testid="onboarding-team-model-implement"]').setValue('gpt-5')
    await wrapper.find('[data-testid="onboarding-team-toggle-test_review"]').setValue(false)
    expect(wrapper.find('[data-testid="onboarding-team-name-test_review"]').exists()).toBe(false)
    await next(wrapper)

    expect(activeStep(wrapper)).toBe('onboarding-rail-workflow')
    expect(wrapper.find('[data-testid="onboarding-preview-node-test_review"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="onboarding-preview-node-implement"]').text()).toContain('编码')
    expect(wrapper.find('[data-testid="onboarding-preview-edge-implement-deliver"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="onboarding-workflow-note"]').text()).toBe('pages.onboarding.workflow.noReview')

    await next(wrapper)
    expect((await lastBody())?.agents).toEqual([
      { templateId: 'clarify', name: '需求澄清' },
      { templateId: 'implement', name: '编码', model: 'gpt-5' },
      { templateId: 'deliver', name: '交付' },
    ])
  })

  it('workflow preview shows the full default workflow with the fail loop', async () => {
    const wrapper = await mountWizard()
    await walkToTeam(wrapper)
    await next(wrapper)
    for (const id of ['input', 'clarify', 'implement', 'test_review', 'deliver', 'output']) {
      expect(wrapper.find(`[data-testid="onboarding-preview-node-${id}"]`).exists()).toBe(true)
    }
    expect(wrapper.find('[data-testid="onboarding-preview-edge-test_review-deliver"]').text()).toContain(
      'pages.onboarding.workflow.pass',
    )
    expect(wrapper.find('[data-testid="onboarding-preview-edge-deliver-output"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="onboarding-preview-edge-test_review-implement-fail"]').text()).toContain(
      'pages.onboarding.workflow.fail',
    )
    expect(wrapper.find('[data-testid="onboarding-workflow-note"]').text()).toBe('pages.onboarding.workflow.failLoop')
  })

  it('duplicate or blank team names block the team step', async () => {
    const { api } = await import('@/lib/api/api')
    const wrapper = await mountWizard()
    await walkToTeam(wrapper)
    await wrapper.find('[data-testid="onboarding-team-name-test_review"]').setValue('')
    expect(wrapper.find('[data-testid="onboarding-team-name-error-test_review"]').exists()).toBe(false)
    await next(wrapper)
    expect(wrapper.find('[data-testid="onboarding-team-name-error-test_review"]').text()).toBe(
      'pages.onboarding.team.nameIssues.required',
    )
    expect(activeStep(wrapper)).toBe('onboarding-rail-team')

    await wrapper.find('[data-testid="onboarding-team-name-test_review"]').setValue('实现')
    expect(wrapper.find('[data-testid="onboarding-team-name-error-test_review"]').text()).toBe(
      'pages.onboarding.team.nameIssues.duplicate',
    )
    await next(wrapper)
    expect(activeStep(wrapper)).toBe('onboarding-rail-team')
    expect(api.bootstrapOnboarding).not.toHaveBeenCalled()
  })

  it('falls back to the default team when templates fail to load', async () => {
    const { api } = await import('@/lib/api/api')
    vi.mocked(api.listAgentTeamTemplates).mockRejectedValueOnce(new Error('down'))
    const wrapper = await mountWizard()
    await walkToTeam(wrapper)
    expect(wrapper.find('[data-testid="onboarding-team-load-error"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="onboarding-team-caps-clarify"]').exists()).toBe(false)
    await next(wrapper)
    expect(activeStep(wrapper)).toBe('onboarding-rail-workflow')
  })

  it('bootstraps with OpenCode, repo, identity and preview flags', async () => {
    const wrapper = await mountWizard()
    await walkToTeam(wrapper, {
      onGit: async () => {
        const hint = () => wrapper.find('[data-testid="onboarding-repo-hint"]')
        expect(hint().exists()).toBe(false)
        expect(wrapper.text()).not.toContain('pages.onboarding.repo.hint')
        await wrapper.find('[data-testid="onboarding-repo-url"]').setValue('https://github.com/org/web.git')
        await wrapper.find('[data-testid="onboarding-repo-branch"]').setValue('develop')
        expect(hint().text()).toBe('pages.onboarding.repo.cloneTo')
      },
    })
    await next(wrapper)
    await next(wrapper)

    expect(await lastBody()).toEqual(
      expect.objectContaining({
        acpBackend: 'opencode',
        apiKey: 'sk-oc-demo',
        openCodeProvider: 'deepseek',
        openCodeModel: 'deepseek/deepseek-v4-pro',
        repoUrl: 'https://github.com/org/web.git',
        repoBranch: 'develop',
        gitUserName: 'Ada Lovelace',
        gitUserEmail: 'ada@example.com',
        vncPreview: true,
        browserMcp: true,
      }),
    )
    expect(wrapper.find('[data-testid="onboarding-success"]').exists()).toBe(true)
    expect(activeStep(wrapper)).toBe('onboarding-rail-done')
  })

  it('skipping Git drops the repo and credentials', async () => {
    const wrapper = await mountWizard()
    await walkToTeam(wrapper, {
      onGit: async () => {
        await wrapper.find('[data-testid="onboarding-repo-url"]').setValue('https://github.com/org/web.git')
        await wrapper.find('[data-testid="onboarding-git-type-github_https"]').trigger('click')
        await wrapper.find('[data-testid="onboarding-github-token"]').setValue('ghp_x')
        await wrapper.find('[data-testid="onboarding-git-skip"]').trigger('click')
        expect(wrapper.find('[data-testid="onboarding-git-skipped"]').exists()).toBe(true)
        expect(wrapper.find('[data-testid="onboarding-repo-url"]').exists()).toBe(false)
      },
    })
    await next(wrapper)
    expect(wrapper.find('[data-testid="onboarding-review-repo"]').text()).toContain('pages.onboarding.workflow.repoSkip')
    await next(wrapper)

    const body = await lastBody()
    expect(body).not.toHaveProperty('repoUrl')
    expect(body).not.toHaveProperty('githubToken')
    expect(body).toEqual(expect.objectContaining({ gitUserName: 'Ada Lovelace' }))
    expect(wrapper.find('[data-testid="onboarding-success-git"]').text()).toBe('pages.onboarding.success.gitSkip')
  })

  it('done page: run once opens the run launcher for the new workflow', async () => {
    const { api } = await import('@/lib/api/api')
    const wrapper = await mountWizard()
    await walkToTeam(wrapper)
    await next(wrapper)
    await next(wrapper)
    expect(wrapper.emitted('completed')?.[0]).toEqual([
      { agentIds: ['需求澄清', '实现', '测试评审'], workflowId: 'wf-1', published: true },
    ])
    expect(wrapper.find('[data-testid="onboarding-success-agents"]').text()).toContain('测试评审')

    await wrapper.find('[data-testid="onboarding-run-once"]').trigger('click')
    await flushPromises()
    expect(api.getWorkflow).toHaveBeenCalledWith('wf-1')
    expect(launchMocks.openLaunch).toHaveBeenCalledWith(expect.objectContaining({ id: 'wf-1' }))
    expect(wrapper.emitted('close')).toBeTruthy()
    expect(wrapper.router.currentRoute.value.path).toBe('/workflows')
    // The created default workflow is what stops the wizard from re-opening.
    expect(isOnboardingSuppressed()).toBe(false)
    expect(shouldAutoOpenOnboarding([{ name: ONBOARDING_WORKFLOW_NAMES[0] }], [])).toBe(false)
  })

  it('done page: edit workflow opens the editor', async () => {
    const wrapper = await mountWizard()
    await walkToTeam(wrapper)
    await next(wrapper)
    await next(wrapper)
    await wrapper.find('[data-testid="onboarding-edit-workflow"]').trigger('click')
    await flushPromises()
    expect(wrapper.router.currentRoute.value.path).toBe('/workflows/wf-1/edit')
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('later, backdrop and close do not persist suppression', async () => {
    for (const id of ['onboarding-later', 'onboarding-backdrop', 'onboarding-close']) {
      const wrapper = await mountWizard()
      await wrapper.find(`[data-testid="${id}"]`).trigger('click')
      expect(wrapper.emitted('close')).toBeTruthy()
      expect(isOnboardingSuppressed()).toBe(false)
    }
    expect(shouldAutoOpenOnboarding([], [])).toBe(true)
  })

  it('the storage escape hatch still suppresses auto-open', () => {
    suppressOnboarding()
    expect(shouldAutoOpenOnboarding([], [])).toBe(false)
  })

  it('coding-tool key step shows an optional model and still requires the key', async () => {
    const wrapper = await mountWizard()
    await goToCliKey(wrapper)
    const model = wrapper.get('[data-testid="onboarding-cli-model"]')
    const trigger = model.get('[data-testid="cli-model-trigger"]').text()
    expect(trigger).toContain('pages.agentStudio.cliModel.auto')
    expect(trigger).not.toContain('pages.agentStudio.cliModel.autoSub')
    expect(model.text()).not.toContain('pages.agentStudio.cliModel.wizardHint')
    expect(wrapper.text()).not.toContain('不确定就先用自动')
    expect(wrapper.text()).not.toContain('由 Cursor 决定用哪个模型')

    await next(wrapper)
    expect(activeStep(wrapper)).toBe('onboarding-rail-key')

    await wrapper.find('[data-testid="onboarding-api-key"]').setValue('crsr_key')
    await next(wrapper)
    expect(activeStep(wrapper)).toBe('onboarding-rail-git')
  })

  it('shows the model picker for every coding tool, and Codex still requires the login file', async () => {
    const wrapper = await mountWizard()
    await next(wrapper)
    await wrapper.find('[data-testid="onboarding-path-cli"]').trigger('click')
    for (const id of ['cursor', 'claude_code', 'codebuddy', 'trae', 'codex', 'deepseek_harness']) {
      await wrapper.find(`[data-testid="onboarding-backend-${id}"]`).trigger('click')
      await next(wrapper)
      expect(wrapper.find('[data-testid="onboarding-cli-model"]').exists()).toBe(true)
      const trigger = wrapper.get('[data-testid="cli-model-trigger"]').text()
      expect(trigger).toContain('pages.agentStudio.cliModel.auto')
      expect(trigger).not.toContain('pages.agentStudio.cliModel.autoSub')
      if (id === 'codex') {
        expect(wrapper.find('textarea[data-testid="onboarding-api-key"]').exists()).toBe(true)
      } else {
        expect(wrapper.find('input[data-testid="onboarding-api-key"]').exists()).toBe(true)
      }
      await wrapper.find('[data-testid="onboarding-key-change"]').trigger('click')
      expect(activeStep(wrapper)).toBe('onboarding-rail-model')
    }

    await wrapper.find('[data-testid="onboarding-backend-codex"]').trigger('click')
    await next(wrapper)
    await next(wrapper)
    expect(activeStep(wrapper)).toBe('onboarding-rail-key')
    await wrapper.find('[data-testid="onboarding-api-key"]').setValue('{"access_token":"x"}')
    await next(wrapper)
    expect(activeStep(wrapper)).toBe('onboarding-rail-git')
  })

  it('keeps a model when the same tool is clicked again and clears it when the tool changes', async () => {
    const wrapper = await mountWizard()
    await goToCliKey(wrapper)
    await pickCliModel(wrapper, 'composer-2.5')
    expect(wrapper.get('[data-testid="cli-model-trigger"]').text()).toContain('Composer 2.5')

    await wrapper.find('[data-testid="onboarding-key-change"]').trigger('click')
    await wrapper.find('[data-testid="onboarding-backend-cursor"]').trigger('click')
    await next(wrapper)
    expect(wrapper.get('[data-testid="cli-model-trigger"]').text()).toContain('Composer 2.5')

    await wrapper.find('[data-testid="onboarding-key-change"]').trigger('click')
    await wrapper.find('[data-testid="onboarding-backend-claude_code"]').trigger('click')
    await next(wrapper)
    const cleared = wrapper.get('[data-testid="cli-model-trigger"]').text()
    expect(cleared).toContain('pages.agentStudio.cliModel.auto')
    expect(cleared).not.toContain('Composer 2.5')

    await wrapper.find('[data-testid="onboarding-key-change"]').trigger('click')
    await wrapper.find('[data-testid="onboarding-backend-cursor"]').trigger('click')
    await next(wrapper)
    const restored = wrapper.get('[data-testid="cli-model-trigger"]').text()
    expect(restored).not.toContain('Composer 2.5')
    expect(restored).toContain('pages.agentStudio.cliModel.auto')
  })

  it('accepts a typed model id when the catalog cannot be read', async () => {
    cliCatalogMocks.load.mockResolvedValue([])
    cliCatalogMocks.failed.mockReturnValue(true)
    const wrapper = await mountWizard()
    await goToCliKey(wrapper)
    await wrapper.get('[data-testid="cli-model-trigger"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="cli-model-failed"]').exists()).toBe(true)
    const search = wrapper.get('[data-testid="cli-model-search"]')
    await search.setValue('my-private-model')
    await search.trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(wrapper.get('[data-testid="cli-model-trigger"]').text()).toContain('my-private-model')
    await wrapper.find('[data-testid="onboarding-api-key"]').setValue('crsr_key')
    await next(wrapper)
    expect(activeStep(wrapper)).toBe('onboarding-rail-git')
  })

  it('keeps a model id that is outside the catalog and still continues', async () => {
    const wrapper = await mountWizard()
    await goToCliKey(wrapper)
    await wrapper.get('[data-testid="cli-model-trigger"]').trigger('click')
    await flushPromises()
    const search = wrapper.get('[data-testid="cli-model-search"]')
    await search.setValue('my-private-model')
    await search.trigger('keydown', { key: 'Enter' })
    await flushPromises()
    const unlisted = wrapper.get('[data-testid="cli-model-unlisted"]').text()
    expect(unlisted).toContain('my-private-model')
    expect(unlisted).toContain('pages.agentStudio.cliModel.unlisted')
    await wrapper.find('[data-testid="onboarding-api-key"]').setValue('crsr_key')
    await next(wrapper)
    expect(activeStep(wrapper)).toBe('onboarding-rail-git')
  })

  it('puts the chosen model on the team placeholder, the preview, and cliModel', async () => {
    const wrapper = await mountWizard()
    await goToCliKey(wrapper)
    await pickCliModel(wrapper, 'composer-2.5')
    await wrapper.find('[data-testid="onboarding-api-key"]').setValue('crsr_key')
    await next(wrapper)
    await fillIdentity(wrapper)
    await next(wrapper)
    expect(wrapper.get('[data-testid="onboarding-team-model-clarify"]').attributes('placeholder')).toBe(
      'pages.onboarding.team.modelInherit',
    )
    await wrapper.find('[data-testid="onboarding-team-model-implement"]').setValue('gpt-5')
    await next(wrapper)
    expect(wrapper.get('[data-testid="onboarding-review-backend"]').text()).toContain('Cursor · composer-2.5')
    await next(wrapper)
    const body = await lastBody()
    expect(body?.cliModel).toBe('composer-2.5')
    expect(body).not.toHaveProperty('openCodeModel')
    expect(body?.agents).toEqual(
      expect.arrayContaining([
        { templateId: 'clarify', name: '需求澄清' },
        { templateId: 'implement', name: '实现', model: 'gpt-5' },
      ]),
    )
  })

  it('leaves auto off the preview and out of the bootstrap body', async () => {
    const wrapper = await mountWizard()
    await goToCliKey(wrapper)
    await wrapper.find('[data-testid="onboarding-api-key"]').setValue('crsr_key')
    await next(wrapper)
    await fillIdentity(wrapper)
    await next(wrapper)
    expect(wrapper.get('[data-testid="onboarding-team-model-implement"]').attributes('placeholder')).toBe(
      'pages.onboarding.team.modelPlaceholder',
    )
    await next(wrapper)
    const review = wrapper.get('[data-testid="onboarding-review-backend"]').text()
    expect(review).toContain('Cursor')
    expect(review).not.toContain('composer-2.5')
    expect(review).not.toContain('·')
    await next(wrapper)
    const body = await lastBody()
    expect(body?.acpBackend).toBe('cursor')
    expect(body).not.toHaveProperty('cliModel')
  })

  it('stays on the wizard and does not complete when bootstrap fails', async () => {
    const { api } = await import('@/lib/api/api')
    vi.mocked(api.bootstrapOnboarding).mockRejectedValueOnce(new Error('bootstrap failed'))
    const wrapper = await mountWizard()
    await walkToTeam(wrapper)
    await next(wrapper)
    await next(wrapper)
    expect(wrapper.emitted('completed')).toBeFalsy()
    expect(wrapper.emitted('close')).toBeFalsy()
    expect(wrapper.find('[data-testid="onboarding-success"]').exists()).toBe(false)
    expect(activeStep(wrapper)).toBe('onboarding-rail-workflow')
  })

  it('hides annotated wizard copy and keeps the replacement sentences', async () => {
    const src = readFileSync(join(dirname(fileURLToPath(import.meta.url)), 'OnboardingWizard.vue'), 'utf8')
    expect(src).toMatch(
      /\.onb-team\s*\{[^}]*grid-template-columns:\s*minmax\(0,\s*1fr\)\s*20px\s*minmax\(0,\s*1fr\)\s*20px\s*minmax\(0,\s*1fr\)\s*20px\s*minmax\(0,\s*1fr\)/,
    )
    expect(src).toContain("t('pages.onboarding.repo.cloneTo', { dir: repoDirName })")
    expect(src).toContain("t('pages.onboarding.toastOk')")
    expect(src).not.toContain('pages.onboarding.workflow.reuseHint')
    expect(src).not.toContain('pages.onboarding.gitUser.hint')
    expect(src).not.toContain('pages.onboarding.git.foot')
    expect(src).not.toContain('pages.onboarding.apiKey.meta')
    const zhOnboarding = (await import('@/locales/zh-CN/pages.json')).default.pages.onboarding
    const enOnboarding = (await import('@/locales/en/pages.json')).default.pages.onboarding
    expect(zhOnboarding.emptyDesc).toContain('不会自动开跑')
    expect(zhOnboarding.emptyDescCreate).toContain('不会自动开跑')
    expect(zhOnboarding.toastOk).toContain('未自动开跑')
    expect(enOnboarding.emptyDesc).toContain('Does not start a run')
    expect(enOnboarding.emptyDescCreate).toContain('Does not start a run')
    expect(enOnboarding.toastOk).toMatch(/not started automatically/i)

    const wrapper = await mountWizard()
    await next(wrapper)
    await next(wrapper)
    expect(activeStep(wrapper)).toBe('onboarding-rail-key')
    expect(wrapper.text()).not.toContain('pages.onboarding.apiKey.meta')
    expect(wrapper.text()).toContain('pages.onboarding.apiKey.hint')
    expect(wrapper.text()).toContain('pages.agentStudio.openCode.modelHintTyped')
    expect(wrapper.text()).not.toMatch(/pages\.agentStudio\.openCode\.modelHint(?!Typed)/)
    expect(wrapper.text()).toContain('pages.agentStudio.openCode.baseHintOwn')
    expect(wrapper.text()).not.toMatch(/pages\.agentStudio\.openCode\.baseHint(?!Own)/)
    expect(wrapper.text()).not.toContain('pages.agentStudio.openCode.visionHint')
    expect(wrapper.get('[data-test="opencode-model-vision"]').text()).toContain('pages.agentStudio.openCode.visionLabel')
    expect(wrapper.get('[data-test="opencode-model-vision-input"]').exists()).toBe(true)

    const codex = await mountWizard()
    await goToCliKey(codex, 'codex')
    expect(codex.text()).toContain('pages.onboarding.apiKey.codexMeta')
    expect(codex.text()).not.toContain('pages.onboarding.apiKey.meta')

    const git = await mountWizard()
    await next(git)
    await next(git)
    await fillOpenCodeAuth(git)
    await next(git)
    expect(activeStep(git)).toBe('onboarding-rail-git')
    expect(git.text()).toContain('pages.onboarding.git.meta')
    expect(git.text()).toContain('pages.onboarding.gitUser.nameLabel')
    expect(git.text()).toContain('pages.onboarding.gitUser.emailLabel')
    expect(git.text()).not.toContain('pages.onboarding.gitUser.hint')
    expect(git.text()).not.toContain('pages.onboarding.git.foot')
    await git.get('[data-testid="onboarding-git-type-github_https"]').trigger('click')
    expect(git.find('[data-testid="onboarding-github-token"]').exists()).toBe(true)
    await git.get('[data-testid="onboarding-git-type-gitlab_https"]').trigger('click')
    expect(git.find('[data-testid="onboarding-gitlab-token"]').exists()).toBe(true)
    await git.get('[data-testid="onboarding-git-type-ssh"]').trigger('click')
    expect(git.find('[data-testid="onboarding-ssh-key"]').exists()).toBe(true)

    await fillIdentity(git)
    await next(git)
    expect(git.find('[data-testid="onboarding-team-card-clarify"]').exists()).toBe(true)
    expect(git.find('[data-testid="onboarding-team-card-implement"]').exists()).toBe(true)
    expect(git.find('[data-testid="onboarding-team-card-test_review"]').exists()).toBe(true)
    expect(git.find('[data-testid="onboarding-team-card-deliver"]').exists()).toBe(true)
    expect(git.findAll('.onb-team-arrow')).toHaveLength(3)

    await next(git)
    expect(activeStep(git)).toBe('onboarding-rail-workflow')
    expect(git.text()).toContain('pages.onboarding.workflow.meta')
    expect(git.text()).toContain('pages.onboarding.workflow.summary')
    expect(git.text()).toContain('pages.onboarding.workflow.failLoop')
    expect(git.text()).not.toContain('pages.onboarding.workflow.reuseHint')
    expect(git.find('[data-testid="onboarding-preview-node-implement"]').exists()).toBe(true)
  })

  it('retry continues in the workspace from the preferences step', async () => {
    const wrapper = await mountWizard({ mode: 'retry' })
    expect(wrapper.find('[data-testid="onboarding-title"]').text()).toBe('pages.onboarding.titleRetry')
    expect(wrapper.find('[data-testid="onboarding-section-language"]').exists()).toBe(true)
    await walkToTeam(wrapper)
    expect((wrapper.find('[data-testid="onboarding-team-name-implement"]').element as HTMLInputElement).value).toBe('实现')
  })
})
