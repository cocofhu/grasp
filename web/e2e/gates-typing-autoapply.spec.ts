/**
 * Regression: a silent inbox auto-apply must never interrupt typing.
 * Repro (GraspReview r2): type a draft while new gates arrive (update held),
 * clear it with Ctrl+A / Backspace and keep typing → the held update applied,
 * the chat re-rendered, the composer lost focus and the following keys were lost.
 * Real node-conversation entry (workspace-config harness = full app shell), mocked API.
 */
import { expect, test, type Page, type WebSocketRoute } from '@playwright/test'
import { installWorkspaceApiDefaults } from './helpers/workspaceApiDefaults'
import { routeApi } from './helpers/apiRoute'
import { CLARIFY_CAPS } from '../src/test/capsFixtures'
import { createDialogueServer, type FakeRow, type FakeTurn } from './helpers/dialogueServer'

const NOW = '2026-10-21T01:00:00Z'
const RUN = 'run-typing-hold'
const NODE = 'clarify'

function clarifyRow(runId: string, title: string) {
  return {
    type: 'clarify',
    kind: 'clarify',
    runId,
    nodeId: NODE,
    iteration: 1,
    workflowId: 'wf-typing',
    workflowName: '输入保护工作流',
    runTitle: title,
    label: '需求澄清',
    done: false,
    requestedAt: NOW,
    updatedAt: NOW,
    tags: [],
  }
}

function unfinishedRun(item: Record<string, unknown>) {
  return {
    id: item.runId,
    workflowId: item.workflowId,
    workflowName: item.workflowName,
    title: item.runTitle,
    status: 'waiting_human',
    startedAt: NOW,
    lastActivityAt: NOW,
    currentNode: { id: item.nodeId, name: item.label, iteration: item.iteration ?? 1 },
  }
}

async function mockInbox(page: Page, state: { items: Record<string, unknown>[] }) {
  await installWorkspaceApiDefaults(page)
  await routeApi(page, '**/api/**', async (route) => {
    const req = route.request()
    const url = new URL(req.url())
    const path = url.pathname
    if (req.method() === 'GET' && path === '/api/pending-nodes') {
      await route.fulfill({ json: { items: state.items, total: state.items.length } })
      return
    }
    // 正在执行只跟随未结束运行查询。新到达的待处理项要出现在侧栏，名单里必须有对应运行。
    if (req.method() === 'GET' && (path === '/api/runs' || path === '/api/runs/')) {
      const status = url.searchParams.get('status') ?? ''
      const unfinished = ['queued', 'running', 'waiting_human'].some((s) => status.split(',').includes(s))
      const items = unfinished ? state.items.map(unfinishedRun) : []
      await route.fulfill({ json: { items, total: items.length, page: 1, pageSize: 50, hasMore: false } })
      return
    }
    if (req.method() === 'GET' && /\/api\/runs\/[^/]+\/nodes\/[^/]+\/context/.test(path)) {
      await route.fulfill({
        json: {
          type: 'clarify',
          status: 'waiting_human',
          nodes: [{ id: NODE, type: 'agent', caps: CLARIFY_CAPS, label: '需求澄清', position: { x: 0, y: 0 }, config: {} }],
          artifacts: [],
          nodeExecutions: { [NODE]: [{ nodeId: NODE, iteration: 1, status: 'waiting_human', outputs: {} }] },
          clarify: {
            nodeId: NODE,
            iteration: 1,
            turns: [{ role: 'assistant', text: '请补充你的需求。', at: NOW }],
            done: false,
            label: '需求澄清',
          },
        },
      })
      return
    }
    await route.fallback()
  })
}

/** Ask the inbox to peek now (same path as returning to the tab). */
const peekNow = (page: Page) => page.evaluate(() => window.dispatchEvent(new Event('focus')))

