/**
 * Every place the clarify / review chat is shown: a reply streams, the server
 * ends it with outcome "empty", the page shows「本轮没有输出」, and after a
 * reload (state read back from REST and the reconnect frame) it still does.
 * The failure card depends only on the server's outcome, never on timing.
 * Real app shell or share-page harness; API and run socket mocked.
 */
import { expect, test, type Page, type WebSocketRoute } from '@playwright/test'
import { installWorkspaceApiDefaults } from './helpers/workspaceApiDefaults'
import { routeApi } from './helpers/apiRoute'
import { CLARIFY_CAPS } from '../src/test/capsFixtures'
import { createDialogueServer, type FakeDialogue } from './helpers/dialogueServer'

const NOW = '2026-10-21T01:00:00Z'
const RUN = 'run-empty-fail'
const NODE = 'clarify'

const human = { role: 'human' as const, text: '做登录页', turnId: 't1' }
const running: FakeDialogue = {
  rows: [human, { role: 'agent', text: '正在梳理登录需求', turnId: 't1', streaming: true }],
  turns: [{ turnId: 't1', phase: 'running', item: { text: human.text } }],
}
const empty: FakeDialogue = {
  rows: [human, { role: 'agent', text: '', turnId: 't1', outcome: 'empty' }],
  turns: [{ turnId: 't1', phase: 'ended', outcome: 'empty' }],
}

const nodes = [
  { id: 'input', type: 'input', label: '输入', position: { x: 0, y: 0 }, config: {} },
  { id: NODE, type: 'agent', caps: CLARIFY_CAPS, label: '需求澄清', position: { x: 200, y: 0 }, config: {} },
]
const currentNode = { id: NODE, name: '需求澄清', iteration: 1, kind: 'clarify', mode: 'manual', state: 'waiting_human' }

function runSnapshot(dialogue: ReturnType<typeof createDialogueServer>) {
  return {
    id: RUN,
    workflowId: 'wf-empty',
    workflowName: '澄清工作流',
    title: '登录页',
    status: 'waiting_human',
    trigger: 'manual',
    startedAt: NOW,
    lastActivityAt: NOW,
    nodes,
    edges: [{ id: 'e0', source: 'input', target: NODE }],
    nodeRuns: {
      input: { nodeId: 'input', status: 'completed', startedAt: NOW, durationSec: 1, varsSnapshot: {}, outputs: {} },
      [NODE]: { nodeId: NODE, status: 'waiting_human', startedAt: NOW, varsSnapshot: {}, outputs: {} },
    },
    nodeExecutions: { [NODE]: [{ nodeId: NODE, iteration: 1, status: 'waiting_human', outputs: {} }] },
    artifacts: [],
    trace: [],
    vars: [],
    currentNode,
    clarify: { nodeId: NODE, iteration: 1, turns: [], done: false, label: '需求澄清' },
    clarifyByNode: { [NODE]: { nodeId: NODE, iteration: 1, turns: [], done: false, label: '需求澄清' } },
    dialogueByNode: dialogue.byNode(),
  }
}

