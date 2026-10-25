// @vitest-environment happy-dom
import { describe, expect, it } from 'vitest'
import { applyAcpBackend, assembleCreatePayload, freshDraft } from './agentCreateWizard'
import { fromDraft, toDraft } from './agentStudioDraft'
import { launchCommand } from './cliModelView'
import { backendKind, kindOfItem } from '@/lib/workspace/credentialKinds'
describe('DeepSeek Harness named model setup', () => {
 it('retains its tool identity, logo and runtime command', () => {
  expect(backendKind('deepseek_harness')?.logoProvider).toBe('deepseek')
  expect(launchCommand('deepseek_harness', 'vendor/model')).toBe('dsh --profile acp')
  expect(kindOfItem({ purpose: 'model_api', provider: 'deepseek_harness' })).toBeUndefined()
 })
 it('creates and round-trips a named model without per-Agent raw key or runtime overrides', () => {
  const d = freshDraft(); d.name = 'dsh-agent'; applyAcpBackend(d, 'deepseek_harness')
  d.modelBinding = { kind: 'configuration', configurationId: 'dsh-team' }
  d.env = [{ k: 'GRASP_DEEPSEEK_API_KEY', v: 'secret' }, { k: 'ACP_BRIDGE_MODEL', v: 'derived' }]
  const p = assembleCreatePayload(d)
  expect(p).toMatchObject({ acpBackend: 'deepseek_harness', modelBinding: { kind: 'configuration', configurationId: 'dsh-team' }, env: {}, layout: { configRoot: '/root/.dsh' } })
  expect(fromDraft(toDraft(p)).modelBinding).toEqual(p.modelBinding)
 })
})
