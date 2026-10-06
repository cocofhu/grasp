import { describe, expect, it } from 'vitest'
import {
  listPrimaryProducts,
  pickProductRef,
  resolveUpstreamOutputs,
  reviewingUpstreamN,
} from './gateUpstream'

describe('pickProductRef', () => {
  it('prefers page over an earlier structured ref', () => {
    const tmpl =
      'see {{nodes.research.outputs.research}} then {{nodes.visual.outputs.page}}'
    expect(pickProductRef(tmpl)).toEqual({ nodeId: 'visual', key: 'page' })
  })

  it('falls back to the first structured ref when no page', () => {
    expect(pickProductRef('{{nodes.plan.outputs.plan}}')).toEqual({
      nodeId: 'plan',
      key: 'plan',
    })
  })

  it('returns null when template has no node output refs', () => {
    expect(pickProductRef('plain markdown')).toBeNull()
  })
})

describe('listPrimaryProducts', () => {
  it('collects outputs refs and artifact() refs', () => {
    const tmpl = `{{nodes.react.outputs.clarified_requirement}}
{{artifact("notes.md")}}
{{nodes.visual.outputs.page}}`
    const got = listPrimaryProducts(tmpl)
    expect(got.map((p) => p.name)).toEqual([
      'clarified_requirement.json',
      'page.html',
      'notes.md',
    ])
  })

  it('marks image artifacts as readonly', () => {
    const got = listPrimaryProducts('{{artifact("shot.png")}} {{nodes.visual.outputs.page}}')
    expect(got).toEqual([
      { name: 'page.html', nodeId: 'visual', outputKey: 'page', kind: 'html', readonly: false },
      { name: 'shot.png', outputKey: undefined, kind: 'image', readonly: true },
    ])
  })
})

describe('listExcludedProduces', () => {
  it('lists produces-only names not in the template whitelist', async () => {
    const { listExcludedProduces } = await import('./gateUpstream')
    const excluded = listExcludedProduces(
      '{{nodes.test.outputs.test_result}} {{artifact("shot.png")}}',
      [
        {
          id: 'test',
          config: { produces: 'test_result.json,shot.png,extra.md' },
        },
      ],
    )
    expect(excluded).toEqual(['extra.md'])
  })

  it('includes produces-only from nodes not referenced by primary nodeId', async () => {
    const { listExcludedProduces } = await import('./gateUpstream')
    // Template mixes outputs + artifact(); visual has page.html but research
    // produces-only node_complete.json must still appear in the excluded hint.
    const excluded = listExcludedProduces(
      '{{nodes.visual.outputs.page}} {{artifact("shot.png")}}',
      [
        {
          id: 'visual',
          config: { produces: 'page.html,shot.png' },
        },
        {
          id: 'research',
          config: { produces: 'research.json,node_complete.json' },
        },
      ],
    )
    expect(excluded).toEqual(['research.json', 'node_complete.json'])
  })
})

describe('resolveUpstreamOutputs', () => {
  const execsByNode = {
    visual: [
      { iteration: 1, status: 'completed', outputs: { page: '<html>v1</html>' } },
      { iteration: 2, status: 'failed', outputs: { page: '<html>fail</html>' } },
      { iteration: 3, status: 'completed', outputs: { page: '<html>v3</html>' } },
    ],
  }

  it('binds visual iter=3 when gate iter=2 and pointer points at 3', () => {
    const r = resolveUpstreamOutputs({
      execsByNode,
      upstreamNodeId: 'visual',
      upstreamIteration: 3,
    })
    expect(r.usedPointer).toBe(true)
    expect(r.pointerMiss).toBe(false)
    expect(r.selectedIteration).toBe(3)
    expect(r.outputs?.page).toBe('<html>v3</html>')
  })

  it('marks pointerMiss without falling back to equals heuristic', () => {
    const r = resolveUpstreamOutputs({
      execsByNode,
      upstreamNodeId: 'visual',
      upstreamIteration: 9,
    })
    expect(r.usedPointer).toBe(true)
    expect(r.pointerMiss).toBe(true)
    expect(r.outputs).toBeNull()
    expect(r.selectedIteration).toBe(9)
    // Banner N stays on pointer; UI must show a secondary fallback hint when
    // loadProduct then reads the run artifact store.
    expect(reviewingUpstreamN({
      upstreamIteration: 9,
      selectedIteration: r.selectedIteration,
    })).toBe(9)
  })

  it('has no upstream snapshot without a pointer', () => {
    expect(resolveUpstreamOutputs({ execsByNode })).toEqual({
      outputs: null,
      selectedIteration: null,
      usedPointer: false,
      pointerMiss: false,
    })
  })
})

describe('reviewingUpstreamN', () => {
  it('uses pointer iteration when present', () => {
    expect(
      reviewingUpstreamN({ upstreamIteration: 3, selectedIteration: 1 }),
    ).toBe(3)
  })

  it('is null without a pointer', () => {
    expect(reviewingUpstreamN({ selectedIteration: null })).toBeNull()
  })
})
