// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import { useAuth } from '@/lib/composables/useAuth'
import AgentDataPanel from './AgentDataPanel.vue'

const apiMocks = vi.hoisted(() => ({
  listAgentMemories: vi.fn(),
  upsertAgentMemory: vi.fn(),
  updateAgentMemory: vi.fn(),
  listAgentThreads: vi.fn(),
  listAgentThreadMessages: vi.fn(),
  deleteAgentThread: vi.fn(),
  listAgentCronJobs: vi.fn(),
  patchAgentCronJob: vi.fn(),
  deleteAgentCronJob: vi.fn(),
}))

const breakpointMocks = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-require-imports
  const vue = require('vue') as typeof import('vue')
  return { isMobile: vue.ref(false) }
})

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      listAgentMemories: apiMocks.listAgentMemories,
      upsertAgentMemory: apiMocks.upsertAgentMemory,
      updateAgentMemory: apiMocks.updateAgentMemory,
      listAgentThreads: apiMocks.listAgentThreads,
      listAgentThreadMessages: apiMocks.listAgentThreadMessages,
      deleteAgentThread: apiMocks.deleteAgentThread,
      listAgentCronJobs: apiMocks.listAgentCronJobs,
      patchAgentCronJob: apiMocks.patchAgentCronJob,
      deleteAgentCronJob: apiMocks.deleteAgentCronJob,
    },
  }
})

const toastSuccess = vi.fn()
const toastError = vi.fn()
vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({ success: toastSuccess, error: toastError }),
}))

vi.mock('@/lib/composables/useBreakpoint', () => ({
  useBreakpoint: () => ({ isMobile: breakpointMocks.isMobile }),
}))

const SAMPLE_JOB = {
  id: 'cron-1',
  agentName: 'demo-agent',
  threadId: 'th-1',
  name: '每日汇报',
  prompt: '汇报',
  scheduleKind: 'cron',
  scheduleExpr: '0 9 * * *',
  enabled: true,
  deliverToChannel: false,
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
}

const SAMPLE_THREAD = {
  id: 'th-user-1',
  userId: 'other',
  agentName: 'demo-agent',
  kind: 'user',
  title: '他人会话',
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
}

function mountPanel(props?: { subTab?: 'memory' | 'context' | 'jobs' }) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(AgentDataPanel, {
    props: { agentName: 'demo-agent', ...props },
    global: {
      plugins: [i18n],
      stubs: {
        Icon: true,
        AppButton: { template: '<button type="button" v-bind="$attrs"><slot /></button>' },
      },
    },
  })
}

async function openTab(wrapper: ReturnType<typeof mountPanel>, label: RegExp) {
  const btn = wrapper.findAll('button').find((b) => label.test(b.text()))
  expect(btn).toBeTruthy()
  await btn!.trigger('click')
  await flushPromises()
}

