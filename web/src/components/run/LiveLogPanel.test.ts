// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import pages from '@/locales/zh-CN/pages.json'
import type { AcpEvent, McpCall, NodeRunStatus } from '@/lib/shared/types'
import { BOOT_STAGE_TIMEOUT_MS } from '@/lib/run/liveLogBootPhase'
import LiveLogPanel from './LiveLogPanel.vue'

function mountPanel(props: {
  events?: AcpEvent[]
  live?: boolean
  busy?: boolean
  status?: NodeRunStatus
  mcpCalls?: McpCall[]
  hasMore?: boolean
  showConsole?: boolean
  sandboxStatus?: string | null
  sandboxContainerStatus?: string | null
  bootSession?: {
    confirmedPhase: number | null
    stageEnteredAt: number | null
    timedOut: boolean
  } | null
  rehydrateStatus?: 'idle' | 'loading' | 'ready' | 'error'
} = {}) {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...pages } },
  })
  return mount(LiveLogPanel, {
    props: {
      events: props.events ?? [],
      live: props.live ?? false,
      busy: props.busy,
      status: props.status,
      mcpCalls: props.mcpCalls,
      hasMore: props.hasMore ?? false,
      showConsole: props.showConsole ?? false,
      sandboxStatus: props.sandboxStatus,
      sandboxContainerStatus: props.sandboxContainerStatus,
      bootSession: props.bootSession,
      // Boot empty-wait requires a successful rehydrate; default ready for boot tests.
      rehydrateStatus: props.rehydrateStatus ?? (props.status === 'running' ? 'ready' : 'idle'),
    },
    global: {
      plugins: [i18n],
      stubs: { Icon: true, AcpStatusPill: false },
    },
  })
}

