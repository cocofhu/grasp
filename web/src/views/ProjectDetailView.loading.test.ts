// @vitest-environment happy-dom
import { defineComponent, h } from 'vue'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'

const apiMocks = vi.hoisted(() => ({
  getProject: vi.fn(),
  listWorkflows: vi.fn(),
  listAgents: vi.fn(),
  getPmLeader: vi.fn(),
  listProjectCronJobs: vi.fn(),
  listProjectChannels: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      getProject: apiMocks.getProject,
      listWorkflows: apiMocks.listWorkflows,
      listAgents: apiMocks.listAgents,
      getPmLeader: apiMocks.getPmLeader,
      listProjectCronJobs: apiMocks.listProjectCronJobs,
      listProjectChannels: apiMocks.listProjectChannels,
    },
  }
})

vi.mock('@/lib/composables/useBreakpoint', async () => {
  const { ref } = await import('vue')
  return { useBreakpoint: () => ({ isMobile: ref(false) }) }
})

vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({ success: vi.fn(), error: vi.fn() }),
}))

vi.mock('@/lib/composables/useProjectContext', () => ({
  writeStoredProjectId: vi.fn(),
}))

import ProjectDetailView from './ProjectDetailView.vue'

const vueSrc = readFileSync(join(dirname(fileURLToPath(import.meta.url)), 'ProjectDetailView.vue'), 'utf8')
const logicSrc = readFileSync(
  join(dirname(fileURLToPath(import.meta.url)), '../lib/project/useProjectDetail.ts'),
  'utf8',
)
const src = `${vueSrc}\n${logicSrc}`

const PROJ_A = {
  id: 'proj-a',
  name: 'Project A',
  description: 'A',
  variables: [],
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
}
const PROJ_B = { ...PROJ_A, id: 'proj-b', name: 'Project B', description: 'B' }

