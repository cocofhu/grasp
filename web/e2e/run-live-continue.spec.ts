/**
 * #850 run live page: when the watched auto node ends and the next node is
 * also auto (实现 → 验证), the conversation continues in place. The re-keyed
 * surface must get the next node's stream: streamed text stays, one busy
 * indicator that clears at the end, no 「本轮没有输出」, no navigation.
 * Workflow connections also recover missed output and completion metadata
 * after explicit disconnects or silent stalls, without reloading the page.
 * Real RunLiveView; API and run WS mocked.
 */
import { expect, test, type Page, type WebSocketRoute } from '@playwright/test'

const RUN = 'run-live-e2e'
type Phase = 'impl' | 'verify' | 'done' | 'gate'

const nodes = [
  { id: 'input', type: 'input', label: '输入', position: { x: 0, y: 0 }, config: {} },
  { id: 'impl', type: 'agent', label: '实现', position: { x: 200, y: 0 }, config: {} },
  { id: 'verify', type: 'agent', label: '验证', position: { x: 400, y: 0 }, config: {} },
  { id: 'output', type: 'output', label: '输出', position: { x: 600, y: 0 }, config: {} },
]
const at = (s: number) => new Date(Date.UTC(2026, 9, 21, 3, 0, s)).toISOString()
const cur = (id: 'impl' | 'verify') => ({
  id,
  name: id === 'impl' ? '实现' : '验证',
  iteration: 1,
  kind: 'agent',
  mode: 'auto',
  state: 'running',
  task: id === 'impl' ? '实现登录页改版' : '验证改动并回报',
})

function runFor(phase: Phase) {
  const done = (id: string, s: number, e: number) => ({ nodeId: id, status: 'completed', startedAt: at(s), finishedAt: at(e), durationSec: e - s, varsSnapshot: {}, outputs: {} })
  const running = (id: string, s: number) => ({ nodeId: id, status: 'running', startedAt: at(s), varsSnapshot: {}, outputs: {} })
  return {
    id: RUN,
    workflowId: 'wf-live-e2e',
    workflowName: '实现+验证',
    title: '登录页改版',
    status: phase === 'done' ? 'completed' : phase === 'gate' ? 'waiting_human' : 'running',
    trigger: 'manual',
    startedAt: at(0),
    nodes,
    edges: [
      { id: 'e0', source: 'input', target: 'impl' },
      { id: 'e1', source: 'impl', target: 'verify' },
      { id: 'e2', source: 'verify', target: 'output' },
    ],
    nodeRuns: {
      input: done('input', 0, 1),
      impl: phase === 'impl' ? running('impl', 1) : done('impl', 1, 13),
      ...(phase === 'impl' ? {} : { verify: phase === 'verify' ? running('verify', 14) : done('verify', 14, 30) }),
    },
    nodeExecutions: {},
    artifacts: [],
    trace: [],
    vars: [],
    ...(phase === 'done' ? {} : { currentNode: phase === 'gate' ? GATE : cur(phase) }),
  }
}
const GATE = { id: 'ship', name: '上线审批', iteration: 1, kind: 'gate', mode: 'manual', state: 'waiting_human' }

function chainFor(phase: Phase) {
  const seg = (id: 'impl' | 'verify', status: string, s: number, e?: number) => ({
    nodeId: id, iteration: 1, name: id === 'impl' ? '实现' : '验证', kind: 'agent', mode: 'auto', status,
    startedAt: at(s), ...(e ? { endedAt: at(e) } : {}), task: cur(id).task, eventCount: 3,
  })
  const segments = [seg('impl', phase === 'impl' ? 'running' : 'completed', 1, phase === 'impl' ? undefined : 13)]
  if (phase === 'gate') return { runId: RUN, status: 'waiting_human', currentNode: GATE, segments }
  if (phase !== 'impl') segments.push(seg('verify', phase === 'verify' ? 'running' : 'completed', 14, phase === 'verify' ? undefined : 30))
  return { runId: RUN, status: phase === 'done' ? 'completed' : 'running', ...(phase === 'done' ? {} : { currentNode: cur(phase) }), segments }
}

