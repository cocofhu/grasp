import { expect, test } from '@playwright/test'

/**
 * plan g1.2 / review v1 — Chromium 149.
 * The browser captures the old snapshot before the update callback. At that
 * callback's first line the sun must still be active and html must still be
 * dark; after the callback (and nextTick) the moon and html.light are what
 * the new snapshot records. Animations are then paused at ready, t = 0.
 */

test.describe('theme icon view transition (plan g1.2 review v1)', () => {
  test('old snapshot is the previous icon and theme; end state is the target', async ({ page }) => {
    test.setTimeout(90_000)
    await page.setViewportSize({ width: 1280, height: 800 })
    await page.addInitScript(() => {
      localStorage.setItem('grasp-theme', 'dark')
    })
    await page.goto('/shell-loading.html')
    const toggle = page.getByTestId('shell-theme-toggle')
    await expect(toggle).toBeVisible({ timeout: 30_000 })
    await expect(page.locator('html')).not.toHaveClass(/light/)
    await expect(page.getByTestId('shell-theme-icon-sun')).toHaveClass(/is-active/)
    await expect(page.getByTestId('shell-theme-icon-moon')).not.toHaveClass(/is-active/)

    await page.evaluate(() => {
      const orig = document.startViewTransition.bind(document)
      const root = document.documentElement
      const iconState = () => ({
        light: root.classList.contains('light'),
        sun: document.querySelector('[data-testid="shell-theme-icon-sun"]')?.classList.contains('is-active') === true,
        moon: document.querySelector('[data-testid="shell-theme-icon-moon"]')?.classList.contains('is-active') === true,
        capture: root.classList.contains('theme-vt-capture'),
      })
      document.startViewTransition = (cb) => {
        const vt = orig(async () => {
          ;(window as unknown as { __vtAtStart: unknown }).__vtAtStart = iconState()
          await cb()
          ;(window as unknown as { __vtAtEnd: unknown }).__vtAtEnd = iconState()
        })
        void vt.ready.then(() => {
          for (const anim of document.getAnimations()) {
            anim.pause()
            try {
              anim.currentTime = 0
            } catch {
              /* some group timings reject; pause still holds the first frame */
            }
          }
          const anims = document.getAnimations().map((anim) => {
            const effect = anim.effect as KeyframeEffect | null
            const timing = effect?.getComputedTiming()
            const frames = effect?.getKeyframes?.() ?? []
            return {
              pseudo: effect?.pseudoElement ?? '',
              name: anim.animationName,
              duration: timing?.duration ?? null,
              currentTime: anim.currentTime,
              frames: frames.map((frame) => `${frame.transform ?? ''} ${frame.opacity ?? ''}`),
            }
          })
          const iconKeyframes: { name: string; css: string }[] = []
          for (const sheet of document.styleSheets) {
            let rules: CSSRuleList
            try {
              rules = sheet.cssRules
            } catch {
              continue
            }
            for (const rule of rules) {
              if (rule instanceof CSSKeyframesRule && rule.name.includes('theme-icon')) {
                iconKeyframes.push({ name: rule.name, css: rule.cssText })
              }
            }
          }
          const slot = window as unknown as {
            __vtAnims: unknown
            __vtIconKeyframes: unknown
            __vtReady: boolean
          }
          slot.__vtAnims = anims
          slot.__vtIconKeyframes = iconKeyframes
          slot.__vtReady = true
        })
        return vt
      }
    })

    await toggle.click()
    await page.waitForFunction(() => (window as unknown as { __vtReady?: boolean }).__vtReady === true, null, {
      timeout: 10_000,
    })

    const recorded = await page.evaluate(() => {
      const w = window as unknown as {
        __vtAtStart: { light: boolean; sun: boolean; moon: boolean; capture: boolean }
        __vtAtEnd: { light: boolean; sun: boolean; moon: boolean; capture: boolean }
        __vtAnims: {
          pseudo: string
          name: string
          duration: number | null
          currentTime: number | null
          frames: string[]
        }[]
        __vtIconKeyframes: { name: string; css: string }[]
      }
      return {
        atStart: w.__vtAtStart,
        atEnd: w.__vtAtEnd,
        anims: w.__vtAnims,
        iconKeyframes: w.__vtIconKeyframes,
      }
    })

    // Old snapshot already taken: previous sun, page still dark, capture class on.
    expect(recorded.atStart).toEqual({ light: false, sun: true, moon: false, capture: true })
    // New snapshot sees the destination icon and theme. Capture still covers both shots.
    expect(recorded.atEnd).toEqual({ light: true, sun: false, moon: true, capture: true })

    const oldShots = recorded.anims.filter((a) => a.pseudo.includes('view-transition-old'))
    expect(oldShots.length).toBeGreaterThan(0)
    expect(oldShots.every((a) => a.currentTime === 0)).toBe(true)
    const iconSpins = recorded.anims.filter((a) => `${a.name ?? ''} ${a.pseudo}`.includes('theme-icon'))
    expect(iconSpins, JSON.stringify(recorded.anims)).not.toEqual([])
    expect(iconSpins.every((a) => a.duration === 280), JSON.stringify(iconSpins)).toBe(true)
    expect(recorded.iconKeyframes.map((k) => k.name).sort()).toEqual([
      'theme-icon-moon-in',
      'theme-icon-moon-out',
      'theme-icon-sun-in',
      'theme-icon-sun-out',
    ])
    expect(
      recorded.iconKeyframes.every((k) => /rotate\((?:-)?90deg\)/.test(k.css) && /scale\(0\.55\)/.test(k.css)),
      JSON.stringify(recorded.iconKeyframes),
    ).toBe(true)
    expect(recorded.iconKeyframes.every((k) => !/translate|scale\(0\.98\)/.test(k.css))).toBe(true)

    const rootShots = recorded.anims.filter((a) => a.pseudo.includes('(root)'))
    expect(rootShots.length, JSON.stringify(recorded.anims)).toBeGreaterThan(0)
    expect(rootShots.every((a) => a.duration === 200), JSON.stringify(rootShots)).toBe(true)
    expect(
      rootShots.every((a) => a.frames.every((frame) => !/translate|scale\(/.test(frame))),
      JSON.stringify(rootShots),
    ).toBe(true)

    await expect(page.getByTestId('shell-theme-icon-moon')).toHaveClass(/is-active/)
    await expect(page.getByTestId('shell-theme-icon-sun')).not.toHaveClass(/is-active/)
    await expect(page.locator('html')).toHaveClass(/light/)
    expect(await page.evaluate(() => localStorage.getItem('grasp-theme'))).toBe('light')
  })

  test('two clicks within 100ms return to the starting theme', async ({ page }) => {
    test.setTimeout(90_000)
    await page.setViewportSize({ width: 1280, height: 800 })
    await page.addInitScript(() => {
      localStorage.setItem('grasp-theme', 'dark')
    })
    await page.goto('/shell-loading.html')
    const toggle = page.getByTestId('shell-theme-toggle')
    await expect(toggle).toBeVisible({ timeout: 30_000 })
    await expect(page.locator('html')).not.toHaveClass(/light/)

    const box = await toggle.boundingBox()
    expect(box).toBeTruthy()
    const x = box!.x + box!.width / 2
    const y = box!.y + box!.height / 2

    await page.evaluate(() => {
      const times: number[] = []
      document.addEventListener(
        'pointerdown',
        () => {
          times.push(performance.now())
        },
        true,
      )
      ;(window as unknown as { __themePointerDowns: number[] }).__themePointerDowns = times
    })

    await page.mouse.click(x, y)
    await page.waitForTimeout(50)
    await page.mouse.click(x, y)
    const gap = await page.evaluate(() => {
      const times = (window as unknown as { __themePointerDowns: number[] }).__themePointerDowns
      return times.length >= 2 ? times[1] - times[0] : Number.POSITIVE_INFINITY
    })
    expect(gap, `pointerdown gap ${gap}ms`).toBeLessThan(100)
    await page.waitForTimeout(400)

    await expect(page.locator('html')).not.toHaveClass(/light/)
    await expect(page.getByTestId('shell-theme-icon-sun')).toHaveClass(/is-active/)
    await expect(page.getByTestId('shell-theme-icon-moon')).not.toHaveClass(/is-active/)
    expect(await page.evaluate(() => localStorage.getItem('grasp-theme'))).toBe('dark')
  })
})
