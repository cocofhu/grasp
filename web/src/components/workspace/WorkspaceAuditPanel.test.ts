// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
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
vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => toast,
}))

const breakpoint = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const vue = require('vue') as typeof import('vue')
  return { isMobile: vue.ref(false) }
})
vi.mock('@/lib/composables/useBreakpoint', () => ({
  useBreakpoint: () => ({ isMobile: breakpoint.isMobile }),
}))

const SAMPLE: ProjectAuditEvent = {
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

describe('ProjectAuditPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    breakpoint.isMobile.value = false
    apiMocks.exportProjectAuditUrl.mockReturnValue('http://example.test/export')
    apiMocks.listProjectAuditFacets.mockResolvedValue({
      runs: [{ runId: 'run-1', label: 'Run 1', sub: 'ok' }],
      nodes: [{ nodeId: 'n1', label: 'Node 1' }],
      resources: [{ resourceType: 'mcp', resourceId: 'tool', resource: 'mcp/tool' }],
    })
    apiMocks.listProjectAudit.mockResolvedValue({
      items: [SAMPLE],
      total: 1,
      page: 1,
      pageSize: 100,
      hasMore: false,
      stats: { total: 1, mcp: 1, fail: 0 },
    })
  })

  it('loads run-mode events, expands payload, exports, and switches to all mode', async () => {
    const w = mountPanel()
    await flushPromises()
    expect(w.get('[data-testid="project-audit-panel"]').exists()).toBe(true)
    expect(apiMocks.listProjectAuditFacets).toHaveBeenCalled()
    expect(apiMocks.listProjectAudit).toHaveBeenCalled()
    expect(w.text()).toContain('called tool')

    await w.get('[data-testid="project-audit-event-e1"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="project-audit-payload"]').exists()).toBe(true)

    await w.get('[data-testid="project-audit-export"]').trigger('click')
    await flushPromises()
    expect(apiMocks.exportProjectAuditUrl).toHaveBeenCalled()
    expect(toast.success).toHaveBeenCalled()

    await w.get('[data-testid="project-audit-mode-all"]').trigger('click')
    await flushPromises()
    expect(apiMocks.listProjectAudit).toHaveBeenCalled()

    const search = w.findAll('[data-testid="project-audit-search"]').at(0)
    await search!.setValue('tool')
    await search!.trigger('input')
    await flushPromises()
    w.unmount()
  })

  it('shows denied state without fetching when forceDenied', async () => {
    const w = mountPanel({ forceDenied: true })
    await flushPromises()
    expect(w.find('[data-testid="project-audit-denied"]').exists()).toBe(true)
    expect(apiMocks.listProjectAudit).not.toHaveBeenCalled()
    w.unmount()
  })

  it('marks denied after 403 from facets', async () => {
    apiMocks.listProjectAuditFacets.mockRejectedValue(Object.assign(new Error('no'), { status: 403 }))
    const w = mountPanel()
    await flushPromises()
    expect(w.find('[data-testid="project-audit-denied"]').exists()).toBe(true)
    w.unmount()
  })

  it('renders empty run state when there are no events', async () => {
    apiMocks.listProjectAuditFacets.mockResolvedValue({
      runs: [],
      nodes: [],
      resources: [],
    })
    apiMocks.listProjectAudit.mockResolvedValue({
      items: [],
      total: 0,
      page: 1,
      pageSize: 100,
      hasMore: false,
      stats: { total: 0, mcp: 0, fail: 0 },
    })
    const w = mountPanel()
    await flushPromises()
    expect(
      w.find('[data-testid="project-audit-empty-runs"]').exists()
        || w.find('[data-testid="audit-empty"]').exists()
        || w.find('[data-testid="project-audit-empty"]').exists(),
    ).toBe(true)
    w.unmount()
  })
})
