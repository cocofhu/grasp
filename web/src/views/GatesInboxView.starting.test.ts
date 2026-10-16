// @vitest-environment happy-dom
import { defineComponent, nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { InboxItem } from '@/lib/shared/types'

const mocks = vi.hoisted(() => ({
  listGates: vi.fn(),
  inboxContext: vi.fn(),
  getRun: vi.fn(),
  nodeEvents: vi.fn(),
  runArtifacts: vi.fn(),
  runEventsWsUrl: vi.fn((id: string) => `ws://test/runs/${id}/events`),
  listGatePrimaryArtifacts: vi.fn(),
}))

const routeState = vi.hoisted(() => ({ query: {} as Record<string, string> }))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      listGates: mocks.listGates,
      inboxContext: mocks.inboxContext,
      getRun: mocks.getRun,
      nodeEvents: mocks.nodeEvents,
      runArtifacts: mocks.runArtifacts,
      runEventsWsUrl: mocks.runEventsWsUrl,
      listGatePrimaryArtifacts: mocks.listGatePrimaryArtifacts,
    },
  }
})

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() }),
  useRoute: () => ({ query: routeState.query }),
}))

vi.mock('@/lib/composables/useBreakpoint', async () => {
  const { ref } = await import('vue')
  return { useBreakpoint: () => ({ isMobile: ref(false) }) }
})

vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({ error: vi.fn(), success: vi.fn(), info: vi.fn(), warn: vi.fn() }),
}))

const filterState = vi.hoisted(() => ({
  workflowSelected: null as { value: string } | null,
  projectSelected: null as { value: string } | null,
}))

vi.mock('@/lib/composables/useWorkflowFilter', async () => {
  const { ref } = await import('vue')
  filterState.workflowSelected = ref('')
  return { useWorkflowFilter: () => ({ selected: filterState.workflowSelected! }) }
})

vi.mock('@/lib/composables/useProjectContext', async () => {
  const { ref } = await import('vue')
  filterState.projectSelected = ref('')
  return {
    useProjectContext: () => ({
      selected: filterState.projectSelected!,
      ensureHydrated: vi.fn(),
    }),
  }
})

vi.mock('@/lib/inbox/useClarifyDraft', async () => {
  const { ref } = await import('vue')
  return {
    useClarifyDraft: () => ({ draft: ref(''), attachments: ref([]), annotations: ref([]) }),
  }
})

import GatesInboxView from './GatesInboxView.vue'
import { usePendingGates } from '@/lib/inbox/usePendingGates'
import { setHomeApproveHandoff, takeHomeApproveHandoff } from '@/lib/run/homeApproveHandoff'
import { CLARIFY_CAPS } from '@/test/capsFixtures'

function paged(items: InboxItem[]) {
  return { items, total: items.length, page: 1, pageSize: 20 }
}

function startingItem(): InboxItem {
  const now = new Date().toISOString()
  return {
    type: 'clarify',
    kind: 'clarify',
    state: 'starting',
    runId: 'run-boot',
    nodeId: 'ap',
    iteration: 1,
    workflowName: 'wf',
    runTitle: '把登录做清楚',
    label: '开发前澄清',
    done: false,
    requestedAt: now,
    updatedAt: now,
  }
}

function parkedItem(): InboxItem {
  const it = startingItem()
  delete (it as { state?: string }).state
  return it
}

const startingContext = {
  type: 'clarify',
  status: 'running',
  nodes: [{ id: 'ap', type: 'agent', caps: CLARIFY_CAPS, label: '开发前澄清' }],
  artifacts: [],
  nodeExecutions: {},
  clarify: { nodeId: 'ap', iteration: 1, turns: [], done: false, label: '开发前澄清', starting: true },
}

