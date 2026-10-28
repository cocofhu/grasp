import { describe, expect, it, afterEach } from 'vitest'
import { hideSandboxPath, resetBrandSettingsForTest, setExecutionMode } from './useBrandSettings'

afterEach(() => {
  resetBrandSettingsForTest()
})

describe('hideSandboxPath', () => {
  it('hides sandbox pages only in worktree mode', () => {
    setExecutionMode('sandbox')
    expect(hideSandboxPath('/sandboxes')).toBe(false)
    expect(hideSandboxPath('/sandboxes/1/console')).toBe(false)
    setExecutionMode('worktree')
    expect(hideSandboxPath('/sandboxes')).toBe(true)
    expect(hideSandboxPath('/sandboxes/1/console')).toBe(true)
    expect(hideSandboxPath('/dashboard')).toBe(false)
  })
})
