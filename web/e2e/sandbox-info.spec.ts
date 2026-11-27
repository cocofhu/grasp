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
    await expect(panel.getByRole('heading', { name: 'grasp-solo-7f3a' })).toHaveCSS('color', await panel.evaluate(el => getComputedStyle(el).color))
    await expect(page.getByTestId('sandbox-info-state')).toHaveText('运行中')
    await expect(page.getByTestId('sandbox-info-reclaim')).toHaveText(/\d+ 小时 \d+ 分钟后/)
    await expect(page.getByTestId('sandbox-info-lifecycle')).not.toContainText(/到期先保存环境|本地 IDE 连接期间暂停计时/)
    const picker = page.getByTestId('sandbox-ide-picker-trigger')
    await expect(picker).toContainText('SSH 连接')
    const pickerBox = (await picker.boundingBox())!
    const browserIde = (await page.getByTestId('sandbox-info-web-ide').boundingBox())!
    expect(pickerBox.height).toBe(28)
    expect(browserIde.x).toBeGreaterThan(pickerBox.x + pickerBox.width)
    expect(browserIde.y).toBeCloseTo(pickerBox.y, 1)
    const box = (await panel.boundingBox())!
    const trigger = (await page.getByTestId('sandbox-info-toggle').boundingBox())!
    const viewport = page.viewportSize()!
    expect(box.y).toBeGreaterThan(trigger.y + trigger.height)
    expect(box.x).toBeGreaterThanOrEqual(8)
    expect(box.x + box.width).toBeLessThanOrEqual(viewport.width - 7)
    const details = page.getByTestId('sandbox-info-details')
    await expect(details).toBeHidden()
    await panel.screenshot({ path: test.info().outputPath(`sandbox-info-running-${theme}.png`) })
    await page.getByTestId('sandbox-info-details-toggle').click()
    await expect(details).toBeVisible()
    await expect(details).toContainText('/root/workspace')
    await expect(page.getByTestId('sandbox-info-task-tokens')).toHaveText('1,268,330')
    await expect(details).not.toContainText(/参考|任务价值/)
    const workflows = page.getByTestId('sandbox-info-workflow-runs')
    await expect(workflows).toHaveText('发布检查')
    await expect(workflows.getByRole('link', { name: '发布检查' })).toHaveAttribute('href', '/runs/verify-sandbox')
    const tokenValue = (await page.getByTestId('sandbox-info-task-tokens').boundingBox())!
    const workflowValue = (await workflows.boundingBox())!
    expect(workflowValue.y).toBeGreaterThan(tokenValue.y + tokenValue.height)
    expect(workflowValue.x + workflowValue.width).toBeCloseTo(tokenValue.x + tokenValue.width)
    await panel.screenshot({ path: test.info().outputPath(`sandbox-info-expanded-${theme}.png`) })
    const token = page.getByTestId('sandbox-info-token-trigger')
    const breakdown = page.getByTestId('sandbox-info-token-breakdown')
    await token.hover()
    await expect(page.getByTestId('sandbox-info-token-sum').getByRole('definition')).toHaveText(['750,000', '125,000', '350,000', '43,330'])
    const models = breakdown.getByTestId('sandbox-info-token-model')
    await expect(models).toHaveCount(3)
    await expect(models.nth(0)).toContainText('claude-sonnet-4-6')
    await expect(models.nth(0)).toContainText('953,330')
    await expect(models.nth(0).getByRole('definition')).toHaveText(['550,000', '100,000', '270,000', '33,330'])
    await expect(models.nth(1)).toContainText('gpt-5.2')
    await expect(models.nth(2)).toContainText('未知模型')
    await expect(models.nth(2)).toBeInViewport({ ratio: 1 })
    await page.getByRole('tooltip').filter({ has: breakdown }).screenshot({
      path: test.info().outputPath(`sandbox-info-token-breakdown-${theme}.png`),
      animations: 'disabled',
    })
    await token.focus()
    await page.keyboard.press('Escape')
    await expect(breakdown).toBeHidden()
    await expect(panel).toBeVisible()
    await page.getByTestId('sandbox-info-details-toggle').click()
    await picker.click()
    const menu = page.getByTestId('sandbox-ide-picker-panel')
    await expect(page.getByTestId('sandbox-ide-vscode')).toHaveAttribute('href', 'vscode://vscode-remote/ssh-remote+sbx-123@ssh.grasp.example.com:2222/root/workspace')
    await expect(menu).toBeVisible()
    expect((await menu.boundingBox())!.width).toBeGreaterThan(pickerBox.width)
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

test('basic details animate in both directions and respect reduced motion', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'no-preference' })
  const panel = await openPanel(page, 'theme=light')
  const toggle = panel.getByTestId('sandbox-info-details-toggle')
  const details = panel.getByTestId('sandbox-info-details')
  await expect(details).toBeHidden()

  for (const expanded of [true, false]) {
    const heights = await toggle.evaluate(async (element) => {
      const button = element as HTMLButtonElement
      const content = document.getElementById(button.getAttribute('aria-controls')!)!
      const from = content.getBoundingClientRect().height
      button.click()
      await Promise.resolve()
      const motion = content.getAnimations().find(animation =>
        animation instanceof CSSTransition && animation.transitionProperty === 'grid-template-rows')
      if (!motion?.effect) return null

      motion.currentTime = Number(motion.effect.getComputedTiming().duration) / 4
      const during = content.getBoundingClientRect().height
      motion.finish()
      return { from, during, to: content.getBoundingClientRect().height }
    })
    expect(heights).not.toBeNull()
    const { from, during, to } = heights!
    expect(expanded ? from : to).toBe(0)
    expect(during).toBeGreaterThan(0)
    expect(during).toBeLessThan(expanded ? to : from)
    await expect(toggle).toHaveAttribute('aria-expanded', String(expanded))
  }

  await page.emulateMedia({ reducedMotion: 'reduce' })
  await toggle.click()
  await expect(details).toBeVisible()
  await expect(details).toHaveCSS('transition-duration', '0s')
  await expect(toggle.locator('svg')).toHaveCSS('rotate', '90deg')
})

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

  await page.setViewportSize({ width: 320, height: 720 })
  await page.getByTestId('sandbox-info-details-toggle').click()
  const tokens = page.getByTestId('sandbox-info-task-tokens')
  const workflows = page.getByTestId('sandbox-info-workflow-runs')
  await expect(tokens).toHaveText('1,268,330')
  await expect(workflows).toHaveText('发布检查')
  const tokenValue = (await tokens.boundingBox())!
  expect((await workflows.boundingBox())!.y).toBeGreaterThan(tokenValue.y + tokenValue.height)
  expect(await panel.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
  await panel.screenshot({ path: test.info().outputPath('sandbox-info-expanded-mobile.png') })
})