async function setup(page: Page) {
  const state = {
    phase: 'impl' as Phase,
    ws: null as WebSocketRoute | null,
    sockets: [] as WebSocketRoute[],
    events: {} as Record<string, unknown[]>,
    runPatch: {} as Record<string, unknown>,
  }
  const snapshot = () => ({ ...runFor(state.phase), ...state.runPatch })
  await page.routeWebSocket(new RegExp(`/api/runs/${RUN}/events`), (ws) => {
    state.ws = ws
    state.sockets.push(ws)
    // Reconnecting to the real endpoint starts with a run snapshot; ACP that
    // arrived while disconnected must be recovered from the node endpoint.
    if (state.sockets.length > 1) ws.send(JSON.stringify({ type: 'snapshot', run: snapshot() }))
  })
  await page.route((u) => u.pathname.startsWith('/api/'), async (route) => {
    const url = new URL(route.request().url())
    const json = (body: unknown) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) })
    if (url.pathname === `/api/runs/${RUN}`) return json(snapshot())
    if (url.pathname === `/api/runs/${RUN}/conversation-chain`) return json(chainFor(state.phase))
    const eventsNode = url.pathname.match(/\/nodes\/([^/]+)\/events$/)?.[1]
    if (eventsNode) return json({ events: state.events[eventsNode] ?? [], live: state.phase !== 'done' })
    return json({})
  })
  const navs: string[] = []
  await page.goto(`/run-live-continue.html?run=${RUN}&node=impl`)
  await page.waitForFunction(() => !!(window as unknown as { __router?: unknown }).__router)
  await page.evaluate(() => {
    const w = window as unknown as { __router: { afterEach: (f: (to: { fullPath: string }) => void) => void }; __navs: string[] }
    w.__navs = []
    w.__router.afterEach((to) => w.__navs.push(to.fullPath))
  })
  await expect.poll(() => state.ws !== null).toBe(true)
  // Tag the mounted view: an in-place continue must not remount it.
  await page.locator('#app > div > *').first().evaluate((el) => { el.setAttribute('data-e2e-mount', '1') })
  const send = (frame: unknown) => state.ws!.send(JSON.stringify(frame))
  return { state, send, navs }
}

const busy = (page: Page) => page.locator('text=/^(输出中|思考中…|生成中…)$/ >> visible=true')
type Step = { kind: 'thought' | 'message'; text: string } | { kind: 'tool'; title: string; status: string }
/** Same shape as the server's run WS acp frame: cumulative flat events plus one timeline snapshot. */
const acp = (nodeId: string, steps: Step[], b = true) => ({
  type: 'acp',
  runId: RUN,
  nodeId,
  busy: b,
  events: flat(steps),
})
/** Flat rails as the server sends them: latest thought, latest message, every tool call, then the ordered timeline. */
function flat(steps: Step[]) {
  const latest = (k: 'thought' | 'message') => [...steps].reverse().find((p) => p.kind === k)
  const out: unknown[] = []
  for (const k of ['thought', 'message'] as const) { const p = latest(k); if (p) out.push({ t: 0, ...p }) }
  for (const p of steps) if (p.kind === 'tool') out.push({ t: 0, kind: 'tool_call', title: p.title, status: p.status })
  out.push({ t: 0, kind: 'timeline', parts: steps })
  return out
}
const T1: Step = { kind: 'thought', text: '先读一下改动和测试说明。' }
const M1: Step = { kind: 'message', text: '开始验证：先跑单元测试。' }
const tool = (title: string, status = 'completed'): Step => ({ kind: 'tool', title, status })
const M2: Step = { kind: 'message', text: '验证完成：登录页改版无回归。' }

