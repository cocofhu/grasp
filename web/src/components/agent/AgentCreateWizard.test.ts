// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import commonZh from '@/locales/zh-CN/common.json'
import pagesZh from '@/locales/zh-CN/pages.json'
import commonEn from '@/locales/en/common.json'
import pagesEn from '@/locales/en/pages.json'
import AgentCreateWizard from './AgentCreateWizard.vue'
import { WIZARD_STEPS } from '@/lib/agent/agentCreateWizard'

const createAgent = vi.fn(async (payload: unknown) => payload)
const listCredentials = vi.fn(async () => ({ items: [] as Array<Record<string, unknown>> }))
const listModelConfigurations = vi.fn(async () => ({ items: [] as Array<Record<string, unknown>> }))
const listModelConnections = vi.fn(async () => ({ items: [] as Array<Record<string, unknown>> }))
const platformTools = ['cursor', 'claude_code', 'codebuddy', 'trae', 'codex']
const defaultCredentials = [
  ...platformTools.map((toolId) => ({ id: `key-${toolId}`, name: `${toolId}团队授权`, type: 'ai', purpose: 'tool_auth', toolId, configured: true })),
  { id: 'model-key', name: '模型服务密钥', type: 'ai', purpose: 'model_api', issuer: 'deepseek', configured: true },
]
const defaultConfigurations = [
  ...platformTools.map((backend) => ({ id: `cfg-${backend}`, name: `${backend}日常模型`, backend, state: 'active', revision: 1, binding: { kind: 'platform', credentialId: `key-${backend}`, modelId: `${backend}-main` } })),
  { id: 'cfg-opencode', name: 'OpenCode日常模型', backend: 'opencode', state: 'active', revision: 1, binding: { kind: 'connection', connectionId: 'service-opencode', modelId: 'vendor/main' } },
]
const defaultConnections = [{ id: 'service-opencode', name: 'OpenCode模型服务', providerId: 'deepseek', protocol: 'openai-chat-completions', baseUrl: 'https://models.example.test/v1', credentialId: 'model-key', auth: { scheme: 'bearer' }, models: [{ id: 'vendor/main', source: 'manual' }], state: 'active', revision: 1 }]
/** Concurrent noise: run-tags 404 must not break git help (g3/g4). */
const listRunTags = vi.fn(async () => {
  throw Object.assign(new Error('not found'), { status: 404 })
})
const getSharedAgentConfig = vi.fn(async () => ({
  env: {},
  files: [],
  mcp: [],
  layout: {},
}))
vi.mock('@/lib/api/api', () => ({
  api: {
    createAgent: (payload: unknown) => createAgent(payload),
    listModelConnections: () => listModelConnections(),
    listModelConfigurations: () => listModelConfigurations(),
    getModelDefaults: async () => ({ bindingsByBackend: {} }),
    listToolCapabilities: async () => ({ items: ['cursor','claude_code','codebuddy','trae','codex'].map((toolId) => ({ toolId, modes: ['platform'], protocols: [], adapterState: 'existing' })).concat([{ toolId: 'opencode', modes: ['connection'], protocols: ['openai-chat-completions'], adapterState: 'existing' }]) }),
    listAgentTeamTemplates: async () => ({
      items: [
        { id: 'test', embedName: 'TestAgent', roleLabelZh: '测试工程师', summary: '测试验证' },
        {
          id: 'preflight',
          embedName: 'PreflightAgent',
          roleLabelZh: '环境确认工程师',
          summary: '环境确认',
        },
        { id: 'implement', embedName: 'ImplementAgent', roleLabelZh: '实现工程师', summary: '实现' },
      ],
    }),
    listRunTags: () => listRunTags(),
    getSharedAgentConfig: () => getSharedAgentConfig(),
    openCodeProviders: async () => ({ providers: [] }),
    openCodeModels: async () => ({ models: [] }),
    listCredentials: () => listCredentials(),
  },
}))
vi.mock('@/lib/agent/cliCatalog', () => ({ loadCliModels: async () => [], cliCatalogFailed: () => false }))

