// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('../composables/useShutdownState', async () => {
  const actual = await vi.importActual<typeof import('../composables/useShutdownState')>(
    '../composables/useShutdownState',
  )
  return {
    ...actual,
    mutationsBlocked: vi.fn(() => false),
    isDraining: vi.fn(() => false),
    isMutationMethod: actual.isMutationMethod,
    showDrainToast: vi.fn(),
    shutdownState: { mode: 'normal', graceRemainingSeconds: 0, message: '', checked: false },
  }
})

import { api, apiState, authApi, isPaginated } from './api'
import { mutationsBlocked, showDrainToast, shutdownState } from '../composables/useShutdownState'

const fetchMock = vi.fn()

beforeEach(() => {
  fetchMock.mockReset()
  vi.stubGlobal('fetch', fetchMock)
  apiState.online = false
  apiState.checked = false
  vi.mocked(mutationsBlocked).mockReturnValue(false)
  shutdownState.message = ''
})

afterEach(() => {
  vi.unstubAllGlobals()
})

function jsonResponse(data: unknown, status = 200, headers?: Record<string, string>) {
  return new Response(JSON.stringify(data), {
    status,
    headers: { 'Content-Type': 'application/json', ...headers },
  })
}

describe('isPaginated', () => {
  it('detects paginated envelope vs array', () => {
    expect(isPaginated([{ id: '1' } as never])).toBe(false)
    expect(
      isPaginated({ items: [], total: 0, page: 1, pageSize: 20, hasMore: false }),
    ).toBe(true)
  })
})

describe('notifications client', () => {
  it('lists items and posts read / read-all without id arrays', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse({ items: [{ runId: 'r1', unread: true }] }))
    await expect(api.listNotifications()).resolves.toMatchObject({
      items: [{ runId: 'r1', unread: true }],
    })
    const lastCall = () => fetchMock.mock.calls[fetchMock.mock.calls.length - 1]
    expect(String(lastCall()?.[0])).toMatch(/\/notifications$/)

    fetchMock.mockResolvedValueOnce(jsonResponse({ status: 'ok' }))
    await expect(api.markNotificationRead('r1')).resolves.toEqual({ status: 'ok' })
    const readCall = lastCall()
    expect(String(readCall?.[0])).toMatch(/\/notifications\/read$/)
    expect(readCall?.[1]).toMatchObject({ method: 'POST' })
    expect(JSON.parse(String(readCall?.[1]?.body))).toEqual({ runId: 'r1' })

    fetchMock.mockResolvedValueOnce(jsonResponse({ status: 'ok' }))
    await expect(api.markAllNotificationsRead()).resolves.toEqual({ status: 'ok' })
    const allCall = lastCall()
    expect(String(allCall?.[0])).toMatch(/\/notifications\/read-all$/)
    expect(allCall?.[1]).toMatchObject({ method: 'POST' })
    expect(allCall?.[1]?.body).toBeUndefined()
  })
})

