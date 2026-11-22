import { beforeAll, describe, expect, it } from 'vitest'
import { BUILTIN_MCPS } from './mcp'
import { i18n } from '@/lib/shared/i18n'
import { loadLocaleMessages } from '@/lib/shared/loadLocaleMessages'

const ARTIFACT_TOOL_COUNT = 34
const ARTIFACT_REQUIRED_TOOLS = ['list_run_history', 'get_history_detail', 'node_complete', 'set_artifact_preview', 'set_preflight', 'get_preflight', 'ask_form', 'set_root_cause', 'get_root_cause'] as const

const INTEGRATIONS_SCOPE_KEYS = [
  'mcp.integrations.scopeRun',
  'mcp.integrations.alwaysAvailable',
  'mcp.integrations.noConfigNeeded',
  'mcp.integrations.io.write',
  'mcp.integrations.io.read',
] as const

beforeAll(async () => {
  const [zh, en] = await Promise.all([
    loadLocaleMessages('zh-CN'),
    loadLocaleMessages('en'),
  ])
  i18n.global.setLocaleMessage('zh-CN', zh)
  i18n.global.setLocaleMessage('en', en)
})

describe('BUILTIN_MCPS grasp-tool catalog', () => {
  const store = BUILTIN_MCPS.find((m) => m.id === 'grasp-tool')

  it('exposes grasp-tool with the catalog tools including history, node_complete, and root_cause', () => {
    expect(store).toBeDefined()
    expect(store!.tools).toHaveLength(ARTIFACT_TOOL_COUNT)
    const names = store!.tools.map((t) => t.name)
    for (const name of ARTIFACT_REQUIRED_TOOLS) {
      expect(names).toContain(name)
    }
  })

  it.each(['zh-CN', 'en'] as const)('%s mcp i18n keys are complete', (locale) => {
    i18n.global.locale.value = locale
    const te = i18n.global.te.bind(i18n.global)

    expect(te('mcp.integrations.toolCount')).toBe(true)
    expect(te('mcp.integrations.backToCatalog')).toBe(true)
    expect(te('mcp.integrations.toolsTitle')).toBe(true)
    expect(te('mcp.integrations.alwaysAvailable')).toBe(true)
    expect(te(store!.descKey)).toBe(true)
    expect(te(store!.overviewKey!)).toBe(true)
    expect(te(store!.conventionKey!)).toBe(true)

    for (const tool of store!.tools) {
      expect(te(tool.descKey), `${locale} missing ${tool.descKey}`).toBe(true)
      expect(te(tool.signatureKey), `${locale} missing ${tool.signatureKey}`).toBe(true)
      expect(i18n.global.t(tool.descKey)).not.toMatch(/^mcp\./)
      expect(i18n.global.t(tool.signatureKey)).not.toMatch(/^mcp\./)
    }
  })
})

describe('BUILTIN_MCPS platform catalog', () => {
  it('lists grasp-tool as the only built-in MCP', () => {
    expect(BUILTIN_MCPS.map((m) => m.id)).toEqual(['grasp-tool'])
  })

  it.each(['zh-CN', 'en'] as const)('%s integrations i18n keys are complete', (locale) => {
    i18n.global.locale.value = locale
    const te = i18n.global.te.bind(i18n.global)
    for (const key of INTEGRATIONS_SCOPE_KEYS) {
      expect(te(key), `${locale} missing ${key}`).toBe(true)
      expect(i18n.global.t(key)).not.toMatch(/^mcp\./)
    }
  })

  it.each(['zh-CN', 'en'] as const)('%s integrations copy avoids all-always-available implication', (locale) => {
    i18n.global.locale.value = locale
    const subtitle = String(i18n.global.t('mcp.integrations.subtitle'))
    const sectionTitle = String(i18n.global.t('mcp.integrations.sectionTitle'))
    if (locale === 'zh-CN') {
      expect(subtitle).not.toMatch(/^内置 MCP 全程可用/)
      expect(sectionTitle).not.toMatch(/运行级全局注入/)
      expect(sectionTitle).toMatch(/按作用域/)
    } else {
      expect(subtitle.toLowerCase()).not.toMatch(/^built-in mcp is always available/)
      expect(sectionTitle.toLowerCase()).not.toMatch(/run-scoped injection$/)
      expect(sectionTitle.toLowerCase()).toMatch(/scope/)
    }
  })
})
