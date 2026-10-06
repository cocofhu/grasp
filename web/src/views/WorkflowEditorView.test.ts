// @vitest-environment happy-dom
import { defineComponent, h, nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import canvas from '@/locales/zh-CN/canvas.json'
import common from '@/locales/zh-CN/common.json'
import nodes from '@/locales/zh-CN/nodes.json'
import pages from '@/locales/zh-CN/pages.json'
import { TEST_REVIEW_CAPS } from '@/test/capsFixtures'
import type { CanvasEditor } from '@/components/canvas/composables/useCanvasEditor'

const apiMocks = vi.hoisted(() => ({
  getWorkflow: vi.fn(),
  saveWorkflow: vi.fn(),
  publishWorkflow: vi.fn(),
  listAgents: vi.fn(),
  getProject: vi.fn(),
  listWorkflowVersions: vi.fn(),
  getWorkflowVersionGraph: vi.fn(),
  restoreWorkflowVersion: vi.fn(),
}))
const toast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn(), warn: vi.fn(), info: vi.fn(), show: vi.fn() }))
const canvasSpy = vi.hoisted(() => ({ centerOn: vi.fn(), editor: null as CanvasEditor | null, props: {} as Record<string, unknown> }))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return { ...actual, api: { ...actual.api, ...apiMocks } }
})
vi.mock('@/lib/composables/useBreakpoint', async () => {
  const { ref } = await import('vue')
  return { useBreakpoint: () => ({ isMobile: ref(false) }) }
})
vi.mock('@/lib/composables/useToast', () => ({ useToast: () => toast }))
vi.mock('@/lib/composables/useProjectContext', () => ({ readStoredProjectId: () => '' }))
vi.mock('@/lib/run/useWorkflowImport', async () => {
  const { ref } = await import('vue')
  return {
    useWorkflowImport: () => ({
      fileInput: ref(null),
      showDiscardConfirm: ref(false),
      triggerImport: vi.fn(),
      onImportFile: vi.fn(),
      confirmDiscardImport: vi.fn(),
      cancelDiscardImport: vi.fn(),
    }),
  }
})
vi.mock('@/components/canvas/WorkflowCanvas.vue', async () => {
  const { defineComponent: dc } = await import('vue')
  return {
    default: dc({
      name: 'WorkflowCanvas',
      props: { nodes: Array, edges: Array, mode: String, editor: Object, agents: Array, autoLayoutOnInit: Boolean },
      emits: ['save'],
      setup(props, { expose }) {
        if (props.editor) {
          canvasSpy.editor = props.editor as CanvasEditor
          canvasSpy.props = props as Record<string, unknown>
        }
        expose({ centerOn: canvasSpy.centerOn, fit: vi.fn(), layout: vi.fn(), openCommandPalette: vi.fn() })
        return {}
      },
      template: '<div data-testid="workflow-canvas-stub" :data-mode="mode" />',
    }),
  }
})

import WorkflowEditorView from './WorkflowEditorView.vue'
import { BACKUP_PREFIX } from '@/components/canvas/composables/useWorkflowSave'

const i18n = createI18n({
  legacy: false,
  locale: 'zh-CN',
  messages: { 'zh-CN': { ...common, ...nodes, ...pages, ...canvas } },
})
const t = i18n.global.t as (k: string, n?: Record<string, unknown>) => string

function workflow(over: Record<string, unknown> = {}) {
  return {
    id: 'wf-1',
    projectId: 'p1',
    name: 'demo-flow',
    description: '',
    status: 'draft',
    version: 3,
    publishedVersion: 0,
    updatedAt: '2026-01-01T00:00:00Z',
    nodes: [
      { id: 'in', type: 'input', label: '输入', position: { x: 0, y: 0 }, config: { variables: [] } },
      { id: 'test', type: 'agent', label: '测试评审', position: { x: 320, y: 0 }, config: { agent_profile: '测试评审', prompt: '跑测试' } },
      { id: 'out', type: 'output', label: '输出', position: { x: 640, y: 0 }, config: { results: [] } },
    ],
    edges: [
      { id: 'e1', source: 'in', target: 'test' },
      { id: 'e2', source: 'test', sourceHandle: 'pass', target: 'out' },
      { id: 'e3', source: 'test', sourceHandle: 'fail', target: 'out' },
    ],
    ...over,
  }
}

const mounted: { unmount: () => void }[] = []

