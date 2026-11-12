import { describe, expect, it } from 'vitest'
import { isAskValueBlank, missingRequiredAskField, useWorkflowAskInputs } from './useWorkflowAskInputs'
import type { Workflow } from '../shared/types'

describe('useWorkflowAskInputs', () => {
  it('maps ask variables with their defaults', () => {
    const wf = {
      id: 'w1',
      name: 'wf',
      nodes: [
        {
          id: 'in',
          type: 'input',
          label: '输入',
          position: { x: 0, y: 0 },
          config: {
            variables: [
              { name: 'title', ask: true, type: 'string', value: 'hi', required: true },
              { name: 'count', ask: true, type: 'number', value: 3 },
              { name: 'hidden', ask: false, type: 'string', value: 'x' },
              null,
            ],
          },
        },
      ],
      edges: [],
    } as unknown as Workflow

    const { fields } = useWorkflowAskInputs(wf)
    expect(fields.value).toHaveLength(2)
    expect(fields.value[0]).toMatchObject({ key: 'title', type: 'text', default: 'hi' })
    expect(fields.value[1]).toMatchObject({ key: 'count', type: 'number', default: '3' })
  })

  it('treats a blank default as a missing required ask field', () => {
    const wf = {
      nodes: [
        {
          id: 'in',
          type: 'input',
          label: 'in',
          position: { x: 0, y: 0 },
          config: {
            variables: [{ name: 'feature', ask: true, required: true, type: 'paragraph', value: '  ' }],
          },
        },
      ],
    } as unknown as Workflow
    expect(missingRequiredAskField(wf)?.key).toBe('feature')
    expect(isAskValueBlank('  ')).toBe(true)
    expect(isAskValueBlank('hi')).toBe(false)
  })
})
