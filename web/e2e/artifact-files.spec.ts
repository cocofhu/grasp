import { test, expect, type Page } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

const media = (path: string) => readFileSync(fileURLToPath(new URL(`../../media/${path}`, import.meta.url)))
const video = media('grasp-demo.mp4')
const image = media('yume-mark.png')
const wide = media('screenshots/artifacts.webp')
const small = '<svg xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24"><path d="M13 2 5 13h6l-2 9 10-13h-6z" fill="none" stroke="#71717a" stroke-width="2"/></svg>'
function wav() {
  const data = Buffer.alloc(8044)
  data.write('RIFF'); data.writeUInt32LE(8036, 4); data.write('WAVEfmt ', 8)
  data.writeUInt32LE(16, 16); data.writeUInt16LE(1, 20); data.writeUInt16LE(1, 22)
  data.writeUInt32LE(8000, 24); data.writeUInt32LE(8000, 28); data.writeUInt16LE(1, 32); data.writeUInt16LE(8, 34)
  data.write('data', 36); data.writeUInt32LE(8000, 40); data.fill(128, 44)
  return data
}
const bodies: Record<string, { body: Buffer | string; type: string }> = {
  report: { body: '# 已交付的版本\n\n报告正文。', type: 'text/markdown' },
  config: { body: '{"fast": true}', type: 'application/json' },
  image: { body: image, type: 'image/png' },
  wide: { body: wide, type: 'image/webp' },
  small: { body: small, type: 'image/svg+xml' },
  video: { body: video, type: 'video/mp4' },
  audio: { body: wav(), type: 'audio/wav' },
  pdf: { body: '%PDF-1.4\n1 0 obj<</Type/Catalog>>endobj\n%%EOF', type: 'application/pdf' },
  zip: { body: 'archive-data', type: 'application/octet-stream' },
}

