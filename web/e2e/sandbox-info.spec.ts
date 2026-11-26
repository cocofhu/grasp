import { expect, test } from '@playwright/test'

async function openPanel(page: import('@playwright/test').Page, query: string) {
  await page.goto(`/sandbox-info.html?${query}`)
  await page.getByTestId('sandbox-info-toggle').click()
  const panel = page.getByTestId('sandbox-info-panel')
  await expect(panel).toBeVisible()
  return panel
}

for (const theme of ['dark', 'light']) {
  test(`running sandbox panel stays inside the viewport under its trigger (${theme})`, async ({ page }) => {
    const panel = await openPanel(page, `theme=${theme}`)
    await expect(page.getByTestId('sandbox-info-state')).toHaveText('运行中')
    await expect(page.getByTestId('sandbox-info-reclaim')).toContainText('后保存并暂停')
    await expect(page.getByTestId('sandbox-ide-picker-trigger')).toContainText('SSH 连接')
    const box = (await panel.boundingBox())!
    const trigger = (await page.getByTestId('sandbox-info-toggle').boundingBox())!
    const viewport = page.viewportSize()!
    expect(box.y).toBeGreaterThan(trigger.y + trigger.height)
    expect(box.x).toBeGreaterThanOrEqual(8)
    expect(box.x + box.width).toBeLessThanOrEqual(viewport.width - 7)
    await page.screenshot({ path: test.info().outputPath(`sandbox-info-running-${theme}.png`) })
    await page.getByTestId('sandbox-ide-picker-trigger').click()
    const menu = page.getByTestId('sandbox-ide-picker-panel')
    await expect(page.getByTestId('sandbox-ide-vscode')).toHaveAttribute('href', 'vscode://vscode-remote/ssh-remote+sbx-123@ssh.grasp.example.com:2222/root/workspace')
    await expect(menu).toBeVisible()
    for (const logo of await menu.locator('img[data-ide-logo]').all()) {
      await expect.poll(() => logo.evaluate((img) => (img as HTMLImageElement).naturalWidth)).toBeGreaterThan(0)
    }
    // Single-color marks are painted in the text color, so they never vanish on the dark theme.
    await expect.poll(() => menu.locator('[data-ide-logo="cursor"]').evaluate((el) => {
      const style = getComputedStyle(el)
      return style.backgroundColor === style.color
    })).toBe(true)
    await page.screenshot({ path: test.info().outputPath(`sandbox-info-menu-${theme}.png`) })
    await page.getByTestId('sandbox-ide-info').click()
    await expect(page.getByTestId('sandbox-info-command')).toHaveText('ssh -p 2222 sbx-123@ssh.grasp.example.com')
    await page.screenshot({ path: test.info().outputPath(`sandbox-info-details-${theme}.png`) })
  })
}

for (const state of ['busy', 'connected', 'paused', 'docker', 'stopped', 'error', 'starting']) {
  test(`sandbox info panel renders the ${state} state`, async ({ page }) => {
    await openPanel(page, `state=${state}`)
    await page.screenshot({ path: test.info().outputPath(`sandbox-info-${state}.png`) })
  })
}

test('paused sandbox wakes when an IDE link is used, and Escape closes the panel', async ({ page }) => {
  await openPanel(page, 'state=paused')
  await page.getByTestId('sandbox-ide-picker-trigger').click()
  await page.getByTestId('sandbox-ide-vscode').evaluate((el) => el.addEventListener('click', (e) => e.preventDefault()))
  await page.getByTestId('sandbox-ide-vscode').click()
  await expect(page.getByTestId('events')).toContainText('action:wake')
  await page.keyboard.press('Escape')
  await expect(page.getByTestId('sandbox-info-panel')).toBeHidden()
  await expect(page.getByTestId('sandbox-info-toggle')).toBeFocused()
})

test('users without a key are sent to the SSH keys page', async ({ page }) => {
  await openPanel(page, 'key=none&lang=en')
  await expect(page.getByTestId('sandbox-info-no-key')).toContainText('Register an SSH public key')
  await page.screenshot({ path: test.info().outputPath('sandbox-info-no-key-en.png') })
})

test('narrow screens get a bottom sheet', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  const panel = await openPanel(page, 'state=paused')
  const box = (await panel.boundingBox())!
  expect(Math.round(box.x)).toBe(0)
  expect(Math.round(box.y + box.height)).toBe(844)
  await page.screenshot({ path: test.info().outputPath('sandbox-info-mobile.png') })
})
