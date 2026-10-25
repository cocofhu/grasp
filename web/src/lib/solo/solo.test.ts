import { describe, expect, it } from 'vitest'
import type { SoloMessage } from '@/lib/api/apiTypes'
import { applyAcp, emptyAgentTurn, partsOf, turnsFromMessages } from './acpTurn'
import {
  accessMismatch,
  bindingKeyProblem,
  credentialsFor,
  draftFromProject,
  draftProblems,
  emptyDraft,
  envKeyProblem,
  inputFromDraft,
  isRepoUrl,
  repoAccessKey,
  repoNameFromUrl,
} from './projectForm'
import { repoProtocol, repoShort } from './soloPrefs'
import { soloErrorText } from './soloErrors'
import { branchOfRepo, repoTriggerLabel, sessionRepoLabel, sessionRepos, soloCreateChoices } from './sessionRepos'

const label = () => ''
const chunk = (kind: string, text: string) => ({ op: 'event', data: { type: 'session_update', update: { sessionUpdate: kind, content: { type: 'text', text } } } })

describe('acpTurn', () => {
  it('folds message, thought and tool frames in arrival order', () => {
    const turn = emptyAgentTurn('live:1', true)
    applyAcp(chunk('agent_thought_chunk', 'think '), turn, label, 'Tool')
    applyAcp(chunk('agent_thought_chunk', 'more'), turn, label, 'Tool')
    applyAcp(
      { data: { type: 'session_update', update: { sessionUpdate: 'tool_call', toolCallId: 't1', title: 'run_shell', status: 'in_progress', rawInput: { command: 'ls -la', token: 'x' } } } },
      turn,
      label,
      'Tool',
    )
    applyAcp({ data: { type: 'session_update', update: { sessionUpdate: 'tool_call_update', toolCallId: 't1', status: 'completed', rawOutput: { stdout: 'ok' } } } }, turn, label, 'Tool')
    applyAcp(chunk('agent_message_chunk', 'Hello'), turn, label, 'Tool')
    applyAcp(chunk('agent_message_chunk', ' world'), turn, label, 'Tool')

    expect(turn.text).toBe('Hello world')
    expect(turn.tools).toHaveLength(1)
    expect(turn.tools[0]).toMatchObject({ id: 't1', title: 'Run Shell', status: 'completed', summary: 'ls -la', output: 'ok' })
    expect(turn.tools[0]!.input).toContain('****')
    expect(partsOf(turn).map((p) => p.kind)).toEqual(['thought', 'tool', 'message'])
    expect(partsOf(turn)[0]!.text).toBe('think more')
  })

  it('rebuilds turns from persisted messages, replaying events', () => {
    const messages: SoloMessage[] = [
      { id: 'u1', sessionId: 's', role: 'user', content: 'hi', createdAt: '2026-01-01T00:00:00Z' },
      { id: 'a1', sessionId: 's', role: 'assistant', content: 'Hello', events: [chunk('agent_message_chunk', 'Hello')], status: 'ok', durationMs: 2000, createdAt: '2026-01-01T00:00:03Z' },
      { id: 'u2', sessionId: 's', role: 'user', content: 'again', status: 'stopped', createdAt: '2026-01-01T00:01:00Z' },
      { id: 'a2', sessionId: 's', role: 'assistant', content: 'partial', status: 'stopped', error: '已停止', createdAt: '2026-01-01T00:01:05Z' },
      { id: 'm1', sessionId: 's', role: 'system', content: '会话已关闭', createdAt: '2026-01-01T00:02:00Z' },
    ]
    const turns = turnsFromMessages(messages, label, 'Tool')
    expect(turns.map((x) => x.id)).toEqual(['u:u1', 'a:a1', 'u:u2', 'a:a2', 's:m1'])
    expect(turns[1]!.steps).toEqual([{ kind: 'message', text: 'Hello' }])
    expect(turns[1]!.startedAt).toBe('2026-01-01T00:00:01.000Z')
    expect(turns[2]!.status).toBe('stopped')
    // No events: the persisted text still renders.
    expect(turns[3]!.steps).toEqual([{ kind: 'message', text: 'partial' }])
    expect(turns[3]!.status).toBe('stopped')
    expect(turns[4]!.role).toBe('system')
  })
})

