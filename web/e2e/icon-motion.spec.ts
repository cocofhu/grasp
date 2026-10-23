import { expect, test, type Locator } from '@playwright/test'

async function animationCount(icon: Locator) {
  return icon.evaluate(element => element.getAnimations({ subtree: true }).filter(animation => animation.playState !== 'finished').length)
}

test('all registered icons are complete at rest and have real single-play hover motion', async ({ page }) => {
  const errors: string[] = []
  page.on('pageerror', error => errors.push(error.message))
  await page.goto('/icon-motion.html')
  const buttons = page.getByTestId('all-icons').locator('button')
  await expect(buttons.first()).toBeVisible()
  expect(await buttons.count()).toBeGreaterThanOrEqual(103)
  const icons = buttons.locator('svg.app-icon')
  await expect(icons).toHaveCount(await buttons.count())
  expect(await icons.evaluateAll(elements => elements.every(element => element.getAnimations({ subtree: true }).length === 0))).toBe(true)
  const missing: string[] = []
  for (const button of await buttons.all()) {
    await button.hover()
    const icon = button.locator('svg.app-icon')
    await expect(icon).toHaveAttribute('data-icon-motion', 'once')
    if (await button.innerText() === 'qq') {
      expect(await animationCount(icon), 'Brand geometry stays static').toBe(0)
    } else if (await animationCount(icon) === 0) missing.push(await button.innerText())
  }
  expect(missing, 'Every registered action icon must animate a visible part').toEqual([])
  expect(errors).toEqual([])
})

test('completed check draws left to right once, without a dot, and replays on reentry', async ({ page }) => {
  await page.goto('/icon-motion.html')
  const host = page.getByTestId('icon-check-draw')
  const icon = host.locator('svg')
  await expect(icon.locator('circle')).toHaveCount(0)
  const path = icon.locator('path')
  const points = await path.evaluate(element => {
    const line = element as SVGPathElement
    const first = line.getPointAtLength(0)
    const last = line.getPointAtLength(line.getTotalLength())
    return { first: { x: first.x, y: first.y }, last: { x: last.x, y: last.y }, dash: getComputedStyle(line).strokeDashoffset }
  })
  expect(points.first.x).toBeLessThan(points.last.x)
  expect(points.last.y).toBeLessThan(points.first.y)
  expect(Number.parseFloat(points.dash)).toBe(0)
  await host.hover()
  await expect(icon).toHaveAttribute('data-icon-motion', 'once')
  expect(await animationCount(icon)).toBeGreaterThan(0)
  await expect.poll(() => animationCount(icon), { timeout: 2500 }).toBe(0)
  await page.waitForTimeout(200)
  expect(await animationCount(icon)).toBe(0)
  expect(Number.parseFloat(await path.evaluate(element => getComputedStyle(element).strokeDashoffset))).toBe(0)
  await page.getByRole('heading', { level: 1 }).hover()
  await host.hover()
  expect(await animationCount(icon)).toBeGreaterThan(0)
})

test('keyboard and nearest host work while disabled and read-only icons stay quiet', async ({ page }) => {
  await page.goto('/icon-motion.html')
  await page.keyboard.press('Tab')
  const keyboard = page.getByTestId('keyboard-host')
  await expect(keyboard).toBeFocused()
  await expect(keyboard.locator('svg')).toHaveAttribute('data-icon-motion', 'once')
  await page.getByTestId('dynamic-wrapper').evaluate(element => element.setAttribute('aria-disabled', 'true'))
  await expect(keyboard.locator('svg')).toHaveAttribute('data-icon-motion', 'idle')
  expect(await animationCount(keyboard.locator('svg'))).toBe(0)
  await page.getByTestId('dynamic-wrapper').evaluate(element => element.removeAttribute('aria-disabled'))
  const disabled = page.getByTestId('disabled-host')
  await disabled.hover({ force: true })
  expect(await animationCount(disabled.locator('svg'))).toBe(0)
  await page.getByTestId('readonly').hover()
  expect(await animationCount(page.getByTestId('readonly').locator('svg'))).toBe(0)
  await page.getByTestId('inner-host').hover()
  await expect(page.getByTestId('inner-host').locator('svg')).toHaveAttribute('data-icon-motion', 'once')
  expect(await animationCount(page.getByTestId('outer-icon'))).toBe(0)
})

test('copy, delete, folder, terminal and workflow animate their parts without moving their frames', async ({ page }) => {
  await page.goto('/icon-motion.html')
  const samples = [
    { name: 'copy', moving: '.front', fixed: null },
    { name: 'trash', moving: '.lid', fixed: '.bin' },
    { name: 'folder', moving: '.front', fixed: 'path:not(.front)' },
    { name: 'terminal', moving: '.prompt,.cursor', fixed: 'rect' },
    { name: 'workflow', moving: '.signal', fixed: 'path:not(.signal)' },
  ]
  for (const sample of samples) {
    const host = page.getByTestId(`icon-${sample.name}`)
    await host.hover()
    const result = await host.locator('svg').evaluate((root, selectors) => {
      const animations = root.getAnimations({ subtree: true })
      animations.forEach(animation => animation.pause())
      const read = (selector: string) => Array.from(root.querySelectorAll(selector)).map(element => {
        const style = getComputedStyle(element)
        return [style.transform, style.opacity, style.fillOpacity, style.strokeDashoffset, style.d].join('|')
      }).join(';')
      const moving: string[] = []
      const fixed: string[] = []
      for (const fraction of [0, .2, .45, .7, .95]) {
        animations.forEach(animation => { animation.currentTime = Number(animation.effect!.getTiming().duration) * fraction })
        moving.push(read(selectors.moving))
        if (selectors.fixed) fixed.push(read(selectors.fixed))
      }
      return { moving, fixed }
    }, sample)
    expect(new Set(result.moving).size, `${sample.name} moving part`).toBeGreaterThan(1)
    if (sample.fixed) expect(new Set(result.fixed).size, `${sample.name} fixed frame`).toBe(1)
  }
})

