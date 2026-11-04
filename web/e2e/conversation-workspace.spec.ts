import { test, expect, type FrameLocator, type Page, type WebSocketRoute } from '@playwright/test'
import type { SoloMessage } from '../src/lib/api/apiTypes'
import type { PreviewChatQueueItem, PreviewChatRow } from '../src/lib/inbox/appPreviewShare'
import type { Locator } from '@playwright/test'
import { routeApi } from './helpers/apiRoute'
import { installWorkspaceApiDefaults } from './helpers/workspaceApiDefaults'

const previewModelCatalog = {
  agent: { name: 'Demo', modelBinding: { kind: 'configuration', configurationId: 'shared-config' } },
  configurations: [{
    id: 'shared-config', name: 'Shared models', state: 'active', backend: 'claude_code',
    binding: { kind: 'platform', modelId: 'model-a', auxiliaryModelIds: ['model-b'] },
  }],
  cliModels: { claude_code: [{ id: 'model-a', name: 'Model A', vendor: 'anthropic' }, { id: 'model-b', name: 'Model B', vendor: 'google' }] },
}
const previewIssue = { kind: 'github_issue' as const, target: 'acme/web#12', title: 'Improve the heading', number: 12, repo: 'acme/web', url: 'https://github.com/acme/web/issues/12', state: 'open' }

async function choosePreviewModel(chat: FrameLocator, id: string, name: string) {
  const picker = chat.getByTestId('solo-model-select')
  await picker.getByRole('button', { name: 'Model A' }).click()
  await chat.getByTestId(`solo-model-option-${id}`).click()
  await expect(picker).toContainText(name)
}

async function expectChatComposerInsets(chat: FrameLocator, inputTestId: string) {
  const input = chat.getByTestId(inputTestId)
  await expect(input).toBeVisible()
  const insets = await input.evaluate(el => {
    const box = el.closest('.chat-composer')!.getBoundingClientRect()
    const rect = el.getBoundingClientRect()
    return [Math.round(rect.left - box.left), Math.round(rect.top - box.top)]
  })
  expect(insets).toEqual([17, 17])
}

async function expectMessageRailGap(rail: Locator, content: Locator) {
  await expect(rail).toBeVisible()
  const [railBox, contentBox] = await Promise.all([rail.boundingBox(), content.boundingBox()])
  expect(contentBox!.x - (railBox!.x + railBox!.width)).toBeGreaterThanOrEqual(14)
}

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

