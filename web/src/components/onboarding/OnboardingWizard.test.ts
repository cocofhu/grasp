// @vitest-environment happy-dom
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'
import OnboardingWizard from './OnboardingWizard.vue'
import {
  DEFAULT_PROJECT_ID,
  suppressOnboarding,
  isOnboardingSuppressed,
  ONBOARDING_WORKFLOW_NAME,
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
]

const launchMocks = vi.hoisted(() => ({ openLaunch: vi.fn(async () => {}) }))

vi.mock('@/lib/api/api', () => ({
  api: {
    createProject: vi.fn(),
    getProject: vi.fn(async () => ({ id: 'p-retry', name: '老项目' })),
    getWorkflow: vi.fn(async (id: string) => ({ id, name: '默认工作流' })),
    listAgentTeamTemplates: vi.fn(async () => ({ items: TEMPLATES })),
    bootstrapProjectOnboarding: vi.fn(async () => ({
      agentIds: ['需求澄清', '实现', '测试评审'],
      workflowId: 'wf-1',
      published: true,
      groupName: '默认项目组',
    })),
    openCodeProviders: vi.fn(async () => ({
      providers: [{ id: 'deepseek', name: 'DeepSeek', models: 1 }],
    })),
    openCodeModels: vi.fn(async () => ({ models: [{ id: 'deepseek-v4-pro' }] })),
  },
}))

vi.mock('@/lib/run/useWorkflowRunLaunch', () => ({
  useWorkflowRunLaunch: () => ({ openLaunch: launchMocks.openLaunch }),
}))

vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({ success: vi.fn(), error: vi.fn(), warn: vi.fn(), show: vi.fn() }),
}))

