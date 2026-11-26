// @vitest-environment happy-dom
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import pages from '@/locales/zh-CN/pages.json'
import common from '@/locales/zh-CN/common.json'
import type { SoloSession, SSHGatewayInfo } from '@/lib/api/apiTypes'
import { resetSSHGatewayForTest } from '@/lib/sandbox/useSSHGateway'
import SandboxInfoButton from './SandboxInfoButton.vue'

const gatewayInfo = vi.hoisted(() => ({ value: { enabled: false } as SSHGatewayInfo }))
vi.mock('@/lib/api/api', () => ({
  api: {
    sshGateway: vi.fn(async () => gatewayInfo.value),
    sandboxIdeUrl: (id: number) => `/sandbox/${id}/?folder=/root/workspace`,
  },
}))
const copied = vi.hoisted(() => [] as string[])
vi.mock('@/lib/shared/copyToClipboard', () => ({
  copyToClipboard: vi.fn(async (text: string) => { copied.push(text); return true }),
}))

const mounted: Array<ReturnType<typeof mount>> = []

function session(patch: Partial<SoloSession> = {}): SoloSession {
  return {
    id: 's1', userId: 'alice', title: 't', agentName: 'claude', status: 'running', restarts: 0,
    createdAt: '2026-11-25T10:00:00Z', updatedAt: '2026-11-25T10:00:00Z', busy: false, waiting: 0,
    sandboxId: 12,
    repos: [{ position: 0, projectId: 'p', name: 'cocofhu/grasp', repoUrl: 'https://github.com/cocofhu/grasp', repoDirName: 'grasp' }],
    branch: 'main',
    sandbox: {
      id: 12, name: 'grasp-solo-7f3a', status: 'running', connected: true, hasCodeServer: true,
      recoveryKind: 'snapshot', destroyAt: new Date(Date.now() + 2 * 3600_000).toISOString(),
      modelActiveAt: new Date(Date.now() - 60_000).toISOString(), createdAt: '2026-11-25T10:02:00Z',
    },
    ...patch,
  }
}