test('a direct Solo preview streams the reply to a message with a picked element', async ({ page, context }) => {
  test.setTimeout(60_000)
  await installWorkspaceApiDefaults(page)
  const grasp = 'http://127.0.0.1:5174'
  const direct = grasp
  const session = { id: 'solo-chat-check', title: 'Solo preview', agentName: 'Demo', status: 'running', artifactRunId: 'solo-preview-chat-check', sandboxId: 42 }
  const runId = session.artifactRunId
  const ticket = { ticket: 'solo-preview-ticket', runId, nodeId: 'solo', expiresAt: '2099-01-01T00:00:00Z' }
  const token = 'solo-preview-token'
  const shareBodies: unknown[] = []
  const modelBodies: unknown[] = []
  let savedTurn: SoloMessage[] = []
  let publish: ((frame: object) => void) | undefined
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
    if (path.endsWith('/messages')) return route.fulfill({ json: [{
      id: 'existing', role: 'user', content: 'Previous Solo message',
      images: [{ mimeType: 'image/png', name: 'preview.png', ref: 'blob:fixture-image' }],
    }, { id: 'legacy', role: 'user', content: 'Legacy text\n\nSelected page elements:\n/ · #old' }, ...savedTurn] })
    if (path.endsWith('/messages/existing/blobs/fixture-image')) return route.fulfill({
      contentType: 'image/png',
      body: Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/7fkAAAAASUVORK5CYII=', 'base64'),
    })
    if (path.endsWith('/turns')) return route.fulfill({ json: { waiting: 1 } })
    if (path.endsWith('/model-catalog')) return route.fulfill({ json: previewModelCatalog })
    if (path.endsWith('/model')) {
      const body = route.request().postDataJSON()
      modelBodies.push(body)
      return route.fulfill({ json: { ...session, currentModelId: body.modelId, currentModelSet: true } })
    }
    return route.fulfill({ json: session })
  })
  await context.routeWebSocket(`**/embed-api/solo/${runId}/chat*`, (ws) => {
    publish = frame => ws.send(JSON.stringify(frame))
    publish({ type: 'session', event: 'queue_state', seq: 0, busy: false, items: [], status: 'running', sandboxId: session.sandboxId })
  })

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
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(`${grasp}/public/approvals/app#t=isolated&lang=zh-CN`)
  await dialog.getByRole('radio', { name: /^主会话/ }).check()
  await dialog.getByRole('button', { name: '复制分享链接' }).click()
  await expect(dialog.getByRole('status')).toContainText('链接已复制')
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(`${grasp}/public/approvals/app#t=main&lang=zh-CN`)
  expect(shareBodies).toEqual([{ port: 8899, chatMode: 'isolated' }, { port: 8899, chatMode: 'main' }])
  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()
  const popup = page.waitForEvent('popup')
  await page.getByTestId('app-preview-direct-open').click()
  const preview = await popup
  const toolbar = preview.locator('grasp-preview-pick')
  const drawer = toolbar.frameLocator('[data-role="drawer"] iframe')
  await expect(drawer.getByText('Previous Solo message')).toBeVisible()
  await expectMessageRailGap(drawer.getByTestId('chat-message-rail-mark').first(), drawer.locator('[data-testid="solo-transcript"] > .chat-pane-column'))
  await expect(drawer.getByRole('img', { name: 'preview.png' })).toHaveAttribute('src', /^blob:/)
  await choosePreviewModel(drawer, 'model-b', 'Model B')
  expect(modelBodies).toEqual([{ modelId: 'model-b' }])
  await expect(drawer.getByTestId('solo-plus')).toBeVisible()
  await expectChatComposerInsets(drawer, 'solo-input')
  await drawer.getByTestId('solo-input').fill('Update this heading')
  await drawer.getByTestId('solo-plus').click()
  await drawer.getByTestId('solo-plus-pick').click()
  await expect(toolbar.locator('[data-role="drawer"]')).toBeHidden()
  await preview.getByRole('heading', { name: 'Feature to edit' }).click()
  await expect(drawer.getByTestId('solo-preview-pick')).toContainText('#feature')
  await expect(drawer.getByTestId('solo-input')).toHaveValue('Update this heading')
  await expect(drawer.getByTestId('solo-plus-menu')).toHaveCount(0)
  const submitted = preview.waitForRequest(request => request.url().endsWith('/' + runId + '/turns'))
  await drawer.getByTestId('solo-send').click()
  const body = (await submitted).postDataJSON()
  expect(body.text).toBe('Update this heading')
  expect(body.annotations).toEqual([expect.objectContaining({ selector: '#feature' })])
  const transcript = drawer.getByTestId('solo-transcript')
  await expect(transcript.getByTestId('chat-message-annotations')).toHaveText(['#old', '#feature'])
  await expect(transcript.getByText('Selected page elements:')).toHaveCount(0)
  const activity = transcript.getByTestId('solo-agent-activity')
  await expect(activity).toContainText('等待沙箱就绪')
  const item = { id: 'submitted', text: body.text, images: [], annotations: body.annotations }
  publish?.({ type: 'session', event: 'turn_begin', seq: 1, item, userMsgId: item.id })
  publish?.({ type: 'session', event: 'phase', seq: 2, phase: 'running' })
  await expect(activity).toContainText('Agent 正在处理')
  const reply = 'The heading has been updated.'
  publish?.({
    type: 'acp',
    seq: 3,
    data: {
      op: 'event',
      data: {
        type: 'session_update',
        update: { sessionUpdate: 'agent_message_chunk', content: { type: 'text', text: reply } },
      },
    },
  })
  await expect(transcript.getByTestId('solo-agent-message')).toHaveText(reply)
  const createdAt = new Date().toISOString()
  savedTurn = [
    { id: item.id, sessionId: session.id, role: 'user', content: item.text, annotations: item.annotations, createdAt },
    { id: 'reply', sessionId: session.id, role: 'assistant', content: reply, status: 'ok', createdAt },
  ]
  publish?.({ type: 'session', event: 'turn_done', seq: 4, userMsgId: item.id })
  await expect(activity).toHaveCount(0)
  await expect(transcript.getByTestId('solo-agent-message')).toHaveText(reply)
})

