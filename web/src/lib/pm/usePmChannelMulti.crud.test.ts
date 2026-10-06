// @vitest-environment happy-dom
import { createApp, defineComponent, nextTick, reactive } from 'vue'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { ChannelConfig } from '@/lib/api/api'
import type { Project } from '@/lib/shared/types'

const mocks = vi.hoisted(() => ({
  listProjectChannels: vi.fn(),
  getProject: vi.fn(),
  listProjectNotifyReceipts: vi.fn(),
  listPmThreads: vi.fn(),
  createProjectChannel: vi.fn(),
  updateProjectChannel: vi.fn(),
  deleteProjectChannelById: vi.fn(),
  updateProject: vi.fn(),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
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
      createProjectChannel: mocks.createProjectChannel,
      updateProjectChannel: mocks.updateProjectChannel,
      deleteProjectChannelById: mocks.deleteProjectChannelById,
      updateProject: mocks.updateProject,
    },
  }
})

vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({
    success: mocks.toastSuccess,
    error: mocks.toastError,
    warn: vi.fn(),
    info: vi.fn(),
  }),
}))

import { usePmChannelMulti } from './usePmChannelMulti'

const baseProject = (over: Partial<Project> = {}): Project =>
  ({
    id: 'proj-a',
    name: 'Project A',
    description: '',
    variables: [],
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
    notifyPolicy: { channelIds: ['ch-1'] },
    ...over,
  }) as unknown as Project

const primary = (over: Partial<ChannelConfig> = {}): ChannelConfig =>
  ({
    id: 'ch-1',
    name: '主通道',
    type: 'feishu',
    agentName: 'pm-leader',
    enabled: true,
    isPrimary: true,
    appId: 'cli_1',
    appSecretSet: true,
    config: { markdown: true, region: 'lark', allowMemoryWrite: true, allowSchedulerWrite: true },
    enabledMcps: ['pm-progress'],
    cronDeliver: true,
    cronDeliverTarget: 'group:1',
    ...over,
  }) as unknown as ChannelConfig

const secondary = (over: Partial<ChannelConfig> = {}): ChannelConfig =>
  ({
    id: 'ch-2',
    name: 'QQ 通道',
    type: 'qq',
    agentName: 'agent-b',
    enabled: false,
    isPrimary: false,
    appId: '10001',
    config: { sandbox: true, markdown: false, intents: 4096 },
    ...over,
  }) as unknown as ChannelConfig

function withPmChannelMulti(over: Record<string, unknown> = {}) {
  let panel!: ReturnType<typeof usePmChannelMulti>
  const emit = vi.fn()
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  const props = reactive({
    projectId: 'proj-a',
    project: baseProject(),
    pmLeaderAgent: 'pm-leader',
    ...over,
  })
  const Comp = defineComponent({
    setup() {
      panel = usePmChannelMulti(props as never, emit as never)
      return () => null
    },
  })
  const app = createApp(Comp)
  app.use(i18n)
  app.mount(document.createElement('div'))
  return { panel, app, emit, props }
}

