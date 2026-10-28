import { describe, expect, it } from 'vitest'
import type { ConfiguredModelChoice } from '@/lib/solo/configuredModels'
import { resolveHomeLaunchModel } from './homeLaunchModel'

const primary: ConfiguredModelChoice = {
  id: 'm1',
  name: 'Primary',
  hint: 'm1',
  vendor: 'openai',
  backend: false,
  unavailable: false,
  efforts: ['low', 'medium', 'high'],
  configurationId: 'group',
  modelId: 'm1',
}
const aux: ConfiguredModelChoice = {
  id: 'm2',
  name: 'Aux',
  hint: 'm2',
  vendor: 'openai',
  backend: false,
  unavailable: false,
  efforts: ['low', 'high'],
  configurationId: 'group',
  modelId: 'm2',
}

describe('resolveHomeLaunchModel', () => {
  it('omits the field when the workflow is not clarify-first', () => {
    const decision = resolveHomeLaunchModel({
      clarify: false, ready: true, custom: true, modelId: 'm2', effort: 'low', choices: [primary, aux],
    })
    expect(decision).toEqual({ action: 'omit' })
  })

  it('sends the displayed highest effort even when the user did not change the primary model', () => {
    const decision = resolveHomeLaunchModel({
      clarify: true, ready: true, custom: false, modelId: '', effort: '', choices: [primary, aux],
    })
    expect(decision).toEqual({
      action: 'send',
      model: { configurationId: 'group', modelId: '', reasoningEffort: 'high' },
    })
  })

  it('sends the chosen auxiliary model and effort', () => {
    const decision = resolveHomeLaunchModel({
      clarify: true, ready: true, custom: true, modelId: 'm2', effort: 'low', choices: [primary, aux],
    })
    expect(decision).toEqual({
      action: 'send',
      model: { configurationId: 'group', modelId: 'm2', reasoningEffort: 'low' },
    })
  })

  it('starts on the primary model when the catalog failed and the user did not change the picker', () => {
    const decision = resolveHomeLaunchModel({
      clarify: true, ready: false, custom: false, modelId: '', effort: '', choices: [],
    })
    expect(decision).toEqual({ action: 'omit', notice: 'default' })
  })

  it('does not send when the catalog failed after the user changed the picker', () => {
    const decision = resolveHomeLaunchModel({
      clarify: true, ready: false, custom: true, modelId: 'm2', effort: 'low', choices: [],
    })
    expect(decision).toEqual({ action: 'block', notice: 'blocked' })
  })

  it('does not send an unavailable model', () => {
    const decision = resolveHomeLaunchModel({
      clarify: true, ready: true, custom: true, modelId: 'm2', effort: 'low',
      choices: [primary, { ...aux, unavailable: true }],
    })
    expect(decision).toEqual({ action: 'block', notice: 'invalid' })
  })
})
