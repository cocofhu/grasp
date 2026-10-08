// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import enPages from '@/locales/en/pages.json'
import ProjectCredentialsPanel from './ProjectCredentialsPanel.vue'

const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  create: vi.fn(),
  put: vi.fn(),
  del: vi.fn(),
  providers: vi.fn(),
  models: vi.fn(),
  success: vi.fn(),
  error: vi.fn(),
}))

vi.mock('@/lib/api/api', () => ({
  api: {
    getProjectCredentials: mocks.get,
    createProjectCredential: mocks.create,
    putProjectCredential: mocks.put,
    deleteProjectCredential: mocks.del,
    openCodeProviders: mocks.providers,
    openCodeModels: mocks.models,
  },
}))
vi.mock('@/lib/composables/useToast', () => ({ useToast: () => mocks }))

const config = {
  items: [
    {
      id: 'cursor-api', type: 'ai', kind: 'ai', name: 'Cursor API key', provider: 'cursor',
      envKey: 'GRASP_CURSOR_API_KEY', configured: true, masked: 'sk-…1234', updatedAt: '2026-01-02T03:04:00Z',
    },
    {
      id: 'github', type: 'git', kind: 'git', name: 'GitHub token', provider: 'github',
      envKey: 'GITHUB_TOKEN', configured: false,
    },
    {
      id: 'ssh', type: 'ssh', kind: 'ssh', name: 'SSH private key', provider: 'ssh',
      configured: false,
    },
  ],
}

function mountPanel(response = config) {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
  mocks.get.mockResolvedValue(response)
  return mount(ProjectCredentialsPanel, {
    props: { projectId: 'p1' },
    global: { plugins: [i18n], stubs: { Icon: true, Teleport: true } },
  })
}

