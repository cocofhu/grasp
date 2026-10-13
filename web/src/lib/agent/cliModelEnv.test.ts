import { describe, expect, it } from 'vitest'
import {
  CLI_EFFORT_ENV,
  CLI_MODEL_ENV,
  isManagedCliEnvKey,
  managedCliEnvKeys,
  readCliEffort,
  readCliModel,
  switchCliModelEnv,
  writeCliEffort,
  writeCliModel,
} from './cliModelEnv'

describe('cliModelEnv', () => {
  it('writes the model and drops the key for auto', () => {
    const env = writeCliModel({ A: '1' }, ' gpt-5 ')
    expect(env).toEqual({ A: '1', [CLI_MODEL_ENV]: 'gpt-5' })
    expect(readCliModel(env)).toBe('gpt-5')
    expect(writeCliModel(env, '')).toEqual({ A: '1' })
  })

  it('reads only known effort levels and treats auto as unset', () => {
    expect(readCliEffort({ [CLI_EFFORT_ENV]: 'HIGH' })).toBe('high')
    expect(readCliEffort({ [CLI_EFFORT_ENV]: 'turbo' })).toBe('')
    expect(writeCliEffort({ [CLI_EFFORT_ENV]: 'low' }, 'auto')).toEqual({})
    expect(writeCliEffort({}, 'medium')).toEqual({ [CLI_EFFORT_ENV]: 'medium' })
  })

  it('clears the pin on a backend switch and reports it', () => {
    const env = { A: '1', [CLI_MODEL_ENV]: 'gpt-5', [CLI_EFFORT_ENV]: 'high' }
    expect(switchCliModelEnv(env, 'codex', 'cursor')).toEqual({ env: { A: '1' }, cleared: true })
    expect(switchCliModelEnv({ A: '1' }, 'codex', 'cursor')).toEqual({ env: { A: '1' }, cleared: false })
    expect(switchCliModelEnv(env, 'codex', 'codex').cleared).toBe(false)
  })

  it('manages effort only for codex and nothing for OpenCode', () => {
    expect(managedCliEnvKeys('codex')).toEqual([CLI_MODEL_ENV, CLI_EFFORT_ENV])
    expect(managedCliEnvKeys('cursor')).toEqual([CLI_MODEL_ENV])
    expect(managedCliEnvKeys('opencode')).toEqual([])
    expect(isManagedCliEnvKey('cursor', ` ${CLI_MODEL_ENV} `)).toBe(true)
    expect(isManagedCliEnvKey('cursor', CLI_EFFORT_ENV)).toBe(false)
  })
})
