import { describe, expect, it } from 'vitest'
import {
  isSecretEnvKey,
  SECRET_ENV_KEYS,
  stripSecretKeysFromKV,
  stripSecretKeysFromRecord,
} from './secretEnvKeys'

describe('secretEnvKeys', () => {
  it('recognizes credential preset keys and CLI auth keys', () => {
    for (const k of SECRET_ENV_KEYS) expect(isSecretEnvKey(k)).toBe(true)
    expect(isSecretEnvKey(' GITHUB_TOKEN ')).toBe(true)
    expect(isSecretEnvKey('GIT_SSH_KNOWN_HOSTS')).toBe(true)
    expect(isSecretEnvKey('CURSOR_API_KEY')).toBe(true)
  })

  it('excludes repos / urls / region / dropped aliases', () => {
    for (const k of ['GIT_REPOS', 'GITHUB_URL', 'GITLAB_URL', 'GRASP_CODEBUDDY_REGION', 'TRAE_API_KEY']) {
      expect(isSecretEnvKey(k)).toBe(false)
    }
  })

  it('strips only secret keys', () => {
    expect(
      stripSecretKeysFromRecord({
        GRASP_CURSOR_API_KEY: 'x',
        GIT_REPOS: 'a|https://x',
        FEATURE_FLAG: '1',
      }),
    ).toEqual({ GIT_REPOS: 'a|https://x', FEATURE_FLAG: '1' })
    expect(
      stripSecretKeysFromKV([
        { k: 'GITLAB_TOKEN', v: 't' },
        { k: 'GIT_SSH_KNOWN_HOSTS', v: 'h' },
        { k: 'LOG_LEVEL', v: 'info' },
      ]),
    ).toEqual([{ k: 'LOG_LEVEL', v: 'info' }])
  })
})
