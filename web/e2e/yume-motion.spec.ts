import { expect, test, type Locator, type Page } from '@playwright/test'

async function mockLists(page: Page) {
  await page.route('**/api/**', async (route) => {
    const path = new URL(route.request().url()).pathname
    if (!path.startsWith('/api/')) {
      await route.continue()
      return
    }
    const list = /\/(?:workflows|projects|agents)$/.test(path)
    await route.fulfill({ json: list ? [] : { items: [], total: 0 } })
  })
}

async function attachmentSamples(character: Locator, times: number[]) {
  return character.evaluate((root, times) => {
    const tail = root.querySelector<SVGPathElement>('.c-tail')!
    const body = root.querySelector<SVGPathElement>('path[fill="#FFFFFF"]')!
    const animations = root.getAnimations({ subtree: true })
    animations.forEach((animation) => animation.pause())
    return times.map((time) => {
      animations.forEach((animation) => { animation.currentTime = time })
      const start = tail.getPointAtLength(0).matrixTransform(tail.getScreenCTM()!)
      const point = start.matrixTransform(body.getScreenCTM()!.inverse())
      const box = root.getBoundingClientRect()
      return { x: point.x, y: point.y, attached: body.isPointInFill(point), width: box.width, height: box.height }
    })
  }, times)
}

test('yume tail stays attached throughout idle, greeting, press and jump', async ({ page }) => {
  await mockLists(page)
  await page.goto('/board.html')
  const character = page.getByTestId('home-yume')
  await expect(character).toBeVisible({ timeout: 30_000 })
  await page.mouse.move(0, 0)
  const times = Array.from({ length: 25 }, (_, index) => index * 150)
  const samples = await attachmentSamples(character, times)
  await character.hover()
  samples.push(...await attachmentSamples(character, times))
  await character.dispatchEvent('pointerdown', { pointerType: 'mouse', button: 0 })
  await expect(character).toHaveClass(/pressed/)
  samples.push(...await attachmentSamples(character, [0, 40, 80]))
  await character.dispatchEvent('pointerup', { pointerType: 'mouse', button: 0 })
  await expect(character).toHaveClass(/jump/)
  samples.push(...await attachmentSamples(character, [0, 70, 150, 280]))

  const first = samples[0]!
  for (const sample of samples) {
    expect(sample.attached, JSON.stringify(sample)).toBe(true)
    expect(Math.hypot(sample.x - first.x, sample.y - first.y)).toBeLessThan(.01)
    expect(sample.width).toBe(240)
    expect(sample.height).toBe(200)
  }
})

test('reduced motion stops yume poses without moving its attachment or layout', async ({ page }) => {
  await mockLists(page)
  await page.goto('/board.html')
  const character = page.getByTestId('home-yume')
  await expect(character).toBeVisible({ timeout: 30_000 })
  await character.hover()
  await character.dispatchEvent('pointerdown', { pointerType: 'mouse', button: 0 })
  await character.dispatchEvent('pointerup', { pointerType: 'mouse', button: 0 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await expect(character).toHaveClass(/home-yume--static/)
  await expect(character).not.toHaveClass(/\b(?:pressed|jump)\b/)
  const samples = await attachmentSamples(character, [0, 650, 1300])
  expect(samples.every((sample) => sample.attached && sample.width === 240 && sample.height === 200)).toBe(true)
  expect(await character.evaluate((element) => element.getAnimations({ subtree: true }).length)).toBe(0)
  await character.press('Enter')
  await expect(character).not.toHaveClass(/\bjump\b/)
  await page.mouse.move(0, 0)
  expect(await character.evaluate((element) => element.getAnimations({ subtree: true }).length)).toBe(0)
  await page.emulateMedia({ reducedMotion: 'no-preference' })
  await expect(character).not.toHaveClass(/home-yume--static/)
  await character.press('Space')
  await expect(character).toHaveClass(/\bjump\b/)
})

test('sidebar greeting animates the character while its wordmark stays aligned', async ({ page }) => {
  await mockLists(page)
  await page.goto('/hover-ink-scroll-shift.html?start=/dashboard')
  const lockup = page.getByTestId('sidebar-brand-lockup')
  const character = lockup.getByTestId('brand-character-mark')
  await expect(character).toBeVisible({ timeout: 30_000 })
  await page.evaluate(() => document.fonts.ready)
  const original = await lockup.boundingBox()
  await lockup.hover()
  await expect.poll(() => character.locator('.brand-character__response').evaluate((element) => getComputedStyle(element).transform)).not.toBe('none')
  expect(await lockup.evaluate((element) => getComputedStyle(element).transform)).toBe('none')
  expect(await lockup.boundingBox()).toEqual(original)
  expect(await character.evaluate((element) => element.getAnimations({ subtree: true }).length)).toBeGreaterThan(0)
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await expect(character).toHaveClass(/brand-character--static/)
  expect(await character.evaluate((element) => element.getAnimations({ subtree: true }).length)).toBe(0)
  expect(await character.boundingBox()).toMatchObject({ width: 28, height: 28 })
})
