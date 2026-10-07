// @vitest-environment happy-dom
import { createApp, computed, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { Run, WFNode, Workflow } from '@/lib/shared/types'
import { AUTO_CAPS, IMPLEMENT_CAPS } from '@/test/capsFixtures'

const toastWarn = vi.fn()
const isMobile = ref(false)

vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({ warn: toastWarn, success: vi.fn(), error: vi.fn(), info: vi.fn() }),
}))

vi.mock('@/lib/composables/useBreakpoint', () => ({
  useBreakpoint: () => ({ isMobile }),
}))

import { useRunDetailSelection } from './useRunDetailSelection'

function stubNode(partial: Partial<WFNode> & Pick<WFNode, 'id' | 'type'>): WFNode {
  return {
    label: partial.id,
    position: { x: 0, y: 0 },
    config: {},
    ...partial,
  }
}

describe('useRunDetailSelection', () => {
  beforeEach(() => {
    toastWarn.mockReset()
    isMobile.value = false
  })

  it('builds tabs for agent node and supports select / deep-link / mobile helpers', async () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-CN',
      messages: { 'zh-CN': { ...common, ...pages } },
    })
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/', component: { template: '<div />' } }],
    })
    await router.push({ path: '/', query: { node: 'a1', tab: 'output' } })
    await router.isReady()

    const run = ref({
      id: 'run-1',
      workflowId: 'wf',
      workflowName: 'wf',
      status: 'running',
      trigger: 'manual',
      startedAt: '2026-08-12T00:00:00Z',
      durationSec: 1,
      progress: 10,
      nodeRuns: { a1: { nodeId: 'a1', status: 'running' } },
      artifacts: [],
      nodes: [stubNode({ id: 'a1', type: 'agent' })],
    } as unknown as Run)
    const wf = ref({
      id: 'wf',
      name: 'wf',
      description: '',
      status: 'published',
      version: 1,
      updatedAt: '',
      needsRepo: false,
      nodes: [stubNode({ id: 'a1', type: 'agent' })],
      edges: [],
    } as Workflow)
    const selected = ref<string | null>('a1')
    const manual = ref(false)
    const selNode = computed(() => wf.value.nodes.find((n) => n.id === selected.value) || null)
    const selStatus = computed(() => run.value.nodeRuns[selected.value || '']?.status || 'pending')
    const selRun = computed(() => run.value.nodeRuns[selected.value || ''] || null)
    const runLoading = ref(false)

    let selection!: ReturnType<typeof useRunDetailSelection>
    const app = createApp({
      setup() {
        selection = useRunDetailSelection({
          run,
          wf,
          selected,
          manual,
          selNode,
          selStatus,
          selRun,
          runLoading,
        })
        return () => null
      },
    })
    app.use(i18n)
    app.use(router)
    app.mount(document.createElement('div'))

    expect(selection.hasLog.value).toBe(true)
    expect(selection.nodeTabs.value.some((t) => t.id === 'log')).toBe(true)
    expect(selection.nodeTabs.value.some((t) => t.id === 'sandbox')).toBe(true)

    selection.selectNode('a1')
    expect(manual.value).toBe(true)
    expect(selected.value).toBe('a1')

    expect(selection.applyOutputDeepLinkFocus()).toBe(true)
    expect(selection.nodeTab.value).toBe('output')

    isMobile.value = true
    selection.showMobileDetailPanel()
    expect(selection.mobileMainPanel.value).toBe('detail')
    selection.backToMobileTimeline()
    expect(selection.mobileMainPanel.value).toBe('timeline')
    expect(selection.timelineScrollToken.value).toBeGreaterThan(0)

    selection.onNodeTabDisabledClick('gate')
    expect(toastWarn).toHaveBeenCalled()

    app.unmount()
  })

  it('opens the product tab by default for a completed node that declares implementation_result', async () => {
    const selection = await mountSelection(
      stubNode({ id: 'impl', type: 'agent', label: '实现', caps: IMPLEMENT_CAPS }),
      'completed',
    )
    const ids = selection.nodeTabs.value.map((tab) => tab.id)
    expect(ids).toContain('product')
    expect(ids.indexOf('product')).toBeLessThan(ids.indexOf('output'))
    expect(selection.nodeTabs.value.find((tab) => tab.id === 'product')?.label).toBe('产物')
    expect(selection.nodeTab.value).toBe('product')
    selection.app.unmount()
  })

  it('keeps an agent without writes on overview, execution log, and sandbox log', async () => {
    const selection = await mountSelection(
      stubNode({ id: 'auto', type: 'agent', label: '自动', caps: AUTO_CAPS }),
      'completed',
    )
    expect(selection.nodeTabs.value.map((tab) => tab.id)).toEqual(['output', 'log', 'sandbox'])
    selection.app.unmount()
  })
})

async function mountSelection(node: WFNode, status: string) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/', component: { template: '<div />' } }],
  })
  await router.push('/')
  await router.isReady()

  const run = ref({
    id: 'run-1',
    workflowId: 'wf',
    workflowName: 'wf',
    status: 'running',
    trigger: 'manual',
    startedAt: '2026-08-12T00:00:00Z',
    durationSec: 1,
    progress: 10,
    nodeRuns: { [node.id]: { nodeId: node.id, status } },
    artifacts: [],
    nodes: [node],
  } as unknown as Run)
  const wf = ref({
    id: 'wf',
    name: 'wf',
    description: '',
    status: 'published',
    version: 1,
    updatedAt: '',
    needsRepo: false,
    nodes: [node],
    edges: [],
  } as Workflow)
  const selected = ref<string | null>(node.id)
  const manual = ref(false)
  const selNode = computed(() => wf.value.nodes.find((n) => n.id === selected.value) || null)
  const selStatus = computed(() => run.value.nodeRuns[selected.value || '']?.status || 'pending')
  const selRun = computed(() => run.value.nodeRuns[selected.value || ''] || null)
  const runLoading = ref(false)

  let selection!: ReturnType<typeof useRunDetailSelection>
  const app = createApp({
    setup() {
      selection = useRunDetailSelection({
        run,
        wf,
        selected,
        manual,
        selNode,
        selStatus,
        selRun,
        runLoading,
      })
      return () => null
    },
  })
  app.use(i18n)
  app.use(router)
  app.mount(document.createElement('div'))
  return { ...selection, app }
}
