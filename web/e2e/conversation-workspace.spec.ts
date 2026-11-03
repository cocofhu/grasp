import { test, expect, type Page } from '@playwright/test'
import { routeApi } from './helpers/apiRoute'
import { installWorkspaceApiDefaults } from './helpers/workspaceApiDefaults'

async function openConversation(page: Page, width = 1440, theme = 'light') {
  await page.setViewportSize({ width, height: 960 })
  await page.goto(`/workspace.html?view=/assistant&assistant=conversation&theme=${theme}`)
  await page.getByTestId('pm-threads-aside').getByRole('button', { name: /本周研发进展/ }).click()
  await expect(page.getByTestId('chat-message-rail-mark')).toHaveCount(10)
}

test('Solo opens a registered app and the pinned HTML from its thumbnail', async ({ page }) => {
  await installWorkspaceApiDefaults(page)
  const session = { id: 'preview-check', title: 'Solo preview', agentName: 'Demo', status: 'running', artifactRunId: 'solo-preview-check', sandboxId: 42 }
  const runId = session.artifactRunId
  const content = '<!doctype html><html><body><h1>Solo preview smoke</h1></body></html>'
  const artifact = { id: 'art-demo', runId, nodeId: 'solo', name: 'demo.html', kind: 'html', sizeBytes: content.length, revision: 1, createdAt: '2026-11-03T00:00:00Z' }
  const port = { runId, nodeId: 'solo', kind: 'port', port: 8899, label: 'Demo app', proxyUrl: '/preview/solo-preview-check/solo/8899/', healthy: true }
  await routeApi(page, '**/api/solo/sessions**', async (route) => {
    const path = new URL(route.request().url()).pathname
    if (path === '/api/solo/sessions') return route.fulfill({ json: { sessions: [session], maxSandboxes: 8 } })
    if (path.endsWith('/messages')) return route.fulfill({ json: [] })
    if (path.endsWith('/resources')) return route.fulfill({
      json: { runId, previewArtifact: artifact.name, artifacts: [artifact], ports: [port, { ...port, port: 5173, label: 'Second app' }] },
    })
    await route.fulfill({ json: session })
  })
  await routeApi(page, '**/api/artifacts/art-demo/content', (route) => route.fulfill({ json: { ...artifact, content } }))
  await routeApi(page, '**/api/artifacts/art-demo/versions', (route) => route.fulfill({ json: [] }))
  await page.routeWebSocket('**/api/solo/sessions/*/chat*', (ws) => ws.send(JSON.stringify({ type: 'session', event: 'queue_state', busy: false, items: [] })))
  await page.routeWebSocket('**/sandbox-vnc/42/ws', (ws) => ws.send(JSON.stringify({ type: 'ready', url: 'http://127.0.0.1:8899/' })))

  await page.setViewportSize({ width: 1440, height: 950 })
  await page.goto(`/workspace-config.html?view=/solo/${session.id}`)
  await page.getByTestId('conversation-resource-toggle').click()
  await page.getByTestId('react-artifact-card-app').click()
  await expect(page.getByTestId('app-preview').getByRole('button', { name: 'Demo app' })).toBeVisible()

  await page.getByTestId('conversation-panel-toggle').click()
  const card = page.getByTestId('react-artifact-card-demo.html')
  await expect(card.locator('iframe')).toBeVisible()
  await card.locator('.artifact-card-cover').click()
  await expect(page.getByTestId('react-artifact-tab-demo.html')).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByTestId('react-artifact-preview-demo.html').frameLocator('iframe:visible').getByText('Solo preview smoke')).toBeVisible()
})

