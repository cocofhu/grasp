// @vitest-environment happy-dom
import { createApp, computed, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { Run } from '@/lib/shared/types'

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      nodeEvents: vi.fn(async () => ({ events: [], nextCursor: '', hasMore: false })),
      nodeSandboxLog: vi.fn(async () => ({ content: 'log', live: false, found: true })),
      getRunNodeSandbox: vi.fn(async () => ({ id: 'sbx-1' })),
    },
  }
})

import { api } from '@/lib/api/api'
import { useRunDetailLiveLog } from './useRunDetailLiveLog'

describe('useRunDetailLiveLog', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('exposes log helpers and selection side effects without hanging', async () => {
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
      cb(0)
      return 1
    })

    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/', component: { template: '<div />' } },
        { path: '/sandboxes/:id/console', component: { template: '<div />' } },
      ],
    })
    await router.push('/')
    await router.isReady()

    const run = ref({
      id: 'run-1',
      status: 'running',
      nodeRuns: { a1: { nodeId: 'a1', status: 'running', events: [], mcpCalls: [] } },
      artifacts: [],
    } as unknown as Run)
    const selected = ref<string | null>(null)
    const nodeTab = ref('log')
    const selIterIdx = ref<number | null>(null)

    let live!: ReturnType<typeof useRunDetailLiveLog>
    const app = createApp({
      setup() {
        live = useRunDetailLiveLog({
          runId: computed(() => 'run-1'),
          run,
          selected,
          selExecIdx: computed(() => 0),
          selIterIdx,
          selRun: computed(() => run.value.nodeRuns.a1 || null),
          selStatus: computed(() => 'running'),
          viewingLatest: computed(() => true),
          nodeTab,
          hasLog: computed(() => true),
        })
        return () => null
      },
    })
    app.use(i18n)
    app.use(router)
    app.mount(document.createElement('div'))

    // Trigger selection watch after mount (not immediate).
    selected.value = 'a1'
    await Promise.resolve()
    await Promise.resolve()

    expect(live.logEvents.value).toEqual([])
    await live.fetchNodeEvents('a1')
    live.mergeLiveWsAcpPage('a1', [{ kind: 'message', text: 'hi', t: 1 }])
    let current = true
    let finish!: (value: { events: { kind: string; text: string; t: number }[] }) => void
    vi.mocked(api.nodeEvents).mockImplementationOnce(() => new Promise((resolve) => { finish = resolve }))
    const staleRead = live.fetchNodeEvents('a1', { isCurrent: () => current })
    live.mergeLiveWsAcpPage('a1', [{ kind: 'message', text: 'newer live reply', t: 2 }])
    current = false
    finish({ events: [{ kind: 'message', text: 'older REST reply should never enter this turn', t: 1 }] })
    expect(await staleRead).toBe(false)
    expect(live.eventPages.a1?.events).toEqual([expect.objectContaining({ kind: 'message', text: 'newer live reply', t: 2 })])
    live.syncAllMcpCallsFromRun()
    live.goSandboxLogTab()
    expect(nodeTab.value).toBe('sandbox')
    live.openSandboxConsole()
    live.maybePollSandboxForBoot()
    live.resetLiveLogState('run-1')
    live.abortSandboxFetches()
    live.disposeAllRehydrateOrchs()

    app.unmount()
  })

  it('silent_poll does not toggle sbxLogLoading; user_initiated does', async () => {
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
      cb(0)
      return 1
    })

    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/', component: { template: '<div />' } }],
    })
    await router.push('/')
    await router.isReady()

    const run = ref({
      id: 'run-1',
      status: 'running',
      nodeRuns: { a1: { nodeId: 'a1', status: 'running', events: [], mcpCalls: [] } },
      artifacts: [],
    } as unknown as Run)
    const selected = ref<string | null>('a1')
    const nodeTab = ref('sandbox')
    const selIterIdx = ref<number | null>(null)

    let live!: ReturnType<typeof useRunDetailLiveLog>
    const app = createApp({
      setup() {
        live = useRunDetailLiveLog({
          runId: computed(() => 'run-1'),
          run,
          selected,
          selExecIdx: computed(() => 0),
          selIterIdx,
          selRun: computed(() => run.value.nodeRuns.a1 || null),
          selStatus: computed(() => 'running'),
          viewingLatest: computed(() => true),
          nodeTab,
          hasLog: computed(() => true),
        })
        return () => null
      },
    })
    app.use(i18n)
    app.use(router)
    app.mount(document.createElement('div'))

    live.sbxLogs.a1 = { content: 'cached', live: true, found: true }
    expect(live.sbxLogLoading.value).toBe(false)

    const silentPromise = live.fetchSandboxLog('a1', { intent: 'silent_poll' })
    expect(live.sbxLogLoading.value).toBe(false)
    await silentPromise
    expect(live.sbxLogLoading.value).toBe(false)

    const userPromise = live.fetchSandboxLog('a1', { intent: 'user_initiated' })
    expect(live.sbxLogLoading.value).toBe(true)
    await userPromise
    expect(live.sbxLogLoading.value).toBe(false)

    app.unmount()
  })

  it('silent_poll skips write-back when snapshot unchanged', async () => {
    const nodeSandboxLog = vi.mocked(api.nodeSandboxLog)
    nodeSandboxLog.mockResolvedValueOnce({
      content: 'same',
      live: true,
      found: true,
    })

    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/', component: { template: '<div />' } }],
    })
    await router.push('/')
    await router.isReady()

    const run = ref({
      id: 'run-1',
      status: 'running',
      nodeRuns: { a1: { nodeId: 'a1', status: 'running', events: [], mcpCalls: [] } },
      artifacts: [],
    } as unknown as Run)
    const selected = ref<string | null>('a1')

    let live!: ReturnType<typeof useRunDetailLiveLog>
    const app = createApp({
      setup() {
        live = useRunDetailLiveLog({
          runId: computed(() => 'run-1'),
          run,
          selected,
          selExecIdx: computed(() => 0),
          selIterIdx: ref(null),
          selRun: computed(() => run.value.nodeRuns.a1 || null),
          selStatus: computed(() => 'running'),
          viewingLatest: computed(() => true),
          nodeTab: ref('sandbox'),
          hasLog: computed(() => true),
        })
        return () => null
      },
    })
    app.use(i18n)
    app.use(router)
    app.mount(document.createElement('div'))

    const prev = { content: 'same', live: true, found: true }
    live.sbxLogs.a1 = { ...prev }
    await live.fetchSandboxLog('a1', { intent: 'silent_poll' })
    expect(live.sbxLogs.a1).toEqual(prev)

    app.unmount()
  })

  it('fetchNodeEvents soft-fail (unavailable) returns false and leaves rehydrate as error', async () => {
    vi.mocked(api.nodeEvents).mockResolvedValueOnce({
      events: [],
      live: false,
      unavailable: true,
      error: 'live event log read failed',
    })

    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
      cb(0)
      return 1
    })

    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/', component: { template: '<div />' } }],
    })
    await router.push('/')
    await router.isReady()

    const run = ref({
      id: 'run-1',
      status: 'running',
      nodeRuns: { a1: { nodeId: 'a1', status: 'running', events: [], mcpCalls: [] } },
      artifacts: [],
    } as unknown as Run)
    const selected = ref<string | null>('a1')

    let live!: ReturnType<typeof useRunDetailLiveLog>
    const app = createApp({
      setup() {
        live = useRunDetailLiveLog({
          runId: computed(() => 'run-1'),
          run,
          selected,
          selExecIdx: computed(() => 0),
          selIterIdx: ref(null),
          selRun: computed(() => run.value.nodeRuns.a1 || null),
          selStatus: computed(() => 'running'),
          viewingLatest: computed(() => true),
          nodeTab: ref('log'),
          hasLog: computed(() => true),
        })
        return () => null
      },
    })
    app.use(i18n)
    app.use(router)
    app.mount(document.createElement('div'))

    await live.rehydrateNodeEvents('a1')
    expect(live.logEvents.value).toEqual([])
    expect(live.selRehydrateStatus.value).toBe('error')

    app.unmount()
  })

  it('unavailable with events still rehydrates as ready (snapshot soft-degrade)', async () => {
    vi.mocked(api.nodeEvents).mockResolvedValueOnce({
      events: [{ kind: 'message', text: 'cached', t: 1 }],
      live: true,
      unavailable: true,
      error: 'live event log read failed',
    })

    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
      cb(0)
      return 1
    })

    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/', component: { template: '<div />' } }],
    })
    await router.push('/')
    await router.isReady()

    const run = ref({
      id: 'run-1',
      status: 'running',
      nodeRuns: { a1: { nodeId: 'a1', status: 'running', events: [], mcpCalls: [] } },
      artifacts: [],
    } as unknown as Run)
    const selected = ref<string | null>('a1')

    let live!: ReturnType<typeof useRunDetailLiveLog>
    const app = createApp({
      setup() {
        live = useRunDetailLiveLog({
          runId: computed(() => 'run-1'),
          run,
          selected,
          selExecIdx: computed(() => 0),
          selIterIdx: ref(null),
          selRun: computed(() => run.value.nodeRuns.a1 || null),
          selStatus: computed(() => 'running'),
          viewingLatest: computed(() => true),
          nodeTab: ref('log'),
          hasLog: computed(() => true),
        })
        return () => null
      },
    })
    app.use(i18n)
    app.use(router)
    app.mount(document.createElement('div'))

    await live.rehydrateNodeEvents('a1')
    expect(live.logEvents.value.map((e) => ({ kind: e.kind, text: e.text, t: e.t }))).toEqual([
      { kind: 'message', text: 'cached', t: 1 },
    ])
    expect(live.selRehydrateStatus.value).toBe('ready')

    app.unmount()
  })
})
