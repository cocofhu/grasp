// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import CreateAgentTeamWizard from './CreateAgentTeamWizard.vue'

const mocks = vi.hoisted(() => ({
  bootstrapAgentTeam: vi.fn(),
  getProjectSharedAgentConfig: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      bootstrapAgentTeam: mocks.bootstrapAgentTeam,
      getProjectSharedAgentConfig: mocks.getProjectSharedAgentConfig,
    },
  }
})

/** Stubs that keep the child contract observable instead of swallowing it. */
const ApiKeyStub = {
  name: 'WizardApiKeyStepPanel',
  props: [
    'acpBackend',
    'configRoot',
    'authMode',
    'apiKeyInput',
    'customConfigContent',
    'customConfigError',
    'authGuide',
  ],
  emits: ['update:authMode', 'update:apiKeyInput', 'update:customConfigContent'],
  template: '<div class="api-key-stub" />',
}

const GitGuideStub = {
  name: 'AgentGitGuide',
  props: ['env', 'inheritedEnv', 'allowTokenRecommend', 'upsertEnv', 'credentialType'],
  emits: ['update:credentialType'],
  template: '<div class="git-guide-stub" />',
}

function mountWizard(props: Record<string, unknown> = {}) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(CreateAgentTeamWizard, {
    props: { open: true, existingNames: [], ...props },
    global: {
      plugins: [i18n],
      stubs: {
        teleport: true,
        Icon: true,
        AppButton: { template: '<button type="button" v-bind="$attrs"><slot /></button>' },
        AgentGitGuide: GitGuideStub,
        WizardApiKeyStepPanel: ApiKeyStub,
      },
    },
  })
}

/** The step pane re-creates its children on every render; always re-find before emitting. */
function emitPanel(w: ReturnType<typeof mountWizard>, event: string, payload: unknown) {
  w.findComponent(ApiKeyStub).vm.$emit(event, payload)
}

/** Fill the required basics so step 0 validation passes. */
function fillBasics(vm: any, name = '登月') {
  vm.draft.projectName = name
  vm.onProjectInput()
  vm.draft.background = '把火箭送上天'
}

/** OpenCode API Key step requires a model before Next (g1.3 default). */
function ensureOpenCodeModel(vm: any) {
  if (vm.draft.acpBackend !== 'opencode') return
  const has = vm.draft.env.some((e: any) => e.k === 'ACP_BRIDGE_MODEL' && String(e.v || '').trim())
  if (!has) vm.draft.env.push({ k: 'ACP_BRIDGE_MODEL', v: 'openai/gpt-4.1' })
}

function goNextThrough(vm: any, count: number) {
  for (let i = 0; i < count; i++) {
    if (vm.currentStep.id === 'apiKey') ensureOpenCodeModel(vm)
    vm.goNext()
  }
}

