import { test, expect, type Page } from '@playwright/test'
import type { WorkspaceChangedPath, WorkspaceChangeSnapshot } from '../src/lib/api/clients/workspaceChangesClient'

test.use({ reducedMotion: 'reduce' })

const previous = '# 部署指南\n\n服务默认使用端口 8080。\n'
const current = '# 部署指南\n\n服务默认使用端口 8090。\n\n## 验证\n\n使用健康检查确认服务就绪。\n'
const patch = 'diff --git a/docs/deploy.md b/docs/deploy.md\n--- a/docs/deploy.md\n+++ b/docs/deploy.md\n' +
  '@@ -1,3 +1,7 @@\n # 部署指南\n \n-服务默认使用端口 8080。\n+服务默认使用端口 8090。\n+\n+## 验证\n+\n+使用健康检查确认服务就绪。\n'
const goBefore = [
  'package deploy', '', 'import "net/http"', '', 'func readiness(w http.ResponseWriter, r *http.Request) {',
  '\t// Return a simple status.', '\tw.WriteHeader(http.StatusOK)', '\t_, _ = w.Write([]byte("ready"))', '}', '',
].join('\n')
const goAfter = [
  'package deploy', '', 'import "net/http"', '', 'func readiness(w http.ResponseWriter, r *http.Request) {',
  '\t// Write a machine-readable readiness response.', '\tw.Header().Set("Content-Type", "application/json")',
  '\tw.WriteHeader(http.StatusOK)', '\t_, _ = w.Write([]byte(`{"status":"ready"}`))', '}', '',
].join('\n')
const goPatch = [
  '@@ -3,7 +3,8 @@', ' import "net/http"', ' ', ' func readiness(w http.ResponseWriter, r *http.Request) {',
  '-\t// Return a simple status.', '+\t// Write a machine-readable readiness response.',
  '+\tw.Header().Set("Content-Type", "application/json")', ' \tw.WriteHeader(http.StatusOK)',
  '-\t_, _ = w.Write([]byte("ready"))', '+\t_, _ = w.Write([]byte(`{"status":"ready"}`))', ' }', '',
].join('\n')
const markdown: WorkspaceChangedPath = {
  id: 'docs', path: 'docs/deploy.md', status: 'M', added: 5, deleted: 1,
  hasBefore: true, hasAfter: true, hasPatch: true,
}
const removed: WorkspaceChangedPath = {
  id: 'removed', path: 'docs/legacy.md', status: 'D', added: 0, deleted: 1,
  hasBefore: true, hasAfter: false, hasPatch: true,
}
const binary: WorkspaceChangedPath = {
  id: 'binary', path: 'media/mark.png', status: 'A', added: null, deleted: null,
  hasBefore: false, hasAfter: false, hasPatch: false,
}
const saved: WorkspaceChangeSnapshot = {
  id: 'saved', workspace: 'workspace-a', nodeId: 'implement', capturedAt: '2026-11-20T08:22:00Z',
  repos: [
    {
      name: 'grasp-deploy', directory: 'grasp-deploy', branch: 'update-deployment', baseBranch: 'main',
      baseSha: 'a1b2c3d4e5f6', headSha: 'b2c3d4e5f6a1', files: [markdown, removed, binary], added: 5, deleted: 2, fileCount: 3,
    },
    {
      name: 'operations', directory: 'operations', branch: 'main', baseBranch: 'main',
      baseSha: 'b2c3d4e5f6a1', headSha: 'b2c3d4e5f6a1', files: [], added: 0, deleted: 0, fileCount: 0,
    },
  ],
}

