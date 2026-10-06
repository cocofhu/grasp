import { describe, expect, it, vi } from 'vitest'
import type { WFEdge, WFNode } from '@/lib/shared/types'
import { CLARIFY_CAPS, IMPLEMENT_CAPS, TEST_REVIEW_CAPS } from '@/test/capsFixtures'
import { agentHueIndex, agentHueVar, agentInitial, HUE_COUNT } from './agentAvatar'
import { buildDefaultWorkflow, pickTemplateAgents } from './defaultTemplate'
import { edgeGeometry, isBackward } from './edgePath'
import { collectGraphIssues, issuesByNode } from './useGraphIssues'
import { formatKey, resolveShortcut, useCanvasShortcuts } from './useCanvasShortcuts'
import { buildPaletteItems, decodePaletteDrag, encodePaletteDrag, filterPaletteItems, paletteKey } from './paletteItems'

const t = (k: string, n?: Record<string, unknown>) => (n ? `${k}:${JSON.stringify(n)}` : k)
const key = (k: string, mods: Partial<Record<'ctrlKey' | 'metaKey' | 'shiftKey' | 'altKey', boolean>> = {}, target?: EventTarget) => ({
  key: k,
  code: '',
  ctrlKey: false,
  metaKey: false,
  shiftKey: false,
  altKey: false,
  ...mods,
  target: target ?? null,
})

describe('canvas shortcuts', () => {
  it('maps the documented keys', () => {
    expect(resolveShortcut(key('z', { ctrlKey: true }), false)).toBe('undo')
    expect(resolveShortcut(key('Z', { ctrlKey: true, shiftKey: true }), false)).toBe('redo')
    expect(resolveShortcut(key('z', { metaKey: true }), true)).toBe('undo')
    expect(resolveShortcut(key('z', { ctrlKey: true }), true)).toBeNull()
    expect(resolveShortcut(key('c', { ctrlKey: true }), false)).toBe('copy')
    expect(resolveShortcut(key('v', { ctrlKey: true }), false)).toBe('paste')
    expect(resolveShortcut(key('d', { ctrlKey: true }), false)).toBe('duplicate')
    expect(resolveShortcut(key('a', { ctrlKey: true }), false)).toBe('selectAll')
    expect(resolveShortcut(key('s', { ctrlKey: true }), false)).toBe('save')
    expect(resolveShortcut(key('k', { ctrlKey: true }), false)).toBe('quickAdd')
    expect(resolveShortcut(key('/'), false)).toBe('quickAdd')
    expect(resolveShortcut(key('!', { shiftKey: true }), false)).toBe('fitView')
    expect(resolveShortcut(key('L', { shiftKey: true }), false)).toBe('autoLayout')
    expect(resolveShortcut(key('?', { shiftKey: true }), false)).toBe('help')
    expect(resolveShortcut(key('Delete'), false)).toBe('delete')
    expect(resolveShortcut(key('Backspace'), false)).toBe('delete')
    expect(resolveShortcut(key('F2'), false)).toBe('rename')
    expect(resolveShortcut(key('Escape'), false)).toBe('escape')
    expect(resolveShortcut(key('ArrowLeft'), false)).toBe('navLeft')
    expect(resolveShortcut(key('ArrowDown'), false)).toBe('navDown')
    expect(resolveShortcut(key('q'), false)).toBeNull()
  })

  it('lets text fields type, except for save and escape', () => {
    const input = { tagName: 'INPUT', type: 'text', isContentEditable: false } as unknown as EventTarget
    const checkbox = { tagName: 'INPUT', type: 'checkbox', isContentEditable: false } as unknown as EventTarget
    expect(resolveShortcut(key('Backspace', {}, input), false)).toBeNull()
    expect(resolveShortcut(key('z', { ctrlKey: true }, input), false)).toBeNull()
    expect(resolveShortcut(key('s', { ctrlKey: true }, input), false)).toBe('save')
    expect(resolveShortcut(key('Escape', {}, input), false)).toBe('escape')
    expect(resolveShortcut(key('Delete', {}, checkbox), false)).toBe('delete')
  })

  it('prevents the default only when a handler takes the key', () => {
    const undo = vi.fn()
    const escape = vi.fn(() => false as const)
    const s = useCanvasShortcuts({ undo, escape }, () => true)
    const ev = (k: string, ctrlKey = false) => ({ ...key(k, { ctrlKey }), defaultPrevented: false, preventDefault: vi.fn(), stopPropagation: vi.fn() }) as any
    const z = ev('z', true)
    s.onKeydown(z)
    expect(undo).toHaveBeenCalled()
    expect(z.preventDefault).toHaveBeenCalled()
    const esc = ev('Escape')
    s.onKeydown(esc)
    expect(esc.preventDefault).not.toHaveBeenCalled()
    s.stop()
    expect(formatKey('Mod', true)).toBe('⌘')
    expect(formatKey('Mod', false)).toBe('Ctrl')
  })
})