describe('LiveLogPanel', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('shows pending empty hint', () => {
    const wrapper = mountPanel({ status: 'pending' })
    expect(wrapper.text()).toMatch(/等待|尚未/)
    expect(wrapper.find('[data-testid="live-log-boot"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('renders ACP events', () => {
    const events: AcpEvent[] = [
      { kind: 'message', title: 'Hello agent', t: 0 },
      { kind: 'tool_call', title: 'write_artifact', t: 1, status: 'completed' },
    ]
    const wrapper = mountPanel({ events })
    expect(wrapper.text()).toContain('Hello agent')
    expect(wrapper.text()).toContain('write_artifact')
    wrapper.unmount()
  })

  it('toggles MCP call details', async () => {
    const mcpCalls: McpCall[] = [
      { at: '2026-07-18T00:00:00Z', tool: 'read_artifact', args: '{"name":"plan.json"}', result: 'ok', isError: false },
    ]
    const wrapper = mountPanel({ mcpCalls })
    const toggle = wrapper.find('button')
    expect(toggle.exists()).toBe(true)
    await toggle.trigger('click')
    expect(wrapper.text()).toContain('read_artifact')
    expect(wrapper.text()).toContain('plan.json')
    wrapper.unmount()
  })

  it('emits load-earlier and console-click', async () => {
    const wrapper = mountPanel({ hasMore: true, showConsole: true, live: true, busy: true })
    await wrapper.find('button.text-accent-2, button.text-xs').trigger('click')
    expect(wrapper.emitted('load-earlier')).toBeTruthy()
    const consoleBtn = wrapper.findAll('button').find((b) => b.text().includes('控制台') || b.text().includes('Console'))
    expect(consoleBtn).toBeTruthy()
    await consoleBtn!.trigger('click')
    expect(wrapper.emitted('console-click')).toBeTruthy()
    wrapper.unmount()
  })

  it('shows the latest sandbox heartbeat in the footer, not as a log row', () => {
    const msg: AcpEvent = { t: 0, kind: 'message', text: 'building' }
    const beat = (idleSec: number, active: boolean): AcpEvent => ({
      t: 1, kind: 'liveness',
      liveness: { active, cpuMs: 2500, ioBytes: 8192, idleSec, limitSec: 1200, lastTool: 'npm test' },
    })
    const wrapper = mountPanel({ live: true, busy: true, status: 'running', events: [msg, beat(0, true)] })
    const footer = wrapper.find('[data-testid="live-log-liveness"]')
    expect(footer.text()).toBe('Agent 活动中 · CPU 2.5s · IO 8 KiB')
    expect(footer.attributes('title')).toBe('npm test')
    expect(wrapper.text()).not.toContain('liveness')
    wrapper.unmount()

    const quiet = mountPanel({ live: true, busy: true, status: 'running', events: [msg, beat(0, true), beat(600, false)] })
    const q = quiet.find('[data-testid="live-log-liveness"]')
    expect(q.text()).toBe('Agent 已 10 分钟无活动（20 分钟后判定卡住）')
    expect(q.classes()).toContain('text-warn')
    quiet.unmount()

    const done = mountPanel({ live: false, status: 'success', events: [msg, beat(0, true)] })
    expect(done.find('[data-testid="live-log-liveness"]').exists()).toBe(false)
    done.unmount()
  })

  it('shows three boot stages while running with empty timeline', () => {
    const wrapper = mountPanel({
      status: 'running',
      live: true,
      sandboxStatus: 'creating',
      sandboxContainerStatus: 'running',
    })
    expect(wrapper.find('[data-testid="live-log-boot"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="boot-stage-creating"]').attributes('data-state')).toBe('done')
    expect(wrapper.find('[data-testid="boot-stage-acp_ready"]').attributes('data-state')).toBe('active')
    expect(wrapper.find('[data-testid="boot-stage-first_event"]').attributes('data-state')).toBe('pending')
    expect(wrapper.text()).toContain('沙箱创建中')
    expect(wrapper.text()).toContain('正在连接 Agent')
    expect(wrapper.text()).toContain('等待首个事件')
    expect(wrapper.text()).not.toContain('正在启动沙箱 / 等待 Agent 首个事件')
    wrapper.unmount()
  })

  it('shows pulling stage and does not arm 120s dwell timeout (g3.2 g3.4)', async () => {
    const wrapper = mountPanel({
      status: 'running',
      live: true,
      sandboxStatus: 'pulling',
    })
    expect(wrapper.find('[data-testid="boot-stage-pulling"]').attributes('data-state')).toBe('active')
    expect(wrapper.text()).toContain('正在拉取镜像')
    await vi.advanceTimersByTimeAsync(BOOT_STAGE_TIMEOUT_MS)
    await flushPromises()
    expect(wrapper.find('[data-testid="boot-timeout-banner"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="boot-stage-pulling"]').attributes('data-state')).toBe('active')
    wrapper.unmount()
  })

  it('hides boot progress after first event', async () => {
    const wrapper = mountPanel({
      status: 'running',
      live: true,
      sandboxStatus: 'running',
      sandboxContainerStatus: 'running',
    })
    expect(wrapper.find('[data-testid="live-log-boot"]').exists()).toBe(true)
    await wrapper.setProps({
      events: [{ kind: 'message', title: 'first', t: 1 } as AcpEvent],
    })
    expect(wrapper.find('[data-testid="live-log-boot"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('first')
    wrapper.unmount()
  })

  it('marks active stage timed out and emits go-sandbox-log', async () => {
    const wrapper = mountPanel({
      status: 'running',
      live: true,
      sandboxStatus: 'creating',
      sandboxContainerStatus: 'running',
    })
    expect(wrapper.find('[data-testid="boot-timeout-banner"]').exists()).toBe(false)
    await vi.advanceTimersByTimeAsync(BOOT_STAGE_TIMEOUT_MS)
    await flushPromises()
    expect(wrapper.find('[data-testid="boot-stage-acp_ready"]').attributes('data-state')).toBe('timeout')
    expect(wrapper.find('[data-testid="boot-timeout-banner"]').exists()).toBe(true)
    await wrapper.find('[data-testid="go-sandbox-log"]').trigger('click')
    expect(wrapper.emitted('go-sandbox-log')).toBeTruthy()
    wrapper.unmount()
  })

  it('shows focusable snapshot chip with tooltip when not live', async () => {
    const events: AcpEvent[] = [{ kind: 'message', title: 'done event', t: 0 }]
    const wrapper = mountPanel({ events, live: false, status: 'completed' })
    const chip = wrapper.findAll('button').find((b) => b.text().includes('快照'))
    expect(chip).toBeTruthy()
    await chip!.trigger('focus')
    expect(wrapper.text()).toMatch(/只读事件副本/)
    wrapper.unmount()
  })

  it('does not show snapshot chip while live', () => {
    const events: AcpEvent[] = [{ kind: 'message', title: 'live event', t: 0 }]
    const wrapper = mountPanel({ events, live: true, status: 'running', busy: true })
    expect(wrapper.text()).not.toContain('快照')
    wrapper.unmount()
  })

  it('exits boot progress when node leaves running without events', async () => {
    const wrapper = mountPanel({
      status: 'running',
      live: true,
      sandboxStatus: 'creating',
    })
    expect(wrapper.find('[data-testid="live-log-boot"]').exists()).toBe(true)
    await wrapper.setProps({ status: 'failed', live: false })
    expect(wrapper.find('[data-testid="live-log-boot"]').exists()).toBe(false)
    expect(wrapper.text()).toMatch(/失败/)
    wrapper.unmount()
  })

  it('shows rehydrate loading instead of Boot first-event wait', () => {
    const wrapper = mountPanel({
      status: 'running',
      live: true,
      sandboxStatus: 'running',
      sandboxContainerStatus: 'running',
      rehydrateStatus: 'loading',
    })
    expect(wrapper.find('[data-testid="rehydrate-loading"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('正在恢复执行日志')
    expect(wrapper.find('[data-testid="live-log-boot"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows rehydrate error with retry and never Boot first-event', async () => {
    const wrapper = mountPanel({
      status: 'running',
      live: true,
      sandboxStatus: 'running',
      sandboxContainerStatus: 'running',
      rehydrateStatus: 'error',
    })
    expect(wrapper.find('[data-testid="rehydrate-error"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('日志恢复失败')
    expect(wrapper.find('[data-testid="live-log-boot"]').exists()).toBe(false)
    await wrapper.find('[data-testid="retry-rehydrate"]').trigger('click')
    expect(wrapper.emitted('retry-rehydrate')).toBeTruthy()
    wrapper.unmount()
  })

  it('error + non-empty events: soft warn + timeline, no retry', () => {
    const events: AcpEvent[] = [
      { kind: 'message', title: 'cached event', t: 0 },
      { kind: 'thought', title: 'still visible', t: 1 },
    ]
    const wrapper = mountPanel({
      status: 'running',
      live: true,
      events,
      rehydrateStatus: 'error',
    })
    expect(wrapper.find('[data-testid="rehydrate-warn"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="rehydrate-snapshot-hint"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('历史日志读取异常')
    expect(wrapper.text()).toContain('实时快照')
    expect(wrapper.text()).toContain('cached event')
    expect(wrapper.text()).toContain('still visible')
    expect(wrapper.find('[data-testid="rehydrate-error"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="retry-rehydrate"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="live-log-boot"]').exists()).toBe(false)
    // Soft-warn path still shows live footer (not monopolized by full-page error).
    expect(wrapper.text()).toMatch(/接收事件流|空闲中/)
    wrapper.unmount()
  })

  it('error + mcpCalls only: soft warn, no retry, no Boot', () => {
    const mcpCalls: McpCall[] = [
      { at: '2026-07-18T00:00:00Z', tool: 'read_artifact', args: '{"name":"plan.json"}', result: 'ok', isError: false },
    ]
    const wrapper = mountPanel({
      status: 'running',
      live: true,
      mcpCalls,
      rehydrateStatus: 'error',
    })
    expect(wrapper.find('[data-testid="rehydrate-warn"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('read_artifact')
    expect(wrapper.find('[data-testid="retry-rehydrate"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="live-log-boot"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('loading + non-empty content does not monopolize timeline', () => {
    const events: AcpEvent[] = [{ kind: 'message', title: 'already here', t: 0 }]
    const wrapper = mountPanel({
      status: 'running',
      live: true,
      events,
      rehydrateStatus: 'loading',
    })
    expect(wrapper.find('[data-testid="rehydrate-loading"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('already here')
    expect(wrapper.find('[data-testid="live-log-boot"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('emits boot-session and restores timeout after remount (tab round-trip)', async () => {
    const first = mountPanel({
      status: 'running',
      live: true,
      sandboxStatus: 'creating',
      sandboxContainerStatus: 'running',
    })
    await vi.advanceTimersByTimeAsync(BOOT_STAGE_TIMEOUT_MS)
    await flushPromises()
    expect(first.find('[data-testid="boot-timeout-banner"]').exists()).toBe(true)
    const sessions = first.emitted('boot-session') as Array<[
      { confirmedPhase: number | null; stageEnteredAt: number | null; timedOut: boolean },
    ]>
    expect(sessions?.length).toBeGreaterThan(0)
    const persisted = sessions[sessions.length - 1][0]
    expect(persisted.timedOut).toBe(true)
    // creating + container ready → acp_ready (index 2 after pulling=0 was added)
    expect(persisted.confirmedPhase).toBe(2)
    expect(persisted.stageEnteredAt).toBeTruthy()
    first.unmount()

    // Simulate parent remount after sandbox tab round-trip with persisted session.
    const second = mountPanel({
      status: 'running',
      live: true,
      sandboxStatus: 'creating',
      sandboxContainerStatus: 'running',
      bootSession: persisted,
    })
    await flushPromises()
    expect(second.find('[data-testid="boot-stage-acp_ready"]').attributes('data-state')).toBe('timeout')
    expect(second.find('[data-testid="boot-timeout-banner"]').exists()).toBe(true)
    second.unmount()
  })
})
