import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

const video = readFileSync(fileURLToPath(new URL('../../media/grasp-demo.mp4', import.meta.url)))
const image = readFileSync(fileURLToPath(new URL('../../media/yume-mark.png', import.meta.url)))
function wav() {
  const data = Buffer.alloc(8044)
  data.write('RIFF'); data.writeUInt32LE(8036, 4); data.write('WAVEfmt ', 8)
  data.writeUInt32LE(16, 16); data.writeUInt16LE(1, 20); data.writeUInt16LE(1, 22)
  data.writeUInt32LE(8000, 24); data.writeUInt32LE(8000, 28); data.writeUInt16LE(1, 32); data.writeUInt16LE(8, 34)
  data.write('data', 36); data.writeUInt32LE(8000, 40); data.fill(128, 44)
  return data
}
test('attachments retain versions, preview common files and download originals', async ({ page }) => {
  const seen: string[] = []
  await page.route('**/api/artifacts/*/*?revision=*', async route => {
    const url = new URL(route.request().url())
    const id = url.pathname.split('/')[3]
    seen.push(url.pathname + url.search)
    const bodies: Record<string, { body: Buffer | string; type: string }> = {
      report: { body: '# 已交付的版本\n\n报告正文。', type: 'text/markdown' },
      image: { body: image, type: 'image/png' },
      video: { body: video, type: 'video/mp4' },
      audio: { body: wav(), type: 'audio/wav' },
      pdf: { body: '%PDF-1.4\n1 0 obj<</Type/Catalog>>endobj\n%%EOF', type: 'application/pdf' },
      zip: { body: 'archive-data', type: 'application/octet-stream' },
    }
    const row = bodies[id]
    if (!row) { await route.fulfill({ status: 503, body: 'unavailable' }); return }
    const body = Buffer.from(row.body)
    const headers: Record<string, string> = { 'Content-Type': row.type, 'Accept-Ranges': 'bytes' }
    if (url.pathname.endsWith('/download')) headers['Content-Disposition'] = `attachment; filename="${id}.bin"`
    const range = route.request().headers().range?.match(/^bytes=(\d+)-(\d*)$/)
    if (range) {
      const start = Number(range[1]); const end = Math.min(Number(range[2] || body.length - 1), body.length - 1)
      headers['Content-Range'] = `bytes ${start}-${end}/${body.length}`
      await route.fulfill({ status: 206, headers, body: body.subarray(start, end + 1) })
    } else await route.fulfill({ headers, body })
  })
  await page.goto('/artifact-files.html')
  await expect(page.getByTestId('artifact-file-attachments')).toBeVisible()
  const card = (name: string) => page.getByTestId('artifact-file-attachments').locator(':scope > div').filter({ has: page.getByText(name, { exact: true }) })
  await card('报告.md').getByRole('button', { name: '预览', exact: true }).click()
  await expect(page.getByRole('heading', { name: '已交付的版本' })).toBeVisible()
  await expect(page.getByTestId('artifact-file-download')).toHaveAttribute('href', /report\/download\?revision=1$/)
  await page.keyboard.press('Escape')
  await card('图片.png').getByRole('button', { name: '预览', exact: true }).click()
  await expect.poll(() => page.getByTestId('artifact-file-preview').locator('img').evaluate((el: HTMLImageElement) => el.naturalWidth)).toBeGreaterThan(0)
  await page.keyboard.press('Escape')
  for (const [name, tag] of [['视频.mp4', 'video'], ['音频.wav', 'audio']] as const) {
    await card(name).getByRole('button', { name: '预览', exact: true }).click()
    const media = page.getByTestId('artifact-file-preview').locator(tag)
    await expect(media).toBeVisible()
    await expect.poll(() => media.evaluate((el: HTMLMediaElement) => el.readyState)).toBeGreaterThanOrEqual(1)
    expect(await media.evaluate((el: HTMLMediaElement) => el.autoplay)).toBe(false)
    await page.keyboard.press('Escape')
  }
  await card('文档.pdf').getByRole('button', { name: '预览', exact: true }).click()
  await expect(page.getByTestId('artifact-file-preview').locator('iframe')).toHaveAttribute('src', /pdf\/raw\?revision=1$/)
  await page.keyboard.press('Escape')
  await expect(card('资料.zip').getByRole('button')).toHaveCount(0)
  const downloaded = page.waitForEvent('download')
  await card('资料.zip').getByRole('link', { name: '下载原文件' }).click()
  expect((await downloaded).suggestedFilename()).toBe('zip.bin')
  await card('缺失.txt').getByRole('button', { name: '预览', exact: true }).click()
  await expect(page.getByTestId('artifact-file-error')).toBeVisible()
  expect(seen.filter(url => url.includes('/raw')).every(url => url.endsWith('?revision=1'))).toBe(true)
})
