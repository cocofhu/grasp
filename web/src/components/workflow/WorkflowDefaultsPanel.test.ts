// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { reactive } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import pages from '@/locales/zh-CN/pages.json'
import common from '@/locales/zh-CN/common.json'
import type { Workflow } from '@/lib/shared/types'
import WorkflowDefaultsPanel from './WorkflowDefaultsPanel.vue'
const apiMocks = vi.hoisted(() => ({ getSettings: undefined as undefined | (() => Promise<{ items: { key: string; value: number }[] }>) }))
vi.mock('@/lib/api/api', () => ({ api: { listChannels: async () => ({ items: [] }), get getSettings() { return apiMocks.getSettings } } }))
vi.mock('@/lib/composables/useWorkspaceContext', () => ({ useWorkspaceContext: () => ({ workspaceId: { value: 'ws' }, ensureLoaded: async () => ({ id: 'ws' }) }) }))
function setup(defaults?: Workflow['defaults']) {
  const workflow = reactive({ id: 'w', name: 'Test', description: '', status: 'draft', version: 1, updatedAt: '', nodes: [], edges: [], defaults } as Workflow)
  const wrapper = mount(WorkflowDefaultsPanel, { props: { workflow, agents: [] }, global: { plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...pages, ...common } } })] } })
  return { workflow, wrapper }
}
describe('Workflow defaults', () => {
  it('preserves independent variables and explicit disabled policies on ordinary edits', async () => {
    const original = { inheritWorkspace: false, variables: [{ name: 'LOCAL', type: 'string', value: 'before' }], notifyPolicy: { enabled: false, channelIds: ['unavailable-channel'], defaultEvents: ['failed'] }, gateAuto: { enabled: false, agentName: 'original', variable: 'gate', prompt: 'prompt', enabledMcps: ['pm-progress'] } }
    const { workflow, wrapper } = setup(original)
    await flushPromises()
    await wrapper.findAll('input').find((input) => input.attributes('aria-label') === pages.pages.workspaceDefaults.defaultValue)!.setValue('after')
    expect(workflow.defaults).toEqual({ ...original, variables: [{ name: 'LOCAL', type: 'string', value: 'after' }] })
    wrapper.unmount()
  })
  it('creates ordinary overrides and restores policy inheritance by removing their fields', async () => {
    const { workflow, wrapper } = setup()
    await flushPromises()
    const sections = wrapper.findAll('section')
    await sections[0].get('input[type="checkbox"]').setValue(false)
    expect(workflow.defaults).toEqual({ inheritWorkspace: false, variables: [] })
    await sections[1].get('input[type="checkbox"]').setValue(true)
    expect(workflow.defaults!.notifyPolicy).toMatchObject({ enabled: true })
    await sections[1].get('input[type="checkbox"]').setValue(false)
    expect(workflow.defaults).not.toHaveProperty('notifyPolicy')
    await sections[2].get('input[type="checkbox"]').setValue(true)
    expect(workflow.defaults!.gateAuto!.enabled).toBe(false)
    await sections[2].get('input[type="checkbox"]').setValue(false)
    expect(workflow.defaults).not.toHaveProperty('gateAuto')
    wrapper.unmount()
  })
  it('does not submit a secret mask as a renamed variable or as a plaintext value', async () => {
    const { workflow, wrapper } = setup({ inheritWorkspace: true, variables: [{ name: 'TOKEN', type: 'string', value: '****', secret: true, ask: true, required: true, editable: true }] })
    await flushPromises()
    const variableSection = wrapper.findAll('section')[0]!
    await variableSection.get(`input[aria-label="${pages.pages.workspaceDefaults.variableName}"]`).setValue('RENAMED_TOKEN')
    expect(workflow.defaults!.variables[0]).toEqual({ name: 'RENAMED_TOKEN', type: 'string', value: '', secret: true, ask: true, required: true, editable: true })
    await variableSection.get(`input[aria-label="${pages.pages.workspaceDefaults.defaultValue}"]`).setValue('replacement')
    const secretToggle = variableSection.findAll('label').find((label) => label.text() === pages.pages.workspaceDefaults.secret)!.get('input')
    await secretToggle.setValue(false)
    expect(workflow.defaults!.variables[0]!.value).toBe('replacement')
    workflow.defaults!.variables[0]!.value = '****'
    await secretToggle.setValue(true)
    await secretToggle.setValue(false)
    expect(workflow.defaults!.variables[0]).toMatchObject({ secret: false, value: '' })
    wrapper.unmount()
  })
  it('stores valid scalar values when the user changes the variable type', async () => {
    const { workflow, wrapper } = setup({ inheritWorkspace: false, variables: [{ name: 'COUNT', type: 'string', value: '12', desc: 'Keep description', ask: true }] })
    await flushPromises()
    const type = wrapper.get(`select[aria-label="${pages.pages.workspaceDefaults.variableType}"]`)
    await type.setValue('number')
    expect(workflow.defaults!.variables[0]).toEqual({ name: 'COUNT', type: 'number', value: 12, desc: 'Keep description', ask: true })
    await type.setValue('string')
    expect(workflow.defaults!.variables[0]!.value).toBe('12')
    await wrapper.get(`input[aria-label="${pages.pages.workspaceDefaults.defaultValue}"]`).setValue('false')
    await type.setValue('bool')
    expect(workflow.defaults!.variables[0]!.value).toBe(false)
    workflow.defaults!.variables[0]!.value = '****'
    workflow.defaults!.variables[0]!.secret = true
    await type.setValue('number')
    expect(workflow.defaults!.variables[0]).toMatchObject({ type: 'number', value: '****', secret: true })
    wrapper.unmount()
  })
  it('lets a migrated workflow with a null variable list add its first override', async () => {
    const { workflow, wrapper } = setup({ inheritWorkspace: false, variables: null } as unknown as Workflow['defaults'])
    await flushPromises()
    await wrapper.findAll('button').find((button) => button.text() === pages.pages.workspaceDefaults.addVariable)!.trigger('click')
    expect(workflow.defaults).toEqual({ inheritWorkspace: false, variables: [{ name: '', type: 'string', value: '' }] })
    expect(wrapper.get(`input[aria-label="${pages.pages.workspaceDefaults.variableName}"]`).exists()).toBe(true)
    wrapper.unmount()
  })
  describe('sandbox mode', () => {
    const knob = (value?: number) => { apiMocks.getSettings = async () => ({ items: value === undefined ? [] : [{ key: 'shared_run_sandbox_enabled', value }] }) }
    afterEach(() => { apiMocks.getSettings = undefined })
    it.each([undefined, 0])('keeps the panel unchanged when the knob is %s', async (value) => {
      knob(value)
      const { wrapper } = setup()
      await flushPromises()
      expect(wrapper.find('[data-testid="workflow-sandbox-mode"]').exists()).toBe(false)
      expect(wrapper.findAll('section')).toHaveLength(3)
      expect(wrapper.text()).not.toContain(pages.pages.workspaceDefaults.sandboxMode.title)
      wrapper.unmount()
    })
    it('stays hidden when settings cannot be read', async () => {
      apiMocks.getSettings = async () => { throw new Error('forbidden') }
      const { wrapper } = setup()
      await flushPromises()
      expect(wrapper.find('[data-testid="workflow-sandbox-mode"]').exists()).toBe(false)
      wrapper.unmount()
    })
    it('writes defaults.sandbox.mode and drops it again for per_node', async () => {
      knob(1)
      const { workflow, wrapper } = setup()
      await flushPromises()
      const section = wrapper.get('[data-testid="workflow-sandbox-mode"]')
      expect(section.text()).toContain(pages.pages.workspaceDefaults.sandboxMode.hint)
      const radios = section.findAll('input[type="radio"]')
      expect((radios[0]!.element as HTMLInputElement).checked).toBe(true)
      await radios[1]!.setValue(true)
      expect(workflow.defaults).toEqual({ inheritWorkspace: true, variables: [], sandbox: { mode: 'shared' } })
      await radios[0]!.setValue(true)
      expect(workflow.defaults).toEqual({ inheritWorkspace: true, variables: [] })
      wrapper.unmount()
    })
    it('per_node on a workflow without defaults does not create them', async () => {
      knob(1)
      const { workflow, wrapper } = setup()
      await flushPromises()
      await wrapper.get('[data-testid="workflow-sandbox-mode"]').findAll('input[type="radio"]')[0]!.trigger('change')
      expect(workflow.defaults).toBeUndefined()
      wrapper.unmount()
    })
  })
})
