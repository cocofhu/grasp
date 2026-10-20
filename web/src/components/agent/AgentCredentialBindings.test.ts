// @vitest-environment happy-dom
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { reactive } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import pages from '@/locales/zh-CN/pages.json'
import common from '@/locales/zh-CN/common.json'
import { toDraft } from '@/lib/agent/agentStudioDraft'
import AgentCredentialBindings from './AgentCredentialBindings.vue'
const mocks = vi.hoisted(() => ({ credentials: vi.fn() }))
vi.mock('@/lib/api/api', () => ({ api: { listCredentials: mocks.credentials } }))
beforeEach(() => mocks.credentials.mockResolvedValue({ items: [{ id: 'cred-1', envKey: 'OPENAI_API_KEY', name: 'Primary' }] }))
function setup(bindings?: Record<string, string | null>) {
  const draft = reactive(toDraft({ name: 'Agent', acpBackend: '', files: [], mcp: [], env: {}, layout: {}, credentialBindings: bindings }))
  const wrapper = mount(AgentCredentialBindings, { props: { draft }, global: { plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...pages, ...common } } })], stubs: { RouterLink: true } } })
  return { draft, wrapper }
}
describe('Agent credential bindings', () => {
  it('edits ID, disable and inherit as three distinct normal configuration values', async () => {
    const { draft, wrapper } = setup()
    await flushPromises()
    const select = wrapper.get('select')
    await select.setValue('cred-1')
    expect(draft.credentialBindings).toEqual({ OPENAI_API_KEY: 'cred-1' })
    await select.setValue('__disabled')
    expect(draft.credentialBindings).toEqual({ OPENAI_API_KEY: null })
    await select.setValue('__inherit')
    expect(draft.credentialBindings).toEqual({})
    wrapper.unmount()
  })
  it('keeps unavailable explicit bindings visible without falling back', async () => {
    const { draft, wrapper } = setup({ MISSING_KEY: 'deleted-id' })
    await flushPromises()
    expect(wrapper.text()).toContain('deleted-id')
    expect(draft.credentialBindings!.MISSING_KEY).toBe('deleted-id')
    expect((wrapper.get('#credential-binding-MISSING_KEY').element as HTMLSelectElement).value).toBe('deleted-id')
    wrapper.unmount()
  })
})
