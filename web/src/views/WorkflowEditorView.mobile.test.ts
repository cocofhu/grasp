// @vitest-environment happy-dom
import { defineComponent, h, nextTick, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'

const apiMocks = vi.hoisted(() => ({
  getWorkflow: vi.fn(),
  saveWorkflow: vi.fn(),
}))

const breakpointMocks = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const vue = require('vue') as typeof import('vue')
  return { isMobile: vue.ref(true) }
})

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      getWorkflow: apiMocks.getWorkflow,
      saveWorkflow: apiMocks.saveWorkflow,
    },
  }
})

vi.mock('@/lib/composables/useBreakpoint', () => ({
  useBreakpoint: () => ({ isMobile: breakpointMocks.isMobile }),
}))

vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({ success: vi.fn(), error: vi.fn() }),
}))

vi.mock('@/lib/composables/useProjectContext', () => ({
  readStoredProjectId: () => '',
}))

vi.mock('@/lib/run/useWorkflowImport', () => ({
  useWorkflowImport: () => ({
    fileInput: ref(null),
    showDiscardConfirm: ref(false),
    triggerImport: vi.fn(),
    onImportFile: vi.fn(),
    confirmDiscardImport: vi.fn(),
    cancelDiscardImport: vi.fn(),
  }),
}))

vi.mock('@/components/canvas/WorkflowCanvas.vue', () => ({
  default: { name: 'WorkflowCanvas', template: '<div data-testid="workflow-canvas-stub" />' },
}))
vi.mock('@/components/canvas/NodePalette.vue', () => ({
  default: { name: 'NodePalette', template: '<div data-testid="node-palette-stub" />' },
}))
vi.mock('@/components/canvas/NodeInspector.vue', () => ({
  default: { name: 'NodeInspector', template: '<div data-testid="node-inspector-stub" />' },
}))
vi.mock('@/components/canvas/EdgeInspector.vue', () => ({
  default: { name: 'EdgeInspector', template: '<div data-testid="edge-inspector-stub" />' },
}))

import WorkflowEditorView from './WorkflowEditorView.vue'

const MOCK_WF = {
  id: 'wf-1',
  name: 'demo-flow',
  description: '',
  status: 'draft',
  version: 1,
  updatedAt: '',
  nodes: [
    { id: 'n1', type: 'react', label: '需求澄清', position: { x: 0, y: 0 }, config: {} },
    { id: 'n2', type: 'gate', label: '人工确认', position: { x: 120, y: 0 }, config: {} },
  ],
  edges: [],
}

async function mountEditor() {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/projects', component: { render: () => h('div') } },
      { path: '/workflows/:id/edit', component: WorkflowEditorView },
    ],
  })
  await router.push('/workflows/wf-1/edit')
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
          AppButton: { template: '<button type="button"><slot /></button>' },
          StatusPill: true,
          AppDrawer: true,
          AppModal: true,
          RunLaunchModal: true,
          ExportVersionModal: true,
          WorkflowApiTab: true,
          WorkflowRunHistoryTab: true,
        },
      },
      attachTo: document.body,
    },
  )
  await flushPromises()
  await nextTick()
  return { wrapper, router }
}

describe('WorkflowEditorView mobile desktop-recommend', () => {
  beforeEach(() => {
    breakpointMocks.isMobile.value = true
    apiMocks.getWorkflow.mockResolvedValue(MOCK_WF)
  })

  afterEach(() => {
    vi.clearAllMocks()
    document.body.innerHTML = ''
  })

  it('shows desktop recommend first and expands a read-only node list without canvas', async () => {
    const { wrapper } = await mountEditor()
    expect(wrapper.find('[data-testid="workflow-editor-mobile"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('推荐在桌面编辑')
    expect(wrapper.find('[data-testid="workflow-canvas-stub"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="node-palette-stub"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="node-inspector-stub"]').exists()).toBe(false)

    await wrapper.get('[data-testid="workflow-editor-peek"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="workflow-editor-summary"]').exists()).toBe(true)
    expect(wrapper.findAll('[data-testid="workflow-editor-node-row"]')).toHaveLength(2)
    expect(wrapper.text()).toContain('react · 需求澄清')
    expect(wrapper.text()).toContain('gate · 人工确认')
    wrapper.unmount()
  })

  it('empty node list is readable and desktop still mounts the canvas', async () => {
    apiMocks.getWorkflow.mockResolvedValue({ ...MOCK_WF, nodes: [] })
    const { wrapper: mobile } = await mountEditor()
    await mobile.get('[data-testid="workflow-editor-peek"]').trigger('click')
    await flushPromises()
    expect(mobile.text()).toContain('当前流程没有节点')
    mobile.unmount()

    breakpointMocks.isMobile.value = false
    const { wrapper: desktop } = await mountEditor()
    expect(desktop.find('[data-testid="workflow-editor-mobile"]').exists()).toBe(false)
    expect(desktop.find('[data-testid="workflow-canvas-stub"]').exists()).toBe(true)
    expect(desktop.find('[data-testid="node-palette-stub"]').exists()).toBe(true)
    desktop.unmount()
  })
})

describe('WorkflowEditorView back navigation', () => {
  afterEach(() => {
    vi.clearAllMocks()
    document.body.innerHTML = ''
  })

  async function backTarget(isMobile: boolean, wf: Record<string, unknown>) {
    breakpointMocks.isMobile.value = isMobile
    apiMocks.getWorkflow.mockResolvedValue(wf)
    const { wrapper, router } = await mountEditor()
    const push = vi.spyOn(router, 'push')
    await wrapper.get('[data-testid="workflow-editor-back"]').trigger('click')
    await flushPromises()
    const target = push.mock.calls[0]?.[0]
    wrapper.unmount()
    return target
  }

  it('desktop back lands on the owning project pipelines Tab', async () => {
    const target = await backTarget(false, { ...MOCK_WF, projectId: 'proj-1' })
    expect(target).toBe('/projects/proj-1?tab=workflows')
  })

  it('mobile back lands on the owning project pipelines Tab', async () => {
    const target = await backTarget(true, { ...MOCK_WF, projectId: 'proj-1' })
    expect(target).toBe('/projects/proj-1?tab=workflows')
  })

  it('falls back to the project list when the workflow has no projectId', async () => {
    const target = await backTarget(false, { ...MOCK_WF })
    expect(target).toBe('/projects')
  })
})
