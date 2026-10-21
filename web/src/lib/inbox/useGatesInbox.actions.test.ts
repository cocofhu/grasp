// @vitest-environment happy-dom
import { createApp, defineComponent, nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { InboxItem, Run } from '@/lib/shared/types'

const shared = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const { ref } = require('vue') as typeof import('vue')
  return {
    isMobile: ref(false),
    selected: ref(''),
    selectedTags: ref<string[]>([]),
    displayedItems: ref<InboxItem[]>([]),
    remoteItems: ref<InboxItem[]>([]),
    totalCount: ref(0),
    hasPendingUpdate: ref(false),
    pendingMeta: ref<{ added?: number; removed?: number } | null>(null),
    lastPeekAt: ref(0),
    lastAutoAppliedAt: ref(0),
    autoApplyHold: null as (() => boolean) | null,
    ariaBusy: ref(false),
    draft: ref(''),
    attachments: ref([]),
    annotations: ref([]),
  }
})

const mocks = vi.hoisted(() => ({
  listGates: vi.fn(),
  inboxContext: vi.fn(),
  getRun: vi.fn(),
  resumeGate: vi.fn(),
  reactReply: vi.fn(),
  reactCancel: vi.fn(),
  reactQueueRemove: vi.fn(),
  reactQueueReorder: vi.fn(),
  nodeEvents: vi.fn(),
  runArtifacts: vi.fn(),
  createGateShareLink: vi.fn(),
  createReviewShareLink: vi.fn(),
  refresh: vi.fn(),
  peek: vi.fn(),
  applyPending: vi.fn(),
  autoApplyPending: vi.fn(),
  removeItemLocally: vi.fn(),
  restoreItemLocally: vi.fn(),
  patchItemReplying: vi.fn(),
  syncDisplayedBaseline: vi.fn(),
  clearVisibleMembership: vi.fn(),
  toastSuccess: vi.fn(),
  toastWarn: vi.fn(),
  toastError: vi.fn(),
}))

vi.mock('@/lib/composables/useBreakpoint', () => ({
  useBreakpoint: () => ({ isMobile: shared.isMobile }),
}))
vi.mock('@/lib/composables/useWorkflowFilter', () => ({
  useWorkflowFilter: () => ({ selected: shared.selected }),
}))
vi.mock('@/lib/composables/useTagFilter', () => ({
  useTagFilter: () => ({ selectedTags: shared.selectedTags }),
}))
vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({
    success: mocks.toastSuccess,
    warn: mocks.toastWarn,
    error: mocks.toastError,
    info: vi.fn(),
  }),
}))
vi.mock('@/lib/inbox/useClarifyDraft', async () => {
  const actual = await vi.importActual<typeof import('@/lib/inbox/useClarifyDraft')>(
    '@/lib/inbox/useClarifyDraft',
  )
  return {
    ...actual,
    useClarifyDraft: () => ({
      draft: shared.draft,
      attachments: shared.attachments,
      annotations: shared.annotations,
    }),
  }
})
vi.mock('@/lib/inbox/usePendingGates', () => ({
  usePendingGates: () => ({
    displayedItems: shared.displayedItems,
    remoteItems: shared.remoteItems,
    totalCount: shared.totalCount,
    refresh: mocks.refresh,
    peek: mocks.peek,
    applyPending: mocks.applyPending,
    removeItemLocally: mocks.removeItemLocally,
    restoreItemLocally: mocks.restoreItemLocally,
    setSidebarExtras: vi.fn(),
    markStartFailed: vi.fn(),
    clearStartFailed: vi.fn(),
    setActiveInboxKey: vi.fn(),
    patchItemReplying: mocks.patchItemReplying,
    syncDisplayedBaseline: mocks.syncDisplayedBaseline,
    clearVisibleMembership: mocks.clearVisibleMembership,
    hasPendingUpdate: shared.hasPendingUpdate,
    pendingMeta: shared.pendingMeta,
    lastPeekAt: shared.lastPeekAt,
    lastAutoAppliedAt: shared.lastAutoAppliedAt,
    autoApplyPending: mocks.autoApplyPending,
    addAutoApplyHold: (hold: () => boolean) => {
      shared.autoApplyHold = hold
      return () => { if (shared.autoApplyHold === hold) shared.autoApplyHold = null }
    },
    itemKey: (it: InboxItem) => `${it.runId}:${it.nodeId}`,
    ariaBusy: shared.ariaBusy,
  }),
}))
vi.mock('@/lib/inbox/inboxContext', () => ({
  adaptInboxContextToRun: (ctx: { run?: Run }, runId: string) =>
    ctx.run || ({ id: runId, status: 'waiting_human', nodes: [], artifacts: [] } as unknown as Run),
}))
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
      reactCancel: mocks.reactCancel,
      reactQueueRemove: mocks.reactQueueRemove,
      reactQueueReorder: mocks.reactQueueReorder,
      nodeEvents: mocks.nodeEvents,
      runArtifacts: mocks.runArtifacts,
      createGateShareLink: mocks.createGateShareLink,
      createReviewShareLink: mocks.createReviewShareLink,
      runEventsWsUrl: (id: string) => `ws://events/${id}`,
    },
  }
})

import { TYPING_IDLE_MS, useGatesInbox } from './useGatesInbox'
import { CLARIFY_CAPS, PREVIEW_REVIEW_CAPS } from '@/test/capsFixtures'

const gateItem = (id = 'run-gate', over: Record<string, unknown> = {}): InboxItem =>
  ({
    type: 'gate',
    runId: id,
    nodeId: 'gate',
    iteration: 1,
    title: `Gate ${id}`,
    bodyMd: 'Review',
    actions: [{ id: 'pass', label: 'Pass' }],
    form: [],
    ...over,
  }) as unknown as InboxItem

const clarifyItem = (id = 'run-chat', over: Record<string, unknown> = {}): InboxItem =>
  ({
    type: 'clarify',
    kind: 'clarify',
    runId: id,
    nodeId: 'react',
    iteration: 1,
    label: `Clarify ${id}`,
    done: false,
    ...over,
  }) as unknown as InboxItem

const contextRun = (id: string, over: Record<string, unknown> = {}): Run =>
  ({
    id,
    status: 'waiting_human',
    nodes: [
      { id: 'gate', type: 'agent', caps: CLARIFY_CAPS, config: {} },
      { id: 'react', type: 'agent', caps: CLARIFY_CAPS, config: {} },
    ],
    artifacts: [],
    reactSessions: {},
    clarifyByNode: { react: { nodeId: 'react', iteration: 1, turns: [], done: false } },
    gate: { nodeId: 'gate', reactUpstreamNodeId: 'producer', reactSessionAlive: true },
    ...over,
  }) as unknown as Run

