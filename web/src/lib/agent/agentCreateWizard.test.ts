import { describe, expect, it } from 'vitest'
import {
  assembleCreatePayload,
  applyAcpBackend,
  applyWizardStartPath,
  buildDefaultRule,
  buildReviewSummary,
  configRootFor,
  freshDraft,
  hasPathDeps,
  envConfiguredCount,
  validateBasics,
  AGENT_SETTINGS_PATH,
  parseCustomConfigJson,
  CLI_BACKENDS,
} from './agentCreateWizard'
import { AGENT_OPENCODE_CONFIG_REL_PATH } from './backendAuthGuide'

describe('freshDraft defaults (g1.2)', () => {
  it('defaults to API Key path with OpenCode', () => {
    const d = freshDraft()
    expect(d.startPath).toBe('apiKey')
    expect(d.acpBackend).toBe('opencode')
    expect(d.cliBackend).toBe('cursor')
    expect(d.configRoot).toBe('/root/.config/opencode')
    expect(CLI_BACKENDS.map((b) => b.id)).toEqual([
      'cursor',
      'claude_code',
      'codebuddy',
      'trae',
      'codex',
    ])
  })

  it('applyWizardStartPath restores last CLI backend', () => {
    const d = freshDraft()
    applyAcpBackend(d, 'codebuddy')
    expect(d.startPath).toBe('cli')
    expect(d.cliBackend).toBe('codebuddy')
    applyWizardStartPath(d, 'apiKey')
    expect(d.acpBackend).toBe('opencode')
    expect(d.configRoot).toBe('/root/.config/opencode')
    applyWizardStartPath(d, 'cli')
    expect(d.acpBackend).toBe('codebuddy')
    expect(d.cliBackend).toBe('codebuddy')
  })
})

describe('configRootFor', () => {
  it('maps backends to protocol roots', () => {
    expect(configRootFor('cursor')).toBe('/root/.cursor')
    expect(configRootFor('claude_code')).toBe('/root/.claude')
    expect(configRootFor('codebuddy')).toBe('/root/.codebuddy')
    expect(configRootFor('trae')).toBe('/root/.trae')
    expect(configRootFor('opencode')).toBe('/root/.config/opencode')
  })
})

describe('buildDefaultRule', () => {
  it('writes alwaysApply identity template', () => {
    const text = buildDefaultRule('demo')
    expect(text).toContain('alwaysApply: true')
    expect(text).toContain('# demo')
    expect(text).toContain('描述该 Agent 的职责与行为。')
  })

  it('prepends description preface when provided', () => {
    const text = buildDefaultRule('demo', '负责代码评审')
    expect(text).toContain('负责代码评审\n\n描述该 Agent 的职责与行为。')
  })
})

