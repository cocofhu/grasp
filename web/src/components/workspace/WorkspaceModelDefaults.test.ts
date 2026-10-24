// @vitest-environment happy-dom
import { defineComponent, onMounted } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import type { WorkspaceModelDefaults as Defaults } from '@/lib/api/api'
import AppSelect from '@/components/ui/AppSelect.vue'
import WorkspaceModelDefaults from './WorkspaceModelDefaults.vue'

const mocks = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn(), success: vi.fn() }))
vi.mock('@/lib/api/api', () => ({ api: { getModelDefaults: mocks.get, putModelDefaults: mocks.put } }))
vi.mock('@/lib/composables/useToast', () => ({ useToast: () => ({ success: mocks.success }) }))
const initial: Defaults = { bindingsByBackend: { cursor: { kind: 'platform', credentialId: 'missing-platform-key' }, opencode: { kind: 'connection', connectionId: 'missing-service', modelId: 'vendor/raw/model' } } }
const BindingStub = defineComponent({
  props: ['backend', 'modelValue', 'allowInherit', 'directOnly'],
  emits: ['update:modelValue', 'validity'],
  setup(_, { emit }) { onMounted(() => emit('validity', true)); return {} },
  template: '<div data-testid="default-binding-editor" />',
})
function setup() {
  return mount(WorkspaceModelDefaults, { global: { plugins: [createI18n({ legacy: false, locale: 'en', messages: { en: {} } })], stubs: { Teleport: true, ModelBindingEditor: BindingStub } } })
}
beforeEach(() => {
  vi.clearAllMocks()
  mocks.get.mockResolvedValue(structuredClone(initial))
  mocks.put.mockImplementation(async (body: Defaults) => body)
})
describe('Workspace model defaults', () => {
  it('searches coding tools with their logos and preserves an explicit unavailable source', async () => {
    const wrapper = setup()
    await flushPromises()
    const picker = wrapper.get('[data-testid="workspace-model-default-backend"]')
    expect(picker.find('[data-provider-logo="cursor"]').exists()).toBe(true)
    await picker.get('[data-test="app-select-trigger"]').trigger('click')
    await wrapper.get('[data-test="app-select-search"]').setValue('OpenCode')
    expect(wrapper.find('[data-test="app-select-option-cursor"]').exists()).toBe(false)
    const option = wrapper.get('[data-test="app-select-option-opencode"]')
    expect(option.find('[data-provider-logo="opencode"]').exists()).toBe(true)
    await option.trigger('click')
    const editor = wrapper.getComponent(BindingStub)
    expect(editor.props('backend')).toBe('opencode')
    expect(editor.props('modelValue')).toEqual(initial.bindingsByBackend.opencode)
    expect(editor.props('allowInherit')).toBe(false)
    expect(picker.find('[data-provider-logo="opencode"]').exists()).toBe(true)
    expect(wrapper.find('select').exists()).toBe(false)
    wrapper.unmount()
  })

  it('keeps unsaved binding edits when reselecting the current tool and merges other defaults on save', async () => {
    const wrapper = setup()
    await flushPromises()
    const draft = { kind: 'platform', credentialId: 'replacement-key', modelId: 'selected-model' }
    wrapper.getComponent(BindingStub).vm.$emit('update:modelValue', draft)
    wrapper.getComponent(AppSelect).vm.$emit('update:modelValue', 'cursor')
    await flushPromises()
    expect(wrapper.getComponent(BindingStub).props('modelValue')).toEqual(draft)
    const concurrent = { kind: 'connection' as const, connectionId: 'new-codex-service', modelId: 'another/model' }
    mocks.get.mockResolvedValue({ bindingsByBackend: { ...initial.bindingsByBackend, codex: concurrent } })
    await wrapper.get('[data-testid="workspace-model-default-save"]').trigger('click')
    await flushPromises()
    expect(mocks.put).toHaveBeenCalledWith({ bindingsByBackend: { ...initial.bindingsByBackend, cursor: draft, codex: concurrent } })
    wrapper.unmount()
  })
})