test('a direct Solo preview opens the same chat and sends a picked element with the message', async ({ page, context }) => {
  await installWorkspaceApiDefaults(page)
  const grasp = 'http://127.0.0.1:5174'
  const direct = grasp
  const session = { id: 'solo-chat-check', title: 'Solo preview', agentName: 'Demo', status: 'running', artifactRunId: 'solo-preview-chat-check', sandboxId: 42 }
  const runId = session.artifactRunId
  const ticket = { ticket: 'solo-preview-ticket', runId, nodeId: 'solo', expiresAt: '2099-01-01T00:00:00Z' }
  const token = 'solo-preview-token'
  const shareBodies: unknown[] = []
  const previewUrl = direct + '/solo-preview-fixture'
  const port = { runId, nodeId: 'solo', kind: 'port', port: 8899, label: 'Demo app', mode: 'direct', directUrl: previewUrl, healthy: true }
  await routeApi(page, '**/api/solo/sessions**', async (route) => {
    const path = new URL(route.request().url()).pathname
    if (path === '/api/solo/sessions') return route.fulfill({ json: { sessions: [session], maxSandboxes: 8 } })
    if (path.endsWith('/messages')) return route.fulfill({ json: [] })
    if (path.endsWith('/embed-ticket')) return route.fulfill({ json: ticket })
    if (path.endsWith('/preview-share')) {
      const body = route.request().postDataJSON()
      shareBodies.push(body)
      return route.fulfill({ json: { url: `${grasp}/public/approvals/app#t=${body.chatMode}`, expiresAt: ticket.expiresAt } })
    }
    if (path.endsWith('/resources')) return route.fulfill({ json: { runId, artifacts: [], ports: [port] } })
    await route.fulfill({ json: session })
  })
  await page.routeWebSocket('**/api/solo/sessions/*/chat*', (ws) => ws.send(JSON.stringify({ type: 'session', event: 'queue_state', busy: false, items: [] })))
  await page.routeWebSocket('**/sandbox-vnc/42/ws', (ws) => ws.send(JSON.stringify({ type: 'ready', url: previewUrl })))
  await context.route(previewUrl, (route) => route.fulfill({
    contentType: 'text/html',
    body: `<h1 id="feature">Feature to edit</h1><script defer src="${grasp}/preview-pick.js"></script>`,
  }))
  await context.route(direct + '/__grasp/embed-origin?*', (route) => {
    expect(new URL(route.request().url()).searchParams.get('ticket')).toBe(ticket.ticket)
    return route.fulfill({ json: { origin: grasp, runId, nodeId: 'solo' } })
  })
  await context.route(`${grasp}/embed/runs/${runId}/nodes/solo/chat`, async (route) => {
    await route.fulfill({ response: await route.fetch({ url: grasp + '/live-entry-chat.html' }) })
  })
  await context.route(grasp + '/embed-api/session', (route) => {
    expect(route.request().postDataJSON()).toEqual({ ticket: ticket.ticket })
    return route.fulfill({ json: { kind: 'solo', token, runId, nodeId: 'solo', expiresAt: ticket.expiresAt } })
  })
  await context.route(grasp + '/embed-api/solo/' + runId + '**', (route) => {
    expect(route.request().headers().authorization).toBe(`Bearer ${token}`)
    const path = new URL(route.request().url()).pathname
    if (path.endsWith('/messages')) return route.fulfill({ json: [{ id: 'existing', role: 'user', content: 'Previous Solo message' }] })
    if (path.endsWith('/turns')) return route.fulfill({ json: { waiting: 1 } })
    return route.fulfill({ json: session })
  })
  await context.routeWebSocket('**/embed-api/solo/*/chat*', (ws) => ws.send(JSON.stringify({
    type: 'session', event: 'queue_state', busy: false, items: [], status: 'running', sandboxId: session.sandboxId,
  })))

  await page.setViewportSize({ width: 1440, height: 950 })
  await page.goto(`${grasp}/workspace-config.html?view=/solo/${session.id}`)
  await page.getByTestId('conversation-resource-toggle').click()
  await page.getByTestId('react-artifact-card-app').click()
  await context.grantPermissions(['clipboard-read', 'clipboard-write'], { origin: grasp })
  await page.getByTestId('app-preview-share').click()
  const dialog = page.getByRole('dialog', { name: '分享应用预览' })
  await expect(dialog.getByRole('radio', { name: /独立会话/ })).toBeChecked()
  await dialog.getByRole('button', { name: '复制分享链接' }).click()
  await expect(dialog.getByRole('status')).toContainText('链接已复制')
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(`${grasp}/public/approvals/app#t=isolated`)
  await dialog.getByRole('radio', { name: /^主会话/ }).check()
  await dialog.getByRole('button', { name: '复制分享链接' }).click()
  await expect(dialog.getByRole('status')).toContainText('链接已复制')
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(`${grasp}/public/approvals/app#t=main`)
  expect(shareBodies).toEqual([{ port: 8899, chatMode: 'isolated' }, { port: 8899, chatMode: 'main' }])
  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()
  const popup = page.waitForEvent('popup')
  await page.getByTestId('app-preview-direct-open').click()
  const preview = await popup
  const toolbar = preview.locator('grasp-preview-pick')
  const drawer = toolbar.frameLocator('[data-role="drawer"] iframe')
  await expect(drawer.getByText('Previous Solo message')).toBeVisible()
  await toolbar.locator('[data-role="toggle"]').click()
  await preview.getByRole('heading', { name: 'Feature to edit' }).click()
  await expect(drawer.getByTestId('solo-preview-pick')).toContainText('#feature')
  await drawer.getByTestId('solo-preview-input').fill('Update this heading')
  const submitted = preview.waitForRequest(request => request.url().endsWith('/' + runId + '/turns'))
  await drawer.locator('button[type="submit"]').click()
  const body = (await submitted).postDataJSON()
  expect(body.text).toContain('Update this heading')
  expect(body.text).toContain('"selector":"#feature"')
})

