/**
 * Canvas acceptance on the production WorkflowEditorView / WorkflowCanvas:
 * palette drag, outlet quick-add, edge insert, undo/redo, edge condition edit,
 * autosave survives reload, run statuses; plus light/dark screenshots.
 */
import { expect, test, type Page } from '@playwright/test'
import { mkdirSync } from 'node:fs'
import { CANVAS_AGENTS } from './workflow-canvas-fixtures'

const SHOT = '/tmp/workflow-canvas-shots'
mkdirSync(SHOT, { recursive: true })

type Graph = { nodes: Record<string, unknown>[]; edges: Record<string, unknown>[] }
type Store = { wf: Record<string, unknown> & Graph; saves: number }

function freshStore(graph: Graph = { nodes: [], edges: [] }): Store {
  return {
    saves: 0,
    wf: {
      id: 'wf-canvas',
      name: '画布验收',
      description: '',
      projectId: 'proj-1',
      status: 'draft',
      version: 1,
      updatedAt: '2026-10-05T00:00:00Z',
      ...graph,
    },
  }
}

async function mockApis(page: Page, store: Store) {
  await page.route('**/api/**', async (route) => {
    const req = route.request()
    const path = new URL(req.url()).pathname
    if (!path.startsWith('/api/')) return route.continue()
    const method = req.method()
    if (path === '/api/workflows/wf-canvas' && method === 'GET') return route.fulfill({ json: store.wf })
    if (path === '/api/workflows/wf-canvas' && method === 'PUT') {
      const body = req.postDataJSON() as Store['wf']
      store.saves++
      store.wf = { ...store.wf, ...body, version: (store.wf.version as number) + 1 }
      return route.fulfill({ json: store.wf })
    }
    if (path === '/api/agents') return route.fulfill({ json: CANVAS_AGENTS.map((a) => ({ ...a, files: [], mcp: [], env: {} })) })
    if (path === '/api/projects/proj-1') return route.fulfill({ json: { id: 'proj-1', name: 'Grasp' } })
    if (path.startsWith('/api/workflows')) return route.fulfill({ json: [] })
    return route.fulfill({ json: {} })
  })
}

async function openEditor(page: Page, store: Store, theme: 'light' | 'dark' = 'light') {
  await mockApis(page, store)
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto(`/workflow-canvas.html?theme=${theme}`)
  await expect(page.getByTestId('workflow-canvas')).toBeVisible({ timeout: 15_000 })
}


const nodes = (page: Page) => page.locator('.vue-flow__node')
const edges = (page: Page) => page.locator('.vue-flow__edge')

async function startFromTemplate(page: Page) {
  await page.getByTestId('empty-canvas-template').click()
  await expect(nodes(page)).toHaveCount(5)
  await expect(edges(page)).toHaveCount(5)
}

/** Hover the true midpoint of an edge path (bounding-box centres can sit on other edges). */
async function hoverEdge(page: Page, id: string) {
  const pt = await page.getByTestId(`canvas-edge-${id}`).evaluate((el) => {
    const path = el as SVGPathElement
    const p = path.getPointAtLength(path.getTotalLength() / 2)
    const m = path.getScreenCTM()!
    return { x: p.x * m.a + p.y * m.c + m.e, y: p.x * m.b + p.y * m.d + m.f }
  })
  await page.mouse.move(pt.x, pt.y)
}

async function paneBox(page: Page) {
  const box = await page.locator('.vue-flow__pane').boundingBox()
  if (!box) throw new Error('pane not visible')
  return box
}

test('palette drag adds a node at the drop point', async ({ page }) => {
  await openEditor(page, freshStore())
  await page.getByTestId('empty-canvas-blank').click()
  await expect(nodes(page)).toHaveCount(2)
  const pane = await paneBox(page)
  await page.getByTestId('palette-item-agent:实现').dragTo(page.locator('.vue-flow__pane'), {
    targetPosition: { x: pane.width / 2, y: pane.height - 120 },
  })
  await expect(nodes(page)).toHaveCount(3)
  await expect(page.locator('.vue-flow__node').filter({ hasText: '实现' })).toBeVisible()
})

