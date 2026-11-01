// @vitest-environment happy-dom
import { flushPromises, mount } from '@vue/test-utils'
import { computed, h, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { SoloQueueItem } from '@/lib/solo/useSoloChat'

const box = vi.hoisted(() => ({
  calls: [] as string[],
  send: vi.fn(async (..._args: unknown[]) => true),
  removeQueued: vi.fn(async (_id: string) => undefined),
  reorderQueued: vi.fn(async (_from: number, _to: number) => undefined),
  takeQueued: vi.fn(async (_id: string): Promise<unknown> => null),
  queued: undefined as undefined | import('vue').Ref<SoloQueueItem[]>,
  displayQueued: undefined as undefined | import('vue').Ref<SoloQueueItem[]>,
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() }),
  RouterLink: { props: ['to'], setup: (props: { to: string }, { slots }: { slots: { default?: () => unknown } }) => () => h('a', { href: props.to }, slots.default?.() as never) },
}))
vi.mock('@/lib/api/api', () => ({
  blobContentUrl: (ref: string) => `/api/blobs/${ref.slice(5)}`,
  api: {
    runArtifacts: vi.fn(async () => []),
    searchSoloReferences: vi.fn(async () => ({ lane: 'solo', items: [] })),
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
    turns: ref([]),
    queued: box.queued,
    displayQueued: box.displayQueued,
    busy: ref(true),
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
    removeQueued: box.removeQueued,
    reorderQueued: box.reorderQueued,
    takeQueued: box.takeQueued,
  }),
}))

import SoloSessionPane from './SoloSessionPane.vue'
import pages from '@/locales/zh-CN/pages.json'

box.queued = ref<SoloQueueItem[]>([])
box.displayQueued = ref<SoloQueueItem[]>([])

const issue = { kind: 'github_issue' as const, target: 'acme/web#3', title: 'Bug', id: 'ref-1', access: 'ok' as const }
const image = { mimeType: 'image/png', ref: 'blob:abc', name: 'shot.png' }
const qa: SoloQueueItem = { id: 'qa', text: '修一下 @#3', images: [image], references: [issue] }
const qb: SoloQueueItem = { id: 'qb', text: 'second', images: [] }
const local: SoloQueueItem = { id: 'local-1', text: 'not yet', images: [], local: true }

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

const rows = (w: ReturnType<typeof mountPane>) => w.findAll('[data-testid="solo-queue"] [data-testid="clarify-queue-item"]')
const textarea = (w: ReturnType<typeof mountPane>) => w.find('textarea').element as HTMLTextAreaElement

beforeEach(() => {
  box.calls = []
  box.send.mockReset()
  box.send.mockImplementation(async (text: unknown) => {
    box.calls.push(`send:${String(text)}`)
    return true
  })
  box.removeQueued.mockClear()
  box.reorderQueued.mockClear()
  box.takeQueued.mockReset()
  box.takeQueued.mockImplementation(async (id: string) => {
    box.calls.push(`take:${id}`)
    const item = box.queued!.value.find((q) => q.id === id)
    return item ? { ...item, images: item.images.slice() } : null
  })
  box.queued!.value = [qa, qb, local]
  // A drag not yet confirmed by the server: display order differs from queued.
  box.displayQueued!.value = [qb, qa, local]
  document.body.innerHTML = ''
})

describe('SoloSessionPane queue', () => {
  it('renders the shared queue panel in display order with locked local rows and reference chips', async () => {
    const w = mountPane()
    await flushPromises()
    expect(w.find('[data-testid="solo-queue-panel"]').exists()).toBe(true)
    const list = rows(w)
    expect(list.map((row) => row.text())).toEqual([expect.stringContaining('second'), expect.stringContaining('修一下'), expect.stringContaining('not yet')])
    expect(list[1]!.find('[data-testid="solo-reference-chip"]').text()).toContain('#3')
    expect(list[2]!.attributes('draggable')).toBe('false')
    expect(list[2]!.findAll('button')).toHaveLength(0)
    expect(list[0]!.find('[data-testid="clarify-queue-edit"]').exists()).toBe(true)
  })

  it('hides the queue panel when nothing is queued', async () => {
    box.queued!.value = []
    box.displayQueued!.value = []
    const w = mountPane()
    await flushPromises()
    expect(w.find('[data-testid="solo-queue"]').exists()).toBe(false)
  })

  it('reorders and removes by display position', async () => {
    const w = mountPane()
    await flushPromises()
    const list = rows(w)
    await list[1]!.trigger('dragstart', { dataTransfer: { setData() {}, getData: () => '1' } })
    await list[0]!.trigger('drop', { dataTransfer: { getData: () => '1' } })
    expect(box.reorderQueued).toHaveBeenCalledWith(1, 0)
    await list[0]!.find('[data-testid="clarify-queue-cancel"]').trigger('click')
    expect(box.removeQueued).toHaveBeenCalledWith('qb')
  })

  it('withdraws a queued message into the composer with its text, images and references', async () => {
    const w = mountPane()
    await flushPromises()
    await rows(w)[1]!.find('[data-testid="clarify-queue-edit"]').trigger('click')
    await flushPromises()
    expect(box.takeQueued).toHaveBeenCalledWith('qa')
    expect(box.send).not.toHaveBeenCalled()
    expect(textarea(w).value).toBe('修一下 @#3')
    expect(w.findAll('[data-testid="solo-composer-references"] [data-testid="solo-reference-chip"]')).toHaveLength(1)
    expect(w.find('.pending-attach-remove').exists()).toBe(true)
    await w.find('[data-testid="solo-send"]').trigger('click')
    await flushPromises()
    expect(box.send).toHaveBeenCalledWith('修一下 @#3', [image], [issue])
  })

  it('queues an unsent draft before withdrawing the target', async () => {
    const w = mountPane()
    await flushPromises()
    await w.find('textarea').setValue('draft first')
    await rows(w)[0]!.find('[data-testid="clarify-queue-edit"]').trigger('click')
    await flushPromises()
    expect(box.calls).toEqual(['send:draft first', 'take:qb'])
    expect(textarea(w).value).toBe('second')
  })

  it('keeps the draft and skips the withdrawal when the draft fails to send', async () => {
    box.send.mockImplementation(async () => false)
    const w = mountPane()
    await flushPromises()
    await w.find('textarea').setValue('draft first')
    await rows(w)[0]!.find('[data-testid="clarify-queue-edit"]').trigger('click')
    await flushPromises()
    expect(box.takeQueued).not.toHaveBeenCalled()
    expect(textarea(w).value).toBe('draft first')
  })

  it('leaves the composer untouched when the withdrawal fails', async () => {
    box.takeQueued.mockImplementation(async () => null)
    const w = mountPane()
    await flushPromises()
    await rows(w)[1]!.find('[data-testid="clarify-queue-edit"]').trigger('click')
    await flushPromises()
    expect(box.takeQueued).toHaveBeenCalledWith('qa')
    expect(textarea(w).value).toBe('')
    expect(w.find('[data-testid="solo-composer-references"]').exists()).toBe(false)
    expect(w.find('.pending-attach-remove').exists()).toBe(false)
  })
})