test('anonymous preview visitors can switch panels and chat in the shared mode', async ({ browser }) => {
  const anonymous = await browser.newContext({ viewport: { width: 390, height: 820 }, locale: 'zh-CN' })
  try {
    const grasp = 'http://127.0.0.1:5174'
    const url = grasp + '/public/approvals/app'
    const mainToken = 'a'.repeat(64)
    const guestToken = 'b'.repeat(64)
    const frame = '/public/approvals/preview-api/guest-frame/'
    await anonymous.route(url, async (route) => {
      await route.fulfill({ response: await route.fetch({ url: grasp + '/live-entry-chat.html' }) })
    })
    await anonymous.route(grasp + frame, (route) => route.fulfill({ contentType: 'text/html', body: '<h1>Shared app fixture</h1>' }))
    await anonymous.route(url + '/session', (route) => {
      const headers = route.request().headers()
      expect(headers.cookie).toBeUndefined()
      expect(headers['x-gate-share-requested']).toBe('1')
      return route.fulfill({ json: { ticket: 'guest-frame', iframePath: frame, chatMode: headers['x-gate-share-token'] === mainToken ? 'main' : 'isolated' } })
    })
    const mainRows = [{ id: 'main-history', role: 'user', text: 'Original main conversation' }]
    await anonymous.route(url + '/main-chat', (route) => {
      expect(route.request().headers()['x-gate-share-token']).toBe(mainToken)
      if (route.request().method() === 'POST') {
        const { text } = route.request().postDataJSON()
        mainRows.push({ id: 'main-new', role: 'user', text })
        return route.fulfill({ json: { ok: true } })
      }
      return route.fulfill({ json: { rows: mainRows, busy: false } })
    })
    await anonymous.routeWebSocket('**/public/approvals/app/guest-chat', (ws) => ws.onMessage((data) => {
      expect(JSON.parse(String(data))).toEqual({ text: 'A separate idea' })
      ws.send(JSON.stringify({ type: 'done', text: 'Visitor-only reply' }))
    }))
    const main = await anonymous.newPage()
    await main.goto(url + '#t=' + mainToken)
    const mainPreview = main.locator('iframe')
    await expect(mainPreview).toHaveAttribute('sandbox', 'allow-scripts allow-forms')
    await expect(main.frameLocator('iframe').getByText('Shared app fixture')).toBeVisible()
    await main.getByRole('button', { name: '聊天', exact: true }).click()
    await expect(main.getByText('Original main conversation')).toBeVisible()
    await main.getByTestId('shared-app-chat-input').fill('A message for the main conversation')
    await main.getByRole('button', { name: '发送', exact: true }).click()
    await expect(main.getByText('A message for the main conversation')).toBeVisible()
    await main.getByRole('button', { name: '预览', exact: true }).click()
    await expect(main.frameLocator('iframe').getByText('Shared app fixture')).toBeVisible()

    const guest = await anonymous.newPage()
    await guest.goto(url + '#t=' + guestToken)
    await guest.getByRole('button', { name: '聊天', exact: true }).click()
    await expect(guest.getByText('Original main conversation')).toHaveCount(0)
    await guest.getByTestId('shared-app-chat-input').fill('A separate idea')
    await guest.getByRole('button', { name: '发送', exact: true }).click()
    await expect(guest.getByText('Visitor-only reply')).toBeVisible()
  } finally {
    await anonymous.close()
  }
})

