import { describe, expect, it } from 'vitest'
import { CREDENTIAL_KINDS, kindById, kindOfItem, normalizeGitCredentialHost, defaultGitCredentialHost } from './credentialKinds'
describe('explicit credential domains', () => {
 it('classifies platform credentials only by purpose and tool ID', () => {
  expect(kindOfItem({ purpose: 'tool_auth', toolId: 'cursor', provider: 'custom', name: 'anything' })?.id).toBe('cursor')
  expect(kindOfItem({ purpose: 'model_api', provider: 'cursor', name: 'Cursor' })).toBeUndefined()
  expect(kindOfItem({ provider: 'cursor', envKey: 'GRASP_CURSOR_API_KEY' })).toBeUndefined()
 })
 it.each([
  ['opencode', 'GRASP_OPENCODE_API_KEY'],
  ['deepseek_harness', 'GRASP_DEEPSEEK_API_KEY'],
  ['kimi_code', 'GRASP_KIMI_CODE_API_KEY'],
 ])('classifies %s official keys independently of API accounts', (toolId, envKey) => {
  expect(kindById(toolId)).toMatchObject({ id: toolId, provider: toolId, envKey, secret: 'apiKey' })
  expect(kindOfItem({ purpose: 'tool_auth', toolId, secretKind: 'api_key' })?.id).toBe(toolId)
  expect(kindOfItem({ purpose: 'model_api', toolId, provider: toolId })).toBeUndefined()
 })
 it('classifies Git tokens and SSH material without reading display aliases', () => {
  expect(kindOfItem({ purpose: 'git', provider: 'github' })?.id).toBe('github')
  expect(kindOfItem({ purpose: 'ssh', secretKind: 'ssh_private_key', name: 'known hosts' })?.id).toBe('ssh_key')
  expect(kindOfItem({ purpose: 'ssh', secretKind: 'ssh_known_hosts', name: 'private key' })).toBeUndefined()
  expect(CREDENTIAL_KINDS.filter((kind) => kind.type === 'ssh').map((kind) => kind.id)).toEqual(['ssh_key'])
  expect(kindOfItem({ purpose: 'ssh', name: 'known hosts', envKey: 'GIT_SSH_PRIVATE_KEY' })).toBeUndefined()
 })
})

describe('Git credential hosts', () => {
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
