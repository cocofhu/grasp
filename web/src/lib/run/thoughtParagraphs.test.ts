import { describe, expect, it } from 'vitest'
import { thoughtParagraphs } from './thoughtParagraphs'

describe('thoughtParagraphs', () => {
  it('splits on blank lines and drops empty blocks', () => {
    expect(thoughtParagraphs('一段\n\n二段\n\n\n三段')).toEqual(['一段', '二段', '三段'])
  })

  it('collapses single newlines inside a paragraph', () => {
    expect(thoughtParagraphs('上一行\n下一行')).toEqual(['上一行 下一行'])
  })

  it('returns nothing for blank thought', () => {
    expect(thoughtParagraphs('')).toEqual([])
    expect(thoughtParagraphs('  \n\n  ')).toEqual([])
  })
})