describe('api req helpers', () => {
  it('lists and mutates projects/workflows/runs', async () => {
    fetchMock
      .mockResolvedValueOnce(jsonResponse([{ id: 'p1' }]))
      .mockResolvedValueOnce(jsonResponse({ id: 'p1', name: 'P' }))
      .mockResolvedValueOnce(jsonResponse({ id: 'p1', name: 'P2' }))
      .mockResolvedValueOnce(jsonResponse({ id: 'p1', name: 'new' }))
      .mockResolvedValueOnce(jsonResponse({ status: 'ok' }))
      .mockResolvedValueOnce(jsonResponse([{ id: 'w1' }]))
      .mockResolvedValueOnce(jsonResponse([{ id: 'w1' }]))
      .mockResolvedValueOnce(jsonResponse({ id: 'w1' }))
      .mockResolvedValueOnce(jsonResponse({ id: 'w1' }))
      .mockResolvedValueOnce(jsonResponse({ id: 'w1' }))
      .mockResolvedValueOnce(jsonResponse({ id: 'w1' }))
      .mockResolvedValueOnce(jsonResponse([]))
      .mockResolvedValueOnce(jsonResponse({ id: 'k1', name: 'k', key: 'secret', key_prefix: 'sec', created_at: 't' }))
      .mockResolvedValueOnce(jsonResponse({ status: 'ok' }))
      .mockResolvedValueOnce(jsonResponse({ status: 'ok' }))
      .mockResolvedValueOnce(jsonResponse([]))
      .mockResolvedValueOnce(jsonResponse({ id: 'w1' }))
      .mockResolvedValueOnce(jsonResponse({ suggestedName: 'copy', sourceName: 'src', sourceId: 'w1' }))
      .mockResolvedValueOnce(jsonResponse({ id: 'w2' }))
      .mockResolvedValueOnce(jsonResponse({ nodes: [], edges: [] }))
      .mockResolvedValueOnce(jsonResponse({ id: 'imp' }))
      .mockResolvedValueOnce(jsonResponse({ id: 'imp2' }))
      .mockResolvedValueOnce(jsonResponse([{ id: 'r1' }]))
      .mockResolvedValueOnce(
        jsonResponse({ items: [{ id: 'r1' }], total: 1, page: 1, pageSize: 10, hasMore: false }),
      )
      .mockResolvedValueOnce(
        jsonResponse({ items: [], total: 0, page: 1, pageSize: 10, hasMore: false }),
      )
      .mockResolvedValueOnce(
        jsonResponse({ items: [], total: 0, page: 1, pageSize: 10, hasMore: false }),
      )
      .mockResolvedValueOnce(jsonResponse({ id: 'r1' }))
      .mockResolvedValueOnce(jsonResponse({ items: [] }))
      .mockResolvedValueOnce(jsonResponse({ tags: ['bugfix'] }))
      .mockResolvedValueOnce(jsonResponse({ id: 'r1', status: 'running' }))
      .mockResolvedValueOnce(jsonResponse({ id: 'r1', status: 'queued', priority: 'high' }))
      .mockResolvedValueOnce(jsonResponse({ status: 'ok' }))
      .mockResolvedValueOnce(jsonResponse({ status: 'deleted' }))
      .mockResolvedValueOnce(jsonResponse({ status: 'ok' }))
      .mockResolvedValueOnce(jsonResponse({ status: 'ok' }))
      .mockResolvedValueOnce(jsonResponse({ status: 'ok' }))
      .mockResolvedValueOnce(jsonResponse({ items: [] }))
      .mockResolvedValueOnce(
        jsonResponse({
          id: 'a1',
          name: 'x',
          kind: 'md',
          sizeBytes: 1,
          updatedAt: 't',
          etag: 'e',
          nodeId: 'n1',
          content: 'c',
        }),
      )
      .mockResolvedValueOnce(jsonResponse({ status: 'ok' }))

    await expect(api.listProjects()).resolves.toEqual([{ id: 'p1' }])
    await expect(api.getProject('p1')).resolves.toMatchObject({ id: 'p1' })
    await expect(api.updateProject('p1', { name: 'P2' })).resolves.toMatchObject({ name: 'P2' })
    await expect(api.createProject({ name: 'new' })).resolves.toMatchObject({ id: 'p1' })
    await expect(api.deleteProject('p1')).resolves.toEqual({ status: 'ok' })

    await expect(api.listWorkflows()).resolves.toEqual([{ id: 'w1' }])
    await expect(api.listWorkflows({ projectId: 'p1' })).resolves.toEqual([{ id: 'w1' }])
    await expect(api.getWorkflow('w1')).resolves.toMatchObject({ id: 'w1' })
    await expect(api.saveWorkflow({ name: 'n' })).resolves.toMatchObject({ id: 'w1' })
    await expect(api.saveWorkflow({ id: 'w1', name: 'n' })).resolves.toMatchObject({ id: 'w1' })
    await expect(api.publishWorkflow('w1')).resolves.toMatchObject({ id: 'w1' })
    await expect(api.listAPIKeys('w1')).resolves.toEqual([])
    await expect(api.createAPIKey('w1', 'k')).resolves.toMatchObject({ key: 'secret' })
    await expect(api.revokeAPIKey('w1', 'k1')).resolves.toEqual({ status: 'ok' })
    await expect(api.deleteWorkflow('w1')).resolves.toEqual({ status: 'ok' })
    await expect(api.listWorkflowVersions('w1')).resolves.toEqual([])
    await expect(api.restoreWorkflowVersion('w1', 2)).resolves.toMatchObject({ id: 'w1' })
    await expect(api.copyPreviewWorkflow('w1')).resolves.toMatchObject({ suggestedName: 'copy' })
    await expect(api.copyWorkflow('w1', 'copy')).resolves.toMatchObject({ id: 'w2' })
    await expect(api.getWorkflowVersionGraph('w1', 1)).resolves.toEqual({ nodes: [], edges: [] })
    await expect(api.importWorkflow('{}')).resolves.toMatchObject({ id: 'imp' })
    await expect(api.importWorkflow('{}', 'p1')).resolves.toMatchObject({ id: 'imp2' })

    await expect(api.listRuns()).resolves.toEqual([{ id: 'r1' }])
    await expect(api.listRuns({ status: 'running', tag: 'bugfix', wf: 'w1', projectId: 'p1', page: 1, pageSize: 10 })).resolves.toMatchObject({
      total: 1,
    })
    await api.listRuns({ page: 1, pageSize: 10, sort: 'priority', order: 'desc' })
    const lastCallUrl = () => String(fetchMock.mock.calls[fetchMock.mock.calls.length - 1]?.[0])
    expect(lastCallUrl()).toMatch(/sort=priority/)
    expect(lastCallUrl()).toMatch(/order=desc/)
    await api.listRuns({ page: 1, pageSize: 10, sort: 'duration', order: 'desc' })
    expect(lastCallUrl()).not.toMatch(/sort=/)
    expect(lastCallUrl()).not.toMatch(/order=/)
    await expect(api.getRun('r1')).resolves.toMatchObject({ id: 'r1' })
    await expect(api.inboxContext('r1', 'n1', 1)).resolves.toEqual({ items: [] })
    await expect(api.listProjectRunTags('p1')).resolves.toEqual({ tags: ['bugfix'] })
    await expect(api.startRun('w1', { a: 1 })).resolves.toMatchObject({ id: 'r1' })
    await expect(api.updateRunPriority('r1', 'high')).resolves.toMatchObject({ priority: 'high' })
    await expect(api.cancelRun('r1')).resolves.toEqual({ status: 'ok' })
    await expect(api.deleteRun('r1')).resolves.toEqual({ status: 'deleted' })
    await expect(api.resumeRun('r1')).resolves.toEqual({ status: 'ok' })
    await expect(api.resumeRun('r1', 'n1')).resolves.toEqual({ status: 'ok' })
    await expect(api.resumeGate('r1', 'n1', 'approve', { comment: 'ok' })).resolves.toEqual({
      status: 'ok',
    })
    await expect(api.listGatePrimaryArtifacts('r1', 'n1')).resolves.toEqual({ items: [] })
    await expect(api.saveGateArtifact('r1', 'n1', 'a.md', 'hi', '"etag"')).resolves.toMatchObject({
      etag: 'e',
    })
    await expect(api.reactReply('r1', 'n1', 'hello', [], true)).resolves.toEqual({ status: 'ok' })

    expect(apiState.online).toBe(true)
    expect(apiState.checked).toBe(true)
    expect(api.runEventsWsUrl('r1')).toMatch(/\/runs\/r1\/events$/)
  })

  it('covers agents, sandboxes, artifacts, settings and platform rules', async () => {
    const agent = { name: 'a1', files: [] }
    fetchMock
      .mockResolvedValueOnce(jsonResponse([agent]))
      .mockResolvedValueOnce(jsonResponse(agent))
      .mockResolvedValueOnce(jsonResponse({ status: 'ok' }))
      .mockResolvedValueOnce(jsonResponse(agent))
      .mockResolvedValueOnce(jsonResponse({ status: 'ok' }))
      .mockResolvedValueOnce(new Response('zip', { status: 200 }))
      .mockResolvedValueOnce(jsonResponse(agent))
      .mockResolvedValueOnce(jsonResponse([]))
      .mockResolvedValueOnce(jsonResponse({ id: 1, name: 's', profile: 'a1', purpose: 'test', status: 'running', createdAt: 't', updatedAt: 't', containerStatus: 'up', busy: false, connected: true, hasCodeServer: true, hasAcp: true }))
      .mockResolvedValueOnce(jsonResponse({ status: 'ok' }))
      .mockResolvedValueOnce(jsonResponse({ status: 'ok' }))
      .mockResolvedValueOnce(jsonResponse({ destroyed: 1, skipped: 0 }))
      .mockResolvedValueOnce(jsonResponse([{ id: 'art' }]))
      .mockResolvedValueOnce(
        jsonResponse({ items: [{ id: 'art' }], total: 1, page: 1, pageSize: 10, hasMore: false }),
      )
      .mockResolvedValueOnce(
        jsonResponse({ items: [{ id: 'art' }], total: 1, page: 1, pageSize: 20, hasMore: false }),
      )
      .mockResolvedValueOnce(jsonResponse({ id: 'art', content: 'x' }))
      .mockResolvedValueOnce(jsonResponse([{ artifactId: 'art', revision: 1, nodeId: 'n', sizeBytes: 1, createdAt: 't' }]))
      .mockResolvedValueOnce(jsonResponse({ artifactId: 'art', revision: 1, content: 'old' }))
      .mockResolvedValueOnce(new Response(null, { status: 204 }))
      .mockResolvedValueOnce(jsonResponse({ events: [], live: false }))
      .mockResolvedValueOnce(jsonResponse({ events: [], nextCursor: '', hasMore: false }))
      .mockResolvedValueOnce(jsonResponse({ events: [] }))
      .mockResolvedValueOnce(jsonResponse({ events: [], nextCursor: '', hasMore: false }))
      .mockResolvedValueOnce(jsonResponse({ content: 'log', live: false, found: true }))
      .mockResolvedValueOnce(jsonResponse({ id: 9, name: 's', profile: 'a1', purpose: 'test', status: 'running', createdAt: 't', updatedAt: 't', containerStatus: 'up', busy: false, connected: true, hasCodeServer: true, hasAcp: true }))
      .mockResolvedValueOnce(jsonResponse({ content: 'log', live: true, found: true }))
      .mockResolvedValueOnce(jsonResponse({ ports: [] }))
      .mockResolvedValueOnce(jsonResponse({ issues: [] }))
      .mockResolvedValueOnce(jsonResponse({ id: 'i1', runId: 'r', nodeId: 'n', body: 'b', status: 'open', createdAt: 't' }))
      .mockResolvedValueOnce(jsonResponse({ status: 'ok' }))
      .mockResolvedValueOnce(jsonResponse({ status: 'ok', ready: true }))
      .mockResolvedValueOnce(jsonResponse([]))
      .mockResolvedValueOnce(
        jsonResponse({ items: [], total: 0, page: 1, pageSize: 10, hasMore: false }),
      )
      .mockResolvedValueOnce(jsonResponse({ running: 0, waitingHuman: 0, failed: 0, completed: 0, workflows: 0, artifacts: 0 }))
      .mockResolvedValueOnce(jsonResponse({ items: [] }))
      .mockResolvedValueOnce(jsonResponse({ items: [] }))
      .mockResolvedValueOnce(jsonResponse({ items: [] }))
      .mockResolvedValueOnce(jsonResponse({ file: 'a.md', source: 'global', content: 'c' }))
      .mockResolvedValueOnce(jsonResponse({ file: 'a.md', source: 'override', content: 'c2' }))
      .mockResolvedValueOnce(jsonResponse({ file: 'a.md', source: 'global', content: 'c' }))
      .mockResolvedValueOnce(jsonResponse({ file: 'a.md', source: 'embed', content: 'e' }))
      .mockResolvedValueOnce(jsonResponse({ items: [] }))
      .mockResolvedValueOnce(jsonResponse({ file: 'a.md', source: 'override', content: 'c' }))
      .mockResolvedValueOnce(jsonResponse({ file: 'a.md', source: 'override', content: 'c2' }))
      .mockResolvedValueOnce(jsonResponse({ status: 'ok' }))
      .mockResolvedValueOnce(jsonResponse({ username: 'u', expires_at: 't' }))
      .mockResolvedValueOnce(jsonResponse({ status: 'ok' }))
      .mockResolvedValueOnce(jsonResponse({ username: 'u', expires_at: 't' }))

    await expect(api.listAgents()).resolves.toEqual([agent])
    await expect(api.createAgent(agent)).resolves.toEqual(agent)
    await expect(api.saveAgent(agent)).resolves.toEqual({ status: 'ok' })
    await expect(api.renameAgent('a1', 'a2')).resolves.toEqual(agent)
    await expect(api.deleteAgent('a1')).resolves.toEqual({ status: 'ok' })
    await expect(api.exportAgent('a1')).resolves.toBeInstanceOf(Blob)
    await expect(
      api.importAgent(new File(['z'], 'a.zip'), { targetName: 'a1', mode: 'create' }),
    ).resolves.toEqual(agent)

    await expect(api.listSandboxes()).resolves.toEqual([])
    await expect(api.getSandbox(1)).resolves.toMatchObject({ id: 1 })
    await expect(api.stopSandbox(1)).resolves.toEqual({ status: 'ok' })
    await expect(api.destroySandbox(1)).resolves.toEqual({ status: 'ok' })
    await expect(api.cleanupSandboxes()).resolves.toEqual({ destroyed: 1, skipped: 0 })
    expect(api.sandboxChatWsUrl(1)).toMatch(/\/sandboxes\/1\/chat$/)
    expect(api.sandboxTerminalWsUrl(1)).toMatch(/\/sandboxes\/1\/terminal$/)
    expect(api.sandboxIdeUrl(1)).toContain('/sandbox/1/')
    expect(api.sandboxBridgeUrl(1)).toContain('/sandbox-bridge/1/')

    await expect(api.listArtifacts()).resolves.toEqual([{ id: 'art' }])
    await expect(
      api.listArtifacts({ page: 1, pageSize: 10, wf: 'w', projectId: 'p', q: 'x' }),
    ).resolves.toMatchObject({ total: 1 })
    await expect(
      api.listArtifacts({ page: 1, pageSize: 20, groupBy: 'run', wf: 'w' }),
    ).resolves.toMatchObject({ total: 1 })
    await expect(api.artifactContent('art')).resolves.toMatchObject({ id: 'art' })
    await expect(api.artifactVersions('art')).resolves.toMatchObject([{ revision: 1 }])
    await expect(api.artifactVersionContent('art', 1)).resolves.toMatchObject({ content: 'old' })
    expect(api.artifactDownloadUrl('art')).toContain('/api/artifacts/art/download')
    expect(api.packRunArtifactsUrl('run-1')).toContain('/api/runs/run-1/artifacts/pack')
    await expect(api.deleteArtifact('art')).resolves.toBeUndefined()

    await expect(api.nodeEvents('r1', 'n1')).resolves.toEqual({ events: [], live: false })
    await expect(api.nodeEvents('r1', 'n1', { cursor: 'c', limit: 10 })).resolves.toMatchObject({
      hasMore: false,
    })
    await expect(api.sandboxEventLog(1)).resolves.toEqual({ events: [] })
    await expect(api.sandboxEventLog(1, { cursor: 'c', limit: 5 })).resolves.toMatchObject({
      hasMore: false,
    })
    await expect(api.nodeSandboxLog('r1', 'n1')).resolves.toMatchObject({ found: true })
    await expect(api.getRunNodeSandbox('r1', 'n1')).resolves.toMatchObject({ id: 9 })
    await expect(api.sandboxLog(1)).resolves.toMatchObject({ live: true })
    await expect(api.nodePreviews('r1', 'n1')).resolves.toEqual({ ports: [] })
    await expect(api.listPreviewIssues('r1', 'n1')).resolves.toEqual({ issues: [] })
    await expect(api.createPreviewIssue('r1', 'n1', 'body', '#x', 5173, [])).resolves.toMatchObject({
      id: 'i1',
    })
    await expect(api.deletePreviewIssue('r1', 'n1', 'i1')).resolves.toEqual({ status: 'ok' })
    await expect(api.health()).resolves.toMatchObject({ ready: true })
    expect(api.previewVncWsUrl('r', 'n', 1)).toMatch(/\/preview-vnc\/r\/n\/1\/ws$/)
    expect(api.sandboxVncWsUrl(3)).toMatch(/\/sandbox-vnc\/3\/ws$/)

    await expect(api.listGates()).resolves.toEqual([])
    await expect(api.listGates({ page: 1, pageSize: 10, wf: 'w', projectId: 'p', tag: 'bugfix' })).resolves.toMatchObject({
      total: 0,
    })
    await expect(api.dashboard()).resolves.toMatchObject({ running: 0 })
    await expect(api.getSettings()).resolves.toEqual({ items: [] })
    await expect(api.updateSettings({ a: 1 })).resolves.toEqual({ items: [] })
    await expect(api.listPlatformRules()).resolves.toEqual({ items: [] })
    await expect(api.getPlatformRule('a.md')).resolves.toMatchObject({ file: 'a.md' })
    await expect(api.savePlatformRule('a.md', 'c2')).resolves.toMatchObject({ content: 'c2' })
    await expect(api.resetPlatformRule('a.md')).resolves.toMatchObject({ source: 'global' })
    await expect(api.getPlatformRuleEmbed('a.md')).resolves.toMatchObject({ source: 'embed' })
    await expect(api.listAgentPlatformRules('a1')).resolves.toEqual({ items: [] })
    await expect(api.getAgentPlatformRule('a1', 'a.md')).resolves.toMatchObject({ file: 'a.md' })
    await expect(api.saveAgentPlatformRule('a1', 'a.md', 'c2')).resolves.toMatchObject({ content: 'c2' })
    await expect(api.deleteAgentPlatformRule('a1', 'a.md')).resolves.toEqual({ status: 'ok' })

    await expect(authApi.login('u', 'p', '/runs')).resolves.toMatchObject({ username: 'u' })
    await expect(authApi.logout()).resolves.toEqual({ status: 'ok' })
    await expect(authApi.me()).resolves.toMatchObject({ username: 'u' })
  })

  it('blocks mutations when draining and surfaces HTTP errors', async () => {
    vi.mocked(mutationsBlocked).mockReturnValue(true)
    shutdownState.message = 'draining'
    await expect(api.createProject({ name: 'x' })).rejects.toThrow('draining')
    expect(showDrainToast).toHaveBeenCalled()

    vi.mocked(mutationsBlocked).mockReturnValue(false)
    fetchMock.mockResolvedValueOnce(
      jsonResponse({ status: 'shutting_down', message: 'bye' }, 503),
    )
    await expect(api.listProjects()).rejects.toThrow('bye')

    fetchMock.mockResolvedValueOnce(jsonResponse({ error: 'boom' }, 500))
    await expect(api.listProjects()).rejects.toThrow('boom')

    fetchMock.mockResolvedValueOnce(new Response(null, { status: 404 }))
    await expect(api.getRunNodeSandbox('r', 'n')).resolves.toBeNull()

    fetchMock.mockResolvedValueOnce(jsonResponse({ error: 'nope' }, 500))
    await expect(api.getRunNodeSandbox('r', 'n')).rejects.toThrow('nope')

    fetchMock.mockResolvedValueOnce(jsonResponse({ error: 'export fail' }, 500))
    await expect(api.exportAgent('a')).rejects.toThrow('export fail')

    fetchMock.mockResolvedValueOnce(jsonResponse({ error: 'import fail' }, 400))
    await expect(
      api.importAgent(new File(['z'], 'a.zip'), { targetName: 'a', mode: 'create' }),
    ).rejects.toThrow('import fail')

    vi.mocked(mutationsBlocked).mockReturnValue(true)
    await expect(api.deleteArtifact('x')).rejects.toThrow('draining')

    vi.mocked(mutationsBlocked).mockReturnValue(false)
    fetchMock.mockResolvedValueOnce(jsonResponse({ status: 'shutting_down', message: 'sd' }, 503))
    await expect(api.deleteArtifact('x')).rejects.toThrow('sd')

    fetchMock.mockResolvedValueOnce(jsonResponse({ error: 'gone' }, 404))
    await expect(api.deleteArtifact('x')).rejects.toThrow('gone')
  })
})

