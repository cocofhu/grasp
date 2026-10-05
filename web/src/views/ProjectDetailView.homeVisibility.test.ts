// @vitest-environment node
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const viewsDir = dirname(fileURLToPath(import.meta.url))
const localesDir = join(viewsDir, '../locales')
const detailSrc = readFileSync(join(viewsDir, 'ProjectDetailView.vue'), 'utf8')
const enPages = JSON.parse(readFileSync(join(localesDir, 'en/pages.json'), 'utf8')) as {
  pages: { projectDetail: { homeVisibility: Record<string, string> }; dashboard: { noWorkflows: string } }
}
const zhPages = JSON.parse(readFileSync(join(localesDir, 'zh-CN/pages.json'), 'utf8')) as {
  pages: { projectDetail: { homeVisibility: Record<string, string> }; dashboard: { noWorkflows: string } }
}

function mobileWorkflowsBlock(): string {
  const start = detailSrc.indexOf('<!-- Mobile card list:')
  const end = detailSrc.indexOf('<!-- Desktop table -->')
  expect(start, 'missing mobile workflows card list').toBeGreaterThanOrEqual(0)
  expect(end, 'missing desktop table marker').toBeGreaterThan(start)
  return detailSrc.slice(start, end)
}

describe('ProjectDetailView home visibility switch (g2.1 / g2.2 / g2.3)', () => {
  it('desktop table has a Show on Home column next to notify policy with click.stop', () => {
    expect(detailSrc).toMatch(/pages\.projectDetail\.notify\.colPolicy[\s\S]*pages\.projectDetail\.homeVisibility\.col/)
    const cellStart = detailSrc.indexOf('data-testid="wf-home-visibility-cell"')
    expect(cellStart, 'missing wf-home-visibility-cell').toBeGreaterThanOrEqual(0)
    const cell = detailSrc.slice(Math.max(0, cellStart - 80), cellStart + 700)
    expect(cell).toMatch(/@click\.stop/)
    expect(cell).toMatch(/data-testid="wf-home-visibility-switch"/)
    expect(cell).toMatch(/toggleWorkflowShowOnHome/)
  })

  it('mobile card shows the switch beside notify and stops row navigation', () => {
    const mobile = mobileWorkflowsBlock()
    expect(mobile).toMatch(/data-testid="wf-home-visibility-inline"/)
    expect(mobile).toMatch(/data-testid="wf-notify-inline"/)
    const homeStart = mobile.indexOf('data-testid="wf-home-visibility-inline"')
    const notifyStart = mobile.indexOf('data-testid="wf-notify-inline"')
    const menuStart = mobile.indexOf('data-wf-menu')
    expect(homeStart).toBeGreaterThan(notifyStart)
    expect(menuStart).toBeGreaterThan(homeStart)
    const home = mobile.slice(homeStart, menuStart)
    expect(home).toMatch(/@click\.stop/)
    expect(home).toMatch(/data-testid="wf-home-visibility-switch"/)
  })

  it('zh/en copy covers column, label, success and failure (g2.2)', () => {
    const zh = zhPages.pages.projectDetail.homeVisibility
    const en = enPages.pages.projectDetail.homeVisibility
    expect(zh.col).toBe('首页可见')
    expect(zh.label).toBe('首页可见')
    expect(zh.updated).toContain('首页可见')
    expect(zh.updateFailed).toContain('失败')
    expect(en.col).toBe('Show on Home')
    expect(en.label).toBe('Show on Home')
    expect(en.updated.toLowerCase()).toContain('home')
    expect(en.updateFailed.toLowerCase()).toMatch(/fail/)
  })

  it('empty Home copy points at enabling in a project, not at lost workflows (g3.2)', () => {
    expect(zhPages.pages.dashboard.noWorkflows).toContain('首页可见')
    expect(zhPages.pages.dashboard.noWorkflows).not.toMatch(/丢失/)
    expect(enPages.pages.dashboard.noWorkflows).toMatch(/Show on Home/)
    expect(enPages.pages.dashboard.noWorkflows).not.toMatch(/missing|lost|deleted/i)
  })
})