async function mountEditor() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/projects/:id?', component: { render: () => h('div') } },
      { path: '/agents', component: { render: () => h('div') } },
      { path: '/workflows/:id/edit', component: WorkflowEditorView },
    ],
  })
  await router.push('/workflows/wf-1/edit')
  await router.isReady()
  const wrapper = mount(defineComponent({ setup: () => () => h(RouterView) }), {
    global: {
      plugins: [i18n, router],
      directives: { hoverInk: {} },
      stubs: {
        Icon: true,
        StatusPill: true,
        AppDrawer: true,
        RunLaunchModal: true,
        ExportVersionModal: true,
        CopyWorkflowModal: true,
        WorkflowApiTab: true,
        WorkflowRunHistoryTab: true,
        HardLoadLayer: true,
        AppModal: {
          props: ['open'],
          template: '<div v-if="open" data-testid="modal"><slot /><slot name="footer" /></div>',
        },
      },
    },
    attachTo: document.body,
  })
  mounted.push(wrapper)
  await flushPromises()
  await nextTick()
  return { wrapper, router }
}

beforeEach(() => {
  apiMocks.getWorkflow.mockResolvedValue(workflow())
  apiMocks.saveWorkflow.mockImplementation(async (wf: any) => ({ ...wf, updatedAt: 'now' }))
  apiMocks.publishWorkflow.mockResolvedValue({ status: 'published', version: 4, publishedVersion: 4 })
  apiMocks.listWorkflowVersions.mockResolvedValue([
    { workflowId: 'wf-1', version: 2, name: 'demo-flow', description: '', nodeCount: 2, source: 'save', createdAt: '2025-12-30T00:00:00Z' },
    { workflowId: 'wf-1', version: 3, name: 'demo-flow', description: '', nodeCount: 3, source: 'restore', restoredFrom: 1, createdAt: '2025-12-31T00:00:00Z' },
    { workflowId: 'wf-1', version: 1, name: 'demo-flow', description: '', nodeCount: 2, source: 'import', createdAt: '2025-12-29T00:00:00Z', publishedAt: '2025-12-29T01:00:00Z' },
  ])
  apiMocks.getWorkflowVersionGraph.mockResolvedValue({ nodes: workflow().nodes.slice(0, 2), edges: [] })
  apiMocks.restoreWorkflowVersion.mockImplementation(async (_id: string, v: number) => workflow({ version: 4, name: `restored-v${v}` }))
  apiMocks.listAgents.mockResolvedValue([{ name: '测试评审', projectId: 'p1', capabilities: TEST_REVIEW_CAPS }])
  apiMocks.getProject.mockResolvedValue({ name: '我的项目' })
})

afterEach(() => {
  while (mounted.length) mounted.pop()!.unmount()
  localStorage.clear()
  vi.useRealTimers()
  vi.clearAllMocks()
  document.body.innerHTML = ''
})

const status = (w: Awaited<ReturnType<typeof mountEditor>>['wrapper']) =>
  w.get('[data-testid="editor-save-status"]').attributes('data-status')

