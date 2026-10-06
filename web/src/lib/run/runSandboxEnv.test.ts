import { describe, expect, it } from 'vitest'
import { isDeniedRunSandboxEnvKey, validateRunSandboxEnvRows } from './runSandboxEnv'

describe('runSandboxEnv', () => {
  it('denies reserved and auth keys', () => {
    expect(isDeniedRunSandboxEnvKey('OPENCODE_API_KEY')).toBe(true)
    expect(isDeniedRunSandboxEnvKey('GRASP_OPENCODE_API_KEY')).toBe(true)
    expect(isDeniedRunSandboxEnvKey('GITHUB_TOKEN')).toBe(true)
    expect(isDeniedRunSandboxEnvKey('GIT_SSH_PRIVATE_KEY')).toBe(true)
    expect(isDeniedRunSandboxEnvKey('ROOT_PASSWORD')).toBe(true)
    expect(isDeniedRunSandboxEnvKey('ACP_BRIDGE_PASSWORD')).toBe(true)
    expect(isDeniedRunSandboxEnvKey('GRASP_ARTIFACT_X')).toBe(true)
    expect(isDeniedRunSandboxEnvKey('LOG_LEVEL')).toBe(false)
  })

  it('validates rows: ignore empty, reject missing key / dup / denied', () => {
    const { entries, problems } = validateRunSandboxEnvRows([
      { key: '', value: '' },
      { key: ' LOG ', value: 'debug' },
      { key: '', value: 'x' },
      { key: 'LOG', value: 'trace' },
      { key: 'CURSOR_API_KEY', value: 'k', secret: true },
      { key: 'EMPTY', value: '' },
    ])
    expect(entries).toEqual([
      { key: 'LOG', value: 'debug', secret: false },
      { key: 'EMPTY', value: '', secret: false },
    ])
    expect(problems.some((p) => p.includes('missing key'))).toBe(true)
    expect(problems).toContain('LOG (duplicate)')
    expect(problems).toContain('CURSOR_API_KEY')
  })
})
