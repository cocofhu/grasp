import { describe, expect, it } from 'vitest'
import {
  applyOpenCodeFields,
  normalizeOpenCodeProvider,
  OPENCODE_FALLBACK_PROVIDERS,
  openCodeCustomBaseRequired,
  openCodeFieldsFromEnv,
  openCodeModelFromCatalog,
  openCodeModelID,
  openCodeModelRequired,
  openCodeModelWithProvider,
  switchOpenCodeEnv,
} from './openCodeProvider'

describe('openCodeProvider', () => {
  it('keeps any catalog-shaped vendor id', () => {
    expect(normalizeOpenCodeProvider('')).toBe('openai')
    expect(normalizeOpenCodeProvider('Anthropic')).toBe('anthropic')
    // The vendor list is the catalog's, so an id we never enumerated survives.
    expect(normalizeOpenCodeProvider('ZAI')).toBe('zai')
    expect(normalizeOpenCodeProvider('my-gateway.v2')).toBe('my-gateway.v2')
    expect(normalizeOpenCodeProvider('has space')).toBe('custom')
  })

  it('requires base URL for custom', () => {
    expect(openCodeCustomBaseRequired('custom', '')).toBe(true)
    expect(openCodeCustomBaseRequired('custom', 'https://x')).toBe(false)
    expect(openCodeCustomBaseRequired('openai', '')).toBe(false)
  })

  it('does not require a base URL for a vendor whose endpoint the server knows', () => {
    // aimlapi may be absent from the catalog; the server fills its endpoint in.
    expect(openCodeCustomBaseRequired('aimlapi', '')).toBe(false)
    expect(openCodeCustomBaseRequired('AIMLAPI', '')).toBe(false)
  })

  it('lists AI/ML API first on the offline shortlist', () => {
    expect(OPENCODE_FALLBACK_PROVIDERS[0]?.id).toBe('aimlapi')
  })

  it('requires a model id', () => {
    expect(openCodeModelRequired('')).toBe(true)
    expect(openCodeModelRequired('  ')).toBe(true)
    expect(openCodeModelRequired('anthropic/claude-sonnet-4-5')).toBe(false)
  })

  it('offers a translated shortlist for an unreachable catalog', () => {
    const ids = OPENCODE_FALLBACK_PROVIDERS.map((p) => p.id)
    expect(ids).toContain('custom')
    expect(ids).toContain('deepseek')
    for (const p of OPENCODE_FALLBACK_PROVIDERS) {
      expect(p.labelKey, p.id).toMatch(/^pages\.agentStudio\.openCode\.providers\./)
      expect(normalizeOpenCodeProvider(p.id)).toBe(p.id)
    }
  })

  it('prefixes the vendor onto a hand-typed id, idempotently', () => {
    expect(openCodeModelWithProvider('deepseek-v4-pro', 'deepseek')).toBe('deepseek/deepseek-v4-pro')
    expect(openCodeModelWithProvider('deepseek/deepseek-v4-pro', 'deepseek')).toBe(
      'deepseek/deepseek-v4-pro',
    )
    // A gateway id carries slashes of its own and is prefixed whole.
    expect(openCodeModelWithProvider('deepseek/deepseek-flash', 'custom')).toBe(
      'custom/deepseek/deepseek-flash',
    )
    expect(openCodeModelWithProvider('', 'zai')).toBe('')
  })

  it("strips the vendor prefix to recover the vendor's own id", () => {
    expect(openCodeModelID('deepseek/deepseek-v4-pro', 'deepseek')).toBe('deepseek-v4-pro')
    expect(openCodeModelID('deepseek-v4-pro', 'deepseek')).toBe('deepseek-v4-pro')
    expect(openCodeModelID('openrouter/qwen/qwen3.7-max', 'openrouter')).toBe('qwen/qwen3.7-max')
    expect(openCodeModelID('', 'zai')).toBe('')
  })

  it('tells whether a model came from the vendor listing', () => {
    const models = [{ id: 'deepseek-v4-pro' }, { id: 'deepseek-v4-flash' }]
    expect(openCodeModelFromCatalog('deepseek/deepseek-v4-pro', 'deepseek', models)).toBe(true)
    expect(openCodeModelFromCatalog('deepseek-v4-pro', 'deepseek', models)).toBe(true)
    // Hand-typed ids survive a vendor switch.
    expect(openCodeModelFromCatalog('custom/deepseek/deepseek-flash', 'custom', models)).toBe(false)
    expect(openCodeModelFromCatalog('', 'deepseek', models)).toBe(false)
  })

  it('recognizes a self-prefixed catalog id as a listing pick', () => {
    const models = [{ id: 'openrouter/auto' }]
    expect(openCodeModelFromCatalog('openrouter/openrouter/auto', 'openrouter', models)).toBe(true)
  })

  it('writes and strips managed OpenCode env', () => {
    const env = applyOpenCodeFields(
      {},
      { provider: 'custom', baseURL: 'https://llm.example/v1', model: 'foo' },
    )
    expect(openCodeFieldsFromEnv(env)).toEqual({
      provider: 'custom',
      baseURL: 'https://llm.example/v1',
      // A bare id gains its vendor on write, so the stored value is complete.
      model: 'custom/foo',
      vision: false,
    })
    expect(switchOpenCodeEnv(env, 'cursor')).toEqual({ ACP_BRIDGE_MODEL: 'custom/foo' })
    expect(switchOpenCodeEnv({}, 'opencode').GRASP_OPENCODE_PROVIDER).toBe('openai')
  })

  it('opts a typed-in model into image input through env', () => {
    const env = applyOpenCodeFields(
      { GRASP_OPENCODE_MODEL_VISION: '1' },
      { provider: 'tencent-tokenhub', model: 'deepseek/deepseek-flash' },
    )
    expect(openCodeFieldsFromEnv(env).vision).toBe(true)
    expect(env.GRASP_OPENCODE_MODEL_VISION).toBe('1')
    expect(applyOpenCodeFields(env, { vision: false }).GRASP_OPENCODE_MODEL_VISION).toBeUndefined()
    expect(switchOpenCodeEnv(env, 'cursor').GRASP_OPENCODE_MODEL_VISION).toBeUndefined()
  })
})
