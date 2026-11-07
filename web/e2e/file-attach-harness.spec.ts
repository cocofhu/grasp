import { test, expect } from '@playwright/test'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import fs from 'node:fs'
import os from 'node:os'
import type { Locator, Page } from '@playwright/test'
import { waveFixture } from './helpers/attachmentFiles'

async function dropFiles(page: Page, zone: Locator) {
  const transfer = await page.evaluateHandle(base64 => {
    const dt = new DataTransfer()
    dt.items.add(new File(['notes'], 'notes.txt', { type: 'text/plain' }))
    dt.items.add(new File([Uint8Array.from(atob(base64), c => c.charCodeAt(0))], '语音.wav', { type: 'audio/wav' }))
    return dt
  }, waveFixture().toString('base64'))
  await zone.dispatchEvent('dragenter', { dataTransfer: transfer })
  await expect(zone.getByTestId('attachment-drop-hint')).toBeVisible()
  await zone.dispatchEvent('drop', { dataTransfer: transfer })
  await expect(zone.getByTestId('attachment-drop-hint')).toHaveCount(0)
  await transfer.dispose()
}

const shotDir = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  '../../../.tmp-file-attach-shots',
)

test.describe('ClarifyChat arbitrary file attachments', () => {
  test.beforeAll(() => {
    fs.mkdirSync(shotDir, { recursive: true })
  })

  test('drops multiple files, previews and plays audio, removes one and sends the rest', async ({ page }) => {
    await page.goto('/file-attach-harness.html')
    const zone = page.getByTestId('clarify-hot-actions')
    await dropFiles(page, zone)
    const audio = zone.getByRole('button', { name: '预览附件：语音.wav' })
    await expect(audio).toBeEnabled()
    await expect(audio).toContainText('音频')
    await expect(audio).toContainText('KiB')
    await audio.click()
    const player = page.locator('audio[data-testid="attachment-media-player"]')
    await expect(player).toBeVisible()
    await expect.poll(() => player.evaluate((el: HTMLMediaElement) => el.readyState)).toBeGreaterThanOrEqual(1)
    await player.evaluate(async (el: HTMLMediaElement) => { await el.play() })
    await expect.poll(() => player.evaluate((el: HTMLMediaElement) => el.currentTime)).toBeGreaterThan(0)
    await player.evaluate((el: HTMLMediaElement) => { el.pause(); el.currentTime = 0.25 })
    await expect.poll(() => player.evaluate((el: HTMLMediaElement) => el.currentTime)).toBeCloseTo(0.25)
    const download = page.getByTestId('attachment-download')
    const [saved] = await Promise.all([page.waitForEvent('download'), download.click()])
    expect(saved.suggestedFilename()).toBe('语音.wav')
    await page.keyboard.press('Escape')
    await expect(player).toHaveCount(0)
    await zone.getByRole('button', { name: '预览附件：notes.txt' }).locator('..').getByRole('button').last().click()
    await expect(zone.getByRole('button', { name: '预览附件：notes.txt' })).toHaveCount(0)
    await page.getByTestId('clarify-input').fill('听一下附件')
    await page.getByTestId('clarify-send-icon').click()
    await expect(page.getByTestId('file-attach-last-sent')).toContainText('语音.wav')
    await expect(page.getByTestId('file-attach-last-sent')).not.toContainText('notes.txt')
    const history = page.getByTestId('clarify-history-file-chip').filter({ hasText: '语音.wav' }).last()
    await history.click()
    await expect(player).toBeVisible()
    await page.keyboard.press('Escape')
  })

  test('plays a dropped WebM video and keeps download available after a decode failure', async ({ page }) => {
    await page.goto('/file-attach-harness.html')
    const transfer = await page.evaluateHandle(async () => {
      const canvas = document.createElement('canvas')
      canvas.width = canvas.height = 32
      const ctx = canvas.getContext('2d')!
      ctx.fillStyle = '#4383ef'
      ctx.fillRect(0, 0, 32, 32)
      const stream = canvas.captureStream(10)
      const recorder = new MediaRecorder(stream, { mimeType: 'video/webm' })
      const chunks: Blob[] = []
      recorder.ondataavailable = e => chunks.push(e.data)
      const recorded = new Promise<Blob>(resolve => { recorder.onstop = () => resolve(new Blob(chunks, { type: 'video/webm' })) })
      recorder.start()
      let frame = 0
      const redraw = setInterval(() => {
        ctx.fillStyle = ++frame % 2 ? '#4383ef' : '#2b62c9'
        ctx.fillRect(0, 0, 32, 32)
      }, 50)
      await new Promise(resolve => setTimeout(resolve, 500))
      clearInterval(redraw)
      recorder.stop()
      const video = await recorded
      stream.getTracks().forEach(track => track.stop())
      const dt = new DataTransfer()
      dt.items.add(new File([video], 'demo.webm', { type: 'video/webm' }))
      return dt
    })
    await page.getByTestId('clarify-hot-actions').dispatchEvent('drop', { dataTransfer: transfer })
    await transfer.dispose()
    const file = page.getByRole('button', { name: '预览附件：demo.webm' })
    await expect(file).toBeEnabled()
    await expect(file.locator('[data-icon="file-video"]')).toBeVisible()
    await file.click()
    const player = page.locator('video[data-testid="attachment-media-player"]')
    await expect.poll(() => player.evaluate((el: HTMLMediaElement) => el.readyState)).toBeGreaterThanOrEqual(2)
    await player.evaluate(async (el: HTMLMediaElement) => { await el.play() })
    await expect.poll(() => player.evaluate((el: HTMLMediaElement) => el.currentTime)).toBeGreaterThan(0)
    await page.keyboard.press('Escape')
    await expect(page.getByRole('dialog', { includeHidden: true })).toHaveCount(0)
    await page.locator('input[type="file"]').setInputFiles({ name: 'broken.mp4', mimeType: 'video/mp4', buffer: Buffer.from('invalid video') })
    await page.getByRole('button', { name: '预览附件：broken.mp4' }).click()
    await expect(page.getByTestId('attachment-preview-fallback')).toContainText('预览失败')
    await expect(page.getByTestId('attachment-download')).toHaveAttribute('download', 'broken.mp4')
  })

  test('uses the same drop intake in PM, tester, paragraph and home composers', async ({ page }) => {
    for (const tab of ['pm', 'tester', 'para']) {
      await page.goto('/chat-image-surfaces.html')
      await page.getByTestId(`surf-tab-${tab}`).click()
      const panel = page.getByTestId(`surf-panel-${tab}`)
      const zone = panel.locator('[data-attachment-drop-zone]').first()
      await dropFiles(page, zone)
      await expect(zone.getByRole('button', { name: '预览附件：语音.wav' })).toBeEnabled()
      await expect(zone.getByRole('button', { name: '预览附件：notes.txt' })).toBeVisible()
    }
    await page.goto('/dashboard-home-chat.html')
    const home = page.getByTestId('home-composer').locator('xpath=ancestor::*[@data-attachment-drop-zone][1]')
    await dropFiles(page, home)
    await expect(home.getByRole('button', { name: '预览附件：语音.wav' })).toBeEnabled()
  })

  test('ask_question + PDF attach + oversize reject + no accept filter', async ({ page }) => {
    await page.setViewportSize({ width: 1100, height: 900 })
    await page.goto('/file-attach-harness.html')
    await expect(page.getByTestId('file-attach-root')).toBeVisible({ timeout: 15_000 })

    const shot = (name: string) =>
      page.screenshot({ path: path.join(shotDir, name), animations: 'disabled' })

    // History shows non-image file chip with original name.
    const histChip = page.getByTestId('clarify-history-file-chip')
    await expect(histChip).toBeVisible()
    await expect(histChip).toContainText('需求说明-v3.pdf')

    // ask_question card is options-only (no file input inside the card).
    await expect(page.getByText('本次需求需要我优先阅读哪类材料？')).toBeVisible()
    const askCard = page.locator('.clarify-request').first()
    await expect(askCard.locator('input[type="file"]')).toHaveCount(0)
    await expect(askCard.getByRole('button', { name: '现有需求文档（PDF/Word）' })).toBeVisible()

    // Composer attach input accepts any type (no accept=image/*).
    const fileInput = page.locator('input[type="file"]')
    await expect(fileInput).toHaveCount(1)
    await expect(fileInput).not.toHaveAttribute('accept', /image/)

    await shot('01-ask-question-and-pdf-history.png')

    // Select a small PDF via composer; pending chip shows original name.
    const tmpPdf = path.join(os.tmpdir(), '验收材料.pdf')
    fs.writeFileSync(tmpPdf, '%PDF-1.4 tiny fixture\n')
    await page.getByTestId('clarify-attach-btn').click()
    await fileInput.setInputFiles({
      name: '验收材料.pdf',
      mimeType: 'application/pdf',
      buffer: Buffer.from('%PDF-1.4 tiny fixture\n'),
    })
    await expect(page.getByTestId('clarify-pending-file-chip')).toBeVisible({ timeout: 5_000 })
    await expect(page.getByTestId('clarify-pending-file-chip')).toContainText('验收材料.pdf')
    await shot('02-pending-pdf-chip.png')

    // Send carries name on the attachment payload.
    await page.getByTestId('clarify-input').fill('附上材料')
    await page.getByTestId('clarify-send-icon').click()
    const lastSent = page.getByTestId('file-attach-last-sent')
    await expect
      .poll(async () => lastSent.textContent(), { timeout: 8_000 })
      .toMatch(/验收材料\.pdf/)

    await shot('03-sent-with-name.png')

    // Selection-stage oversize (>50 MiB): write a sparse file (Playwright forbids >50Mb buffers).
    const hugePath = path.join(os.tmpdir(), 'dataset-backup.tar.gz')
    const fd = fs.openSync(hugePath, 'w')
    fs.ftruncateSync(fd, 50 * 1024 * 1024 + 64)
    fs.closeSync(fd)
    await fileInput.setInputFiles(hugePath)
    const notice = page.getByTestId('clarify-attach-notice')
    await expect(notice).toBeVisible({ timeout: 15_000 })
    await expect(notice).toContainText('50 MiB')
    await expect(notice).toContainText('dataset-backup.tar.gz')
    await expect(page.getByTestId('clarify-pending-file-chip')).toHaveCount(0)
    await shot('04-oversize-select-reject.png')
    fs.unlinkSync(hugePath)
  })
})
