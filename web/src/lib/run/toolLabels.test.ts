import { describe, expect, it } from 'vitest'
import { formatToolDuration, graspToolName, toolMeta } from './toolLabels'

describe('graspToolName', () => {
  it('strips agent prefixes but not partial words', () => {
    expect(graspToolName('set_plan')).toBe('set_plan')
    expect(graspToolName('mcp__grasp__set_plan')).toBe('set_plan')
    expect(graspToolName('artifact-store.write_artifact')).toBe('write_artifact')
    expect(graspToolName('grasp: page_click')).toBe('page_click')
    expect(graspToolName('reset_plan')).toBe('')
    expect(graspToolName('Shell')).toBe('')
  })

  it('prefers the longest Grasp name', () => {
    expect(graspToolName('mcp__x__set_artifact_preview')).toBe('set_artifact_preview')
    expect(graspToolName('mcp__x__set_preview')).toBe('set_preview')
  })
})

describe('toolMeta', () => {
  it('maps structured tools to their artifact and named ones to the summary', () => {
    expect(toolMeta({ title: 'set_clarified_requirement' })).toEqual({ labelKey: 'grasp.set_clarified_requirement', kind: 'artifact', artifact: 'clarified_requirement.json' })
    expect(toolMeta({ title: 'write_artifact', summary: 'a.md' }).artifact).toBe('a.md')
    expect(toolMeta({ title: 'write_artifact' }).artifact).toBeUndefined()
    expect(toolMeta({ title: 'set_preview' })).toEqual({ labelKey: 'grasp.set_preview', kind: 'preview', preview: true })
    expect(toolMeta({ title: 'get_research' }).kind).toBe('read')
  })

  it('recognises agent built-ins across spellings', () => {
    expect(toolMeta({ title: 'Bash' })).toEqual({ labelKey: 'shell', kind: 'shell' })
    expect(toolMeta({ title: 'read_file' }).labelKey).toBe('read')
    expect(toolMeta({ title: 'Get mcp tools' }).labelKey).toBe('mcpTools')
    expect(toolMeta({ title: 'StrReplace' }).kind).toBe('edit')
  })

  it('leaves unknown tools on their raw name', () => {
    expect(toolMeta({ title: 'mcp__other__thing' })).toEqual({ labelKey: '', kind: 'mcp' })
    expect(toolMeta({ title: 'whatever' })).toEqual({ labelKey: '', kind: 'other' })
  })
})

describe('formatToolDuration', () => {
  it.each([
    [undefined, ''],
    [999, ''],
    [1000, '1.0s'],
    [1550, '1.5s'],
    [42_400, '42s'],
    [60_000, '1m'],
    [125_000, '2m 5s'],
    [3_600_000, '1h'],
    [3_900_000, '1h 5m'],
  ])('%s ms -> %s', (ms, want) => {
    expect(formatToolDuration(ms)).toBe(want)
  })
})
