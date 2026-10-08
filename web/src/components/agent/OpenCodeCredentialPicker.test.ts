// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import OpenCodeCredentialPicker from './OpenCodeCredentialPicker.vue'
import OpenCodeProviderFields from './OpenCodeProviderFields.vue'

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

const rowA = {
  id: 'a', type: 'ai', provider: 'opencode', name: '主密钥', configured: true, masked: 'sk-…aaaa',
  metadata: { provider: 'openai', model: 'openai/gpt-4o', vision: false },
}
const rowB = {
  id: 'b', type: 'ai', provider: 'opencode', name: '备用', configured: true, masked: 'sk-…bbbb',
  metadata: { provider: 'deepseek', model: 'deepseek/deepseek-flash', vision: true },
}

function mountPicker(props: Record<string, unknown>) {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
  return mount(OpenCodeCredentialPicker, {
    props,
    global: { plugins: [i18n], stubs: { Icon: true, Teleport: true } },
  })
}

describe('OpenCodeCredentialPicker', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.get.mockResolvedValue({ items: [rowA, rowB] })
    mocks.providers.mockResolvedValue({ providers: [{ id: 'openai', name: 'OpenAI' }] })
    mocks.models.mockResolvedValue({ models: [{ id: 'gpt-4o', name: 'GPT-4o' }] })
    mocks.create.mockResolvedValue({
      id: 'cred-new', type: 'ai', provider: 'opencode', name: '新密钥', configured: true, masked: '••••••••',
      metadata: { provider: 'openai', model: 'openai/gpt-4o', vision: false },
    })
  })

  it('lets meta pick one row and hides replace and clear', async () => {
    const wrapper = mountPicker({ mode: 'select', projectId: 'p1', selectedId: 'a' })
    await flushPromises()
    expect(wrapper.find('[data-test="opencode-provider-fields"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="opencode-credential-replace-a"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="opencode-credential-clear-a"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="opencode-credential-current"]').text()).toContain('当前使用')
    expect(wrapper.find('[data-provider-logo="openai"]').exists()).toBe(true)
    expect(wrapper.find('[data-provider-logo="deepseek"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('请选择或填写模型')

    await wrapper.get('[data-testid="opencode-credential-row-a"] button').trigger('click')
    expect(wrapper.emitted('update:selectedId')).toBeUndefined()

    await wrapper.get('[data-testid="opencode-credential-row-b"] button').trigger('click')
    expect(wrapper.emitted('update:selectedId')?.[0]).toEqual(['b'])
    wrapper.unmount()
  })

  it('saves a new key in place and selects it without echoing the secret', async () => {
    const wrapper = mountPicker({ mode: 'select', projectId: 'p1', selectedId: '' })
    await flushPromises()
    expect(wrapper.find('[data-testid="opencode-credential-empty"]').exists()).toBe(false)

    await wrapper.get('[data-testid="opencode-credential-add"]').trigger('click')
    await wrapper.get('[data-testid="opencode-credential-form"]').trigger('submit')
    expect(mocks.create).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('请填写名称')

    await wrapper.get('[data-testid="opencode-credential-name"]').setValue('新密钥')
    await wrapper.get('[data-testid="opencode-credential-key"]').setValue('sk-secret-value')
    wrapper.findComponent(OpenCodeProviderFields).vm.$emit('update:model', 'gpt-4o')
    await wrapper.get('[data-testid="opencode-credential-form"]').trigger('submit')
    await flushPromises()

    expect(mocks.create).toHaveBeenCalledWith('p1', expect.objectContaining({
      type: 'ai',
      provider: 'opencode',
      name: '新密钥',
      value: 'sk-secret-value',
      metadata: expect.objectContaining({ provider: 'openai', model: 'openai/gpt-4o', vision: false }),
    }))
    expect(wrapper.emitted('update:selectedId')?.at(-1)).toEqual(['cred-new'])
    expect(wrapper.text()).not.toContain('sk-secret-value')
    wrapper.unmount()
  })

  it('disables add until a project is chosen', async () => {
    const wrapper = mountPicker({ mode: 'select', projectId: '' })
    await flushPromises()
    expect(wrapper.find('[data-testid="opencode-credential-need-project"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="opencode-credential-add"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-testid="opencode-credential-empty"]').exists()).toBe(true)
    wrapper.unmount()
  })
})