describe('AgentDataPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    breakpointMocks.isMobile.value = false
    apiMocks.listAgentMemories.mockResolvedValue({ items: [] })
    apiMocks.listAgentThreads.mockResolvedValue({ items: [], messageCounts: {} })
    apiMocks.listAgentThreadMessages.mockResolvedValue({ items: [], total: 0 })
    apiMocks.listAgentCronJobs.mockResolvedValue({ items: [{ ...SAMPLE_JOB }] })
    useAuth().clearUser()
  })

  it('non-admin: loads memories, threads, and can use job write controls', async () => {
    useAuth().setUser({ username: 'u', expiresAt: 't', isAdmin: false })
    apiMocks.listAgentMemories.mockResolvedValue({
      items: [
        {
          id: 'm1',
          agentName: 'demo-agent',
          title: '部署约定',
          content: '确认配置',
          source: 'user',
          updatedBy: 'u',
          createdAt: '2026-01-01T00:00:00Z',
          updatedAt: '2026-01-01T00:00:00Z',
        },
      ],
    })
    apiMocks.listAgentThreads.mockResolvedValue({
      items: [{ ...SAMPLE_THREAD }],
      messageCounts: { 'th-user-1': 1 },
    })
    apiMocks.listAgentThreadMessages.mockResolvedValue({
      items: [
        {
          id: 'msg-1',
          threadId: 'th-user-1',
          role: 'user',
          content: '你好',
          createdAt: '2026-01-01T00:00:00Z',
        },
      ],
      total: 1,
    })
    apiMocks.patchAgentCronJob.mockResolvedValue({ ...SAMPLE_JOB, enabled: false })
    apiMocks.deleteAgentCronJob.mockResolvedValue({ status: 'deleted' })

    const w = mountPanel()
    await flushPromises()

    expect(apiMocks.listAgentMemories).toHaveBeenCalledWith('demo-agent')
    expect(w.find('[data-testid="agent-data-lock-banner"]').exists()).toBe(false)
    expect(w.find('textarea').exists()).toBe(true)
    expect(w.text()).toContain('部署约定')
    expect(w.text()).toContain('更新者 u')

    await openTab(w, /上下文/)
    expect(apiMocks.listAgentThreads).toHaveBeenCalledWith('demo-agent')
    expect(w.find('[data-testid="agent-data-lock-banner"]').exists()).toBe(false)
    expect(w.text()).not.toContain('需要平台管理员权限')
    expect(w.text()).toContain('他人会话')

    const threadBtn = w.findAll('button').find((b) => b.text().includes('他人会话'))
    expect(threadBtn).toBeTruthy()
    await threadBtn!.trigger('click')
    await flushPromises()
    expect(apiMocks.listAgentThreadMessages).toHaveBeenCalledWith('demo-agent', 'th-user-1')
    expect(w.text()).toContain('你好')

    await openTab(w, /定时任务/)
    expect(apiMocks.listAgentCronJobs).toHaveBeenCalledWith('demo-agent')
    expect(w.text()).toContain('每日汇报')
    expect(w.text()).toContain('查看与管理')
    expect(w.text()).not.toContain('需要平台管理员权限')

    const enabled = w.get('[data-testid="agent-cron-enabled"]')
    const deliver = w.get('[data-testid="agent-cron-deliver"]')
    const del = w.get('[data-testid="agent-cron-delete"]')
    expect((enabled.element as HTMLButtonElement).disabled).toBe(false)
    expect((deliver.element as HTMLButtonElement).disabled).toBe(false)
    expect((del.element as HTMLButtonElement).disabled).toBe(false)
    expect(enabled.attributes('role')).toBe('switch')
    expect(deliver.attributes('role')).toBe('switch')
    expect(enabled.attributes('title')).toBeUndefined()
    expect(deliver.attributes('title')).toBeUndefined()
    expect(del.attributes('title')).toBeUndefined()

    await enabled.trigger('click')
    await flushPromises()
    expect(apiMocks.patchAgentCronJob).toHaveBeenCalledWith('demo-agent', 'cron-1', { enabled: false })

    vi.spyOn(window, 'confirm').mockReturnValue(true)
    await del.trigger('click')
    await flushPromises()
    expect(apiMocks.deleteAgentCronJob).toHaveBeenCalledWith('demo-agent', 'cron-1')
    expect(toastError).not.toHaveBeenCalled()
  })

  it('admin: loads memories and can use job write controls', async () => {
    useAuth().setUser({ username: 'admin', expiresAt: 't', isAdmin: true })
    apiMocks.listAgentMemories.mockResolvedValue({
      items: [
        {
          id: 'm1',
          agentName: 'demo-agent',
          title: '部署约定',
          content: '确认配置',
          source: 'user',
          updatedBy: 'admin',
          createdAt: '2026-01-01T00:00:00Z',
          updatedAt: '2026-01-01T00:00:00Z',
        },
      ],
    })
    apiMocks.patchAgentCronJob.mockResolvedValue({ ...SAMPLE_JOB, enabled: false })

    const w = mountPanel()
    await flushPromises()
    expect(apiMocks.listAgentMemories).toHaveBeenCalledWith('demo-agent')
    expect(w.find('[data-testid="agent-data-lock-banner"]').exists()).toBe(false)
    expect(w.text()).toContain('部署约定')
    expect(w.find('textarea').exists()).toBe(true)

    await openTab(w, /定时任务/)
    expect(apiMocks.listAgentCronJobs).toHaveBeenCalledWith('demo-agent')
    expect(w.text()).toContain('查看与管理')
    const enabled = w.get('[data-testid="agent-cron-enabled"]')
    expect((enabled.element as HTMLButtonElement).disabled).toBe(false)
    await enabled.trigger('click')
    await flushPromises()
    expect(apiMocks.patchAgentCronJob).toHaveBeenCalledWith('demo-agent', 'cron-1', { enabled: false })
  })

  it('first-load generic failure shows red card without toast', async () => {
    useAuth().setUser({ username: 'admin', expiresAt: 't', isAdmin: true })
    apiMocks.listAgentMemories.mockRejectedValue(new Error('admin required'))
    const w = mountPanel()
    await flushPromises()
    expect(w.find('[data-testid="agent-data-failed"]').exists()).toBe(true)
    expect(w.text()).toContain('加载失败')
    expect(w.text()).toContain('重试')
    expect(toastError).not.toHaveBeenCalled()
  })

  it('keepStale refresh failure toasts and keeps old list', async () => {
    useAuth().setUser({ username: 'admin', expiresAt: 't', isAdmin: true })
    apiMocks.listAgentMemories.mockResolvedValue({
      items: [
        {
          id: 'm1',
          agentName: 'demo-agent',
          title: '部署约定',
          content: '确认配置',
          source: 'user',
          updatedBy: 'admin',
          createdAt: '2026-01-01T00:00:00Z',
          updatedAt: '2026-01-01T00:00:00Z',
        },
      ],
    })
    const w = mountPanel()
    await flushPromises()
    expect(w.text()).toContain('部署约定')
    apiMocks.listAgentMemories.mockRejectedValue(new Error('admin required'))
    await w.setProps({ agentName: 'other-agent' })
    await flushPromises()
    expect(w.text()).toContain('部署约定')
    expect(w.find('[data-testid="agent-data-failed"]').exists()).toBe(false)
    expect(toastError).toHaveBeenCalledWith('需要平台管理员权限')
  })

  it('honors initial subTab prop', async () => {
    useAuth().setUser({ username: 'u', expiresAt: 't', isAdmin: false })
    const w = mountPanel({ subTab: 'jobs' })
    await flushPromises()
    expect(apiMocks.listAgentCronJobs).toHaveBeenCalledWith('demo-agent')
    expect(apiMocks.listAgentMemories).not.toHaveBeenCalled()
  })

  it('desktop jobs keep table layout', async () => {
    useAuth().setUser({ username: 'u', expiresAt: 't', isAdmin: false })
    const w = mountPanel({ subTab: 'jobs' })
    await flushPromises()
    expect(w.find('[data-testid="agent-cron-desktop-table"]').exists()).toBe(true)
    expect(w.find('[data-testid="agent-cron-mobile-cards"]').exists()).toBe(false)
  })

  it('mobile jobs use card stack with enable/deliver/delete mapped to API', async () => {
    useAuth().setUser({ username: 'u', expiresAt: 't', isAdmin: false })
    breakpointMocks.isMobile.value = true
    apiMocks.patchAgentCronJob.mockResolvedValue({ ...SAMPLE_JOB, deliverToChannel: true })
    apiMocks.deleteAgentCronJob.mockResolvedValue({ status: 'deleted' })

    const w = mountPanel({ subTab: 'jobs' })
    await flushPromises()

    expect(w.find('[data-testid="agent-cron-mobile-cards"]').exists()).toBe(true)
    expect(w.find('[data-testid="agent-cron-desktop-table"]').exists()).toBe(false)
    expect(w.findAll('[data-testid="agent-cron-card"]').length).toBe(1)
    expect(w.text()).toContain('每日汇报')
    expect(w.text()).toContain('推送到渠道')
    // No create-job control; hint may still mention creating via MCP
    expect(w.find('[data-testid="agent-cron-create"]').exists()).toBe(false)
    expect(w.findAll('button').some((b) => /^新建/.test(b.text()))).toBe(false)

    const deliver = w.get('[data-testid="agent-cron-deliver"]')
    await deliver.trigger('click')
    await flushPromises()
    expect(apiMocks.patchAgentCronJob).toHaveBeenCalledWith('demo-agent', 'cron-1', {
      deliverToChannel: true,
    })
    expect(toastSuccess).toHaveBeenCalled()

    const enabled = w.get('[data-testid="agent-cron-enabled"]')
    apiMocks.patchAgentCronJob.mockResolvedValueOnce({ ...SAMPLE_JOB, enabled: false })
    await enabled.trigger('click')
    await flushPromises()
    expect(apiMocks.patchAgentCronJob).toHaveBeenCalledWith('demo-agent', 'cron-1', { enabled: false })

    vi.spyOn(window, 'confirm').mockReturnValue(true)
    await w.get('[data-testid="agent-cron-delete"]').trigger('click')
    await flushPromises()
    expect(apiMocks.deleteAgentCronJob).toHaveBeenCalledWith('demo-agent', 'cron-1')
    expect(toastError).not.toHaveBeenCalled()
  })

  it('memory/context/jobs keep old list while refreshing and disable delete', async () => {
    useAuth().setUser({ username: 'u', expiresAt: 't', isAdmin: false })
    apiMocks.listAgentMemories.mockResolvedValue({
      items: [
        {
          id: 'm1',
          agentName: 'demo-agent',
          title: '部署约定',
          content: '确认配置',
          source: 'user',
          updatedBy: 'u',
          createdAt: '2026-01-01T00:00:00Z',
          updatedAt: '2026-01-01T00:00:00Z',
        },
      ],
    })
    apiMocks.listAgentThreads.mockResolvedValue({
      items: [{ ...SAMPLE_THREAD }],
      messageCounts: { 'th-user-1': 1 },
    })
    apiMocks.listAgentCronJobs.mockResolvedValue({ items: [{ ...SAMPLE_JOB }] })

    const w = mountPanel()
    await flushPromises()
    expect(w.text()).toContain('部署约定')

    let releaseMem!: (v: unknown) => void
    apiMocks.listAgentMemories.mockReturnValue(new Promise((resolve) => { releaseMem = resolve }))
    await w.setProps({ agentName: 'other-agent' })
    await flushPromises()
    expect(w.text()).toContain('部署约定')
    expect(w.find('[data-testid="agent-data-thin-progress"]').exists()).toBe(true)
    releaseMem!({ items: [{ id: 'm2', title: '新记忆', content: 'x', agentName: 'other-agent', source: 'user', createdAt: '', updatedAt: '' }] })
    await flushPromises()

    await openTab(w, /上下文/)
    expect(w.text()).toContain('他人会话')
    const threadDel = w.get('[data-testid="agent-thread-delete"]')
    expect((threadDel.element as HTMLButtonElement).disabled).toBe(false)

    await openTab(w, /定时任务/)
    const jobDel = w.get('[data-testid="agent-cron-delete"]')
    expect((jobDel.element as HTMLButtonElement).disabled).toBe(false)
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    let releaseJob!: () => void
    apiMocks.deleteAgentCronJob.mockReturnValue(new Promise<void>((resolve) => { releaseJob = resolve }))
    await jobDel.trigger('click')
    await flushPromises()
    expect((w.get('[data-testid="agent-cron-delete"]').element as HTMLButtonElement).disabled).toBe(true)
    expect(w.get('[data-testid="agent-cron-delete"]').text()).toBe('删除中…')
    releaseJob!()
    await flushPromises()
  })

  it('memory save button shows Saving… pending and is disabled', async () => {
    useAuth().setUser({ username: 'u', expiresAt: 't', isAdmin: false })
    apiMocks.listAgentMemories.mockResolvedValue({ items: [] })
    let release!: (v: unknown) => void
    apiMocks.upsertAgentMemory.mockReturnValue(new Promise((resolve) => { release = resolve }))
    const w = mountPanel()
    await flushPromises()
    await w.find('input').setValue('新记忆')
    const saveBtn = w.get('[data-testid="agent-data-mem-save"]')
    expect(saveBtn.text()).toBe('添加记忆')
    await saveBtn.trigger('click')
    await flushPromises()
    expect(saveBtn.text()).toBe('保存中…')
    expect((saveBtn.element as HTMLButtonElement).disabled).toBe(true)
    release!({
      id: 'm-new',
      agentName: 'demo-agent',
      title: '新记忆',
      content: '',
      source: 'user',
      createdAt: '2026-01-01T00:00:00Z',
      updatedAt: '2026-01-01T00:00:00Z',
    })
    apiMocks.listAgentMemories.mockResolvedValue({
      items: [{
        id: 'm-new',
        agentName: 'demo-agent',
        title: '新记忆',
        content: '',
        source: 'user',
        createdAt: '2026-01-01T00:00:00Z',
        updatedAt: '2026-01-01T00:00:00Z',
      }],
    })
    await flushPromises()
    expect(w.get('[data-testid="agent-data-mem-save"]').text()).toBe('添加记忆')
  })

  it('403 uses independent denied surface instead of toast-only', async () => {
    useAuth().setUser({ username: 'u', expiresAt: 't', isAdmin: false })
    apiMocks.listAgentMemories.mockRejectedValue(Object.assign(new Error('admin required'), { status: 403 }))
    const w = mountPanel()
    await flushPromises()
    expect(w.find('[data-testid="agent-data-denied"]').exists()).toBe(true)
    expect(w.text()).toContain('权限不足')
    expect(toastError).not.toHaveBeenCalled()
  })

  it('deepens unselected data tabs one step and keeps the selected accent', async () => {
    useAuth().setUser({ username: 'u', expiresAt: 't', isAdmin: false })
    const w = mountPanel()
    await flushPromises()
    const tab = (label: string) => w.findAll('button').find((b) => b.text() === label)
    const memory = tab('记忆')
    const context = tab('上下文')
    expect(memory).toBeTruthy()
    expect(context).toBeTruthy()
    expect(memory!.classes()).toContain('text-accent-2')
    expect(memory!.classes()).not.toContain('hover:text-txt')
    expect(memory!.classes()).not.toContain('hover:text-txt2')
    expect(context!.classes()).toEqual(expect.arrayContaining(['text-txt3', 'hover:text-txt2']))
    expect(context!.classes()).not.toContain('hover:text-txt')
    w.unmount()
  })
})
