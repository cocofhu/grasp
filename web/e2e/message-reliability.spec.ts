import { test, expect, type Page } from '@playwright/test'

type HarnessSocket = {
  url: string
  emit: (frame: unknown) => void
  close: () => void
}
type HarnessWindow = Window & {
  __reliabilitySockets: HarnessSocket[]
  __studioEvents: Record<string, unknown>[]
  __pmExternal: boolean
}

async function openHarness(page: Page) {
  await page.goto('/message-reliability.html')
  await expect(page.getByTestId('message-reliability-root')).toBeVisible({ timeout: 15_000 })
}

async function socketCount(page: Page, urlPart: string) {
  return page.evaluate((part) => {
    const w = window as HarnessWindow
    return w.__reliabilitySockets.filter((socket) => socket.url.includes(part)).length
  }, urlPart)
}

async function emitSocket(page: Page, urlPart: string, index: number, frame: unknown) {
  await page.evaluate(({ part, at, value }) => {
    const w = window as HarnessWindow
    const socket = w.__reliabilitySockets.filter((item) => item.url.includes(part))[at]
    if (!socket) throw new Error(`missing socket ${part}[${at}]`)
    socket.emit(value)
  }, { part: urlPart, at: index, value: frame })
}

test('同文连续澄清在 Inbox、Public 与 Embed 共享组件中各自保留两轮', async ({ page }) => {
  await openHarness(page)
  await page.getByTestId('sim-same-text').click()
  for (const host of ['inbox', 'public', 'embed']) {
    const section = page.getByTestId(`clarify-host-${host}`)
    await expect(section.getByTestId('clarify-human-message')).toHaveCount(2)
    await expect(section.getByTestId('clarify-human-message').nth(1)).toHaveText('继续')
    await expect(section.getByTestId('clarify-agent-message').filter({ hasText: '新同文回答' })).toHaveCount(1)
  }
})

test('并发回合结束后拒绝迟到队列，旧会话帧不会打断新会话的相同消息', async ({ page }) => {
  await openHarness(page)
  const chat = page.getByTestId('clarify-host-inbox')
  const sameTextHumans = chat.getByTestId('clarify-human-message').filter({ hasText: '继续检查' })
  await page.getByTestId('sim-concurrent-complete').click()
  await expect(sameTextHumans).toHaveCount(3)
  for (let round = 1; round <= 3; round++) {
    await expect(chat.getByText(`并发回合 ${round} 已完成。`, { exact: true })).toHaveCount(1)
  }
  await expect(chat.getByTestId('clarify-review-queue')).toHaveCount(0)
  await expect(chat.getByTestId('clarify-review-cancel')).toHaveCount(0)
  await expect(chat.getByTestId('clarify-busy-status')).toHaveCount(0)

  await page.getByTestId('sim-delayed-queue').click()
  await expect(sameTextHumans).toHaveCount(3)
  await expect(chat.getByTestId('clarify-review-queue')).toHaveCount(0)
  await expect(chat.getByTestId('clarify-review-cancel')).toHaveCount(0)
  await expect(chat.getByTestId('clarify-busy-status')).toHaveCount(0)

  await page.getByTestId('sim-new-generation').click()
  await expect(sameTextHumans).toHaveCount(4)
  await expect(chat.getByText('新会话正在继续检查。', { exact: true })).toHaveCount(1)
  await expect(chat.getByTestId('clarify-review-cancel')).toHaveCount(1)
  await page.getByTestId('sim-retired-frames').click()
  await expect(chat.getByText('新会话继续输出，旧快照没有打断。', { exact: true })).toHaveCount(1)
  await expect(sameTextHumans).toHaveCount(4)
  await expect(chat.getByTestId('clarify-review-queue')).toHaveCount(0)
  await expect(chat.getByTestId('clarify-review-cancel')).toHaveCount(1)
  await expect(chat.getByTestId('clarify-busy-status')).toHaveCount(1)
  expect(await page.evaluate(() => performance.getEntriesByType('navigation').length)).toBe(1)
})

