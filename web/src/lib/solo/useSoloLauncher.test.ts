// @vitest-environment happy-dom
import { createApp, defineComponent } from 'vue'
import { createI18n } from 'vue-i18n'
import { flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import pages from '@/locales/zh-CN/pages.json'
import type { SoloCreateBody } from '@/lib/api/apiTypes'

const mocks = vi.hoisted(() => ({
  push: vi.fn(),
  listAgents: vi.fn(async () => [{ name: 'coder' }]),
  listProjects: vi.fn(async () => [
    { id: 'web', name: 'webapp', repoUrl: 'https://github.com/acme/webapp.git', defaultBranch: 'develop', createdAt: '', updatedAt: '' },
    { id: 'api', name: 'api', repoUrl: 'https://github.com/acme/api.git', defaultBranch: 'main', createdAt: '', updatedAt: '' },
  ]),
  listSoloSessions: vi.fn(async () => ({ sessions: [], maxSandboxes: 8 })),
  listModelConfigurations: vi.fn(async () => ({ items: [] })),
  listModelConnections: vi.fn(async () => ({ items: [] })),
  cliModels: vi.fn(async () => ({ models: [] })),
  createSoloSession: vi.fn(async (body: SoloCreateBody) => ({ id: 's1', clientRequestId: body.clientRequestId, initialTurnId: 'first-s1', agentName: 'coder', status: 'starting', title: '', restarts: 0, busy: false, waiting: 0, createdAt: '', updatedAt: '', userId: 'u' })),
  resolveSoloCreation: vi.fn(async (_id: string): Promise<never> => { throw { status: 404, code: 'creation_not_found' } }),
  continueWorkflow: vi.fn(),
}))

vi.mock('vue-router', () => ({ useRouter: () => ({ push: mocks.push }) }))
vi.mock('@/lib/api/api', () => ({ api: mocks }))

import { useSoloLauncher } from './useSoloLauncher'
import { soloPrefs } from './soloPrefs'
import { firstInstallCompletedAt } from '@/lib/pm/firstInstall'
import { useAuth } from '@/lib/composables/useAuth'
import { readSoloOutbox, saveSoloOutbox, soloOutboxScope } from './soloOutbox'

const mountedApps: ReturnType<typeof createApp>[] = []
function mountLauncher() {
  let launcher!: ReturnType<typeof useSoloLauncher>
  const app = createApp(defineComponent({
    setup() {
      launcher = useSoloLauncher()
      return () => null
    },
  }))
  app.use(createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...pages } } }))
  app.mount(document.createElement('div'))
  mountedApps.push(app)
  return launcher
}

