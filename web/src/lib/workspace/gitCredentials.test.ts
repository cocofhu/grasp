import { describe, expect, it } from 'vitest'
import { credentialFitsRepo, validGitScope, validCredentialGitScope } from './gitCredentials'

describe('Git credential scope', () => {
  it('requires repository URLs to use the selected credential transport', () => {
    expect(validGitScope('ssh://git@host/org/repo.git', 'github')).toBe(false)
    expect(validGitScope('git@host:org/repo.git', 'gitlab')).toBe(false)
    expect(validGitScope('https://host/org/repo.git', 'ssh_key')).toBe(false)
    expect(validGitScope('host', 'ssh_key')).toBe(true)
    expect(validGitScope('https://host/', 'github')).toBe(true)
  })
  it.each(['', 'github.com', 'git.example.test:8443', '[::1]:8443', 'https://git.example.test/', 'https://git.example.test', 'https://github.com/org/repo.git', 'http://git.example.test/org/repo', 'ssh://git@git.example.test/org/repo.git', 'git@git.example.test:org/repo.git'])('accepts an optional host or repository: %s', (scope) => { expect(validGitScope(scope)).toBe(true) })
  it.each(['bad scope', 'https://git.example.test/a/../b', 'https://git.example.test/a/%2e%2e/b', 'https://git.example.test/a//b', 'bad_host', '-bad.example', 'bad..example', 'host:99999', '[not-ipv6]', 'host:0', 'host/repo', 'https://user:secret@git.example.test/repo', 'https://token@git.example.test/repo', 'https://', 'file:///tmp/repo', 'https://git.example.test/repo?token=secret', 'https://git.example.test/repo#branch', '\nhost\nother'])('rejects unsafe or malformed scope: %s', (scope) => { expect(validGitScope(scope)).toBe(false) })
})


describe('Git scope stays within the credential Host', () => {
  const credential = { id: 'gl', type: 'git', provider: 'gitlab', purpose: 'git' as const, host: 'http://git.example.test:8080', name: 'Self-hosted', configured: true }
  it.each(['', 'git.example.test:8080', 'http://git.example.test:8080/team/repo.git'])('accepts a narrowed scope: %s', scope => {
    expect(validCredentialGitScope(scope, credential)).toBe(true)
  })
  it.each(['other.example.test:8080', 'git.example.test:8443', 'https://git.example.test:8080/team/repo.git', 'http://git.example.test:8081/team/repo.git'])('rejects another origin: %s', scope => {
    expect(validCredentialGitScope(scope, credential)).toBe(false)
  })
  it('defaults to the provider host and leaves SSH scope rules intact', () => {
    expect(validCredentialGitScope('https://gitlab.com/team/repo', { ...credential, host: undefined })).toBe(true)
    expect(validCredentialGitScope('git@git.internal:team/repo.git', { ...credential, provider: 'ssh', purpose: 'ssh', secretKind: 'ssh_private_key' })).toBe(true)
  })
})

describe('repository credential fit', () => {
  const github = { id: 'gh', type: 'git', provider: 'github', purpose: 'git' as const, name: 'GitHub', configured: true }
  const ssh = { id: 'ssh', type: 'ssh', provider: 'ssh', purpose: 'ssh' as const, secretKind: 'ssh_private_key', name: 'Key', configured: true }
  it('matches transport and host', () => {
    expect(credentialFitsRepo('https://github.com/team/web.git', github)).toBe(true)
    expect(credentialFitsRepo('https://gitlab.com/team/web.git', github)).toBe(false)
    expect(credentialFitsRepo('git@github.com:team/web.git', github)).toBe(false)
    expect(credentialFitsRepo('git@github.com:team/web.git', ssh)).toBe(true)
    expect(credentialFitsRepo('https://github.com/team/web.git', ssh)).toBe(false)
    expect(credentialFitsRepo('', github)).toBe(false)
    expect(credentialFitsRepo('https://github.com/team/web.git', undefined)).toBe(false)
  })
})
