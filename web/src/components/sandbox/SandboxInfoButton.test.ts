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
  it('shows the state dot, opens the panel and asks for a fresh view', async () => {
    const wrapper = await render()
    expect(wrapper.get('[data-testid="sandbox-info-dot"]').attributes('data-tone')).toBe('ok')
    const panel = await openPanel(wrapper)
    expect(panel).not.toBeNull()
    expect(wrapper.emitted('refresh')).toHaveLength(1)
    expect(q('sandbox-info-state')!.textContent).toBe('运行中')
    expect(q('sandbox-info-reclaim')!.textContent).toMatch(/^1h 5\dm 后保存并暂停$/)
    expect(panel.textContent).toContain('cocofhu/grasp · main')
    expect(panel.textContent).toContain('#12 · grasp-solo-7f3a')
    expect(q('sandbox-info-web-ide')!.getAttribute('href')).toBe('/sandbox/12/?folder=/root/workspace')
    expect(q('sandbox-info-ide')).toBeNull()
  })

  it('emits the primary action and keep-alive changes', async () => {
    const wrapper = await render()
    await openPanel(wrapper)
    q('sandbox-info-action')!.click()
    expect(wrapper.emitted('action')).toEqual([['stop']])
    q('sandbox-info-keep-alive')!.click()
    expect(wrapper.emitted('keepAlive')).toEqual([[true]])
  })

  it('disables stop while the agent is busy and hides actions when read-only', async () => {
    const wrapper = await render({ busy: true })
    await openPanel(wrapper)
    expect(q('sandbox-info-state')!.textContent).toBe('运行中 · Agent 执行中')
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
    const primary = q('sandbox-ide-primary')!
    expect(primary.getAttribute('href')).toBe('vscode://vscode-remote/ssh-remote+sbx-12@ssh.example.com:2222/root/workspace')
    expect(primary.textContent).toContain('用 VS Code 打开')
    primary.addEventListener('click', (e) => e.preventDefault())
    primary.click()
    expect(wrapper.emitted('action')).toEqual([['wake']])
    expect(panel.textContent).toContain('连接时会自动唤醒')
    expect(q('sandbox-info-command')).toBeNull()

    q('sandbox-ide-caret')!.click()
    await flushPromises()
    expect(q('sandbox-ide-list')!.textContent).toContain('本地 IDE（Remote-SSH）')
    expect(q('sandbox-ide-cursor')!.getAttribute('href')).toBe('cursor://vscode-remote/ssh-remote+sbx-12@ssh.example.com:2222/root/workspace')
    expect(q('sandbox-ide-trae-cn')!.getAttribute('href')).toMatch(/^trae-cn:\/\/vscode-remote\/ssh-remote\+sbx-12@/)
    expect(q('sandbox-ide-codebuddy-cn')!.getAttribute('href')).toMatch(/^codebuddycn:\/\//)
    expect(q('sandbox-ide-jetbrains')!.hasAttribute('href')).toBe(false)
    expect(document.querySelectorAll('[data-ide-logo]').length).toBeGreaterThanOrEqual(6)
    const trae = q('sandbox-ide-trae-cn')!
    trae.addEventListener('click', (e) => e.preventDefault())
    trae.click()
    await flushPromises()
    expect(q('sandbox-ide-list')).toBeNull()
    expect(q('sandbox-ide-primary')!.textContent).toContain('用 Trae 中国版 打开')
    expect(JSON.parse(localStorage.getItem('grasp.sandbox.localIde')!)).toEqual({ id: 'trae', cn: true })

    q('sandbox-ide-caret')!.click()
    await flushPromises()
    q('sandbox-ide-info')!.click()
    await flushPromises()
    expect(q('sandbox-info-command')!.textContent).toBe('ssh -p 2222 sbx-12@ssh.example.com')
    expect(q('sandbox-info-config')!.textContent).toContain('HostName ssh.example.com')
    q('sandbox-info-copy-command')!.click()
    q('sandbox-ide-copy-port')!.click()
    await flushPromises()
    expect(copied).toEqual(['ssh -p 2222 sbx-12@ssh.example.com', '2222'])
    expect(panel.textContent).toContain('SHA256:abc')
  })

  it('JetBrains Gateway shows connection details instead of a link', async () => {
    gatewayInfo.value = { enabled: true, host: 'ssh.example.com', port: 2222, allowed: true, hasKey: true }
    const wrapper = await render()
    await openPanel(wrapper)
    q('sandbox-ide-caret')!.click()
    await flushPromises()
    q('sandbox-ide-jetbrains')!.click()
    await flushPromises()
    expect(wrapper.emitted('action')).toBeUndefined()
    expect(q('sandbox-ide-jetbrains-hint')!.textContent).toContain('New Connection')
    expect(q('sandbox-ide-details')!.textContent).toContain('sbx-12')
    const primary = q('sandbox-ide-primary')!
    expect(primary.tagName).toBe('BUTTON')
    expect(primary.textContent).toContain('用 JetBrains Gateway 连接')
    primary.click()
    await flushPromises()
    expect(q('sandbox-ide-details')).toBeNull()

    q('sandbox-ide-caret')!.click()
    await flushPromises()
    q('sandbox-ide-list')!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushPromises()
    expect(q('sandbox-ide-list')).toBeNull()
    expect(q('sandbox-info-panel')).not.toBeNull()
    q('sandbox-ide-primary')!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushPromises()
    expect(q('sandbox-info-panel')).toBeNull()
    await openPanel(wrapper)
    q('sandbox-ide-caret')!.click()
    await flushPromises()
    document.body.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    await flushPromises()
    expect(q('sandbox-ide-list')).toBeNull()
  })

  it('shows open connections and why the countdown is paused', async () => {
    gatewayInfo.value = { enabled: true, port: 2222, allowed: true, hasKey: true }
    const s = session()
    s.sandbox = { ...s.sandbox!, externalSessions: 2, destroyAt: undefined }
    const wrapper = await render({ session: s })
    await openPanel(wrapper)
    expect(q('sandbox-info-connections')!.textContent).toContain('2 个连接进行中')
    expect(q('sandbox-info-reclaim')!.textContent).toBe('本地 IDE 连接中，暂停计时')
    expect(q('sandbox-ide-primary')!.getAttribute('href')).toContain(`sbx-12@${window.location.hostname}:2222`)
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
    expect(q('sandbox-ide-primary')!.tagName).toBe('BUTTON')
    expect(q('sandbox-ide-primary')!.hasAttribute('disabled')).toBe(true)
    q('sandbox-ide-caret')!.click()
    await flushPromises()
    expect(q('sandbox-ide-vscode')!.getAttribute('aria-disabled')).toBe('true')
    expect(q('sandbox-ide-vscode')!.hasAttribute('href')).toBe(false)
    expect(q('sandbox-ide-trae-cn')!.tagName).toBe('SPAN')
    expect(q('sandbox-info-action')!.textContent).toContain('恢复')
    expect(panel.textContent).toContain('2.0 KiB')
    expect(q('sandbox-info-reclaim')!.textContent).toBe('—')
    expect(q('sandbox-info-web-ide')).toBeNull()
  })

  it('explains Docker environments, errors and preparing steps', async () => {
    const s = session({ idleReclaimExempt: true })
    s.sandbox = { ...s.sandbox!, recoveryKind: 'files', destroyAt: undefined }
    let wrapper = await render({ session: s })
    await openPanel(wrapper)
    expect(q('sandbox-info-reclaim')!.textContent).toBe('不会自动回收，手动停止才释放')
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
