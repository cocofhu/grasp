import type { ModelTokenUsage, TokenUsage, TokenUsageByModel } from '../shared/types'

/** Persistence / API key for unbucketed usage (matches server). */
export const TOKEN_USAGE_UNKNOWN_MODEL = '未知/未分桶'

/** Default user-visible label when no project alias is configured. */
export const TOKEN_USAGE_UNKNOWN_MODEL_DISPLAY = '未知模型'

/** Max rune/char length for project unknown-model display name (matches server). */
export const UNKNOWN_MODEL_DISPLAY_NAME_MAX_LEN = 64

function isUnsetUnknownAlias(v: string): boolean {
  return !v || v === TOKEN_USAGE_UNKNOWN_MODEL || v === TOKEN_USAGE_UNKNOWN_MODEL_DISPLAY
}

/**
 * Resolve display text for the unknown token bucket.
 * Empty / whitespace / equal to the bucket key or default label → 「未知模型」.
 */
export function unknownDisplayName(modelKey: string, alias?: string | null): string {
  if (modelKey !== TOKEN_USAGE_UNKNOWN_MODEL) return modelKey
  const v = (alias ?? '').trim()
  if (isUnsetUnknownAlias(v)) return TOKEN_USAGE_UNKNOWN_MODEL_DISPLAY
  return v
}

/**
 * Whether to apply unknown-bucket visuals (「未知」badge + unknown-gray).
 * Only when the row is an unknown bucket AND the resolved display name is still
 * the default 「未知/未分桶」. A configured alias (e.g. Auto) keeps Unknown=true
 * for identity but must not show the unknown badge or #71717A gray.
 */
export function shouldShowUnknownVisual(
  unknown: boolean | undefined,
  displayName: string | null | undefined,
): boolean {
  if (!unknown) return false
  const v = (displayName ?? '').trim()
  return v === TOKEN_USAGE_UNKNOWN_MODEL_DISPLAY || v === TOKEN_USAGE_UNKNOWN_MODEL
}

/**
 * Client-side normalize for save: trim; empty/default → ''; over max → error message.
 * Returns { value, error } where error is a user-facing message when invalid.
 */
export function normalizeUnknownModelDisplayNameInput(
  raw: string,
): { value: string; error?: string } {
  const trimmed = raw.trim()
  if (trimmed.length > UNKNOWN_MODEL_DISPLAY_NAME_MAX_LEN) {
    return {
      value: raw,
      error: `显示名最多 ${UNKNOWN_MODEL_DISPLAY_NAME_MAX_LEN} 个字符，请缩短后再保存。`,
    }
  }
  if (!trimmed || trimmed === TOKEN_USAGE_UNKNOWN_MODEL || trimmed === TOKEN_USAGE_UNKNOWN_MODEL_DISPLAY) {
    return { value: '' }
  }
  return { value: trimmed }
}

export const TOKEN_USAGE_SOURCE_BRIDGE = 'via ACP_BRIDGE_MODEL'

/** Sum of the four usage components. */
export function tokenUsageTotal(u: TokenUsage): number {
  return (
    (u.inputTokens || 0) +
    (u.outputTokens || 0) +
    (u.cacheReadTokens || 0) +
    (u.cacheWriteTokens || 0)
  )
}

/** Format token counts for display (en-US grouping). */
export function fmtTokenCount(n: number): string {
  return n.toLocaleString('en-US')
}

/**
 * Compact K/M format (Demo-aligned).
 * null/undefined → "—"; 0 → "0"; under 1000 plain; ≥1000 K (1 decimal); ≥1e6 M (2 decimals).
 * Allowed for project totals, single-run stats KPI main values, and TokenUsageByModelTable
 * row figures (pair with fmtTokenCount in title for exact hover values).
 * Do not use for Run detail timeline / other out-of-scope surfaces (keep fmtTokenCount there).
 */
