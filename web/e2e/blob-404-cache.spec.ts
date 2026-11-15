import { test, expect } from '@playwright/test'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import fs from 'node:fs'

const shotDir = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  '../../../.tmp-blob-404-shots',
)

const ORPHAN_A = 'e54381fb9ce8471dbe0765d99fc0239f'
const ORPHAN_B = '5b32f70529a64bdebafade19ca497a35'
const ORPHAN_C = '6f70eb9a67f2432983d16bc26a1bb420'
const ORPHANS = [ORPHAN_A, ORPHAN_B, ORPHAN_C]

const TINY_PNG = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==',
  'base64',
)

async function openHarness(page: import('@playwright/test').Page) {
  const blobGets: string[] = []
  const restored = new Set<string>()

  await page.route('**/api/blobs/**', async (route) => {
    const url = route.request().url()
    const m = url.match(/\/api\/blobs\/([^/?#]+)/)
    const id = m?.[1] || 'unknown'
    blobGets.push(id)
    if (restored.has(id)) {
      await route.fulfill({
        status: 200,
        contentType: 'image/png',
        body: TINY_PNG,
      })
      return
    }
    await route.fulfill({
      status: 404,
      contentType: 'application/json',
      body: JSON.stringify({ error: 'blob not found' }),
    })
  })

  await page.setViewportSize({ width: 1280, height: 900 })
  await page.goto('/blob-404-cache.html')
  await expect(page.getByTestId('blob-404-root')).toBeVisible({ timeout: 20_000 })
  await expect
    .poll(async () => page.getByTestId('composite-image-failed').count(), { timeout: 15_000 })
    .toBeGreaterThanOrEqual(6)

  return { blobGets, restored }
}

test.describe('blob 404 negative cache (orphan /api/blobs)', () => {
  test.beforeAll(() => {
    fs.mkdirSync(shotDir, { recursive: true })
  })

  test('g1.2 dual strip+chat: auto GET ≤1 per missing id', async ({ page }) => {
    const { blobGets } = await openHarness(page)
    await page.screenshot({
      path: path.join(shotDir, '01-after-first-auto-load.png'),
      animations: 'disabled',
    })
    const afterFirst = [...blobGets]
    for (const id of ORPHANS) {
      const n = afterFirst.filter((x) => x === id).length
      expect(n, `auto GET for ${id} should be ≤1, got ${n}; all=${JSON.stringify(afterFirst)}`).toBeLessThanOrEqual(1)
    }
    expect(afterFirst.length).toBe(3)
  })

  test('g2.* poll replace does not remount img / no extra GET', async ({ page }) => {
    const { blobGets } = await openHarness(page)
    const baseline = blobGets.length
    for (let i = 0; i < 8; i++) {
      await page.getByTestId('btn-poll').click()
      await page.waitForTimeout(100)
    }
    expect(blobGets.length, 'poll must not trigger new blob GETs').toBe(baseline)
    await expect(page.getByTestId('panel-vars').getByTestId('composite-image-failed')).toHaveCount(3)
    await expect(page.locator('#app img[src*="/api/blobs/"]')).toHaveCount(0)
    await page.screenshot({
      path: path.join(shotDir, '02-after-poll-still-placeholder.png'),
      animations: 'disabled',
    })
  })

  test('g3.2 knownMissing preview: no requesting img, no extra GET', async ({ page }) => {
    const { blobGets } = await openHarness(page)
    const baseline = blobGets.length
    await page.getByTestId('btn-open-preview-a').click()
    await expect(page.getByTestId('blob-preview-failed')).toBeVisible({ timeout: 5_000 })
    await expect(page.getByTestId('blob-preview-img')).toHaveCount(0)
    expect(blobGets.length).toBe(baseline)
    await page.screenshot({
      path: path.join(shotDir, '03-preview-known-missing.png'),
      animations: 'disabled',
    })
  })

  test('g3.1/g3.3 chat retry fail +1 then silent; restore syncs strips', async ({ page }) => {
    const { blobGets, restored } = await openHarness(page)
    const aBefore = blobGets.filter((x) => x === ORPHAN_A).length

    await page.getByTestId('chat-thumb-a-retry').click()
    await expect
      .poll(() => blobGets.filter((x) => x === ORPHAN_A).length, { timeout: 5_000 })
      .toBe(aBefore + 1)
    await expect(page.getByTestId('chat-thumb-a-retry')).toBeVisible()

    for (let i = 0; i < 3; i++) {
      await page.getByTestId('btn-poll').click()
      await page.waitForTimeout(80)
    }
    expect(blobGets.filter((x) => x === ORPHAN_A).length).toBe(aBefore + 1)

    await page.screenshot({
      path: path.join(shotDir, '04-retry-still-404.png'),
      animations: 'disabled',
    })

    restored.add(ORPHAN_A)
    await page.getByTestId('chat-thumb-a-retry').click()
    await expect(page.getByTestId('chat-thumb-a')).toContainText('点击放大', { timeout: 5_000 })
    await expect
      .poll(async () => page.getByTestId('panel-vars').getByTestId('composite-image-ok').count(), {
        timeout: 5_000,
      })
      .toBe(1)
    await expect(page.getByTestId('panel-out').getByTestId('composite-image-ok')).toHaveCount(1)
    // The retry GET carries ?_r=, so the strips may share one more GET of the plain URL.
    const afterRestore = blobGets.filter((x) => x === ORPHAN_A).length
    expect(afterRestore).toBeGreaterThanOrEqual(aBefore + 2)
    expect(afterRestore).toBeLessThanOrEqual(aBefore + 3)
    for (let i = 0; i < 3; i++) {
      await page.getByTestId('btn-poll').click()
      await page.waitForTimeout(80)
    }
    expect(blobGets.filter((x) => x === ORPHAN_A).length).toBe(afterRestore)

    await page.screenshot({
      path: path.join(shotDir, '05-retry-success-synced.png'),
      animations: 'disabled',
    })
  })
})
