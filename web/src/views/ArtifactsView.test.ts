// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import { defineComponent, h } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { Artifact, ArtifactTreeProject } from '@/lib/shared/types'

const apiMocks = vi.hoisted(() => ({
  getArtifactTree: vi.fn(),
  listArtifacts: vi.fn(),
  getRun: vi.fn(),
}))

const viewport = vi.hoisted(() => ({ mobile: false }))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      getArtifactTree: apiMocks.getArtifactTree,
      listArtifacts: apiMocks.listArtifacts,
      getRun: apiMocks.getRun,
    },
  }
})

vi.mock('@/lib/composables/useBreakpoint', async () => {
  const { ref } = await import('vue')
  return { useBreakpoint: () => ({ isMobile: ref(viewport.mobile) }) }
})

import ArtifactsView from './ArtifactsView.vue'

const tree: ArtifactTreeProject[] = [
  {
    projectId: 'p1',
    projectName: 'Alpha',
    count: 3,
    sessionCount: 1,
    workflows: [{ workflowId: 'w1', workflowName: 'Build', count: 2 }],
  },
  { projectId: 'p2', projectName: 'Beta', count: 1, sessionCount: 0, workflows: [] },
]

function art(id: string, overrides: Partial<Artifact> = {}): Artifact {
  return {
    id,
    name: `${id}.md`,
    kind: 'markdown',
    nodeId: 'n1',
    runId: 'run-1',
    workflowId: 'w1',
    workflowName: 'Build',
    sizeBytes: 1,
    createdAt: '2026-09-13T00:00:00Z',
    ...overrides,
  }
}

function page(items: Artifact[]) {
  return { items, total: items.length, page: 1, pageSize: 20, hasMore: false }
}

const ArtifactListStub = defineComponent({
  name: 'ArtifactList',
  props: ['artifacts', 'headerTitle', 'groupTotal', 'emptyText', 'search'],
  emits: ['select', 'update:search'],
  methods: { scrollToTop() {} },
  setup(props, { emit }) {
    return () =>
      h('div', { 'data-testid': 'artifact-list-stub' }, [
        h('span', { 'data-testid': 'list-title' }, `${props.headerTitle} · ${props.groupTotal}`),
        h('input', {
          'data-testid': 'list-search',
          onInput: (e: Event) => emit('update:search', (e.target as HTMLInputElement).value),
        }),
        ...((props.artifacts as Artifact[]) ?? []).map((a) =>
          h('button', { 'data-testid': 'list-item', onClick: () => emit('select', a) }, a.id),
        ),
      ])
  },
})

const ArtifactPreviewStub = defineComponent({
  name: 'ArtifactPreview',
  props: ['artifact'],
  emits: ['deleted'],
  setup(props, { emit }) {
    return () =>
      h('div', { 'data-testid': 'preview-stub' }, [
        h('span', { 'data-testid': 'preview-id' }, (props.artifact as Artifact | null)?.id ?? ''),
        h('button', {
          'data-testid': 'preview-delete',
          onClick: () => emit('deleted', (props.artifact as Artifact).id),
        }),
      ])
  },
})

let router: Router
let wrapper: VueWrapper | null = null

async function mountAt(path: string) {
  router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/artifacts', name: 'artifacts', component: ArtifactsView },
      { path: '/other', name: 'other', component: { render: () => null } },
    ],
  })
  await router.push(path)
  await router.isReady()
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
  wrapper = mount(ArtifactsView, {
    attachTo: document.body,
    global: {
      plugins: [i18n, router],
      stubs: {
        ArtifactList: ArtifactListStub,
        ArtifactPreview: ArtifactPreviewStub,
        Pagination: true,
        Teleport: true,
      },
    },
  })
  return wrapper
}

function lastListCall() {
  return apiMocks.listArtifacts.mock.calls.at(-1)?.[0]
}

beforeEach(() => {
  vi.clearAllMocks()
  localStorage.clear()
  viewport.mobile = false
  apiMocks.getArtifactTree.mockResolvedValue(tree)
  apiMocks.listArtifacts.mockResolvedValue(page([art('a1'), art('a2')]))
  apiMocks.getRun.mockResolvedValue({ id: 'run-1', artifacts: [] })
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  vi.useRealTimers()
})

