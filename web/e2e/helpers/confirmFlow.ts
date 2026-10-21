import type { Page } from '@playwright/test'

/** Accept the in-app「确认流转」dialog opened by a flow button. */
export async function acceptConfirmFlow(page: Page) {
  await page.getByTestId('confirm-flow-dialog-accept').click()
}
