// @vitest-environment happy-dom
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it, vi } from 'vitest'
import { nextTick, reactive } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import pages from '@/locales/zh-CN/pages.json'
import common from '@/locales/zh-CN/common.json'
import type { Workflow } from '@/lib/shared/types'
import WorkflowDefaultsPanel from './WorkflowDefaultsPanel.vue'
const listProjects = vi.hoisted(() => vi.fn(async () => [] as Array<Record<string, unknown>>))
vi.mock('@/lib/api/api', () => ({ api: { listProjects } }))
vi.mock('@/lib/composables/useWorkspaceContext', () => ({ useWorkspaceContext: () => ({ workspaceId: { value: 'ws' }, ensureLoaded: async () => ({ id: 'ws' }) }) }))
function setup(defaults?: Workflow['defaults']) {
  const workflow = reactive({ id: 'w', name: 'Test', description: '', status: 'draft', version: 1, updatedAt: '', nodes: [], edges: [], defaults } as Workflow)
  const wrapper = mount(WorkflowDefaultsPanel, { props: { workflow }, global: { stubs: { Teleport: true }, directives: { 'hover-ink': {} }, plugins: [createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...pages, ...common } } })] } })
  return { workflow, wrapper }
}
async function chooseType(wrapper: VueWrapper, value: string) {
  await wrapper.get('[data-test="app-select-trigger"][aria-label="变量类型"]').trigger('click')
  await wrapper.get('[data-test="app-select-search"]').setValue(value)
  await wrapper.get(`[data-test="app-select-option-${value}"]`).trigger('click')
}
describe('Workflow defaults', () => {
  it('has no sandbox mode or auto-approval control', async () => {
    const { wrapper } = setup()
    await flushPromises()
    expect(wrapper.find('[data-testid="workflow-sandbox-settings"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="workflow-gate-settings"]').exists()).toBe(false)
    wrapper.unmount()
  })
  it('binds repositories in order with optional branches and removes the field when none remain', async () => {
    listProjects.mockResolvedValueOnce([
      { id: 'p1', name: 'web', repoUrl: 'https://github.com/org/web.git', defaultBranch: 'main' },
      { id: 'p2', name: 'api', repoUrl: 'https://github.com/org/api.git' },
    ])
    const { workflow, wrapper } = setup({})
    await flushPromises()
    const section = wrapper.get('[data-testid="workflow-repos-settings"]')
    await section.get('[data-testid="repo-choice-toggle-p2"]').trigger('click')
    await section.get('[data-testid="repo-choice-toggle-p1"]').trigger('click')
    expect(workflow.defaults!.repos).toEqual([{ projectId: 'p2' }, { projectId: 'p1' }])
    await section.get('[data-testid="repo-choice-branch-p1"]').setValue('release')
    await section.get('[data-testid="repo-choice-branch-p1"]').trigger('change')
    expect(workflow.defaults!.repos).toEqual([{ projectId: 'p2' }, { projectId: 'p1', branch: 'release' }])
    await section.get('[data-testid="repo-choice-toggle-p2"]').trigger('click')
    await section.get('[data-testid="repo-choice-toggle-p1"]').trigger('click')
    expect(workflow.defaults).not.toHaveProperty('repos')
    wrapper.unmount()
  })
  it('keeps a binding to a deleted repository visible so it can be removed', async () => {
    const { workflow, wrapper } = setup({ repos: [{ projectId: 'gone' }] })
    await flushPromises()
    expect(wrapper.get('[data-testid="repo-choice-gone"]').text()).toContain(common.common.repoChoices.deleted)
    await wrapper.get('[data-testid="repo-choice-toggle-gone"]').trigger('click')
    expect(workflow.defaults).not.toHaveProperty('repos')
    wrapper.unmount()
  })
  it('preserves independent variables and repositories on ordinary edits', async () => {
    const original = { variables: [{ name: 'LOCAL', type: 'string', value: 'before' }], repos: [{ projectId: 'p-app', branch: 'main' }] }
    const { workflow, wrapper } = setup(original)
    await flushPromises()
    await wrapper.findAll('input').find((input) => input.attributes('aria-label') === pages.pages.workspaceDefaults.defaultValue)!.setValue('after')
    expect(workflow.defaults).toEqual({ ...original, variables: [{ name: 'LOCAL', type: 'string', value: 'after' }] })
    wrapper.unmount()
  })
  it('does not submit a secret mask as a renamed variable or as a plaintext value', async () => {
    const { workflow, wrapper } = setup({ variables: [{ name: 'TOKEN', type: 'string', value: '****', secret: true, ask: true, required: true, editable: true }] })
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
    const { workflow, wrapper } = setup({ variables: [{ name: 'COUNT', type: 'string', value: '12', desc: 'Keep description', ask: true }] })
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
    const { workflow, wrapper } = setup({ variables: null } as unknown as Workflow['defaults'])
    await flushPromises()
    await wrapper.get(`[aria-label="${pages.pages.workspaceDefaults.addVariable}"]`).trigger('click')
    expect(workflow.defaults).toEqual({ variables: [{ name: '', type: 'string', value: '' }] })
    expect(wrapper.get(`input[aria-label="${pages.pages.workspaceDefaults.variableName}"]`).exists()).toBe(true)
    wrapper.unmount()
  })
  it('inserts a variable row, then removes it only after the row collapses', async () => {
    const { workflow, wrapper } = setup({ variables: [] })
    await flushPromises()
    const motion = readFileSync(join(dirname(fileURLToPath(import.meta.url)), '../../styles/icon-motion.css'), 'utf8')
    expect(motion).toMatch(/@keyframes iconMotion_plus_m20_39998[\s\S]*?scale\(1\.26\)/)
    expect(motion).toMatch(/@keyframes iconMotion_plus_m20_62948[\s\S]*?scale\(1\.26\)/)
    await wrapper.get(`[aria-label="${pages.pages.workspaceDefaults.addVariable}"]`).trigger('click')
    expect(workflow.defaults?.variables).toHaveLength(1)
    const row = wrapper.get('[data-variable-row]')
    expect(row.find('input').exists()).toBe(true)
    await row.get(`[aria-label="${common.common.buttons.delete}"]`).trigger('click')
    expect(workflow.defaults?.variables).toHaveLength(1)
    expect(row.classes()).toContain('is-collapsing')
    row.element.dispatchEvent(new TransitionEvent('transitionend', { propertyName: 'grid-template-rows', bubbles: true }))
    await nextTick()
    expect(workflow.defaults?.variables).toHaveLength(0)
    expect(wrapper.find('[data-variable-row]').exists()).toBe(false)
    wrapper.unmount()
  })
})
