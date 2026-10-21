import type { ReactOption, ReactQuestion } from '../shared/types'

/** Options that carry a non-empty demoHtml for iframe preview. */
export function demoOptionsOf(q: ReactQuestion): ReactOption[] {
  return q.options.filter((o) => !!o.demoHtml?.trim())
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

/** Same separator used when a submitted choice joins several answers into one line. */
const CHOICE_ANSWER_SEPARATOR = '、'

function labelMatchesAt(answer: string, index: number, label: string): boolean {
  if (!label || index >= answer.length || !answer.startsWith(label, index)) return false
  const after = index + label.length
  return after === answer.length || answer[after] === CHOICE_ANSWER_SEPARATOR
}

/**
 * Turn the arrow-side answer back into complete option labels and custom text.
 * Fragments were split on every separator, so they are joined first. Labels are
 * tried longest-first, and a label is taken only when the separator or the end
 * follows it. A long label that equals several shorter labels joined by the
 * separator wins. Text that matches no label stays one piece, even if it
 * contains the separator.
 */
export function restoreChoiceAnswers(answers: string[], labels: string[]): string[] {
  const answer = answers.join(CHOICE_ANSWER_SEPARATOR).trim()
  if (!answer) return []
  const sorted = [...new Set(labels.filter((label) => label.length > 0))].sort(
    (a, b) => b.length - a.length,
  )
  const parts: string[] = []
  let i = 0
  while (i < answer.length) {
    if (answer[i] === CHOICE_ANSWER_SEPARATOR) {
      i += 1
      continue
    }
    const matched = sorted.find((label) => labelMatchesAt(answer, i, label))
    if (matched) {
      parts.push(matched)
      i += matched.length
      continue
    }
    let end = answer.length
    for (let j = i + 1; j < answer.length; j++) {
      if (answer[j] !== CHOICE_ANSWER_SEPARATOR) continue
      if (sorted.some((label) => labelMatchesAt(answer, j + 1, label))) {
        end = j
        break
      }
    }
    const custom = answer.slice(i, end).trim()
    if (custom) parts.push(custom)
    i = end
  }
  return parts
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
    const answers = question
      ? restoreChoiceAnswers(row.answers, question.options.map((option) => option.label))
      : row.answers
    const cards = question ? selectedPreviewOptions(question, answers) : []
    const cardLabels = new Set(cards.map((card) => card.label))
    return {
      prompt: row.q,
      showPrompt: !(single && cards.length > 0),
      cards,
      texts: answers.filter((answer) => !cardLabels.has(answer)),
      question,
    }
  })
}