/** One server: REST and the run socket read the same dialogue; a new socket gets it at once. */
async function mockServer(page: Page) {
  const dialogue = createDialogueServer(RUN, NODE, { rows: [] })
  const sockets: WebSocketRoute[] = []
  await installWorkspaceApiDefaults(page)
  await page.routeWebSocket(new RegExp(`/api/runs/${RUN}/events`), (ws) => {
    sockets.push(ws)
    ws.send(JSON.stringify(dialogue.frame()))
  })
  await routeApi(page, '**/api/**', async (route) => {
    const path = new URL(route.request().url()).pathname
    const json = (body: unknown) => route.fulfill({ json: body })
    if (path === `/api/runs/${RUN}`) return json(runSnapshot(dialogue))
    if (path === `/api/runs/${RUN}/nodes/${NODE}/dialogue`) return json(dialogue.frame())
    if (path === `/api/runs/${RUN}/nodes/${NODE}/context`) {
      return json({
        type: 'clarify', status: 'waiting_human', nodes, artifacts: [],
        nodeExecutions: { [NODE]: [{ nodeId: NODE, iteration: 1, status: 'waiting_human', outputs: {} }] },
        clarify: { nodeId: NODE, iteration: 1, turns: [], done: false, label: '需求澄清' },
        dialogueByNode: dialogue.byNode(),
      })
    }
    if (path === `/api/runs/${RUN}/conversation-chain`) {
      return json({ runId: RUN, status: 'waiting_human', currentNode,
        segments: [{ nodeId: NODE, name: '需求澄清', iteration: 1, kind: 'clarify', status: 'waiting_human',
          startedAt: NOW, turns: [], boot: { status: 'ready', readyAt: NOW } }] })
    }
    if (path === `/api/runs/${RUN}/nodes/${NODE}/sandbox`) return json({ status: 'running', updatedAt: NOW })
    if (/\/api\/runs\/[^/]+\/nodes\/[^/]+\/events$/.test(path)) return json({ events: [], live: false })
    if (path === '/api/pending-nodes') {
      return json({ items: [{ type: 'clarify', kind: 'clarify', runId: RUN, nodeId: NODE, iteration: 1,
        workflowId: 'wf-empty', workflowName: '澄清工作流', runTitle: '登录页', label: '需求澄清', done: false,
        requestedAt: NOW, updatedAt: NOW, tags: [] }], total: 1 })
    }
    return route.fallback()
  })
  const push = (next: FakeDialogue) => {
    dialogue.set(next)
    sockets.at(-1)?.send(JSON.stringify(dialogue.frame()))
  }
  return { push, sockets, dialogue }
}

async function expectEmptyFail(page: Page) {
  const card = page.getByTestId('clarify-empty-fail')
  await expect(card).toHaveCount(1)
  await expect(card).toBeVisible()
  await expect(page.getByTestId('clarify-empty-fail-title')).toHaveText('本轮没有输出')
  await expect(page.getByTestId('clarify-stream-caret')).toHaveCount(0)
}

/** Streams, ends empty, survives a reload. `open` lands on the chat. */
async function streamEndEmptyReload(page: Page, open: () => Promise<void>, reselect?: () => Promise<void>) {
  const { push, sockets } = await mockServer(page)
  await page.setViewportSize({ width: 1280, height: 860 })
  await open()
  await expect.poll(() => sockets.length).toBeGreaterThan(0)
  push(running)
  await expect(page.getByText('正在梳理登录需求')).toBeVisible({ timeout: 15_000 })
  await expect(page.getByTestId('clarify-empty-fail')).toHaveCount(0)

  push(empty)
  await expectEmptyFail(page)
  await expect(page.getByText('正在梳理登录需求')).toHaveCount(0)

  await page.reload()
  await reselect?.()
  await expectEmptyFail(page)
  await expect(page.getByText('做登录页').first()).toBeVisible()
}

const shell = (view: string) => `/workspace-config.html?view=${encodeURIComponent(view)}`

test('inbox node conversation: empty reply shows the failure card before and after reload', async ({ page }) => {
  await streamEndEmptyReload(page, () => page.goto(shell(`/runs/${RUN}/nodes/${NODE}`)))
})

test('run detail: empty reply shows the failure card before and after reload', async ({ page }) => {
  await streamEndEmptyReload(page, async () => {
    await page.goto(shell(`/runs/${RUN}?node=${NODE}`))
    await page.getByRole('button', { name: 'Agent交互' }).click()
  }, () => page.getByRole('button', { name: 'Agent交互' }).click())
})

test('run detail LLM transcript: empty reply shows the failure card before and after reload', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('grasp-run-view-mode', 'llm'))
  await streamEndEmptyReload(page, async () => {
    await page.goto(shell(`/runs/${RUN}`))
    const llm = page.getByTestId('view-mode-llm')
    await expect(llm).toBeVisible({ timeout: 15_000 })
    await llm.click()
    await expect(page.getByTestId('run-llm-transcript')).toBeVisible()
  })
})

test('run live page: empty reply shows the failure card before and after reload', async ({ page }) => {
  await streamEndEmptyReload(page, () => page.goto(shell(`/runs/${RUN}/live?node=${NODE}`)))
})

