// @vitest-environment node
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const viewsDir = dirname(fileURLToPath(import.meta.url))
const detailSrc = readFileSync(join(viewsDir, 'ProjectDetailView.vue'), 'utf8')

function mobileWorkflowsBlock(): string {
  const start = detailSrc.indexOf('<!-- Mobile card list:')
  const end = detailSrc.indexOf('<!-- Desktop table -->')
  expect(start, 'missing mobile workflows card list').toBeGreaterThanOrEqual(0)
  expect(end, 'missing desktop table marker').toBeGreaterThan(start)
  return detailSrc.slice(start, end)
}

function desktopWorkflowsBlock(): string {
  const start = detailSrc.indexOf('<!-- Desktop table -->')
  const end = detailSrc.indexOf('<!-- Agents: embed')
  expect(start, 'missing desktop table marker').toBeGreaterThanOrEqual(0)
  expect(end, 'missing agents tab marker').toBeGreaterThan(start)
  return detailSrc.slice(start, end)
}

describe('ProjectDetailView pipelines desktop table layout (g1.2 / g1.3)', () => {
  it('table keeps w-full plus a min-width so narrow columns trigger horizontal scroll', () => {
    const desktop = desktopWorkflowsBlock()
    const tableStart = desktop.indexOf('<table')
    expect(tableStart, 'missing desktop workflows table').toBeGreaterThanOrEqual(0)
    const tableTag = desktop.slice(tableStart, desktop.indexOf('>', tableStart) + 1)
    expect(tableTag).toMatch(/\bw-full\b/)
    expect(tableTag).toMatch(/min-w-\[1080px\]/)
    // Existing overflow-x-auto wrapper is what actually scrolls the min-width table.
    expect(desktop).toMatch(/scroll-area overflow-x-auto/)
  })

  it('action column never wraps: removes flex-wrap and keeps every button single-line', () => {
    const desktop = desktopWorkflowsBlock()
    const groupStart = desktop.indexOf('data-testid="wf-actions-group"')
    expect(groupStart, 'missing wf-actions-group').toBeGreaterThanOrEqual(0)
    const group = desktop.slice(desktop.lastIndexOf('<div', groupStart), desktop.indexOf('</td>', groupStart))
    expect(group).not.toMatch(/flex-wrap/)
    expect(group).toMatch(/whitespace-nowrap/)
    // edit / run / favorite / copy / export / delete all forced onto one line.
    expect(group.match(/whitespace-nowrap/g)?.length).toBeGreaterThanOrEqual(7)
  })

  it('header labels of Home visibility and Updated are nowrap so they do not break mid-word', () => {
    const desktop = desktopWorkflowsBlock()
    const thead = desktop.slice(desktop.indexOf('<thead'), desktop.indexOf('</thead>'))
    const headers = thead.match(/<th\b[\s\S]*?<\/th>/g) ?? []
    expect(headers).toHaveLength(6)
    for (const th of headers) {
      expect(th).toMatch(/whitespace-nowrap/)
    }
    const homeTh = headers.find((h) => h.includes('homeVisibility.col'))
    const updatedTh = headers.find((h) => h.includes('colUpdated'))
    expect(homeTh).toBeTruthy()
    expect(updatedTh).toBeTruthy()
  })
})

describe('ProjectDetailView pipelines mobile card layout regression (g1.4)', () => {
  it('mobile branch stays a card list and is not given the desktop min-width', () => {
    const mobile = mobileWorkflowsBlock()
    expect(mobile).toMatch(/data-testid="wf-notify-inline"/)
    expect(mobile).toMatch(/data-testid="wf-home-visibility-inline"/)
    expect(mobile).not.toMatch(/<table/)
    expect(mobile).not.toMatch(/min-w-\[1080px\]/)
  })
})
