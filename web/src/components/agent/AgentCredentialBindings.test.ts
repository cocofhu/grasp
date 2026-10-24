// @vitest-environment happy-dom
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { reactive } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import pages from '@/locales/zh-CN/pages.json'
import common from '@/locales/zh-CN/common.json'
import { toDraft } from '@/lib/agent/agentStudioDraft'
import AgentCredentialBindings from './AgentCredentialBindings.vue'
import AppSelect from '@/components/ui/AppSelect.vue'
const mocks = vi.hoisted(() => ({ credentials: vi.fn() }))
vi.mock('@/lib/api/api', () => ({ api: { listCredentials: mocks.credentials } }))
beforeEach(() => { vi.clearAllMocks(); mocks.credentials.mockResolvedValue({ items: [{ id: 'cred-1', envKey: 'OPENAI_API_KEY', name: 'Primary', provider: 'openai' }] }) })
function setup(bindings?: Record<string, string | null>) {
  const draft = reactive(toDraft({ name: 'Agent', acpBackend: '', files: [], mcp: [], env: {}, layout: {}, credentialBindings: bindings }))
  const wrapper = mount(AgentCredentialBindings, { props: { draft }, global: { plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...pages, ...common } } })], stubs: { RouterLink: true, Teleport: true } } })
  return { draft, wrapper }
}
describe('Agent credential bindings', () => {
  it('edits ID, disable and inherit as three distinct normal configuration values', async () => {
    const { draft, wrapper } = setup()
    await flushPromises()
    const select = wrapper.getComponent(AppSelect)
    select.vm.$emit('update:modelValue', 'cred-1')
    await flushPromises()
    expect(draft.credentialBindings).toEqual({ OPENAI_API_KEY: 'cred-1' })
    select.vm.$emit('update:modelValue', '__disabled')
    await flushPromises()
    expect(draft.credentialBindings).toEqual({ OPENAI_API_KEY: null })
    select.vm.$emit('update:modelValue', '__inherit')
    await flushPromises()
    expect(draft.credentialBindings).toEqual({})
    wrapper.unmount()
  })
  it('keeps unavailable explicit bindings visible without falling back', async () => {
    const { draft, wrapper } = setup({ MISSING_KEY: 'deleted-id' })
    await flushPromises()
    expect(wrapper.text()).toContain('deleted-id')
    expect(draft.credentialBindings!.MISSING_KEY).toBe('deleted-id')
    expect(wrapper.getComponent('#credential-binding-MISSING_KEY').props('modelValue')).toBe('deleted-id')
    wrapper.unmount()
  })
  it('searches credential names and shows their provider logos in the menu and selection', async () => {
    mocks.credentials.mockResolvedValue({ items: [{ id: 'cred-1', envKey: 'OPENAI_API_KEY', name: 'Primary', provider: 'openai' }, { id: 'cred-2', envKey: 'OPENAI_API_KEY', name: 'Sandbox', provider: 'openai' }] })
    const { draft, wrapper } = setup()
    await flushPromises()
    const select = wrapper.get('#credential-binding-OPENAI_API_KEY')
    await select.get('[data-test="app-select-trigger"]').trigger('click')
    await wrapper.get('[data-test="app-select-search"]').setValue('Sandbox')
    expect(wrapper.find('[data-test="app-select-option-cred-1"]').exists()).toBe(false)
    const option = wrapper.get('[data-test="app-select-option-cred-2"]')
    expect(option.find('[data-provider-logo="openai"]').exists()).toBe(true)
    await option.trigger('click')
    expect(draft.credentialBindings).toEqual({ OPENAI_API_KEY: 'cred-2' })
    expect(select.get('[data-test="app-select-trigger"]').text()).toContain('Sandbox')
    expect(select.find('[data-provider-logo="openai"]').exists()).toBe(true)
    wrapper.unmount()
  })
  it('keeps an explicit credential from a different environment key visibly unavailable', async () => {
    const { draft, wrapper } = setup({ OTHER_KEY: 'cred-1' })
    await flushPromises()
    expect(wrapper.get('#credential-binding-OTHER_KEY').text()).toContain('cred-1')
    expect(wrapper.getComponent('#credential-binding-OTHER_KEY').props('modelValue')).toBe('cred-1')
    expect(draft.credentialBindings).toEqual({ OTHER_KEY: 'cred-1' })
    wrapper.unmount()
  })
})
