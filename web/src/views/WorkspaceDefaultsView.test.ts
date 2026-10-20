// @vitest-environment happy-dom
import { flushPromises, mount } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import nav from '@/locales/zh-CN/nav.json'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
const mocks = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const { ref } = require('vue') as typeof import('vue')
  return { workspace: ref<any>(null), updateWorkspace: vi.fn(), success: vi.fn(), error: vi.fn() }
})
vi.mock('@/lib/api/api', () => ({ api: mocks }))
vi.mock('@/lib/composables/useWorkspaceContext', () => ({ useWorkspaceContext: () => ({ workspace: mocks.workspace }) }))
vi.mock('@/lib/composables/useToast', () => ({ useToast: () => ({ success: mocks.success, error: mocks.error }) }))
import WorkspaceDefaultsView from './WorkspaceDefaultsView.vue'
async function setup(kind: string) {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: WorkspaceDefaultsView, meta: { workspaceTool: kind } }] })
  await router.push('/')
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...nav, ...common, ...pages } } })
  return mount(WorkspaceDefaultsView, { global: { plugins: [router, i18n], directives: { 'hover-ink': {} }, stubs: { WorkspacePage: { template: '<section><slot /></section>' } } } })
}
beforeEach(() => {
  vi.clearAllMocks()
  mocks.workspace.value = { id: 'original-workspace', name: 'Studio', description: 'Our studio', variables: [], unknownModelDisplayName: '' }
  mocks.updateWorkspace.mockImplementation(async (patch) => ({ ...mocks.workspace.value, ...patch }))
})
describe('workspace defaults pages', () => {
  it('edits workspace metadata without exposing a delete action', async () => {
    const wrapper = await setup('workspaceInfo')
    expect(wrapper.find('[data-testid="workspace-meta-delete"]').exists()).toBe(false)
    await wrapper.get('#workspace-meta-name').setValue('New studio')
    await wrapper.get('#workspace-meta-desc').setValue('A shared workspace')
    await wrapper.findAll('button').find((button) => button.text() === '保存')!.trigger('click')
    await flushPromises()
    expect(mocks.updateWorkspace).toHaveBeenCalledWith(expect.objectContaining({ name: 'New studio', description: 'A shared workspace' }))
    expect(mocks.workspace.value.name).toBe('New studio')
    wrapper.unmount()
  })
  it('preserves hidden variable metadata and masked secrets when editing defaults', async () => {
    mocks.workspace.value.variables = [{ name: 'token', value: '****', type: 'string', secret: true, ask: true, required: true, editable: false, desc: 'API', options: '' }]
    const wrapper = await setup('variables')
    await wrapper.findAll('button').find((button) => button.text() === '保存')!.trigger('click')
    await flushPromises()
    expect(mocks.updateWorkspace).toHaveBeenCalledWith({ variables: [expect.objectContaining({ name: 'token', value: '****', secret: true, ask: true, required: true, editable: false })] })
    wrapper.unmount()
  })
  it('labels variable fields and saves changes from the complete name and value controls', async () => {
    mocks.workspace.value.variables = [{ name: 'ENVIRONMENT', type: 'select', value: 'staging', options: 'staging,production', desc: 'Deploy target', editable: true }]
    const wrapper = await setup('variables')
    await wrapper.get('[data-testid="workspace-var-name-0"]').setValue('RELEASE_ENVIRONMENT')
    await wrapper.get('select[aria-label="默认值"]').setValue('production')
    expect(wrapper.findAll('.workspace-var-label').map((label) => label.text())).toEqual(['名称', '默认值', '类型'])
    await wrapper.findAll('button').find((button) => button.text() === '保存')!.trigger('click')
    await flushPromises()
    expect(mocks.updateWorkspace).toHaveBeenCalledWith({ variables: [expect.objectContaining({ name: 'RELEASE_ENVIRONMENT', value: 'production', type: 'select', options: 'staging,production', editable: true })] })
    wrapper.unmount()
  })
  it('validates the unknown-model display alias before sending metadata', async () => {
    const wrapper = await setup('workspaceInfo')
    await wrapper.get('#workspace-meta-unknown-display').setValue('x'.repeat(81))
    await wrapper.findAll('button').find((button) => button.text() === '保存')!.trigger('click')
    await flushPromises()
    expect(mocks.updateWorkspace).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="workspace-meta-unknown-display-error"]').text()).not.toBe('')
    wrapper.unmount()
  })
  it('clears edited secrets on logout and ignores a prior account save response', async () => {
    mocks.workspace.value.variables = [{ name: 'token', value: 'private secret', type: 'string', secret: true }]
    const wrapper = await setup('variables')
    let resolveSave!: (value: any) => void
    mocks.updateWorkspace.mockReturnValueOnce(new Promise((resolve) => { resolveSave = resolve }))
    await wrapper.findAll('button').find((button) => button.text() === '保存')!.trigger('click')
    mocks.workspace.value = null
    await flushPromises()
    expect(wrapper.html()).not.toContain('private secret')
    const nextWorkspace = { id: 'original-workspace', name: 'Other account', variables: [] }
    mocks.workspace.value = nextWorkspace
    resolveSave({ id: 'original-workspace', name: 'Previous account', variables: [{ name: 'token', value: 'private secret' }] })
    await flushPromises()
    expect(mocks.workspace.value).toEqual(nextWorkspace)
    expect(mocks.success).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
