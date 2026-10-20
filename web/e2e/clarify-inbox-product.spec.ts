import { test, expect } from '@playwright/test'
import { mkdir } from 'node:fs/promises'

const SHOT_DIR = 'test-results/screenshots'

test.describe('GatesInbox clarify 产物台', () => {
  test.beforeAll(async () => {
    await mkdir(SHOT_DIR, { recursive: true })
  })
  test('research 预览、多产物切换与 react 澄清产物', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 })

    await page.goto('/clarify-inbox-product.html?scenario=research')
    await page.getByTestId('conversation-panel-toggle').click()
    await expect(page.getByTestId('clarify-inbox-product-root')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByTestId('clarify-product-stage')).toBeVisible()
    await expect(page.getByTestId('clarify-product-tabs')).toBeVisible()
    await expect(page.getByText('调研结论摘要：澄清产物台应对齐 Gate 载荷。')).toBeVisible()
    await expect(page.getByTestId('upstream-context')).toHaveCount(0)
    await page.screenshot({ path: `${SHOT_DIR}/06-inbox-research-no-req-doc.png`, fullPage: true })

    await page.getByTestId('clarify-product-tab-plan_1').click()
    await expect(page.getByTestId('clarify-product-panel').getByText('实施计划', { exact: true })).toBeVisible()
    await expect(page.getByText('修复 clarify 空白')).toBeVisible()

    await page.goto('/clarify-inbox-product.html?scenario=react')
    await page.getByTestId('conversation-panel-toggle').click()
    await expect(page.getByTestId('clarify-product-stage')).toBeVisible()
    await expect(page.getByText('在审批箱澄清阶段展示结构化产物。')).toBeVisible()
    await expect(page.getByTestId('clarify-product-tabs')).toHaveCount(0)
    await expect(page.getByTestId('structured-product-name')).toHaveText('clarified_requirement.json')
    await expect(page.getByTestId('upstream-context')).toHaveCount(0)
    await page.screenshot({ path: `${SHOT_DIR}/04-inbox-react-no-upstream-bar.png`, fullPage: true })
  })

  test('Inbox 视觉复审可从常驻条打开上游需求模态并读到结构化标题', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 })

    await page.goto('/clarify-inbox-product.html?scenario=visual')
    await page.getByTestId('conversation-panel-toggle').click()
    await expect(page.getByTestId('clarify-product-stage')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByTestId('structured-product-name')).toHaveText('page.html')
    await expect(page.getByTestId('structured-product-header')).not.toContainText(
      'clarified_requirement.json',
    )
    await expect(page.getByText('查看澄清需求文档')).toHaveCount(0)

    const bar = page.getByTestId('upstream-context')
    await expect(bar).toBeVisible()
    await expect(bar).toHaveAttribute('data-variant', 'persistent-bar')
    await expect(bar.getByTestId('upstream-bar-hint')).toContainText('上游上下文')
    await expect(bar.getByTestId('upstream-enlarge')).toBeInViewport()

    await page.screenshot({ path: `${SHOT_DIR}/01-inbox-visual-upstream-bar.png`, fullPage: true })

    const preview = page.getByTestId('structured-product-preview')
    await preview.evaluate((el) => {
      el.scrollTop = el.scrollHeight
    })
    await expect(bar.getByTestId('upstream-enlarge')).toBeInViewport()
    await page.screenshot({ path: `${SHOT_DIR}/02-inbox-visual-bar-after-scroll.png`, fullPage: true })

    await bar.getByTestId('upstream-enlarge').click()
    // AppModal Teleports to <body> and does not inherit root data-testid.
    const modalBody = page.getByTestId('upstream-modal-body')
    await expect(modalBody).toBeVisible()
    await expect(modalBody).toContainText('复审产物台展示上游澄清需求文档')
    await expect(page.getByTestId('upstream-modal-readonly-footer')).toContainText('只读对照')
    await expect(modalBody.getByText('↗ 标注')).toHaveCount(0)
    await expect(page.locator('body > .fixed.inset-0.z-50 .relative.flex.max-h-\\[88vh\\]')).toHaveCSS(
      'max-width',
      '960px',
    )
    await page.screenshot({ path: `${SHOT_DIR}/03-inbox-visual-upstream-modal.png`, fullPage: true })

    await page.getByTestId('upstream-modal-mode-raw').click()
    await expect(page.locator('.json-code-view--modal')).toBeVisible()
    await expect(page.locator('.json-code-view--modal')).toHaveCSS(
      'background-color',
      'rgb(30, 30, 30)',
    )
    await page.screenshot({ path: `${SHOT_DIR}/05-inbox-visual-upstream-modal-json.png`, fullPage: true })
    await page.getByTestId('upstream-modal-mode-structured').click()
    await expect(modalBody).toContainText('复审产物台展示上游澄清需求文档')

    await page.keyboard.press('Escape')
    await expect(modalBody).toBeVisible()
  })

  test('浅色主题下上游原始 JSON 不是黑底（g3.2）', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 })
    await page.goto('/clarify-inbox-product.html?scenario=visual&theme=light')
    await page.getByTestId('conversation-panel-toggle').click()
    await expect(page.getByTestId('clarify-product-stage')).toBeVisible({ timeout: 10_000 })
    await expect(page.locator('html')).toHaveClass(/light/)
    await page.getByTestId('upstream-enlarge').click()
    await page.getByTestId('upstream-modal-mode-raw').click()
    const jsonView = page.locator('.json-code-view--modal')
    await expect(jsonView).toBeVisible()
    const bg = await jsonView.evaluate((el) => getComputedStyle(el).backgroundColor)
    expect(bg).not.toBe('rgb(30, 30, 30)')
    expect(bg).toMatch(/rgb\(\s*25[0-5],\s*25[0-5],\s*25[0-5]\s*\)/)
    await page.screenshot({ path: `${SHOT_DIR}/07-inbox-visual-upstream-json-light.png`, fullPage: true })
  })

  test('三态空态、重试恢复与加载中不闪尚未执行', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 800 })

    await page.goto('/clarify-inbox-product.html?scenario=pending')
    await page.getByTestId('conversation-panel-toggle').click()
    const pendingEmpty = page.getByTestId('clarify-product-empty-pending')
    await expect(pendingEmpty).toBeVisible({ timeout: 10_000 })
    await expect(pendingEmpty.getByText('节点尚未执行', { exact: true })).toBeVisible()

    await page.goto('/clarify-inbox-product.html?scenario=executedEmpty')
    await page.getByTestId('conversation-panel-toggle').click()
    const executedEmpty = page.getByTestId('clarify-product-empty-executedEmpty')
    await expect(executedEmpty).toBeVisible()
    await expect(executedEmpty.getByText('已执行但暂无产物', { exact: true })).toBeVisible()

    await page.goto('/clarify-inbox-product.html?scenario=loadFailed')
    await page.getByTestId('conversation-panel-toggle').click()
    const loadFailed = page.getByTestId('clarify-product-empty-loadFailed')
    await expect(loadFailed).toBeVisible()
    await expect(loadFailed.getByText('无法加载产物', { exact: true })).toBeVisible()
    await expect(page.getByText('请点击重试重新加载审批上下文')).toBeVisible()
    await page.getByTestId('clarify-product-retry').click()
    await page.getByTestId('conversation-panel-toggle').click()
    await expect(page.getByText('调研结论摘要：澄清产物台应对齐 Gate 载荷。')).toBeVisible({
      timeout: 10_000,
    })

    await page.goto('/clarify-inbox-product.html?scenario=loading')
    // While loading, final empty-state copy must not flash.
    await expect(page.getByTestId('clarify-product-empty-pending')).toHaveCount(0)
    await expect(page.getByText('加载运行详情…')).toBeVisible({ timeout: 5_000 })
    await page.getByTestId('conversation-panel-toggle').click()
    await expect(page.getByText('调研结论摘要：澄清产物台应对齐 Gate 载荷。')).toBeVisible({
      timeout: 10_000,
    })
  })
})