async function mockChanges(page: Page, { snapshot = saved, canRefresh = true } = {}) {
  const seen: string[] = []
  let refreshed = false
  await page.route('**/api/**/changes**', async route => {
    const url = new URL(route.request().url())
    seen.push(url.pathname)
    if (url.pathname.endsWith('/files/docs')) {
      if (url.pathname.includes('/refreshed/')) {
        await route.fulfill({ json: { patch: patch.replace('8090', '8091'), before: previous, after: current.replace('8090', '8091') } })
        return
      }
      await route.fulfill({ json: { patch, before: previous, after: current } })
      return
    }
    if (url.pathname.endsWith('/files/removed')) {
      await route.fulfill({ json: { patch: '@@ -1 +0,0 @@\n-# 旧部署方式\n', before: '# 旧部署方式\n' } })
      return
    }
    if (url.pathname.endsWith('/files/ops')) {
      await route.fulfill({ json: { patch: '@@ -1 +1,2 @@\n port: 8090\n+timeout: 30\n', after: 'port: 8090\ntimeout: 30\n' } })
      return
    }
    if (url.pathname.endsWith('/files/go')) {
      await route.fulfill({ json: { patch: goPatch, before: goBefore, after: goAfter } })
      return
    }
    if (url.pathname.endsWith('/refresh')) refreshed = true
    if (url.searchParams.get('scope') === 'turn') {
      await route.fulfill({ json: {
        status: 'ready', canRefresh: false,
        snapshot: { ...saved, id: 'turn-snapshot', turnId: 'turn-a', repos: [{ ...saved.repos[0], files: [markdown], fileCount: 1, deleted: 1 }] },
      } })
      return
    }
    const workspaces = [
      { id: 'workspace-a', nodeId: 'implement', capturedAt: saved.capturedAt },
      { id: 'workspace-b', nodeId: 'verify', capturedAt: saved.capturedAt },
    ]
    const selected = url.searchParams.get('workspace') === 'workspace-b'
      ? { ...saved, id: 'other', workspace: 'workspace-b', nodeId: 'verify', repos: [saved.repos[1]] }
      : refreshed ? { ...snapshot, id: 'refreshed' } : snapshot
    await route.fulfill({ json: { status: 'ready', canRefresh, snapshot: selected, workspaces } })
  })
  return { seen, publishNewSnapshot: () => { refreshed = true } }
}

