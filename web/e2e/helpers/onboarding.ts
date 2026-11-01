import type { Page } from '@playwright/test'

/** localStorage key read by isOnboardingSuppressed. */
export const ONBOARDING_SUPPRESS_KEY = 'grasp-onboarding-suppress'

/**
 * Seed dismiss flag before navigation so stubs without the default workflow do
 * not auto-open the onboarding overlay and intercept pointer events in
 * unrelated e2e suites. Dismissing the wizard writes the same key.
 */
export async function seedOnboardingDismissed(page: Page): Promise<void> {
  await page.addInitScript((key: string) => {
    try {
      localStorage.setItem(key, '1')
    } catch {
      /* ignore */
    }
  }, ONBOARDING_SUPPRESS_KEY)
}

/**
 * If the onboarding wizard is already open, dismiss via backdrop click.
 * Safe no-op when the overlay is absent.
 */
export async function dismissOnboardingIfOpen(page: Page): Promise<void> {
  const backdrop = page.getByTestId('onboarding-backdrop')
  if ((await backdrop.count()) === 0) return
  if (!(await backdrop.first().isVisible().catch(() => false))) return
  await backdrop.first().click({ force: true })
  await page.getByTestId('onboarding-backdrop').waitFor({ state: 'hidden', timeout: 5_000 }).catch(() => {})
}
