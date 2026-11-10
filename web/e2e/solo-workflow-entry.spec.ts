import { expect, test, type Page } from '@playwright/test'

type Task = { id: string; title: string; workflowName: string; status: string; controlVersion: number; nodes: { id: string; label: string; type: string; status: string; iteration: number }[] }
async function setup(page: Page, enabled = true, initial: Task[] = []) {
  const launches: { body: Record<string, unknown>; key: string | undefined }[] = []
  const tasks = initial.slice()
  const graph = { version: 3, name: '代码评估', variables: [], nodes: [{ id: 'in', type: 'input', label: '开始', position: { x: 0, y: 0 }, config: {} }, { id: 'out', type: 'output', label: '完成', position: { x: 1, y: 0 }, config: {} }], edges: [{ id: 'e', source: 'in', target: 'out' }] }
  await page.route('**/api/**', async route => {
    const request = route.request(); const path = new URL(request.url()).pathname
    if (!path.startsWith('/api/')) { await route.continue(); return }
    let response: unknown = {}
    if (path.endsWith('/references/search')) response = { items: [] }
    else if (path === '/api/solo/sessions/solo-1/workflows') response = { items: tasks, workflows: { enabled, locked: false, source: 'db' } }
    else if (path === '/api/workflows') response = [{ id: 'wf', name: '代码评估', publishedVersion: 3, publishedSnapshot: graph }]
    else if (path.endsWith('/workflows/wf/runs') && request.method() === 'POST') {
      launches.push({ body: request.postDataJSON(), key: request.headers()['idempotency-key'] })
      const id = `run-${launches.length}`
      tasks.push({ id, title: `任务 ${launches.length}`, workflowName: '代码评估', status: 'running', controlVersion: 0, nodes: [{ id: 'evaluate', label: '评估', type: 'agent', status: 'running', iteration: 1 }] })
      response = { id, status: 'queued' }
    } else if (path.includes('/model-configurations') || path.includes('/model-connections')) response = { items: [] }
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(response) })
  })
  await page.goto('/solo-workflows.html')
  return { launches, tasks }
}

async function openLaunch(page: Page, selection: 'click' | 'Enter' | 'Tab' = 'click') {
  const input = page.getByTestId('resident-solo-input')
  await input.fill('@wor')
  await expect(page.getByTestId('solo-reference-picker')).toBeVisible()
  if (selection === 'click') await page.getByTestId('solo-reference-workflow').click()
  else {
    if (selection === 'Tab') { await input.press('Tab'); await input.press('Shift+Tab') }
    await input.press('ArrowDown')
    await input.press('Enter')
  }
  await expect(input).toHaveValue('')
  await page.getByRole('button', { name: /代码评估/ }).click()
  return page.getByRole('button', { name: '开始运行', exact: true })
}

test('workflow selection is a draft until the human starts; Solo remains in place', async ({ page }, testInfo) => {
  const { launches } = await setup(page)
  await page.getByTestId('resident-solo-input').fill('@')
  await expect(page.getByTestId('solo-reference-workflow')).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('solo-reference-workflow-desktop.png'), fullPage: true })
  const start = await openLaunch(page)
  expect(launches).toHaveLength(0)
  await start.click()
  await expect(page.getByTestId('solo-workflow-run-1')).toBeVisible()
  await expect(page.getByTestId('solo-workflow-command')).toHaveCount(0)
  expect(launches).toHaveLength(1)
  expect(launches[0].body).toMatchObject({ confirmed: true, expectedWorkflowVersion: 3 })
  expect(launches[0].key).toBeTruthy()
  await page.getByTestId('resident-solo-input').fill('同时继续分析第二个问题')
  await expect(page.getByTestId('resident-solo-input')).toHaveValue('同时继续分析第二个问题')
  expect(page.url()).toContain('solo-workflows.html')
})

test('three independent workflow cards can coexist while Solo remains editable', async ({ page }) => {
  const { launches } = await setup(page)
  for (let index = 1; index <= 3; index++) {
    await (await openLaunch(page, index === 1 ? 'Enter' : index === 2 ? 'Tab' : 'click')).click()
    await expect(page.getByTestId(`solo-workflow-run-${index}`)).toBeVisible()
  }
  expect(launches).toHaveLength(3)
  expect(new Set(launches.map(launch => launch.key)).size).toBe(3)
  await page.getByTestId('resident-solo-input').fill('我的 Solo 对话')
  await expect(page.getByTestId('resident-solo-input')).toHaveValue('我的 Solo 对话')
})

