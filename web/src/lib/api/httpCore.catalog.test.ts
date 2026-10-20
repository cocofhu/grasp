// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { req } from './httpCore'
import { workflowCatalogRevision } from './workflowCatalogEvents'

afterEach(() => {
  vi.unstubAllGlobals()
})

function respond(status: number, body: unknown) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async () =>
      new Response(JSON.stringify(body), {
        status,
        headers: { 'Content-Type': 'application/json' },
      }),
    ),
  )
}

describe('workflow catalog invalidation', () => {
  it('advances the revision after a successful onboarding response and not after a failure', async () => {
    const before = workflowCatalogRevision.value
    respond(500, { error: 'bootstrap failed' })
    await expect(req('/workspace/bootstrap-onboarding', { method: 'POST', body: '{}' })).rejects.toThrow(
      'bootstrap failed',
    )
    expect(workflowCatalogRevision.value).toBe(before)

    respond(200, { agentIds: ['需求澄清'], workflowId: 'wf-1', published: true })
    await req('/workspace/bootstrap-onboarding', { method: 'POST', body: '{}' })
    expect(workflowCatalogRevision.value).toBe(before + 1)
  })

  it('keeps unrelated writes and reads from advancing the onboarding revision', async () => {
    const before = workflowCatalogRevision.value
    respond(200, { ok: true })
    await req('/agent-teams/bootstrap', { method: 'POST', body: '{}' })
    await req('/workspace/bootstrap-onboarding', { method: 'GET' })
    await req('/workflows', { method: 'GET' })
    expect(workflowCatalogRevision.value).toBe(before)

    await req('/workflows', { method: 'POST', body: '{}' })
    expect(workflowCatalogRevision.value).toBe(before + 1)
  })
})
