import { expect, test, type Page } from '@playwright/test'
import { installWorkspaceApiDefaults } from './helpers/workspaceApiDefaults'

type RequestRecord = { path: string; method: string; body?: Record<string, unknown> }

async function setup(page: Page, options: { lostCapture?: boolean; disabled?: boolean; workflow?: boolean; app?: 'running' | 'closed' } = {}) {
  if (options.app) await installWorkspaceApiDefaults(page)
  const calls: RequestRecord[] = []
  let restoreState = '', bindingState = '', captureAttempts = 0
  let currentBinding = 'original-binding'
  const base = '/api/solo/sessions/solo-1', bindingBase = `${base}/workspace/binding`
  const session = { id: 'solo-1', userId: 'workspace-config-test', title: '快照交互', agentName: 'Demo', status: options.app, sandboxId: 7, artifactRunId: '', busy: false, waiting: 0, restarts: 0, createdAt: '2026-11-09T12:00:00Z', updatedAt: '2026-11-09T12:00:00Z' }
  const snapshot = { id: 'snapshot-1', scopeKind: options.workflow ? 'run' : 'solo', scopeId: options.workflow ? 'run-1' : 'solo-1', state: 'ready', nodeId: options.workflow ? 'implementation' : undefined, iteration: 2, runAttempt: 3, bytes: 1048576, entries: 12, createdAt: '2026-11-09T12:00:00Z' }
  const restore = () => ({ id: 'restore-1', snapshotId: snapshot.id, state: restoreState, createdAt: snapshot.createdAt, updatedAt: snapshot.createdAt })
  const operation = () => ({ id: 'binding-operation-1', action: 'activate', state: bindingState, createdAt: snapshot.createdAt })
  await page.route('**/api/**', async route => {
    const request = route.request(), path = new URL(request.url()).pathname
    if (!path.startsWith('/api/')) { await route.continue(); return }
    if (options.app && !path.startsWith(base)) {
      if (path === '/api/solo/sessions') { await route.fulfill({ json: { sessions: [session], maxSandboxes: 5 } }); return }
      await route.fallback(); return
    }
    const record = { path, method: request.method(), body: request.postData() ? request.postDataJSON() as Record<string, unknown> : undefined }
    calls.push(record)
    let body: unknown = {}, status = 200
    if (options.app && path === base) body = session
    else if (options.app && path.endsWith('/messages')) body = []
    else if (options.app && path.endsWith('/resources')) body = { runId: '', previewArtifact: '', artifacts: [], ports: [] }
    else if (path === bindingBase) body = { available: true, id: 'binding-original', revision: currentBinding === 'original-binding' ? 1 : 2, fingerprint: currentBinding, canCapture: !options.disabled, canRestore: !options.disabled, canActivate: !options.disabled, canReturn: false, returnSnapshotId: snapshot.id, retainedBindings: [], returnPoints: [] }
    else if (path === `${bindingBase}/operations` && request.method() === 'GET') body = bindingState ? [operation()] : []
    else if (path === `${bindingBase}/operations`) { bindingState = 'unknown'; body = operation() }
    else if (path.startsWith(`${bindingBase}/operations/`)) {
      if (path.endsWith('/reconcile')) { bindingState = 'completed'; currentBinding = 'restored-binding' }
      body = operation()
    } else if (path === `${base}/workspace-restores` && request.method() === 'GET') body = { items: restoreState ? [restore()] : [], hasMore: false }
    else if (path === `${base}/workspace-restores`) { restoreState = 'queued'; body = restore() }
    else if (path.includes('/workspace-restores/') || path.includes('/workspace-restore-requests/')) {
      restoreState = path.endsWith('/reconcile') ? 'restored' : restoreState === 'queued' ? 'unknown' : restoreState
      body = restore()
    } else if (path.endsWith('/workspace-snapshots') && request.method() === 'GET') body = { items: [snapshot], hasMore: false }
    else if (path.endsWith('/workspace-snapshots')) {
      captureAttempts++
      if (options.lostCapture && captureAttempts === 1) { status = 503; body = { code: 'response_missing' } }
      else body = snapshot
    } else if (path.includes('/workspace-snapshot-requests/')) { status = 404; body = { code: 'not_found' } }
    else if (path.includes('/workspace-snapshots/')) body = snapshot
    else if (options.app) { await route.fallback(); return }
    await route.fulfill({ status, json: body })
  })
  if (options.app) await page.routeWebSocket('**/api/solo/sessions/*/chat*', ws => ws.send(JSON.stringify({ type: 'session', event: 'queue_state', busy: false, items: [] })))
  await page.goto(options.app ? '/workspace-config.html?view=/solo/solo-1' : `/workspace-snapshots.html${options.workflow ? '?workflow' : ''}`)
  if (!options.app) {
    await page.getByTestId('snapshots-open').click()
    await expect(page.getByTestId('workspace-snapshot-snapshot-1')).toBeVisible()
  }
  return { calls, changeHead: () => { currentBinding = 'concurrently-changed-binding' } }
}