test('dragging an outlet to empty space opens quick add and connects the new node', async ({ page }) => {
  await openEditor(page, freshStore())
  await page.getByTestId('empty-canvas-blank').click()
  const before = await edges(page).count()
  const outlet = page.locator('.vue-flow__node-control').getByTestId('canvas-outlet-default').first()
  const ob = await outlet.boundingBox()
  if (!ob) throw new Error('outlet not visible')
  await page.mouse.move(ob.x + ob.width / 2, ob.y + ob.height / 2)
  await page.mouse.down()
  await page.mouse.move(ob.x + 120, ob.y + 220, { steps: 8 })
  await page.mouse.move(ob.x + 160, ob.y + 240, { steps: 4 })
  await page.mouse.up()
  await expect(page.getByTestId('quick-add')).toBeVisible()
  await page.getByTestId('quick-add-item-type:set_var').click()
  await expect(nodes(page)).toHaveCount(3)
  await expect(edges(page)).toHaveCount(before + 1)
})

test('insert at edge midpoint, undo and redo', async ({ page }) => {
  await openEditor(page, freshStore())
  await startFromTemplate(page)
  await hoverEdge(page, 'e_input_clarify')
  await page.getByTestId('canvas-edge-insert').click()
  await expect(page.getByTestId('quick-add')).toBeVisible()
  await page.getByTestId('quick-add-item-type:set_var').click()
  await expect(nodes(page)).toHaveCount(6)
  await expect(edges(page)).toHaveCount(6)
  await expect(page.locator('.vue-flow__node').filter({ hasText: '赋值' })).toHaveCount(1)

  await page.getByTestId('canvas-undo').click()
  await expect(nodes(page)).toHaveCount(5)
  await expect(page.locator('.vue-flow__node').filter({ hasText: '赋值' })).toHaveCount(0)
  await page.getByTestId('canvas-redo').click()
  await expect(nodes(page)).toHaveCount(6)
  await page.keyboard.press('Control+z')
  await expect(nodes(page)).toHaveCount(5)
  await page.keyboard.press('Control+Shift+z')
  await expect(nodes(page)).toHaveCount(6)
})

test('connecting an occupied outlet replaces its edge, previews it while dragging, and Undo restores it', async ({ page }) => {
  await openEditor(page, freshStore())
  await startFromTemplate(page)
  const outlet = page.getByTestId('canvas-node-input').getByTestId('canvas-outlet-default')
  const port = page.getByTestId('canvas-node-implement').getByTestId('canvas-port-in')
  const ob = (await outlet.boundingBox())!
  const pb = (await port.boundingBox())!

  await page.mouse.move(ob.x + ob.width / 2, ob.y + ob.height / 2)
  await page.mouse.down()
  await page.mouse.move((ob.x + pb.x) / 2, (ob.y + pb.y) / 2 + 40, { steps: 6 })
  await expect(page.getByTestId('canvas-edge-e_input_clarify')).toHaveAttribute('data-replacing', 'true')
  await page.mouse.move(pb.x + pb.width / 2, pb.y + pb.height / 2, { steps: 6 })
  await page.mouse.up()

  await expect(page.getByTestId('canvas-edge-e_input_clarify')).toHaveCount(0)
  await expect(edges(page)).toHaveCount(5)
  await expect(page.locator('[data-testid^="canvas-edge-"][data-replacing]')).toHaveCount(0)
  const toastHost = page.getByTestId('toast-host')
  await expect(toastHost).toContainText('已替换')
  await expect(toastHost).not.toContainText('无条件连线')
  await page.screenshot({ path: `${SHOT}/edge-replaced.png` })

  await toastHost.getByTestId('toast-action').click()
  await expect(page.getByTestId('canvas-edge-e_input_clarify')).toHaveCount(1)
  await expect(edges(page)).toHaveCount(5)
})