vi.mock('@/lib/composables/useProjectContext', () => ({
  writeStoredProjectId: vi.fn(),
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
      { path: '/projects/:id', component: { template: '<div />' } },
      { path: '/workflows/:id/edit', component: { template: '<div />' } },
    ],
  })
  await router.push('/')
  const wrapper = mount(OnboardingWizard, {
    props: { open: true, projectId: DEFAULT_PROJECT_ID, mode: 'firstInstall', ...props },
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

async function fillConnect(wrapper: Wrapper, key = 'sk-oc-demo') {
  await fillOpenCodeAuth(wrapper, key)
  await wrapper.find('[data-testid="onboarding-git-user-name"]').setValue('Ada Lovelace')
  await wrapper.find('[data-testid="onboarding-git-user-email"]').setValue('ada@example.com')
}

async function next(wrapper: Wrapper) {
  await wrapper.find('[data-testid="onboarding-next"]').trigger('click')
  await flushPromises()
}

function activeStep(wrapper: Wrapper) {
  return wrapper.find('[data-active="1"]').attributes('data-testid')
}

async function lastBody() {
  const { api } = await import('@/lib/api/api')
  const calls = vi.mocked(api.bootstrapProjectOnboarding).mock.calls
  return calls[calls.length - 1]?.[1]
}

describe('OnboardingWizard', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
  })

  it('shows four steps and puts language, backend, key and Git on the connect page', async () => {
    const wrapper = await mountWizard()
    expect(wrapper.findAll('[data-testid^="onboarding-rail-"]').map((w) => w.attributes('data-testid'))).toEqual([
      'onboarding-rail-connect',
      'onboarding-rail-team',
      'onboarding-rail-workflow',
      'onboarding-rail-done',
    ])
    expect(activeStep(wrapper)).toBe('onboarding-rail-connect')
    for (const id of ['language', 'backend', 'key', 'git']) {
      expect(wrapper.find(`[data-testid="onboarding-section-${id}"]`).exists()).toBe(true)
    }
    expect(wrapper.find('[data-testid="onboarding-project-name"]').exists()).toBe(false)
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

  it('connect requires the key, the OpenCode model and the Git identity', async () => {
    const wrapper = await mountWizard()
    await next(wrapper)
    expect(activeStep(wrapper)).toBe('onboarding-rail-connect')

    await wrapper.find('[data-testid="onboarding-api-key"]').setValue('sk-oc-demo')
    await next(wrapper)
    expect(wrapper.find('[data-test="opencode-model-required"]').exists()).toBe(true)
    expect(activeStep(wrapper)).toBe('onboarding-rail-connect')

    await fillOpenCodeAuth(wrapper)
    await next(wrapper)
    expect(activeStep(wrapper)).toBe('onboarding-rail-connect')

    await wrapper.find('[data-testid="onboarding-git-user-name"]').setValue('Ada')
    await wrapper.find('[data-testid="onboarding-git-user-email"]').setValue('ada@example.com')
    await next(wrapper)
    expect(activeStep(wrapper)).toBe('onboarding-rail-team')
  })

  it('team step shows three template cards with capabilities', async () => {
    const { api } = await import('@/lib/api/api')
    const wrapper = await mountWizard()
    expect(api.listAgentTeamTemplates).toHaveBeenCalled()
    await fillConnect(wrapper)
    await next(wrapper)

    for (const id of ['clarify', 'implement', 'test_review']) {
      expect(wrapper.find(`[data-testid="onboarding-team-card-${id}"]`).exists()).toBe(true)
      expect(wrapper.find(`[data-testid="onboarding-team-caps-${id}"]`).exists()).toBe(true)
    }
    const name = (id: string) =>
      (wrapper.find(`[data-testid="onboarding-team-name-${id}"]`).element as HTMLInputElement).value
    expect([name('clarify'), name('implement'), name('test_review')]).toEqual(['需求澄清', '实现', '测试评审'])

    const clarifyCaps = wrapper.find('[data-testid="onboarding-team-caps-clarify"]').text()
    expect(clarifyCaps).toContain('pages.onboarding.team.interactionClarify')
    // te() is stubbed to false, so tools fall back to ids and schemas to manifest labels.
    expect(clarifyCaps).toContain('ask_question')
    expect(clarifyCaps).toContain('计划')
    expect(wrapper.find('[data-testid="onboarding-team-preview-clarify"]').text()).toBe('pages.onboarding.team.preview')
    expect(wrapper.find('[data-testid="onboarding-team-preview-test_review"]').text()).toBe(
      'pages.onboarding.team.noPreview',
    )
    expect(wrapper.find('[data-testid="onboarding-team-caps-implement"]').text()).toContain(
      'pages.onboarding.team.review',
    )

    const toggle = (id: string) => wrapper.find(`[data-testid="onboarding-team-toggle-${id}"]`).element as HTMLInputElement
    expect(toggle('clarify').disabled).toBe(true)
    expect(toggle('implement').disabled).toBe(true)
    expect(toggle('test_review').disabled).toBe(false)
  })

  it('renames, picks a model, and unchecking test_review trims the preview and the request', async () => {
    const wrapper = await mountWizard()
    await fillConnect(wrapper)
    await next(wrapper)

    await wrapper.find('[data-testid="onboarding-team-name-implement"]').setValue('编码')
    await wrapper.find('[data-testid="onboarding-team-model-implement"]').setValue('gpt-5')
    await wrapper.find('[data-testid="onboarding-team-toggle-test_review"]').setValue(false)
    expect(wrapper.find('[data-testid="onboarding-team-name-test_review"]').exists()).toBe(false)
    await next(wrapper)

    expect(activeStep(wrapper)).toBe('onboarding-rail-workflow')
    expect(wrapper.find('[data-testid="onboarding-preview-node-test_review"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="onboarding-preview-node-implement"]').text()).toContain('编码')
    expect(wrapper.find('[data-testid="onboarding-preview-edge-implement-output"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="onboarding-workflow-note"]').text()).toBe('pages.onboarding.workflow.noReview')

    await next(wrapper)
    expect((await lastBody())?.agents).toEqual([
      { templateId: 'clarify', name: '需求澄清' },
      { templateId: 'implement', name: '编码', model: 'gpt-5' },
    ])
  })

  it('workflow preview shows the full default workflow with the fail loop', async () => {
    const wrapper = await mountWizard()
    await fillConnect(wrapper)
    await next(wrapper)
    await next(wrapper)
    for (const id of ['input', 'clarify', 'implement', 'test_review', 'output']) {
      expect(wrapper.find(`[data-testid="onboarding-preview-node-${id}"]`).exists()).toBe(true)
    }
    expect(wrapper.find('[data-testid="onboarding-preview-edge-test_review-output"]').text()).toContain(
      'pages.onboarding.workflow.pass',
    )
    expect(wrapper.find('[data-testid="onboarding-preview-edge-test_review-implement-fail"]').text()).toContain(
      'pages.onboarding.workflow.fail',
    )
    expect(wrapper.find('[data-testid="onboarding-workflow-note"]').text()).toBe('pages.onboarding.workflow.failLoop')
  })

  it('duplicate or blank team names block the team step', async () => {
    const { api } = await import('@/lib/api/api')
    const wrapper = await mountWizard()
    await fillConnect(wrapper)
    await next(wrapper)
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
    expect(api.bootstrapProjectOnboarding).not.toHaveBeenCalled()
  })

  it('falls back to the default team when templates fail to load', async () => {
    const { api } = await import('@/lib/api/api')
    vi.mocked(api.listAgentTeamTemplates).mockRejectedValueOnce(new Error('down'))
    const wrapper = await mountWizard()
    await fillConnect(wrapper)
    await next(wrapper)
    expect(wrapper.find('[data-testid="onboarding-team-load-error"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="onboarding-team-caps-clarify"]').exists()).toBe(false)
    await next(wrapper)
    expect(activeStep(wrapper)).toBe('onboarding-rail-workflow')
  })

  it('bootstraps with OpenCode, repo, identity and preview flags', async () => {
    const wrapper = await mountWizard()
    await fillConnect(wrapper)
    const hint = () => wrapper.find('[data-testid="onboarding-repo-hint"]').text()
    expect(hint()).toBe('pages.onboarding.repo.hint')
    await wrapper.find('[data-testid="onboarding-repo-url"]').setValue('https://github.com/org/web.git')
    await wrapper.find('[data-testid="onboarding-repo-branch"]').setValue('develop')
    expect(hint()).toBe('pages.onboarding.repo.cloneTo')
    await next(wrapper)
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
    await fillConnect(wrapper)
    await wrapper.find('[data-testid="onboarding-repo-url"]').setValue('https://github.com/org/web.git')
    await wrapper.find('[data-testid="onboarding-git-type-github_https"]').trigger('click')
    await wrapper.find('[data-testid="onboarding-github-token"]').setValue('ghp_x')
    await wrapper.find('[data-testid="onboarding-git-skip"]').trigger('click')
    expect(wrapper.find('[data-testid="onboarding-git-skipped"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="onboarding-repo-url"]').exists()).toBe(false)
    await next(wrapper)
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
    await fillConnect(wrapper)
    await next(wrapper)
    await next(wrapper)
    await next(wrapper)
    expect(wrapper.emitted('completed')?.[0]?.[0]).toEqual(expect.objectContaining({ workflowId: 'wf-1' }))
    expect(wrapper.find('[data-testid="onboarding-success-agents"]').text()).toContain('测试评审')

    await wrapper.find('[data-testid="onboarding-run-once"]').trigger('click')
    await flushPromises()
    expect(api.getWorkflow).toHaveBeenCalledWith('wf-1')
    expect(launchMocks.openLaunch).toHaveBeenCalledWith(expect.objectContaining({ id: 'wf-1' }))
    expect(wrapper.emitted('close')).toBeTruthy()
    expect(wrapper.router.currentRoute.value.path).toBe(`/projects/${DEFAULT_PROJECT_ID}`)
    // The created default workflow is what stops the wizard from re-opening.
    expect(isOnboardingSuppressed(DEFAULT_PROJECT_ID)).toBe(false)
    expect(shouldAutoOpenOnboarding(DEFAULT_PROJECT_ID, [{ name: ONBOARDING_WORKFLOW_NAME }], [])).toBe(false)
  })

  it('done page: edit workflow opens the editor', async () => {
    const wrapper = await mountWizard()
    await fillConnect(wrapper)
    await next(wrapper)
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
      expect(isOnboardingSuppressed(DEFAULT_PROJECT_ID)).toBe(false)
    }
    expect(shouldAutoOpenOnboarding(DEFAULT_PROJECT_ID, [], [])).toBe(true)
  })

  it('the storage escape hatch still suppresses auto-open', () => {
    suppressOnboarding(DEFAULT_PROJECT_ID)
    expect(shouldAutoOpenOnboarding(DEFAULT_PROJECT_ID, [], [])).toBe(false)
  })

  it('create mode: project name on connect, prefixed names, and bootstrap retry never re-creates (s3/f6)', async () => {
    const { api } = await import('@/lib/api/api')
    vi.mocked(api.createProject).mockResolvedValue({ id: 'proj-created-1', name: '支付中台' } as never)
    vi.mocked(api.bootstrapProjectOnboarding)
      .mockRejectedValueOnce(new Error('bootstrap blew up'))
      .mockResolvedValueOnce({
        agentIds: ['支付中台实现'],
        workflowId: 'wf-x',
        published: true,
        groupName: '支付中台项目组',
      } as never)

    const wrapper = await mountWizard({ mode: 'createProject', projectId: '' })
    expect(wrapper.find('[data-testid="onboarding-title"]').text()).toBe('pages.onboarding.titleCreate')
    expect(wrapper.find('[data-testid="onboarding-section-language"]').exists()).toBe(false)
    await fillConnect(wrapper, 'sk-create')
    await next(wrapper)
    expect(activeStep(wrapper)).toBe('onboarding-rail-connect')

    await wrapper.find('[data-testid="onboarding-project-name"]').setValue('支付中台')
    await next(wrapper)
    expect(
      (wrapper.find('[data-testid="onboarding-team-name-clarify"]').element as HTMLInputElement).value,
    ).toBe('支付中台需求澄清')
    await next(wrapper)
    await next(wrapper)

    expect(api.createProject).toHaveBeenCalledTimes(1)
    expect(api.bootstrapProjectOnboarding).toHaveBeenCalledWith('proj-created-1', expect.any(Object))
    expect(wrapper.find('[data-testid="onboarding-create-error"]').text()).toBe('bootstrap blew up')

    await next(wrapper)
    expect(api.createProject).toHaveBeenCalledTimes(1)
    expect(api.bootstrapProjectOnboarding).toHaveBeenCalledTimes(2)
    expect(api.bootstrapProjectOnboarding).toHaveBeenNthCalledWith(2, 'proj-created-1', expect.any(Object))
    expect(wrapper.find('[data-testid="onboarding-success"]').exists()).toBe(true)

    await wrapper.find('[data-testid="onboarding-success-close"]').trigger('click')
    await flushPromises()
    expect(wrapper.router.currentRoute.value.path).toBe('/projects/proj-created-1')
  })

  it('createProject does not overwrite grasp-locale (g2.1)', async () => {
    localStorage.setItem('grasp-locale', 'zh-CN')
    const { locale } = await import('@/lib/shared/locale')
    locale.value = 'zh-CN'
    vi.stubGlobal('navigator', { language: 'en-US' })

    const wrapper = await mountWizard({ mode: 'createProject', projectId: '' })
    expect(wrapper.find('[data-testid="onboarding-language-zh-CN"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="onboarding-project-name"]').exists()).toBe(true)
    await wrapper.find('[data-testid="onboarding-later"]').trigger('click')
    expect(localStorage.getItem('grasp-locale')).toBe('zh-CN')
    expect(locale.value).toBe('zh-CN')
    vi.unstubAllGlobals()
  })

  it('retry on a non-default project derives names from the project name', async () => {
    const { api } = await import('@/lib/api/api')
    const wrapper = await mountWizard({ mode: 'retry', projectId: 'p-retry' })
    expect(api.getProject).toHaveBeenCalledWith('p-retry')
    expect(wrapper.find('[data-testid="onboarding-title"]').text()).toBe('pages.onboarding.titleRetry')
    await fillConnect(wrapper)
    await next(wrapper)
    expect(
      (wrapper.find('[data-testid="onboarding-team-name-implement"]').element as HTMLInputElement).value,
    ).toBe('老项目实现')
  })
})
