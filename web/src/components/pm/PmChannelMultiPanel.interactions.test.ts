// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { Workspace } from '@/lib/shared/types'

const mocks = vi.hoisted(() => ({
  listChannels: vi.fn(), getWorkspace: vi.fn(), receipts: vi.fn(), threads: vi.fn(),
  create: vi.fn(), update: vi.fn(), del: vi.fn(), updateWorkspace: vi.fn(),
  success: vi.fn(), error: vi.fn(),
}))
vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return { ...actual, api: { ...actual.api,
    listChannels: mocks.listChannels, getWorkspace: mocks.getWorkspace,
    listNotifyReceipts: mocks.receipts, listPmThreads: mocks.threads,
    createChannel: mocks.create, updateChannel: mocks.update,
    deleteChannel: mocks.del, updateWorkspace: mocks.updateWorkspace,
  } }
})
vi.mock('@/lib/composables/useToast', () => ({ useToast: () => mocks }))
import PmChannelMultiPanel from './PmChannelMultiPanel.vue'

const workspace = { name: 'P', description: '', variables: [], notifyPolicy: { enabled: true, channelIds: ['c1'] } } as Workspace
const channels: any[] = [
  { id: 'c1', type: 'qq', name: 'QQ', appId: 'a1', enabled: true, isPrimary: true, agentName: 'agent1', appSecretSet: true, connectionState: 'connected', connectionDetail: 'healthy', config: { sandbox: true, intents: 3 }, enabledMcps: ['pm-progress'], cronDeliver: true, cronDeliverTarget: 'guild:1' },
  { id: 'c2', type: 'feishu', name: 'Feishu', appId: 'a2', enabled: false, isPrimary: false, agentName: 'agent2', connectionState: 'auth_failed', config: { region: 'lark' } },
  { id: 'c3', type: 'dingtalk', name: 'Ding', appId: 'a3', enabled: true, isPrimary: false, agentName: 'agent3', connectionState: 'disconnected', config: {} },
]
function mountPanel() {
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...common, ...pages } } })
  return mount(PmChannelMultiPanel, {
    props: { workspace, pmLeaderAgent: 'agent1' },
    attachTo: document.body,
    global: { plugins: [i18n], stubs: {
      Icon: true,
      AppButton: { template: '<button v-bind="$attrs"><slot/></button>' },
      AppSwitch: { props: ['modelValue'], emits: ['update:modelValue'], template: '<button role="switch" @click="$emit(\'update:modelValue\', !modelValue)"/>' },
    } },
  })
}

