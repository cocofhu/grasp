import type { Page, Route } from '@playwright/test'

/**
 * True only for backend HTTP paths (/api/...), never Vite modules
 * such as /src/lib/api/clients/runsClient.ts that collide with loose
 * Playwright globs matching the substring "/api/runs" inside module URLs.
 */
function isBackendApiRequest(url: string): boolean {
  try {
    return new URL(url).pathname.startsWith('/api/')
  } catch {
    return false
  }
}

/**
 * page.route wrapper: skip non-/api/ pathnames so module scripts are not
 * fulfilled as application/json (MIME / white-screen regression).
 */
export async function routeApi(
  page: Page,
  url: string | RegExp,
  handler: (route: Route) => Promise<void>,
): Promise<void> {
  await page.route(url, async (route) => {
    if (!isBackendApiRequest(route.request().url())) {
      await route.continue()
      return
    }
    await handler(route)
  })
}