describe('assembleCreatePayload', () => {
  it('always writes default rule even when Rules skipped', () => {
    const d = freshDraft()
    d.name = 'reviewer'
    d.description = '审阅 PR'
    d.skipped.rules = true
    const payload = assembleCreatePayload(d)
    expect(payload.files?.some((f) => f.path === 'rules/reviewer.md')).toBe(true)
    const rule = payload.files!.find((f) => f.path === 'rules/reviewer.md')!
    expect(rule.content).toContain('审阅 PR')
    expect(rule.content).toContain('alwaysApply: true')
  })

  it('uses edited rules content when present', () => {
    const d = freshDraft()
    d.name = 'x'
    d.rulesEdited = true
    d.rulesContent = '# custom'
    const payload = assembleCreatePayload(d)
    expect(payload.files![0].content).toBe('# custom')
  })

  it('omits prompts when skipped or all empty', () => {
    const d = freshDraft()
    d.name = 'x'
    d.skipped.prompts = true
    d.prompts.producesContract = 'should-not-send'
    expect(assembleCreatePayload(d).prompts).toBeUndefined()

    const d2 = freshDraft()
    d2.name = 'y'
    expect(assembleCreatePayload(d2).prompts).toBeUndefined()
  })

  it('includes non-empty prompt overrides', () => {
    const d = freshDraft()
    d.name = 'x'
    d.prompts.reactOpenSuffix = 'hello'
    expect(assembleCreatePayload(d).prompts).toEqual({ reactOpenSuffix: 'hello' })
  })

  it('maps skills and commands into files[]', () => {
    const d = freshDraft()
    d.name = 'x'
    d.skills = [{ name: 'git', content: '# git skill' }]
    d.commands = [{ name: 'ship', content: '# ship' }]
    const paths = assembleCreatePayload(d).files!.map((f) => f.path)
    expect(paths).toContain('skills/git/SKILL.md')
    expect(paths).toContain('commands/ship.md')
  })

  it('includes command-transport MCP env in payload', () => {
    const d = freshDraft()
    d.name = 'x'
    d.mcp = [
      {
        name: 'local-mcp',
        transport: 'command',
        url: '',
        headers: [],
        command: 'npx',
        args: '-y\n@example/mcp',
        env: [
          { k: 'API_KEY', v: 'secret' },
          { k: '', v: 'ignored' },
        ],
      },
    ]
    const mcp = assembleCreatePayload(d).mcp!
    expect(mcp).toHaveLength(1)
    expect(mcp[0]).toEqual({
      name: 'local-mcp',
      command: 'npx',
      args: ['-y', '@example/mcp'],
      env: { API_KEY: 'secret' },
    })
  })

  it('sets layout.configRoot from backend', () => {
    const d = freshDraft()
    d.name = 'x'
    d.acpBackend = 'claude_code'
    d.configRoot = '/root/.claude'
    expect(assembleCreatePayload(d).layout?.configRoot).toBe('/root/.claude')
    expect(assembleCreatePayload(d).acpBackend).toBe('claude_code')
  })

  it('includes the confirmed Git credential type in the create payload', () => {
    const d = freshDraft()
    d.name = 'x'
    d.gitCredentialType = 'ssh'
    expect(assembleCreatePayload(d).gitCredentialType).toBe('ssh')
  })

  it('strictly normalizes region env without adding a top-level region', () => {
    const d = freshDraft()
    d.name = 'x'
    d.acpBackend = 'trae'
    d.env = [
      { k: 'GRASP_CODEBUDDY_REGION', v: 'internal' },
      { k: 'GRASP_TRAE_REGION', v: 'bad' },
      { k: 'OTHER', v: 'ok' },
    ]
    const payload = assembleCreatePayload(d)
    expect(payload.env).toEqual({ GRASP_TRAE_REGION: 'intl', OTHER: 'ok' })
    expect(payload).not.toHaveProperty('region')
  })

  it('strips Token-class keys from create payload.env (g3.1)', () => {
    const d = freshDraft()
    d.name = 'x'
    d.authMode = 'apiKey'
    d.env = [
      { k: 'GRASP_CURSOR_API_KEY', v: 'sk-test' },
      { k: 'GITLAB_TOKEN', v: 'glpat-x' },
      { k: 'GIT_REPOS', v: 'app|https://gitlab.com/a/b.git' },
      { k: 'FEATURE_FLAG', v: '1' },
    ]
    const payload = assembleCreatePayload(d)
    expect(payload.env).toEqual({
      GIT_REPOS: 'app|https://gitlab.com/a/b.git',
      FEATURE_FLAG: '1',
    })
  })
})

describe('validateBasics', () => {
  it('rejects empty / invalid / duplicate names', () => {
    const d = freshDraft()
    expect(validateBasics(d, [])).toBe('required')
    d.name = 'bad name'
    expect(validateBasics(d, [])).toBe('invalid')
    d.name = 'ok'
    expect(validateBasics(d, ['ok'])).toBe('exists')
    expect(validateBasics(d, [])).toBe('')
  })
})

