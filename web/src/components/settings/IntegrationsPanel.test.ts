// @vitest-environment happy-dom
import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import common from '@/locales/zh-CN/common.json'
import mcp from '@/locales/zh-CN/mcp.json'
import IntegrationsPanel from './IntegrationsPanel.vue'
import { BUILTIN_MCPS } from '@/data/mcp'

function mountPanel() {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh-CN',
    messages: { 'zh-CN': { ...common, ...mcp } },
  })
  return mount(IntegrationsPanel, {
    global: {
      plugins: [i18n],
      stubs: { Icon: true },
    },
  })
}

describe('IntegrationsPanel', () => {
  it('shows the complete MCP catalog without a modal', async () => {
    const w = mountPanel()
    await flushPromises()
    expect(w.find('[data-testid="integrations-panel"]').exists()).toBe(true)
    expect(w.find('[data-testid="integrations-panel-catalog"]').exists()).toBe(true)
    expect(w.findAll('[data-testid="integrations-mcp-card"]').length).toBe(BUILTIN_MCPS.length)
    expect(w.find('[role="dialog"]').exists()).toBe(false)
    w.unmount()
  })

  it('switches to detail in the same pane and can return', async () => {
    const w = mountPanel()
    await flushPromises()
    await w.findAll('[data-testid="integrations-mcp-card"]')[0]!.trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="integrations-panel-detail"]').exists()).toBe(true)
    expect(w.find('[data-testid="integrations-panel-catalog"]').exists()).toBe(false)
    expect(w.find('[data-testid="integrations-panel-back"]').exists()).toBe(true)
    await w.find('[data-testid="integrations-panel-back"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="integrations-panel-catalog"]').exists()).toBe(true)
    w.unmount()
  })
  it('preserves every integration tool, signature, availability, and usage convention', async () => {
    const w = mountPanel()
    await flushPromises()
    for (const [index, integration] of BUILTIN_MCPS.entries()) {
      await w.findAll('[data-testid="integrations-mcp-card"]')[index]!.trigger('click')
      const detail = w.get('[data-testid="integrations-panel-detail"]')
      const rows = detail.findAll('[data-testid="integration-tool-row"]')
      expect(rows).toHaveLength(integration.tools.length)
      for (const [toolIndex, tool] of integration.tools.entries()) {
        expect(rows[toolIndex]!.text()).toContain(tool.name)
        expect(rows[toolIndex]!.findAll('code').length).toBeGreaterThan(0)
        expect(rows[toolIndex]!.text()).toContain(tool.io)
      }
      if (integration.scope === 'workspace') expect(detail.text()).toContain('设置 → 助手设置')
      if (integration.scope === 'agent') expect(detail.text()).toContain('Agent Studio → MCP')
      if (integration.scope === 'run') expect(detail.text()).toContain('全程可用')
      if (integration.conventionKey) expect(detail.find('p').exists()).toBe(true)
      await w.get('[data-testid="integrations-panel-back"]').trigger('click')
    }
    w.unmount()
  })

})
