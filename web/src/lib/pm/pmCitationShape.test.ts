import { describe, expect, it } from 'vitest'
import {
  isBareExtractSnippet,
  isClearlyInvalidRunRouteId,
  isValidPmCitationShape,
  shortRunId,
} from './pmCitationShape'

describe('pmCitationShape', () => {
  it('accepts legal run/workflow/artifact/gate/plan shapes', () => {
    expect(isValidPmCitationShape('run', 'run-a1b2c3d4')).toBe(true)
    expect(isValidPmCitationShape('workflow', 'wf-deadbeef')).toBe(true)
    expect(isValidPmCitationShape('artifact', 'art-11223344')).toBe(true)
    expect(isValidPmCitationShape('artifact', 'research.json')).toBe(true)
    expect(isValidPmCitationShape('gate', 'run-a1b2c3d4:human_gate')).toBe(true)
    expect(isValidPmCitationShape('plan', 'g1.2')).toBe(true)
    expect(isValidPmCitationShape('plan', 'run-a1b2c3d4:g1')).toBe(true)
  })

  it('rejects false-positive targets like trigger/the/npm', () => {
    expect(isValidPmCitationShape('run', 'trigger')).toBe(false)
    expect(isValidPmCitationShape('run', 'the')).toBe(false)
    expect(isValidPmCitationShape('run', 'npm')).toBe(false)
    expect(isValidPmCitationShape('artifact', 'npm')).toBe(false)
    expect(isClearlyInvalidRunRouteId('trigger')).toBe(true)
    expect(isClearlyInvalidRunRouteId('run-a1b2c3d4')).toBe(false)
  })

  it('formats short run id and detects bare extract snippets', () => {
    expect(shortRunId('run-a1b2c3d4')).toBe('a1b2c3d4')
    expect(isBareExtractSnippet('run', 'run:trigger', 'trigger')).toBe(true)
    expect(isBareExtractSnippet('run', '需求澄清 · 进行中', 'run-a1b2c3d4')).toBe(false)
  })
})
