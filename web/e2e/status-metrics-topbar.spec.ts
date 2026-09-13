import { test, expect } from '@playwright/test'
import fs from 'node:fs'
import path from 'node:path'

const OUT = '/tmp/status-metrics-shots'
fs.mkdirSync(OUT, { recursive: true })

test.describe('StatusMetrics topbar E2E', () => {
  test('desktop: four metrics left of lang, today tokens, tip, no TOK labels', async ({ page }, testInfo) => {
    await page.setViewportSize({ width: 1280, height: 720 })
    await page.goto('/status-metrics-topbar.html')
    const metrics = page.getByTestId('status-metrics')
    await expect(metrics).toBeVisible({ timeout: 15_000 })

    await expect(page.getByTestId('status-metrics-tokens')).toContainText(/1\.24M/i)
    await expect(page.getByTestId('status-metrics-today')).toContainText(/4\.8K/)
    await expect(page.getByTestId('status-metrics-today')).not.toContainText(/\/5m/)
    await expect(page.getByTestId('status-metrics-rate')).toHaveCount(0)
    await expect(page.getByTestId('status-metrics-peak')).toHaveCount(0)
    await expect(page.getByTestId('status-metrics-running')).toContainText('3')
    await expect(page.getByTestId('status-metrics-queued')).toContainText('5')
    // Visible values are icon+compact numbers (no TOK/5M/PEAK label chips).
    const visibleVals = await page.locator('[data-testid^="status-metrics-"] .sm-val').allTextContents()
    expect(visibleVals.join(' ')).not.toMatch(/\bTOK\b|\b5M\b|\bPEAK\b/)
    await expect(page.getByTestId('status-metrics-compact')).toHaveCount(0)

    // StatusMetrics sits before LangSelect inside ShellChromeControls (bar layout).
    const order = await page.evaluate(() => {
      const root = document.querySelector('[data-testid="shell-chrome-controls"]')
      if (!root) return []
      return Array.from(root.children).map((el) => {
        if ((el as HTMLElement).dataset?.testid === 'status-metrics') return 'status-metrics'
        if (el.querySelector?.('[data-testid="status-metrics"]')) return 'status-metrics'
        if (el.tagName === 'SELECT' || el.querySelector?.('select') || el.textContent?.includes('中文'))
          return 'lang'
        return el.tagName.toLowerCase()
      })
    })
    const smIdx = order.indexOf('status-metrics')
    const langIdx = order.findIndex((x) => x === 'lang')
    expect(smIdx).toBeGreaterThanOrEqual(0)
    expect(langIdx).toBeGreaterThan(smIdx)

    const tokensTip = page.getByTestId('status-metrics-tokens').locator('.sm-tip')
    await page.getByTestId('status-metrics-tokens').hover()
    await expect(tokensTip).toBeVisible()
    await expect(tokensTip).toContainText('累计')
    await expect(tokensTip).toContainText('1,240,582')
    await expect(tokensTip).not.toContainText('今日 Token')
    await expect(tokensTip).not.toContainText('执行中')
    await expect(tokensTip).not.toContainText('完整值')
    await expect(tokensTip).not.toContainText('/5m')

    const todayTip = page.getByTestId('status-metrics-today').locator('.sm-tip')
    await page.getByTestId('status-metrics-today').hover()
    await expect(todayTip).toBeVisible()
    await expect(todayTip).toContainText('今日 Token')
    await expect(todayTip).toContainText('4,812')
    await expect(todayTip).not.toContainText('累计 Token')
    await expect(todayTip).not.toContainText('/5m')

    await page.getByTestId('status-metrics-running').click()
    await expect(page).toHaveURL(/#\/stats/)
    await expect(page.getByTestId('token-analytics-page')).toBeVisible()

    const shot = path.join(testInfo.outputDir, 'desktop-five-metrics.png')
    await page.locator('header').screenshot({ path: shot })
    await page.screenshot({
      path: path.join(OUT, '01-desktop-status-metrics.png'),
      fullPage: false,
    })
  })

  test('narrow: two-zone strip; Token tip vs run tip partitioned (plan g1.2)', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await page.goto('/status-metrics-topbar.html')
    await expect(page.getByTestId('status-metrics-compact')).toBeVisible({ timeout: 15_000 })
    await expect(page.getByTestId('status-metrics-tokens')).toHaveCount(0)
    const compact = page.getByTestId('status-metrics-compact')
    const tokenZone = page.getByTestId('status-metrics-compact-token')
    const runZone = page.getByTestId('status-metrics-compact-run')
    await expect(compact).toContainText(/1\.24M/i)
    await expect(compact).toContainText('3')
    await expect(compact).toContainText('5')

    await tokenZone.hover()
    const tokenTip = page.getByTestId('status-metrics-compact-token-tip')
    await expect(tokenTip).toBeVisible()
    await expect(tokenTip).toContainText(/累计 Token/)
    await expect(tokenTip).toContainText('1,240,582')
    await expect(tokenTip).toContainText(/今日 Token/)
    await expect(tokenTip).toContainText('4,812')
    await expect(tokenTip).not.toContainText('执行中')
    await expect(tokenTip).not.toContainText('排队')
    await expect(tokenTip).not.toContainText('/5m')
    await expect(tokenTip).not.toContainText('完整值')

    await runZone.hover()
    const runTip = page.getByTestId('status-metrics-compact-run-tip')
    await expect(runTip).toBeVisible()
    await expect(runTip).toContainText(/执行中/)
    await expect(runTip).toContainText('3')
    await expect(runTip).toContainText(/排队/)
    await expect(runTip).toContainText('5')
    await expect(runTip).not.toContainText('累计 Token')
    await expect(runTip).not.toContainText('今日 Token')

    await runZone.click()
    await expect(page).toHaveURL(/#\/stats/)
    await expect(page.getByTestId('token-analytics-page')).toBeVisible()
    await expect(runTip).toBeHidden()

    await page.screenshot({
      path: path.join(OUT, '02-narrow-status-metrics.png'),
      fullPage: false,
    })
  })

  test('null tokens show em-dash; running/queued true zero', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 720 })
    await page.goto('/status-metrics-topbar.html?scene=null')
    await expect(page.getByTestId('status-metrics-tokens')).toContainText('—', { timeout: 15_000 })
    await expect(page.getByTestId('status-metrics-today')).toContainText('—')
    await expect(page.getByTestId('status-metrics-running')).toContainText('0')
    await expect(page.getByTestId('status-metrics-queued')).toContainText('0')

    await page.screenshot({
      path: path.join(OUT, '03-null-emdash.png'),
      fullPage: false,
    })
  })

  test('metrics click navigates to stats (plan g2.2)', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 720 })
    await page.goto('/status-metrics-topbar.html')
    await expect(page.getByTestId('status-metrics')).toBeVisible({ timeout: 15_000 })
    await page.getByTestId('status-metrics-today').click()
    await expect(page).toHaveURL(/#\/stats/)
    await expect(page.getByTestId('token-analytics-page')).toBeVisible()
    await page.getByTestId('status-metrics-tokens').click()
    await expect(page).toHaveURL(/#\/stats/)
    await expect(page.getByTestId('token-analytics-page')).toBeVisible()
  })
})