const parkedContext = {
  type: 'clarify',
  status: 'waiting_human',
  nodes: [{ id: 'ap', type: 'agent', caps: CLARIFY_CAPS, label: '开发前澄清' }],
  artifacts: [],
  nodeExecutions: {},
  clarify: {
    nodeId: 'ap',
    iteration: 1,
    turns: [{ role: 'human', text: '把登录做清楚', at: '' }],
    done: false,
    label: '开发前澄清',
  },
}

class FakeWebSocket {
  static instances: FakeWebSocket[] = []
  onmessage: ((ev: { data: string }) => void) | null = null
  onopen: (() => void) | null = null
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

/** The socket the view is currently listening on. */
function liveSocket(): FakeWebSocket {
  return FakeWebSocket.instances[FakeWebSocket.instances.length - 1]
}

const ReviewComposerStub = defineComponent({
  name: 'ReviewComposer',
  props: {
    seedHumanText: { type: String, default: '' },
    seedHumanImages: { type: Array, default: () => [] },
    turns: { type: Array, default: () => [] },
  },
  emits: ['send', 'finish', 'cancel'],
  setup(_, { expose }) {
    expose({
      applyReviewFrame: () => {},
      applyAcpEvents: () => {},
      discardLastQueued: () => {},
      isSessionBusy: () => false,
    })
    return {}
  },
  template: '<div data-testid="review-composer-stub" />',
})

function mountInbox(opts?: { realGatePanel?: boolean }) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  const stubs: Record<string, unknown> = {
    Icon: true,
    EmptyState: true,
    WorkflowFilter: true,
    ProjectFilter: true,
    TagFilter: true,
    Pagination: true,
    ArtifactLoadingPane: true,
    GateApproval: true,
    ReviewShell: defineComponent({
      template: '<div><slot name="stage" /><slot name="sidebar" /></div>',
    }),
    ReviewComposer: ReviewComposerStub,
    ClarifyProductStage: true,
    ReactArtifactStage: true,
  }
  if (opts?.realGatePanel) {
    delete stubs.ArtifactLoadingPane
    delete stubs.GateApproval
  }
  return mount(GatesInboxView, {
    global: {
      plugins: [i18n],
      stubs,
    },
  })
}

beforeEach(async () => {
  vi.clearAllMocks()
  routeState.query = {}
  takeHomeApproveHandoff()
  FakeWebSocket.instances = []
  vi.stubGlobal('WebSocket', FakeWebSocket as unknown as typeof WebSocket)
  mocks.nodeEvents.mockResolvedValue({ events: [], live: false })
  mocks.runArtifacts.mockResolvedValue([])
  if (filterState.workflowSelected) filterState.workflowSelected.value = ''
  if (filterState.projectSelected) filterState.projectSelected.value = ''
  mocks.listGates.mockResolvedValue(paged([]))
  mocks.listGatePrimaryArtifacts.mockResolvedValue({ items: [] })
  mocks.getRun.mockResolvedValue({ id: 'run-unknown', status: 'running' })
  await usePendingGates().refresh({ mode: 'force' })
})

