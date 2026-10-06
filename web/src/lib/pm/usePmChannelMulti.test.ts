// @vitest-environment happy-dom
import { createApp, defineComponent, nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { Project } from '@/lib/shared/types'

const mocks = vi.hoisted(() => ({
  listProjectChannels: vi.fn(),
  getProject: vi.fn(),
  listProjectNotifyReceipts: vi.fn(),
  listPmThreads: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      listProjectChannels: mocks.listProjectChannels,
      getProject: mocks.getProject,
      listProjectNotifyReceipts: mocks.listProjectNotifyReceipts,
      listPmThreads: mocks.listPmThreads,
    },
  }
})

vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({ success: vi.fn(), error: vi.fn(), warn: vi.fn(), info: vi.fn() }),
}))

import { usePmChannelMulti } from './usePmChannelMulti'

const baseProject: Project = {
  id: 'proj-a',
  name: 'Project A',
  description: '',
  variables: [],
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
  notifyPolicy: { channelIds: [] },
}

function withPmChannelMulti(over: Partial<Parameters<typeof usePmChannelMulti>[0]> = {}) {
  let api!: ReturnType<typeof usePmChannelMulti>
  const emit = vi.fn()
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  const props = {
    projectId: 'proj-a',
    project: baseProject,
    pmLeaderAgent: 'pm-leader',
    ...over,
  }
  const Comp = defineComponent({
    setup() {
      api = usePmChannelMulti(props, emit)
      return () => null
    },
  })
  const app = createApp(Comp)
  app.use(i18n)
  app.mount(document.createElement('div'))
  return { api, app, emit, props }
}

describe('usePmChannelMulti', () => {
  beforeEach(() => {
    mocks.listProjectChannels.mockReset()
    mocks.getProject.mockReset()
    mocks.listProjectNotifyReceipts.mockReset()
    mocks.listPmThreads.mockReset()

    mocks.listProjectChannels.mockResolvedValue({
      items: [
        {
          id: 'ch-1',
          name: '主通道',
          type: 'feishu',
          agentName: 'pm-leader',
          enabled: true,
          isPrimary: true,
          config: { markdown: true, region: 'cn' },
          enabledMcps: ['pm-progress'],
        },
      ],
      freeAgents: ['pm-leader', 'agent-b'],
      secretsKeyConfigured: true,
    })
    mocks.getProject.mockResolvedValue(baseProject)
    mocks.listProjectNotifyReceipts.mockResolvedValue({ items: [] })
    mocks.listPmThreads.mockResolvedValue({ items: [] })
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('loads channels on mount and exposes form helpers', async () => {
    const { api, app } = withPmChannelMulti()
    await flushPromises()
    await nextTick()

    expect(api.loading.value).toBe(false)
    expect(api.channelList.value.length).toBe(1)
    expect(api.freeAgents.value).toContain('pm-leader')
    expect(api.hasPrimary.value).toBe(true)

    api.openEdit(api.channelList.value[0] as any)
    expect(api.tab.value).toBe('edit')
    api.cancelEdit()
    expect(api.tab.value).toBe('list')

    api.toggleChMcp('pm-agent-fs')
    api.setChannelType('qq')
    api.resetForm()
    api.setTargetComboOpen(false)
    api.setChannelType('feishu')
    await api.load()
    await flushPromises()
    api.openAdd()
    api.cancelEdit()
    api.askDelete(api.channelList.value[0] as any)
    api.deleteOpen.value = false
    await api.saveNotifyTargets()
    await api.toggleNotify()

    document.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))

    app.unmount()
  })

  it('blocks add when no free agents', async () => {
    mocks.listProjectChannels.mockResolvedValue({
      items: [],
      freeAgents: [],
      secretsKeyConfigured: false,
    })
    const { api, app } = withPmChannelMulti()
    await flushPromises()
    api.openAdd()
    expect(api.tab.value).toBe('list')
    app.unmount()
  })
})