describe('api.saveWorkflow description payload', () => {
  it('includes description in PUT/POST body (including empty string)', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse({ id: 'w1', name: 'n', description: 'list subtitle' }))
    await api.saveWorkflow({ id: 'w1', name: 'n', description: 'list subtitle' })
    const putCall = fetchMock.mock.calls[fetchMock.mock.calls.length - 1]
    expect(String(putCall?.[0])).toMatch(/\/workflows\/w1$/)
    expect(putCall?.[1]).toMatchObject({ method: 'PUT' })
    expect(JSON.parse(String(putCall?.[1]?.body))).toMatchObject({
      id: 'w1',
      name: 'n',
      description: 'list subtitle',
    })

    fetchMock.mockResolvedValueOnce(jsonResponse({ id: 'w2', name: 'n', description: '' }))
    await api.saveWorkflow({ name: 'n', description: '' })
    const postCall = fetchMock.mock.calls[fetchMock.mock.calls.length - 1]
    expect(String(postCall?.[0])).toMatch(/\/workflows$/)
    expect(postCall?.[1]).toMatchObject({ method: 'POST' })
    expect(JSON.parse(String(postCall?.[1]?.body))).toMatchObject({
      name: 'n',
      description: '',
    })
  })
})

describe('api.createWorkflowFromBaseline', () => {
  it('POSTs the workflow name, projectId, and repos to /workflows/from-baseline (plan g3.1)', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse({ id: 'wf-base', name: 'Requirements', status: 'published' }))
    await expect(
      api.createWorkflowFromBaseline('p1', 'Requirements', [
        { name: '', url: 'https://github.com/acme/app.git', branch: 'main' },
      ]),
    ).resolves.toMatchObject({ id: 'wf-base', status: 'published' })
    const call = fetchMock.mock.calls[fetchMock.mock.calls.length - 1]
    expect(String(call?.[0])).toMatch(/\/workflows\/from-baseline$/)
    expect(call?.[1]).toMatchObject({ method: 'POST' })
    expect(JSON.parse(String(call?.[1]?.body))).toEqual({
      projectId: 'p1',
      name: 'Requirements',
      repos: [{ name: '', url: 'https://github.com/acme/app.git', branch: 'main' }],
    })
  })
})