test('only live busy states loop, events are explicit, and reduced motion stops everything', async ({ page }) => {
  await page.goto('/icon-motion.html')
  const busy = page.getByTestId('busy-host').locator('svg')
  expect(await animationCount(busy)).toBe(0)
  await page.getByTestId('toggle-busy').click()
  await expect(busy).toHaveAttribute('data-icon-motion', 'busy')
  expect(await animationCount(busy)).toBeGreaterThan(0)
  const bell = page.getByTestId('event-bell').locator('svg')
  const check = page.getByTestId('event-check').locator('svg')
  expect(await animationCount(bell)).toBe(0)
  await page.getByTestId('new-event').click()
  await expect(bell).toHaveAttribute('data-icon-motion', 'once')
  expect(await animationCount(bell)).toBeGreaterThan(0)
  expect(await animationCount(check)).toBe(0)
  await page.getByTestId('toggle-busy').click()
  expect(await animationCount(busy)).toBe(0)
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.getByTestId('icon-copy').hover()
  await page.getByTestId('toggle-busy').click()
  expect(await page.locator('svg.app-icon').evaluateAll(elements => elements.every(element => element.getAnimations({ subtree: true }).length === 0))).toBe(true)
  expect(Number.parseFloat(await check.locator('path').evaluate(element => getComputedStyle(element).strokeDashoffset))).toBe(0)
})

test('canvas dragging stops decorative feedback and reentry can play again', async ({ page }) => {
  await page.goto('/icon-motion.html')
  const host = page.getByTestId('keyboard-host')
  const wrapper = page.getByTestId('dynamic-wrapper')
  await host.hover()
  await expect(host.locator('svg')).toHaveAttribute('data-icon-motion', 'once')
  await wrapper.evaluate(element => { element.className = 'vue-flow__node dragging' })
  await expect(host.locator('svg')).toHaveAttribute('data-icon-motion', 'idle')
  expect(await animationCount(host.locator('svg'))).toBe(0)
  await wrapper.evaluate(element => { element.className = 'vue-flow__node' })
  await page.getByRole('heading', { level: 1 }).hover()
  await host.hover()
  await expect(host.locator('svg')).toHaveAttribute('data-icon-motion', 'once')
})

test('native sandbox and injected icons share single-play, keyboard, nearest-host and reduced-motion rules', async ({ page }) => {
  await page.goto('/icon-motion.html')
  const keyboard = page.getByTestId('native-keyboard')
  const icon = keyboard.locator('svg')
  expect(await animationCount(icon)).toBe(0)
  await page.getByTestId('native-before').focus()
  await page.keyboard.press('Tab')
  await expect(keyboard).toBeFocused()
  await expect(icon).toHaveAttribute('data-icon-motion', 'once')
  expect(await animationCount(icon)).toBeGreaterThan(0)
  await expect.poll(() => animationCount(icon), { timeout: 2500 }).toBe(0)
  await page.keyboard.press('Tab')
  await keyboard.hover()
  expect(await animationCount(icon)).toBeGreaterThan(0)
  await page.getByTestId('native-before').hover()
  await keyboard.hover()
  expect(await animationCount(icon)).toBeGreaterThan(0)
  await page.getByTestId('native-disabled').hover({ force: true })
  expect(await animationCount(page.getByTestId('native-disabled').locator('svg'))).toBe(0)
  await page.getByTestId('native-before').click()
  const outer = page.getByTestId('native-outer')
  const outerIcon = outer.locator(':scope > svg')
  const inner = page.getByTestId('native-inner')
  await outer.hover({ position: { x: 5, y: 5 } })
  expect(await animationCount(outerIcon)).toBeGreaterThan(0)
  await inner.hover()
  expect(await animationCount(inner.locator('svg'))).toBeGreaterThan(0)
  expect(await animationCount(outerIcon)).toBe(0)
  await outer.hover({ position: { x: 5, y: 5 } })
  expect(await animationCount(outerIcon)).toBeGreaterThan(0)
  expect(await animationCount(inner.locator('svg'))).toBe(0)
  const busy = page.getByTestId('native-busy').locator('svg')
  await busy.hover()
  await keyboard.hover()
  await expect(busy).toHaveAttribute('data-icon-motion', 'busy')
  expect(await animationCount(busy)).toBeGreaterThan(0)
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await keyboard.hover()
  expect(await page.getByTestId('native-icons').locator('svg').evaluateAll(elements => elements.every(element => element.getAnimations({ subtree: true }).length === 0))).toBe(true)
})