describe('WorkflowEditorView', () => {
  it('loads the workflow into an editable canvas with breadcrumb and saved state', async () => {
    const { wrapper } = await mountEditor()
    expect(apiMocks.getWorkflow).toHaveBeenCalledWith('wf-1')
    expect(canvasSpy.props.nodes).toHaveLength(3)
    expect(canvasSpy.props.mode).toBe('edit')
    expect(canvasSpy.props.autoLayoutOnInit).toBe(true)
    expect(wrapper.get('[data-testid="editor-breadcrumb-project"]').text()).toBe('我的项目')
    expect((wrapper.get('[data-testid="editor-name"]').element as HTMLInputElement).value).toBe('demo-flow')
    expect(status(wrapper)).toBe('saved')
    expect(wrapper.find('[data-testid="node-palette"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="node-inspector"]').exists()).toBe(false)
  })

  it('never saves on its own; the Save button saves and shows the new version', async () => {
    const { wrapper } = await mountEditor()
    vi.useFakeTimers()
    const save = wrapper.get('[data-testid="editor-save"]')
    expect(save.attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="editor-save-status"]').text()).toBe(t('canvas.save.saved', { n: 3 }))
    await wrapper.get('[data-testid="editor-name"]').setValue('renamed')
    expect(status(wrapper)).toBe('dirty')
    expect(wrapper.get('[data-testid="editor-save-status"]').text()).toBe(t('canvas.save.dirty'))
    await vi.advanceTimersByTimeAsync(10_000)
    expect(apiMocks.saveWorkflow).not.toHaveBeenCalled()
    vi.useRealTimers()
    apiMocks.saveWorkflow.mockImplementationOnce(async (wf: any) => ({ ...wf, version: 4, updatedAt: 'now' }))
    await wrapper.get('[data-testid="editor-save"]').trigger('click')
    await flushPromises()
    expect(apiMocks.saveWorkflow).toHaveBeenCalledTimes(1)
    expect(apiMocks.saveWorkflow.mock.calls[0]![0]).toMatchObject({ id: 'wf-1', name: 'renamed' })
    expect(status(wrapper)).toBe('saved')
    expect(wrapper.get('[data-testid="editor-save-status"]').text()).toBe(t('canvas.save.saved', { n: 4 }))
  })

  it('saves on Ctrl+S and the canvas save shortcut, and surfaces failures', async () => {
    const { wrapper } = await mountEditor()
    canvasSpy.editor!.graph.nodes[1]!.label = 'changed'
    await nextTick()
    apiMocks.saveWorkflow.mockRejectedValueOnce(new Error('offline'))
    wrapper.findComponent({ name: 'WorkflowCanvas' }).vm.$emit('save')
    await flushPromises()
    expect(apiMocks.saveWorkflow).toHaveBeenCalledTimes(1)
    expect(status(wrapper)).toBe('error')
    expect(toast.error).toHaveBeenCalled()
    const ev = new KeyboardEvent('keydown', { key: 's', ctrlKey: true, cancelable: true })
    window.dispatchEvent(ev)
    expect(ev.defaultPrevented).toBe(true)
    await flushPromises()
    expect(apiMocks.saveWorkflow).toHaveBeenCalledTimes(2)
    expect(status(wrapper)).toBe('saved')
  })

  it('asks before leaving with unsaved changes', async () => {
    const { wrapper, router } = await mountEditor()
    await wrapper.get('[data-testid="editor-name"]').setValue('renamed')
    await flushPromises()

    let nav = router.push('/projects/p1')
    await flushPromises()
    expect(wrapper.find('[data-testid="editor-leave-save"]').exists()).toBe(true)
    await wrapper.get('[data-testid="editor-leave-cancel"]').trigger('click')
    await nav
    expect(router.currentRoute.value.path).toBe('/workflows/wf-1/edit')

    nav = router.push('/projects/p1')
    await flushPromises()
    await wrapper.get('[data-testid="editor-leave-save"]').trigger('click')
    await nav
    expect(apiMocks.saveWorkflow).toHaveBeenCalledTimes(1)
    expect(router.currentRoute.value.path).toBe('/projects/p1')
  })

  it('leaves without saving and drops the local backup', async () => {
    const { wrapper, router } = await mountEditor()
    await wrapper.get('[data-testid="editor-name"]').setValue('renamed')
    await flushPromises()
    const nav = router.push('/projects/p1')
    await flushPromises()
    await wrapper.get('[data-testid="editor-leave-discard"]').trigger('click')
    await nav
    expect(apiMocks.saveWorkflow).not.toHaveBeenCalled()
    expect(router.currentRoute.value.path).toBe('/projects/p1')
    expect(localStorage.getItem(BACKUP_PREFIX + 'wf-1')).toBeNull()
  })

  it('warns on tab close with unsaved changes', async () => {
    const { wrapper } = await mountEditor()
    const clean = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(clean)
    expect(clean.defaultPrevented).toBe(false)
    await wrapper.get('[data-testid="editor-name"]').setValue('renamed')
    const dirty = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(dirty)
    expect(dirty.defaultPrevented).toBe(true)
    expect(JSON.parse(localStorage.getItem(BACKUP_PREFIX + 'wf-1')!).snapshot).toContain('renamed')
  })

  it('offers to restore a newer local backup and applies it', async () => {
    const local = { ...workflow(), name: 'from-backup' }
    localStorage.setItem(
      BACKUP_PREFIX + 'wf-1',
      JSON.stringify({
        snapshot: JSON.stringify({ name: local.name, description: '', needsRepo: false, nodes: local.nodes, edges: local.edges }),
        savedAt: Date.parse('2026-01-02T00:00:00Z'),
      }),
    )
    const { wrapper } = await mountEditor()
    expect(wrapper.find('[data-testid="editor-backup-banner"]').exists()).toBe(true)
    await wrapper.get('[data-testid="editor-backup-restore"]').trigger('click')
    expect((wrapper.get('[data-testid="editor-name"]').element as HTMLInputElement).value).toBe('from-backup')
    expect(status(wrapper)).toBe('dirty')
    expect(wrapper.find('[data-testid="editor-backup-banner"]').exists()).toBe(false)
  })

  it('ignores a backup older than the server copy and discards on request', async () => {
    const snap = JSON.stringify({ name: 'stale', description: '', needsRepo: false, nodes: [], edges: [] })
    localStorage.setItem(BACKUP_PREFIX + 'wf-1', JSON.stringify({ snapshot: snap, savedAt: Date.parse('2025-12-01T00:00:00Z') }))
    const first = await mountEditor()
    expect(first.wrapper.find('[data-testid="editor-backup-banner"]').exists()).toBe(false)
    first.wrapper.unmount()
    mounted.length = 0

    localStorage.setItem(BACKUP_PREFIX + 'wf-1', JSON.stringify({ snapshot: snap, savedAt: Date.parse('2026-02-01T00:00:00Z') }))
    const { wrapper } = await mountEditor()
    await wrapper.get('[data-testid="editor-backup-discard"]').trigger('click')
    expect(wrapper.find('[data-testid="editor-backup-banner"]').exists()).toBe(false)
    expect(localStorage.getItem(BACKUP_PREFIX + 'wf-1')).toBeNull()
    expect((wrapper.get('[data-testid="editor-name"]').element as HTMLInputElement).value).toBe('demo-flow')
  })

  it('slides the inspector in for a single selected node and closes it', async () => {
    const { wrapper } = await mountEditor()
    canvasSpy.editor!.setSelection(['test'])
    await flushPromises()
    const insp = wrapper.find('[data-testid="node-inspector"]')
    expect(insp.exists()).toBe(true)
    expect(insp.find('[data-testid="agent-picker"]').text()).toContain('测试评审')
    await insp.get('[data-testid="inspector-close"]').trigger('click')
    expect(wrapper.find('[data-testid="node-inspector"]').exists()).toBe(false)
    expect(canvasSpy.editor!.selectedNodeIds.value).toEqual([])
  })

  it('deletes the inspected node', async () => {
    const { wrapper } = await mountEditor()
    canvasSpy.editor!.setSelection(['out'])
    await flushPromises()
    await wrapper.get('[data-testid="inspector-delete"]').trigger('click')
    expect(canvasSpy.editor!.graph.nodes.map((n) => n.id)).toEqual(['in', 'test'])
  })

  it('enters placement mode on palette click instead of adding a node', async () => {
    const { wrapper } = await mountEditor()
    const before = canvasSpy.editor!.graph.nodes.length
    const item = wrapper.get('[data-testid="palette-item-type:human_gate"]')
    await item.trigger('click')
    expect(canvasSpy.editor!.graph.nodes).toHaveLength(before)
    expect(canvasSpy.editor!.placing.value).toEqual({ type: 'human_gate' })
    expect(item.attributes('data-placing')).toBe('true')
    await item.trigger('click')
    expect(canvasSpy.editor!.placing.value).toBeNull()
    await item.trigger('keydown', { key: 'Enter' })
    expect(canvasSpy.editor!.graph.nodes).toHaveLength(before + 1)
    expect(canvasSpy.editor!.graph.nodes.at(-1)!.type).toBe('human_gate')
  })

  it('has no blank Agent in the palette', async () => {
    const { wrapper } = await mountEditor()
    expect(wrapper.find('[data-testid="palette-item-agent:"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="palette-item-agent:测试评审"]').exists()).toBe(true)
  })

  it('counts issues and jumps to the offending node', async () => {
    apiMocks.getWorkflow.mockResolvedValue(
      workflow({
        nodes: [
          ...workflow().nodes,
          { id: 'blank', type: 'agent', label: '空', position: { x: 0, y: 300 }, config: { agent_profile: '', prompt: '' } },
        ],
      }),
    )
    const { wrapper } = await mountEditor()
    const btn = wrapper.get('[data-testid="editor-issues"]')
    expect(btn.text()).not.toBe(t('canvas.issues.none'))
    await btn.trigger('click')
    const items = wrapper.findAll('[data-testid^="editor-issue-"]')
    const toBlank = items.find((i) => i.text().includes('空'))!
    await toBlank.trigger('click')
    expect(canvasSpy.editor!.selectedNodeIds.value).toEqual(['blank'])
    expect(canvasSpy.centerOn).toHaveBeenCalledWith('blank')
    expect(wrapper.find('[data-testid="editor-issues-list"]').exists()).toBe(false)
  })

  it('reports no issues for a valid graph', async () => {
    const { wrapper } = await mountEditor()
    expect(wrapper.get('[data-testid="editor-issues"]').text()).toBe(t('canvas.issues.none'))
  })

  it('saves unsaved changes before publishing the latest version', async () => {
    const { wrapper } = await mountEditor()
    await wrapper.get('[data-testid="editor-name"]').setValue('to publish')
    await wrapper.get('[data-testid="editor-publish"]').trigger('click')
    expect(wrapper.find('[data-testid="editor-publish-dirty"]').exists()).toBe(true)
    await wrapper.get('[data-testid="editor-publish-confirm"]').trigger('click')
    await flushPromises()
    expect(apiMocks.saveWorkflow).toHaveBeenCalledTimes(1)
    expect(apiMocks.publishWorkflow).toHaveBeenCalledWith('wf-1')
    expect(apiMocks.saveWorkflow.mock.invocationCallOrder[0]!).toBeLessThan(apiMocks.publishWorkflow.mock.invocationCallOrder[0]!)
    expect(wrapper.text()).toContain(t('canvas.publish.done', { n: 4 }))
  })

  it('lists versions, previews one read-only and restores it after confirmation', async () => {
    apiMocks.getWorkflow.mockResolvedValue(workflow({ publishedVersion: 1 }))
    const { wrapper } = await mountEditor()
    await wrapper.get('[data-testid="editor-versions"]').trigger('click')
    await flushPromises()
    const items = wrapper.findAll('[data-testid^="version-item-"]')
    expect(items.map((i) => i.attributes('data-testid'))).toEqual(['version-item-3', 'version-item-2', 'version-item-1'])
    expect(items[0]!.find('[data-testid="version-latest"]').exists()).toBe(true)
    expect(items[0]!.text()).toContain(t('canvas.versions.source.restoredFrom', { n: 1 }))
    expect(items[2]!.find('[data-testid="version-published"]').exists()).toBe(true)
    expect(items[2]!.text()).toContain(t('canvas.versions.source.import'))
    expect(items[1]!.text()).toContain(t('canvas.versions.nodes', { n: 2 }))
    expect(wrapper.find('[data-testid="version-restore-3"]').exists()).toBe(false)

    await items[1]!.trigger('click')
    await flushPromises()
    expect(apiMocks.getWorkflowVersionGraph).toHaveBeenCalledWith('wf-1', 2)
    expect(wrapper.get('[data-testid="editor-preview-banner"]').text()).toContain(t('canvas.versions.previewing', { n: 2 }))
    expect(wrapper.get('[data-testid="workflow-editor-preview-canvas"]').attributes('data-mode')).toBe('view')
    expect(wrapper.find('[data-testid="node-palette"]').exists()).toBe(false)

    await wrapper.get('[data-testid="editor-name"]').setValue('unsaved edit')
    await wrapper.get('[data-testid="editor-preview-restore"]').trigger('click')
    expect(wrapper.find('[data-testid="editor-restore-dirty"]').exists()).toBe(true)
    await wrapper.get('[data-testid="editor-restore-confirm"]').trigger('click')
    await flushPromises()
    expect(apiMocks.restoreWorkflowVersion).toHaveBeenCalledWith('wf-1', 2)
    expect(apiMocks.saveWorkflow).not.toHaveBeenCalled()
    expect((wrapper.get('[data-testid="editor-name"]').element as HTMLInputElement).value).toBe('restored-v2')
    expect(status(wrapper)).toBe('saved')
    expect(wrapper.find('[data-testid="editor-preview-banner"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="workflow-canvas-stub"]').attributes('data-mode')).toBe('edit')
  })

  it('exits a version preview back to the editable canvas', async () => {
    const { wrapper } = await mountEditor()
    await wrapper.get('[data-testid="editor-versions"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="version-item-1"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="editor-preview-exit"]').trigger('click')
    expect(wrapper.find('[data-testid="editor-preview-banner"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="workflow-canvas-stub"]').attributes('data-mode')).toBe('edit')
    expect(wrapper.find('[data-testid="version-drawer"]').exists()).toBe(true)
  })

  it('opens the more menu with import, export, duplicate and delete', async () => {
    const { wrapper } = await mountEditor()
    await wrapper.get('[data-testid="editor-more"]').trigger('click')
    const menu = wrapper.get('[data-testid="editor-more-menu"]')
    for (const id of ['import', 'export', 'duplicate', 'delete']) {
      expect(menu.find(`[data-testid="editor-menu-${id}"]`).exists()).toBe(true)
    }
  })
})