describe('ProjectCredentialsPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.get.mockResolvedValue(config)
    mocks.put.mockImplementation(async (_project: string, id: string) => ({
      ...config.items.find((item) => item.id === id), configured: true, masked: '••••', updatedAt: '2026-01-03T00:00:00Z',
    }))
    mocks.create.mockResolvedValue({ id: 'cred-custom', type: 'custom', name: 'Custom', configured: true, masked: '••••' })
    mocks.del.mockResolvedValue({ status: 'ok' })
    mocks.providers.mockResolvedValue({ providers: [{ id: 'openai', name: 'OpenAI' }] })
    mocks.models.mockResolvedValue({ models: [{ id: 'gpt-4o', name: 'GPT-4o' }] })
  })

  it('uses the short English credential list copy', () => {
    const copy = enPages.pages.projectDetail.projectCredentials
    expect(copy.groups.ai.title).toBe('Models')
    expect(copy.groups.git.title).toBe('Repositories')
    expect(copy.groups.ssh.title).toBe('SSH')
    expect(copy.groups.other.title).toBe('Other')
    expect(copy.typeAi).toBe('Model')
    expect(copy.typeGit).toBe('Repository')
    expect(copy.writeOnly).toBe('Hidden')
    expect(copy.summary.apiKeys).toBe('Model slots')
    expect(copy.replacePlaceholder).toBe('Enter a new value to replace it')
    expect(copy.valuePlaceholder).toBe('Enter a credential. It is sent only when you save.')
  })

  it('loads project credentials and masks values in the form', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    expect(mocks.get).toHaveBeenCalledWith('p1')
    expect(wrapper.find('[data-testid="project-credentials-panel"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="project-credentials-summary"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="project-credential-group-cursor"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('Cursor')
    expect(wrapper.text()).toContain('GitHub')
    expect(wrapper.text()).toContain('SSH 私钥')
    expect(wrapper.text()).not.toContain('AI / API Key')
    expect(wrapper.text()).toContain('不回显')
    expect(wrapper.text()).not.toContain('仅写入')
    expect(wrapper.find('[data-testid="project-credential-masked"]').text()).toContain('sk-…1234')
    expect(wrapper.find('[data-testid="project-credential-input-cursor-api"]').attributes('type')).toBe('password')
    expect(wrapper.text()).not.toContain('secret-value')
    expect(wrapper.find('[data-testid="project-credential-input-ssh"]').element.tagName).toBe('TEXTAREA')
    expect(wrapper.find('[data-provider-logo="cursor"]').exists()).toBe(true)
    expect(wrapper.find('[data-provider-logo="github"]').exists()).toBe(true)
    expect(wrapper.find('[data-provider-logo="ssh-key"]').exists()).toBe(true)
    expect(wrapper.find('[data-provider-logo="cursor"]').attributes('aria-label')).toContain('Cursor')
    expect(wrapper.text()).not.toContain('GRASP_CURSOR_API_KEY')
    expect(wrapper.text()).not.toContain('GITHUB_TOKEN')
    const content = wrapper.find('.scroll-area > div')
    expect(content.classes()).toContain('w-full')
    expect(content.classes().some((name) => name.startsWith('max-w-'))).toBe(false)
    const cards = wrapper.get('[data-testid="project-credential-group-cursor"] .grid')
    expect(cards.classes()).toEqual(expect.arrayContaining(['grid-cols-1', 'lg:grid-cols-2', '2xl:grid-cols-3']))
    wrapper.unmount()
  })

  it('writes only on an explicit save and clears through DELETE', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    const input = wrapper.find('[data-testid="project-credential-input-github"]')
    await input.setValue('ghp-new')
    await wrapper.find('[data-testid="project-credential-save-github"]').trigger('click')
    await flushPromises()
    expect(mocks.put).toHaveBeenCalledWith('p1', 'github', expect.objectContaining({ value: 'ghp-new', envKey: 'GITHUB_TOKEN' }))

    await wrapper.find('[data-testid="project-credential-clear-cursor-api"]').trigger('click')
    await flushPromises()
    expect(mocks.del).toHaveBeenCalledWith('p1', 'cursor-api')
    wrapper.unmount()
  })

  it('creates a project credential from the dedicated form', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.get('[data-testid="project-credential-create"]').trigger('click')
    expect(wrapper.find('[data-testid="project-credential-create-type"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="project-credential-create-provider"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="project-credential-create-env"]').exists()).toBe(false)
    const cards = wrapper.findAll('[data-testid^="credential-kind-"]')
    expect(cards.length).toBe(10)
    for (const card of cards) {
      expect(card.find('[data-provider-logo]').exists()).toBe(true)
    }
    await wrapper.get('[data-testid="credential-kind-github"]').trigger('click')
    await wrapper.get('[data-testid="project-credential-create-next"]').trigger('click')
    expect(wrapper.get('[data-testid="project-credential-create-form"]').attributes('data-credential-step')).toBe('2')
    expect(wrapper.get('[data-testid="project-credential-create-form"]').attributes('data-step-motion')).toBe('forward')
    expect(wrapper.text()).toContain('别名')
    await new Promise((resolve) => setTimeout(resolve, 220))
    await wrapper.get('[data-testid="project-credential-create-alias"]').setValue('工作号')
    await wrapper.get('[data-testid="project-credential-create-value"]').setValue('ghp-secret')
    await wrapper.get('[data-testid="project-credential-create-submit"]').trigger('click')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith('p1', expect.objectContaining({
      type: 'git', provider: 'github', name: '工作号', envKey: 'GITHUB_TOKEN', value: 'ghp-secret',
    }))
    wrapper.unmount()
  })

  it('rejects a duplicate alias and keeps the step while the animation is locked', async () => {
    const wrapper = mountPanel({
      items: [
        {
          id: 'cursor-api', type: 'ai', name: 'Cursor', provider: 'cursor',
          envKey: 'GRASP_CURSOR_API_KEY', configured: true, masked: 'sk-…1234',
        },
        {
          id: 'cursor-work', type: 'ai', name: '工作号', provider: 'cursor',
          envKey: 'GRASP_CURSOR_API_KEY', configured: true,
        },
      ],
    })
    await flushPromises()
    const titles = wrapper.findAll('[data-testid="project-credential-alias"]').map((node) => node.text())
    const kinds = wrapper.findAll('[data-testid="project-credential-kind"]').map((node) => node.text())
    expect(titles).toEqual(expect.arrayContaining(['Cursor', '工作号']))
    expect(kinds.filter((label) => label === 'Cursor').length).toBe(2)

    await wrapper.get('[data-testid="project-credential-create"]').trigger('click')
    await wrapper.get('[data-testid="credential-kind-cursor"]').trigger('click')
    await wrapper.get('[data-testid="project-credential-create-next"]').trigger('click')
    expect(wrapper.get('[data-testid="project-credential-create-form"]').attributes('data-step-locked')).toBe('true')
    await wrapper.get('[data-testid="project-credential-create-back"]').trigger('click')
    expect(wrapper.get('[data-testid="project-credential-create-form"]').attributes('data-credential-step')).toBe('2')
    await new Promise((resolve) => setTimeout(resolve, 220))

    await wrapper.get('[data-testid="project-credential-create-alias"]').setValue('cursor')
    await wrapper.get('[data-testid="project-credential-create-value"]').setValue('sk-new')
    await wrapper.get('[data-testid="project-credential-create-submit"]').trigger('click')
    expect(mocks.create).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="project-credential-alias-error"]').text()).toContain('这一类里已经有别名「Cursor」')
    expect(wrapper.get('[data-testid="project-credential-create-form"]').attributes('data-credential-step')).toBe('2')

    await wrapper.get('[data-testid="project-credential-create-alias"]').setValue('值班号')
    await wrapper.get('[data-testid="project-credential-create-submit"]').trigger('click')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith('p1', expect.objectContaining({
      type: 'ai', provider: 'cursor', name: '值班号', envKey: 'GRASP_CURSOR_API_KEY', value: 'sk-new',
    }))
    wrapper.unmount()
  })

  it('lists model vendor keys without the required model form, and replaces or clears a row', async () => {
    const opencode = {
      id: 'opencode', type: 'ai', kind: 'ai', name: 'DeepSeek', provider: 'opencode',
      envKey: 'GRASP_OPENCODE_API_KEY', configured: true, masked: 'sk-…9999',
      metadata: { provider: 'deepseek', model: 'deepseek/deepseek-flash', vision: false },
    }
    const wrapper = mountPanel({ items: [...config.items, opencode] })
    await flushPromises()
    expect(wrapper.find('[data-testid="project-credential-opencode"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="opencode-provider-fields"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="opencode-credential-current"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('DeepSeek')
    expect(wrapper.text()).toContain('sk-…9999')
    expect(wrapper.find('[data-testid="project-credential-input-cursor-api"]').exists()).toBe(true)

    await wrapper.get('[data-testid="opencode-credential-add"]').trigger('click')
    expect(wrapper.find('[data-testid="opencode-credential-form"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="project-credential-create-value"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="project-credential-create-form"]').attributes('data-credential-step')).toBe('1')
    await wrapper.get('[data-testid="credential-kind-opencode"]').trigger('click')
    await wrapper.get('[data-testid="project-credential-create-next"]').trigger('click')
    expect(wrapper.get('[data-testid="project-credential-create-form"]').attributes('data-step-motion')).toBe('forward')
    expect(wrapper.get('[data-testid="project-credential-create-alias"]').attributes('placeholder')).toBe('工作号')
    expect(wrapper.text()).toContain('别名')
    expect(wrapper.text()).not.toContain('名称')

    await wrapper.get('[data-testid="opencode-credential-replace-opencode"]').trigger('click')
    await wrapper.get('[data-testid="opencode-credential-replace-key"]').setValue('sk-new')
    mocks.put.mockResolvedValueOnce({ ...opencode, masked: '••••' })
    await wrapper.get('[data-testid="opencode-credential-replace-form-opencode"]').trigger('submit')
    await flushPromises()
    expect(mocks.put).toHaveBeenCalledWith('p1', 'opencode', { value: 'sk-new' })

    await wrapper.get('[data-testid="opencode-credential-clear-opencode"]').trigger('click')
    await flushPromises()
    expect(mocks.del).toHaveBeenCalledWith('p1', 'opencode')
    wrapper.unmount()
  })

  it('shows a retry surface when loading fails', async () => {
    mocks.get.mockRejectedValueOnce(new Error('credentials unavailable'))
    const wrapper = mountPanel()
    await flushPromises()
    expect(wrapper.text()).toContain('credentials unavailable')
    mocks.get.mockResolvedValueOnce(config)
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="project-credential-row"]').exists()).toBe(true)
    wrapper.unmount()
  })
})
