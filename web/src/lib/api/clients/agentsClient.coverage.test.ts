// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { agentsClient } from './agentsClient'

const jsonResponse = (body: unknown = { status: 'ok' }, init: ResponseInit = {}) =>
  new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json', ...init.headers },
    ...init,
  })

describe('agentsClient request coverage', () => {
  const fetchMock = vi.fn()

  beforeEach(() => {
    fetchMock.mockReset()
    fetchMock.mockImplementation(async () => jsonResponse())
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => vi.unstubAllGlobals())

  it('builds agent, team, project bundle, revision, memory, thread, and cron requests', async () => {
    const agent = { name: 'agent / one', description: 'reviewer' } as never

    await agentsClient.listAgents()
    await agentsClient.getAgent('agent / one')
    await agentsClient.bootstrapProjectOnboarding('project / 1', {
      acpBackend: 'cursor',
      apiKey: 'secret',
    })
    await agentsClient.bootstrapAgentTeam({ projectId: 'p1' } as never)
    await agentsClient.getAgentTeamBootstrap('team / 1')
    await agentsClient.retryAgentTeamBootstrap('team / 1')
    await agentsClient.listAgentTeamTemplates()
    await agentsClient.createAgent(agent)
    await agentsClient.saveAgent(agent, { reason: 'update' })
    await agentsClient.renameAgent('agent / one', 'renamed')
    await agentsClient.deleteAgent('agent / one')
    await agentsClient.listAgentWorkspaceRevisions('agent / one')
    await agentsClient.getAgentWorkspaceRevisionDiff('agent / one', 'sha / 1')
    await agentsClient.restoreAgentWorkspaceRevision('agent / one', 'sha / 1')
    await agentsClient.listAgentMemories('agent / one')
    await agentsClient.upsertAgentMemory('agent / one', { title: 't', content: 'c' })
    await agentsClient.updateAgentMemory('agent / one', 'm1', { content: 'new' })
    await agentsClient.deleteAgentMemory('agent / one', 'm1')
    await agentsClient.clearAgentMemories('agent / one')
    await agentsClient.listAgentThreads('agent / one')
    await agentsClient.listAgentThreadMessages('agent / one', 't1')
    await agentsClient.deleteAgentThread('agent / one', 't1')
    await agentsClient.listAgentCronJobs('agent / one')
    await agentsClient.patchAgentCronJob('agent / one', 'j1', { enabled: true })
    await agentsClient.deleteAgentCronJob('agent / one', 'j1')

    expect(fetchMock).toHaveBeenCalledWith('/api/agents/agent%20%2F%20one/rename', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ name: 'renamed' }),
    }))
    expect(fetchMock).toHaveBeenCalledWith('/api/projects/project%20%2F%201/bootstrap-onboarding', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ acpBackend: 'cursor', apiKey: 'secret' }),
    }))
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/agents/agent%20%2F%20one/workspace/revisions/sha%20%2F%201/restore',
      expect.objectContaining({ method: 'POST', body: JSON.stringify({ reason: '' }) }),
    )
  })

  it('exports agents and maps JSON and non-JSON failures', async () => {
    const blob = new Blob(['zip'])
    fetchMock.mockResolvedValueOnce(new Response(blob, { status: 200 }))
    await expect(agentsClient.exportAgent('a / b')).resolves.toBeInstanceOf(Blob)
    expect(fetchMock).toHaveBeenLastCalledWith('/api/agents/a%20%2F%20b/export', {
      credentials: 'include',
    })

    fetchMock.mockResolvedValueOnce(jsonResponse({ error: 'export denied' }, { status: 403 }))
    await expect(agentsClient.exportAgent('a')).rejects.toThrow('export denied')
    fetchMock.mockResolvedValueOnce(new Response('bad gateway', { status: 502 }))
    await expect(agentsClient.exportAgent('a')).rejects.toThrow('502 export failed')
  })

  it('exports project bundles with every content-disposition filename form', async () => {
    const cases = [
      ["attachment; filename*=UTF-8''report%20pack.zip", 'report pack.zip'],
      ['attachment; filename="quoted.zip"', 'quoted.zip'],
      ['attachment; filename=plain.zip', 'plain.zip'],
      ['attachment', 'project.zip'],
      ["attachment; filename*=UTF-8''%E0%A4%A; filename=\"fallback.zip\"", 'fallback.zip'],
    ]
    for (const [header, filename] of cases) {
      fetchMock.mockResolvedValueOnce(
        new Response(new Blob(['zip']), {
          status: 200,
          headers: { 'Content-Disposition': header },
        }),
      )
      await expect(agentsClient.exportProjectAgents('p / 1')).resolves.toMatchObject({ filename })
    }
    expect(fetchMock).toHaveBeenCalledWith('/api/projects/p%20%2F%201/agents/export', {
      credentials: 'include',
    })

    fetchMock.mockResolvedValueOnce(jsonResponse({ error: 'project denied' }, { status: 403 }))
    await expect(agentsClient.exportProjectAgents('p')).rejects.toThrow('project denied')
    fetchMock.mockResolvedValueOnce(new Response('oops', { status: 500 }))
    await expect(agentsClient.exportProjectAgents('p')).rejects.toThrow('500 export failed')
  })

  it('imports project bundles and agents with exact multipart fields and handles failures', async () => {
    const file = new File(['zip'], 'agents.zip')
    fetchMock.mockResolvedValueOnce(jsonResponse({ created: ['a'], overwritten: ['b'] }))
    await expect(agentsClient.importProjectAgents('p / 1', file, { mode: 'overwrite' })).resolves.toEqual({
      created: ['a'],
      overwritten: ['b'],
    })
    let [, init] = fetchMock.mock.calls.at(-1)!
    expect(fetchMock.mock.calls.at(-1)![0]).toBe('/api/projects/p%20%2F%201/agents/import')
    expect(init).toMatchObject({ method: 'POST', credentials: 'include' })
    expect((init.body as FormData).get('file')).toBe(file)
    expect((init.body as FormData).get('mode')).toBe('overwrite')

    fetchMock.mockResolvedValueOnce(jsonResponse({ name: 'new-agent' }))
    await agentsClient.importAgent(file, { projectId: 'p1', targetName: 'new-agent', mode: 'create' })
    ;[, init] = fetchMock.mock.calls.at(-1)!
    expect(fetchMock.mock.calls.at(-1)![0]).toBe('/api/agents/import')
    expect((init.body as FormData).get('projectId')).toBe('p1')
    expect((init.body as FormData).get('targetName')).toBe('new-agent')
    expect((init.body as FormData).get('mode')).toBe('create')

    fetchMock.mockResolvedValueOnce(jsonResponse({ error: 'bad archive' }, { status: 400 }))
    await expect(agentsClient.importProjectAgents('p1', file, { mode: 'rename' })).rejects.toThrow('bad archive')
    fetchMock.mockResolvedValueOnce(new Response('invalid', { status: 422 }))
    await expect(agentsClient.importProjectAgents('p1', file, { mode: 'rename' })).rejects.toThrow('422 import failed')
    fetchMock.mockResolvedValueOnce(jsonResponse({ error: 'name exists' }, { status: 409 }))
    await expect(agentsClient.importAgent(file, { projectId: 'p1', targetName: 'a', mode: 'create' })).rejects.toThrow('name exists')
    fetchMock.mockResolvedValueOnce(new Response('invalid', { status: 500 }))
    await expect(agentsClient.importAgent(file, { projectId: 'p1', targetName: 'a', mode: 'create' })).rejects.toThrow('500 import failed')
  })
})
