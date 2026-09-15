// @vitest-environment happy-dom
import { createApp, defineComponent, nextTick, reactive } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'

const mocks = vi.hoisted(() => ({
  createAgent: vi.fn(),
  listAgentTeamTemplates: vi.fn(async () => ({ items: [] })),
  inheritedEnv: [{ k: 'GIT_REPOS', v: 'repo|https://example.test/repo.git' }],
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

vi.mock('@/lib/api/api', () => ({
  api: {
    createAgent: mocks.createAgent,
    listAgentTeamTemplates: () => mocks.listAgentTeamTemplates(),
  },
}))

vi.mock('@/lib/agent/useInheritedGitEnv', () => ({
  useInheritedGitEnv: () => ({ inheritedEnv: { value: mocks.inheritedEnv } }),
}))

import { useAgentCreateWizard, type AgentCreateWizardProps } from './useAgentCreateWizard'

function mountWizard(over: Partial<AgentCreateWizardProps> = {}) {
  let wizard!: ReturnType<typeof useAgentCreateWizard>
  const emit = vi.fn()
  const props = reactive<AgentCreateWizardProps>({
    open: true,
    existingNames: [],
    projectId: 'p1',
    ...over,
  })
  const Comp = defineComponent({
    setup() {
      wizard = useAgentCreateWizard(props, emit as never)
      return () => null
    },
  })
  const app = createApp(Comp)
  app.mount(document.createElement('div'))
  return { wizard, emit, props, app }
}

describe('useAgentCreateWizard coverage', () => {
  beforeEach(() => {
    mocks.createAgent.mockReset()
    mocks.createAgent.mockResolvedValue({ name: 'created-agent' })
  })

  afterEach(() => vi.restoreAllMocks())

  it('resets and focuses whenever the dialog opens', async () => {
    const input = document.createElement('input')
    input.id = 'wiz-name-input'
    document.body.appendChild(input)
    const focus = vi.spyOn(input, 'focus')
    const { wizard, props, app } = mountWizard({ open: false })
    wizard.draft.value.name = 'stale'
    wizard.createError.value = 'stale'
    wizard.envHelpOpen.value = true
    props.open = true
    await nextTick()
    await nextTick()
    expect(wizard.draft.value.name).toBe('')
    expect(wizard.createError.value).toBe('')
    expect(wizard.envHelpOpen.value).toBe(false)
    expect(wizard.stepAnimKey.value).toBe(1)
    expect(focus).toHaveBeenCalled()
    input.remove()
    app.unmount()
  })

  it('validates required, invalid, and duplicate names before navigation', () => {
    const { wizard, app } = mountWizard({ existingNames: ['taken'] })
    wizard.goNext()
    expect(wizard.nameError.value).toContain('nameRequired')
    expect(wizard.draft.value.step).toBe(0)
    wizard.draft.value.name = 'bad name'
    wizard.goNext()
    expect(wizard.nameError.value).toContain('nameInvalid')
    wizard.draft.value.name = 'taken'
    wizard.goNext()
    expect(wizard.nameError.value).toContain('nameExists')
    wizard.draft.value.name = 'valid-agent'
    wizard.goNext()
    expect(wizard.nameError.value).toBe('')
    expect(wizard.currentStep.value.id).toBe('acp')
    expect(wizard.progressPct.value).toBe(40)
    expect(wizard.headSub.value).toContain('optional')
    app.unmount()
  })

  it('navigates, skips optional steps, and respects busy guards', () => {
    const { wizard, emit, app } = mountWizard()
    wizard.draft.value.name = 'valid'
    wizard.goPrev()
    wizard.goSkip()
    expect(wizard.draft.value.step).toBe(0)
    wizard.goNext()
    wizard.goSkip()
    expect(wizard.draft.value.skipped.acp).toBe(true)
    expect(wizard.currentStep.value.id).toBe('apiKey')
    wizard.onApiKeyInput('secret')
    wizard.goSkip()
    expect(wizard.draft.value.env).toEqual([])
    expect(wizard.draft.value.skipped.apiKey).toBe(true)
    wizard.goPrev()
    expect(wizard.currentStep.value.id).toBe('apiKey')
    wizard.creating.value = true
    wizard.goPrev()
    wizard.goSkip()
    wizard.goNext()
    wizard.close()
    expect(wizard.currentStep.value.id).toBe('apiKey')
    expect(emit).not.toHaveBeenCalled()
    wizard.creating.value = false
    wizard.close()
    expect(emit).toHaveBeenCalledWith('close')
    app.unmount()
  })

  it('switches backends directly or behind dependency confirmation', async () => {
    const { wizard, app } = mountWizard()
    wizard.selectAcp('cursor')
    wizard.selectAcp('claude_code')
    expect(wizard.draft.value.acpBackend).toBe('claude_code')
    expect(wizard.primaryAuthKey.value).toBe('GRASP_CLAUDE_API_KEY')
    wizard.draft.value.skills.push({ name: 'skill', content: '' })
    wizard.selectAcp('trae')
    expect(wizard.draft.value.acpBackend).toBe('claude_code')
    expect(wizard.pendingAcp.value).toBe('trae')
    expect(wizard.showAcpConfirm.value).toBe(true)
    wizard.cancelAcpSwitch()
    expect(wizard.pendingAcp.value).toBeNull()
    wizard.selectAcp('codebuddy')
    wizard.confirmAcpSwitch()
    await nextTick()
    expect(wizard.draft.value.acpBackend).toBe('codebuddy')
    expect(wizard.pendingAcp.value).toBeNull()
    wizard.confirmAcpSwitch()
    expect(wizard.authGuide.value.backend).toBe('codebuddy')
    app.unmount()
  })

  it('manages region, environment, auth key, and custom config modes', async () => {
    const { wizard, app } = mountWizard()
    wizard.upsertEnv('CUSTOM', 'one')
    wizard.upsertEnv('CUSTOM', 'two')
    expect(wizard.draft.value.env).toContainEqual({ k: 'CUSTOM', v: 'two' })
    wizard.selectAcp('codebuddy')
    wizard.selectRegion('internal')
    expect(wizard.currentRegion.value).toBe('internal')

    wizard.onApiKeyInput(' key-value ')
    expect(wizard.apiKeyInput.value).toBe(' key-value ')
    expect(wizard.authConfigured.value).toBe(true)
    expect(wizard.showAuthReminder.value).toBe(false)
    wizard.onApiKeyInput('')
    expect(wizard.authConfigured.value).toBe(false)
    wizard.draft.value.acpBackend = 'cursor'
    await nextTick()
    wizard.setAuthMode('customConfig')
    expect(wizard.draft.value.authMode).toBe('customConfig')
    expect(wizard.draft.value.customConfigContent).toContain('CURSOR_API_KEY')
    wizard.onCustomConfigInput('{bad')
    expect(wizard.authConfigured.value).toBe(false)
    wizard.draft.value.step = 2
    wizard.goNext()
    expect(wizard.customConfigError.value).toBe(true)
    expect(wizard.draft.value.step).toBe(2)
    wizard.onCustomConfigInput('{"env":{"CURSOR_API_KEY":"x"}}')
    wizard.goNext()
    expect(wizard.customConfigError.value).toBe(false)
    expect(wizard.draft.value.step).toBe(3)
    wizard.setAuthMode('apiKey')
    expect(wizard.draft.value.customConfigContent).toBe('')
    wizard.setAuthMode('apiKey')
    wizard.setAuthMode('customConfig')
    expect(wizard.draft.value.customConfigContent).toContain('CURSOR_API_KEY')
    app.unmount()
  })

  it('marks Git selection, annotates picks, and derives presentation helpers', () => {
    const { wizard, app } = mountWizard()
    wizard.draft.value.skipped.git = true
    wizard.onGitCredentialType('ssh')
    expect(wizard.draft.value.gitCredentialType).toBe('ssh')
    expect(wizard.draft.value.skipped.git).toBeUndefined()
    expect(wizard.inheritedEnv.value).toEqual(mocks.inheritedEnv)
    expect(wizard.chipClass('ok')).toContain('border-ok')
    expect(wizard.chipClass('def')).toContain('border-accent')
    expect(wizard.chipClass('empty')).toContain('border-line')
    expect(wizard.headSub.value).toContain('basics')
    wizard.draft.value.step = 4
    expect(wizard.headSub.value).toContain('review')
    expect(wizard.reviewItems.value.length).toBeGreaterThan(0)
    app.unmount()
  })

  it('submits from review and emits the created agent', async () => {
    const { wizard, emit, app } = mountWizard()
    wizard.draft.value.name = 'valid'
    wizard.draft.value.step = 4
    wizard.goNext()
    expect(wizard.creating.value).toBe(true)
    await flushPromises()
    expect(mocks.createAgent).toHaveBeenCalledWith(expect.objectContaining({ name: 'valid' }))
    expect(emit).toHaveBeenNthCalledWith(1, 'created', { name: 'created-agent' })
    expect(emit).toHaveBeenNthCalledWith(2, 'close')
    expect(wizard.creating.value).toBe(false)
    app.unmount()
  })

  it('returns invalid review submissions to basics and surfaces create failures', async () => {
    const { wizard, app } = mountWizard({ existingNames: ['taken'] })
    wizard.draft.value.step = 4
    wizard.draft.value.name = ''
    await wizard.submitCreate()
    expect(wizard.draft.value.step).toBe(0)
    expect(wizard.nameError.value).toContain('nameRequired')
    wizard.draft.value.step = 4
    wizard.draft.value.name = 'bad name'
    await wizard.submitCreate()
    expect(wizard.nameError.value).toContain('nameInvalid')
    wizard.draft.value.step = 4
    wizard.draft.value.name = 'taken'
    await wizard.submitCreate()
    expect(wizard.nameError.value).toContain('nameExists')

    wizard.draft.value.name = 'valid'
    mocks.createAgent.mockRejectedValueOnce(new Error('backend unavailable'))
    await wizard.submitCreate()
    expect(wizard.createError.value).toBe('backend unavailable')
    expect(wizard.creating.value).toBe(false)
    mocks.createAgent.mockRejectedValueOnce('plain failure')
    await wizard.submitCreate()
    expect(wizard.createError.value).toBe('plain failure')
    app.unmount()
  })
})