describe('CreateAgentTeamWizard flow', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.getProjectSharedAgentConfig.mockResolvedValue({ env: {}, files: [], mcp: [], layout: {} })
    mocks.bootstrapAgentTeam.mockResolvedValue({ sessionId: 's1' })
  })

  it('derives names from the project name and blocks Next until basics are valid', async () => {
    const w = mountWizard()
    await flushPromises()
    const vm = w.vm as any

    // Empty project name fails validation and keeps the wizard on step 0.
    vm.goNext()
    await flushPromises()
    expect(vm.draft.step).toBe(0)
    expect(vm.fieldError).toBeTruthy()

    fillBasics(vm)
    await flushPromises()
    expect(vm.draft.prefix).toBe('登月')
    expect(vm.draft.rootGroupName).toBe('登月项目组')
    expect(vm.draft.pmName).toBe('登月项目经理')
    expect(vm.previewLine).toContain('登月项目组')

    // A touched prefix wins over the project name for derived values.
    vm.draft.prefixTouched = true
    vm.draft.prefix = 'Luna'
    vm.onProjectInput()
    expect(vm.draft.rootGroupName).toBe('Luna项目组')

    vm.goNext()
    await flushPromises()
    expect(vm.draft.step).toBe(1)
    expect(vm.fieldError).toBe('')

    w.unmount()
  })

  it('rejects a PM name that collides with an existing agent', async () => {
    const w = mountWizard({ existingNames: ['登月项目经理'] })
    await flushPromises()
    const vm = w.vm as any

    fillBasics(vm)
    vm.goNext()
    await flushPromises()
    expect(vm.draft.step).toBe(0)
    expect(vm.fieldError).toContain('已存在')

    vm.draft.pmTouched = true
    vm.draft.pmName = '登月PM'
    vm.goNext()
    await flushPromises()
    expect(vm.draft.step).toBe(1)

    w.unmount()
  })

  it('switches the ACP backend and its region env key', async () => {
    const w = mountWizard()
    await flushPromises()
    const vm = w.vm as any

    fillBasics(vm)
    vm.goNext()
    await flushPromises()
    expect(vm.currentStep.id).toBe('acp')

    // Re-selecting the active backend is a no-op.
    const before = vm.draft.configRoot
    vm.selectAcp(vm.draft.acpBackend)
    expect(vm.draft.configRoot).toBe(before)
    expect(vm.draft.acpBackend).toBe('opencode')
    expect(vm.draft.startPath).toBe('apiKey')

    vm.selectStartPath('cli')
    await flushPromises()
    expect(vm.draft.startPath).toBe('cli')
    expect(vm.draft.acpBackend).toBe('cursor')

    vm.selectAcp('claude_code')
    await flushPromises()
    expect(vm.draft.acpBackend).toBe('claude_code')
    expect(vm.draft.configRoot).not.toBe(before)

    // Backends with a region policy expose selectable regions.
    const withRegion = ['claude_code', 'codebuddy', 'trae', 'cursor'].find((id) => {
      vm.selectAcp(id)
      return !!vm.regionPolicy
    })
    if (withRegion) {
      const option = vm.regionPolicy.options[0]
      vm.selectRegion(option.id)
      await flushPromises()
      expect(vm.currentRegion).toBe(option.id)
    }

    w.unmount()
  })

  it('keeps the api key in sync with the env rows and clears it on skip', async () => {
    const w = mountWizard()
    await flushPromises()
    const vm = w.vm as any

    fillBasics(vm)
    vm.goNext()
    vm.goNext()
    await flushPromises()
    expect(vm.currentStep.id).toBe('apiKey')

    const key = vm.primaryAuthKey
    expect(key).toBeTruthy()

    emitPanel(w, 'update:apiKeyInput', 'sk-live')
    await flushPromises()
    expect(vm.draft.env.find((e: any) => e.k === key)?.v).toBe('sk-live')

    // Writing the same key again updates in place rather than appending.
    emitPanel(w, 'update:apiKeyInput', 'sk-live-2')
    await flushPromises()
    expect(vm.draft.env.filter((e: any) => e.k === key)).toHaveLength(1)

    // Blanking the input removes the row entirely.
    emitPanel(w, 'update:apiKeyInput', '   ')
    await flushPromises()
    expect(vm.draft.env.some((e: any) => e.k === key)).toBe(false)

    emitPanel(w, 'update:apiKeyInput', 'sk-live')
    await flushPromises()
    vm.goSkip()
    await flushPromises()
    expect(vm.draft.skipped.apiKey).toBe(true)
    expect(vm.draft.env.some((e: any) => e.k === key)).toBe(false)
    expect(vm.apiKeyInput).toBe('')

    w.unmount()
  })

  it('validates custom config JSON before leaving the api key step', async () => {
    const w = mountWizard()
    await flushPromises()
    const vm = w.vm as any

    fillBasics(vm)
    vm.goNext()
    vm.goNext()
    await flushPromises()

    emitPanel(w, 'update:apiKeyInput', 'sk-live')
    await flushPromises()

    // Switching to custom config strips the auth keys and seeds a placeholder.
    emitPanel(w, 'update:authMode', 'customConfig')
    await flushPromises()
    expect(vm.draft.authMode).toBe('customConfig')
    expect(vm.apiKeyInput).toBe('')
    expect(vm.draft.customConfigContent).toBeTruthy()

    // Re-emitting the current mode is a no-op.
    const seeded = vm.draft.customConfigContent
    emitPanel(w, 'update:authMode', 'customConfig')
    expect(vm.draft.customConfigContent).toBe(seeded)

    emitPanel(w, 'update:customConfigContent', '{ not json')
    await flushPromises()
    expect(vm.customConfigError).toBe(false)

    vm.goNext()
    await flushPromises()
    expect(vm.customConfigError).toBe(true)
    expect(vm.currentStep.id).toBe('apiKey')

    emitPanel(w, 'update:customConfigContent', '{"model":"x"}')
    await flushPromises()
    vm.goNext()
    await flushPromises()
    expect(vm.currentStep.id).toBe('git')

    // Going back restores the api-key input from env; toggling back keeps the draft.
    vm.goPrev()
    await flushPromises()
    expect(vm.currentStep.id).toBe('apiKey')
    emitPanel(w, 'update:authMode', 'apiKey')
    await flushPromises()
    expect(vm.draft.authMode).toBe('apiKey')
    expect(vm.draft.customConfigContent).toBe('')

    emitPanel(w, 'update:authMode', 'customConfig')
    await flushPromises()
    expect(vm.draft.customConfigContent).toBe('{"model":"x"}')

    w.unmount()
  })

  it('collects git url and credential type from the guide', async () => {
    const w = mountWizard({ projectId: 'proj-a' })
    await flushPromises()
    const vm = w.vm as any
    expect(mocks.getProjectSharedAgentConfig).toHaveBeenCalledWith('proj-a')

    fillBasics(vm)
    vm.goNext()
    vm.goNext()
    ensureOpenCodeModel(vm)
    vm.goNext()
    await flushPromises()
    expect(vm.currentStep.id).toBe('git')

    vm.draft.gitUrl = 'https://github.com/org/repo.git'
    const guide = w.findComponent(GitGuideStub)
    guide.props('upsertEnv')('GIT_USERNAME', 'bot')
    guide.props('upsertEnv')('GIT_USERNAME', 'bot2')
    guide.vm.$emit('update:credentialType', 'token')
    await flushPromises()

    expect(vm.draft.gitCredentialType).toBe('token')
    expect(vm.draft.env.filter((e: any) => e.k === 'GIT_USERNAME')).toHaveLength(1)
    expect(vm.draft.env.find((e: any) => e.k === 'GIT_USERNAME')?.v).toBe('bot2')

    w.unmount()
  })

  it('edits the MCP list with presets, transports and removal', async () => {
    const w = mountWizard()
    await flushPromises()
    const vm = w.vm as any

    fillBasics(vm)
    goNextThrough(vm, 4)
    await flushPromises()
    expect(vm.currentStep.id).toBe('mcp')
    expect(vm.hasArtifact).toBe(true)

    // artifact-store is already present, so restoring is a no-op.
    const count = vm.draft.mcp.length
    vm.restoreArtifact()
    expect(vm.draft.mcp).toHaveLength(count)

    vm.removeMcp(0)
    expect(vm.hasArtifact).toBe(false)
    vm.restoreArtifact()
    expect(vm.hasArtifact).toBe(true)

    vm.addNamedMcp('memory-store')
    vm.addNamedMcp('memory-store')
    expect(vm.draft.mcp.filter((m: any) => m.name === 'memory-store')).toHaveLength(1)

    vm.addMcp()
    await flushPromises()
    expect(vm.draft.mcp.at(-1).name).toBe('')
    expect(vm.mcpNames).toContain('memory-store')

    // Unnamed rows are dropped from the payload.
    const rendered = w.html()
    expect(rendered).toContain('MCP #1')

    w.unmount()
  })

  it('summarises env rows on the review step', async () => {
    const w = mountWizard()
    await flushPromises()
    const vm = w.vm as any

    fillBasics(vm)
    goNextThrough(vm, 5)
    await flushPromises()
    expect(vm.currentStep.id).toBe('env')
    expect(vm.envSummary).toContain('GIT_REPOS')

    vm.draft.env = []
    await flushPromises()
    expect(vm.envSummary).toBe('无')

    vm.draft.mcp = []
    expect(vm.mcpNames).toBe('无')

    w.unmount()
  })

  it('submits the assembled payload and emits started', async () => {
    const w = mountWizard()
    await flushPromises()
    const vm = w.vm as any

    fillBasics(vm)
    vm.goNext()
    vm.goNext()
    await flushPromises()
    emitPanel(w, 'update:apiKeyInput', 'sk-live')
    await flushPromises()
    goNextThrough(vm, 4)
    await flushPromises()
    expect(vm.currentStep.id).toBe('review')
    expect(w.html()).toContain('登月项目组')

    vm.bgExpanded = false
    await flushPromises()
    expect(w.html()).not.toContain('把火箭送上天')

    vm.goNext()
    await flushPromises()

    expect(mocks.bootstrapAgentTeam).toHaveBeenCalledTimes(1)
    const payload = mocks.bootstrapAgentTeam.mock.calls[0][0]
    expect(payload.projectName).toBe('登月')
    expect(payload.pmName).toBe('登月项目经理')
    expect(payload.rootGroupName).toBe('登月项目组')
    expect(payload.apiKey).toBe('sk-live')
    expect(w.emitted('started')).toBeTruthy()
    expect(w.emitted('close')).toBeTruthy()
    expect(vm.submitting).toBe(false)

    w.unmount()
  })

  it('surfaces a bootstrap failure and keeps the wizard open', async () => {
    const w = mountWizard()
    await flushPromises()
    const vm = w.vm as any
    mocks.bootstrapAgentTeam.mockRejectedValueOnce(new Error('quota exhausted'))

    fillBasics(vm)
    goNextThrough(vm, 6)
    await flushPromises()
    expect(vm.currentStep.id).toBe('review')

    vm.goNext()
    await flushPromises()
    expect(vm.submitError).toBe('quota exhausted')
    expect(vm.submitting).toBe(false)
    expect(w.emitted('started')).toBeFalsy()
    expect(w.html()).toContain('quota exhausted')

    w.unmount()
  })

  it('jumps back to step 0 when review-time validation fails', async () => {
    const w = mountWizard()
    await flushPromises()
    const vm = w.vm as any

    fillBasics(vm)
    goNextThrough(vm, 6)
    await flushPromises()
    expect(vm.currentStep.id).toBe('review')

    vm.draft.background = '  '
    await vm.submit()
    await flushPromises()
    expect(vm.draft.step).toBe(0)
    expect(vm.fieldError).toBeTruthy()
    expect(mocks.bootstrapAgentTeam).not.toHaveBeenCalled()

    w.unmount()
  })

  it('freezes navigation while submitting and resets the draft on reopen', async () => {
    const w = mountWizard()
    await flushPromises()
    const vm = w.vm as any

    fillBasics(vm)
    vm.goNext()
    await flushPromises()
    expect(vm.draft.step).toBe(1)

    vm.submitting = true
    vm.goNext()
    vm.goPrev()
    vm.goSkip()
    vm.close()
    await flushPromises()
    expect(vm.draft.step).toBe(1)
    expect(w.emitted('close')).toBeFalsy()

    vm.submitting = false
    vm.goPrev()
    await flushPromises()
    expect(vm.draft.step).toBe(0)
    // Step 0 has no skip affordance.
    vm.goSkip()
    expect(vm.draft.step).toBe(0)

    vm.close()
    expect(w.emitted('close')).toHaveLength(1)

    await w.setProps({ open: false })
    await w.setProps({ open: true })
    await flushPromises()
    expect(vm.draft.projectName).toBe('')
    expect(vm.draft.step).toBe(0)

    w.unmount()
  })

  it('renders nothing while closed', async () => {
    const w = mountWizard({ open: false })
    await flushPromises()
    expect(w.find('.wiz-root').exists()).toBe(false)
    w.unmount()
  })
})
