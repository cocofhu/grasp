import { expect, test, type FrameLocator, type Page } from '@playwright/test'
import { LIVE_ORIGINAL } from './live-variants-mock'

const origin = 'http://127.0.0.1:5174'
const overlay = (page: Page) => page.locator('grasp-live-overlay')
const drawer = (page: Page) => page.frameLocator('grasp-preview-pick [data-role="drawer"] iframe')

async function state(page: Page, key: string) {
  return (await page.request.get(`${origin}/__e2e/live/state?key=${key}`)).json()
}

async function open(page: Page, key: string) {
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.request.post(`${origin}/__e2e/live/reset?key=${key}`)
  await page.goto(`${origin}/live-variants.html?key=${key}`)
  await expect(drawer(page).getByTestId('live-drawer')).toBeVisible()
  const button = page.locator('grasp-preview-pick [data-role="live"]')
  await expect(button).toBeEnabled()
  await button.click()
  await expect(overlay(page).locator('[data-act="pick"]')).toBeVisible()
}

async function generate(page: Page, prompt = '', inserted = false) {
  await overlay(page).locator(`[data-act="${inserted ? 'insert' : 'pick'}"]`).click()
  await page.locator('#newsletter').click({ position: { x: 15, y: 15 } })
  if (prompt) await overlay(page).locator('[data-input="prompt"]').fill(prompt)
  await overlay(page).locator('[data-act="go"]').click()
  await expect(drawer(page).getByTestId('live-variant-card').last()).toHaveAttribute('data-state', 'ready')
}

async function tuneSecond(page: Page) {
  await overlay(page).locator('[data-act="next"]').click()
  await expect(page.locator('[data-grasp-variant="1"]')).toBeHidden()
  await expect(page.locator('[data-grasp-variant="2"]')).toBeVisible()
  await expect(drawer(page).getByTestId('live-variant-viewing').last()).toContainText('2')
  const gap = overlay(page).locator('input[data-param$="|2|gap"]')
  await gap.focus()
  await gap.press('ArrowRight')
  await gap.press('ArrowRight')
  await overlay(page).locator('[data-param$="|2|tone"][data-v="strong"]').click()
  await expect(page.locator('[data-grasp-variant="2"]')).toHaveCSS('gap', '32px')
  await expect(page.locator('[data-grasp-variant="2"]')).toHaveAttribute('data-gp-tone', 'strong')
}

async function expectCleanSource(page: Page, key: string, title: string) {
  await expect.poll(async () => (await state(page, key)).source).not.toMatch(/data-grasp-|data-gp-|--gp-|\bhidden\b/)
  const source = (await state(page, key)).source as string
  expect(source).toContain(title)
  await expect(page.locator('[data-grasp-live]')).toHaveCount(0)
}

async function markSelection(page: Page) {
  await overlay(page).locator('[data-act="mark-draw"]').click()
  const canvas = overlay(page).locator('.live-marks svg')
  await expect(canvas).toBeVisible()
  const button = await page.locator('#newsletter button').boundingBox()
  expect(button).not.toBeNull()
  const b = button!
  await page.mouse.move(b.x - 6, b.y - 6)
  await page.mouse.down()
  await page.mouse.move(b.x + b.width + 6, b.y - 6, { steps: 4 })
  await page.mouse.move(b.x + b.width + 6, b.y + b.height + 6, { steps: 4 })
  await page.mouse.move(b.x - 6, b.y + b.height + 6, { steps: 4 })
  await page.mouse.move(b.x - 6, b.y - 6, { steps: 4 })
  await page.mouse.up()
  await expect(canvas.locator('polyline')).toHaveCount(1)

  await overlay(page).locator('[data-act="mark-note"]').click()
  const heading = await page.locator('#newsletter h2').boundingBox()
  expect(heading).not.toBeNull()
  await page.mouse.click(heading!.x + 100, heading!.y + heading!.height / 2)
  await overlay(page).locator('textarea[data-mark-note]').fill('标题更突出；保留 Subscribe 交互')
}

