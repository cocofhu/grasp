// @vitest-environment happy-dom
import { describe, expect, it, vi } from 'vitest'
import { reactive } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import pages from '@/locales/zh-CN/pages.json'
import common from '@/locales/zh-CN/common.json'
import type { Agent } from '@/lib/api/api'
import type { Workflow } from '@/lib/shared/types'
import WorkflowDefaultsPanel from './WorkflowDefaultsPanel.vue'
vi.mock('@/lib/api/api', () => ({ api: { listChannels: async () => ({ items: [] }) } }))
vi.mock('@/lib/composables/useWorkspaceContext', () => ({ useWorkspaceContext: () => ({ workspaceId: { value: 'ws' }, ensureLoaded: async () => ({ id: 'ws' }) }) }))
function setup(defaults?: Workflow['defaults'], agents: Agent[] = []) {
  const workflow = reactive({ id: 'w', name: 'Test', description: '', status: 'draft', version: 1, updatedAt: '', nodes: [], edges: [], defaults } as Workflow)
  const wrapper = mount(WorkflowDefaultsPanel, { props: { workflow, agents }, global: { stubs: { Teleport: true }, directives: { 'hover-ink': {} }, plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...pages, ...common } } })] } })
  return { workflow, wrapper }
}
async function chooseType(wrapper: VueWrapper, value: string) {
  await wrapper.get('[data-test="app-select-trigger"][aria-label="变量类型"]').trigger('click')
  await wrapper.get('[data-test="app-select-search"]').setValue(value)
  await wrapper.get(`[data-test="app-select-option-${value}"]`).trigger('click')
}
describe('Workflow defaults', () => {
  it('starts with independent sandboxes and switches the workflow with one control', async () => {
    const { workflow, wrapper } = setup()
    await flushPromises()
    const toggle = wrapper.get('[data-testid="workflow-shared-sandbox-switch"]')
    expect(toggle.attributes('aria-checked')).toBe('false')
    expect(workflow.defaults).toBeUndefined()
    await toggle.trigger('click')
    expect(toggle.attributes('aria-checked')).toBe('true')
    expect(workflow.defaults).toEqual({ inheritWorkspace: true, variables: [], sandbox: { mode: 'shared' } })
    await toggle.trigger('click')
    expect(workflow.defaults!.sandbox).toEqual({ mode: 'per_node' })
    wrapper.unmount()
  })
  it('loads the saved shared mode and preserves every other workflow default when toggled', async () => {
    const original: Workflow['defaults'] = {
      inheritWorkspace: false,
      variables: [{ name: 'LOCAL', type: 'string', value: 'keep' }],
      sandbox: { mode: 'shared' },
      notifyPolicy: { enabled: false, channelIds: ['channel'], defaultEvents: ['failed'] },
      gateAuto: { enabled: false, agentName: 'reviewer', variable: 'review', prompt: 'Keep prompt', enabledMcps: ['pm-progress'] },
    }
    const { workflow, wrapper } = setup(structuredClone(original))
    await flushPromises()
    const toggle = wrapper.get('[data-testid="workflow-shared-sandbox-switch"]')
    expect(toggle.attributes('aria-checked')).toBe('true')
    await toggle.trigger('click')
    expect(workflow.defaults).toEqual({ ...original, sandbox: { mode: 'per_node' } })
    await toggle.trigger('click')
    expect(workflow.defaults).toEqual(original)
    wrapper.unmount()
  })
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
    await wrapper.get('[role="switch"][aria-label="继承工作区共享变量"]').trigger('click')
    expect(workflow.defaults).toEqual({ inheritWorkspace: false, variables: [] })
    const notify = wrapper.get('[role="switch"][aria-label="单独设置运行通知"]')
    await notify.trigger('click')
    expect(workflow.defaults!.notifyPolicy).toMatchObject({ enabled: true })
    await notify.trigger('click')
    expect(workflow.defaults).not.toHaveProperty('notifyPolicy')
    const gate = wrapper.get('[role="switch"][aria-label="单独设置自动审批"]')
    await gate.trigger('click')
    expect(workflow.defaults!.gateAuto!.enabled).toBe(false)
    await gate.trigger('click')
    expect(workflow.defaults).not.toHaveProperty('gateAuto')
    wrapper.unmount()
  })
  it('does not submit a secret mask as a renamed variable or as a plaintext value', async () => {
    const { workflow, wrapper } = setup({ inheritWorkspace: true, variables: [{ name: 'TOKEN', type: 'string', value: '****', secret: true, ask: true, required: true, editable: true }] })
    await flushPromises()
    const variableSection = wrapper.get('[data-testid="workflow-variables-settings"]')
    await variableSection.get(`input[aria-label="${pages.pages.workspaceDefaults.variableName}"]`).setValue('RENAMED_TOKEN')
    expect(workflow.defaults!.variables[0]).toEqual({ name: 'RENAMED_TOKEN', type: 'string', value: '', secret: true, ask: true, required: true, editable: true })
    await variableSection.get(`input[aria-label="${pages.pages.workspaceDefaults.defaultValue}"]`).setValue('replacement')
    const secretToggle = variableSection.get('[role="switch"][aria-label="密钥"]')
    await secretToggle.trigger('click')
    expect(workflow.defaults!.variables[0]!.value).toBe('replacement')
    workflow.defaults!.variables[0]!.value = '****'
    await secretToggle.trigger('click')
    await secretToggle.trigger('click')
    expect(workflow.defaults!.variables[0]).toMatchObject({ secret: false, value: '' })
    wrapper.unmount()
  })
  it('stores valid scalar values when the user changes the variable type', async () => {
    const { workflow, wrapper } = setup({ inheritWorkspace: false, variables: [{ name: 'COUNT', type: 'string', value: '12', desc: 'Keep description', ask: true }] })
    await flushPromises()
    await chooseType(wrapper, 'number')
    expect(workflow.defaults!.variables[0]).toEqual({ name: 'COUNT', type: 'number', value: 12, desc: 'Keep description', ask: true })
    await chooseType(wrapper, 'string')
    expect(workflow.defaults!.variables[0]!.value).toBe('12')
    await wrapper.get(`input[aria-label="${pages.pages.workspaceDefaults.defaultValue}"]`).setValue('false')
    await chooseType(wrapper, 'bool')
    expect(workflow.defaults!.variables[0]!.value).toBe(false)
    workflow.defaults!.variables[0]!.value = '****'
    workflow.defaults!.variables[0]!.secret = true
    await chooseType(wrapper, 'number')
    expect(workflow.defaults!.variables[0]).toMatchObject({ type: 'number', value: '****', secret: true })
    wrapper.unmount()
  })
  it('lets a migrated workflow with a null variable list add its first override', async () => {
    const { workflow, wrapper } = setup({ inheritWorkspace: false, variables: null } as unknown as Workflow['defaults'])
    await flushPromises()
    await wrapper.get(`[aria-label="${pages.pages.workspaceDefaults.addVariable}"]`).trigger('click')
    expect(workflow.defaults).toEqual({ inheritWorkspace: false, variables: [{ name: '', type: 'string', value: '' }] })
    expect(wrapper.get(`input[aria-label="${pages.pages.workspaceDefaults.variableName}"]`).exists()).toBe(true)
    wrapper.unmount()
  })
  it('shows effective notification and MCP defaults without writing them until the user changes them', async () => {
    const original = {
      inheritWorkspace: true,
      variables: [],
      notifyPolicy: { enabled: null, defaultEvents: null, channelIds: [] },
      gateAuto: { enabled: false, agentName: 'saved-agent', variable: '', prompt: '', enabledMcps: null },
    }
    const { workflow, wrapper } = setup(structuredClone(original) as unknown as Workflow['defaults'])
    await flushPromises()
    expect(wrapper.get('[role="switch"][aria-label="发送运行通知"]').attributes('aria-checked')).toBe('true')
    const notify = wrapper.get('[data-testid="workflow-notification-settings"]')
    const failed = notify.findAll('[aria-pressed]').find((button) => button.text() === '运行失败')!
    expect(failed.attributes('aria-pressed')).toBe('true')
    const gate = wrapper.get('[data-testid="workflow-gate-settings"]')
    expect(gate.findAll('[aria-pressed="true"]')).toHaveLength(5)
    expect(workflow.defaults).toEqual(original)
    await failed.trigger('click')
    expect(workflow.defaults!.notifyPolicy!.defaultEvents).toEqual(['waiting_human'])
    await gate.findAll('[aria-pressed]').find((button) => button.text() === 'pm-progress')!.trigger('click')
    expect(workflow.defaults!.gateAuto!.enabledMcps).toEqual(['pm-workflow-read', 'pm-workflow-write', 'pm-agent-fs', 'pm-prd-manager'])
    await wrapper.get('[role="switch"][aria-label="发送运行通知"]').trigger('click')
    expect(workflow.defaults!.notifyPolicy!.enabled).toBe(false)
    wrapper.unmount()
  })
  it('preserves explicit disabled settings and unknown selections while editing known options', async () => {
    const { workflow, wrapper } = setup({
      inheritWorkspace: true,
      variables: [],
      notifyPolicy: { enabled: false, defaultEvents: ['other-event'], channelIds: ['unavailable-channel'] },
      gateAuto: { enabled: false, agentName: 'unavailable-agent', variable: '', prompt: '', enabledMcps: ['other-pack'] },
    }, [{ name: 'review-agent' }, { name: 'delivery-agent' }] as Agent[])
    await flushPromises()
    expect(wrapper.get('[role="switch"][aria-label="发送运行通知"]').attributes('aria-checked')).toBe('false')
    expect(wrapper.find('input[type="checkbox"], select').exists()).toBe(false)
    const agent = wrapper.get('[data-test="app-select-trigger"][aria-label="执行 Agent"]')
    expect(agent.text()).toBe('unavailable-agent')
    await agent.trigger('click')
    await wrapper.get('[data-test="app-select-search"]').setValue('review')
    expect(wrapper.find('[data-test="app-select-option-delivery-agent"]').exists()).toBe(false)
    await wrapper.get('[data-test="app-select-search"]').trigger('keydown', { key: 'Enter' })
    expect(workflow.defaults!.gateAuto!.agentName).toBe('review-agent')
    const notification = wrapper.get('[data-testid="workflow-notification-settings"]')
    await notification.findAll('[aria-pressed]').find((button) => button.text() === '运行完成')!.trigger('click')
    expect(workflow.defaults!.notifyPolicy).toEqual({ enabled: false, defaultEvents: ['other-event', 'completed'], channelIds: ['unavailable-channel'] })
    const pack = wrapper.get('[data-testid="workflow-gate-settings"]').findAll('[aria-pressed]').find((button) => button.text() === 'pm-progress')!
    await pack.trigger('click')
    expect(workflow.defaults!.gateAuto!.enabledMcps).toEqual(['other-pack', 'pm-progress'])
    await pack.trigger('click')
    expect(workflow.defaults!.gateAuto!.enabledMcps).toEqual(['other-pack'])
    wrapper.unmount()
  })
})
