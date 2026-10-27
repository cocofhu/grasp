import { describe, expect, it } from 'vitest'
import type { ModelConnection } from '@/lib/api/api'
import { mergeModelMetadata, modelInputMode, setModelInputMode } from './modelCapabilities'

type Model = ModelConnection['models'][number]
const saved: Model = { id: 'kimi-k3.1-pro', source: 'discovered', capabilities: {} }
const catalog: Model = { ...saved, name: 'Kimi', input: ['text', 'image'], capabilities: { vision: true, toolCalls: true, reasoning: true }, contextWindow: 1000000, maxTokens: 32000, reasoningEfforts: ['low', 'high'] }

describe('model image input declarations', () => {
  it.each<[Partial<Model> | undefined, string]>([
    [undefined, 'default'], [{ capabilities: {} }, 'default'], [{ input: [] }, 'default'],
    [{ capabilities: { vision: true } }, 'image'], [{ capabilities: { vision: false } }, 'text'],
    [{ input: ['text', 'image'], capabilities: { vision: false } }, 'image'],
    [{ input: ['text'], capabilities: { vision: true } }, 'text'],
  ])('resolves explicit input before capability flags: %j', (fields, mode) => {
    expect(modelInputMode(fields && { ...saved, ...fields })).toBe(mode)
  })

  it('fills missing saved metadata even when capabilities is an empty object', () => {
    expect(mergeModelMetadata(saved, catalog)).toEqual(catalog)
    expect(saved).toEqual({ id: 'kimi-k3.1-pro', source: 'discovered', capabilities: {} })
  })

  it.each<Model>([
    { ...saved, capabilities: { vision: false } },
    { ...saved, input: ['text'] },
  ])('keeps explicit text-only settings during a catalog refresh: %j', (model) => {
    const merged = mergeModelMetadata(model, catalog)
    expect(modelInputMode(merged)).toBe('text')
    expect(merged.input || []).not.toContain('image')
    expect(merged.capabilities?.vision).not.toBe(true)
    expect(merged.capabilities?.toolCalls).toBe(true)
  })

  it('preserves saved values and fills individual capability fields', () => {
    const model: Model = { ...saved, name: 'Team model', source: 'manual', contextWindow: 128000, maxTokens: 4000, reasoningEfforts: ['medium'], capabilities: { reasoning: false } }
    expect(mergeModelMetadata(model, catalog)).toEqual({ ...model, input: ['text', 'image'], capabilities: { vision: true, toolCalls: true, reasoning: false } })
    expect(mergeModelMetadata(model)).toBe(model)
    expect(mergeModelMetadata(model, { ...catalog, id: 'another-model' })).toBe(model)
    expect(mergeModelMetadata(saved, { ...saved, name: undefined })).toEqual(saved)
  })

  it('does not let catalog text-only declarations disable a saved image model', () => {
    const model: Model = { ...saved, capabilities: { vision: true } }
    const merged = mergeModelMetadata(model, { ...catalog, input: ['text'], capabilities: { vision: false } })
    expect(modelInputMode(merged)).toBe('image')
    expect(merged.input).toBeUndefined()
    expect(merged.capabilities?.vision).toBe(true)
  })

  it('sets consistent image and text metadata without changing other capabilities', () => {
    const image = setModelInputMode({ ...catalog, capabilities: { ...catalog.capabilities, reasoning: false } }, 'image')
    expect(image.input).toEqual(['text', 'image'])
    expect(image.capabilities).toEqual({ vision: true, reasoning: false, toolCalls: true })
    const text = setModelInputMode(image, 'text')
    expect(text.input).toEqual(['text'])
    expect(text.capabilities?.vision).toBe(false)
    const defaults = setModelInputMode(text, 'default')
    expect(defaults.input).toBeUndefined()
    expect(defaults.capabilities).toEqual({ reasoning: false, toolCalls: true })
    expect(catalog.capabilities?.vision).toBe(true)
    expect(setModelInputMode(saved, 'default')).toEqual({ id: saved.id, source: saved.source })
  })
})