export function fmtCompactTokenCount(n: number | null | undefined): string {
  if (n === null || n === undefined) return '—'
  const abs = Math.abs(n)
  if (abs >= 1_000_000_000) {
    return `${Math.round((n / 1_000_000_000) * 100) / 100}B`
  }
  if (abs >= 1_000_000) {
    return `${Math.round((n / 1_000_000) * 100) / 100}M`
  }
  if (abs >= 1000) {
    return `${Math.round((n / 1000) * 10) / 10}K`
  }
  return String(n)
}

/**
 * token/s display aligned with Demo: ≥10 → one decimal, else two.
 * Returns null when rate is not computable (caller shows —).
 */
export function fmtTokenRate(totalTokens: number, wallSec: number): string | null {
  if (wallSec <= 0) return null
  const rate = totalTokens / wallSec
  return rate >= 10 ? rate.toFixed(1) : rate.toFixed(2)
}

/**
 * Compact token/s for Run stats KPI main values (Demo-aligned).
 * wallSec ≤ 0 → "—" (no fake rate); ≥1000 → two-decimal K/s (2604.7→2.60K/s);
 * else precise rate + "/s".
 */
export function fmtCompactTokenRate(totalTokens: number, wallSec: number): string {
  if (wallSec <= 0) return '—'
  const rate = totalTokens / wallSec
  if (!Number.isFinite(rate)) return '—'
  if (Math.abs(rate) >= 1_000_000) {
    return `${(rate / 1_000_000).toFixed(2)}M/s`
  }
  if (Math.abs(rate) >= 1000) {
    return `${(rate / 1000).toFixed(2)}K/s`
  }
  const precise = fmtTokenRate(totalTokens, wallSec)
  return precise == null ? '—' : `${precise}/s`
}

export type TimelineUsageSummary = {
  /** null = no reported usage on any item (show — for total & rate). */
  totalTokens: number | null
  /** null = show — (no usage, or invalid wall denominator). */
  tokenRate: string | null
}

/**
 * Aggregate timeline items that have usage (incl. reported 0).
 * Items without usage are skipped; all-reported-zero yields total 0 (not —).
 */
export function summarizeTimelineUsage(
  items: { usage?: TokenUsage | null }[],
  wallSec: number,
): TimelineUsageSummary {
  let total = 0
  let hasAny = false
  for (const it of items) {
    if (it.usage == null) continue
    hasAny = true
    total += tokenUsageTotal(it.usage)
  }
  if (!hasAny) {
    return { totalTokens: null, tokenRate: null }
  }
  return {
    totalTokens: total,
    tokenRate: fmtTokenRate(total, wallSec),
  }
}

export type MultiRunUsageSummary = {
  /** null = no selected run reported usage (show — for Σ / avg / rate). */
  totalTokens: number | null
  /** Runs with at least one process that reported usage. */
  usageRunCount: number
  /** null when usageRunCount is 0; else round(Σ / usageRunCount). */
  avgTokens: number | null
  /** null when no total or wallSumSec ≤ 0. */
  tokenRate: string | null
}

/**
 * Multi-run token KPI: Σ and average only count runs with reported usage;
 * token/s uses the selected wall-clock sum (may include runs without usage).
 */
export function summarizeMultiRunUsage(
  perRunTotals: Array<number | null>,
  wallSumSec: number,
): MultiRunUsageSummary {
  let total = 0
  let usageRunCount = 0
  for (const t of perRunTotals) {
    if (t == null) continue
    usageRunCount += 1
    total += t
  }
  if (usageRunCount === 0) {
    return { totalTokens: null, usageRunCount: 0, avgTokens: null, tokenRate: null }
  }
  return {
    totalTokens: total,
    usageRunCount,
    avgTokens: Math.round(total / usageRunCount),
    tokenRate: fmtTokenRate(total, wallSumSec),
  }
}

