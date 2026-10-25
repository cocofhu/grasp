// @vitest-environment happy-dom
import { computed, defineComponent, nextTick, onMounted, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { InboxItem } from '@/lib/shared/types'

const mocks = vi.hoisted(() => ({
  listGates: vi.fn(),
  inboxContext: vi.fn(),
  resumeGate: vi.fn(),
  reactReply: vi.fn(),
  nodeEvents: vi.fn(),
  runArtifacts: vi.fn(),
  // Inbox context has nodes but no edges; the view asks GET /runs/:id before it
  // leaves the loading pane. This suite has no run record, same as a refused call.
  getRun: vi.fn(async () => {
    throw new Error('no run in this suite')
  }),
  runEventsWsUrl: vi.fn((id: string) => `ws://test/runs/${id}/events`),
  toastError: vi.fn(),
  toastSuccess: vi.fn(),
}))

const routeState = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports -- vi.hoisted runs before ESM imports
  const { reactive } = require('vue') as typeof import('vue')
  return reactive({ query: {} as Record<string, string> })
})

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      listGates: mocks.listGates,
      inboxContext: mocks.inboxContext,
      getRun: mocks.getRun,
      resumeGate: mocks.resumeGate,
      reactReply: mocks.reactReply,
      nodeEvents: mocks.nodeEvents,
      runArtifacts: mocks.runArtifacts,
      runEventsWsUrl: mocks.runEventsWsUrl,
      // Run chain unavailable → hand-off keeps main behaviour (next pending item).
      runConversationChain: async () => {
        throw new Error('chain unavailable in this suite')
      },
    },
  }
})

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() }),
  useRoute: () => ({ query: routeState.query }),
}))

vi.mock('@/lib/composables/useBreakpoint', async () => {
  const { ref } = await import('vue')
  return {
    useBreakpoint: () => ({ isMobile: ref(false) }),
  }
})

vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({
    error: mocks.toastError,
    success: mocks.toastSuccess,
    info: vi.fn(),
    warning: vi.fn(),
  }),
}))

const filterState = vi.hoisted(() => ({
  workflowSelected: null as { value: string } | null,
}))

vi.mock('@/lib/composables/useWorkflowFilter', async () => {
  const { ref } = await import('vue')
  filterState.workflowSelected = ref('')
  return {
    useWorkflowFilter: () => ({ selected: filterState.workflowSelected! }),
  }
})

vi.mock('@/lib/inbox/useClarifyDraft', async () => {
  const { ref } = await import('vue')
  return {
    useClarifyDraft: () => ({
      draft: ref(''),
      attachments: ref([]),
      annotations: ref([]),
    }),
  }
})

// Use the real usePendingGates singleton (api.listGates is mocked) so peek/force
// races and sidebar totalCount stay covered — do not stub the composable away.

import GatesInboxView from './GatesInboxView.vue'
import { beginInboxCancelDeparture, endInboxCancelDeparture } from '@/lib/inbox/inboxCancelDeparture'
import { usePendingGates } from '@/lib/inbox/usePendingGates'
import { setHomeApproveHandoff, takeHomeApproveHandoff } from '@/lib/run/homeApproveHandoff'
import { CLARIFY_CAPS, IMPLEMENT_CAPS } from '@/test/capsFixtures'

function paged(items: InboxItem[]) {
  return { items, total: items.length, page: 1, pageSize: 20 }
}

function gateItem(id: string, iteration = 1): InboxItem {
  return {
    type: 'gate',
    runId: `run-${id}`,
    nodeId: `gate-${id}`,
    iteration,
    workflowName: 'wf',
    title: `Gate ${id}`,
    bodyMd: '',
    actions: [{ id: 'approve', label: 'Approve' }],
    requestedAt: new Date().toISOString(),
    status: 'waiting_human' as any,
  } as InboxItem
}

function clarifyItem(id: string, iteration = 1): InboxItem {
  return {
    type: 'clarify',
    runId: `run-${id}`,
    nodeId: `clarify-${id}`,
    iteration,
    workflowName: 'wf',
    label: `Clarify ${id}`,
    done: false,
    requestedAt: new Date().toISOString(),
    updatedAt: new Date().toISOString(),
  }
}

class FakeWebSocket {
  static instances: FakeWebSocket[] = []
  onmessage: ((ev: { data: string }) => void) | null = null
  onclose: (() => void) | null = null
  readyState = 1
  url: string
  constructor(url: string) {
    this.url = url
    FakeWebSocket.instances.push(this)
  }
  close() {
    this.readyState = 3
    this.onclose?.()
  }
  emit(type: string, extra?: Record<string, unknown>) {
    this.onmessage?.({ data: JSON.stringify({ type, ...extra }) })
  }
}

const GateApprovalStub = defineComponent({
  name: 'GateApproval',
  emits: ['resolve', 'react-revised'],
  setup(_, { expose }) {
    // Real-form stand-in: typing into the comment makes the gate "editing".
    const comment = ref('')
    expose({
      isEditing: computed(() => comment.value.trim().length > 0),
      applyReviewFrame: (f: Record<string, unknown>) => {
        composerFrames.applied.push(f)
        if (f.event === 'turn_begin') composerFrames.busy = true
        if (f.event === 'turn_done' || f.event === 'error') composerFrames.busy = false
        if (f.event === 'queue_state' && typeof f.busy === 'boolean') composerFrames.busy = !!f.busy
      },
      applyAcpEvents: (events: unknown) => {
        composerFrames.acp.push({ events })
      },
      cancelReactRevise: () => {},
    })
    return { comment }
  },
  template:
    `<div>
      <textarea data-testid="gate-comment" v-model="comment"></textarea>
      <button data-testid="resolve-btn" @click="$emit('resolve', 'approve', {})">resolve</button>
    </div>`,
})

/** Captures applyReviewFrame / applyAcpEvents for hard-load restore assertions. */
const composerFrames = vi.hoisted(() => ({
  applied: [] as Record<string, unknown>[],
  acp: [] as { events: unknown; nodeId?: string }[],
  busy: false,
}))

const ReviewComposerStub = defineComponent({
  name: 'ReviewComposer',
  props: {
    seedHumanText: { type: String, default: '' },
    seedHumanImages: { type: Array, default: () => [] },
    nodeType: { type: String, default: '' },
    mode: { type: String, default: 'clarify' },
  },
  emits: ['send', 'finish', 'cancel'],
  setup(_, { expose }) {
    expose({
      applyReviewFrame: (f: Record<string, unknown>) => {
        composerFrames.applied.push(f)
        if (f.event === 'turn_begin') composerFrames.busy = true
        if (f.event === 'turn_done' || f.event === 'error') composerFrames.busy = false
        if (f.event === 'queue_state' && typeof f.busy === 'boolean') composerFrames.busy = !!f.busy
      },
      applyAcpEvents: (events: unknown, nodeId?: string) => {
        composerFrames.acp.push({ events, nodeId })
      },
      discardLastQueued: () => {},
      isSessionBusy: () => composerFrames.busy,
    })
    return {}
  },
  template:
    `<div data-testid="review-composer-stub">
      <button data-testid="clarify-send" @click="$emit('send', 'hi', [], [], true)">finish</button>
      <button data-testid="clarify-turn" @click="$emit('send', 'hi', [], [], false)">turn</button>
    </div>`,
})

/** Hang until aborted via opts.signal; otherwise never resolves. */
function hangInboxContext(_runId: string, _nodeId: string, _iteration?: number, opts?: { signal?: AbortSignal }) {
  const signal = opts?.signal
  if (!signal) return new Promise(() => {})
  if (signal.aborted) return Promise.reject(new DOMException('Aborted', 'AbortError'))
  return new Promise((_resolve, reject) => {
    signal.addEventListener(
      'abort',
      () => reject(new DOMException('Aborted', 'AbortError')),
      { once: true },
    )
  })
}

const reviewShellMounts = { n: 0 }

function mountInbox(opts: { attach?: boolean } = {}) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(GatesInboxView, {
    ...(opts.attach ? { attachTo: document.body } : {}),
    global: {
      plugins: [i18n],
      stubs: {
        Icon: true,
        EmptyState: true,
        WorkflowFilter: true,
        Pagination: true,
        ArtifactLoadingPane: true,
        GateApproval: GateApprovalStub,
        ReviewShell: defineComponent({
          setup() {
            onMounted(() => { reviewShellMounts.n++ })
          },
          template: '<div><slot name="stage" /><slot name="sidebar" /></div>',
        }),
        ReviewComposer: ReviewComposerStub,
        ClarifyProductStage: true,
        ReactArtifactStage: true,
      },
    },
  })
}

/** The active pane identity stays testable without exposing technical IDs in the chat. */
function activeDetail(wrapper: ReturnType<typeof mountInbox>) {
  const detail = wrapper.find('[data-testid="inbox-active-detail"]')
  return detail.exists()
    ? { runId: detail.attributes('data-run-id'), nodeId: detail.attributes('data-node-id') }
    : null
}

