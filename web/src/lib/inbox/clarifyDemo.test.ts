import { describe, expect, it } from 'vitest'
import {
  choiceEchoRows,
  demoGridColsClass,
  demoOptionsOf,
  matchSelectedLabels,
  selectedDemoOption,
  selectedPreviewOptions,
  useSideBySide,
} from './clarifyDemo'
import type { ReactQuestion } from '../shared/types'

const q: ReactQuestion = {
  id: 'q1',
  prompt: '布局?',
  options: [
    { id: 'a', label: 'A', demoHtml: '<!doctype html><html></html>' },
    { id: 'b', label: 'B' },
    { id: 'c', label: 'C', demoHtml: '  ' },
    { id: 'd', label: 'D', demoHtml: '<!doctype html><html><body>x</body></html>' },
  ],
}

describe('demoOptionsOf', () => {
  it('keeps only non-empty demoHtml', () => {
    expect(demoOptionsOf(q).map((o) => o.id)).toEqual(['a', 'd'])
  })
})

describe('useSideBySide', () => {
  it('is true for 1–3 demos', () => {
    expect(useSideBySide(demoOptionsOf(q))).toBe(true)
  })
  it('is false for >3 demos', () => {
    const many = Array.from({ length: 4 }, (_, i) => ({
      id: `o${i}`,
      label: `L${i}`,
      demoHtml: '<!doctype html><html></html>',
    }))
    expect(useSideBySide(many)).toBe(false)
  })
  it('is false when no demos', () => {
    expect(useSideBySide([])).toBe(false)
  })
})

describe('demoGridColsClass', () => {
  it('adapts column count', () => {
    expect(demoGridColsClass(1)).toContain('grid-cols-1')
    expect(demoGridColsClass(2)).toContain('sm:grid-cols-2')
    expect(demoGridColsClass(3)).toContain('sm:grid-cols-3')
  })
})

describe('matchSelectedLabels', () => {
  it('matches by label', () => {
    const matched = matchSelectedLabels(q, ['A', 'D'])
    expect(matched.map((o) => o.id)).toEqual(['a', 'd'])
  })
  it('returns empty for no match', () => {
    expect(matchSelectedLabels(q, ['Z'])).toEqual([])
  })
})

describe('selectedDemoOption', () => {
  it('returns first selected option with demo', () => {
    const opt = selectedDemoOption(q, ['B', 'D'])
    expect(opt?.id).toBe('d')
  })
  it('returns null when selected has no demo', () => {
    expect(selectedDemoOption(q, ['B'])).toBeNull()
  })
})

describe('selectedPreviewOptions', () => {
  it('keeps every selected preview in answer order and skips blanks', () => {
    expect(selectedPreviewOptions(q, ['D', 'B', 'A', 'D']).map((o) => o.id)).toEqual(['d', 'a'])
  })
})

describe('choiceEchoRows', () => {
  const previewQ: ReactQuestion = {
    id: 'layout',
    prompt: '布局',
    options: [
      { id: 'a', label: 'A', demoHtml: '<p>a</p>' },
      { id: 'b', label: 'B', demoHtml: '<p>b</p>' },
      { id: 'c', label: 'C' },
    ],
  }

  it('hides the prompt for a single previewed question and keeps custom text', () => {
    const [echo] = choiceEchoRows([{ q: '布局', answers: ['A', '自己写'] }], [previewQ])
    expect(echo?.showPrompt).toBe(false)
    expect(echo?.cards.map((card) => card.id)).toEqual(['a'])
    expect(echo?.texts).toEqual(['自己写'])
  })

  it('keeps a prompt above each group when several questions were answered', () => {
    const plain: ReactQuestion = { id: 'p', prompt: '文字', options: [{ id: 'l', label: '左' }] }
    const echoes = choiceEchoRows(
      [
        { q: '布局', answers: ['B', 'A'] },
        { q: '文字', answers: ['左'] },
      ],
      [previewQ, plain],
    )
    expect(echoes[0]?.showPrompt).toBe(true)
    expect(echoes[0]?.cards.map((card) => card.id)).toEqual(['b', 'a'])
    expect(echoes[0]?.texts).toEqual([])
    expect(echoes[1]?.showPrompt).toBe(true)
    expect(echoes[1]?.cards).toEqual([])
    expect(echoes[1]?.texts).toEqual(['左'])
  })

  it('does not invent a card when the answer matches nothing', () => {
    const [echo] = choiceEchoRows([{ q: '布局', answers: ['没有'] }], [previewQ])
    expect(echo?.cards).toEqual([])
    expect(echo?.showPrompt).toBe(true)
    expect(echo?.texts).toEqual(['没有'])
    const [unknown] = choiceEchoRows([{ q: '别的', answers: ['A'] }], [previewQ])
    expect(unknown?.question).toBeNull()
    expect(unknown?.cards).toEqual([])
    expect(unknown?.texts).toEqual(['A'])
  })
})