test('@ workflow entry dismisses with Escape and leaves slashes and IME input alone', async ({ page }) => {
  const { launches } = await setup(page)
  const input = page.getByTestId('resident-solo-input')
  const menu = page.getByTestId('solo-reference-picker')
  await expect(menu).toHaveCount(0)
  await expect(page.getByTestId('workflow-picker-open')).toHaveCount(0)
  await expect(page.getByTestId('solo-workflow-menu')).toHaveCount(0)
  await input.fill('@')
  await expect(menu).toBeVisible()
  await input.press('Escape')
  await expect(menu).toHaveCount(0)
  await expect(input).toHaveValue('@')
  await input.press('ArrowLeft')
  await expect(menu).toHaveCount(0)
  await input.fill('@work')
  await expect(menu).toBeVisible()
  await input.evaluate(element => (element as HTMLTextAreaElement).blur())
  await expect(menu).toHaveCount(0)
  await input.focus()
  await expect(menu).toBeVisible()
  await input.press('Shift+Enter')
  await expect(input).toHaveValue('@work\n')
  await expect(menu).toHaveCount(0)
  await input.fill('@')
  await input.dispatchEvent('compositionstart')
  await input.dispatchEvent('keydown', { key: 'Enter', isComposing: true, keyCode: 229 })
  await expect(input).toHaveValue('@')
  await expect(menu).toHaveCount(0)
  await input.dispatchEvent('compositionend')
  await expect(menu).toBeVisible()
  for (const text of ['/', '/wor', '/workspace/file', '查看 /workflow', '/unknown']) {
    await input.fill(text)
    await expect(menu).toHaveCount(0)
    await expect(input).toHaveValue(text)
  }
  await expect(page.getByRole('button', { name: '开始运行', exact: true })).toHaveCount(0)
  expect(launches).toHaveLength(0)
})

test('the @ picker fits a 390px viewport and workflow selection still requires an explicit start', async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 390, height: 844 })
  const { launches } = await setup(page)
  const input = page.getByTestId('resident-solo-input')
  await input.fill('@')
  const menu = page.getByTestId('solo-reference-picker')
  await expect(menu).toBeVisible()
  const box = await menu.boundingBox()
  expect(box).not.toBeNull()
  expect(box!.x).toBeGreaterThanOrEqual(0)
  expect(box!.y).toBeGreaterThanOrEqual(0)
  expect(box!.x + box!.width).toBeLessThanOrEqual(390)
  expect(box!.y + box!.height).toBeLessThanOrEqual(844)
  await page.screenshot({ path: testInfo.outputPath('solo-reference-workflow-mobile.png'), fullPage: true })
  await input.press('ArrowDown')
  await input.press('Enter')
  await page.getByRole('button', { name: /代码评估/ }).click()
  await expect(page.getByRole('button', { name: '开始运行', exact: true })).toBeVisible()
  expect(launches).toHaveLength(0)
})

test('disabled workflows retain existing cards and remove workflow selection from @', async ({ page }) => {
  const { launches } = await setup(page, false, [{ id: 'existing', title: '已有任务', workflowName: '工作流', status: 'paused', controlVersion: 2, nodes: [] }])
  await expect(page.getByTestId('solo-workflow-existing')).toBeVisible()
  await expect(page.getByTestId('workflow-picker-open')).toHaveCount(0)
  await expect(page.getByTestId('solo-workflow-command')).toHaveCount(0)
  await page.getByTestId('resident-solo-input').fill('@')
  await expect(page.getByTestId('solo-reference-workflow')).toHaveCount(0)
  await page.getByTestId('resident-solo-input').press('Tab')
  await expect(page.getByRole('button', { name: /代码评估/ })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '继续执行', exact: true })).toBeVisible()
  expect(launches).toHaveLength(0)
})

