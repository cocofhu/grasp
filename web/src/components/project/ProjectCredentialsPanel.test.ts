// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
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

  it('loads project credentials and masks values in the form', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    expect(mocks.get).toHaveBeenCalledWith('p1')
    expect(wrapper.find('[data-testid="project-credentials-panel"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="project-credentials-summary"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="project-credential-group-ai"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('AI / API Key')
    expect(wrapper.find('[data-testid="project-credential-masked"]').text()).toContain('sk-…1234')
    expect(wrapper.find('[data-testid="project-credential-input-cursor-api"]').attributes('type')).toBe('password')
    expect(wrapper.text()).not.toContain('secret-value')
    expect(wrapper.find('[data-testid="project-credential-input-ssh"]').element.tagName).toBe('TEXTAREA')
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
    await wrapper.get('[data-testid="project-credential-create-name"]').setValue('Custom')
    await wrapper.get('[data-testid="project-credential-create-env"]').setValue('CUSTOM_API_KEY')
    await wrapper.get('[data-testid="project-credential-create-value"]').setValue('custom-secret')
    await wrapper.get('[data-testid="project-credential-create-submit"]').trigger('click')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith('p1', expect.objectContaining({ type: 'custom', name: 'Custom', envKey: 'CUSTOM_API_KEY', value: 'custom-secret' }))
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
    expect(wrapper.find('[data-test="opencode-provider-fields"]').exists()).toBe(true)

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
