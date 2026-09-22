import { describe, expect, it } from 'vitest'
import { NODE_DEFS } from './nodeRegistry'
import { productOutputDefs } from '@/lib/run/productNodeArtifacts'

describe('grasp node inspector', () => {
  it('configures agent_profile and timeout', () => {
    expect(NODE_DEFS.grasp.fields.map((f) => f.key)).toEqual(['agent_profile', 'timeout'])
    expect(NODE_DEFS.grasp.defaults).toEqual({ timeout: 30 })
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
