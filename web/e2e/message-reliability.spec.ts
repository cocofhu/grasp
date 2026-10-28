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
