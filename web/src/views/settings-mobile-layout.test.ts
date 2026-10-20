import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const dir = dirname(fileURLToPath(import.meta.url))

function read(name: string) {
  return readFileSync(join(dir, name), 'utf8')
}

describe('settings-family narrow-screen stacking (g3)', () => {
  it('Settings uses a natural-height single column with mobile control stacking', () => {
    const src = read('SettingsView.vue')
    const row = readFileSync(join(dir, '../components/settings/SettingsRow.vue'), 'utf8')
    expect(src).toMatch(/max-width: 860px/)
    expect(src).not.toMatch(/overflow-y-auto/)
    expect(src).toMatch(/min-h-11 sm:min-h-9/)
    expect(row).toMatch(/@media \(max-width: 639px\)/)
    expect(row).toMatch(/flex-direction: column; align-items: stretch/)
    expect(row).toMatch(/settings-row-inline \.settings-row-main \{ flex-direction: row; align-items: center/)
    expect(row).toMatch(/'settings-row-inline': inline && !stacked/)
  })

  it('Integrations catalog rows wrap metadata and keep accessible touch targets', () => {
    const panel = readFileSync(join(dir, '../components/settings/IntegrationsPanel.vue'), 'utf8')
    expect(panel).toMatch(/<SettingsRow :label="m.name" :description="t\(m.descKey\)" inline>/)
    expect(panel).toMatch(/flex flex-wrap items-center gap-x-3 gap-y-2/)
    expect(panel).toMatch(/min-h-11 items-center gap-1/)
    expect(panel).toMatch(/data-testid="integrations-panel-back"/)
    expect(panel).not.toMatch(/AppModal|overflow-y-auto|h-full/)
    const settings = read('SettingsView.vue')
    expect(settings).not.toMatch(/data-testid="settings-integrations-open"/)
    expect(settings).not.toMatch(/data-testid="settings-integrations-card"/)
    expect(settings).toMatch(/IntegrationsPanel v-if="showIntegrations"/)
  })

  it('Notifications controls keep touch height', () => {
    expect(read('NotificationsView.vue')).toMatch(/min-h-11 border border-line bg-transparent/)
    expect(read('NotificationsView.vue')).toMatch(/min-h-11 border-b-2 border-transparent px-4/)
  })
})
