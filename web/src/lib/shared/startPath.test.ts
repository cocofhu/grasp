import { describe, expect, it } from 'vitest'
import {
  APIKEY_BACKEND,
  CLI_BACKEND_DEFAULT,
  CLI_BACKENDS,
  START_PATH_OPTIONS,
  backendForStartPath,
  startPathForBackend,
  syncStartPathFields,
} from './startPath'

describe('startPath shared model (g1.1)', () => {
  it('maps backends and path cards', () => {
    expect(APIKEY_BACKEND).toBe('opencode')
    expect(CLI_BACKEND_DEFAULT).toBe('cursor')
    expect(CLI_BACKENDS.map((b) => b.id)).toEqual([
      'cursor',
      'claude_code',
      'codebuddy',
      'trae',
      'codex',
    ])
    expect(startPathForBackend('opencode')).toBe('apiKey')
    expect(startPathForBackend('cursor')).toBe('cli')
    expect(backendForStartPath('apiKey', 'trae')).toBe('opencode')
    expect(backendForStartPath('cli', 'trae')).toBe('trae')
    expect(backendForStartPath('cli', 'cursor')).toBe('cursor')
    expect(START_PATH_OPTIONS.map((o) => o.id)).toEqual(['apiKey', 'cli'])
  })

  it('syncStartPathFields remembers CLI picks', () => {
    const draft = {
      startPath: 'apiKey' as const,
      acpBackend: APIKEY_BACKEND,
      cliBackend: CLI_BACKEND_DEFAULT,
    }
    syncStartPathFields(draft, 'claude_code')
    expect(draft.startPath).toBe('cli')
    expect(draft.cliBackend).toBe('claude_code')
    syncStartPathFields(draft, 'opencode')
    expect(draft.startPath).toBe('apiKey')
    expect(draft.cliBackend).toBe('claude_code')
  })
})
