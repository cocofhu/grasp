// @vitest-environment happy-dom
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import enCommon from '@/locales/en/common.json'
import type { Artifact, Workflow } from '@/lib/shared/types'

const apiMocks = vi.hoisted(() => ({
  listArtifacts: vi.fn(),
  listWorkflows: vi.fn(),
  getRun: vi.fn(),
}))

const filterState = vi.hoisted(() => ({
  pipelineSelected: null as { value: string } | null,
  projectSelected: null as { value: string } | null,
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      listArtifacts: apiMocks.listArtifacts,
      listWorkflows: apiMocks.listWorkflows,
      getRun: apiMocks.getRun,
    },
  }
})

vi.mock('@/lib/composables/useBreakpoint', async () => {
  const { ref } = await import('vue')
  return { useBreakpoint: () => ({ isMobile: ref(false) }) }
})

vi.mock('@/lib/composables/usePipelineFilter', async () => {
  const { ref } = await import('vue')
  filterState.pipelineSelected = ref('')
  return { usePipelineFilter: () => ({ selected: filterState.pipelineSelected! }) }
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

vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({ error: vi.fn(), success: vi.fn(), info: vi.fn(), warn: vi.fn() }),
}))

import ArtifactsView from './ArtifactsView.vue'

const src = readFileSync(join(dirname(fileURLToPath(import.meta.url)), 'ArtifactsView.vue'), 'utf8')

const sampleArtifact: Artifact = {
  id: 'art-1',
  name: 'notes.md',
  kind: 'markdown',
  nodeId: 'n1',
  runId: 'run-1',
  workflowId: 'wf-1',
  workflowName: 'Demo',
  sizeBytes: 12,
  createdAt: '2026-09-13T00:00:00Z',
}

const sampleWorkflow: Workflow = {
  id: 'wf-1',
  name: 'Demo',
  description: '',
  nodes: [],
  edges: [],
  createdAt: '2026-09-13T00:00:00Z',
  updatedAt: '2026-09-13T00:00:00Z',
}

function mountView() {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(ArtifactsView, {
    global: {
      plugins: [i18n],
      stubs: {
        Icon: true,
        ProjectFilter: true,
        ArtifactList: {
          template: '<div data-testid="artifact-list-stub" />',
          methods: { scrollToTop() {} },
        },
        ArtifactPreview: true,
        Pagination: true,
      },
    },
  })
}