describe('PmChannelMultiPanel interactions', () => {
  beforeEach(() => {
    vi.clearAllMocks(); vi.stubGlobal('confirm', vi.fn(() => true))
    mocks.listChannels.mockResolvedValue({ items: channels, freeAgents: ['agent1', 'agent2'], secretsKeyConfigured: false })
    mocks.getWorkspace.mockResolvedValue(workspace)
    mocks.receipts.mockResolvedValue({ items: [{ runId: 'r1', kind: 'failed', status: 'error', error: 'boom', createdAt: '' }] })
    mocks.threads.mockResolvedValue({ items: [] })
    mocks.create.mockResolvedValue({}); mocks.update.mockResolvedValue({}); mocks.del.mockResolvedValue({})
    mocks.updateWorkspace.mockResolvedValue({ ...workspace, notifyPolicy: { enabled: true, channelIds: ['c2'] } })
  })

  it('renders all connection variants, edits types and builds provider inputs', async () => {
    const w = mountPanel()
    await flushPromises()
    const vm = w.vm as any
    expect(w.findAll('[data-testid="channel-row"]')).toHaveLength(3)
    expect(vm.connectionLabel(channels[0])).toBeTruthy()
    expect(vm.connectionClass(channels[1])).toBe('text-err')
    expect(vm.connectionDotClass(channels[2])).toBe('bg-warn')
    vm.openEdit(channels[0])
    await flushPromises()
    expect(vm.formConnectionHint().kind).toBe('ok')
    vm.cancelEdit(); vm.openAdd()
    vm.setChannelType('qq'); vm.chIntents = '7'; vm.chSandbox = true
    expect(vm.buildInput().config).toEqual(expect.objectContaining({ sandbox: true, markdown: true, intents: 7 }))
    vm.setChannelType('feishu'); vm.chRegion = 'lark'
    expect(vm.buildInput().config.region).toBe('lark')
    vm.setChannelType('wecom'); vm.setChannelType('dingtalk')
    expect(vm.defaultChannelName('dingtalk')).toBeTruthy()
    w.unmount()
  })

  it('validates, creates, updates and reports save failures', async () => {
    const w = mountPanel()
    await flushPromises()
    const vm = w.vm as any
    vm.openAdd(); vm.chName = ''; await vm.saveChannel()
    vm.chName = 'New'; vm.chAgent = ''; await vm.saveChannel()
    vm.chAgent = 'agent1'; vm.chAppId = ''; await vm.saveChannel()
    vm.chAppId = 'app'; vm.chAppSecret = ''; await vm.saveChannel()
    expect(mocks.error).toHaveBeenCalledTimes(4)
    vm.chAppSecret = 'secret'
    await vm.saveChannel()
    expect(mocks.create).toHaveBeenCalled()
    vm.openEdit(channels[0]); vm.chAgent = 'agent2'
    await vm.saveChannel()
    expect(mocks.update).toHaveBeenCalledWith('c1', expect.objectContaining({ syncPmLeader: true }))
    vm.openAdd(); vm.chName = 'Bad'; vm.chAgent = 'agent1'; vm.chAppId = 'x'; vm.chAppSecret = 's'
    mocks.create.mockRejectedValueOnce(new Error('save failed'))
    await vm.saveChannel()
    expect(vm.saveError).toBe('save failed')
    w.unmount()
  })

  it('handles recent-target keyboard selection, MCP toggles and outside clicks', async () => {
    mocks.threads.mockResolvedValue({ items: [{ id: 't1', channelType: 'qq', channelTarget: 'guild:123', title: 'Guild' }] })
    const w = mountPanel()
    await flushPromises()
    const vm = w.vm as any
    vm.chCronDeliver = true
    await vm.ensureRecentTargets()
    vm.recentTargets = [{ value: 'guild:123', label: 'Guild', unspoken: true }]
    vm.setTargetComboOpen(true)
    vm.onTargetInputKeydown(new KeyboardEvent('keydown', { key: 'ArrowDown' }))
    vm.onTargetInputKeydown(new KeyboardEvent('keydown', { key: 'ArrowUp' }))
    vm.onTargetInputKeydown(new KeyboardEvent('keydown', { key: 'Enter' }))
    expect(vm.chCronDeliverTarget).toBe('guild:123')
    vm.setTargetComboOpen(true)
    vm.targetComboRoot = document.createElement('div')
    vm.onTargetComboDocClick({ target: document.body } as MouseEvent)
    expect(vm.targetComboOpen).toBe(false)
    const before = vm.chEnabledMcps.length
    vm.toggleChMcp('pm-progress'); vm.toggleChMcp('pm-progress')
    expect(vm.chEnabledMcps).toHaveLength(before)
    w.unmount()
  })

  it('deletes primary/secondary channels and saves notification targets', async () => {
    const w = mountPanel()
    await flushPromises()
    const vm = w.vm as any
    vm.askDelete(channels[0])
    expect(vm.deleteOpen).toBe(true)
    vm.deleteMode = 'promote'; vm.deleteNewPrimaryId = ''
    await vm.confirmDeletePrimary()
    expect(mocks.del).not.toHaveBeenCalled()
    vm.deleteNewPrimaryId = 'c2'
    await vm.confirmDeletePrimary()
    expect(mocks.del).toHaveBeenCalledWith('c1', expect.objectContaining({ newPrimaryId: 'c2' }))
    vm.askDelete(channels[1])
    await flushPromises()
    expect(mocks.del).toHaveBeenCalledWith('c2', { confirmNoPrimary: false })
    vm.tab = 'notify'; vm.toggleNotify('c1'); vm.toggleNotify('c2')
    await vm.saveNotifyTargets()
    expect(w.emitted('workspace-updated')).toBeTruthy()
    mocks.updateWorkspace.mockRejectedValueOnce(new Error('notify failed'))
    await vm.saveNotifyTargets()
    expect(mocks.error).toHaveBeenCalledWith('notify failed')
    w.unmount()
  })

  it('recovers from load, receipt, target and delete failures', async () => {
    mocks.receipts.mockRejectedValueOnce(new Error('no receipts'))
    const w = mountPanel()
    await flushPromises()
    const vm = w.vm as any
    expect(vm.notifyReceipts).toEqual([])
    vm.chCronDeliver = true; mocks.threads.mockRejectedValueOnce(new Error('offline'))
    vm.clearRecentTargetsCache(); await vm.ensureRecentTargets()
    expect(vm.recentTargets).toEqual([])
    mocks.del.mockRejectedValueOnce(new Error('delete failed'))
    await vm.doDelete(channels[1], {})
    expect(mocks.error).toHaveBeenCalledWith('delete failed')
    mocks.listChannels.mockRejectedValueOnce(new Error('load failed'))
    await vm.load()
    expect(mocks.error).toHaveBeenCalledWith('load failed')
    w.unmount()
  })

  it('renders every provider edit form, notify receipts, and primary-delete choices', async () => {
    const w = mountPanel(); await flushPromises()
    const vm = w.vm as any
    for (const ch of channels) {
      vm.openEdit(ch)
      await w.vm.$nextTick()
      expect(w.find('[data-testid="channel-panel-edit"]').exists()).toBe(true)
      if (ch.type === 'qq') expect(w.find('[data-testid="channel-qq-only"]').exists()).toBe(true)
      if (ch.type === 'feishu') expect(w.find('[data-testid="channel-region"]').exists()).toBe(true)
      if (ch.type === 'dingtalk') expect(w.find('[data-testid="channel-dingtalk-hint"]').exists()).toBe(true)
    }
    vm.openAdd()
    for (const type of ['qq', 'wecom', 'feishu', 'dingtalk']) {
      vm.setChannelType(type)
      await w.vm.$nextTick()
      expect(vm.chType).toBe(type)
    }
    vm.chCronDeliver = true; vm.targetComboOpen = true
    vm.recentTargets = [{ value: 'guild:1', label: 'Guild', unspoken: true }]
    await w.vm.$nextTick()
    expect(w.find('[role="listbox"]').exists()).toBe(true)
    await w.find('[role="listbox"] button').trigger('click')
    vm.notifySelected = []; vm.tab = 'notify'; await w.vm.$nextTick()
    expect(w.find('[data-testid="channel-deliver-log"]').text()).toContain('boom')
    const checkbox = w.find('input[type="checkbox"]')
    await checkbox.trigger('change')
    vm.askDelete(channels[0]); await w.vm.$nextTick()
    expect(w.find('[data-testid="channel-delete-primary-modal"]').exists()).toBe(true)
    vm.deleteMode = 'none'; await w.vm.$nextTick()
    expect(w.find('[data-testid="channel-delete-primary-modal"]').text()).toBeTruthy()
    w.unmount()
  })

  it('renders list loading/empty states and blocks add without a free agent', async () => {
    const w = mountPanel(); await flushPromises()
    const vm = w.vm as any
    vm.loading = true; await w.vm.$nextTick()
    expect(w.text()).toContain('加载')
    vm.loading = false; vm.channelList = []; vm.freeAgents = []; vm.tab = 'list'
    await w.vm.$nextTick()
    vm.openAdd()
    expect(mocks.error).toHaveBeenCalled()
    vm.notifySelected = []; vm.tab = 'notify'; await w.vm.$nextTick()
    expect(w.find('[data-testid="notify-empty-hint"]').exists()).toBe(true)
    w.unmount()
  })

  it('drives rendered edit controls and provider selectors', async () => {
    const w = mountPanel(); await flushPromises()
    await w.findAll('[data-testid="channel-row"]')[1]!.findAll('button')[0]!.trigger('click')
    await w.vm.$nextTick()
    const vm = w.vm as any
    await w.get('#ch-multi-name').setValue('Edited')
    await w.get('#ch-multi-agent').setValue('agent1')
    await w.get('[data-testid="channel-appid"]').setValue('appid')
    await w.get('[data-testid="channel-secret"]').setValue('secret')
    await w.get('#ch-multi-timeout').setValue('20')
    await w.get('[data-testid="channel-region"]').setValue('cn')
    for (const sw of w.findAll('button[role="switch"]')) await sw.trigger('click')
    await w.get('[data-testid="channel-save"]').trigger('click')
    await flushPromises()
    expect(mocks.update).toHaveBeenCalledWith('c2', expect.objectContaining({
      name: 'Edited',
      agentName: 'agent1',
      enabled: true,
      enabledMcps: [],
      appId: 'appid',
      appSecret: 'secret',
      turnTimeoutSeconds: 20,
      cronDeliver: true,
      config: { region: 'cn', allowMemoryWrite: true, allowSchedulerWrite: true },
    }))
    await w.get('[data-testid="channel-add"]').trigger('click')
    await w.vm.$nextTick()
    for (const type of ['qq', 'wecom', 'feishu', 'dingtalk']) {
      await w.get(`[data-testid="channel-type-${type}"]`).trigger('click')
      await w.vm.$nextTick()
    }
    await w.get('[data-testid="channel-type-qq"]').trigger('click')
    await w.vm.$nextTick()
    await w.get('#ch-multi-intents').setValue('9')
    const role = w.get('[data-testid="channel-role"]')
    await role.setValue('primary')
    vm.chCronDeliver = true
    await w.vm.$nextTick()
    await w.get('[data-testid="cron-deliver-target-input"]').setValue('guild:9')
    await w.get('[data-testid="cron-deliver-target-input"]').trigger('focus')
    await w.get('[data-testid="cron-deliver-target-input"]').trigger('keydown', { key: 'Escape' })
    w.unmount()
  })
})
