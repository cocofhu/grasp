import { describe, expect, it } from 'vitest'
import { boundConfigurationIds, effectiveModelAgent, modelRouteValue, parseModelRoute } from './modelBindings'

describe('group-qualified model routing', () => {
  it('reads existing singleton bindings and keeps the explicit default first', () => {
    expect(boundConfigurationIds(undefined)).toEqual([])
    expect(boundConfigurationIds({ kind: 'configuration', configurationId: 'primary' })).toEqual(['primary'])
    expect(boundConfigurationIds({ kind: 'configuration', configurationId: 'b', configurationIds: ['a', 'b', 'c', 'a'] })).toEqual(['b', 'a', 'c'])
  })
  it('preserves original model IDs and distinguishes the same model through two groups', () => {
    const id = '厂商/model/with spaces'
    expect(modelRouteValue('first', id)).not.toBe(modelRouteValue('second', id))
    expect(parseModelRoute(modelRouteValue('first', id))).toEqual({ configurationId: 'first', modelId: id })
    expect(parseModelRoute('vendor/model')).toEqual({ modelId: 'vendor/model' })
    expect(parseModelRoute('@group:broken')).toEqual({ modelId: '@group:broken' })
    expect(parseModelRoute('@group:[1,2]')).toEqual({ modelId: '@group:[1,2]' })
  })
  it('projects defaults only for Agents that explicitly inherit', () => {
    const local = { name: 'Coder', acpBackend: 'codex' as const, modelBinding: { kind: 'configuration' as const, configurationId: 'local' } }
    const defaults = { acpBackend: 'opencode' as const, modelBinding: { kind: 'configuration' as const, configurationId: 'shared', configurationIds: ['shared', 'other'] } }
    expect(effectiveModelAgent(local, defaults)).toBe(local)
    expect(effectiveModelAgent({ ...local, inheritModel: true }, defaults).modelBinding).toEqual(defaults.modelBinding)
    expect(effectiveModelAgent({ ...local, inheritModel: true }, undefined).modelBinding).toBeUndefined()
  })

})