async function render(props: Record<string, unknown> = {}) {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:p(.*)*', component: { template: '<div />' } }] })
  const host = document.createElement('div')
  document.body.appendChild(host)
  const wrapper = mount(SandboxInfoButton, {
    props: { session: session(), busy: false, paused: false, waking: false, ...props },
    attachTo: host,
    global: {
      plugins: [router, createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': { ...pages, ...common } } })],
      stubs: { Icon: true },
    },
  })
  mounted.push(wrapper)
  return wrapper
}

async function openPanel(wrapper: Awaited<ReturnType<typeof render>>) {
  await wrapper.get('[data-testid="sandbox-info-toggle"]').trigger('click')
  await flushPromises()
  return document.body.querySelector<HTMLElement>('[data-testid="sandbox-info-panel"]')!
}

function q(testId: string) {
  return document.body.querySelector<HTMLElement>(`[data-testid="${testId}"]`)
}

beforeEach(() => {
  resetSSHGatewayForTest()
  gatewayInfo.value = { enabled: false }
  copied.length = 0
  localStorage.clear()
})

afterEach(() => {
  while (mounted.length) mounted.pop()!.unmount()
  document.body.innerHTML = ''
})

describe('SandboxInfoButton', () => {
  it('re-reads the session while the panel stays open', async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval', 'Date'] })
    try {
      const wrapper = await render()
      await openPanel(wrapper)
      expect(wrapper.emitted('refresh')).toHaveLength(1)
      vi.advanceTimersByTime(10_000)
      expect(wrapper.emitted('refresh')).toHaveLength(2)
      vi.advanceTimersByTime(20_000)
      expect(wrapper.emitted('refresh')).toHaveLength(4)
      await wrapper.get('[data-testid="sandbox-info-toggle"]').trigger('click')
      await flushPromises()
      vi.advanceTimersByTime(30_000)
      expect(wrapper.emitted('refresh')).toHaveLength(4)
    } finally {
      vi.useRealTimers()
    }
  })

  it('shows the state dot, opens the panel and asks for a fresh view', async () => {
    const wrapper = await render()
    expect(wrapper.get('[data-testid="sandbox-info-dot"]').attributes('data-tone')).toBe('ok')
    const panel = await openPanel(wrapper)
    expect(panel).not.toBeNull()
    expect(wrapper.emitted('refresh')).toHaveLength(1)
    expect(q('sandbox-info-state')!.textContent).toBe('运行中')
    expect(q('sandbox-info-reclaim')!.textContent).toMatch(/^\d+ 小时 \d+ 分钟后$/)
    expect(q('sandbox-info-lifecycle')!.textContent).not.toContain('到期先保存环境')
    expect(panel.querySelector('h2')!.textContent).toBe('grasp-solo-7f3a')
    expect(panel.textContent).toContain('#12')
    expect(q('sandbox-info-web-ide')!.getAttribute('href')).toBe('/sandbox/12/?folder=/root/workspace')
    expect(q('sandbox-info-ide')).toBeNull()
    const toggle = q('sandbox-info-details-toggle')!
    const details = q('sandbox-info-details')!
    expect(toggle.getAttribute('aria-expanded')).toBe('false')
    expect(details.style.display).toBe('none')
    toggle.click()
    await flushPromises()
    expect(toggle.getAttribute('aria-expanded')).toBe('true')
    expect(details.style.display).not.toBe('none')
    expect(details.textContent).toContain('cocofhu/grasp · main')
    expect(details.textContent).toContain('最近模型活动')
    details.querySelector('button')!.click()
    await flushPromises()
    expect(copied).toEqual(['/root/workspace'])
  })

  it('emits the primary action and keep-alive changes', async () => {
    const wrapper = await render()
    await openPanel(wrapper)
    expect(q('sandbox-info-action')!.textContent).toContain('保存并停止')
    q('sandbox-info-action')!.click()
    expect(wrapper.emitted('action')).toEqual([['stop']])
    q('sandbox-info-keep-alive')!.click()
    expect(wrapper.emitted('keepAlive')).toEqual([[true]])
  })

  it('disables stop while the agent is busy and hides actions when read-only', async () => {
    const wrapper = await render({ busy: true })
    await openPanel(wrapper)
    expect(q('sandbox-info-state')!.textContent).toBe('运行中 · Agent 执行中')
    expect(q('sandbox-info-reclaim')!.textContent).toBe('任务结束后计时')
    expect(q('sandbox-info-action')!.hasAttribute('disabled')).toBe(true)
    await wrapper.setProps({ readonly: true })
    expect(q('sandbox-info-action')).toBeNull()
    expect(q('sandbox-info-keep-alive')).toBeNull()
  })

  it('offers wake for a paused sandbox and wakes it when an IDE link is used', async () => {
    gatewayInfo.value = { enabled: true, host: 'ssh.example.com', port: 2222, allowed: true, hasKey: true, hostKeyFingerprint: 'SHA256:abc' }
    const wrapper = await render({ paused: true })
    expect(wrapper.get('[data-testid="sandbox-info-dot"]').attributes('data-tone')).toBe('idle')
    const panel = await openPanel(wrapper)
    expect(q('sandbox-info-state')!.textContent).toBe('已暂停')
    const trigger = q('sandbox-ide-picker-trigger')!
    expect(trigger.textContent).toContain('SSH 连接')
    expect(trigger.querySelector('icon-stub')?.getAttribute('name')).toBe('terminal')
    trigger.click()
    await flushPromises()
    const vscode = q('sandbox-ide-vscode')!
    expect(vscode.getAttribute('href')).toBe('vscode://vscode-remote/ssh-remote+sbx-12@ssh.example.com:2222/root/workspace')
    vscode.addEventListener('click', (e) => e.preventDefault())
    vscode.click()
    expect(wrapper.emitted('action')).toEqual([['wake']])
    expect(panel.textContent).toContain('连接时会自动唤醒')
    expect(q('sandbox-info-command')).toBeNull()

    q('sandbox-ide-picker-trigger')!.click()
    await flushPromises()
    expect(q('sandbox-ide-picker-search')).toBeNull()
    expect(q('sandbox-ide-cursor')!.getAttribute('href')).toBe('cursor://vscode-remote/ssh-remote+sbx-12@ssh.example.com:2222/root/workspace')
    expect(q('sandbox-ide-trae-cn')!.getAttribute('href')).toMatch(/^trae-cn:\/\/vscode-remote\/ssh-remote\+sbx-12@/)
    expect(q('sandbox-ide-trae-cn')!.textContent).toContain('Trae 中国版')
    expect(q('sandbox-ide-codebuddy-cn')!.getAttribute('href')).toMatch(/^codebuddycn:\/\//)
    expect(q('sandbox-ide-codebuddy-cn')!.textContent).toContain('CodeBuddy 中国版')
    expect(q('sandbox-ide-picker-panel')!.querySelector('.ide-menu-cn')).toBeNull()
    expect(q('sandbox-ide-antigravity')!.getAttribute('href')).toBe('antigravity://vscode-remote/ssh-remote+sbx-12@ssh.example.com:2222/root/workspace')
    expect(q('sandbox-ide-antigravity')!.textContent).toContain('Antigravity')
    expect(q('sandbox-ide-codex')!.getAttribute('href')).toBe('codex://vscode-remote/ssh-remote+sbx-12@ssh.example.com:2222/root/workspace')
    expect(q('sandbox-ide-codex')!.textContent).toContain('Codex')
    expect(q('sandbox-ide-jetbrains')).toBeNull()
    expect(document.querySelectorAll('[data-ide-logo]').length).toBeGreaterThanOrEqual(9)
    const trae = q('sandbox-ide-trae-cn')!
    trae.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    expect(q('sandbox-info-panel')).not.toBeNull()
    trae.addEventListener('click', (e) => e.preventDefault())
    trae.click()
    await flushPromises()
    expect(q('sandbox-ide-picker-panel')).toBeNull()
    expect(q('sandbox-ide-picker-trigger')!.textContent).toContain('Trae 中国版')
    expect(JSON.parse(localStorage.getItem('grasp.sandbox.localIde')!)).toEqual({ id: 'trae', cn: true })

    q('sandbox-ide-picker-trigger')!.click()
    await flushPromises()
    q('sandbox-ide-info')!.click()
    await flushPromises()
    expect(q('sandbox-info-command')!.textContent).toBe('ssh -p 2222 sbx-12@ssh.example.com')
    expect(q('sandbox-info-config')!.textContent).toContain('HostName ssh.example.com')
    q('sandbox-info-copy-command')!.click()
    q('sandbox-ide-copy-port')!.click()
    await flushPromises()
    expect(copied).toEqual(['ssh -p 2222 sbx-12@ssh.example.com', '2222'])
    expect(q('sandbox-ide-details')!.textContent).toContain('SHA256:abc')
  })

  it('SSH connection details open in a dialog', async () => {
    gatewayInfo.value = { enabled: true, host: 'ssh.example.com', port: 2222, allowed: true, hasKey: true }
    const wrapper = await render()
    await openPanel(wrapper)
    q('sandbox-ide-picker-trigger')!.click()
    await flushPromises()
    q('sandbox-ide-info')!.click()
    await flushPromises()
    expect(wrapper.emitted('action')).toBeUndefined()
    expect(q('sandbox-ide-picker-panel')).toBeNull()
    const details = q('sandbox-ide-details')!
    expect(details.querySelector('[role="dialog"]')).not.toBeNull()
    expect(q('sandbox-info-panel')!.contains(details)).toBe(false)
    expect(details.textContent).toContain('sbx-12')
    details.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    expect(q('sandbox-info-panel')).not.toBeNull()
    q('sandbox-ide-details-close')!.click()
    await flushPromises()
    expect(q('sandbox-ide-details')).toBeNull()
    expect(q('sandbox-info-panel')).not.toBeNull()

    q('sandbox-ide-picker-trigger')!.click()
    await flushPromises()
    q('sandbox-ide-picker-panel')!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushPromises()
    expect(q('sandbox-ide-picker-panel')).toBeNull()
    expect(q('sandbox-info-panel')).not.toBeNull()
    q('sandbox-ide-picker-trigger')!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushPromises()
    expect(q('sandbox-info-panel')).toBeNull()
    await openPanel(wrapper)
    q('sandbox-ide-picker-trigger')!.click()
    await flushPromises()
    document.body.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    await flushPromises()
    expect(q('sandbox-ide-picker-panel')).toBeNull()
  })

  it('shows open connections and why the countdown is paused', async () => {
    gatewayInfo.value = { enabled: true, port: 2222, allowed: true, hasKey: true }
    const s = session()
    s.sandbox = { ...s.sandbox!, externalSessions: 2, destroyAt: undefined }
    const wrapper = await render({ session: s })
    await openPanel(wrapper)
    expect(q('sandbox-info-connections')!.textContent).toBe('2 个本地 IDE 连接')
    expect(q('sandbox-info-reclaim')!.textContent).toBe('计时已暂停')
    expect(q('sandbox-info-lifecycle')!.textContent).toContain('全部断开后重新计时')
    q('sandbox-ide-picker-trigger')!.click()
    await flushPromises()
    expect(q('sandbox-ide-vscode')!.getAttribute('href')).toContain(`sbx-12@${window.location.hostname}:2222`)
  })

  it('guides users without a key or permission, and blocks stopped sandboxes', async () => {
    gatewayInfo.value = { enabled: true, port: 2222, allowed: true, hasKey: false }
    let wrapper = await render()
    await openPanel(wrapper)
    expect(q('sandbox-info-no-key')!.textContent).toContain('先登记 SSH 公钥')
    wrapper.unmount()
    mounted.pop()
    document.body.innerHTML = ''

    resetSSHGatewayForTest()
    gatewayInfo.value = { enabled: true, port: 2222, allowed: false, hasKey: true }
    wrapper = await render()
    let panel = await openPanel(wrapper)
    expect(panel.textContent).toContain('SSH 连接只对管理员开放')
    wrapper.unmount()
    mounted.pop()
    document.body.innerHTML = ''

    resetSSHGatewayForTest()
    gatewayInfo.value = { enabled: true, port: 2222, allowed: true, hasKey: true }
    wrapper = await render({ session: session({ status: 'stopped', backupAt: new Date().toISOString(), backupSize: 2048 }) })
    panel = await openPanel(wrapper)
    expect(q('sandbox-info-ide-blocked')!.textContent).toBe('环境已停止，恢复后才能连接')
    expect(q('sandbox-ide-picker-trigger')!.tagName).toBe('BUTTON')
    q('sandbox-ide-picker-trigger')!.click()
    await flushPromises()
    expect(q('sandbox-ide-vscode')!.getAttribute('aria-disabled')).toBe('true')
    expect(q('sandbox-ide-vscode')!.hasAttribute('disabled')).toBe(true)
    expect(q('sandbox-ide-vscode')!.hasAttribute('href')).toBe(false)
    expect(q('sandbox-ide-trae-cn')!.tagName).toBe('BUTTON')
    expect(q('sandbox-ide-trae-cn')!.getAttribute('aria-disabled')).toBe('true')
    expect(q('sandbox-ide-trae-cn')!.hasAttribute('href')).toBe(false)
    expect(q('sandbox-info-action')!.textContent).toContain('恢复')
    expect(panel.textContent).toContain('2.0 KiB')
    expect(q('sandbox-info-reclaim')).toBeNull()
    expect(q('sandbox-info-web-ide')).toBeNull()
  })

  it('explains Docker environments, errors and preparing steps', async () => {
    const s = session({ idleReclaimExempt: true })
    s.sandbox = { ...s.sandbox!, recoveryKind: 'files', destroyAt: undefined }
    let wrapper = await render({ session: s })
    await openPanel(wrapper)
    expect(q('sandbox-info-reclaim')!.textContent).toBe('持续运行')
    expect(q('sandbox-info-keep-alive')).toBeNull()
    wrapper.unmount()
    mounted.pop()

    wrapper = await render({ session: session({ status: 'error', error: 'clone failed' }) })
    expect(wrapper.get('[data-testid="sandbox-info-dot"]').attributes('data-tone')).toBe('err')
    await openPanel(wrapper)
    expect(q('sandbox-info-error')!.textContent).toContain('clone failed')
    wrapper.unmount()
    mounted.pop()

    const preparing = session({ status: 'starting' })
    preparing.sandbox = { ...preparing.sandbox!, status: 'pulling' }
    wrapper = await render({ session: preparing })
    const panel = await openPanel(wrapper)
    expect(panel.textContent).toContain('正在拉取镜像')
    expect(q('sandbox-info-action')).toBeNull()
  })

  it('closes on Escape and on outside clicks', async () => {
    const wrapper = await render()
    const panel = await openPanel(wrapper)
    panel.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushPromises()
    expect(q('sandbox-info-panel')).toBeNull()
    expect(document.activeElement).toBe(wrapper.get('[data-testid="sandbox-info-toggle"]').element)

    await openPanel(wrapper)
    document.body.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    await flushPromises()
    expect(q('sandbox-info-panel')).toBeNull()

    await openPanel(wrapper)
    await wrapper.get('[data-testid="sandbox-info-toggle"]').trigger('click')
    await flushPromises()
    expect(q('sandbox-info-panel')).toBeNull()
  })

  it('becomes a bottom sheet on narrow screens', async () => {
    const width = window.innerWidth
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 400 })
    try {
      const wrapper = await render()
      const panel = await openPanel(wrapper)
      expect(panel.classList.contains('is-sheet')).toBe(true)
    } finally {
      Object.defineProperty(window, 'innerWidth', { configurable: true, value: width })
    }
  })
})
