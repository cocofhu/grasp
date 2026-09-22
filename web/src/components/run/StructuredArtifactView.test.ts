import { describe, expect, it } from 'vitest'
import { isFeedbackArtifactName, isStructuredArtifactName } from './StructuredArtifactView.vue'

describe('isStructuredArtifactName', () => {
  it('matches the reserved structured JSON artifact names', () => {
    const names = [
      'clarified_requirement.json',
      'research.json',
      'root_cause.json',
      'proposals.json',
      'proposal.json',
      'plan.json',
      'implementation_result.json',
      'test_result.json',
      'review.json',
      'preflight.json',
    ]
    for (const name of names) {
      expect(isStructuredArtifactName(name)).toBe(true)
    }
  })

  it('does not match markdown, html, or other artifact names', () => {
    expect(isStructuredArtifactName('clarified_requirement.md')).toBe(false)
    expect(isStructuredArtifactName('page.html')).toBe(false)
    expect(isStructuredArtifactName('design.md')).toBe(false)
    expect(isStructuredArtifactName('result.json')).toBe(false)
  })

  // Round names are generated per node and iteration, so the ledger can only be
  // recognized by prefix — an exact-name whitelist would drop every round into
  // the raw JSON fallback.
  it('recognizes the ledger by prefix, not by an exact name', () => {
    const names = [
      'feedback_index.json',
      'feedback.review.research-1.i2r3.json',
      'feedback.gate.approve.i1r1.json',
    ]
    for (const name of names) {
      expect(isFeedbackArtifactName(name)).toBe(true)
      expect(isStructuredArtifactName(name)).toBe(true)
    }
    expect(isFeedbackArtifactName('feedbackish.json')).toBe(false)
    expect(isFeedbackArtifactName('research.json')).toBe(false)
  })
})
