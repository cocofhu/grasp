// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import { useAuth } from '@/lib/composables/useAuth'
import PmCronJobsPanel from './PmCronJobsPanel.vue'

const toastSuccess = vi.fn()
const toastError = vi.fn()

const apiMocks = vi.hoisted(() => ({
  listCronJobs: vi.fn(),
  patchCronJob: vi.fn(),
  deleteCronJob: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      listCronJobs: apiMocks.listCronJobs,
      patchCronJob: apiMocks.patchCronJob,
      deleteCronJob: apiMocks.deleteCronJob,
    },
  }
})

vi.mock('@/lib/composables/useToast', () => ({
  useToast: () => ({ success: toastSuccess, error: toastError }),
}))

const SAMPLE_JOB = {
  id: 'cron-1',
  agentName: 'agent-a',
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

function mountPanel() {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(PmCronJobsPanel, {
    global: {
      plugins: [i18n],
      stubs: {
        EmptyState: { props: ['title'], template: '<div data-testid="cron-empty">{{ title }}</div>' },
        StatusPill: true,
        Icon: true,
        AppButton: { template: '<button type="button" v-bind="$attrs"><slot /></button>' },
      },
    },
  })
}

describe('PmCronJobsPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    const job = { ...SAMPLE_JOB, deliverToChannel: false }
    apiMocks.listCronJobs.mockResolvedValue({ items: [job] })
    apiMocks.patchCronJob.mockResolvedValue({ ...job, deliverToChannel: true })
    apiMocks.deleteCronJob.mockResolvedValue({ status: 'deleted' })
    useAuth().clearUser()
  })

  it('loads workspace cron jobs on mount', async () => {
    useAuth().setUser({ username: 'admin', expiresAt: 't', isAdmin: true })
    const w = mountPanel()
    await flushPromises()
    expect(apiMocks.listCronJobs).toHaveBeenCalledWith()
    expect(w.find('[data-testid="workspace-cron-jobs-panel"]').exists()).toBe(true)
    expect(w.text()).toContain('每日汇报')
    expect(w.text()).toContain('agent-a')
    expect(w.text()).toContain('cron: 0 9 * * *')
    expect(w.text()).not.toMatch(/可查看与删除/)
    expect(w.text()).not.toContain('本项目下全部 Agent 的定时任务')
  })

  it('allows deliver toggle for non-admin without readonly banner', async () => {
    useAuth().setUser({ username: 'u', expiresAt: 't', isAdmin: false })
    const w = mountPanel()
    await flushPromises()
    const toggle = w.get('[data-testid="cron-deliver-toggle"]')
    expect((toggle.element as HTMLButtonElement).disabled).toBe(false)
    expect(toggle.attributes('role')).toBe('switch')
    expect(w.text()).not.toContain('任意已登录用户')
    expect(w.text()).not.toMatch(/修改需平台管理员|但不能修改渠道推送/)
    expect(w.find('.bg-amber-500\\/10').exists()).toBe(false)
    await toggle.trigger('click')
    await flushPromises()
    expect(apiMocks.patchCronJob).toHaveBeenCalledWith('cron-1', {
      deliverToChannel: true,
    })
  })

  it('deliver toggle on one row does not block another row', async () => {
    useAuth().setUser({ username: 'u', expiresAt: 't', isAdmin: false })
    const jobA = { ...SAMPLE_JOB, id: 'cron-1', name: '任务一', deliverToChannel: false }
    const jobB = { ...SAMPLE_JOB, id: 'cron-2', name: '任务二', deliverToChannel: false }
    apiMocks.listCronJobs.mockResolvedValue({ items: [jobA, jobB] })
    let releaseA!: (v: unknown) => void
    apiMocks.patchCronJob.mockImplementation((id: string) => {
      if (id === 'cron-1') return new Promise((resolve) => { releaseA = resolve })
      return Promise.resolve({ ...jobB, deliverToChannel: true })
    })
    const w = mountPanel()
    await flushPromises()
    const toggles = w.findAll('[data-testid="cron-deliver-toggle"]')
    expect(toggles).toHaveLength(2)
    await toggles[0].trigger('click')
    await flushPromises()
    expect((toggles[0].element as HTMLButtonElement).disabled).toBe(true)
    expect((toggles[1].element as HTMLButtonElement).disabled).toBe(false)
    await toggles[1].trigger('click')
    await flushPromises()
    expect(apiMocks.patchCronJob).toHaveBeenCalledWith('cron-1', { deliverToChannel: true })
    expect(apiMocks.patchCronJob).toHaveBeenCalledWith('cron-2', { deliverToChannel: true })
    releaseA!({ ...jobA, deliverToChannel: true })
    await flushPromises()
  })

  it('patches deliverToChannel for admin', async () => {
    useAuth().setUser({ username: 'admin', expiresAt: 't', isAdmin: true })
    const w = mountPanel()
    await flushPromises()
    const toggle = w.get('[data-testid="cron-deliver-toggle"]')
    expect((toggle.element as HTMLButtonElement).disabled).toBe(false)
    await toggle.trigger('click')
    await flushPromises()
    expect(apiMocks.patchCronJob).toHaveBeenCalledWith('cron-1', {
      deliverToChannel: true,
    })
  })

  it('cancel confirm does not call delete API', async () => {
    useAuth().setUser({ username: 'u', expiresAt: 't', isAdmin: false })
    const w = mountPanel()
    await flushPromises()
    vi.spyOn(window, 'confirm').mockReturnValue(false)
    await w.get('[data-testid="workspace-cron-delete"]').trigger('click')
    await flushPromises()
    expect(apiMocks.deleteCronJob).not.toHaveBeenCalled()
    expect(w.text()).toContain('每日汇报')
  })

  it('confirm delete calls API, refreshes list, and toasts success', async () => {
    useAuth().setUser({ username: 'u', expiresAt: 't', isAdmin: false })
    apiMocks.listCronJobs
      .mockResolvedValueOnce({ items: [{ ...SAMPLE_JOB }] })
      .mockResolvedValueOnce({ items: [] })
    const w = mountPanel()
    await flushPromises()
    expect(w.text()).toContain('每日汇报')

    vi.spyOn(window, 'confirm').mockReturnValue(true)
    await w.get('[data-testid="workspace-cron-delete"]').trigger('click')
    await flushPromises()

    expect(apiMocks.deleteCronJob).toHaveBeenCalledWith('cron-1')
    expect(apiMocks.listCronJobs).toHaveBeenCalledTimes(2)
    expect(toastSuccess).toHaveBeenCalled()
    expect(toastError).not.toHaveBeenCalled()
    expect(w.text()).not.toContain('每日汇报')
  })

  it('shows table skeleton before rows arrive', async () => {
    useAuth().setUser({ username: 'u', expiresAt: 't', isAdmin: false })
    let release!: (v: unknown) => void
    apiMocks.listCronJobs.mockReset()
    apiMocks.listCronJobs.mockReturnValue(new Promise((resolve) => { release = resolve }))
    const w = mountPanel()
    await flushPromises()
    expect(w.find('[data-testid="cron-table-skeleton"]').exists()).toBe(true)
    expect(w.text()).not.toContain('加载中')
    release!({ items: [{ ...SAMPLE_JOB }] })
    await flushPromises()
    expect(w.find('[data-testid="cron-table-skeleton"]').exists()).toBe(false)
    expect(w.text()).toContain('每日汇报')
  })

  it('delete keeps old row visible until reload finishes and shows deleting pending', async () => {
    useAuth().setUser({ username: 'u', expiresAt: 't', isAdmin: false })
    apiMocks.listCronJobs.mockResolvedValue({ items: [{ ...SAMPLE_JOB }] })
    let releaseDel!: () => void
    apiMocks.deleteCronJob.mockReturnValue(new Promise<void>((resolve) => { releaseDel = resolve }))
    const w = mountPanel()
    await flushPromises()
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    await w.get('[data-testid="workspace-cron-delete"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="workspace-cron-delete"]').text()).toBe('删除中…')
    expect((w.get('[data-testid="workspace-cron-delete"]').element as HTMLButtonElement).disabled).toBe(true)
    expect(w.text()).toContain('每日汇报')
    releaseDel!()
    apiMocks.listCronJobs.mockResolvedValue({ items: [{ ...SAMPLE_JOB }] })
    await flushPromises()
  })

  it('failure is distinct from empty', async () => {
    useAuth().setUser({ username: 'u', expiresAt: 't', isAdmin: false })
    apiMocks.listCronJobs.mockRejectedValue(Object.assign(new Error('down'), { status: 500 }))
    const w = mountPanel()
    await flushPromises()
    expect(w.find('[data-testid="cron-failed"]').exists()).toBe(true)
    expect(w.find('[data-testid="cron-empty"]').exists()).toBe(false)
    expect(w.text()).toContain('加载失败')
    expect(w.text()).toContain('重试')
  })
})
