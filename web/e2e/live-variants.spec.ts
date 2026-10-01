import { expect, test, type FrameLocator, type Page } from '@playwright/test'
import { LIVE_ORIGINAL } from './live-variants-mock'

const origin = 'http://127.0.0.1:5174'
const overlay = (page: Page) => page.locator('grasp-live-overlay')
const drawer = (page: Page) => page.frameLocator('grasp-preview-pick [data-role="drawer"] iframe')
const bar = (page: Page, role: string) => page.locator(`grasp-preview-pick [data-role="${role}"]`)

/** Pick the newsletter with the bar's Pick and open the design form from the action card. */
async function pickForDesign(page: Page) {
  await bar(page, 'toggle').click()
  await page.locator('#newsletter').click({ position: { x: 15, y: 15 } })
  await overlay(page).locator('[data-act="to-design"]').click()
}

async function state(page: Page, key: string) {
  return (await page.request.get(`${origin}/__e2e/live/state?key=${key}`)).json()
}

async function open(page: Page, key: string) {
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.request.post(`${origin}/__e2e/live/reset?key=${key}`)
  await page.goto(`${origin}/live-variants.html?key=${key}`)
  await expect(drawer(page).getByTestId('live-drawer')).toBeVisible()
  // Pick only gets its Live tooltip once the overlay is loaded and usable.
  await expect(bar(page, 'toggle')).toHaveAttribute('title', /.+/)
}

async function generate(page: Page, prompt = '', inserted = false) {
  if (inserted) {
    await bar(page, 'toggle').click()
    await overlay(page).locator('[data-act="mode-insert"]').click()
    await page.locator('#newsletter').click({ position: { x: 15, y: 15 } })
  } else {
    await pickForDesign(page)
  }
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

async function openMarks(page: Page) {
  const toggle = overlay(page).locator('[data-act="marks-toggle"]')
  if ((await toggle.getAttribute('aria-expanded')) !== 'true') await toggle.click()
}

async function markSelection(page: Page) {
  await openMarks(page)
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
    const pick = () => pickForDesign(page)
    await pick()
    await overlay(page).locator('[data-act="action"][data-v="animate"]').click()
    await expect(overlay(page).locator('[data-act="action"][data-v="animate"]')).toHaveAttribute('aria-pressed', 'true')
    await openMarks(page)
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
    await expect(overlay(page).locator('[data-act="mark-clear"]')).toHaveCount(0)
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
    // Compare leaves the page layout alone and has one toolbar to leave it.
    await expect(page.locator('[data-grasp-live]')).not.toHaveCSS('display', 'grid')
    const compareBar = overlay(page).locator('[data-sw][data-compare]')
    await expect(compareBar).toBeVisible()
    await expect(overlay(page).locator('[data-tag]')).toHaveCount(4)
    await overlay(page).locator('[data-tag][data-n="3"]').click()
    await expect(overlay(page).locator('[data-tag][data-n="3"]')).toHaveAttribute('aria-pressed', 'true')
    await page.keyboard.press('Escape')
    await expect(compareBar).toBeHidden()
    await expect(page.locator('[data-grasp-variant="1"]')).toBeHidden()
    await expect(page.locator('[data-grasp-variant="3"]')).toBeVisible()
    await overlay(page).locator('[data-act="compare"]').click()
    await compareBar.locator('[data-act="inplace"]').click()
    await expect(page.locator('[data-grasp-variant="0"]')).toBeHidden()
    await expect(page.locator('[data-grasp-variant="3"]')).toBeVisible()
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
      // Whole-page adjustment is the input in Pick's bar.
      await bar(page, 'toggle').click()
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
      await expect(bar(page, 'live')).toHaveCount(0)
      await expect(bar(page, 'toggle')).toHaveAttribute('title', '点选元素发到对话或生成候选，也可以插入区块或整页调整')
      await expect(bar(page, 'toggle')).toHaveText('取点')
      await expect(bar(page, 'insert')).toHaveCount(0)
      await expect(bar(page, 'steer')).toHaveCount(0)
      await expect(bar(page, 'eye')).toBeHidden()
      // Pick opens one bar for select / insert and the whole-page input.
      await bar(page, 'toggle').click()
      await expect(overlay(page).locator('[data-act="mode-select"]')).toHaveText('选元素')
      await expect(overlay(page).locator('[data-input="steer"]')).toBeVisible()
      await page.screenshot({ path: testInfo.outputPath(`live-${nodeType}-pickbar.png`), animations: 'disabled' })
      await bar(page, 'toggle').click()
      await expect(overlay(page).locator('.pickbar')).toHaveCount(0)
      // The action card can still send the element to Chat as a plain pick.
      await bar(page, 'toggle').click()
      await page.locator('#newsletter').click({ position: { x: 15, y: 15 } })
      await expect(overlay(page).locator('[data-act="to-design"]')).toBeEnabled()
      await overlay(page).locator('[data-act="to-chat"]').click()
      await expect(drawer(page).getByTestId('clarify-annotation-chip')).toBeVisible()
      await pickForDesign(page)
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
      // Hiding the candidate controls keeps the viewed candidate and the session.
      const eye = bar(page, 'eye')
      await expect(eye).toBeVisible()
      await eye.click()
      await expect(eye).toHaveAttribute('aria-pressed', 'true')
      await expect(overlay(page).locator('[data-act="next"]')).toBeHidden()
      await expect(page.locator('[data-grasp-variant="2"]')).toBeVisible()
      expect((await state(page, key)).sessions.at(-1).state).toBe('ready')
      await eye.click()
      await expect(eye).toHaveAttribute('aria-pressed', 'false')
      await expect(overlay(page).locator('[data-act="next"]')).toBeVisible()
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
      await expect(bar(page, 'eye')).toBeHidden()
      await expect(overlay(page)).toHaveCount(0)
      expect((await state(page, key)).requests).toEqual([])
    })
  }
})