test('clearing the draft and typing on keeps focus and every character while updates are pending', async ({ page }) => {
  test.setTimeout(45_000)
  const state = { items: [clarifyRow(RUN, '输入保护验收')] }
  await mockInbox(page, state)
  await page.setViewportSize({ width: 1280, height: 800 })
  await page.goto(`/workspace-config.html?view=${encodeURIComponent(`/runs/${RUN}/nodes/${NODE}`)}`)

  const input = page.getByTestId('clarify-input')
  await expect(input).toBeVisible({ timeout: 15_000 })
  await input.click()
  await page.keyboard.type('有草稿', { delay: 20 })
  await input.evaluate((el) => {
    ;(el as HTMLElement).dataset.original = 'yes'
    const w = window as unknown as { __blurs: number }
    w.__blurs = 0
    el.addEventListener('blur', () => w.__blurs++)
  })

  // New gates arrive while a draft is in the composer: the list update is held.
  state.items = [clarifyRow('run-arrived-a', '新到达 A'), clarifyRow('run-arrived-b', '新到达 B'), ...state.items]
  await peekNow(page)
  await page.waitForTimeout(600)
  await expect(page.getByTestId('nav-pending-row').filter({ hasText: '新到达 A' })).toHaveCount(0)

  // Clear the draft and keep typing straight away.
  await page.keyboard.press('ControlOrMeta+A')
  await page.keyboard.press('Backspace')
  await page.keyboard.type('继续输入不丢字', { delay: 60 })
  await page.waitForTimeout(300)
  await page.keyboard.type('还在打', { delay: 60 })
  await expect(input).toHaveValue('继续输入不丢字还在打')
  await expect(input).toBeFocused()

  // Clear again and stop typing: once input is idle the held update applies
  // without moving focus, the caret or the composer element.
  await page.keyboard.press('ControlOrMeta+A')
  await page.keyboard.press('Backspace')
  // 侧栏成员来自未结束运行查询，约 15s 轮询才写回；空闲后等到这一轮即可。
  await expect(page.getByTestId('nav-pending-row').filter({ hasText: '新到达 A' })).toBeVisible({ timeout: 20_000 })
  await expect(input).toBeFocused()
  await page.keyboard.type('更新后继续', { delay: 30 })
  await expect(input).toHaveValue('更新后继续')
  expect(await input.evaluate((el) => (el as HTMLElement).dataset.original)).toBe('yes')
  expect(await page.evaluate(() => (window as unknown as { __blurs: number }).__blurs)).toBe(0)
  expect(await input.evaluate((el) => (el as HTMLTextAreaElement).selectionStart)).toBe('更新后继续'.length)
})

