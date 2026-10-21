import { test, expect, type Page } from '@playwright/test'
import { mkdir } from 'node:fs/promises'

const SHOT_DIR = 'test-results/screenshots-confirm-clip'

async function waitForToolbarAnimations(page: Page) {
  const toolbar = page.getByTestId('clarify-action-row')
  if (await toolbar.count()) {
    await expect.poll(() => toolbar.evaluate((el) =>
      el.getAnimations({ subtree: true }).filter((animation) => animation.playState === 'running').length,
    )).toBe(0)
  }
}

async function assertButtonFullyInsideHost(page: Page, label = '确认并流转') {
  const host = page.getByTestId('clip-host')
  const btn = page.getByTestId('clarify-confirm-flow')
  await expect(host).toBeVisible()
  await expect(btn).toBeVisible()
  await expect(btn).toContainText(label)
  await waitForToolbarAnimations(page)

  const geometry = await page.evaluate(() => {
    const hostEl = document.querySelector('[data-testid="clip-host"]') as HTMLElement | null
    const btnEl = document.querySelector('[data-testid="clarify-confirm-flow"]') as HTMLElement | null
    if (!hostEl || !btnEl) return null
    const hr = hostEl.getBoundingClientRect()
    const br = btnEl.getBoundingClientRect()
    const style = getComputedStyle(btnEl)
    return {
      host: { left: hr.left, right: hr.right, top: hr.top, bottom: hr.bottom, height: hr.height },
      btn: { left: br.left, right: br.right, top: br.top, bottom: br.bottom, height: br.height, width: br.width },
      btnCssHeight: parseFloat(style.height),
      fullyInside:
        br.left >= hr.left - 0.5 &&
        br.right <= hr.right + 0.5 &&
        br.top >= hr.top - 0.5 &&
        br.bottom <= hr.bottom + 0.5 &&
        br.height >= 35 /* h-9 ≈ 36px */,
    }
  })

  expect(geometry).toBeTruthy()
  expect(geometry!.btnCssHeight).toBeGreaterThanOrEqual(35)
  expect(geometry!.btn.height).toBeGreaterThanOrEqual(35)
  expect(geometry!.fullyInside).toBe(true)
  // Not truncated to ~14 CSS px as in the bug screenshot.
  expect(geometry!.btn.height).toBeGreaterThan(20)
}

async function assertIntegratedActions(page: Page) {
  await expect(page.getByTestId('clarify-input-row')).toBeVisible()
  await waitForToolbarAnimations(page)
  const geometry = await page.evaluate(() => {
    const host = document.querySelector('[data-testid="clip-host"]') as HTMLElement
    const input = document.querySelector('[data-testid="clarify-input-row"]') as HTMLElement
    const toolbar = document.querySelector('[data-testid="clarify-action-row"]') as HTMLElement
    const confirm = document.querySelector('[data-testid="clarify-confirm-flow"]') as HTMLElement
    const attach = document.querySelector('[data-testid="clarify-attach-btn"]') as HTMLElement
    if (!host || !input || !toolbar || !confirm || !attach) return null
    const inside = (child: HTMLElement, parent: HTMLElement) => {
      const cr = child.getBoundingClientRect()
      const pr = parent.getBoundingClientRect()
      return cr.left >= pr.left - 0.5 && cr.right <= pr.right + 0.5 &&
        cr.top >= pr.top - 0.5 && cr.bottom <= pr.bottom + 0.5
    }
    const cr = confirm.getBoundingClientRect()
    const ar = attach.getBoundingClientRect()
    return {
      confirmInInput: confirm.closest('[data-testid="clarify-input-row"]') === input,
      confirmInToolbar: inside(confirm, toolbar),
      toolbarInInput: inside(toolbar, input),
      inputInHost: inside(input, host),
      toolbarInHost: inside(toolbar, host),
      attachmentInToolbar: inside(attach, toolbar),
      noAttachmentOverlap: cr.right <= ar.left + 0.5 || cr.left >= ar.right - 0.5 ||
        cr.bottom <= ar.top + 0.5 || cr.top >= ar.bottom - 0.5,
      noHorizontalOverflow: host.scrollWidth <= host.clientWidth + 1,
    }
  })
  expect(geometry).not.toBeNull()
  expect(geometry).toEqual({
    confirmInInput: true,
    confirmInToolbar: true,
    toolbarInInput: true,
    inputInHost: true,
    toolbarInHost: true,
    attachmentInToolbar: true,
    noAttachmentOverlap: true,
    noHorizontalOverflow: true,
  })
}

