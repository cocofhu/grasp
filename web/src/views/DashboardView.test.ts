// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import enCommon from '@/locales/en/common.json'
import enPages from '@/locales/en/pages.json'

const mocks = vi.hoisted(() => ({ create: vi.fn(), resolve: vi.fn(), startRun: vi.fn(), listWorkflows: vi.fn(), push: vi.fn(), replace: vi.fn() }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push: mocks.push, replace: mocks.replace }), RouterLink: { props: ['to'], template: '<a><slot /></a>' } }))
vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return { ...actual, api: { ...actual.api,
    listAgents: vi.fn(async () => [{ name: 'Solo', acpBackend: 'cursor' }, { name: 'clarifier', capabilities: { interaction: 'clarify', tools: ['ask_question'] } }]),
    listProjects: vi.fn(async () => []),
    listSoloSessions: vi.fn(async () => ({ items: [], maxSandboxes: 5 })),
    listModelConfigurations: vi.fn(async () => ({ items: [] })),
    listModelConnections: vi.fn(async () => ({ items: [] })),
    listCredentials: vi.fn(async () => ({ items: [] })),
    cliModels: vi.fn(async () => ({ models: [] })),
    createSoloSession: mocks.create, resolveSoloCreation: mocks.resolve, startRun: mocks.startRun, listWorkflows: mocks.listWorkflows,
  } }
})

import DashboardView from './DashboardView.vue'
import { setBrandSettings } from '@/lib/composables/useBrandSettings'
import { setWorkflowFeature } from '@/lib/composables/useWorkflowFeature'
import { __resetDraftIdbForTests, __setDraftIdbBackendForTests, createMemoryDraftIdb } from '@/lib/run/draftIdb'
import { loadHomeComposerDraft, saveHomeComposerDraft } from '@/lib/run/homeComposerDraft'
import { useAuth } from '@/lib/composables/useAuth'
import type { SoloCreateBody } from '@/lib/api/apiTypes'

const mounted: ReturnType<typeof mount>[] = []
function mountHome(locale = 'zh-CN') {
  const wrapper = mount(DashboardView, { attachTo: document.body, global: {
    plugins: [createI18n({ legacy: false, locale, missingWarn: false, fallbackWarn: false, messages: { 'zh-CN': { ...common, ...pages }, en: { ...enCommon, ...enPages } } })],
    stubs: { HomeParticleMeshBackground: true, HomeYumeCharacter: true, SoloLimitModal: true, ProjectDrawer: true },
  } })
  mounted.push(wrapper)
  return wrapper
}
async function render(locale = 'zh-CN') {
  const wrapper = mountHome(locale)
  await vi.waitFor(() => expect(wrapper.get('[data-testid="home-composer-send"]').attributes('disabled')).toBeUndefined())
  return wrapper
}