test('workspace snapshots keep diff, preview and source on the same version', async ({ page }, testInfo) => {
  const { seen, publishNewSnapshot } = await mockChanges(page)
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/workspace-changes.html')
  await expect(page.getByTestId('workspace-turn-changes')).toContainText('deploy.md')
  expect(seen).not.toContain('/api/runs/run-review/changes')
  const trigger = page.getByTestId('workspace-changes')
  const card = page.getByRole('dialog', { name: '更改', exact: true })
  const dialog = page.getByRole('dialog', { name: '本次运行改动' })
  await trigger.click()
  await expect(trigger).toHaveAttribute('aria-expanded', 'true')
  await expect(card.getByTestId('workspace-change-stats')).toHaveText('+5−2')
  await expect(card).toContainText('update-deployment')
  await expect(card).toContainText('实现')
  await expect(dialog).toBeHidden()
  expect(seen.some(path => path.includes('/files/'))).toBe(false)
  await page.getByRole('heading', { name: '部署配置更新' }).click()
  await expect(card).toBeHidden()
  await trigger.focus()
  await trigger.press('Enter')
  await expect(card).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(trigger).toBeFocused()
  await expect(card).toBeHidden()
  await trigger.press('Enter')
  await expect(card).toBeFocused()
  await page.keyboard.press('Tab')
  await expect(card.getByTestId('workspace-changes-card-open')).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(card).toBeHidden()
  const diff = dialog.getByTestId('workspace-change-diff')
  await expect(diff).toContainText('端口 8090')
  await expect(diff.locator('.changes-line-add')).toHaveCount(5)
  await expect(diff.locator('.changes-line-remove')).toHaveCount(1)
  await page.evaluate(() => document.fonts.ready.then(() => undefined))
  const lines = diff.locator('.changes-diff-grid')
  const rendered = await lines.screenshot({ animations: 'disabled', path: testInfo.outputPath('desktop-diff.png') })
  await dialog.getByRole('button', { name: '预览', exact: true }).click()
  await expect(dialog.getByTestId('workspace-change-markdown').getByRole('heading', { name: '验证' })).toBeVisible()
  await dialog.screenshot({ animations: 'disabled', path: testInfo.outputPath('desktop-markdown.png') })
  await dialog.getByRole('button', { name: '源码', exact: true }).click()
  await expect(dialog.getByTestId('workspace-change-source').locator('.monaco-editor')).toBeVisible()
  await dialog.getByRole('button', { name: '差异', exact: true }).click()
  await page.evaluate(() => document.fonts.ready.then(() => undefined))
  const restored = await lines.screenshot({ animations: 'disabled', path: testInfo.outputPath('desktop-diff-restored.png') })
  expect(restored.equals(rendered)).toBe(true)
  publishNewSnapshot()
  await Promise.all([
    page.waitForResponse(response => response.url().includes('/api/runs/run-review/changes?')),
    page.evaluate(() => window.dispatchEvent(new Event('workspace-fixture-update'))),
  ])
  await expect(diff).toContainText('端口 8090')
  await dialog.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(diff).toContainText('端口 8091')
  expect(seen).toContain('/api/runs/run-review/changes/refresh')
  await dialog.getByTestId('workspace-change-file-row').filter({ hasText: 'legacy.md' }).click()
  await dialog.getByRole('button', { name: '预览', exact: true }).click()
  await expect(dialog.getByText('删除前版本')).toBeVisible()
  await expect(dialog.getByRole('heading', { name: '旧部署方式' })).toBeVisible()
  await dialog.getByTestId('workspace-change-file-row').filter({ hasText: 'mark.png' }).click()
  await expect(dialog.getByText('二进制文件已更改。')).toBeVisible()
  expect(seen.some(path => path.endsWith('/files/binary'))).toBe(false)
  await dialog.getByTestId('workspace-change-workspaces').selectOption('workspace-b')
  await expect(dialog.getByText('暂无改动')).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()
  await expect(trigger).toBeFocused()
  await page.getByTestId('workspace-turn-changes').click()
  const turn = page.getByRole('dialog', { name: '本轮改动' })
  await expect(turn.getByTestId('workspace-change-summary')).toContainText('1 个文件')
  await expect(turn.getByTestId('workspace-change-diff')).toContainText('端口 8090')
  expect(seen).toContain('/api/solo/sessions/solo-review/changes/turn-snapshot/files/docs')
})

test('workspace changes stays within the dark narrow viewport', async ({ page }, testInfo) => {
  await mockChanges(page)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/workspace-changes.html?theme=dark')
  await page.getByTestId('workspace-changes').click()
  const card = page.getByTestId('workspace-changes-card')
  await expect(card.getByTestId('workspace-change-stats')).toHaveText('+5−2')
  const cardBounds = await card.boundingBox()
  expect(cardBounds?.x).toBeGreaterThanOrEqual(8)
  expect((cardBounds?.x ?? 390) + (cardBounds?.width ?? 390)).toBeLessThanOrEqual(382)
  await card.screenshot({ animations: 'disabled', path: testInfo.outputPath('mobile-dark-card.png') })
  await card.getByTestId('workspace-changes-card-open').click()
  const dialog = page.getByRole('dialog', { name: '本次运行改动' })
  await expect(dialog.getByTestId('workspace-change-file-list')).toBeVisible()
  await expect(dialog.getByTestId('workspace-change-file-pane')).toBeHidden()
  await dialog.getByTestId('workspace-change-file-row').filter({ hasText: 'deploy.md' }).click()
  await expect(dialog.getByTestId('workspace-change-diff')).toBeVisible()
  const bounds = await dialog.boundingBox()
  expect(bounds?.width).toBeLessThan(390)
  expect((bounds?.x ?? -2) + (bounds?.width ?? 400)).toBeLessThanOrEqual(390)
  await dialog.screenshot({ animations: 'disabled', path: testInfo.outputPath('mobile-dark-diff.png') })
  await dialog.getByRole('button', { name: '预览', exact: true }).click()
  await expect(dialog.getByTestId('workspace-change-markdown')).toBeVisible()
  await dialog.getByRole('button', { name: '文件', exact: true }).click()
  await expect(dialog.getByTestId('workspace-change-file-list')).toBeVisible()
})

