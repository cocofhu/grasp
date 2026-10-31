// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'

const mocks = vi.hoisted(() => ({
  push: vi.fn(),
  createSoloSession: vi.fn(),
  searchLaunchReferences: vi.fn(),
}))

vi.mock('@/lib/composables/useWorkspaceContext', async () => {
  const { ref } = await import('vue')
  const workspace = ref({ name: '工作区', description: '', variables: [] })
  return { useWorkspaceContext: () => ({ workspace, ensureLoaded: async () => workspace.value, reload: async () => workspace.value }) }
})
vi.mock('vue-router', () => ({ useRouter: () => ({ push: mocks.push }), RouterLink: { props: ['to'], template: '<a><slot /></a>' } }))
vi.mock('@/lib/composables/useToast', () => ({ useToast: () => ({ warn: vi.fn(), error: vi.fn(), success: vi.fn() }) }))
vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      listWorkflows: vi.fn(async () => []),
      listAgents: vi.fn(async () => [{ name: 'Solo', acpBackend: 'cursor' }]),
      listProjects: vi.fn(async () => [{ id: 'p1', name: 'web', repoUrl: 'https://github.com/acme/web.git', defaultBranch: 'main' }]),
      listSoloSessions: vi.fn(async () => ({ items: [], maxSandboxes: 5 })),
      listModelConfigurations: vi.fn(async () => ({ items: [] })),
      listModelConnections: vi.fn(async () => ({ items: [] })),
      listCredentials: vi.fn(async () => ({ items: [] })),
      cliModels: vi.fn(async () => ({ models: [] })),
      createSoloSession: mocks.createSoloSession,
      searchLaunchReferences: mocks.searchLaunchReferences,
    },
  }
})

import DashboardView from './DashboardView.vue'

function mountDashboard() {
  return mount(DashboardView, {
    attachTo: document.body,
    global: {
      plugins: [createI18n({ legacy: false, locale: 'zh-CN', missingWarn: false, fallbackWarn: false, messages: { 'zh-CN': { ...common, ...pages } } })],
      stubs: { RunLaunchModal: true, AppModal: true, Teleport: false, HomeParticleMeshBackground: true, HomeYumeCharacter: true },
    },
  })
}

async function typeAt(w: ReturnType<typeof mountDashboard>, value: string) {
  const ta = w.get('[data-testid="home-composer-input"]')
  await ta.setValue(value)
  ;(ta.element as HTMLTextAreaElement).setSelectionRange(value.length, value.length)
  await ta.trigger('input')
  await new Promise((r) => setTimeout(r, 300))
  await flushPromises()
}

beforeEach(() => {
  localStorage.clear()
  localStorage.setItem('grasp.home.mode', 'solo')
  localStorage.setItem('grasp.home.lastSoloAgent', 'Solo')
  localStorage.setItem('grasp.home.lastSoloProjects', JSON.stringify(['p1']))
  Element.prototype.scrollIntoView = vi.fn()
  mocks.createSoloSession.mockReset()
  mocks.createSoloSession.mockResolvedValue({ id: 'new', agentName: 'Solo', status: 'starting', title: '', createdAt: '', updatedAt: '' })
  mocks.searchLaunchReferences.mockReset()
  mocks.searchLaunchReferences.mockImplementation(async (p: { lane: string }) =>
    p.lane === 'solo'
      ? { lane: 'solo', items: [{ kind: 'solo', target: 's2', title: '登录重构', status: 'closed' }] }
      : { lane: 'github', items: [{ kind: 'github_issue', target: 'acme/web#3', title: 'Bug', state: 'open', url: 'u', comments: 0, repo: 'acme/web', number: 3 }] })
})
afterEach(() => {
  document.body.innerHTML = ''
})

describe('home Solo composer @ references', () => {
  it('searches with the picked agent and projects and starts the session with the references', async () => {
    const w = mountDashboard()
    await flushPromises()
    await typeAt(w, '参考 @登')
    expect(w.find('[data-testid="solo-reference-picker"]').exists()).toBe(true)
    expect(mocks.searchLaunchReferences).toHaveBeenCalledWith(expect.objectContaining({ lane: 'solo', q: '登', agent: 'Solo', projects: ['p1'] }), expect.anything())
    const ta = w.get('[data-testid="home-composer-input"]')
    await ta.trigger('keydown', { key: 'ArrowDown' })
    await ta.trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect((ta.element as HTMLTextAreaElement).value).toBe('参考 @登录重构 ')
    expect(w.findAll('[data-testid="home-reference-chips"] [data-testid="solo-reference-chip"]').length).toBe(1)

    await w.get('[data-testid="home-composer"]').trigger('submit')
    await flushPromises()
    expect(mocks.createSoloSession).toHaveBeenCalledWith(expect.objectContaining({ agentName: 'Solo', text: '参考 @登录重构', references: [{ kind: 'solo', target: 's2' }] }))
    expect((ta.element as HTMLTextAreaElement).value).toBe('')
  })

  it('offers references from the + menu and turns pasted links into #n for the picked repo', async () => {
    const w = mountDashboard()
    await flushPromises()
    await w.get('[data-testid="home-composer-plus"]').trigger('click')
    await w.get('[data-testid="home-plus-github"]').trigger('click')
    await new Promise((r) => setTimeout(r, 300))
    await flushPromises()
    await w.get('[data-testid="solo-reference-option-acme/web#3"]').trigger('click')
    await flushPromises()
    const el = w.get('[data-testid="home-composer-input"]').element as HTMLTextAreaElement
    expect(el.value).toBe('@#3 ')

    el.setSelectionRange(el.value.length, el.value.length)
    const paste = new Event('paste', { cancelable: true }) as ClipboardEvent
    Object.defineProperty(paste, 'clipboardData', { value: { getData: () => 'https://github.com/acme/web/pull/9', items: [], files: [] } })
    el.dispatchEvent(paste)
    await flushPromises()
    expect(el.value).toBe('@#3 @#9 ')

    await w.get('[data-testid="home-composer"]').trigger('submit')
    await flushPromises()
    expect(mocks.createSoloSession).toHaveBeenCalledWith(expect.objectContaining({ references: [{ kind: 'github_issue', target: 'acme/web#3' }, { kind: 'github_pr', target: 'acme/web#9' }] }))
  })

  it('keeps the text but drops references when switching to workflow mode', async () => {
    const w = mountDashboard()
    await flushPromises()
    await typeAt(w, '看 @')
    await w.get('[data-testid="home-composer-input"]').trigger('keydown', { key: 'ArrowDown' })
    await w.get('[data-testid="home-composer-input"]').trigger('keydown', { key: 'Enter' })
    await flushPromises()
    const workflowTab = w.findAll('button').find((b) => b.text().includes('工作流'))!
    await workflowTab.trigger('click')
    await flushPromises()
    expect((w.get('[data-testid="home-composer-input"]').element as HTMLTextAreaElement).value).toBe('看 @登录重构 ')
    expect(w.find('[data-testid="home-reference-chips"]').exists()).toBe(false)
    await w.get('[data-testid="home-composer-plus"]').trigger('click')
    expect(w.find('[data-testid="home-plus-menu"]').exists()).toBe(false)
  })
})
