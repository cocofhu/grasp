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
    if (path === '/api/solo/sessions/solo-1/workflows') response = { items: tasks, workflows: { enabled, locked: false, source: 'db' } }
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

async function openLaunch(page: Page) {
  await page.getByTestId('workflow-picker-open').click()
  await page.getByRole('button', { name: /代码评估/ }).click()
  return page.getByRole('button', { name: '开始运行', exact: true })
}

test('workflow selection is a draft until the human starts; Solo remains in place', async ({ page }) => {
  const { launches } = await setup(page)
  await page.getByTestId('resident-solo-draft').fill('同时继续分析第二个问题')
  const start = await openLaunch(page)
  expect(launches).toHaveLength(0)
  await start.click()
  await expect(page.getByTestId('solo-workflow-run-1')).toBeVisible()
  expect(launches).toHaveLength(1)
  expect(launches[0].body).toMatchObject({ confirmed: true, expectedWorkflowVersion: 3 })
  expect(launches[0].key).toBeTruthy()
  await expect(page.getByTestId('resident-solo-draft')).toHaveValue('同时继续分析第二个问题')
  expect(page.url()).toContain('solo-workflows.html')
})

test('three independent workflow cards can coexist while the Solo draft is retained', async ({ page }) => {
  const { launches } = await setup(page)
  await page.getByTestId('resident-solo-draft').fill('我的 Solo 对话')
  for (let index = 1; index <= 3; index++) {
    await (await openLaunch(page)).click()
    await expect(page.getByTestId(`solo-workflow-run-${index}`)).toBeVisible()
  }
  expect(launches).toHaveLength(3)
  expect(new Set(launches.map(launch => launch.key)).size).toBe(3)
  await expect(page.getByTestId('resident-solo-draft')).toHaveValue('我的 Solo 对话')
})

test('disabled workflows retain existing cards and remove the new-launch command', async ({ page }) => {
  const { launches } = await setup(page, false, [{ id: 'existing', title: '已有任务', workflowName: '工作流', status: 'paused', controlVersion: 2, nodes: [] }])
  await expect(page.getByTestId('solo-workflow-existing')).toBeVisible()
  await expect(page.getByTestId('workflow-picker-open')).toHaveCount(0)
  await expect(page.getByTestId('solo-workflow-command')).toHaveCount(0)
  await expect(page.getByRole('button', { name: '继续执行', exact: true })).toBeVisible()
  expect(launches).toHaveLength(0)
})