test('change card opens the selected repository from an archived snapshot', async ({ page }, testInfo) => {
  const opsFile: WorkspaceChangedPath = {
    id: 'ops', path: 'service.yaml', status: 'M', added: 1, deleted: 0,
    hasPatch: true, hasBefore: true, hasAfter: true,
  }
  await mockChanges(page, {
    canRefresh: false,
    snapshot: { ...saved, repos: [
      saved.repos[0],
      { ...saved.repos[1], branch: '', fileCount: 1, added: 1, files: [opsFile] },
    ] },
  })
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/workspace-changes.html')
  await page.getByTestId('workspace-changes').click()
  const card = page.getByTestId('workspace-changes-card')
  const operation = card.getByTestId('workspace-changes-card-open').filter({ hasText: 'operations' })
  await expect(operation).toContainText('分离状态的工作树')
  await expect(operation.getByTestId('workspace-change-stats')).toHaveText('+1−0')
  await page.evaluate(() => document.fonts.ready.then(() => undefined))
  await card.screenshot({ animations: 'disabled', path: testInfo.outputPath('desktop-card.png') })
  await operation.click()
  const dialog = page.getByRole('dialog', { name: '本次运行改动' })
  await expect(dialog.getByTestId('workspace-change-file-row').filter({ hasText: 'service.yaml' })).toHaveAttribute('aria-current', 'true')
  await expect(dialog.getByTestId('workspace-change-diff')).toContainText('timeout: 30')
  await expect(dialog.getByTestId('workspace-change-summary')).toContainText('归档于')
})

test('code diffs preserve line markers and syntax colors in both themes', async ({ page }, testInfo) => {
  const goFile: WorkspaceChangedPath = {
    id: 'go', path: 'server/readiness.go', status: 'M', added: 3, deleted: 2,
    hasPatch: true, hasBefore: true, hasAfter: true,
  }
  await mockChanges(page, {
    canRefresh: false,
    snapshot: { ...saved, repos: [{ ...saved.repos[0], fileCount: 1, added: 3, deleted: 2, files: [goFile] }] },
  })

  await page.setViewportSize({ width: 1440, height: 900 })
  for (const theme of ['light', 'dark']) {
    await page.goto('/workspace-changes.html?theme=' + theme)
    await page.getByTestId('workspace-changes').click()
    await page.getByTestId('workspace-changes-card-open').click()
    const dialog = page.getByRole('dialog', { name: '本次运行改动' })
    const diff = dialog.getByTestId('workspace-change-diff')
    await expect(diff.locator('.changes-line-context .hljs-keyword').filter({ hasText: /^func$/ })).toBeVisible()
    await expect(diff.locator('.changes-line-remove .hljs-string')).toHaveText('"ready"')
    const addedString = diff.locator('.changes-line-add .hljs-string').filter({ hasText: '"application/json"' })
    await expect(addedString).toBeVisible()
    const plainColor = await diff.locator('.changes-line-add .changes-line-text').first().evaluate(el => getComputedStyle(el).color)
    await expect(addedString).not.toHaveCSS('color', plainColor)
    await expect(diff.locator('.changes-line-add .changes-line-mark')).toHaveText(['+', '+', '+'])
    await expect(diff.locator('.changes-line-remove .changes-line-mark')).toHaveText(['−', '−'])
    await page.evaluate(() => document.fonts.ready.then(() => undefined))
    await dialog.screenshot({ animations: 'disabled', path: testInfo.outputPath(`go-diff-${theme}.png`) })
  }
})