describe('hasPathDeps / buildReviewSummary', () => {
  it('detects path dependencies', () => {
    const d = freshDraft()
    expect(hasPathDeps(d)).toBe(false)
    d.skills.push({ name: 'a', content: '' })
    expect(hasPathDeps(d)).toBe(true)
  })

  it('builds 5-step review chips without ENV/capability steps and reminds when Key skipped', () => {
    const d = freshDraft()
    d.name = 'n'
    d.skipped.apiKey = true
    const items = buildReviewSummary(d)
    const keys = items.map((i) => i.key)
    expect(keys).toContain('acp')
    expect(keys).toContain('apiKey')
    expect(keys).toContain('git')
    expect(keys).toContain('authReminder')
    expect(keys).not.toContain('env')
    expect(keys).not.toContain('mcp')
    expect(keys).not.toContain('rules')
    expect(keys).not.toContain('skills')
    expect(keys).not.toContain('commands')
    expect(keys).not.toContain('prompts')
    expect(items.find((i) => i.key === 'acp')?.labelKey).toBe('pages.agentStudio.wizard.review.acp')
    expect(items.find((i) => i.key === 'apiKey')?.kind).toBe('empty')
    expect(items.find((i) => i.key === 'git')?.kind).toBe('empty')
  })

  it('marks API Key configured when auth env is present', () => {
    const d = freshDraft()
    d.name = 'n'
    d.acpBackend = 'cursor'
    d.startPath = 'cli'
    d.env = [{ k: 'GRASP_CURSOR_API_KEY', v: 'crsr_demo' }]
    const items = buildReviewSummary(d)
    expect(items.find((i) => i.key === 'apiKey')?.kind).toBe('ok')
    expect(items.find((i) => i.key === 'authReminder')).toBeUndefined()
  })

  it('writes settings.json and omits auth env in custom config mode', () => {
    const d = freshDraft()
    d.name = 'cfg-agent'
    d.acpBackend = 'claude_code'
    d.authMode = 'customConfig'
    d.customConfigContent = JSON.stringify({ env: { ANTHROPIC_API_KEY: 'sk-ant-x' } })
    d.env = [{ k: 'GRASP_CLAUDE_API_KEY', v: 'should-strip' }]
    const payload = assembleCreatePayload(d)
    expect(payload.files?.some((f) => f.path === AGENT_SETTINGS_PATH)).toBe(true)
    expect(payload.env?.GRASP_CLAUDE_API_KEY).toBeUndefined()
    const items = buildReviewSummary(d)
    expect(items.find((i) => i.key === 'apiKey')?.labelKey).toBe(
      'pages.agentStudio.wizard.review.customConfigWritten',
    )
  })

  it('writes opencode.json in OpenCode custom config mode', () => {
    const d = freshDraft()
    d.name = 'oc-agent'
    applyAcpBackend(d, 'opencode')
    d.authMode = 'customConfig'
    d.customConfigContent = JSON.stringify({ model: 'openai/gpt-4.1' })
    d.env.push({ k: 'GRASP_OPENCODE_API_KEY', v: 'should-strip' })
    const payload = assembleCreatePayload(d)
    expect(payload.files?.some((f) => f.path === AGENT_OPENCODE_CONFIG_REL_PATH)).toBe(true)
    expect(payload.files?.some((f) => f.path === AGENT_SETTINGS_PATH)).toBe(false)
    expect(payload.env?.GRASP_OPENCODE_API_KEY).toBeUndefined()
    expect(payload.env?.GRASP_OPENCODE_PROVIDER).toBe('openai')
  })

  it('rejects invalid custom config json', () => {
    expect(parseCustomConfigJson('{bad').ok).toBe(false)
    expect(parseCustomConfigJson('{"ok":true}').ok).toBe(true)
  })

  it('resets regions on backend switches and excludes the managed key from ENV count', () => {
    const d = freshDraft()
    applyAcpBackend(d, 'opencode')
    expect(d.configRoot).toBe('/root/.config/opencode')
    expect(d.env).toContainEqual({ k: 'GRASP_OPENCODE_PROVIDER', v: 'openai' })
    applyAcpBackend(d, 'codebuddy')
    expect(d.env).toContainEqual({ k: 'GRASP_CODEBUDDY_REGION', v: 'public' })
    d.env.push({ k: 'CUSTOM', v: '1' })
    applyAcpBackend(d, 'trae')
    expect(d.env).toEqual([
      { k: 'CUSTOM', v: '1' },
      { k: 'GRASP_TRAE_REGION', v: 'intl' },
    ])
    expect(envConfiguredCount(d)).toBe(1)
    expect(buildReviewSummary(d).find((item) => item.key === 'region')).toMatchObject({
      labelKey: 'pages.agentStudio.region.international',
      detail: 'intl',
    })
  })
})
