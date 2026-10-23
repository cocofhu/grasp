// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import commonZh from '@/locales/zh-CN/common.json'
import pagesZh from '@/locales/zh-CN/pages.json'
import commonEn from '@/locales/en/common.json'
import pagesEn from '@/locales/en/pages.json'
import AgentCreateWizard from './AgentCreateWizard.vue'
import { WIZARD_STEPS } from '@/lib/agent/agentCreateWizard'

const createAgent = vi.fn(async (payload: unknown) => payload)
const listCredentials = vi.fn(async () => ({ items: [] as Array<Record<string, unknown>> }))
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
    global: { plugins: [i18n] },
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

afterEach(() => {
  document.body.innerHTML = ''
  listCredentials.mockResolvedValue({ items: [] })
})

describe('AgentCreateWizard 5-step IA', () => {
  it('exposes exactly 5 wizard steps in model order', () => {
    expect(WIZARD_STEPS.map((s) => s.id)).toEqual(['basics', 'acp', 'apiKey', 'git', 'review'])
  })

  it('renders sidebar without ENV/ACP/capability steps and labels Agent', async () => {
    const wrapper = mountWizard()
    const labels = railLabels()
    expect(labels).toEqual(['基础信息', 'Agent', 'API Key', 'Git', '确认创建'])
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
    wrapper.unmount()
  })

  it('defaults CodeBuddy to international on the Agent step', async () => {
    const wrapper = mountWizard()
    fillName('region-agent')
    await wrapper.vm.$nextTick()
    buttonByText('下一步').click()
    await wrapper.vm.$nextTick()

    buttonByText('用编码工具').click()
    await wrapper.vm.$nextTick()
    buttonByText('CodeBuddy').click()
    await wrapper.vm.$nextTick()
    const international = document.body.querySelector(
      'button[role="radio"][aria-label="国际站 (public)"]',
    )
    expect(international?.getAttribute('aria-checked')).toBe('true')
    expect(document.body.textContent).toContain('Agent')
    expect(document.body.textContent).not.toMatch(/配置步骤[\s\S]*\bACP\b/)
    expect(document.body.textContent).toContain('用模型厂商的 API Key')
    wrapper.unmount()
  })

  it('shows API Key apply guide for current backend and allows skip', async () => {
    const wrapper = mountWizard()
    fillName('key-agent')
    await wrapper.vm.$nextTick()
    buttonByText('下一步').click()
    await wrapper.vm.$nextTick()
    buttonByText('用编码工具').click()
    await wrapper.vm.$nextTick()
    buttonByText('Cursor').click()
    await wrapper.vm.$nextTick()
    buttonByText('下一步').click()
    await wrapper.vm.$nextTick()

    expect(document.body.textContent).toContain('请到工作区凭据保存')
    expect(document.body.textContent).not.toContain('如何获取')
    expect(document.body.querySelector('[data-test="api-key-input"]')).toBeNull()
    expect(document.body.textContent).not.toContain('CURSOR_API_KEY')

    buttonByText('跳过').click()
    await wrapper.vm.$nextTick()
    expect(document.body.textContent).toContain('Git')
    wrapper.unmount()
  })

  it('defaults Agent step to API Key path and creates OpenCode when skipped', async () => {
    const wrapper = mountWizard()
    fillName('default-opencode')
    await wrapper.vm.$nextTick()
    buttonByText('下一步').click()
    await wrapper.vm.$nextTick()
    expect(document.body.textContent).toContain('用模型厂商的 API Key')
    expect(document.body.querySelector('[data-testid="agent-wizard-path-apiKey"]')).toBeTruthy()
    expect(document.body.querySelector('[data-testid="agent-wizard-path-apikey-detail"]')).toBeTruthy()
    expect(document.body.textContent).not.toContain('/root/.cursor')
    // Skip Agent step — payload stays OpenCode
    buttonByText('跳过').click()
    await wrapper.vm.$nextTick()
    buttonByText('跳过').click()
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
    expect(payload).not.toHaveProperty('aiCredentialId')
    expect(payload).not.toHaveProperty('openCodeCredentialId')
    expect(payload).not.toHaveProperty('gitCredentialId')
    wrapper.unmount()
  })

  it('review page shows non-blocking auth reminder when API Key skipped', async () => {
    const wrapper = mountWizard()
    fillName('skip-agent')
    await wrapper.vm.$nextTick()
    buttonByText('下一步').click()
    await wrapper.vm.$nextTick()
    buttonByText('下一步').click()
    await wrapper.vm.$nextTick()
    buttonByText('跳过').click()
    await wrapper.vm.$nextTick()
    buttonByText('跳过').click()
    await wrapper.vm.$nextTick()

    expect(document.body.textContent).toContain('确认创建')
    expect(document.body.textContent).toContain('Studio Env')
    expect(document.body.textContent).toContain('鉴权提醒')
    expect(railLabels()).not.toContain('ENV')
    expect(railLabels()).not.toContain('MCP')
    expect(railLabels()).toContain('Agent')
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
    buttonByText('下一步').click()
    await wrapper.vm.$nextTick()
    buttonByText('跳过').click()
    await wrapper.vm.$nextTick()

    expect(document.body.textContent).toContain('Git')
    expect(document.body.textContent).not.toContain('不会验证变量引用的实际值')
    const helpLink = document.body.querySelector('[data-test="git-help-link"]') as HTMLButtonElement
    expect(helpLink?.textContent?.trim()).toBe('帮助')
    helpLink.click()
    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick()

    expect(document.body.textContent).toContain('环境变量与凭据')
    expect(document.body.textContent).toContain('不会验证变量引用的实际值')
    expect(document.body.querySelectorAll('[data-test="env-credential-help"]')).toHaveLength(1)
    expect(document.body.querySelector('.wiz-root')).toBeTruthy()
    expect(railLabels()).toEqual(['基础信息', 'Agent', 'API Key', 'Git', '确认创建'])

    const gotIt = document.body.querySelector('[data-test="env-help-got-it"]') as HTMLButtonElement
    gotIt.click()
    await wrapper.vm.$nextTick()

    expect(document.body.querySelector('[data-test="env-credential-help"]')).toBeFalsy()
    expect(document.body.querySelector('.wiz-root')).toBeTruthy()
    expect(document.body.textContent).toContain('Git')
    expect(railLabels()).toEqual(['基础信息', 'Agent', 'API Key', 'Git', '确认创建'])
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
    buttonByText('下一步').click()
    await wrapper.vm.$nextTick()
    buttonByText('跳过').click()
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

  it('renders equivalent English site semantics and Agent step label', async () => {
    const wrapper = mountWizard('en')
    expect(railLabels()).toEqual(['Basics', 'Agent', 'API Key', 'Git', 'Confirm'])
    fillName('region-agent')
    await wrapper.vm.$nextTick()
    buttonByText('Next').click()
    await wrapper.vm.$nextTick()
    buttonByText('Use a coding tool').click()
    await wrapper.vm.$nextTick()
    buttonByText('Trae').click()
    await wrapper.vm.$nextTick()

    const international = document.body.querySelector(
      'button[role="radio"][aria-label="International (intl)"]',
    )
    expect(international?.getAttribute('aria-checked')).toBe('true')
    expect(document.body.textContent).toContain('www.trae.ai · intl')
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
      global: { plugins: [i18n], stubs: { Teleport: false } },
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
    buttonByText('下一步').click()
    await wrapper.vm.$nextTick()
    buttonByText('跳过').click()
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

  it('selects saved credentials, shows GIT_REPOS on the Git step, and posts ids without secrets', async () => {
    listCredentials.mockResolvedValue({
      items: [
        {
          id: 'c1',
          name: '工作号',
          type: 'ai',
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
    createAgent.mockClear()
    const wrapper = mountWizard()
    await flushPromises()
    fillName('cred-agent')
    await wrapper.vm.$nextTick()
    buttonByText('下一步').click()
    await wrapper.vm.$nextTick()
    buttonByText('用编码工具').click()
    await wrapper.vm.$nextTick()
    buttonByText('Cursor').click()
    await wrapper.vm.$nextTick()
    buttonByText('下一步').click()
    await flushPromises()

    expect(document.body.textContent).toContain('工作号')
    expect(document.body.textContent).not.toContain('已撤销')
    expect(document.body.querySelector('[data-test="api-key-input"]')).toBeNull()
    ;(document.body.querySelector('[data-testid="credential-alias-option-c1"]') as HTMLButtonElement).click()
    await wrapper.vm.$nextTick()
    buttonByText('下一步').click()
    await wrapper.vm.$nextTick()

    ;(document.body.querySelector('[data-test="git-choice-github_https"]') as HTMLButtonElement).click()
    await flushPromises()
    expect(document.body.textContent).toContain('org-read')
    expect(document.body.textContent).not.toContain('gl-read')
    expect(document.body.textContent).not.toContain('部署私钥')
    ;(document.body.querySelector('[data-testid="credential-alias-option-g1"]') as HTMLButtonElement).click()
    await wrapper.vm.$nextTick()
    ;(document.body.querySelector('[data-test="git-apply-recommended"]') as HTMLButtonElement).click()
    await wrapper.vm.$nextTick()
    const repos = document.body.querySelector('[data-test="wizard-git-repos"]')
    const inputs = repos?.querySelectorAll('input')
    expect(inputs?.[0]?.value).toBe('GIT_REPOS')
    expect(inputs?.[1]?.value).toBe('${vars.repos}')
    ;(document.body.querySelector('[data-test="git-apply-recommended"]') as HTMLButtonElement).click()
    await wrapper.vm.$nextTick()
    expect(document.body.querySelectorAll('[data-test="wizard-git-repos"]')).toHaveLength(1)

    ;(document.body.querySelector('[data-test="git-choice-ssh"]') as HTMLButtonElement).click()
    await flushPromises()
    expect(document.body.querySelector('[data-testid="credential-alias-option-g1"]')).toBeNull()
    expect(document.body.textContent).toContain('部署私钥')
    expect(document.body.textContent).toContain('已知主机')
    ;(document.body.querySelector('[data-testid="credential-alias-option-s1"]') as HTMLButtonElement).click()
    ;(document.body.querySelector('[data-testid="credential-alias-option-h1"]') as HTMLButtonElement).click()
    await wrapper.vm.$nextTick()
    buttonByText('下一步').click()
    await wrapper.vm.$nextTick()

    expect(document.body.textContent).toContain('工作号')
    expect(document.body.textContent).toContain('部署私钥')
    expect(document.body.textContent).toContain('GIT_REPOS')
    expect(document.body.textContent).not.toContain('API Key · 未配置')
    buttonByText('创建并进入 Studio').click()
    await vi.waitFor(() => expect(createAgent).toHaveBeenCalled())
    const payload = createAgent.mock.calls.at(-1)?.[0] as {
      aiCredentialId?: string
      openCodeCredentialId?: string
      gitCredentialType?: string
      gitCredentialId?: string
      sshHostsCredentialId?: string
      env?: Record<string, string>
    }
    expect(payload.aiCredentialId).toBe('c1')
    expect(payload).not.toHaveProperty('openCodeCredentialId')
    expect(payload.gitCredentialType).toBe('ssh')
    expect(payload.gitCredentialId).toBe('s1')
    expect(payload.sshHostsCredentialId).toBe('h1')
    expect(payload.env?.GIT_REPOS).toBe('${vars.repos}')
    expect(payload.env).not.toHaveProperty('GITHUB_TOKEN')
    expect(payload.env).not.toHaveProperty('GITLAB_TOKEN')
    expect(payload.env).not.toHaveProperty('GIT_SSH_PRIVATE_KEY')
    expect(payload.env).not.toHaveProperty('GIT_SSH_KNOWN_HOSTS')
    expect(payload.env).not.toHaveProperty('GRASP_CURSOR_API_KEY')
    listCredentials.mockResolvedValue({ items: [] })
    wrapper.unmount()
  })
})