describe('ArtifactsView loading source lock (plan g1/g2)', () => {
  it('starts groupsLoading true and tracks reloadGroups with a request gen (g1.1)', () => {
    expect(src).toMatch(/const groupsLoading = ref\(true\)/)
    expect(src).toMatch(/let groupsLoadGen = 0/)
    expect(src).toMatch(/const gen = \+\+groupsLoadGen/)
    expect(src).toMatch(/if \(gen !== groupsLoadGen\) return/)
  })

  it('gates empty groups behind !surfaceBusy so first load cannot flash empty (g1.2)', () => {
    expect(src).toMatch(/v-if="!groups\.length && !surfaceBusy"/)
    expect(src).toMatch(/common\.empty\.noMatchingGroups/)
    expect(src).not.toMatch(/v-if="!groups\.length" class="px-2 py-8/)
  })

  it('failed reloadGroups without cache keeps HardLoadLayer and retries via onSurfaceRetry (g1.3)', () => {
    expect(src).toMatch(/failedNoCache/)
    expect(src).toMatch(/Leave groupsLoading true/)
    expect(src).toMatch(/function onSurfaceRetry/)
    expect(src).toMatch(/@retry="onSurfaceRetry"/)
    expect(src).not.toMatch(/catch \{\s*groupArtifacts\.value = \[\]\s*workflows\.value = \[\]/)
  })

  it('keeps RefreshStrip / HardLoadLayer dual-track on surfaceBusy + hasCachedSurface (g2.1)', () => {
    expect(src).toMatch(/surfaceBusy && hasCachedSurface/)
    expect(src).toMatch(/surfaceBusy && !hasCachedSurface/)
    expect(src).toMatch(/data-testid="refresh-strip"|RefreshStrip v-if/)
    expect(src).toMatch(/HardLoadLayer/)
    expect(src).toMatch(/common\.loading\.label/)
    expect(src).toMatch(/:aria-busy="surfaceBusy \? 'true' : 'false'"/)
  })

  it('reuses zh/en common.loading.label (g2.2)', () => {
    expect((common as { common: { loading: { label: string } } }).common.loading.label).toBe('加载中')
    expect((enCommon as { common: { loading: { label: string } } }).common.loading.label).toBe('Loading')
    expect(src).toMatch(/t\('common\.loading\.label'\)/)
  })
})

describe('ArtifactsView loading mount behavior', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    filterState.projectSelected!.value = ''
    apiMocks.getRun.mockResolvedValue({ id: 'run-1', artifacts: [] })
  })

  it('first paint shows HardLoadLayer and not「无匹配分组」while groups pending (g1.2)', async () => {
    let resolveArts!: (v: Artifact[]) => void
    apiMocks.listArtifacts.mockImplementation(
      () =>
        new Promise<Artifact[]>((resolve) => {
          resolveArts = resolve
        }),
    )
    apiMocks.listWorkflows.mockResolvedValue([sampleWorkflow])

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-testid="hard-load-layer"]').exists()).toBe(true)
    expect(wrapper.attributes('aria-busy') || wrapper.find('[aria-busy="true"]').exists()).toBeTruthy()
    expect(wrapper.text()).not.toContain('无匹配分组')
    expect(wrapper.find('.card').attributes('aria-busy')).toBe('true')

    resolveArts([sampleArtifact])
    await flushPromises()
    // page list may still be in flight via second listArtifacts; settle remaining
    await flushPromises()
    wrapper.unmount()
  })

  it('project switch re-enters loading; with cached groups uses RefreshStrip (g2.1)', async () => {
    apiMocks.listArtifacts.mockImplementation(async (opts?: { page?: number; groupBy?: string }) => {
      if (opts?.groupBy === 'run' || opts?.page) {
        return { items: [sampleArtifact], total: 1, page: 1, pageSize: 20 }
      }
      return [sampleArtifact]
    })
    apiMocks.listWorkflows.mockResolvedValue([sampleWorkflow])

    const wrapper = mountView()
    await flushPromises()
    await flushPromises()

    expect(wrapper.find('[data-testid="hard-load-layer"]').exists()).toBe(false)
    expect(wrapper.find('.card').attributes('aria-busy')).toBe('false')

    let resolveNext!: (v: Artifact[]) => void
    apiMocks.listArtifacts.mockImplementationOnce(
      () =>
        new Promise<Artifact[]>((resolve) => {
          resolveNext = resolve
        }),
    )
    apiMocks.listWorkflows.mockResolvedValueOnce([sampleWorkflow])

    filterState.projectSelected!.value = 'proj-b'
    await flushPromises()

    expect(wrapper.find('[data-testid="refresh-strip"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="hard-load-layer"]').exists()).toBe(false)
    expect(wrapper.find('.card').attributes('aria-busy')).toBe('true')
    expect(wrapper.text()).not.toContain('无匹配分组')

    resolveNext([sampleArtifact])
    await flushPromises()
    await flushPromises()
    wrapper.unmount()
  })

  it('reloadGroups failure with no cache keeps HardLoadLayer and retry reloads (g1.3)', async () => {
    vi.useFakeTimers()
    apiMocks.listArtifacts.mockRejectedValueOnce(new Error('network'))
    apiMocks.listWorkflows.mockRejectedValueOnce(new Error('network'))

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-testid="hard-load-layer"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('无匹配分组')
    expect(wrapper.find('.card').attributes('aria-busy')).toBe('true')

    apiMocks.listArtifacts.mockResolvedValue([])
    apiMocks.listWorkflows.mockResolvedValue([])

    await vi.advanceTimersByTimeAsync(10_000)
    await flushPromises()
    const retry = wrapper.find('[data-testid="hard-load-retry"]')
    expect(retry.exists()).toBe(true)
    await retry.trigger('click')
    await flushPromises()
    vi.useRealTimers()

    expect(apiMocks.listArtifacts.mock.calls.length).toBeGreaterThan(1)
    // Success empty — overlay gone, real empty allowed
    expect(wrapper.find('[data-testid="hard-load-layer"]').exists()).toBe(false)
    expect(wrapper.text()).toMatch(/无匹配分组|暂无/)
    wrapper.unmount()
  })
})
