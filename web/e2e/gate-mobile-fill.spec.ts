import { test, expect } from '@playwright/test'

async function mockApi(
  page: import('@playwright/test').Page,
  issues: Array<Record<string, unknown>> = [],
) {
  await page.route('**/api/**', async (route) => {
    // Skip Vite module URLs like /@fs/.../src/lib/api/api.ts (pathname is not /api/...)
    if (!new URL(route.request().url()).pathname.startsWith('/api/')) {
      await route.continue()
      return
    }
    const url = new URL(route.request().url())
    if (url.pathname.includes('/preview-issues')) {
      await route.fulfill({ json: { issues } })
      return
    }
    if (url.pathname.includes('/primary-artifacts')) {
      await route.fulfill({
        json: { items: [{ name: 'page.html', kind: 'html', readonly: false, nodeId: 'visual', outputKey: 'page' }] },
      })
      return
    }
    if (url.pathname.includes('/gate/')) {
      await route.fulfill({
        status: 400,
        json: { error: 'offline' },
      })
      return
    }
    if (url.pathname.includes('/artifacts/')) {
      await route.fulfill({
        json: {
          content: JSON.stringify({ summary: '上游需求', goals: ['g1'] }),
          etag: 'e1',
          updatedAt: '2026-07-18T00:00:00Z',
          sizeBytes: 32,
        },
      })
      return
    }
    await route.fulfill({ status: 404, json: { error: 'not mocked' } })
  })
}