function mountWizard(locale: 'zh-CN' | 'en' = 'zh-CN') {
  const i18n = createI18n({
    legacy: false,
    locale,
    messages: {
      'zh-CN': { ...commonZh, ...pagesZh },
      en: { ...commonEn, ...pagesEn },
    },
  })
  return mount(AgentCreateWizard, {
    attachTo: document.body,
    props: { open: true, existingNames: [] },
    global: { plugins: [i18n], stubs: { RouterLink: { template: '<a><slot/></a>' } } },
  })
}

function buttonByText(text: string) {
  const button = Array.from(document.body.querySelectorAll('button')).find(
    (item) => item.textContent?.trim().startsWith(text),
  )
  if (!button) throw new Error(`button not found: ${text}`)
  return button
}

function fillName(value: string) {
  const input = document.body.querySelector('#wiz-name-input') as HTMLInputElement
  input.value = value
  input.dispatchEvent(new Event('input', { bubbles: true }))
}

function railLabels(): string[] {
  return Array.from(document.body.querySelectorAll('.rail-item .lbl strong')).map(
    (el) => el.textContent?.trim() || '',
  )
}

async function openSelect(testId: string) {
  const trigger = document.body.querySelector<HTMLButtonElement>(`[data-testid="${testId}"] [data-test="app-select-trigger"]`)
  expect(trigger).toBeTruthy()
  trigger!.click()
  await flushPromises()
}

async function chooseOpenOption(value: string) {
  const option = Array.from(document.body.querySelectorAll<HTMLButtonElement>('[role="option"]'))
    .find((item) => item.dataset.test === `app-select-option-${value}`)
  expect(option).toBeTruthy()
  option!.click()
  await flushPromises()
}

async function chooseSelect(testId: string, value: string) {
  await openSelect(testId)
  await chooseOpenOption(value)
}

async function chooseModel(id = 'cfg-opencode') {
  await flushPromises()
  await chooseSelect('model-binding-configuration-select', id)
  expect(buttonByText('下一步').disabled).toBe(false)
}

async function clickTestId(testId: string) {
  const button = document.body.querySelector<HTMLButtonElement>(`[data-testid="${testId}"]`)
  expect(button).toBeTruthy()
  button!.click()
  await flushPromises()
}

beforeEach(() => {
  createAgent.mockClear()
  listCredentials.mockResolvedValue({ items: structuredClone(defaultCredentials) })
  listModelConfigurations.mockResolvedValue({ items: structuredClone(defaultConfigurations) })
  listModelConnections.mockResolvedValue({ items: structuredClone(defaultConnections) })
})
afterEach(() => { document.body.innerHTML = '' })

