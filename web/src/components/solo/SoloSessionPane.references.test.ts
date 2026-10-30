// @vitest-environment happy-dom
import { flushPromises, mount } from '@vue/test-utils'
import { computed, h, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { SoloTurn } from '@/lib/solo/acpTurn'

const box = vi.hoisted(() => ({
  send: vi.fn(async (..._args: unknown[]) => true),
  search: vi.fn(),
  turns: undefined as undefined | import('vue').Ref<SoloTurn[]>,
  queued: undefined as undefined | import('vue').Ref<unknown[]>,
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() }),
  RouterLink: { props: ['to'], setup: (props: { to: string }, { slots }: { slots: { default?: () => unknown } }) => () => h('a', { href: props.to }, slots.default?.() as never) },
}))
vi.mock('@/lib/api/api', () => ({
  api: {
    runArtifacts: vi.fn(async () => []),
    getSoloEnv: vi.fn(async () => []),
    listAgents: vi.fn(async () => []),
    listModelConfigurations: vi.fn(async () => ({ items: [] })),
    listModelConnections: vi.fn(async () => ({ items: [] })),
    cliModels: vi.fn(async () => ({ models: [] })),
    searchSoloReferences: box.search,
  },
}))
vi.mock('@/lib/solo/useSessionModelCatalog', () => ({
  useSessionModelCatalog: () => ({
    choices: ref([]),
    ready: ref(true),
    blocked: ref(false),
    configLoaded: ref(false),
    catalogConfirmed: ref(false),
    defaultConfigurationId: ref(''),
    modelInConfiguration: () => undefined,
  }),
}))
vi.mock('@/lib/solo/useSoloSessions', () => ({ useSoloSessions: () => ({ upsert: vi.fn(), remove: vi.fn() }) }))
vi.mock('@/lib/solo/useSoloChat', () => ({
  useSoloChat: () => ({
    session: ref({
      id: 's1', title: 'Solo', agentName: 'Coder', status: 'running', artifactRunId: '', projectId: 'p1',
      repos: [{ position: 0, projectId: 'p1', name: 'web', repoUrl: 'https://github.com/acme/web.git', repoDirName: 'web', branch: 'main' }],
    }),
    turns: box.turns,
    queued: box.queued,
    busy: ref(false),
    phase: ref(''),
    link: ref('open'),
    loading: ref(false),
    loadError: ref(''),
    actionError: ref(''),
    status: ref('running'),
    closed: computed(() => false),
    canSend: computed(() => true),
    currentModelId: ref(''),
    currentReasoningEffort: ref(''),
    modelChangePending: ref(false),
    setCurrentReasoningEffort: vi.fn(),
    load: vi.fn(),
    send: box.send,
    setCurrentModel: vi.fn(),
    removeQueued: vi.fn(),
  }),
}))

import SoloSessionPane from './SoloSessionPane.vue'
import pages from '@/locales/zh-CN/pages.json'

box.turns = ref<SoloTurn[]>([])
box.queued = ref<unknown[]>([])

function mountPane() {
  return mount(SoloSessionPane, {
    attachTo: document.body,
    props: { sessionId: 's1' },
    global: {
      plugins: [createI18n({ legacy: false, locale: 'zh-CN', missingWarn: false, fallbackWarn: false, messages: { 'zh-CN': pages } })],
      stubs: {
        Icon: true,
        ReviewShell: { template: '<div><slot name="actions" /><slot name="sidebar" /></div>' },
        ReactArtifactStage: true,
        AgentTimeline: true,
        SoloStartupCard: true,
        SoloLimitModal: true,
        AppModal: true,
        ChatImagePreviewModal: true,
        SoloModelSelect: true,
        ChatMessageRail: true,
      },
    },
  })
}

async function type(w: ReturnType<typeof mountPane>, value: string, caret = value.length) {
  const ta = w.find('textarea')
  const el = ta.element as HTMLTextAreaElement
  await ta.setValue(value)
  el.setSelectionRange(caret, caret)
  await ta.trigger('input')
  await new Promise((r) => setTimeout(r, 160))
  await flushPromises()
}

function key(w: ReturnType<typeof mountPane>, k: string, extra: KeyboardEventInit = {}) {
  return w.find('textarea').trigger('keydown', { key: k, ...extra })
}

beforeEach(() => {
  box.send.mockClear()
  box.search.mockReset()
  box.search.mockImplementation(async (_id: string, p: { lane: string }) =>
    p.lane === 'solo'
      ? { lane: 'solo', items: [{ kind: 'solo', target: 's2', title: '登录重构', status: 'closed' }] }
      : { lane: 'github', items: [{ kind: 'github_pr', target: 'acme/web#7', title: 'Fix', state: 'open', url: 'u', comments: 0, repo: 'acme/web', number: 7 }] })
  Element.prototype.scrollIntoView = vi.fn()
  box.turns!.value = []
  box.queued!.value = []
})