for (const theme of ['light', 'dark'] as const) {
  test(`live page: 实现 → 验证 (auto → auto) continues in place and keeps the stream (${theme})`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: theme })
    await page.addInitScript((t) => localStorage.setItem('grasp-theme', t), theme)
    const { state, send } = await setup(page)
    await expect(page.getByTestId('clarify-observe-stop')).toBeVisible()
    // Same split toolbar as the clarify chat: 资源 and 产物 open separately.
    await expect(page.getByTestId('conversation-resource-toggle')).toBeVisible()
    await expect(page.getByTestId('conversation-panel-toggle')).toBeVisible()
    send(acp('impl', [T1, { kind: 'message', text: '实现完成，登录页已改版。' }]))
    await expect(page.getByText('实现完成，登录页已改版。')).toBeVisible()

    // 实现 ends, 验证 starts. As on the server: node_enter, then 验证's acp frames
    // right away (before the page has switched and re-keyed its surface); the
    // GET that follows reports 验证 as the current node.
    state.phase = 'verify'
    send(acp('impl', [T1, { kind: 'message', text: '实现完成，登录页已改版。' }], false))
    send({ type: 'node_enter', runId: RUN, nodeId: 'verify', iteration: 1, kind: 'agent', mode: 'auto', name: '验证' })
    send(acp('verify', [T1]))
    send(acp('verify', [T1, M1]))
    await expect(page.getByTestId('clarify-segment-divider')).toContainText('验证')
    await expect(page.getByText('开始验证：先跑单元测试。')).toHaveCount(1)
    await expect(busy(page)).toHaveCount(1)
    send(acp('verify', [T1, M1, tool('运行测试 npm test', 'in_progress')]))
    send(acp('verify', [T1, M1, tool('运行测试 npm test')]))
    send(acp('verify', [T1, M1, tool('运行测试 npm test'), M2]))
    await expect(page.getByText('验证完成：登录页改版无回归。')).toHaveCount(1)
    // Let the background poll run (it still reads 「follow 验证」 from the opened URL):
    // the stream must not be re-seeded into a new 「输出中」 row.
    await page.waitForTimeout(4600)
    await expect(page.getByText('Agent', { exact: true })).toHaveCount(1)
    // Streamed text is kept (not replaced by a fresh empty row per frame).
    await expect(page.getByText('开始验证：先跑单元测试。')).toHaveCount(1)
    await expect(busy(page)).toHaveCount(1)

    // Run ends: the page ends in place, the answer stays, nothing busy, no false empty-turn error.
    state.phase = 'done'
    send(acp('verify', [T1, M1, tool('运行测试 npm test'), M2], false))
    send({ type: 'node_enter', runId: RUN, nodeId: 'output', iteration: 1, kind: 'agent', mode: 'auto', name: '输出' })
    send({ type: 'status', runId: RUN, status: 'completed' })
    await expect(page.getByTestId('clarify-observe-stop')).toHaveCount(0)
    await expect(busy(page)).toHaveCount(0)
    await expect(page.getByText('本轮没有输出')).toHaveCount(0)
    await expect(page.getByText('验证完成：登录页改版无回归。')).toBeVisible()
    await expect(page.getByTestId('clarify-segment-divider')).toHaveCount(1)
    // The footer names how the run ended (not the generic 「交互已完成，进入下一步」).
    await expect(page.getByTestId('clarify-done-note')).toHaveText('运行已完成')
    await expect(page.getByText('交互已完成，进入下一步')).toHaveCount(0)
    // ?node= follows the node on screen (router.replace, query only) so a refresh lands on 验证;
    // nothing else routes, and the view was not remounted.
    expect(await page.evaluate(() => (window as unknown as { __navs: string[] }).__navs)).toEqual([`/runs/${RUN}/live?node=verify`])
    await expect(page.locator('[data-e2e-mount="1"]')).toHaveCount(1)
  })
}


