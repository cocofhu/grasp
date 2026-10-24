// @vitest-environment happy-dom
import { defineComponent, nextTick, reactive } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useAgentCreateWizard } from './useAgentCreateWizard'
const mocks = vi.hoisted(() => ({ createAgent: vi.fn(), listAgentTeamTemplates: vi.fn(), listCredentials: vi.fn(), listModelConfigurations: vi.fn() }))
vi.mock('@/lib/api/api', () => ({ api: mocks }))
const config = { id: 'cfg', name: 'Team model', backend: 'cursor', state: 'active' }
const mounted: ReturnType<typeof mount>[] = []
function setup(open = true, existingNames: string[] = []) {
 let w!: ReturnType<typeof useAgentCreateWizard>
 const emit = vi.fn(); const props = reactive({ open, existingNames })
 const wrapper = mount(defineComponent({ setup() { w = useAgentCreateWizard(props, emit as never); return () => null } }), { global: { plugins: [createI18n({ legacy: false, locale: 'en', messages: { en: {} } })] } })
 mounted.push(wrapper); return { w, props, emit }
}
beforeEach(() => {
 vi.clearAllMocks(); mocks.createAgent.mockResolvedValue({ name: 'created' }); mocks.listAgentTeamTemplates.mockResolvedValue({ items: [] })
 mocks.listCredentials.mockResolvedValue({ items: [{ id: 'gh', name: 'GitHub team' }] }); mocks.listModelConfigurations.mockResolvedValue({ items: [config] })
})
afterEach(() => { mounted.splice(0).forEach(w => w.unmount()); document.body.innerHTML = '' })
describe('Agent wizard with explicit saved model', () => {
 it('inherits by default and turns off only model inheritance after an explicit local model choice', async () => {
  const { w } = setup(); await flushPromises()
  expect(w.draft.value).toMatchObject({ inheritModel: true, inheritGit: true })
  w.draft.value.modelBinding = { kind: 'configuration', configurationId: 'cfg' }
  expect(w.draft.value).toMatchObject({ inheritModel: false, inheritGit: true })
  expect(w.effectiveModel.value?.configurationId).toBe('cfg')
 })
 it('resets on opening and focuses the name', async () => {
  const input = document.createElement('input'); input.id = 'wiz-name-input'; document.body.append(input)
  const { w, props } = setup(false); w.draft.value.name = 'old'; w.createError.value = 'old'; props.open = true
  await nextTick(); await nextTick(); expect(w.draft.value.name).toBe(''); expect(w.createError.value).toBe(''); expect(document.activeElement).toBe(input)
 })
 it('validates names, then blocks progression until an active matching model is selected', async () => {
  const { w } = setup(true, ['taken']); await flushPromises()
  w.goNext(); expect(w.nameError.value).toContain('nameRequired')
  w.draft.value.name = 'bad name'; w.goNext(); expect(w.nameError.value).toContain('nameInvalid')
  w.draft.value.name = 'taken'; w.goNext(); expect(w.nameError.value).toContain('nameExists')
  w.draft.value.name = 'new'; w.goNext(); expect(w.currentStep.value.id).toBe('acp')
  w.goNext(); expect(w.currentStep.value.id).toBe('acp'); expect(w.createError.value).toContain('missing or disabled')
  w.draft.value.modelBinding = { kind: 'configuration', configurationId: 'cfg' }; w.goNext(); expect(w.currentStep.value.id).toBe('git')
 })
 it('requires tool confirmation only for path-dependent content and clears the model on tool changes', async () => {
  const { w } = setup(); await flushPromises()
  w.draft.value.modelBinding = { kind: 'configuration', configurationId: 'cfg' }; w.draft.value.rulesEdited = true
  w.selectAcp('opencode'); expect(w.showAcpConfirm.value).toBe(true); expect(w.draft.value.acpBackend).toBe('cursor')
  w.cancelAcpSwitch(); expect(w.showAcpConfirm.value).toBe(false)
  w.selectAcp('codebuddy'); w.confirmAcpSwitch()
  expect(w.draft.value.acpBackend).toBe('codebuddy'); expect(w.draft.value.modelBinding?.configurationId).toBe('')
  w.selectRegion('internal'); expect(w.currentRegion.value).toBe('internal')
 })
 it('clones multiple Git authorizations, reviews aliases and supports an explicit skip', async () => {
  const { w } = setup(); await flushPromises()
  const git = { items: [{ credentialId: 'gh' }, { credentialId: 'ssh' }], preferredCredentialId: 'gh' }
  w.onGitBindings(git); git.items[0].credentialId = 'changed'
  expect(w.draft.value.inheritGit).toBe(false)
  expect(w.draft.value.gitBindings?.items[0].credentialId).toBe('gh')
  expect(w.reviewItems.value.find(row => row.key === 'git')?.detail).toContain('GitHub team')
  w.draft.value.step = 2; w.goSkip(); expect(w.currentStep.value.id).toBe('review'); expect(w.draft.value.gitBindings).toEqual({ items: [] })
  w.goPrev(); expect(w.currentStep.value.id).toBe('git')
 })
 it('creates with the selected configuration and reports API errors without closing', async () => {
  const { w, emit } = setup(); await flushPromises()
  w.draft.value.name = 'valid'; w.draft.value.modelBinding = { kind: 'configuration', configurationId: 'cfg' }
  mocks.createAgent.mockRejectedValueOnce(new Error('server error')); await w.submitCreate()
  expect(w.createError.value).toBe('server error'); expect(emit).not.toHaveBeenCalled(); expect(w.creating.value).toBe(false)
  await w.submitCreate(); expect(mocks.createAgent.mock.calls[0][0]).toMatchObject({ modelBinding: { kind: 'configuration', configurationId: 'cfg' }, gitBindings: { items: [] } })
  expect(emit).toHaveBeenCalledWith('created', { name: 'created' }); expect(emit).toHaveBeenCalledWith('close')
 })
 it('redirects invalid submission to the required step and never substitutes another tool', async () => {
  const { w } = setup(); await flushPromises(); await w.submitCreate(); expect(w.draft.value.step).toBe(0)
  w.draft.value.name = 'valid'; w.draft.value.acpBackend = 'opencode'; w.draft.value.modelBinding = { kind: 'configuration', configurationId: 'cfg' }
  await w.submitCreate(); expect(w.draft.value.step).toBe(1); expect(w.createError.value).toContain('different coding tool'); expect(mocks.createAgent).not.toHaveBeenCalled()
 })
 it('loads templates, keeps role pack names explicit and tolerates option loading errors', async () => {
  mocks.listAgentTeamTemplates.mockResolvedValue({ items: [{ id: 'planner', name: 'Planner' }] })
  const { w } = setup(); await flushPromises(); w.draft.value.description = 'old'; w.onTemplateSelect('planner')
  expect(w.showDescField.value).toBe(false); expect(w.draft.value.description).toBe('')
  expect(w.draft.value.name).toBe('')
  mocks.listModelConfigurations.mockRejectedValue(new Error('offline')); const failed = setup(); await flushPromises()
  failed.w.draft.value.name = 'valid'; await failed.w.submitCreate(); expect(mocks.createAgent).not.toHaveBeenCalled()
 })
 it('locks navigation and close while creating', async () => {
  const { w, emit } = setup(); w.creating.value = true; w.goNext(); w.goPrev(); w.goSkip(); w.close()
  expect(w.draft.value.step).toBe(0); expect(emit).not.toHaveBeenCalled()
  w.creating.value = false; w.close(); expect(emit).toHaveBeenCalledWith('close')
 })
})
