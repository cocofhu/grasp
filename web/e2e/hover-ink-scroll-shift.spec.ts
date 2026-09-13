import { expect, test, type Page } from '@playwright/test'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { mkdirSync } from 'node:fs'

const shotDir = path.join(path.dirname(fileURLToPath(import.meta.url)), '../../../test-screenshots')
mkdirSync(shotDir, { recursive: true })

const PAGE_READY_MS = 30_000

async function mockApi(page: Page) {
  await page.route('**/api/**', async (route) => {
    if (!new URL(route.request().url()).pathname.startsWith('/api/')) {
      await route.continue()
      return
    }
    const p = new URL(route.request().url()).pathname
    if (p.includes('/gates')) {
      await route.fulfill({ json: { items: [], total: 0 } })
      return
    }
    if (p.includes('/workflows')) {
      await route.fulfill({ json: [] })
      return
    }
    if (p.includes('/runs')) {
      await route.fulfill({ json: { items: [], total: 0, page: 1, pageSize: 20, hasMore: false } })
      return
    }
    if (p.includes('/health') || p.includes('/live')) {
      await route.fulfill({ json: { status: 'ok', ready: true } })
      return
    }
    if (p.includes('/platform') || p.includes('/status')) {
      await route.fulfill({
        json: {
          cumulativeTokens: null,
          todayTokens: null,
          runningCount: 0,
          queuedCount: 0,
          asOf: '2026-08-19T00:00:00Z',
          timezone: 'UTC',
        },
      })
      return
    }
    await route.fulfill({ json: {} })
  })
}