function inboxCallsFor(runId: string, nodeId: string, iteration = 1) {
  return mocks.inboxContext.mock.calls.filter(
    (c) => c[0] === runId && c[1] === nodeId && (c[2] ?? 1) === iteration,
  )
}

/** Sidebar row navigation: query selects the panel without a card column. */
async function selectByQuery(runId: string, nodeId: string) {
  routeState.query.run = runId
  routeState.query.node = nodeId
  await nextTick()
  await flushPromises()
}

/** Tag watch is the remaining list reload; the inbox top-bar refresh control is gone. */
async function reloadInboxList() {
  routeState.query.tag = String((Number(routeState.query.tag) || 0) + 1)
  await nextTick()
  await flushPromises()
  await nextTick()
  await flushPromises()
}

beforeEach(async () => {
  vi.clearAllMocks()
  endInboxCancelDeparture()
  routeState.query = {}
  composerFrames.applied = []
  composerFrames.acp = []
  composerFrames.busy = false
  FakeWebSocket.instances = []
  vi.stubGlobal('WebSocket', FakeWebSocket as unknown as typeof WebSocket)
  mocks.resumeGate.mockResolvedValue({ status: 'ok' })
  mocks.reactReply.mockResolvedValue({ status: 'ok' })
  mocks.nodeEvents.mockResolvedValue({ events: [], live: false })
  mocks.runArtifacts.mockResolvedValue([])
  if (filterState.workflowSelected) filterState.workflowSelected.value = ''
  // Reset singleton so each test starts from an empty pending badge/list.
  mocks.listGates.mockResolvedValue(paged([]))
  await usePendingGates().refresh({ mode: 'force' })
  mocks.inboxContext.mockImplementation(async (runId: string, nodeId: string) => {
    if (nodeId.startsWith('clarify-')) {
      return {
        type: 'clarify',
        status: 'waiting_human',
        nodes: [{ id: nodeId, type: 'agent', caps: CLARIFY_CAPS, label: nodeId }],
        artifacts: [],
        nodeExecutions: {},
        clarify: { nodeId, iteration: 1, turns: [], done: false, label: nodeId },
      }
    }
    return {
      type: 'gate',
      nodes: [],
      artifacts: [],
      nodeExecutions: {},
    }
  })
})

afterEach(() => {
  takeHomeApproveHandoff()
  vi.unstubAllGlobals()
})