// plan_coverage: g2.1 default mid-split; g2.3 e2e full-top/full-bottom + drag-back.
test.describe('Run 详情移动端 visual 定高预览', () => {
  // plan_coverage: g2.1 — adaptive drawer mid-split; preview + chat both visible
  test('390×844：n_open=0 确认并流转 + 取点，预览占满 stage', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await mockApi(page, [])

    await page.goto('/gate-mobile-fill.html')
    await expect(page.getByTestId('gate-mobile-fill-root')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByTestId('mobile-fill-remaining')).toBeVisible()
    await expect(page.getByTestId('review-shell')).toBeVisible()
    await expect(page.getByTestId('review-shell-drawer-handle')).toBeVisible()

    const preview = page.getByTestId('mobile-fill-preview')
    const drawer = page.getByTestId('review-shell-sidebar')
    const feedback = page.getByTestId('mobile-fill-feedback')
    await expect(preview).toBeVisible()
    await expect(drawer).toBeVisible()
    await expect(feedback).toBeVisible()

    const [previewBox, drawerBox, rootBox, feedbackBox] = await Promise.all([
      preview.boundingBox(),
      drawer.boundingBox(),
      page.getByTestId('gate-mobile-fill-panel').boundingBox(),
      feedback.boundingBox(),
    ])
    expect(previewBox && drawerBox && rootBox && feedbackBox).toBeTruthy()

    // Stage (preview) sits above the ReviewShell drawer; default is mid-split (not extremes).
    expect(previewBox!.y).toBeLessThan(drawerBox!.y)
    expect(previewBox!.height).toBeGreaterThan(44)
    expect(drawerBox!.height).toBeGreaterThan(44)
    expect(drawerBox!.height).toBeLessThan(340)
    expect(drawerBox!.y + drawerBox!.height).toBeLessThanOrEqual(rootBox!.y + rootBox!.height + 1)

    // Dragging the handle upward grows the drawer.
    const handle = page.getByTestId('review-shell-drawer-handle')
    const handleBox = await handle.boundingBox()
    expect(handleBox).toBeTruthy()
    const initialDrawerHeight = drawerBox!.height
    await page.mouse.move(handleBox!.x + handleBox!.width / 2, handleBox!.y + handleBox!.height / 2)
    await page.mouse.down()
    await page.mouse.move(handleBox!.x + handleBox!.width / 2, handleBox!.y - 80, { steps: 6 })
    await page.mouse.up()
    const draggedDrawerBox = await drawer.boundingBox()
    expect(draggedDrawerBox).toBeTruthy()
    expect(draggedDrawerBox!.height).toBeGreaterThan(initialDrawerHeight)

    // Feedback lives inside the sidebar/drawer (not under the stage preview).
    expect(feedbackBox!.y).toBeGreaterThanOrEqual(drawerBox!.y - 1)
    expect(feedbackBox!.y).toBeLessThan(drawerBox!.y + drawerBox!.height)

    // Review semantics: 确认并流转 visible; no 通过/打回 dual buttons.
    await expect(page.getByTestId('review-composer-pass')).toBeVisible()
    await expect(page.getByRole('button', { name: '确认并流转' })).toBeVisible()
    await expect(page.getByRole('button', { name: '通过并流转' })).toHaveCount(0)
    await expect(page.getByRole('button', { name: '打回修改' })).toHaveCount(0)
    await expect(page.getByTestId('review-record-issue')).toBeVisible()
    await expect(page.getByRole('button', { name: '记入意见' })).toBeVisible()
    // Hot session: send remains available for in-place revise.
    await expect(page.getByTestId('review-composer-send')).toBeVisible()
    await expect(page.getByRole('button', { name: '发送' })).toBeVisible()
    // Single main text input in the drawer.
    await expect(drawer.getByTestId('paragraph-input')).toHaveCount(1)
    // Narrow drawer must expose image attach — not text-only.
    const attach = drawer.getByTestId('paragraph-input-attach')
    await expect(attach).toBeVisible()

    // Unsubmitted draft alone must not restore 打回 wording.
    const fileInput = drawer.locator('input[type="file"]')
    await fileInput.setInputFiles({
      name: 'reject-only.png',
      mimeType: 'image/png',
      buffer: Buffer.from(
        'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==',
        'base64',
      ),
    })
    await expect(page.getByRole('button', { name: '打回修改' })).toHaveCount(0)
    await expect(page.getByTestId('review-composer-pass')).toBeVisible()

    // Inspect toggle remains available inside the preview shell.
    await expect(page.getByTestId('html-preview-inspect-bar')).toBeVisible()
    await expect(page.getByRole('button', { name: '取点标注' })).toBeVisible()
  })

  // plan_coverage: g2.3 — pull to full top / full bottom; handle remains; reverse drag restores panels
  test('390×844：抽屉可拉满顶/底且手柄可反向拖回', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await mockApi(page, [])

    await page.goto('/gate-mobile-fill.html')
    await expect(page.getByTestId('gate-mobile-fill-root')).toBeVisible({ timeout: 10_000 })
    const shell = page.getByTestId('review-shell')
    const drawer = page.getByTestId('review-shell-sidebar')
    const stage = page.getByTestId('review-shell-stage')
    const handle = page.getByTestId('review-shell-drawer-handle')
    await expect(handle).toBeVisible()

    const shellBox = await shell.boundingBox()
    expect(shellBox).toBeTruthy()
    const shellH = shellBox!.height

    async function dragHandleBy(dy: number) {
      const box = await handle.boundingBox()
      expect(box).toBeTruthy()
      const cx = box!.x + box!.width / 2
      const cy = box!.y + box!.height / 2
      await page.mouse.move(cx, cy)
      await page.mouse.down()
      await page.mouse.move(cx, cy + dy, { steps: 12 })
      await page.mouse.up()
    }

    // Full top: drag handle far upward → drawer ≈ shell height, stage collapsed
    await dragHandleBy(-(shellH + 100))
    const fullTopDrawer = await drawer.boundingBox()
    const fullTopStage = await stage.boundingBox()
    expect(fullTopDrawer).toBeTruthy()
    expect(Math.abs(fullTopDrawer!.height - shellH)).toBeLessThanOrEqual(2)
    expect(fullTopStage!.height).toBeLessThanOrEqual(2)
    await expect(handle).toBeVisible()

    // Drag back down → stage reappears
    await dragHandleBy(Math.round(shellH * 0.4))
    const midFromTop = await drawer.boundingBox()
    const stageFromTop = await stage.boundingBox()
    expect(midFromTop!.height).toBeLessThan(shellH - 10)
    expect(midFromTop!.height).toBeGreaterThan(50)
    expect(stageFromTop!.height).toBeGreaterThan(10)

    // Full bottom: drag handle far downward → drawer ≈ 44px handle
    await dragHandleBy(shellH + 100)
    const fullBottomDrawer = await drawer.boundingBox()
    const fullBottomStage = await stage.boundingBox()
    expect(fullBottomDrawer).toBeTruthy()
    expect(fullBottomDrawer!.height).toBeGreaterThanOrEqual(42)
    expect(fullBottomDrawer!.height).toBeLessThanOrEqual(48)
    expect(fullBottomStage!.height).toBeGreaterThan(shellH - 50)
    await expect(handle).toBeVisible()

    // Drag back up → chat/drawer expands again
    await dragHandleBy(-Math.round(shellH * 0.35))
    const midFromBottom = await drawer.boundingBox()
    expect(midFromBottom!.height).toBeGreaterThan(60)
    expect(midFromBottom!.height).toBeLessThan(shellH - 10)
  })

  test('390×844：n_open≥1 可继续发送，确认并流转禁用', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await mockApi(page, [
      {
        id: 'iss-open',
        runId: 'run-gate-fill',
        nodeId: 'hg-visual',
        body: '布局需调整',
        status: 'open',
        createdAt: '2026-07-18T00:01:00Z',
      },
    ])

    await page.goto('/gate-mobile-fill.html')
    await expect(page.getByTestId('gate-mobile-fill-root')).toBeVisible({ timeout: 10_000 })

    // Open issues: keep send (no 打回) + confirm disabled (not unmounted).
    const send = page.getByTestId('review-composer-send')
    const pass = page.getByTestId('review-composer-pass')
    await expect(send).toBeVisible()
    await expect(pass).toBeVisible()
    await expect(send).toContainText('发送')
    await expect(pass).toContainText('确认并流转')
    await expect(pass).toBeDisabled()
    await expect(send).toBeEnabled()
    await expect(page.getByRole('button', { name: /打回/ })).toHaveCount(0)
    await expect(page.getByRole('button', { name: '通过并流转' })).toHaveCount(0)
  })
})