test('inspector capabilities chips do not overlap or overflow in English', async ({ page }) => {
  await mockApis(page, freshStore())
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/workflow-canvas.html?lang=en')
  await expect(page.getByTestId('workflow-canvas')).toBeVisible({ timeout: 15_000 })
  await startFromTemplate(page)
  await page.getByTestId('canvas-node-clarify').click()
  const caps = page.getByTestId('inspector-caps-list')
  await expect(caps).toBeVisible()
  await expect(caps).not.toContainText(/[\u3400-\u9fff]/)
  await page.getByTestId('inspector-capabilities').screenshot({ path: `${SHOT}/inspector-caps-en.png` })

  const problems = await caps.evaluate((root) => {
    const out: string[] = []
    const chips = [...root.querySelectorAll<HTMLElement>('.insp-chip')]
    const rects = chips.map((c) => c.getBoundingClientRect())
    chips.forEach((c, i) => {
      if (c.scrollWidth > c.clientWidth + 1 || c.scrollHeight > c.clientHeight + 1) out.push(`clipped: ${c.textContent}`)
      const parent = c.parentElement!.getBoundingClientRect()
      if (rects[i]!.right > parent.right + 0.5) out.push(`overflows row: ${c.textContent}`)
      for (let j = i + 1; j < chips.length; j++) {
        const a = rects[i]!
        const b = rects[j]!
        if (a.left < b.right - 0.5 && b.left < a.right - 0.5 && a.top < b.bottom - 0.5 && b.top < a.bottom - 0.5) {
          out.push(`overlap: ${c.textContent} / ${chips[j]!.textContent}`)
        }
      }
    })
    for (const dt of root.querySelectorAll<HTMLElement>('dt')) {
      if (dt.scrollWidth > dt.clientWidth + 1 || dt.getClientRects().length !== 1 || dt.getBoundingClientRect().height > 26) out.push(`label wraps: ${dt.textContent}`)
    }
    return { out, chips: chips.length }
  })
  expect(problems.chips).toBeGreaterThan(5)
  expect(problems.out).toEqual([])
})

test('edge condition edit autosaves and survives reload; node drag snaps to the grid', async ({ page }) => {
  const store = freshStore()
  await openEditor(page, store)
  await startFromTemplate(page)

  await hoverEdge(page, 'e_clarify_implement')
  await page.getByTestId('canvas-edge-edit').click()
  await page.getByTestId('edge-when-input').fill('需求已确认')
  await page.getByTestId('edge-done').click()
  await expect(page.getByTestId('edge-label-editor')).toHaveCount(0)

  const node = page.getByTestId('canvas-node-implement')
  const nb = await node.boundingBox()
  if (!nb) throw new Error('node not visible')
  await page.mouse.move(nb.x + 40, nb.y + 14)
  await page.mouse.down()
  await page.mouse.move(nb.x + 40 + 37, nb.y + 14 + 61, { steps: 10 })
  await expect(page.getByTestId('canvas-guides')).toBeAttached()
  await page.mouse.up()

  await expect(page.getByTestId('editor-save-status')).toContainText('已保存', { timeout: 10_000 })
  await expect
    .poll(() => (store.wf.edges as { id: string; when?: string }[]).find((e) => e.id === 'e_clarify_implement')?.when)
    .toBe('需求已确认')
  const saved = (store.wf.nodes as { id: string; position: { x: number; y: number } }[]).find((n) => n.id === 'implement')!
  expect(saved.position.x % 8).toBe(0)
  expect(saved.position.y % 8).toBe(0)

  await page.reload()
  await expect(page.getByTestId('workflow-canvas')).toBeVisible({ timeout: 15_000 })
  await expect(nodes(page)).toHaveCount(5)
  await expect(page.getByTestId('empty-canvas')).toHaveCount(0)
  const label = page.getByTestId('canvas-edge-mid-e_clarify_implement').getByTestId('canvas-edge-label')
  await expect(label).toHaveText('需求已确认')
  await label.click()
  await expect(page.getByTestId('edge-when-input')).toHaveValue('需求已确认')
})

test('run canvas shows running and failed node states', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/workflow-canvas.html?view=run&scenario=running')
  await expect(page.getByTestId('canvas-node-implement')).toHaveAttribute('data-status', 'running')
  await expect(page.getByTestId('canvas-node-clarify')).toHaveAttribute('data-status', 'completed')
  await expect(page.getByTestId('canvas-node-implement').getByTestId('canvas-node-iteration')).toBeVisible()
  await expect(page.getByTestId('canvas-palette')).toHaveCount(0)

  await page.goto('/workflow-canvas.html?view=run&scenario=failed')
  await expect(page.getByTestId('canvas-node-test_review')).toHaveAttribute('data-status', 'failed')
  await expect(page.getByTestId('canvas-node-fail')).toContainText('登录接口返回 500')
})

test('run canvas draws every edge when the graph arrives after mount', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/workflow-canvas.html?view=run&scenario=running&async=1')
  await expect(page.locator('.vue-flow__node')).toHaveCount(5)
  await expect(page.locator('path.cedge-path')).toHaveCount(5)
  await page.waitForTimeout(400)
  await page.screenshot({ path: `${SHOT}/run-async.png` })
})