async function sampleDraftTransition(page: Page, text: string, duration = 360) {
  return page.evaluate(async ({ value, durationMs }) => {
    const host = document.querySelector('[data-testid="clip-host"]') as HTMLElement
    const composer = document.querySelector('[data-testid="clarify-input-row"]') as HTMLElement
    const toolbar = document.querySelector('[data-testid="clarify-action-row"]') as HTMLElement
    const input = document.querySelector('[data-testid="clarify-input"]') as HTMLTextAreaElement
    const snapshot = () => {
      // The composer renders exactly one primary action that morphs between
      // 确认并流转 and 发送 (ComposerPrimaryAction); track that single button.
      const primaries = toolbar.querySelectorAll<HTMLButtonElement>('.composer-primary')
      const primary = primaries[0]!
      const pr = primary.getBoundingClientRect()
      const cr = composer.getBoundingClientRect()
      const hr = host.getBoundingClientRect()
      return {
        primaryX: pr.left,
        primaryY: pr.top,
        primaryCount: primaries.length,
        kind: primary.dataset.kind ?? null,
        disabled: primary.disabled,
        height: cr.height,
        inside: cr.left >= hr.left - 0.5 && cr.right <= hr.right + 0.5 &&
          cr.top >= hr.top - 0.5 && cr.bottom <= hr.bottom + 0.5,
        noOverflow: host.scrollWidth <= host.clientWidth + 1,
        runningAnimations: toolbar.getAnimations({ subtree: true }).filter((animation) => animation.playState === 'running').length,
      }
    }
    const samples = [snapshot()]
    input.value = value
    input.dispatchEvent(new Event('input', { bubbles: true }))
    const started = performance.now()
    do {
      await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()))
      samples.push(snapshot())
    } while (performance.now() - started < durationMs)
    return samples
  }, { value: text, durationMs: duration })
}

type MotionSamples = Awaited<ReturnType<typeof sampleDraftTransition>>

function assertStableComposer(samples: MotionSamples, allowRowChange = false) {
  const heights = samples.map((sample) => sample.height)
  if (!allowRowChange) expect(Math.max(...heights) - Math.min(...heights)).toBeLessThanOrEqual(1)
  expect(samples.every((sample) => sample.inside && sample.noOverflow)).toBe(true)
}

function assertSmoothPrimaryMorph(samples: MotionSamples) {
  // Exactly one primary in every frame; its edge travels through intermediate
  // positions (width morph) rather than jumping.
  expect(samples.every((sample) => sample.primaryCount === 1)).toBe(true)
  const start = samples[0]!
  const end = samples.at(-1)!
  const dx = end.primaryX - start.primaryX
  expect(Math.abs(dx)).toBeGreaterThan(2)
  const progresses = samples.slice(1, -1).map((sample) =>
    (sample.primaryX - start.primaryX) / dx,
  )
  expect(progresses.some((progress) => progress > 0.1 && progress < 0.9)).toBe(true)
}

