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
}))
const toast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn(), warn: vi.fn(), info: vi.fn() }))
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
      props: { nodes: Array, edges: Array, mode: String, editor: Object, autoLayoutOnInit: Boolean },
      emits: ['save'],
      setup(props, { expose }) {
        canvasSpy.editor = props.editor as CanvasEditor
        canvasSpy.props = props as Record<string, unknown>
        expose({ centerOn: canvasSpy.centerOn, fit: vi.fn(), layout: vi.fn(), openCommandPalette: vi.fn() })
        return {}
      },
      template: '<div data-testid="workflow-canvas-stub" />',
    }),
  }
})

import WorkflowEditorView from './WorkflowEditorView.vue'

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
    updatedAt: '',
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
  apiMocks.publishWorkflow.mockResolvedValue({ status: 'published', version: 4 })
  apiMocks.listAgents.mockResolvedValue([{ name: '测试评审', projectId: 'p1', capabilities: TEST_REVIEW_CAPS }])
  apiMocks.getProject.mockResolvedValue({ name: '我的项目' })
})

afterEach(() => {
  while (mounted.length) mounted.pop()!.unmount()
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

  it('autosaves the draft 1s after editing stops', async () => {
    const { wrapper } = await mountEditor()
    vi.useFakeTimers()
    await wrapper.get('[data-testid="editor-name"]').setValue('renamed')
    expect(status(wrapper)).toBe('unsaved')
    await vi.advanceTimersByTimeAsync(900)
    expect(apiMocks.saveWorkflow).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(100)
    expect(apiMocks.saveWorkflow).toHaveBeenCalledTimes(1)
    expect(apiMocks.saveWorkflow.mock.calls[0]![0]).toMatchObject({ id: 'wf-1', name: 'renamed' })
    await flushPromises()
    expect(status(wrapper)).toBe('saved')
  })

  it('saves immediately on the canvas save shortcut and surfaces failures', async () => {
    const { wrapper } = await mountEditor()
    canvasSpy.editor!.graph.nodes[1]!.label = 'changed'
    await nextTick()
    apiMocks.saveWorkflow.mockRejectedValueOnce(new Error('offline'))
    wrapper.findComponent({ name: 'WorkflowCanvas' }).vm.$emit('save')
    await flushPromises()
    expect(apiMocks.saveWorkflow).toHaveBeenCalledTimes(1)
    expect(status(wrapper)).toBe('error')
    expect(toast.error).toHaveBeenCalled()
    await wrapper.get('[data-testid="editor-save-status"] button').trigger('click')
    await flushPromises()
    expect(apiMocks.saveWorkflow).toHaveBeenCalledTimes(2)
    expect(status(wrapper)).toBe('saved')
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

  it('adds palette nodes after the selected node', async () => {
    const { wrapper } = await mountEditor()
    canvasSpy.editor!.removeEdge('e3')
    canvasSpy.editor!.setSelection(['test'])
    await flushPromises()
    await wrapper.get('[data-testid="palette-item-type:human_gate"]').trigger('click')
    const added = canvasSpy.editor!.graph.nodes.at(-1)!
    expect(added.type).toBe('human_gate')
    expect(canvasSpy.editor!.graph.edges.at(-1)).toMatchObject({ source: 'test', sourceHandle: 'fail', target: added.id })
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

  it('flushes the draft before publishing a version', async () => {
    const { wrapper } = await mountEditor()
    await wrapper.get('[data-testid="editor-name"]').setValue('to publish')
    await wrapper.get('[data-testid="editor-publish"]').trigger('click')
    await wrapper.get('[data-testid="editor-publish-confirm"]').trigger('click')
    await flushPromises()
    expect(apiMocks.saveWorkflow).toHaveBeenCalledTimes(1)
    expect(apiMocks.publishWorkflow).toHaveBeenCalledWith('wf-1')
    expect(apiMocks.saveWorkflow.mock.invocationCallOrder[0]!).toBeLessThan(apiMocks.publishWorkflow.mock.invocationCallOrder[0]!)
    expect(wrapper.text()).toContain(t('pages.workflowEditor.publish.published', { version: 4 }))
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
