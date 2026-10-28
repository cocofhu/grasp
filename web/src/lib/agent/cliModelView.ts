import type { CliCatalogModel, CliModelTag } from '@/lib/api/apiTypes'

/** The argv head each backend's sandbox launches; the picker previews `--model` against it. */
const LAUNCH_HEAD: Record<string, string> = {
  cursor: 'cursor-agent',
  claude_code: 'claude',
  codebuddy: 'codebuddy',
  trae: 'traecli acp serve',
  codex: 'codex exec',
  deepseek_harness: 'dsh --profile acp',
}

const VENDOR_LABELS: Record<string, string> = {
  anthropic: 'Anthropic',
  openai: 'OpenAI',
  google: 'Google',
  xai: 'xAI',
  deepseek: 'DeepSeek',
  moonshot: 'Moonshot',
  kimi: 'Kimi',
  hunyuan: 'Tencent Hunyuan',
  qwen: 'Qwen',
  minimax: 'MiniMax',
  zhipu: 'Zhipu',
  zai: 'Z.ai',
  doubao: 'Doubao',
  qianfan: 'Baidu Qianfan',
  openrouter: 'OpenRouter',
  siliconflow: 'SiliconFlow',
  tencent: 'Tencent',
  bytedance: 'ByteDance',
  cursor: 'Cursor',
  opencode: 'OpenCode',
  'opencode-go': 'OpenCode Go',
}

/** Logo files under `assets/provider-logos` that stand for a vendor. */
const VENDOR_LOGOS: Record<string, string> = {
  anthropic: 'claude',
  openai: 'openai',
  google: 'google',
  xai: 'xai',
  deepseek: 'deepseek',
  moonshot: 'moonshot',
  moonshotai: 'moonshot',
  kimi: 'kimi',
  hunyuan: 'hunyuan',
  tencent: 'hunyuan',
  qwen: 'qwen',
  alibaba: 'qwen',
  minimax: 'minimax',
  zhipu: 'zhipu',
  glm: 'zhipu',
  zai: 'zai',
  doubao: 'doubao',
  qianfan: 'qianfan',
  openrouter: 'openrouter',
  siliconflow: 'siliconflow',
  bytedance: 'bytedance',
  cursor: 'cursor',
  opencode: 'opencode',
  'opencode-go': 'opencode',
}

export const TAG_ORDER: CliModelTag[] = ['vision', 'reasoning', 'fast']

/** Full context bar; anything larger is drawn full. */
export const CONTEXT_FULL_K = 1000

export function vendorLabel(vendor: string): string {
  return VENDOR_LABELS[vendor] || (vendor ? vendor.charAt(0).toUpperCase() + vendor.slice(1) : '')
}

export function vendorLogoProvider(vendor: string): string {
  return VENDOR_LOGOS[vendor.trim().toLowerCase()] || ''
}

const MODEL_BRANDS: [RegExp, string][] = [
  [/^(?:gpt|chatgpt)(?:$|[-_.\s:])|^o[1-9]\d?(?:$|[-_.\s:])|^openai$/, 'openai'],
  [/^kimi(?:$|[-_.\s:])/, 'kimi'],
  [/^moonshot(?:ai)?(?:$|[-_.\s:])/, 'moonshot'],
  [/^(?:chatglm|glm)(?:$|[-_.\s:\d])|^zhipu$/, 'zhipu'],
  [/^hunyuan(?:$|[-_.\s:\d])|^hy(?:\d|[-_.\s:])/, 'hunyuan'],
  [/^deepseek(?:$|[-_.\s:])/, 'deepseek'],
  [/^(?:qwen|qvq)(?:$|[-_.\s:\d])/, 'qwen'],
  [/^minimax(?:$|[-_.\s:])/, 'minimax'],
  [/^claude(?:$|[-_.\s:])|^anthropic$/, 'anthropic'],
  [/^gemini(?:$|[-_.\s:])|^google$/, 'google'],
  [/^grok(?:$|[-_.\s:])|^xai$/, 'xai'],
  [/^(?:doubao|seed|seedream)(?:$|[-_.\s:])|^bytedance$/, 'doubao'],
  [/^(?:ernie|wenxin)(?:$|[-_.\s:])|^baidu$/, 'qianfan'],
]

/** Visual brand only: never use this inference to route requests or credentials. */
export function modelVendor(modelId: string, modelName = '', fallbackVendor = ''): string {
  for (const value of [modelId, modelName]) {
    // The model family wins over an aggregator/provider prefix in a full ID.
    for (const part of value.trim().toLowerCase().split('/').reverse()) {
      const brand = MODEL_BRANDS.find(([pattern]) => pattern.test(part.trim()))
      if (brand) return brand[1]
    }
  }
  return fallbackVendor
}

export function formatContext(k?: number): string {
  if (!k) return ''
  if (k >= 1000) {
    const m = k / 1000
    return `${Number.isInteger(m) ? m : m.toFixed(1)}M`
  }
  return `${k}k`
}