describe('ProjectDetailView loading source lock', () => {
  it('keeps the original token card in the top-aligned back row', () => {
    const headerTestId = vueSrc.indexOf('data-testid="project-detail-header-row"')
    const headerStart = vueSrc.lastIndexOf('<div', headerTestId)
    const titleStart = vueSrc.indexOf('data-testid="project-detail-title-row"')
    const header = vueSrc.slice(headerStart, titleStart)
    const tokenTestId = header.indexOf('data-testid="project-token-stat"')
    const tokenStart = header.lastIndexOf('<div', tokenTestId)
    const tokenEnd = header.indexOf('</div>', tokenStart)
    const token = header.slice(tokenStart, tokenEnd)

    expect(headerStart).toBeGreaterThan(0)
    expect(header).toMatch(/items-start justify-between/)
    expect(header).toContain('data-testid="project-token-stat"')
    expect(token).toMatch(/min-w-\[132px\]/)
    expect(token).toMatch(/shrink-0/)
    expect(token).toMatch(/px-3 py-2/)
    expect(header).toMatch(/text-\[11px\]/)
    expect(header).toMatch(/text-\[22px\]/)
    expect(header).toMatch(/aria-describedby/)
    expect(header).toContain('<TokenUsageHoverTip')
    expect(token).not.toMatch(/-m[trblxy]?-\d|translate-/)
    expect(vueSrc.slice(titleStart, vueSrc.indexOf('</template>', titleStart))).not.toContain(
      'project-token-stat',
    )
  })

  it('first load shows title + tabs + content skeleton together', () => {
    expect(src).toMatch(/data-testid="project-detail-title-skeleton"/)
    expect(src).toMatch(/data-testid="project-detail-content-skeleton"/)
    expect(src).toMatch(/data-testid="project-detail-tabs"/)
    expect(src).toMatch(/initialLoading \|\| project/)
    expect(src).not.toMatch(/v-if="loading" class="h-8 w-48 animate-pulse rounded bg-elevated"/)
  })

  it('initialLoading skeleton precedes real board/cron panels (v-else-if chain)', () => {
    const skelIdx = src.indexOf('data-testid="project-detail-content-skeleton"')
    const boardIdx = src.indexOf('data-testid="project-board-panel"')
    const cronIdx = src.indexOf('tab === \'cronJobs\'')
    expect(skelIdx).toBeGreaterThan(0)
    expect(boardIdx).toBeGreaterThan(skelIdx)
    expect(cronIdx).toBeGreaterThan(skelIdx)
    expect(src).toMatch(/v-else-if="tab === 'board'"/)
    expect(src).toMatch(/v-else-if="tab === 'cronJobs'"/)
  })

  it('uses requestSeq on watch\(projectId\) and reloadWorkflows', () => {
    expect(src).toMatch(/createListRequestSeq/)
    expect(src).toMatch(/projectSeq\.beginListRequest/)
    expect(src).toMatch(/workflowSeq\.beginListRequest/)
    expect(src).toMatch(/watch\(projectId/)
  })

  it('fail/403 are independent of notFound', () => {
    expect(src).toMatch(/data-testid="project-detail-failed"/)
    expect(src).toMatch(/data-testid="project-detail-denied"/)
    expect(src).toMatch(/loadFailed\.value = true/)
    expect(src).toMatch(/status === 404/)
  })

  it('copy pending uses copying not loading; save buttons use saving', () => {
    expect(src).toMatch(/common\.buttons\.copying/)
    expect(src).not.toMatch(/copyPreviewLoading === w\.id \? t\('common\.buttons\.loading'\)/)
    expect(src).toMatch(/savingMeta \? t\('common\.buttons\.saving'\)/)
    expect(src).toMatch(/savingVars \? t\('common\.buttons\.saving'\)/)
    expect(src).not.toMatch(/savingEnv/)
    expect(src).toMatch(/h-\[2px\].*bg-line/)
    expect(src).toMatch(/admin-list-thin-bar bg-accent/)
    expect(src).not.toMatch(/#7B61FF/)
  })
})

describe('ProjectDetailView project switch race', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.listAgents.mockResolvedValue([])
    apiMocks.getPmLeader.mockResolvedValue({ enabled: false })
    apiMocks.listProjectCronJobs.mockResolvedValue({ items: [] })
    apiMocks.listProjectChannels.mockResolvedValue({ items: [], freeAgents: [], secretsKeyConfigured: true })
  })

  async function mountDetail(id: string, query = '?tab=workflows') {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/projects/:id', component: ProjectDetailView },
        { path: '/projects', component: { template: '<div />' } },
      ],
    })
    await router.push(`/projects/${id}${query}`)
    await router.isReady()
    const wrapper = mount(
      defineComponent({
        setup() {
          return () => h(RouterView)
        },
      }),
      {
        global: {
          plugins: [i18n, router],
          stubs: {
            Icon: true,
            AppButton: { template: '<button type="button" v-bind="$attrs"><slot /></button>' },
            AppModal: true,
            AppSwitch: true,
            EmptyState: true,
            StatusPill: true,
            BoardView: true,
            PmLeaderChat: true,
            PmCronJobsPanel: true,
            PmSettingsPanel: true,
            ProjectAuditPanel: true,
            ProjectNotifyPanel: true,
            OnboardingWizard: true,
            RunLaunchModal: true,
            CopyWorkflowModal: true,
            ExportVersionModal: true,
            TokenUsageHoverTip: true,
          },
        },
      },
    )
    return { wrapper, router }
  }

  it('default board tab first load shows content skeleton without mounting BoardView', async () => {
    let release!: (v: unknown) => void
    apiMocks.getProject.mockReturnValue(new Promise((resolve) => { release = resolve }))
    apiMocks.listWorkflows.mockResolvedValue([])
    const { wrapper } = await mountDetail('proj-a', '')
    await flushPromises()
    expect(wrapper.find('[data-testid="project-detail-content-skeleton"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="project-board-panel"]').exists()).toBe(false)
    release!(PROJ_A)
    await flushPromises()
    expect(wrapper.find('[data-testid="project-detail-content-skeleton"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="project-board-panel"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('shows title+tabs+list skeleton on first load', async () => {
    let release!: (v: unknown) => void
    apiMocks.getProject.mockReturnValue(new Promise((resolve) => { release = resolve }))
    apiMocks.listWorkflows.mockResolvedValue([])
    const { wrapper } = await mountDetail('proj-a')
    await flushPromises()
    expect(wrapper.find('[data-testid="project-detail-title-skeleton"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="project-detail-tabs"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="project-detail-content-skeleton"]').exists()).toBe(true)
    release!(PROJ_A)
    await flushPromises()
    expect(wrapper.text()).toContain('Project A')
    expect(wrapper.find('[data-testid="project-detail-title-skeleton"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('fast project switch keeps last project only', async () => {
    let resolveA!: (v: unknown) => void
    apiMocks.getProject.mockImplementation(async (id: string) => {
      if (id === 'proj-a') {
        return new Promise((resolve) => { resolveA = resolve })
      }
      return PROJ_B
    })
    apiMocks.listWorkflows.mockResolvedValue([])
    const { wrapper, router } = await mountDetail('proj-a')
    await flushPromises()
    await router.push('/projects/proj-b?tab=workflows')
    await flushPromises()
    expect(wrapper.text()).toContain('Project B')
    resolveA!(PROJ_A)
    await flushPromises()
    expect(wrapper.text()).toContain('Project B')
    expect(wrapper.text()).not.toContain('Project A')
    wrapper.unmount()
  })

  it('load failure is not notFound', async () => {
    apiMocks.getProject.mockRejectedValue(Object.assign(new Error('down'), { status: 500 }))
    apiMocks.listWorkflows.mockRejectedValue(Object.assign(new Error('down'), { status: 500 }))
    const { wrapper } = await mountDetail('proj-a')
    await flushPromises()
    expect(wrapper.find('[data-testid="project-detail-failed"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('加载失败')
    expect(wrapper.text()).not.toMatch(/未找到|not found/i)
    wrapper.unmount()
  })
})