describe('session repos', () => {
  it('labels the trigger and recent sessions from selection order', () => {
    expect(repoTriggerLabel([], () => '', '不绑定仓库')).toBe('不绑定仓库')
    expect(repoTriggerLabel(['a'], (id) => (id === 'a' ? 'grasp' : ''), '不绑定仓库')).toBe('grasp')
    expect(repoTriggerLabel(['a', 'b', 'c'], (id) => id, '不绑定仓库')).toBe('a +2')
    expect(sessionRepoLabel({ repos: [{ position: 0, projectId: 'a', name: 'grasp', repoUrl: 'https://github.com/acme/grasp.git', repoDirName: 'grasp' }] })).toBe('grasp')
    expect(sessionRepoLabel({
      repos: [
        { position: 0, projectId: 'a', name: 'grasp', repoUrl: 'https://github.com/acme/grasp.git', repoDirName: 'grasp' },
        { position: 1, projectId: 'b', name: 'webapp', repoUrl: 'https://github.com/acme/webapp.git', repoDirName: 'webapp' },
      ],
    })).toBe('grasp +1')
    expect(sessionRepoLabel({})).toBe('')
  })

  it('reads a legacy single project as one frozen repository', () => {
    const repos = sessionRepos({
      projectId: 'p1',
      branch: 'release',
      project: { id: 'p1', name: 'app', repoUrl: 'https://github.com/acme/app.git', repoDirName: 'app', defaultBranch: 'main', createdAt: '', updatedAt: '' },
    })
    expect(repos).toHaveLength(1)
    expect(repos[0]).toMatchObject({ name: 'app', branch: 'release', repoDirName: 'app' })
    expect(branchOfRepo({ branch: '' }, '远端默认分支')).toBe('远端默认分支')
  })

  it('sends branch overrides only when they are non-empty', () => {
    expect(soloCreateChoices([' a ', 'b'], { a: ' feat ', b: '  ' })).toEqual([
      { projectId: 'a', branch: 'feat' },
      { projectId: 'b' },
    ])
    expect(soloCreateChoices([], {})).toEqual([])
  })

  it('interpolates conflict names in the active language', () => {
    const dict: Record<string, string> = {
      'pages.solo.errors.repo_dir_conflict': '目录名 {dir} 被仓库 {repos} 同时使用',
      'pages.solo.errors.env_key_conflict': 'Variable {key} conflicts across {repos}',
    }
    const t = (key: string, params?: Record<string, unknown>) => {
      let text = dict[key] || key
      for (const [name, value] of Object.entries(params || {})) text = text.replace(`{${name}}`, String(value))
      return text
    }
    const te = (key: string) => key in dict
    expect(soloErrorText({ code: 'repo_dir_conflict', params: { dir: 'webapp', repos: ['webapp', 'checkout'] } }, t, te, 'zh-CN')).toBe(
      '目录名 webapp 被仓库 webapp、checkout 同时使用',
    )
    expect(soloErrorText({ code: 'env_key_conflict', params: { key: 'APP_ENV', repos: ['webapp', 'api'] } }, t, te, 'en')).toBe(
      'Variable APP_ENV conflicts across webapp, api',
    )
  })
})

