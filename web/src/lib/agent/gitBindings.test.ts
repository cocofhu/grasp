import { describe, expect, it } from 'vitest'
import { cloneGitBindings, setDraftGitBindings, validGitScope, validCredentialGitScope } from './gitBindings'
import { fromDraft, toDraft } from './agentStudioDraft'
import { assembleCreatePayload, buildReviewSummary, freshDraft } from './agentCreateWizard'

describe('multiple Git credential payloads', () => {
  it('deep-clones saved bindings and preserves an explicit empty selection', () => {
    const bindings = { items: [{ credentialId: 'key', scope: 'host', sshHostsCredentialId: 'hosts' }], preferredCredentialId: 'key' }
    const draft = toDraft({ name: 'configured', acpBackend: 'opencode', gitBindings: bindings })
    draft.gitBindings!.items[0].scope = 'another-host'
    expect(bindings.items[0].scope).toBe('host')
    const payload = fromDraft(draft)
    payload.gitBindings!.items[0].scope = 'changed-again'
    expect(draft.gitBindings!.items[0].scope).toBe('another-host')
    setDraftGitBindings(draft, { items: [] })
    expect(fromDraft(draft).gitBindings).toEqual({ items: [] })
    expect(cloneGitBindings({ items: [] })).toEqual({ items: [] })
  })
  it('creates with an explicit list, reviews aliases and preference, and never serializes secret material', () => {
    const draft = freshDraft()
    draft.name = 'multi-git'
    expect(assembleCreatePayload(draft).gitBindings).toEqual({ items: [] })
    draft.env = [{ k: 'GITHUB_TOKEN', v: 'old-secret' }, { k: 'GIT_SSH_PRIVATE_KEY', v: 'key-content' }]
    draft.gitBindings = { items: [{ credentialId: 'gh-1' }, { credentialId: 'gh-2' }], preferredCredentialId: 'gh-2' }
    const payload = assembleCreatePayload(draft)
    expect(payload.gitBindings).toEqual(draft.gitBindings)
    expect(payload).not.toHaveProperty('gitCredentialId')
    expect(payload.env).toEqual({})
    expect(JSON.stringify(payload)).not.toMatch(/old-secret|key-content/)
    const review = buildReviewSummary(draft, { gitItems: { 'gh-1': '团队 GitHub', 'gh-2': '个人 GitHub' }, preferredLabel: '首选' }).find((item) => item.key === 'git')
    expect(review).toMatchObject({ kind: 'ok', detail: '团队 GitHub · 个人 GitHub (首选)' })
  })
})

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
