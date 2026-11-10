import { expect, test, type Page } from '@playwright/test'
import { installWorkspaceApiDefaults } from './helpers/workspaceApiDefaults'

async function setup(page: Page, stopPending = false) {
  await installWorkspaceApiDefaults(page)
  const sessions = ['solo-1', 'solo-2'].map((id, index) => ({
    id, userId: 'workspace-config-test', title: index ? '另一个会话' : '关闭交互', agentName: 'Demo', status: 'running', error: '',
    busy: false, waiting: 0, restarts: 0, createdAt: '2026-11-11T00:00:00Z', updatedAt: '2026-11-11T00:00:00Z',
    sandbox: { id: index + 7, desiredState: 'running', status: 'running', error: '' },
  }))
  let release!: () => void
  const stopped = new Promise<void>(resolve => { release = resolve })
  const calls: string[] = []
  await page.route('**/api/solo/sessions**', async route => {
    const path = new URL(route.request().url()).pathname
    if (path === '/api/solo/sessions') { await route.fulfill({ json: { sessions, maxSandboxes: 5 } }); return }
    const session = sessions.find(row => path.startsWith(`/api/solo/sessions/${row.id}`))
    if (!session) { await route.fallback(); return }
    if (path.endsWith('/close')) {
      calls.push(session.id)
      await stopped
      session.status = 'closed'
      session.error = stopPending ? '环境停止尚未确认，聊天已关闭，工作区已保留' : ''
      session.sandbox = { ...session.sandbox, desiredState: 'stopped', status: stopPending ? 'error' : 'stopped' }
      await route.fulfill(stopPending
        ? { status: 409, json: { code: 'environment_stop_pending', error: `${session.error}: POST http://gateway.example.test/stop: context deadline exceeded` } }
        : { json: session })
    } else if (path.endsWith('/messages')) await route.fulfill({ json: [] })
    else if (path.endsWith('/resources')) await route.fulfill({ json: { runId: '', artifacts: [], ports: [] } })
    else await route.fulfill({ json: session })
  })
  await page.routeWebSocket('**/api/solo/sessions/*/chat*', ws => ws.send(JSON.stringify({ type: 'session', event: 'queue_state', busy: false, items: [] })))
  await page.goto('/workspace-config.html?view=/solo/solo-1')
  await expect(page.getByTestId('solo-session-pane')).toBeVisible()
  const completeStop = () => {
    sessions[0]!.error = ''
    sessions[0]!.sandbox.status = 'stopped'
  }
  return { release, completeStop, calls }
}

async function confirmClose(page: Page) {
  await page.locator('[data-kind="solo"][data-key="solo-1"]').click({ button: 'right' })
  await page.getByTestId('nav-pending-menu-cancel').click()
  const dialog = page.getByRole('dialog', { name: '关闭 Solo 会话', exact: true })
  await expect(dialog).toBeVisible()
  await dialog.getByTestId('nav-solo-close-confirm').click()
  // The HTTP request is deliberately held until the test releases it.
  await expect(dialog).toBeHidden({ timeout: 1000 })
  await expect(page.getByTestId('solo-closed-bar')).toContainText('正在关闭会话')
  await expect(page.getByTestId('toast-host')).toContainText('正在关闭「关闭交互」')
  return dialog
}

test('a slow close dismisses immediately and does not block other sessions or duplicate requests', async ({ page }) => {
  const { release, calls } = await setup(page)
  await confirmClose(page)
  const row = page.locator('[data-kind="solo"][data-key="solo-1"]')
  await expect(row.getByTestId('nav-solo-closing')).toBeVisible()
  await row.click({ button: 'right' })
  await expect(page.getByTestId('nav-pending-menu-cancel')).toBeDisabled()
  await expect(page.getByTestId('nav-solo-menu-environment')).toBeDisabled()
  await page.keyboard.press('Escape')
  await page.locator('[data-kind="solo"][data-key="solo-2"]').click()
  await expect(page.getByTestId('solo-closed-bar')).toHaveCount(0)
  await expect(page.getByTestId('solo-input')).toBeEnabled()
  await page.screenshot({ path: '/tmp/grasp-solo-close-pending.png' })
  expect(calls).toEqual(['solo-1'])
  release()
  await expect(row).toHaveCount(0)
  await expect(page.getByTestId('sidebar-recent-solo').filter({ hasText: '关闭交互' })).toBeVisible()
  await expect(page.getByTestId('toast-host')).toContainText('「关闭交互」已关闭')
  await expect(page.getByTestId('toast-host')).not.toContainText('正在关闭')
  expect(calls).toEqual(['solo-1'])
})

test('a pending stop reports the real outcome and updates the open chat without another confirmation', async ({ page }) => {
  const { release, completeStop, calls } = await setup(page, true)
  const dialog = await confirmClose(page)
  release()
  await expect(page.getByTestId('toast-host')).toContainText('环境停止尚未确认')
  await expect(page.getByTestId('toast-host')).not.toContainText('正在关闭')
  await expect(page.getByTestId('toast-host')).not.toContainText('已关闭，工作区已保留')
  await expect(page.getByTestId('toast-host')).not.toContainText('gateway.example.test')
  await expect(page.getByTestId('toast-host')).not.toContainText('context deadline exceeded')
  await expect(page.getByTestId('solo-closed-bar')).toContainText('正在核对状态')
  await expect(page.getByRole('button', { name: '恢复会话', exact: true })).toHaveCount(0)
  completeStop()
  await expect(page.getByRole('button', { name: '恢复会话', exact: true })).toBeVisible()
  await expect(page.getByTestId('solo-closed-bar')).not.toContainText('正在核对状态')
  await expect(dialog).toHaveCount(0)
  expect(calls).toEqual(['solo-1'])
})

test('mobile drawer can close while a session stops and still shows its result', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  const { release, calls } = await setup(page)
  await page.getByTestId('floating-nav-ball').click()
  const drawer = page.getByTestId('mobile-nav-drawer')
  const row = drawer.locator('[data-kind="solo"][data-key="solo-1"]')
  await row.focus()
  await page.keyboard.press('Shift+F10')
  await page.getByTestId('nav-pending-menu-cancel').click()
  const dialog = page.getByRole('dialog', { name: '关闭 Solo 会话', exact: true })
  await dialog.getByTestId('nav-solo-close-confirm').click()
  await expect(dialog).toBeHidden({ timeout: 1000 })
  await expect(drawer.getByTestId('nav-solo-closing')).toBeVisible()
  await drawer.locator('[data-kind="solo"][data-key="solo-2"]').click()
  await expect(drawer).toHaveCount(0)
  release()
  await expect(page.getByTestId('toast-host')).toContainText('「关闭交互」已关闭')
  await page.getByTestId('floating-nav-ball').click()
  await expect(page.getByTestId('mobile-nav-drawer').locator('[data-kind="solo"][data-key="solo-1"]')).toHaveCount(0)
  expect(calls).toEqual(['solo-1'])
  await page.screenshot({ path: '/tmp/grasp-solo-close-mobile.png' })
})