test.describe('hover-ink scroll-shift fix', () => {
  test('workspace nav icons stay left-aligned after left-edge click/hover', async ({ page }) => {
    await mockApi(page)
    await page.setViewportSize({ width: 1280, height: 800 })
    await page.goto('/hover-ink-scroll-shift.html?start=/dashboard')
    await expect(page.getByTestId('page-dashboard')).toBeVisible({ timeout: PAGE_READY_MS })
    await expect(page.getByTestId('nav-workspace-chrome')).toBeVisible()

    const measure = async () =>
      page.evaluate(() => {
        const chrome = document.querySelector('[data-testid="nav-workspace-chrome"]')
        if (!chrome) return null
        const links = [...chrome.querySelectorAll('a.nav-item')] as HTMLAnchorElement[]
        return links.map((a) => {
          const icon = a.querySelector('svg, [class*="icon"], i') as HTMLElement | null
          const target = icon ?? (a.children[0] as HTMLElement) ?? a
          const r = target.getBoundingClientRect()
          return {
            href: a.getAttribute('href') || a.getAttribute('data-to') || '',
            text: (a.textContent || '').trim(),
            active: a.classList.contains('active'),
            iconLeft: r.left,
            scrollLeft: a.scrollLeft,
            overflow: getComputedStyle(a).overflow,
            inkD: a.style.getPropertyValue('--ink-d'),
          }
        })
      })

    const before = await measure()
    expect(before).not.toBeNull()
    expect(before!.length).toBeGreaterThanOrEqual(4)
    expect(before!.every((x) => x.overflow === 'clip')).toBe(true)

    const active = before!.find((x) => x.active)
    const idle = before!.find((x) => !x.active)
    expect(active).toBeTruthy()
    expect(idle).toBeTruthy()
    expect(Math.abs(active!.iconLeft - idle!.iconLeft)).toBeLessThanOrEqual(1)

    await page.screenshot({
      path: path.join(shotDir, 'hover-ink-dashboard-active.png'),
      fullPage: false,
    })

    // Left-edge pointerenter on active「开始」— max left overflow for ink circle.
    const dashboardLink = page.locator('[data-testid="nav-workspace-chrome"] a.nav-item').first()
    const box = await dashboardLink.boundingBox()
    expect(box).toBeTruthy()
    await page.mouse.move(box!.x + 2, box!.y + box!.height / 2)
    await page.waitForTimeout(80)

    const afterHover = await measure()
    expect(afterHover).not.toBeNull()
    expect(afterHover!.every((x) => x.scrollLeft === 0)).toBe(true)
    const activeAfter = afterHover!.find((x) => x.active)!
    const idleAfter = afterHover!.find((x) => !x.active)!
    expect(Math.abs(activeAfter.iconLeft - idleAfter.iconLeft)).toBeLessThanOrEqual(1)
    expect(Math.abs(activeAfter.iconLeft - active!.iconLeft)).toBeLessThanOrEqual(1)

    await page.screenshot({
      path: path.join(shotDir, 'hover-ink-dashboard-left-hover.png'),
      fullPage: false,
    })

    // plan g2.1: click 「运行」, pointerleave, icon left vs 「开始」 ≤ 1px (real geometry).
    const runsLink = page.locator('[data-testid="nav-workspace-chrome"] a.nav-item').nth(2)
    const runsBox = await runsLink.boundingBox()
    expect(runsBox).toBeTruthy()
    await page.mouse.click(runsBox!.x + 2, runsBox!.y + runsBox!.height / 2)
    await expect(page.getByTestId('page-runs')).toBeVisible({ timeout: PAGE_READY_MS })
    await page.waitForTimeout(100)
    // Leave the item so ink retracts; sticky shift must not remain.
    await page.mouse.move(runsBox!.x + runsBox!.width + 40, runsBox!.y + runsBox!.height / 2)
    await page.waitForTimeout(400)

    const afterRunsLeave = await measure()
    expect(afterRunsLeave).not.toBeNull()
    const runsRow = afterRunsLeave!.find((x) => x.active)
    const homeRow = afterRunsLeave!.find((x) => !x.active)
    expect(runsRow).toBeTruthy()
    expect(homeRow).toBeTruthy()
    expect(Math.abs(runsRow!.iconLeft - homeRow!.iconLeft)).toBeLessThanOrEqual(1)
    expect(runsRow!.inkD === '' || runsRow!.inkD === '0px').toBe(true)

    await page.screenshot({
      path: path.join(shotDir, 'hover-ink-after-runs-leave.png'),
      fullPage: false,
    })

    // Click through 待办 / 设置; leave after each click (sticky-shift contract).
    const paths = ['/gates', '/settings'] as const
    const pageIds = ['page-gates', 'page-settings'] as const
    for (let i = 0; i < paths.length; i++) {
      const link = page.locator(`[data-testid="nav-workspace-chrome"] a.nav-item[href="${paths[i]}"]`)
      const target =
        (await link.count()) > 0
          ? link
          : page.locator('[data-testid="nav-workspace-chrome"] a.nav-item').nth(paths[i] === '/gates' ? 1 : 3)
      const b = await target.boundingBox()
      expect(b).toBeTruthy()
      await page.mouse.click(b!.x + 2, b!.y + b!.height / 2)
      await expect(page.getByTestId(pageIds[i])).toBeVisible({ timeout: PAGE_READY_MS })
      await page.waitForTimeout(100)

      if (paths[i] === '/settings') {
        // settings chrome replaces workspace nav — skip workspace measure
        await page.screenshot({
          path: path.join(shotDir, `hover-ink-after-${paths[i].slice(1)}.png`),
          fullPage: false,
        })
        continue
      }

      await page.mouse.move(b!.x + b!.width + 40, b!.y + b!.height / 2)
      await page.waitForTimeout(400)

      const m = await measure()
      expect(m).not.toBeNull()
      expect(m!.every((x) => x.scrollLeft === 0)).toBe(true)
      const act = m!.find((x) => x.active)
      const idl = m!.find((x) => !x.active)
      expect(act).toBeTruthy()
      expect(idl).toBeTruthy()
      expect(Math.abs(act!.iconLeft - idl!.iconLeft)).toBeLessThanOrEqual(1)
      await page.screenshot({
        path: path.join(shotDir, `hover-ink-after-${paths[i].slice(1)}.png`),
        fullPage: false,
      })
    }

    // plan g2.2: settings chrome — click General, leave, real icon offset ≤ 1px vs siblings / self.
    await expect(page.getByTestId('nav-settings-chrome')).toBeVisible()
    await page.waitForTimeout(350)

    const measureSettings = async () =>
      page.evaluate(() => {
        const chrome = document.querySelector('[data-testid="nav-settings-chrome"]')
        if (!chrome) return null
        // Exclude back-home: different density/classes; compare category rows only.
        const links = [...chrome.querySelectorAll('a.nav-item:not([data-testid="nav-back-home"])')] as HTMLAnchorElement[]
        return links.map((a) => {
          const icon = a.querySelector('svg, [class*="icon"], i') as HTMLElement | null
          if (!icon) return null
          const host = a.getBoundingClientRect()
          const ir = icon.getBoundingClientRect()
          return {
            text: (a.textContent || '').trim(),
            active: a.classList.contains('active'),
            iconLeft: ir.left,
            offset: ir.left - host.left,
            scrollLeft: a.scrollLeft,
            inkD: a.style.getPropertyValue('--ink-d'),
          }
        }).filter(Boolean) as Array<{
          text: string
          active: boolean
          iconLeft: number
          offset: number
          scrollLeft: number
          inkD: string
        }>
      })

    const beforeSettings = await measureSettings()
    expect(beforeSettings).not.toBeNull()
    expect(beforeSettings!.length).toBeGreaterThanOrEqual(2)
    const baselineOffset = beforeSettings![0].offset
    expect(beforeSettings!.every((x) => Math.abs(x.offset - baselineOffset) <= 1)).toBe(true)

    const general = page.locator('[data-testid="nav-settings-chrome"] a.nav-item').filter({ hasText: '通用' })
    const gBox = await general.boundingBox()
    expect(gBox).toBeTruthy()
    await page.mouse.click(gBox!.x + 2, gBox!.y + gBox!.height / 2)
    await page.waitForTimeout(100)
    await page.mouse.move(gBox!.x + gBox!.width + 40, gBox!.y + gBox!.height / 2)
    await page.waitForTimeout(400)
    const afterGeneral = await measureSettings()
    expect(afterGeneral).not.toBeNull()
    const clicked = afterGeneral!.find((x) => x.text.includes('通用')) ?? afterGeneral!.find((x) => x.active)
    expect(clicked).toBeTruthy()
    expect(Math.abs(clicked!.offset - baselineOffset)).toBeLessThanOrEqual(1)
    expect(afterGeneral!.every((x) => Math.abs(x.offset - baselineOffset) <= 1)).toBe(true)
    expect(clicked!.inkD === '' || clicked!.inkD === '0px').toBe(true)

    const back = page.getByTestId('nav-back-home')
    const backBox = await back.boundingBox()
    expect(backBox).toBeTruthy()
    const backBefore = await back.evaluate((el) => {
      const icon = el.querySelector('svg')
      if (!icon) throw new Error('back-home svg missing')
      const host = el.getBoundingClientRect()
      return {
        offset: icon.getBoundingClientRect().left - host.left,
        scrollLeft: (el as HTMLElement).scrollLeft,
        overflow: getComputedStyle(el).overflow,
      }
    })
    expect(backBefore.overflow).toBe('clip')
    // Left-edge hover then leave without navigating away (move back into chrome first).
    await page.mouse.move(backBox!.x + 2, backBox!.y + backBox!.height / 2)
    await page.waitForTimeout(80)
    await page.mouse.move(backBox!.x + backBox!.width + 40, backBox!.y + backBox!.height / 2)
    await page.waitForTimeout(400)
    const backAfterLeave = await back.evaluate((el) => {
      const icon = el.querySelector('svg')
      if (!icon) throw new Error('back-home svg missing')
      const host = el.getBoundingClientRect()
      return {
        offset: icon.getBoundingClientRect().left - host.left,
        scrollLeft: (el as HTMLElement).scrollLeft,
        inkD: (el as HTMLElement).style.getPropertyValue('--ink-d'),
      }
    })
    expect(backAfterLeave.scrollLeft).toBe(0)
    expect(Math.abs(backAfterLeave.offset - backBefore.offset)).toBeLessThanOrEqual(1)
    expect(backAfterLeave.inkD === '' || backAfterLeave.inkD === '0px').toBe(true)

    await page.screenshot({
      path: path.join(shotDir, 'hover-ink-settings-chrome.png'),
      fullPage: false,
    })
  })

  test('AppButton left-edge ink does not shift label', async ({ page }) => {
    await page.setViewportSize({ width: 800, height: 400 })
    await page.setContent(`
      <!DOCTYPE html>
      <html>
      <head>
        <style>
          :root { --c-elevated: 40 40 48; --ease-out-expo: cubic-bezier(0.16, 1, 0.3, 1); }
          body { margin: 40px; font-family: system-ui; background: #111; color: #eee; }
          .hover-ink-host {
            position: relative;
            overflow: clip;
            isolation: isolate;
            display: inline-flex;
            align-items: center;
            gap: 8px;
            padding: 10px 16px;
            border-radius: 10px;
            border: 1px solid #444;
            background: #1a1a22;
          }
          .hover-ink-host > .hover-ink {
            position: absolute;
            left: var(--ink-x, 50%);
            top: var(--ink-y, 50%);
            width: var(--ink-d, 0px);
            height: var(--ink-d, 0px);
            margin: 0;
            border-radius: 50%;
            background: rgb(var(--c-elevated));
            transform: translate(-50%, -50%) scale(0);
            transform-origin: center;
            transition: transform 350ms var(--ease-out-expo);
            pointer-events: none;
            z-index: -1;
          }
          .hover-ink-host.is-hover-ink > .hover-ink { transform: translate(-50%, -50%) scale(1); }
          .hover-ink-host.is-leave-ink > .hover-ink { transform: translate(-50%, -50%) scale(0); }
          .hover-ink-host > :not(.hover-ink) { position: relative; z-index: 1; }
        </style>
      </head>
      <body>
        <button id="btn" class="hover-ink-host" type="button">
          <span class="hover-ink"></span>
          <span id="label">Primary</span>
        </button>
        <script>
          const btn = document.getElementById('btn');
          btn.addEventListener('pointerenter', (e) => {
            const r = btn.getBoundingClientRect();
            const x = e.clientX - r.left;
            const y = e.clientY - r.top;
            const d = Math.ceil(2 * Math.max(
              Math.hypot(x, y),
              Math.hypot(r.width - x, y),
              Math.hypot(x, r.height - y),
              Math.hypot(r.width - x, r.height - y),
            ));
            btn.style.setProperty('--ink-x', x + 'px');
            btn.style.setProperty('--ink-y', y + 'px');
            btn.style.setProperty('--ink-d', d + 'px');
            btn.classList.remove('is-leave-ink');
            btn.classList.add('is-hover-ink');
          });
          btn.addEventListener('pointerleave', () => {
            btn.classList.remove('is-hover-ink');
            btn.classList.add('is-leave-ink');
            setTimeout(() => {
              if (!btn.classList.contains('is-hover-ink')) btn.style.setProperty('--ink-d', '0px');
            }, 350);
          });
        </script>
      </body>
      </html>
    `)

    const btn = page.locator('#btn')
    const label = page.locator('#label')
    const before = await label.evaluate((el) => el.getBoundingClientRect().left)
    const box = await btn.boundingBox()
    expect(box).toBeTruthy()
    await page.mouse.move(box!.x + 2, box!.y + box!.height / 2)
    await page.waitForTimeout(100)
    const after = await label.evaluate((el) => el.getBoundingClientRect().left)
    const scrollLeft = await btn.evaluate((el) => (el as HTMLElement).scrollLeft)
    expect(scrollLeft).toBe(0)
    expect(Math.abs(after - before)).toBeLessThanOrEqual(1)

    await page.mouse.move(box!.x + box!.width + 40, box!.y + box!.height / 2)
    await page.waitForTimeout(400)
    const afterLeave = await label.evaluate((el) => el.getBoundingClientRect().left)
    expect(Math.abs(afterLeave - before)).toBeLessThanOrEqual(1)
    const inkD = await btn.evaluate((el) => (el as HTMLElement).style.getPropertyValue('--ink-d'))
    expect(inkD === '' || inkD === '0px').toBe(true)

    await page.screenshot({
      path: path.join(shotDir, 'hover-ink-appbutton-left.png'),
      fullPage: false,
    })
  })
})
