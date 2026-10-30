import { describe, expect, it } from 'vitest'
import type { ModelConfiguration, ModelConnection } from '@/lib/api/apiTypes'
import { boundModelChoices } from './configuredModels'
import { parseModelRoute } from '@/lib/agent/modelBindings'

const configurations: ModelConfiguration[] = [
  { id: 'codex', name: 'Direct', backend: 'codex', state: 'active', revision: 1, binding: { kind: 'connection', connectionId: 'openai', modelId: 'same/model' } },
  { id: 'code', name: 'Gateway', backend: 'opencode', state: 'active', revision: 1, binding: { kind: 'connection', connectionId: 'gateway', modelId: 'same/model', auxiliaryModelIds: ['other'] } },
]
const connections = [
  { id: 'openai', name: 'Account A', state: 'active', models: [{ id: 'same/model', name: 'Real model', reasoningEfforts: ['high'] }] },
  { id: 'gateway', name: 'Account B', state: 'active', models: [{ id: 'same/model', name: 'Real model' }, { id: 'other', name: 'Another model' }] },
] as ModelConnection[]
const input = { configurations, connections, cliModels: {}, cliReady: true }

describe('models from all Agent groups', () => {
  it('offers all groups with separate routing and per-group metadata', () => {
    const choices = boundModelChoices({ ...input, binding: { kind: 'configuration', configurationId: 'code', configurationIds: ['codex', 'code'] } })
    expect(choices.map(choice => [choice.name, choice.configurationName, choice.tool, choice.source])).toEqual([
      ['Real model', 'Gateway', 'opencode', 'Account B'], ['Another model', 'Gateway', 'opencode', 'Account B'], ['Real model', 'Direct', 'codex', 'Account A'],
    ])
    expect(new Set(choices.map(choice => choice.id)).size).toBe(3)
    expect(parseModelRoute(choices[2]!.id)).toEqual({ configurationId: 'codex', modelId: 'same/model' })
    expect(choices[2]?.efforts).toEqual(['high'])
  })
  it('preserves raw selection values for existing single-group Agents', () => {
    const choices = boundModelChoices({ ...input, binding: { kind: 'configuration', configurationId: 'codex' } })
    expect(choices[0]?.id).toBe('same/model')
  })
  it('automatically expands all groups and keeps route values stable as groups are added', () => {
    const binding = { kind: 'configuration' as const, configurationId: 'codex', allConfigurations: true }
    const single = boundModelChoices({ ...input, configurations: [configurations[0]!], binding })
    const expanded = boundModelChoices({ ...input, binding })
    expect(single[0]?.id).toBe(expanded[0]?.id)
    expect(parseModelRoute(single[0]!.id)).toEqual({ configurationId: 'codex', modelId: 'same/model' })
    expect(expanded.map(choice => choice.configurationId)).toEqual(['codex', 'code', 'code'])
    const disabled = boundModelChoices({ ...input, configurations: [configurations[0]!, { ...configurations[1]!, state: 'disabled' }], binding })
    expect(disabled).toEqual(single)
  })
  it('removes disabled groups and marks unavailable connections without substituting accounts', () => {
    const choices = boundModelChoices({ ...input, configurations: [configurations[0]!, { ...configurations[1]!, state: 'disabled' }], connections: [], binding: { kind: 'configuration', configurationId: 'code', configurationIds: ['code', 'codex'] } })
    expect(choices).toHaveLength(1)
    expect(choices[0]).toMatchObject({ configurationId: 'codex', unavailable: true })
    const uncertain = boundModelChoices({ ...input, connections: [], connectionReady: false, binding: { kind: 'configuration', configurationId: 'codex' } })
    expect(uncertain[0]?.unavailable).toBe(false)
  })
  it('marks cleared or revoked credentials unavailable while preserving the exact route', () => {
    const binding = { kind: 'configuration' as const, configurationId: 'codex' }
    const service = { ...connections[0]!, credentialId: 'key' }
    const cleared = boundModelChoices({ ...input, connections: [service], binding, credentials: [{ id: 'key', configured: false, purpose: 'model_api' }] as never })
    expect(cleared[0]).toMatchObject({ id: 'same/model', configurationId: 'codex', unavailable: true })
    const revoked = boundModelChoices({ ...input, connections: [service], binding, credentials: [{ id: 'key', configured: true, purpose: 'model_api', revokedAt: '2026-10-28' }] as never })
    expect(revoked[0]?.unavailable).toBe(true)
    const usable = boundModelChoices({ ...input, connections: [service], binding, credentials: [{ id: 'key', configured: true, purpose: 'model_api' }] as never })
    expect(usable[0]?.unavailable).toBe(false)
  })

})