async function close(page: Page) { await page.getByRole('dialog', { name: '快照', exact: true }).getByRole('button', { name: '关闭', exact: true }).click() }

test('refresh preserves original restore and separate manual activation, including mobile modal', async ({ page }) => {
  const { calls } = await setup(page, { workflow: true })
  await expect(page.getByTestId('solo-workspace-snapshots')).toContainText('实现 · 第 2 轮 · 执行 3')
  await page.getByTestId('workspace-restore-copy').click()
  await expect(page.getByTestId('workspace-pending-request')).toBeVisible()
  await page.reload()
  await page.getByTestId('snapshots-open').click()
  await expect(page.getByTestId('workspace-pending-request')).toContainText('结果待核对')
  expect(calls.filter(row => row.method === 'POST' && row.path.endsWith('/workspace-restores'))).toHaveLength(1)
  expect(calls.filter(row => row.method === 'POST' && row.path.endsWith('/operations'))).toHaveLength(0)
  await page.getByTestId('workspace-pending-request').getByRole('button', { name: '核对状态' }).click()
  await expect(page.getByTestId('workspace-activate')).toBeEnabled()
  await expect(page.getByTestId('solo-workspace-snapshots')).not.toContainText('恢复先创建独立目录')
  await page.screenshot({ path: '/tmp/grasp-workspace-snapshots-desktop.png', fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.screenshot({ path: '/tmp/grasp-workspace-snapshots-mobile.png', fullPage: true })
  await page.getByTestId('workspace-activate').click()
  expect(calls.filter(row => row.method === 'POST' && row.path.endsWith('/operations'))).toHaveLength(0)
  await page.getByRole('button', { name: '确认切换', exact: true }).click()
  await expect(page.getByTestId('workspace-pending-request')).toContainText('结果待核对')
  await close(page)
  await page.getByTestId('snapshots-open').click()
  await expect(page.getByTestId('workspace-pending-request')).toContainText('结果待核对')
  await page.getByTestId('workspace-pending-request').getByRole('button', { name: '核对状态' }).click()
  await expect(page.getByTestId('binding-changed')).toBeVisible()
  const mutations = calls.filter(row => row.method === 'POST' && row.path.endsWith('/operations'))
  expect(mutations).toHaveLength(1)
  expect(mutations[0]!.body).toMatchObject({ action: 'activate', expectedBinding: 'original-binding', restoreId: 'restore-1', returnSnapshotId: 'snapshot-1' })
})

test('snapshots are centered, concise and keyboard accessible on desktop and mobile', async ({ page }) => {
  await setup(page)
  const dialog = page.getByRole('dialog', { name: '快照', exact: true })
  await expect(dialog).toBeVisible()
  await expect(dialog).not.toContainText('保存当前文件')
  await expect(dialog).not.toContainText('不启动工作流')
  await expect(dialog.getByRole('button', { name: '保存快照' })).toBeEnabled()
  for (const viewport of [{ width: 1280, height: 800 }, { width: 390, height: 844 }]) {
    await page.setViewportSize(viewport)
    await expect.poll(async () => {
      const bounds = await dialog.boundingBox()
      return bounds ? Math.max(Math.abs(bounds.x + bounds.width / 2 - viewport.width / 2), Math.abs(bounds.y + bounds.height / 2 - viewport.height / 2)) : Infinity
    }).toBeLessThan(2)
    const bounds = await dialog.boundingBox()
    expect(bounds).not.toBeNull()
    expect(Math.abs(bounds!.x + bounds!.width / 2 - viewport.width / 2)).toBeLessThan(2)
    expect(Math.abs(bounds!.y + bounds!.height / 2 - viewport.height / 2)).toBeLessThan(2)
    expect(bounds!.width).toBeLessThanOrEqual(viewport.width - 32)
    await page.screenshot({ path: `/tmp/grasp-snapshots-${viewport.width}.png` })
  }
  await page.keyboard.press('Escape')
  await expect(dialog).toHaveCount(0)
  await expect(page.getByTestId('snapshots-open')).toBeFocused()
})

for (const status of ['running', 'closed'] as const) {
  test(`a real touch long press opens snapshots from the ${status} Solo row`, async ({ browser }) => {
    const context = await browser.newContext({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, locale: 'zh-CN' })
    try {
      const page = await context.newPage()
      const { calls } = await setup(page, { app: status })
      await page.getByTestId('floating-nav-ball').click()
      const drawer = page.getByTestId('mobile-nav-drawer')
      const row = drawer.locator(status === 'running' ? '[data-kind="solo"]' : '[data-testid="sidebar-recent-solo"]')
      await row.click({ trial: true })
      const bounds = (await row.boundingBox())!
      const touch = await context.newCDPSession(page)
      await touch.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x: bounds.x + 40, y: bounds.y + bounds.height / 2 }] })
      await expect(page.getByTestId('nav-pending-menu')).toBeVisible()
      // Keep holding beyond the click suppression grace period before lifting.
      await page.waitForTimeout(1100)
      await touch.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] })
      await expect(page.getByTestId('nav-pending-menu')).toBeVisible()
      await expect(drawer).toBeVisible()
      await page.getByTestId('nav-solo-menu-snapshots').tap()
      await expect(page.getByRole('dialog', { name: '快照', exact: true })).toBeVisible()
      await expect(page.getByTestId('workspace-snapshot-snapshot-1')).toBeVisible()
      await expect(drawer).toHaveCount(0)
      expect(calls.filter(row => row.method === 'POST')).toHaveLength(0)
    } finally { await context.close() }
  })
}