describe('projectForm', () => {
  it('flags invalid, reserved and sensitive env keys', () => {
    expect(envKeyProblem('NODE_ENV')).toBe('')
    expect(envKeyProblem('')).toBe('')
    expect(envKeyProblem('1BAD')).toBe('invalid')
    expect(envKeyProblem('GIT_REPOS')).toBe('reserved')
    expect(envKeyProblem('GITHUB_TOKEN')).toBe('sensitive')
    expect(envKeyProblem('db_password')).toBe('sensitive')
    expect(envKeyProblem('MY_API_KEY')).toBe('sensitive')
    // Bindings exist precisely to carry secrets.
    expect(bindingKeyProblem('NPM_TOKEN')).toBe('')
    expect(bindingKeyProblem('GIT_REPOS')).toBe('reserved')
  })

  it('validates repo URLs and derives directory names', () => {
    expect(isRepoUrl('https://github.com/acme/app.git')).toBe(true)
    expect(isRepoUrl('git@github.com:acme/app.git')).toBe(true)
    expect(isRepoUrl('ssh://git@host:22/x/y.git')).toBe(true)
    expect(isRepoUrl('github.com/acme/app')).toBe(false)
    expect(repoNameFromUrl('https://github.com/acme/app.git/')).toBe('app')
    expect(repoNameFromUrl('git@github.com:acme/my-repo.git')).toBe('my-repo')
    expect(repoShort('git@github.com:acme/app.git')).toBe('github.com/acme/app')
    expect(repoShort('https://user@gitlab.com/g/p.git')).toBe('gitlab.com/g/p')
    expect(repoProtocol('git@x:y')).toBe('ssh')
    expect(repoProtocol('https://x/y')).toBe('https')
  })

  it('treats an empty access mode as this repository having none', () => {
    expect(repoAccessKey('')).toBe('none')
    expect(repoAccessKey('github_https')).toBe('github_https')
  })

  it('warns when the access mode does not fit the URL', () => {
    expect(accessMismatch('git@github.com:a/b.git', 'github_https')).toBe('https')
    expect(accessMismatch('https://github.com/a/b.git', 'ssh')).toBe('ssh')
    expect(accessMismatch('https://github.com/a/b.git', 'github_https')).toBe('')
    expect(accessMismatch('https://github.com/a/b.git', '')).toBe('')
  })

  it('round-trips a project and drops fields the access mode does not use', () => {
    const draft = draftFromProject({
      id: 'p1',
      name: ' app ',
      repoUrl: 'https://github.com/acme/app.git',
      gitCredentialType: 'github_https',
      gitCredentialId: 'c1',
      sshHostsCredentialId: 'h1',
      env: { NODE_ENV: 'test' },
      credentialBindings: { NPM_TOKEN: 'c2' },
      createdAt: '',
      updatedAt: '',
    })
    draft.env.push({ k: '', v: 'ignored' })
    draft.bindings.push({ k: 'EMPTY', v: '' })
    const body = inputFromDraft(draft)
    expect(body).toMatchObject({
      name: 'app',
      gitCredentialType: 'github_https',
      gitCredentialId: 'c1',
      sshHostsCredentialId: '',
      env: { NODE_ENV: 'test' },
      credentialBindings: { NPM_TOKEN: 'c2' },
    })
    draft.gitCredentialType = ''
    expect(inputFromDraft(draft).gitCredentialId).toBe('')
  })

  it('lists draft problems before submit', () => {
    const d = emptyDraft()
    expect(draftProblems(d)).toEqual(['name', 'repoUrl'])
    d.name = 'x'
    d.repoUrl = 'https://h/x.git'
    d.gitCredentialType = 'ssh'
    d.env = [{ k: 'API_TOKEN', v: 'v' }]
    expect(draftProblems(d)).toEqual(['credential', 'env'])
  })

  it('prefers credentials whose env key matches the access mode', () => {
    const items = [
      { id: 'a', type: 'git', name: 'gh', envKey: 'GITHUB_TOKEN', configured: true },
      { id: 'b', type: 'ssh', name: 'key', envKey: 'GIT_SSH_PRIVATE_KEY', configured: true },
      { id: 'c', type: 'git', name: 'old', envKey: 'GITHUB_TOKEN', configured: true, revokedAt: '2026-01-01' },
    ]
    expect(credentialsFor(items, 'GITHUB_TOKEN').map((c) => c.id)).toEqual(['a'])
    expect(credentialsFor(items, 'GITLAB_TOKEN').map((c) => c.id)).toEqual(['a', 'b'])
  })
})
