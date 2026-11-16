import { test, expect } from '@playwright/test'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import fs from 'node:fs'

const shotDir = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  '../../../.tmp-chat-image-surfaces-shots',
)

const PNG_FILE = {
  name: '待发截图.png',
  mimeType: 'image/png',
  buffer: Buffer.from(
    'iVBORw0KGgoAAAANSUhEUgAAAPAAAACgCAIAAAC9uXYyAAABRElEQVR42u3SQQ0AAAgDsQmbfxWIQQUf0qQKLpdp4Y1IgKHB0GBoMDSGBkODocHQYGgMDYYGQ4OhwdAYGgwNhgZDg6ExNBgaDA2GBkNjaDA0GBoMDYbG0GBoMDQYGgyNocHQYGgwNIYGQ4OhwdBgaAwNhgZDg6HB0BgaDA2GBkODoTE0GBoMDYYGQ2NoMDQYGgwNhsbQYGgwNBgaDI2hwdBgaDA0GBpDg6HB0GBoDA2GBkODocHQGBoMDYYGQ4OhMTQYGgwNhgZDY2gwNBgaDA2GxtBgaDA0GBoMjaHB0GBoMDQYGkODocHQYGgMrQKGBkODocHQGBoMDYYGQ4OhMTQYGgwNhgZDY2gwNBgaDA2GxtBgaDA0GBoMjaHB0GBoMDQYGkODocHQYGgwNIYGQ4OhwdAYGgwNhgZDg6ExNBgaDA03FrNxQ6p/RCs5AAAAAElFTkSuQmCC',
    'base64',
  ),
}