/** Merge usage components; null/undefined entries are skipped (unreported ≠ 0). */
export function mergeTokenUsage(
  ...parts: Array<TokenUsage | null | undefined>
): TokenUsage | null {
  let hasAny = false
  const out: TokenUsage = {
    inputTokens: 0,
    outputTokens: 0,
    cacheReadTokens: 0,
    cacheWriteTokens: 0,
  }
  for (const u of parts) {
    if (u == null) continue
    hasAny = true
    out.inputTokens += u.inputTokens || 0
    out.outputTokens += u.outputTokens || 0
    out.cacheReadTokens += u.cacheReadTokens || 0
    out.cacheWriteTokens += u.cacheWriteTokens || 0
  }
  return hasAny ? out : null
}

/** totalTokens for one usage blob; null when unreported. */
export function totalTokensOrNull(usage: TokenUsage | null | undefined): number | null {
  if (usage == null) return null
  return tokenUsageTotal(usage)
}

export type ModelUsageRow = {
  modelKey: string
  inputTokens: number
  outputTokens: number
  cacheReadTokens: number
  cacheWriteTokens: number
  total: number
  source: string
  filled: boolean
  unknown: boolean
}

/**
 * Effective by-model rows for display. When byModel is absent but usage is
 * present, maps to a single「未知/未分桶」bucket (no model guessing).
 */
export function effectiveModelUsageRows(
  usage: TokenUsage | null | undefined,
  byModel: TokenUsageByModel | null | undefined,
): ModelUsageRow[] {
  if (byModel != null) {
    return Object.entries(byModel)
      .map(([modelKey, u]) => toModelRow(modelKey, u))
      .sort((a, b) => b.total - a.total || a.modelKey.localeCompare(b.modelKey))
  }
  if (usage == null) return []
  return [toModelRow(TOKEN_USAGE_UNKNOWN_MODEL, { ...usage, source: 'unknown' })]
}

function toModelRow(modelKey: string, u: ModelTokenUsage): ModelUsageRow {
  const row: TokenUsage = {
    inputTokens: u.inputTokens || 0,
    outputTokens: u.outputTokens || 0,
    cacheReadTokens: u.cacheReadTokens || 0,
    cacheWriteTokens: u.cacheWriteTokens || 0,
  }
  return {
    modelKey,
    ...row,
    total: tokenUsageTotal(row),
    source: u.source || (u.filled ? TOKEN_USAGE_SOURCE_BRIDGE : 'upstream'),
    filled: !!u.filled,
    unknown: modelKey === TOKEN_USAGE_UNKNOWN_MODEL,
  }
}

/** Merge by-model maps (same key = component sum; filled OR). */
export function mergeTokenUsageByModel(
  ...parts: Array<TokenUsageByModel | null | undefined>
): TokenUsageByModel | null {
  let out: TokenUsageByModel | null = null
  for (const part of parts) {
    if (part == null) continue
    if (out == null) out = {}
    for (const [k, u] of Object.entries(part)) {
      const prev = out[k]
      if (!prev) {
        out[k] = { ...u }
        continue
      }
      out[k] = {
        inputTokens: (prev.inputTokens || 0) + (u.inputTokens || 0),
        outputTokens: (prev.outputTokens || 0) + (u.outputTokens || 0),
        cacheReadTokens: (prev.cacheReadTokens || 0) + (u.cacheReadTokens || 0),
        cacheWriteTokens: (prev.cacheWriteTokens || 0) + (u.cacheWriteTokens || 0),
        filled: !!(prev.filled || u.filled),
        source: prev.filled || u.filled ? TOKEN_USAGE_SOURCE_BRIDGE : prev.source || u.source,
      }
    }
  }
  return out
}

/** Sum totalTokens where present; all-null → null (never coerce unreported to 0). */
export function sumTotalTokens(...values: Array<number | null | undefined>): number | null {
  let sum = 0
  let hasAny = false
  for (const v of values) {
    if (v == null) continue
    hasAny = true
    sum += v
  }
  return hasAny ? sum : null
}
