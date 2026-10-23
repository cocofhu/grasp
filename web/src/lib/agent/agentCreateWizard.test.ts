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
      'deepseek_harness',
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
    expect(assembleCreatePayload(d)).not.toHaveProperty('gitCredentialId')
    expect(assembleCreatePayload(d)).not.toHaveProperty('sshHostsCredentialId')
  })

  it('writes the selected credential ids and omits them when unset', () => {
    const cursor = freshDraft()
    cursor.name = 'cursor-agent'
    cursor.acpBackend = 'cursor'
    cursor.aiCredentialId = 'cred-cursor'
    cursor.openCodeCredentialId = 'cred-should-not-send'
    cursor.gitCredentialType = 'github_https'
    cursor.gitCredentialId = 'cred-gh'
    cursor.sshHostsCredentialId = 'cred-hosts'
    cursor.env = [
      { k: 'GRASP_CURSOR_API_KEY', v: 'sk-test' },
      { k: 'GITHUB_TOKEN', v: 'ghp-x' },
      { k: 'GIT_SSH_PRIVATE_KEY', v: 'key' },
      { k: 'GIT_SSH_KNOWN_HOSTS', v: 'hosts' },
      { k: 'GIT_REPOS', v: '${vars.repos}' },
    ]
    const cursorPayload = assembleCreatePayload(cursor)
    expect(cursorPayload.aiCredentialId).toBe('cred-cursor')
    expect(cursorPayload).not.toHaveProperty('openCodeCredentialId')
    expect(cursorPayload.gitCredentialType).toBe('github_https')
    expect(cursorPayload.gitCredentialId).toBe('cred-gh')
    expect(cursorPayload).not.toHaveProperty('sshHostsCredentialId')
    expect(cursorPayload.env).toEqual({ GIT_REPOS: '${vars.repos}' })

    const opencode = freshDraft()
    opencode.name = 'oc-agent'
    opencode.openCodeCredentialId = 'cred-oc'
    opencode.aiCredentialId = 'cred-cursor'
    const ocPayload = assembleCreatePayload(opencode)
    expect(ocPayload.openCodeCredentialId).toBe('cred-oc')
    expect(ocPayload).not.toHaveProperty('aiCredentialId')

    const blank = freshDraft()
    blank.name = 'blank-agent'
    blank.acpBackend = 'cursor'
    const blankPayload = assembleCreatePayload(blank)
    expect(blankPayload).not.toHaveProperty('aiCredentialId')
    expect(blankPayload).not.toHaveProperty('openCodeCredentialId')
    expect(blankPayload).not.toHaveProperty('gitCredentialType')
    expect(blankPayload).not.toHaveProperty('gitCredentialId')
  })

  it('sends the SSH known-hosts id only for SSH', () => {
    const d = freshDraft()
    d.name = 'ssh-agent'
    d.gitCredentialType = 'ssh'
    d.gitCredentialId = 'key-1'
    d.sshHostsCredentialId = 'hosts-1'
    const payload = assembleCreatePayload(d)
    expect(payload.gitCredentialId).toBe('key-1')
    expect(payload.sshHostsCredentialId).toBe('hosts-1')
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

  it('marks API Key configured from the selected credential alias, not a pasted secret', () => {
    const pasted = freshDraft()
    pasted.name = 'n'
    pasted.acpBackend = 'cursor'
    pasted.startPath = 'cli'
    pasted.env = [{ k: 'GRASP_CURSOR_API_KEY', v: 'crsr_demo' }]
    expect(buildReviewSummary(pasted).find((i) => i.key === 'apiKey')?.kind).toBe('empty')

    const picked = freshDraft()
    picked.name = 'n'
    picked.acpBackend = 'cursor'
    picked.aiCredentialId = 'cred-cursor'
    const items = buildReviewSummary(picked, { model: '工作号' })
    const apiKey = items.find((i) => i.key === 'apiKey')
    expect(apiKey?.kind).toBe('ok')
    expect(apiKey?.detail).toBe('工作号')
    expect(items.find((i) => i.key === 'authReminder')).toBeUndefined()
    expect(items.find((i) => i.key === 'git')?.kind).toBe('empty')
  })

  it('shows Git as configured when a credential or GIT_REPOS is present', () => {
    const repos = freshDraft()
    repos.name = 'n'
    repos.gitCredentialType = 'github_https'
    repos.env = [{ k: 'GIT_REPOS', v: '${vars.repos}' }]
    const reposGit = buildReviewSummary(repos).find((i) => i.key === 'git')
    expect(reposGit?.kind).toBe('ok')
    expect(reposGit?.detail).toBe('GIT_REPOS')

    const cred = freshDraft()
    cred.name = 'n'
    cred.gitCredentialType = 'ssh'
    cred.gitCredentialId = 'key-1'
    cred.sshHostsCredentialId = 'hosts-1'
    cred.env = [{ k: 'GIT_REPOS', v: '${vars.repos}' }]
    const credGit = buildReviewSummary(cred, { git: '部署私钥', sshHosts: '已知主机' }).find((i) => i.key === 'git')
    expect(credGit?.kind).toBe('ok')
    expect(credGit?.detail).toBe('部署私钥 · 已知主机 · GIT_REPOS')
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