async function mockArtifacts(page: Page) {
  const seen: string[] = []
  await page.route('**/api/artifacts/*/*?revision=*', async route => {
    const url = new URL(route.request().url())
    const id = url.pathname.split('/')[3]
    seen.push(url.pathname + url.search)
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
  return seen
}
const chip = (page: Page, name: string) => page.getByTestId('artifact-file-chip').filter({ has: page.getByText(name, { exact: true }) })
const preview = (page: Page, name: string) => chip(page, name).getByRole('button', { name: `预览 ${name}`, exact: true }).click()

test('attachments retain versions, preview common files and download originals', async ({ page }) => {
  const seen = await mockArtifacts(page)
  await page.goto('/artifact-files.html')
  await expect(page.getByTestId('artifact-file-attachments')).toBeVisible()
  await expect(page.getByTestId('artifact-file-chip').locator('.truncate.font-semibold')).toHaveText([
    '图片.png', '宽截图.webp', 'bolt-12.svg', '旧版截图.png', '视频.mp4', '报告.md', 'fast-config.json', '音频.wav', '文档.pdf', '资料.zip', '缺失.txt',
  ])
  await expect(chip(page, '报告.md')).toContainText('48 B · v1')
  await expect(chip(page, '报告.md')).toHaveAttribute('title', '报告.md\n固定发送时的版本')

  await preview(page, '报告.md')
  await expect(page.getByRole('heading', { name: '已交付的版本' })).toBeVisible()
  await expect(page.getByTestId('artifact-file-download')).toHaveAttribute('href', /report\/download\?revision=1$/)
  await page.keyboard.press('Escape')
  await preview(page, '图片.png')
  await expect.poll(() => page.getByTestId('artifact-file-preview').locator('img').evaluate((el: HTMLImageElement) => el.naturalWidth)).toBeGreaterThan(0)
  await page.keyboard.press('Escape')
  for (const [name, tag] of [['视频.mp4', 'video'], ['音频.wav', 'audio']] as const) {
    await preview(page, name)
    const el = page.getByTestId('artifact-file-preview').locator(tag)
    await expect(el).toBeVisible()
    await expect.poll(() => el.evaluate((m: HTMLMediaElement) => m.readyState)).toBeGreaterThanOrEqual(1)
    expect(await el.evaluate((m: HTMLMediaElement) => m.autoplay)).toBe(false)
    await page.keyboard.press('Escape')
  }
  await preview(page, '文档.pdf')
  await expect(page.getByTestId('artifact-file-preview').locator('iframe')).toHaveAttribute('src', /pdf\/raw\?revision=1$/)
  await page.keyboard.press('Escape')

  await expect(chip(page, '资料.zip').getByRole('button')).toBeDisabled()
  const zipDownload = chip(page, '资料.zip').getByRole('link', { name: '下载原文件' })
  await expect(zipDownload).toHaveCSS('opacity', '1')
  await expect(zipDownload).toHaveAttribute('href', /zip\/download\?revision=1$/)
  const downloaded = page.waitForEvent('download')
  await zipDownload.click()
  expect((await downloaded).suggestedFilename()).toBe('zip.bin')
  await expect(page.getByRole('dialog')).toHaveCount(0)

  await preview(page, '缺失.txt')
  await expect(page.getByTestId('artifact-file-error')).toBeVisible()
  await page.keyboard.press('Escape')
  expect(seen.filter(url => url.includes('/raw')).every(url => url.endsWith('?revision=1'))).toBe(true)
})

test('chips show real thumbnails, fall back to file icons and reveal download on hover or focus', async ({ page }) => {
  const seen = await mockArtifacts(page)
  await page.goto('/artifact-files.html')
  for (const name of ['图片.png', '宽截图.webp', 'bolt-12.svg']) {
    const thumb = chip(page, name).locator('img')
    await expect(thumb).toHaveAttribute('src', /\/raw\?revision=1$/)
    await expect.poll(() => thumb.evaluate((el: HTMLImageElement) => el.complete && el.naturalWidth)).toBeGreaterThan(0)
  }
  await expect.poll(() => chip(page, 'bolt-12.svg').locator('img').evaluate(el => {
    const box = el.getBoundingClientRect()
    return [box.width, box.height]
  })).toEqual([24, 24])

  const broken = chip(page, '旧版截图.png')
  await expect(broken.locator('img')).toHaveCount(0)
  await expect(broken.getByTestId('file-type-icon')).toHaveAttribute('data-tone', 'media')
  await expect(broken.getByTestId('file-type-icon')).toContainText('PNG')
  await expect(chip(page, 'fast-config.json').getByTestId('file-type-icon')).toHaveAttribute('data-tone', 'data')
  await expect(chip(page, '文档.pdf').getByTestId('file-type-icon')).toContainText('PDF')
  await preview(page, '旧版截图.png')
  await expect(page.getByTestId('artifact-file-error')).toBeVisible()
  await page.keyboard.press('Escape')

  const thumbVideo = chip(page, '视频.mp4').locator('video')
  expect(await thumbVideo.evaluate((el: HTMLVideoElement) => [el.autoplay, el.muted, el.preload])).toEqual([false, true, 'metadata'])

  const heights = await page.getByTestId('artifact-file-chip').evaluateAll(els => els.map(el => el.getBoundingClientRect().height))
  expect(new Set(heights).size).toBe(1)

  await page.mouse.move(0, 0)
  const download = chip(page, '报告.md').getByTestId('artifact-file-chip-download')
  await expect(download).toHaveCSS('opacity', '0')
  await chip(page, '报告.md').hover()
  await expect(download).toHaveCSS('opacity', '1')
  await page.mouse.move(0, 0)
  await expect(download).toHaveCSS('opacity', '0')
  await chip(page, '报告.md').getByRole('button').focus()
  await page.keyboard.press('Tab')
  await expect(download).toBeFocused()
  await expect(download).toHaveCSS('opacity', '1')
  await expect(download).toHaveAttribute('aria-label', '下载原文件')
  expect(seen.filter(url => url.includes('/raw')).every(url => url.endsWith('?revision=1'))).toBe(true)
})

test('dark theme keeps thumbnails and paper icons light; narrow screens stack full-width chips', async ({ page }) => {
  await mockArtifacts(page)
  await page.setViewportSize({ width: 375, height: 800 })
  await page.goto('/artifact-files.html?theme=dark')
  await expect(page.locator('html')).not.toHaveClass(/light/)
  const thumb = chip(page, '图片.png').locator('.artifact-chip__thumb')
  await expect(thumb).toHaveCSS('background-image', /rgb\(236, 236, 239\).*rgb\(255, 255, 255\)/)
  await expect(chip(page, '报告.md').locator('.file-type-icon__paper')).toHaveCSS('fill', 'rgb(230, 230, 235)')

  const list = page.getByTestId('artifact-file-attachments')
  const listWidth = await list.evaluate(el => el.getBoundingClientRect().width)
  const widths = await page.getByTestId('artifact-file-chip').evaluateAll(els => els.map(el => el.getBoundingClientRect().width))
  expect(widths.every(w => Math.abs(w - listWidth) < 1)).toBe(true)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true)
})
