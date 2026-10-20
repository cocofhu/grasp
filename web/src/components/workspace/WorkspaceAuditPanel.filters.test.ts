// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { ProjectAuditEvent } from '@/lib/shared/types'
import ProjectAuditPanel from './ProjectAuditPanel.vue'

const apiMocks = vi.hoisted(() => ({
  listProjectAudit: vi.fn(),
  listProjectAuditFacets: vi.fn(),
  exportProjectAuditUrl: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      listProjectAudit: apiMocks.listProjectAudit,
      listProjectAuditFacets: apiMocks.listProjectAuditFacets,
      exportProjectAuditUrl: apiMocks.exportProjectAuditUrl,
    },
  }
})

const toast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn(), warn: vi.fn(), info: vi.fn() }))
vi.mock('@/lib/composables/useToast', () => ({ useToast: () => toast }))

const breakpoint = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const vue = require('vue') as typeof import('vue')
  return { isMobile: vue.ref(false) }
})
vi.mock('@/lib/composables/useBreakpoint', () => ({
  useBreakpoint: () => ({ isMobile: breakpoint.isMobile }),
}))

function ev(over: Partial<ProjectAuditEvent> = {}): ProjectAuditEvent {
  return {
    id: 'e1',
    projectId: 'proj-1',
    occurredAt: '2026-01-01T00:00:00Z',
    actor: 'pm',
    unattributable: false,
    callerKind: 'pm',
    action: 'mcp.call',
    resourceType: 'mcp',
    resourceId: 'tool',
    resource: 'mcp/tool',
    runId: 'run-1',
    nodeId: 'n1',
    outcome: 'ok',
    summary: 'called tool',
    payload: { foo: 'bar' },
    ...over,
  }
}

function paged(items: ProjectAuditEvent[], over: Record<string, unknown> = {}) {
  return {
    items,
    total: items.length,
    page: 1,
    pageSize: 100,
    hasMore: false,
    stats: {
      total: items.length,
      mcp: items.filter((e) => e.action.startsWith('mcp')).length,
      fail: items.filter((e) => e.outcome === 'fail').length,
    },
    ...over,
  }
}

function mountPanel(props: Record<string, unknown> = {}) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(ProjectAuditPanel, {
    props: { projectId: 'proj-1', ...props },
    global: {
      plugins: [i18n],
      stubs: {
        EmptyState: { props: ['title'], template: '<div data-testid="audit-empty">{{ title }}</div>' },
        Pagination: { template: '<div data-testid="audit-pagination" />' },
      },
    },
  })
}