test('editor: dragging blank space pans, Shift + drag box-selects', async ({ page }) => {
  await openEditor(page, freshStore())
  await startFromTemplate(page)
  const transform = () => page.locator('.vue-flow__transformationpane').getAttribute('style')
  const pane = await paneBox(page)
  const before = await transform()
  await page.mouse.move(pane.x + 40, pane.y + pane.height - 60)
  await page.mouse.down()
  await page.mouse.move(pane.x + 140, pane.y + pane.height - 120, { steps: 6 })
  await page.mouse.up()
  await expect.poll(transform).not.toBe(before)
  await expect(page.locator('.vue-flow__node.selected')).toHaveCount(0)

  const first = (await nodes(page).first().boundingBox())!
  await page.keyboard.down('Shift')
  await page.mouse.move(first.x - 30, first.y - 30)
  await page.mouse.down()
  await page.mouse.move(first.x + first.width + 20, first.y + first.height + 20, { steps: 6 })
  await page.mouse.up()
  await page.keyboard.up('Shift')
  await expect(page.locator('.vue-flow__node.selected')).not.toHaveCount(0)
})

test('mouse wheel zooms the canvas', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/workflow-canvas.html?view=run&scenario=running')
  await expect(page.locator('.vue-flow__node')).toHaveCount(5)
  await page.waitForTimeout(400)
  const zoom = page.getByTestId('canvas-zoom')
  const before = await zoom.textContent()
  await page.mouse.move(400, 300)
  await page.mouse.wheel(0, -400)
  await expect(zoom).not.toHaveText(before || '')
})

for (const theme of ['light', 'dark'] as const) {
  test(`screenshots (${theme}): empty, default, running, failed`, async ({ page }) => {
    await openEditor(page, freshStore(), theme)
    await expect(page.getByTestId('empty-canvas')).toBeVisible()
    await page.screenshot({ path: `${SHOT}/${theme}-empty.png` })
    await startFromTemplate(page)
    await page.mouse.move(0, 0)
    await page.waitForTimeout(400)
    await page.screenshot({ path: `${SHOT}/${theme}-default.png` })
    for (const scenario of ['running', 'failed'] as const) {
      await page.goto(`/workflow-canvas.html?view=run&scenario=${scenario}&theme=${theme}`)
      await expect(page.locator('.vue-flow__node')).toHaveCount(5)
      await page.waitForTimeout(600)
      await page.screenshot({ path: `${SHOT}/${theme}-${scenario}.png` })
    }
  })
}

test('200-node graph stays responsive while dragging', async ({ page }) => {
  const big: Graph = { nodes: [], edges: [] }
  for (let i = 0; i < 200; i++) {
    big.nodes.push({
      id: `n${i}`,
      type: i === 0 ? 'input' : 'set_var',
      label: `节点 ${i}`,
      position: { x: (i % 20) * 280, y: Math.floor(i / 20) * 120 },
      config: i === 0 ? { variables: [] } : { assignments: [] },
    })
    if (i > 0) big.edges.push({ id: `e${i}`, source: `n${i - 1}`, target: `n${i}` })
  }
  await openEditor(page, freshStore(big))
  await expect(nodes(page).first()).toBeVisible()
  const nb = await page.getByTestId('canvas-node-n0').boundingBox()
  if (!nb) throw new Error('node not visible')
  await page.evaluate(() => {
    const w = window as unknown as { __frames: number[]; __stop: boolean }
    w.__frames = []
    w.__stop = false
    const tick = (ts: number) => {
      w.__frames.push(ts)
      if (!w.__stop) requestAnimationFrame(tick)
    }
    requestAnimationFrame(tick)
  })
  await page.mouse.move(nb.x + 30, nb.y + 14)
  await page.mouse.down()
  for (let s = 0; s < 60; s++) await page.mouse.move(nb.x + 30 + s * 4, nb.y + 14 + s * 2)
  await page.mouse.up()
  const fps = await page.evaluate(() => {
    const w = window as unknown as { __frames: number[]; __stop: boolean }
    w.__stop = true
    const f = w.__frames
    return ((f.length - 1) * 1000) / (f[f.length - 1] - f[0])
  })
  console.log(`[canvas-perf] 200 nodes drag: ${fps.toFixed(1)} fps`)
  expect(fps).toBeGreaterThan(30)
})
