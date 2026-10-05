// @vitest-environment node
/**
 * plan g3.1 — static contract: list A pages + ProjectDetail list B tabs
 * each have exactly one primary vertical scroll exit (fill-height + overflow-y-auto).
 */
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const dir = dirname(fileURLToPath(import.meta.url))

function read(name: string) {
  return readFileSync(join(dir, name), 'utf8')
}

const FILL_ROOT = /flex h-full min-h-0 flex-col/
const SCROLL_EXIT = /overflow-y-auto/

describe('mobile scroll contract — list A (plan g1 / g3.1)', () => {
  it('ProjectListView: fill root, shrink-0 header, list overflow-y-auto (g1.1)', () => {
    const src = read('ProjectListView.vue')
    expect(src).toMatch(FILL_ROOT)
    expect(src).toMatch(/mb-5 flex shrink-0/)
    expect(src).toMatch(/data-testid="project-list-panel"[\s\S]*?class="min-h-0 flex-1 overflow-y-auto py-0\.5"/)
    expect(src).not.toMatch(/^\s*<div>\s*$/m)
  })

  it('SettingsView: fill root + content overflow-y-auto (g1.2)', () => {
    const src = read('SettingsView.vue')
    expect(src).toMatch(/data-testid="settings-panel"/)
    expect(src).toMatch(FILL_ROOT)
    expect(src).toMatch(/mb-5 flex shrink-0/)
    expect(src).toMatch(/min-h-0 flex-1 overflow-y-auto/)
  })

  it('SandboxListView: fill root; mobile list scrolls; desktop table scrolls (g1.2)', () => {
    const src = read('SandboxListView.vue')
    expect(src).toMatch(FILL_ROOT)
    expect(src).toMatch(/mb-5 flex shrink-0/)
    expect(src).toMatch(/v-if="isMobile"[\s\S]*?class="min-h-0 flex-1 overflow-y-auto py-0\.5"/)
    expect(src).toMatch(/card flex min-h-0 flex-1 flex-col overflow-hidden/)
    expect(src).toMatch(/scroll-area min-h-0 flex-1 overflow-auto/)
  })

  // Integrations remain inside the settings chrome and own the right-pane scroll exit.
  it('settings integrations panel owns catalog and detail scroll', () => {
    const panel = readFileSync(
      join(dir, '../components/settings/IntegrationsPanel.vue'),
      'utf8',
    )
    expect(panel).toMatch(/data-testid="integrations-panel"/)
    expect(panel).toMatch(/data-testid="integrations-panel-catalog"/)
    expect(panel).toMatch(/data-testid="integrations-panel-detail"/)
    expect(panel).toMatch(/data-testid="integrations-panel-scroll"/)
    expect(panel).toMatch(/min-h-0 flex-1 overflow-y-auto/)
    expect(panel).not.toMatch(/AppModal/)
    const settings = read('SettingsView.vue')
    expect(settings).toMatch(/IntegrationsPanel v-if="showIntegrations"/)
    expect(settings).not.toMatch(/data-testid="settings-integrations-card"/)
    expect(settings).not.toMatch(/data-testid="settings-integrations-open"/)
  })

  it('BoardView: fill root + body overflow-y-auto for standalone and embedded (g1.3)', () => {
    const src = read('BoardView.vue')
    expect(src).toMatch(/data-testid="board-view"[^>]*flex h-full min-h-0/)
    expect(src).toMatch(/min-h-0 flex-1 overflow-y-auto/)
    expect(src).toMatch(/v-if="!embedded"/)
  })

  it('DashboardView: always h-full + content overflow-y-auto (g1.4)', () => {
    const src = read('DashboardView.vue')
    expect(src).toMatch(/data-testid="dashboard-view"[^>]*h-full min-h-0/)
    expect(src).toMatch(/home-shell__content[^>]*overflow-y-auto/)
  })
})