test('missing capture acknowledgement is never reposted on reload and explicit retry keeps frozen body', async ({ page }) => {
  const { calls, changeHead } = await setup(page, { lostCapture: true })
  await page.getByTestId('workspace-capture').click()
  await expect(page.getByTestId('workspace-retry-original')).toBeVisible()
  const first = calls.find(row => row.method === 'POST' && row.path.endsWith('/workspace-snapshots'))!
  changeHead()
  await page.reload()
  await page.getByTestId('snapshots-open').click()
  await expect(page.getByTestId('workspace-retry-original')).toBeVisible()
  expect(calls.filter(row => row.method === 'POST' && row.path.endsWith('/workspace-snapshots'))).toHaveLength(1)
  await page.getByTestId('workspace-retry-original').click()
  await expect(page.getByTestId('workspace-pending-request')).toHaveCount(0)
  const requests = calls.filter(row => row.method === 'POST' && row.path.endsWith('/workspace-snapshots'))
  expect(requests).toHaveLength(2)
  expect(requests[1]!.body).toEqual(first.body)
  expect(requests[1]!.body!.expectedBinding).toBe('original-binding')
})

test('disabled file operations leave history readable and submit no mutations', async ({ page }) => {
  const { calls } = await setup(page, { disabled: true })
  await expect(page.getByTestId('workspace-capture')).toBeDisabled()
  await expect(page.getByTestId('workspace-restore-copy')).toBeDisabled()
  await expect(page.getByTestId('solo-workspace-snapshots')).toContainText('历史记录仍可查看')
  expect(calls.filter(row => row.method === 'POST')).toHaveLength(0)
})
