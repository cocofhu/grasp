import { expect, test, type Page } from '@playwright/test'

/** Recommended options are already pressed on mount; a click would toggle them off. */
async function ensureOptionPressed(page: Page, index: number) {
  const button = page.getByTestId('clarify-option-btn').nth(index)
  if ((await button.getAttribute('aria-pressed')) !== 'true') await button.click()
  await expect(button).toHaveAttribute('aria-pressed', 'true')
}

for (const width of [1440, 390, 240]) {
  for (const lang of ['zh-CN', 'en']) {
    test(`question metadata has its own line at ${width}px (${lang})`, async ({ page }) => {
      await page.setViewportSize({ width, height: 900 })
      await page.goto(`/clarify-question-layout.html?lang=${lang}&theme=${width === 390 ? 'dark' : 'light'}`)
      const prompt = page.getByTestId('clarify-question-prompt')
      const kind = page.getByText(lang === 'en' ? 'Single' : '单选', { exact: true })
      await expect(prompt).toBeVisible()
      await expect(kind).toBeVisible()
      await page.evaluate(() => document.fonts.ready)
      const geometry = await kind.evaluate((element) => {
        const prompt = document.querySelector('[data-testid="clarify-question-prompt"]')!.getBoundingClientRect()
        const kind = element.getBoundingClientRect()
        return { prompt: { x: prompt.x, bottom: prompt.bottom, right: prompt.right }, kind: { x: kind.x, y: kind.y, right: kind.right }, overflow: document.documentElement.scrollWidth > innerWidth }
      })
      expect(geometry.kind.y - geometry.prompt.bottom).toBeGreaterThanOrEqual(5)
      expect(Math.abs(geometry.kind.x - geometry.prompt.x)).toBeLessThan(1)
      expect(geometry.kind.right).toBeLessThanOrEqual(width)
      expect(geometry.prompt.right).toBeLessThanOrEqual(width)
      expect(geometry.overflow).toBe(false)
      await ensureOptionPressed(page, 0)
      await page.getByRole('button', { name: lang === 'en' ? 'Confirm selection' : '确认选择', exact: true }).click()
      await expect(page.getByTestId('question-layout-submission')).toContainText(lang === 'en' ? 'Conversation layout' : '聊天布局')
    })
  }
}

test('short multi-select metadata remains separate after resizing', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/clarify-question-layout.html?multiple=1&prompt=short')
  const prompt = page.getByTestId('clarify-question-prompt')
  const kind = page.getByText('多选', { exact: true })
  await expect(kind).toHaveText('多选')
  await ensureOptionPressed(page, 0)
  await ensureOptionPressed(page, 1)
  await page.setViewportSize({ width: 390, height: 700 })
  const titleBox = (await prompt.boundingBox())!
  const kindBox = (await kind.boundingBox())!
  expect(kindBox.y - titleBox.y - titleBox.height).toBeGreaterThanOrEqual(5)
  await expect(page.locator('[data-testid="clarify-option-btn"][aria-pressed="true"]')).toHaveCount(2)
})
