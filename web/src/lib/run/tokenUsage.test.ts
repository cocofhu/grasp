import { describe, expect, it } from 'vitest'
import {
  effectiveModelUsageRows,
  fmtCompactTokenCount,
  fmtCompactTokenRate,
  fmtTokenCount,
  fmtTokenRate,
  mergeTokenUsage,
  normalizeUnknownModelDisplayNameInput,
  summarizeMultiRunUsage,
  summarizeTimelineUsage,
  sumTotalTokens,
  TOKEN_USAGE_UNKNOWN_MODEL,
  TOKEN_USAGE_UNKNOWN_MODEL_DISPLAY,
  tokenUsageTotal,
  totalTokensOrNull,
  shouldShowUnknownVisual,
  unknownDisplayName,
} from './tokenUsage'

describe('tokenUsage', () => {
  it('sums four components', () => {
    expect(
      tokenUsageTotal({
        inputTokens: 10,
        outputTokens: 5,
        cacheReadTokens: 2,
        cacheWriteTokens: 1,
      }),
    ).toBe(18)
  })

  it('formats counts and rates like Demo', () => {
    expect(fmtTokenCount(1234)).toBe('1,234')
    expect(fmtTokenRate(100, 10)).toBe('10.0')
    expect(fmtTokenRate(5, 10)).toBe('0.50')
    expect(fmtTokenRate(100, 0)).toBeNull()
  })

  it('formats workspace totals with Demo K/M compact rules', () => {
    expect(fmtCompactTokenCount(null)).toBe('—')
    expect(fmtCompactTokenCount(undefined)).toBe('—')
    expect(fmtCompactTokenCount(0)).toBe('0')
    expect(fmtCompactTokenCount(42)).toBe('42')
    expect(fmtCompactTokenCount(999)).toBe('999')
    expect(fmtCompactTokenCount(1000)).toBe('1K')
    expect(fmtCompactTokenCount(128400)).toBe('128.4K')
    expect(fmtCompactTokenCount(1_020_000)).toBe('1.02M')
    expect(fmtCompactTokenCount(1_000_000)).toBe('1M')
    expect(fmtCompactTokenCount(9_645_255)).toBe('9.65M')
    expect(fmtCompactTokenCount(2_080_982_825)).toBe('2.08B')
  })

  it('formats compact token/s for KPI main values', () => {
    expect(fmtCompactTokenRate(9_645_255, 3703)).toBe('2.60K/s')
    expect(fmtCompactTokenRate(1360, 120)).toBe('11.3/s')
    expect(fmtCompactTokenRate(5, 10)).toBe('0.50/s')
    expect(fmtCompactTokenRate(100, 0)).toBe('—')
    expect(fmtCompactTokenRate(0, 0)).toBe('—')
  })

  it('summarizes: no usage → —; reported 0 → 0; partial sum', () => {
    expect(summarizeTimelineUsage([{ usage: null }, {}], 60)).toEqual({
      totalTokens: null,
      tokenRate: null,
    })
    expect(
      summarizeTimelineUsage(
        [
          {
            usage: {
              inputTokens: 0,
              outputTokens: 0,
              cacheReadTokens: 0,
              cacheWriteTokens: 0,
            },
          },
        ],
        10,
      ),
    ).toEqual({ totalTokens: 0, tokenRate: '0.00' })
    expect(
      summarizeTimelineUsage(
        [
          { usage: null },
          {
            usage: {
              inputTokens: 100,
              outputTokens: 50,
              cacheReadTokens: 10,
              cacheWriteTokens: 0,
            },
          },
          {
            usage: {
              inputTokens: 20,
              outputTokens: 0,
              cacheReadTokens: 0,
              cacheWriteTokens: 5,
            },
          },
        ],
        35,
      ),
    ).toEqual({ totalTokens: 185, tokenRate: '5.29' })
  })

  it('merges usage components and skips unreported', () => {
    expect(mergeTokenUsage(null, undefined)).toBeNull()
    expect(
      mergeTokenUsage(
        { inputTokens: 10, outputTokens: 0, cacheReadTokens: 0, cacheWriteTokens: 0 },
        null,
        { inputTokens: 5, outputTokens: 3, cacheReadTokens: 1, cacheWriteTokens: 2 },
      ),
    ).toEqual({
      inputTokens: 15,
      outputTokens: 3,
      cacheReadTokens: 1,
      cacheWriteTokens: 2,
    })
    expect(totalTokensOrNull(null)).toBeNull()
    expect(
      totalTokensOrNull({
        inputTokens: 0,
        outputTokens: 0,
        cacheReadTokens: 0,
        cacheWriteTokens: 0,
      }),
    ).toBe(0)
    expect(sumTotalTokens(null, 10, null, 5)).toBe(15)
    expect(sumTotalTokens(null, undefined)).toBeNull()
  })

  it('summarizes multi-run: ignores runs without usage for Σ/avg denom', () => {
    expect(summarizeMultiRunUsage([null, null], 100)).toEqual({
      totalTokens: null,
      usageRunCount: 0,
      avgTokens: null,
      tokenRate: null,
    })
    expect(summarizeMultiRunUsage([100, null, 50], 200)).toEqual({
      totalTokens: 150,
      usageRunCount: 2,
      avgTokens: 75,
      tokenRate: '0.75',
    })
    expect(summarizeMultiRunUsage([0], 0)).toEqual({
      totalTokens: 0,
      usageRunCount: 1,
      avgTokens: 0,
      tokenRate: null,
    })
  })

  it('maps legacy usage to unknown/unbucketed model row', () => {
    const rows = effectiveModelUsageRows(
      { inputTokens: 4, outputTokens: 1, cacheReadTokens: 0, cacheWriteTokens: 0 },
      null,
    )
    expect(rows).toHaveLength(1)
    expect(rows[0]?.modelKey).toBe(TOKEN_USAGE_UNKNOWN_MODEL)
    expect(rows[0]?.total).toBe(5)
    expect(rows[0]?.unknown).toBe(true)
  })

  it('keeps real model keys and filled bridge semantics', () => {
    const rows = effectiveModelUsageRows(
      { inputTokens: 10, outputTokens: 0, cacheReadTokens: 0, cacheWriteTokens: 0 },
      {
        'claude-sonnet-4': {
          inputTokens: 10,
          outputTokens: 0,
          cacheReadTokens: 0,
          cacheWriteTokens: 0,
          filled: true,
          source: 'via ACP_BRIDGE_MODEL',
        },
      },
    )
    expect(rows[0]?.modelKey).toBe('claude-sonnet-4')
    expect(rows[0]?.filled).toBe(true)
  })

  it('unknownDisplayName covers alias / empty / default-name / non-unknown keys', () => {
    expect(unknownDisplayName(TOKEN_USAGE_UNKNOWN_MODEL, 'gpt-5')).toBe('gpt-5')
    expect(unknownDisplayName(TOKEN_USAGE_UNKNOWN_MODEL, '  ')).toBe(TOKEN_USAGE_UNKNOWN_MODEL_DISPLAY)
    expect(unknownDisplayName(TOKEN_USAGE_UNKNOWN_MODEL, TOKEN_USAGE_UNKNOWN_MODEL)).toBe(
      TOKEN_USAGE_UNKNOWN_MODEL_DISPLAY,
    )
    expect(unknownDisplayName(TOKEN_USAGE_UNKNOWN_MODEL, TOKEN_USAGE_UNKNOWN_MODEL_DISPLAY)).toBe(
      TOKEN_USAGE_UNKNOWN_MODEL_DISPLAY,
    )
    expect(unknownDisplayName(TOKEN_USAGE_UNKNOWN_MODEL, null)).toBe(TOKEN_USAGE_UNKNOWN_MODEL_DISPLAY)
    expect(unknownDisplayName('gpt-5', 'alias')).toBe('gpt-5')
  })

  it('shouldShowUnknownVisual: only unknown + default label', () => {
    expect(shouldShowUnknownVisual(true, TOKEN_USAGE_UNKNOWN_MODEL_DISPLAY)).toBe(true)
    expect(shouldShowUnknownVisual(true, TOKEN_USAGE_UNKNOWN_MODEL)).toBe(true)
    expect(shouldShowUnknownVisual(true, ' 未知/未分桶 ')).toBe(true)
    expect(shouldShowUnknownVisual(true, ' 未知模型 ')).toBe(true)
    expect(shouldShowUnknownVisual(true, 'Auto')).toBe(false)
    expect(shouldShowUnknownVisual(true, 'gpt-5')).toBe(false)
    expect(shouldShowUnknownVisual(false, TOKEN_USAGE_UNKNOWN_MODEL)).toBe(false)
    expect(shouldShowUnknownVisual(undefined, 'Auto')).toBe(false)
  })

  it('normalizeUnknownModelDisplayNameInput trims, clears default, rejects >64', () => {
    expect(normalizeUnknownModelDisplayNameInput(' gpt-5 ')).toEqual({ value: 'gpt-5' })
    expect(normalizeUnknownModelDisplayNameInput('   ')).toEqual({ value: '' })
    expect(normalizeUnknownModelDisplayNameInput(TOKEN_USAGE_UNKNOWN_MODEL)).toEqual({ value: '' })
    expect(normalizeUnknownModelDisplayNameInput(TOKEN_USAGE_UNKNOWN_MODEL_DISPLAY)).toEqual({ value: '' })
    const tooLong = 'a'.repeat(65)
    expect(normalizeUnknownModelDisplayNameInput(tooLong).error).toMatch(/64/)
  })
})