beforeEach(() => {
  vi.clearAllMocks()
  useAuth().clearUser()
  localStorage.clear()
  localStorage.setItem('grasp.home.mode', 'solo')
  localStorage.setItem('grasp.home.lastSoloAgent', 'Solo')
  __setDraftIdbBackendForTests(createMemoryDraftIdb())
  setBrandSettings(null)
  setWorkflowFeature({ enabled: true, locked: false, source: 'config' })
  mocks.create.mockImplementation(async (body: SoloCreateBody) => ({ id: 'solo-new', userId: 'anonymous', clientRequestId: body.clientRequestId, initialTurnId: 'first-new', agentName: 'Solo', status: 'starting', createdAt: '', updatedAt: '' }))
  mocks.resolve.mockRejectedValue({ status: 404, code: 'creation_not_found' })
  mocks.listWorkflows.mockResolvedValue([])
})
afterEach(() => {
  for (const wrapper of mounted.splice(0)) wrapper.unmount()
  __resetDraftIdbForTests()
  vi.useRealTimers()
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('Solo home entry', () => {
  it('shows the Solo controls when Solo is the remembered home mode', async () => {
    const wrapper = await render(); await flushPromises()
    expect(wrapper.get('[data-testid="home-mode-solo"]').attributes('aria-selected')).toBe('true')
    expect(wrapper.find('[data-testid="home-workflow-cards"]').exists()).toBe(false)
    expect(wrapper.findComponent({ name: 'SoloAgentSelect' }).exists()).toBe(true)
    expect(wrapper.findComponent({ name: 'SoloProjectSelect' }).exists()).toBe(true)
    expect(mocks.create).not.toHaveBeenCalled()
    expect(mocks.startRun).not.toHaveBeenCalled()
  })
  it('keeps the configured hero copy and the existing character', async () => {
    setBrandSettings({ home_subtitle: '继续完成你的想法' })
    const wrapper = await render(); await flushPromises()
    expect(wrapper.get('[data-testid="home-hero-copy"]').text()).toBe('继续完成你的想法')
    expect(wrapper.findComponent({ name: 'HomeYumeCharacter' }).exists()).toBe(true)
    expect(wrapper.get('[data-testid="home-brand-block"]').text()).not.toContain('Grasp')
  })
  it('uses the locale default when the configured subtitle is blank', async () => {
    setBrandSettings({ home_subtitle: '   ' })
    const wrapper = await render('en'); await flushPromises()
    expect(wrapper.get('[data-testid="home-hero-copy"]').text()).toBe(enPages.pages.dashboard.heroSubtitle)
  })
  it('starts a Solo with the text after the explicit submit and clears the draft', async () => {
    const wrapper = await render(); await flushPromises()
    await wrapper.get('[data-testid="home-composer-input"]').setValue('继续评估 CI')
    expect(mocks.create).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="home-composer"]').trigger('submit'); await flushPromises()
    await vi.waitFor(() => expect(mocks.create).toHaveBeenCalledTimes(1))
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({ agentName: 'Solo', text: '继续评估 CI' }))
    expect(mocks.startRun).not.toHaveBeenCalled()
    await vi.waitFor(() => expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe(''))
    expect(await loadHomeComposerDraft()).toBeNull()
  })
  it('keeps a failed launch draft and allows retry', async () => {
    mocks.create.mockRejectedValueOnce(new Error('unavailable'))
    const wrapper = await render(); await flushPromises()
    await wrapper.get('textarea').setValue('不要丢掉这段话')
    await wrapper.get('[data-testid="home-composer"]').trigger('submit'); await flushPromises()
    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe('不要丢掉这段话')
    await vi.waitFor(() => expect(wrapper.get('[data-testid="home-solo-error"]').text()).toContain('unavailable'))
    await wrapper.get('[data-testid="solo-creation-check"]').trigger('click')
    await vi.waitFor(() => expect(wrapper.get('[data-testid="solo-creation-retry"]').attributes('disabled')).toBeUndefined())
    await wrapper.get('[data-testid="solo-creation-retry"]').trigger('click')
    await vi.waitFor(() => expect(mocks.create).toHaveBeenCalledTimes(2))
    expect(mocks.create.mock.calls[1]?.[0]).toEqual(mocks.create.mock.calls[0]?.[0])
  })
  it('Ctrl/Meta+Enter sends while plain Enter and IME composition preserve input', async () => {
    const wrapper = await render(); await flushPromises()
    const input = wrapper.get('textarea'); await input.setValue('输入')
    await input.trigger('keydown', { key: 'Enter' }); await input.trigger('keydown', { key: 'Enter', shiftKey: true })
    await input.trigger('compositionstart'); await input.trigger('keydown', { key: 'Enter', ctrlKey: true })
    expect(mocks.create).not.toHaveBeenCalled()
    await input.trigger('compositionend'); await input.trigger('keydown', { key: 'Enter', metaKey: true }); await flushPromises()
    await vi.waitFor(() => expect(mocks.create).toHaveBeenCalledTimes(1))
  })
  it('offers no workflow entry in @ and sends /workflow to Solo as plain text', async () => {
    const wrapper = await render(); await flushPromises()
    const input = wrapper.get<HTMLTextAreaElement>('[data-testid="home-composer-input"]')
    input.element.focus()
    await input.setValue('@wor')
    input.element.setSelectionRange(4, 4)
    await input.trigger('input')
    expect(wrapper.find('[data-testid="solo-reference-picker"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="solo-reference-workflow"]').exists()).toBe(false)
    await input.setValue('/workflow 修复 CI')
    await wrapper.get('[data-testid="home-composer"]').trigger('submit'); await flushPromises()
    await vi.waitFor(() => expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({ text: '/workflow 修复 CI' })))
    expect(mocks.replace).not.toHaveBeenCalledWith(expect.objectContaining({ query: expect.objectContaining({ workflow: expect.anything() }) }))
    expect(mocks.startRun).not.toHaveBeenCalled()
  })
  it('dismisses the @ picker without creating a Solo and keeps slashes, Shift+Enter and IME as text', async () => {
    const wrapper = await render(); await flushPromises()
    const input = wrapper.get<HTMLTextAreaElement>('[data-testid="home-composer-input"]')
    input.element.focus()
    await input.setValue('@')
    input.element.setSelectionRange(1, 1)
    await input.trigger('input')
    await input.trigger('keydown', { key: 'Enter', shiftKey: true })
    await input.trigger('keydown', { key: 'Enter', isComposing: true })
    await input.trigger('compositionstart')
    await input.trigger('keydown', { key: 'Enter' })
    await input.trigger('compositionend')
    expect(wrapper.find('[data-testid="solo-reference-picker"]').exists()).toBe(true)
    await input.trigger('keydown', { key: 'Escape' })
    expect(wrapper.find('[data-testid="solo-reference-picker"]').exists()).toBe(false)
    expect(input.element.value).toBe('@')
    for (const text of ['/', '/wor', '/workspace/file', '处理 /workflow', '/unknown']) {
      await input.setValue(text)
      input.element.setSelectionRange(text.length, text.length)
      await input.trigger('input')
      expect(wrapper.find('[data-testid="home-command-menu"]').exists()).toBe(false)
      expect(wrapper.find('[data-testid="solo-reference-picker"]').exists()).toBe(false)
    }
    expect(mocks.create).not.toHaveBeenCalled()
    expect(mocks.startRun).not.toHaveBeenCalled()
  })
  it('restores a saved draft without launching anything', async () => {
    await saveHomeComposerDraft('待续的草稿', [], 'legacy-workflow')
    const wrapper = await render(); await flushPromises()
    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe('待续的草稿')
    expect(mocks.create).not.toHaveBeenCalled()
    expect(mocks.startRun).not.toHaveBeenCalled()
  })
  it('sends attachment-only drafts and keeps image preview/removal available', async () => {
    await saveHomeComposerDraft('', [{ name: 'draft.png', mimeType: 'image/png', data: 'eA==' }], '')
    const wrapper = await render(); await flushPromises()
    expect(wrapper.find('[data-testid="home-draft-image-thumb"]').exists()).toBe(true)
    await wrapper.get('[data-testid="home-composer"]').trigger('submit'); await flushPromises()
    await vi.waitFor(() => expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({ images: [expect.objectContaining({ name: 'draft.png', data: 'eA==' })] })))
  })
})