describe('AgentCreateWizard 4-step IA', () => {
  it('combines coding tool and model selection in one runtime step', () => {
    expect(WIZARD_STEPS.map((s) => s.id)).toEqual(['basics', 'acp', 'git', 'review'])
  })

  it('renders a combined tool-and-model sidebar item without ENV/ACP/capability steps', async () => {
    const wrapper = mountWizard()
    const labels = railLabels()
    expect(labels).toEqual(['基础信息', '工具与模型', 'Git', '确认创建'])
    expect(labels.join(' ')).not.toContain('ACP')
    expect(labels.join(' ')).not.toContain('ENV')
    expect(labels.join(' ')).not.toMatch(/MCP|Rules|Skills|Commands|Prompts/)
    wrapper.unmount()
  })

  it('leaves basics once a valid name is entered', async () => {
    const wrapper = mountWizard()
    await flushPromises()
    fillName('workspace-agent')
    await wrapper.vm.$nextTick()
    buttonByText('下一步').click()
    await wrapper.vm.$nextTick()
    expect(document.body.querySelector('#wiz-name-input')).toBeNull()
    expect(document.body.querySelector('[data-testid="agent-tool-model-step"]')).toBeTruthy()
    expect(document.body.querySelector('[data-testid="coding-tool-picker"]')).toBeTruthy()
    expect(document.body.querySelector('[data-testid="model-binding-editor"]')).toBeTruthy()
    wrapper.unmount()
  })

  it('defaults CodeBuddy to international and saves a site selected beside its model settings', async () => {
    const wrapper = mountWizard()
    fillName('region-agent')
    await wrapper.vm.$nextTick()
    buttonByText('下一步').click()
    await wrapper.vm.$nextTick()

    buttonByText('CodeBuddy').click()
    await wrapper.vm.$nextTick()
    expect(document.body.querySelector('[data-testid="agent-wizard-backend-codebuddy"]')?.getAttribute('aria-checked')).toBe('true')
    expect(document.body.textContent).toContain('国际站')
    expect(document.body.textContent).toContain('Agent')
    expect(document.body.textContent).not.toMatch(/配置步骤[\s\S]*\bACP\b/)
    expect(document.body.querySelector('[data-testid="agent-wizard-backend-opencode"]')).toBeTruthy()
    ;(document.body.querySelector('[data-testid="agent-wizard-region-internal"]') as HTMLButtonElement).click()
    await flushPromises()
    expect(document.body.querySelector('[data-testid="agent-wizard-region-internal"]')?.getAttribute('aria-checked')).toBe('true')
    await chooseModel('cfg-codebuddy')
    buttonByText('下一步').click()
    await wrapper.vm.$nextTick()
    buttonByText('跳过').click()
    await wrapper.vm.$nextTick()
    buttonByText('创建并进入 Studio').click()
    await vi.waitFor(() => expect(createAgent).toHaveBeenCalled())
    expect(createAgent.mock.calls.at(-1)?.[0]).toMatchObject({ acpBackend: 'codebuddy', env: { GRASP_CODEBUDDY_REGION: 'internal' } })
    wrapper.unmount()
  })

  it('requires a model choice and offers no runtime-step skip or direct credential controls', async () => {
    const wrapper = mountWizard()
    fillName('key-agent')
    await wrapper.vm.$nextTick()
    buttonByText('下一步').click()
    await wrapper.vm.$nextTick()
    buttonByText('Cursor').click()
    await flushPromises()
    expect(document.body.querySelector('[data-testid="model-binding-configuration-select"]')).toBeTruthy()
    expect(document.body.textContent).not.toContain('如何获取')
    expect(document.body.querySelector('[data-test="api-key-input"]')).toBeNull()
    expect(document.body.textContent).not.toContain('CURSOR_API_KEY')
    expect(document.body.querySelector('[data-testid="model-binding-advanced"]')).toBeNull()
    expect(document.body.querySelector('[data-testid="model-binding-platform"]')).toBeNull()
    expect(document.body.querySelector('[data-testid="model-binding-inherit"]')).toBeNull()
    expect(Array.from(document.body.querySelectorAll('button')).some((button) => button.textContent?.trim() === '跳过')).toBe(false)
    expect(buttonByText('下一步').disabled).toBe(true)
    buttonByText('下一步').click()
    await flushPromises()
    expect(document.body.querySelector('[data-testid="agent-tool-model-step"]')).toBeTruthy()
    expect(document.body.querySelector('[data-test="git-help-link"]')).toBeNull()
    expect(createAgent).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('keeps OpenCode as the default and submits only an explicitly selected model reference', async () => {
    const wrapper = mountWizard()
    fillName('default-opencode')
    await wrapper.vm.$nextTick()
    buttonByText('下一步').click()
    await wrapper.vm.$nextTick()
    expect(document.body.querySelector('[data-testid="agent-wizard-backend-opencode"]')).toBeTruthy()
    expect(document.body.querySelector('[data-testid="agent-wizard-backend-opencode"]')?.getAttribute('aria-checked')).toBe('true')
    expect(document.body.textContent).not.toContain('/root/.cursor')
    await chooseModel()
    buttonByText('下一步').click()
    await wrapper.vm.$nextTick()
    buttonByText('跳过').click()
    await wrapper.vm.$nextTick()
    buttonByText('创建并进入 Studio').click()
    await wrapper.vm.$nextTick()
    await vi.waitFor(() => {
      expect(createAgent).toHaveBeenCalled()
    })
    const payload = createAgent.mock.calls[0][0]
    expect(payload.acpBackend).toBe('opencode')
    expect(payload.layout?.configRoot).toBe('/root/.config/opencode')
    expect(payload).toHaveProperty('modelBinding', { kind: 'configuration', configurationId: 'cfg-opencode' })
    expect(payload).not.toHaveProperty('aiCredentialId')
    expect(payload).not.toHaveProperty('openCodeCredentialId')
    expect(payload).not.toHaveProperty('gitCredentialId')
    wrapper.unmount()
  })

  it('reviews an explicit model without inheritance or a false missing-key reminder', async () => {
    const wrapper = mountWizard()
    fillName('skip-agent')
    await wrapper.vm.$nextTick()
    buttonByText('下一步').click()
    await wrapper.vm.$nextTick()
    await chooseModel()
    buttonByText('下一步').click()
    await wrapper.vm.$nextTick()
    buttonByText('跳过').click()
    await wrapper.vm.$nextTick()

    expect(document.body.textContent).toContain('确认创建')
    expect(document.body.textContent).not.toContain('鉴权提醒')
    expect(document.body.textContent).toContain('OpenCode日常模型')
    expect(document.body.textContent).not.toContain('继承')
    expect(railLabels()).not.toContain('ENV')
    expect(railLabels()).not.toContain('MCP')
    expect(railLabels()).toContain('工具与模型')
    expect(railLabels()).not.toContain('ACP')

    buttonByText('创建并进入 Studio').click()
    await wrapper.vm.$nextTick()
    await vi.waitFor(() => {
      expect(createAgent).toHaveBeenCalled()
    })
    wrapper.unmount()
  })

  it('Git step help stacks on wizard and does not close or reset it', async () => {
    const wrapper = mountWizard()
    fillName('help-agent')
    await wrapper.vm.$nextTick()
    buttonByText('下一步').click()
    await wrapper.vm.$nextTick()
    await chooseModel()
    buttonByText('下一步').click()
    await wrapper.vm.$nextTick()

    expect(document.body.textContent).toContain('Git')
    expect(document.body.textContent).not.toContain('不会验证变量引用的实际值')
    const helpLink = document.body.querySelector('[data-test="git-help-link"]') as HTMLButtonElement
    expect(helpLink?.textContent?.trim()).toBe('Git 配置帮助')
    helpLink.click()
    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick()

    expect(document.body.textContent).toContain('环境变量与凭据')
    expect(document.body.textContent).toContain('不会验证变量引用的实际值')
    expect(document.body.querySelectorAll('[data-test="env-credential-help"]')).toHaveLength(1)
    expect(document.body.querySelector('.wiz-root')).toBeTruthy()
    expect(railLabels()).toEqual(['基础信息', '工具与模型', 'Git', '确认创建'])

    const gotIt = document.body.querySelector('[data-test="env-help-got-it"]') as HTMLButtonElement
    gotIt.click()
    await wrapper.vm.$nextTick()

    expect(document.body.querySelector('[data-test="env-credential-help"]')).toBeFalsy()
    expect(document.body.querySelector('.wiz-root')).toBeTruthy()
    expect(document.body.textContent).toContain('Git')
    expect(railLabels()).toEqual(['基础信息', '工具与模型', 'Git', '确认创建'])
    wrapper.unmount()
  })

  it('opens Git credential help even when listRunTags would 404', async () => {
    listRunTags.mockRejectedValue(new Error('not found'))
    const unhandled: unknown[] = []
    const onUnhandled = (reason: unknown) => {
      unhandled.push(reason)
    }
    process.on('unhandledRejection', onUnhandled)

    const wrapper = mountWizard()
    fillName('help-under-404')
    await wrapper.vm.$nextTick()
    buttonByText('下一步').click()
    await wrapper.vm.$nextTick()
    await chooseModel()
    buttonByText('下一步').click()
    await wrapper.vm.$nextTick()

    // Simulate concurrent TagFilter-style call while user opens help
    void listRunTags().catch(() => {})
    const helpLink = document.body.querySelector('[data-test="git-help-link"]') as HTMLButtonElement
    helpLink.click()
    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick()

    expect(document.body.textContent).toContain('环境变量与凭据')
    expect(document.body.querySelector('[data-test="env-credential-help"]')).toBeTruthy()
    expect(unhandled.some((e) => String(e).includes('is not iterable'))).toBe(false)

    process.off('unhandledRejection', onUnhandled)
    wrapper.unmount()
  })

  it('renders equivalent English site semantics and a combined step label', async () => {
    const wrapper = mountWizard('en')
    expect(railLabels()).toEqual(['Basics', 'Tool & model', 'Git', 'Confirm'])
    fillName('region-agent')
    await wrapper.vm.$nextTick()
    buttonByText('Next').click()
    await wrapper.vm.$nextTick()
    buttonByText('Trae').click()
    await wrapper.vm.$nextTick()

    expect(document.body.querySelector('[data-testid="agent-wizard-backend-trae"]')?.getAttribute('aria-checked')).toBe('true')
    expect(document.body.textContent).toContain('International')
    wrapper.unmount()
  })

  // plan g3.1 — search test; name unchanged; posts templateId
  it('template dropdown: pick test keeps name qa-1 and posts templateId', async () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: {
        'zh-CN': { ...commonZh, ...pagesZh },
        en: { ...commonEn, ...pagesEn },
      },
    })
    const wrapper = mount(AgentCreateWizard, {
      attachTo: document.body,
      props: { open: true, existingNames: [] },
      global: { plugins: [i18n], stubs: { Teleport: false, RouterLink: { template: '<a><slot/></a>' } } },
    })
    await flushPromises()
    fillName('qa-1')
    await wrapper.vm.$nextTick()
    expect((document.body.querySelector('#wiz-name-input') as HTMLInputElement).value).toBe('qa-1')

    const trigger = document.body.querySelector(
      '[data-testid="agent-template-select-trigger"]',
    ) as HTMLButtonElement
    trigger.click()
    await flushPromises()
    const search = document.body.querySelector(
      '[data-testid="agent-template-select-search"]',
    ) as HTMLInputElement
    search.value = '测试'
    search.dispatchEvent(new Event('input', { bubbles: true }))
    await flushPromises()
    const testOpt = document.body.querySelector(
      '[data-testid="agent-template-select-option-test"]',
    ) as HTMLElement
    expect(testOpt).toBeTruthy()
    testOpt.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, cancelable: true }))
    await flushPromises()

    expect((document.body.querySelector('#wiz-name-input') as HTMLInputElement).value).toBe('qa-1')
    expect(document.body.textContent).not.toContain('职责 / 用途简述')

    buttonByText('下一步').click()
    await wrapper.vm.$nextTick()
    await chooseModel()
    buttonByText('下一步').click()
    await wrapper.vm.$nextTick()
    buttonByText('下一步').click()
    await wrapper.vm.$nextTick()
    buttonByText('创建并进入 Studio').click()
    await vi.waitFor(() => {
      expect(createAgent).toHaveBeenCalled()
    })
    const payload = createAgent.mock.calls.at(-1)?.[0] as { name: string; templateId?: string }
    expect(payload.name).toBe('qa-1')
    expect(payload.templateId).toBe('test')
    wrapper.unmount()
  })

  it('selects a saved model and Git credentials, then posts only their references without secrets', async () => {
    listCredentials.mockResolvedValue({
      items: [
        {
          id: 'c1',
          name: '团队共享',
          type: 'ai',
          purpose: 'tool_auth',
          toolId: 'cursor',
          provider: 'cursor',
          envKey: 'GRASP_CURSOR_API_KEY',
          configured: true,
        },
        {
          id: 'revoked',
          name: '已撤销',
          type: 'ai',
          provider: 'cursor',
          envKey: 'GRASP_CURSOR_API_KEY',
          configured: true,
          revokedAt: '2026-01-01T00:00:00Z',
        },
        {
          id: 'g1',
          name: 'org-read',
          type: 'git',
          provider: 'github',
          envKey: 'GITHUB_TOKEN',
          configured: true,
        },
        {
          id: 'gl1',
          name: 'gl-read',
          type: 'git',
          provider: 'gitlab',
          envKey: 'GITLAB_TOKEN',
          configured: true,
        },
        {
          id: 's1',
          name: '部署私钥',
          type: 'ssh',
          provider: 'ssh',
          envKey: 'GIT_SSH_PRIVATE_KEY',
          configured: true,
        },
        {
          id: 'h1',
          name: '已知主机',
          type: 'ssh',
          provider: 'ssh',
          envKey: 'GIT_SSH_KNOWN_HOSTS',
          configured: true,
        },
      ],
    })
    listModelConfigurations.mockResolvedValue({ items: [{ id: 'cfg-creds', name: '团队模型', backend: 'cursor', state: 'active', revision: 1, binding: { kind: 'platform', credentialId: 'c1', modelId: 'composer-1' } }] })
    createAgent.mockClear()
    const wrapper = mountWizard()
    await flushPromises()
    fillName('cred-agent')
    await wrapper.vm.$nextTick()
    buttonByText('下一步').click()
    await wrapper.vm.$nextTick()
    buttonByText('Cursor').click()
    await flushPromises()

    await chooseModel('cfg-creds')
    expect(document.body.querySelector('[data-testid="model-binding-advanced"]')).toBeNull()
    expect(document.body.querySelector('[data-testid="model-binding-credential"]')).toBeNull()
    expect(document.body.querySelector('[data-test="api-key-input"]')).toBeNull()
    buttonByText('下一步').click()
    await wrapper.vm.$nextTick()

    await chooseSelect('git-credential-add', 'g1')
    await chooseSelect('git-credential-add', 'gl1')
    await chooseSelect('git-credential-add', 's1')
    expect(document.body.querySelectorAll('[data-testid^="git-credential-row-"]')).toHaveLength(3)
    expect(document.body.textContent).not.toContain('GIT_REPOS')
    expect(document.body.querySelector('[data-test="git-choice-github_https"]')).toBeNull()
    ;(document.body.querySelector('[data-testid="git-preferred-s1"]') as HTMLButtonElement).click()
    await flushPromises()
    await chooseSelect('git-hosts-s1', 'h1')
    const scope = document.body.querySelector<HTMLInputElement>('[data-testid="git-scope-s1"]')!
    scope.value = 'git.example.test'; scope.dispatchEvent(new Event('input', { bubbles: true }))
    await flushPromises()
    buttonByText('下一步').click()
    await wrapper.vm.$nextTick()

    expect(document.body.textContent).toContain('团队模型')
    expect(document.body.textContent).toContain('部署私钥 (首选)')
    expect(document.body.textContent).not.toContain('API Key · 未配置')
    buttonByText('创建并进入 Studio').click()
    await vi.waitFor(() => expect(createAgent).toHaveBeenCalled())
    const payload = createAgent.mock.calls.at(-1)?.[0] as Record<string, any>
    expect(payload).not.toHaveProperty('aiCredentialId')
    expect(payload).toHaveProperty('modelBinding', { kind: 'configuration', configurationId: 'cfg-creds' })
    expect(payload).not.toHaveProperty('openCodeCredentialId')
    expect(payload.gitBindings).toEqual({ items: [{ credentialId: 'g1' }, { credentialId: 'gl1' }, { credentialId: 's1', scope: 'git.example.test', sshHostsCredentialId: 'h1' }], preferredCredentialId: 's1' })
    expect(payload).not.toHaveProperty('gitCredentialType')
    expect(payload).not.toHaveProperty('gitCredentialId')
    expect(payload).not.toHaveProperty('sshHostsCredentialId')
    expect(payload.env).not.toHaveProperty('GITHUB_TOKEN')
    expect(payload.env).not.toHaveProperty('GITLAB_TOKEN')
    expect(payload.env).not.toHaveProperty('GIT_SSH_PRIVATE_KEY')
    expect(payload.env).not.toHaveProperty('GIT_SSH_KNOWN_HOSTS')
    expect(payload.env).not.toHaveProperty('GRASP_CURSOR_API_KEY')
    listCredentials.mockResolvedValue({ items: [] })
    wrapper.unmount()
  })
})


