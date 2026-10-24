import { describe, expect, it } from 'vitest'
import type { Agent } from '@/lib/api/api'
import { draftPayloadJson, fromDraft, fromDraftRaw, hydrateStudioDraft, toDraft } from './agentStudioDraft'

const baseAgent: Agent = {
  name: '综合代码审查工程师',
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
      env: { FOO: '1' },
      capabilities: { interaction: 'auto', reads: ['*'] },
    })
    const canonical = fromDraft(d)
    expect(canonical.env).toEqual({ FOO: '1' })
    expect(JSON.parse(draftPayloadJson(d))).toEqual(canonical)
  })
})

describe('credential binding round-trip', () => {
  it('serializes only custom environment credential bindings alongside named model and Git selections', () => {
    const draft = toDraft({ ...baseAgent, modelBinding: { kind: 'configuration', configurationId: 'cfg' }, gitBindings: { items: [{ credentialId: 'git' }], preferredCredentialId: 'git' }, credentialBindings: { OPENAI_API_KEY: 'model-key', GITHUB_TOKEN: 'git', SENTRY_TOKEN: 'custom' } })
    expect(fromDraft(draft).credentialBindings).toEqual({ SENTRY_TOKEN: 'custom' })
    expect(fromDraft(draft).modelBinding).toEqual({ kind: 'configuration', configurationId: 'cfg' })
    expect(fromDraft(draft).gitBindings?.preferredCredentialId).toBe('git')
  })
  it('preserves explicit IDs, disabled keys and the distinction from inheritance', () => {
    const bindings = { SENTRY_TOKEN: 'cred-1', CUSTOM_TOKEN: null }
    const draft = toDraft({ ...baseAgent, credentialBindings: bindings })
    expect(fromDraft(draft).credentialBindings).toEqual(bindings)
    draft.credentialBindings!.SENTRY_TOKEN = 'cred-2'
    expect(bindings.SENTRY_TOKEN).toBe('cred-1')
    expect(fromDraftRaw(draft).credentialBindings).toEqual({ SENTRY_TOKEN: 'cred-2', CUSTOM_TOKEN: null })
    expect(fromDraft(toDraft(baseAgent))).not.toHaveProperty('credentialBindings')
  })
})
