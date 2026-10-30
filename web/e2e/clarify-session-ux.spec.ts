import { test, expect } from '@playwright/test'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const shotDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../../.tmp-clarify-ux-shots')

/** This tab's stash for the harness run (key carries the per-tab id). */
type Page = import('@playwright/test').Page
const STASH = 'grasp-clarify-stash:anon:run-clarify-e2e:clarify'

/** This tab's stash for the harness run (sessionStorage: only the sender tab has it). */
async function readStash(page: Page): Promise<string | null> {
  return page.evaluate((k) => sessionStorage.getItem(k), STASH)
}

/** Any clarify key in the storage every tab shares (must stay empty). */
async function sharedClarifyKeys(page: Page): Promise<string[]> {
  return page.evaluate(() => Object.keys(localStorage).filter((k) => k.startsWith('grasp-clarify-')))
}

async function openHarness(page: Page) {
  await page.setViewportSize({ width: 1100, height: 900 })
  await page.goto('/clarify-session-ux.html')
  await expect(page.getByTestId('clarify-ux-root')).toBeVisible({ timeout: 15_000 })
}

/** A tab that never sent anything: nothing refilled, nothing stashed, no notice. */
async function expectNothingRefilled(page: Page) {
  await expect(page.locator('[data-testid="clarify-input"]')).toHaveValue('')
  await expect(page.getByTestId('clarify-send-notice')).toHaveCount(0)
  expect(await readStash(page)).toBeNull()
  expect(await sharedClarifyKeys(page)).toEqual([])
}

async function sendClarify(page: import('@playwright/test').Page, text: string) {
  const box = page.locator('[data-testid="clarify-input"]')
  await box.fill(text)
  await page.getByTestId('clarify-send-label').click()
}