test('live page: the watched node ends at a gate → ends in place and names the gate', async ({ page }) => {
  const { state, send } = await setup(page)
  await expect(page.getByTestId('clarify-observe-stop')).toBeVisible()
  send(acp('impl', [T1, { kind: 'message', text: '实现完成，登录页已改版。' }]))
  await expect(page.getByText('实现完成，登录页已改版。')).toBeVisible()
  state.phase = 'gate'
  send(acp('impl', [T1, { kind: 'message', text: '实现完成，登录页已改版。' }], false))
  send({ type: 'node_enter', runId: RUN, nodeId: 'ship', iteration: 1, kind: 'gate', mode: 'manual', name: '上线审批' })
  send({ type: 'status', runId: RUN, status: 'waiting_human' })
  await expect(page.getByTestId('clarify-done-note')).toHaveText('「上线审批」人工审批 · 待审批')
  await expect(page.getByTestId('clarify-observe-stop')).toHaveCount(0)
  await expect(busy(page)).toHaveCount(0)
  await expect(page.getByText('实现完成，登录页已改版。')).toBeVisible()
  await expect(page.getByText('本轮没有输出')).toHaveCount(0)
  expect(await page.evaluate(() => (window as unknown as { __navs: string[] }).__navs)).toEqual([])
})

test('live page: interrupted workflow stream recovers missed output and final tokens without reloading', async ({ page }) => {
  const { state, send } = await setup(page)
  const partial: Step = { kind: 'message', text: '正在实现登录页。' }
  const recovered: Step = { kind: 'message', text: '连接中断期间已完成布局，正在运行测试。' }
  const final: Step = { kind: 'message', text: '登录页实现完成，测试通过。' }
  send(acp('impl', [T1, partial]))
  await expect(page.getByText(partial.text)).toBeVisible()
  await expect(busy(page)).toHaveCount(1)

  // No ACP frame is sent on the replacement socket. Its snapshot must cause
  // the already mounted conversation to fetch output missed during the gap.
  state.events.impl = flat([T1, recovered])
  state.ws!.close({ code: 1011, reason: 'connection interrupted' })
  await expect.poll(() => state.sockets.length).toBe(2)
  await expect(page.getByText(recovered.text)).toBeVisible()
  await expect(busy(page)).toHaveCount(1)
  await expect(page.getByTestId('clarify-done-note')).toHaveCount(0)

  send(acp('impl', [T1, final]))
  await expect(page.getByText(final.text)).toBeVisible()
  const usage = { inputTokens: 1000, outputTokens: 200, cacheReadTokens: 300, cacheWriteTokens: 0 }
  state.events.impl = [
    { kind: 'prompt', t: 0, at: at(1), text: cur('impl').task },
    ...flat([T1, final]),
    { kind: 'turn_end', t: 12, at: at(13), usage },
  ]
  const completed = { ...runFor('done').nodeRuns.impl, iteration: 1, outputMd: final.text, events: state.events.impl, usage }
  state.runPatch = { nodeRuns: { impl: completed }, nodeExecutions: { impl: [completed] } }
  state.phase = 'done'
  send({ type: 'snapshot', run: { ...runFor('done'), ...state.runPatch } })
  await expect(page.getByTestId('clarify-done-note')).toHaveText('运行已完成')
  await expect(busy(page)).toHaveCount(0)
  await expect(page.getByText(final.text)).toBeVisible()
  await expect(page.getByText('本轮没有输出')).toHaveCount(0)
  await page.getByTestId('agent-timeline').last().getByTestId('chat-message-info').click()
  await expect(page.getByTestId('chat-message-token-total')).toHaveText('1,500')
  expect(await page.evaluate(() => (window as unknown as { __navs: string[] }).__navs)).toEqual([])
  await expect(page.locator('[data-e2e-mount="1"]')).toHaveCount(1)
  expect(await page.evaluate(() => performance.getEntriesByType('navigation').length)).toBe(1)
})

