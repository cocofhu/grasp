// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import TeamBootstrapPanel from './TeamBootstrapPanel.vue'

const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  retry: vi.fn(),
}))

vi.mock('@/lib/api/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/api')>('@/lib/api/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      getAgentTeamBootstrap: mocks.get,
      retryAgentTeamBootstrap: mocks.retry,
    },
  }
})

const session = (status: string, extra: Record<string, unknown> = {}) => ({
  id: 's1',
  status,
  events: [],
  resources: [],
  createdAt: '',
  updatedAt: '',
  ...extra,
})

function mountPanel(sessionId = 's1') {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(TeamBootstrapPanel, {
    props: { sessionId },
    global: {
      plugins: [i18n],
      stubs: {
        AppButton: { template: '<button type="button" v-bind="$attrs"><slot /></button>' },
      },
    },
  })
}

describe('TeamBootstrapPanel interactions', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllMocks()
    mocks.get.mockResolvedValue(session('running'))
    mocks.retry.mockResolvedValue(session('running'))
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('polls resources, renders all event kinds, and completes ready flow', async () => {
    mocks.get
      .mockResolvedValueOnce(session('running', {
        events: [
          { kind: 'ok', message: 'created' },
          { kind: 'err', message: 'oops' },
          { kind: 'warn', message: 'careful' },
          { kind: 'mcp', message: 'mcp setup' },
          { kind: 'plain', message: 'plain' },
        ],
        resources: [{ name: 'PM', kind: 'agent', detail: 'lead' }],
      }))
      .mockResolvedValueOnce(session('ready', {
        pmAgent: 'PM',
        agentNames: ['PM', 'Dev'],
        resources: [{ name: 'PM', kind: 'agent' }],
      }))
    const w = mountPanel()
    await flushPromises()
    expect(w.text()).toContain('created')
    expect(w.text()).toContain('mcp setup')
    expect(w.text()).toContain('agent · lead')
    expect(w.emitted('refresh')).toHaveLength(1)

    await vi.advanceTimersByTimeAsync(800)
    await flushPromises()
    expect(w.emitted('selectPm')?.[0]).toEqual(['PM'])
    const buttons = w.findAll('button')
    await buttons.find((b) => b.text().includes('打开'))!.trigger('click')
    await buttons.find((b) => b.text().includes('完成'))!.trigger('click')
    expect(w.emitted('openPm')?.[0]).toEqual(['PM'])
    expect(w.emitted('done')).toBeTruthy()

    await vi.advanceTimersByTimeAsync(1600)
    expect(mocks.get).toHaveBeenCalledTimes(2)
    w.unmount()
  })

  it('shows polling errors and retries a failed bootstrap', async () => {
    mocks.get.mockRejectedValueOnce(new Error('network down'))
    const w = mountPanel()
    await flushPromises()
    expect(w.text()).toContain('network down')

    mocks.get.mockResolvedValueOnce(session('failed', { error: 'bootstrap failed' }))
    await vi.advanceTimersByTimeAsync(800)
    await flushPromises()
    expect(w.text()).toContain('bootstrap failed')

    let release!: (value: any) => void
    mocks.retry.mockReturnValueOnce(new Promise((resolve) => { release = resolve }))
    const retry = w.findAll('button').find((b) => b.text().includes('重试'))!
    await retry.trigger('click')
    await flushPromises()
    expect((retry.element as HTMLButtonElement).disabled).toBe(true)
    ;(w.vm as any).onRetry()
    expect(mocks.retry).toHaveBeenCalledTimes(1)
    release(session('running', { resources: [{ name: 'x', kind: 'agent' }] }))
    await flushPromises()
    expect(w.emitted('refresh')).toBeTruthy()

    mocks.get.mockResolvedValueOnce(session('failed'))
    await vi.advanceTimersByTimeAsync(800)
    await flushPromises()
    const keep = w.findAll('button').find((b) => b.text().includes('保留'))!
    await keep.trigger('click')
    expect(w.emitted('done')).toBeTruthy()
    w.unmount()
  })

  it('reports retry rejection and restarts polling when session id changes', async () => {
    mocks.get.mockResolvedValueOnce(session('failed'))
    const w = mountPanel()
    await flushPromises()
    mocks.retry.mockRejectedValueOnce(new Error('retry denied'))
    await (w.vm as any).onRetry()
    expect((w.vm as any).pollError).toBe('retry denied')
    expect((w.vm as any).retrying).toBe(false)

    mocks.get.mockResolvedValueOnce(session('running'))
    await w.setProps({ sessionId: 's2' })
    await flushPromises()
    expect(mocks.get).toHaveBeenLastCalledWith('s2')
    expect((w.vm as any).lineClass('ok')).toBe('text-ok')
    expect((w.vm as any).lineClass('err')).toBe('text-err')
    expect((w.vm as any).lineClass('warn')).toBe('text-warn')
    expect((w.vm as any).lineClass('mcp')).toBe('mcp-block')
    w.unmount()
  })

  it('shows pulling loading copy while sandboxStatus is pulling (g3.3)', async () => {
    mocks.get.mockResolvedValueOnce(session('running', {
      sandboxStatus: 'pulling',
      events: [{ kind: 'sys', message: 'pulling runtime image…' }],
      resources: [],
    }))
    const w = mountPanel()
    await flushPromises()
    expect(w.text()).toContain('正在拉取镜像')
    expect(w.find('[data-testid="team-bootstrap-pulling"]').exists()).toBe(true)
    expect(w.find('[data-testid="team-bootstrap-pulling"]').text()).toContain('运行时镜像')
    w.unmount()
  })

  it('shows pulling badge when session status is pulling', async () => {
    mocks.get.mockResolvedValueOnce(session('pulling', {
      events: [{ kind: 'sys', message: 'pulling runtime image…' }],
    }))
    const w = mountPanel()
    await flushPromises()
    expect(w.text()).toContain('正在拉取镜像…')
    expect(w.find('[data-testid="team-bootstrap-pulling"]').exists()).toBe(true)
    w.unmount()
  })
})
