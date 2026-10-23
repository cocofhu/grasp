import { describe, expect, it } from 'vitest'
import {
  APIKEY_BACKEND,
  APIKEY_BACKENDS,
  CLI_BACKEND_DEFAULT,
  CLI_BACKENDS,
  START_PATH_OPTIONS,
  backendForStartPath,
  startPathForBackend,
  syncStartPathFields,
  type StartPathDraft,
} from './startPath'

describe('startPath shared model (g1.1)', () => {
  it('maps backends and path cards', () => {
    expect(APIKEY_BACKEND).toBe('opencode')
    expect(CLI_BACKEND_DEFAULT).toBe('cursor')
    expect(APIKEY_BACKENDS.map((b) => b.id)).toEqual(['opencode'])
    expect(CLI_BACKENDS.map((b) => b.id)).toEqual([
      'cursor',
      'claude_code',
      'codebuddy',
      'trae',
      'codex',
      'deepseek_harness',
    ])
    expect(startPathForBackend('opencode')).toBe('apiKey')
    expect(startPathForBackend('cursor')).toBe('cli')
    expect(startPathForBackend('codex')).toBe('cli')
    expect(startPathForBackend('deepseek_harness')).toBe('cli')
    expect(backendForStartPath('apiKey', 'trae')).toBe('opencode')
    expect(backendForStartPath('cli', 'trae')).toBe('trae')
    expect(backendForStartPath('cli', 'cursor')).toBe('cursor')
    expect(START_PATH_OPTIONS.map((o) => o.id)).toEqual(['apiKey', 'cli'])
  })

  it('normalizes remembered backends that no longer belong to the selected path', () => {
    expect(backendForStartPath('apiKey', 'cursor', 'deepseek_harness')).toBe('opencode')
    expect(backendForStartPath('cli', 'opencode')).toBe('cursor')
    expect(backendForStartPath('cli', 'deepseek_harness')).toBe('deepseek_harness')
    const draft: StartPathDraft = {
      startPath: 'apiKey',
      acpBackend: 'deepseek_harness',
      cliBackend: 'cursor',
      apiKeyBackend: 'deepseek_harness',
    }
    syncStartPathFields(draft, 'deepseek_harness')
    expect(draft).toMatchObject({ startPath: 'cli', cliBackend: 'deepseek_harness', apiKeyBackend: 'opencode' })
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