test.describe('ClarifyChat 非 reviewMode 澄清会话', () => {
  test('提交选择遇到迟到的排队快照后，后续问题无需刷新即可继续选择', async ({ page }) => {
    await openHarness(page)
    await page.getByTestId('sim-choice-rounds').click()
    for (let round = 1; round <= 2; round++) {
      await expect(page.getByTestId('clarify-question-prompt')).toHaveText(`第 ${round} 轮图标显示范围`)
      const submit = page.getByRole('button', { name: '确认选择', exact: true })
      await expect(submit).toBeEnabled()
      await submit.click()
      await expect(page.getByTestId('clarify-question-prompt')).toHaveText(`第 ${round + 1} 轮图标显示范围`)
      await expect(submit).toBeEnabled()
      await expect(page.getByTestId('clarify-review-cancel')).toHaveCount(0)
      await expect(page.getByTestId('clarify-review-queue')).toHaveCount(0)
    }
  })

  test('排队 → 流式 → Cancel 保留队列 → 刷新续传', async ({ page }) => {
    await page.setViewportSize({ width: 1100, height: 900 })
    await page.goto('/clarify-session-ux.html')
    await expect(page.getByTestId('clarify-ux-root')).toBeVisible({ timeout: 15_000 })

    await sendClarify(page, '澄清意见甲（队列首条）')
    await sendClarify(page, '澄清意见乙（仍在队列）')
    const queue = page.getByTestId('clarify-review-queue')
    await expect(queue).toBeVisible()
    await expect(queue).toContainText('待发送队列')
    await expect(queue).toContainText('澄清意见甲')
    await expect(queue).toContainText('澄清意见乙')
    await expect(page.getByTestId('clarify-review-cancel')).toBeVisible()
    await expect(page.getByTestId('clarify-send-label')).toHaveCount(0)
    await expect(page.getByTestId('clarify-review-queue')).toBeVisible()
    await page.screenshot({ path: path.join(shotDir, '01-clarify-queue.png'), fullPage: true })

    await page.getByTestId('sim-turn-stream').click()
    await expect(page.getByText('流式产出正文（非整轮一次性）。')).toBeVisible()
    // After real pump order, the live stream binds to 甲. 乙 waits only in the
    // queue panel: no user row and no empty failure until its own turn_begin.
    const scrollerAfterBegin = page.getByTestId('clarify-scroller')
    await expect(scrollerAfterBegin).toContainText('澄清意见甲（队列首条）')
    await expect(scrollerAfterBegin).not.toContainText('澄清意见乙（仍在队列）')
    const beginText = await scrollerAfterBegin.innerText()
    expect(beginText.match(/澄清意见甲（队列首条）/g)?.length ?? 0).toBe(1)
    await expect(scrollerAfterBegin.getByTestId('clarify-empty-fail')).toHaveCount(0)
    const jiaAt = beginText.indexOf('澄清意见甲（队列首条）')
    const streamAt = beginText.indexOf('流式产出正文')
    expect(streamAt).toBeGreaterThan(jiaAt)
    await expect(queue).toContainText('澄清意见乙')
    await expect(queue).not.toContainText('澄清意见甲')
    // Host busy gate: react mid-turn must skip full refresh.
    await expect(page.getByTestId('host-skip-refresh')).toContainText('hostSkip=1')
    await page.screenshot({ path: path.join(shotDir, '02-clarify-stream.png'), fullPage: true })

    // Demo Cancel: keep queue (乙 remains), mark interrupted.
    await page.getByTestId('clarify-review-cancel').click()
    await expect(page.getByTestId('clarify-interrupted')).toBeVisible()
    await expect(page.getByTestId('clarify-review-queue')).toContainText('澄清意见乙')
    await page.screenshot({ path: path.join(shotDir, '03-clarify-cancel-keep.png'), fullPage: true })

    // Refresh resume: busy + activeItem + continued stream.
    await page.getByTestId('sim-refresh-resume').click()
    await expect(page.getByText('刷新后续上的流式正文。')).toBeVisible()
    await expect(page.getByTestId('clarify-review-queue')).toContainText('澄清意见乙')
    await page.screenshot({ path: path.join(shotDir, '04-clarify-refresh-resume.png'), fullPage: true })
  })

  test('真实泵帧序：queue_state→turn_begin 双消息不踩踏', async ({ page }) => {
    await page.setViewportSize({ width: 1100, height: 900 })
    await page.goto('/clarify-session-ux.html')
    await expect(page.getByTestId('clarify-ux-root')).toBeVisible({ timeout: 15_000 })

    await sendClarify(page, '澄清意见甲（队列首条）')
    await sendClarify(page, '澄清意见乙（仍在队列）')
    await page.getByTestId('sim-pump-order-dual').click()

    const scroller = page.getByTestId('clarify-scroller')
    await expect(scroller).toContainText('澄清意见甲（队列首条）')
    await expect(scroller).not.toContainText('澄清意见乙（仍在队列）')
    const text = await scroller.innerText()
    expect(text.match(/澄清意见甲（队列首条）/g)?.length ?? 0).toBe(1)
    await expect(scroller.getByTestId('clarify-empty-fail')).toHaveCount(0)
    const jiaAt = text.indexOf('澄清意见甲（队列首条）')
    expect(text.indexOf('思考中')).toBeGreaterThan(jiaAt)
    const queue = page.getByTestId('clarify-review-queue')
    await expect(queue).toContainText('澄清意见乙（仍在队列）')
    await expect(queue).not.toContainText('澄清意见甲')
    await page.screenshot({ path: path.join(shotDir, '06-clarify-pump-order.png'), fullPage: true })
  })

  test('transcript 追上 + live streaming 不双显 human', async ({ page }) => {
    await page.setViewportSize({ width: 1100, height: 900 })
    await page.goto('/clarify-session-ux.html')
    await expect(page.getByTestId('clarify-ux-root')).toBeVisible({ timeout: 15_000 })

    await page.getByTestId('sim-transcript-catchup').click()
    await expect(page.getByText('流式产出正文（非整轮一次性）。')).toBeVisible()
    const scroller = page.getByTestId('clarify-scroller')
    const text = await scroller.innerText()
    const matches = text.match(/澄清意见甲（队列首条）/g) || []
    expect(matches.length).toBe(1)
    await expect(page.getByTestId('host-skip-refresh')).toContainText(/hostSkip=[1-9]/)
    await page.screenshot({ path: path.join(shotDir, '05-clarify-no-dup-human.png'), fullPage: true })
  })

  test('硬刷新 ACP 竞态：延迟 seed 恢复 thought/message 并续流', async ({ page }) => {
    await page.setViewportSize({ width: 1100, height: 900 })
    await page.goto('/clarify-session-ux.html')
    await expect(page.getByTestId('clarify-ux-root')).toBeVisible({ timeout: 15_000 })

    await page.getByTestId('sim-hard-refresh-acp-race').click()
    await expect(page.getByTestId('agent-timeline-thought')).toContainText('竞态恢复的思考', {
      timeout: 5_000,
    })
    await expect(page.getByText('竞态恢复的正文续流。继续追加。')).toBeVisible()
    await expect(page.getByTestId('clarify-busy-status')).toContainText('输出中')
    await expect(page.getByTestId('clarify-stream-caret')).toBeVisible()
    await page.screenshot({ path: path.join(shotDir, '07-clarify-hard-refresh-acp-race.png'), fullPage: true })
  })

  test('四阶段完成态：输出中三件套消失 + 已完成脚注', async ({ page }) => {
    await page.setViewportSize({ width: 1100, height: 900 })
    await page.goto('/clarify-session-ux.html')
    await expect(page.getByTestId('clarify-ux-root')).toBeVisible({ timeout: 15_000 })

    await page.getByTestId('sim-four-phase-complete').click()
    await expect(page.getByText('四阶段正文输出。')).toBeVisible()
    await expect(page.getByTestId('clarify-busy-status')).toHaveCount(0)
    await expect(page.getByTestId('clarify-stream-caret')).toHaveCount(0)
    await expect(page.getByTestId('clarify-turn-completed')).toHaveCount(0)
    // Every chat surface folds a settled reply through the shared timeline.
    await expect(page.getByTestId('agent-timeline-elapsed')).toHaveAttribute('aria-expanded', 'false')
    await expect(page.getByTestId('chat-message-info')).toBeVisible()
    await page.screenshot({ path: path.join(shotDir, '08-clarify-four-phase-done.png'), fullPage: true })
  })

  test('运行已结束(409 clarify_visit_ended)：回填输入框、保留后打的字、焦点不丢', async ({ page }) => {
    await page.setViewportSize({ width: 1100, height: 900 })
    await page.goto('/clarify-session-ux.html')
    await expect(page.getByTestId('clarify-ux-root')).toBeVisible({ timeout: 15_000 })

    await sendClarify(page, '运行结束后才到的回复')
    const queue = page.getByTestId('clarify-review-queue')
    await expect(queue).toContainText('运行结束后才到的回复')
    const box = page.locator('[data-testid="clarify-input"]')
    await box.click()
    await page.keyboard.type('又打了几个字')

    await page.getByTestId('sim-visit-ended').click()
    // The sim button took focus; the chat hands it back to the composer.
    await expect(box).toBeFocused()
    await expect(box).toHaveValue('运行结束后才到的回复\n又打了几个字')
    await expect(page.getByTestId('clarify-send-notice')).toHaveText('运行已结束，这条消息没有发送；续跑后请重新发送')
    await expect(page.getByTestId('clarify-queue-item')).toHaveCount(0)
    await expect(page.getByTestId('clarify-scroller')).not.toContainText('运行结束后才到的回复')

    // Keep typing: nothing is lost and the caret stays at the end.
    await page.keyboard.type('！')
    await expect(box).toHaveValue('运行结束后才到的回复\n又打了几个字！')
  })

  test('取消运行的同时发送：原文暂存，续跑后回到输入框并能发出去', async ({ page }) => {
    await page.setViewportSize({ width: 1100, height: 900 })
    await page.goto('/clarify-session-ux.html')
    await expect(page.getByTestId('clarify-ux-root')).toBeVisible({ timeout: 15_000 })

    await sendClarify(page, '取消时发出的回复')
    await page.getByTestId('sim-cancel-race').click()
    await expect(page.locator('[data-testid="clarify-input"]')).toHaveCount(0)
    await expect(page.getByTestId('clarify-send-notice')).toHaveText('运行已结束，这条消息没有发送；续跑后请重新发送')
    await expect(page.getByTestId('clarify-scroller')).not.toContainText('取消时发出的回复')
    const stash = await readStash(page)
    expect(JSON.parse(stash!).text).toBe('取消时发出的回复')

    // Survives a refresh; the harness reloads with the run resumed.
    await page.reload()
    await expect(page.getByTestId('clarify-ux-root')).toBeVisible({ timeout: 15_000 })
    const box = page.locator('[data-testid="clarify-input"]')
    await expect(box).toHaveValue('取消时发出的回复')
    // Kept until it is really sent (another refresh must not lose it).
    expect(await readStash(page)).not.toBeNull()

    await page.getByTestId('clarify-send-label').click()
    await expect(page.getByTestId('clarify-review-queue')).toContainText('取消时发出的回复')
    await expect(box).toHaveValue('')
    expect(await readStash(page)).toBeNull()
  })

  test('运行结束时正在打的草稿：先暂存，续跑后回到输入框', async ({ page }) => {
    await page.setViewportSize({ width: 1100, height: 900 })
    await page.goto('/clarify-session-ux.html')
    await expect(page.getByTestId('clarify-ux-root')).toBeVisible({ timeout: 15_000 })
    const box = page.locator('[data-testid="clarify-input"]')
    await box.fill('还没发的草稿')
    await page.getByTestId('sim-cancel-race').click()
    await expect(page.getByTestId('clarify-send-notice')).toBeVisible()
    await page.getByTestId('sim-resume').click()
    await expect(box).toHaveValue('还没发的草稿')
  })

  test('同一用户三个标签页同时开着：只有发送的 A 回填，B、C 刷新后也没有', async ({ page, context }) => {
    await openHarness(page)
    await sendClarify(page, '只在发送的标签页回来')
    // B and C open at the same time, before the refusal arrives.
    const [tabB, tabC] = await Promise.all([context.newPage(), context.newPage()])
    await Promise.all([openHarness(tabB), openHarness(tabC)])
    // A's reply is refused because the run is being cancelled.
    await page.getByTestId('sim-cancel-race').click()
    await expect(page.getByTestId('clarify-send-notice')).toHaveText('运行已结束，这条消息没有发送；续跑后请重新发送')
    // Every tab sees the run end, then all resume together.
    await Promise.all([tabB, tabC].map((t) => t.getByTestId('sim-cancel-race').click()))
    await Promise.all([page, tabB, tabC].map((t) => t.getByTestId('sim-resume').click()))
    await expect(page.locator('[data-testid="clarify-input"]')).toHaveValue('只在发送的标签页回来')
    await expectNothingRefilled(tabB)
    await expectNothingRefilled(tabC)
    await Promise.all([tabB.reload(), tabC.reload()])
    for (const t of [tabB, tabC]) {
      await expect(t.getByTestId('clarify-ux-root')).toBeVisible({ timeout: 15_000 })
      await expectNothingRefilled(t)
    }
    // A sends it; a refresh of A afterwards does not bring it back either.
    await page.getByTestId('clarify-send-label').click()
    await expect(page.getByTestId('clarify-review-queue')).toContainText('只在发送的标签页回来')
    expect(await readStash(page)).toBeNull()
    await page.reload()
    await expect(page.getByTestId('clarify-ux-root')).toBeVisible({ timeout: 15_000 })
    await expect(page.locator('[data-testid="clarify-input"]')).toHaveValue('')
  })

  test('A 被拒后刷新、再续跑：A 回填，同时开着的 B 不回填', async ({ page, context }) => {
    await openHarness(page)
    const tabB = await context.newPage()
    await openHarness(tabB)
    await sendClarify(page, '刷新后还在发送的标签页')
    await page.getByTestId('sim-cancel-race').click()
    await tabB.getByTestId('sim-cancel-race').click()
    // A refreshes (the harness reloads with the run resumed); B resumes too.
    await Promise.all([page.reload(), tabB.getByTestId('sim-resume').click()])
    await expect(page.getByTestId('clarify-ux-root')).toBeVisible({ timeout: 15_000 })
    await expect(page.locator('[data-testid="clarify-input"]')).toHaveValue('刷新后还在发送的标签页')
    await expectNothingRefilled(tabB)
  })

  test('清空输入框后暂存立即删除，刷新也不会回来', async ({ page }) => {
    await openHarness(page)
    await sendClarify(page, '我不要了')
    await page.getByTestId('sim-cancel-race').click()
    await page.getByTestId('sim-resume').click()
    const box = page.locator('[data-testid="clarify-input"]')
    await expect(box).toHaveValue('我不要了')
    await box.fill('')
    await expect.poll(() => readStash(page)).toBeNull()
    await page.reload()
    await expect(page.getByTestId('clarify-ux-root')).toBeVisible({ timeout: 15_000 })
    await expect(box).toHaveValue('')
  })

  test('发送的标签页关掉后：原文不会出现在别的标签页（接受丢失）', async ({ page, context }) => {
    await openHarness(page)
    await sendClarify(page, '标签页关了就没了')
    await page.getByTestId('sim-cancel-race').click()
    await expect(page.getByTestId('clarify-send-notice')).toHaveText('运行已结束，这条消息没有发送；续跑后请重新发送')
    expect(JSON.parse((await readStash(page))!).text).toBe('标签页关了就没了')
    await page.close()
    const tabB = await context.newPage()
    await openHarness(tabB)
    await expectNothingRefilled(tabB)
  })

  test('复制标签页（sessionStorage 被拷贝）：副本不回填，原标签页照常回填', async ({ page, context }) => {
    await openHarness(page)
    await sendClarify(page, '复制出来的标签页别回填')
    await page.getByTestId('sim-cancel-race').click()
    await expect(page.getByTestId('clarify-send-notice')).toHaveText('运行已结束，这条消息没有发送；续跑后请重新发送')
    // Chrome's "Duplicate" copies sessionStorage into the new tab.
    const copied = await page.evaluate(() => JSON.stringify(Object.fromEntries(Object.entries(sessionStorage))))
    const dup = await context.newPage()
    await dup.addInitScript((data) => {
      if (sessionStorage.getItem('__dup_seeded')) return
      for (const [k, v] of Object.entries(JSON.parse(data) as Record<string, string>)) sessionStorage.setItem(k, v)
      sessionStorage.setItem('__dup_seeded', '1')
    }, copied)
    await openHarness(dup)
    await expectNothingRefilled(dup)
    expect(await dup.evaluate(() => sessionStorage.getItem('grasp-clarify-tab-id'))).not.toBe(
      await page.evaluate(() => sessionStorage.getItem('grasp-clarify-tab-id')),
    )
    await page.getByTestId('sim-resume').click()
    await expect(page.locator('[data-testid="clarify-input"]')).toHaveValue('复制出来的标签页别回填')
  })
})
