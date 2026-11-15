// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import zh from '@/locales/zh-CN/pages.json'
import SoloStartupCard from '@/components/solo/SoloStartupCard.vue'
import { soloBootPhase } from '@/lib/solo/soloBoot'
import type { SoloSandboxInfo, SoloSession } from '@/lib/api/apiTypes'

const i18n = () => createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': zh } })
const T0 = '2026-11-15T00:00:00Z'

function session(sandbox: Partial<SoloSandboxInfo> | null, status: SoloSession['status'] = 'starting'): SoloSession {
  return {
    id: 's1', userId: 'u', title: '', agentName: 'Solo', status, restarts: 0, busy: false, waiting: 0,
    createdAt: T0, updatedAt: T0,
    sandbox: sandbox ? { id: 7, name: 'sb', status: 'creating', connected: false, hasCodeServer: false, createdAt: T0, ...sandbox } : undefined,
  }
}
const mountCard = (s: SoloSession) => mount(SoloStartupCard, { props: { session: s }, global: { plugins: [i18n()] } })
const states = (w: ReturnType<typeof mountCard>) =>
  w.findAll('[data-testid="solo-startup-step"]').map((s) => s.attributes('data-state'))

describe('soloBootPhase', () => {
  it('follows sandbox status, container status and session status', () => {
    expect(soloBootPhase(session(null))).toBe(0)
    expect(soloBootPhase(session({ status: 'pulling' }))).toBe(0)
    expect(soloBootPhase(session({ status: 'creating' }))).toBe(0)
    expect(soloBootPhase(session({ status: 'creating', containerStatus: 'created' }))).toBe(0)
    expect(soloBootPhase(session({ status: 'creating', containerStatus: 'running' }))).toBe(1)
    expect(soloBootPhase(session({ status: 'running', containerStatus: 'running' }))).toBe(2)
    expect(soloBootPhase(session({ status: 'running' }, 'running'))).toBe(3)
  })
})

describe('SoloStartupCard', () => {
  afterEach(() => vi.useRealTimers())

  it('uses the workflow boot checklist steps and advances only on real states', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(Date.parse(T0) + 42_000)
    const w = mountCard(session({ status: 'pulling' }))
    expect(w.findAll('[data-testid="solo-startup-step"]').map((s) => s.text())).toEqual(['启动沙箱', '准备工作区', '连接 Agent'])
    expect(w.get('[data-testid="solo-startup-title"]').text()).toBe('正在启动 Agent')
    expect(w.get('[data-testid="solo-startup-elapsed"]').text()).toBe('42s')
    expect(w.get('[data-testid="solo-startup-context"]').text()).toBe('正在拉取镜像…')
    expect(w.get('[data-testid="solo-startup-hint"]').text()).toContain('首次拉取镜像')
    expect(states(w)).toEqual(['running', 'pending', 'pending'])

    vi.advanceTimersByTime(60_000)
    await w.vm.$nextTick()
    expect(states(w)).toEqual(['running', 'pending', 'pending'])

    await w.setProps({ session: session({ status: 'creating', containerStatus: 'running' }) })
    expect(states(w)).toEqual(['done', 'running', 'pending'])
    expect(w.get('[data-testid="solo-startup-context"]').text()).toBe('正在准备工作环境…')
    expect(w.get('[data-testid="solo-startup-hint"]').text()).not.toContain('首次拉取镜像')

    await w.setProps({ session: session({ status: 'running', containerStatus: 'running' }) })
    expect(states(w)).toEqual(['done', 'done', 'running'])
    expect(w.get('[data-testid="solo-startup-context"]').text()).toBe('正在连接 Agent…')
  })

  it('does not step back when a poll loses the live container status', async () => {
    const w = mountCard(session({ status: 'creating', containerStatus: 'running' }))
    await w.setProps({ session: session({ status: 'creating' }) })
    expect(states(w)).toEqual(['done', 'running', 'pending'])
  })

  it('starts over for a new sandbox', async () => {
    const w = mountCard(session({ status: 'creating', containerStatus: 'running' }))
    await w.setProps({ session: session({ id: 8, status: 'pulling' }) })
    expect(states(w)).toEqual(['running', 'pending', 'pending'])
  })

  it('hides the elapsed time without a sandbox timestamp', () => {
    const w = mountCard(session({ createdAt: undefined }))
    expect(w.find('[data-testid="solo-startup-elapsed"]').exists()).toBe(false)
  })
})
