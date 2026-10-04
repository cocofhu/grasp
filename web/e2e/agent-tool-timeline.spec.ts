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

      const groups = page.getByTestId('agent-tool-group')
      await expect(groups).toHaveCount(2)
      await expect(groups.nth(0)).toHaveAttribute('data-state', 'done')
      await expect(groups.nth(0).getByTestId('agent-tool-group-names')).toHaveText('运行命令 · 读取文件')
      await expect(groups.nth(1)).toHaveAttribute('data-state', 'failed')
      await expect(groups.nth(1).getByTestId('agent-tool-group-failed')).toHaveText('1 个失败')

      await groups.nth(0).getByTestId('agent-tool-group-head').click()
      await expect(groups.nth(0).getByTestId('agent-tool-duration')).toHaveText(['42s'])
      await groups.nth(1).getByTestId('agent-tool-group-head').click()
      await expect(groups.nth(1).getByTestId('agent-tool-detail')).toContainText('findings 不能为空')
      await page.screenshot({ path: path.join(shotDir, `tool-timeline-${theme}.png`) })

      await groups.nth(1).getByTestId('agent-tool-open-artifact').click()
      await expect(page.getByTestId('react-artifact-tab-plan.json')).toHaveAttribute('aria-selected', 'true')
      await page.screenshot({ path: path.join(shotDir, `tool-timeline-open-${theme}.png`) })
    })
  }
})