for (const theme of ['light', 'dark']) {
  test(`desktop ${theme}: artifact references open beside chat, with deliberate file provenance and preserved draft`, async ({ page }) => {
    await openConversation(page, 1440, theme)
    const input = page.getByTestId('pm-chat-section').locator('textarea')
    await input.fill('保留这条未发送的修改意见')
    await input.evaluate(el => { el.dataset.retained = 'yes' })
    await page.getByTestId('citation-open-source').first().click()
    const panel = page.getByTestId('conversation-artifact-panel')
    await expect(panel).toBeVisible()
    await expect(page.locator('[data-artifact-id]')).toHaveCount(2)
    await expect(page.getByTestId('artifact-preview')).toHaveCount(0)
    await page.locator('[data-artifact-id="art-a1b2c3d4"]').click()
    await expect(panel).toContainText('发布说明')
    const [chatBox, panelBox] = await Promise.all([page.getByTestId('conversation-main').boundingBox(), panel.boundingBox()])
    expect(panelBox!.x).toBeGreaterThanOrEqual(chatBox!.x + chatBox!.width)
    expect(chatBox!.width).toBeGreaterThan(400)
    const sash = page.getByTestId('conversation-panel-sash')
    const previous = Number(await sash.getAttribute('aria-valuenow'))
    await sash.focus()
    await sash.press('ArrowLeft')
    expect(Number(await sash.getAttribute('aria-valuenow'))).toBe(previous + 24)
    await page.getByTestId('conversation-panel-close').click()
    await expect(panel).toBeHidden()
    await expect(input).toHaveValue('保留这条未发送的修改意见')
    await expect(input).toHaveAttribute('data-retained', 'yes')
    await page.getByTestId('citation-open-source').nth(1).click()
    await expect(panel).toContainText('评审结论')
    await expect(panel.getByTestId('conversation-artifact-sources')).toHaveCount(0)
    expect(Number(await sash.getAttribute('aria-valuenow'))).toBe(previous + 24)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  })
}

