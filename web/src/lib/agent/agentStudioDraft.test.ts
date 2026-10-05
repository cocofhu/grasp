import { describe, expect, it } from 'vitest'
import type { Agent } from '@/lib/api/api'
import { draftPayloadJson, fromDraft, fromDraftRaw, hydrateStudioDraft, toDraft } from './agentStudioDraft'

const baseAgent: Agent = {
  name: '综合代码审查工程师',
  projectId: 'p1',
  acpBackend: 'cursor',
  files: [],
  mcp: [],
  env: {},
  layout: { configRoot: '/root/.cursor', workspaceDir: '/root/workspace' },
}

describe('capabilities draft round-trip', () => {
  it('toDraft deep-clones capabilities and keeps null when undeclared', () => {
    const caps = { interaction: 'auto' as const, writes: [{ schema: 'plan', required: true }] }
    const d = toDraft({ ...baseAgent, capabilities: caps })
    expect(d.capabilities).toEqual({ ...caps, tools: [], reads: [] })
    d.capabilities!.writes![0].required = false
    expect(caps.writes[0].required).toBe(true)
    expect(toDraft({ ...baseAgent }).capabilities).toBeNull()
  })

  it('fromDraft normalizes capabilities and omits them when undeclared', () => {
    const d = hydrateStudioDraft({
      ...baseAgent,
      capabilities: { interaction: 'clarify', review: true, tools: ['ask_question'], reads: ['*', 'plan.json'], writes: [] },
    })
    expect(fromDraft(d).capabilities).toEqual({ interaction: 'clarify', tools: ['ask_question'], reads: ['*'] })
    expect(fromDraftRaw(d).capabilities).toEqual(fromDraft(d).capabilities)
    expect('capabilities' in fromDraft(toDraft({ ...baseAgent }))).toBe(false)
  })

  it('draftPayloadJson matches the canonical payload', () => {
    const d = hydrateStudioDraft({
      ...baseAgent,
      env: { GIT_SSH_PRIVATE_KEY: 'secret', FOO: '1' },
      capabilities: { interaction: 'auto', reads: ['*'] },
    })
    const raw = fromDraftRaw(d)
    const canonical = fromDraft(d)
    expect(canonical.env?.GIT_SSH_PRIVATE_KEY).toBeUndefined()
    expect(raw.env?.GIT_SSH_PRIVATE_KEY).toBe('secret')
    expect(JSON.parse(draftPayloadJson(d))).toEqual(canonical)
  })
})
