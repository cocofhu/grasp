import { test, expect } from '@playwright/test'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const shotDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../../.tmp-tool-timeline-shots')

test.describe('Agent 工具调用时间线', () => {
  for (const theme of ['light', 'dark']) {
    test(`思考 / 工具 / 正文按序展示，查看打开产物 (${theme})`, async ({ page }) => {
      await page.setViewportSize({ width: 1200, height: 720 })
      await page.goto(`/agent-tool-timeline.html?theme=${theme}`)
      await expect(page.getByTestId('tool-timeline-root')).toBeVisible({ timeout: 15_000 })
      const panel = page.getByTestId('conversation-artifact-panel')
      await expect(panel).toBeHidden()
      await expect(page.getByTestId('conversation-panel-toggle')).toHaveAttribute('aria-expanded', 'false')

      // A finished reply with a failed tool call folds like any other turn.
      // The fold line names the elapsed time and how many tools failed; the reply
      // stays under the line. Opening the fold shows the tool groups, and a failed
      // call is already expanded.
      const elapsed = page.getByTestId('agent-timeline-elapsed')
      await expect(elapsed).toBeVisible()
      await expect(elapsed.getByTestId('agent-timeline-elapsed-time')).toHaveText('用时 3m 34s')
      await expect(elapsed.getByTestId('agent-timeline-elapsed-failed')).toHaveText('1 个失败')
      const response = page.getByTestId('agent-timeline-message')
      await expect(response).toHaveText('计划已写入，调研结论缺少 findings，下一轮补上。')
      await expect(response).toBeVisible()
      await expect(page.getByTestId('agent-tool-group')).toHaveCount(0)

      await elapsed.click()
      await expect(elapsed).toHaveAttribute('aria-expanded', 'true')
      const thoughts = page.locator('details[data-testid="agent-timeline-thought"]')
      const thoughtBodies = thoughts.locator('.agent-thought-body')
      await expect(thoughts).toHaveCount(2)
      for (let index = 0; index < 2; index++) {
        await expect(thoughts.nth(index)).not.toHaveAttribute('open')
        await expect(thoughts.nth(index).locator('summary')).toBeVisible()
        await expect(thoughtBodies.nth(index)).toBeHidden()
      }
      await expect(response).toBeVisible()
      const groups = page.getByTestId('agent-tool-group')
      await expect(groups).toHaveCount(2)
      await expect(groups.nth(0)).toHaveAttribute('data-state', 'done')
      await expect(groups.nth(0).getByTestId('agent-tool-group-names')).toHaveText('运行命令 · 读取文件')
      await expect(groups.nth(1)).toHaveAttribute('data-state', 'failed')
      await expect(groups.nth(1).getByTestId('agent-tool-group-failed')).toHaveText('1 个失败')
      await page.screenshot({ path: path.join(shotDir, `tool-timeline-thoughts-collapsed-${theme}.png`) })
      await page.setViewportSize({ width: 390, height: 720 })
      for (let index = 0; index < 2; index++) {
        await expect(thoughts.nth(index).locator('summary')).toBeVisible()
        await expect(thoughtBodies.nth(index)).toBeHidden()
      }
      await expect(response).toBeVisible()
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
      await page.screenshot({ path: path.join(shotDir, `tool-timeline-thoughts-collapsed-narrow-${theme}.png`) })
      await page.setViewportSize({ width: 1200, height: 720 })

      // Each thought has its own fold. Revealing the process keeps both bodies
      // hidden until their own summary is clicked, and never hides the reply.
      await thoughts.nth(0).locator('summary').click()
      await expect(thoughtBodies.nth(0)).toBeVisible()
      await expect(thoughtBodies.nth(0)).toHaveText('先确认依赖能装上，再把计划写进产物。')
      await expect(thoughtBodies.nth(1)).toBeHidden()
      await thoughts.nth(1).locator('summary').click()
      await expect(thoughtBodies.nth(0)).toBeVisible()
      await expect(thoughtBodies.nth(1)).toBeVisible()
      await expect(thoughtBodies.nth(1)).toHaveText('依赖正常，开始整理计划。')
      await thoughts.nth(0).locator('summary').click()
      await expect(thoughtBodies.nth(0)).toBeHidden()
      await expect(thoughtBodies.nth(1)).toBeVisible()
      await expect(response).toBeVisible()
      await thoughts.nth(1).locator('summary').click()
      await expect(thoughtBodies.nth(1)).toBeHidden()

      // Native summary keyboard activation also keeps per-thought choices when
      // the outer process is closed and reopened.
      await thoughts.nth(0).locator('summary').focus()
      await page.keyboard.press('Enter')
      await expect(thoughtBodies.nth(0)).toBeVisible()
      await expect(thoughtBodies.nth(1)).toBeHidden()
      await elapsed.click()
      await expect(response).toBeVisible()
      await elapsed.click()
      await expect(thoughtBodies.nth(0)).toBeVisible()
      await expect(thoughtBodies.nth(1)).toBeHidden()
      await thoughts.nth(0).locator('summary').focus()
      await page.keyboard.press('Space')
      await expect(thoughtBodies.nth(0)).toBeHidden()

      await groups.nth(0).getByTestId('agent-tool-group-head').click()
      await expect(groups.nth(0).getByTestId('agent-tool-duration')).toHaveText(['42s'])
      await groups.nth(1).getByTestId('agent-tool-group-head').click()
      await expect(groups.nth(1).getByTestId('agent-tool-detail')).toContainText('findings 不能为空')
      await page.screenshot({ path: path.join(shotDir, `tool-timeline-${theme}.png`) })

      await groups.nth(1).getByTestId('agent-tool-open-artifact').click()
      await expect(panel).toBeVisible()
      await expect(panel).toHaveAttribute('role', 'complementary')
      await expect(page.getByTestId('conversation-panel-toggle')).toHaveAttribute('aria-expanded', 'true')
      await expect(page.getByTestId('react-artifact-tab-plan.json')).toHaveAttribute('aria-selected', 'true')
      await page.screenshot({ path: path.join(shotDir, `tool-timeline-open-${theme}.png`) })
      await page.getByTestId('conversation-panel-close').click()
      await expect(panel).toBeHidden()
      await expect(groups.nth(1).getByTestId('agent-tool-detail')).toContainText('findings 不能为空')
      await groups.nth(1).getByTestId('agent-tool-open-artifact').click()
      await expect(panel).toBeVisible()
      await expect(page.getByTestId('react-artifact-tab-plan.json')).toHaveAttribute('aria-selected', 'true')
    })
  }
})
