// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import enCommon from '@/locales/en/common.json'
import enPages from '@/locales/en/pages.json'

const mocks = vi.hoisted(() => ({ create: vi.fn(), startRun: vi.fn(), listWorkflows: vi.fn(), push: vi.fn(), replace: vi.fn() }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push: mocks.push, replace: mocks.replace }), RouterLink: { props: ['to'], template: '<a><slot /></a>' } }))
vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return { ...actual, api: { ...actual.api,
    listAgents: vi.fn(async () => [{ name: 'Solo', acpBackend: 'cursor' }]),
    listProjects: vi.fn(async () => []),
    listSoloSessions: vi.fn(async () => ({ items: [], maxSandboxes: 5 })),
    listModelConfigurations: vi.fn(async () => ({ items: [] })),
    listModelConnections: vi.fn(async () => ({ items: [] })),
    listCredentials: vi.fn(async () => ({ items: [] })),
    cliModels: vi.fn(async () => ({ models: [] })),
    createSoloSession: mocks.create, startRun: mocks.startRun, listWorkflows: mocks.listWorkflows,
  } }
})

import DashboardView from './DashboardView.vue'
import { setBrandSettings } from '@/lib/composables/useBrandSettings'
import { setWorkflowFeature } from '@/lib/composables/useWorkflowFeature'
import { __resetDraftIdbForTests, __setDraftIdbBackendForTests, createMemoryDraftIdb } from '@/lib/run/draftIdb'
import { loadHomeComposerDraft, saveHomeComposerDraft } from '@/lib/run/homeComposerDraft'

const mounted: ReturnType<typeof mount>[] = []
function render(locale = 'zh-CN') {
  const wrapper = mount(DashboardView, { attachTo: document.body, global: {
    plugins: [createI18n({ legacy: false, locale, missingWarn: false, fallbackWarn: false, messages: { 'zh-CN': { ...common, ...pages }, en: { ...enCommon, ...enPages } } })],
    stubs: { HomeParticleMeshBackground: true, HomeYumeCharacter: true, SoloLimitModal: true, ProjectDrawer: true },
  } })
  mounted.push(wrapper)
  return wrapper
}

