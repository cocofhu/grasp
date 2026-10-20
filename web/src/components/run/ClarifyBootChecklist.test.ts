// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import zh from '@/locales/zh-CN/pages.json'
import ClarifyBootLoader from '@/components/run/ClarifyBootLoader.vue'
import {
  beginBoot,
  bootKey,
  bootSteps,
  clearBootRecords,
  getBootRecord,
  markBootLive,
  observeBootSandbox,
} from '@/lib/run/bootRecord'

const i18n = () => createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': zh } })
const KEY = bootKey('r1', 'n1', 1)
const T0 = '2026-10-21T00:00:00Z'
const at = (sec: number) => new Date(Date.parse(T0) + sec * 1000).toISOString()

function mountLoader(phase: 'starting' | 'done', key: string | null = KEY) {
  return mount(ClarifyBootLoader, { props: { phase, variant: 'inline', bootKey: key }, global: { plugins: [i18n()] } })
}
const states = (w: ReturnType<typeof mountLoader>) =>
  w.findAll('[data-testid="clarify-boot-step"]').map((s) => s.attributes('data-state'))

describe('boot record', () => {
  beforeEach(() => clearBootRecords())

  it('is keyed per run, node and iteration', () => {
    beginBoot(bootKey('r1', 'n1', 1), T0)
    beginBoot(bootKey('r1', 'n1', 2), T0)
    observeBootSandbox(bootKey('r1', 'n1', 2), { status: 'running', updatedAt: at(5) })
    expect(getBootRecord(bootKey('r1', 'n1', 1))?.readyAt).toBeNull()
    expect(getBootRecord(bootKey('r1', 'n1', 2))?.readyAt).toBe(Date.parse(at(5)))
    expect(bootKey('r1', 'n1', undefined)).toBe('r1:n1:1')
  })

  it('ticks only on real states and times only server-stamped spans', () => {
    beginBoot(KEY, T0)
    expect(bootSteps(getBootRecord(KEY)).map((s) => s.state)).toEqual(['running', 'pending', 'pending'])
    observeBootSandbox(KEY, { status: 'pulling', updatedAt: at(1) })
    observeBootSandbox(KEY, { status: 'creating', updatedAt: at(4) })
    expect(bootSteps(getBootRecord(KEY)).map((s) => s.state)).toEqual(['running', 'pending', 'pending'])
    observeBootSandbox(KEY, { status: 'running', updatedAt: at(12) })
    expect(bootSteps(getBootRecord(KEY))).toEqual([
      { id: 'sandbox', state: 'done', sec: 12 },
      { id: 'workspace', state: 'done', sec: null },
      { id: 'agent', state: 'running', sec: null },
    ])
    markBootLive(KEY)
    expect(bootSteps(getBootRecord(KEY)).map((s) => s.state)).toEqual(['done', 'done', 'done'])
  })

  it('ignores a sandbox row left over from an earlier visit', () => {
    beginBoot(KEY, T0)
    observeBootSandbox(KEY, { status: 'running', updatedAt: at(-60) })
    expect(bootSteps(getBootRecord(KEY))[0]!.state).toBe('running')
  })

  it('shows no time when the ready timestamp is unknown', () => {
    beginBoot(KEY, null)
    observeBootSandbox(KEY, { status: 'running', updatedAt: at(9) })
    markBootLive(KEY)
    expect(bootSteps(getBootRecord(KEY)).map((s) => s.sec)).toEqual([null, null, null])
  })
})

describe('boot checklist', () => {
  beforeEach(() => clearBootRecords())
  afterEach(() => vi.useRealTimers())

  it('follows the record, not a timer', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(Date.parse(at(7)))
    beginBoot(KEY, T0)
    const w = mountLoader('starting')
    expect(states(w)).toEqual(['running', 'pending', 'pending'])
    expect(w.get('[data-testid="clarify-boot-elapsed"]').text()).toBe('7s')
    expect(w.get('[data-testid="clarify-boot-context"]').text()).toBe('正在准备工作环境…')
    vi.advanceTimersByTime(30_000)
    await nextTick()
    expect(states(w)).toEqual(['running', 'pending', 'pending'])
    observeBootSandbox(KEY, { status: 'pulling', updatedAt: at(2) })
    await nextTick()
    expect(w.get('[data-testid="clarify-boot-context"]').text()).toBe('正在拉取镜像…')
    observeBootSandbox(KEY, { status: 'running', updatedAt: at(13) })
    await nextTick()
    expect(states(w)).toEqual(['done', 'done', 'running'])
    const rows = w.findAll('[data-testid="clarify-boot-step"]').map((s) => s.text())
    expect(rows).toEqual(['启动沙箱13s', '准备工作区', '连接 Agent'])
    expect(w.get('[data-testid="clarify-boot-context"]').text()).toBe('正在连接 Agent…')
  })

  it('hides the elapsed counter without a node start time', () => {
    beginBoot(KEY, null)
    const w = mountLoader('starting')
    expect(w.find('[data-testid="clarify-boot-elapsed"]').exists()).toBe(false)
  })

  it('done row appears once live, folded, and expands to the steps', async () => {
    beginBoot(KEY, T0)
    observeBootSandbox(KEY, { status: 'running', updatedAt: at(13) })
    const w = mountLoader('done')
    expect(w.find('[data-testid="clarify-boot-done"]').exists()).toBe(false)
    markBootLive(KEY)
    await nextTick()
    expect(w.text()).toContain('环境已就绪')
    expect(w.get('[data-testid="clarify-boot-done-meta"]').text()).toBe('3 步 · 13s')
    expect(w.text()).not.toContain('准备工作区')
    await w.get('[data-testid="clarify-boot-done-head"]').trigger('click')
    expect(w.findAll('[data-testid="clarify-boot-done-step"]').map((s) => s.text())).toEqual(['启动沙箱13s', '准备工作区', '连接 Agent'])
  })

  it('done row drops the total when it is not server-stamped', async () => {
    beginBoot(KEY, null)
    markBootLive(KEY)
    const w = mountLoader('done')
    await nextTick()
    expect(w.get('[data-testid="clarify-boot-done-meta"]').text()).toBe('3 步')
  })
})