export function contextRatio(k?: number): number {
  if (!k) return 0
  return Math.min(1, k / CONTEXT_FULL_K)
}

export function launchCommand(backend: string, modelId: string): string {
  const head = LAUNCH_HEAD[backend] || backend
  // DeepSeek sets its model through ACP session configuration, not a CLI flag.
  if (backend === 'deepseek_harness') return head
  return modelId ? `${head} --model ${modelId}` : head
}

export function hasTag(m: CliCatalogModel | null | undefined, tag: CliModelTag): boolean {
  return !!m?.tags?.includes(tag)
}

/**
 * i18n keys under `pages.agentStudio.cliModel.strengths` describing what a model
 * suits, derived from its tags so no per-model copy has to be kept up to date.
 */
export function strengthKeys(m: CliCatalogModel): string[] {
  const keys: string[] = []
  if (hasTag(m, 'reasoning')) keys.push('refactor')
  if (hasTag(m, 'vision')) keys.push('screenshots')
  if (hasTag(m, 'fast')) keys.push('quick')
  if ((m.context || 0) >= CONTEXT_FULL_K) keys.push('largeRepo')
  if (!keys.length) keys.push('general')
  return keys
}

/** Higher is better; 0 means the model does not match. */
export function searchScore(m: CliCatalogModel, query: string): number {
  const q = query.trim().toLowerCase()
  if (!q) return 1
  const name = m.name.toLowerCase()
  const id = m.id.toLowerCase()
  if (id === q) return 6
  if (name.startsWith(q)) return 5
  if (id.startsWith(q)) return 4
  if (name.includes(q)) return 3
  if (id.includes(q)) return 2
  if (m.vendor.toLowerCase().includes(q) || vendorLabel(m.vendor).toLowerCase().includes(q)) return 1
  return 0
}

export type Segment = { text: string; hit: boolean }

/** Splits text around the first case-insensitive occurrence of the query. */
export function highlight(text: string, query: string): Segment[] {
  const q = query.trim()
  if (!q) return [{ text, hit: false }]
  const at = text.toLowerCase().indexOf(q.toLowerCase())
  if (at < 0) return [{ text, hit: false }]
  const out: Segment[] = []
  if (at > 0) out.push({ text: text.slice(0, at), hit: false })
  out.push({ text: text.slice(at, at + q.length), hit: true })
  if (at + q.length < text.length) out.push({ text: text.slice(at + q.length), hit: false })
  return out
}

export type VendorChip = { id: string; count: number }
export type ModelGroup = { vendor: string; models: CliCatalogModel[] }

export type CliModelView = {
  /** Vendor chips, in first-seen order; empty when only one vendor is listed. */
  vendors: VendorChip[]
  /** Featured cards; only without a query. */
  recommended: CliCatalogModel[]
  groups: ModelGroup[]
  /** The models in display order, for keyboard navigation. */
  flat: CliCatalogModel[]
  /** True when some listed id equals the query exactly. */
  exact: boolean
}

export function buildView(models: CliCatalogModel[], query: string, vendor: string): CliModelView {
  const order: string[] = []
  const counts = new Map<string, number>()
  for (const m of models) {
    if (!counts.has(m.vendor)) order.push(m.vendor)
    counts.set(m.vendor, (counts.get(m.vendor) || 0) + 1)
  }
  const vendors = order.length > 1 ? order.map((id) => ({ id, count: counts.get(id) || 0 })) : []

  const q = query.trim()
  const inVendor = vendor ? models.filter((m) => m.vendor === vendor) : models
  const scored = inVendor
    .map((m, i) => ({ m, i, s: searchScore(m, q) }))
    .filter((x) => x.s > 0)

  let groups: ModelGroup[]
  if (q) {
    // A query ranks across vendors, so the best hit comes first.
    scored.sort((a, b) => b.s - a.s || a.i - b.i)
    groups = [{ vendor: '', models: scored.map((x) => x.m) }]
  } else {
    groups = order
      .map((v) => ({ vendor: v, models: scored.filter((x) => x.m.vendor === v).map((x) => x.m) }))
      .filter((g) => g.models.length)
  }

  const lower = q.toLowerCase()
  return {
    vendors,
    recommended: q ? [] : inVendor.filter((m) => m.recommended),
    groups,
    flat: groups.flatMap((g) => g.models),
    exact: !!q && models.some((m) => m.id.toLowerCase() === lower),
  }
}

const PALETTE = ['#7B61FF', '#2BB673', '#F2994A', '#2D9CDB', '#EB5757', '#BB6BD9', '#27AE9F', '#E0A526']

/** Stable soft color for a vendor without a logo. */
export function monogramColor(vendor: string): string {
  let h = 0
  for (const ch of vendor) h = (h * 31 + ch.charCodeAt(0)) >>> 0
  return PALETTE[h % PALETTE.length]!
}