describe('GatesInboxView starting approvals', () => {
  it('renders a connecting ReviewShell for a starting card', async () => {
    mocks.listGates.mockResolvedValue(paged([startingItem()]))
    mocks.inboxContext.mockResolvedValue(startingContext)
    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()
    await flushPromises()

    expect(wrapper.find('[data-testid="inbox-boot-loader"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="react-connecting-stage"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="react-connecting-sidebar"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="react-connecting-pill"]').text()).toContain('连接中')
    expect((wrapper.get('[data-testid="react-connecting-input"]').element as HTMLTextAreaElement).disabled).toBe(true)
    expect(wrapper.find('[data-testid="review-composer-stub"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="inbox-item-card"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('swaps the boot loader for the chat once a status frame reports the park', async () => {
    setHomeApproveHandoff({ runId: 'run-boot', nodeId: 'ap', text: '把登录做清楚', images: [] })
    routeState.query = { run: 'run-boot', node: 'ap' }
    mocks.listGates.mockResolvedValue(paged([startingItem()]))
    mocks.inboxContext.mockResolvedValue(startingContext)
    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()
    await flushPromises()
    expect(wrapper.find('[data-testid="react-connecting-sidebar"]').exists()).toBe(true)

    mocks.listGates.mockResolvedValue(paged([parkedItem()]))
    mocks.inboxContext.mockResolvedValue(parkedContext)
    liveSocket().emit('status', { runId: 'run-boot' })
    await flushPromises()
    await nextTick()
    await flushPromises()

    expect(wrapper.find('[data-testid="inbox-boot-loader"]').exists()).toBe(false)
    const composer = wrapper.findComponent({ name: 'ReviewComposer' })
    expect(composer.exists()).toBe(true)
    expect(composer.props('turns')).toHaveLength(1)
    expect(composer.props('seedHumanText')).toBe('把登录做清楚')
    wrapper.unmount()
  })

  it('keeps a dismissible failure card when the run dies before parking', async () => {
    mocks.listGates.mockResolvedValue(paged([startingItem()]))
    mocks.inboxContext.mockResolvedValue(startingContext)
    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()
    await flushPromises()
    expect(wrapper.find('[data-testid="react-connecting-sidebar"]').exists()).toBe(true)

    mocks.listGates.mockResolvedValue(paged([]))
    mocks.getRun.mockResolvedValue({ id: 'run-boot', status: 'failed', error: 'sandbox setup failed' })
    liveSocket().emit('status', { runId: 'run-boot' })
    await flushPromises()
    await nextTick()
    await flushPromises()

    expect(mocks.getRun).toHaveBeenCalledWith('run-boot')
    expect(wrapper.find('[data-testid="inbox-start-failed"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="inbox-boot-loader"]').exists()).toBe(false)

    await wrapper.get('[data-testid="inbox-start-failed-dismiss"]').trigger('click')
    await flushPromises()
    await nextTick()
    expect(wrapper.find('[data-testid="inbox-start-failed"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('surfaces the failure card when only the node execution failed, not the run', async () => {
    mocks.listGates.mockResolvedValue(paged([startingItem()]))
    mocks.inboxContext.mockResolvedValue(startingContext)
    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()
    await flushPromises()
    expect(wrapper.find('[data-testid="react-connecting-sidebar"]').exists()).toBe(true)

    // An approve sandbox-setup failure stops the run without marking it
    // terminal: the verdict lives on the node execution, not on run.status.
    mocks.listGates.mockResolvedValue(paged([]))
    mocks.getRun.mockResolvedValue({
      id: 'run-boot',
      status: 'running',
      nodeRuns: { ap: { nodeId: 'ap', status: 'failed', error: 'sandbox setup failed' } },
    })
    liveSocket().emit('status', { runId: 'run-boot' })
    await flushPromises()
    await nextTick()
    await flushPromises()

    expect(wrapper.find('[data-testid="inbox-start-failed"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="inbox-boot-loader"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('does not rebuild the ghost after the approval leaves the pending list', async () => {
    routeState.query = { run: 'run-boot', node: 'ap' }
    setHomeApproveHandoff({ runId: 'run-boot', nodeId: 'ap', text: '把登录做清楚', images: [] })
    mocks.listGates.mockResolvedValue(paged([parkedItem()]))
    mocks.inboxContext.mockResolvedValue(parkedContext)
    // Still-booting getRun must not fight the parked row; completed comes after leave.
    mocks.getRun.mockResolvedValue({
      id: 'run-boot',
      status: 'waiting_human',
      nodeRuns: { ap: { nodeId: 'ap', status: 'waiting_human' } },
    })
    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()
    await flushPromises()
    expect(wrapper.find('[data-testid="review-composer-stub"]').exists()).toBe(true)

    // Answered: the row leaves pending. `?run=` and the consumed handoff both
    // still point at it, but that must not resurrect a "starting" ghost.
    mocks.listGates.mockResolvedValue(paged([]))
    mocks.inboxContext.mockRejectedValue(new Error('no pending inbox item'))
    mocks.getRun.mockResolvedValue({
      id: 'run-boot',
      status: 'completed',
      nodeRuns: { ap: { nodeId: 'ap', status: 'completed' } },
    })
    filterState.projectSelected!.value = 'proj-x'
    await flushPromises()
    await nextTick()
    await flushPromises()
    await vi.waitFor(() => {
      expect(wrapper.find('[data-testid="review-composer-stub"]').exists()).toBe(false)
    })
    expect(wrapper.find('[data-testid="inbox-boot-loader"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('drops a cold-open ghost when ?run= points at an approval that already moved on', async () => {
    // Reopen / refresh with the deep link after approve already completed and
    // implement is running: pending list is empty, but that must not leave a
    // permanent "…" / 启动中 ghost.
    routeState.query = { run: 'run-f66f29e4', node: 'approve_7gl6' }
    mocks.listGates.mockResolvedValue(paged([]))
    mocks.getRun.mockResolvedValue({
      id: 'run-f66f29e4',
      status: 'running',
      nodes: [
        { id: 'in', type: 'input' },
        { id: 'approve_7gl6', type: 'agent', caps: CLARIFY_CAPS },
        { id: 'implement_qnlc', type: 'implement' },
      ],
      nodeRuns: {
        approve_7gl6: { nodeId: 'approve_7gl6', status: 'completed' },
        implement_qnlc: { nodeId: 'implement_qnlc', status: 'running' },
      },
    })
    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()
    await flushPromises()

    expect(mocks.getRun).toHaveBeenCalledWith('run-f66f29e4')
    expect(wrapper.find('[data-testid="react-connecting-sidebar"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="inbox-boot-loader"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('drops a cold-open ghost when the query node id is the published hint, not the runtime id', async () => {
    routeState.query = { run: 'run-f66f29e4', node: 'ap' }
    mocks.listGates.mockResolvedValue(paged([]))
    mocks.getRun.mockResolvedValue({
      id: 'run-f66f29e4',
      status: 'running',
      nodes: [
        { id: 'in', type: 'input' },
        { id: 'approve_7gl6', type: 'agent', caps: CLARIFY_CAPS },
        { id: 'implement_qnlc', type: 'implement' },
      ],
      nodeRuns: {
        approve_7gl6: { nodeId: 'approve_7gl6', status: 'completed' },
        implement_qnlc: { nodeId: 'implement_qnlc', status: 'running' },
      },
    })
    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()
    await flushPromises()

    expect(wrapper.find('[data-testid="react-connecting-sidebar"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('keeps a cold-open ghost while getRun still says the approve node is booting', async () => {
    routeState.query = { run: 'run-boot', node: 'ap' }
    mocks.listGates.mockResolvedValue(paged([]))
    mocks.getRun.mockResolvedValue({
      id: 'run-boot',
      status: 'running',
      nodeRuns: { ap: { nodeId: 'ap', status: 'running' } },
    })
    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()
    await flushPromises()

    expect(wrapper.find('[data-testid="react-connecting-sidebar"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('pins a parked deep-link row when filtered list omits it (plan g2.1 / g2.3)', async () => {
    // Stored/filter project is proj-a; the deep-linked run belongs to proj-b and
    // only appears on an unfiltered listGates. Identity is runId+nodeId.
    routeState.query = { run: 'run-1', node: 'n1' }
    filterState.projectSelected!.value = 'proj-a'
    const parked = parkedItem()
    parked.runId = 'run-1'
    parked.nodeId = 'n1'
    // Filtered loadList (with projectId) returns empty; unfiltered pin peek returns the row.
    mocks.listGates.mockImplementation(async (params?: { projectId?: string }) => {
      if (params?.projectId) return paged([])
      return paged([parked])
    })
    mocks.getRun.mockResolvedValue({
      id: 'run-1',
      status: 'waiting_human',
      nodeRuns: { n1: { nodeId: 'n1', status: 'waiting_human' } },
    })
    mocks.inboxContext.mockResolvedValue({
      ...parkedContext,
      clarify: { ...parkedContext.clarify, nodeId: 'n1' },
    })
    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()
    await flushPromises()
    await vi.waitFor(() => {
      expect(wrapper.find('[data-testid="review-composer-stub"]').exists()).toBe(true)
    })
    expect(wrapper.find('[data-testid="react-connecting-sidebar"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('leaves a live starting card alone when it merely drops out of a filtered page', async () => {
    mocks.listGates.mockResolvedValue(paged([startingItem()]))
    mocks.inboxContext.mockResolvedValue(startingContext)
    const wrapper = mountInbox()
    await flushPromises()
    await nextTick()
    await flushPromises()

    mocks.listGates.mockResolvedValue(paged([]))
    mocks.getRun.mockResolvedValue({ id: 'run-boot', status: 'running' })
    liveSocket().emit('status', { runId: 'run-boot' })
    await flushPromises()
    await nextTick()
    await flushPromises()

    expect(wrapper.find('[data-testid="inbox-start-failed"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('opens a gate past the first inbox page without staying on the loading layer', async () => {
    const now = new Date().toISOString()
    const rows: InboxItem[] = Array.from({ length: 21 }, (_, i) => {
      const n = i + 1
      return {
        type: 'gate',
        runId: `run-${n}`,
        nodeId: `g${n}`,
        iteration: 1,
        workflowName: 'wf',
        title: `待审批项 ${n}`,
        bodyMd: n === 21 ? '正文 21' : `正文 ${n}`,
        actions: [{ id: 'approve', label: '批准' }],
        requestedAt: now,
      }
    })
    routeState.query = { run: 'run-21', node: 'g21' }
    mocks.listGates.mockImplementation(async (params?: { page?: number; pageSize?: number }) => {
      const page = params?.page ?? 1
      const pageSize = params?.pageSize ?? 20
      const start = (page - 1) * pageSize
      const items = rows.slice(start, start + pageSize)
      return {
        items,
        total: rows.length,
        page,
        pageSize,
        hasMore: start + pageSize < rows.length,
      }
    })
    mocks.getRun.mockResolvedValue({
      id: 'run-21',
      status: 'waiting_human',
      nodes: [{ id: 'g21', type: 'human_gate', label: '门禁' }],
      nodeRuns: { g21: { nodeId: 'g21', status: 'waiting_human' } },
    })
    mocks.inboxContext.mockResolvedValue({
      type: 'gate',
      nodes: [{ id: 'g21', type: 'human_gate', label: '门禁', position: { x: 0, y: 0 }, config: {} }],
      artifacts: [],
      nodeExecutions: {},
    })
    const wrapper = mountInbox({ realGatePanel: true })
    await flushPromises()
    await nextTick()
    await flushPromises()
    await vi.waitFor(() => {
      expect(wrapper.text()).toContain('正文 21')
      expect(wrapper.text()).toContain('确认并流转')
    })
    expect(wrapper.text()).not.toContain('加载运行详情')
    expect(wrapper.findComponent({ name: 'ArtifactLoadingPane' }).exists()).toBe(false)
    expect(wrapper.findComponent({ name: 'GateApproval' }).exists()).toBe(true)
    wrapper.unmount()
  })

  it('does not render the inbox top bar or the desktop card column', async () => {
    mocks.listGates.mockResolvedValue(paged([]))
    const wrapper = mountInbox()
    await flushPromises()
    expect(wrapper.find('[data-testid="gates-inbox-status-pill"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="gates-inbox-refresh"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="inbox-item-card"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('运筹帷幄之中')
    expect(wrapper.text()).not.toContain('决胜千里之外')
    wrapper.unmount()
  })
})
