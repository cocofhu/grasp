import type { ReactOption, ReactQuestion } from '../shared/types'

/** Options that carry a non-empty demoHtml for iframe preview. */
export function demoOptionsOf(q: ReactQuestion): ReactOption[] {
  return q.options.filter((o) => !!o.demoHtml?.trim())
}

/**
 * Historical 1–3 demo count. Inline chat layout no longer branches on this;
 * a question renders at most the one selected preview.
 */
export function useSideBySide(demoOptions: ReactOption[]): boolean {
  return demoOptions.length > 0 && demoOptions.length <= 3
}

/** Tailwind grid column class for 1–3 side-by-side demos (responsive ≤640px → single col). */
export function demoGridColsClass(count: number): string {
  if (count <= 1) return 'grid-cols-1'
  if (count === 2) return 'grid-cols-1 sm:grid-cols-2'
  return 'grid-cols-1 sm:grid-cols-3'
}

/** Match selected option labels from a parsed choice summary row. */
export function matchSelectedLabels(
  q: ReactQuestion,
  selectedLabels: string[],
): ReactOption[] {
  if (!selectedLabels.length) return []
  const set = new Set(selectedLabels)
  return q.options.filter((o) => set.has(o.label))
}

/** First still-selected option with non-empty demoHtml, in option-list order. */
export function selectedDemoOption(
  q: ReactQuestion,
  selectedLabels: string[],
): ReactOption | null {
  const matched = matchSelectedLabels(q, selectedLabels)
  return matched.find((o) => !!o.demoHtml?.trim()) ?? null
}

export interface ChoiceEcho {
  prompt: string
  /** Hide the prompt when the only question already shows a preview card. */
  showPrompt: boolean
  cards: ReactOption[]
  texts: string[]
  question: ReactQuestion | null
}

/**
 * At most one preview card: the first selected option with a non-empty demo,
 * in option-list order. Further preview labels stay in the text answers.
 */
export function selectedPreviewOptions(q: ReactQuestion, answers: string[]): ReactOption[] {
  const first = selectedDemoOption(q, answers)
  return first ? [first] : []
}

/**
 * Pair each parsed choice row with the question it answers.
 * One selected preview becomes a card; every other answer, including extra
 * preview labels, stays readable text.
 * A single previewed question does not repeat its prompt.
 */
export function choiceEchoRows(
  rows: { q: string; answers: string[] }[],
  questions: ReactQuestion[],
): ChoiceEcho[] {
  const single = rows.length === 1
  return rows.map((row) => {
    const question = questions.find((q) => q.prompt === row.q) ?? null
    const cards = question ? selectedPreviewOptions(question, row.answers) : []
    const cardLabels = new Set(cards.map((card) => card.label))
    return {
      prompt: row.q,
      showPrompt: !(single && cards.length > 0),
      cards,
      texts: row.answers.filter((answer) => !cardLabels.has(answer)),
      question,
    }
  })
}