test.describe('production Chat composer page candidates', () => {
  async function entry(page: Page, key: string, options = '') {
    await page.setViewportSize({ width: 1440, height: 1000 })
    await page.evaluate(() => localStorage.removeItem('__grasp_embed')).catch(() => {})
    await page.request.post(`${origin}/__e2e/live/reset?key=${key}&nodeType=grasp${options}`)
    await page.goto(`${origin}/live-variants.html?key=${key}&tab=layout`)
    await expect(drawer(page).getByTestId('clarify-input')).toBeVisible({ timeout: 15_000 })
  }

  async function openControls(page: Page) {
    const toggle = drawer(page).getByTestId('page-collaboration-toggle')
    await expect(toggle).toBeVisible()
    if (await toggle.getAttribute('aria-expanded') !== 'true') await toggle.click()
    await expect(drawer(page).getByTestId('page-collaboration-controls')).toBeVisible()
  }

  async function attachReference(page: Page) {
    await drawer(page).locator('input[type="file"]').setInputFiles({ name: 'reference.png', mimeType: 'image/png', buffer: Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aNf8AAAAASUVORK5CYII=', 'base64') })
    await expect(drawer(page).getByTestId('clarify-draft-image-thumb')).toBeVisible()
  }

  test('Chat switch generates 3 candidates without picking, syncs selection 2 and waits for Chat adoption', async ({ page }, testInfo) => {
    const key = 'entry-composer'
    await entry(page, key)
    const mode = drawer(page).getByTestId('live-candidate-mode')
    const tools = bar(page, 'toggle')
    await expect(drawer(page).getByTestId('page-collaboration-toggle')).toHaveAttribute('aria-expanded', 'false')
    await openControls(page)
    await expect(mode).toHaveAttribute('aria-checked', 'false')
    await expect(tools).toHaveAttribute('aria-pressed', 'false')
    await attachReference(page)
    // A normal preview annotation, delivered through the authenticated parent.
    // It does not select a Live target or open the Live tools panel.
    await page.evaluate(() => {
      const frame = document.querySelector('grasp-preview-pick')?.shadowRoot?.querySelector<HTMLIFrameElement>('[data-role="drawer"] iframe')
      frame?.contentWindow?.postMessage({ type: 'grasp-embed:pick', payload: { selector: 'section#newsletter h2', tagName: 'H2', text: 'Newsletter original', outerHTML: '<h2>Newsletter original</h2>', url: location.href } }, location.origin)
    })
    await expect(drawer(page).getByTestId('clarify-annotation-chip')).toBeVisible()
    await openControls(page)
    await mode.click()
    await expect(drawer(page).getByTestId('live-candidate-hint')).toContainText('3')
    await drawer(page).getByTestId('clarify-input').fill('保留订阅交互，生成三个整页设计候选供我挑选')
    await page.screenshot({ path: testInfo.outputPath('chat-page-candidates-switch.png'), animations: 'disabled' })
    await drawer(page).getByTestId('public-gate-chat-host').screenshot({ path: testInfo.outputPath('chat-page-candidates-composer.png'), animations: 'disabled' })
    const request = page.waitForRequest((item) => item.url().endsWith('/public/gate-approvals/reply') && item.postDataJSON()?.live?.scope === 'page')
    await drawer(page).getByTestId('clarify-send-label').click()
    const body = (await request).postDataJSON()
    expect(body).toMatchObject({
      text: '保留订阅交互，生成三个整页设计候选供我挑选',
      live: { op: 'generate', scope: 'page', count: 3, url: `${origin}/live-variants.html?key=${key}&tab=layout` },
      images: [{ name: 'reference.png', mimeType: 'image/png' }],
      annotations: [{ selector: 'section#newsletter h2' }],
    })
    expect(body.live).not.toHaveProperty('element')
    expect(body.live).not.toHaveProperty('action')
    await expect(drawer(page).getByTestId('live-variant-card').last()).toHaveAttribute('data-state', 'ready')
    await expect(page.locator('[data-grasp-variant]:not([data-grasp-variant="0"])')).toHaveCount(3)
    await expect(overlay(page).locator('[data-act="next"]')).toBeVisible()
    await expect(tools).toHaveAttribute('aria-pressed', 'false')
    await expect(overlay(page).locator('.panel')).toHaveCount(0)
    const generated = await state(page, key)
    expect(generated.sessions).toHaveLength(1)
    expect(generated.sessions[0].state).toBe('ready')
    expect(generated.source).toContain('data-grasp-live')
    await overlay(page).locator('[data-act="next"]').click()
    await expect(page.locator('[data-grasp-variant="2"]')).toBeVisible()
    await expect(drawer(page).getByTestId('live-variant-viewing').last()).toContainText('2')
    // Turning the Chat send mode off does not close, accept or discard candidates.
    await openControls(page)
    await mode.click()
    await expect(mode).toHaveAttribute('aria-checked', 'false')
    await expect(page.locator('[data-grasp-variant="2"]')).toBeVisible()
    expect((await state(page, key)).sessions[0].state).toBe('ready')
    await page.screenshot({ path: testInfo.outputPath('chat-page-candidates-choose.png'), animations: 'disabled' })
    await openControls(page)
    await mode.click()
    await drawer(page).getByTestId('clarify-input').fill('就用这个')
    await drawer(page).getByTestId('clarify-send-label').click()
    await expect(drawer(page).getByTestId('live-variant-card').last()).toHaveAttribute('data-state', 'accepted')
    const final = await state(page, key)
    expect(final.sessions).toHaveLength(1)
    expect(final.requests.at(-1)).toMatchObject({ text: '就用这个', liveCtx: { sid: body.live.sid, current: 2 } })
    expect(final.requests.at(-1)).not.toHaveProperty('live')
    await expectCleanSource(page, key, 'Newsletter variant 2')
  })

  test('off sends ordinary Chat and never creates candidate source', async ({ page }) => {
    const key = 'entry-composer-off'
    await entry(page, key)
    await openControls(page)
    await expect(drawer(page).getByTestId('live-candidate-mode')).toHaveAttribute('aria-checked', 'false')
    await drawer(page).getByTestId('clarify-input').fill('普通聊天，请解释页面结构')
    await drawer(page).getByTestId('clarify-send-label').click()
    await expect.poll(async () => (await state(page, key)).requests.length).toBe(1)
    expect((await state(page, key)).requests[0]).toMatchObject({ text: '普通聊天，请解释页面结构', liveCtx: null })
    expect((await state(page, key)).requests[0]).not.toHaveProperty('live')
    expect((await state(page, key)).sessions).toEqual([])
    expect((await state(page, key)).source).toBe(LIVE_ORIGINAL)
  })

  test('read-only and disabled previews do not expose candidate sending', async ({ page }) => {
    for (const options of ['&permission=react_only', '&live=false']) {
      const key = `entry-composer-${options.includes('permission') ? 'readonly' : 'disabled'}`
      await entry(page, key, options)
      await openControls(page)
      await expect(drawer(page).getByTestId('live-candidate-mode')).toHaveCount(0)
      await drawer(page).getByTestId('clarify-input').fill('普通只读回复')
      await drawer(page).getByTestId('clarify-send-label').click()
      await expect.poll(async () => (await state(page, key)).requests.length).toBe(1)
      expect((await state(page, key)).requests[0]).not.toHaveProperty('live')
    }
  })

  test('a rejected generate preserves draft, attachments and stable retry SID', async ({ page }) => {
    const key = 'entry-composer-retry'
    await entry(page, key)
    let rejectedSid = ''
    await page.route('**/public/gate-approvals/reply', async (route) => {
      if (!rejectedSid) {
        rejectedSid = route.request().postDataJSON().live.sid
        await route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: 'temporary outage' }) })
      } else await route.continue()
    })
    await attachReference(page)
    await openControls(page)
    await drawer(page).getByTestId('live-candidate-mode').click()
    await drawer(page).getByTestId('clarify-input').fill('保留这个需求与参考图')
    await drawer(page).getByTestId('clarify-send-label').click()
    await expect(drawer(page).getByTestId('clarify-confirm-error')).toContainText('temporary outage')
    await expect(drawer(page).getByTestId('clarify-input')).toHaveValue('保留这个需求与参考图')
    await expect(drawer(page).getByTestId('clarify-draft-image-thumb')).toBeVisible()
    expect((await state(page, key)).requests).toEqual([])
    await drawer(page).getByTestId('clarify-send-label').click()
    await expect(drawer(page).getByTestId('live-variant-card').last()).toHaveAttribute('data-state', 'ready')
    expect((await state(page, key)).requests[0].live.sid).toBe(rejectedSid)
    await expect(drawer(page).getByTestId('clarify-input')).toHaveValue('')
  })

  test('unavailable page controls keep the request instead of silently sending ordinary Chat', async ({ page }) => {
    const key = 'entry-composer-no-overlay'
    await page.route('**/live-overlay.js', (route) => route.abort())
    await entry(page, key)
    await openControls(page)
    await drawer(page).getByTestId('live-candidate-mode').click()
    await drawer(page).getByTestId('clarify-input').fill('三个页面候选')
    await drawer(page).getByTestId('clarify-send-label').click()
    await expect(drawer(page).getByTestId('clarify-confirm-error')).toBeVisible()
    await expect(drawer(page).getByTestId('clarify-input')).toHaveValue('三个页面候选')
    expect((await state(page, key)).requests).toEqual([])
  })

  test('attachment-adjacent page collaboration menu supports keyboard, narrow Chat and independent switches', async ({ page }, testInfo) => {
    await page.addInitScript(() => { if (!localStorage.getItem('grasp-locale')) localStorage.setItem('grasp-locale', 'en') })
    const key = 'entry-grouped-controls'
    await entry(page, key)
    const chat = drawer(page)
    const group = chat.getByTestId('page-collaboration-controls')
    const toggle = chat.getByTestId('page-collaboration-toggle')
    const summary = chat.getByTestId('page-collaboration-summary')
    const mode = chat.getByTestId('live-candidate-mode')
    const control = chat.getByTestId('page-control-toggle')
    const input = chat.getByTestId('clarify-input')
    const send = chat.getByTestId('clarify-send-label')
    const attachment = chat.getByTestId('clarify-attach-btn')
    const actions = chat.getByTestId('clarify-action-row')
    async function closeByOutside() {
      // The upward popup may cover the textarea. Click the history instead of
      // forcing an intercepted input click, then restore the draft's focus.
      await chat.getByTestId('public-gate-chat-host').click({ position: { x: 8, y: 8 } })
      await expect(group).toBeHidden()
      await input.focus()
    }
    await expect(toggle).toHaveAttribute('aria-expanded', 'false')
    await expect(group).toBeHidden()
    await expect(summary).toHaveText('')
    await expect(summary).toHaveClass(/sr-only/)
    await expect(toggle).toHaveAccessibleName('Page collaboration')
    await expect(toggle).toHaveAttribute('title', /Page collaboration/)
    await expect(actions.getByTestId('clarify-attach-btn')).toHaveCount(1)
    await expect(actions.getByTestId('page-collaboration-toggle')).toHaveCount(1)
    const draft = 'Keep the subscription flow and generate three designs for me to choose.'
    await input.fill(draft)
    await toggle.focus()
    await toggle.press('Enter')
    await expect(toggle).toHaveAttribute('aria-expanded', 'true')
    await expect(group).toBeVisible()
    await expect(group).toHaveAccessibleName('Page collaboration')
    await expect(group.getByTestId('live-candidate-mode')).toHaveCount(1)
    await expect(group.getByTestId('page-control-toggle')).toHaveCount(1)
    await expect(control).toHaveAttribute('aria-checked', 'false')
    await expect(mode).toHaveAttribute('aria-checked', 'false')
    await expect(control).toBeFocused()
    // A native switch is operable with Space. Escape closes the popup and
    // restores the trigger, so keyboard users can immediately reopen it.
    await mode.focus()
    await mode.press('Escape')
    await expect(group).toBeHidden()
    await expect(toggle).toHaveAttribute('aria-expanded', 'false')
    await expect(toggle).toBeFocused()
    await toggle.press('Space')
    await expect(group).toBeVisible()
    // Language options sit after the switches. Tab moves through them, and
    // only leaving the last option closes the panel and returns to send.
    const localeEn = chat.getByTestId('preview-chat-locale-en')
    const localeZh = chat.getByTestId('preview-chat-locale-zh')
    await mode.focus()
    await mode.press('Tab')
    await expect(localeEn).toBeFocused()
    await expect(group).toBeVisible()
    await localeEn.press('Tab')
    await expect(localeZh).toBeFocused()
    await localeZh.press('Tab')
    await expect(group).toBeHidden()
    await expect(send).toBeFocused()
    await openControls(page)
    await closeByOutside()
    await expect(group).toBeHidden()
    await expect(input).toBeFocused()
    await expect(input).toHaveValue(draft)
    await openControls(page)
    await page.evaluate(() => {
      const captured: unknown[] = []
      ;(window as Window & { groupedPageMessages?: unknown[] }).groupedPageMessages = captured
      window.addEventListener('message', (event) => {
        const frame = document.querySelector('grasp-preview-pick')?.shadowRoot?.querySelector<HTMLIFrameElement>('[data-role="drawer"] iframe')
        if (event.source === frame?.contentWindow && event.origin === location.origin && event.data?.type === 'grasp-embed:control') captured.push(event.data)
      })
    })
    const controlMessages = () => page.evaluate(() => (window as Window & { groupedPageMessages?: unknown[] }).groupedPageMessages || [])
    await mode.focus()
    await mode.press('Space')
    await expect(mode).toHaveAttribute('aria-checked', 'true')
    await expect(control).toHaveAttribute('aria-checked', 'false')
    await expect(chat.getByTestId('live-candidate-hint')).toContainText('3')
    expect(await controlMessages()).toEqual([])
    await control.click()
    await expect(control).toHaveAttribute('aria-checked', 'true')
    await expect(mode).toHaveAttribute('aria-checked', 'true')
    await expect(chat.getByTestId('page-control-status')).toBeVisible()
    await expect.poll(controlMessages).toEqual([expect.objectContaining({ on: true })])
    await mode.click()
    await expect(mode).toHaveAttribute('aria-checked', 'false')
    await expect(control).toHaveAttribute('aria-checked', 'true')
    expect(await controlMessages()).toEqual([expect.objectContaining({ on: true })])
    await mode.click()
    await control.click()
    await expect(mode).toHaveAttribute('aria-checked', 'true')
    await expect(control).toHaveAttribute('aria-checked', 'false')
    await expect.poll(controlMessages).toEqual([expect.objectContaining({ on: true }), expect.objectContaining({ on: false })])
    await closeByOutside()
    await expect(group).toBeHidden()
    await expect(summary).toContainText('Candidates')
    await expect(toggle).toContainText('1')
    await expect(toggle).toHaveAttribute('aria-describedby', await summary.getAttribute('id') || '')
    await expect(toggle).toHaveAttribute('title', /Candidates/)
    await expect(input).toHaveValue(draft)
    await openControls(page)
    await expect(mode).toHaveAttribute('aria-checked', 'true')
    await expect(control).toHaveAttribute('aria-checked', 'false')

    // Resize the actual shipping drawer, checking closed and open states. The
    // popup opens upwards, fits the iframe, and leaves the send action visible.
    async function resizeChat(width: number) {
      const frame = page.locator('grasp-preview-pick [data-role="drawer"] iframe')
      const initial = await frame.boundingBox()
      const edge = await page.locator('grasp-preview-pick [data-role="drawer"] .edge.w').boundingBox()
      expect(initial).not.toBeNull()
      expect(edge).not.toBeNull()
      const x = edge!.x + edge!.width / 2
      const y = edge!.y + edge!.height / 2
      await page.mouse.move(x, y)
      await page.mouse.down()
      await page.mouse.move(x - (width - initial!.width), y, { steps: 4 })
      await page.mouse.up()
      await expect.poll(async () => Math.round((await frame.boundingBox())!.width)).toBe(width)
    }
    for (const width of [350, 390, 500]) {
      await closeByOutside()
      await resizeChat(width)
      await expect(toggle).toBeInViewport({ ratio: 1 })
      await expect(attachment).toBeInViewport({ ratio: 1 })
      await expect(summary).toHaveClass(/sr-only/)
      const alignment = await toggle.evaluate((element) => {
        const row = element.closest('[data-testid="clarify-action-row"]')!
        const attachment = row.querySelector('[data-testid="clarify-attach-btn"]')!
        const button = element.getBoundingClientRect()
        const attach = attachment.getBoundingClientRect()
        return { sameRow: !!row, deltaY: Math.abs(button.top - attach.top), gap: button.left - attach.right, width: button.width, height: button.height, overflow: row.scrollWidth - row.clientWidth }
      })
      expect(alignment.sameRow).toBe(true)
      expect(alignment.deltaY).toBeLessThanOrEqual(1)
      expect(alignment.gap).toBeGreaterThanOrEqual(0)
      expect(alignment.gap).toBeLessThanOrEqual(12)
      expect(alignment.width).toBe(40)
      expect(alignment.height).toBe(40)
      expect(alignment.overflow).toBeLessThanOrEqual(1)
      await expect(send).toBeInViewport({ ratio: 1 })
      await expect(group).toBeHidden()
      await openControls(page)
      await expect(send).toBeInViewport({ ratio: 1 })
      await expect(mode).toBeInViewport({ ratio: 1 })
      await expect(control).toBeInViewport({ ratio: 1 })
      const dimensions = await group.evaluate((element) => ({
        groupOverflow: element.scrollWidth - element.clientWidth,
        pageOverflow: document.documentElement.scrollWidth - document.documentElement.clientWidth,
        groupTop: element.getBoundingClientRect().top,
        groupRight: element.getBoundingClientRect().right,
        groupBottom: element.getBoundingClientRect().bottom,
        triggerTop: document.querySelector('[data-testid="page-collaboration-toggle"]')!.getBoundingClientRect().top,
        viewportWidth: innerWidth,
      }))
      expect(dimensions.groupOverflow).toBeLessThanOrEqual(1)
      expect(dimensions.pageOverflow).toBeLessThanOrEqual(1)
      expect(dimensions.groupTop).toBeGreaterThanOrEqual(0)
      expect(dimensions.groupRight).toBeLessThanOrEqual(dimensions.viewportWidth)
      expect(dimensions.groupBottom).toBeLessThanOrEqual(dimensions.triggerTop)
      await expect(input).toHaveValue(draft)
    }
    await closeByOutside()
    await resizeChat(350)
    await openControls(page)
    await chat.getByTestId('public-gate-chat-host').screenshot({ path: testInfo.outputPath('page-collaboration-toolbar-narrow.png'), animations: 'disabled' })

    await page.evaluate(() => localStorage.setItem('grasp-locale', 'zh-CN'))
    await entry(page, 'entry-grouped-controls-zh')
    await resizeChat(500)
    await input.fill('保留订阅交互，给我三个页面候选，选好后再采用。')
    await expect(group).toBeHidden()
    await chat.getByTestId('clarify-input-row').screenshot({ path: testInfo.outputPath('page-collaboration-toolbar-closed.png'), animations: 'disabled' })
    await openControls(page)
    await expect(group).toHaveAccessibleName('页面协作')
    await mode.click()
    await chat.getByTestId('public-gate-chat-host').screenshot({ path: testInfo.outputPath('page-collaboration-toolbar-open.png'), animations: 'disabled' })
    await closeByOutside()
    await expect(summary).toContainText('页面候选')
    expect((await state(page, key)).requests).toEqual([])
  })

})