describe('AgentCreateWizard explicit Git selection', () => {
  it('keeps an invalid scope in the draft, blocks Next, and Skip clears the full selected list', async () => {
    listCredentials.mockResolvedValue({ items: [...defaultCredentials, { id: 'gh-1', name: '团队 GitHub', type: 'git', provider: 'github', envKey: 'GITHUB_TOKEN', configured: true }] })
    const wrapper = mountWizard()
    await flushPromises()
    fillName('skip-git-agent')
    buttonByText('下一步').click()
    await flushPromises()
    await chooseModel()
    buttonByText('下一步').click()
    await flushPromises()
    await chooseSelect('git-credential-add', 'gh-1')
    const scope = document.body.querySelector<HTMLInputElement>('[data-testid="git-scope-gh-1"]')!
    scope.value = 'ssh://git@github.com/org/repo'; scope.dispatchEvent(new Event('input', { bubbles: true }))
    await flushPromises()
    expect(buttonByText('下一步').disabled).toBe(true)
    buttonByText('上一步').click()
    await flushPromises()
    buttonByText('下一步').click()
    await flushPromises()
    expect(document.body.querySelector<HTMLInputElement>('[data-testid="git-scope-gh-1"]')!.value).toBe('ssh://git@github.com/org/repo')
    buttonByText('跳过').click()
    await flushPromises()
    expect(buttonByText('创建并进入 Studio').disabled).toBe(false)
    buttonByText('创建并进入 Studio').click()
    await vi.waitFor(() => expect(createAgent).toHaveBeenCalled())
    expect(createAgent.mock.calls.at(-1)![0]).toMatchObject({ gitBindings: { items: [] }, repoIds: [] })
    expect(createAgent.mock.calls.at(-1)![0]).not.toHaveProperty('gitCredentialId')
    wrapper.unmount()
  })
})