describe('ProjectAuditPanel filters and grouping', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.useFakeTimers({ shouldAdvanceTime: true })
    breakpoint.isMobile.value = false
    apiMocks.exportProjectAuditUrl.mockReturnValue('http://example.test/export')
    apiMocks.listProjectAuditFacets.mockResolvedValue({
      runs: [
        { runId: 'run-1', label: 'Run 1', sub: 'ok' },
        { runId: 'run-2', label: 'Run 2', sub: 'fail' },
      ],
      nodes: [{ nodeId: 'n1', label: 'Node 1' }],
      resources: [
        { resourceType: 'mcp', resourceId: 'tool', resource: 'mcp/tool' },
        { resourceType: 'gate', resourceId: 'g1', resource: 'gate/g1' },
      ],
    })
    apiMocks.listProjectAudit.mockResolvedValue(paged([ev()]))
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('auto-selects the first run and derives chips from active filters', async () => {
    const w = mountPanel()
    await flushPromises()
    const vm = w.vm as any

    expect(vm.mode).toBe('run')
    expect(vm.runId).toBe('run-1')
    expect(vm.chips).toEqual([])

    vm.nodeId = 'n1'
    vm.resource = 'mcp/tool'
    vm.search = '  tool  '
    vm.timeWindow = '7d'
    await flushPromises()
    expect(vm.chips.map((c: any) => c.key)).toEqual(['node', 'resource', 'search', 'time'])
    expect(vm.chips.find((c: any) => c.key === 'search').value).toBe('tool')
    expect(vm.clearableChips).toHaveLength(4)

    vm.timeWindow = '30d'
    await flushPromises()
    expect(vm.chips.find((c: any) => c.key === 'time').value).toBeTruthy()
    w.unmount()
  })

  it('swaps the node chip for a caller chip in all mode', async () => {
    const w = mountPanel()
    await flushPromises()
    const vm = w.vm as any

    await vm.setMode('all')
    await flushPromises()
    expect(vm.mode).toBe('all')

    vm.callerKind = 'apikey'
    await flushPromises()
    expect(vm.chips.map((c: any) => c.key)).toEqual(['caller'])

    // An unknown caller kind falls back to the raw value.
    vm.callerKind = 'mystery'
    await flushPromises()
    expect(vm.chips[0].value).toBe('mystery')

    // Re-entering the same mode is a no-op.
    const calls = apiMocks.listProjectAudit.mock.calls.length
    await vm.setMode('all')
    expect(apiMocks.listProjectAudit).toHaveBeenCalledTimes(calls)
    w.unmount()
  })

  it('maps resource prefixes to dot classes and groups', async () => {
    const w = mountPanel()
    await flushPromises()
    const vm = w.vm as any

    expect(vm.resDot('')).toBe('')
    expect(vm.resDot('mcp')).toBe('mcp')
    expect(vm.resDot('mcp/tool')).toBe('mcp')
    expect(vm.resDot('run/1')).toBe('run')
    expect(vm.resDot('gate/1')).toBe('gate')
    expect(vm.resDot('workflow/1')).toBe('wf')
    expect(vm.resDot('project/1')).toBe('prj')
    expect(vm.resDot('audit/1')).toBe('aud')
    expect(vm.resDot('other/1')).toBe('')

    expect(vm.resGroup({ value: '' })).toBeTruthy()
    expect(vm.resGroup({ value: 'mcp/tool' })).toBe('mcp')
    expect(vm.resGroup({ value: 'plain' })).toBe('other')
    w.unmount()
  })

  it('formats run ids, caller labels, node labels and resource aux lines', async () => {
    const w = mountPanel()
    await flushPromises()
    const vm = w.vm as any

    expect(vm.shortRun('run-abcdefghij')).toBe('abcdefgh')
    expect(vm.shortRun('run-abc')).toBe('abc')

    expect(vm.callerLabel(ev({ callerKind: 'apikey' }))).toBeTruthy()
    expect(vm.callerLabel(ev({ callerKind: '', unattributable: true }))).toBeTruthy()
    expect(vm.callerLabel(ev({ callerKind: '', unattributable: false, actor: 'system' }))).toBeTruthy()
    expect(vm.callerLabel(ev({ callerKind: 'weird' }))).toBe('weird')

    expect(vm.nodeLabel()).toBeTruthy()
    expect(vm.nodeLabel('n1')).toBeTruthy()

    expect(vm.resourceAux(ev())).toBe('mcp / tool')
    expect(vm.resourceAux(ev({ resourceType: '', resourceId: '', resource: 'gate/g1' }))).toBe('gate / g1')
    expect(vm.resourceAux(ev({ resourceType: '', resourceId: '', resource: '' }))).toBe('')
    expect(vm.resourceAux(ev({ resourceType: '', resourceId: '', resource: '—' }))).toBe('')
    expect(vm.resourceAux(ev({ resourceType: '', resourceId: '', resource: 'plain' }))).toBe('')

    expect(vm.resourceText(ev())).toBe('mcp/tool')
    expect(vm.resourceText(ev({ resource: '', resourceType: 'a', resourceId: 'b' }))).toBe('a/b')
    expect(vm.resourceText(ev({ resource: '', resourceType: '', resourceId: '' }))).toBe('—')

    expect(vm.outcomeLabel(ev({ outcome: 'fail' }))).not.toBe(vm.outcomeLabel(ev({ outcome: 'ok' })))
    expect(vm.prettyPayload({ a: 1 })).toBeTruthy()
    w.unmount()
  })

  it('groups run events by node and defaults expansion to failing groups', async () => {
    apiMocks.listProjectAudit.mockResolvedValue(
      paged([
        ev({ id: 'a', nodeId: 'n1', occurredAt: '2026-01-01T00:00:02Z' }),
        ev({ id: 'b', nodeId: 'n1', occurredAt: '2026-01-01T00:00:01Z' }),
        ev({ id: 'c', nodeId: 'n2', outcome: 'fail', occurredAt: '2026-01-01T00:00:03Z' }),
        ev({ id: 'd', nodeId: '', occurredAt: '2026-01-01T00:00:04Z' }),
      ]),
    )
    const w = mountPanel()
    await flushPromises()
    const vm = w.vm as any

    const groups = vm.runGroups
    expect(groups.map((g: any) => g.id)).toEqual(['n1', 'n2', '_system'])
    expect(groups[0].events.map((e: any) => e.id)).toEqual(['b', 'a'])
    expect(groups[1].fail).toBe(1)
    expect(groups[2].fullId).toBe('')

    // With a failure present only the failing group is open by default.
    expect(vm.isGroupOpen(groups[0])).toBe(false)
    expect(vm.isGroupOpen(groups[1])).toBe(true)

    vm.toggleGroup('n1')
    await flushPromises()
    expect(vm.isGroupOpen(vm.runGroups[0])).toBe(true)

    // Toggling an unknown group id is ignored.
    vm.toggleGroup('nope')
    expect(Object.keys(vm.groupOpen)).toEqual(['n1'])

    expect(vm.statOk).toBe(3)
    expect(vm.listFailCount).toBe(1)
    w.unmount()
  })

  it('opens every group when nothing failed or only one group exists', async () => {
    apiMocks.listProjectAudit.mockResolvedValue(
      paged([ev({ id: 'a', nodeId: 'n1' }), ev({ id: 'b', nodeId: 'n2' })]),
    )
    const w = mountPanel()
    await flushPromises()
    const vm = w.vm as any

    expect(vm.runGroups.every((g: any) => vm.isGroupOpen(g))).toBe(true)
    w.unmount()
  })

  it('prunes stale group overrides when the event set changes', async () => {
    apiMocks.listProjectAudit.mockResolvedValue(
      paged([ev({ id: 'a', nodeId: 'n1', outcome: 'fail' }), ev({ id: 'b', nodeId: 'n2' })]),
    )
    const w = mountPanel()
    await flushPromises()
    const vm = w.vm as any

    vm.toggleGroup('n1')
    vm.toggleGroup('n2')
    await flushPromises()
    expect(Object.keys(vm.groupOpen).sort()).toEqual(['n1', 'n2'])

    apiMocks.listProjectAudit.mockResolvedValue(paged([ev({ id: 'a', nodeId: 'n1', outcome: 'fail' })]))
    await vm.load(true)
    await flushPromises()
    expect(Object.keys(vm.groupOpen)).toEqual(['n1'])
    w.unmount()
  })

  it('pages through run-aligned results until hasMore clears', async () => {
    const page1 = paged([ev({ id: 'p1' })], { total: 2, hasMore: true, stats: undefined })
    const page2 = paged([ev({ id: 'p2' })], { total: 2, hasMore: false, stats: undefined })
    apiMocks.listProjectAudit.mockResolvedValueOnce(page1).mockResolvedValueOnce(page2)

    const w = mountPanel()
    await flushPromises()
    const vm = w.vm as any

    expect(vm.events.map((e: any) => e.id)).toEqual(['p1', 'p2'])
    expect(vm.runCapped).toBe(false)
    expect(vm.runFetched).toBe(2)
    // Without server stats the panel derives them from the collected page.
    expect(vm.stats.mcp).toBe(2)
    w.unmount()
  })

  it('flags a capped run fetch when the server reports more than it returns', async () => {
    apiMocks.listProjectAudit.mockResolvedValue(paged([ev({ id: 'p1' })], { total: 900, hasMore: false }))
    const w = mountPanel()
    await flushPromises()
    expect((w.vm as any).runCapped).toBe(true)
    w.unmount()
  })

  it('clears the list when run mode has no selected run', async () => {
    apiMocks.listProjectAuditFacets.mockResolvedValue({ runs: [], nodes: [], resources: [] })
    const w = mountPanel()
    await flushPromises()
    const vm = w.vm as any

    expect(vm.runId).toBe('')
    expect(vm.events).toEqual([])
    expect(vm.noRuns).toBe(true)
    expect(vm.loading).toBe(false)
    expect(apiMocks.listProjectAudit).not.toHaveBeenCalled()
    w.unmount()
  })

  it('picks the first run when switching back into run mode', async () => {
    const w = mountPanel()
    await flushPromises()
    const vm = w.vm as any

    await vm.setMode('all')
    await flushPromises()
    vm.runId = ''
    await vm.setMode('run')
    await flushPromises()
    expect(vm.runId).toBe('run-1')
    w.unmount()
  })

  it('reloads facets and events when the run selection changes', async () => {
    const w = mountPanel()
    await flushPromises()
    const vm = w.vm as any
    vm.nodeId = 'n1'
    vm.resource = 'mcp/tool'
    vm.openId = 'e1'

    await vm.onRunChange('run-2')
    await flushPromises()
    expect(vm.runId).toBe('run-2')
    expect(vm.nodeId).toBe('')
    expect(vm.resource).toBe('')
    expect(vm.openId).toBeNull()
    w.unmount()
  })

  it('falls back to another run when the time window drops the current one', async () => {
    const w = mountPanel()
    await flushPromises()
    const vm = w.vm as any
    expect(vm.runId).toBe('run-1')

    apiMocks.listProjectAuditFacets.mockResolvedValue({
      runs: [{ runId: 'run-9', label: 'Run 9' }],
      nodes: [],
      resources: [],
    })
    await vm.onTimeChange('7d')
    await flushPromises()
    expect(vm.timeWindow).toBe('7d')
    expect(vm.runId).toBe('run-9')

    // When the window empties entirely the selection is dropped.
    apiMocks.listProjectAuditFacets.mockResolvedValue({ runs: [], nodes: [], resources: [] })
    await vm.onTimeChange('30d')
    await flushPromises()
    expect(vm.runId).toBe('')
    w.unmount()
  })

  it('debounces the search box and clears individual chips', async () => {
    const w = mountPanel()
    await flushPromises()
    const vm = w.vm as any
    const before = apiMocks.listProjectAudit.mock.calls.length

    vm.search = 'to'
    vm.onSearchInput()
    vm.search = 'tool'
    vm.onSearchInput()
    expect(apiMocks.listProjectAudit).toHaveBeenCalledTimes(before)

    await vi.advanceTimersByTimeAsync(250)
    await flushPromises()
    expect(apiMocks.listProjectAudit.mock.calls.length).toBeGreaterThan(before)

    vm.nodeId = 'n1'
    vm.clearChip('node')
    await flushPromises()
    expect(vm.nodeId).toBe('')

    vm.callerKind = 'pm'
    vm.clearChip('caller')
    expect(vm.callerKind).toBe('')

    vm.resource = 'mcp/tool'
    vm.clearChip('resource')
    expect(vm.resource).toBe('')

    vm.clearChip('search')
    expect(vm.search).toBe('')

    vm.timeWindow = '7d'
    vm.clearChip('time')
    await flushPromises()
    expect(vm.timeWindow).toBe('24h')
    w.unmount()
  })

  it('clears every optional filter at once', async () => {
    const w = mountPanel()
    await flushPromises()
    const vm = w.vm as any

    vm.nodeId = 'n1'
    vm.callerKind = 'pm'
    vm.resource = 'mcp/tool'
    vm.search = 'x'
    vm.timeWindow = '30d'
    vm.clearOptional()
    await flushPromises()

    expect(vm.chips).toEqual([])
    expect(vm.runId).toBe('run-1')
    w.unmount()
  })

  it('toggles a single expanded row at a time', async () => {
    const w = mountPanel()
    await flushPromises()
    const vm = w.vm as any

    vm.toggleOpen('e1')
    expect(vm.openId).toBe('e1')
    vm.toggleOpen('e1')
    expect(vm.openId).toBeNull()
    vm.toggleOpen('e2')
    expect(vm.openId).toBe('e2')

    vm.onFilterChange()
    await flushPromises()
    expect(vm.openId).toBeNull()
    w.unmount()
  })

  it('drives pagination and page-size changes in all mode', async () => {
    const w = mountPanel()
    await flushPromises()
    const vm = w.vm as any
    await vm.setMode('all')
    await flushPromises()

    apiMocks.listProjectAudit.mockResolvedValue(paged([ev()], { page: 3 }))
    vm.onPageChange(3)
    await flushPromises()
    expect(vm.page).toBe(3)

    vm.onPageSizeChange(20)
    await flushPromises()
    expect(vm.pageSize).toBe(20)

    // A falsy size falls back to the default.
    vm.onPageSizeChange(0)
    await flushPromises()
    expect(vm.pageSize).toBe(10)
    w.unmount()
  })

  it('reports a load failure through a toast', async () => {
    const w = mountPanel()
    await flushPromises()
    const vm = w.vm as any

    apiMocks.listProjectAudit.mockRejectedValueOnce(new Error('boom'))
    await vm.load(true)
    await flushPromises()
    expect(toast.error).toHaveBeenCalledWith('boom')
    expect(vm.loading).toBe(false)
    w.unmount()
  })

  it('marks denied when the event list returns 403', async () => {
    const w = mountPanel()
    await flushPromises()
    const vm = w.vm as any

    apiMocks.listProjectAudit.mockRejectedValueOnce(Object.assign(new Error('no'), { status: 403 }))
    await vm.load(true)
    await flushPromises()
    expect(vm.denied).toBe(true)
    expect(vm.events).toEqual([])
    expect(w.find('[data-testid="project-audit-denied"]').exists()).toBe(true)
    w.unmount()
  })

  it('swallows non-403 facet failures and keeps the panel usable', async () => {
    apiMocks.listProjectAuditFacets.mockRejectedValue(new Error('flaky'))
    const w = mountPanel()
    await flushPromises()
    const vm = w.vm as any
    expect(vm.denied).toBe(false)
    expect(vm.runOptions).toEqual([])
    w.unmount()
  })

  it('skips the facet call entirely once denied', async () => {
    const w = mountPanel({ forceDenied: true })
    await flushPromises()
    const vm = w.vm as any

    apiMocks.listProjectAuditFacets.mockClear()
    await vm.loadFacets()
    expect(apiMocks.listProjectAuditFacets).not.toHaveBeenCalled()
    expect(vm.runOptions).toEqual([])

    // Export is refused rather than producing a download link.
    await vm.exportAudit()
    expect(toast.error).toHaveBeenCalled()
    expect(apiMocks.exportProjectAuditUrl).not.toHaveBeenCalled()
    w.unmount()
  })

  it('builds an export link with the active run filters', async () => {
    const w = mountPanel()
    await flushPromises()
    const vm = w.vm as any
    vm.nodeId = 'n1'
    vm.resource = 'mcp/tool'
    vm.search = ' tool '

    await vm.exportAudit()
    await flushPromises()
    expect(apiMocks.exportProjectAuditUrl).toHaveBeenCalledWith('proj-1', {
      format: 'json',
      time: '24h',
      callerKind: undefined,
      resource: 'mcp/tool',
      runId: 'run-1',
      nodeId: 'n1',
      search: 'tool',
    })
    expect(toast.success).toHaveBeenCalled()

    // The deferred refresh fires after the download settles.
    const before = apiMocks.listProjectAudit.mock.calls.length
    await vi.advanceTimersByTimeAsync(500)
    await flushPromises()
    expect(apiMocks.listProjectAudit.mock.calls.length).toBeGreaterThan(before)
    w.unmount()
  })

  it('reports an export failure through a toast', async () => {
    const w = mountPanel()
    await flushPromises()
    apiMocks.exportProjectAuditUrl.mockImplementationOnce(() => {
      throw new Error('no url')
    })
    await (w.vm as any).exportAudit()
    expect(toast.error).toHaveBeenCalledWith('no url')
    w.unmount()
  })

  it('re-bootstraps when the project changes', async () => {
    const w = mountPanel()
    await flushPromises()
    apiMocks.listProjectAuditFacets.mockClear()

    await w.setProps({ projectId: 'proj-2' })
    await flushPromises()
    expect(apiMocks.listProjectAuditFacets).toHaveBeenCalled()
    expect(apiMocks.listProjectAuditFacets.mock.calls[0][0]).toBe('proj-2')
    w.unmount()
  })

  it('renders the mobile filter summary and toggles the editor', async () => {
    breakpoint.isMobile.value = true
    const w = mountPanel()
    await flushPromises()
    const vm = w.vm as any

    expect(w.find('[data-testid="project-audit-filter-summary"]').exists()).toBe(true)
    expect(vm.filtersExpanded).toBe(false)
    expect(vm.filterSummaryText).toContain('Run')

    await w.get('[data-testid="project-audit-filter-summary"]').trigger('click')
    expect(vm.filtersExpanded).toBe(true)

    // All mode summarises the caller instead of the run.
    await vm.setMode('all')
    await flushPromises()
    expect(vm.filterSummaryText).not.toContain('Run ·')

    vm.callerKind = 'apikey'
    await flushPromises()
    expect(vm.filterSummaryText).toBeTruthy()
    w.unmount()
  })

  it('falls back to a shortened run id in the mobile summary', async () => {
    breakpoint.isMobile.value = true
    const w = mountPanel()
    await flushPromises()
    const vm = w.vm as any

    vm.runId = 'run-unknown-long-id'
    await flushPromises()
    expect(vm.filterSummaryText).toContain('unknown-')
    w.unmount()
  })
})
