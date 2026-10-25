import { describe, expect, it } from 'vitest'
import { configuredModelChoices } from './configuredModels'

describe('configuredModelChoices', () => {
  it('keeps an old single model as the only choice', () => {
    const choices = configuredModelChoices({
      binding: { kind: 'connection', connectionId: 'c', modelId: 'vendor/main' },
      backend: 'opencode',
      connectionModels: [{ id: 'vendor/main', name: 'Main' }, { id: 'vendor/other' }],
      providerId: 'deepseek',
      catalogReady: true,
    })
    expect(choices.map((choice) => choice.id)).toEqual(['vendor/main'])
    expect(choices[0]).toMatchObject({ name: 'Main', vendor: 'deepseek', unavailable: false })
  })

  it('lists automatic first and does not invent a role label', () => {
    const choices = configuredModelChoices({
      binding: { kind: 'platform', credentialId: 'key', auxiliaryModelIds: ['grok-4.7'] },
      backend: 'cursor',
      cliModels: [{ id: 'grok-4.7', name: 'Grok 4.7', vendor: 'xai' }, { id: 'composer-2.5', name: 'Composer', vendor: 'cursor' }],
      catalogReady: true,
    })
    expect(choices.map((choice) => choice.id)).toEqual(['', 'grok-4.7'])
    expect(choices[0]).toMatchObject({ name: '自动', backend: true, unavailable: false })
    expect(choices[1]).toMatchObject({ name: 'Grok 4.7', vendor: 'xai' })
    expect(JSON.stringify(choices)).not.toContain('主模型')
    expect(JSON.stringify(choices)).not.toContain('辅助模型')
  })

  it('marks a saved model missing from the catalog as unavailable', () => {
    const choices = configuredModelChoices({
      binding: { kind: 'connection', connectionId: 'c', modelId: 'primary', auxiliaryModelIds: ['gone'] },
      backend: 'opencode',
      connectionModels: [{ id: 'primary', name: 'Primary' }],
      providerId: 'openai',
      catalogReady: true,
    })
    expect(choices[1]).toMatchObject({ id: 'gone', unavailable: true })
  })
})
