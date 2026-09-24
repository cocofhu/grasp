import { describe, expect, it } from 'vitest'
import { NODE_DEFS } from './nodeRegistry'
import { productOutputDefs } from '@/lib/run/productNodeArtifacts'

describe('grasp node inspector', () => {
  it('configures agent, timeout, and direct preview switches', () => {
    expect(NODE_DEFS.grasp.fields.map((f) => f.key)).toEqual([
      'agent_profile',
      'timeout',
      'direct_preview',
      'auto_inject',
    ])
    expect(NODE_DEFS.grasp.defaults).toEqual({ timeout: 30, direct_preview: false, auto_inject: true })
    expect(NODE_DEFS.approve.fields.map((f) => f.key)).toEqual(NODE_DEFS.grasp.fields.map((f) => f.key))
    expect(NODE_DEFS.approve.defaults).toEqual(NODE_DEFS.grasp.defaults)
  })

  it('has no leftover inspector knobs', () => {
    for (const key of ['prompt', 'max_rounds', 'auto_var', 'conditional_prompt']) {
      expect(NODE_DEFS.grasp.fields.some((f) => f.key === key)).toBe(false)
      expect(NODE_DEFS.grasp.defaults?.[key]).toBeUndefined()
    }
  })

  it('derives outputs from the nodereg manifest', () => {
    expect(NODE_DEFS.grasp.outputs).toEqual(
      productOutputDefs('grasp', [{ key: 'transcript', desc: 'nodes.grasp.outputs.transcript.desc' }]),
    )
    expect(NODE_DEFS.grasp.outputs.map((o) => o.key)).toEqual([
      'clarified_requirement',
      'clarified_requirement_json',
      'plan',
      'plan_json',
      'research',
      'research_json',
      'root_cause',
      'root_cause_json',
      'proposals',
      'proposals_json',
      'page',
      'transcript',
    ])
  })
})
