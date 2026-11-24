import { expect, test, type Locator, type Page } from '@playwright/test'

/** Fill of the first flowchart node; Mermaid paints it from themeVars().primaryColor. */
function nodeFill(scope: Locator) {
  return scope.locator('.md-mermaid-svg svg .node rect, .md-mermaid-svg svg .node path').first().evaluate((el) => getComputedStyle(el).fill)
}

function labelColor(scope: Locator) {
  return scope.locator('.md-mermaid-svg svg .nodeLabel').first().evaluate((el) => getComputedStyle(el).color)
}

function luminance(rgb: string) {
  const [r, g, b] = (rgb.match(/\d+(\.\d+)?/g) ?? []).map(Number)
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}

async function expectDrawn(scope: Locator) {
  const pre = scope.locator('pre.md-mermaid').first()
  await expect(pre).toHaveAttribute('data-mermaid', 'done')
  await expect(pre.locator('code')).toBeHidden()
  const box = await pre.locator('.md-mermaid-svg svg').boundingBox()
  expect(box!.width).toBeGreaterThan(200)
  expect(box!.height).toBeGreaterThan(80)
}

async function expectTheme(page: Page, theme: 'dark' | 'light') {
  const chat = page.getByTestId('chat')
  await expect.poll(async () => luminance(await nodeFill(chat)) > 128).toBe(theme === 'light')
  await expect.poll(async () => luminance(await labelColor(chat)) > 128).toBe(theme === 'dark')
}

for (const theme of ['dark', 'light'] as const) {
  test(`chat and artifact markdown draw mermaid fences in ${theme}`, async ({ page }) => {
    const errors: string[] = []
    page.on('pageerror', (err) => errors.push(err.message))
    await page.goto(`/markdown-mermaid.html?theme=${theme}`)

    const chat = page.getByTestId('chat')
    await expectDrawn(chat)
    await expect(chat.locator('.md-mermaid-svg')).toContainText('CodexLoginManager')
    await expect(chat.locator('.md-mermaid-svg')).toContainText('沙箱 Docker/K8s/E2B')
    await expect(chat.locator('.md-mermaid-svg')).toContainText('WriteFileAtomic')
    await expectTheme(page, theme)

    // The wide flowchart keeps readable labels and scrolls inside its card.
    const chatPre = chat.locator('pre.md-mermaid')
    const scale = await chatPre.locator('svg').evaluate((svg) => svg.getBoundingClientRect().width / Number(svg.getAttribute('viewBox')!.split(/\s+/)[2]))
    expect(scale).toBeGreaterThanOrEqual(0.74)
    expect(await chatPre.evaluate((el) => el.scrollWidth > el.clientWidth)).toBe(true)
    // Edges must stand out from the canvas, not use the faint border color.
    const edge = await chat.locator('.md-mermaid-svg svg .flowchart-link').first().evaluate((el) => getComputedStyle(el).stroke)
    const canvas = await chatPre.evaluate((el) => getComputedStyle(el).backgroundColor)
    expect(Math.abs(luminance(edge) - luminance(canvas))).toBeGreaterThan(60)

    // Prose labels must not pick up `.md p` / chat paragraph margins.
    const labelMargin = await chat.locator('.md-mermaid-svg svg p').first().evaluate((el) => getComputedStyle(el).marginTop)
    expect(labelMargin).toBe('0px')

    const artifact = page.getByTestId('artifact')
    await expectDrawn(artifact)
    await expect(artifact.locator('.md-mermaid-svg')).toContainText('Grasp Server')
    const broken = artifact.locator('pre.md-mermaid').nth(1)
    await expect(broken).toHaveAttribute('data-mermaid', 'failed')
    await expect(broken.locator('code')).toBeVisible()
    await expect(broken.locator('code')).toContainText('A -->')
    await expect(artifact.locator('code.language-ts')).toBeVisible()
    await expect(page.locator('body > *', { hasText: 'Syntax error in text' })).toHaveCount(0)

    await page.screenshot({ path: test.info().outputPath(`markdown-mermaid-${theme}.png`), fullPage: true })
    expect(errors).toEqual([])
  })
}

test('switching theme redraws diagrams in place', async ({ page }) => {
  await page.goto('/markdown-mermaid.html?theme=dark')
  await expectDrawn(page.getByTestId('chat'))
  await expectTheme(page, 'dark')

  await page.getByTestId('theme-light').click()
  await expectTheme(page, 'light')
  await expectDrawn(page.getByTestId('chat'))
  await expectDrawn(page.getByTestId('artifact'))

  await page.getByTestId('theme-dark').click()
  await expectTheme(page, 'dark')
})

test('a streaming fence stays source until it closes', async ({ page }) => {
  await page.goto('/markdown-mermaid.html')
  const stream = page.getByTestId('stream')
  await expect(stream.locator('code.language-mermaid')).toBeVisible()
  await expect(stream.locator('pre.md-mermaid')).toHaveCount(0)

  await page.getByTestId('stream-close').click()
  await expectDrawn(stream)
  await expect(stream).toContainText('继续输出。')
})
