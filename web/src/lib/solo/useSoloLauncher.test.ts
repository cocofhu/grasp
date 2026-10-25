// @vitest-environment happy-dom
import { createApp, defineComponent } from 'vue'
import { createI18n } from 'vue-i18n'
import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import pages from '@/locales/zh-CN/pages.json'

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
  createSoloSession: vi.fn(async () => ({ id: 's1', agentName: 'coder', status: 'starting', title: '', restarts: 0, busy: false, waiting: 0, createdAt: '', updatedAt: '', userId: 'u' })),
}))

vi.mock('vue-router', () => ({ useRouter: () => ({ push: mocks.push }) }))
vi.mock('@/lib/api/api', () => ({ api: mocks }))

import { useSoloLauncher } from './useSoloLauncher'
import { soloPrefs } from './soloPrefs'

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
  return launcher
}

describe('useSoloLauncher', () => {
  beforeEach(() => {
    localStorage.clear()
    mocks.push.mockClear()
    mocks.createSoloSession.mockClear()
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