describe('useSoloLauncher', () => {
  beforeEach(() => {
    localStorage.clear()
    useAuth().setUser({ username: 'u', expiresAt: '' })
    firstInstallCompletedAt.value = 0
    mocks.push.mockClear()
    mocks.createSoloSession.mockClear()
    mocks.listAgents.mockClear()
    mocks.resolveSoloCreation.mockReset().mockRejectedValue({ status: 404, code: 'creation_not_found' })
    mocks.continueWorkflow.mockReset()
  })
  afterEach(() => { for (const app of mountedApps.splice(0)) app.unmount() })

  it.each(['', 'deleted-agent'])('defaults to Solo when the remembered agent "%s" is unavailable', async (remembered) => {
    soloPrefs.setLastAgent(remembered)
    mocks.listAgents.mockResolvedValueOnce([{ name: 'Implement' }, { name: 'Clarify' }, { name: 'Solo' }, { name: 'Deliver' }])
    const launcher = mountLauncher()
    await launcher.load()
    await flushPromises()
    expect(launcher.agentName.value).toBe('Solo')
    expect(launcher.agent.value?.name).toBe('Solo')
    expect(soloPrefs.lastAgent()).toBe('Solo')
    await launcher.start('A standalone task')
    expect(mocks.createSoloSession).toHaveBeenCalledWith(expect.objectContaining({ agentName: 'Solo', text: 'A standalone task' }))
  })

  it.each(['Implement', 'Solo'])('preserves the valid remembered selection "%s" when Solo exists', async (remembered) => {
    soloPrefs.setLastAgent(remembered)
    mocks.listAgents.mockResolvedValueOnce([{ name: 'Solo' }, { name: 'Implement' }])
    const launcher = mountLauncher()
    await launcher.load()
    await flushPromises()
    expect(launcher.agentName.value).toBe(remembered)
    expect(soloPrefs.lastAgent()).toBe(remembered)
  })

  it('leaves multiple workflow agents unselected when no Solo agent exists', async () => {
    mocks.listAgents.mockResolvedValueOnce([{ name: 'Implement' }, { name: 'Clarify' }])
    const launcher = mountLauncher()
    await launcher.load()
    expect(launcher.agentName.value).toBe('')
    expect(launcher.canStart.value).toBe(false)
  })

  it('refreshes an already loaded launcher when onboarding creates Solo', async () => {
    mocks.listAgents.mockResolvedValueOnce([]).mockResolvedValueOnce([{ name: 'Implement' }, { name: 'Solo' }])
    const launcher = mountLauncher()
    await launcher.load()
    expect(launcher.canStart.value).toBe(false)
    firstInstallCompletedAt.value = 1
    await flushPromises()
    expect(mocks.listAgents).toHaveBeenCalledTimes(2)
    expect(launcher.agentName.value).toBe('Solo')
    expect(launcher.canStart.value).toBe(true)
  })

  it('keeps an unopened launcher lazy after onboarding completes', async () => {
    const launcher = mountLauncher()
    firstInstallCompletedAt.value = 1
    await flushPromises()
    expect(mocks.listAgents).not.toHaveBeenCalled()
    expect(launcher.loading.value).toBe(false)
  })

  it('ignores an older pending load after the onboarding refresh selects Solo', async () => {
    let finishInitial!: (agents: { name: string }[]) => void
    mocks.listAgents.mockImplementationOnce(() => new Promise((resolve) => { finishInitial = resolve }))
      .mockResolvedValueOnce([{ name: 'Implement' }, { name: 'Solo' }])
    const launcher = mountLauncher()
    const pending = launcher.load()
    firstInstallCompletedAt.value = 1
    await flushPromises()
    expect(launcher.agentName.value).toBe('Solo')
    finishInitial([])
    await pending
    expect(launcher.agentName.value).toBe('Solo')
    await vi.waitFor(() => expect(launcher.loading.value).toBe(false))
  })

  it.each(['', 'kimi-k2.5'])('shows real model names instead of the configuration alias while starting with the upstream model choice "%s"', async (selected) => {
    mocks.listAgents.mockResolvedValueOnce([{ name: 'Solo', acpBackend: 'opencode', modelBinding: { kind: 'configuration', configurationId: 'kimi-config' } }] as never)
    mocks.listModelConfigurations.mockResolvedValueOnce({ items: [{
      id: 'kimi-config', name: 'TokenHub', backend: 'opencode', state: 'active', revision: 1,
      binding: { kind: 'connection', connectionId: 'kimi-service', modelId: 'kimi-k3.1-pro', auxiliaryModelIds: ['kimi-k2.5'] },
    }] } as never)
    mocks.listModelConnections.mockResolvedValueOnce({ items: [{
      id: 'kimi-service', name: 'Kimi API', providerId: 'moonshot', protocol: 'openai-chat-completions',
      baseUrl: 'https://api.example.test/v1', credentialId: 'kimi-key', auth: { scheme: 'bearer' }, state: 'active', revision: 1,
      models: [{ id: 'kimi-k3.1-pro' }, { id: 'kimi-k2.5', name: 'Kimi 2.5' }],
    }] } as never)
    const launcher = mountLauncher()
    await launcher.load()
    await flushPromises()
    expect(launcher.modelChoices.value.map(({ id, name }) => ({ id, name }))).toEqual([
      { id: 'kimi-k3.1-pro', name: 'kimi-k3.1-pro' },
      { id: 'kimi-k2.5', name: 'Kimi 2.5' },
    ])
    expect(launcher.modelChoices.value.map(({ vendor, backend }) => ({ vendor, backend }))).toEqual([
      { vendor: 'kimi', backend: false },
      { vendor: 'kimi', backend: false },
    ])
    launcher.initialModelId.value = selected
    await launcher.start('Start a task')
    const body = mocks.createSoloSession.mock.calls.at(-1)?.[0] as { initialModelId?: string; agentName: string }
    expect(body.agentName).toBe('Solo')
    expect(body.initialModelId).toBe(selected || undefined)
    expect(body.initialModelId).not.toBe('TokenHub')
  })
  it('sends an advertised effort and clears it when switching to a model without effort support', async () => {
    mocks.listAgents.mockResolvedValueOnce([{ name: 'Solo', acpBackend: 'deepseek_harness', modelBinding: { kind: 'configuration', configurationId: 'kimi-config' } }] as never)
    mocks.listModelConfigurations.mockResolvedValueOnce({ items: [{
      id: 'kimi-config', name: 'TokenHub', backend: 'deepseek_harness', state: 'active', revision: 1,
      binding: { kind: 'connection', connectionId: 'kimi-service', modelId: 'kimi-k3.1-pro', auxiliaryModelIds: ['kimi-k2.5'] },
    }] } as never)
    mocks.listModelConnections.mockResolvedValueOnce({ items: [{
      id: 'kimi-service', name: 'TokenHub API', providerId: 'tokenhub', protocol: 'openai-chat-completions',
      baseUrl: 'https://api.example.test/v1', credentialId: 'kimi-key', auth: { scheme: 'bearer' }, state: 'active', revision: 1,
      models: [{ id: 'kimi-k3.1-pro', reasoningEfforts: ['low', 'high'] }, { id: 'kimi-k2.5', name: 'Kimi 2.5' }],
    }] } as never)
    const launcher = mountLauncher()
    await launcher.load()
    await flushPromises()
    expect(launcher.modelChoices.value[0]).toMatchObject({ id: 'kimi-k3.1-pro', name: 'kimi-k3.1-pro', vendor: 'kimi', backend: false, efforts: ['low', 'high'] })
    expect(launcher.modelChoices.value[0]?.name).not.toBe('TokenHub')
    launcher.reasoningEffort.value = 'high'
    await launcher.start('Use the selected effort')
    expect(mocks.createSoloSession).toHaveBeenLastCalledWith(expect.objectContaining({ agentName: 'Solo', reasoningEffort: 'high' }))
    expect(mocks.createSoloSession.mock.calls.at(-1)?.[0]).not.toHaveProperty('initialModelId')
    launcher.initialModelId.value = 'kimi-k2.5'
    await flushPromises()
    expect(launcher.reasoningEffort.value).toBe('')
    await launcher.start('Use the other model')
    expect(mocks.createSoloSession.mock.calls.at(-1)?.[0]).toMatchObject({ initialModelId: 'kimi-k2.5' })
    expect(mocks.createSoloSession.mock.calls.at(-1)?.[0]).not.toHaveProperty('reasoningEffort')
  })

  it('sends the highest Gemini 4 Argon effort when the slider is left untouched and keeps a saved lower level', async () => {
    mocks.listAgents.mockResolvedValueOnce([{ name: 'Solo', acpBackend: 'opencode', modelBinding: { kind: 'configuration', configurationId: 'argon-config' } }] as never)
    mocks.listModelConfigurations.mockResolvedValueOnce({ items: [{
      id: 'argon-config', name: 'Argon', backend: 'opencode', state: 'active', revision: 1,
      binding: { kind: 'connection', connectionId: 'argon-service', modelId: 'gemini-4-argon', auxiliaryModelIds: ['gemini-3.1-flash-lite-image'] },
    }] } as never)
    mocks.listModelConnections.mockResolvedValueOnce({ items: [{
      id: 'argon-service', name: 'Argon API', providerId: 'google', protocol: 'openai-chat-completions',
      baseUrl: 'https://api.example.test/v1', credentialId: 'argon-key', auth: { scheme: 'bearer' }, state: 'active', revision: 1,
      models: [
        { id: 'gemini-4-argon', name: 'Gemini 4 Argon' },
        { id: 'gemini-3.1-flash-lite-image', name: 'Lite image' },
      ],
    }] } as never)
    const launcher = mountLauncher()
    await launcher.load()
    await flushPromises()
    expect(launcher.modelChoices.value[0]).toMatchObject({ id: 'gemini-4-argon', efforts: ['low', 'medium', 'high'] })
    expect(launcher.reasoningEffort.value).toBe('')
    await launcher.start('Start at the top')
    expect(mocks.createSoloSession).toHaveBeenLastCalledWith(expect.objectContaining({ agentName: 'Solo', reasoningEffort: 'high' }))
    expect(launcher.reasoningEffort.value).toBe('high')
    launcher.reasoningEffort.value = 'low'
    await launcher.start('Stay low')
    expect(mocks.createSoloSession).toHaveBeenLastCalledWith(expect.objectContaining({ reasoningEffort: 'low' }))
    launcher.initialModelId.value = 'gemini-3.1-flash-lite-image'
    await flushPromises()
    expect(launcher.modelChoices.value.find(choice => choice.id === 'gemini-3.1-flash-lite-image')?.efforts).toEqual(['minimal', 'high'])
    expect(launcher.reasoningEffort.value).toBe('high')
    expect(launcher.error.value).toBe('所选思考强度已不可用，已改用该模型的最高强度。')
  })

  it('starts Kimi Code with the selected CLI effort and clears it for K2.7', async () => {
    mocks.listAgents.mockResolvedValueOnce([{ name: 'Solo', acpBackend: 'kimi_code', modelBinding: { kind: 'configuration', configurationId: 'kimi-cli' } }] as never)
    mocks.listModelConfigurations.mockResolvedValueOnce({ items: [{
      id: 'kimi-cli', name: 'Kimi Code', backend: 'kimi_code', state: 'active', revision: 1,
      binding: { kind: 'platform', credentialId: 'kimi-key', modelId: 'k3.1-pro', auxiliaryModelIds: ['kimi-for-coding-highspeed'] },
    }] } as never)
    mocks.cliModels.mockResolvedValueOnce({ models: [
      { id: 'k3.1-pro', name: 'Kimi K3.1 Pro', vendor: 'kimi', efforts: ['low', 'high', 'max'] },
      { id: 'kimi-for-coding-highspeed', name: 'Kimi K2.7 Code Highspeed', vendor: 'kimi' },
    ] } as never)
    const launcher = mountLauncher()
    await launcher.load()
    await flushPromises()
    expect(launcher.modelChoices.value[0]).toMatchObject({ id: 'k3.1-pro', efforts: ['low', 'high', 'max'] })
    await launcher.start('Use the displayed highest effort')
    expect(mocks.createSoloSession).toHaveBeenLastCalledWith(expect.objectContaining({ reasoningEffort: 'max' }))
    for (const effort of ['low', 'high', 'max']) {
      launcher.reasoningEffort.value = effort
      await launcher.start('Use Kimi Code with the selected effort')
      expect(mocks.createSoloSession).toHaveBeenLastCalledWith(expect.objectContaining({ agentName: 'Solo', reasoningEffort: effort }))
    }
    launcher.initialModelId.value = 'kimi-for-coding-highspeed'
    await flushPromises()
    expect(launcher.reasoningEffort.value).toBe('')
    await launcher.start('Use Kimi Code without selectable effort')
    expect(mocks.createSoloSession.mock.calls.at(-1)?.[0]).toMatchObject({ initialModelId: 'kimi-for-coding-highspeed' })
    expect(mocks.createSoloSession.mock.calls.at(-1)?.[0]).not.toHaveProperty('reasoningEffort')
  })

  it('keeps automatic platform selection on its coding-tool mark and omits a model override', async () => {
    mocks.listAgents.mockResolvedValueOnce([{ name: 'Solo', acpBackend: 'cursor', modelBinding: { kind: 'configuration', configurationId: 'cursor-default' } }] as never)
    mocks.listModelConfigurations.mockResolvedValueOnce({ items: [{
      id: 'cursor-default', name: 'Cursor daily', backend: 'cursor', state: 'active', revision: 1,
      binding: { kind: 'platform', credentialId: 'cursor-account' },
    }] } as never)
    const launcher = mountLauncher()
    await launcher.load()
    await flushPromises()
    expect(launcher.modelChoices.value).toMatchObject([{ id: '', name: 'Cursor daily', hint: '交给编码工具选择', vendor: 'cursor', backend: true, unavailable: false }])
    await launcher.start('Let the tool choose')
    const body = mocks.createSoloSession.mock.calls.at(-1)?.[0]
    expect(body).toMatchObject({ agentName: 'Solo', text: 'Let the tool choose' })
    expect(body).not.toHaveProperty('initialModelId')
    expect(body).not.toHaveProperty('reasoningEffort')
  })
  it('keeps repository order, drops deleted ids, and does not remember branch overrides', async () => {
    soloPrefs.setLastProjects(['gone', 'web'])
    const launcher = mountLauncher()
    await launcher.load()
    await flushPromises()
    expect(launcher.projectIds.value).toEqual(['web'])

    launcher.toggleProject('api')
    launcher.setProjectBranch('web', 'feat/pay')
    expect(launcher.projectIds.value).toEqual(['web', 'api'])
    await flushPromises()
    expect(soloPrefs.lastProjects()).toEqual(['web', 'api'])
    expect(localStorage.getItem('grasp.home.lastSoloProjects')).toBe(JSON.stringify(['web', 'api']))
    expect(localStorage.getItem('grasp.home.lastSoloBranch')).toBeNull()

    const started = await launcher.start('hello')
    expect(started?.id).toBe('s1')
    expect(mocks.createSoloSession).toHaveBeenCalledWith({
      clientRequestId: expect.any(String),
      agentName: 'coder',
      repos: [
        { projectId: 'web', branch: 'feat/pay' },
        { projectId: 'api' },
      ],
      text: 'hello',
      title: undefined,
      images: undefined,
    })

    launcher.clearProjects()
    await launcher.start('')
    expect(mocks.createSoloSession).toHaveBeenLastCalledWith({
      clientRequestId: expect.any(String),
      agentName: 'coder',
      repos: [],
      text: undefined,
      title: undefined,
      images: undefined,
    })
    expect(launcher.branches.value).toEqual({})
  })

  it('shows a localized conflict on the existing error', async () => {
    mocks.createSoloSession.mockRejectedValueOnce({
      code: 'repo_dir_conflict',
      message: 'server',
      params: { dir: 'webapp', repos: ['webapp', 'checkout'] },
    })
    soloPrefs.setLastAgent('coder')
    const launcher = mountLauncher()
    await launcher.load()
    launcher.selectOnly('web')
    const started = await launcher.start('go')
    expect(started).toBeNull()
    expect(launcher.error.value).toContain('webapp')
    expect(launcher.error.value).toContain('checkout')
    expect(launcher.error.value).toContain('目录名')
  })

  it('sends an illegal branch unchanged and names the repository', async () => {
    mocks.createSoloSession.mockRejectedValueOnce({
      code: 'branch_invalid_repo',
      message: 'server',
      params: { repo: 'webapp' },
    })
    const launcher = mountLauncher()
    await launcher.load()
    launcher.selectOnly('web')
    launcher.setProjectBranch('web', 'bad branch')
    const started = await launcher.start('go')
    expect(started).toBeNull()
    expect(mocks.createSoloSession).toHaveBeenCalledWith(expect.objectContaining({
      repos: [{ projectId: 'web', branch: 'bad branch' }],
    }))
    expect(launcher.error.value).toContain('webapp')
    expect(launcher.error.value).toContain('空格')
  })

  it('does not overwrite remembered repositories while persistence is paused', async () => {
    soloPrefs.setLastProjects(['web', 'api'])
    const launcher = mountLauncher()
    await launcher.load()
    launcher.setPersistProjects(false)
    launcher.selectOnly('web')
    expect(launcher.projectIds.value).toEqual(['web'])
    expect(soloPrefs.lastProjects()).toEqual(['web', 'api'])

    const started = await launcher.start('')
    expect(started?.id).toBe('s1')
    expect(soloPrefs.lastProjects()).toEqual(['web'])
  })

  it('writes the frozen opening request before POST and restores it without POST after remount', async () => {
    const scope = soloOutboxScope('u', 'creation')
    const images = [{ data: 'aW1hZ2U=', mimeType: 'image/png', name: 'original.png' }]
    const references = [{ id: 'display-only', kind: 'solo', target: 'other', title: 'Original title', createdAt: '' }] as never[]
    mocks.createSoloSession.mockImplementationOnce(async body => {
      const saved = await readSoloOutbox(scope)
      expect(saved).toHaveLength(1)
      expect(saved[0]?.request).toEqual(body)
      throw new Error('lost acknowledgement')
    })
    const first = mountLauncher()
    await first.load()
    first.selectOnly('web')
    first.setProjectBranch('web', 'feature/original')
    expect(await first.start('Original message', images, 'Original task', references)).toBeNull()
    const original = mocks.createSoloSession.mock.calls.at(-1)![0]
    expect(first.canStart.value).toBe(false)
    images[0]!.name = 'edited.png'
    for (const app of mountedApps.splice(0)) app.unmount()

    const restored = mountLauncher()
    await restored.load()
    expect(mocks.createSoloSession).toHaveBeenCalledTimes(1)
    expect(restored.pendingCreation.value?.request).toEqual(original)
    expect(restored.pendingCreation.value?.request.images?.[0]?.name).toBe('original.png')
    expect(restored.canStart.value).toBe(false)
    restored.agentName.value = 'another-agent'
    restored.projectIds.value = []
    restored.branches.value = { web: 'different' }
    restored.initialModelId.value = 'a-different-model'
    restored.reasoningEffort.value = 'low'
    expect((await restored.retryPendingCreation())?.id).toBe('s1')
    expect(mocks.createSoloSession).toHaveBeenCalledTimes(2)
    expect(mocks.createSoloSession.mock.calls[1]?.[0]).toEqual(original)
    expect(await readSoloOutbox(scope)).toEqual([])
    expect(mocks.push).toHaveBeenLastCalledWith('/solo/s1')
  })

  it('reconciles a lost creation acknowledgement with GET and opens the original chat without another POST', async () => {
    mocks.createSoloSession.mockRejectedValueOnce(new Error('response was lost'))
    const launcher = mountLauncher()
    await launcher.load()
    await launcher.start('Do this once')
    const pending = launcher.pendingCreation.value!
    mocks.resolveSoloCreation.mockResolvedValue({ id: 'accepted', userId: 'u', clientRequestId: pending.id, initialTurnId: 'accepted-turn' } as never)
    await launcher.checkPendingCreation()
    expect(launcher.recoveredSession.value?.id).toBe('accepted')
    expect(mocks.push).not.toHaveBeenCalled()
    expect((await launcher.retryPendingCreation())?.id).toBe('accepted')
    expect(mocks.createSoloSession).toHaveBeenCalledTimes(1)
    expect(mocks.push).toHaveBeenCalledWith('/solo/accepted')
  })

  it('keeps an invalid receipt pending and will not POST against a server without creation lookup support', async () => {
    mocks.createSoloSession.mockResolvedValueOnce({ id: 'old-server', userId: 'u' } as never)
    const launcher = mountLauncher()
    await launcher.load()
    expect(await launcher.start('Keep this message')).toBeNull()
    expect(launcher.error.value).toBe(pages.pages.solo.invalidReceipt)
    expect(launcher.pendingCreation.value?.request.text).toBe('Keep this message')
    mocks.resolveSoloCreation.mockRejectedValue({ status: 404 })
    await launcher.retryPendingCreation()
    expect(mocks.createSoloSession).toHaveBeenCalledTimes(1)
    expect(mocks.push).not.toHaveBeenCalled()
    expect(launcher.recoveryChecked.value).toBe(false)
  })

  it('does not POST if the durable browser write fails', async () => {
    const launcher = mountLauncher()
    await launcher.load()
    const transaction = vi.spyOn(IDBDatabase.prototype, 'transaction').mockImplementationOnce(() => { throw new DOMException('storage quota exceeded', 'QuotaExceededError') })
    expect(await launcher.start('Keep this editable')).toBeNull()
    expect(mocks.createSoloSession).not.toHaveBeenCalled()
    expect(launcher.error.value).toBe(pages.pages.solo.outboxUnavailable)
    transaction.mockRestore()
  })

  it('adopts the pending creation saved by another entry instead of submitting a second nonce', async () => {
    const first = mountLauncher()
    const second = mountLauncher()
    await first.load()
    await second.load()
    mocks.createSoloSession.mockRejectedValueOnce(new Error('unconfirmed first entry'))
    await first.start('First entry')
    expect(await second.start('Do not create a second chat')).toBeNull()
    expect(mocks.createSoloSession).toHaveBeenCalledTimes(1)
    expect(second.pendingCreation.value?.id).toBe(first.pendingCreation.value?.id)
    expect(second.pendingCreation.value?.request.text).toBe('First entry')
    expect(second.canStart.value).toBe(false)
  })

  it('never displays local requests before the current account passes authorized API reads', async () => {
    await saveSoloOutbox({ scope: soloOutboxScope('u', 'creation'), id: 'private-request', kind: 'creation', createdAt: '', request: { clientRequestId: 'private-request', agentName: 'coder', text: 'Private draft' } })
    mocks.listAgents.mockRejectedValueOnce({ status: 401, message: 'unauthorized' })
    const launcher = mountLauncher()
    await launcher.load()
    expect(launcher.pendingCreation.value).toBeNull()
    expect(launcher.canStart.value).toBe(false)
    expect(mocks.resolveSoloCreation).not.toHaveBeenCalled()
  })

  it('keeps a pending request private across account changes and preserves it for its owner', async () => {
    mocks.createSoloSession.mockRejectedValueOnce(new Error('lost response'))
    const launcher = mountLauncher()
    await launcher.load()
    await launcher.start('Private owner request')
    const original = launcher.pendingCreation.value!
    useAuth().setUser({ username: 'other-user', expiresAt: '' })
    await flushPromises()
    expect(launcher.pendingCreation.value).toBeNull()
    expect(await readSoloOutbox(soloOutboxScope('u', 'creation'))).toHaveLength(1)
    expect(await launcher.retryPendingCreation()).toBeNull()
    expect(mocks.createSoloSession).toHaveBeenCalledTimes(1)
    useAuth().setUser({ username: 'u', expiresAt: '' })
    await flushPromises()
    await vi.waitFor(() => expect(launcher.pendingCreation.value?.id).toBe(original.id))
  })

  it('ignores a previous account acknowledgement instead of navigating or exposing its chat', async () => {
    let finish!: (value: Awaited<ReturnType<typeof mocks.createSoloSession>>) => void
    mocks.createSoloSession.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const launcher = mountLauncher()
    await launcher.load()
    const starting = launcher.start('Private in-flight request')
    await flushPromises()
    const original = (await readSoloOutbox(soloOutboxScope('u', 'creation')))[0]!
    useAuth().setUser({ username: 'other-user', expiresAt: '' })
    await flushPromises()
    finish({ id: 'old-owner-chat', userId: 'u', clientRequestId: original.id, initialTurnId: 'initial' } as never)
    expect(await starting).toBeNull()
    expect(mocks.push).not.toHaveBeenCalled()
    expect(launcher.pendingCreation.value).toBeNull()
    expect(await readSoloOutbox(original.scope)).toHaveLength(1)
  })

  it('moves an unaccepted continuation message into the existing chat outbox without sending it', async () => {
    mocks.continueWorkflow.mockImplementation(async (_run, request: SoloCreateBody) => ({ id: 'existing-chat', userId: 'u', clientRequestId: request.clientRequestId }))
    const launcher = mountLauncher()
    await launcher.load()
    const images = [{ data: 'aW1hZ2U=', mimeType: 'image/png' }]
    expect((await launcher.start('Continue with this', images, '', [], 'run-1'))?.id).toBe('existing-chat')
    const [handoff] = await readSoloOutbox(soloOutboxScope('u', 'message', 'existing-chat'))
    expect(handoff?.kind).toBe('message')
    expect(handoff?.request).toMatchObject({ clientMessageId: `creation:${mocks.continueWorkflow.mock.calls[0]?.[1].clientRequestId}`, text: 'Continue with this', images })
    expect(await readSoloOutbox(soloOutboxScope('u', 'creation'))).toEqual([])
    expect(mocks.createSoloSession).not.toHaveBeenCalled()
    expect(mocks.push).toHaveBeenCalledWith('/solo/existing-chat')
  })

  it('reuses the continuation handoff identity if deleting the creation record was interrupted', async () => {
    mocks.continueWorkflow.mockImplementation(async (_run, request: SoloCreateBody) => ({ id: 'existing-chat', userId: 'u', clientRequestId: request.clientRequestId }))
    const launcher = mountLauncher()
    await launcher.load()
    const originalTransaction = IDBDatabase.prototype.transaction
    let writes = 0
    const transaction = vi.spyOn(IDBDatabase.prototype, 'transaction').mockImplementation(function (this: IDBDatabase, names, mode, options) {
      if (mode === 'readwrite' && ++writes === 3) throw new DOMException('write interrupted', 'AbortError')
      return originalTransaction.call(this, names, mode, options)
    })
    expect(await launcher.start('Unaccepted opening text', [], '', [], 'run-1')).toBeNull()
    transaction.mockRestore()
    const pending = launcher.pendingCreation.value!
    mocks.resolveSoloCreation.mockResolvedValue({ id: 'existing-chat', userId: 'u', clientRequestId: pending.id } as never)
    expect((await launcher.retryPendingCreation())?.id).toBe('existing-chat')
    const messages = await readSoloOutbox(soloOutboxScope('u', 'message', 'existing-chat'))
    expect(messages).toHaveLength(1)
    expect(messages[0]?.id).toBe(`creation:${pending.id}`)
    expect(mocks.continueWorkflow).toHaveBeenCalledTimes(1)
  })

  it('removes only the local creation retry record when requested', async () => {
    mocks.createSoloSession.mockRejectedValueOnce(new Error('lost response'))
    const launcher = mountLauncher()
    await launcher.load()
    await launcher.start('Local retry only')
    await saveSoloOutbox({ scope: soloOutboxScope('other-owner', 'creation'), kind: 'creation', id: 'other-request', createdAt: '', request: { clientRequestId: 'other-request', agentName: 'coder', text: 'Private' } })
    await launcher.stopCreationRetry()
    expect(launcher.pendingCreation.value).toBeNull()
    expect(await readSoloOutbox(soloOutboxScope('u', 'creation'))).toEqual([])
    expect(await readSoloOutbox(soloOutboxScope('other-owner', 'creation'))).toHaveLength(1)
    expect(mocks.createSoloSession).toHaveBeenCalledTimes(1)
  })
})