test('a healthy socket recovers startup, missed ongoing output and a missed terminal without reloading', async ({ page }) => {
  const state = { items: [{ ...clarifyRow(RUN, '启动与消息恢复'), state: 'starting' }] }
  await mockInbox(page, state)
  const sockets: WebSocketRoute[] = []
  await page.routeWebSocket(new RegExp(`/api/runs/${RUN}/events`), ws => { sockets.push(ws) })
  const image = { data: 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+j4e8AAAAASUVORK5CYII=', mimeType: 'image/png', name: 'opening.png' }
  const opening = { text: '检查这张图', images: [image] }
  const openingHuman: FakeRow = { role: 'human', ...opening, turnId: 'opening' }
  let starting = true
  const dialogue = createDialogueServer(RUN, NODE, { rows: [openingHuman] })
  let queued: FakeTurn[] = []
  let acceptedId = ''
  const running = (thought: string) => dialogue.set({
    rows: [openingHuman, { role: 'agent', text: '', thought, turnId: 'opening', streaming: true }],
    turns: [{ turnId: 'opening', phase: 'running', item: opening }, ...queued],
  })
  await routeApi(page, '**/api/**', async route => {
    const request = route.request()
    const pathname = new URL(request.url()).pathname
    if (pathname === `/api/runs/${RUN}/nodes/${NODE}/context`) {
      await route.fulfill({ json: {
        type: 'clarify', status: 'waiting_human',
        ...(starting ? {} : { dialogueByNode: dialogue.byNode() }),
        nodes: [{ id: NODE, type: 'agent', caps: CLARIFY_CAPS, label: '需求澄清', config: {} }],
        artifacts: [], nodeExecutions: {},
        clarify: { nodeId: NODE, iteration: 1, starting, done: false, label: '需求澄清', turns: [] },
      } })
      return
    }
    if (pathname === `/api/runs/${RUN}/nodes/${NODE}/dialogue`) {
      await route.fulfill({ json: dialogue.frame() })
      return
    }
    if (pathname === `/api/runs/${RUN}/nodes/${NODE}/reply` && request.method() === 'POST') {
      const body = request.postDataJSON() as { text: string; clientMessageId: string }
      acceptedId = body.clientMessageId
      queued = [{ turnId: 'queued-next', phase: 'queued', item: { text: body.text, clientMessageId: body.clientMessageId } }]
      dialogue.set({ ...dialogue.get(), turns: [...(dialogue.get().turns ?? []), ...queued] })
      sockets.at(-1)!.send(JSON.stringify(dialogue.frame()))
      await route.fulfill({ json: { status: 'queued', waiting: 1 } })
      return
    }
    await route.fallback()
  })
  await page.goto(`/workspace-config.html?view=${encodeURIComponent(`/runs/${RUN}/nodes/${NODE}`)}`)
  await expect(page.getByTestId('clarify-boot-loader')).toBeVisible({ timeout: 15_000 })
  await expect.poll(() => sockets.length).toBeGreaterThan(0)

  // Omit status pushes. The selected object still says starting while the
  // server's current context and pending row have moved forward.
  starting = false
  state.items[0]!.state = 'replying'
  running('已启动，正在检查图片。')
  sockets.at(-1)!.send(JSON.stringify({ type: 'heartbeat' }))
  await expect(page.getByTestId('clarify-input')).toBeVisible()
  await expect(page.getByTestId('agent-timeline-thought')).toContainText('已启动，正在检查图片。')
  await expect(page.getByTestId('clarify-scroller').locator('img')).toHaveCount(1)
  const connectedAfterStartup = sockets.length

  const input = page.getByTestId('clarify-input')
  await input.fill('只修改前端')
  await input.press('Enter')
  await expect(page.getByTestId('clarify-review-queue')).toContainText('只修改前端')
  await input.fill('草稿保持不动')
  // This state's push is lost; the heartbeat read brings it.
  running('工具已经完成，继续核对页面；这段输出的推送丢失了。')
  sockets.at(-1)!.send(JSON.stringify({ type: 'heartbeat' }))
  await expect(page.getByTestId('agent-timeline-thought')).toContainText('工具已经完成，继续核对页面')
  await expect(page.getByTestId('clarify-review-queue')).toContainText('只修改前端')
  await expect(page.getByTestId('clarify-busy-status')).toBeVisible()
  await expect(input).toHaveValue('草稿保持不动')

  // Both turns really finished on the server; their pushes were lost.
  dialogue.set({
    rows: [openingHuman,
      { role: 'agent', text: '图片已检查。', turnId: 'opening' },
      { role: 'human', text: '只修改前端', clientMessageId: acceptedId, turnId: 'queued-next' },
      { role: 'agent', text: '前端修改已完成。', turnId: 'queued-next' },
    ],
    turns: [{ turnId: 'queued-next', phase: 'ended', outcome: 'ok', clientMessageId: acceptedId }],
  })
  sockets.at(-1)!.send(JSON.stringify({ type: 'heartbeat' }))
  await expect(page.getByTestId('clarify-review-queue')).toHaveCount(0)
  await expect(page.getByTestId('clarify-busy-status')).toHaveCount(0)
  await expect(page.getByText('前端修改已完成。', { exact: true })).toBeVisible()
  await expect(input).toHaveValue('草稿保持不动')
  expect(sockets).toHaveLength(connectedAfterStartup)
  expect(await page.evaluate(() => performance.getEntriesByType('navigation').length)).toBe(1)
})

test('tool-only recovery survives repeated identical states and three identical accepted sends settle once each', async ({ page }) => {
  test.setTimeout(45_000)
  await mockInbox(page, { items: [{ ...clarifyRow(RUN, '历史消息故障组合回归'), state: 'replying' }] })
  const sockets: WebSocketRoute[] = []
  await page.routeWebSocket(new RegExp(`/api/runs/${RUN}/events`), ws => { sockets.push(ws) })
  const opening = { clientMessageId: 'opening-client', text: '先检查代码' }
  const openingHuman: FakeRow = { role: 'human', ...opening, turnId: 'opening' }
  const accepted: { id: string; clientMessageId: string; text: string }[] = []
  const toolReply = (output: string): FakeRow => ({ role: 'agent', text: '', turnId: 'opening', streaming: true,
    parts: [{ kind: 'tool', title: 'Shell', status: 'in_progress', input: 'npm test', output }] })
  const dialogue = createDialogueServer(RUN, NODE)
  const running = (output: string) => dialogue.set({
    rows: [openingHuman, toolReply(output)],
    turns: [{ turnId: 'opening', phase: 'running', item: opening },
      ...accepted.map((a): FakeTurn => ({ turnId: a.id, phase: 'queued', item: { text: a.text, clientMessageId: a.clientMessageId } }))],
  })
  running('line one')
  let holdContext: Promise<void> | undefined
  let heldReads = 0
  await routeApi(page, '**/api/**', async route => {
    const request = route.request()
    const pathname = new URL(request.url()).pathname
    if (pathname === `/api/runs/${RUN}/conversation-chain`) {
      await route.fulfill({ json: { runId: RUN, status: 'waiting_human',
        currentNode: { id: NODE, name: '需求澄清', iteration: 1, kind: 'clarify', mode: 'manual', state: 'waiting_human' },
        segments: [{ nodeId: NODE, name: '需求澄清', iteration: 1, kind: 'clarify', status: 'waiting_human',
          startedAt: NOW, turns: [], boot: { status: 'ready', readyAt: NOW } }],
      } })
      return
    }
    if (pathname === `/api/runs/${RUN}/nodes/${NODE}/sandbox`) {
      await route.fulfill({ json: { status: 'running', updatedAt: NOW } })
      return
    }
    if (pathname === `/api/runs/${RUN}/nodes/${NODE}/context`) {
      if (holdContext) { heldReads++; await holdContext }
      await route.fulfill({ json: {
        type: 'clarify', status: 'waiting_human',
        dialogueByNode: dialogue.byNode(),
        nodes: [{ id: NODE, type: 'agent', caps: CLARIFY_CAPS, label: '需求澄清', config: {} }],
        artifacts: [], nodeExecutions: {},
        clarify: { nodeId: NODE, iteration: 1, done: false, label: '需求澄清', turns: [] },
      } })
      return
    }
    if (pathname === `/api/runs/${RUN}/nodes/${NODE}/dialogue`) {
      await route.fulfill({ json: dialogue.frame() })
      return
    }
    if (pathname === `/api/runs/${RUN}/nodes/${NODE}/reply` && request.method() === 'POST') {
      const body = request.postDataJSON() as { text: string; clientMessageId: string }
      accepted.push({ id: `accepted-${accepted.length + 1}`, text: body.text, clientMessageId: body.clientMessageId })
      running('line one')
      // The WebSocket push deliberately overtakes the HTTP response.
      sockets.at(-1)!.send(JSON.stringify(dialogue.frame()))
      await route.fulfill({ json: { status: 'queued', waiting: accepted.length } })
      return
    }
    await route.fallback()
  })
  await page.goto(`/workspace-config.html?view=${encodeURIComponent(`/runs/${RUN}/nodes/${NODE}`)}`)
  const input = page.getByTestId('clarify-input')
  await expect(input).toBeVisible({ timeout: 15_000 })
  await expect(page.getByTestId('agent-tool-group-head')).toBeVisible()
  await page.getByTestId('agent-tool-group-head').click()
  await page.getByTestId('agent-tool-row-toggle').click()
  await expect(page.getByTestId('agent-tool-output')).toContainText('line one')
  const connections = sockets.length
  const push = (frame: unknown) => sockets.at(-1)!.send(JSON.stringify(frame))
  // The same state arriving again changes nothing.
  const repeat = () => push(dialogue.frame())
  repeat()
  push({ type: 'heartbeat' })
  for (let count = 1; count <= 3; count++) {
    await input.fill('继续检查')
    await input.press('Enter')
    await expect.poll(() => accepted.length).toBe(count)
  }
  expect(new Set(accepted.map(item => item.clientMessageId)).size).toBe(3)
  // Waiters behind the active reply stay in the queue panel only.
  await expect(page.getByTestId('clarify-queue-item').filter({ hasText: '继续检查' })).toHaveCount(3)
  await expect(page.getByTestId('clarify-human-message').filter({ hasText: '继续检查' })).toHaveCount(0)
  await input.fill('保留这段未发送的草稿')

  let release!: () => void
  holdContext = new Promise(resolve => { release = resolve })
  const stale = dialogue.frame()
  running('line one\nline two')
  push({ type: 'heartbeat' })
  await expect.poll(() => heldReads).toBeGreaterThan(0)
  push(stale)
  holdContext = undefined
  release()
  await expect(page.getByTestId('agent-tool-output')).toContainText('line two')
  await expect(input).toHaveValue('保留这段未发送的草稿')
  await expect(page.getByTestId('clarify-review-queue')).toContainText('继续检查')

  // All three same-text requests complete while this viewer misses their pushes.
  dialogue.set({
    rows: [openingHuman, { role: 'agent', text: '首轮检查完成', turnId: 'opening' },
      ...accepted.flatMap((item): FakeRow[] => [
        { role: 'human', text: item.text, clientMessageId: item.clientMessageId, turnId: item.id },
        { role: 'agent', text: '检查完成', turnId: item.id },
      ])],
    turns: accepted.map((item): FakeTurn => ({ turnId: item.id, phase: 'ended', outcome: 'ok', clientMessageId: item.clientMessageId })),
  })
  push({ type: 'heartbeat' })
  await expect(page.getByTestId('clarify-review-queue')).toHaveCount(0)
  await expect(page.getByTestId('clarify-human-message').filter({ hasText: '继续检查' })).toHaveCount(3)
  await expect(page.getByText('检查完成', { exact: true })).toHaveCount(3)
  await expect(page.getByTestId('clarify-busy-status')).toHaveCount(0)
  // A delayed older state cannot bring the queue back.
  push(stale)
  await expect(page.getByTestId('clarify-review-queue')).toHaveCount(0)
  await expect(input).toHaveValue('保留这段未发送的草稿')
  expect(accepted).toHaveLength(3)
  expect(sockets).toHaveLength(connections)
  expect(await page.evaluate(() => performance.getEntriesByType('navigation').length)).toBe(1)
})
