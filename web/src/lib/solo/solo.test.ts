import { describe, expect, it } from 'vitest'
import type { SoloMessage } from '@/lib/api/apiTypes'
import { applyAcp, emptyAgentTurn, partsOf, turnsFromMessages } from './acpTurn'
import { draftFromProject, draftProblems, emptyDraft, inputFromDraft, isRepoUrl, repoNameFromUrl } from './projectForm'
import { repoProtocol, repoShort } from './soloPrefs'
import { soloErrorText } from './soloErrors'
import { branchOfRepo, repoTriggerLabel, sessionRepoLabel, sessionRepos, soloCreateChoices } from './sessionRepos'

const label = () => ''
const chunk = (kind: string, text: string) => ({ op: 'event', data: { type: 'session_update', update: { sessionUpdate: kind, content: { type: 'text', text } } } })

describe('acpTurn', () => {
  it('keeps tool updates inside the current message and starts a new run for an unseen tool update', () => {
    const turn = emptyAgentTurn('live:updates', true)
    const tool = (kind: string, id: string) => ({ data: { type: 'session_update', update: { sessionUpdate: kind, toolCallId: id, title: 'Screenshot' } } })
    applyAcp(tool('tool_call', 'one'), turn, label, 'Tool')
    applyAcp(chunk('agent_message_chunk', 'Server '), turn, label, 'Tool')
    applyAcp(tool('tool_call', 'one'), turn, label, 'Tool')
    applyAcp(chunk('agent_message_chunk', 'is up.'), turn, label, 'Tool')
    applyAcp(tool('tool_call_update', 'two'), turn, label, 'Tool')
    applyAcp(chunk('agent_message_chunk', 'Screenshot ready.'), turn, label, 'Tool')
    expect(turn.text).toBe('Server is up.\n\nScreenshot ready.')
    turn.historyContent = turn.text
    turn.processComplete = true
    expect(partsOf(turn).filter(part => part.kind === 'message').map(part => part.text)).toEqual(['Server is up.', 'Screenshot ready.'])
  })
  it('separates progress messages across tools and recognizes the saved reply when its process finishes loading', () => {
    const turn = emptyAgentTurn('live:1', true)
    applyAcp(chunk('agent_message_chunk', 'Checking '), turn, label, 'Tool')
    applyAcp(chunk('agent_message_chunk', 'the setup.'), turn, label, 'Tool')
    applyAcp({ data: { type: 'session_update', update: { sessionUpdate: 'tool_call', toolCallId: 'screenshot', title: 'Screenshot' } } }, turn, label, 'Tool')
    applyAcp(chunk('agent_message_chunk', '![Homepage](/tmp/full.png)'), turn, label, 'Tool')
    expect(turn.text).toBe('Checking the setup.\n\n![Homepage](/tmp/full.png)')
    turn.historyContent = turn.text
    turn.processComplete = true
    expect(partsOf(turn).map(part => part.kind)).toEqual(['message', 'tool', 'message'])
    expect(partsOf(turn).filter(part => part.kind === 'message').map(part => part.text)).toEqual(['Checking the setup.', '![Homepage](/tmp/full.png)'])
  })
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

  it('keeps provider diagnostics and partial output when the turn fails', () => {
    const turn = emptyAgentTurn('live:failed', true)
    applyAcp(chunk('agent_message_chunk', 'Partial reply'), turn, label, 'Tool')
    for (const text of ['Quota exhausted', 'Check the selected model']) {
      applyAcp({ op: 'event', data: { type: 'error_text', text } }, turn, label, 'Tool')
    }
    applyAcp({ data: { type: 'prompt_done', stopReason: 'failed' } }, turn, label, 'Tool')
    expect(turn.status).toBe('failed')
    expect(turn.error).toBe('Quota exhausted\nCheck the selected model')
    expect(partsOf(turn)).toEqual([{ kind: 'message', text: 'Partial reply' }])
  })

  it.each<{ events: NonNullable<SoloMessage['events']>; error?: string }>([
    { events: [{ data: { type: 'error_text', text: 'Unexpected server error. Check server logs for details.' } }, { data: { type: 'prompt_done', stopReason: 'failed' } }], error: 'Unexpected server error. Check server logs for details.' },
    { events: [{ data: { type: 'prompt_done', stopReason: 'failed' } }] },
    { events: [{ data: { type: 'prompt_done', stopReason: 'refusal' } }] },
  ])('recovers provider failures from history incorrectly persisted as ok', ({ events, error }) => {
    const [turn] = turnsFromMessages([{
      id: 'a1', sessionId: 's', role: 'assistant', content: '', status: 'ok', error: '', createdAt: '', events,
    }], label, 'Tool')
    expect(turn).toMatchObject({ status: 'failed', steps: [], streaming: false })
    expect(turn!.error).toBe(error)
  })

  it('preserves explicit stopped status and stored diagnostics when replaying errors', () => {
    const [turn] = turnsFromMessages([{
      id: 'a1', sessionId: 's', role: 'assistant', content: '', status: 'stopped', error: 'Stopped by user', createdAt: '',
      events: [{ data: { type: 'error_text', text: 'Provider interrupted' } }],
    }], label, 'Tool')
    expect(turn).toMatchObject({ status: 'stopped', error: 'Stopped by user' })
  })

  it('retains persisted process capability without fetching or fabricating work parts', () => {
    const [turn] = turnsFromMessages([{
      id: 'reply:q', sessionId: 's', role: 'assistant', content: '最终答案', createdAt: '2026-01-01T00:00:00Z',
      eventLog: { version: 1, from: 7, through: 9, count: 3, generation: 1 }, durationMs: 12_000,
    }], label, 'Tool')
    expect(turn).toMatchObject({ historyMessageId: 'reply:q', replyTo: 'q', processAvailable: true, durationMs: 12_000, text: '最终答案' })
    expect(partsOf(turn!)).toEqual([{ kind: 'message', text: '最终答案' }])
    const [empty] = turnsFromMessages([{
      id: 'empty', sessionId: 's', role: 'assistant', content: '', createdAt: '',
      eventLog: { version: 1, from: 0, through: -1, count: 0, generation: 1 },
    }], label, 'Tool')
    expect(empty?.processAvailable).toBe(false)
  })

  it('preserves earlier progress in a complete journal instead of replacing it with aggregate text', () => {
    const [turn] = turnsFromMessages([{
      id: 'a', sessionId: 's', role: 'assistant', content: '先检查文件最终答案', createdAt: '',
      eventLog: { version: 1, from: 1, through: 3, count: 3, generation: 1 },
      events: [chunk('agent_message_chunk', '先检查文件'),
        { data: { type: 'session_update', update: { sessionUpdate: 'tool_call', toolCallId: 'read', title: 'read_file', status: 'completed' } } },
        chunk('agent_message_chunk', '最终答案')],
    }], label, 'Tool')
    turn!.processComplete = true
    expect(partsOf(turn!).map(part => part.kind)).toEqual(['message', 'tool', 'message'])
    expect(partsOf(turn!).filter(part => part.kind === 'message').map(part => part.text)).toEqual(['先检查文件', '最终答案'])
    expect(turn?.text).toBe('先检查文件最终答案')
  })

  it('keeps the complete saved reply during partial pages and renders an exact journal copy only once', () => {
    const [turn] = turnsFromMessages([{
      id: 'a', sessionId: 's', role: 'assistant', content: '最终答案', createdAt: '', legacyEvents: true,
      events: [chunk('agent_message_chunk', '先检查文件'),
        { data: { type: 'session_update', update: { sessionUpdate: 'tool_call', toolCallId: 'read', title: 'read_file', status: 'completed' } } },
        chunk('agent_message_chunk', '最终答案')],
    }], label, 'Tool')
    expect(turn?.text).toBe('最终答案')
    expect(partsOf(turn!).filter(part => part.kind === 'message').map(part => part.text)).toEqual(['先检查文件', '最终答案'])
    turn!.processComplete = true
    expect(partsOf(turn!).filter(part => part.kind === 'message').map(part => part.text)).toEqual(['先检查文件', '最终答案'])
  })

  it('matches a trimmed saved reply against a journal with surrounding whitespace', () => {
    const events = [chunk('agent_message_chunk', '\n先检查文件'),
      { data: { type: 'session_update', update: { sessionUpdate: 'tool_call', toolCallId: 'read', title: 'read_file', status: 'completed' } } },
      chunk('agent_message_chunk', '最终答案\n')]
    const [complete] = turnsFromMessages([{
      id: 'a', sessionId: 's', role: 'assistant', content: '先检查文件最终答案', createdAt: '',
      eventLog: { version: 1, from: 1, through: 3, count: 3, generation: 1 }, events,
    }], label, 'Tool')
    complete!.processComplete = true
    expect(partsOf(complete!).map(part => part.kind)).toEqual(['message', 'tool', 'message'])
    const [single] = turnsFromMessages([{
      id: 'b', sessionId: 's', role: 'assistant', content: '最终答案', createdAt: '', legacyEvents: true,
      events: [chunk('agent_message_chunk', '最终答案\n')],
    }], label, 'Tool')
    expect(partsOf(single!).filter(part => part.kind === 'message').map(part => part.text)).toEqual(['最终答案'])
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
      'pages.solo.errors.branch_invalid_repo': '分支名不能包含空格（{repo}）',
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
    expect(soloErrorText({ code: 'branch_invalid_repo', params: { repo: 'webapp' } }, t, te, 'en')).toBe(
      '分支名不能包含空格（webapp）',
    )
  })
})

