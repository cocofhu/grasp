/**
 * Regression: a silent inbox auto-apply must never interrupt typing.
 * Repro (GraspReview r2): type a draft while new gates arrive (update held),
 * clear it with Ctrl+A / Backspace and keep typing → the held update applied,
 * the chat re-rendered, the composer lost focus and the following keys were lost.
 * Real /gates entry (workspace-config harness = full app shell), mocked API.
 */
import { expect, test, type Page } from '@playwright/test'
import { installWorkspaceApiDefaults } from './helpers/workspaceApiDefaults'
import { routeApi } from './helpers/apiRoute'
import { CLARIFY_CAPS } from '../src/test/capsFixtures'

const NOW = '2026-10-21T01:00:00Z'
const RUN = 'run-typing-hold'
const NODE = 'clarify'

function clarifyRow(runId: string, title: string) {
  return {
    type: 'clarify',
    kind: 'clarify',
    runId,
    nodeId: NODE,
    iteration: 1,
    workflowId: 'wf-typing',
    workflowName: '输入保护工作流',
    runTitle: title,
    label: '需求澄清',
    done: false,
    requestedAt: NOW,
    updatedAt: NOW,
    tags: [],
  }
}

async function mockInbox(page: Page, state: { items: Record<string, unknown>[] }) {
  await installWorkspaceApiDefaults(page)
  await routeApi(page, '**/api/**', async (route) => {
    const req = route.request()
    const url = new URL(req.url())
    const path = url.pathname
    if (req.method() === 'GET' && path === '/api/gates') {
      await route.fulfill({ json: { items: state.items, total: state.items.length } })
      return
    }
    if (req.method() === 'GET' && /\/api\/runs\/[^/]+\/inbox-context/.test(path)) {
      await route.fulfill({
        json: {
          type: 'clarify',
          status: 'waiting_human',
          nodes: [{ id: NODE, type: 'agent', caps: CLARIFY_CAPS, label: '需求澄清', position: { x: 0, y: 0 }, config: {} }],
          artifacts: [],
          nodeExecutions: { [NODE]: [{ nodeId: NODE, iteration: 1, status: 'waiting_human', outputs: {} }] },
          clarify: {
            nodeId: NODE,
            iteration: 1,
            turns: [{ role: 'assistant', text: '请补充你的需求。', at: NOW }],
            done: false,
            label: '需求澄清',
          },
        },
      })
      return
    }
    await route.fallback()
  })
}

/** Ask the inbox to peek now (same path as returning to the tab). */
const peekNow = (page: Page) => page.evaluate(() => window.dispatchEvent(new Event('focus')))

test('clearing the draft and typing on keeps focus and every character while updates are pending', async ({ page }) => {
  const state = { items: [clarifyRow(RUN, '输入保护验收')] }
  await mockInbox(page, state)
  await page.setViewportSize({ width: 1280, height: 800 })
  await page.goto(`/workspace-config.html?view=${encodeURIComponent(`/gates?run=${RUN}&node=${NODE}`)}`)

  const input = page.getByTestId('clarify-input')
  await expect(input).toBeVisible({ timeout: 15_000 })
  await input.click()
  await page.keyboard.type('有草稿', { delay: 20 })
  await input.evaluate((el) => {
    ;(el as HTMLElement).dataset.original = 'yes'
    const w = window as unknown as { __blurs: number }
    w.__blurs = 0
    el.addEventListener('blur', () => w.__blurs++)
  })

  // New gates arrive while a draft is in the composer: the list update is held.
  state.items = [clarifyRow('run-arrived-a', '新到达 A'), clarifyRow('run-arrived-b', '新到达 B'), ...state.items]
  await peekNow(page)
  await page.waitForTimeout(600)
  await expect(page.getByTestId('nav-pending-row').filter({ hasText: '新到达 A' })).toHaveCount(0)

  // Clear the draft and keep typing straight away.
  await page.keyboard.press('ControlOrMeta+A')
  await page.keyboard.press('Backspace')
  await page.keyboard.type('继续输入不丢字', { delay: 60 })
  await page.waitForTimeout(300)
  await page.keyboard.type('还在打', { delay: 60 })
  await expect(input).toHaveValue('继续输入不丢字还在打')
  await expect(input).toBeFocused()

  // Clear again and stop typing: once input is idle the held update applies
  // without moving focus, the caret or the composer element.
  await page.keyboard.press('ControlOrMeta+A')
  await page.keyboard.press('Backspace')
  await expect(page.getByTestId('nav-pending-row').filter({ hasText: '新到达 A' })).toBeVisible({ timeout: 6_000 })
  await expect(input).toBeFocused()
  await page.keyboard.type('更新后继续', { delay: 30 })
  await expect(input).toHaveValue('更新后继续')
  expect(await input.evaluate((el) => (el as HTMLElement).dataset.original)).toBe('yes')
  expect(await page.evaluate(() => (window as unknown as { __blurs: number }).__blurs)).toBe(0)
  expect(await input.evaluate((el) => (el as HTMLTextAreaElement).selectionStart)).toBe('更新后继续'.length)
})