describe('AgentCreateWizard reusable model configurations', () => {
  it('keeps the combined-page draft when going back, reviews it and stores only its reference', async () => {
    listCredentials.mockResolvedValue({ items: [{ id: 'tool-key', name: '工作账号', purpose: 'tool_auth', toolId: 'cursor', configured: true }] })
    listModelConfigurations.mockResolvedValue({ items: [{ id: 'cfg-prod', name: '日常开发', backend: 'cursor', state: 'active', revision: 1, binding: { kind: 'platform', credentialId: 'tool-key', modelId: 'composer-1' } }] })
    const wrapper = mountWizard()
    await flushPromises()
    fillName('configured-agent')
    await wrapper.vm.$nextTick()
    buttonByText('下一步').click()
    await wrapper.vm.$nextTick()
    ;(document.body.querySelector('[data-testid="agent-wizard-backend-cursor"]') as HTMLButtonElement).click()
    await flushPromises()
    expect(document.body.querySelector('[data-testid="model-binding-configuration"]')).toBeNull()
    await chooseSelect('model-binding-configuration-select', 'cfg-prod')
    buttonByText('下一步').click()
    await wrapper.vm.$nextTick()
    expect(document.body.querySelector('[data-test="git-help-link"]')).toBeTruthy()
    buttonByText('上一步').click()
    await flushPromises()
    expect(document.body.querySelector('[data-testid="agent-wizard-backend-cursor"]')?.getAttribute('aria-checked')).toBe('true')
    expect(document.body.querySelector('[data-testid="model-binding-configuration-select"] [data-test="app-select-trigger"]')?.textContent).toContain('日常开发')
    buttonByText('下一步').click()
    await wrapper.vm.$nextTick()
    buttonByText('跳过').click()
    await wrapper.vm.$nextTick()
    expect(document.body.textContent).toContain('日常开发 · Cursor · composer-1')
    expect(document.body.textContent).not.toContain('鉴权提醒')
    buttonByText('创建并进入 Studio').click()
    await vi.waitFor(() => expect(createAgent).toHaveBeenCalled())
    expect(createAgent.mock.calls.at(-1)?.[0]).toMatchObject({ acpBackend: 'cursor', modelBinding: { kind: 'configuration', configurationId: 'cfg-prod' } })
    expect(createAgent.mock.calls.at(-1)?.[0]).not.toHaveProperty('aiCredentialId')
    wrapper.unmount()
  })

  it('filters models immediately on the same page and clears a previous tool source when switching tools', async () => {
    listCredentials.mockResolvedValue({ items: [
      { id: 'cursor-key', name: 'Cursor授权', purpose: 'tool_auth', toolId: 'cursor', configured: true },
      { id: 'claude-key', name: 'Claude授权', purpose: 'tool_auth', toolId: 'claude_code', configured: true },
    ] })
    listModelConfigurations.mockResolvedValue({ items: [
      { id: 'cfg-cursor', name: 'Cursor日常', backend: 'cursor', state: 'active', revision: 1, binding: { kind: 'platform', credentialId: 'cursor-key', modelId: 'composer-1' } },
      { id: 'cfg-claude', name: 'Claude日常', backend: 'claude_code', state: 'active', revision: 1, binding: { kind: 'platform', credentialId: 'claude-key', modelId: 'claude-main' } },
      { id: 'cfg-disabled', name: '停用配置', backend: 'cursor', state: 'disabled', revision: 1, binding: { kind: 'platform', credentialId: 'cursor-key' } },
    ] })
    const wrapper = mountWizard()
    await flushPromises()
    fillName('filtered-agent')
    await wrapper.vm.$nextTick()
    buttonByText('下一步').click()
    await flushPromises()
    await clickTestId('agent-wizard-backend-cursor')
    await openSelect('model-binding-configuration-select')
    expect(document.body.querySelector('[data-test="app-select-option-cfg-cursor"]')).toBeTruthy()
    expect(document.body.querySelector('[data-test="app-select-option-cfg-claude"]')).toBeNull()
    expect(document.body.querySelector('[data-test="app-select-option-cfg-disabled"]')).toBeNull()
    await chooseOpenOption('cfg-cursor')

    await clickTestId('agent-wizard-backend-claude_code')
    expect(document.body.querySelector('[data-testid="model-binding-inherit"]')).toBeNull()
    expect(document.body.querySelector('[data-testid="model-binding-configuration-select"] [data-test="app-select-trigger"]')?.textContent).toContain('选择模型配置')
    expect(buttonByText('下一步').disabled).toBe(true)
    await openSelect('model-binding-configuration-select')
    expect(document.body.querySelector('[data-test="app-select-option-__inherit"]')).toBeNull()
    expect(document.body.querySelector('[data-test="app-select-option-cfg-claude"]')).toBeTruthy()
    expect(document.body.querySelector('[data-test="app-select-option-cfg-cursor"]')).toBeNull()
    await chooseOpenOption('cfg-claude')
    buttonByText('下一步').click()
    await wrapper.vm.$nextTick()
    buttonByText('跳过').click()
    await wrapper.vm.$nextTick()
    buttonByText('创建并进入 Studio').click()
    await vi.waitFor(() => expect(createAgent).toHaveBeenCalled())
    expect(createAgent.mock.calls.at(-1)?.[0]).toMatchObject({ acpBackend: 'claude_code', modelBinding: { kind: 'configuration', configurationId: 'cfg-claude' } })
    expect(JSON.stringify(createAgent.mock.calls.at(-1)?.[0])).not.toContain('cfg-cursor')
    wrapper.unmount()
  })

  it('blocks a selected model whose credential was revoked while the user was on the Git step', async () => {
    const wrapper = mountWizard()
    await flushPromises()
    fillName('revoked-agent')
    await wrapper.vm.$nextTick()
    buttonByText('下一步').click()
    await flushPromises()
    await clickTestId('agent-wizard-backend-cursor')
    await chooseModel('cfg-cursor')
    buttonByText('下一步').click()
    await wrapper.vm.$nextTick()
    expect(document.body.querySelector('[data-test="git-help-link"]')).toBeTruthy()
    listCredentials.mockResolvedValue({ items: defaultCredentials.map((key) => key.id === 'key-cursor' ? { ...key, revokedAt: '2026-10-24T00:00:00Z' } : key) })
    buttonByText('上一步').click()
    await flushPromises()
    expect(document.body.querySelector('[data-testid="model-binding-configuration-select"] [data-test="app-select-trigger"]')?.textContent).toContain('cursor日常模型')
    expect(buttonByText('下一步').disabled).toBe(true)
    buttonByText('下一步').click()
    await flushPromises()
    expect(document.body.querySelector('[data-testid="agent-tool-model-step"]')).toBeTruthy()
    expect(document.body.querySelector('[data-test="git-help-link"]')).toBeNull()
    expect(document.body.querySelector('[data-testid="model-binding-inherit"]')).toBeNull()
    expect(createAgent).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
