import { describe, expect, it } from 'vitest'
import { configuredModelChoices } from './configuredModels'

describe('configuredModelChoices', () => {
  it('uses real model names instead of the configuration name while preserving upstream IDs', () => {
    const choices = configuredModelChoices({
      binding: { kind: 'connection', connectionId: 'kimi-account', modelId: 'kimi-k3.1-pro', auxiliaryModelIds: ['kimi-k2.5', 'kimi-k3.1-pro'] },
      configurationName: 'TokenHub',
      backend: 'opencode',
      connectionModels: [{ id: 'kimi-k3.1-pro', name: 'Kimi K3.1 Pro' }, { id: 'kimi-k2.5', name: 'Kimi K2.5' }],
      providerId: 'moonshot',
      catalogReady: true,
    })
    expect(choices.map(({ id, name, hint, vendor, backend, unavailable }) => ({ id, name, hint, vendor, backend, unavailable }))).toEqual([
      { id: 'kimi-k3.1-pro', name: 'Kimi K3.1 Pro', hint: 'kimi-k3.1-pro', vendor: 'kimi', backend: false, unavailable: false },
      { id: 'kimi-k2.5', name: 'Kimi K2.5', hint: 'kimi-k2.5', vendor: 'kimi', backend: false, unavailable: false },
    ])
    expect(JSON.stringify(choices)).not.toContain('TokenHub')
  })

  it('uses a saved name for an automatic platform primary without changing its empty ID', () => {
    const choices = configuredModelChoices({
      binding: { kind: 'platform', credentialId: 'cursor-key', auxiliaryModelIds: ['grok-4.7'] },
      configurationName: 'Cursor daily',
      backend: 'cursor',
      cliModels: [{ id: 'grok-4.7', name: 'Grok 4.7', vendor: 'xai' }],
      catalogReady: true,
    })
    expect(choices[0]).toMatchObject({ id: '', name: 'Cursor daily', vendor: 'cursor', backend: true, unavailable: false })
    expect(choices[1]).toMatchObject({ id: 'grok-4.7', name: 'Grok 4.7', vendor: 'xai', backend: false })
  })

  it('identifies each configured model brand through an aggregator without using the configuration name', () => {
    const ids = ['kimi-k3.1-pro', 'GPT-5.5', 'glm-5.1', 'hy3-pro', 'ep-private-deployment'] as const
    const choices = configuredModelChoices({
      binding: { kind: 'connection', connectionId: 'tokenhub-account', modelId: ids[0], auxiliaryModelIds: ids.slice(1) },
      configurationName: 'Kimi',
      backend: 'deepseek_harness',
      providerId: 'tokenhub',
      connectionModels: [
        { id: ids[0]!, name: 'Kimi K3.1 Pro' },
        { id: ids[1]!, name: 'GPT 5.5' },
        { id: ids[2]!, name: 'GLM 5.1' },
        { id: ids[3]!, name: 'HY 3 Pro' },
        { id: ids[4]!, name: 'Kimi Private Deployment' },
      ],
      catalogReady: true,
    })
    expect(choices.map(({ id, vendor, backend }) => ({ id, vendor, backend }))).toEqual([
      { id: ids[0], vendor: 'kimi', backend: false },
      { id: ids[1], vendor: 'openai', backend: false },
      { id: ids[2], vendor: 'zhipu', backend: false },
      { id: ids[3], vendor: 'hunyuan', backend: false },
      { id: ids[4], vendor: 'kimi', backend: false },
    ])
    expect(choices[0]).toMatchObject({ name: 'Kimi K3.1 Pro', hint: ids[0], unavailable: false })
    expect(choices[4]).toMatchObject({ name: 'Kimi Private Deployment', hint: ids[4] })
  })

  it('uses a known model brand before a conflicting catalog vendor and configuration alias', () => {
    const choices = configuredModelChoices({
      binding: { kind: 'platform', credentialId: 'codex-key', modelId: 'kimi-k3.1-pro' },
      configurationName: 'GPT team alias',
      backend: 'codex',
      cliModels: [{ id: 'kimi-k3.1-pro', name: 'GPT model alias', vendor: 'openai' }],
      catalogReady: true,
    })
    expect(choices[0]).toMatchObject({ id: 'kimi-k3.1-pro', name: 'GPT model alias', vendor: 'kimi', backend: false })
  })

  it('keeps the coding-tool fallback for an unknown model without a model vendor', () => {
    const choices = configuredModelChoices({
      binding: { kind: 'connection', connectionId: 'private-account', modelId: 'private/internal-model' },
      backend: 'deepseek_harness',
      connectionModels: [{ id: 'private/internal-model', name: 'Internal model' }],
      catalogReady: true,
    })
    expect(choices[0]).toMatchObject({ id: 'private/internal-model', vendor: 'deepseek_harness', backend: true, unavailable: false })
  })

  it.each([undefined, '  '])('uses the catalog name when the configuration name is %s', (configurationName) => {
    const choices = configuredModelChoices({
      binding: { kind: 'connection', connectionId: 'kimi-account', modelId: 'kimi-k3.1-pro' },
      configurationName,
      backend: 'opencode',
      connectionModels: [{ id: 'kimi-k3.1-pro', name: 'Kimi K3.1 Pro' }],
      catalogReady: true,
    })
    expect(choices[0]).toMatchObject({ id: 'kimi-k3.1-pro', name: 'Kimi K3.1 Pro', hint: 'kimi-k3.1-pro', unavailable: false })
  })

  it.each([false, true])('keeps availability tied to the catalog when a named primary is missing (catalog ready: %s)', (catalogReady) => {
    const choices = configuredModelChoices({
      binding: { kind: 'connection', connectionId: 'kimi-account', modelId: 'kimi-k3.1-pro' },
      configurationName: 'Kimi',
      backend: 'opencode',
      connectionModels: [],
      catalogReady,
    })
    expect(choices[0]).toMatchObject({ id: 'kimi-k3.1-pro', name: 'kimi-k3.1-pro', hint: 'kimi-k3.1-pro', unavailable: catalogReady })
  })

  it('offers only the Codex effort levels advertised for each configured model', () => {
    const choices = configuredModelChoices({
      binding: { kind: 'platform', credentialId: 'codex-key', modelId: 'codex-main', auxiliaryModelIds: ['codex-fast'] },
      configurationName: 'Codex', backend: 'codex', catalogReady: true,
      cliModels: [
        { id: 'codex-main', name: 'Main', vendor: 'openai', efforts: ['low', 'high', 'high'] },
        { id: 'codex-fast', name: 'Fast', vendor: 'openai', efforts: ['minimal', 'medium'] },
      ],
    })
    expect(choices[0]).toMatchObject({ name: 'Main', id: 'codex-main', vendor: 'openai', backend: false, efforts: ['low', 'high'] })
    expect(choices[1]).toMatchObject({ name: 'Fast', id: 'codex-fast', efforts: ['minimal', 'medium'] })
    expect(choices[0]?.efforts).not.toContain('ultra')
  })

  it('offers connection-declared levels for a single model without adding model alternatives', () => {
    const choices = configuredModelChoices({
      binding: { kind: 'connection', connectionId: 'deepseek-account', modelId: 'deepseek-pro' },
      configurationName: 'DeepSeek', backend: 'deepseek_harness', catalogReady: true,
      connectionModels: [{ id: 'deepseek-pro', reasoningEfforts: [' low ', 'high', 'high'] }],
    })
    expect(choices).toHaveLength(1)
    expect(choices[0]).toMatchObject({ name: 'deepseek-pro', id: 'deepseek-pro', efforts: ['low', 'high'] })
  })

  it.each(['opencode', 'cursor'])('does not offer effort controls for an unsupported %s adapter', (backend) => {
    const choices = configuredModelChoices({
      binding: { kind: 'connection', connectionId: 'account', modelId: 'model' },
      backend, catalogReady: true,
      connectionModels: [{ id: 'model', reasoningEfforts: ['low', 'high'] }],
    })
    expect(choices[0]?.efforts).toEqual([])
  })

  it('does not expose declared levels when the model explicitly disables reasoning', () => {
    const choices = configuredModelChoices({
      binding: { kind: 'connection', connectionId: 'account', modelId: 'model' },
      backend: 'codex', catalogReady: true,
      connectionModels: [{ id: 'model', reasoningEfforts: ['low', 'high'], capabilities: { reasoning: false } }],
    })
    expect(choices[0]?.efforts).toEqual([])
  })

  it('keeps an old single model as the only choice', () => {
    const choices = configuredModelChoices({
      binding: { kind: 'connection', connectionId: 'c', modelId: 'vendor/main' },
      backend: 'opencode',
      connectionModels: [{ id: 'vendor/main', name: 'Main' }, { id: 'vendor/other' }],
      providerId: 'deepseek',
      catalogReady: true,
    })
    expect(choices.map((choice) => choice.id)).toEqual(['vendor/main'])
    expect(choices[0]).toMatchObject({ name: 'Main', vendor: 'deepseek', backend: false, unavailable: false })
  })

  it('lists automatic first and does not invent a role label', () => {
    const choices = configuredModelChoices({
      binding: { kind: 'platform', credentialId: 'key', auxiliaryModelIds: ['grok-4.7'] },
      backend: 'cursor',
      cliModels: [{ id: 'grok-4.7', name: 'Grok 4.7', vendor: 'xai' }, { id: 'composer-2.5', name: 'Composer', vendor: 'cursor' }],
      catalogReady: true,
    })
    expect(choices.map((choice) => choice.id)).toEqual(['', 'grok-4.7'])
    expect(choices[0]).toMatchObject({ name: '自动', vendor: 'cursor', backend: true, unavailable: false })
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
