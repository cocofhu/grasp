import { describe, expect, it } from 'vitest'
import type { CliCatalogModel } from '@/lib/api/apiTypes'
import {
  buildView,
  contextRatio,
  formatContext,
  highlight,
  launchCommand,
  monogramColor,
  modelVendor,
  searchScore,
  strengthKeys,
  vendorLabel,
  vendorLogoProvider,
} from './cliModelView'

const list: CliCatalogModel[] = [
  { id: 'claude-4.5-sonnet', name: 'Claude Sonnet 4.5', vendor: 'anthropic', tags: ['vision', 'reasoning'], context: 200, recommended: true },
  { id: 'gpt-5', name: 'GPT-5', vendor: 'openai', tags: ['vision', 'reasoning'], context: 400, recommended: true },
  { id: 'gpt-5-codex', name: 'GPT-5 Codex', vendor: 'openai', tags: ['reasoning'], context: 400 },
  { id: 'gemini-2.5-pro', name: 'Gemini 2.5 Pro', vendor: 'google', tags: ['vision'], context: 1000 },
]

describe('model-brand icons for API accounts', () => {
  it.each([
    ['gpt-5.5', 'openai'],
    ['ChatGPT-5', 'openai'],
    ['o1-preview', 'openai'],
    ['o3-mini', 'openai'],
    ['TokenHub/Kimi-K3.1-Pro', 'kimi'],
    ['openai/kimi-k3.1-pro', 'kimi'],
    ['moonshot-v1', 'moonshot'],
    ['glm-5.1', 'zhipu'],
    ['ChatGLM-6B', 'zhipu'],
    ['hy3-pro', 'hunyuan'],
    ['hy-preview', 'hunyuan'],
    ['hunyuan-turbo', 'hunyuan'],
    ['deepseek-chat', 'deepseek'],
    ['qwen3-max', 'qwen'],
    ['qvq-72b-preview', 'qwen'],
    ['minimax-m2.7', 'minimax'],
    ['claude-sonnet-4-6', 'anthropic'],
    ['gemini-3-pro', 'google'],
    ['grok-4', 'xai'],
    ['doubao-pro', 'doubao'],
    ['seed-2.0', 'doubao'],
    ['ernie-4.5', 'qianfan'],
    ['wenxin-x1', 'qianfan'],
  ])('identifies %s independently of its API-account provider', (id, vendor) => {
    expect(modelVendor(id, '', 'tokenhub')).toBe(vendor)
    expect(vendorLogoProvider(vendor)).not.toBe('')
  })

  it('uses the model name for opaque IDs, gives recognized IDs precedence and preserves unknown fallbacks', () => {
    expect(modelVendor('ep-opaque-deployment', 'Kimi K3.1 Pro', 'tokenhub')).toBe('kimi')
    expect(modelVendor('gpt-5.5', 'Kimi alias', 'tokenhub')).toBe('openai')
    expect(modelVendor('enterprise/custom-model', 'Private model', 'tokenhub')).toBe('tokenhub')
    expect(modelVendor('', '', '')).toBe('')
    expect(vendorLogoProvider(' KIMI ')).toBe('kimi')
    expect(vendorLogoProvider('GLM')).toBe('zhipu')
    expect(vendorLogoProvider('Tencent')).toBe('hunyuan')
  })
})

describe('buildView', () => {
  it('groups by vendor in first-seen order and features recommended models', () => {
    const v = buildView(list, '', '')
    expect(v.vendors).toEqual([
      { id: 'anthropic', count: 1 },
      { id: 'openai', count: 2 },
      { id: 'google', count: 1 },
    ])
    expect(v.groups.map((g) => g.vendor)).toEqual(['anthropic', 'openai', 'google'])
    expect(v.recommended.map((m) => m.id)).toEqual(['claude-4.5-sonnet', 'gpt-5'])
    expect(v.flat).toHaveLength(4)
  })

  it('hides vendor chips when only one vendor is listed', () => {
    expect(buildView(list.slice(1, 3), '', '').vendors).toEqual([])
  })

  it('filters by vendor, recommended cards included', () => {
    const v = buildView(list, '', 'openai')
    expect(v.flat.map((m) => m.id)).toEqual(['gpt-5', 'gpt-5-codex'])
    expect(v.recommended.map((m) => m.id)).toEqual(['gpt-5'])
  })

  it('ranks a query across vendors and drops the featured cards', () => {
    const v = buildView(list, 'gpt-5', '')
    expect(v.recommended).toEqual([])
    expect(v.groups).toHaveLength(1)
    expect(v.flat.map((m) => m.id)).toEqual(['gpt-5', 'gpt-5-codex'])
    expect(v.exact).toBe(true)
  })

  it('reports no exact match for a partial query', () => {
    expect(buildView(list, 'gem', '').exact).toBe(false)
  })
})

describe('searchScore', () => {
  it('prefers an exact id, then name prefix, id prefix, contains, vendor', () => {
    const m = list[1]!
    expect(searchScore(m, 'gpt-5')).toBeGreaterThan(searchScore(m, 'gpt'))
    expect(searchScore(list[2]!, 'codex')).toBeGreaterThan(0)
    expect(searchScore(list[0]!, 'anthropic')).toBe(1)
    expect(searchScore(list[0]!, 'zzz')).toBe(0)
  })
})

describe('highlight', () => {
  it('splits around the first case-insensitive hit', () => {
    expect(highlight('Claude Sonnet', 'son')).toEqual([
      { text: 'Claude ', hit: false },
      { text: 'Son', hit: true },
      { text: 'net', hit: false },
    ])
    expect(highlight('GPT-5', '')).toEqual([{ text: 'GPT-5', hit: false }])
    expect(highlight('GPT-5', 'x')).toEqual([{ text: 'GPT-5', hit: false }])
  })
})

describe('formatting', () => {
  it('formats context windows', () => {
    expect(formatContext(200)).toBe('200k')
    expect(formatContext(1000)).toBe('1M')
    expect(formatContext(1500)).toBe('1.5M')
    expect(formatContext(undefined)).toBe('')
    expect(contextRatio(500)).toBe(0.5)
    expect(contextRatio(2000)).toBe(1)
  })

  it('previews the launch command per backend', () => {
    expect(launchCommand('cursor', 'gpt-5')).toBe('cursor-agent --model gpt-5')
    expect(launchCommand('codex', 'gpt-5-codex')).toBe('codex exec --model gpt-5-codex')
    expect(launchCommand('claude_code', '')).toBe('claude')
    expect(launchCommand('kimi_code', 'kimi-k3.1-pro')).toBe('kimi acp')
    expect(launchCommand('kimi_code', '')).toBe('kimi acp')
  })

  it('derives strengths from tags', () => {
    expect(strengthKeys(list[0]!)).toEqual(['refactor', 'screenshots'])
    expect(strengthKeys(list[3]!)).toEqual(['screenshots', 'largeRepo'])
    expect(strengthKeys({ id: 'x', name: 'x', vendor: 'x' })).toEqual(['general'])
  })

  it('labels vendors and colors monograms stably', () => {
    expect(vendorLabel('openai')).toBe('OpenAI')
    expect(vendorLabel('zhipu')).toBe('Zhipu')
    expect(vendorLabel('opencode')).toBe('OpenCode')
    expect(vendorLabel('opencode-go')).toBe('OpenCode Go')
    expect(vendorLogoProvider('opencode')).toBe('')
    expect(vendorLogoProvider('opencode-go')).toBe('opencode')
    expect(monogramColor('zhipu')).toBe(monogramColor('zhipu'))
  })
})