test('a signed-in workflow preview uses the regular chat model and reference pickers', async ({ page }) => {
  test.setTimeout(45_000)
  const grasp = 'http://127.0.0.1:5174'
  const key = 'entry-model-picker'
  const runId = 'live-e2e-' + key
  const modelPath = grasp + '/embed-api/runs/' + runId + '/nodes/preview'
  await page.addInitScript(() => localStorage.setItem('grasp-theme', 'dark'))
  await page.request.post(`${grasp}/__e2e/live/reset?key=${key}&caps=review`)
  await page.route(grasp + '/embed-api/session', async (route) => {
    const response = await route.fetch()
    await route.fulfill({ response, json: { ...await response.json(), kind: 'session' } })
  })
  await page.route(modelPath + '/model-catalog', (route) => {
    expect(route.request().headers().authorization).toBe(`Bearer ${runId}`)
    return route.fulfill({ json: previewModelCatalog })
  })
  await page.route(modelPath + '/references/search?*', (route) => {
    expect(route.request().headers().authorization).toBe(`Bearer ${runId}`)
    const lane = new URL(route.request().url()).searchParams.get('lane')
    return route.fulfill({ json: { lane, items: lane === 'github' ? [previewIssue] : [] } })
  })
  let saved: unknown
  await page.route(new RegExp(modelPath + '/model(?:\\?.*)?$'), (route) => {
    if (route.request().method() === 'POST') {
      saved = route.request().postDataJSON()
      return route.fulfill({ json: { currentModelSet: true, currentConfigurationId: 'shared-config', currentModelId: 'model-b' } })
    }
    return route.fulfill({ json: { currentModelSet: false } })
  })
  await page.goto(`${grasp}/live-variants.html?key=${key}`)
  const chat = page.frameLocator('grasp-preview-pick [data-role="drawer"] iframe')
  await expect(chat.getByTestId('clarify-attach-btn')).toBeVisible({ timeout: 15_000 })
  const picker = chat.getByTestId('solo-model-select')
  await picker.getByRole('button', { name: 'Model A' }).click()
  const claudeTrigger = picker.locator('[data-vendor-mark="anthropic"] img')
  const claudeOption = chat.getByTestId('solo-model-option-model-a').locator('img')
  const geminiOption = chat.getByTestId('solo-model-option-model-b')
  const themeButton = page.locator('grasp-preview-pick [data-role="drawer-theme"]')
  await expect(chat.locator('html')).not.toHaveClass(/\blight\b/)
  await themeButton.click()
  await expect(chat.locator('html')).toHaveClass(/\blight\b/)
  await expect(claudeTrigger).toHaveCSS('filter', 'none')
  await expect(claudeOption).toHaveCSS('filter', 'none')
  await expect(geminiOption.locator('img')).toHaveCSS('filter', 'none')
  await themeButton.click()
  await expect(claudeTrigger).toHaveCSS('filter', 'invert(1)')
  await expect(claudeOption).toHaveCSS('filter', 'invert(1)')
  await expect(geminiOption.locator('img')).toHaveCSS('filter', 'none')
  await geminiOption.click()
  await expect(picker).toContainText('Model B')
  expect(saved).toEqual({ iteration: 1, configurationId: 'shared-config', modelId: 'model-b', reasoningEffort: '' })
  const input = chat.getByTestId('clarify-input')
  await input.fill('Please check @')
  await chat.getByTestId('solo-reference-option-acme/web#12').click()
  const sent = page.waitForRequest(request => request.url().endsWith('/public/approvals/reply'))
  await input.press('Enter')
  expect((await sent).postDataJSON().references).toEqual([{ kind: 'github_issue', target: 'acme/web#12' }])
})

