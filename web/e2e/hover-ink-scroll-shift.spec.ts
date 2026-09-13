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

    // Click through 待办 / 运行 / 设置 and assert alignment each time.
    const paths = ['/gates', '/runs', '/settings'] as const
    const pageIds = ['page-gates', 'page-runs', 'page-settings'] as const
    for (let i = 0; i < paths.length; i++) {
      const link = page.locator(`[data-testid="nav-workspace-chrome"] a.nav-item[href="${paths[i]}"]`)
      // memory history may use data-to; fall back to text order
      const target =
        (await link.count()) > 0
          ? link
          : page.locator('[data-testid="nav-workspace-chrome"] a.nav-item').nth(i + 1)
      const b = await target.boundingBox()
      expect(b).toBeTruthy()
      await page.mouse.click(b!.x + 2, b!.y + b!.height / 2)
      await expect(page.getByTestId(pageIds[i])).toBeVisible({ timeout: PAGE_READY_MS })
      await page.waitForTimeout(100)

      const m = await measure()
      expect(m).not.toBeNull()
      if (paths[i] === '/settings') {
        // settings chrome replaces workspace nav — skip workspace measure
        await page.screenshot({
          path: path.join(shotDir, `hover-ink-after-${paths[i].slice(1)}.png`),
          fullPage: false,
        })
        continue
      }
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

    // Settings chrome: back-home left-edge hover must not create scroll-origin shift.
    // Allow slightly looser absolute delta than workspace (chrome slide may still settle),
    // but overflow:clip + scrollLeft=0 is the hard contract for plan g1.2 / g2.1.
    await expect(page.getByTestId('nav-settings-chrome')).toBeVisible()
    await page.waitForTimeout(350)
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
    await page.mouse.move(backBox!.x + 2, backBox!.y + backBox!.height / 2)
    await page.waitForTimeout(120)
    const backAfter = await back.evaluate((el) => {
      const icon = el.querySelector('svg')
      if (!icon) throw new Error('back-home svg missing')
      const host = el.getBoundingClientRect()
      return {
        offset: icon.getBoundingClientRect().left - host.left,
        scrollLeft: (el as HTMLElement).scrollLeft,
      }
    })
    expect(backAfter.scrollLeft).toBe(0)
    expect(Math.abs(backAfter.offset - backBefore.offset)).toBeLessThanOrEqual(1)

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
            margin: calc(var(--ink-d, 0px) / -2) 0 0 calc(var(--ink-d, 0px) / -2);
            border-radius: 50%;
            background: rgb(var(--c-elevated));
            transform: scale(0);
            transition: transform 350ms var(--ease-out-expo);
            pointer-events: none;
            z-index: -1;
          }
          .hover-ink-host.is-hover-ink > .hover-ink { transform: scale(1); }
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
          const ink = btn.querySelector('.hover-ink');
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
            btn.classList.add('is-hover-ink');
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

    await page.screenshot({
      path: path.join(shotDir, 'hover-ink-appbutton-left.png'),
      fullPage: false,
    })
  })
})
