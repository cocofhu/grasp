// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import CreateAgentTeamWizard from './CreateAgentTeamWizard.vue'

const mocks = vi.hoisted(() => ({
  bootstrapAgentTeam: vi.fn(),
  listModelConfigurations: vi.fn(),
  listCredentials: vi.fn(),
  listModelConnections: vi.fn(),
  listToolCapabilities: vi.fn(),
  listMcpPresets: vi.fn(async () => ({
    presets: [{ id: 'artifact-store', requiresDesktop: false, server: { name: 'artifact-store', url: '${GRASP_ARTIFACT_URL}', headers: { Authorization: 'Bearer ${GRASP_ARTIFACT_TOKEN}' } } }],
  })),
}))
vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return { ...actual, api: { ...actual.api, ...mocks } }
})
const ModelPicker = {
  name: 'ModelBindingEditor',
  props: ['modelValue'],
  emits: ['update:modelValue', 'validity'],
  template: '<div class="model-picker-stub" />',
}
function mountWizard(props: Record<string, unknown> = {}) {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
  return mount(CreateAgentTeamWizard, {
    props: { open: true, ...props },
    global: {
      plugins: [i18n],
      stubs: {
        teleport: true,
        Icon: true,
        AppButton: { template: '<button type="button" v-bind="$attrs"><slot /></button>' },
        ModelBindingEditor: ModelPicker,
        WorkspaceCredentialsPanel: true,
      },
    },
  })
}
function fillBasics(vm: any) {
  vm.draft.teamName = '登月'
  vm.onTeamNameInput()
}
async function advance(w: ReturnType<typeof mountWizard>, count: number) {
  const vm = w.vm as any
  for (let i = 0; i < count; i++) {
    if (vm.currentStep.id === 'model') {
      w.findComponent(ModelPicker).vm.$emit('update:modelValue', {
        kind: 'configuration',
        configurationId: 'model-team',
      })
      w.findComponent(ModelPicker).vm.$emit('validity', true)
    }
    vm.goNext()
    await flushPromises()
  }
}
beforeEach(() => {
  vi.clearAllMocks()
  mocks.listModelConfigurations.mockResolvedValue({
    items: [
      {
        id: 'model-team',
        name: 'Team model',
        backend: 'opencode',
        binding: { kind: 'connection', connectionId: 'connection', modelId: 'coding' },
        state: 'active',
        revision: 1,
      },
    ],
  })
  mocks.listCredentials.mockResolvedValue({
    items: [{ id: 'model-key', purpose: 'model_api', configured: true }],
  })
  mocks.listModelConnections.mockResolvedValue({
    items: [
      {
        id: 'connection',
        state: 'active',
        credentialId: 'model-key',
        protocol: 'openai-chat-completions',
        auth: { scheme: 'bearer' },
        models: [{ id: 'coding' }],
      },
    ],
  })
  mocks.listToolCapabilities.mockResolvedValue({
    items: [
      { toolId: 'opencode', adapterState: 'existing', modes: ['connection'], protocols: ['openai-chat-completions'] },
    ],
  })
  mocks.bootstrapAgentTeam.mockResolvedValue({ sessionId: 's1' })
})
describe('CreateAgentTeamWizard flow', () => {
  it('requires team basics and preserves edited derived names', async () => {
    const w = mountWizard()
    await flushPromises()
    const vm = w.vm as any
    vm.goNext()
    expect(vm.draft.step).toBe(0)
    expect(vm.fieldError).toBeTruthy()
    fillBasics(vm)
    expect(vm.draft.prefix).toBe('登月')
    vm.draft.prefixTouched = true
    vm.draft.prefix = 'Luna'
    vm.onTeamNameInput()
    expect(vm.draft.prefix).toBe('Luna')
    vm.goNext()
    await flushPromises()
    expect(vm.currentStep.id).toBe('model')
    expect(vm.fieldError).toBe('')
    w.unmount()
  })
  it('requires a valid model configuration and has no inline key or backend source', async () => {
    const w = mountWizard()
    await flushPromises()
    const vm = w.vm as any
    fillBasics(vm)
    await advance(w, 1)
    vm.goNext()
    expect(vm.currentStep.id).toBe('model')
    w.findComponent(ModelPicker).vm.$emit('update:modelValue', { kind: 'configuration', configurationId: 'model-team' })
    w.findComponent(ModelPicker).vm.$emit('validity', false)
    vm.goNext()
    expect(vm.currentStep.id).toBe('model')
    w.findComponent(ModelPicker).vm.$emit('validity', true)
    vm.goNext()
    await flushPromises()
    expect(vm.currentStep.id).toBe('mcp')
    for (const field of ['apiKeyInput', 'acpBackend', 'gitCredentialType']) expect(vm.draft).not.toHaveProperty(field)
    w.unmount()
  })
  it.each(['configuration', 'credential', 'connection'])(
    'returns to model selection when the selected %s becomes unavailable in the resource manager',
    async (resource) => {
      const w = mountWizard()
      await flushPromises()
      const vm = w.vm as any
      fillBasics(vm)
      await advance(w, 2)
      if (resource === 'configuration') mocks.listModelConfigurations.mockResolvedValue({ items: [] })
      if (resource === 'credential')
        mocks.listCredentials.mockResolvedValue({
          items: [{ id: 'model-key', purpose: 'model_api', configured: true, revokedAt: '2026-10-24T00:00:00Z' }],
        })
      if (resource === 'connection') mocks.listModelConnections.mockResolvedValue({ items: [] })
      vm.resourceManagerOpen = true
      const refreshing = vm.refreshResources()
      expect(vm.modelValid).toBe(false)
      await refreshing
      await flushPromises()
      expect(vm.currentStep.id).toBe('model')
      expect(vm.draft.modelConfigurationId).toBe('model-team')
      expect(vm.modelValid).toBe(false)
      await vm.submit()
      expect(mocks.bootstrapAgentTeam).not.toHaveBeenCalled()
      w.unmount()
    },
  )
  it('preserves the current step and selected model after a healthy resource refresh', async () => {
    const w = mountWizard()
    await flushPromises()
    const vm = w.vm as any
    fillBasics(vm)
    await advance(w, 2)
    expect(mocks.listCredentials).not.toHaveBeenCalled()
    vm.resourceManagerOpen = true
    await vm.refreshResources()
    await flushPromises()
    expect(vm.currentStep.id).toBe('mcp')
    expect(vm.modelValid).toBe(true)
    expect(vm.draft.modelConfigurationId).toBe('model-team')
    expect(mocks.listCredentials).toHaveBeenCalledTimes(1)
    expect(mocks.listModelConnections).toHaveBeenCalledTimes(1)
    expect(mocks.listToolCapabilities).toHaveBeenCalledTimes(1)
    await advance(w, 2)
    await vm.submit()
    expect(mocks.bootstrapAgentTeam.mock.calls[0][0].modelConfigurationId).toBe('model-team')
    w.unmount()
  })
  it('blocks a previously valid model when resource refresh fails', async () => {
    const w = mountWizard()
    await flushPromises()
    const vm = w.vm as any
    fillBasics(vm)
    await advance(w, 2)
    mocks.listCredentials.mockRejectedValueOnce(new Error('offline'))
    vm.resourceManagerOpen = true
    await vm.refreshResources()
    expect(vm.currentStep.id).toBe('model')
    expect(vm.modelValid).toBe(false)
    await vm.submit()
    expect(mocks.bootstrapAgentTeam).not.toHaveBeenCalled()
    w.unmount()
  })
  it('edits MCP presets and searches transport choices without discarding connection values', async () => {
    const w = mountWizard()
    await flushPromises()
    const vm = w.vm as any
    fillBasics(vm)
    await advance(w, 2)
    expect(vm.currentStep.id).toBe('mcp')
    const original = vm.draft.mcp.length
    vm.restoreArtifact()
    expect(vm.draft.mcp).toHaveLength(original)
    vm.removeMcp(0)
    expect(vm.hasArtifact).toBe(false)
    vm.restoreArtifact()
    expect(vm.hasArtifact).toBe(true)
    vm.addMcp()
    const index = vm.draft.mcp.length - 1
    const service = vm.draft.mcp[index]
    Object.assign(service, {
      name: 'custom',
      url: 'https://mcp.example',
      command: 'npx',
      args: 'example',
      headers: [{ k: 'X-Test', v: 'value' }],
    })
    await flushPromises()
    await w.get(`[data-testid="team-mcp-transport-${index}"] [data-test="app-select-trigger"]`).trigger('click')
    await w.get('[data-test="app-select-search"]').setValue('stdio')
    expect(w.find('[data-test="app-select-option-url"]').exists()).toBe(false)
    await w.get('[data-test="app-select-option-command"]').trigger('click')
    expect(service.transport).toBe('command')
    expect(service.url).toBe('https://mcp.example')
    expect(service.headers).toEqual([{ k: 'X-Test', v: 'value' }])
    w.unmount()
  })
  it('submits only the selected configuration reference with workspace settings', async () => {
    const w = mountWizard()
    await flushPromises()
    const vm = w.vm as any
    fillBasics(vm)
    await advance(w, 4)
    expect(vm.currentStep.id).toBe('review')
    expect(w.text()).toContain('Team model')
    vm.goNext()
    await flushPromises()
    expect(mocks.bootstrapAgentTeam).toHaveBeenCalledTimes(1)
    expect(mocks.bootstrapAgentTeam.mock.calls[0][0]).toMatchObject({
      prefix: '登月',
      modelConfigurationId: 'model-team',
      env: {},
    })
    for (const field of ['teamName', 'repoId', 'apiKey', 'acpBackend', 'gitCredentialType', 'gitBindings'])
      expect(mocks.bootstrapAgentTeam.mock.calls[0][0]).not.toHaveProperty(field)
    expect(w.emitted('started')?.[0]).toEqual([{ sessionId: 's1' }])
    expect(w.emitted('close')).toHaveLength(1)
    w.unmount()
  })
  it('surfaces server errors and revalidates basics before submitting', async () => {
    const w = mountWizard()
    await flushPromises()
    const vm = w.vm as any
    fillBasics(vm)
    await advance(w, 4)
    mocks.bootstrapAgentTeam.mockRejectedValueOnce(new Error('quota exhausted'))
    vm.goNext()
    await flushPromises()
    expect(vm.submitError).toBe('quota exhausted')
    expect(w.emitted('started')).toBeFalsy()
    vm.draft.prefix = ''
    await vm.submit()
    expect(vm.draft.step).toBe(0)
    expect(mocks.bootstrapAgentTeam).toHaveBeenCalledTimes(1)
    w.unmount()
  })
  it('freezes navigation while submitting and resets on reopen', async () => {
    const w = mountWizard()
    await flushPromises()
    const vm = w.vm as any
    fillBasics(vm)
    await advance(w, 1)
    vm.submitting = true
    vm.goNext()
    vm.goPrev()
    vm.goSkip()
    vm.close()
    expect(vm.draft.step).toBe(1)
    expect(w.emitted('close')).toBeFalsy()
    vm.submitting = false
    await w.setProps({ open: false })
    await w.setProps({ open: true })
    await flushPromises()
    expect(vm.draft.step).toBe(0)
    expect(vm.draft.modelConfigurationId).toBe('')
    expect(vm.draft.teamName).toBe('')
    w.unmount()
  })
  it('renders nothing when closed', async () => {
    const w = mountWizard({ open: false })
    await flushPromises()
    expect(w.find('.wiz-root').exists()).toBe(false)
    w.unmount()
  })
})
