// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import { defineComponent, h } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { Artifact, ArtifactTree } from '@/lib/shared/types'

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

const tree: ArtifactTree = {
  count: 4,
  sessionCount: 1,
  workflows: [{ workflowId: 'w1', workflowName: 'Build', count: 2 }],
}

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
    let resolveTree!: (v: ArtifactTree) => void
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

  it('defaults to all sources without a selection query', async () => {
    const w = await mountAt('/artifacts')
    await flushPromises()

    expect(router.currentRoute.value.query).toEqual({})
    expect(lastListCall()).toEqual({ page: 1, pageSize: 20, groupBy: 'run', q: undefined })
    expect(w.find('[data-testid="artifacts-breadcrumb"]').text()).toBe('产物')
    expect(w.find('[data-testid="list-title"]').text()).toBe('产物 · 4')
    expect(w.find('[data-testid="preview-id"]').text()).toBe('a1')
  })

  it('opens workflow and session sources on demand instead of a third pane', async () => {
    const w = await mountAt('/artifacts')
    await flushPromises()

    expect(w.find('[data-testid="artifacts-tree-pane"]').exists()).toBe(false)
    expect(w.find('[data-testid="resource-list"]').exists()).toBe(false)
    await w.find('[data-testid="artifacts-tree-toggle"]').trigger('click')
    const labels = w.findAll('[data-testid="resource-list"] [data-resource-key]').map((el) => el.text())
    expect(labels).toEqual(['产物4', 'Build2', 'Agent 会话产物1'])
  })

  it('selecting a child syncs the URL and narrows the list; back/forward restores', async () => {
    const w = await mountAt('/artifacts')
    await flushPromises()

    await w.find('[data-testid="artifacts-tree-toggle"]').trigger('click')
    await w.find('[data-resource-key="workflow:w1"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query).toEqual({ workflow: 'w1' })
    expect(lastListCall()).toMatchObject({ workflowId: 'w1' })
    expect(w.find('[data-testid="artifacts-breadcrumb"]').text()).toBe('产物Build')

    await w.find('[data-testid="artifacts-tree-toggle"]').trigger('click')
    await w.find('[data-resource-key="session"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query).toEqual({ session: '1' })
    expect(lastListCall()).toMatchObject({ session: true })
    expect(lastListCall()).not.toHaveProperty('workflowId')

    router.back()
    await flushPromises()
    await flushPromises()
    expect(router.currentRoute.value.query).toEqual({ workflow: 'w1' })
    expect(lastListCall()).toMatchObject({ workflowId: 'w1' })

    await w.find('[data-testid="artifacts-crumb-root"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query).toEqual({})
    expect(lastListCall()).toEqual(expect.objectContaining({}))
    expect(lastListCall()).not.toHaveProperty('workflowId')
  })

  it('does not rewrite the query of the route it is leaving to', async () => {
    await mountAt('/artifacts')
    await flushPromises()
    const calls = apiMocks.listArtifacts.mock.calls.length
    await router.push('/other')
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/other')
    expect(apiMocks.listArtifacts.mock.calls.length).toBe(calls)
  })

  it('canonicalizes unknown workflow URLs to all sources', async () => {
    await mountAt('/artifacts?workflow=gone')
    await flushPromises()
    expect(router.currentRoute.value.query).toEqual({})
    expect(lastListCall()).toMatchObject({})
  })

  it('shows the global empty state when no sources own artifacts', async () => {
    apiMocks.getArtifactTree.mockResolvedValue({ count: 0, sessionCount: 0, workflows: [] })
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
    expect(lastListCall()).toMatchObject({})
  })

  it('debounces server search into q', async () => {
    vi.useFakeTimers()
    const w = await mountAt('/artifacts')
    await flushPromises()
    const before = apiMocks.listArtifacts.mock.calls.length

    await w.find('[data-testid="list-search"]').setValue('spec')
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()
    expect(apiMocks.listArtifacts.mock.calls.length).toBe(before + 1)
    expect(lastListCall()).toMatchObject({ q: 'spec', page: 1 })
  })

  it('refreshes tree counts and the list after a delete, selecting the neighbour', async () => {
    const w = await mountAt('/artifacts')
    await flushPromises()
    expect(w.find('[data-testid="preview-id"]').text()).toBe('a1')

    apiMocks.getArtifactTree.mockResolvedValue({ ...tree, count: 3 })
    apiMocks.listArtifacts.mockResolvedValue(page([art('a2')]))
    await w.find('[data-testid="preview-delete"]').trigger('click')
    await flushPromises()

    expect(apiMocks.getArtifactTree).toHaveBeenCalledTimes(2)
    expect(w.find('[data-testid="artifacts-total"]').text()).toBe('3 个文件')
    expect(w.find('[data-testid="list-title"]').text()).toBe('产物 · 3')
    expect(w.find('[data-testid="preview-id"]').text()).toBe('a2')
  })

  it('falls back to all sources when a delete empties the selected workflow', async () => {
    const w = await mountAt('/artifacts?workflow=w1')
    await flushPromises()
    apiMocks.getArtifactTree.mockResolvedValue({ ...tree, count: 2, workflows: [] })
    apiMocks.listArtifacts.mockResolvedValue(page([art('a9')]))
    await w.find('[data-testid="preview-delete"]').trigger('click')
    await flushPromises()
    await flushPromises()

    expect(router.currentRoute.value.query).toEqual({})
    expect(lastListCall()).toMatchObject({})
  })

  it('clears the previous source before loading and exposes a failed source request', async () => {
    const w = await mountAt('/artifacts')
    await flushPromises()
    let fail!: (error: Error) => void
    apiMocks.listArtifacts.mockImplementationOnce(() => new Promise((_, reject) => { fail = reject }))
    await w.find('[data-testid="artifacts-tree-toggle"]').trigger('click')
    await w.find('[data-resource-key="workflow:w1"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="artifacts-list-skeleton"]').exists()).toBe(true)
    expect(w.find('[data-testid="list-item"]').exists()).toBe(false)
    expect(w.find('[data-testid="preview-id"]').text()).toBe('')
    fail(new Error('network'))
    await flushPromises()
    expect(w.find('[data-testid="artifacts-list-failed"]').exists()).toBe(true)
    await w.find('[data-testid="artifacts-list-failed"] button').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="list-item"]').exists()).toBe(true)
  })

  it('returns a narrow canvas to the list after deleting the last file on a later page', async () => {
    viewport.mobile = true
    apiMocks.listArtifacts.mockResolvedValue({ ...page([art('first')]), total: 21 })
    const w = await mountAt('/artifacts')
    await flushPromises()
    apiMocks.listArtifacts.mockResolvedValueOnce({ ...page([art('last')]), total: 21 })
    w.findComponent({ name: 'Pagination' }).vm.$emit('update:page', 2)
    await flushPromises()
    await w.find('[data-testid="list-item"]').trigger('click')
    expect(w.find('[data-testid="artifacts-preview-pane"]').exists()).toBe(true)
    apiMocks.listArtifacts.mockResolvedValueOnce({ ...page([]), total: 20 })
    apiMocks.listArtifacts.mockResolvedValueOnce({ ...page([art('first')]), total: 20 })
    await w.find('[data-testid="preview-delete"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="artifacts-list-pane"]').exists()).toBe(true)
    expect(w.find('[data-testid="artifacts-preview-pane"]').exists()).toBe(false)
    expect(w.find('[data-testid="list-item"]').text()).toBe('first')
    expect(lastListCall()).toMatchObject({ page: 1 })
  })

  it('closes sources with Escape and restores trigger focus', async () => {
    const w = await mountAt('/artifacts')
    await flushPromises()
    const trigger = w.find('[data-testid="artifacts-tree-toggle"]')
    await trigger.trigger('click')
    await flushPromises()
    expect(document.activeElement?.tagName).toBe('INPUT')
    await w.find('[role="dialog"]').trigger('keydown', { key: 'Escape' })
    await flushPromises()
    expect(w.find('[data-testid="artifacts-tree-sheet"]').exists()).toBe(false)
    expect(document.activeElement).toBe(trigger.element)
  })

  it('selecting the current source closes without fetching again', async () => {
    const w = await mountAt('/artifacts')
    await flushPromises()
    const calls = apiMocks.listArtifacts.mock.calls.length
    await w.find('[data-testid="artifacts-tree-toggle"]').trigger('click')
    await w.find('[data-resource-key="all"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="artifacts-tree-sheet"]').exists()).toBe(false)
    expect(apiMocks.listArtifacts.mock.calls.length).toBe(calls)
  })

  it('mobile: tree lives in a sheet toggled from the breadcrumb bar', async () => {
    viewport.mobile = true
    const w = await mountAt('/artifacts')
    await flushPromises()

    expect(w.find('[data-testid="artifacts-tree-pane"]').exists()).toBe(false)
    expect(w.find('[data-testid="artifacts-tree-sheet"]').exists()).toBe(false)

    await w.find('[data-testid="artifacts-tree-toggle"]').trigger('click')
    expect(w.find('[data-testid="artifacts-tree-sheet"]').exists()).toBe(true)

    await w.find('[data-resource-key="workflow:w1"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query).toEqual({ workflow: 'w1' })
    expect(w.find('[data-testid="artifacts-tree-sheet"]').exists()).toBe(false)

    await w.find('[data-testid="list-item"]').trigger('click')
    expect(w.find('[data-testid="artifacts-preview-pane"]').exists()).toBe(true)
    expect(w.find('[data-testid="artifacts-list-pane"]').exists()).toBe(false)
    await w.find('[data-testid="artifacts-mobile-back"]').trigger('click')
    expect(w.find('[data-testid="artifacts-list-pane"]').exists()).toBe(true)
  })

  it('hides the annotated subtitle and keeps the title, count, source filter, and empty state', async () => {
    const { readFileSync } = await import('node:fs')
    const { dirname, join } = await import('node:path')
    const { fileURLToPath } = await import('node:url')
    const src = readFileSync(join(dirname(fileURLToPath(import.meta.url)), 'ArtifactsView.vue'), 'utf8')
    expect(src).not.toContain('pages.artifacts.subtitle')
    expect(src).toContain('pages.artifacts.emptyTitle')
    const w = await mountAt('/artifacts')
    await flushPromises()
    expect(w.text()).toContain('产物')
    expect(w.get('[data-testid="artifacts-total"]').text()).toContain('4')
    expect(w.text()).toContain('全部来源')
    expect(w.text()).not.toContain('浏览、检索和下载工作流与会话生成的文件。')
    expect(w.text()).not.toContain('Browse, find and download files from workflows and conversations.')
    w.unmount()

    apiMocks.getArtifactTree.mockResolvedValueOnce({ count: 0, sessionCount: 0, workflows: [] })
    apiMocks.listArtifacts.mockResolvedValueOnce(page([]))
    const empty = await mountAt('/artifacts')
    await flushPromises()
    expect(empty.get('[data-testid="artifacts-empty"]').text()).toContain('还没有产物')
    expect(empty.text()).toContain('工作流运行和 Agent 会话生成的文件会显示在这里。')
    expect(empty.text()).not.toContain('浏览、检索和下载工作流与会话生成的文件。')
  })
})
