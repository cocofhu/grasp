import { chromium } from 'playwright'
import { fileURLToPath } from 'node:url'
import { dirname, resolve } from 'node:path'

const __dirname = dirname(fileURLToPath(import.meta.url))
const OUT = resolve(__dirname, '../../showcases')
const BASE = process.env.SHOT_BASE || 'http://127.0.0.1:4173'

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

async function shot(page, name) {
  await sleep(700)
  await page.screenshot({ path: resolve(OUT, name) })
  console.log('saved', name)
}

async function goto(page, path) {
  await page.goto(BASE + path, { waitUntil: 'networkidle' })
  await sleep(900)
}

async function clickTab(page, label) {
  const btn = page.getByRole('button', { name: label, exact: true }).first()
  if (await btn.count()) {
    await btn.click().catch(() => {})
    await sleep(700)
  }
}

const run = async () => {
  const browser = await chromium.launch()
  const page = await browser.newPage({ viewport: { width: 1512, height: 950 }, deviceScaleFactor: 2 })

  // 01 dashboard (dark)
  await goto(page, '/dashboard')
  await shot(page, '01-dashboard.png')

  // 02 dashboard (light theme)
  const themeBtn = page.getByRole('button', { name: /切换到浅色|切换到深色/ }).first()
  if (await themeBtn.count()) {
    await themeBtn.click().catch(() => {})
    await sleep(700)
    await shot(page, '02-dashboard-light.png')
    // back to dark for the rest
    const back = page.getByRole('button', { name: /切换到深色|切换到浅色/ }).first()
    if (await back.count()) await back.click().catch(() => {})
    await sleep(500)
  }

  // 03 workflow list
  await goto(page, '/workflows')
  await shot(page, '03-workflow-list.png')

  // 05 workflow canvas (FSM: prd-to-mr)
  await goto(page, '/workflows/prd-to-mr/edit')
  await sleep(1400)
  await shot(page, '05-workflow-canvas.png')

  // 06 node inspector — click a node on the canvas
  const node = page.locator('.vue-flow__node').filter({ hasText: '编码' }).first()
  if (await node.count()) {
    await node.click({ position: { x: 30, y: 16 } }).catch(() => {})
    await sleep(800)
    await shot(page, '06-node-inspector.png')
  }

  // 07 edge inspector (FSM transition) — click a rollback/carry edge label
  const edge = page.locator('.vue-flow__edge').filter({ hasText: /last_error|回滚|rollback/ }).first()
  let edgeShot = false
  if (await edge.count()) {
    await edge.click().catch(() => {})
    await sleep(700)
    if (await page.getByText('转移类型').count()) {
      await shot(page, '07-edge-inspector-fsm.png')
      edgeShot = true
    }
  }
  if (!edgeShot) {
    // fallback: click any edge
    const anyEdge = page.locator('.vue-flow__edge-interaction, .vue-flow__edge').first()
    if (await anyEdge.count()) {
      await anyEdge.click({ force: true }).catch(() => {})
      await sleep(700)
      await shot(page, '07-edge-inspector-fsm.png')
    }
  }

  // 08 run list
  await goto(page, '/runs')
  await shot(page, '08-run-list.png')

  // 09 run detail — running (run-45), node output
  await goto(page, '/runs/run-45')
  await sleep(1400)
  await clickTab(page, '节点输出')
  await shot(page, '09-run-detail-running.png')

  // 10 state trace (run-38 has rollback loop)
  await goto(page, '/runs/run-38')
  await sleep(1400)
  await clickTab(page, '状态轨迹')
  await shot(page, '10-run-state-trace.png')

  // 11 variables (run-45)
  await goto(page, '/runs/run-45')
  await sleep(1300)
  await clickTab(page, '全局变量')
  await shot(page, '11-run-variables.png')

  // 12 gate approval (run-42 waiting_human)
  await goto(page, '/runs/run-42')
  await sleep(1300)
  await clickTab(page, '门禁审批')
  await shot(page, '12-run-gate.png')

  // 13 clarify chat (run-39)
  await goto(page, '/runs/run-39')
  await sleep(1300)
  await clickTab(page, '需求澄清')
  await shot(page, '13-run-clarify.png')

  // 14 run artifacts tab (run-45)
  await goto(page, '/runs/run-45')
  await sleep(1300)
  await clickTab(page, '产物')
  await shot(page, '14-run-artifacts.png')

  // 15 gates inbox
  await goto(page, '/gates')
  await shot(page, '15-gates-inbox.png')

  // 16 artifacts library
  await goto(page, '/artifacts')
  await sleep(900)
  await shot(page, '16-artifacts.png')

  // 17 agent management
  await goto(page, '/agents')
  await shot(page, '17-agent-studio.png')

  // 18 integrations
  await goto(page, '/integrations')
  await shot(page, '18-integrations.png')

  await browser.close()
  console.log('done')
}

run().catch((e) => {
  console.error(e)
  process.exit(1)
})
