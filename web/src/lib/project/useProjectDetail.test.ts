// @vitest-environment happy-dom
import { createApp, defineComponent, nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'

const mocks = vi.hoisted(() => ({
  getProject: vi.fn(),
  listWorkflows: vi.fn(),
  listAgents: vi.fn(),
  getPmLeader: vi.fn(),
  writeStoredProjectId: vi.fn(),
  patchWorkflowHomeVisibility: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      getProject: mocks.getProject,
      listWorkflows: mocks.listWorkflows,
      listAgents: mocks.listAgents,
      getPmLeader: mocks.getPmLeader,
      patchWorkflowHomeVisibility: mocks.patchWorkflowHomeVisibility,
    },
  }
})

vi.mock('@/lib/composables/useBreakpoint', async () => {
  const { ref } = await import('vue')
  return { useBreakpoint: () => ({ isMobile: ref(false) }) }
})

vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({ success: vi.fn(), error: vi.fn(), warn: vi.fn(), info: vi.fn() }),
}))

vi.mock('@/lib/composables/useProjectContext', () => ({
  writeStoredProjectId: (...args: unknown[]) => mocks.writeStoredProjectId(...args),
}))

vi.mock('@/lib/run/runDraft', () => ({
  mergeRunDraft: async (_id: string, seed: Record<string, string>) => ({ inputs: seed, images: {}, restored: false }),
  saveRunDraft: vi.fn(async () => 'ok'),
  clearRunDraft: vi.fn(async () => {}),
}))

vi.mock('@/lib/run/useWorkflowImport', () => ({
  useWorkflowImport: () => ({
    fileInput: { value: null },
    triggerImport: vi.fn(),
    handleFileChange: vi.fn(),
  }),
}))

import { useProjectDetail } from './useProjectDetail'

const sampleProject = {
  id: 'proj-a',
  name: 'Project A',
  description: 'desc',
  variables: [],
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
}

const sampleWorkflows = [
  {
    id: 'wf-1',
    projectId: 'proj-a',
    name: 'WF',
    description: '',
    status: 'published' as const,
    version: 1,
    updatedAt: '',
    needsRepo: false,
    nodes: [],
    edges: [],
  },
]

async function withProjectDetail(path = '/projects/proj-a') {
  let detail!: ReturnType<typeof useProjectDetail>
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { path: '/projects/:id', component: { template: '<div />' } },
    ],
  })
  await router.push(path)
  await router.isReady()
  const Comp = defineComponent({
    setup() {
      detail = useProjectDetail()
      return () => null
    },
  })
  const app = createApp(Comp)
  app.use(i18n)
  app.use(router)
  app.mount(document.createElement('div'))
  return { detail, app, router }
}

describe('useProjectDetail', () => {
  beforeEach(() => {
    mocks.getProject.mockReset()
    mocks.listWorkflows.mockReset()
    mocks.listAgents.mockReset()
    mocks.getPmLeader.mockReset()
    mocks.writeStoredProjectId.mockReset()
    mocks.patchWorkflowHomeVisibility.mockReset()

    mocks.getProject.mockResolvedValue(sampleProject)
    mocks.listWorkflows.mockResolvedValue(sampleWorkflows)
    mocks.listAgents.mockResolvedValue([{ name: 'agent-a', projectId: 'proj-a' }])
    mocks.getPmLeader.mockResolvedValue({ agentName: 'agent-a', channelId: '' })
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('loads project on mount and exposes tabs/helpers', async () => {
    const { detail, app } = await withProjectDetail()
    await flushPromises()
    await nextTick()

    expect(detail.projectId.value).toBe('proj-a')
    expect(detail.project.value?.id).toBe('proj-a')
    expect(detail.workflows.value.length).toBe(1)
    expect(detail.hasInitialLoaded.value).toBe(true)
    expect(detail.parseProjectTab('board')).toBe('board')
    expect(detail.parseProjectTab('pmSettings')).toBe('board')

    detail.setTab('workflows')
    expect(detail.tab.value).toBe('workflows')
    detail.closeMenu()
    detail.toggleWorkflowFavorite(sampleWorkflows[0] as any)
    detail.parseProjectTab('audit')
    detail.parseProjectTab('cron')
    detail.setTab('board')
    detail.setTab('pmLeader')
    detail.toggleMenu('wf-1')
    detail.menuIdFor('wf-1')
    detail.fmtTime('2026-01-01T00:00:00Z')
    detail.fmtCompactTokenCount(1200)
    detail.addVarRow()
    const row = detail.project.value?.variables?.[0]
    if (row) {
      expect(typeof detail.isBoolTrue(row)).toBe('boolean')
      detail.setBoolValue(row, true)
    }
    await detail.reloadWorkflows()
    detail.onNotifyProjectUpdated(sampleProject as any)
    detail.openNotifyChannelSettings()
    detail.closeRunModal()
    detail.closeCopyModal()

    document.dispatchEvent(new Event('click'))
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    window.dispatchEvent(new Event('scroll'))

    app.unmount()
  })

  it('PATCHes home visibility without opening the editor and rolls back on failure (g2.1 / g2.3)', async () => {
    mocks.patchWorkflowHomeVisibility.mockResolvedValueOnce({
      ...sampleWorkflows[0],
      showOnHome: true,
    })
    const { detail, app } = await withProjectDetail()
    await flushPromises()
    await nextTick()
    const wf = detail.workflows.value[0]
    expect(wf.showOnHome).toBeFalsy()

    await detail.toggleWorkflowShowOnHome(wf as any, true)
    await flushPromises()
    expect(mocks.patchWorkflowHomeVisibility).toHaveBeenCalledWith('wf-1', true)
    expect(detail.workflows.value[0].showOnHome).toBe(true)

    mocks.patchWorkflowHomeVisibility.mockRejectedValueOnce(new Error('network down'))
    await detail.toggleWorkflowShowOnHome(detail.workflows.value[0] as any, false)
    await flushPromises()
    expect(detail.workflows.value[0].showOnHome).toBe(true)

    app.unmount()
  })
})