describe('SoloSessionPane @ references', () => {
  it('opens the picker on @, picks with the keyboard and sends the reference', async () => {
    const w = mountPane()
    await type(w, '看看 @登')
    expect(w.find('[data-testid="solo-reference-picker"]').exists()).toBe(true)
    expect(box.search).toHaveBeenCalledWith('s1', expect.objectContaining({ lane: 'solo', q: '登' }), expect.anything())

    await key(w, 'Tab')
    await key(w, 'Tab', { shiftKey: true })
    await key(w, 'ArrowDown')
    await key(w, 'Enter')
    await flushPromises()
    const ta = w.find('textarea').element as HTMLTextAreaElement
    expect(ta.value).toBe('看看 @登录重构 ')
    expect(w.find('[data-testid="solo-reference-picker"]').exists()).toBe(false)
    expect(w.findAll('[data-testid="solo-composer-references"] [data-testid="solo-reference-chip"]').length).toBe(1)

    await key(w, 'Enter')
    await flushPromises()
    expect(box.send).toHaveBeenCalledWith('看看 @登录重构', [], [{ kind: 'solo', target: 's2', title: '登录重构' }])
    expect(ta.value).toBe('')
  })

  it('Enter without a highlight sends the typed text; Escape dismisses the picker', async () => {
    const w = mountPane()
    await type(w, 'mail me@x')
    expect(w.find('[data-testid="solo-reference-picker"]').exists()).toBe(false)
    await type(w, 'a @b')
    await key(w, 'Escape')
    expect(w.find('[data-testid="solo-reference-picker"]').exists()).toBe(false)
    await key(w, 'Enter')
    await flushPromises()
    expect(box.send).toHaveBeenCalledWith('a @b', [], [])
  })

  it('turns a pasted GitHub URL into a short #n token and drops it on edit', async () => {
    const w = mountPane()
    const ta = w.find('textarea')
    const el = ta.element as HTMLTextAreaElement
    el.setSelectionRange(0, 0)
    const paste = new Event('paste', { cancelable: true }) as ClipboardEvent
    Object.defineProperty(paste, 'clipboardData', { value: { getData: () => 'https://github.com/acme/web/pull/7', items: [], files: [] } })
    el.dispatchEvent(paste)
    await flushPromises()
    expect(paste.defaultPrevented).toBe(true)
    expect(el.value).toBe('@#7 ')
    expect(w.find('[data-testid="solo-reference-chip"]').text()).toContain('#7')

    el.setSelectionRange(3, 3)
    await key(w, 'Backspace')
    await flushPromises()
    expect(el.value).toBe(' ')
    expect(w.find('[data-testid="solo-composer-references"]').exists()).toBe(false)
  })

  it('opens the picker from the + menu, and chips can be removed', async () => {
    const w = mountPane()
    await w.find('[data-testid="solo-plus"]').trigger('click')
    await w.find('[data-testid="solo-plus-github"]').trigger('click')
    await new Promise((r) => setTimeout(r, 300))
    await flushPromises()
    expect(w.find('[data-testid="solo-reference-tab-issue"]').classes()).toContain('ref-picker__tab--on')
    await w.find('[data-testid="solo-reference-tab-pr"]').trigger('click')
    await new Promise((r) => setTimeout(r, 300))
    await flushPromises()
    await w.find('[data-testid="solo-reference-option-acme/web#7"]').trigger('click')
    await flushPromises()
    expect((w.find('textarea').element as HTMLTextAreaElement).value).toBe('@#7 ')
    expect(w.find('[data-testid="solo-send"]').attributes('disabled')).toBeUndefined()
    await w.find('[data-testid="solo-composer-references"] button').trigger('click')
    expect((w.find('textarea').element as HTMLTextAreaElement).value).toBe('')
  })

  it('renders reference chips on history turns and queued rows', async () => {
    box.turns!.value = [{ id: 'u1', role: 'user', text: '@登录重构', streaming: false, steps: [], plan: [], references: [{ id: 'ref-1', kind: 'solo', target: 's2', title: '登录重构', messages: 4 }] } as unknown as SoloTurn]
    box.queued!.value = [{ id: 'q1', text: '', references: [{ kind: 'github_issue', target: 'acme/web#3', title: 'Bug', access: 'denied', error: 'forbidden' }] }]
    const w = mountPane()
    await flushPromises()
    const chip = w.find('[data-testid="solo-turn-references"] [data-testid="solo-reference-chip"]')
    expect(chip.attributes('href')).toBe('/solo/s2')
    expect(chip.attributes('title')).toContain('4 条消息')
    const queued = w.find('[data-testid="solo-queue"] [data-testid="solo-reference-chip"]')
    expect(queued.text()).toContain('#3')
    expect(queued.attributes('title')).toContain('无权访问')
  })
})