describe('mobile scroll contract — list B ProjectDetail tabs (plan g2.1 / g3.1)', () => {
  const detail = read('ProjectDetailView.vue')

  function tabBlock(marker: string, nextMarkers: string[]): string {
    const start = detail.indexOf(marker)
    expect(start, `missing ${marker}`).toBeGreaterThanOrEqual(0)
    let end = detail.length
    for (const n of nextMarkers) {
      const i = detail.indexOf(n, start + marker.length)
      if (i > start && i < end) end = i
    }
    return detail.slice(start, end)
  }

  it('workflows tab: min-h-0 flex-1 + scroll-area overflow-y-auto', () => {
    const block = tabBlock("<!-- Workflows tab", ["<!-- Sandbox env tab"])
    expect(block).toMatch(/class="flex min-h-0 flex-1 flex-col overflow-hidden"/)
    expect(block).toMatch(/scroll-area min-h-0 flex-1 overflow-y-auto/)
    expect(block).toMatch(/mb-3 flex shrink-0 justify-end gap-2/)
  })

  it('board tab: flex-1 min-h-0 host for BoardView scroll exit', () => {
    const block = tabBlock("tab === 'board'", ["tab === 'requirementDrafts'"])
    expect(block).toMatch(/flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden/)
    expect(block).toMatch(/data-testid="project-board-panel"/)
  })

  it('requirementDrafts tab: flex-1 min-h-0 host for panel fill-height (like board)', () => {
    const block = tabBlock("tab === 'requirementDrafts'", ["tab === 'pmLeader'"])
    expect(block).toMatch(/flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden/)
    expect(block).toMatch(/data-testid="project-requirement-drafts-panel"/)
    expect(block).toMatch(/RequirementDraftsPanel[\s\S]*?class="min-h-0 flex-1"/)
    const panelSrc = readFileSync(
      join(dir, '../components/project/RequirementDraftsPanel.vue'),
      'utf8',
    )
    expect(panelSrc).toMatch(/data-testid="requirement-drafts-panel"/)
    expect(panelSrc).toMatch(/flex h-full min-h-0 min-w-0 flex-1 flex-col/)
    expect(panelSrc).toMatch(/overflow-(auto|y-auto)/)
  })

  it('cronJobs / notify tabs: drop 420 floor; use overflow-y-auto', () => {
    const cron = tabBlock("tab === 'cronJobs'", ["tab === 'notify'"])
    const notify = tabBlock("tab === 'notify' && project", ["<!-- Workflows tab"])
    expect(cron).toMatch(/scroll-area flex min-h-0 flex-1 flex-col overflow-y-auto/)
    expect(notify).toMatch(/scroll-area min-h-0 flex-1 overflow-y-auto/)
    expect(cron).not.toMatch(/min-h-\[420px\]/)
    expect(notify).not.toMatch(/min-h-\[420px\]/)
  })

  it('existing fill tabs keep scroll exits (g2.2 regression)', () => {
    expect(detail).toMatch(/tab === 'meta'" class="flex min-h-0 flex-1 flex-col"/)
    expect(detail).toMatch(/tab === 'audit'" class="flex min-h-0 flex-1 flex-col"/)
    expect(detail).toMatch(/tab === 'variables'" class="flex min-h-0 flex-1 flex-col"/)
    expect(detail).toMatch(/tab === 'agents'" class="flex min-h-0 flex-1 flex-col"/)
    expect(detail).toMatch(/tab === 'sharedAgent'" class="flex min-h-0 flex-1 flex-col"/)
    expect(detail).toMatch(/tab === 'pmLeader'[\s\S]*?class="flex min-h-0 flex-1 flex-col"/)
    expect(detail).toMatch(
      /scroll-area flex min-h-0 flex-1 flex-col gap-3\.5 overflow-y-auto p-4/,
    )
  })
})

describe('mobile scroll contract — shell single-exit lock (plan g3.2)', () => {
  it('keeps route-view-wrap height:100% overflow:hidden (page-local scroll)', () => {
    const globalCss = readFileSync(join(dir, '../styles/global.css'), 'utf8')
    const appSrc = readFileSync(join(dir, '../App.vue'), 'utf8')
    const shellSrc = readFileSync(join(dir, '../components/shell/AppShell.vue'), 'utf8')
    expect(appSrc).toMatch(/route-view-wrap/)
    expect(globalCss).toMatch(/\.route-view-wrap\s*\{[^}]*height:\s*100%/s)
    expect(globalCss).toMatch(/\.route-view-wrap\s*\{[^}]*overflow:\s*hidden/s)
    expect(shellSrc).toMatch(/h-screen/)
    expect(shellSrc).toMatch(/min-h-0 flex-1/)
    // shell outer may overflow-y-auto but inner h-full means page must own the exit
    expect(shellSrc).toMatch(/flex h-full min-h-0 flex-col/)
  })

  it('sampling fill pages still declare internal overflow-y-auto', () => {
    expect(read('RunListView.vue')).toMatch(/overflow-y-auto/)
    expect(read('NotificationsView.vue')).toMatch(/min-h-0 flex-1 overflow-y-auto/)
    expect(read('ArtifactsView.vue')).toMatch(/overflow-y-auto/)
    expect(read('GatesInboxView.vue')).toMatch(/overflow-y-auto/)
  })
})