describe('api.patchWorkflowNotifyPolicy', () => {
  it('PATCHes notify-only body without nodes/edges', async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse({
        id: 'w1',
        status: 'published',
        notifyPolicy: { mode: 'custom', events: ['failed'] },
      }),
    )
    await expect(
      api.patchWorkflowNotifyPolicy('w1', { mode: 'custom', events: ['failed'] }),
    ).resolves.toMatchObject({ id: 'w1', status: 'published' })
    const call = fetchMock.mock.calls[fetchMock.mock.calls.length - 1]
    expect(String(call?.[0])).toMatch(/\/workflows\/w1\/notify-policy$/)
    expect(call?.[1]).toMatchObject({ method: 'PATCH' })
    const body = JSON.parse(String(call?.[1]?.body))
    expect(body).toEqual({ notifyPolicy: { mode: 'custom', events: ['failed'] } })
    expect(body.nodes).toBeUndefined()
    expect(body.edges).toBeUndefined()
  })
})

describe('api.patchWorkflowHomeVisibility', () => {
  it('PATCHes showOnHome-only body without nodes/edges (plan g1.2 / g2.1)', async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse({
        id: 'w1',
        status: 'published',
        showOnHome: true,
      }),
    )
    await expect(api.patchWorkflowHomeVisibility('w1', true)).resolves.toMatchObject({
      id: 'w1',
      showOnHome: true,
    })
    const call = fetchMock.mock.calls[fetchMock.mock.calls.length - 1]
    expect(String(call?.[0])).toMatch(/\/workflows\/w1\/home-visibility$/)
    expect(call?.[1]).toMatchObject({ method: 'PATCH' })
    const body = JSON.parse(String(call?.[1]?.body))
    expect(body).toEqual({ showOnHome: true })
    expect(body.nodes).toBeUndefined()
    expect(body.edges).toBeUndefined()
  })

  it('startRun sends title when provided', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse({ id: 'r1', status: 'queued' }))
    await api.startRun('w1', { a: 1 }, 'manual', 'normal', [], { title: '  用户第一句话  ' })
    const call = fetchMock.mock.calls[fetchMock.mock.calls.length - 1]
    const body = JSON.parse(String(call?.[1]?.body))
    expect(body.title).toBe('用户第一句话')
  })

  it('packRunArtifacts downloads zip with credentials and filename', async () => {
    fetchMock.mockResolvedValueOnce(
      new Response(new Blob(['PK']), {
        status: 200,
        headers: {
          'Content-Type': 'application/zip',
          'Content-Disposition': 'attachment; filename="Demo-artifacts.zip"',
        },
      }),
    )
    const result = await api.packRunArtifacts('run-1')
    expect(result.filename).toBe('Demo-artifacts.zip')
    expect(result.blob).toBeInstanceOf(Blob)
    const call = fetchMock.mock.calls[0]
    expect(String(call?.[0])).toContain('/runs/run-1/artifacts/pack')
    expect(call?.[1]).toMatchObject({ credentials: 'include' })
  })
})

describe('getGlobalTokenStats window fallback (g1.2)', () => {
  it('omits window by falling back to all, not 30d', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse({ window: 'all', empty: true }))
    await api.getGlobalTokenStats({})
    const url = String(fetchMock.mock.calls[0]?.[0])
    expect(url).toMatch(/\/stats\/token\?/)
    expect(new URL(url, 'http://localhost').searchParams.get('window')).toBe('all')
  })

  it('passes an explicit window through', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse({ window: '30d', empty: true }))
    await api.getGlobalTokenStats({ window: '30d' })
    const url = String(fetchMock.mock.calls[0]?.[0])
    expect(new URL(url, 'http://localhost').searchParams.get('window')).toBe('30d')
  })
})
