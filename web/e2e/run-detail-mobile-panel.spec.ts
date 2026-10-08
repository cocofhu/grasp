import { test, expect } from '@playwright/test'

const longPageStub = `<!doctype html><html><body>${'<p>x</p>'.repeat(20)}</body></html>`

async function gotoPanel(
  page: import('@playwright/test').Page,
  scenario: 'completed' | 'gate' | 'review' = 'completed',
) {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto(`/run-detail-mobile-panel.html?scenario=${scenario}`)
  await expect(page.getByTestId('run-detail-root')).toBeVisible({ timeout: 15_000 })
}

function boxesIntersect(
  a: { x: number; y: number; width: number; height: number },
  b: { x: number; y: number; width: number; height: number },
) {
  return !(
    a.x + a.width <= b.x ||
    b.x + b.width <= a.x ||
    a.y + a.height <= b.y ||
    b.y + b.height <= a.y
  )
}

test.describe('Run 详情移动端单面板生产路径 (390×844)', () => {
  // plan_coverage: g2.3 — ReviewShell mobile entries (gate/review/mobile-fill) share drawer behavior
  test('completed：单面板 + 末项可视 + 汇总与条目无几何相交 (g1/g2)', async ({ page }) => {
    await gotoPanel(page, 'completed')

    await expect(page.getByTestId('mobile-main-panel-tabs')).toBeVisible()
    await expect(page.getByTestId('run-timeline-pane')).toBeVisible()
    await expect(page.getByTestId('run-detail-right-panel')).toHaveCount(0)

    const selected = page.locator('[data-testid="timeline-item"][data-selected="true"]')
    await expect(selected).toBeVisible()
    await expect(selected).toContainText('输出')

    const selBox = await selected.boundingBox()
    expect(selBox).toBeTruthy()
    // Selected last item must sit inside the 844 viewport (scrollIntoView).
    expect(selBox!.y).toBeGreaterThanOrEqual(0)
    expect(selBox!.y + selBox!.height).toBeLessThanOrEqual(844 + 1)

    // Click 开始 label (avoid var chips which stopPropagation) → detail panel.
    await page
      .getByTestId('timeline-item')
      .filter({ hasText: '开始' })
      .getByTestId('timeline-node-label')
      .click()
    await expect(page.getByTestId('run-detail-right-panel')).toBeVisible()
    await expect(page.getByTestId('run-timeline-pane')).toHaveCount(0)
    await expect(page.getByTestId('mobile-back-to-timeline')).toBeVisible()

    await page.getByTestId('mobile-back-to-timeline').click()
    await expect(page.getByTestId('run-timeline-pane')).toBeVisible()
    await expect(page.getByTestId('run-detail-right-panel')).toHaveCount(0)
    await expect(page.locator('[data-testid="timeline-item"][data-selected="true"]')).toBeVisible()

    const footer = page.getByTestId('timeline-footer')
    await footer.scrollIntoViewIfNeeded()
    await expect(footer).toBeVisible()
    await expect(footer).toContainText('Run 汇总')
    await expect(footer.getByTestId('timeline-total-tokens')).toBeVisible()
    await expect(footer.getByTestId('timeline-token-rate')).toBeVisible()
    await expect(footer.getByTestId('timeline-wall-clock')).toBeVisible()

    // Footer follows list in document flow: last item ends above footer (no sticky cut).
    const items = page.getByTestId('timeline-item')
    const count = await items.count()
    expect(count).toBeGreaterThanOrEqual(4)
    const footerBox = await footer.boundingBox()
    const lastBox = await items.last().boundingBox()
    expect(footerBox && lastBox).toBeTruthy()
    expect(lastBox!.y + lastBox!.height).toBeLessThanOrEqual(footerBox!.y + 1)
    for (let i = 0; i < count; i++) {
      const box = await items.nth(i).boundingBox()
      if (!box) continue
      if (box.y + box.height < 0 || box.y > 844) continue
      expect(boxesIntersect(box, footerBox!)).toBe(false)
    }
  })

  test('completed：点视觉网页默认产物 HtmlPreview (g3.1)', async ({ page }) => {
    await gotoPanel(page, 'completed')
    const visual = page.getByTestId('timeline-item').filter({ hasText: '视觉网页' })
    await expect(visual).toBeVisible()
    await visual.click()
    await expect(page.getByTestId('run-detail-right-panel')).toBeVisible()
    await expect(page.getByTestId('tab-product')).toBeVisible()
    await expect(page.getByTestId('product-preview')).toBeVisible()
    await expect(page.getByTestId('html-preview')).toContainText('视觉网页产物')
    await expect(page.getByTestId('output-overview')).toHaveCount(0)

    await page.getByTestId('tab-output').click()
    await expect(page.getByTestId('output-overview')).toBeVisible()
  })

  test('waiting_human 门禁：确认并流转吸底常显 (g3.2)', async ({ page }) => {
    // Review semantics: sticky 确认并流转; no 通过/打回 dual buttons.
    await page.route('**/api/**', async (route) => {
    // Skip Vite module URLs like /@fs/.../src/lib/api/api.ts (pathname is not /api/...)
    if (!new URL(route.request().url()).pathname.startsWith('/api/')) {
      await route.continue()
      return
    }
      const url = new URL(route.request().url())
      if (url.pathname.includes('/preview-issues')) {
        await route.fulfill({ json: { issues: [] } })
        return
      }
      if (url.pathname.includes('/primary-artifacts') || url.pathname.includes('/gate/')) {
        await route.fulfill({ status: 400, json: { error: 'offline' } })
        return
      }
      if (url.pathname.includes('/artifacts/')) {
        await route.fulfill({
          json: {
            content: longPageStub,
            etag: 'e1',
            updatedAt: '2026-07-27T00:00:00Z',
            sizeBytes: 64,
          },
        })
        return
      }
      await route.fulfill({ status: 404, json: { error: 'not mocked' } })
    })

    await gotoPanel(page, 'gate')
    await expect(page.getByTestId('run-detail-right-panel')).toBeVisible()
    await expect(page.getByTestId('mobile-fill-remaining')).toBeVisible({ timeout: 10_000 })

    const sticky = page.getByTestId('mobile-fill-sticky-actions')
    await expect(sticky).toBeVisible()
    const pass = page.getByTestId('review-composer-pass')
    await expect(pass).toBeVisible()
    await expect(page.getByRole('button', { name: '确认并流转' })).toBeVisible()
    await expect(page.getByRole('button', { name: '通过并流转' })).toHaveCount(0)
    await expect(page.getByRole('button', { name: '打回修改' })).toHaveCount(0)

    const passBox = await pass.boundingBox()
    expect(passBox).toBeTruthy()
    expect(passBox!.y).toBeGreaterThanOrEqual(0)
    expect(passBox!.y + passBox!.height).toBeLessThanOrEqual(844 + 1)

    // Scroll preview — sticky confirm remains in viewport.
    const preview = page.getByTestId('mobile-fill-preview')
    if (await preview.count()) {
      await preview.evaluate((el) => {
        el.scrollTop = el.scrollHeight
      })
    }
    await expect(pass).toBeVisible()
    const after = await pass.boundingBox()
    expect(after!.y + after!.height).toBeLessThanOrEqual(844 + 1)

    // Round-trip timeline → detail keeps sticky decisions.
    await page.getByTestId('mobile-panel-timeline').click()
    await expect(page.getByTestId('run-timeline-pane')).toBeVisible()
    await page.getByTestId('mobile-panel-detail').click()
    await expect(page.getByTestId('mobile-fill-sticky-actions')).toBeVisible()
    await expect(page.getByTestId('review-composer-pass')).toBeVisible()
  })

  test('waiting_human 复审：预览可滚 + 发送/确认并流转吸底 (g3.3)', async ({ page }) => {
    await gotoPanel(page, 'review')
    await expect(page.getByTestId('run-detail-right-panel')).toBeVisible()
    await expect(page.getByTestId('review-shell')).toBeVisible()
    await expect(page.getByTestId('review-product-preview')).toBeVisible()

    const pass = page.getByTestId('review-composer-pass')
    const send = page.getByTestId('review-composer-send')
    await expect(pass).toBeVisible()
    await expect(send).toBeVisible()
    await expect(pass).toContainText('确认并流转')
    await expect(send).toHaveAttribute('aria-label', '发送')
    await expect(send).not.toContainText('发送')
    await expect(page.getByRole('button', { name: '通过并流转' })).toHaveCount(0)
    await expect(page.getByRole('button', { name: '打回修改' })).toHaveCount(0)
    const passBox = await pass.boundingBox()
    expect(passBox).toBeTruthy()
    expect(passBox!.y + passBox!.height).toBeLessThanOrEqual(844 + 1)

    await page.getByTestId('review-product-preview').evaluate((el) => {
      el.scrollTop = el.scrollHeight
    })
    await expect(pass).toBeVisible()
    const after = await pass.boundingBox()
    expect(after!.y + after!.height).toBeLessThanOrEqual(844 + 1)
  })
})
