// @vitest-environment node
/**
 * Fill-height list pages keep their primary vertical scroll exit.
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
  it('SettingsView delegates vertical scrolling to the settings shell', () => {
    const src = read('SettingsView.vue')
    expect(src).toMatch(/data-testid="settings-panel"/)
    expect(src).toMatch(/class="settings-page"/)
    expect(src).not.toMatch(/overflow-y-auto|class="flex h-full min-h-0 flex-col"/)
  })

  it('SandboxListView: fill root; mobile list scrolls; desktop table scrolls (g1.2)', () => {
    const src = read('SandboxListView.vue')
    expect(src).toMatch(FILL_ROOT)
    expect(src).toMatch(/mb-5 flex shrink-0/)
    expect(src).toMatch(/v-if="isMobile"[\s\S]*?class="min-h-0 flex-1 overflow-y-auto py-0\.5"/)
    expect(src).toMatch(/flex min-h-0 flex-1 flex-col overflow-hidden/)
    expect(src).toMatch(/scroll-area min-h-0 flex-1 overflow-auto/)
  })

  // Settings pages use the shell scroll exit, including integration detail.
  it('settings integrations catalog and detail stay in the shell scroll flow', () => {
    const panel = readFileSync(
      join(dir, '../components/settings/IntegrationsPanel.vue'),
      'utf8',
    )
    expect(panel).toMatch(/data-testid="integrations-panel"/)
    expect(panel).toMatch(/data-testid="integrations-panel-catalog"/)
    expect(panel).toMatch(/data-testid="integrations-panel-detail"/)
    expect(panel).toMatch(/data-testid="integrations-panel-content"/)
    expect(panel).not.toMatch(/overflow-y-auto|h-full/)
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
    expect(shellSrc).toMatch(/flex h-full min-h-0 min-w-0 flex-col/)
  })

  it('sampling fill pages still declare internal overflow-y-auto', () => {
    expect(read('RunListView.vue')).toMatch(/overflow-y-auto/)
    expect(read('NotificationsView.vue')).toMatch(/min-h-0 flex-1 overflow-y-auto/)
    // The artifact canvas delegates independent scrolling to its file and
    // preview panes; the view must keep both height-constrained.
    expect(read('ArtifactsView.vue')).toMatch(/artifact-browser min-h-0 flex-1/)
    const artifactComponents = join(dir, '../components/run')
    expect(readFileSync(join(artifactComponents, 'ArtifactList.vue'), 'utf8')).toMatch(/overflow-y-auto/)
    expect(readFileSync(join(artifactComponents, 'ArtifactPreview.vue'), 'utf8')).toMatch(/overflow-y-auto/)
    expect(read('GatesInboxView.vue')).toMatch(/overflow-y-auto/)
  })
})
