import { beforeAll, describe, expect, it } from 'vitest'
import { BUILTIN_MCPS } from './mcp'
import { i18n } from '@/lib/shared/i18n'
import { loadLocaleMessages } from '@/lib/shared/loadLocaleMessages'

const ARTIFACT_TOOL_COUNT = 29
const ARTIFACT_REQUIRED_TOOLS = ['list_run_history', 'get_history_detail', 'node_complete', 'set_artifact_preview', 'set_preflight', 'get_preflight', 'ask_form', 'set_root_cause', 'get_root_cause'] as const

const CATALOG = [
  {
    id: 'memory-store',
    scope: 'agent' as const,
    toolCount: 5,
    writeTools: ['upsert_memory', 'delete_memory'],
  },
  {
    id: 'context-store',
    scope: 'agent' as const,
    toolCount: 5,
    writeTools: [] as string[],
  },
  {
    id: 'task-scheduler',
    scope: 'agent' as const,
    toolCount: 6,
    writeTools: ['create_job', 'update_job', 'delete_job', 'run_job_now'],
  },
  {
    id: 'pm-progress',
    scope: 'project' as const,
    toolCount: 6,
    writeTools: [] as string[],
  },
  {
    id: 'pm-workflow-read',
    scope: 'project' as const,
    toolCount: 7,
    writeTools: [] as string[],
  },
  {
    id: 'pm-workflow-write',
    scope: 'project' as const,
    toolCount: 9,
    writeTools: [
      'pm_create_workflow',
      'pm_update_workflow',
      'pm_copy_workflow',
      'pm_delete_workflow',
      'pm_publish_workflow',
      'pm_start_run',
      'pm_resume_gate',
      'pm_react_reply',
      'pm_cancel_run',
    ],
  },
  {
    id: 'pm-agent-fs',
    scope: 'project' as const,
    toolCount: 14,
    writeTools: [
      'pm_fs_write',
      'pm_fs_delete',
      'pm_fs_mkdir',
      'pm_fs_rename',
      'pm_fs_restore',
      'pm_create_agent_from_template',
      'pm_set_org_membership',
      'pm_ensure_child_group',
    ],
  },
  {
    id: 'pm-prd-manager',
    scope: 'project' as const,
    toolCount: 3,
    writeTools: ['pm_create_requirement_draft'],
  },
] as const

const INTEGRATIONS_SCOPE_KEYS = [
  'mcp.integrations.scopeRun',
  'mcp.integrations.scopeProject',
  'mcp.integrations.scopeAgent',
  'mcp.integrations.alwaysAvailable',
  'mcp.integrations.seenInPmLeader',
  'mcp.integrations.seenInAgentStudio',
  'mcp.integrations.addViaAgentStudio',
  'mcp.integrations.notRunScoped',
  'mcp.integrations.noConfigNeeded',
  'mcp.integrations.noConfigHere',
  'mcp.integrations.entryPath',
  'mcp.integrations.entryPathAgent',
  'mcp.integrations.entryPathLabel',
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

describe('BUILTIN_MCPS artifact-store catalog', () => {
  const store = BUILTIN_MCPS.find((m) => m.id === 'artifact-store')

  it('exposes artifact-store with the catalog tools including history, node_complete, and root_cause', () => {
    expect(store).toBeDefined()
    expect(store!.scope).toBe('run')
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
    expect(te('mcp.integrations.openDetail')).toBe(true)
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
  it('does not expose legacy pm-leader', () => {
    expect(BUILTIN_MCPS.find((m) => m.id === 'pm-leader')).toBeUndefined()
  })

  it.each(CATALOG)('$id has expected scope, tools, and write set', (entry) => {
    const mcp = BUILTIN_MCPS.find((m) => m.id === entry.id)
    expect(mcp).toBeDefined()
    expect(mcp!.name).toBe(entry.id)
    expect(mcp!.scope).toBe(entry.scope)
    expect(mcp!.tools).toHaveLength(entry.toolCount)
    expect(mcp!.tools.filter((t) => t.io === 'write').map((t) => t.name).sort()).toEqual(
      [...entry.writeTools].sort(),
    )
  })

  it.each(['zh-CN', 'en'] as const)('%s platform MCP i18n keys are complete', (locale) => {
    i18n.global.locale.value = locale
    const te = i18n.global.te.bind(i18n.global)

    for (const key of INTEGRATIONS_SCOPE_KEYS) {
      expect(te(key), `${locale} missing ${key}`).toBe(true)
      expect(i18n.global.t(key)).not.toMatch(/^mcp\./)
    }

    for (const entry of CATALOG) {
      const mcp = BUILTIN_MCPS.find((m) => m.id === entry.id)!
      expect(te(mcp.descKey)).toBe(true)
      expect(te(mcp.overviewKey!)).toBe(true)
      expect(te(mcp.conventionKey!)).toBe(true)
      for (const tool of mcp.tools) {
        expect(te(tool.descKey), `${locale} missing ${tool.descKey}`).toBe(true)
        expect(te(tool.signatureKey), `${locale} missing ${tool.signatureKey}`).toBe(true)
        expect(i18n.global.t(tool.descKey)).not.toMatch(/^mcp\./)
        expect(i18n.global.t(tool.signatureKey)).not.toMatch(/^mcp\./)
      }
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