test('manual node supplements stay in the current Solo and report their processing state', async ({ page }, testInfo) => {
  const { launches } = await setup(page)
  const submissions: Record<string, unknown>[] = []
  let state = 'queued'
  const target = { runId: 'run-1', nodeId: 'evaluate', iteration: 1, attempt: 1, generation: 'execution-a', state: 'open' }
  await page.route('**/api/runs/run-1', route => route.fulfill({ json: { id: 'run-1', status: 'running', nodes: [{ id: 'evaluate', type: 'agent', label: '评估' }], currentNode: { id: 'evaluate', name: '评估', iteration: 1, kind: 'agent', mode: 'auto', state: 'running' } } }))
  await page.route('**/api/runs/run-1/nodes/evaluate/instructions**', route => {
    if (route.request().method() === 'POST') {
      submissions.push(route.request().postDataJSON())
      return route.fulfill({ json: { ...submissions[0], id: 'instruction-1', sequence: 1, actorId: 'alice', state } })
    }
    return route.fulfill({ json: { target, items: submissions.length ? [{ ...submissions[0], id: 'instruction-1', sequence: 1, actorId: 'alice', state }] : [], hasMore: false } })
  })
  await (await openLaunch(page)).click()
  await page.getByTestId('resident-solo-input').fill('继续我的 Solo 对话')
  await page.getByTestId('solo-workflow-run-1').getByRole('button', { name: '评估 · running' }).click()
  const input = page.getByTestId('instruction-input')
  await input.fill('补充边界与回滚分析')
  expect(submissions).toHaveLength(0)
  await page.getByTestId('instruction-send').click()
  await expect(page.getByTestId('instruction-instruction-1')).toContainText('等待本轮处理')
  expect(submissions).toHaveLength(1)
  expect(submissions[0]).toMatchObject({ text: '补充边界与回滚分析', iteration: 1, generation: 'execution-a' })
  state = 'applied'
  await expect(page.getByTestId('instruction-instruction-1')).toContainText('本轮已处理')
  await page.screenshot({ path: testInfo.outputPath('workflow-instructions.png'), fullPage: true })
  await page.locator('.fixed.inset-0.z-40').getByRole('button').first().click()
  await expect(page.getByTestId('resident-solo-input')).toHaveValue('继续我的 Solo 对话')
  expect(launches).toHaveLength(1)
})

test('an uncertain supplement preserves its draft and request identity across closing the task drawer', async ({ page }) => {
  await setup(page)
  const submissions: Record<string, unknown>[] = []
  const target = { runId: 'run-1', nodeId: 'evaluate', iteration: 1, generation: 'execution-a', state: 'open' }
  await page.route('**/api/runs/run-1', route => route.fulfill({ json: { id: 'run-1', status: 'running', nodes: [{ id: 'evaluate', type: 'agent', label: '评估' }], currentNode: { id: 'evaluate', iteration: 1, kind: 'agent', mode: 'auto', state: 'running' } } }))
  await page.route('**/api/runs/run-1/nodes/evaluate/instructions**', route => {
    if (route.request().method() === 'POST') {
      submissions.push(route.request().postDataJSON())
      if (submissions.length === 1) return route.fulfill({ status: 500, json: { error: '请求结果尚未确认' } })
      return route.fulfill({ json: { ...submissions[0], id: 'instruction-1', sequence: 1, actorId: 'alice', state: 'queued' } })
    }
    return route.fulfill({ json: { target, items: [], hasMore: false } })
  })
  await (await openLaunch(page)).click()
  const node = page.getByTestId('solo-workflow-run-1').getByRole('button', { name: '评估 · running' })
  await node.click()
  await page.getByTestId('instruction-input').fill('保留我未发出的补充要求')
  await page.getByTestId('instruction-send').click()
  await expect(page.getByRole('alert').filter({ hasText: '请求结果尚未确认' })).toBeVisible()
  await page.locator('.fixed.inset-0.z-40').getByRole('button').first().click()
  await node.click()
  await expect(page.getByTestId('instruction-input')).toHaveValue('保留我未发出的补充要求')
  await page.getByTestId('instruction-send').click()
  await expect(page.getByTestId('instruction-input')).toHaveValue('')
  expect(submissions).toHaveLength(2)
  expect(submissions[1]).toEqual(submissions[0])
})
