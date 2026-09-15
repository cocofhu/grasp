import { describe, expect, it } from 'vitest'
import { isClarifyInteractive, isGrasp } from './clarifyInteractive'

describe('isClarifyInteractive', () => {
  it('covers react, approve, and preflight', () => {
    expect(isClarifyInteractive('react')).toBe(true)
    expect(isClarifyInteractive('approve')).toBe(true)
    expect(isClarifyInteractive('grasp')).toBe(true)
    expect(isGrasp('approve')).toBe(true)
    expect(isGrasp('grasp')).toBe(true)
    expect(isGrasp('react')).toBe(false)
    expect(isClarifyInteractive('preflight')).toBe(true)
    expect(isClarifyInteractive('agent')).toBe(false)
    expect(isClarifyInteractive('plan')).toBe(false)
    expect(isClarifyInteractive('')).toBe(false)
    expect(isClarifyInteractive(undefined)).toBe(false)
  })
})
