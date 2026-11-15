import { describe, expect, it } from 'vitest'
import { agentConfigRelPath } from './backendAuthGuide'

describe('backendAuthGuide', () => {
  it('routes OpenCode custom config to opencode.json', () => {
    expect(agentConfigRelPath('opencode')).toBe('opencode.json')
    expect(agentConfigRelPath('cursor')).toBe('settings.json')
  })
})
