import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { getSmoothStepPath, Position } from '@vue-flow/core'
import type { WFEdge, WFNode } from '@/lib/shared/types'
import { CLARIFY_CAPS, IMPLEMENT_CAPS, TEST_REVIEW_CAPS } from '@/test/capsFixtures'
import { agentHueIndex, agentHueVar, agentInitial, HUE_COUNT } from './agentAvatar'
import { buildDefaultWorkflow, pickTemplateAgents } from './defaultTemplate'
import { backEdgeRoute, backLaneIndexes, edgeGeometry, isBackward, type EdgeEnds, type NodeBox } from './edgePath'
import { useFlowElements, type FlowInputs } from './useFlowElements'
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

type Pt = [number, number]

function anchors(d: string): Pt[] {
  const tokens = d.match(/[a-zA-Z]|[-+]?(?:\d*\.\d+|\d+)/g) ?? []
  const pts: Pt[] = []
  let i = 0
  let cmd = ''
  while (i < tokens.length) {
    const tok = tokens[i]!
    if (/[a-zA-Z]/.test(tok)) {
      cmd = tok
      i += 1
      continue
    }
    if (cmd === 'M' || cmd === 'L') {
      pts.push([Number(tok), Number(tokens[i + 1])])
      i += 2
    } else if (cmd === 'Q') {
      pts.push([Number(tokens[i + 2]), Number(tokens[i + 3])])
      i += 4
    } else {
      i += 1
    }
  }
  return pts
}

function verticals(d: string): { x: number }[] {
  const pts = anchors(d)
  const out: { x: number }[] = []
  for (let i = 1; i < pts.length; i++) {
    const a = pts[i - 1]!
    const b = pts[i]!
    if (Math.abs(a[0] - b[0]) < 0.5 && Math.abs(a[1] - b[1]) > 1) out.push({ x: a[0] })
  }
  return out
}

function selfIntersects(pts: Pt[]): boolean {
  const seg = (i: number) => [pts[i]!, pts[i + 1]!] as const
  const between = (v: number, a: number, b: number) => v >= Math.min(a, b) - 1e-6 && v <= Math.max(a, b) + 1e-6
  for (let i = 0; i < pts.length - 1; i++) {
    for (let j = i + 1; j < pts.length - 1; j++) {
      const [a1, a2] = seg(i)
      const [b1, b2] = seg(j)
      const adjacent = j === i + 1
      const ah = a1[1] === a2[1]
      const av = a1[0] === a2[0]
      const bh = b1[1] === b2[1]
      const bv = b1[0] === b2[0]
      if (!ah && !av) return true
      if (!bh && !bv) return true
      if (ah && bh && a1[1] === b1[1]) {
        const lo = Math.max(Math.min(a1[0], a2[0]), Math.min(b1[0], b2[0]))
        const hi = Math.min(Math.max(a1[0], a2[0]), Math.max(b1[0], b2[0]))
        if (hi - lo > (adjacent ? 1e-6 : 0.5)) return true
      } else if (av && bv && a1[0] === b1[0]) {
        const lo = Math.max(Math.min(a1[1], a2[1]), Math.min(b1[1], b2[1]))
        const hi = Math.min(Math.max(a1[1], a2[1]), Math.max(b1[1], b2[1]))
        if (hi - lo > (adjacent ? 1e-6 : 0.5)) return true
      } else if (ah !== bh && av !== bv) {
        const h1 = ah ? a1 : b1
        const h2 = ah ? a2 : b2
        const v1 = av ? a1 : b1
        const v2 = av ? a2 : b2
        const x = v1[0]
        const y = h1[1]
        if (!between(x, h1[0], h2[0]) || !between(y, v1[1], v2[1])) continue
        const atJoint = (p: Pt) => p[0] === x && p[1] === y
        if (adjacent && (atJoint(a2) || atJoint(b1))) continue
        if ((atJoint(a1) || atJoint(a2)) && (atJoint(b1) || atJoint(b2))) continue
        return true
      }
    }
  }
  return false
}

