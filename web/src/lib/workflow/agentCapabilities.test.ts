import { describe, expect, it } from 'vitest'
import {
  canPreview,
  declaredProducts,
  isGated,
  isInteractive,
  normalizeCapabilities,
  reviewEnabled,
  summarizeCapabilities,
  validateCapabilities,
} from './agentCapabilities'
import { CLARIFY_CAPS, IMPLEMENT_CAPS, TEST_REVIEW_CAPS } from '@/test/capsFixtures'

describe('agentCapabilities', () => {
  it('derives behaviour flags from the three templates', () => {
    expect(isInteractive(CLARIFY_CAPS)).toBe(true)
    expect(reviewEnabled(CLARIFY_CAPS)).toBe(false)
    expect(reviewEnabled(IMPLEMENT_CAPS)).toBe(true)
    expect(canPreview(IMPLEMENT_CAPS)).toBe(true)
    expect(isGated(IMPLEMENT_CAPS)).toBe(false)
    expect(isGated(TEST_REVIEW_CAPS)).toBe(true)
    expect(isInteractive(null)).toBe(false)
  })

  it('lists declared products in order and skips unknown schemas', () => {
    const products = declaredProducts({ interaction: 'auto', writes: [{ schema: 'review', required: true }, { schema: 'nope' }, { schema: 'plan' }] })
    expect(products.map((p) => [p.name, p.required])).toEqual([
      ['review', true],
      ['plan', false],
    ])
  })

  it('validates in the same order as the server', () => {
    expect(validateCapabilities(null)).toEqual({ code: 'missing' })
    expect(validateCapabilities({ interaction: 'x' as never })).toEqual({ code: 'interaction', value: 'x' })
    expect(validateCapabilities({ interaction: 'auto', tools: ['rm_rf' as never] })).toEqual({ code: 'tool', value: 'rm_rf' })
    expect(validateCapabilities({ interaction: 'clarify' })).toEqual({ code: 'clarifyNeedsAskQuestion' })
    expect(validateCapabilities({ interaction: 'clarify', tools: ['ask_question'], review: true })).toEqual({ code: 'clarifyReview' })
    expect(validateCapabilities({ interaction: 'auto', maxRounds: -1 })).toEqual({ code: 'maxRounds' })
    expect(validateCapabilities({ interaction: 'auto', writes: [{ schema: 'zzz' }] })).toEqual({ code: 'unknownSchema', value: 'zzz' })
    expect(validateCapabilities({ interaction: 'auto', writes: [{ schema: 'plan' }, { schema: 'plan' }] })).toEqual({
      code: 'duplicateSchema',
      value: 'plan',
    })
    for (const caps of [CLARIFY_CAPS, IMPLEMENT_CAPS, TEST_REVIEW_CAPS]) expect(validateCapabilities(caps)).toBeNull()
  })

  it('normalizes to a canonical payload', () => {
    expect(
      normalizeCapabilities({
        interaction: 'clarify',
        review: true,
        tools: ['set_preview', 'ask_question'],
        reads: [' plan.json ', '', '*'],
        writes: [{ schema: 'plan', required: false }, { schema: '' }],
        maxRounds: 2.7,
      }),
    ).toEqual({
      interaction: 'clarify',
      tools: ['ask_question', 'set_preview'],
      reads: ['*'],
      writes: [{ schema: 'plan' }],
      maxRounds: 2,
    })
    expect(normalizeCapabilities({ interaction: 'auto', reads: ['a', 'a'], maxRounds: 0 })).toEqual({ interaction: 'auto', reads: ['a'] })
  })

  it('summarizes for cards and pickers', () => {
    expect(summarizeCapabilities(undefined)).toBeNull()
    const s = summarizeCapabilities(TEST_REVIEW_CAPS)!
    expect(s).toMatchObject({ interaction: 'auto', review: true, gated: true, preview: true, asksHuman: false, readsAll: true, reads: [] })
    expect(s.writes.map((w) => w.name)).toEqual(['test_result', 'review'])
    expect(summarizeCapabilities(CLARIFY_CAPS)!.asksHuman).toBe(true)
  })
})
