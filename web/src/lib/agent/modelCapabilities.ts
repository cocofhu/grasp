import type { ModelConnection } from '@/lib/api/api'

type Model = ModelConnection['models'][number]
export type ModelInputMode = 'default' | 'text' | 'image'

// An explicit input list is authoritative, including a text-only list.
export function modelInputMode(model?: Model): ModelInputMode {
  if (model?.input?.length) return model.input.includes('image') ? 'image' : 'text'
  if (model?.capabilities?.vision !== undefined) return model.capabilities.vision ? 'image' : 'text'
  return 'default'
}

// Catalog refreshes fill gaps, but cannot replace an account's explicit choices.
export function mergeModelMetadata(saved: Model, discovered?: Model): Model {
  if (!discovered || saved.id !== discovered.id) return saved
  const merged = { ...saved }
  if (!saved.name && discovered.name) merged.name = discovered.name
  if (!saved.contextWindow && discovered.contextWindow) merged.contextWindow = discovered.contextWindow
  if (!saved.maxTokens && discovered.maxTokens) merged.maxTokens = discovered.maxTokens
  if (!saved.reasoningEfforts?.length && discovered.reasoningEfforts?.length) merged.reasoningEfforts = [...discovered.reasoningEfforts]
  const savedMode = modelInputMode(saved)
  if (!saved.input?.length && discovered.input?.length && (savedMode === 'default' || savedMode === modelInputMode(discovered))) {
    merged.input = [...discovered.input]
  }
  for (const key of ['toolCalls', 'vision', 'reasoning'] as const) {
    const value = discovered.capabilities?.[key]
    if (saved.capabilities?.[key] !== undefined || value === undefined) continue
    if (key === 'vision' && savedMode !== 'default' && value !== (savedMode === 'image')) continue
    merged.capabilities = { ...merged.capabilities, [key]: value }
  }
  return merged
}

export function setModelInputMode(model: Model, mode: ModelInputMode): Model {
  const capabilities = { ...model.capabilities }
  const updated: Model = { ...model, capabilities }
  if (mode === 'default') {
    delete updated.input
    delete capabilities.vision
    if (!Object.keys(capabilities).length) delete updated.capabilities
  } else {
    updated.input = mode === 'image' ? ['text', 'image'] : ['text']
    capabilities.vision = mode === 'image'
  }
  return updated
}
