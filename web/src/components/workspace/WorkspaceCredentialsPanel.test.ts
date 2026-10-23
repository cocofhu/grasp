// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import enCommon from '@/locales/en/common.json'
import pages from '@/locales/zh-CN/pages.json'
import enPages from '@/locales/en/pages.json'
import WorkspaceCredentialsPanel from './WorkspaceCredentialsPanel.vue'

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
    listCredentials: mocks.get,
    createCredential: mocks.create,
    putCredential: mocks.put,
    deleteCredential: mocks.del,
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
  return mount(WorkspaceCredentialsPanel, {
    global: { plugins: [i18n], stubs: { Icon: true, Teleport: true } },
  })
}

function rowByAlias(wrapper: ReturnType<typeof mountPanel>, name: string) {
  const row = wrapper.findAll('[data-testid="workspace-credential-row"]').find((node) => node.get('[data-testid="workspace-credential-alias"]').text() === name)
  if (!row) throw new Error(`missing credential row ${name}`)
  return row
}

describe('WorkspaceCredentialsPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.get.mockResolvedValue(config)
    mocks.put.mockImplementation(async (id: string) => ({
      ...config.items.find((item) => item.id === id), configured: true, masked: '••••', updatedAt: '2026-01-03T00:00:00Z',
    }))
    mocks.create.mockResolvedValue({ id: 'cred-custom', type: 'custom', name: 'Custom', configured: true, masked: '••••' })
    mocks.del.mockResolvedValue({ status: 'ok' })
    mocks.providers.mockResolvedValue({ providers: [{ id: 'openai', name: 'OpenAI' }] })
    mocks.models.mockResolvedValue({ models: [{ id: 'gpt-4o', name: 'GPT-4o' }] })
  })

  it('uses the short English credential list copy', () => {
    const copy = enPages.pages.workspace.credentials
    expect(copy.groups.other.title).toBe('Other')
    expect(copy.typeAi).toBe('Model')
    expect(copy.typeGit).toBe('Repository')
    expect(copy.edit).toBe('Edit')
    expect(copy.fillIn).toBe('Fill in')
    expect(copy.summary.apiKeys).toBe('Model slots')
    expect(copy.replacePlaceholder).toBe('Enter a new value to replace it')
    expect(copy.valuePlaceholder).toBe('Enter a credential. It is sent only when you save.')
  })

  it('loads workspace credentials and masks values in the form', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    expect(mocks.get).toHaveBeenCalledWith()
    expect(wrapper.find('[data-testid="workspace-credentials-panel"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="workspace-credentials-summary"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="workspace-credential-group-cursor"]').exists()).toBe(true)
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
    const mask = wrapper.get('[data-testid="workspace-credential-masked"]')
    expect(mask.element.tagName).toBe('P')
    expect(mask.classes()).not.toContain('border')
    expect(mask.classes()).not.toContain('bg-surface')
    expect(mask.text()).toContain('sk-…1234')
    expect(mask.text()).not.toContain('不回显')
    expect(wrapper.find('[data-testid="workspace-credential-input-cursor-api"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="workspace-credential-input-ssh"]').exists()).toBe(false)
    expect(wrapper.find('input, textarea').exists()).toBe(false)
    const cursor = rowByAlias(wrapper, 'Cursor API key')
    expect(cursor.get('[data-testid="workspace-credential-headline"]').text()).toContain('编辑')
    expect(cursor.get('[data-testid="workspace-credential-headline"]').text()).toContain('清除')
    expect(cursor.get('[data-testid="workspace-credential-actions"]').text()).toContain('已配置')
    const github = rowByAlias(wrapper, 'GitHub token')
    expect(github.get('[data-testid="workspace-credential-headline"]').text()).toContain('填写')
    expect(github.get('[data-testid="workspace-credential-headline"]').text()).toContain('未配置')
    expect(github.find('[data-testid="workspace-credential-clear-github"]').exists()).toBe(false)
    expect(github.find('[data-testid="workspace-credential-masked"]').exists()).toBe(false)
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
    const credentials = wrapper.get('[data-testid="workspace-credential-group-cursor"]')
    expect(credentials.findAll('[data-testid="workspace-credential-row"]')).toHaveLength(1)
    expect(credentials.get('[data-testid="workspace-credential-alias"]').text()).toContain('Cursor')
    wrapper.unmount()
  })

  it('writes only on an explicit save and clears through DELETE', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    expect(wrapper.find('[data-testid="workspace-credential-input-github"]').exists()).toBe(false)
    await wrapper.get('[data-testid="workspace-credential-fill-github"]').trigger('click')
    const input = wrapper.get('[data-testid="workspace-credential-input-github"]')
    expect((input.element as HTMLInputElement).value).toBe('')
    expect(input.attributes('type')).toBe('password')
    const editor = wrapper.get('[data-testid="workspace-credential-editor-github"]')
    expect(editor.classes()).toContain('flex')
    expect(editor.find('[data-testid="workspace-credential-save-github"]').exists()).toBe(true)
    expect(editor.find('[data-testid="workspace-credential-cancel-github"]').exists()).toBe(true)
    await input.setValue('   ')
    expect(wrapper.get('[data-testid="workspace-credential-save-github"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="workspace-credential-save-github"]').trigger('click')
    expect(mocks.put).not.toHaveBeenCalled()
    await input.setValue('ghp-new')
    await wrapper.find('[data-testid="workspace-credential-save-github"]').trigger('click')
    await flushPromises()
    expect(mocks.put).toHaveBeenCalledWith('github', expect.objectContaining({ value: 'ghp-new', envKey: 'GITHUB_TOKEN' }))
    expect(wrapper.find('[data-testid="workspace-credential-input-github"]').exists()).toBe(false)
    expect(rowByAlias(wrapper, 'GitHub token').get('[data-testid="workspace-credential-masked"]').text()).toContain('••••')

    await wrapper.find('[data-testid="workspace-credential-clear-cursor-api"]').trigger('click')
    await flushPromises()
    expect(mocks.del).toHaveBeenCalledWith('cursor-api')
    const cleared = rowByAlias(wrapper, 'Cursor API key')
    expect(cleared.find('[data-testid="workspace-credential-masked"]').exists()).toBe(false)
    expect(cleared.find('[data-testid="workspace-credential-fill-cursor-api"]').exists()).toBe(true)
    expect(cleared.find('[data-testid="workspace-credential-clear-cursor-api"]').exists()).toBe(false)
    expect(cleared.text()).not.toContain('sk-…1234')
    wrapper.unmount()
  })

  it('opens an empty editor for one row and cancels without saving', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.get('[data-testid="workspace-credential-edit-cursor-api"]').trigger('click')
    const input = wrapper.get('[data-testid="workspace-credential-input-cursor-api"]')
    expect(input.element.tagName).toBe('INPUT')
    expect(input.attributes('type')).toBe('password')
    expect((input.element as HTMLInputElement).value).toBe('')
    expect(input.element.textContent).not.toContain('sk-…1234')
    const editor = wrapper.get('[data-testid="workspace-credential-editor-cursor-api"]')
    expect(editor.find('[data-testid="workspace-credential-save-cursor-api"]').exists()).toBe(true)
    expect(editor.find('[data-testid="workspace-credential-cancel-cursor-api"]').exists()).toBe(true)
    expect(rowByAlias(wrapper, 'Cursor API key').get('[data-testid="workspace-credential-masked"]').text()).toContain('sk-…1234')
    expect(wrapper.find('[data-testid="workspace-credential-clear-cursor-api"]').exists()).toBe(true)

    await wrapper.get('[data-testid="workspace-credential-fill-ssh"]').trigger('click')
    expect(wrapper.get('[data-testid="workspace-credential-input-ssh"]').element.tagName).toBe('TEXTAREA')
    expect((wrapper.get('[data-testid="workspace-credential-input-ssh"]').element as HTMLTextAreaElement).value).toBe('')

    await input.setValue('sk-draft')
    await wrapper.get('[data-testid="workspace-credential-cancel-cursor-api"]').trigger('click')
    expect(mocks.put).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="workspace-credential-input-cursor-api"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="workspace-credential-editor-ssh"]').exists()).toBe(true)
    expect((wrapper.get('[data-testid="workspace-credential-input-ssh"]').element as HTMLTextAreaElement).value).toBe('')
    expect(rowByAlias(wrapper, 'Cursor API key').text()).not.toContain('sk-draft')
    wrapper.unmount()
  })

  it('keeps the draft open when save fails and the mask when clear fails', async () => {
    mocks.put.mockRejectedValueOnce(new Error('save failed'))
    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.get('[data-testid="workspace-credential-edit-cursor-api"]').trigger('click')
    await wrapper.get('[data-testid="workspace-credential-input-cursor-api"]').setValue('sk-next')
    await wrapper.get('[data-testid="workspace-credential-save-cursor-api"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="workspace-credential-editor-cursor-api"]').exists()).toBe(true)
    expect((wrapper.get('[data-testid="workspace-credential-input-cursor-api"]').element as HTMLInputElement).value).toBe('sk-next')
    expect(rowByAlias(wrapper, 'Cursor API key').get('[data-testid="workspace-credential-masked"]').text()).toContain('sk-…1234')
    expect(mocks.error).toHaveBeenCalled()

    mocks.del.mockRejectedValueOnce(new Error('clear failed'))
    await wrapper.get('[data-testid="workspace-credential-clear-cursor-api"]').trigger('click')
    await flushPromises()
    expect(rowByAlias(wrapper, 'Cursor API key').get('[data-testid="workspace-credential-masked"]').text()).toContain('sk-…1234')
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
    expect(row.find('[data-testid="workspace-credential-edit-vault-key"]').exists()).toBe(false)
    expect(row.find('[data-testid="workspace-credential-fill-vault-key"]').exists()).toBe(false)
    expect(row.find('[data-testid="workspace-credential-input-vault-key"]').exists()).toBe(false)
    expect(row.find('[data-testid="workspace-credential-clear-vault-key"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('hides the removed explanations in English and uses Edit and Fill in', async () => {
    const wrapper = mountPanel(config, 'en')
    await flushPromises()
    expect(wrapper.text()).toContain('SECURITY CREDENTIALS')
    expect(wrapper.text()).toContain('Workspace credentials')
    expect(wrapper.text()).toContain('Add credential')
    expect(rowByAlias(wrapper, 'Cursor API key').get('[data-testid="workspace-credential-headline"]').text()).toContain('Edit')
    expect(rowByAlias(wrapper, 'Cursor API key').get('[data-testid="workspace-credential-headline"]').text()).toContain('Clear')
    expect(rowByAlias(wrapper, 'GitHub token').get('[data-testid="workspace-credential-headline"]').text()).toContain('Fill in')
    expect(wrapper.text()).not.toContain('Hidden')
    expect(wrapper.text()).not.toContain('Keys used by coding assistants and model calls.')
    expect(wrapper.find('[data-testid="workspace-credential-input-cursor-api"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('creates a workspace credential from the dedicated form', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.get('[data-testid="workspace-credential-create"]').trigger('click')
    expect(wrapper.find('[data-testid="workspace-credential-create-type"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="workspace-credential-create-provider"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="workspace-credential-create-env"]').exists()).toBe(false)
    const cards = wrapper.findAll('[data-testid^="credential-kind-"]')
    expect(cards.length).toBe(11)
    for (const card of cards) {
      expect(card.find('[data-provider-logo]').exists()).toBe(true)
    }
    await wrapper.get('[data-testid="credential-kind-github"]').trigger('click')
    await wrapper.get('[data-testid="workspace-credential-create-next"]').trigger('click')
    expect(wrapper.get('[data-testid="workspace-credential-create-form"]').attributes('data-credential-step')).toBe('2')
    expect(wrapper.get('[data-testid="workspace-credential-create-form"]').attributes('data-step-motion')).toBe('forward')
    expect(wrapper.text()).toContain('别名')
    await new Promise((resolve) => setTimeout(resolve, 220))
    await wrapper.get('[data-testid="workspace-credential-create-alias"]').setValue('工作号')
    await wrapper.get('[data-testid="workspace-credential-create-value"]').setValue('ghp-secret')
    await wrapper.get('[data-testid="workspace-credential-create-submit"]').trigger('click')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({
      type: 'git', provider: 'github', name: '工作号', envKey: 'GITHUB_TOKEN', value: 'ghp-secret',
    }))
    wrapper.unmount()
  })

  it('creates a DeepSeek Harness key without OpenCode model metadata', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.get('[data-testid="workspace-credential-create"]').trigger('click')
    const kind = wrapper.get('[data-testid="credential-kind-deepseek_harness"]')
    expect(kind.find('[data-provider-logo="deepseek"]').exists()).toBe(true)
    await kind.trigger('click')
    await wrapper.get('[data-testid="workspace-credential-create-next"]').trigger('click')
    await new Promise((resolve) => setTimeout(resolve, 220))
    expect(wrapper.find('[data-test="opencode-provider"]').exists()).toBe(false)
    await wrapper.get('[data-testid="workspace-credential-create-alias"]').setValue('DeepSeek team')
    await wrapper.get('[data-testid="workspace-credential-create-value"]').setValue('dsh-example-key')
    await wrapper.get('[data-testid="workspace-credential-create-submit"]').trigger('click')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith({
      type: 'ai', provider: 'deepseek_harness', name: 'DeepSeek team',
      envKey: 'GRASP_DEEPSEEK_API_KEY', value: 'dsh-example-key',
    })
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
    const titles = wrapper.findAll('[data-testid="workspace-credential-alias"]').map((node) => node.text())
    const kinds = wrapper.findAll('[data-testid="workspace-credential-kind"]').map((node) => node.text())
    expect(titles).toEqual(expect.arrayContaining(['Cursor', '工作号']))
    expect(kinds.filter((label) => label === 'Cursor').length).toBe(2)

    await wrapper.get('[data-testid="workspace-credential-create"]').trigger('click')
    await wrapper.get('[data-testid="credential-kind-cursor"]').trigger('click')
    await wrapper.get('[data-testid="workspace-credential-create-next"]').trigger('click')
    expect(wrapper.get('[data-testid="workspace-credential-create-form"]').attributes('data-step-locked')).toBe('true')
    await wrapper.get('[data-testid="workspace-credential-create-back"]').trigger('click')
    expect(wrapper.get('[data-testid="workspace-credential-create-form"]').attributes('data-credential-step')).toBe('2')
    await new Promise((resolve) => setTimeout(resolve, 220))

    await wrapper.get('[data-testid="workspace-credential-create-alias"]').setValue('cursor')
    await wrapper.get('[data-testid="workspace-credential-create-value"]').setValue('sk-new')
    await wrapper.get('[data-testid="workspace-credential-create-submit"]').trigger('click')
    expect(mocks.create).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="workspace-credential-alias-error"]').text()).toContain('这一类里已经有别名「Cursor」')
    expect(wrapper.get('[data-testid="workspace-credential-create-form"]').attributes('data-credential-step')).toBe('2')

    await wrapper.get('[data-testid="workspace-credential-create-alias"]').setValue('值班号')
    await wrapper.get('[data-testid="workspace-credential-create-submit"]').trigger('click')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({
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
    expect(wrapper.find('[data-testid="credential-opencode"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="opencode-provider-fields"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="opencode-credential-current"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('DeepSeek')
    expect(wrapper.text()).toContain('sk-…9999')
    expect(wrapper.find('[data-testid="workspace-credential-input-cursor-api"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="workspace-credential-edit-cursor-api"]').exists()).toBe(true)

    await wrapper.get('[data-testid="opencode-credential-add"]').trigger('click')
    expect(wrapper.find('[data-testid="opencode-credential-form"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="workspace-credential-create-value"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="workspace-credential-create-form"]').attributes('data-credential-step')).toBe('1')
    await wrapper.get('[data-testid="credential-kind-opencode"]').trigger('click')
    await wrapper.get('[data-testid="workspace-credential-create-next"]').trigger('click')
    expect(wrapper.get('[data-testid="workspace-credential-create-form"]').attributes('data-step-motion')).toBe('forward')
    expect(wrapper.get('[data-testid="workspace-credential-create-alias"]').attributes('placeholder')).toBe('工作号')
    expect(wrapper.text()).toContain('别名')
    expect(wrapper.text()).not.toContain('名称')

    await wrapper.get('[data-testid="opencode-credential-replace-opencode"]').trigger('click')
    await wrapper.get('[data-testid="opencode-credential-replace-key"]').setValue('sk-new')
    mocks.put.mockResolvedValueOnce({ ...opencode, masked: '••••' })
    await wrapper.get('[data-testid="opencode-credential-replace-form-opencode"]').trigger('submit')
    await flushPromises()
    expect(mocks.put).toHaveBeenCalledWith('opencode', { value: 'sk-new' })

    await wrapper.get('[data-testid="opencode-credential-clear-opencode"]').trigger('click')
    await flushPromises()
    expect(mocks.del).toHaveBeenCalledWith('opencode')
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
    expect(wrapper.find('[data-testid="workspace-credential-row"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('sizes the create step shell with min-height so tall step-two forms stay reachable', async () => {
    const wrapper = mountPanel()
    await flushPromises()
    await wrapper.get('[data-testid="workspace-credential-create"]').trigger('click')
    await wrapper.get('[data-testid="credential-kind-opencode"]').trigger('click')
    await wrapper.get('[data-testid="workspace-credential-create-next"]').trigger('click')
    await flushPromises()
    await new Promise((resolve) => setTimeout(resolve, 220))

    const shell = wrapper.get('[data-testid="workspace-credential-create-form"]')
    const shellEl = shell.element as HTMLElement
    const styleAttr = shell.attributes('style') || ''
    // Fixed height + overflow:hidden clipped the secret after async growth; floor only.
    expect(shellEl.style.height).toBe('')
    expect(styleAttr).not.toMatch(/(?:^|;)\s*height\s*:/)
    if (shellEl.style.minHeight) {
      expect(shellEl.style.minHeight).toMatch(/^\d+(\.\d+)?px$/)
    }

    const pane = shell.get('.credential-step-pane').element as HTMLElement
    pane.style.minHeight = '1600px'
    await flushPromises()

    const secret = wrapper.get('[data-testid="workspace-credential-create-value"]')
    expect(secret.exists()).toBe(true)
    await secret.setValue('sk-reachable')
    expect((secret.element as HTMLInputElement).value).toBe('sk-reachable')
    expect(shellEl.style.height).toBe('')

    await wrapper.get('[data-testid="workspace-credential-create-back"]').trigger('click')
    await new Promise((resolve) => setTimeout(resolve, 220))
    expect(shell.attributes('data-credential-step')).toBe('1')
    expect(shell.attributes('data-step-motion')).toBe('back')
    wrapper.unmount()
  })
})