test.describe('Live preview and Chat browser bridge', () => {
  test('drawings and positioned notes reach the agent request, and clearing keeps them out of the next generation', async ({ page }, testInfo) => {
    const key = 'annotations'
    await open(page, key)
    const pick = async () => {
      await overlay(page).locator('[data-act="pick"]').click()
      await page.locator('#newsletter').click({ position: { x: 15, y: 15 } })
    }
    await pick()
    await overlay(page).locator('[data-act="action"][data-v="animate"]').click()
    await expect(overlay(page).locator('[data-act="action"][data-v="animate"]')).toHaveAttribute('aria-pressed', 'true')
    await overlay(page).locator('[data-input="notes"]').fill('颜色沿用现有色板')
    await markSelection(page)
    expect((await state(page, key)).source).toBe(LIVE_ORIGINAL)
    await page.screenshot({ path: testInfo.outputPath('live-annotations.png'), animations: 'disabled' })

    await overlay(page).locator('[data-act="go"]').click()
    await expect(drawer(page).getByTestId('live-variant-card').last()).toHaveAttribute('data-state', 'ready')
    await expect(page.locator('[data-grasp-variant="1"]')).toBeVisible()
    const request = (await state(page, key)).requests[0].live
    expect(request).toMatchObject({ op: 'generate', action: 'animate', notes: ['颜色沿用现有色板'] })
    expect(request.marks).toHaveLength(2)
    expect(request.marks[0].kind).toBe('draw')
    expect(request.marks[0].points.length).toBeGreaterThan(2)
    expect(request.marks[0].targets).toEqual(expect.arrayContaining([expect.objectContaining({ selector: expect.stringContaining('button'), text: 'Subscribe' })]))
    expect(request.marks[1]).toMatchObject({ kind: 'note', text: '标题更突出；保留 Subscribe 交互' })
    expect(request.marks[1].targets).toEqual(expect.arrayContaining([expect.objectContaining({ selector: expect.stringContaining('h2'), text: 'Newsletter original' })]))
    for (const mark of request.marks) {
      for (const point of mark.points) {
        expect(point.x).toBeGreaterThanOrEqual(0)
        expect(point.x).toBeLessThanOrEqual(1)
        expect(point.y).toBeGreaterThanOrEqual(0)
        expect(point.y).toBeLessThanOrEqual(1)
      }
    }
    expect((await state(page, key)).source).not.toMatch(/polyline|data-mark-note|标题更突出|颜色沿用/)
    await expect(overlay(page).locator('polyline')).toHaveCount(0)
    await page.screenshot({ path: testInfo.outputPath('live-variants.png'), animations: 'disabled' })

    await drawer(page).getByTestId('live-variant-discard').last().click()
    await expect.poll(async () => (await state(page, key)).source).toBe(LIVE_ORIGINAL)
    await expect(page.locator('[data-grasp-live]')).toHaveCount(0)
    await pick()
    await markSelection(page)
    await overlay(page).locator('[data-act="mark-clear"]').click()
    await expect(overlay(page).locator('polyline')).toHaveCount(0)
    await expect(overlay(page).locator('textarea[data-mark-note]')).toHaveCount(0)
    await expect(overlay(page).locator('[data-act="mark-clear"]')).toBeDisabled()
    await overlay(page).locator('[data-act="go"]').click()
    await expect(drawer(page).getByTestId('live-variant-card').last()).toHaveAttribute('data-state', 'ready')
    const cleanRequest = (await state(page, key)).requests.at(-1).live
    expect(cleanRequest.op).toBe('generate')
    expect(cleanRequest).not.toHaveProperty('marks')
  })

  test('restored selection and tuned parameters reach Chat accept and persist after reload', async ({ page }) => {
    const key = 'chat-accept'
    await open(page, key)
    await generate(page)
    await expect(page.locator('[data-grasp-variant="1"]')).toBeVisible()
    await expect(page.locator('[data-grasp-variant="3"]')).toBeHidden()
    await expect(drawer(page).getByTestId('live-active-context')).toContainText('"current":1')
    await tuneSecond(page)
    await expect(drawer(page).getByTestId('live-active-context')).toContainText('"gap":"32px"')
    // Page tuning changes only the temporary view until it is accepted.
    expect((await state(page, key)).source).toContain('var(--gp-gap,24px)')

    await page.reload()
    await expect(drawer(page).getByTestId('live-drawer')).toBeVisible()
    await expect(page.locator('[data-grasp-variant="2"]')).toBeVisible()
    await expect(page.locator('[data-grasp-variant="1"]')).toBeHidden()
    await expect(drawer(page).getByTestId('live-variant-viewing').last()).toContainText('2')
    await expect(drawer(page).getByTestId('live-active-context')).toContainText('"gap":"32px"')

    await drawer(page).getByTestId('live-chat-input').fill('就用这个')
    await drawer(page).getByTestId('live-chat-send').click()
    await expect(drawer(page).getByTestId('live-variant-card').last()).toHaveAttribute('data-state', 'accepted')
    await expectCleanSource(page, key, 'Newsletter variant 2')
    const accepted = await state(page, key)
    expect(accepted.requests.at(-1)).toMatchObject({ text: '就用这个', liveCtx: { current: 2, params: { gap: '32px', tone: 'strong' } } })
    expect(accepted.requests[0].live.element).toMatchObject({ selector: 'section#newsletter', text: expect.stringContaining('Newsletter original') })
    await page.reload()
    await expect(page.locator('#live-source h2')).toHaveText('Newsletter variant 2')
    await expect(page.locator('#live-source .newsletter')).toHaveCSS('gap', '32px')
    await expect(page.locator('#live-source .newsletter')).toHaveCSS('color', 'rgb(18, 73, 182)')
  })

  test('drawer switching and comparison drive the real page; card accept keeps final knob values', async ({ page }) => {
    const key = 'card-accept'
    await open(page, key)
    await generate(page)
    await drawer(page).getByTestId('live-variant-chip').nth(2).click()
    await expect(page.locator('[data-grasp-variant="3"]')).toBeVisible()
    await expect(page.locator('[data-grasp-variant="3"]')).toHaveCSS('display', 'grid')
    await expect(page.locator('[data-grasp-variant="1"]')).toBeHidden()
    await drawer(page).getByTestId('live-variant-mode').click()
    await expect(page.locator('[data-grasp-variant="0"]')).toBeVisible()
    await expect(page.locator('[data-grasp-variant="1"]')).toBeVisible()
    await expect(page.locator('[data-grasp-variant="2"]')).toBeVisible()
    await drawer(page).getByTestId('live-variant-mode').click()
    await drawer(page).getByTestId('live-variant-chip').first().click()
    await tuneSecond(page)
    await drawer(page).getByTestId('live-variant-accept').click()
    await expectCleanSource(page, key, 'Newsletter variant 2')
    expect((await state(page, key)).requests.at(-1).live).toMatchObject({ op: 'accept', variant: 2, params: { gap: '32px', tone: 'strong' } })
    await page.reload()
    await expect(page.locator('#live-source .newsletter')).toHaveCSS('gap', '32px')
  })

  test('Chat refinement updates only the viewed candidate, then comparison and acceptance retain it', async ({ page }) => {
    const key = 'chat-refine'
    await open(page, key)
    await generate(page)
    await overlay(page).locator('[data-act="next"]').click()
    await expect(drawer(page).getByTestId('live-active-context')).toContainText('"current":2')
    await drawer(page).getByTestId('live-chat-input').fill('这个标题再大一点')
    await drawer(page).getByTestId('live-chat-send').click()
    await expect(page.locator('[data-grasp-variant="2"] h2')).toHaveText('Refined variant 2')
    await expect(drawer(page).getByTestId('live-variant-card').last()).toHaveAttribute('data-state', 'ready')
    await expect(page.locator('[data-grasp-variant="1"] h2')).toHaveText('Newsletter variant 1')
    await expect(page.locator('[data-grasp-variant="3"] h2')).toHaveText('Newsletter variant 3')
    await expect(page.locator('[data-grasp-variant="3"]')).toBeHidden()
    await drawer(page).getByTestId('live-variant-mode').last().click()
    await expect(page.locator('[data-grasp-variant="1"]')).toBeVisible()
    await expect(page.locator('[data-grasp-variant="2"]')).toBeVisible()
    await expect(page.locator('[data-grasp-variant="3"]')).toBeVisible()
    await drawer(page).getByTestId('live-variant-accept').last().click()
    await expectCleanSource(page, key, 'Refined variant 2')
    expect((await state(page, key)).requests[1]).toMatchObject({ text: '这个标题再大一点', liveCtx: { current: 2 } })
    await page.reload()
    await expect(page.locator('#live-source h2')).toHaveText('Refined variant 2')
  })

  test('discard restores source exactly; insertion discard removes only the new block', async ({ page }) => {
    const key = 'discard'
    await open(page, key)
    await generate(page)
    await drawer(page).getByTestId('live-variant-discard').click()
    await expect.poll(async () => (await state(page, key)).source).toBe(LIVE_ORIGINAL)
    await expect(page.locator('[data-grasp-live]')).toHaveCount(0)
    await expect(page.locator('#newsletter')).toBeVisible()

    await generate(page, 'Add another newsletter', true)
    await expect(page.locator('#newsletter')).toBeVisible()
    await expect(page.locator('[data-grasp-variant="0"]')).toHaveCount(0)
    await expect(page.locator('[data-grasp-variant="1"] h2')).toHaveText('Inserted variant 1')
    await drawer(page).getByTestId('live-variant-discard').last().click()
    await expect.poll(async () => (await state(page, key)).source).toBe(LIVE_ORIGINAL)
    await page.reload()
    await expect(page.locator('#live-source h2')).toHaveText('Newsletter original')
  })

  test('ready without a mounted wrapper reports failure and remains dismissible from Chat', async ({ page }) => {
    const key = 'mount-failure'
    await open(page, key)
    await generate(page, 'simulate missing wrapper')
    const card = drawer(page).getByTestId('live-variant-card').first()
    await expect(card).toHaveAttribute('data-state', 'failed', { timeout: 12_000 })
    await expect(drawer(page).getByTestId('live-variant-error').first()).toContainText('no [data-grasp-live=')
    await drawer(page).getByTestId('live-variant-discard').last().click()
    await expect.poll(async () => (await state(page, key)).sessions[0].state).toBe('discarded')
    expect((await state(page, key)).source).toBe(LIVE_ORIGINAL)
  })

  test('failed Steer can retry the same session or dismiss while retaining disclosed partial edits', async ({ page }) => {
    const key = 'steer-recovery'
    await open(page, key)
    const steer = async (chat: FrameLocator, prompt: string) => {
      await overlay(page).locator('[data-input="steer"]').fill(prompt)
      await overlay(page).locator('[data-act="steer"]').click()
      await expect(chat.getByTestId('live-variant-card').last()).toHaveAttribute('data-state', 'failed')
      await expect(page.locator('#steer-partial')).toBeVisible()
    }
    await steer(drawer(page), 'Make page hierarchy clearer')
    const failed = (await state(page, key)).sessions[0].sid
    await drawer(page).getByTestId('live-variant-retry').last().click()
    await expect.poll(async () => (await state(page, key)).sessions[0].state).toBe('done')
    await expect(page.locator('#steer-complete')).toBeVisible()
    const retried = await state(page, key)
    expect(retried.requests.filter((reply: { live?: { op: string; sid: string } }) => reply.live?.op === 'steer').map((reply: { live: { sid: string } }) => reply.live.sid)).toEqual([failed, failed])

    await steer(drawer(page), 'Improve the footer too')
    await drawer(page).getByTestId('live-variant-discard').last().click()
    await expect.poll(async () => (await state(page, key)).sessions.at(-1).state).toBe('discarded')
    await page.reload()
    await expect(page.locator('#steer-partial')).toBeVisible()
    await expect(page.locator('#steer-complete')).toBeVisible()
  })
})