test('navigation previews question and answer, jumps and retains anchor identity after loading earlier history', async ({ page }) => {
  await openConversation(page)
  const mark = page.getByTestId('chat-message-rail-mark').nth(2)
  await expect(mark).toHaveAttribute('data-message-id', 'question-8')
  await mark.hover()
  await expect(page.getByTestId('chat-message-rail-card')).toContainText('第 9 轮')
  await expect(page.getByTestId('chat-message-rail-card')).toContainText('发布配置检查')
  await expect(page.getByTestId('chat-message-rail-card')).not.toContainText('##')
  await expect(page.getByTestId('chat-message-rail-card')).not.toContainText('- 研发')
  await mark.click()
  const target = page.locator('[data-chat-anchor="question-8"]')
  await expect(target).toHaveClass(/chat-message-located/)
  await expect.poll(async () => {
    const [a, b] = await Promise.all([target.boundingBox(), page.getByTestId('pm-message-scroller').boundingBox()])
    return a && b ? a.y >= b.y && a.y < b.y + 80 : false
  }).toBe(true)
  await page.getByTestId('chat-message-rail-earlier').click()
  await expect(page.getByTestId('chat-message-rail-mark')).toHaveCount(16)
  await expect(page.locator('[data-testid="chat-message-rail-mark"][data-message-id="question-8"]')).toHaveCount(1)
  await expect(target).toHaveCount(1)
  await page.locator('[data-testid="chat-message-rail-mark"][data-message-id="question-8"]').click()
  await expect(target).toHaveClass(/chat-message-located/)
})

for (const width of [390, 768]) {
  test(`${width}px: preview returns to mounted chat and draft, with modal focus containment`, async ({ page }) => {
    await openConversation(page, width)
    const input = page.getByTestId('pm-chat-section').locator('textarea')
    await input.fill('返回后继续编辑')
    await input.evaluate(el => { el.dataset.retained = 'yes' })
    await page.getByTestId('citation-open-source').nth(1).click()
    const panel = page.getByTestId('conversation-artifact-panel')
    await expect(panel).toHaveAttribute('role', 'dialog')
    await expect(panel).toHaveAttribute('aria-modal', 'true')
    await expect(page.getByTestId('conversation-main')).toHaveAttribute('inert', '')
    await expect(panel).toContainText('评审结论')
    await page.getByTestId('conversation-panel-close').focus()
    await page.keyboard.press('Shift+Tab')
    expect(await panel.evaluate(el => el.contains(document.activeElement))).toBe(true)
    await page.keyboard.press('Escape')
    await expect(panel).toBeHidden()
    await expect(input).toHaveValue('返回后继续编辑')
    await expect(input).toHaveAttribute('data-retained', 'yes')
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  })
}

for (const width of [1280, 1440]) test(`${width}px: opening, resizing and closing artifacts preserves the passage being read`, async ({ page }) => {
  await openConversation(page, width)
  await page.locator('[data-testid="chat-message-rail-mark"][data-message-id="question-10"]').click()
  const offset = () => page.locator('[data-chat-anchor="question-10"]').evaluate(el => {
    const scroll = el.closest('[data-conversation-scroll]')!
    return el.getBoundingClientRect().top - scroll.getBoundingClientRect().top
  })
  const initial = await offset()
  await page.getByTestId('pm-chat-artifacts').click()
  await expect(page.getByTestId('conversation-panel-sash')).toBeVisible()
  await expect.poll(async () => Math.abs(await offset() - initial)).toBeLessThan(4)
  await page.getByTestId('conversation-panel-sash').press('ArrowLeft')
  await expect.poll(async () => Math.abs(await offset() - initial)).toBeLessThan(4)
  await page.getByTestId('conversation-panel-close').click()
  await expect.poll(async () => Math.abs(await offset() - initial)).toBeLessThan(4)
})

test('Escape dismisses a version menu before the artifact pane and closing the pane leaves no floating menu', async ({ page }) => {
  await openConversation(page)
  await page.getByTestId('citation-open-source').nth(2).click()
  await page.locator('[data-artifact-id="artifact-3"]').click()
  const version = page.getByTestId('artifact-preview-version-chip-btn')
  const menu = page.getByTestId('artifact-preview-version-menu')
  await version.click()
  await expect(menu).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(menu).toHaveCount(0)
  await expect(page.getByTestId('conversation-artifact-panel')).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.getByTestId('conversation-artifact-panel')).toBeHidden()
  await page.getByTestId('pm-chat-artifacts').click()
  await version.click()
  await expect(menu).toBeVisible()
  await page.getByTestId('conversation-panel-close').click()
  await expect(menu).toHaveCount(0)
})
