import { describe, expect, it } from 'vitest'
import { cloneGitBindings, legacyGitBindings, setDraftGitBindings, validGitScope } from './gitBindings'
import { fromDraft, toDraft } from './agentStudioDraft'
import { assembleCreatePayload, buildReviewSummary, freshDraft } from './agentCreateWizard'

describe('multiple Git credential payloads', () => {
  it('preserves old Agent Git data until a deliberate edit and then removes old authorization slots', () => {
    const original = { name: 'legacy', acpBackend: 'cursor', gitCredentialType: 'ssh' as const, gitCredentialId: 'old-key', sshHostsCredentialId: 'old-hosts', credentialBindings: { GITHUB_TOKEN: 'old-github', GIT_SSH_PRIVATE_KEY: 'old-key', OPENAI_API_KEY: 'ai-key' }, env: { GITLAB_TOKEN: 'legacy-secret', GIT_REPOS: 'app|https://github.com/org/app.git', FEATURE_FLAG: 'on' } }
    const draft = toDraft(original)
    const before = fromDraft(draft)
    expect(before).not.toHaveProperty('gitBindings')
    expect(before.gitCredentialId).toBe('old-key')
    expect(before.credentialBindings).toEqual(original.credentialBindings)
    expect(legacyGitBindings(draft)).toEqual({ items: [{ credentialId: 'old-key', sshHostsCredentialId: 'old-hosts' }, { credentialId: 'old-github' }], preferredCredentialId: 'old-key' })
    const selection = { items: [{ credentialId: 'team-gh', scope: 'github.com' }, { credentialId: 'team-gl' }], preferredCredentialId: 'team-gl' }
    setDraftGitBindings(draft, selection)
    selection.items[0].scope = 'mutated.example'
    const after = fromDraft(draft)
    expect(after.gitBindings).toEqual({ items: [{ credentialId: 'team-gh', scope: 'github.com' }, { credentialId: 'team-gl' }], preferredCredentialId: 'team-gl' })
    expect(after).not.toHaveProperty('gitCredentialId')
    expect(after).not.toHaveProperty('gitCredentialType')
    expect(after).not.toHaveProperty('sshHostsCredentialId')
    expect(after.credentialBindings).toEqual({ OPENAI_API_KEY: 'ai-key' })
    expect(after.env).toEqual({ GIT_REPOS: 'app|https://github.com/org/app.git', FEATURE_FLAG: 'on' })
    expect(original.gitCredentialId).toBe('old-key')
  })
  it('deep-clones new saved bindings and preserves explicit none rather than inheriting legacy keys', () => {
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
  it('uses the legacy selected kind to resolve a generic binding without accidentally choosing another provider', () => {
    expect(legacyGitBindings({ gitCredentialType: 'github_https', credentialBindings: { GITHUB_TOKEN: 'gh', GITLAB_TOKEN: 'gl' } })).toEqual({ items: [{ credentialId: 'gh' }, { credentialId: 'gl' }], preferredCredentialId: 'gh' })
    expect(legacyGitBindings({ gitCredentialType: 'gitlab_https', credentialBindings: { GITLAB_TOKEN: 'gl' } })?.preferredCredentialId).toBe('gl')
    expect(legacyGitBindings({ gitCredentialType: 'ssh', credentialBindings: { GIT_SSH_PRIVATE_KEY: 'ssh', GIT_SSH_KNOWN_HOSTS: 'hosts' } })?.items[0]).toEqual({ credentialId: 'ssh', sshHostsCredentialId: 'hosts' })
    expect(legacyGitBindings({ credentialBindings: { GITHUB_TOKEN: 'gh', GITLAB_TOKEN: 'gl' } })).toEqual({ items: [{ credentialId: 'gh' }, { credentialId: 'gl' }], preferredCredentialId: 'gh' })
    expect(legacyGitBindings({ credentialBindings: { GITHUB_TOKEN: null } })).toEqual({ items: [] })
    expect(legacyGitBindings({ sshHostsCredentialId: 'hosts-only' })).toEqual({ items: [] })
    expect(legacyGitBindings({ credentialBindings: { OPENAI_API_KEY: 'ai' } })).toBeUndefined()
  })
  it('creates with an explicit list, reviews aliases and preference, and never serializes secret material', () => {
    const draft = freshDraft()
    draft.name = 'multi-git'
    expect(assembleCreatePayload(draft).gitBindings).toEqual({ items: [] })
    expect(assembleCreatePayload(draft).repoIds).toEqual([])
    draft.gitCredentialType = 'github_https'
    draft.gitCredentialId = 'old-credential'
    draft.env = [{ k: 'GITHUB_TOKEN', v: 'old-secret' }, { k: 'GIT_SSH_PRIVATE_KEY', v: 'key-content' }]
    draft.gitBindings = { items: [{ credentialId: 'gh-1' }, { credentialId: 'gh-2' }], preferredCredentialId: 'gh-2' }
    const payload = assembleCreatePayload(draft)
    expect(payload.gitBindings).toEqual(draft.gitBindings)
    expect(payload.repoIds).toEqual([])
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