test('anonymous shares open the direct app and use its floating chat in both modes', async ({ browser }) => {
  test.setTimeout(60_000)
  const anonymous = await browser.newContext({ viewport: { width: 390, height: 820 }, locale: 'en-US' })
  try {
    await anonymous.addInitScript(() => { if (!localStorage.getItem('grasp-locale')) localStorage.setItem('grasp-locale', 'en') })
    const grasp = 'http://127.0.0.1:5174'
    const direct = 'http://127.0.0.1:19084'
    const previewUrl = direct + '/shared-app-fixture'
    const url = grasp + '/public/approvals/app'
    const runId = 'run-shared-app'
    const mainToken = 'a'.repeat(64)
    const guestToken = 'b'.repeat(64)
    const mainSession = 'drawer-main'
    const guestSession = 'drawer-isolated'
    const expiresAt = '2099-01-01T00:00:00Z'
    await anonymous.route(url, async (route) => {
      await route.fulfill({ response: await route.fetch({ url: grasp + '/live-entry-chat.html' }) })
    })
    await anonymous.route(url + '/open', (route) => {
      const headers = route.request().headers()
      expect(headers.cookie).toBeUndefined()
      expect(headers['x-gate-share-requested']).toBe('1')
      expect([mainToken, guestToken]).toContain(headers['x-gate-share-token'])
      const ticket = headers['x-gate-share-token'] === mainToken ? 'one-use-main' : 'one-use-guest'
      return route.fulfill({ json: { directUrl: previewUrl, ticket, expiresAt, runId, nodeId: 'solo' } })
    })
    await anonymous.route(previewUrl, (route) => route.fulfill({
      contentType: 'text/html',
      body: '<h1 id="app-title">Shared app fixture</h1><script defer src="/__grasp/preview-pick.js"></script>',
    }))
    await anonymous.route(direct + '/__grasp/preview-pick.js', async (route) => {
      await route.fulfill({ response: await route.fetch({ url: grasp + '/preview-pick.js' }) })
    })
    await anonymous.route(direct + '/__grasp/embed-origin?*', (route) => {
      const params = new URL(route.request().url()).searchParams
      expect(['one-use-main', 'one-use-guest']).toContain(params.get('ticket'))
      expect(params.get('node')).toBe('solo')
      return route.fulfill({ json: { origin: grasp, runId, nodeId: 'solo', kind: 'app-preview' } })
    })
    await anonymous.route(grasp + '/embed/runs/' + runId + '/nodes/solo/shared-chat', async (route) => {
      await route.fulfill({ response: await route.fetch({ url: grasp + '/live-entry-chat.html' }) })
    })
    await anonymous.route(grasp + '/embed/runs/' + runId + '/nodes/solo/image', async route => {
      await route.fulfill({ response: await route.fetch({ url: grasp + '/live-entry-chat.html' }) })
    })
    await anonymous.route(url + '/images/main-image/0', route => route.fulfill({ contentType: 'image/png',
      body: Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=', 'base64') }))
    await anonymous.route(grasp + '/embed-api/session', (route) => {
      const { ticket } = route.request().postDataJSON()
      return route.fulfill({ json: {
        kind: 'app-preview', token: ticket === 'one-use-main' ? mainSession : guestSession, runId, nodeId: 'solo', expiresAt,
      } })
    })
    await anonymous.route(url + '/session', (route) => {
      const token = route.request().headers()['x-gate-share-token']
      expect([mainSession, guestSession]).toContain(token)
      return route.fulfill({ json: { ticket: 'guest-chat-ticket', chatMode: token === mainSession ? 'main' : 'isolated', agentName: 'Demo' } })
    })
    await anonymous.route(url + '/references/search?*', route => {
      expect(new URL(route.request().url()).searchParams.get('lane')).toBe('github')
      return route.fulfill({ json: { lane: 'github', items: [previewIssue] } })
    })
    const mainRows: PreviewChatRow[] = [{ id: 'main-history', role: 'user', text: 'Original main conversation' },
      { id: 'main-image', role: 'user', text: 'Shared screenshot', images: [{ mimeType: 'image/png', name: 'Shared.png' }] }]
    let mainQueue: PreviewChatQueueItem[] = [{ id: 'queued-first', text: 'First waiting message', images: [{ mimeType: 'image/png' }] },
      { id: 'queued-second', text: 'Second waiting message' }]
    const queueSent = new WeakMap<WebSocketRoute, string>()
    const mainSockets = new Map<WebSocketRoute, PreviewChatRow[] | undefined>()
    let mainBusy = false
    const publishMain = () => {
      for (const [ws, previous] of mainSockets) {
        if (!previous) ws.send(JSON.stringify({ type: 'snapshot', history: { rows: mainRows, queue: mainQueue, busy: mainBusy } }))
        else {
          let start = 0
          while (start < previous.length && start < mainRows.length && JSON.stringify(previous[start]) === JSON.stringify(mainRows[start])) start++
          ws.send(JSON.stringify({ type: 'tail', start, rows: mainRows.slice(start), busy: mainBusy,
            queue: queueSent.get(ws) !== JSON.stringify(mainQueue) ? mainQueue : undefined }))
        }
        mainSockets.set(ws, structuredClone(mainRows))
        queueSent.set(ws, JSON.stringify(mainQueue))
      }
    }
    await anonymous.routeWebSocket('**/public/approvals/app/main-events', ws => {
      mainSockets.set(ws, undefined)
      ws.onClose(() => { mainSockets.delete(ws) })
      publishMain()
    })
    await anonymous.route(url + '/main-chat', (route) => {
      expect(route.request().headers()['x-gate-share-token']).toBe(mainSession)
      if (route.request().method() === 'POST') {
        const { text, annotations, images, references } = route.request().postDataJSON()
        expect(text).toContain('A message for the main conversation')
        expect(annotations).toEqual([expect.objectContaining({ selector: '#app-title' })])
        expect(references).toEqual([{ kind: 'github_issue', target: 'acme/web#12' }])
        expect(images).toEqual([expect.objectContaining({ name: 'main-notes.txt', data: Buffer.from('Main notes').toString('base64') })])
        mainRows.push({ id: 'main-new', role: 'user', text, annotations, images: [{ name: 'main-notes.txt', mimeType: 'text/plain' }], references: [previewIssue] })
        publishMain()
        return route.fulfill({ json: { ok: true } })
      }
      return route.fulfill({ json: { rows: mainRows, queue: mainQueue, busy: mainBusy } })
    })
    let mainChoice = { currentModelSet: false, currentConfigurationId: 'shared-config', currentModelId: '' }
    const modelWrites: string[] = []
    await anonymous.route(url + '/model-catalog', (route) => {
      const token = route.request().headers()['x-gate-share-token']
      if (token === mainSession) return route.fulfill({ json: previewModelCatalog })
      expect(token).toBe(guestSession)
      return route.fulfill({ json: { runtime: {
        default: 'model-a', models: [{ id: 'model-a', name: 'Model A' }, { id: 'model-b', name: 'Model B' }],
      } } })
    })
    await anonymous.route(url + '/model', (route) => {
      expect(route.request().headers()['x-gate-share-token']).toBe(mainSession)
      if (route.request().method() === 'POST') {
        const body = route.request().postDataJSON()
        expect(body.configurationId).toBe('shared-config')
        mainChoice = { ...mainChoice, currentModelSet: true, currentModelId: body.modelId }
        modelWrites.push('main:' + body.modelId)
      }
      return route.fulfill({ json: mainChoice })
    })
    let guestSocket: WebSocketRoute | undefined
    await anonymous.routeWebSocket('**/public/approvals/app/guest-chat', (ws) => ws.onMessage((data) => {
      const body = JSON.parse(String(data))
      if ('modelId' in body) {
        expect(body).toEqual({ modelId: 'model-b', reasoningEffort: '' })
        modelWrites.push('guest:' + body.modelId)
        ws.send(JSON.stringify({ type: 'model', ok: true }))
        return
      }
      expect(body.text).toContain('A separate idea')
      expect(body.annotations).toEqual([expect.objectContaining({ selector: '#app-title' })])
      expect(body.references).toEqual([{ kind: 'github_issue', target: 'acme/web#12' }])
      expect(body.images).toEqual([expect.objectContaining({ name: 'guest-notes.txt', data: Buffer.from('Guest notes').toString('base64') })])
      guestSocket = ws
      ws.send(JSON.stringify({ type: 'reply', text: '', parts: [{ kind: 'thought', text: 'Check the selected heading' }], startedAt: '2026-11-04T02:00:00Z' }))
    }))

    const main = await anonymous.newPage()
    await main.goto(url + '#t=' + mainToken + '&lang=zh-CN')
    await expect(main).toHaveURL(previewUrl)
    await expect(main.getByRole('heading', { name: 'Shared app fixture' })).toBeVisible()
    const mainToolbar = main.locator('grasp-preview-pick')
    const bar = mainToolbar.locator('[data-role="bar"]')
    const grip = mainToolbar.locator('[data-role="bar-grip"]')
    const original = (await bar.boundingBox())!
    const handle = (await grip.boundingBox())!
    await main.mouse.move(handle.x + handle.width / 2, handle.y + handle.height / 2)
    await main.mouse.down()
    await main.mouse.move(handle.x - 70, handle.y - 130, { steps: 5 })
    await main.mouse.up()
    const moved = (await bar.boundingBox())!
    expect(moved.x).toBeLessThan(original.x - 50)
    expect(moved.y).toBeLessThan(original.y - 100)
    await main.reload()
    await expect.poll(() => bar.boundingBox()).toEqual(moved)
    await grip.press('ArrowDown')
    await expect.poll(async () => (await bar.boundingBox())?.y).toBe(moved.y + 16)
    const mainDrawer = mainToolbar.frameLocator('[data-role="drawer"] iframe')
    await expect(mainToolbar.locator('[data-role="chat"]')).toHaveText('对话')
    await expect(mainDrawer.locator('html')).toHaveAttribute('lang', 'zh-CN')
    expect(await mainDrawer.locator('html').evaluate(() => localStorage.getItem('grasp-locale'))).toBe('en')
    await expect(mainDrawer.getByTestId('shared-app-chat-input')).toHaveAttribute('placeholder', /给 Demo 发消息/)
    await expect(mainToolbar.locator('[data-role="artifact"]')).toBeHidden()
    await expect(mainDrawer.getByText('Original main conversation')).toBeVisible()
    const queuePanel = mainDrawer.getByTestId('shared-app-chat-queue')
    await expect(queuePanel.getByTestId('clarify-queue-item')).toHaveCount(2)
    await expect(queuePanel.getByTestId('clarify-queue-item').first()).toContainText('First waiting message')
    await expect(queuePanel.getByTestId('clarify-queue-edit')).toHaveCount(0)
    mainQueue = [...mainQueue].reverse()
    publishMain()
    await expect(queuePanel.getByTestId('clarify-queue-item').first()).toContainText('Second waiting message')
    await main.reload()
    await expect(queuePanel.getByTestId('clarify-queue-item').first()).toContainText('Second waiting message')
    mainQueue = []
    publishMain()
    await expect(queuePanel).toHaveCount(0)
    await mainDrawer.getByRole('button', { name: /Shared\.png/ }).click()
    const imageModal = mainToolbar.locator('[data-role="image-modal"]')
    await expect(imageModal).toBeVisible()
    const picture = imageModal.frameLocator('iframe')
    await expect(picture.getByTestId('embed-image-preview-img')).toBeVisible()
    await expect(picture.locator('html')).toHaveAttribute('lang', 'zh-CN')
    expect(await mainToolbar.locator('[data-role="artifact-mask"]').evaluate(el => Number(getComputedStyle(el).zIndex))).toBeGreaterThan(
      await mainToolbar.locator('[data-role="drawer"]').evaluate(el => Number(getComputedStyle(el).zIndex)))
    const phoneImage = (await imageModal.boundingBox())!
    expect(phoneImage.x).toBeCloseTo(16)
    expect(phoneImage.width).toBeCloseTo(358)
    await main.setViewportSize({ width: 1440, height: 1000 })
    await expect.poll(async () => (await imageModal.boundingBox())?.width).toBe(960)
    expect((await imageModal.boundingBox())!.x).toBeCloseTo(240)
    await picture.locator('body').press('Escape')
    await expect(imageModal).toBeHidden()
    await expect(mainToolbar.locator('[data-role="drawer"]')).toBeVisible()
    await main.setViewportSize({ width: 390, height: 820 })
    await choosePreviewModel(mainDrawer, 'model-b', 'Model B')
    await expect.poll(() => modelWrites).toEqual(['main:model-b'])
    const mainInput = mainDrawer.getByTestId('shared-app-chat-input')
    await mainInput.fill('Draft before picking')
    await mainDrawer.getByTestId('shared-app-chat-plus').click()
    await expect(mainDrawer.getByTestId('shared-app-chat-plus-pick')).toHaveText('取点')
    await mainDrawer.getByTestId('shared-app-chat-plus-pick').click()
    await expect(mainToolbar.locator('[data-role="drawer"]')).toBeHidden()
    await main.keyboard.press('Escape')
    await expect(mainInput).toBeVisible()
    await expect(mainInput).toHaveValue('Draft before picking')
    await mainDrawer.getByTestId('shared-app-chat-plus').click()
    await mainDrawer.getByTestId('shared-app-chat-plus-pick').click()
    await main.getByRole('heading', { name: 'Shared app fixture' }).click()
    await expect(mainDrawer.getByTestId('shared-app-chat-pick')).toContainText('#app-title')
    await expectChatComposerInsets(mainDrawer, 'shared-app-chat-input')
    await expect(mainInput).toHaveValue('Draft before picking')
    await mainInput.fill('A message for the main conversation @')
    await mainDrawer.getByTestId('solo-reference-option-acme/web#12').click()
    await mainDrawer.getByTestId('shared-app-chat-plus').click()
    await expect(mainDrawer.getByTestId('shared-app-chat-plus-upload')).toBeVisible()
    await expect(mainDrawer.getByTestId('shared-app-chat-plus-session')).toHaveCount(0)
    await mainDrawer.locator('input[type="file"]').setInputFiles({ name: 'main-notes.txt', mimeType: 'text/plain', buffer: Buffer.from('Main notes') })
    await expect(mainDrawer.getByText('main-notes.txt')).toBeVisible()
    await mainDrawer.getByTestId('shared-app-chat-send').click()
    await expect(mainDrawer.getByText('A message for the main conversation')).toBeVisible()
    const sentBubble = mainDrawer.locator('.chat-user-message').filter({ hasText: 'A message for the main conversation' })
    await expect(sentBubble.getByTestId('chat-message-annotations')).toHaveText('#app-title')
    await expect(mainDrawer.getByText('Selected page elements:')).toHaveCount(0)
    const mainReply: PreviewChatRow = {
      id: 'main-streaming', role: 'assistant', text: '', streaming: true, startedAt: '2026-11-04T02:00:00Z',
      parts: [{ kind: 'thought', text: 'Inspecting the page' }],
    }
    mainRows.push(mainReply)
    mainBusy = true
    publishMain()
    await expect(mainDrawer.getByTestId('agent-timeline-thought')).toContainText('Inspecting the page')
    mainReply.parts!.push({ kind: 'tool', title: 'read_file', status: 'in_progress' })
    publishMain()
    await expect(mainDrawer.getByTestId('agent-tool-group')).toBeVisible()
    mainReply.text = 'The heading is being updated'
    mainReply.parts!.push({ kind: 'message', text: mainReply.text })
    publishMain()
    await expect(mainDrawer.getByText(mainReply.text, { exact: true })).toBeVisible()
    mainReply.text = 'The heading is now updated'
    mainReply.parts![1]!.status = 'completed'
    mainReply.parts![2]!.text = mainReply.text
    mainReply.streaming = false
    mainReply.at = mainReply.endedAt = '2026-11-04T02:00:46Z'
    mainReply.execution = { agentName: 'Recorded Agent', backend: 'opencode', modelId: 'recorded-model', startedAt: mainReply.startedAt }
    mainReply.usage = { inputTokens: 123, outputTokens: 45, cacheReadTokens: 0, cacheWriteTokens: 0 }
    mainReply.durationMs = 46000
    mainBusy = false
    publishMain()
    await expect(mainDrawer.getByText(mainReply.text, { exact: true })).toBeVisible()
    await expect(mainDrawer.getByTestId('agent-timeline-elapsed')).toContainText('46s')
    await mainDrawer.getByTestId('chat-message-info').click()
    const info = mainDrawer.getByTestId('chat-message-info-panel')
    await expect(info.locator('[data-info-row="agent"] dd')).toHaveText('Recorded Agent')
    await expect(info.locator('[data-info-row="model"] dd')).toHaveText('recorded-model')
    await expect(info.getByTestId('chat-message-token-total')).toHaveText('168')
    await mainDrawer.locator('body').press('Escape')
    await mainDrawer.getByTestId('agent-timeline-elapsed').click()
    await expect(mainDrawer.getByTestId('agent-tool-group')).toBeVisible()
    await main.reload()
    await expect(sentBubble.getByTestId('chat-message-annotations')).toHaveText('#app-title')
    await expect(mainDrawer.getByText('main-notes.txt')).toBeVisible()
    await expect(mainDrawer.locator('html')).toHaveAttribute('lang', 'zh-CN')
    expect(await mainDrawer.locator('html').evaluate(() => localStorage.getItem('grasp-locale'))).toBe('en')
    await expect(mainDrawer.getByTestId('agent-timeline-elapsed')).toContainText('46s')
    await mainToolbar.locator('[data-role="chat"]').click()
    await expect(mainToolbar.locator('[data-role="drawer"]')).toBeHidden()

    const guest = await anonymous.newPage()
    await guest.goto(url + '#t=' + guestToken + '&lang=en')
    await expect(guest).toHaveURL(previewUrl)
    const guestToolbar = guest.locator('grasp-preview-pick')
    const guestDrawer = guestToolbar.frameLocator('[data-role="drawer"] iframe')
    await expect(guestToolbar.locator('[data-role="chat"]')).toHaveText('Chat')
    await expect(guestDrawer.locator('html')).toHaveAttribute('lang', 'en')
    await guestDrawer.getByTestId('shared-app-chat-plus').click()
    await expect(guestDrawer.getByTestId('shared-app-chat-plus-pick')).toHaveText('Pick an element')
    await guestDrawer.getByTestId('shared-app-chat-plus-pick').click()
    await expect(guestToolbar.locator('[data-role="drawer"]')).toBeHidden()
    await guest.getByRole('heading', { name: 'Shared app fixture' }).click()
    await expect(guestDrawer.getByTestId('shared-app-chat-pick')).toContainText('#app-title')
    await choosePreviewModel(guestDrawer, 'model-b', 'Model B')
    await expect.poll(() => modelWrites).toEqual(['main:model-b', 'guest:model-b'])
    await expect(guestDrawer.getByText('Original main conversation')).toHaveCount(0)
    await guestDrawer.getByTestId('shared-app-chat-input').fill('A separate idea @')
    await guestDrawer.getByTestId('solo-reference-option-acme/web#12').click()
    await guestDrawer.locator('input[type="file"]').setInputFiles({ name: 'guest-notes.txt', mimeType: 'text/plain', buffer: Buffer.from('Guest notes') })
    await expect(guestDrawer.getByText('guest-notes.txt')).toBeVisible()
    await guestDrawer.getByTestId('shared-app-chat-send').click()
    await expect(guestDrawer.getByTestId('agent-timeline-thought')).toContainText('Check the selected heading')
    const guestParts = [{ kind: 'thought' as const, text: 'Check the selected heading' }, { kind: 'tool' as const, title: 'edit_file', status: 'in_progress' }]
    guestSocket!.send(JSON.stringify({ type: 'reply', text: '', parts: guestParts }))
    await expect(guestDrawer.getByTestId('agent-tool-group')).toBeVisible()
    guestSocket!.send(JSON.stringify({ type: 'reply', text: 'Visitor-only', parts: [...guestParts, { kind: 'message', text: 'Visitor-only' }] }))
    await expect(guestDrawer.getByText('Visitor-only', { exact: true })).toBeVisible()
    guestParts[1]!.status = 'completed'
    guestSocket!.send(JSON.stringify({ type: 'done', text: 'Visitor-only reply', parts: [...guestParts, { kind: 'message', text: 'Visitor-only reply' }], endedAt: '2026-11-04T02:00:46Z',
      execution: { agentName: 'Recorded Guest', backend: 'opencode', modelId: 'guest-recorded-model' }, durationMs: 46000,
      usage: { inputTokens: 0, outputTokens: 0, cacheReadTokens: 0, cacheWriteTokens: 0 } }))
    await expect(guestDrawer.getByText('Visitor-only reply')).toBeVisible()
    await expect(guestDrawer.getByTestId('agent-timeline-elapsed')).toContainText('46s')
    await guestDrawer.getByTestId('chat-message-info').click()
    const guestInfo = guestDrawer.getByTestId('chat-message-info-panel')
    await expect(guestInfo.locator('[data-info-row="agent"] dd')).toHaveText('Recorded Guest')
    await expect(guestInfo.locator('[data-info-row="model"] dd')).toHaveText('guest-recorded-model')
    await expect(guestInfo.getByTestId('chat-message-token-total')).toHaveText('0')
    await guestDrawer.locator('body').press('Escape')
    await guestDrawer.getByTestId('agent-timeline-elapsed').click()
    await expect(guestDrawer.getByTestId('agent-tool-group')).toBeVisible()
    await expect(guestDrawer.getByTestId('chat-message-annotations')).toHaveText('#app-title')
    await expect(guestDrawer.getByText('Selected page elements:')).toHaveCount(0)
    await guest.reload()
    await expect(guestDrawer.getByTestId('shared-app-chat-input')).toBeVisible()
    await expect(guestDrawer.locator('html')).toHaveAttribute('lang', 'en')
    await expect(guestDrawer.getByText('Visitor-only reply')).toHaveCount(0)
    await expect(guestDrawer.getByText('Original main conversation')).toHaveCount(0)
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
    await expectMessageRailGap(page.getByTestId('chat-message-rail-mark').first(), page.getByTestId('pm-message-scroller').locator('[data-chat-anchor]').first())
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
