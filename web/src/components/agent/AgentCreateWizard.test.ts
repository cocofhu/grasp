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
/** Concurrent noise: run-tags 404 must not break git help (g3/g4). */
const listProjectRunTags = vi.fn(async (_projectId: string) => {
  throw Object.assign(new Error('not found'), { status: 404 })
})
const getProjectSharedAgentConfig = vi.fn(async (_projectId?: string) => ({
  projectId: '',
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
    listProjectRunTags: (projectId: string) => listProjectRunTags(projectId),
    getProjectSharedAgentConfig: (projectId: string) => getProjectSharedAgentConfig(projectId),
    openCodeProviders: async () => ({ providers: [] }),
    openCodeModels: async () => ({ models: [] }),
  },
}))

function mountWizard(locale: 'zh-CN' | 'en' = 'zh-CN', projectId?: string) {
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
    props: { open: true, existingNames: [], projectId },
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

  it('defaults CodeBuddy to international on the Agent step', async () => {
    const wrapper = mountWizard()
    fillName('region-agent')
    await wrapper.vm.$nextTick()
    buttonByText('下一步').click()
    await wrapper.vm.$nextTick()

    buttonByText('用编码 CLI 账号').click()
    await wrapper.vm.$nextTick()
    buttonByText('CodeBuddy').click()
    await wrapper.vm.$nextTick()
    const international = document.body.querySelector(
      'button[role="radio"][aria-label="国际站 (public)"]',
    )
    expect(international?.getAttribute('aria-checked')).toBe('true')
    expect(document.body.textContent).toContain('Agent')
    expect(document.body.textContent).not.toMatch(/配置步骤[\s\S]*\bACP\b/)
    expect(document.body.textContent).toContain('从 API Key 开始')
    wrapper.unmount()
  })

  it('shows API Key apply guide for current backend and allows skip', async () => {
    const wrapper = mountWizard()
    fillName('key-agent')
    await wrapper.vm.$nextTick()
    buttonByText('下一步').click()
    await wrapper.vm.$nextTick()
    buttonByText('用编码 CLI 账号').click()
    await wrapper.vm.$nextTick()
    buttonByText('Cursor').click()
    await wrapper.vm.$nextTick()
    buttonByText('下一步').click()
    await wrapper.vm.$nextTick()

    expect(document.body.textContent).toContain('GRASP_CURSOR_API_KEY')
    expect(document.body.textContent).toContain('CURSOR_API_KEY')
    expect(document.body.textContent).toContain('Cursor Dashboard')
    const dash = Array.from(document.body.querySelectorAll('a')).find((a) =>
      a.href.includes('cursor.com/dashboard'),
    )
    expect(dash).toBeTruthy()

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
    expect(document.body.textContent).toContain('从 API Key 开始')
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

  it('opens Git credential help even when listProjectRunTags would 404', async () => {
    listProjectRunTags.mockRejectedValue(new Error('not found'))
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
    void listProjectRunTags('proj-28d13430').catch(() => {})
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

  it('Git 步在共享 Token 存在时仍渲染三选并可改选（plan g1.2 / g3.2）', async () => {
    getProjectSharedAgentConfig.mockResolvedValue({
      projectId: 'proj-shared',
      env: { GITLAB_TOKEN: '${vars.gitlab_pat}' },
      files: [],
      mcp: [],
      layout: {},
    })
    const wrapper = mountWizard('zh-CN', 'proj-shared')
    fillName('inherit-agent')
    await wrapper.vm.$nextTick()
    buttonByText('下一步').click()
    await wrapper.vm.$nextTick()
    buttonByText('下一步').click()
    await wrapper.vm.$nextTick()
    buttonByText('跳过').click()
    await wrapper.vm.$nextTick()
    await vi.waitFor(() => {
      expect(getProjectSharedAgentConfig).toHaveBeenCalledWith('proj-shared')
    })
    await vi.waitFor(() => {
      expect(document.body.querySelector('[data-test="git-guide"]')).toBeTruthy()
      expect(document.body.querySelector('[data-test="git-choice-github_https"]')).toBeTruthy()
      expect(document.body.querySelector('[data-test="git-choice-gitlab_https"]')).toBeTruthy()
      expect(document.body.querySelector('[data-test="git-choice-ssh"]')).toBeTruthy()
    })
    expect(document.body.textContent).toContain('Git')
    expect(document.body.textContent).toContain('预选类型')
    expect(document.body.textContent).toContain('仍可改选或跳过')
    expect(document.body.textContent).not.toContain('无需选择')
    expect(document.body.textContent).not.toContain('调整类型')

    const gitlab = document.body.querySelector(
      '[data-test="git-choice-gitlab_https"]',
    ) as HTMLButtonElement
    expect(gitlab.getAttribute('aria-pressed')).toBe('true')

    const ssh = document.body.querySelector('[data-test="git-choice-ssh"]') as HTMLButtonElement
    ssh.click()
    await wrapper.vm.$nextTick()
    expect(ssh.getAttribute('aria-pressed')).toBe('true')
    wrapper.unmount()
  })

  it('renders equivalent English site semantics and Agent step label', async () => {
    const wrapper = mountWizard('en')
    expect(railLabels()).toEqual(['Basics', 'Agent', 'API Key', 'Git', 'Confirm'])
    fillName('region-agent')
    await wrapper.vm.$nextTick()
    buttonByText('Next').click()
    await wrapper.vm.$nextTick()
    buttonByText('Use a coding-CLI account').click()
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
})