describe('edge paths', () => {
  it('routes forward edges as a smooth step', () => {
    const g = edgeGeometry({ sourceX: 0, sourceY: 0, targetX: 200, targetY: 80 })
    expect(g.back).toBe(false)
    expect(g.path).toMatch(/^M/)
  })

  it('routes back edges below both nodes', () => {
    const e = { sourceX: 600, sourceY: 50, targetX: 200, targetY: 50 }
    expect(isBackward(e)).toBe(true)
    const g = edgeGeometry(e, { y: 0, height: 100 }, { y: 20, height: 140 })
    expect(g.back).toBe(true)
    expect(g.labelY).toBe(200)
    expect(g.labelX).toBe(400)
  })

  it('falls back to a straight line for invalid coordinates', () => {
    expect(edgeGeometry({ sourceX: NaN, sourceY: 0, targetX: 10, targetY: 0 }).back).toBe(false)
  })
})

describe('default template', () => {
  const agents = [
    { name: 'c', capabilities: CLARIFY_CAPS },
    { name: 'i', capabilities: IMPLEMENT_CAPS },
    { name: 'tr', capabilities: TEST_REVIEW_CAPS },
  ]

  it('picks agents by template id first, then by capabilities', () => {
    expect(pickTemplateAgents(agents)).toEqual({ clarify: 'c', implement: 'i', test_review: 'tr' })
    expect(pickTemplateAgents([...agents, { name: 'pinned', templateId: 'implement' }]).implement).toBe('pinned')
    expect(pickTemplateAgents([])).toEqual({ clarify: '', implement: '', test_review: '' })
  })

  it('builds input → clarify → implement → test/review with pass and fail loops', () => {
    const { nodes, edges } = buildDefaultWorkflow(agents, t)
    expect(nodes.map((n) => n.type)).toEqual(['input', 'agent', 'agent', 'agent', 'output'])
    expect(nodes[3]!.config.agent_profile).toBe('tr')
    expect(edges.find((e) => e.sourceHandle === 'pass')).toMatchObject({ source: 'test_review', target: 'output' })
    expect(edges.find((e) => e.sourceHandle === 'fail')).toMatchObject({ source: 'test_review', target: 'implement' })
  })
})