test('持久化首段回复后，当前思考仍在排队用户前继续输出', async ({ page }) => {
  await page.setViewportSize({ width: 1100, height: 1000 })
  await page.goto('/message-reliability.html?sealed-segment')
  const chat = page.getByTestId('clarify-host-inbox')
  await page.getByTestId('sim-segmented-reply').click()
  await expect(chat.getByTestId('agent-timeline-thought')).toContainText('继续检查统计信息的展示效果。')
  await chat.getByTestId('clarify-input').fill('暗色主题感觉不太行')
  await chat.getByTestId('clarify-send-icon').click()
  await page.getByTestId('sim-persist-segment').click()

  const currentHuman = chat.getByTestId('clarify-human-message').filter({ hasText: '回复信息改成统计信息' })
  const nextHuman = chat.getByTestId('clarify-human-message').filter({ hasText: '暗色主题感觉不太行' })
  await expect(currentHuman).toHaveCount(1)
  await expect(nextHuman).toHaveCount(1)
  await expect(chat.getByText('已把回复信息改成统计信息。', { exact: true })).toHaveCount(1)
  await expect(chat.getByTestId('agent-timeline-thought')).toContainText('继续检查统计信息的展示效果。')
  await expect(chat.getByTestId('clarify-busy-status')).toHaveCount(1)
  await expect(chat.getByTestId('clarify-review-cancel')).toBeVisible()
  await expect(chat.getByTestId('clarify-review-queue')).toContainText('暗色主题感觉不太行')
  await expect(chat.getByTestId('clarify-empty-fail')).toHaveCount(0)
  const text = await chat.getByTestId('clarify-scroller').innerText()
  expect(text.indexOf('继续检查统计信息的展示效果。')).toBeGreaterThan(text.indexOf('回复信息改成统计信息'))
  expect(text.indexOf('暗色主题感觉不太行')).toBeGreaterThan(text.indexOf('继续检查统计信息的展示效果。'))
  await page.screenshot({ path: test.info().outputPath('sealed-segment-still-running.png'), fullPage: false })

  await page.getByTestId('sim-continue-segment').click()
  await expect(chat.getByText('统计信息检查还在继续，无需刷新。', { exact: true })).toBeVisible()
  await expect(chat.getByTestId('clarify-busy-status')).toHaveCount(1)
  await expect(chat.getByTestId('clarify-review-queue')).toContainText('暗色主题感觉不太行')
  expect(await page.evaluate(() => performance.getEntriesByType('navigation').length)).toBe(1)
})

test('Studio 断线期间完成后由事件历史与 idle 重连恢复且不重复用户消息', async ({ page }) => {
  await openHarness(page)
  await expect.poll(() => socketCount(page, '/sandboxes/7/chat')).toBe(1)
  await emitSocket(page, '/sandboxes/7/chat', 0, {
    type: 'session', event: 'queue_state', busy: true, activeItem: { id: 'srv-1', text: '继续验证' }, items: [],
  })
  await emitSocket(page, '/sandboxes/7/chat', 0, {
    type: 'session', event: 'turn_begin', item: { id: 'srv-1', text: '继续验证' },
  })
  await emitSocket(page, '/sandboxes/7/chat', 0, {
    type: 'acp',
    data: { type: 'session_update', update: { sessionUpdate: 'agent_message_chunk', content: { text: 'partial' } } },
  })
  const studio = page.getByTestId('studio-race')
  await expect(studio.getByText('partial', { exact: true })).toHaveCount(1)
  await expect(studio.locator('[data-chat-anchor="question:srv-1"]')).toHaveCount(1)

  await page.evaluate(() => {
    const w = window as HarnessWindow
    w.__studioEvents = [
      { type: 'prompt_begin', opId: 'srv-1', promptText: '继续验证' },
      { type: 'session_update', update: { sessionUpdate: 'agent_message_chunk', content: { text: 'partial more final' } } },
      { type: 'prompt_done' },
    ]
    w.__reliabilitySockets.find((socket) => socket.url.includes('/sandboxes/7/chat'))?.close()
  })
  await expect.poll(() => socketCount(page, '/sandboxes/7/chat'), { timeout: 5_000 }).toBe(2)
  await emitSocket(page, '/sandboxes/7/chat', 1, { type: 'session', event: 'queue_state', busy: false, items: [] })
  await expect(studio.getByText('partial more final', { exact: true })).toHaveCount(1)
  await expect(studio.locator('[data-chat-anchor="question:srv-1"]')).toHaveCount(1)
  await expect(studio.getByText('正在生成…', { exact: true })).toHaveCount(0)
})

test('PM 冷启动收到 idle 后有界重读持久历史并恢复外部最终消息', async ({ page }) => {
  await openHarness(page)
  const pm = page.getByTestId('pm-race')
  await expect(pm.getByTestId('pm-reconciled-message')).toHaveCount(2)
  await expect.poll(() => socketCount(page, '/workspace/pm/threads/thr-race/chat')).toBe(1)
  await page.evaluate(() => { (window as HarnessWindow).__pmExternal = true })
  await emitSocket(page, '/workspace/pm/threads/thr-race/chat', 0, {
    type: 'session', event: 'queue_state', busy: false, items: [],
  })
  await expect(pm.getByTestId('pm-reconciled-message')).toHaveCount(4)
  await expect(pm.locator('[data-message-id="a-new"]')).toHaveText('断线期间的最终回答')
  await expect(pm.locator('[data-message-id="u-new"]')).toHaveCount(1)
})
