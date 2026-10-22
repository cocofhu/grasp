// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { AuditEvent } from '@/lib/shared/types'
import WorkspaceAuditPanel from './WorkspaceAuditPanel.vue'

const apiMocks = vi.hoisted(() => ({
  listAudit: vi.fn(),
  listAuditFacets: vi.fn(),
  exportAuditUrl: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      listAudit: apiMocks.listAudit,
      listAuditFacets: apiMocks.listAuditFacets,
      exportAuditUrl: apiMocks.exportAuditUrl,
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

const SAMPLE: AuditEvent = {
  id: 'e1',
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
  return mount(WorkspaceAuditPanel, {
    props,
    global: {
      plugins: [i18n],
      stubs: {
        EmptyState: { props: ['title'], template: '<div data-testid="audit-empty">{{ title }}</div>' },
        Pagination: { template: '<div data-testid="audit-pagination" />' },
      },
    },
  })
}

describe('WorkspaceAuditPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    breakpoint.isMobile.value = false
    apiMocks.exportAuditUrl.mockReturnValue('http://example.test/export')
    apiMocks.listAuditFacets.mockResolvedValue({
      runs: [{ runId: 'run-1', label: 'Run 1', sub: 'ok' }],
      nodes: [{ nodeId: 'n1', label: 'Node 1' }],
      resources: [{ resourceType: 'mcp', resourceId: 'tool', resource: 'mcp/tool' }],
    })
    apiMocks.listAudit.mockResolvedValue({
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
    expect(w.get('[data-testid="workspace-audit-panel"]').exists()).toBe(true)
    expect(apiMocks.listAuditFacets).toHaveBeenCalled()
    expect(apiMocks.listAudit).toHaveBeenCalled()
    expect(w.text()).toContain('called tool')

    await w.get('[data-testid="workspace-audit-event-e1"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="workspace-audit-payload"]').exists()).toBe(true)

    await w.get('[data-testid="workspace-audit-export"]').trigger('click')
    await flushPromises()
    expect(apiMocks.exportAuditUrl).toHaveBeenCalled()
    expect(toast.success).toHaveBeenCalled()

    await w.get('[data-testid="workspace-audit-mode-all"]').trigger('click')
    await flushPromises()
    expect(apiMocks.listAudit).toHaveBeenCalled()

    const search = w.findAll('[data-testid="workspace-audit-search"]').at(0)
    await search!.setValue('tool')
    await search!.trigger('input')
    await flushPromises()
    w.unmount()
  })

  it('shows denied state without fetching when forceDenied', async () => {
    const w = mountPanel({ forceDenied: true })
    await flushPromises()
    expect(w.find('[data-testid="workspace-audit-denied"]').exists()).toBe(true)
    expect(apiMocks.listAudit).not.toHaveBeenCalled()
    w.unmount()
  })

  it('marks denied after 403 from facets', async () => {
    apiMocks.listAuditFacets.mockRejectedValue(Object.assign(new Error('no'), { status: 403 }))
    const w = mountPanel()
    await flushPromises()
    expect(w.find('[data-testid="workspace-audit-denied"]').exists()).toBe(true)
    w.unmount()
  })

  it('renders empty run state when there are no events', async () => {
    apiMocks.listAuditFacets.mockResolvedValue({
      runs: [],
      nodes: [],
      resources: [],
    })
    apiMocks.listAudit.mockResolvedValue({
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
      w.find('[data-testid="workspace-audit-empty-runs"]').exists()
        || w.find('[data-testid="audit-empty"]').exists()
        || w.find('[data-testid="workspace-audit-empty"]').exists(),
    ).toBe(true)
    w.unmount()
  })

  it('hides the annotated intro and keeps mode, search, and export', async () => {
    const { readFileSync } = await import('node:fs')
    const { dirname, join } = await import('node:path')
    const { fileURLToPath } = await import('node:url')
    const src = readFileSync(join(dirname(fileURLToPath(import.meta.url)), 'WorkspaceAuditPanel.vue'), 'utf8')
    expect(src).not.toContain('pages.workspace.audit.description')
    expect(src).toContain('pages.workspace.audit.emptyTitle')
    const w = mountPanel()
    await flushPromises()
    expect(w.get('[data-testid="workspace-audit-mode-run"]').text()).toContain('按 Run 查看')
    expect(w.get('[data-testid="workspace-audit-mode-all"]').text()).toContain('全部日志')
    expect(w.get('[data-testid="workspace-audit-search"]').exists()).toBe(true)
    expect(w.get('[data-testid="workspace-audit-export"]').text()).toContain('导出')
    expect(w.text()).not.toContain('查看工作流与系统操作，追踪工具调用和执行结果。')
    expect(w.text()).not.toContain('Trace workflow and system actions, tool calls and execution results.')
    w.unmount()
  })
})