test.describe('chat image preview surfaces (feedback / para / tester / shared)', () => {
  test.beforeAll(() => {
    fs.mkdirSync(shotDir, { recursive: true })
  })

  test('four previewable entries + load-failed placeholder', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 900 })
    await page.goto('/chat-image-surfaces.html')
    await expect(page.getByTestId('chat-image-surfaces-root')).toBeVisible({ timeout: 20_000 })
    const shot = (name: string) => page.screenshot({ path: path.join(shotDir, name), animations: 'disabled' })

    const closePreview = async () => {
      await page.getByRole('dialog').getByRole('button', { name: 'Close', exact: true }).click()
      await expect(page.getByRole('dialog', { includeHidden: true })).toHaveCount(0)
    }

    // --- Preview feedback issue + element + draft (FR-f3) ---
    await expect(page.getByTestId('surf-panel-feedback')).toBeVisible()
    const issueThumb = page.getByTestId('preview-issue-image-thumb')
    await expect(issueThumb).toBeVisible()
    await expect(issueThumb).toContainText('点击放大')
    await expect(issueThumb).not.toContainText('不可预览')
    const elemThumb = page.getByTestId('preview-element-image-thumb')
    await expect(elemThumb).toBeVisible()
    await expect(page.getByTestId('paragraph-draft-image-thumb')).toBeVisible()
    await expect(page.getByTestId('paragraph-draft-image-thumb')).not.toContainText('不可预览')
    await shot('01-feedback-thumbs.png')

    await issueThumb.click()
    await expect(page.getByText('图片预览 · issue附图.png')).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(page.getByTestId('preview-feedback-image-preview-img')).toBeVisible()
    await closePreview()
    await expect(page.getByTestId('preview-feedback-image-preview-img')).toHaveCount(0)

    await elemThumb.click()
    await expect(page.getByText('图片预览 · 元素截图.png')).toBeVisible()
    await closePreview()
    await expect(page.getByText('button.surf.active')).toBeVisible()
    await expect(elemThumb).toBeVisible()
    await expect(page.getByTestId('paragraph-draft-image-thumb')).toBeVisible()

    await page.getByTestId('paragraph-draft-image-thumb').click()
    await expect(page.getByText('图片预览 · 反馈草稿.png')).toBeVisible()
    await closePreview()
    await expect(page.getByTestId('paragraph-draft-image-thumb')).toBeVisible()

    // --- ParagraphInput gate composer (FR-f5) ---
    await page.getByTestId('surf-tab-para').click()
    await expect(page.getByTestId('surf-panel-para')).toBeVisible()
    const paraThumb = page.getByTestId('paragraph-draft-image-thumb')
    await expect(paraThumb).toBeVisible()
    await expect(paraThumb).toContainText('点击放大')
    await expect(page.getByTestId('paragraph-pending-file-chip')).toBeVisible()
    await paraThumb.click()
    await expect(page.getByText('图片预览 · 门禁附件.png')).toBeVisible()
    await expect(page.getByTestId('paragraph-image-preview-img')).toHaveClass(/max-h-\[74vh\]/)
    await page.keyboard.press('Escape')
    await expect(page.getByTestId('paragraph-image-preview-img')).toBeVisible()
    await shot('02-paragraph-modal.png')
    await closePreview()
    await expect(paraThumb).toBeVisible()

    // --- Agent tester restored history (FR-f4 / g5.1) ---
    await page.getByTestId('surf-tab-tester').click()
    await expect(page.getByTestId('surf-panel-tester')).toBeVisible()
    const testerHist = page.getByTestId('tester-history-image-thumb')
    await expect(testerHist).toBeVisible({ timeout: 15_000 })
    await expect(testerHist).toContainText('点击放大')
    await expect(testerHist).not.toContainText('不可预览')
    await testerHist.click()
    await expect(page.getByText(/图片预览/)).toBeVisible()
    await expect(page.getByTestId('tester-image-preview-img')).toBeVisible()
    await shot('03-tester-history-modal.png')
    await closePreview()

    await page.locator('[data-testid="surf-panel-tester"] input[type="file"]').setInputFiles(PNG_FILE)
    const testerDraft = page.getByTestId('tester-draft-image-thumb')
    await expect(testerDraft).toBeVisible()
    await expect(testerDraft).not.toContainText('不可预览')
    await testerDraft.click()
    await expect(page.getByText('图片预览 · 待发截图.png')).toBeVisible()
    await closePreview()
    await expect(testerDraft).toBeVisible()

    // --- Shared sizes / locked / load-failed (FR-f6/f8) ---
    await page.getByTestId('surf-tab-shared').click()
    await expect(page.getByTestId('surf-panel-shared')).toBeVisible()
    await expect(page.getByTestId('shared-md-thumb')).toHaveClass(/h-20/)
    await expect(page.getByTestId('shared-sm-thumb')).toHaveClass(/h-14/)
    await expect(page.getByTestId('shared-xs-thumb')).toHaveClass(/h-8/)
    const locked = page.getByTestId('shared-locked-thumb')
    await expect(locked).toContainText('不可预览')
    await expect(locked).not.toContainText('点击放大')
    await locked.click()
    await expect(page.getByTestId('shared-image-preview-img')).toHaveCount(0)

    const failThumb = page.getByTestId('shared-fail-thumb')
    await expect(failThumb).toContainText('图片加载失败', { timeout: 10_000 })
    await expect(failThumb).toContainText('失效图.png')
    await expect(failThumb).toContainText('重试')
    await failThumb.click()
    await expect(page.getByTestId('shared-image-preview-failed')).toHaveCount(0)
    await page.getByTestId('shared-fail-thumb-retry').click()
    await expect(failThumb).toContainText('图片加载失败')
    await shot('04-load-failed.png')

    const dialog = page.getByTestId('shared-md-thumb')
    await dialog.click()
    await expect(page.getByRole('dialog')).toBeVisible()
    await expect(page.getByRole('dialog')).toHaveAttribute('aria-modal', 'true')
    await expect(page.getByText('图片预览 · 历史.md.png')).toBeVisible()
    await shot('05-shared-dialog-a11y.png')
    await closePreview()
  })
})
