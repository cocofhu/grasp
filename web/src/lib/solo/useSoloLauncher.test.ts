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
  createSoloSession: vi.fn(async (_body: SoloCreateBody) => ({ id: 's1', agentName: 'coder', status: 'starting', title: '', restarts: 0, busy: false, waiting: 0, createdAt: '', updatedAt: '', userId: 'u' })),
}))

vi.mock('vue-router', () => ({ useRouter: () => ({ push: mocks.push }) }))
vi.mock('@/lib/api/api', () => ({ api: mocks }))

import { useSoloLauncher } from './useSoloLauncher'
import { soloPrefs } from './soloPrefs'
import { firstInstallCompletedAt } from '@/lib/pm/firstInstall'

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
    firstInstallCompletedAt.value = 0
    mocks.push.mockClear()
    mocks.createSoloSession.mockClear()
    mocks.listAgents.mockClear()
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
    expect(launcher.loading.value).toBe(false)
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

  it('keeps automatic platform selection on its coding-tool mark and omits a model override', async () => {
    mocks.listAgents.mockResolvedValueOnce([{ name: 'Solo', acpBackend: 'cursor', modelBinding: { kind: 'configuration', configurationId: 'cursor-default' } }] as never)
    mocks.listModelConfigurations.mockResolvedValueOnce({ items: [{
      id: 'cursor-default', name: 'Cursor daily', backend: 'cursor', state: 'active', revision: 1,
      binding: { kind: 'platform', credentialId: 'cursor-account' },
    }] } as never)
    const launcher = mountLauncher()
    await launcher.load()
    await flushPromises()
    expect(launcher.modelChoices.value).toEqual([{ id: '', name: 'Cursor daily', hint: '交给编码工具选择', vendor: 'cursor', backend: true, unavailable: false }])
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
})
