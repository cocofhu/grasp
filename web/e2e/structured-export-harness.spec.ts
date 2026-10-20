import { test, expect } from '@playwright/test'
import path from 'node:path'
import fs from 'node:fs'

const shotDir = '/tmp/structured-export-shots'
fs.mkdirSync(shotDir, { recursive: true })

test.describe('structured artifact export harness', () => {
  test('structured preview shows export buttons and downloads PNG/PDF', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 900 })
    await page.goto('/structured-export-harness.html?scenario=structured&theme=dark')
    await expect(page.getByTestId('export-harness-root')).toBeVisible({ timeout: 15_000 })
    await expect(page.getByTestId('artifact-preview-download-png')).toBeVisible()
    await expect(page.getByTestId('artifact-preview-download-pdf')).toBeVisible()
    await expect(page.getByTestId('artifact-preview-download-png')).toHaveText('下载图片')
    await expect(page.getByTestId('artifact-preview-download-pdf')).toHaveText('下载 PDF')
    await expect(page.getByTestId('artifact-preview-download-raw')).toHaveAttribute(
      'title',
      '下载原文件',
    )

    // Button order: copy → raw download → png → pdf
    const toolbar = page.getByTestId('artifact-preview-toolbar')
    const order = await toolbar.locator('[data-testid]').evaluateAll((nodes) =>
      nodes.map((n) => n.getAttribute('data-testid')),
    )
    expect(order.indexOf('artifact-preview-copy')).toBeLessThan(
      order.indexOf('artifact-preview-download-raw'),
    )
    expect(order.indexOf('artifact-preview-download-raw')).toBeLessThan(
      order.indexOf('artifact-preview-download-png'),
    )
    expect(order.indexOf('artifact-preview-download-png')).toBeLessThan(
      order.indexOf('artifact-preview-download-pdf'),
    )

    await expect(page.getByTestId('structured-artifact-export-root')).toBeVisible()
    await expect(page.getByText('结构化 JSON 产物预览导出 PNG / PDF')).toBeVisible()
    await page.screenshot({
      path: path.join(shotDir, '01-structured-inline-dark.png'),
      fullPage: true,
    })

    const pngDownload = page.waitForEvent('download', { timeout: 30_000 })
    await page.getByTestId('artifact-preview-download-png').click()
    const png = await pngDownload
    expect(png.suggestedFilename()).toBe('clarified_requirement.png')
    const pngPath = path.join(shotDir, 'exported-clarified_requirement.png')
    await png.saveAs(pngPath)
    expect(fs.statSync(pngPath).size).toBeGreaterThan(1000)

    const pdfDownload = page.waitForEvent('download', { timeout: 30_000 })
    await page.getByTestId('artifact-preview-download-pdf').click()
    const pdf = await pdfDownload
    expect(pdf.suggestedFilename()).toBe('clarified_requirement.pdf')
    const pdfPath = path.join(shotDir, 'exported-clarified_requirement.pdf')
    await pdf.saveAs(pdfPath)
    expect(fs.statSync(pdfPath).size).toBeGreaterThan(500)
    const pdfHead = fs.readFileSync(pdfPath).subarray(0, 5).toString('utf8')
    expect(pdfHead).toBe('%PDF-')

    // Zoom footer
    await page.getByTitle('放大查看').click()
    await expect(page.getByTestId('artifact-preview-zoom-download-png')).toBeVisible()
    await expect(page.getByTestId('artifact-preview-zoom-download-pdf')).toBeVisible()
    await expect(page.getByTestId('artifact-preview-zoom-download-raw')).toHaveAttribute(
      'title',
      '下载原文件',
    )
    await page.screenshot({
      path: path.join(shotDir, '02-structured-zoom-dark.png'),
      fullPage: true,
    })
  })

  test('plain JSON does not show style export buttons', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 })
    await page.goto('/structured-export-harness.html?scenario=plain-json&theme=dark')
    await expect(page.getByTestId('export-harness-root')).toBeVisible({ timeout: 15_000 })
    await expect(page.getByTestId('artifact-preview-download-raw')).toBeVisible()
    await expect(page.getByTestId('artifact-preview-download-png')).toHaveCount(0)
    await expect(page.getByTestId('artifact-preview-download-pdf')).toHaveCount(0)
    await page.screenshot({
      path: path.join(shotDir, '03-plain-json-no-export.png'),
      fullPage: true,
    })
  })

  test('raw JSON code board follows dark and light tokens (g3.2)', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 900 })

    await page.goto('/structured-export-harness.html?scenario=structured&theme=dark')
    await expect(page.getByTestId('export-harness-root')).toBeVisible({ timeout: 15_000 })
    await page.getByTestId('artifact-preview-mode-raw').click()
    const darkView = page.getByTestId('artifact-preview-raw-json')
    await expect(darkView).toBeVisible()
    await expect(darkView).toHaveCSS('background-color', 'rgb(30, 30, 30)')
    await expect(darkView.locator('.tok-key').first()).toHaveCSS('color', 'rgb(156, 220, 254)')
    await page.screenshot({
      path: path.join(shotDir, '05-raw-json-dark.png'),
      fullPage: true,
    })

    await page.getByTestId('artifact-preview-zoom').click()
    const darkModal = page.getByTestId('artifact-preview-zoom-raw-json')
    await expect(darkModal).toBeVisible()
    await expect(darkModal).toHaveCSS('background-color', 'rgb(30, 30, 30)')
    await page.keyboard.press('Escape')

    await page.goto('/structured-export-harness.html?scenario=structured&theme=light')
    await expect(page.getByTestId('export-harness-root')).toBeVisible({ timeout: 15_000 })
    await expect(page.locator('html')).toHaveClass(/light/)
    await page.getByTestId('artifact-preview-mode-raw').click()
    const lightView = page.getByTestId('artifact-preview-raw-json')
    await expect(lightView).toBeVisible()
    const lightBg = await lightView.evaluate((el) => getComputedStyle(el).backgroundColor)
    expect(lightBg).not.toBe('rgb(30, 30, 30)')
    expect(lightBg).toMatch(/rgb\(\s*25[0-5],\s*25[0-5],\s*25[0-5]\s*\)/)
    await expect(lightView.locator('.tok-key').first()).toHaveCSS('color', 'rgb(4, 81, 165)')
    await page.screenshot({
      path: path.join(shotDir, '06-raw-json-light.png'),
      fullPage: true,
    })
  })

  test('light theme export still produces readable PNG', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 900 })
    await page.goto('/structured-export-harness.html?scenario=structured&theme=light')
    await expect(page.getByTestId('artifact-preview-download-png')).toBeVisible({ timeout: 15_000 })
    await page.screenshot({
      path: path.join(shotDir, '04-structured-inline-light.png'),
      fullPage: true,
    })
    const pngDownload = page.waitForEvent('download', { timeout: 30_000 })
    await page.getByTestId('artifact-preview-download-png').click()
    const png = await pngDownload
    expect(png.suggestedFilename()).toBe('clarified_requirement.png')
    const pngPath = path.join(shotDir, 'exported-clarified_requirement-light.png')
    await png.saveAs(pngPath)
    expect(fs.statSync(pngPath).size).toBeGreaterThan(1000)
  })
})
