import { describe, expect, it } from 'vitest'
import { addClarifyAnnotation, useClarifyDraft } from './useClarifyDraft'

describe('useClarifyDraft', () => {
  it('stores draft and attachments per run/node and ignores empty keys', () => {
    const { draft, attachments } = useClarifyDraft('run-1', () => 'node-1')
    draft.value = 'hello'
    attachments.value = [{ data: 'abc', mimeType: 'image/png' }]
    expect(draft.value).toBe('hello')
    expect(attachments.value).toHaveLength(1)

    const again = useClarifyDraft(() => 'run-1', () => 'node-1')
    expect(again.draft.value).toBe('hello')

    const empty = useClarifyDraft('', () => null)
    expect(empty.draft.value).toBe('')
    empty.draft.value = 'nope'
    empty.attachments.value = [{ data: 'x', mimeType: 'image/png' }]
    expect(empty.draft.value).toBe('')
    expect(empty.attachments.value).toEqual([])
  })

  it('stores review annotations per run/node and dedups by ref', () => {
    const { annotations } = useClarifyDraft('run-ann', () => 'prop')
    expect(annotations.value).toEqual([])

    addClarifyAnnotation('run-ann', 'prop', { jsonPath: 'findings[r1]', label: 'A' })
    addClarifyAnnotation('run-ann', 'prop', { jsonPath: 'findings[r1]', label: 'A again' })
    addClarifyAnnotation('run-ann', 'prop', { selector: '#hero', label: 'Hero' })
    expect(annotations.value).toHaveLength(2)
    expect(annotations.value[0].jsonPath).toBe('findings[r1]')
    expect(annotations.value[1].selector).toBe('#hero')

    annotations.value = []
    expect(useClarifyDraft('run-ann', () => 'prop').annotations.value).toEqual([])

    addClarifyAnnotation('', 'prop', { jsonPath: 'x' })
    addClarifyAnnotation('run-ann', '', { jsonPath: 'x' })
    expect(annotations.value).toEqual([])
  })

  it('allows multiple quotes on the same path and soft-truncates', () => {
    const { annotations } = useClarifyDraft('run-quote', () => 'research')
    annotations.value = []

    expect(
      addClarifyAnnotation('run-quote', 'research', {
        jsonPath: 'summary',
        quote: 'first sentence',
      }),
    ).toBe('added')
    expect(
      addClarifyAnnotation('run-quote', 'research', {
        jsonPath: 'summary',
        quote: 'second sentence',
      }),
    ).toBe('added')
    expect(
      addClarifyAnnotation('run-quote', 'research', {
        jsonPath: 'summary',
        quote: 'first sentence',
      }),
    ).toBe('duplicate')
    expect(annotations.value).toHaveLength(2)

    const long = '字'.repeat(520)
    expect(
      addClarifyAnnotation('run-quote', 'research', { jsonPath: 'demo.long', quote: long }),
    ).toBe('added')
    const last = annotations.value[annotations.value.length - 1]
    expect(last.truncated).toBe(true)
    expect(last.quote?.length).toBe(500)
  })
})