class MockWebSocket {
  static instances: MockWebSocket[] = []
  onopen: ((event: Event) => void) | null = null
  onmessage: ((event: MessageEvent) => void) | null = null
  onclose: ((event: CloseEvent) => void) | null = null
  constructor(readonly url: string) {
    MockWebSocket.instances.push(this)
  }
  close() {
    this.onclose?.(new CloseEvent('close'))
  }
  open() {
    this.onopen?.(new Event('open'))
  }
  message(payload: unknown) {
    this.onmessage?.(new MessageEvent('message', { data: JSON.stringify(payload) }))
  }
}

async function withInbox(path = '/inbox') {
  let inbox!: ReturnType<typeof useGatesInbox>
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { path: '/inbox', component: { template: '<div />' } },
    ],
  })
  await router.push(path)
  await router.isReady()
  const Comp = defineComponent({
    setup() {
      inbox = useGatesInbox()
      return () => null
    },
  })
  const app = createApp(Comp)
  app.use(i18n)
  app.use(router)
  app.mount(document.createElement('div'))
  await flushPromises()
  await nextTick()
  return { inbox, app, router }
}

describe('useGatesInbox actions', () => {
  beforeEach(() => {
    vi.stubGlobal('WebSocket', MockWebSocket)
    MockWebSocket.instances = []
    for (const fn of Object.values(mocks)) (fn as ReturnType<typeof vi.fn>).mockReset()
    shared.isMobile.value = false
    shared.selected.value = ''
    shared.selectedTags.value = []
    shared.displayedItems.value = []
    shared.remoteItems.value = []
    shared.totalCount.value = 0
    shared.hasPendingUpdate.value = false
    shared.pendingMeta.value = null
    shared.lastPeekAt.value = 0
    shared.lastAutoAppliedAt.value = 0
    shared.autoApplyHold = null
    shared.ariaBusy.value = false
    shared.draft.value = ''
    shared.attachments.value = []
    shared.annotations.value = []
    mocks.listGates.mockResolvedValue({ items: [gateItem(), clarifyItem()], total: 2 })
    mocks.inboxContext.mockImplementation(async (runId: string) => ({ run: contextRun(runId) }))
    mocks.getRun.mockImplementation(async (runId: string) => contextRun(runId))
    mocks.resumeGate.mockResolvedValue({})
    mocks.reactReply.mockResolvedValue({})
    mocks.reactCancel.mockResolvedValue({})
    mocks.reactQueueRemove.mockResolvedValue({})
    mocks.reactQueueReorder.mockResolvedValue({})
    mocks.nodeEvents.mockResolvedValue({ events: [], hasMore: false })
    mocks.runArtifacts.mockResolvedValue([])
    mocks.refresh.mockResolvedValue({})
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('loads, filters and selects inbox rows', async () => {
    const { inbox, app } = await withInbox()
    expect(mocks.listGates).toHaveBeenCalledWith({
      page: 1,
      pageSize: 20,
      wf: undefined,
      tag: undefined,
    })
    expect(inbox.listItems.value).toHaveLength(2)
    expect(inbox.listTotal.value).toBe(2)
    expect(inbox.active.value?.runId).toBe('run-gate')
    expect(inbox.itemTitle(inbox.active.value!)).toContain('Gate')
    expect(inbox.isActive(inbox.active.value!)).toBe(true)
    expect(inbox.showListSkeleton.value).toBe(false)
    expect(inbox.showListError.value).toBe(false)
    // plan g1.2: successful loadList syncs peek baseline from visible listItems
    expect(mocks.syncDisplayedBaseline).toHaveBeenCalled()
    const synced = mocks.syncDisplayedBaseline.mock.calls.at(-1)?.[0] as InboxItem[]
    expect(synced.map((it) => it.runId).sort()).toEqual(['run-chat', 'run-gate'])

    shared.selected.value = 'wf-1'
    shared.selectedTags.value = ['urgent']
    await nextTick()
    await flushPromises()
    await inbox.loadList()
    expect(mocks.listGates).toHaveBeenLastCalledWith(expect.objectContaining({
      wf: 'wf-1',
      tag: 'urgent',
    }))

    inbox.workflowFilterOpen.value = true
    await nextTick()
    inbox.tagFilterOpen.value = true
    await nextTick()
    expect(inbox.workflowFilterOpen.value).toBe(false)
    app.unmount()
  })

  it('surfaces first-load failures and keeps existing rows during refresh errors', async () => {
    mocks.listGates.mockRejectedValue(new Error('inbox offline'))
    const { inbox, app } = await withInbox()
    expect(inbox.listLoadError.value).toBe('inbox offline')
    expect(inbox.showListError.value).toBe(true)
    expect(inbox.listLoading.value).toBe(false)

    inbox.listItems.value = [gateItem()]
    await inbox.loadList({ showLoading: true })
    expect(inbox.listItems.value).toHaveLength(1)
    expect(inbox.showListError.value).toBe(false)

    mocks.listGates.mockResolvedValue({ items: [], total: 0 })
    inbox.retryListLoad()
    await flushPromises()
    expect(inbox.listItems.value).toEqual([])
    app.unmount()
  })

  it('resolves gates after click leave and restores desk on rejection (g1.2 / g2.2)', async () => {
    const { inbox, app } = await withInbox()
    const submitted = inbox.active.value!
    await inbox.onResolve('pass', { note: 'ok' })
    expect(mocks.resumeGate).toHaveBeenCalledWith('run-gate', 'gate', 'pass', { note: 'ok' })
    expect(inbox.listItems.value.some((it) => it.runId === 'run-gate')).toBe(false)
    expect(inbox.processingLock.value).toBe(false)
    expect(mocks.removeItemLocally).toHaveBeenCalled()

    inbox.listItems.value = [submitted, clarifyItem()]
    inbox.active.value = submitted
    inbox.unmarkProcessed(submitted)
    mocks.resumeGate.mockRejectedValueOnce(new Error('validation failed'))
    mocks.restoreItemLocally.mockClear()
    mocks.removeItemLocally.mockClear()
    await inbox.onResolve('revise')
    // Failure after optimistic leave: card restored for retry (g2.2).
    expect(inbox.listItems.value[0]?.runId).toBe('run-gate')
    expect(inbox.active.value?.runId).toBe('run-gate')
    expect(mocks.restoreItemLocally).toHaveBeenCalled()
    expect(mocks.removeItemLocally).toHaveBeenCalled()

    await inbox.onResolve('pass')
    inbox.active.value = clarifyItem()
    await inbox.onResolve('pass')
    expect(inbox.active.value?.type).toBe('clarify')
    app.unmount()
  })

  it('sends ordinary and forced clarify replies with rollback behavior', async () => {
    const { inbox, app } = await withInbox()
    const clarify = inbox.listItems.value[1]!
    inbox.selectItem(clarify)
    await nextTick()

    const discardLastQueued = vi.fn()
    inbox.reviewChatRef.value = { discardLastQueued, isSessionBusy: () => false }
    inbox.onAppPreviewStagedPick({ selector: '#buy', url: 'https://app/', tagName: 'BUTTON' })
    await inbox.onClarifySend('please revise')
    expect(mocks.reactReply).toHaveBeenCalledWith(
      'run-chat',
      'react',
      'please revise',
      [],
      false,
      expect.arrayContaining([expect.objectContaining({ selector: '#buy' })]),
      false,
      false,
    )
    expect(mocks.patchItemReplying).toHaveBeenCalledWith('run-chat:react', true)

    mocks.reactReply.mockRejectedValueOnce(new Error('queue full'))
    await inbox.onClarifySend('again')
    expect(discardLastQueued).toHaveBeenCalled()
    expect(inbox.clarifyConfirmError.value).toBe('queue full')

    inbox.onAppPreviewStagedPick({ selector: '#x', url: '', tagName: 'DIV' })
    expect(inbox.mergeStagedAppPreviewPick([])).toHaveLength(1)
    const existing = [{ selector: '#x' }]
    expect(inbox.mergeStagedAppPreviewPick(existing as never)).toBe(existing)

    mocks.reactReply.mockRejectedValueOnce(new Error('finish denied'))
    await inbox.onClarifySend('done', [], [], true)
    expect(inbox.active.value?.runId).toBe('run-chat')
    expect(inbox.clarifyConfirmError.value).toBe('finish denied')

    mocks.reactReply.mockResolvedValueOnce({})
    await inbox.onClarifySend('done', [], [], true)
    expect(mocks.toastSuccess).toHaveBeenCalled()
    expect(inbox.processingLock.value).toBe(false)
    app.unmount()
  })

  it('cancels and mutates the clarify queue while respecting guards', async () => {
    const { inbox, app } = await withInbox()
    inbox.active.value = inbox.listItems.value[1]!
    await nextTick()
    await inbox.onClarifyCancel()
    await inbox.onClarifyQueueRemove('item-1')
    await inbox.onClarifyQueueReorder(['a', 'b'])
    expect(mocks.reactCancel).toHaveBeenCalledWith('run-chat', 'react')
    expect(mocks.reactQueueRemove).toHaveBeenCalledWith('run-chat', 'react', 'item-1')
    expect(mocks.reactQueueReorder).toHaveBeenCalledWith('run-chat', 'react', ['a', 'b'])

    mocks.reactCancel.mockRejectedValueOnce(new Error('cancel failed'))
    mocks.reactQueueRemove.mockRejectedValueOnce(new Error('remove failed'))
    mocks.reactQueueReorder.mockRejectedValueOnce(new Error('reorder failed'))
    await inbox.onClarifyCancel()
    await inbox.onClarifyQueueRemove('item-1')
    await inbox.onClarifyQueueReorder(['a'])
    await inbox.onClarifyQueueRemove(undefined)
    await inbox.onClarifyQueueReorder([])

    mocks.reactReply.mockClear()
    inbox.onClarifyFinish()
    await flushPromises()
    expect(mocks.reactReply).toHaveBeenCalled()
    await inbox.onReactRevised()
    expect(typeof inbox.itemSecondary(inbox.active.value!)).toBe('string')

    inbox.active.value = gateItem()
    await inbox.onClarifyCancel()
    await inbox.onClarifyQueueRemove('x')
    await inbox.onClarifyQueueReorder(['x'])
    app.unmount()
  })

  it('handles mobile detail navigation, sharing and card guards', async () => {
    shared.isMobile.value = true
    const { inbox, app } = await withInbox()
    const list = document.createElement('div')
    Object.defineProperty(list, 'scrollTop', { value: 45, writable: true })
    inbox.listEl.value = list
    const second = inbox.listItems.value[1]!
    inbox.openDetail(second)
    expect(inbox.mobileView.value).toBe('detail')
    expect(inbox.listScrollTop.value).toBe(45)
    inbox.backToList()
    await nextTick()
    expect(inbox.mobileView.value).toBe('list')
    expect(list.scrollTop).toBe(45)

    inbox.openSharePanel(second, true)
    expect(inbox.sharePanelOpen.value).toBe(true)
    expect(inbox.shareTarget.value?.runId).toBe('run-chat')
    inbox.patchShareStatus(second, { enabled: true, token: 'abc' } as never)
    expect(inbox.shareTarget.value?.shareLink).toEqual(expect.objectContaining({ enabled: true }))

    inbox.markProcessed(second)
    expect(inbox.isItemCardDisabled(second)).toBe(true)
    inbox.selectItem(second)
    inbox.openDetail(second)
    expect(inbox.isProcessedTriple(second)).toBe(true)
    inbox.unmarkProcessed(second)
    expect(inbox.isProcessedTriple(second)).toBe(false)
    app.unmount()
  })

  it('manages active context signals and converges items that left pending', async () => {
    const { inbox, app } = await withInbox()
    const first = inbox.listItems.value[0]!
    const second = inbox.listItems.value[1]!
    const signal = inbox.acquireInboxContextSignal('run-gate:gate:1')
    expect(signal).toBeInstanceOf(AbortSignal)
    expect(inbox.acquireInboxContextSignal('run-gate:gate:1')).toBeNull()
    const nextSignal = inbox.acquireInboxContextSignal('run-chat:react:1')
    expect(signal?.aborted).toBe(true)
    inbox.releaseInboxContextSignal('run-chat:react:1', nextSignal!)

    inbox.active.value = first
    inbox.listItems.value = [first, second]
    inbox.handleLeftInboxContext(first)
    expect(inbox.listItems.value).toEqual([second])
    expect(inbox.active.value?.runId).toBe('run-chat')
    expect(inbox.shouldFetchActiveInboxContext()).toBe(true)

    inbox.active.value = null
    expect(inbox.shouldFetchActiveInboxContext()).toBe(false)
    inbox.rollbackProcessingIntent(first)
    inbox.endProcessingIntent(first)
    app.unmount()
  })

  it('loads active context, reports hard-load errors and ignores soft failures', async () => {
    const { inbox, app } = await withInbox()
    await inbox.loadActiveRun(true)
    expect(inbox.activeRun.value?.id).toBe('run-gate')

    mocks.inboxContext.mockRejectedValueOnce(new Error('temporary'))
    await inbox.loadActiveRun(true)
    expect(inbox.activeRun.value).toBeNull()
    expect(inbox.activeRunLoadError.value).toBe(true)
    await inbox.retryActiveRun()

    mocks.inboxContext.mockRejectedValueOnce(new Error('temporary'))
    await inbox.softRefreshActiveRun()
    expect(inbox.activeRun.value?.id).toBe('run-gate')

    inbox.active.value = null
    await inbox.loadActiveRun()
    expect(inbox.activeRun.value).toBeNull()
    await inbox.softRefreshActiveRun()
    app.unmount()
  })

  it('applies manual refreshes, pending updates and editing banners', async () => {
    const { inbox, app } = await withInbox()
    // No update banner any more: a held pending change is applied by manual refresh.
    expect('showUpdateBanner' in inbox).toBe(false)
    expect('dismissUpdateBanner' in inbox).toBe(false)
    shared.hasPendingUpdate.value = true
    shared.lastPeekAt.value++
    await nextTick()
    await inbox.onManualRefresh()
    expect(mocks.applyPending).toHaveBeenCalled()

    shared.hasPendingUpdate.value = false
    shared.draft.value = 'editing'
    await nextTick()
    shared.remoteItems.value = []
    inbox.checkProcessedWhileEditing()
    expect(inbox.showProcessedBanner.value).toBe(true)
    await inbox.onManualRefresh()
    expect(mocks.refresh).toHaveBeenCalledWith({ source: 'manual', mode: 'force' })

    shared.draft.value = ''
    await nextTick()
    expect(inbox.showProcessedBanner.value).toBe(false)
    app.unmount()
  })

  it('registers an auto-apply hold while editing, focused or processing and releases it after', async () => {
    const { inbox, app } = await withInbox()
    expect(shared.autoApplyHold).toBeTypeOf('function')
    expect(shared.autoApplyHold!()).toBe(false)

    shared.draft.value = '还在写'
    await nextTick()
    expect(shared.autoApplyHold!()).toBe(true)
    mocks.autoApplyPending.mockClear()
    shared.draft.value = ''
    await nextTick()
    // Hold ended → apply whatever the peek left pending.
    expect(mocks.autoApplyPending).toHaveBeenCalledTimes(1)

    const field = document.createElement('input')
    document.body.appendChild(field)
    field.focus()
    document.dispatchEvent(new FocusEvent('focusin'))
    await nextTick()
    // Focus alone (empty field, e.g. the composer right after sending) does not hold.
    expect(inbox.focusedFieldHasDraft.value).toBe(false)
    expect(shared.autoApplyHold!()).toBe(false)
    field.value = '筛选词'
    field.dispatchEvent(new Event('input', { bubbles: true }))
    await nextTick()
    expect(inbox.focusedFieldHasDraft.value).toBe(true)
    expect(shared.autoApplyHold!()).toBe(true)
    field.blur()
    document.dispatchEvent(new FocusEvent('focusout'))
    await nextTick()
    await nextTick()
    expect(inbox.focusedFieldHasDraft.value).toBe(false)
    field.remove()

    app.unmount()
    // Page gone: the hold is cleared and pending changes flow to the sidebar.
    expect(shared.autoApplyHold).toBeNull()
    expect(mocks.autoApplyPending).toHaveBeenCalled()
  })

  it('reloads the page list silently after an auto-apply and keeps the selection', async () => {
    const { inbox, app } = await withInbox()
    inbox.active.value = inbox.listItems.value[1]!
    await flushPromises()
    const selectedKey = `${inbox.active.value.runId}:${inbox.active.value.nodeId}`
    mocks.listGates.mockClear()
    mocks.listGates.mockResolvedValue({
      items: [{ ...gateItem(), runId: 'run-new', title: '新审批' } as InboxItem, gateItem(), clarifyItem()],
      total: 3,
    })
    shared.lastAutoAppliedAt.value = Date.now()
    await nextTick()
    expect(inbox.listLoading.value).toBe(false)
    await flushPromises()
    expect(mocks.listGates).toHaveBeenCalledTimes(1)
    expect(inbox.listItems.value.map((it) => it.runId)).toContain('run-new')
    expect(`${inbox.active.value!.runId}:${inbox.active.value!.nodeId}`).toBe(selectedKey)
    app.unmount()
  })

  it('keeps holding while a focused field is cleared and typed into again, then applies once input is idle', async () => {
    const { inbox, app } = await withInbox()
    const field = document.createElement('textarea')
    document.body.appendChild(field)
    field.focus()
    document.dispatchEvent(new FocusEvent('focusin'))
    const type = async (value: string) => {
      field.value = value
      field.dispatchEvent(new Event('input', { bubbles: true }))
      await nextTick()
    }
    vi.useFakeTimers()
    try {
      await type('有草稿')
      expect(shared.autoApplyHold!()).toBe(true)
      mocks.autoApplyPending.mockClear()

      // Ctrl+A, Backspace: the draft is empty but the user is still typing.
      await type('')
      expect(inbox.focusedFieldHasDraft.value).toBe(false)
      expect(inbox.typingRecently.value).toBe(true)
      expect(shared.autoApplyHold!()).toBe(true)
      expect(mocks.autoApplyPending).not.toHaveBeenCalled()

      vi.advanceTimersByTime(TYPING_IDLE_MS - 200)
      await type('继续')
      vi.advanceTimersByTime(TYPING_IDLE_MS + 200)
      await nextTick()
      // Idle, but a draft is in the field: still held.
      expect(shared.autoApplyHold!()).toBe(true)
      expect(mocks.autoApplyPending).not.toHaveBeenCalled()

      await type('')
      vi.advanceTimersByTime(TYPING_IDLE_MS - 200)
      await nextTick()
      expect(mocks.autoApplyPending).not.toHaveBeenCalled()
      vi.advanceTimersByTime(400)
      await nextTick()
      // Empty and idle for TYPING_IDLE_MS: the pending change applies once.
      expect(shared.autoApplyHold!()).toBe(false)
      expect(mocks.autoApplyPending).toHaveBeenCalledTimes(1)
    } finally {
      vi.useRealTimers()
      field.remove()
      app.unmount()
    }
  })

  it('applies on blur right after typing without waiting for the idle timer', async () => {
    const { inbox, app } = await withInbox()
    const field = document.createElement('textarea')
    document.body.appendChild(field)
    field.focus()
    document.dispatchEvent(new FocusEvent('focusin'))
    field.value = 'x'
    field.dispatchEvent(new Event('input', { bubbles: true }))
    field.value = ''
    field.dispatchEvent(new Event('input', { bubbles: true }))
    await nextTick()
    expect(shared.autoApplyHold!()).toBe(true)
    mocks.autoApplyPending.mockClear()
    field.blur()
    document.dispatchEvent(new FocusEvent('focusout'))
    await nextTick()
    await nextTick()
    expect(inbox.typingRecently.value).toBe(false)
    expect(mocks.autoApplyPending).toHaveBeenCalledTimes(1)
    field.remove()
    app.unmount()
  })

  it('keeps the deep-linked selection object across a silent reload so the chat is not remounted', async () => {
    mocks.listGates.mockResolvedValue({ items: [gateItem(), clarifyItem()], total: 2 })
    const { inbox, app } = await withInbox('/inbox?run=run-chat&node=react')
    await flushPromises()
    const before = inbox.active.value
    expect(before?.runId).toBe('run-chat')
    mocks.listGates.mockResolvedValue({
      items: [gateItem('run-new'), gateItem(), clarifyItem()],
      total: 3,
    })
    shared.lastAutoAppliedAt.value = Date.now()
    await flushPromises()
    expect(inbox.listItems.value.map((it) => it.runId)).toContain('run-new')
    // Same object: watch(active) does not reload the run / remount the composer.
    expect(inbox.active.value).toBe(before)
    app.unmount()
  })

  it('puts focus and the caret back if a silent reload re-renders the focused composer', async () => {
    const { app } = await withInbox()
    const mk = () => {
      const ta = document.createElement('textarea')
      ta.setAttribute('data-testid', 'clarify-input')
      ta.value = '继续输入'
      document.body.appendChild(ta)
      return ta
    }
    const old = mk()
    old.focus()
    old.setSelectionRange(2, 2)
    let replacement: HTMLTextAreaElement | null = null
    mocks.listGates.mockImplementation(async () => {
      old.remove()
      replacement = mk()
      return { items: [gateItem(), clarifyItem()], total: 2 }
    })
    shared.lastAutoAppliedAt.value = Date.now()
    await flushPromises()
    await nextTick()
    expect(replacement).not.toBeNull()
    expect(document.activeElement).toBe(replacement)
    expect(replacement!.selectionStart).toBe(2)
    expect(replacement!.selectionEnd).toBe(2)
    replacement!.remove()
    app.unmount()
  })

  it('never pulls focus back once the user moved to another field', async () => {
    const { app } = await withInbox()
    const old = document.createElement('textarea')
    old.setAttribute('data-testid', 'clarify-input')
    document.body.appendChild(old)
    old.focus()
    const other = document.createElement('input')
    document.body.appendChild(other)
    let replacement: HTMLTextAreaElement | null = null
    mocks.listGates.mockImplementation(async () => {
      old.remove()
      replacement = document.createElement('textarea')
      replacement.setAttribute('data-testid', 'clarify-input')
      document.body.appendChild(replacement)
      other.focus()
      return { items: [gateItem()], total: 1 }
    })
    shared.lastAutoAppliedAt.value = Date.now()
    await flushPromises()
    await nextTick()
    expect(document.activeElement).toBe(other)
    replacement!.remove()
    other.remove()
    app.unmount()
  })

  it('merges remote replying state and patches visible clarify cards', async () => {
    const { inbox, app } = await withInbox()
    const clarify = inbox.listItems.value[1]!
    inbox.patchVisibleCardBusy('run-chat', 'react', true)
    expect(mocks.patchItemReplying).toHaveBeenCalledWith('run-chat:react', true)

    shared.remoteItems.value = [{ ...clarify, state: 'replying' } as InboxItem]
    await nextTick()
    expect(inbox.listItems.value.find((it) => it.runId === 'run-chat')?.state).toBe('replying')
    shared.remoteItems.value = []
    await nextTick()
    app.unmount()
  })

  it('routes focus and visibility peeks and reacts to query deep links', async () => {
    const { inbox, app, router } = await withInbox('/inbox?run=run-chat&node=react')
    expect(inbox.active.value?.runId).toBe('run-chat')
    inbox.onFocus()
    expect(mocks.peek).toHaveBeenCalledWith({ source: 'focus' })
    Object.defineProperty(document, 'visibilityState', { value: 'visible', configurable: true })
    inbox.onVisible()
    expect(mocks.peek).toHaveBeenCalledWith({ source: 'visibility' })
    Object.defineProperty(document, 'visibilityState', { value: 'hidden', configurable: true })
    mocks.peek.mockClear()
    inbox.onVisible()
    expect(mocks.peek).not.toHaveBeenCalled()

    await router.push('/inbox?run=run-gate&node=gate')
    await flushPromises()
    expect(inbox.selectFromQuery()).toBe(true)
    app.unmount()
  })

  it('processes active-run websocket review, ACP and artifact frames', async () => {
    const { inbox, app } = await withInbox()
    const socket = MockWebSocket.instances.at(-1)!
    const applyReviewFrame = vi.fn(() => true)
    const applyAcpEvents = vi.fn(() => true)
    inbox.gateApprovalRef.value = { applyReviewFrame, applyAcpEvents, isEditing: false } as never
    socket.open()
    socket.message({ type: 'review', event: 'turn_begin', nodeId: 'gate' })
    expect(inbox.clarifyLiveBusy.value).toBe(true)
    expect(applyReviewFrame).toHaveBeenCalled()

    socket.message({
      type: 'acp',
      nodeId: 'producer',
      events: [{ kind: 'thought', text: 'think' }, { kind: 'message', text: 'done' }],
    })
    expect(applyAcpEvents).toHaveBeenCalled()

    mocks.inboxContext.mockClear()
    socket.message({ type: 'artifact_edit', previewArtifact: 'preview.png' })
    await flushPromises()
    expect(mocks.inboxContext).toHaveBeenCalled()

    mocks.runArtifacts.mockResolvedValueOnce([{ id: 'a1', name: 'preview.png' }])
    await inbox.patchActiveRunArtifacts({ previewArtifact: 'preview.png' })
    expect(mocks.runArtifacts).toHaveBeenCalled()

    socket.message({ type: 'review', event: 'queue_state', nodeId: 'gate', busy: false, waiting: 0 })
    socket.message({ type: 'review', event: 'turn_done', nodeId: 'gate' })
    socket.message({ type: 'status' })
    socket.message({ type: 'trace' })
    socket.onmessage?.(new MessageEvent('message', { data: '{bad json' }))
    socket.close()
    app.unmount()
  })

  it('tracks this user\'s page-control state per node from the run socket', async () => {
    const { inbox, app } = await withInbox()
    const socket = MockWebSocket.instances.at(-1)!
    socket.open()
    socket.message({ type: 'page_control_state', nodeId: 'producer', state: 'online' })
    expect(inbox.pageControlByNode.value).toEqual({ producer: 'online' })
    socket.message({ type: 'page_control_state', nodeId: 'producer', state: 'weird' })
    expect(inbox.pageControlByNode.value).toEqual({ producer: 'offline' })
    socket.message({ type: 'page_control_state', state: 'online' })
    expect(inbox.pageControlByNode.value).toEqual({ producer: 'offline' })
    app.unmount()
  })

  it('buffers dialogue frames until child surfaces mount', async () => {
    const { inbox, app } = await withInbox()
    inbox.active.value = inbox.listItems.value[1]!
    await nextTick()
    inbox.reviewChatRef.value = null
    inbox.applyOrBufferReviewFrame({ event: 'queue_state', nodeId: 'react', busy: true })
    inbox.applyOrBufferAcpFrame({
      nodeId: 'react',
      events: [{ kind: 'message', text: 'buffered' }],
      busy: true,
    })
    const applyReviewFrame = vi.fn()
    const applyAcpEvents = vi.fn(() => true)
    inbox.reviewChatRef.value = { applyReviewFrame, applyAcpEvents }
    inbox.flushPendingReviewFrames()
    inbox.flushPendingAcpFrames()
    expect(applyReviewFrame).toHaveBeenCalled()
    expect(applyAcpEvents).toHaveBeenCalled()

    mocks.nodeEvents.mockResolvedValueOnce({
      events: [{ kind: 'thought', text: 'seed' }],
      hasMore: false,
    })
    expect(await inbox.seedClarifyAcpFromNodeEventsOnce('run-chat', 'react')).toBe(true)
    mocks.nodeEvents.mockRejectedValueOnce(new Error('offline'))
    expect(await inbox.seedClarifyAcpFromNodeEventsOnce('run-chat', 'react')).toBe(false)
    app.unmount()
  })

  it('repairs selection and starting-failure cards', async () => {
    const { inbox, app } = await withInbox()
    const first = inbox.listItems.value[0]!
    const second = inbox.listItems.value[1]!
    inbox.active.value = gateItem('missing')
    inbox.ensureValidActive()
    expect(inbox.active.value?.runId).toBe(first.runId)

    inbox.listItems.value = []
    inbox.ensureValidActive()
    expect(inbox.active.value).toBeNull()
    inbox.listItems.value = [first, second]
    inbox.selectActiveAfterRemove([first, second], 'run-gate:gate')
    expect(inbox.active.value?.runId).toBe('run-chat')

    const starting = gateItem('booting', { state: 'starting' })
    mocks.getRun.mockResolvedValueOnce(contextRun('booting', { status: 'failed' }))
    await inbox.confirmStartingVanished(starting)
    if (inbox.startFailedItem.value) {
      inbox.dismissStartFailure()
      expect(inbox.startFailedItem.value).toBeNull()
    }
    inbox.dismissStartFailure()
    app.unmount()
  })

  it('reconciles processed rows only after an absent-then-present cycle', async () => {
    const { inbox, app } = await withInbox()
    const first = inbox.listItems.value[0]!
    inbox.markProcessed(first)
    inbox.reconcileProcessedWithList([])
    expect(inbox.confirmedAbsentTriples.size).toBe(1)
    inbox.reconcileProcessedWithList([first])
    expect(inbox.isProcessedTriple(first)).toBe(false)

    inbox.listItems.value = [first]
    inbox.active.value = first
    shared.draft.value = 'dirty'
    await nextTick()
    inbox.syncActiveAfterApply([], 'run-gate:gate')
    expect(inbox.showProcessedBanner.value).toBe(true)
    shared.draft.value = ''
    await nextTick()
    inbox.syncActiveAfterApply([], 'run-gate:gate')
    expect(inbox.active.value).toBeNull()

    inbox.active.value = null
    inbox.syncActiveAfterApply([first], null)
    expect(inbox.active.value).toBe(first)
    inbox.syncActiveAfterApply([first], 'run-gate:gate')
    expect(inbox.showProcessedBanner.value).toBe(false)
    app.unmount()
  })

  it('drops a completed deep-link ghost and retains a failed startup card', async () => {
    mocks.listGates.mockResolvedValue({ items: [], total: 0 })
    mocks.getRun.mockResolvedValue(contextRun('finished', { status: 'completed' }))
    const completed = await withInbox('/inbox?run=finished&node=approve')
    await flushPromises()
    expect(completed.inbox.incomingGhost.value).toBeNull()
    expect(completed.inbox.incomingArmed.value).toBe(false)
    completed.app.unmount()

    mocks.getRun.mockResolvedValue(contextRun('failed', { status: 'failed' }))
    const failed = await withInbox('/inbox?run=failed&node=approve')
    await flushPromises()
    expect(failed.inbox.incomingGhost.value).toBeNull()
    expect(failed.inbox.startFailedItem.value?.runId).toBe('failed')
    expect(failed.inbox.startFailedActive.value).toBe(true)
    failed.inbox.dismissStartFailure()
    expect(failed.inbox.startFailedItem.value).toBeNull()
    failed.app.unmount()
  })

  it('restores session snapshots and derives producer node ids', async () => {
    const { inbox, app } = await withInbox()
    const applyReviewFrame = vi.fn(() => true)
    inbox.gateApprovalRef.value = { applyReviewFrame, isEditing: false } as never
    expect(inbox.activeDialogueNodeId(contextRun('run-gate'))).toBe('producer')
    inbox.restoreReactSessions(contextRun('run-gate', {
      reactSessions: {
        producer: { busy: true, waiting: 1, items: [{ id: 'q1', text: 'wait' }] },
      },
    }))
    expect(inbox.clarifyLiveBusy.value).toBe(true)
    expect(applyReviewFrame).toHaveBeenCalledWith(expect.objectContaining({
      event: 'queue_state',
      nodeId: 'producer',
      busy: true,
    }))

    const clarify = inbox.listItems.value[1]!
    inbox.active.value = clarify
    await nextTick()
    expect(inbox.activeDialogueNodeId()).toBe('react')
    inbox.reviewChatRef.value = null
    inbox.restoreReactSessions(contextRun('run-chat', {
      reactSessions: { react: { busy: false, waiting: 0, items: [] } },
    }))
    inbox.reviewChatRef.value = { applyReviewFrame }
    inbox.flushPendingReviewFrames()
    expect(applyReviewFrame).toHaveBeenCalled()
    app.unmount()
  })

  it('handles clarify websocket busy guards and artifact patch failures', async () => {
    const { inbox, app } = await withInbox()
    const clarify = inbox.listItems.value[1]!
    inbox.active.value = clarify
    await flushPromises()
    const socket = MockWebSocket.instances.at(-1)!
    const applyReviewFrame = vi.fn(() => true)
    inbox.reviewChatRef.value = {
      applyReviewFrame,
      applyAcpEvents: vi.fn(() => true),
      isSessionBusy: () => true,
    }
    expect(inbox.isClarifySoftRefreshBlocked()).toBe(true)
    socket.message({ type: 'review', event: 'turn_begin', nodeId: 'react' })
    socket.message({ type: 'artifact_edit', previewArtifact: 'preview.png' })
    await flushPromises()
    expect(mocks.runArtifacts).toHaveBeenCalled()
    socket.message({ type: 'review', event: 'error', nodeId: 'react', message: 'turn failed' })
    await flushPromises()
    expect(inbox.clarifyLiveBusy.value).toBe(false)

    mocks.runArtifacts.mockRejectedValueOnce(new Error('offline'))
    await inbox.patchActiveRunArtifacts()
    expect(inbox.activeRun.value).not.toBeNull()

    inbox.reviewChatRef.value = { isSessionBusy: () => false }
    expect(inbox.isClarifySoftRefreshBlocked()).toBe(false)
    inbox.clarifyLiveBusy.value = true
    expect(inbox.isClarifySoftRefreshBlocked()).toBe(true)
    app.unmount()
  })

  it('derives status, preview and composer presentation states', async () => {
    const { inbox, app } = await withInbox()
    expect(inbox.autoApplyHeld.value).toBe(false)
    shared.draft.value = 'draft'
    await nextTick()
    expect(inbox.autoApplyHeld.value).toBe(true)
    shared.draft.value = ''
    await nextTick()
    expect(inbox.autoApplyHeld.value).toBe(false)

    const clarify = inbox.listItems.value[1]!
    inbox.active.value = clarify
    await flushPromises()
    inbox.activeRun.value = contextRun('run-chat', {
      nodes: [{ id: 'react', type: 'agent', caps: PREVIEW_REVIEW_CAPS, config: {} }],
      status: 'running',
    })
    expect(inbox.inboxAppPreviewActive.value).toBe(true)
    expect(inbox.inboxStageNode.value?.id).toBe('react')
    expect(inbox.clarifyInputActive.value).toBe(true)
    expect(inbox.clarifyComposerNodeId.value).toBe('react')
    expect(inbox.clarifyComposerIteration.value).toBe(1)
    expect(inbox.clarifyComposerTurns.value).toEqual([])
    expect(inbox.clarifyComposerDone.value).toBe(false)

    inbox.onAppPreviewReviewPick({ selector: '#new', url: ' https://app/ ', tagName: 'A' })
    expect(inbox.lastStagedAppPreviewPick.value).toBeNull()
    inbox.active.value = gateItem()
    inbox.onAppPreviewReviewPick({ selector: '#ignored', url: '', tagName: 'DIV' })
    app.unmount()
  })

  it('starts and stops the bounded startup poll', async () => {
    vi.useFakeTimers()
    const { inbox, app } = await withInbox()
    const starting = clarifyItem('booting', { state: 'starting' })
    inbox.listItems.value = [starting]
    inbox.active.value = starting
    await nextTick()
    mocks.listGates.mockClear()
    inbox.startStartingPoll()
    await vi.advanceTimersByTimeAsync(inbox.STARTING_POLL_MS)
    expect(mocks.listGates).toHaveBeenCalled()

    inbox.active.value = null
    await nextTick()
    await vi.advanceTimersByTimeAsync(inbox.STARTING_POLL_MS)
    inbox.stopStartingPoll()
    app.unmount()
    vi.useRealTimers()
  })

  it('handles websocket construction failures and explicit closes', async () => {
    const { inbox, app } = await withInbox()
    inbox.connectActiveRunWs('')
    expect(inbox.activeRunWsRunId).toBe('')

    vi.stubGlobal('WebSocket', class {
      constructor() {
        throw new Error('socket denied')
      }
    })
    inbox.connectActiveRunWs('run-gate')
    expect(inbox.activeRunWsRunId).toBe('')
    inbox.closeActiveRunWs()
    expect(inbox.clarifyLiveBusy.value).toBe(false)
    app.unmount()
  })

  it('handles inbox-context 404s and incoming-ghost transient checks', async () => {
    const { inbox, app } = await withInbox()
    const first = inbox.listItems.value[0]!
    inbox.active.value = first
    mocks.inboxContext.mockRejectedValueOnce(new Error('no pending inbox item'))
    await inbox.loadActiveRun(true)
    expect(inbox.listItems.value.some((it) => it.runId === first.runId)).toBe(false)

    inbox.incomingArmed.value = true
    mocks.getRun.mockRejectedValueOnce(new Error('temporary'))
    await inbox.confirmIncomingGhostStillNeeded({ runId: 'new-run', nodeId: 'approve' })
    expect(inbox.incomingGhostConfirmInFlight).toBe('')

    const rows = [clarifyItem()]
    inbox.incomingArmed.value = false
    expect(inbox.mergeIncomingGhost(rows)).toBe(rows)
    app.unmount()
  })

  it('projects busy clarify sessions and websocket snapshots after reconnect', async () => {
    const { inbox, app } = await withInbox()
    const clarify = inbox.listItems.value[1]!
    inbox.active.value = clarify
    await flushPromises()
    const applyReviewFrame = vi.fn(() => true)
    const applyAcpEvents = vi.fn(() => true)
    inbox.reviewChatRef.value = { applyReviewFrame, applyAcpEvents, isSessionBusy: () => false }
    const busyRun = contextRun('run-chat', {
      reactSessions: {
        react: {
          busy: true,
          waiting: 1,
          items: [{ id: 'q1', text: 'queued' }],
          activeItem: { id: 'active', text: 'active' },
        },
      },
    })
    inbox.activeRun.value = busyRun
    mocks.nodeEvents.mockResolvedValue({
      events: [{ kind: 'thought', text: 'seed thought' }, { kind: 'message', text: 'seed answer' }],
      hasMore: false,
    })
    await inbox.projectClarifySessionAfterLoad(busyRun)
    expect(applyReviewFrame).toHaveBeenCalled()
    expect(applyAcpEvents).toHaveBeenCalled()

    const current = MockWebSocket.instances.at(-1)!
    current.message({
      type: 'snapshot',
      run: { reactSessions: { react: { busy: false, waiting: 0, items: [] } } },
    })
    await flushPromises()
    expect(inbox.activeRun.value?.reactSessions?.react?.busy).toBe(false)

    inbox.connectActiveRunWs('run-chat', { fromReconnect: true })
    const replacement = MockWebSocket.instances.at(-1)!
    replacement.open()
    await flushPromises()
    expect(MockWebSocket.instances.length).toBeGreaterThan(1)

    await inbox.reseedAfterWsReconnect('other-run')
    inbox.activeRun.value = null
    await inbox.reseedAfterWsReconnect('run-chat')
    app.unmount()
  })

  it('derives a matching home seed and merges retained startup failures', async () => {
    const { inbox, app } = await withInbox()
    inbox.homeSeed.value = {
      runId: 'run-gate',
      nodeId: 'gate',
      text: 'Started from home',
    } as never
    expect(inbox.activeHomeSeed.value?.text).toBe('Started from home')
    inbox.active.value = null
    expect(inbox.activeHomeSeed.value).toBeNull()

    const failed = clarifyItem('failed-start', { state: 'starting' })
    inbox.startFailedItem.value = failed
    expect(inbox.mergeFailedStarting([gateItem()])[0]?.runId).toBe('failed-start')
    expect(inbox.mergeFailedStarting([failed, gateItem()])[0]?.runId).toBe('failed-start')
    expect(inbox.startFailedItem.value).toBeNull()
    app.unmount()
  })

  it('opens the existing share panel once from the sidebar intent and does not create a link', async () => {
    const { inbox, app, router } = await withInbox('/inbox?run=run-chat&node=react&share=1')
    await flushPromises()
    expect(inbox.sharePanelOpen.value).toBe(true)
    expect(inbox.shareTarget.value?.runId).toBe('run-chat')
    expect(inbox.shareTarget.value?.nodeId).toBe('react')
    expect(router.currentRoute.value.query.share).toBeUndefined()
    expect(router.currentRoute.value.query.run).toBe('run-chat')
    expect(router.currentRoute.value.query.node).toBe('react')
    expect(mocks.createGateShareLink).not.toHaveBeenCalled()
    expect(mocks.createReviewShareLink).not.toHaveBeenCalled()
    app.unmount()

    const again = await withInbox('/inbox?run=run-chat&node=react')
    await flushPromises()
    expect(again.inbox.sharePanelOpen.value).toBe(false)
    expect(mocks.createReviewShareLink).not.toHaveBeenCalled()
    again.app.unmount()
  })

  it('opens the share panel for each new share intent on the same inbox instance', async () => {
    mocks.listGates.mockResolvedValue({
      items: [
        gateItem('run-gate', { nodeType: 'human_gate' }),
        clarifyItem('run-chat'),
      ],
      total: 2,
    })
    const { inbox, app, router } = await withInbox('/inbox?run=run-chat&node=react')
    await flushPromises()
    expect(inbox.sharePanelOpen.value).toBe(false)
    expect(inbox.active.value?.runId).toBe('run-chat')

    await router.push({ path: '/inbox', query: { run: 'run-chat', node: 'react', share: '1' } })
    await flushPromises()
    expect(inbox.sharePanelOpen.value).toBe(true)
    expect(inbox.shareTarget.value?.runId).toBe('run-chat')
    expect(router.currentRoute.value.query.share).toBeUndefined()
    expect(mocks.createGateShareLink).not.toHaveBeenCalled()
    expect(mocks.createReviewShareLink).not.toHaveBeenCalled()

    inbox.sharePanelOpen.value = false
    inbox.shareTarget.value = null

    await router.push({ path: '/inbox', query: { run: 'run-gate', node: 'gate', share: '1' } })
    await flushPromises()
    expect(inbox.active.value?.runId).toBe('run-gate')
    expect(inbox.sharePanelOpen.value).toBe(true)
    expect(inbox.shareTarget.value?.runId).toBe('run-gate')
    expect(inbox.shareTarget.value?.nodeId).toBe('gate')
    expect(router.currentRoute.value.query.share).toBeUndefined()
    expect(router.currentRoute.value.query.run).toBe('run-gate')
    expect(router.currentRoute.value.query.node).toBe('gate')
    expect(mocks.createGateShareLink).not.toHaveBeenCalled()
    expect(mocks.createReviewShareLink).not.toHaveBeenCalled()

    inbox.sharePanelOpen.value = false
    await router.push({ path: '/inbox', query: { run: 'run-gate', node: 'gate' } })
    await flushPromises()
    expect(inbox.sharePanelOpen.value).toBe(false)
    expect(router.currentRoute.value.query.share).toBeUndefined()
    app.unmount()
  })

  it('does not open the share panel for a used link, a starting row, a lock, or a non-shareable row', async () => {
    mocks.listGates.mockResolvedValue({
      items: [
        clarifyItem('run-used', { shareLink: { state: 'used', canCreate: false } }),
        clarifyItem('run-start', { state: 'starting' }),
        gateItem('run-other', { nodeType: 'agent' }),
        clarifyItem('run-open'),
      ],
      total: 4,
    })
    const used = await withInbox('/inbox?run=run-used&node=react&share=1')
    await flushPromises()
    expect(used.inbox.sharePanelOpen.value).toBe(false)
    expect(used.router.currentRoute.value.query.share).toBeUndefined()
    used.app.unmount()

    const starting = await withInbox('/inbox?run=run-start&node=react&share=1')
    await flushPromises()
    expect(starting.inbox.sharePanelOpen.value).toBe(false)
    expect(starting.router.currentRoute.value.query.share).toBeUndefined()
    starting.app.unmount()

    const other = await withInbox('/inbox?run=run-other&node=gate&share=1')
    await flushPromises()
    expect(other.inbox.sharePanelOpen.value).toBe(false)
    expect(other.router.currentRoute.value.query.share).toBeUndefined()
    other.app.unmount()

    const locked = await withInbox('/inbox?run=run-open&node=react')
    await flushPromises()
    const it = locked.inbox.listItems.value.find((row) => row.runId === 'run-open')!
    locked.inbox.active.value = it
    locked.inbox.beginProcessingIntent(it)
    await locked.router.push({ path: '/inbox', query: { run: 'run-open', node: 'react', share: '1' } })
    await flushPromises()
    await nextTick()
    expect(locked.inbox.sharePanelOpen.value).toBe(false)
    expect(locked.router.currentRoute.value.query.share).toBeUndefined()
    expect(mocks.createGateShareLink).not.toHaveBeenCalled()
    expect(mocks.createReviewShareLink).not.toHaveBeenCalled()
    locked.app.unmount()
  })

  it('still opens the share panel when share status is missing, expired, or revoked', async () => {
    mocks.listGates.mockResolvedValue({
      items: [gateItem('run-gate', { nodeType: 'human_gate' })],
      total: 1,
    })
    const missing = await withInbox('/inbox?run=run-gate&node=gate&share=1')
    await flushPromises()
    expect(missing.inbox.sharePanelOpen.value).toBe(true)
    expect(missing.router.currentRoute.value.query.share).toBeUndefined()
    expect(mocks.createGateShareLink).not.toHaveBeenCalled()
    missing.app.unmount()

    mocks.listGates.mockResolvedValue({
      items: [clarifyItem('run-chat', { shareLink: { state: 'expired', canCreate: true } })],
      total: 1,
    })
    const expired = await withInbox('/inbox?run=run-chat&node=react&share=1')
    await flushPromises()
    expect(expired.inbox.sharePanelOpen.value).toBe(true)
    expired.app.unmount()

    mocks.listGates.mockResolvedValue({
      items: [clarifyItem('run-chat', { kind: 'review', shareLink: { state: 'revoked', canCreate: true } })],
      total: 1,
    })
    const revoked = await withInbox('/inbox?run=run-chat&node=react&share=1')
    await flushPromises()
    expect(revoked.inbox.sharePanelOpen.value).toBe(true)
    expect(mocks.createReviewShareLink).not.toHaveBeenCalled()
    revoked.app.unmount()
  })
})