const streamingMore: FakeDialogue = {
  rows: [human, { role: 'agent', text: '已确认需要验证码', turnId: 't1', streaming: true }],
  turns: [{ turnId: 't1', phase: 'running', item: { text: human.text } }],
}
const sealedOk: FakeDialogue = {
  rows: [human, { role: 'agent', text: '登录页需要账号密码与验证码', turnId: 't1', outcome: 'ok' }],
  turns: [{ turnId: 't1', phase: 'ended', outcome: 'ok' }],
}

/**
 * The socket drops mid-reply while the reply keeps going, then the server
 * restarts (newer epoch, seq from 1) with the reply sealed. Each reconnect
 * frame brings the page up to date without a reload or a failure card.
 */
async function dropRestartRecover(page: Page, open: () => Promise<void>) {
  const { push, sockets, dialogue } = await mockServer(page)
  await page.setViewportSize({ width: 1280, height: 860 })
  await open()
  await expect.poll(() => sockets.length).toBeGreaterThan(0)
  push(running)
  await expect(page.getByText('正在梳理登录需求')).toBeVisible({ timeout: 15_000 })
  await page.evaluate(() => { (window as unknown as { __sameDocument: boolean }).__sameDocument = true })

  let seen = sockets.length
  dialogue.set(streamingMore)
  await sockets.at(-1)!.close()
  await expect.poll(() => sockets.length, { timeout: 20_000 }).toBeGreaterThan(seen)
  await expect(page.getByText('已确认需要验证码')).toBeVisible()
  await expect(page.getByTestId('clarify-empty-fail')).toHaveCount(0)

  seen = sockets.length
  dialogue.restart(sealedOk)
  await sockets.at(-1)!.close()
  await expect.poll(() => sockets.length, { timeout: 20_000 }).toBeGreaterThan(seen)
  await expect(page.getByText('登录页需要账号密码与验证码')).toBeVisible()
  await expect(page.getByText('已确认需要验证码')).toHaveCount(0)
  await expect(page.getByTestId('clarify-stream-caret')).toHaveCount(0)
  await expect(page.getByTestId('clarify-empty-fail')).toHaveCount(0)
  expect(await page.evaluate(() => (window as unknown as { __sameDocument?: boolean }).__sameDocument)).toBe(true)
}

test('inbox node conversation: socket drop and server restart recover without reload', async ({ page }) => {
  await dropRestartRecover(page, () => page.goto(shell(`/runs/${RUN}/nodes/${NODE}`)))
})

test('run detail: socket drop and server restart recover without reload', async ({ page }) => {
  await dropRestartRecover(page, async () => {
    await page.goto(shell(`/runs/${RUN}?node=${NODE}`))
    await page.getByRole('button', { name: 'Agent交互' }).click()
  })
})

test('run live page: socket drop and server restart recover without reload', async ({ page }) => {
  await dropRestartRecover(page, () => page.goto(shell(`/runs/${RUN}/live?node=${NODE}`)))
})

/**
 * Share harness: the fixture server lives in the page, so "reload" opens the
 * page again with the server already holding the empty reply (`dialogue=empty`).
 */
for (const scene of ['inbox-clarify', 'public-clarify', 'public-review'] as const) {
  test(`share harness ${scene}: empty reply shows the failure card before and after reload`, async ({ page }) => {
    await page.goto(`/gate-share-link.html?scene=${scene}`)
    await expect(page.getByTestId('gate-share-e2e-root')).toBeVisible()
    const push = (d: FakeDialogue) =>
      page.evaluate((next) => (window as unknown as { __pushDialogue: (x: unknown) => void }).__pushDialogue(next), d)

    await expect.poll(async () => {
      await push(running)
      return page.getByText('正在梳理登录需求').count()
    }).toBeGreaterThan(0)
    await expect(page.getByTestId('clarify-empty-fail')).toHaveCount(0)

    await push(empty)
    await expectEmptyFail(page)
    await expect(page.getByText('正在梳理登录需求')).toHaveCount(0)

    await page.goto(`/gate-share-link.html?scene=${scene}&dialogue=empty`)
    await expectEmptyFail(page)
    await expect(page.getByText('做登录页').first()).toBeVisible()
  })
}
