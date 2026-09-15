import { test, expect } from '@playwright/test'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const shotDir = path.join(__dirname, '..', '..', 'test-screenshots')

test.describe('preflight UI acceptance', () => {
  test('FormCard plaintext + no asterisk + hide confirm; PreflightView shows password', async ({
    page,
  }) => {
    await page.goto('/preflight-ui-harness.html')
    await expect(page.getByTestId('preflight-ux-root')).toBeVisible({ timeout: 15_000 })

    const formCard = page.getByTestId('clarify-form-card')
    await expect(formCard).toBeVisible()

    // Labels visible; internal field names not shown as primary label text
    await expect(formCard.getByText('测试环境地址')).toBeVisible()
    await expect(formCard.getByText('数据库密码')).toBeVisible()
    await expect(formCard.getByText('test_env_url')).toHaveCount(0)
    await expect(formCard.getByText('db_password')).toHaveCount(0)

    // No required asterisk near labels
    const fieldLabels = formCard.locator('[data-testid="clarify-form-field"]')
    await expect(fieldLabels).toHaveCount(2)
    for (const el of await fieldLabels.all()) {
      const text = await el.innerText()
      expect(text).not.toMatch(/\*/)
    }

    // Password field is plaintext text input, not type=password
    const inputs = formCard.locator('[data-testid="clarify-form-input"]')
    await expect(inputs).toHaveCount(2)
    await expect(inputs.nth(0)).toHaveAttribute('type', 'url')
    await expect(inputs.nth(1)).toHaveAttribute('type', 'text')
    await expect(formCard.locator('input[type="password"]')).toHaveCount(0)

    // Hover why on label
    const pwLabel = formCard.locator('[data-testid="clarify-form-field"]').nth(1).locator('label, .label, span').first()
    // Title attribute on field wrapper / label for why
    const whyHost = formCard.locator('[data-testid="clarify-form-field"]').nth(1)
    const titleAttr = await whyHost.getAttribute('title')
    const whyFromChild = await whyHost.locator('[title]').first().getAttribute('title').catch(() => null)
    const why = titleAttr || whyFromChild || ''
    expect(why).toContain('明文凭据')

    // Confirm & continue hidden for preflight
    await expect(page.getByTestId('clarify-confirm-flow')).toHaveCount(0)
    await expect(page.getByText('确认并流转')).toHaveCount(0)

    // Fill and submit
    await inputs.nth(0).fill('http://127.0.0.1:18080')
    await inputs.nth(1).fill('s3cret!')
    await page.getByTestId('clarify-form-submit').click()

    // Product view plaintext password
    const product = page.getByTestId('preflight-view')
    await expect(product).toBeVisible()
    await expect(product.getByTestId('preflight-confirmed')).toContainText(/已确认|confirmed/i)
    await expect(product).toContainText('s3cret!')
    await expect(product).toContainText('http://127.0.0.1:18080')

    await page.screenshot({
      path: path.join(shotDir, 'preflight-form-card.png'),
      fullPage: true,
    })

    // Expand raw JSON
    await page.getByTestId('preflight-raw-toggle').click()
    await expect(page.getByTestId('preflight-raw-json')).toContainText('s3cret!')
    await page.screenshot({
      path: path.join(shotDir, 'preflight-product-view.png'),
      fullPage: true,
    })
  })
})