describe('edge paths', () => {
  const testCard: NodeBox = { y: 58, height: 196 }
  const implementCard: NodeBox = { y: 78, height: 156 }
  const failToImplement: EdgeEnds = { sourceX: 932, sourceY: 238, targetX: 468, targetY: 156 }

  it('keeps a smooth step when the target is to the right', () => {
    const e = { sourceX: 0, sourceY: 0, targetX: 200, targetY: 80 }
    expect(isBackward(e)).toBe(false)
    const g = edgeGeometry(e)
    const [path, labelX, labelY] = getSmoothStepPath({
      sourceX: 0,
      sourceY: 0,
      targetX: 200,
      targetY: 80,
      sourcePosition: Position.Right,
      targetPosition: Position.Left,
      borderRadius: 10,
      offset: 24,
    })
    expect(g.back).toBe(false)
    expect(g.path).toBe(path)
    expect(g.labelX).toBe(labelX)
    expect(g.labelY).toBe(labelY)
  })

  it('draws a backward edge as one lane under both cards', () => {
    expect(isBackward(failToImplement)).toBe(true)
    const route = backEdgeRoute(failToImplement, testCard, implementCard)
    const bottom = Math.max(58 + 196, 78 + 156)
    expect(route.points).toEqual([
      [932, 238],
      [944, 238],
      [944, bottom + 40],
      [436, bottom + 40],
      [436, 156],
      [468, 156],
    ])
    expect(selfIntersects(route.points)).toBe(false)
    expect(Math.max(...route.points.map((p) => p[0]))).toBe(944)
    const g = edgeGeometry(failToImplement, testCard, implementCard)
    expect(g.back).toBe(true)
    expect(g.labelY).toBe(route.lane)
    expect(g.labelY).toBeGreaterThan(bottom)
    expect(g.labelX).toBe(690)
    expect(g.path.startsWith('M 932,238')).toBe(true)
    expect(g.path.endsWith('L 468,156')).toBe(true)
  })

  it('keeps the rise off the forward edge verticals', () => {
    const back = backEdgeRoute(
      { sourceX: 700, sourceY: 200, targetX: 400, targetY: 120 },
      { y: 40, height: 180 },
      { y: 40, height: 140 },
    )
    const intoTarget = edgeGeometry({ sourceX: 200, sourceY: 40, targetX: 400, targetY: 120 })
    const onward = edgeGeometry({ sourceX: 700, sourceY: 160, targetX: 820, targetY: 80 })
    expect(intoTarget.back).toBe(false)
    expect(onward.back).toBe(false)
    const riseX = back.points[4]![0]
    const dropX = back.points[2]![0]
    const forwardVerts = [...verticals(intoTarget.path), ...verticals(onward.path)]
    expect(forwardVerts.length).toBeGreaterThan(0)
    for (const vert of forwardVerts) {
      expect(riseX).not.toBeCloseTo(vert.x, 0)
      expect(dropX).not.toBeCloseTo(vert.x, 0)
    }
    expect(riseX).toBeLessThan(400)
    expect(400 - riseX).toBeGreaterThan(12)
    expect(selfIntersects(back.points)).toBe(false)
    expect(back.lane).toBeGreaterThan(40 + 180)
    expect(back.lane).toBeGreaterThan(40 + 140)
  })

  it('stacks backward lanes and staggers their rises', () => {
    const upper = backEdgeRoute(failToImplement, testCard, implementCard, 0)
    const lower = backEdgeRoute(failToImplement, testCard, implementCard, 1)
    expect(lower.lane).toBeGreaterThan(upper.lane)
    expect(lower.points[3]![0]).not.toBe(upper.points[3]![0])
    expect(lower.points[3]![1]).not.toBe(upper.points[3]![1])
    expect(selfIntersects(lower.points)).toBe(false)
    const g0 = edgeGeometry(failToImplement, testCard, implementCard, 0)
    const g1 = edgeGeometry(failToImplement, testCard, implementCard, 1)
    expect(g1.labelY).toBeGreaterThan(g0.labelY)
    expect(g1.labelY).toBeGreaterThan(58 + 196)
  })

  it('still drops when the fail port is already near the card bottom', () => {
    const e = { sourceX: 932, sourceY: 250, targetX: 468, targetY: 156 }
    const route = backEdgeRoute(e, testCard, implementCard)
    expect(route.points[1]![1]).toBe(250)
    expect(route.points[2]![1]).toBeGreaterThan(250)
    expect(route.lane).toBeGreaterThan(58 + 196)
  })

  it('falls back to a straight line for invalid coordinates', () => {
    const g = edgeGeometry({ sourceX: NaN, sourceY: 0, targetX: 10, targetY: 0 })
    expect(g.back).toBe(false)
    expect(g.path).toBe('M 0,0 L 10,0')
    expect(isBackward({ sourceX: 100, sourceY: 0, targetX: 120, targetY: 0 })).toBe(false)
  })

  it('gives overlapping backward edges distinct lanes and leaves forward edges out', () => {
    const nodes = [
      { id: 'impl', x: 468, y: 78 },
      { id: 'test', x: 728, y: 58 },
      { id: 'ship', x: 988, y: 78 },
      { id: 'other', x: 2000, y: 800 },
      { id: 'far', x: 2300, y: 800 },
    ]
    const edges = [
      { id: 'fwd', source: 'impl', target: 'test' },
      { id: 'back-far', source: 'far', target: 'other' },
      { id: 'back-b', source: 'ship', target: 'impl' },
      { id: 'back-a', source: 'test', target: 'impl' },
    ]
    const lanes = backLaneIndexes(nodes, edges)
    expect(lanes.has('fwd')).toBe(false)
    expect(lanes.get('back-a')).toBe(0)
    expect(lanes.get('back-b')).toBe(1)
    expect(lanes.get('back-far')).toBe(0)
    expect(backLaneIndexes(nodes, [...edges].reverse()).get('back-b')).toBe(1)
  })

  it('splits chained back edges when equal-height cards only meet at a corner', () => {
    const y = 40
    const height = 170
    const width = 240
    const cards = [
      { id: 'b', x: 80, y, width },
      { id: 'c', x: 400, y, width },
      { id: 'd', x: 720, y, width },
    ]
    const edges = [
      { id: 'd-c', source: 'd', target: 'c' },
      { id: 'c-b', source: 'c', target: 'b' },
    ]
    const lanes = backLaneIndexes(cards, edges)
    expect(lanes.get('c-b')).not.toBe(lanes.get('d-c'))
    expect(backLaneIndexes(cards, [...edges].reverse()).get('d-c')).toBe(lanes.get('d-c'))

    const box: NodeBox = { y, height }
    const byId = new Map(cards.map((card) => [card.id, card]))
    const routeOf = (id: string, source: string, target: string) => {
      const s = byId.get(source)!
      const t = byId.get(target)!
      return backEdgeRoute(
        { sourceX: s.x + width, sourceY: y + height - 16, targetX: t.x, targetY: y + 48 },
        box,
        box,
        lanes.get(id),
      )
    }
    const left = routeOf('c-b', 'c', 'b')
    const right = routeOf('d-c', 'd', 'c')
    const horiz = (points: [number, number][], lane: number) => {
      const seg = points.find((p, i) => i > 0 && p[1] === lane && points[i - 1]![1] === lane && p[0] !== points[i - 1]![0])
      const i = seg ? points.indexOf(seg) : -1
      const a = points[i - 1]!
      const b = points[i]!
      return [Math.min(a[0], b[0]), Math.max(a[0], b[0])] as const
    }
    const [l0, l1] = horiz(left.points, left.lane)
    const [r0, r1] = horiz(right.points, right.lane)
    const overlapLo = Math.max(l0, r0)
    const overlapHi = Math.min(l1, r1)
    expect(overlapHi - overlapLo).toBeGreaterThan(100)
    expect(left.lane).not.toBe(right.lane)
    expect(left.lane).toBeGreaterThan(y + height)
    expect(right.lane).toBeGreaterThan(y + height)
    const gLeft = edgeGeometry(
      { sourceX: 400 + width, sourceY: y + height - 16, targetX: 80, targetY: y + 48 },
      box,
      box,
      lanes.get('c-b'),
    )
    const gRight = edgeGeometry(
      { sourceX: 720 + width, sourceY: y + height - 16, targetX: 400, targetY: y + 48 },
      box,
      box,
      lanes.get('d-c'),
    )
    expect(gLeft.labelY).toBe(left.lane)
    expect(gRight.labelY).toBe(right.lane)
    expect(gLeft.labelY).not.toBe(gRight.labelY)
  })
})

