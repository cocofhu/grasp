import { describe, expect, it } from 'vitest'
import { kindOfItem } from './credentialKinds'
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
