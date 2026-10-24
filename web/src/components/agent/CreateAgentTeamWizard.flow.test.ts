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
const GitPicker = {
  name: 'AgentGitCredentialPicker',
  props: ['modelValue'],
  emits: ['update:modelValue', 'validity'],
  template: '<div class="git-picker-stub" />',
}
function mountWizard(props: Record<string, unknown> = {}) {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
  return mount(CreateAgentTeamWizard, {
    props: { open: true, existingNames: [], ...props },
    global: {
      plugins: [i18n],
      stubs: {
        teleport: true,
        Icon: true,
        AppButton: { template: '<button type="button" v-bind="$attrs"><slot /></button>' },
        ModelBindingEditor: ModelPicker,
        AgentGitCredentialPicker: GitPicker,
        WorkspaceCredentialsPanel: true,
      },
    },
  })
}
function fillBasics(vm: any) {
  vm.draft.teamName = '登月'
  vm.onTeamNameInput()
  vm.draft.background = '把火箭送上天'
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
    expect(vm.draft.pmName).toBe('登月项目经理')
    vm.draft.prefixTouched = true
    vm.draft.prefix = 'Luna'
    vm.onTeamNameInput()
    expect(vm.draft.pmName).toBe('Luna项目经理')
    vm.goNext()
    await flushPromises()
    expect(vm.currentStep.id).toBe('model')
    expect(vm.fieldError).toBe('')
    w.unmount()
  })
  it('rejects an existing PM name', async () => {
    const w = mountWizard({ existingNames: ['登月项目经理'] })
    await flushPromises()
    const vm = w.vm as any
    fillBasics(vm)
    vm.goNext()
    expect(vm.draft.step).toBe(0)
    expect(vm.fieldError).toContain('已存在')
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
    expect(vm.currentStep.id).toBe('git')
    for (const field of ['apiKeyInput', 'acpBackend', 'gitCredentialType']) expect(vm.draft).not.toHaveProperty(field)
    w.unmount()
  })
  it('selects multiple Git credentials and clears them when skipped', async () => {
    const w = mountWizard()
    await flushPromises()
    const vm = w.vm as any
    fillBasics(vm)
    await advance(w, 2)
    const selected = {
      items: [{ credentialId: 'gh' }, { credentialId: 'gl', scope: 'git.example' }],
      preferredCredentialId: 'gl',
    }
    w.findComponent(GitPicker).vm.$emit('update:modelValue', selected)
    w.findComponent(GitPicker).vm.$emit('validity', false)
    vm.goNext()
    expect(vm.currentStep.id).toBe('git')
    vm.draft.gitUrl = 'https://git.example/team/repo.git'
    expect(vm.draft.gitBindings).toEqual(selected)
    vm.goSkip()
    await flushPromises()
    expect(vm.currentStep.id).toBe('mcp')
    expect(vm.draft.gitBindings).toEqual({ items: [] })
    expect(vm.draft.gitUrl).toBe('')
    await advance(w, 2)
    vm.goNext()
    await flushPromises()
    expect(mocks.bootstrapAgentTeam.mock.calls[0][0].gitBindings).toEqual({ items: [] })
    expect(mocks.bootstrapAgentTeam.mock.calls[0][0]).not.toHaveProperty('gitUrl')
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
  it('preserves the Git step and selected model after a healthy resource refresh', async () => {
    const w = mountWizard()
    await flushPromises()
    const vm = w.vm as any
    fillBasics(vm)
    await advance(w, 2)
    expect(mocks.listCredentials).not.toHaveBeenCalled()
    const gitBindings = { items: [{ credentialId: 'git-team' }], preferredCredentialId: 'git-team' }
    vm.draft.gitBindings = gitBindings
    vm.resourceManagerOpen = true
    await vm.refreshResources()
    await flushPromises()
    expect(vm.currentStep.id).toBe('git')
    expect(vm.modelValid).toBe(true)
    expect(vm.draft.modelConfigurationId).toBe('model-team')
    expect(vm.draft.gitBindings).toEqual(gitBindings)
    expect(mocks.listCredentials).toHaveBeenCalledTimes(1)
    expect(mocks.listModelConnections).toHaveBeenCalledTimes(1)
    expect(mocks.listToolCapabilities).toHaveBeenCalledTimes(1)
    await advance(w, 3)
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
    await advance(w, 3)
    expect(vm.currentStep.id).toBe('mcp')
    const original = vm.draft.mcp.length
    vm.restoreArtifact()
    expect(vm.draft.mcp).toHaveLength(original)
    vm.removeMcp(0)
    expect(vm.hasArtifact).toBe(false)
    vm.restoreArtifact()
    expect(vm.hasArtifact).toBe(true)
    vm.addNamedMcp('memory-store')
    vm.addNamedMcp('memory-store')
    expect(vm.draft.mcp.filter((row: any) => row.name === 'memory-store')).toHaveLength(1)
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
  it('submits only selected configuration and Git references with workspace settings', async () => {
    const w = mountWizard()
    await flushPromises()
    const vm = w.vm as any
    fillBasics(vm)
    await advance(w, 2)
    w.findComponent(GitPicker).vm.$emit('update:modelValue', {
      items: [{ credentialId: 'git-team' }],
      preferredCredentialId: 'git-team',
    })
    await advance(w, 3)
    expect(vm.currentStep.id).toBe('review')
    expect(w.text()).toContain('Team model')
    vm.goNext()
    await flushPromises()
    expect(mocks.bootstrapAgentTeam).toHaveBeenCalledTimes(1)
    expect(mocks.bootstrapAgentTeam.mock.calls[0][0]).toMatchObject({
      pmName: '登月项目经理',
      modelConfigurationId: 'model-team',
      gitBindings: { items: [{ credentialId: 'git-team' }], preferredCredentialId: 'git-team' },
      env: { GIT_REPOS: '${vars.repos}' },
    })
    for (const field of ['teamName', 'apiKey', 'acpBackend', 'gitCredentialType'])
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
    await advance(w, 5)
    mocks.bootstrapAgentTeam.mockRejectedValueOnce(new Error('quota exhausted'))
    vm.goNext()
    await flushPromises()
    expect(vm.submitError).toBe('quota exhausted')
    expect(w.emitted('started')).toBeFalsy()
    vm.draft.background = ''
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