test('live page: a silent open workflow socket reconnects and resumes output automatically', async ({ page }) => {
  await page.clock.install()
  const { state, send } = await setup(page)
  const partial: Step = { kind: 'message', text: '开始执行工作流。' }
  const recovered: Step = { kind: 'message', text: '网络恢复后继续输出，工作流仍在执行。' }
  send(acp('impl', [T1, partial]))
  await expect(page.getByText(partial.text)).toBeVisible()

  // Model a half-open connection: neither close nor error reaches the page.
  // The server keeps progressing, but this transport delivers no heartbeat.
  state.events.impl = flat([T1, recovered])
  await page.clock.fastForward(61_000)
  await page.clock.runFor(1000)
  await expect.poll(() => state.sockets.length).toBe(2)
  await expect(page.getByText(recovered.text)).toBeVisible()
  await expect(busy(page)).toHaveCount(1)
  await expect(page.getByTestId('clarify-done-note')).toHaveCount(0)
  await expect(page.locator('[data-e2e-mount="1"]')).toHaveCount(1)
  expect(await page.evaluate(() => (window as unknown as { __navs: string[] }).__navs)).toEqual([])
})

test('live page: continued workflow chat reconciles a completion missed while disconnected', async ({ page }) => {
  const { state, send } = await setup(page)
  send(acp('impl', [{ kind: 'message', text: '实现完成，等待验收。' }]))
  await expect(page.getByText('实现完成，等待验收。')).toBeVisible()
  state.phase = 'verify'
  state.runPatch = {
    status: 'waiting_human',
    currentNode: { ...cur('verify'), kind: 'clarify', mode: 'manual', state: 'waiting_human' },
    clarifyByNode: { verify: { nodeId: 'verify', iteration: 1, turns: [], done: false } },
    reactSessions: { verify: { busy: false, waiting: 0, items: [] } },
  }
  send({ type: 'snapshot', run: { ...runFor(state.phase), ...state.runPatch } })
  await expect(page.getByTestId('clarify-segment-divider')).toContainText('验证')
  await expect(page.getByRole('textbox')).toBeEnabled()

  const item = { id: 'verify-reply-1', text: '请补充测试结论。' }
  send({ type: 'review', nodeId: 'verify', event: 'turn_begin', item })
  send(acp('verify', [{ kind: 'message', text: '正在执行验收测试。' }]))
  await expect(page.getByText('正在执行验收测试。')).toBeVisible()
  await expect(busy(page)).toHaveCount(1)

  // turn_done and the final ACP frame never reach this tab. The next run
  // snapshot carries the persisted answer, its usage, and authoritative idle.
  state.runPatch.clarifyByNode = {
    verify: {
      nodeId: 'verify', iteration: 1, done: false,
      turns: [
        { role: 'human', text: item.text, at: at(15) },
        {
          role: 'agent', text: '全部测试通过，无需刷新。', at: at(25), startedAt: at(15), endedAt: at(25),
          usage: { inputTokens: 1000, outputTokens: 200, cacheReadTokens: 300, cacheWriteTokens: 0 },
        },
      ],
    },
  }
  state.ws!.close({ code: 1011, reason: 'completion lost during disconnect' })
  await expect.poll(() => state.sockets.length).toBe(2)
  await expect(page.getByText('全部测试通过，无需刷新。')).toHaveCount(1)
  await expect(busy(page)).toHaveCount(0)
  await page.getByTestId('agent-timeline').last().getByTestId('chat-message-info').click()
  await expect(page.getByTestId('chat-message-token-total')).toHaveText('1,500')
  await page.keyboard.press('Escape')

  send({ type: 'review', nodeId: 'verify', event: 'turn_begin', item: { id: 'verify-reply-2', text: '再检查可访问性。' } })
  send(acp('verify', [{ kind: 'message', text: '继续检查可访问性。' }]))
  await expect(page.getByText('继续检查可访问性。')).toBeVisible()
  await expect(busy(page)).toHaveCount(1)
  await expect(page.getByTestId('clarify-done-note')).toHaveCount(0)
  await expect(page.locator('[data-e2e-mount="1"]')).toHaveCount(1)
  expect(await page.evaluate(() => (window as unknown as { __navs: string[] }).__navs)).toEqual([`/runs/${RUN}/live?node=verify`])
})
