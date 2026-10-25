import { expect, test, type Page } from '@playwright/test'

test.use({ baseURL: 'http://127.0.0.1:5174', viewport: { width: 1280, height: 800 } })

interface ConfirmLeaveWindow extends Window {
  __confirmLeave?: {
    visualCloseCount: number
    atSignal: { dialogConnected: boolean } | null
    decision: { type: string } | null
    isHeld: () => boolean
  }
}

async function readLeave(page: Page) {
  return page.evaluate(() => {
    const probe = (window as ConfirmLeaveWindow).__confirmLeave
    const dialog = document.querySelector('[role="dialog"]')
    return {
      closes: probe?.visualCloseCount ?? 0,
      held: probe?.isHeld() ?? false,
      dialogConnected: !!dialog && (dialog as HTMLElement).isConnected,
      at: probe?.atSignal ?? null,
      decision: probe?.decision?.type ?? null,
    }
  })
}

async function openDone(page: Page) {
  await page.goto('/confirm-flow-leave.html')
  await page.getByTestId('confirm-flow-dialog-accept').click()
  await expect(page.getByTestId('confirm-flow-dialog-done')).toBeVisible()
  await expect(page.getByTestId('confirm-flow-dialog-done')).toContainText('已流转')
}

/** While the overlay is fading, the sidebar must not be released yet. */
async function assertHeldThroughFade(page: Page) {
  const mid = await page.waitForFunction(() => {
    const overlay = document.querySelector('.modal-leave-active')
    const probe = (window as ConfirmLeaveWindow).__confirmLeave
    const dialog = document.querySelector('[role="dialog"]')
    if (!overlay || !probe?.isHeld() || probe.visualCloseCount !== 0) return null
    if (!dialog || !(dialog as HTMLElement).isConnected) return null
    return { closes: probe.visualCloseCount, held: true, dialogConnected: true }
  }).then((handle) => handle.jsonValue())
  expect(mid).toEqual({ closes: 0, held: true, dialogConnected: true })
}

/** Flight or placement starts on visual-close, which is after the overlay is gone. */
async function assertReleasedAfterLeave(page: Page, decision: 'fly-in' | 'place') {
  await page.waitForFunction(() => ((window as ConfirmLeaveWindow).__confirmLeave?.visualCloseCount ?? 0) === 1)
  const at = await readLeave(page)
  expect(at.at).toEqual({ dialogConnected: false })
  expect(at.decision).toBe(decision)
  expect(at.held).toBe(false)
  expect(at.dialogConnected).toBe(false)
  // beginFlight writes the transform on the second frame after the signal.
  await page.evaluate(() => new Promise<void>((resolve) => {
    requestAnimationFrame(() => requestAnimationFrame(() => resolve()))
  }))
  await expect(page.locator('[role="dialog"]')).toHaveCount(0)
  expect((await readLeave(page)).closes).toBe(1)
}

test.describe('确认窗口离场结束后才放行侧栏', () => {
  test('完成态结束的淡出期间不飞，窗口离开后才飞入', async ({ page }) => {
    await openDone(page)
    const duringHold = await readLeave(page)
    expect(duringHold.closes).toBe(0)
    expect(duringHold.held).toBe(true)
    expect(duringHold.dialogConnected).toBe(true)
    await assertHeldThroughFade(page)
    await assertReleasedAfterLeave(page, 'fly-in')
  })

  test('减少动效也等淡出结束再直接落位', async ({ page }) => {
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await openDone(page)
    await assertHeldThroughFade(page)
    await assertReleasedAfterLeave(page, 'place')
  })

  test('Esc、关闭按钮和遮罩都等淡出结束再放行', async ({ page }) => {
    for (const dismiss of ['escape', 'close', 'backdrop'] as const) {
      await openDone(page)
      const started = Date.now()
      if (dismiss === 'escape') {
        await page.keyboard.press('Escape')
      } else if (dismiss === 'close') {
        await page.locator('[role="dialog"] button').first().click()
      } else {
        // The dialog sits above the backdrop. A corner click is the dimmed page, not the panel.
        await page.locator('.fixed.inset-0 > .absolute').click({ position: { x: 4, y: 4 } })
      }
      await assertHeldThroughFade(page)
      await assertReleasedAfterLeave(page, 'fly-in')
      const elapsed = Date.now() - started
      expect(elapsed, dismiss).toBeGreaterThan(80)
      expect(elapsed, dismiss).toBeLessThan(1200)
    }
  })
})
