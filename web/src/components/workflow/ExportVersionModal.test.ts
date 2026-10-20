// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'

const apiMocks = vi.hoisted(() => ({
  listWorkflowVersions: vi.fn(),
  exportWorkflowVersion: vi.fn(),
  getWorkflow: vi.fn(),
  getWorkflowVersionGraph: vi.fn(),
}))
const ioMocks = vi.hoisted(() => ({ downloadJson: vi.fn() }))

vi.mock('@/lib/run/workflowIO', async () => ({
  ...await vi.importActual<typeof import('@/lib/run/workflowIO')>('@/lib/run/workflowIO'),
  downloadJson: ioMocks.downloadJson,
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      listWorkflowVersions: apiMocks.listWorkflowVersions,
      exportWorkflowVersion: apiMocks.exportWorkflowVersion,
      getWorkflow: apiMocks.getWorkflow,
      getWorkflowVersionGraph: apiMocks.getWorkflowVersionGraph,
    },
  }
})

import ExportVersionModal from './ExportVersionModal.vue'

function mountModal(open = true) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(ExportVersionModal, {
    props: {
      open,
      workflowId: 'wf-1',
      workflowName: 'demo',
      description: '',
      needsRepo: false,
      status: 'published',
    },
    global: {
      plugins: [i18n],
      stubs: {
        AppButton: { template: '<button><slot /></button>' },
        AppModal: {
          props: ['open'],
          template: '<div v-if="open"><slot /><slot name="footer" /></div>',
        },
      },
    },
  })
}

beforeEach(() => {
  vi.clearAllMocks()
  apiMocks.listWorkflowVersions.mockResolvedValue([])
})

describe('ExportVersionModal', () => {
  it('loads versions when opened for published workflow', async () => {
    mountModal(true)
    await flushPromises()
    expect(apiMocks.listWorkflowVersions).toHaveBeenCalledWith(
      'wf-1',
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    )
  })

  it('aborts version list when modal closes', async () => {
    let resolveList!: (v: unknown[]) => void
    apiMocks.listWorkflowVersions.mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveList = resolve
        }),
    )
    const wrapper = mountModal(true)
    await wrapper.setProps({ open: false })
    resolveList([{ version: 1 }])
    await flushPromises()
    expect(wrapper.text()).not.toMatch(/已发布版本|Published/)
    wrapper.unmount()
  })

  it('renders export options', async () => {
    const wrapper = mountModal(true)
    await flushPromises()
    expect(wrapper.text().length).toBeGreaterThan(0)
    wrapper.unmount()
  })
  it('exports unsaved editor runtime defaults without including secret values', async () => {
    const wrapper = mountModal(true)
    await wrapper.setProps({
      localDraft: { nodes: [], edges: [] },
      defaults: { inheritWorkspace: false, variables: [{ name: 'TOKEN', type: 'string', secret: true, value: 'private-editor-secret' }], gateAuto: { enabled: true, agentName: 'reviewer', variable: 'review', prompt: 'Inspect', enabledMcps: [] } },
    })
    await flushPromises()
    await wrapper.findAll('button').at(-1)!.trigger('click')
    await flushPromises()
    const envelope = ioMocks.downloadJson.mock.calls[0][1]
    expect(envelope.defaults).toMatchObject({ inheritWorkspace: false, variables: [{ name: 'TOKEN', secret: true }], gateAuto: { agentName: 'reviewer' } })
    expect(envelope.defaults.variables[0]).not.toHaveProperty('value')
    expect(apiMocks.getWorkflow).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('exports the fresh saved head defaults instead of stale list metadata', async () => {
    apiMocks.getWorkflow.mockResolvedValue({ nodes: [], edges: [], defaults: { inheritWorkspace: false, variables: [{ name: 'branch', type: 'string', value: 'release' }] } })
    const wrapper = mountModal(true)
    await wrapper.setProps({ defaults: { inheritWorkspace: true, variables: [] } })
    await flushPromises()
    await wrapper.findAll('button').at(-1)!.trigger('click')
    await flushPromises()
    expect(ioMocks.downloadJson.mock.calls[0][1].defaults).toMatchObject({ inheritWorkspace: false, variables: [{ name: 'branch', value: 'release' }] })
    wrapper.unmount()
  })
})
