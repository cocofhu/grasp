import { describe, expect, it } from 'vitest'
import { kindOfItem, displayKindOfItem, normalizeGitCredentialHost, defaultGitCredentialHost } from './credentialKinds'
describe('explicit credential domains', () => {
 it('classifies platform credentials only by purpose and tool ID', () => {
  expect(kindOfItem({ purpose: 'tool_auth', toolId: 'cursor', provider: 'custom', name: 'anything' })?.id).toBe('cursor')
  expect(kindOfItem({ purpose: 'model_api', provider: 'cursor', name: 'Cursor' })).toBeUndefined()
  expect(kindOfItem({ provider: 'cursor', envKey: 'GRASP_CURSOR_API_KEY' })).toBeUndefined()
 })
 it('classifies Git tokens and SSH material without reading display aliases', () => {
  expect(kindOfItem({ purpose: 'git', provider: 'github' })?.id).toBe('github')
  expect(kindOfItem({ purpose: 'ssh', secretKind: 'ssh_private_key', name: 'known hosts' })?.id).toBe('ssh_key')
  expect(kindOfItem({ purpose: 'ssh', secretKind: 'ssh_known_hosts', name: 'private key' })?.id).toBe('ssh_hosts')
  expect(kindOfItem({ purpose: 'ssh', name: 'known hosts', envKey: 'GIT_SSH_PRIVATE_KEY' })).toBeUndefined()
 })
})

describe('credential list presentation and Git hosts', () => {
 it('classifies old provider rows for display without authorizing them', () => {
  for (const [type, provider] of [['ai', 'cursor'], ['git', 'github'], ['git', 'gitlab']]) {
   const row = { type, provider }
   expect(displayKindOfItem(row)?.id).toBe(provider)
   expect(kindOfItem(row)).toBeUndefined()
  }
  expect(displayKindOfItem({ type: 'ai', provider: 'cursor', purpose: 'custom' })).toBeUndefined()
 })
 it.each([
  ['gitlab.com', 'https://gitlab.com'],
  ['github.com', 'https://github.com'],
  ['https://git.example.test/', 'https://git.example.test'],
  ['http://git.example.test:8080', 'http://git.example.test:8080'],
  ['git.example.test:8443', 'https://git.example.test:8443'],
 ])('accepts a single hosting origin: %s', (raw, normalized) => {
  expect(normalizeGitCredentialHost(raw)).toBe(normalized)
 })
 it.each(['', 'ftp://git.example.test', 'https://user:secret@git.example.test', 'https://git.example.test/group', 'https://git.example.test/?token=secret', 'https://git.example.test/#branch', 'bad host', 'git.example.test:0', 'git.example.test:99999', 'bad_host', '//git.example.test', 'https:///git.example.test', 'https://git.example.test/.', 'https://git.example.test?'])('rejects non-host input: %s', raw => {
  expect(normalizeGitCredentialHost(raw)).toBeNull()
 })
 it('supplies provider defaults without creating URL credentials', () => {
  expect(defaultGitCredentialHost('gitlab')).toBe('gitlab.com')
  expect(defaultGitCredentialHost('github')).toBe('github.com')
  expect(defaultGitCredentialHost('ssh')).toBe('')
 })
})