describe('flow edge lanes', () => {
  it('stores a lane index on backward edges when the canvas assembles them', () => {
    const node = (id: string, x: number, y: number): WFNode => ({ id, type: 'agent', label: id, position: { x, y }, config: {} })
    const nodes = [node('impl', 468, 78), node('test', 728, 58), node('ship', 988, 78)]
    const edges: WFEdge[] = [
      { id: 'fwd', source: 'impl', target: 'test' },
      { id: 'back-a', source: 'test', target: 'impl' },
      { id: 'back-b', source: 'ship', target: 'impl' },
    ]
    const inp: FlowInputs = {
      nodes: () => nodes,
      edges: () => edges,
      mode: () => 'edit',
      agents: () => [],
      lookup: () => ({}),
      statusMap: () => undefined,
      iterations: () => undefined,
      failReasons: () => undefined,
      activePath: () => undefined,
      issues: () => undefined,
      selectedNodes: () => [],
      selectedEdges: () => [],
      renamingId: () => null,
      connecting: ref(null),
      typeText: (type) => ({ label: type, desc: '' }),
      t: (k) => k,
    }
    const edgesOut = useFlowElements(inp).flowEdges.value
    const byId = new Map(edgesOut.map((e) => [e.id, e.data.backLane]))
    expect(byId.get('fwd')).toBeUndefined()
    expect(byId.get('back-a')).toBe(0)
    expect(byId.get('back-b')).toBe(1)
    const order = edgesOut.map((e) => e.id)
    expect(order.indexOf('fwd')).toBeGreaterThan(order.indexOf('back-a'))
    expect(order.indexOf('fwd')).toBeGreaterThan(order.indexOf('back-b'))
    expect(edgesOut.find((e) => e.id === 'fwd')!.zIndex).toBeGreaterThan(edgesOut.find((e) => e.id === 'back-a')!.zIndex)
  })

  it('stores distinct lanes for chained back edges on equal-height cards', () => {
    const node = (id: string, x: number): WFNode => ({ id, type: 'agent', label: id, position: { x, y: 40 }, config: {} })
    const nodes = [node('b', 80), node('c', 400), node('d', 720)]
    const edges: WFEdge[] = [
      { id: 'c-b', source: 'c', target: 'b' },
      { id: 'd-c', source: 'd', target: 'c' },
    ]
    const inp: FlowInputs = {
      nodes: () => nodes,
      edges: () => edges,
      mode: () => 'edit',
      agents: () => [],
      lookup: () => ({}),
      statusMap: () => undefined,
      iterations: () => undefined,
      failReasons: () => undefined,
      activePath: () => undefined,
      issues: () => undefined,
      selectedNodes: () => [],
      selectedEdges: () => [],
      renamingId: () => null,
      connecting: ref(null),
      typeText: (type) => ({ label: type, desc: '' }),
      t: (k) => k,
    }
    const byId = new Map(useFlowElements(inp).flowEdges.value.map((e) => [e.id, e.data.backLane]))
    expect(byId.get('c-b')).not.toBe(byId.get('d-c'))
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