describe('usePmChannelMulti CRUD', () => {
  beforeEach(() => {
    for (const fn of Object.values(mocks)) (fn as ReturnType<typeof vi.fn>).mockReset()
    mocks.listProjectChannels.mockResolvedValue({
      items: [primary(), secondary()],
      freeAgents: ['pm-leader', 'agent-b'],
      secretsKeyConfigured: true,
    })
    mocks.getProject.mockResolvedValue(baseProject())
    mocks.listProjectNotifyReceipts.mockResolvedValue({
      items: [{ runId: 'run-1', kind: 'waiting_human', status: 'ok', createdAt: '2026-01-01T00:00:00Z' }],
    })
    mocks.listPmThreads.mockResolvedValue({
      items: [
        { userId: 'feishu:group:1', title: '群 1', updatedAt: '2026-01-02T00:00:00Z' },
        { userId: 'feishu:group:2', title: '', updatedAt: '2026-01-01T00:00:00Z', unspoken: true },
        // Different channel type / illegal scene are filtered out.
        { userId: 'qq:c2c:9', title: 'QQ', updatedAt: '2026-01-03T00:00:00Z' },
        { userId: 'feishu:room:3', title: '房间', updatedAt: '2026-01-03T00:00:00Z' },
      ],
    })
    mocks.createProjectChannel.mockResolvedValue({ id: 'ch-3' })
    mocks.updateProjectChannel.mockResolvedValue({ id: 'ch-1' })
    mocks.deleteProjectChannelById.mockResolvedValue({ status: 'ok' })
    mocks.updateProject.mockImplementation(async (_id: string, patch: Partial<Project>) => ({
      ...baseProject(),
      ...patch,
    }))
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('reports a failed load through the toast', async () => {
    mocks.listProjectChannels.mockRejectedValue(new Error('channels down'))
    const { panel, app } = withPmChannelMulti()
    await flushPromises()
    expect(panel.loading.value).toBe(false)
    expect(mocks.toastError).toHaveBeenCalledWith('channels down')
    app.unmount()
  })

  it('falls back to prop notify ids and tolerates missing receipts', async () => {
    mocks.getProject.mockRejectedValue(new Error('offline'))
    mocks.listProjectNotifyReceipts.mockRejectedValue(new Error('offline'))
    const { panel, app } = withPmChannelMulti()
    await flushPromises()
    expect(panel.notifySelected.value).toEqual(['ch-1'])
    expect(panel.notifyReceipts.value).toEqual([])
    app.unmount()

    // Neither source has a policy → empty selection.
    mocks.getProject.mockResolvedValue(baseProject({ notifyPolicy: undefined }))
    const bare = withPmChannelMulti({ project: baseProject({ notifyPolicy: undefined }) })
    await flushPromises()
    expect(bare.panel.notifySelected.value).toEqual([])
    bare.app.unmount()
  })

  it('applies an existing channel into the edit form', async () => {
    const { panel, app } = withPmChannelMulti()
    await flushPromises()

    panel.openEdit(primary())
    expect(panel.tab.value).toBe('edit')
    expect(panel.editingId.value).toBe('ch-1')
    expect(panel.editingChannel.value?.id).toBe('ch-1')
    expect(panel.chRegion.value).toBe('lark')
    expect(panel.chAppSecretSet.value).toBe(true)
    expect(panel.chCronDeliver.value).toBe(true)
    expect(panel.chEnabledMcps.value).toEqual(['pm-progress'])
    await flushPromises()
    expect(panel.recentTargets.value.length).toBeGreaterThan(0)

    panel.openEdit(secondary())
    expect(panel.chType.value).toBe('qq')
    expect(panel.chSandbox.value).toBe(true)
    expect(panel.chMarkdown.value).toBe(false)
    expect(panel.chIntents.value).toBe('4096')
    expect(panel.chEnabledMcps.value).toHaveLength(5)

    panel.openEdit(primary({ type: 'wecom', config: {}, enabledMcps: undefined, cronDeliver: false }))
    expect(panel.chType.value).toBe('wecom')
    expect(panel.chAllowMemoryWrite.value).toBe(false)
    panel.openEdit(primary({ type: 'dingtalk', config: { intents: '512' } }))
    expect(panel.chType.value).toBe('dingtalk')
    expect(panel.chIntents.value).toBe('512')
    panel.openEdit(primary({ type: 'unknown' as never, config: { intents: {} as never } }))
    expect(panel.chType.value).toBe('qq')
    expect(panel.chIntents.value).toBe('')

    app.unmount()
  })

  it('opens the add form seeded from the primary/free agents', async () => {
    const { panel, app } = withPmChannelMulti()
    await flushPromises()

    expect(panel.addButtonLabel.value).toBeTruthy()
    panel.openAdd()
    expect(panel.isNew.value).toBe(true)
    expect(panel.tab.value).toBe('edit')
    expect(panel.chType.value).toBe('feishu')
    expect(panel.chAgent.value).toBe('pm-leader')
    expect(panel.agentOptions.value).toEqual(['agent-b', 'pm-leader'])

    // New-channel type switch renames only default names.
    panel.setChannelType('qq')
    expect(panel.chName.value).toBe(panel.defaultChannelName('qq'))
    panel.setChannelType('wecom')
    expect(panel.chName.value).toBe(panel.defaultChannelName('wecom'))
    panel.setChannelType('dingtalk')
    expect(panel.chName.value).toBe(panel.defaultChannelName('dingtalk'))
    panel.chName.value = '自定义'
    panel.setChannelType('feishu')
    expect(panel.chName.value).toBe('自定义')

    // Cron delivery pre-warms the recent targets on type switch.
    panel.chCronDeliver.value = true
    await flushPromises()
    expect(panel.recentTargetsLoaded.value).toBe(true)
    panel.setChannelType('feishu')
    await flushPromises()
    expect(panel.recentTargetsLoaded.value).toBe(true)

    // Editing an existing channel never rewrites its name.
    panel.isNew.value = false
    panel.chName.value = '保持'
    panel.setChannelType('qq')
    expect(panel.chName.value).toBe('保持')

    app.unmount()
  })

  it('blocks the add form without a free agent', async () => {
    mocks.listProjectChannels.mockResolvedValue({ items: [], freeAgents: [], secretsKeyConfigured: false })
    const { panel, app } = withPmChannelMulti()
    await flushPromises()
    expect(panel.hasPrimary.value).toBe(false)
    panel.openAdd()
    expect(panel.tab.value).toBe('list')
    expect(mocks.toastError).toHaveBeenCalled()
    app.unmount()
  })

  it('validates the channel form before saving', async () => {
    const { panel, app } = withPmChannelMulti()
    await flushPromises()
    panel.openAdd()

    panel.chName.value = '  '
    await panel.saveChannel()
    expect(mocks.createProjectChannel).not.toHaveBeenCalled()

    panel.chName.value = '新通道'
    panel.chAgent.value = ' '
    await panel.saveChannel()
    expect(mocks.createProjectChannel).not.toHaveBeenCalled()

    panel.chAgent.value = 'agent-b'
    panel.chAppId.value = ''
    await panel.saveChannel()
    expect(mocks.createProjectChannel).not.toHaveBeenCalled()
    panel.setChannelType('wecom')
    await panel.saveChannel()
    expect(mocks.createProjectChannel).not.toHaveBeenCalled()

    panel.chAppId.value = 'bot-1'
    panel.chAppSecret.value = ' '
    await panel.saveChannel()
    expect(mocks.createProjectChannel).not.toHaveBeenCalled()

    panel.chAppSecret.value = 's3cret'
    await panel.saveChannel()
    await flushPromises()
    expect(mocks.createProjectChannel).toHaveBeenCalledWith('proj-a', expect.objectContaining({
      type: 'wecom',
      name: '新通道',
      agentName: 'agent-b',
      appId: 'bot-1',
    }))
    expect(panel.tab.value).toBe('list')
    expect(panel.editingId.value).toBeNull()

    app.unmount()
  })

  it('builds transport-specific config payloads', async () => {
    const { panel, app } = withPmChannelMulti()
    await flushPromises()
    panel.openAdd()

    panel.setChannelType('feishu')
    panel.chRegion.value = 'lark'
    expect(panel.buildInput().config).toMatchObject({ region: 'lark' })
    panel.chRegion.value = 'cn'
    expect(panel.buildInput().config).toMatchObject({ region: 'cn' })

    panel.setChannelType('qq')
    panel.chSandbox.value = true
    panel.chMarkdown.value = false
    panel.chIntents.value = '2048'
    expect(panel.buildInput().config).toMatchObject({ sandbox: true, markdown: false, intents: 2048 })
    panel.chIntents.value = 'not-a-number'
    expect(panel.buildInput().config).not.toHaveProperty('intents')
    panel.chIntents.value = '0'
    expect(panel.buildInput().config).not.toHaveProperty('intents')

    panel.setChannelType('dingtalk')
    expect(panel.buildInput().config).toEqual({ allowMemoryWrite: true, allowSchedulerWrite: true })
    panel.chTurnTimeout.value = 'abc' as unknown as number
    expect(panel.buildInput().turnTimeoutSeconds).toBe(0)

    // The MCP toggle keeps the canonical option order.
    panel.chEnabledMcps.value = []
    panel.toggleChMcp('pm-prd-manager')
    panel.toggleChMcp('pm-progress')
    expect(panel.chEnabledMcps.value).toEqual(['pm-progress', 'pm-prd-manager'])
    panel.toggleChMcp('pm-progress')
    expect(panel.chEnabledMcps.value).toEqual(['pm-prd-manager'])

    app.unmount()
  })

  it('confirms PmLeader sync when the primary channel rebinds its agent', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const { panel, app } = withPmChannelMulti()
    await flushPromises()

    panel.openEdit(primary())
    panel.chAgent.value = 'agent-b'
    await panel.saveChannel()
    await flushPromises()
    expect(confirm).toHaveBeenCalled()
    expect(mocks.updateProjectChannel).toHaveBeenCalledWith(
      'proj-a',
      'ch-1',
      expect.objectContaining({ syncPmLeader: true }),
    )

    // Declining leaves the flag unset.
    confirm.mockReturnValue(false)
    panel.openEdit(primary())
    panel.chAgent.value = 'agent-b'
    await panel.saveChannel()
    await flushPromises()
    expect(mocks.updateProjectChannel).toHaveBeenLastCalledWith(
      'proj-a',
      'ch-1',
      expect.not.objectContaining({ syncPmLeader: true }),
    )

    // Rebinding to the already-bound PmLeader agent needs no confirmation.
    confirm.mockClear()
    panel.openEdit(primary({ agentName: 'agent-b' }))
    panel.chAgent.value = 'pm-leader'
    await panel.saveChannel()
    await flushPromises()
    expect(confirm).not.toHaveBeenCalled()

    // Secondary channels never prompt.
    panel.openEdit(secondary())
    panel.chAgent.value = 'pm-leader'
    await panel.saveChannel()
    await flushPromises()
    expect(confirm).not.toHaveBeenCalled()

    mocks.updateProjectChannel.mockRejectedValueOnce(new Error('appId in use'))
    panel.openEdit(secondary())
    await panel.saveChannel()
    expect(panel.saveError.value).toBe('appId in use')
    expect(panel.saving.value).toBe(false)

    // Neither new nor editing → no write at all.
    panel.isNew.value = false
    panel.editingId.value = null
    mocks.updateProjectChannel.mockClear()
    mocks.createProjectChannel.mockClear()
    await panel.saveChannel()
    expect(mocks.updateProjectChannel).not.toHaveBeenCalled()
    expect(mocks.createProjectChannel).not.toHaveBeenCalled()

    app.unmount()
  })

  it('deletes secondary channels behind a native confirm', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const { panel, app } = withPmChannelMulti()
    await flushPromises()

    panel.askDelete(secondary())
    expect(mocks.deleteProjectChannelById).not.toHaveBeenCalled()

    confirm.mockReturnValue(true)
    panel.askDelete(secondary())
    await flushPromises()
    expect(mocks.deleteProjectChannelById).toHaveBeenCalledWith('proj-a', 'ch-2', {
      confirmNoPrimary: false,
    })

    // Deleting the channel currently open in the editor closes the form.
    panel.openEdit(secondary())
    panel.askDelete(secondary())
    await flushPromises()
    expect(panel.tab.value).toBe('list')

    mocks.deleteProjectChannelById.mockRejectedValueOnce(new Error('delete failed'))
    await panel.doDelete(secondary(), {})
    expect(mocks.toastError).toHaveBeenCalledWith('delete failed')
    expect(panel.saving.value).toBe(false)

    app.unmount()
  })

  it('promotes a replacement before deleting the primary channel', async () => {
    const { panel, app } = withPmChannelMulti()
    await flushPromises()

    panel.askDelete(primary())
    expect(panel.deleteOpen.value).toBe(true)
    expect(panel.deleteMode.value).toBe('promote')
    expect(panel.deleteNewPrimaryId.value).toBe('ch-2')

    panel.deleteNewPrimaryId.value = ''
    await panel.confirmDeletePrimary()
    expect(mocks.deleteProjectChannelById).not.toHaveBeenCalled()
    expect(panel.deleteOpen.value).toBe(true)

    panel.deleteNewPrimaryId.value = 'ch-2'
    await panel.confirmDeletePrimary()
    await flushPromises()
    expect(mocks.deleteProjectChannelById).toHaveBeenCalledWith('proj-a', 'ch-1', {
      newPrimaryId: 'ch-2',
      syncPmLeader: true,
    })
    expect(panel.deleteOpen.value).toBe(false)
    expect(panel.deleteTarget.value).toBeNull()

    // Last channel: no promotion available, confirm running without a primary.
    mocks.listProjectChannels.mockResolvedValue({
      items: [primary()],
      freeAgents: ['pm-leader'],
      secretsKeyConfigured: true,
    })
    await panel.load()
    panel.askDelete(primary())
    expect(panel.deleteMode.value).toBe('none')
    await panel.confirmDeletePrimary()
    await flushPromises()
    expect(mocks.deleteProjectChannelById).toHaveBeenLastCalledWith('proj-a', 'ch-1', {
      confirmNoPrimary: true,
    })

    // No target selected → nothing to confirm.
    await panel.confirmDeletePrimary()

    app.unmount()
  })

  it('labels channel connection state', async () => {
    const { panel, app } = withPmChannelMulti()
    await flushPromises()

    for (const state of ['connected', 'auth_failed', 'disconnected'] as const) {
      const ch = primary({ connectionState: state } as never)
      expect(panel.connectionLabel(ch)).toBeTruthy()
      expect(panel.connectionClass(ch)).toMatch(/text-/)
      expect(panel.connectionDotClass(ch)).toMatch(/bg-/)

      // formConnectionHint reads the loaded list entry, not the form snapshot.
      panel.channelList.value = [ch]
      panel.openEdit(ch)
      expect(panel.formConnectionHint()).not.toBeNull()

      const detailed = primary({ connectionState: state, connectionDetail: '细节' } as never)
      panel.channelList.value = [detailed]
      panel.openEdit(detailed)
      expect(panel.formConnectionHint()?.text).toBe('细节')
    }

    const online = primary({ connectionState: undefined, online: true } as never)
    expect(panel.connectionClass(online)).toBe('text-ok')
    expect(panel.connectionDotClass(online)).toBe('bg-ok')
    const offline = primary({ connectionState: undefined, online: false } as never)
    expect(panel.connectionClass(offline)).toBe('text-txt3')
    expect(panel.connectionDotClass(offline)).toBe('bg-txt3')
    expect(panel.connectionLabel(offline)).toBeTruthy()

    const unknown = primary({ connectionState: undefined, online: undefined } as never)
    expect(panel.connectionLabel(unknown)).toBeTruthy()
    expect(panel.connectionClass(unknown)).toBe('text-txt3')
    const disabled = primary({ connectionState: undefined, online: undefined, enabled: false } as never)
    expect(panel.connectionLabel(disabled)).toBeTruthy()

    panel.channelList.value = [unknown]
    panel.openEdit(unknown)
    expect(panel.formConnectionHint()).toBeNull()
    panel.cancelEdit()
    expect(panel.formConnectionHint()).toBeNull()

    app.unmount()
  })

  it('drives the recent push-target combobox', async () => {
    const { panel, app } = withPmChannelMulti()
    await flushPromises()
    panel.openAdd()

    // Closed while cron delivery is off: no fetch.
    panel.setTargetComboOpen(true)
    await flushPromises()
    expect(mocks.listPmThreads).not.toHaveBeenCalled()

    panel.chCronDeliver.value = true
    await flushPromises()
    expect(mocks.listPmThreads).toHaveBeenCalledTimes(1)
    expect(panel.recentTargets.value.length).toBeGreaterThan(0)
    // Already loaded → no repeat request.
    await panel.ensureRecentTargets()
    expect(mocks.listPmThreads).toHaveBeenCalledTimes(1)
    expect(panel.pushTargetPrimaryLabel(panel.recentTargets.value[0]!)).toBeTruthy()

    panel.setTargetComboOpen(true)
    const opts = panel.recentTargets.value
    panel.onTargetInputKeydown(new KeyboardEvent('keydown', { key: 'ArrowDown' }))
    expect(panel.targetActiveIndex.value).toBe(0)
    panel.onTargetInputKeydown(new KeyboardEvent('keydown', { key: 'ArrowDown' }))
    expect(panel.targetActiveIndex.value).toBe(1 % opts.length)
    panel.onTargetInputKeydown(new KeyboardEvent('keydown', { key: 'ArrowUp' }))
    expect(panel.targetActiveIndex.value).toBe(0)
    panel.onTargetInputKeydown(new KeyboardEvent('keydown', { key: 'ArrowUp' }))
    expect(panel.targetActiveIndex.value).toBe(opts.length - 1)

    panel.onTargetInputKeydown(new KeyboardEvent('keydown', { key: 'Enter' }))
    expect(panel.chCronDeliverTarget.value).toBe(opts[opts.length - 1]!.value)
    expect(panel.targetComboOpen.value).toBe(false)

    // Arrow keys re-open a closed combobox.
    panel.onTargetInputKeydown(new KeyboardEvent('keydown', { key: 'ArrowDown' }))
    expect(panel.targetComboOpen.value).toBe(true)
    panel.setTargetComboOpen(false)
    panel.onTargetInputKeydown(new KeyboardEvent('keydown', { key: 'ArrowUp' }))
    expect(panel.targetComboOpen.value).toBe(true)

    // Enter without an active row and Escape.
    panel.targetActiveIndex.value = -1
    panel.onTargetInputKeydown(new KeyboardEvent('keydown', { key: 'Enter' }))
    expect(panel.targetComboOpen.value).toBe(true)
    panel.onTargetInputKeydown(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(panel.targetComboOpen.value).toBe(false)
    panel.onTargetInputKeydown(new KeyboardEvent('keydown', { key: 'a' }))

    // An empty recent list leaves the pointer alone.
    panel.recentTargets.value = []
    panel.setTargetComboOpen(true)
    panel.onTargetInputKeydown(new KeyboardEvent('keydown', { key: 'ArrowDown' }))
    expect(panel.targetActiveIndex.value).toBe(-1)
    panel.onTargetInputKeydown(new KeyboardEvent('keydown', { key: 'ArrowUp' }))
    expect(panel.targetActiveIndex.value).toBe(-1)

    panel.selectPushTarget('group:9')
    expect(panel.chCronDeliverTarget.value).toBe('group:9')

    // Turning cron delivery off drops the cache.
    panel.chCronDeliver.value = false
    await nextTick()
    expect(panel.recentTargetsLoaded.value).toBe(false)

    // A failing thread list still marks the cache as resolved.
    mocks.listPmThreads.mockRejectedValueOnce(new Error('offline'))
    panel.chCronDeliver.value = true
    await flushPromises()
    expect(panel.recentTargets.value).toEqual([])
    expect(panel.recentTargetsLoaded.value).toBe(true)

    app.unmount()
  })

  it('closes the combobox on an outside mousedown only', async () => {
    const { panel, app } = withPmChannelMulti()
    await flushPromises()
    const root = document.createElement('div')
    const inside = document.createElement('input')
    root.appendChild(inside)
    document.body.appendChild(root)
    panel.targetComboRoot.value = root

    panel.onTargetComboDocClick({ target: document.body } as unknown as MouseEvent)
    panel.targetComboOpen.value = true
    panel.onTargetComboDocClick({ target: inside } as unknown as MouseEvent)
    expect(panel.targetComboOpen.value).toBe(true)
    panel.onTargetComboDocClick({ target: document.body } as unknown as MouseEvent)
    expect(panel.targetComboOpen.value).toBe(false)

    panel.targetComboRoot.value = null
    panel.targetComboOpen.value = true
    panel.onTargetComboDocClick({ target: document.body } as unknown as MouseEvent)
    expect(panel.targetComboOpen.value).toBe(true)

    root.remove()
    app.unmount()
  })

  it('saves the project notify targets', async () => {
    const { panel, app, emit } = withPmChannelMulti()
    await flushPromises()

    panel.toggleNotify('ch-2')
    expect(panel.notifySelected.value).toEqual(['ch-1', 'ch-2'])
    panel.toggleNotify('ch-1')
    expect(panel.notifySelected.value).toEqual(['ch-2'])

    mocks.updateProject.mockResolvedValueOnce(
      baseProject({ notifyPolicy: { enabled: true, defaultEvents: ['failed'], channelIds: ['ch-2'] } }),
    )
    await panel.saveNotifyTargets()
    expect(mocks.updateProject).toHaveBeenCalledWith('proj-a', {
      notifyPolicy: expect.objectContaining({ channelIds: ['ch-2'] }),
    })
    expect(emit).toHaveBeenCalledWith('project-updated', expect.objectContaining({ id: 'proj-a' }))
    expect(panel.notifySelected.value).toEqual(['ch-2'])
    expect(panel.notifySaving.value).toBe(false)

    mocks.updateProject.mockResolvedValueOnce(baseProject({ notifyPolicy: undefined }))
    await panel.saveNotifyTargets()
    expect(panel.notifySelected.value).toEqual([])

    mocks.updateProject.mockRejectedValueOnce(new Error('notify failed'))
    await panel.saveNotifyTargets()
    expect(mocks.toastError).toHaveBeenCalledWith('notify failed')

    app.unmount()
  })

  it('reloads when the bound project changes', async () => {
    const { panel, app, props } = withPmChannelMulti()
    await flushPromises()
    expect(mocks.listProjectChannels).toHaveBeenCalledTimes(1)

    props.projectId = 'proj-b'
    await flushPromises()
    expect(mocks.listProjectChannels).toHaveBeenLastCalledWith('proj-b')
    expect(panel.recentTargetsLoaded.value).toBe(false)

    app.unmount()
  })
})
