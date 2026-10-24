import { describe, expect, it } from 'vitest'
import { assembleCreatePayload, applyAcpBackend, buildDefaultRule, buildReviewSummary, configRootFor, freshDraft, hasPathDeps, envConfiguredCount, validateBasics, WIZARD_STEPS, ACP_BACKENDS } from './agentCreateWizard'
describe('Agent creation data contract', () => {
 it('requires one combined tool and named-model step before optional Git', () => {
  expect(WIZARD_STEPS.map(step => [step.id, step.skip])).toEqual([['basics', false], ['acp', false], ['git', true], ['review', false]])
  expect(freshDraft()).toMatchObject({ acpBackend: 'cursor', modelBinding: { kind: 'configuration', configurationId: '' }, gitBindings: { items: [] } })
 })
 it.each(ACP_BACKENDS)('changes to $id with a matching layout and an empty explicit model selection', backend => {
  const d = freshDraft(); d.modelBinding = { kind: 'configuration', configurationId: 'cfg' }
  if (backend.id !== 'cursor') applyAcpBackend(d, backend.id)
  expect(d.configRoot).toBe(configRootFor(backend.id))
  expect(d.modelBinding?.configurationId).toBe(backend.id === 'cursor' ? 'cfg' : '')
 })
 it('does not clear a selection on reselection and detects path-dependent edits', () => {
  const d = freshDraft(); d.modelBinding = { kind: 'configuration', configurationId: 'cfg' }
  applyAcpBackend(d, 'cursor'); expect(d.modelBinding?.configurationId).toBe('cfg')
  expect(hasPathDeps(d)).toBe(false); d.rulesEdited = true; expect(hasPathDeps(d)).toBe(true)
 })
 it('validates names and compares normalized duplicates', () => {
  const d = freshDraft(); expect(validateBasics(d, [])).toBe('required')
  d.name = 'bad name'; expect(validateBasics(d, [])).toBe('invalid')
  d.name = 'team'; expect(validateBasics(d, ['team'])).toBe('exists')
  expect(validateBasics(d, [])).toBe('')
 })
 it('writes identity, skills and commands while retaining their contents', () => {
  const d = freshDraft(); d.name = 'reviewer'; d.description = 'Review PRs'
  d.skills = [{ name: 'git', content: '# skill' }, { name: '', content: '' }]
  d.commands = [{ name: 'ship', content: '' }]
  const p = assembleCreatePayload(d)
  expect(p.files?.map(f => f.path)).toEqual(['rules/reviewer.md', 'skills/git/SKILL.md', 'commands/ship.md'])
  expect(p.files?.[0].content).toContain('Review PRs')
  expect(buildDefaultRule('reviewer')).toContain('alwaysApply: true')
  d.rulesEdited = true; d.rulesContent = '# custom'; expect(assembleCreatePayload(d).files?.[0].content).toBe('# custom')
 })
 it('uses role packs without synthesizing identity files or capabilities', () => {
  const d = freshDraft(); d.name = 'planner'; d.templateId = '需求澄清工程师'
  const p = assembleCreatePayload(d); expect(p.templateId).toBe('需求澄清工程师'); expect(p.files).toEqual([]); expect(p).not.toHaveProperty('capabilities')
 })
 it('serializes only a named model and explicit multi-Git references, excluding derived model and auth env', () => {
  const d = freshDraft(); d.name = 'configured'; d.modelBinding = { kind: 'configuration', configurationId: 'cfg' }
  d.gitBindings = { items: [{ credentialId: 'gh' }, { credentialId: 'ssh', sshHostsCredentialId: 'hosts' }], preferredCredentialId: 'ssh' }
  d.env = [{ k: 'GITHUB_TOKEN', v: 'secret' }, { k: 'OPENAI_API_KEY', v: 'secret' }, { k: 'ACP_BRIDGE_MODEL', v: 'derived' }, { k: 'FEATURE', v: 'on' }]
  const p = assembleCreatePayload(d)
  expect(p.modelBinding).toEqual(d.modelBinding); expect(p.gitBindings).toEqual(d.gitBindings); expect(p.gitBindings).not.toBe(d.gitBindings)
  expect(p.env).toEqual({ FEATURE: 'on' }); expect(JSON.stringify(p)).not.toContain('secret')
 })
 it('defaults missing Git bindings to an explicit empty list', () => {
  const d = freshDraft(); delete d.gitBindings; expect(assembleCreatePayload(d).gitBindings).toEqual({ items: [] })
 })
 it('maps both MCP transports and ignores unnamed records', () => {
  const d = freshDraft(); d.mcp = [{ name: 'stdio', transport: 'command', url: '', headers: [], command: 'npx', args: '-y\npkg', env: [{ k: 'CUSTOM', v: 'yes' }] }, { name: 'http', transport: 'url', url: 'https://mcp.example', headers: [{ k: 'X-Tag', v: 'team' }], command: '', args: '', env: [] }]
  expect(assembleCreatePayload(d).mcp).toEqual([{ name: 'stdio', command: 'npx', args: ['-y', 'pkg'], env: { CUSTOM: 'yes' } }, { name: 'http', url: 'https://mcp.example', headers: { 'X-Tag': 'team' } }])
 })
 it('normalizes only the selected tool region and reviews configured aliases', () => {
  const d = freshDraft(); applyAcpBackend(d, 'codebuddy'); d.modelBinding = { kind: 'configuration', configurationId: 'cfg' }
  d.gitBindings = { items: [{ credentialId: 'gh' }], preferredCredentialId: 'gh' }
  const summary = buildReviewSummary(d, { model: 'Team model', gitItems: { gh: 'GitHub team' }, preferredLabel: 'Preferred' })
  expect(summary.find(row => row.key === 'apiKey')?.detail).toBe('Team model')
  expect(summary.find(row => row.key === 'git')?.detail).toBe('GitHub team (Preferred)')
  d.env.push({ k: 'FEATURE', v: 'on' }, { k: '', v: '' })
  expect(envConfiguredCount(d)).toBe(1)
 })
})