const clarifyWorkflow = {
  id: 'wf-ap', name: '需求澄清', description: '开发前澄清 + 计划', status: 'published', version: 1, updatedAt: '', needsRepo: false, showOnHome: true,
  nodes: [
    { id: 'in', type: 'input', label: '开始', position: { x: 0, y: 0 }, config: {} },
    { id: 'ap', type: 'agent', label: '澄清', position: { x: 0, y: 0 }, config: { agent_profile: 'clarifier' } },
    { id: 'out', type: 'output', label: '结束', position: { x: 0, y: 0 }, config: {} },
  ],
  edges: [{ id: 'e1', source: 'in', target: 'ap' }, { id: 'e2', source: 'ap', target: 'out' }],
}

describe('Workflow home mode', () => {
  beforeEach(() => {
    localStorage.setItem('grasp.home.mode', 'workflow')
    mocks.listWorkflows.mockResolvedValue([clarifyWorkflow])
    mocks.startRun.mockResolvedValue({ id: 'run-1', status: 'queued' })
  })

  it('restores the workflow rail and starts a Run from the shared composer', async () => {
    const wrapper = mountHome(); await flushPromises()
    expect(wrapper.get('[data-testid="home-mode-workflow"]').attributes('aria-selected')).toBe('true')
    await vi.waitFor(() => expect(wrapper.find('[data-testid="home-workflow-card-wf-ap"]').exists()).toBe(true))
    expect(wrapper.get('[data-testid="home-workflow-card-wf-ap"]').classes()).toContain('home-shell__card--selected')
    expect(wrapper.findComponent({ name: 'HomeWorkflowSelect' }).exists()).toBe(true)
    expect(wrapper.findComponent({ name: 'SoloAgentSelect' }).exists()).toBe(false)
    expect(wrapper.findAll('[data-testid="home-composer"]')).toHaveLength(1)

    await wrapper.get('[data-testid="home-composer-input"]').setValue('修复登录接口')
    await wrapper.get('[data-testid="home-composer"]').trigger('submit'); await flushPromises()
    await vi.waitFor(() => expect(mocks.startRun).toHaveBeenCalledTimes(1))
    expect(mocks.startRun.mock.calls[0]?.[0]).toBe('wf-ap')
    expect(mocks.startRun.mock.calls[0]?.[5]).toMatchObject({ firstMessage: { text: '修复登录接口' }, publishedSnapshot: true })
    expect(mocks.create).not.toHaveBeenCalled()
    await vi.waitFor(() => expect(mocks.push).toHaveBeenCalled())
    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe('')
    expect(await loadHomeComposerDraft()).toBeNull()
  })

  it('keeps the draft when switching to Solo and remembers the mode', async () => {
    const wrapper = mountHome(); await flushPromises()
    await wrapper.get('[data-testid="home-composer-input"]').setValue('先写一半')
    await wrapper.get('[data-testid="home-mode-solo"]').trigger('click'); await flushPromises()
    expect(localStorage.getItem('grasp.home.mode')).toBe('solo')
    await vi.waitFor(() => expect(wrapper.findComponent({ name: 'SoloAgentSelect' }).exists()).toBe(true))
    expect(wrapper.find('[data-testid="home-workflow-cards"]').exists()).toBe(false)
    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe('先写一半')
  })

  it('hides the switch and stays on Solo when workflows are disabled', async () => {
    setWorkflowFeature({ enabled: false, locked: true, source: 'config' })
    const wrapper = mountHome(); await flushPromises()
    expect(wrapper.find('[data-testid="home-mode-switch"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="home-workflow-cards"]').exists()).toBe(false)
    expect(wrapper.findComponent({ name: 'SoloAgentSelect' }).exists()).toBe(true)
  })
})

