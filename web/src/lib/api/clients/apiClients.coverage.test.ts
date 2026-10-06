// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { projectsClient } from './projectsClient'
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

  it('projectsClient builds audit/token/draft URLs and issues req', async () => {
    await projectsClient.listProjects()
    await projectsClient.getProject('p1')
    await projectsClient.createProject({ name: 'n', description: '', variables: [] })
    await projectsClient.updateProject('p1', { name: 'n2' })
    await projectsClient.deleteProject('p1')
    await projectsClient.getProjectSharedAgentConfig('p1')
    await projectsClient.putProjectSharedAgentConfig('p1', {
      files: [],
      mcp: [],
      env: {},
      layout: {},
    })
    await projectsClient.createProjectSharedAgentTest('p1', { agentName: 'a' })
    await projectsClient.getProjectCredentials('p1')
    await projectsClient.createProjectCredential('p1', {
      type: 'custom',
      name: 'demo',
      value: 'secret',
    })
    await projectsClient.putProjectCredential('p1', 'cred-1', {
      type: 'custom',
      name: 'demo',
      value: 'rotated',
    })
    await projectsClient.deleteProjectCredential('p1', 'cred-1')
    await projectsClient.listRequirementDrafts('p1', { status: 'open', q: 'x' })
    await projectsClient.getRequirementDraft('p1', 'd1')
    await projectsClient.createRequirementDraft('p1', { title: 't' })
    await projectsClient.updateRequirementDraft('p1', 'd1', { title: 't' })
    await projectsClient.patchRequirementDraftStatus('p1', 'd1', 'done')
    await projectsClient.patchRequirementDraftSchedule('p1', 'd1', {
      parentId: null,
      progress: 1,
    })
    await projectsClient.deleteRequirementDraft('p1', 'd1')
    await projectsClient.getProjectExternalMcp('p1')
    await projectsClient.updateProjectExternalMcp('p1', { enabled: true, enabledPacks: [] })
    await projectsClient.listProjectMcpKeys('p1')
    await projectsClient.createProjectMcpKey('p1', 'k')
    await projectsClient.revokeProjectMcpKey('p1', 'k1')
    await projectsClient.listProjectAudit('p1', {
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
    await projectsClient.listProjectAuditFacets('p1', { time: '7d', runId: 'r', from: 'a', to: 'b' })
    const url = projectsClient.exportProjectAuditUrl('p1', {
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
    await projectsClient.getProjectTokenStats('p1', { window: '7d', timezone: 'UTC', utcOffsetMinutes: 480 })
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