describe('GatesInboxView inbox-context lifecycle', () => {
  it('gate resolve: stops requesting processed triple and selects neighbor', async () => {
    const a = gateItem('a')
    const b = gateItem('b')
    const c = gateItem('c')
    let list = [a, b, c]
    mocks.listGates.mockImplementation(async () => paged(list))

    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()

    // Select middle item b
    await selectByQuery('run-b', 'gate-b')

    const before = inboxCallsFor('run-b', 'gate-b', 1).length
    expect(before).toBeGreaterThan(0)

    list = [a, c]
    await wrapper.get('[data-testid="resolve-btn"]').trigger('click')
    await flushPromises()
    await nextTick()
    await flushPromises()

    const after = inboxCallsFor('run-b', 'gate-b', 1).length
    expect(after).toBe(before)

    // Neighbor after removing middle b is c
    expect(activeDetail(wrapper)).toEqual({ runId: 'run-c', nodeId: 'gate-c' })
    expect(inboxCallsFor('run-c', 'gate-c', 1).length).toBeGreaterThan(0)

    // WS status must not re-fetch processed triple
    const ws = FakeWebSocket.instances.find((w) => w.url.includes('run-b'))
    ws?.emit('status')
    await flushPromises()
    expect(inboxCallsFor('run-b', 'gate-b', 1).length).toBe(after)

    wrapper.unmount()
  })

  it('gate resolve on last item: selects previous neighbor', async () => {
    const a = gateItem('a')
    const b = gateItem('b')
    const c = gateItem('c')
    let list = [a, b, c]
    mocks.listGates.mockImplementation(async () => paged(list))

    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()

    await selectByQuery('run-c', 'gate-c')

    const before = inboxCallsFor('run-c', 'gate-c', 1).length
    expect(before).toBeGreaterThan(0)

    list = [a, b]
    await wrapper.get('[data-testid="resolve-btn"]').trigger('click')
    await flushPromises()
    await nextTick()
    await flushPromises()

    expect(inboxCallsFor('run-c', 'gate-c', 1).length).toBe(before)
    expect(activeDetail(wrapper)).toEqual({ runId: 'run-b', nodeId: 'gate-b' })
    expect(inboxCallsFor('run-b', 'gate-b', 1).length).toBeGreaterThan(0)
    wrapper.unmount()
  })

  it('gate resolve on sole item: clears active and never re-fetches', async () => {
    const only = gateItem('solo')
    let list: InboxItem[] = [only]
    mocks.listGates.mockImplementation(async () => paged(list))

    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()

    const before = inboxCallsFor('run-solo', 'gate-solo', 1).length
    expect(before).toBeGreaterThan(0)

    list = []
    await wrapper.get('[data-testid="resolve-btn"]').trigger('click')
    await flushPromises()
    await nextTick()
    await flushPromises()

    expect(inboxCallsFor('run-solo', 'gate-solo', 1).length).toBe(before)
    expect(wrapper.find('[data-testid="resolve-btn"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('clarify force finish: stops requesting processed triple', async () => {
    const a = clarifyItem('a')
    const b = clarifyItem('b')
    let list: InboxItem[] = [a, b]
    mocks.listGates.mockImplementation(async () => paged(list))

    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()

    const before = inboxCallsFor('run-a', 'clarify-a', 1).length
    expect(before).toBeGreaterThan(0)

    list = [b]
    await wrapper.get('[data-testid="clarify-send"]').trigger('click')
    await flushPromises()
    await nextTick()
    await flushPromises()

    expect(mocks.reactReply).toHaveBeenCalled()
    expect(inboxCallsFor('run-a', 'clarify-a', 1).length).toBe(before)
    // v3「流转了继续聊，不要跳」: the confirmed conversation stays (follows its run), b is not opened.
    expect(activeDetail(wrapper)).toEqual({ runId: 'run-a', nodeId: 'clarify-a' })
    expect(mocks.toastError).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('left-pending absence converges without toast and without retry loop', async () => {
    const a = gateItem('a')
    const b = gateItem('b')
    mocks.listGates.mockResolvedValue(paged([a, b]))
    mocks.inboxContext
      .mockResolvedValueOnce({
        type: 'gate',
        nodes: [],
        artifacts: [],
        nodeExecutions: {},
      })
      .mockResolvedValueOnce({ pending: false })

    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()

    // Trigger soft refresh via artifact_edit after initial load
    const ws = FakeWebSocket.instances[FakeWebSocket.instances.length - 1]
    expect(ws).toBeTruthy()
    ws.emit('artifact_edit')
    await flushPromises()
    await nextTick()
    await flushPromises()

    expect(mocks.toastError).not.toHaveBeenCalled()
    // Ghost row removed; active moves to neighbor b
    expect(activeDetail(wrapper)).not.toEqual({ runId: 'run-a', nodeId: 'gate-a' })
    expect(activeDetail(wrapper)).toEqual({ runId: 'run-b', nodeId: 'gate-b' })
    const callsForA = inboxCallsFor('run-a', 'gate-a', 1).length
    // Irrelevant status must not re-fetch the processed triple
    ws.emit('status')
    await flushPromises()
    expect(inboxCallsFor('run-a', 'gate-a', 1).length).toBe(callsForA)
    wrapper.unmount()
  })

  it('gate resolve with list lag: drops ghost and selects neighbor without re-fetch', async () => {
    const a = gateItem('a')
    const b = gateItem('b')
    const c = gateItem('c')
    // List lags: still returns a after resolve
    mocks.listGates.mockResolvedValue(paged([a, b, c]))

    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()

    const before = inboxCallsFor('run-a', 'gate-a', 1).length
    expect(before).toBeGreaterThan(0)

    await wrapper.get('[data-testid="resolve-btn"]').trigger('click')
    await flushPromises()
    await nextTick()
    await flushPromises()

    expect(inboxCallsFor('run-a', 'gate-a', 1).length).toBe(before)
    expect(activeDetail(wrapper)).not.toEqual({ runId: 'run-a', nodeId: 'gate-a' })
    expect(activeDetail(wrapper)).toEqual({ runId: 'run-b', nodeId: 'gate-b' })
    expect(inboxCallsFor('run-b', 'gate-b', 1).length).toBeGreaterThan(0)
    wrapper.unmount()
  })

  it('manual refresh after external remove: selects neighbor and skips old triple', async () => {
    const a = gateItem('a')
    const b = gateItem('b')
    const c = gateItem('c')
    let list = [a, b, c]
    mocks.listGates.mockImplementation(async () => paged(list))

    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()

    await selectByQuery('run-b', 'gate-b')

    const before = inboxCallsFor('run-b', 'gate-b', 1).length
    expect(before).toBeGreaterThan(0)

    // External removal: an idle peek applies on its own (no banner), reloads and picks the neighbor.
    list = [a, c]
    await usePendingGates().peek()
    await nextTick()
    await flushPromises()
    expect(wrapper.text()).not.toContain('有新审批待处理')
    expect(wrapper.findAll('button').some((btn) => btn.text().includes('立即刷新'))).toBe(false)
    await flushPromises()
    await nextTick()
    await flushPromises()

    expect(inboxCallsFor('run-b', 'gate-b', 1).length).toBe(before)
    expect(activeDetail(wrapper)).toEqual({ runId: 'run-c', nodeId: 'gate-c' })
    expect(inboxCallsFor('run-c', 'gate-c', 1).length).toBeGreaterThan(0)
    wrapper.unmount()
  })

  it('auto-refresh never touches the input: peek while typing keeps value, caret, focus and selection; applies after editing', async () => {
    const a = gateItem('a')
    const b = gateItem('b')
    const n = gateItem('new')
    let list = [a, b]
    mocks.listGates.mockImplementation(async () => paged(list))
    const pg = usePendingGates()

    const wrapper = mountInbox({ attach: true })
    await flushPromises()
    await nextTick()
    expect(activeDetail(wrapper)).toEqual({ runId: 'run-a', nodeId: 'gate-a' })

    const field = wrapper.get('[data-testid="gate-comment"]')
    const el = field.element as HTMLTextAreaElement
    el.focus()
    await field.setValue('请补充回归截图')
    el.setSelectionRange(3, 3)
    expect(document.activeElement).toBe(el)
    const listCallsBefore = mocks.listGates.mock.calls.length
    const contextCallsBefore = inboxCallsFor('run-a', 'gate-a', 1).length

    // A new approval arrives while the user types.
    list = [n, a, b]
    await pg.peek({ source: 'focus' })
    await nextTick()
    await flushPromises()

    // Input untouched: same element, same value, caret and focus; selection unchanged.
    const sameField = wrapper.get('[data-testid="gate-comment"]').element as HTMLTextAreaElement
    expect(sameField).toBe(el)
    expect(el.value).toBe('请补充回归截图')
    expect(el.selectionStart).toBe(3)
    expect(document.activeElement).toBe(el)
    expect(activeDetail(wrapper)).toEqual({ runId: 'run-a', nodeId: 'gate-a' })
    // Held: neither the sidebar snapshot nor the page list moved, no context reload, no banner.
    expect(pg.displayedItems.value.map((it) => it.runId)).toEqual(['run-a', 'run-b'])
    expect(pg.hasPendingUpdate.value).toBe(true)
    expect(mocks.listGates.mock.calls.length).toBe(listCallsBefore + 1) // the peek itself
    expect(inboxCallsFor('run-a', 'gate-a', 1).length).toBe(contextCallsBefore)
    expect(wrapper.text()).not.toContain('有新审批待处理')

    // Done editing: clear the draft and leave the field → list updates by itself.
    await field.setValue('')
    el.blur()
    await nextTick()
    await flushPromises()
    await nextTick()
    await flushPromises()
    expect(pg.hasPendingUpdate.value).toBe(false)
    expect(pg.displayedItems.value.map((it) => it.runId)).toEqual(['run-new', 'run-a', 'run-b'])
    expect(mocks.listGates.mock.calls.length).toBeGreaterThan(listCallsBefore + 1)
    expect(activeDetail(wrapper)).toEqual({ runId: 'run-a', nodeId: 'gate-a' })
    wrapper.unmount()
  })

  it('auto-refresh waits for processingLock: a peek during approve applies after it settles', async () => {
    const a = gateItem('a')
    const b = gateItem('b')
    const n = gateItem('new')
    let list = [a, b]
    mocks.listGates.mockImplementation(async () => paged(list))
    let releaseResume!: (v: unknown) => void
    mocks.resumeGate.mockImplementation(() => new Promise((resolve) => { releaseResume = resolve }))
    const pg = usePendingGates()

    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()
    await wrapper.get('[data-testid="resolve-btn"]').trigger('click')
    await flushPromises()

    // Server lags (still lists a) and a new gate shows up mid-approve.
    list = [n, a, b]
    await pg.peek({ source: 'sidebar-poll' })
    await flushPromises()
    // a was removed locally and must not come back as a ghost.
    expect(pg.displayedItems.value.some((it) => it.runId === 'run-a')).toBe(false)

    list = [n, b]
    releaseResume({ status: 'ok' })
    await flushPromises()
    await nextTick()
    await flushPromises()
    await pg.peek({ source: 'sidebar-poll' })
    await flushPromises()
    expect(pg.displayedItems.value.map((it) => it.runId)).toEqual(['run-new', 'run-b'])
    expect(pg.count.value).toBe(2)
    expect(activeDetail(wrapper)).toEqual({ runId: 'run-b', nodeId: 'gate-b' })
    wrapper.unmount()
  })

  it('processing intent before resume resolves: WS status does not fetch processed triple', async () => {
    const a = gateItem('a')
    const b = gateItem('b')
    let list = [a, b]
    mocks.listGates.mockImplementation(async () => paged(list))

    let releaseResume!: (v: unknown) => void
    mocks.resumeGate.mockImplementation(
      () =>
        new Promise((resolve) => {
          releaseResume = resolve
        }),
    )

    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()

    const before = inboxCallsFor('run-a', 'gate-a', 1).length
    expect(before).toBeGreaterThan(0)
    const ws = FakeWebSocket.instances.find((w) => w.url.includes('run-a'))
    expect(ws).toBeTruthy()

    // Click resolve but keep resumeGate pending — intent already short-circuits.
    const resolveClick = wrapper.get('[data-testid="resolve-btn"]').trigger('click')
    await flushPromises()

    expect(ws!.readyState).toBe(3)
    ws!.emit('status')
    await flushPromises()
    expect(inboxCallsFor('run-a', 'gate-a', 1).length).toBe(before)

    list = [b]
    releaseResume({ status: 'ok' })
    await resolveClick
    await flushPromises()
    await nextTick()
    await flushPromises()

    expect(inboxCallsFor('run-a', 'gate-a', 1).length).toBe(before)
    expect(activeDetail(wrapper)).toEqual({ runId: 'run-b', nodeId: 'gate-b' })
    wrapper.unmount()
  })

  it('aborts in-flight inbox-context on processing intent and does not retry', async () => {
    const a = gateItem('a')
    const b = gateItem('b')
    let list = [a, b]
    mocks.listGates.mockImplementation(async () => paged(list))

    let loadCount = 0
    mocks.inboxContext.mockImplementation(async (runId, nodeId, iteration, opts) => {
      loadCount += 1
      // First call completes so GateApproval mounts; second (softRefresh) hangs.
      if (loadCount >= 2 && runId === 'run-a') {
        return hangInboxContext(runId, nodeId, iteration, opts)
      }
      return {
        type: 'gate',
        nodes: [],
        artifacts: [],
        nodeExecutions: {},
      }
    })

    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()

    expect(wrapper.find('[data-testid="resolve-btn"]').exists()).toBe(true)
    const ws = FakeWebSocket.instances.find((w) => w.url.includes('run-a'))
    expect(ws).toBeTruthy()
    ws!.emit('artifact_edit')
    await flushPromises()

    expect(loadCount).toBeGreaterThanOrEqual(2)
    const hangingCall = [...mocks.inboxContext.mock.calls]
      .reverse()
      .find((c) => c[0] === 'run-a' && c[3]?.signal && !c[3].signal.aborted)
    expect(hangingCall?.[3]?.signal).toBeInstanceOf(AbortSignal)
    const hangingSignal = hangingCall![3]!.signal!
    const beforeResolve = inboxCallsFor('run-a', 'gate-a', 1).length

    list = [b]
    await wrapper.get('[data-testid="resolve-btn"]').trigger('click')
    await flushPromises()
    await nextTick()
    await flushPromises()

    expect(hangingSignal.aborted).toBe(true)
    // Aborted request must not auto-retry the processed triple.
    expect(inboxCallsFor('run-a', 'gate-a', 1).length).toBe(beforeResolve)
    expect(mocks.toastError).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('softRefresh single-flight: concurrent artifact_edit does not fan out requests', async () => {
    const a = gateItem('a')
    mocks.listGates.mockResolvedValue(paged([a]))

    let releaseFirst!: (v: unknown) => void
    let calls = 0
    mocks.inboxContext.mockImplementation(async (_runId, _nodeId, _iteration, opts) => {
      calls += 1
      if (calls === 1) {
        return new Promise((resolve) => {
          releaseFirst = resolve
          opts?.signal?.addEventListener(
            'abort',
            () => resolve(Promise.reject(new DOMException('Aborted', 'AbortError'))),
            { once: true },
          )
        }).catch((e) => {
          throw e
        })
      }
      return hangInboxContext(_runId, _nodeId, _iteration, opts)
    })

    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()
    expect(calls).toBe(1)

    const ws = FakeWebSocket.instances[FakeWebSocket.instances.length - 1]
    expect(ws).toBeTruthy()
    // status/trace must not softRefresh; only artifact_edit starts the in-flight refresh.
    ws.emit('status')
    ws.emit('trace')
    await flushPromises()
    expect(calls).toBe(1)

    ws.emit('artifact_edit')
    ws.emit('artifact_edit')
    ws.emit('status')
    await flushPromises()

    // Single-flight drops duplicates while the first request is in flight.
    expect(calls).toBe(1)

    releaseFirst({
      type: 'gate',
      nodes: [],
      artifacts: [],
      nodeExecutions: {},
    })
    await flushPromises()
    wrapper.unmount()
  })

  it('gate: irrelevant status/trace/react do not softRefresh; artifact_edit and turn_done do', async () => {
    const a = gateItem('a')
    mocks.listGates.mockResolvedValue(paged([a]))

    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()

    const before = inboxCallsFor('run-a', 'gate-a', 1).length
    expect(before).toBeGreaterThan(0)
    const ws = FakeWebSocket.instances[FakeWebSocket.instances.length - 1]
    expect(ws).toBeTruthy()

    ws.emit('status')
    ws.emit('trace')
    ws.emit('react')
    await flushPromises()
    expect(inboxCallsFor('run-a', 'gate-a', 1).length).toBe(before)

    ws.emit('artifact_edit')
    await flushPromises()
    expect(inboxCallsFor('run-a', 'gate-a', 1).length).toBe(before + 1)

    ws.emit('review', { event: 'turn_done' })
    await flushPromises()
    expect(inboxCallsFor('run-a', 'gate-a', 1).length).toBe(before + 2)

    ws.emit('review', { event: 'error' })
    await flushPromises()
    expect(inboxCallsFor('run-a', 'gate-a', 1).length).toBe(before + 3)

    wrapper.unmount()
  })

  it('processing lock: confirm leaves immediately; other pending selectable while in-flight', async () => {
    const a = gateItem('a')
    const b = gateItem('b')
    const c = gateItem('c')
    let list = [a, b, c]
    mocks.listGates.mockImplementation(async () => paged(list))

    let releaseResume!: (v: unknown) => void
    mocks.resumeGate.mockImplementation(
      () =>
        new Promise((resolve) => {
          releaseResume = resolve
        }),
    )

    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()

    const resolveClick = wrapper.get('[data-testid="resolve-btn"]').trigger('click')
    await flushPromises()

    // Intent leave: a gone before resume returns; neighbor b selected/fetched.
    expect(activeDetail(wrapper)).not.toEqual({ runId: 'run-a', nodeId: 'gate-a' })
    expect(activeDetail(wrapper)).toEqual({ runId: 'run-b', nodeId: 'gate-b' })
    expect(inboxCallsFor('run-b', 'gate-b', 1).length).toBeGreaterThan(0)

    const bCallsBefore = inboxCallsFor('run-b', 'gate-b', 1).length
    await selectByQuery('run-c', 'gate-c')

    // Per-triple gate: c is not in-flight — user may switch while a's resume pending.
    expect(inboxCallsFor('run-c', 'gate-c', 1).length).toBeGreaterThan(0)
    expect(activeDetail(wrapper)).toEqual({ runId: 'run-c', nodeId: 'gate-c' })
    expect(inboxCallsFor('run-b', 'gate-b', 1).length).toBe(bCallsBefore)

    list = [b, c]
    releaseResume({ status: 'ok' })
    await resolveClick
    await flushPromises()
    await nextTick()
    await flushPromises()

    expect(activeDetail(wrapper)).toEqual({ runId: 'run-c', nodeId: 'gate-c' })
    expect(activeDetail(wrapper)).not.toEqual({ runId: 'run-a', nodeId: 'gate-a' })
    wrapper.unmount()
  })

  it('plan g1.2 (inline hand-off): clarify force keeps the conversation with a hand-off row, then leaves', async () => {
    const a = clarifyItem('a')
    const b = clarifyItem('b')
    let list: InboxItem[] = [a, b]
    mocks.listGates.mockImplementation(async () => paged(list))

    let releaseReply!: (v: unknown) => void
    mocks.reactReply.mockImplementation(
      () =>
        new Promise((resolve) => {
          releaseReply = resolve
        }),
    )

    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()

    const finishClick = wrapper.get('[data-testid="clarify-send"]').trigger('click')
    await flushPromises()
    await nextTick()

    // Inline hand-off: a stays on screen with the pending row while reactReply(force) runs.
    expect(activeDetail(wrapper)).toEqual({ runId: 'run-a', nodeId: 'clarify-a' })

    list = [b]
    releaseReply({ status: 'ok' })
    await finishClick
    await flushPromises()
    await nextTick()
    await flushPromises()

    // v3: no jump to the next pending item — a stays and follows its run.
    expect(activeDetail(wrapper)).toEqual({ runId: 'run-a', nodeId: 'clarify-a' })
    // Success is the inline row, not a toast.
    expect(mocks.toastSuccess).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('方案 B: after the hand-off the inbox follows the run into its next waiting node', async () => {
    const a = clarifyItem('a')
    const next: InboxItem = { ...clarifyItem('a'), nodeId: 'clarify-accept-a', label: 'Accept a' }
    const b = clarifyItem('b')
    let list: InboxItem[] = [b, a]
    mocks.listGates.mockImplementation(async () => paged(list))
    let releaseReply!: (v: unknown) => void
    mocks.reactReply.mockImplementation(() => new Promise((resolve) => { releaseReply = resolve }))

    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()
    await selectByQuery('run-a', 'clarify-a')
    expect(activeDetail(wrapper)).toEqual({ runId: 'run-a', nodeId: 'clarify-a' })
    const mountsBefore = reviewShellMounts.n
    expect(mountsBefore).toBeGreaterThan(0)

    const finishClick = wrapper.get('[data-testid="clarify-send"]').trigger('click')
    await flushPromises()
    await nextTick()
    expect(activeDetail(wrapper)).toEqual({ runId: 'run-a', nodeId: 'clarify-a' })

    // Engine advanced run-a to its next interactive node.
    list = [next, b]
    releaseReply({ status: 'ok' })
    await finishClick
    await flushPromises()
    await nextTick()
    await flushPromises()

    expect(activeDetail(wrapper)).toEqual({ runId: 'run-a', nodeId: 'clarify-accept-a' })
    expect(wrapper.find('[data-testid="review-composer-stub"]').exists()).toBe(true)
    // Same run, next node: one chat — the shell (stage, drawers) is not rebuilt.
    expect(reviewShellMounts.n).toBe(mountsBefore)
    wrapper.unmount()
  })

  it('plan g2.1/s1: same-run second Approve arriving during lock window is selectable', async () => {
    const runId = 'run-same'
    const approve1: InboxItem = {
      type: 'clarify',
      runId,
      nodeId: 'clarify-approve-1',
      iteration: 1,
      workflowName: 'wf',
      label: 'Run #same · clarify-approve-1',
      done: false,
      requestedAt: new Date().toISOString(),
      updatedAt: new Date().toISOString(),
    }
    const approve2: InboxItem = {
      type: 'clarify',
      runId,
      nodeId: 'clarify-approve-2',
      iteration: 1,
      workflowName: 'wf',
      label: 'Run #same · clarify-approve-2',
      done: false,
      requestedAt: new Date().toISOString(),
      updatedAt: new Date().toISOString(),
    }
    let list: InboxItem[] = [approve1]
    let listLoads = 0
    mocks.listGates.mockImplementation(async () => {
      listLoads += 1
      return paged(list)
    })

    let releaseReply!: (v: unknown) => void
    mocks.reactReply.mockImplementation(
      () =>
        new Promise((resolve) => {
          releaseReply = resolve
        }),
    )

    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()

    const finishClick = wrapper.get('[data-testid="clarify-send"]').trigger('click')
    await flushPromises()
    await nextTick()

    // First Approve stays on screen (inline hand-off) while reactReply(force) is in flight.
    expect(activeDetail(wrapper)).toEqual({ runId: 'run-same', nodeId: 'clarify-approve-1' })

    // Engine advances: second Approve enters the list during the lock window.
    list = [approve2]
    const vm = wrapper.vm as { listPage?: number }
    vm.listPage = 2
    await flushPromises()
    await nextTick()
    expect(listLoads).toBeGreaterThan(1)

    await selectByQuery(runId, 'clarify-approve-2')

    expect(inboxCallsFor(runId, 'clarify-approve-2', 1).length).toBeGreaterThan(0)
    expect(activeDetail(wrapper)).toEqual({ runId: 'run-same', nodeId: 'clarify-approve-2' })
    expect(activeDetail(wrapper)?.runId).toBe('run-same')

    releaseReply({ status: 'ok' })
    await finishClick
    await flushPromises()
    await nextTick()
    await flushPromises()

    expect(activeDetail(wrapper)).toEqual({ runId: 'run-same', nodeId: 'clarify-approve-2' })
    expect(activeDetail(wrapper)).not.toEqual({ runId: 'run-same', nodeId: 'clarify-approve-1' })
    wrapper.unmount()
  })

  it('plan g2.1/f1: neighbor force confirm proceeds while first reactReply pending', async () => {
    const a = clarifyItem('a')
    const b = clarifyItem('b')
    let list: InboxItem[] = [a, b]
    mocks.listGates.mockImplementation(async () => paged(list))

    const replyResolvers: Array<(v: unknown) => void> = []
    mocks.reactReply.mockImplementation(
      () =>
        new Promise((resolve) => {
          replyResolvers.push(resolve)
        }),
    )

    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()

    const finishA = wrapper.get('[data-testid="clarify-send"]').trigger('click')
    await flushPromises()
    await nextTick()

    // A stays with its hand-off row while its reactReply hangs; the user can still open B.
    expect(activeDetail(wrapper)).toEqual({ runId: 'run-a', nodeId: 'clarify-a' })
    await selectByQuery('run-b', 'clarify-b')
    expect(activeDetail(wrapper)).toEqual({ runId: 'run-b', nodeId: 'clarify-b' })
    expect(mocks.reactReply).toHaveBeenCalledTimes(1)
    expect(mocks.reactReply).toHaveBeenNthCalledWith(
      1,
      'run-a',
      'clarify-a',
      'hi',
      [],
      true,
      [],
      false,
      false,
    )

    // Neighbor confirm must actually call reactReply — not silent-return on global lock.
    const finishB = wrapper.get('[data-testid="clarify-send"]').trigger('click')
    await flushPromises()
    await nextTick()

    expect(mocks.reactReply).toHaveBeenCalledTimes(2)
    expect(mocks.reactReply).toHaveBeenNthCalledWith(
      2,
      'run-b',
      'clarify-b',
      'hi',
      [],
      true,
      [],
      false,
      false,
    )
    expect(activeDetail(wrapper)).toEqual({ runId: 'run-b', nodeId: 'clarify-b' })

    list = []
    replyResolvers[0]!({ status: 'ok' })
    replyResolvers[1]!({ status: 'ok' })
    await finishA
    await finishB
    await flushPromises()
    await nextTick()
    await flushPromises()

    expect(mocks.toastSuccess).not.toHaveBeenCalled()
    expect(usePendingGates().displayedItems.value).toHaveLength(0)
    wrapper.unmount()
  })

  it('plan g2.1/s2: first force fails after neighbor selected; page confirm stays usable', async () => {
    const a = clarifyItem('a')
    const b = clarifyItem('b')
    let list: InboxItem[] = [a, b]
    mocks.listGates.mockImplementation(async () => paged(list))

    let rejectA!: (e: unknown) => void
    mocks.reactReply.mockImplementationOnce(
      () =>
        new Promise((_resolve, reject) => {
          rejectA = reject
        }),
    )

    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()

    const finishA = wrapper.get('[data-testid="clarify-send"]').trigger('click')
    await flushPromises()
    await nextTick()

    // Inline hand-off: A stays on screen while its reactReply(force) is pending.
    expect(activeDetail(wrapper)).toEqual({ runId: 'run-a', nodeId: 'clarify-a' })

    // A fails while B was briefly selected — restore A for retry and unlock the page.
    rejectA(new Error('confirm blew up'))
    await finishA.catch(() => {})
    await flushPromises()
    await nextTick()
    await flushPromises()

    expect(activeDetail(wrapper)).toEqual({ runId: 'run-a', nodeId: 'clarify-a' })
    expect(usePendingGates().displayedItems.value.map((it) => it.nodeId).sort()).toEqual([
      'clarify-a',
      'clarify-b',
    ])
    expect(wrapper.find('[data-testid="clarify-send"]').exists()).toBe(true)

    // Neighbor B remains confirmable after failure unlock (no whole-page lock residue).
    await selectByQuery('run-b', 'clarify-b')

    mocks.reactReply.mockResolvedValueOnce({ status: 'ok' })
    list = [a]
    await wrapper.get('[data-testid="clarify-send"]').trigger('click')
    await flushPromises()
    await nextTick()
    await flushPromises()

    expect(mocks.reactReply).toHaveBeenCalledWith(
      'run-b',
      'clarify-b',
      'hi',
      [],
      true,
      [],
      false,
      false,
    )
    // v3: b stays after its own confirm (follows run-b; never jumps to a).
    expect(activeDetail(wrapper)).toEqual({ runId: 'run-b', nodeId: 'clarify-b' })
    wrapper.unmount()
  })

  it('plan g1.3: force-finish failure restores row and allows retry', async () => {
    const a = clarifyItem('a')
    const b = clarifyItem('b')
    mocks.listGates.mockResolvedValue(paged([a, b]))
    mocks.reactReply.mockRejectedValueOnce(new Error('confirm blew up'))

    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()

    await wrapper.get('[data-testid="clarify-send"]').trigger('click')
    await flushPromises()
    await nextTick()
    await flushPromises()

    // Restored for retry — a is back and selectable.
    expect(activeDetail(wrapper)).toEqual({ runId: 'run-a', nodeId: 'clarify-a' })
    expect(wrapper.find('[data-testid="clarify-send"]').exists()).toBe(true)

    mocks.reactReply.mockResolvedValueOnce({ status: 'ok' })
    mocks.listGates.mockResolvedValue(paged([b]))
    await wrapper.get('[data-testid="clarify-send"]').trigger('click')
    await flushPromises()
    await nextTick()
    await flushPromises()

    // v3: the retried confirm succeeds and a stays (no jump to b).
    expect(activeDetail(wrapper)).toEqual({ runId: 'run-a', nodeId: 'clarify-a' })
    wrapper.unmount()
  })

  it('clarify ordinary turn does not markProcessed; force finish short-circuits like gate', async () => {
    const a = clarifyItem('a')
    const b = clarifyItem('b')
    let list: InboxItem[] = [a, b]
    mocks.listGates.mockImplementation(async () => paged(list))

    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()

    const before = inboxCallsFor('run-a', 'clarify-a', 1).length
    expect(before).toBeGreaterThan(0)

    // Ordinary turn: stay pending; status must not softRefresh.
    // Mid-turn react is gated by liveBusy (turn_begin) — must not softRefresh.
    await wrapper.get('[data-testid="clarify-turn"]').trigger('click')
    await flushPromises()
    await nextTick()
    await flushPromises()

    expect(mocks.reactReply).toHaveBeenCalledWith(
      'run-a',
      'clarify-a',
      'hi',
      [],
      false,
      [],
      false,
      false,
      expect.stringMatching(/\S/),
    )
    const ws = FakeWebSocket.instances.find((w) => w.url.includes('run-a'))
    expect(ws).toBeTruthy()
    expect(ws!.readyState).toBe(1)
    const afterTurn = inboxCallsFor('run-a', 'clarify-a', 1).length
    ws!.emit('status')
    ws!.emit('trace')
    await flushPromises()
    expect(inboxCallsFor('run-a', 'clarify-a', 1).length).toBe(afterTurn)

    // turn_begin marks live busy → react must not softRefresh (stale reactSessions gate was the bug).
    ws!.emit('review', { event: 'turn_begin', nodeId: 'clarify-a' })
    await flushPromises()
    const afterBegin = inboxCallsFor('run-a', 'clarify-a', 1).length
    ws!.emit('react')
    await flushPromises()
    expect(inboxCallsFor('run-a', 'clarify-a', 1).length).toBe(afterBegin)
    // artifact_edit mid-busy must also skip softRefresh (review v4).
    ws!.emit('artifact_edit', { previewArtifact: 'page.html' })
    await flushPromises()
    expect(inboxCallsFor('run-a', 'clarify-a', 1).length).toBe(afterBegin)
    expect(mocks.runArtifacts).toHaveBeenCalledWith('run-a')

    // Idle react (no busy) may softRefresh.
    ws!.emit('review', { event: 'turn_done', nodeId: 'clarify-a' })
    await flushPromises()
    const afterDone = inboxCallsFor('run-a', 'clarify-a', 1).length
    expect(afterDone).toBeGreaterThan(afterBegin)
    ws!.emit('react')
    await flushPromises()
    expect(inboxCallsFor('run-a', 'clarify-a', 1).length).toBe(afterDone + 1)

    // Force finish: short-circuit symmetric with gate resolve.
    const afterReact = inboxCallsFor('run-a', 'clarify-a', 1).length
    list = [b]
    await wrapper.get('[data-testid="clarify-send"]').trigger('click')
    await flushPromises()
    await nextTick()
    await flushPromises()

    expect(mocks.reactReply).toHaveBeenLastCalledWith(
      'run-a',
      'clarify-a',
      'hi',
      [],
      true,
      [],
      false,
      false,
    )
    expect(inboxCallsFor('run-a', 'clarify-a', 1).length).toBe(afterReact)
    expect(activeDetail(wrapper)).toEqual({ runId: 'run-a', nodeId: 'clarify-a' })
    wrapper.unmount()
  })

  it('resumeGate failure rolls back: unlocks selection and allows refetch', async () => {
    const a = gateItem('a')
    const b = gateItem('b')
    mocks.listGates.mockResolvedValue(paged([a, b]))
    mocks.resumeGate.mockRejectedValueOnce(new Error('resume failed'))

    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()

    const before = inboxCallsFor('run-a', 'gate-a', 1).length
    await wrapper.get('[data-testid="resolve-btn"]').trigger('click')
    await flushPromises()
    await nextTick()

    // Still on a after rollback.
    expect(wrapper.find('[data-testid="resolve-btn"]').exists()).toBe(true)

    await selectByQuery('run-b', 'gate-b')
    expect(inboxCallsFor('run-b', 'gate-b', 1).length).toBeGreaterThan(0)

    // Reselect a — refetch allowed after unmark.
    await selectByQuery('run-a', 'gate-a')
    expect(inboxCallsFor('run-a', 'gate-a', 1).length).toBeGreaterThan(before)
    wrapper.unmount()
  })

  it('refresh failure after resume still unlocks processing lock and converges', async () => {
    const a = gateItem('a')
    const b = gateItem('b')
    let list = [a, b]
    mocks.listGates.mockImplementation(async () => paged(list))

    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()

    // Force refresh + loadList both fail after resume — local converge must still win.
    list = [b]
    mocks.listGates.mockRejectedValueOnce(new Error('refresh blew up'))
    mocks.listGates.mockRejectedValueOnce(new Error('loadList blew up'))
    await wrapper.get('[data-testid="resolve-btn"]').trigger('click')
    await flushPromises()
    await nextTick()
    await flushPromises()

    // Local converge + unlock must happen even when submit refresh soft-fails.
    expect(activeDetail(wrapper)).not.toEqual({ runId: 'run-a', nodeId: 'gate-a' })
    expect(activeDetail(wrapper)).toEqual({ runId: 'run-b', nodeId: 'gate-b' })
    expect(inboxCallsFor('run-b', 'gate-b', 1).length).toBeGreaterThan(0)
    expect(usePendingGates().totalCount.value).toBe(1)

    // Lock released: user can select another (re-select b after converge is fine;
    // add a third via list to prove selectItem is not ignored).
    const c = gateItem('c')
    list = [b, c]
    mocks.listGates.mockImplementation(async () => paged(list))
    await reloadInboxList()

    await selectByQuery('run-c', 'gate-c')
    expect(inboxCallsFor('run-c', 'gate-c', 1).length).toBeGreaterThan(0)
    wrapper.unmount()
  })

  it('refresh failure after clarify force-finish still unlocks and converges', async () => {
    const a = clarifyItem('a')
    const b = clarifyItem('b')
    let list = [a, b]
    mocks.listGates.mockImplementation(async () => paged(list))

    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()

    list = [b]
    mocks.listGates.mockRejectedValueOnce(new Error('refresh blew up'))
    mocks.listGates.mockRejectedValueOnce(new Error('loadList blew up'))
    await wrapper.get('[data-testid="clarify-send"]').trigger('click')
    await flushPromises()
    await nextTick()
    await flushPromises()

    // Symmetric with gate: ghost gone, neighbor selected, lock released.
    expect(activeDetail(wrapper)).not.toEqual({ runId: 'run-a', nodeId: 'clarify-a' })
    expect(activeDetail(wrapper)).toEqual({ runId: 'run-b', nodeId: 'clarify-b' })
    expect(inboxCallsFor('run-b', 'clarify-b', 1).length).toBeGreaterThan(0)
    expect(mocks.toastSuccess).toHaveBeenCalled()
    expect(usePendingGates().totalCount.value).toBe(1)

    const c = clarifyItem('c')
    list = [b, c]
    mocks.listGates.mockImplementation(async () => paged(list))
    await reloadInboxList()

    await selectByQuery('run-c', 'clarify-c')
    expect(inboxCallsFor('run-c', 'clarify-c', 1).length).toBeGreaterThan(0)
    wrapper.unmount()
  })

  it('processing lock: manual refresh ignored until resolve converges', async () => {
    const a = gateItem('a')
    const b = gateItem('b')
    let list = [a, b]
    mocks.listGates.mockImplementation(async () => paged(list))

    let releaseResume!: (v: unknown) => void
    mocks.resumeGate.mockImplementation(
      () =>
        new Promise((resolve) => {
          releaseResume = resolve
        }),
    )

    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()

    const resolveClick = wrapper.get('[data-testid="resolve-btn"]').trigger('click')
    await flushPromises()

    // Intent leave already selected neighbor. The top-bar refresh control is gone;
    // selection stays on the neighbor until resume converges.
    expect(activeDetail(wrapper)).not.toEqual({ runId: 'run-a', nodeId: 'gate-a' })
    expect(activeDetail(wrapper)).toEqual({ runId: 'run-b', nodeId: 'gate-b' })

    list = [b]
    releaseResume({ status: 'ok' })
    await resolveClick
    await flushPromises()
    await nextTick()
    await flushPromises()

    expect(activeDetail(wrapper)).toEqual({ runId: 'run-b', nodeId: 'gate-b' })
    wrapper.unmount()
  })

  it('approve success: list removes item, detail retains the selected neighbor identity, counts sync', async () => {
    const a = gateItem('a')
    const b = gateItem('b')
    let list: InboxItem[] = [a, b]
    mocks.listGates.mockImplementation(async () => paged(list))

    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()

    list = [b]
    await wrapper.get('[data-testid="resolve-btn"]').trigger('click')
    await flushPromises()
    await nextTick()
    await flushPromises()

    expect(activeDetail(wrapper)).not.toEqual({ runId: 'run-a', nodeId: 'gate-a' })
    expect(activeDetail(wrapper)).toEqual({ runId: 'run-b', nodeId: 'gate-b' })
    expect(wrapper.findAll('[data-testid="inbox-active-detail"]')).toHaveLength(1)
    expect(activeDetail(wrapper)?.runId).toBe('run-b')
    expect(usePendingGates().totalCount.value).toBe(1)
    expect(usePendingGates().displayedItems.value.map((it) => it.nodeId)).toEqual(['gate-b'])
    wrapper.unmount()
  })

  it('late overlapping listGates after unlock does not restore a ghost or empty detail pane', async () => {
    const only = gateItem('solo')
    let list: InboxItem[] = [only]
    mocks.listGates.mockImplementation(async () => paged(list))

    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()
    expect(activeDetail(wrapper)).toEqual({ runId: 'run-solo', nodeId: 'gate-solo' })
    expect(activeDetail(wrapper)?.runId).toBe('run-solo')

    // Start a background loadList via filter watch; keep its snapshot pending.
    let releaseStale!: (value: ReturnType<typeof paged>) => void
    mocks.listGates.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          releaseStale = resolve
        }),
    )
    filterState.workflowSelected!.value = 'wf-stale'
    await flushPromises()

    // Approve while the stale listGates is still in flight.
    list = []
    mocks.listGates.mockImplementation(async () => paged(list))
    await wrapper.get('[data-testid="resolve-btn"]').trigger('click')
    await flushPromises()
    await nextTick()
    await flushPromises()

    // Converged empty inbox — no active detail pane or ghost card.
    expect(activeDetail(wrapper)).not.toEqual({ runId: 'run-solo', nodeId: 'gate-solo' })
    expect(activeDetail(wrapper)).toBeNull()
    expect(wrapper.find('[data-testid="resolve-btn"]').exists()).toBe(false)
    expect(usePendingGates().totalCount.value).toBe(0)

    // Stale snapshot arrives with the approved item — must be discarded.
    releaseStale(paged([only]))
    await flushPromises()
    await nextTick()
    expect(activeDetail(wrapper)).not.toEqual({ runId: 'run-solo', nodeId: 'gate-solo' })
    expect(activeDetail(wrapper)).toBeNull()
    expect(usePendingGates().totalCount.value).toBe(0)
    wrapper.unmount()
  })

  it('clarify hard load: early queue_state/snapshot still restores live+queue after mount (review v1)', async () => {
    const item = clarifyItem('resume')
    mocks.listGates.mockResolvedValue(paged([item]))

    let releaseContext!: (v: unknown) => void
    const hung = new Promise((resolve) => {
      releaseContext = resolve
    })
    mocks.inboxContext.mockImplementationOnce(() => hung as Promise<unknown>)

    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()

    // Hard load in flight: composer unmounted (loading pane). WS already connected.
    const ws = FakeWebSocket.instances.find((w) => w.url.includes('run-resume'))
    expect(ws).toBeTruthy()
    expect(wrapper.find('[data-testid="review-composer-stub"]').exists()).toBe(false)

    // Broadcast / snapshot arrive before chat mounts — must not be dropped.
    ws!.emit('snapshot', {
      run: {
        reactSessions: {
          'clarify-resume': {
            kind: 'clarify',
            waiting: 1,
            busy: true,
            items: [{ id: 'q-b', text: '乙' }],
            activeItem: { id: 'q-a', text: '甲' },
          },
        },
      },
    })
    ws!.emit('review', {
      event: 'queue_state',
      nodeId: 'clarify-resume',
      waiting: 1,
      items: [{ id: 'q-b', text: '乙' }],
      busy: true,
      activeItem: { id: 'q-a', text: '甲' },
    })
    await flushPromises()
    expect(composerFrames.applied.length).toBe(0)

    releaseContext({
      type: 'clarify',
      status: 'waiting_human',
      nodes: [{ id: 'clarify-resume', type: 'agent', caps: CLARIFY_CAPS, label: '澄清' }],
      artifacts: [],
      nodeExecutions: {},
      reactSessions: {
        'clarify-resume': {
          kind: 'clarify',
          waiting: 1,
          busy: true,
          items: [{ id: 'q-b', text: '乙' }],
          activeItem: { id: 'q-a', text: '甲' },
        },
      },
      clarify: {
        nodeId: 'clarify-resume',
        iteration: 1,
        turns: [],
        done: false,
        label: '澄清',
      },
    })
    await flushPromises()
    await nextTick()
    await flushPromises()
    await nextTick()

    expect(wrapper.find('[data-testid="review-composer-stub"]').exists()).toBe(true)
    // REST restore and/or buffered WS frames projected after mount.
    const queueFrames = composerFrames.applied.filter((f) => f.event === 'queue_state')
    expect(queueFrames.length).toBeGreaterThanOrEqual(1)
    const last = queueFrames[queueFrames.length - 1] as {
      busy?: boolean
      activeItem?: { text?: string }
      items?: { text?: string }[]
    }
    expect(last.busy).toBe(true)
    expect(last.activeItem?.text).toBe('甲')
    expect(last.items?.some((it) => it.text === '乙')).toBe(true)
    wrapper.unmount()
  })

  it('clarify hard load: early ACP is buffered and seeded after mount (g4.2 hard gate)', async () => {
    const item = clarifyItem('acp-race')
    mocks.listGates.mockResolvedValue(paged([item]))
    mocks.nodeEvents.mockResolvedValue({
      events: [{ kind: 'thought', text: 'REST seed thought', t: 1 }],
      live: true,
    })

    let releaseContext!: (v: unknown) => void
    const hung = new Promise((resolve) => {
      releaseContext = resolve
    })
    mocks.inboxContext.mockImplementationOnce(() => hung as Promise<unknown>)

    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()

    const ws = FakeWebSocket.instances.find((w) => w.url.includes('run-acp-race'))
    expect(ws).toBeTruthy()
    expect(wrapper.find('[data-testid="review-composer-stub"]').exists()).toBe(false)

    // ACP arrives while chat unmounted — must buffer (not drop).
    ws!.emit('acp', {
      nodeId: 'clarify-acp-race',
      busy: true,
      events: [
        { kind: 'thought', text: 'buffered thought', t: 1 },
        { kind: 'message', text: 'buffered message', t: 2 },
      ],
    })
    await flushPromises()
    expect(composerFrames.acp.length).toBe(0)

    releaseContext({
      type: 'clarify',
      status: 'waiting_human',
      nodes: [{ id: 'clarify-acp-race', type: 'agent', caps: CLARIFY_CAPS, label: '澄清' }],
      artifacts: [],
      nodeExecutions: {},
      reactSessions: {
        'clarify-acp-race': {
          kind: 'clarify',
          waiting: 0,
          busy: true,
          items: [],
          activeItem: { id: 'q-a', text: '硬刷新提问' },
        },
      },
      clarify: {
        nodeId: 'clarify-acp-race',
        iteration: 1,
        turns: [],
        done: false,
        label: '澄清',
      },
    })
    await flushPromises()
    await nextTick()
    await flushPromises()
    await nextTick()
    await flushPromises()

    expect(wrapper.find('[data-testid="review-composer-stub"]').exists()).toBe(true)
    // REST seed and/or buffered WS ACP applied after busy slot rebuild.
    expect(composerFrames.acp.length).toBeGreaterThanOrEqual(1)
    const seeded = composerFrames.acp.some((a) => {
      const ev = a.events as { kind?: string; text?: string }[] | undefined
      if (!Array.isArray(ev)) return false
      return ev.some(
        (e) =>
          (e.kind === 'thought' && (e.text === 'buffered thought' || e.text === 'REST seed thought')) ||
          (e.kind === 'message' && e.text === 'buffered message'),
      )
    })
    expect(seeded).toBe(true)
    expect(mocks.nodeEvents).toHaveBeenCalledWith('run-acp-race', 'clarify-acp-race', expect.anything())
    wrapper.unmount()
  })

  it('gate hard load: early ACP is buffered and seeded after mount (g4.2 gate parity)', async () => {
    const item = gateItem('acp-gate')
    mocks.listGates.mockResolvedValue(paged([item]))
    mocks.nodeEvents.mockResolvedValue({
      events: [{ kind: 'thought', text: 'REST gate thought', t: 1 }],
      live: true,
    })

    let releaseContext!: (v: unknown) => void
    const hung = new Promise((resolve) => {
      releaseContext = resolve
    })
    mocks.inboxContext.mockImplementationOnce(() => hung as Promise<unknown>)

    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()

    const ws = FakeWebSocket.instances.find((w) => w.url.includes('run-acp-gate'))
    expect(ws).toBeTruthy()
    expect(wrapper.find('[data-testid="resolve-btn"]').exists()).toBe(false)

    ws!.emit('acp', {
      nodeId: 'visual-producer',
      busy: true,
      events: [
        { kind: 'thought', text: 'buffered gate thought', t: 1 },
        { kind: 'message', text: 'buffered gate message', t: 2 },
      ],
    })
    await flushPromises()
    expect(composerFrames.acp.length).toBe(0)

    releaseContext({
      type: 'gate',
      nodes: [
        { id: 'visual-producer', type: 'agent', caps: IMPLEMENT_CAPS, label: '视觉' },
        { id: 'gate-acp-gate', type: 'human_gate', label: '审批' },
      ],
      artifacts: [],
      nodeExecutions: {},
      reactSessions: {
        'visual-producer': {
          kind: 'review',
          waiting: 0,
          busy: true,
          items: [],
          activeItem: { id: 'q-a', text: '热修提问' },
        },
      },
      gate: {
        runId: 'run-acp-gate',
        nodeId: 'gate-acp-gate',
        iteration: 1,
        title: 'Gate acp-gate',
        bodyMd: '',
        actions: [{ id: 'approve', label: 'Approve' }],
        reactUpstreamNodeId: 'visual-producer',
        reactSessionAlive: true,
        requestedAt: new Date().toISOString(),
      },
    })
    await flushPromises()
    await nextTick()
    await flushPromises()
    await nextTick()
    await flushPromises()

    expect(wrapper.find('[data-testid="resolve-btn"]').exists()).toBe(true)
    expect(composerFrames.acp.length).toBeGreaterThanOrEqual(1)
    const seeded = composerFrames.acp.some((a) => {
      const ev = a.events as { kind?: string; text?: string }[] | undefined
      if (!Array.isArray(ev)) return false
      return ev.some(
        (e) =>
          (e.kind === 'thought' &&
            (e.text === 'buffered gate thought' || e.text === 'REST gate thought')) ||
          (e.kind === 'message' && e.text === 'buffered gate message'),
      )
    })
    expect(seeded).toBe(true)
    expect(mocks.nodeEvents).toHaveBeenCalledWith(
      'run-acp-gate',
      'visual-producer',
      expect.anything(),
    )
    const queueFrames = composerFrames.applied.filter((f) => f.event === 'queue_state')
    expect(queueFrames.length).toBeGreaterThanOrEqual(1)
    wrapper.unmount()
  })

  it('selects the inbox card matching ?run&node instead of the first item', async () => {
    const a = clarifyItem('a')
    const b = clarifyItem('b')
    routeState.query = { run: 'run-b', node: 'clarify-b' }
    mocks.listGates.mockResolvedValue(paged([a, b]))
    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()
    await flushPromises()
    expect(activeDetail(wrapper)).toEqual({ runId: 'run-b', nodeId: 'clarify-b' })
    expect(wrapper.find('[data-testid="inbox-item-card"]').exists()).toBe(false)
    expect(inboxCallsFor('run-b', 'clarify-b', 1).length).toBeGreaterThan(0)
    wrapper.unmount()
  })

  it('applies home handoff seed onto the matching Approve card', async () => {
    const item = {
      ...clarifyItem('ap'),
      runId: 'run-home',
      nodeId: 'ap',
      label: '开发前澄清',
    }
    routeState.query = { run: 'run-home', node: 'ap' }
    setHomeApproveHandoff({
      runId: 'run-home',
      nodeId: 'ap',
      text: '把登录做清楚',
      images: [{ data: 'abc', mimeType: 'image/png', name: 'shot.png' }],
    })
    mocks.listGates.mockResolvedValue(paged([item]))
    mocks.inboxContext.mockResolvedValue({
      type: 'clarify',
      status: 'waiting_human',
      nodes: [{ id: 'ap', type: 'agent', caps: CLARIFY_CAPS, label: '澄清' }],
      artifacts: [],
      nodeExecutions: {},
      clarify: { nodeId: 'ap', iteration: 1, turns: [], done: false, label: '澄清' },
    })
    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()
    await flushPromises()
    expect(activeDetail(wrapper)).toEqual({ runId: 'run-home', nodeId: 'ap' })
    const seedProp = wrapper.findComponent({ name: 'ReviewComposer' }).props('seedHumanText')
    expect(seedProp).toBe('把登录做清楚')
    expect(wrapper.findComponent({ name: 'ReviewComposer' }).props('seedHumanImages')).toEqual([
      { data: 'abc', mimeType: 'image/png', name: 'shot.png' },
    ])
    wrapper.unmount()
  })

  it('applies home handoff seed when the parked node id is still empty', async () => {
    const item = {
      ...clarifyItem('ap'),
      runId: 'run-home',
      nodeId: 'ap',
      label: '开发前澄清',
    }
    routeState.query = { run: 'run-home' }
    setHomeApproveHandoff({
      runId: 'run-home',
      nodeId: '',
      text: '附图',
      images: [{ data: 'QQ==', mimeType: 'application/pdf', name: 'brief.pdf' }],
    })
    mocks.listGates.mockResolvedValue(paged([item]))
    mocks.inboxContext.mockResolvedValue({
      type: 'clarify',
      status: 'waiting_human',
      nodes: [{ id: 'ap', type: 'agent', caps: CLARIFY_CAPS, label: '澄清' }],
      artifacts: [],
      nodeExecutions: {},
      clarify: { nodeId: 'ap', iteration: 1, turns: [], done: false, label: '澄清' },
    })
    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()
    await flushPromises()
    expect(wrapper.findComponent({ name: 'ReviewComposer' }).props('seedHumanText')).toBe('附图')
    expect(wrapper.findComponent({ name: 'ReviewComposer' }).props('seedHumanImages')).toEqual([
      { data: 'QQ==', mimeType: 'application/pdf', name: 'brief.pdf' },
    ])
    wrapper.unmount()
  })

  it('shows a starting ghost card with the connecting ReAct panes while listGates is still empty', async () => {
    routeState.query = { run: 'run-home', node: 'ap' }
    setHomeApproveHandoff({
      runId: 'run-home',
      nodeId: 'ap',
      text: '把登录做清楚',
      images: [{ data: 'abc', mimeType: 'image/png', name: 'shot.png' }],
    })
    mocks.listGates.mockResolvedValue(paged([]))
    mocks.inboxContext.mockResolvedValue({ pending: false })
    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()
    await flushPromises()
    expect(wrapper.findComponent({ name: 'EmptyState' }).exists()).toBe(false)
    expect(wrapper.find('[data-testid="inbox-boot-loader"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="react-connecting-stage"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="react-connecting-sidebar"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="react-connecting-pill"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="clarify-boot-loader"]').exists()).toBe(true)
    expect(
      (wrapper.get('[data-testid="react-connecting-input"]').element as HTMLTextAreaElement)
        .disabled,
    ).toBe(true)
    expect(wrapper.findComponent({ name: 'ReviewComposer' }).exists()).toBe(false)
    wrapper.unmount()
  })

  it('applies home handoff seed when the parked node id differs from the guess', async () => {
    const item = {
      ...clarifyItem('approve_7gl8'),
      runId: 'run-home',
      nodeId: 'approve_7gl8',
      label: '开发前澄清',
    }
    routeState.query = { run: 'run-home', node: 'ap' }
    setHomeApproveHandoff({
      runId: 'run-home',
      nodeId: 'ap',
      text: '把登录做清楚',
      images: [{ data: 'abc', mimeType: 'image/png', name: 'shot.png' }],
    })
    mocks.listGates.mockResolvedValue(paged([item]))
    mocks.inboxContext.mockResolvedValue({
      type: 'clarify',
      status: 'waiting_human',
      nodes: [{ id: 'approve_7gl8', type: 'agent', caps: CLARIFY_CAPS, label: '澄清' }],
      artifacts: [],
      nodeExecutions: {},
      clarify: { nodeId: 'approve_7gl8', iteration: 1, turns: [], done: false, label: '澄清' },
    })
    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()
    await flushPromises()
    expect(activeDetail(wrapper)).toEqual({ runId: 'run-home', nodeId: 'approve_7gl8' })
    expect(wrapper.findComponent({ name: 'ReviewComposer' }).props('seedHumanText')).toBe(
      '把登录做清楚',
    )
    expect(wrapper.findComponent({ name: 'ReviewComposer' }).props('seedHumanImages')).toEqual([
      { data: 'abc', mimeType: 'image/png', name: 'shot.png' },
    ])
    wrapper.unmount()
  })

  it('queue_state busy patches the visible card to 正在回复中 without the update banner (plan g2.1)', async () => {
    const a = clarifyItem('a')
    mocks.listGates.mockResolvedValue(paged([a]))
    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()
    await flushPromises()
    // Mount peek may flag membership vs the empty beforeEach snapshot. Sync first
    // so the busy patch itself is the only change under test.
    usePendingGates().applyPending()
    await nextTick()

    const pending = () => usePendingGates().displayedItems.value.find((it) => it.nodeId === 'clarify-a')
    expect(pending()?.state).not.toBe('replying')
    expect(usePendingGates().hasPendingUpdate.value).toBe(false)

    const ws = FakeWebSocket.instances.find((w) => w.url.includes('run-a'))
    expect(ws).toBeTruthy()
    const contextCallsBeforeBusy = inboxCallsFor('run-a', 'clarify-a').length
    expect(contextCallsBeforeBusy).toBeGreaterThan(0)
    ws!.emit('review', { event: 'turn_begin', nodeId: 'clarify-a' })
    await flushPromises()
    await nextTick()

    expect(pending()?.state).toBe('replying')
    expect(usePendingGates().hasPendingUpdate.value).toBe(false)
    // Badge patch must not hard-reload context (would unmount live composer).
    expect(inboxCallsFor('run-a', 'clarify-a')).toHaveLength(contextCallsBeforeBusy)
    expect(wrapper.find('[data-testid="review-composer-stub"]').exists()).toBe(true)

    ws!.emit('review', { event: 'queue_state', nodeId: 'clarify-a', busy: false, waiting: 0 })
    await flushPromises()
    await nextTick()
    expect(pending()?.state).not.toBe('replying')
    expect(usePendingGates().hasPendingUpdate.value).toBe(false)
    expect(inboxCallsFor('run-a', 'clarify-a')).toHaveLength(contextCallsBeforeBusy)
    expect(wrapper.find('[data-testid="review-composer-stub"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('ordinary clarify send marks the card replying without remounting the composer', async () => {
    const a = clarifyItem('a')
    mocks.listGates.mockResolvedValue(paged([a]))
    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()
    await flushPromises()
    usePendingGates().applyPending()
    await nextTick()

    const pending = () => usePendingGates().displayedItems.value.find((it) => it.nodeId === 'clarify-a')
    expect(pending()?.state).not.toBe('replying')
    const contextCallsBeforeSend = inboxCallsFor('run-a', 'clarify-a').length
    expect(wrapper.find('[data-testid="clarify-turn"]').exists()).toBe(true)

    await wrapper.get('[data-testid="clarify-turn"]').trigger('click')
    await flushPromises()
    await nextTick()

    expect(pending()?.state).toBe('replying')
    expect(usePendingGates().hasPendingUpdate.value).toBe(false)
    expect(inboxCallsFor('run-a', 'clarify-a')).toHaveLength(contextCallsBeforeSend)
    expect(wrapper.find('[data-testid="review-composer-stub"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('keeps the open conversation when that run is being cancelled, and does not open the next row or the empty inbox', async () => {
    const a = gateItem('a')
    const b = gateItem('b')
    let list = [a, b]
    mocks.listGates.mockImplementation(async () => paged(list))

    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()
    await selectByQuery('run-a', 'gate-a')
    expect(activeDetail(wrapper)).toEqual({ runId: 'run-a', nodeId: 'gate-a' })

    beginInboxCancelDeparture('run-a')
    list = [b]
    await reloadInboxList()
    expect(activeDetail(wrapper)).toEqual({ runId: 'run-a', nodeId: 'gate-a' })
    expect(wrapper.findComponent({ name: 'EmptyState' }).exists()).toBe(false)
    expect(wrapper.text()).not.toContain('没有待审批项')

    list = []
    await reloadInboxList()
    expect(activeDetail(wrapper)).toEqual({ runId: 'run-a', nodeId: 'gate-a' })
    expect(wrapper.findComponent({ name: 'EmptyState' }).exists()).toBe(false)
    expect(wrapper.text()).not.toContain('没有待审批项')

    endInboxCancelDeparture('run-a')
    await reloadInboxList()
    expect(activeDetail(wrapper)).toBeNull()
    const empty = wrapper.findComponent({ name: 'EmptyState' })
    expect(empty.exists()).toBe(true)
    expect(empty.props('title')).toBe('没有待审批项')
    wrapper.unmount()
  })
})