describe('ArtifactsView', () => {
  it('shows skeletons (not the empty state) while the tree is pending', async () => {
    let resolveTree!: (v: ArtifactTreeProject[]) => void
    apiMocks.getArtifactTree.mockImplementation(() => new Promise((r) => (resolveTree = r)))
    const w = await mountAt('/artifacts')
    await flushPromises()

    expect(w.find('[data-testid="artifacts-surface"]').attributes('aria-busy')).toBe('true')
    expect(w.find('[data-testid="artifacts-list-skeleton"]').exists()).toBe(true)
    expect(w.find('[data-testid="artifacts-empty"]').exists()).toBe(false)

    resolveTree(tree)
    await flushPromises()
    expect(w.find('[data-testid="artifacts-list-skeleton"]').exists()).toBe(false)
    expect(w.find('[data-testid="artifacts-surface"]').attributes('aria-busy')).toBe('false')
  })

  it('defaults to the first project and writes it to the URL', async () => {
    const w = await mountAt('/artifacts')
    await flushPromises()

    expect(router.currentRoute.value.query).toEqual({ project: 'p1' })
    expect(lastListCall()).toEqual({ projectId: 'p1', page: 1, pageSize: 20, groupBy: 'run', q: undefined })
    expect(w.find('[data-testid="artifacts-breadcrumb"]').text()).toBe('Alpha')
    expect(w.find('[data-testid="list-title"]').text()).toBe('Alpha · 3')
    expect(w.find('[data-testid="preview-id"]').text()).toBe('a1')
  })

  it('renders project → workflow / session children in the tree', async () => {
    const w = await mountAt('/artifacts')
    await flushPromises()

    const labels = w.findAll('[data-testid="project-tree"] [role="treeitem"]').map((el) => el.text())
    expect(labels).toEqual(['Alpha3', 'Build2', 'Agent 会话产物1', 'Beta1'])
  })

  it('selecting a child syncs the URL and narrows the list; back/forward restores', async () => {
    const w = await mountAt('/artifacts?project=p1')
    await flushPromises()

    await w.find('[data-tree-key="c:p1:w1"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query).toEqual({ project: 'p1', workflow: 'w1' })
    expect(lastListCall()).toMatchObject({ projectId: 'p1', workflowId: 'w1' })
    expect(w.find('[data-testid="artifacts-breadcrumb"]').text()).toBe('AlphaBuild')

    await w.find('[data-tree-key="c:p1:__session__"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query).toEqual({ project: 'p1', session: '1' })
    expect(lastListCall()).toMatchObject({ projectId: 'p1', session: true })
    expect(lastListCall()).not.toHaveProperty('workflowId')

    router.back()
    await flushPromises()
    await flushPromises()
    expect(router.currentRoute.value.query).toEqual({ project: 'p1', workflow: 'w1' })
    expect(lastListCall()).toMatchObject({ projectId: 'p1', workflowId: 'w1' })

    await w.find('[data-testid="artifacts-crumb-project"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query).toEqual({ project: 'p1' })
    expect(lastListCall()).toEqual(expect.objectContaining({ projectId: 'p1' }))
    expect(lastListCall()).not.toHaveProperty('workflowId')
  })

  it('does not rewrite the query of the route it is leaving to', async () => {
    await mountAt('/artifacts?project=p1')
    await flushPromises()
    const calls = apiMocks.listArtifacts.mock.calls.length
    await router.push('/other')
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/other')
    expect(apiMocks.listArtifacts.mock.calls.length).toBe(calls)
  })

  it('canonicalizes stale URLs (unknown workflow → its project)', async () => {
    await mountAt('/artifacts?project=p2&workflow=gone')
    await flushPromises()
    expect(router.currentRoute.value.query).toEqual({ project: 'p2' })
    expect(lastListCall()).toMatchObject({ projectId: 'p2' })
  })

  it('shows the global empty state when no project owns artifacts', async () => {
    apiMocks.getArtifactTree.mockResolvedValue([])
    const w = await mountAt('/artifacts')
    await flushPromises()
    expect(w.find('[data-testid="artifacts-empty"]').text()).toContain('还没有产物')
    expect(apiMocks.listArtifacts).not.toHaveBeenCalled()
  })

  it('cold tree failure shows a retry that reloads', async () => {
    apiMocks.getArtifactTree.mockRejectedValueOnce(new Error('network'))
    const w = await mountAt('/artifacts')
    await flushPromises()
    expect(w.find('[data-testid="artifacts-load-failed"]').exists()).toBe(true)

    await w.find('[data-testid="artifacts-retry"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="artifacts-load-failed"]').exists()).toBe(false)
    expect(apiMocks.getArtifactTree).toHaveBeenCalledTimes(2)
    expect(lastListCall()).toMatchObject({ projectId: 'p1' })
  })

  it('debounces server search into q', async () => {
    vi.useFakeTimers()
    const w = await mountAt('/artifacts?project=p1')
    await flushPromises()
    const before = apiMocks.listArtifacts.mock.calls.length

    await w.find('[data-testid="list-search"]').setValue('spec')
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()
    expect(apiMocks.listArtifacts.mock.calls.length).toBe(before + 1)
    expect(lastListCall()).toMatchObject({ projectId: 'p1', q: 'spec', page: 1 })
  })

  it('refreshes tree counts and the list after a delete, selecting the neighbour', async () => {
    const w = await mountAt('/artifacts?project=p1')
    await flushPromises()
    expect(w.find('[data-testid="preview-id"]').text()).toBe('a1')

    apiMocks.getArtifactTree.mockResolvedValue([{ ...tree[0], count: 2 }, tree[1]])
    apiMocks.listArtifacts.mockResolvedValue(page([art('a2')]))
    await w.find('[data-testid="preview-delete"]').trigger('click')
    await flushPromises()

    expect(apiMocks.getArtifactTree).toHaveBeenCalledTimes(2)
    expect(w.find('[data-tree-key="p:p1"] [data-tree-count]').text()).toBe('2')
    expect(w.find('[data-testid="list-title"]').text()).toBe('Alpha · 2')
    expect(w.find('[data-testid="preview-id"]').text()).toBe('a2')
  })

  it('falls back to the project when a delete empties the selected workflow', async () => {
    const w = await mountAt('/artifacts?project=p2')
    await flushPromises()
    apiMocks.getArtifactTree.mockResolvedValue([tree[0]])
    apiMocks.listArtifacts.mockResolvedValue(page([art('a9')]))
    await w.find('[data-testid="preview-delete"]').trigger('click')
    await flushPromises()
    await flushPromises()

    expect(router.currentRoute.value.query).toEqual({ project: 'p1' })
    expect(lastListCall()).toMatchObject({ projectId: 'p1' })
  })

  it('mobile: tree lives in a sheet toggled from the breadcrumb bar', async () => {
    viewport.mobile = true
    const w = await mountAt('/artifacts?project=p1')
    await flushPromises()

    expect(w.find('[data-testid="artifacts-tree-pane"]').exists()).toBe(false)
    expect(w.find('[data-testid="artifacts-tree-sheet"]').exists()).toBe(false)

    await w.find('[data-testid="artifacts-tree-toggle"]').trigger('click')
    expect(w.find('[data-testid="artifacts-tree-sheet"]').exists()).toBe(true)

    await w.find('[data-tree-key="p:p2"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query).toEqual({ project: 'p2' })
    expect(w.find('[data-testid="artifacts-tree-sheet"]').exists()).toBe(false)

    await w.find('[data-testid="list-item"]').trigger('click')
    expect(w.find('[data-testid="artifacts-preview-pane"]').exists()).toBe(true)
    expect(w.find('[data-testid="artifacts-list-pane"]').exists()).toBe(false)
    await w.find('[data-testid="artifacts-mobile-back"]').trigger('click')
    expect(w.find('[data-testid="artifacts-list-pane"]').exists()).toBe(true)
  })
})