describe('graph issues', () => {
  const node = (id: string, type: string, config: Record<string, unknown> = {}) => ({ id, type, label: id, config }) as WFNode

  it('reports workflow-level and node-level problems', () => {
    const nodes = [node('a', 'agent', { agent_profile: 'ghost', prompt: '' }), node('x', 'research')]
    const issues = collectGraphIssues({ nodes, edges: [] }, [], t)
    const msgs = issues.map((i) => i.message)
    expect(msgs).toContain('canvas.issues.missingInput')
    expect(msgs).toContain('canvas.issues.missingOutput')
    expect(msgs).toContain('canvas.issues.unknownType:{"type":"research"}')
    const byNode = issuesByNode(issues)
    expect(byNode.get('a')).toEqual(
      expect.arrayContaining(['canvas.issues.unreachable', 'canvas.issues.agentNotFound:{"name":"ghost"}', 'canvas.issues.noGoal']),
    )
  })

  it('skips agent checks while agents load and flags undeclared capabilities', () => {
    const edges: WFEdge[] = [{ id: 'e', source: 'in', target: 'a' }]
    const nodes = [node('in', 'input'), node('a', 'agent', { agent_profile: 'bare', prompt: 'go' }), node('out', 'output')]
    const loading = issuesByNode(collectGraphIssues({ nodes, edges }, null, t)).get('a') ?? []
    expect(loading.some((m) => m.includes('bare'))).toBe(false)
    const loaded = issuesByNode(collectGraphIssues({ nodes, edges }, [{ name: 'bare' }], t)).get('a')
    expect(loaded).toContain('canvas.issues.agentNoCaps:{"name":"bare"}')
  })

  it('flags stale outlets, duplicate fan-out and misplaced edges', () => {
    const nodes = [node('in', 'input'), node('in2', 'input'), node('b', 'branch', { cases: [{ id: 'c1', when: 'x' }] }), node('out', 'output')]
    const edges: WFEdge[] = [
      { id: 'e0', source: 'in', target: 'b' },
      { id: 'e1', source: 'b', sourceHandle: 'gone', target: 'out' },
      { id: 'e2', source: 'b', sourceHandle: 'c1', target: 'out' },
      { id: 'e3', source: 'b', sourceHandle: 'c1', target: 'in' },
      { id: 'e4', source: 'out', target: 'b' },
    ]
    const byNode = issuesByNode(collectGraphIssues({ nodes, edges }, [], t))
    expect(byNode.get('in2')).toContain('canvas.issues.tooManyInputs')
    expect(byNode.get('b')).toEqual(expect.arrayContaining(['canvas.issues.staleOutlet:{"handle":"gone"}', 'canvas.issues.duplicateFanOut']))
    expect(byNode.get('in')).toContain('canvas.issues.inputHasIncoming')
    expect(byNode.get('out')).toContain('canvas.issues.outputHasOutgoing')
  })
})

describe('avatars and palette items', () => {
  it('maps names to a stable hue token and initial', () => {
    const i = agentHueIndex('测试评审')
    expect(i).toBeGreaterThanOrEqual(1)
    expect(i).toBeLessThanOrEqual(HUE_COUNT)
    expect(agentHueIndex('测试评审')).toBe(i)
    expect(agentHueVar('测试评审')).toBe(`--c-hue-${i}`)
    expect(agentInitial('reviewer')).toBe('R')
    expect(agentInitial('测试评审')).toBe('测')
  })

  it('builds, filters and round-trips palette items', () => {
    const items = buildPaletteItems([{ name: 'impl', capabilities: IMPLEMENT_CAPS }], (type) => ({ label: type, desc: '' }), t)
    expect(items.map((i) => i.key)).toEqual([
      'agent:impl',
      'type:input',
      'type:output',
      'type:set_var',
      'type:branch',
      'type:human_gate',
    ])
    expect(filterPaletteItems(items, 'IMPL').map((i) => i.key)).toEqual(['agent:impl'])
    expect(filterPaletteItems(items, '  ')).toBe(items)
    expect(decodePaletteDrag(encodePaletteDrag({ type: 'agent', agentProfile: 'impl' }))).toEqual({ type: 'agent', agentProfile: 'impl' })
    expect(decodePaletteDrag(encodePaletteDrag({ type: 'agent' }))).toBeNull()
    expect(items.map((i) => i.key)).toEqual(items.map((i) => paletteKey(i.spec)))
    expect(decodePaletteDrag('not json')).toBeNull()
    expect(decodePaletteDrag('')).toBeNull()
  })
})