describe('projectForm', () => {
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

  it('round-trips clone fields and drops retired runtime keys', () => {
    const raw = {
      id: 'p1',
      name: ' app ',
      description: 'demo',
      repoUrl: 'https://github.com/acme/app.git',
      defaultBranch: 'main',
      repoDirName: 'app',
      gitCredentialType: 'github_https',
      gitCredentialId: 'c1',
      sshHostsCredentialId: 'h1',
      env: { NODE_ENV: 'test' },
      credentialBindings: { NPM_TOKEN: 'c2' },
      createdAt: '',
      updatedAt: '',
    }
    const draft = draftFromProject(raw)
    const body = inputFromDraft(draft)
    expect(body).toEqual({
      name: 'app',
      description: 'demo',
      repoUrl: 'https://github.com/acme/app.git',
      defaultBranch: 'main',
      repoDirName: 'app',
    })
    expect(body).not.toHaveProperty('gitCredentialType')
    expect(body).not.toHaveProperty('env')
    expect(body).not.toHaveProperty('credentialBindings')
    expect(draft).not.toHaveProperty('gitCredentialType')
  })

  it('lists draft problems before submit', () => {
    const d = emptyDraft()
    expect(draftProblems(d)).toEqual(['name', 'repoUrl'])
    d.name = 'x'
    d.repoUrl = 'https://h/x.git'
    d.defaultBranch = 'bad branch'
    d.repoDirName = '../up'
    expect(draftProblems(d)).toEqual(['branch', 'repoDir'])
    d.defaultBranch = ''
    d.repoDirName = 'app'
    expect(draftProblems(d)).toEqual([])
  })

  it('keeps the repository URL placeholder to a single HTTPS example', async () => {
    const zh = (await import('@/locales/zh-CN/pages.json')).default as {
      pages: { projects: { form: { repoUrlPlaceholder: string }; subtitle: string } }
    }
    const en = (await import('@/locales/en/pages.json')).default as {
      pages: { projects: { form: { repoUrlPlaceholder: string }; subtitle: string } }
    }
    expect(zh.pages.projects.form.repoUrlPlaceholder).toBe('https://github.com/acme/app.git')
    expect(en.pages.projects.form.repoUrlPlaceholder).toBe('https://github.com/acme/app.git')
    expect(zh.pages.projects.subtitle).not.toMatch(/凭据|环境变量/)
    expect(en.pages.projects.subtitle).not.toMatch(/credential|variable/i)
  })
})