beforeEach(() => {
  vi.clearAllMocks()
  localStorage.clear()
  localStorage.setItem('grasp.home.mode', 'workflow') // old preference must not restore the removed entry
  localStorage.setItem('grasp.home.lastSoloAgent', 'Solo')
  __setDraftIdbBackendForTests(createMemoryDraftIdb())
  setBrandSettings(null)
  setWorkflowFeature({ enabled: true, locked: false, source: 'config' })
  mocks.create.mockResolvedValue({ id: 'solo-new', agentName: 'Solo', status: 'starting', createdAt: '', updatedAt: '' })
})
afterEach(() => {
  for (const wrapper of mounted.splice(0)) wrapper.unmount()
  __resetDraftIdbForTests()
  vi.useRealTimers()
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('Solo home entry', () => {
  it('ignores the legacy workflow mode and shows only Solo controls', async () => {
    const wrapper = render(); await flushPromises()
    expect(wrapper.find('[data-testid="home-mode-switch"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="home-workflow-cards"]').exists()).toBe(false)
    expect(wrapper.findComponent({ name: 'SoloAgentSelect' }).exists()).toBe(true)
    expect(wrapper.findComponent({ name: 'SoloProjectSelect' }).exists()).toBe(true)
    expect(mocks.listWorkflows).not.toHaveBeenCalled()
    expect(mocks.create).not.toHaveBeenCalled()
    expect(mocks.startRun).not.toHaveBeenCalled()
  })
  it('keeps the configured hero copy and the existing character', async () => {
    setBrandSettings({ home_subtitle: '继续完成你的想法' })
    const wrapper = render(); await flushPromises()
    expect(wrapper.get('[data-testid="home-hero-copy"]').text()).toBe('继续完成你的想法')
    expect(wrapper.findComponent({ name: 'HomeYumeCharacter' }).exists()).toBe(true)
    expect(wrapper.get('[data-testid="home-brand-block"]').text()).not.toContain('Grasp')
  })
  it('uses the locale default when the configured subtitle is blank', async () => {
    setBrandSettings({ home_subtitle: '   ' })
    const wrapper = render('en'); await flushPromises()
    expect(wrapper.get('[data-testid="home-hero-copy"]').text()).toBe(enPages.pages.dashboard.heroSubtitle)
  })
  it('starts a Solo with the text after the explicit submit and clears the draft', async () => {
    const wrapper = render(); await flushPromises()
    await wrapper.get('[data-testid="home-composer-input"]').setValue('继续评估 CI')
    expect(mocks.create).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="home-composer"]').trigger('submit'); await flushPromises()
    expect(mocks.create).toHaveBeenCalledTimes(1)
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({ agentName: 'Solo', text: '继续评估 CI' }))
    expect(mocks.startRun).not.toHaveBeenCalled()
    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe('')
    expect(await loadHomeComposerDraft()).toBeNull()
  })
  it('keeps a failed launch draft and allows retry', async () => {
    mocks.create.mockRejectedValueOnce(new Error('unavailable'))
    const wrapper = render(); await flushPromises()
    await wrapper.get('textarea').setValue('不要丢掉这段话')
    await wrapper.get('[data-testid="home-composer"]').trigger('submit'); await flushPromises()
    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe('不要丢掉这段话')
    expect(wrapper.get('[data-testid="home-solo-error"]').text()).toContain('unavailable')
    await wrapper.get('[data-testid="home-composer"]').trigger('submit'); await flushPromises()
    expect(mocks.create).toHaveBeenCalledTimes(2)
  })
  it('Ctrl/Meta+Enter sends while plain Enter and IME composition preserve input', async () => {
    const wrapper = render(); await flushPromises()
    const input = wrapper.get('textarea'); await input.setValue('输入')
    await input.trigger('keydown', { key: 'Enter' }); await input.trigger('keydown', { key: 'Enter', shiftKey: true })
    await input.trigger('compositionstart'); await input.trigger('keydown', { key: 'Enter', ctrlKey: true })
    expect(mocks.create).not.toHaveBeenCalled()
    await input.trigger('compositionend'); await input.trigger('keydown', { key: 'Enter', metaKey: true }); await flushPromises()
    expect(mocks.create).toHaveBeenCalledTimes(1)
  })
  it('opens /workflow configuration without launching a Run or sending it to the model', async () => {
    const wrapper = render(); await flushPromises()
    await wrapper.get('textarea').setValue('/workflow 修复 CI')
    await wrapper.get('[data-testid="home-composer"]').trigger('submit'); await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({ text: undefined }))
    expect(mocks.replace).toHaveBeenCalledWith({ path: '/solo/solo-new', query: { workflow: '修复 CI' } })
    expect(mocks.startRun).not.toHaveBeenCalled()
  })
  it('rejects the slash entry when workflows are disabled without creating compute', async () => {
    setWorkflowFeature({ enabled: false, locked: false, source: 'db' })
    const wrapper = render(); await flushPromises()
    await wrapper.get('textarea').setValue('/workflow')
    await wrapper.get('[data-testid="home-composer"]').trigger('submit'); await flushPromises()
    expect(mocks.create).not.toHaveBeenCalled()
    expect(mocks.startRun).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="home-solo-error"]').text()).toContain('已关闭')
  })
  it('restores a saved draft without launching anything', async () => {
    await saveHomeComposerDraft('待续的草稿', [], 'legacy-workflow')
    const wrapper = render(); await flushPromises()
    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe('待续的草稿')
    expect(mocks.create).not.toHaveBeenCalled()
    expect(mocks.startRun).not.toHaveBeenCalled()
  })
  it('sends attachment-only drafts and keeps image preview/removal available', async () => {
    await saveHomeComposerDraft('', [{ name: 'draft.png', mimeType: 'image/png', data: 'eA==' }], '')
    const wrapper = render(); await flushPromises()
    expect(wrapper.find('[data-testid="home-draft-image-thumb"]').exists()).toBe(true)
    await wrapper.get('[data-testid="home-composer"]').trigger('submit'); await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({ images: [expect.objectContaining({ name: 'draft.png', data: 'eA==' })] }))
  })
})
