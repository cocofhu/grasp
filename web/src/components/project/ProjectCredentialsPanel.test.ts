// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import enCommon from '@/locales/en/common.json'
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

function mountPanel(response = config, locale: 'zh-CN' | 'en' = 'zh-CN') {
  const i18n = createI18n({
    legacy: false,
    locale,
    messages: {
      'zh-CN': { ...common, ...pages },
      en: { ...enCommon, ...enPages },
    },
  })
  mocks.get.mockResolvedValue(response)
  return mount(ProjectCredentialsPanel, {
    props: { projectId: 'p1' },
    global: { plugins: [i18n], stubs: { Icon: true, Teleport: true } },
  })
}

function rowByAlias(wrapper: ReturnType<typeof mountPanel>, name: string) {
  const row = wrapper.findAll('[data-testid="project-credential-row"]').find((node) => node.get('[data-testid="project-credential-alias"]').text() === name)
  if (!row) throw new Error(`missing credential row ${name}`)
  return row
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
    expect(copy.edit).toBe('Edit')
    expect(copy.fillIn).toBe('Fill in')
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
    expect(wrapper.text()).not.toContain('不回显')
    expect(wrapper.text()).not.toContain('凭据由项目统一管理')
    expect(wrapper.text()).not.toContain('编码助手和模型调用使用的密钥')
    expect(wrapper.text()).not.toContain('拉取、推送和合并请求使用的凭据')
    expect(wrapper.text()).not.toContain('通过 SSH 访问仓库时使用')
    expect(wrapper.text()).toContain('安全凭据中心')
    expect(wrapper.text()).toContain('新增凭据')
    expect(wrapper.text()).toContain('最近更新')
    const mask = wrapper.get('[data-testid="project-credential-masked"]')
    expect(mask.element.tagName).toBe('P')
    expect(mask.classes()).not.toContain('border')
    expect(mask.classes()).not.toContain('bg-surface')
    expect(mask.text()).toContain('sk-…1234')
    expect(mask.text()).not.toContain('不回显')
    expect(wrapper.find('[data-testid="project-credential-input-cursor-api"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="project-credential-input-ssh"]').exists()).toBe(false)
    expect(wrapper.find('input, textarea').exists()).toBe(false)
    const cursor = rowByAlias(wrapper, 'Cursor API key')
    expect(cursor.get('[data-testid="project-credential-headline"]').text()).toContain('编辑')
    expect(cursor.get('[data-testid="project-credential-headline"]').text()).toContain('清除')
    expect(cursor.get('[data-testid="project-credential-actions"]').text()).toContain('已配置')
    const github = rowByAlias(wrapper, 'GitHub token')
    expect(github.get('[data-testid="project-credential-headline"]').text()).toContain('填写')
    expect(github.get('[data-testid="project-credential-headline"]').text()).toContain('未配置')
    expect(github.find('[data-testid="project-credential-clear-github"]').exists()).toBe(false)
    expect(github.find('[data-testid="project-credential-masked"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('secret-value')
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
    expect(wrapper.find('[data-testid="project-credential-input-github"]').exists()).toBe(false)
    await wrapper.get('[data-testid="project-credential-fill-github"]').trigger('click')
    const input = wrapper.get('[data-testid="project-credential-input-github"]')
    expect((input.element as HTMLInputElement).value).toBe('')
    expect(input.attributes('type')).toBe('password')
    const editor = wrapper.get('[data-testid="project-credential-editor-github"]')
    expect(editor.classes()).toContain('flex')
    expect(editor.find('[data-testid="project-credential-save-github"]').exists()).toBe(true)
    expect(editor.find('[data-testid="project-credential-cancel-github"]').exists()).toBe(true)
    await input.setValue('   ')
    expect(wrapper.get('[data-testid="project-credential-save-github"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="project-credential-save-github"]').trigger('click')
    expect(mocks.put).not.toHaveBeenCalled()
    await input.setValue('ghp-new')
    await wrapper.find('[data-testid="project-credential-save-github"]').trigger('click')
    await flushPromises()
    expect(mocks.put).toHaveBeenCalledWith('p1', 'github', expect.objectContaining({ value: 'ghp-new', envKey: 'GITHUB_TOKEN' }))
    expect(wrapper.find('[data-testid="project-credential-input-github"]').exists()).toBe(false)
    expect(rowByAlias(wrapper, 'GitHub token').get('[data-testid="project-credential-masked"]').text()).toContain('••••')

    await wrapper.find('[data-testid="project-credential-clear-cursor-api"]').trigger('click')
    await flushPromises()
    expect(mocks.del).toHaveBeenCalledWith('p1', 'cursor-api')
    const cleared = rowByAlias(wrapper, 'Cursor API key')
    expect(cleared.find('[data-testid="project-credential-masked"]').exists()).toBe(false)
    expect(cleared.find('[data-testid="project-credential-fill-cursor-api"]').exists()).toBe(true)
    expect(cleared.find('[data-testid="project-credential-clear-cursor-api"]').exists()).toBe(false)
    expect(cleared.text()).not.toContain('sk-…1234')
    wrapper.unmount()
  })

  it('opens an empty editor for one row and cancels without saving', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.get('[data-testid="project-credential-edit-cursor-api"]').trigger('click')
    const input = wrapper.get('[data-testid="project-credential-input-cursor-api"]')
    expect(input.element.tagName).toBe('INPUT')
    expect(input.attributes('type')).toBe('password')
    expect((input.element as HTMLInputElement).value).toBe('')
    expect(input.element.textContent).not.toContain('sk-…1234')
    const editor = wrapper.get('[data-testid="project-credential-editor-cursor-api"]')
    expect(editor.find('[data-testid="project-credential-save-cursor-api"]').exists()).toBe(true)
    expect(editor.find('[data-testid="project-credential-cancel-cursor-api"]').exists()).toBe(true)
    expect(rowByAlias(wrapper, 'Cursor API key').get('[data-testid="project-credential-masked"]').text()).toContain('sk-…1234')
    expect(wrapper.find('[data-testid="project-credential-clear-cursor-api"]').exists()).toBe(true)

    await wrapper.get('[data-testid="project-credential-fill-ssh"]').trigger('click')
    expect(wrapper.get('[data-testid="project-credential-input-ssh"]').element.tagName).toBe('TEXTAREA')
    expect((wrapper.get('[data-testid="project-credential-input-ssh"]').element as HTMLTextAreaElement).value).toBe('')

    await input.setValue('sk-draft')
    await wrapper.get('[data-testid="project-credential-cancel-cursor-api"]').trigger('click')
    expect(mocks.put).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="project-credential-input-cursor-api"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="project-credential-editor-ssh"]').exists()).toBe(true)
    expect((wrapper.get('[data-testid="project-credential-input-ssh"]').element as HTMLTextAreaElement).value).toBe('')
    expect(rowByAlias(wrapper, 'Cursor API key').text()).not.toContain('sk-draft')
    wrapper.unmount()
  })

  it('keeps the draft open when save fails and the mask when clear fails', async () => {
    mocks.put.mockRejectedValueOnce(new Error('save failed'))
    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.get('[data-testid="project-credential-edit-cursor-api"]').trigger('click')
    await wrapper.get('[data-testid="project-credential-input-cursor-api"]').setValue('sk-next')
    await wrapper.get('[data-testid="project-credential-save-cursor-api"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="project-credential-editor-cursor-api"]').exists()).toBe(true)
    expect((wrapper.get('[data-testid="project-credential-input-cursor-api"]').element as HTMLInputElement).value).toBe('sk-next')
    expect(rowByAlias(wrapper, 'Cursor API key').get('[data-testid="project-credential-masked"]').text()).toContain('sk-…1234')
    expect(mocks.error).toHaveBeenCalled()

    mocks.del.mockRejectedValueOnce(new Error('clear failed'))
    await wrapper.get('[data-testid="project-credential-clear-cursor-api"]').trigger('click')
    await flushPromises()
    expect(rowByAlias(wrapper, 'Cursor API key').get('[data-testid="project-credential-masked"]').text()).toContain('sk-…1234')
    expect(rowByAlias(wrapper, 'Cursor API key').text()).toContain('已配置')
    wrapper.unmount()
  })

  it('leaves adapter-managed credentials read only', async () => {
    const wrapper = mountPanel({
      items: [
        ...config.items,
        {
          id: 'vault-key', type: 'custom', name: 'Vault token', provider: 'custom',
          configured: true, masked: 'vk-…8888', source: 'vault',
        },
      ],
    })
    await flushPromises()
    const row = rowByAlias(wrapper, 'Vault token')
    expect(row.text()).toContain('由 vault 专用设置管理')
    expect(row.find('[data-testid="project-credential-edit-vault-key"]').exists()).toBe(false)
    expect(row.find('[data-testid="project-credential-fill-vault-key"]').exists()).toBe(false)
    expect(row.find('[data-testid="project-credential-input-vault-key"]').exists()).toBe(false)
    expect(row.find('[data-testid="project-credential-clear-vault-key"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('hides the removed explanations in English and uses Edit and Fill in', async () => {
    const wrapper = mountPanel(config, 'en')
    await flushPromises()
    expect(wrapper.text()).toContain('SECURITY CREDENTIALS')
    expect(wrapper.text()).toContain('Project credentials')
    expect(wrapper.text()).toContain('Add credential')
    expect(rowByAlias(wrapper, 'Cursor API key').get('[data-testid="project-credential-headline"]').text()).toContain('Edit')
    expect(rowByAlias(wrapper, 'Cursor API key').get('[data-testid="project-credential-headline"]').text()).toContain('Clear')
    expect(rowByAlias(wrapper, 'GitHub token').get('[data-testid="project-credential-headline"]').text()).toContain('Fill in')
    expect(wrapper.text()).not.toContain('Hidden')
    expect(wrapper.text()).not.toContain('Manage credentials once at project scope')
    expect(wrapper.text()).not.toContain('Keys used by coding assistants and model calls.')
    expect(wrapper.find('[data-testid="project-credential-input-cursor-api"]').exists()).toBe(false)
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
    expect(wrapper.find('[data-testid="project-credential-input-cursor-api"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="project-credential-edit-cursor-api"]').exists()).toBe(true)

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