test.describe('确认并流转底栏裁切回归', () => {
  test.beforeAll(async () => {
    await mkdir(SHOT_DIR, { recursive: true })
  })

  test('截图同款：pageControl=offline + 定高宿主，按钮完整在卡片内', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 })
    await page.goto('/confirm-flow-clip.html?h=320')
    await expect(page.getByTestId('page-control-status')).toContainText('未连接')
    await expect(page.getByTestId('clarify-hot-actions')).toBeVisible()
    await assertButtonFullyInsideHost(page)
    await page.screenshot({ path: `${SHOT_DIR}/01-offline-status-confirm-full.png` })
  })

  test('确认失败条出现时按钮与错误条都在卡片内', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 })
    await page.goto('/confirm-flow-clip.html?h=360&error=1')
    await expect(page.getByTestId('clarify-confirm-error')).toBeVisible()
    await assertButtonFullyInsideHost(page)

    const errInside = await page.evaluate(() => {
      const hostEl = document.querySelector('[data-testid="clip-host"]') as HTMLElement
      const errEl = document.querySelector('[data-testid="clarify-confirm-error"]') as HTMLElement
      const hr = hostEl.getBoundingClientRect()
      const er = errEl.getBoundingClientRect()
      return er.bottom <= hr.bottom + 0.5 && er.top >= hr.top - 0.5 && er.height > 10
    })
    expect(errInside).toBe(true)
    await page.screenshot({ path: `${SHOT_DIR}/02-confirm-error-bar-full.png` })
  })

  test('多行未发送内容隐藏流转，消息可滚动，清空后按钮完整恢复', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 })
    await page.goto('/confirm-flow-clip.html?h=380&tall=1')
    await expect(page.getByTestId('clarify-input')).toHaveValue(/多行输入第 1 行/)
    await expect(page.getByTestId('clarify-confirm-flow')).toBeHidden()
    const scroller = page.getByTestId('clarify-scroller')
    await expect(scroller).toBeVisible()
    // Scroller should be the flex grow / scroll region.
    const overflowY = await scroller.evaluate((el) => getComputedStyle(el).overflowY)
    expect(overflowY).toMatch(/auto|scroll/)
    expect(await page.getByTestId('clarify-input').evaluate((el) => el.getBoundingClientRect().height)).toBeLessThanOrEqual(128)
    await page.getByTestId('clarify-input').fill('')
    await assertButtonFullyInsideHost(page)
    await assertIntegratedActions(page)
    await page.screenshot({ path: `${SHOT_DIR}/03-tall-input-confirm-full.png` })
  })

  test('冷会话确认按钮仍完整可见', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 })
    await page.goto('/confirm-flow-clip.html?h=280&cold=1')
    await expect(page.getByTestId('clarify-cold-actions')).toBeVisible()
    await expect(page.getByTestId('clarify-hot-actions')).toHaveCount(0)
    await expect(page.getByTestId('clarify-input-row')).toHaveCount(0)
    await expect(page.getByTestId('clarify-input')).toHaveCount(0)
    await assertButtonFullyInsideHost(page)
    await page.screenshot({ path: `${SHOT_DIR}/04-cold-session-confirm-full.png` })
  })

  for (const width of [400, 240, 200]) {
    for (const lang of ['zh', 'en']) {
      test(`${width}px ${lang}: 确认动作收在输入框内，四边完整且不与附件按钮重叠`, async ({ page }) => {
        await page.setViewportSize({ width: 900, height: 800 })
        await page.goto(`/confirm-flow-clip.html?h=440&w=${width}&lang=${lang}&theme=light`)
        await assertButtonFullyInsideHost(page, lang === 'en' ? 'Confirm & continue' : '确认并流转')
        await assertIntegratedActions(page)
        await expect(page.getByTestId('clarify-confirm-flow')).toBeEnabled()
      })
    }
  }

  test('未发送草稿隐藏流转并保留内容，清空后恢复', async ({ page }) => {
    await page.setViewportSize({ width: 900, height: 800 })
    await page.goto('/confirm-flow-clip.html?h=440&w=240&theme=light')
    const input = page.getByTestId('clarify-input')
    const confirm = page.getByTestId('clarify-confirm-flow')
    await expect(confirm).toBeEnabled()
    await input.fill('需要先发送的修改意见')
    await expect(confirm).toBeHidden()
    await expect(input).toHaveValue('需要先发送的修改意见')
    await expect(page.getByTestId(/^clarify-send-(label|icon)$/)).toBeEnabled()
    await input.fill('')
    await expect(confirm).toBeEnabled()
    await expect(page.getByTestId('clarify-confirm-hint')).toHaveCount(0)
    await assertIntegratedActions(page)
  })

  for (const width of [400, 240, 200]) {
    test(`${width}px: 流转进出时发送平滑移动，正常折行且不溢出`, async ({ page }, testInfo) => {
      await page.emulateMedia({ reducedMotion: 'no-preference' })
      await page.setViewportSize({ width: 900, height: 800 })
      await page.goto(`/confirm-flow-clip.html?h=440&w=${width}&theme=light`)
      await assertIntegratedActions(page)
      const leaving = await sampleDraftTransition(page, 'a')
      await expect(page.getByTestId('clarify-confirm-flow')).toBeHidden()
      await expect(page.getByTestId(/^clarify-send-(label|icon)$/)).toBeEnabled()
      expect(leaving[0]!.kind).toBe('confirm')
      assertSmoothPrimaryMorph(leaving)
      assertStableComposer(leaving, width === 200)
      // The same button becomes 发送 as soon as a draft exists; no second primary lingers.
      expect(leaving.slice(1).every((sample) => sample.kind === 'send' && !sample.disabled)).toBe(true)

      const entering = await sampleDraftTransition(page, '')
      await expect(page.getByTestId('clarify-confirm-flow')).toBeEnabled()
      expect(entering.slice(1).every((sample) => sample.kind === 'confirm')).toBe(true)
      assertSmoothPrimaryMorph(entering)
      assertStableComposer(entering, width === 200)
      await assertIntegratedActions(page)
      await testInfo.attach('composer-transition-geometry', {
        body: JSON.stringify({ width, leaving, entering }),
        contentType: 'application/json',
      })
    })
  }

  test('快速输入清空后只恢复一个可用流转动作', async ({ page }) => {
    await page.goto('/confirm-flow-clip.html?h=440&w=240&theme=light')
    await expect(page.getByTestId('clarify-confirm-flow')).toBeEnabled()
    await page.getByTestId('clarify-input').evaluate(async (element) => {
      const input = element as HTMLTextAreaElement
      for (const value of ['a', '', 'ab', '', 'abc', '']) {
        input.value = value
        input.dispatchEvent(new Event('input', { bubbles: true }))
        await new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve())))
      }
    })
    await expect(page.getByTestId('clarify-input')).toHaveValue('')
    await expect(page.getByTestId('clarify-confirm-flow')).toHaveCount(1)
    await expect(page.getByTestId('clarify-confirm-flow')).toBeEnabled()
    await expect.poll(() => page.getByTestId('clarify-action-row').evaluate((el) =>
      el.getAnimations({ subtree: true }).filter((animation) => animation.playState === 'running').length,
    )).toBe(0)
    await assertIntegratedActions(page)
  })

  test('减少动态偏好无中间位移动画，恢复偏好后重新平滑过渡', async ({ page }) => {
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.goto('/confirm-flow-clip.html?h=440&w=240&theme=light')
    await expect(page.getByTestId('clarify-confirm-flow')).toBeEnabled()
    const reduced = await sampleDraftTransition(page, 'a', 160)
    await expect(page.getByTestId('clarify-confirm-flow')).toBeHidden()
    const start = reduced[0]!
    const end = reduced.at(-1)!
    expect(reduced.slice(1).every((sample) => {
      const atStart = Math.abs(sample.primaryX - start.primaryX) <= 0.5 && Math.abs(sample.primaryY - start.primaryY) <= 0.5
      const atEnd = Math.abs(sample.primaryX - end.primaryX) <= 0.5 && Math.abs(sample.primaryY - end.primaryY) <= 0.5
      return (atStart || atEnd) && sample.runningAnimations === 0 && sample.primaryCount === 1
    })).toBe(true)
    assertStableComposer(reduced)

    await page.emulateMedia({ reducedMotion: 'no-preference' })
    const restored = await sampleDraftTransition(page, '')
    await expect(page.getByTestId('clarify-confirm-flow')).toBeEnabled()
    assertSmoothPrimaryMorph(restored)
    assertStableComposer(restored)
    await assertIntegratedActions(page)
  })
})
