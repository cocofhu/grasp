// @vitest-environment happy-dom
import { flushPromises, mount } from '@vue/test-utils'
import { computed, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import snapshots from '@/locales/zh-CN/workspaceSnapshots.json'
import type { soloEmbedClient } from '@/lib/solo/soloEmbedClient'

const mocks = vi.hoisted(() => ({ send: vi.fn(), startRun: vi.fn() }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }), RouterLink: { props: ['to'], template: '<a><slot /></a>' } }))
vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return { ...actual, api: { ...actual.api, startRun: mocks.startRun } }
})
vi.mock('@/lib/solo/useSessionModelCatalog', () => ({
  useSessionModelCatalog: () => ({ choices: ref([]), ready: ref(true), blocked: ref(false), configLoaded: ref(false), catalogConfirmed: ref(false), defaultConfigurationId: ref(''), modelInConfiguration: () => undefined }),
}))
vi.mock('@/lib/solo/useSoloSessions', () => ({ useSoloSessions: () => ({ upsert: vi.fn(), remove: vi.fn() }) }))
vi.mock('@/lib/solo/useSoloChat', () => ({
  useSoloChat: () => ({
    hasEarlier: ref(false), loadingEarlier: ref(false), earlierError: ref(''), processPages: ref({}),
    loadEarlier: vi.fn(), loadProcess: vi.fn(),
    session: ref({ id: 'solo-1', title: '当前对话', agentName: 'Coder', status: 'running', artifactRunId: '', repos: [] }),
    turns: ref([]), queued: ref([]), displayQueued: ref([]), busy: ref(false), phase: ref(''), link: ref('open'),
    loading: ref(false), loadError: ref(''), actionError: ref(''), status: ref('running'), closed: computed(() => false),
    canSend: computed(() => true), currentModelId: ref(''), currentReasoningEffort: ref(''), modelChangePending: ref(false),
    setCurrentReasoningEffort: vi.fn(), load: vi.fn(), send: mocks.send, setCurrentModel: vi.fn(), removeQueued: vi.fn(),
  }),
}))

import SoloSessionPane from './SoloSessionPane.vue'
import { setWorkflowFeature } from '@/lib/composables/useWorkflowFeature'

const mounted: ReturnType<typeof mount>[] = []
function render(embedded = false) {
  const wrapper = mount(SoloSessionPane, {
    attachTo: document.body,
    props: { sessionId: 'solo-1', ...(embedded ? { embedded: {} as ReturnType<typeof soloEmbedClient> } : {}) },
    global: {
      plugins: [createI18n({ legacy: false, locale: 'zh-CN', missingWarn: false, fallbackWarn: false, messages: { 'zh-CN': { ...common, ...pages, ...snapshots } } })],
      stubs: {
        SoloWorkflowPanel: true,
        ReviewShell: { template: '<div><slot name="sidebar" /></div>' },
        Icon: true, ReactArtifactStage: true, AgentTimeline: true, SoloStartupCard: true, SoloLimitModal: true,
        AppModal: true, AppDrawer: true, ChatImagePreviewModal: true, SoloModelSelect: true, ChatMessageRail: true,
      },
    },
  })
  mounted.push(wrapper)
  return wrapper
}

beforeEach(() => {
  vi.clearAllMocks()
  setWorkflowFeature({ enabled: true, locked: false, source: 'config' })
  Element.prototype.scrollIntoView = vi.fn()
})
afterEach(() => {
  mounted.splice(0).forEach(wrapper => wrapper.unmount())
  document.body.innerHTML = ''
})

describe('SoloSessionPane workspace wiring', () => {
  it('opens snapshots from the sidebar intent without a persistent workspace toolbar', async () => {
    const wrapper = render(); await flushPromises()
    expect(wrapper.find('[data-testid="solo-workspace-history"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="solo-stop-environment"]').exists()).toBe(false)
    await wrapper.setProps({ workspaceIntent: 'snapshots' })
    const modal = wrapper.findComponent({ name: 'AppModal' })
    expect(modal.props()).toMatchObject({ open: true, title: '快照', width: 560, trapFocus: true, closeOnEsc: true })
    expect(wrapper.emitted('workspaceOpened')).toHaveLength(1)
    await wrapper.setProps({ workspaceIntent: undefined })
    modal.vm.$emit('close')
    await flushPromises()
    expect(modal.props('open')).toBe(false)
    await wrapper.setProps({ workspaceIntent: 'snapshots' })
    expect(modal.props('open')).toBe(true)
    expect(mocks.send).not.toHaveBeenCalled()
  })

  it('ignores workspace intents in an embedded chat', async () => {
    const wrapper = render(true)
    await wrapper.setProps({ workspaceIntent: 'snapshots' })
    expect(wrapper.findComponent({ name: 'AppModal' }).props('open')).toBe(false)
    expect(wrapper.emitted('workspaceOpened')).toBeUndefined()
  })

  it('offers no workflow entry in @ and sends /workflow as a normal message', async () => {
    const wrapper = render(); await flushPromises()
    const input = wrapper.get<HTMLTextAreaElement>('[data-testid="solo-input"]')
    input.element.focus()
    await input.setValue('@wor')
    input.element.setSelectionRange(4, 4)
    await input.trigger('input')
    expect(wrapper.find('[data-testid="solo-reference-picker"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="solo-reference-workflow"]').exists()).toBe(false)
    await input.setValue('/workflow 修复 CI')
    await input.trigger('keydown', { key: 'Enter' }); await flushPromises()
    expect(mocks.send).toHaveBeenCalledWith('/workflow 修复 CI', [], [], [])
    expect(mocks.startRun).not.toHaveBeenCalled()
  })
})
