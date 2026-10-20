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

  it('workspaceClient builds audit/credential/draft URLs and issues req', async () => {
    await workspaceClient.getWorkspace()
    await workspaceClient.getWorkspace()
    await workspaceClient.updateWorkspace({ name: 'n2' })
    await workspaceClient.getProjectSharedAgentConfig('p1')
    await workspaceClient.putProjectSharedAgentConfig('p1', {
      files: [],
      mcp: [],
      env: {},
      layout: {},
    })
    await workspaceClient.createProjectSharedAgentTest('p1', { agentName: 'a' })
    await workspaceClient.getProjectCredentials('p1')
    await workspaceClient.createProjectCredential('p1', {
      type: 'custom',
      name: 'demo',
      value: 'secret',
    })
    await workspaceClient.putProjectCredential('p1', 'cred-1', {
      type: 'custom',
      name: 'demo',
      value: 'rotated',
    })
    await workspaceClient.deleteProjectCredential('p1', 'cred-1')
    await workspaceClient.listRequirementDrafts('p1', { status: 'open', q: 'x' })
    await workspaceClient.getRequirementDraft('p1', 'd1')
    await workspaceClient.createRequirementDraft('p1', { title: 't' })
    await workspaceClient.updateRequirementDraft('p1', 'd1', { title: 't' })
    await workspaceClient.patchRequirementDraftStatus('p1', 'd1', 'done')
    await workspaceClient.patchRequirementDraftSchedule('p1', 'd1', {
      parentId: null,
      progress: 1,
    })
    await workspaceClient.deleteRequirementDraft('p1', 'd1')
    await workspaceClient.getProjectExternalMcp('p1')
    await workspaceClient.updateProjectExternalMcp('p1', { enabled: true, enabledPacks: [] })
    await workspaceClient.listProjectMcpKeys('p1')
    await workspaceClient.createProjectMcpKey('p1', 'k')
    await workspaceClient.revokeProjectMcpKey('p1', 'k1')
    await workspaceClient.listProjectAudit('p1', {
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
    await workspaceClient.listProjectAuditFacets('p1', { time: '7d', runId: 'r', from: 'a', to: 'b' })
    const url = workspaceClient.exportProjectAuditUrl('p1', {
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
    await pmClient.getPmLeader('p1')
    await pmClient.updatePmLeader('p1', { enabled: true })
    await pmClient.listProjectCronJobs('p1')
    await pmClient.patchProjectCronJob('p1', 'j1', { deliverToChannel: true })
    await pmClient.deleteProjectCronJob('p1', 'j1')
    await pmClient.listPmThreads('p1')
    await pmClient.createPmThread('p1', { title: 't' })
    await pmClient.getPmThread('p1', 't1')
    await pmClient.deletePmThread('p1', 't1')
    await pmClient.listPmMessages('p1', 't1', { limit: 10, before: 'm0' })
    await pmClient.appendPmMessage('p1', 't1', { content: 'hi' })
    await pmClient.patchPmMessage('p1', 't1', 'm1', { status: 'ok' })
    await pmClient.ensurePmSandbox('p1', 't1', { injectHistory: true })
    await pmClient.startPmTurn('p1', 't1', { content: 'hi' })
    await pmClient.cancelPmTurn('p1', 't1')
    expect(pmClient.pmThreadChatWsUrl('p1', 't1')).toContain('/chat')
  })

  it('agentsClient list/get plus import error path', async () => {
    await agentsClient.listAgents()
    await agentsClient.getAgent('a1')
    const file = new File(['x'], 'a.zip')
    await agentsClient.importAgent(file, { projectId: 'p1', targetName: 'n', mode: 'create' })
    ;(fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce(
      new Response('not-json', { status: 500 }),
    )
    await expect(agentsClient.importAgent(file, { projectId: 'p1', targetName: 'n', mode: 'overwrite' })).rejects.toThrow()
  })
})