// Entry regression uses the production EmbedNodeChatView and public Chat with
// real HTTP + WebSocket + postMessage. The HTTP surrogate derives capability
// from a saved node, including the omitted switch on historical approve runs.
// Go tests cover that same configuration against the actual handler/engine;
// source generation here remains a deterministic agent/HMR surrogate.
test.describe('Live entry on Grasp direct previews', () => {
  for (const nodeType of ['approve', 'grasp']) {
    test(`${nodeType} with an omitted Live switch supports pick, annotate, generate and Chat adopt`, async ({ page }, testInfo) => {
      const key = `entry-${nodeType}`
      await page.setViewportSize({ width: 1440, height: 1000 })
      await page.request.post(`${origin}/__e2e/live/reset?key=${key}&nodeType=${nodeType}`)
      const configured = await state(page, key)
      expect(configured.node).toEqual({ type: nodeType, config: { direct_preview: true } })
      const capability = page.waitForResponse((response) => response.url().endsWith('/public/gate-approvals/live-sessions'))
      await page.goto(`${origin}/live-variants.html?key=${key}`)
      await expect(drawer(page).getByTestId('embed-chat-root')).toBeVisible({ timeout: 15_000 })
      expect(await (await capability).json()).toMatchObject({ status: 'active', enabled: true, sessions: [] })
      await expect(drawer(page).getByTestId('clarify-input')).toBeVisible({ timeout: 15_000 })
      const liveSwitch = page.locator('grasp-preview-pick [data-role="live"]')
      await expect(liveSwitch).toHaveAttribute('aria-label', 'Live 实时变体')
      await expect(liveSwitch).toHaveAttribute('aria-pressed', 'false')
      await expect(liveSwitch).toHaveText('Live · 关')
      await liveSwitch.click()
      await expect(liveSwitch).toHaveAttribute('aria-pressed', 'true')
      await expect(liveSwitch).toHaveText('Live · 开')
      await overlay(page).locator('[data-act="close"]').click()
      await expect(liveSwitch).toHaveAttribute('aria-pressed', 'false')
      await expect(overlay(page).locator('[data-act="pick"]')).toBeHidden()
      await liveSwitch.click()
      await overlay(page).locator('[data-act="pick"]').click()
      await page.locator('#newsletter').click({ position: { x: 15, y: 15 } })
      await overlay(page).locator('[data-input="prompt"]').fill('保留订阅行为，给出三个更清晰的设计')
      await markSelection(page)
      await page.screenshot({ path: testInfo.outputPath(`live-${nodeType}-annotations.png`), animations: 'disabled' })
      const generated = page.waitForRequest((request) => request.url().endsWith('/public/gate-approvals/reply') && request.postDataJSON()?.live?.op === 'generate')
      await overlay(page).locator('[data-act="go"]').click()
      expect((await generated).postDataJSON()).toMatchObject({
        token: `live-e2e-${key}`, live: { op: 'generate', element: { selector: 'section#newsletter' }, marks: [{ kind: 'draw' }, { kind: 'note' }] },
      })
      await expect(drawer(page).getByTestId('live-variant-card').last()).toHaveAttribute('data-state', 'ready')
      await overlay(page).locator('[data-act="next"]').click()
      await expect(page.locator('[data-grasp-variant="2"]')).toBeVisible()
      await expect(page.locator('[data-grasp-variant="1"]')).toBeHidden()
      await expect(drawer(page).getByTestId('live-variant-viewing').last()).toContainText('2')
      await liveSwitch.click()
      await expect(liveSwitch).toHaveText('Live · 关')
      await expect(overlay(page).locator('[data-act="pick"]')).toBeHidden()
      await expect(page.locator('[data-grasp-variant="2"]')).toBeVisible()
      expect((await state(page, key)).sessions.at(-1).state).toBe('ready')
      await liveSwitch.click()
      await expect(liveSwitch).toHaveText('Live · 开')
      await expect(overlay(page).locator('[data-act="pick"]')).toBeVisible()
      await page.screenshot({ path: testInfo.outputPath(`live-${nodeType}-variants.png`), animations: 'disabled' })
      await drawer(page).getByTestId('clarify-input').fill('就用这个')
      await drawer(page).getByTestId('clarify-send-label').click()
      await expect(drawer(page).getByTestId('live-variant-card').last()).toHaveAttribute('data-state', 'accepted')
      expect((await state(page, key)).requests.at(-1)).toMatchObject({ text: '就用这个', liveCtx: { current: 2 } })
      await expectCleanSource(page, key, 'Newsletter variant 2')
      await page.reload()
      await expect(page.locator('#live-source h2')).toHaveText('Newsletter variant 2')
      await expect(drawer(page).getByTestId('live-variant-card').last()).toHaveAttribute('data-state', 'accepted')
    })
  }

  for (const setting of ['live=false', 'direct=false']) {
    test(`legacy approve hides Live when ${setting} while retaining Chat`, async ({ page }) => {
      const key = `entry-disabled-${setting.split('=')[0]}`
      await page.request.post(`${origin}/__e2e/live/reset?key=${key}&nodeType=approve&${setting}`)
      const capability = page.waitForResponse((response) => response.url().endsWith('/public/gate-approvals/live-sessions'))
      await page.goto(`${origin}/live-variants.html?key=${key}`)
      await expect(drawer(page).getByTestId('clarify-input')).toBeVisible({ timeout: 15_000 })
      expect(await (await capability).json()).toMatchObject({ enabled: false })
      await expect(page.locator('grasp-preview-pick [data-role="live"]')).toBeHidden()
      await expect(overlay(page)).toHaveCount(0)
      expect((await state(page, key)).requests).toEqual([])
    })
  }
})
