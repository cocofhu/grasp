// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import OnboardingWizard from './OnboardingWizard.vue'
import { isOnboardingSuppressed, shouldAutoOpenOnboarding } from '@/lib/pm/onboardingWizard'

const mocks = vi.hoisted(() => ({
  listModelConfigurations: vi.fn(),
  listCredentials: vi.fn(),
  listModelConnections: vi.fn(),
  listToolCapabilities: vi.fn(),
  listAgentTeamTemplates: vi.fn(),
  bootstrapOnboarding: vi.fn(),
  getWorkflow: vi.fn(),
  openLaunch: vi.fn(),
  error: vi.fn(),
}))
vi.mock('@/lib/api/api', () => ({
  api: {
    listModelConfigurations: mocks.listModelConfigurations,
    listCredentials: mocks.listCredentials,
    listModelConnections: mocks.listModelConnections,
    listToolCapabilities: mocks.listToolCapabilities,
    listAgentTeamTemplates: mocks.listAgentTeamTemplates,
    bootstrapOnboarding: mocks.bootstrapOnboarding,
    getWorkflow: mocks.getWorkflow,
  },
}))
vi.mock('@/lib/run/useWorkflowRunLaunch', () => ({ useWorkflowRunLaunch: () => ({ openLaunch: mocks.openLaunch }) }))
vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({ success: vi.fn(), error: mocks.error, warn: vi.fn(), show: vi.fn() }),
}))
vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return { ...actual, useI18n: () => ({ locale: { value: 'zh-CN' }, t: (key: string) => key, te: () => false }) }
})
const ModelPicker = {
  name: 'ModelBindingEditor',
  props: ['modelValue'],
  emits: ['update:modelValue', 'validity'],
  template: '<div class="model-picker-stub" />',
}
const GitPicker = {
  name: 'AgentGitCredentialPicker',
  props: ['modelValue'],
  emits: ['update:modelValue', 'validity'],
  template: '<div class="git-picker-stub" />',
}
const ResourcesPanel = {
  name: 'WorkspaceCredentialsPanel',
  props: { inline: Boolean, scope: String, view: String, setup: Boolean, configurationId: String },
  emits: ['changed', 'modelSaved', 'credentialSaved', 'editing'],
  template: '<section class="resources-panel-stub" />',
}
const modelRows = [
  {
    id: 'model-main',
    name: 'Main model',
    backend: 'opencode',
    binding: { kind: 'connection', connectionId: 'conn-main', modelId: 'coding' },
    state: 'active',
    revision: 1,
  },
  {
    id: 'model-review',
    name: 'Review model',
    backend: 'codex',
    binding: { kind: 'connection', connectionId: 'conn-review', modelId: 'review' },
    state: 'active',
    revision: 1,
  },
]
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
      stubs: {
        Teleport: true,
        Icon: true,
        AppButton: { props: { disabled: Boolean }, template: '<button type="button" :disabled="disabled" v-bind="$attrs"><slot /></button>' },
        ModelBindingEditor: ModelPicker,
        AgentGitCredentialPicker: GitPicker,
        WorkspaceCredentialsPanel: ResourcesPanel,
      },
    },
  })
  await flushPromises()
  return Object.assign(wrapper, { router })
}
type Wrapper = Awaited<ReturnType<typeof mountWizard>>
async function next(w: Wrapper) {
  await w.get('[data-testid="onboarding-next"]').trigger('click')
  await flushPromises()
}
function activeStep(w: Wrapper) {
  return w.get('[data-active="1"]').attributes('data-testid')
}
async function chooseModel(w: Wrapper, id = 'model-main', valid = true) {
  w.findComponent(ModelPicker).vm.$emit('update:modelValue', { kind: 'configuration', configurationId: id })
  w.findComponent(ModelPicker).vm.$emit('validity', valid)
  await flushPromises()
}
async function identity(w: Wrapper) {
  await w.get('[data-testid="onboarding-git-user-name"]').setValue('Ada Lovelace')
  await w.get('[data-testid="onboarding-git-user-email"]').setValue('ada@example.com')
}
async function toGitCredentials(w: Wrapper) {
  await next(w)
  await next(w)
  await chooseModel(w)
  await next(w)
  expect(activeStep(w)).toBe('onboarding-rail-gitCredentials')
}
async function toGit(w: Wrapper) {
  await toGitCredentials(w)
  await next(w)
  expect(activeStep(w)).toBe('onboarding-rail-git')
}
async function toTeam(w: Wrapper) {
  await toGit(w)
  await identity(w)
  await next(w)
  expect(activeStep(w)).toBe('onboarding-rail-team')
}
async function complete(w: Wrapper) {
  await toTeam(w)
  await next(w)
  await next(w)
  expect(w.find('[data-testid="onboarding-success"]').exists()).toBe(true)
}
beforeEach(async () => {
  localStorage.clear()
  vi.clearAllMocks()
  vi.stubGlobal('navigator', { ...navigator, language: 'zh-CN' })
  const { locale } = await import('@/lib/shared/locale')
  locale.value = 'zh-CN'
  mocks.listModelConfigurations.mockResolvedValue({ items: modelRows })
  mocks.listCredentials.mockResolvedValue({ items: [{ id: 'model-key', purpose: 'model_api', configured: true }] })
  mocks.listModelConnections.mockResolvedValue({
    items: [
      {
        id: 'conn-main',
        state: 'active',
        credentialId: 'model-key',
        protocol: 'openai-chat-completions',
        auth: { scheme: 'bearer' },
        models: [{ id: 'coding' }],
      },
    ],
  })
  mocks.listToolCapabilities.mockResolvedValue({
    items: [
      { toolId: 'opencode', adapterState: 'existing', modes: ['connection'], protocols: ['openai-chat-completions'] },
    ],
  })
  mocks.listAgentTeamTemplates.mockResolvedValue({ items: [] })
  mocks.bootstrapOnboarding.mockResolvedValue({
    agentIds: ['需求澄清', '实现', '测试评审', '交付'],
    soloAgentId: 'Solo',
    workflowId: 'wf-1',
    published: true,
  })
  mocks.getWorkflow.mockImplementation(async (id: string) => ({ id, name: '默认工作流' }))
})
afterEach(() => {
  vi.unstubAllGlobals()
})
describe('OnboardingWizard', () => {
  it('shows credentials before model configuration in one outer onboarding dialog', async () => {
    const w = await mountWizard()
    expect(activeStep(w)).toBe('onboarding-rail-prefs')
    for (const id of ['prefs', 'credentials', 'model', 'gitCredentials', 'git', 'team', 'workflow', 'done'])
      expect(w.find(`[data-testid="onboarding-rail-${id}"]`).exists()).toBe(true)
    expect(w.find('[data-testid="onboarding-rail-key"]').exists()).toBe(false)
    expect(w.findAll('[role="dialog"]')).toHaveLength(1)
    expect(w.findAll('[aria-modal="true"]')).toHaveLength(1)
    expect(w.find('[data-testid="onboarding-backdrop"]').exists()).toBe(true)
    await next(w)
    expect(activeStep(w)).toBe('onboarding-rail-credentials')
    expect(w.getComponent(ResourcesPanel).props()).toMatchObject({ inline: true, scope: 'model', view: 'credentials' })
    expect(w.findComponent(ModelPicker).exists()).toBe(false)
    await next(w)
    expect(w.findComponent(ModelPicker).exists()).toBe(true)
    expect(w.find('[data-testid="onboarding-api-key"]').exists()).toBe(false)
    w.unmount()
  })
  it('requires a saved credential before model setup and selects a newly saved model', async () => {
    mocks.listModelConfigurations.mockResolvedValue({ items: [] })
    mocks.listCredentials.mockResolvedValue({ items: [] })
    const w = await mountWizard()
    await next(w)
    const credentialPanel = w.getComponent(ResourcesPanel)
    expect(credentialPanel.props()).toMatchObject({ inline: true, scope: 'model', view: 'credentials' })
    expect(w.findAll('[role="dialog"]')).toHaveLength(1)
    await next(w)
    expect(activeStep(w)).toBe('onboarding-rail-credentials')
    expect(w.findComponent(ModelPicker).exists()).toBe(false)
    mocks.listCredentials.mockResolvedValue({ items: [{ id: 'model-key', purpose: 'model_api', configured: true }] })
    w.getComponent(ResourcesPanel).vm.$emit('credentialSaved', { id: 'model-key', purpose: 'model_api', configured: true })
    await flushPromises()
    expect(activeStep(w)).toBe('onboarding-rail-model')
    const modelPanel = w.getComponent(ResourcesPanel)
    expect(modelPanel.props()).toMatchObject({ inline: true, scope: 'model', view: 'models', setup: true })
    mocks.listModelConfigurations.mockResolvedValue({ items: modelRows })
    modelPanel.vm.$emit('modelSaved', modelRows[0])
    await flushPromises()
    expect((w.vm as any).draft.modelConfigurationId).toBe('model-main')
    expect(activeStep(w)).toBe('onboarding-rail-gitCredentials')
    w.unmount()
  })
  it.each([
    { id: 'git-only', purpose: 'git', configured: true },
    { id: 'disabled-key', purpose: 'model_api', configured: true, enabled: false },
    { id: 'empty-key', purpose: 'tool_auth', configured: false },
  ])('does not advance credentials setup with unusable or unrelated $id', async (credential) => {
    mocks.listCredentials.mockResolvedValue({ items: [credential] })
    const w = await mountWizard()
    await next(w)
    await next(w)
    expect(activeStep(w)).toBe('onboarding-rail-credentials')
    expect(w.findComponent(ModelPicker).exists()).toBe(false)
    expect(mocks.bootstrapOnboarding).not.toHaveBeenCalled()
    w.unmount()
  })
  it('keeps the outer step in place while an inline form is being edited', async () => {
    mocks.listModelConfigurations.mockResolvedValue({ items: [] })
    const w = await mountWizard()
    await next(w)
    const panel = w.getComponent(ResourcesPanel)
    panel.vm.$emit('editing', true)
    await flushPromises()
    expect(w.get('[data-testid="onboarding-next"]').attributes('disabled')).toBeDefined()
    expect(w.get('[data-testid="onboarding-prev"]').attributes('disabled')).toBeDefined()
    w.getComponent(ResourcesPanel).vm.$emit('editing', false)
    await flushPromises()
    expect(w.get('[data-testid="onboarding-prev"]').attributes('disabled')).toBeUndefined()
    w.unmount()
  })
  it('adds Git credentials on their own step before the repository and author', async () => {
    const w = await mountWizard()
    await toGitCredentials(w)
    const panel = w.getComponent(ResourcesPanel)
    expect(panel.props()).toMatchObject({ inline: true, scope: 'git', view: 'credentials' })
    expect(w.findAll('[role="dialog"]')).toHaveLength(1)
    const credential = { id: 'new-git', purpose: 'git', provider: 'github', type: 'git', secretKind: 'token', configured: true, enabled: true }
    panel.vm.$emit('credentialSaved', credential)
    await flushPromises()
    expect(w.findComponent(GitPicker).props('modelValue')).toEqual({ items: [{ credentialId: 'new-git' }] })
    await next(w)
    expect(activeStep(w)).toBe('onboarding-rail-git')
    expect(w.findComponent(GitPicker).exists()).toBe(false)
    expect(w.findComponent(ResourcesPanel).exists()).toBe(false)
    await w.get('[data-testid="onboarding-prev"]').trigger('click')
    await flushPromises()
    expect(w.findComponent(GitPicker).props('modelValue')).toEqual({ items: [{ credentialId: 'new-git' }] })
    w.unmount()
  })
  it('blocks missing or invalid model configuration before Git', async () => {
    const w = await mountWizard()
    await next(w)
    await next(w)
    await next(w)
    expect(activeStep(w)).toBe('onboarding-rail-model')
    await chooseModel(w, 'model-main', false)
    await next(w)
    expect(activeStep(w)).toBe('onboarding-rail-model')
    await chooseModel(w)
    await next(w)
    expect(activeStep(w)).toBe('onboarding-rail-gitCredentials')
    w.unmount()
  })
  it('validates selected credentials and requires Git identity even when cloning is optional', async () => {
    const w = await mountWizard()
    await toGitCredentials(w)
    w.findComponent(GitPicker).vm.$emit('validity', false)
    await next(w)
    expect(activeStep(w)).toBe('onboarding-rail-gitCredentials')
    w.findComponent(GitPicker).vm.$emit('validity', true)
    await next(w)
    expect(activeStep(w)).toBe('onboarding-rail-git')
    await next(w)
    expect(activeStep(w)).toBe('onboarding-rail-git')
    await identity(w)
    await next(w)
    expect(activeStep(w)).toBe('onboarding-rail-team')
    w.unmount()
  })
  it.each(['configuration', 'credential', 'connection'])(
    'returns to model selection when the selected %s becomes unavailable in the resource manager',
    async (resource) => {
      const w = await mountWizard()
      await toGitCredentials(w)
      if (resource === 'configuration')
        mocks.listModelConfigurations.mockResolvedValue({
          items: modelRows.map((model) => ({ ...model, state: 'disabled' })),
        })
      if (resource === 'credential')
        mocks.listCredentials.mockResolvedValue({
          items: [{ id: 'model-key', purpose: 'model_api', configured: true, enabled: false }],
        })
      if (resource === 'connection') mocks.listModelConnections.mockResolvedValue({ items: [] })
      const vm = w.vm as any
      const refreshing = vm.refreshResources()
      expect(vm.modelValid).toBe(false)
      await refreshing
      await flushPromises()
      expect(activeStep(w)).toBe('onboarding-rail-model')
      expect(vm.draft.modelConfigurationId).toBe('model-main')
      expect(vm.modelValid).toBe(false)
      await next(w)
      expect(activeStep(w)).toBe('onboarding-rail-model')
      expect(mocks.bootstrapOnboarding).not.toHaveBeenCalled()
      w.unmount()
    },
  )
  it('keeps a healthy platform configuration and current Git inputs after resource management', async () => {
    mocks.listModelConfigurations.mockResolvedValue({
      items: [
        {
          ...modelRows[0],
          backend: 'cursor',
          binding: { kind: 'platform', credentialId: 'cursor-key' },
        },
      ],
    })
    mocks.listCredentials.mockResolvedValue({
      items: [{ id: 'cursor-key', purpose: 'tool_auth', toolId: 'cursor', configured: true }],
    })
    const w = await mountWizard()
    await toGit(w)
    await identity(w)
    expect(mocks.listCredentials).toHaveBeenCalledTimes(1)
    await w.get('[data-testid="onboarding-repo-url"]').setValue('https://github.com/team/repo.git')
    await w.get('[data-testid="onboarding-prev"]').trigger('click')
    await flushPromises()
    const vm = w.vm as any
    await vm.refreshResources()
    await flushPromises()
    expect(activeStep(w)).toBe('onboarding-rail-gitCredentials')
    expect(vm.modelValid).toBe(true)
    expect(vm.draft.modelConfigurationId).toBe('model-main')
    expect(vm.draft.repoUrl).toBe('https://github.com/team/repo.git')
    expect(vm.draft.gitUserName).toBe('Ada Lovelace')
    expect(mocks.listCredentials).toHaveBeenCalledTimes(2)
    expect(mocks.listModelConnections).toHaveBeenCalledTimes(2)
    expect(mocks.listToolCapabilities).toHaveBeenCalledTimes(2)
    await next(w)
    await next(w)
    await next(w)
    await next(w)
    expect(mocks.bootstrapOnboarding.mock.calls[0][0].modelConfigurationId).toBe('model-main')
    w.unmount()
  })
  it('blocks a previously valid model when resource refresh fails', async () => {
    const w = await mountWizard()
    await toGitCredentials(w)
    mocks.listCredentials.mockRejectedValueOnce(new Error('offline'))
    const vm = w.vm as any
    await vm.refreshResources()
    await flushPromises()
    expect(activeStep(w)).toBe('onboarding-rail-model')
    expect(vm.modelValid).toBe(false)
    await next(w)
    expect(activeStep(w)).toBe('onboarding-rail-model')
    expect(mocks.bootstrapOnboarding).not.toHaveBeenCalled()
    w.unmount()
  })
  it('submits configuration and multiple Git references without inline secrets', async () => {
    const w = await mountWizard()
    await toGitCredentials(w)
    const selected = {
      items: [{ credentialId: 'gh' }, { credentialId: 'gl', scope: 'git.example' }],
    }
    w.findComponent(GitPicker).vm.$emit('update:modelValue', selected)
    await next(w)
    await identity(w)
    await w.get('[data-testid="onboarding-repo-url"]').setValue('https://git.example/team/repo.git')
    await w.get('[data-testid="onboarding-repo-branch"]').setValue('main')
    await next(w)
    await next(w)
    await next(w)
    const body = mocks.bootstrapOnboarding.mock.calls[0][0]
    expect(body).toMatchObject({
      modelConfigurationId: 'model-main',
      gitBindings: selected,
      repoUrl: 'https://git.example/team/repo.git',
      repoBranch: 'main',
      gitUserName: 'Ada Lovelace',
      gitUserEmail: 'ada@example.com',
    })
    for (const field of [
      'apiKey',
      'githubToken',
      'gitlabToken',
      'acpBackend',
      'cliModel',
      'openCodeModel',
      'gitCredentialType',
    ])
      expect(body).not.toHaveProperty(field)
    expect(w.emitted('completed')).toHaveLength(1)
    w.unmount()
  })
  it('skipping Git sends an empty selection and drops the clone request', async () => {
    const w = await mountWizard()
    await toGitCredentials(w)
    w.findComponent(GitPicker).vm.$emit('update:modelValue', {
      items: [{ credentialId: 'gh' }],
    })
    await next(w)
    await identity(w)
    await w.get('[data-testid="onboarding-repo-url"]').setValue('https://github.com/team/repo.git')
    await w.get('[data-testid="onboarding-git-skip"]').trigger('click')
    await next(w)
    expect(activeStep(w)).toBe('onboarding-rail-team')
    await next(w)
    await next(w)
    const body = mocks.bootstrapOnboarding.mock.calls[0][0]
    expect(body.gitBindings).toEqual({ items: [] })
    expect(body).not.toHaveProperty('repoUrl')
    expect(body.gitUserName).toBe('Ada Lovelace')
    w.unmount()
  })
  it('keeps canonical names read-only and sends every role with its model', async () => {
    const w = await mountWizard()
    await toTeam(w)
    const name = w.get('[data-testid="onboarding-team-name-implement"]')
    expect(name.attributes('readonly')).toBeDefined()
    expect((name.element as HTMLInputElement).value).toBe('实现')
    expect(w.find('[data-testid="onboarding-team-toggle-test_review"]').exists()).toBe(false)
    await w.get('[data-testid="onboarding-team-model-implement"] [data-test="app-select-trigger"]').trigger('click')
    await w.get('[data-test="app-select-option-model-review"]').trigger('click')
    await next(w)
    expect(w.find('[data-testid="onboarding-preview-node-test_review"]').exists()).toBe(true)
    await next(w)
    expect(mocks.bootstrapOnboarding.mock.calls[0][0].agents).toEqual([
      { templateId: 'clarify', name: '需求澄清' },
      { templateId: 'implement', name: '实现', modelConfigurationId: 'model-review' },
      { templateId: 'test_review', name: '测试评审' },
      { templateId: 'deliver', name: '交付' },
    ])
    w.unmount()
  })
  it('creates an empty Solo separately from the four workflow roles', async () => {
    const w = await mountWizard()
    await toTeam(w)
    expect(w.get('[data-testid="onboarding-solo-agent"]').text()).toContain('Solo')
    expect(w.findAll('[data-testid^="onboarding-team-card-"]')).toHaveLength(4)
    await next(w)
    expect(w.find('[data-testid="onboarding-preview-node-Solo"]').exists()).toBe(false)
    expect(w.find('[data-testid="onboarding-preview-node-solo"]').exists()).toBe(false)
    await next(w)
    expect(mocks.bootstrapOnboarding.mock.calls[0][0].agents).toHaveLength(4)
    expect(mocks.bootstrapOnboarding.mock.calls[0][0].agents.some((agent: { name: string }) => agent.name === 'Solo')).toBe(false)
    const agents = w.get('[data-testid="onboarding-success-agents"]')
    expect(agents.text()).toContain('Solo')
    expect(agents.text()).toContain('pages.onboarding.solo.emptyShort')
    expect(agents.findAll('li')).toHaveLength(6)
    expect(w.emitted('completed')?.[0]?.[0]).toMatchObject({ soloAgentId: 'Solo', agentIds: ['需求澄清', '实现', '测试评审', '交付'] })
    w.unmount()
  })
  it('does not let a name edit leave the canonical team', async () => {
    const w = await mountWizard()
    await toTeam(w)
    const name = w.get('[data-testid="onboarding-team-name-test_review"]')
    expect(name.attributes('readonly')).toBeDefined()
    expect((name.element as HTMLInputElement).value).toBe('测试评审')
    await next(w)
    expect(activeStep(w)).toBe('onboarding-rail-workflow')
    expect(w.find('[data-testid="onboarding-team-name-error-test_review"]').exists()).toBe(false)
    w.unmount()
  })
  it('keeps the default team usable when template descriptions fail to load', async () => {
    mocks.listAgentTeamTemplates.mockRejectedValueOnce(new Error('offline'))
    const w = await mountWizard()
    await toTeam(w)
    expect(w.find('[data-testid="onboarding-team-load-error"]').exists()).toBe(true)
    for (const id of ['clarify', 'implement', 'test_review', 'deliver'])
      expect(w.find(`[data-testid="onboarding-team-card-${id}"]`).exists()).toBe(true)
    await next(w)
    expect(activeStep(w)).toBe('onboarding-rail-workflow')
    w.unmount()
  })
  it('surfaces bootstrap failures and does not emit completion', async () => {
    const w = await mountWizard()
    await toTeam(w)
    await next(w)
    mocks.bootstrapOnboarding.mockRejectedValueOnce(new Error('configuration disabled'))
    await next(w)
    expect(w.get('[data-testid="onboarding-create-error"]').text()).toContain('configuration disabled')
    expect(w.emitted('completed')).toBeFalsy()
    expect(w.find('[data-testid="onboarding-success"]').exists()).toBe(false)
    w.unmount()
  })
  it('opens the created workflow launcher and editor from success', async () => {
    const w = await mountWizard()
    await complete(w)
    await w.get('[data-testid="onboarding-run-once"]').trigger('click')
    await flushPromises()
    expect(mocks.getWorkflow).toHaveBeenCalledWith('wf-1')
    expect(mocks.openLaunch).toHaveBeenCalledWith({ id: 'wf-1', name: '默认工作流' })
    expect(w.emitted('close')).toHaveLength(1)
    await w.get('[data-testid="onboarding-edit-workflow"]').trigger('click')
    await flushPromises()
    expect(w.router.currentRoute.value.path).toBe('/workflows/wf-1/edit')
    w.unmount()
  })
  it('closing or later stops automatic re-opening in this browser', async () => {
    for (const selector of ['onboarding-later', 'onboarding-backdrop', 'onboarding-close']) {
      localStorage.clear()
      const w = await mountWizard()
      await w.get(`[data-testid="${selector}"]`).trigger('click')
      expect(w.emitted('close')).toHaveLength(1)
      expect(isOnboardingSuppressed()).toBe(true)
      expect(shouldAutoOpenOnboarding([], [])).toBe(false)
      w.unmount()
    }
  })
  it('closing from the success page does not write the dismissal flag', async () => {
    const w = await mountWizard()
    await complete(w)
    await w.get('[data-testid="onboarding-success-close"]').trigger('click')
    expect(w.emitted('close')).toHaveLength(1)
    expect(isOnboardingSuppressed()).toBe(false)
    w.unmount()
  })
  it('a blocked close does not write the dismissal flag', async () => {
    const w = await mountWizard()
    await toTeam(w)
    await next(w)
    mocks.bootstrapOnboarding.mockReturnValueOnce(new Promise(() => {}))
    await w.get('[data-testid="onboarding-next"]').trigger('click')
    await flushPromises()
    await w.get('[data-testid="onboarding-backdrop"]').trigger('click')
    expect(w.emitted('close')).toBeUndefined()
    expect(isOnboardingSuppressed()).toBe(false)
    w.unmount()
  })
  it('retry starts with preferences and keeps the selected model contract', async () => {
    const w = await mountWizard({ mode: 'retry' })
    expect(activeStep(w)).toBe('onboarding-rail-prefs')
    expect(w.get('[data-testid="onboarding-title"]').text()).toContain('titleRetry')
    await toTeam(w)
    await next(w)
    await next(w)
    expect(mocks.bootstrapOnboarding.mock.calls[0][0].modelConfigurationId).toBe('model-main')
    w.unmount()
  })
})
