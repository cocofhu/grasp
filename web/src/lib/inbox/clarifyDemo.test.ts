import { describe, expect, it } from 'vitest'
import {
  choiceEchoRows,
  demoOptionsOf,
  matchSelectedLabels,
  restoreChoiceAnswers,
  selectedDemoOption,
  selectedPreviewOptions,
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

const chipLabel = '留在输入框上方，改成和项目标签、文件芯片一样的单行芯片'
const scopeLabel = '输入区、已发送消息、反馈记录一起改'

describe('restoreChoiceAnswers', () => {
  it('keeps a long label that equals shorter labels joined by the separator', () => {
    const long = '方案 A、方案 B'
    expect(restoreChoiceAnswers(['方案 A', '方案 B'], [long, '方案 A', '方案 B'])).toEqual([long])
    expect(restoreChoiceAnswers(['方案 A', '方案 B', '自己写的'], [long, '方案 A', '方案 B'])).toEqual([
      long,
      '自己写的',
    ])
  })

  it('keeps custom text that contains the separator when no label matches inside it', () => {
    expect(restoreChoiceAnswers(['不是', '选项'], ['方案 A'])).toEqual(['不是、选项'])
    expect(restoreChoiceAnswers(['方案 A', '自己写', '一段'], ['方案 A', '方案 B'])).toEqual([
      '方案 A',
      '自己写、一段',
    ])
  })
})

describe('selectedPreviewOptions', () => {
  it('keeps the first selected preview in option order and skips blanks', () => {
    expect(selectedPreviewOptions(q, ['D', 'B', 'A', 'D']).map((o) => o.id)).toEqual(['a'])
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
    expect(echoes[0]?.cards.map((card) => card.id)).toEqual(['a'])
    expect(echoes[0]?.texts).toEqual(['B'])
    expect(echoes[1]?.showPrompt).toBe(true)
    expect(echoes[1]?.cards).toEqual([])
    expect(echoes[1]?.texts).toEqual(['左'])
  })

  it('keeps extra selected preview labels in the text answer', () => {
    const [echo] = choiceEchoRows([{ q: '布局', answers: ['B', 'A', '自己写'] }], [previewQ])
    expect(echo?.cards.map((card) => card.id)).toEqual(['a'])
    expect(echo?.texts).toEqual(['B', '自己写'])
    expect(echo?.showPrompt).toBe(false)
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

  it('renders one preview card for a label that contains the separator', () => {
    const placed = questionWith(chipLabel, '<p>chip</p>')
    const [echo] = choiceEchoRows(
      [{ q: '芯片放哪', answers: chipLabel.split('、') }],
      [placed],
    )
    expect(echo?.cards.map((card) => card.label)).toEqual([chipLabel])
    expect(echo?.texts).toEqual([])
    expect(echo?.showPrompt).toBe(false)
  })

  it('keeps a separator-bearing label without a preview as one sentence', () => {
    const scope: ReactQuestion = {
      id: 'scope',
      prompt: '改哪些',
      options: [
        { id: 'all', label: scopeLabel },
        { id: 'input', label: '只改输入区' },
      ],
    }
    const [echo] = choiceEchoRows(
      [{ q: '改哪些', answers: scopeLabel.split('、') }],
      [scope],
    )
    expect(echo?.cards).toEqual([])
    expect(echo?.texts).toEqual([scopeLabel])
    expect(echo?.showPrompt).toBe(true)
  })

  it('still separates plain multi-select labels from custom text', () => {
    const [echo] = choiceEchoRows(
      [{ q: '布局', answers: ['方案 A', '方案 B', '自己写的'] }],
      [{
        ...previewQ,
        options: [
          { id: 'a', label: '方案 A', demoHtml: '<p>a</p>' },
          { id: 'b', label: '方案 B', demoHtml: '<p>b</p>' },
          { id: 'c', label: '方案 C' },
        ],
      }],
    )
    expect(echo?.cards.map((card) => card.id)).toEqual(['a'])
    expect(echo?.texts).toEqual(['方案 B', '自己写的'])
  })

  it('prefers one long label over the shorter labels it is made of', () => {
    const long = '方案 A、方案 B'
    const [echo] = choiceEchoRows(
      [{ q: '布局', answers: ['方案 A', '方案 B'] }],
      [{
        id: 'layout',
        prompt: '布局',
        options: [
          { id: 'long', label: long, demoHtml: '<p>long</p>' },
          { id: 'a', label: '方案 A', demoHtml: '<p>a</p>' },
          { id: 'b', label: '方案 B', demoHtml: '<p>b</p>' },
        ],
      }],
    )
    expect(echo?.cards.map((card) => card.id)).toEqual(['long'])
    expect(echo?.texts).toEqual([])
  })
})

function questionWith(label: string, demoHtml: string): ReactQuestion {
  return {
    id: 'place',
    prompt: '芯片放哪',
    options: [
      { id: 'chip', label, demoHtml },
      { id: 'above', label: '放在消息列表上方', demoHtml: '<p>above</p>' },
      { id: 'menu', label: '收进输入框里的菜单', demoHtml: '<p>menu</p>' },
    ],
  }
}
