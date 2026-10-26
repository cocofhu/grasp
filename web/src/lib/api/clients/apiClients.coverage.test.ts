// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { workspaceClient } from './workspaceClient'
import { agentsClient } from './agentsClient'
import { pmClient } from './pmClient'

describe('API clients (Vitest 4 coverage: exercise request builders)', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => {
        return new Response(JSON.stringify({ items: [], keys: [], status: 'ok' }), {
          status: 200,
          headers: {
            'Content-Type': 'application/json',
            'Content-Disposition': 'attachment; filename="pack.zip"',
          },
        })
      }),
    )
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('reads an API-account model catalog by encoded connection ID without sending credentials or a body', async () => {
    const result = { models: [{ id: 'provider/Exact.Model:latest', name: 'Provider model', source: 'discovered' }] }
    vi.mocked(fetch).mockResolvedValueOnce(new Response(JSON.stringify(result), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    expect(await workspaceClient.listModelConnectionModels('team/account name')).toEqual(result)
    const [url, options] = vi.mocked(fetch).mock.calls[0]!
    expect(String(url)).toBe('/api/workspace/model-connections/team%2Faccount%20name/models')
    expect(options?.body).toBeUndefined()
    expect(options?.method || 'GET').toBe('GET')
  })

  it('workspaceClient builds audit/credential/draft URLs and issues req', async () => {
    await workspaceClient.getWorkspace()
    await workspaceClient.getWorkspace()
    await workspaceClient.updateWorkspace({ name: 'n2' })
    await workspaceClient.getSharedAgentConfig()
    await workspaceClient.putSharedAgentConfig({
      files: [],
      mcp: [],
      env: {},
      layout: {},
    })
    await workspaceClient.createSharedAgentTest({ agentName: 'a' })
    await workspaceClient.listCredentials()
    await workspaceClient.createCredential({
      type: 'custom',
      name: 'demo',
      value: 'secret',
    })
    await workspaceClient.putCredential('cred-1', {
      type: 'custom',
      name: 'demo',
      value: 'rotated',
    })
    await workspaceClient.deleteCredential('cred-1')
    await workspaceClient.removeCredential('cred-1')
    await workspaceClient.listRequirementDrafts({ status: 'open', q: 'x' })
    await workspaceClient.getRequirementDraft('d1')
    await workspaceClient.createRequirementDraft({ title: 't' })
    await workspaceClient.updateRequirementDraft('d1', { title: 't' })
    await workspaceClient.patchRequirementDraftStatus('d1', 'done')
    await workspaceClient.patchRequirementDraftSchedule('d1', {
      parentId: null,
      progress: 1,
    })
    await workspaceClient.deleteRequirementDraft('d1')
    await workspaceClient.getExternalMcp()
    await workspaceClient.updateExternalMcp({ enabled: true, enabledPacks: [] })
    await workspaceClient.listMcpKeys()
    await workspaceClient.createMcpKey('k')
    await workspaceClient.revokeMcpKey('k1')
    await workspaceClient.listAudit({
      time: '7d',
      actor: 'pm',
      callerKind: 'pm',
      action: 'x',
      resource: 'r',
      runId: 'r',
      nodeId: 'n',
      search: 'q',
      from: 'a',
      to: 'b',
      page: 2,
      pageSize: 10,
    })
    await workspaceClient.listAuditFacets({ time: '7d', runId: 'r', from: 'a', to: 'b' })
    const url = workspaceClient.exportAuditUrl({
      format: 'text',
      time: '7d',
      actor: 'pm',
      callerKind: 'pm',
      action: 'x',
      resource: 'r',
      runId: 'r',
      nodeId: 'n',
      search: 'q',
    })
    expect(url).toContain('/audit/export')
  })

  it('pmClient issues PM and cron requests', async () => {
    await pmClient.getPmLeader()
    await pmClient.updatePmLeader({ enabled: true })
    await pmClient.listCronJobs()
    await pmClient.patchCronJob('j1', { deliverToChannel: true })
    await pmClient.deleteCronJob('j1')
    await pmClient.listPmThreads()
    await pmClient.createPmThread({ title: 't' })
    await pmClient.getPmThread('t1')
    await pmClient.deletePmThread('t1')
    await pmClient.listPmMessages('t1', { limit: 10, before: 'm0' })
    await pmClient.appendPmMessage('t1', { content: 'hi' })
    await pmClient.patchPmMessage('t1', 'm1', { status: 'ok' })
    await pmClient.ensurePmSandbox('t1', { injectHistory: true })
    await pmClient.startPmTurn('t1', { content: 'hi' })
    await pmClient.cancelPmTurn('t1')
    expect(pmClient.pmThreadChatWsUrl('t1')).toContain('/chat')
  })

  it('agentsClient list/get plus import error path', async () => {
    await agentsClient.listAgents()
    await agentsClient.getAgent('a1')
    const file = new File(['x'], 'a.zip')
    await agentsClient.importAgent(file, { targetName: 'n', mode: 'create' })
    ;(fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response('not-json', { status: 500 }),
    )
    await expect(agentsClient.importAgent(file, { targetName: 'n', mode: 'overwrite' })).rejects.toThrow()
  })
})
