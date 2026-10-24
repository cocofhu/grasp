import { describe, expect, it } from 'vitest'
import { bindingProblem, connectionCapabilities, isModelCredential, isToolCredential } from './modelRouting'
import { fromDraftRaw, toDraft } from './agentStudioDraft'
import type { CredentialItem, ModelConnection, ToolCapability } from '@/lib/api/api'

const key: CredentialItem = { id: 'key', type: 'ai', name: 'Shared key', purpose: 'model_api', issuer: 'deepseek', configured: true }
const platform: CredentialItem = { id: 'cursor', type: 'ai', name: 'Cursor account', purpose: 'tool_auth', toolId: 'cursor', configured: true }
const connection: ModelConnection = { id: 'service', name: 'Independent service', providerId: 'deepseek', protocol: 'anthropic-messages', baseUrl: 'https://api.deepseek.com/anthropic', credentialId: key.id, auth: { scheme: 'bearer' }, models: [{ id: 'vendor/model-v1', source: 'manual' }], state: 'active', revision: 1 }
const capabilities: ToolCapability[] = [{ toolId: 'claude_code', modes: ['connection'], protocols: ['anthropic-messages'], adapterState: 'existing', roleNames: ['fast', 'subagent'] }]

describe('independent model routing', () => {
  it('keeps a model key independent of the tool and rejects tool authorization as a model key', () => {
    expect(isModelCredential(key)).toBe(true)
    expect(isToolCredential(key, 'claude_code')).toBe(false)
    expect(isModelCredential(platform)).toBe(false)
    expect(isToolCredential(platform, 'cursor')).toBe(true)
    expect(isToolCredential(platform, 'trae')).toBe(false)
  })
  it('does not expose Cursor or Trae BYOK even if a malformed capability claims it', () => {
    for (const toolId of ['cursor', 'trae']) expect(connectionCapabilities([{ ...capabilities[0], toolId }], toolId, connection)).toEqual([])
  })
  it('filters unadapted protocols and unsupported auth schemes', () => {
    expect(connectionCapabilities(capabilities, 'claude_code', connection)).toHaveLength(1)
    expect(connectionCapabilities([{ ...capabilities[0], adapterState: 'planned' }], 'claude_code', connection)).toEqual([])
    expect(connectionCapabilities(capabilities, 'claude_code', { ...connection, protocol: 'openai-responses' })).toEqual([])
    expect(connectionCapabilities(capabilities, 'claude_code', { ...connection, auth: { scheme: 'custom-header', headerName: 'X-Key' } })).toEqual([])
  })
  it('requires a saved configuration and validates its concrete source', () => {
    const configuration = { id: 'cfg', name: 'Team', backend: 'claude_code', state: 'active' as const, revision: 1, binding: { kind: 'connection' as const, connectionId: connection.id, modelId: 'vendor/model-v1' } }
    expect(bindingProblem(undefined, 'claude_code', [connection], [key], capabilities)).toBe('missingConfiguration')
    expect(bindingProblem({ kind: 'configuration', configurationId: 'cfg' }, 'claude_code', [connection], [key], capabilities, [configuration])).toBe('')
    expect(bindingProblem({ kind: 'configuration', configurationId: 'cfg' }, 'cursor', [connection], [key], capabilities, [configuration])).toBe('configurationBackendMismatch')
    expect(bindingProblem({ kind: 'platform', credentialId: platform.id }, 'cursor', [], [platform], [])).toBe('')
    expect(bindingProblem({ kind: 'platform', credentialId: platform.id }, 'trae', [], [platform], [])).toBe('missingCredential')
  })
  it('keeps invalid references invalid without falling back to a different key, connection or model', () => {
    const binding = { kind: 'connection' as const, connectionId: connection.id, modelId: 'vendor/model-v1' }
    expect(bindingProblem(binding, 'claude_code', [{ ...connection, state: 'disabled' }], [key], capabilities)).toBe('missingConnection')
    expect(bindingProblem(binding, 'claude_code', [connection], [{ ...key, configured: false }], capabilities)).toBe('missingCredential')
    expect(bindingProblem({ ...binding, modelId: 'another/model' }, 'claude_code', [connection], [key], capabilities)).toBe('missingModel')
    expect(bindingProblem({ ...binding, toolSettings: { modelRoles: { subagent: 'another/model' } } }, 'claude_code', [connection], [key], capabilities)).toBe('missingModel')
  })
  it('serializes a new binding without legacy selections or derived runtime model env', () => {
    const draft = toDraft({ name: 'dev', acpBackend: 'claude_code', env: { ACP_BRIDGE_MODEL: 'old/model', FEATURE: 'yes' }, modelBinding: { kind: 'configuration', configurationId: 'cfg' } })
    const payload = fromDraftRaw(draft)
    expect(payload.modelBinding).toEqual({ kind: 'configuration', configurationId: 'cfg' })
    expect(payload).not.toHaveProperty('aiCredentialId')
    expect(payload).not.toHaveProperty('openCodeCredentialId')
    expect(payload.env).toEqual({ FEATURE: 'yes' })
    expect(payload.modelBinding).not.toBe(draft.modelBinding)
  })
})
