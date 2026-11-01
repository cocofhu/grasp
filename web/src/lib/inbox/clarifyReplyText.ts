import type { ClarifyImage, ClarifyTurn, ReactAnnotation } from '@/lib/shared/types'

/** Choice prefixes the UI and models.FormatChoiceReply emit. Both are recognized in either locale. */
export const CHOICE_PREFIX_ZH = '我的选择:'
export const CHOICE_PREFIX_EN = 'My choices:'
/** zh-CN form-summary prefix, recognized regardless of the viewer's locale. */
export const FORM_PREFIX_ZH = '我的填写:'
/** Skip-envelope first line (zh / en) — not a choice reply. */
export const SKIP_PREFIX_ZH = '回答已跳过'
export const SKIP_USER_LABEL_ZH = '用户回复'
export const SKIP_PREFIX_EN = 'Answers skipped'
export const SKIP_USER_LABEL_EN = 'User reply'

export interface ChoiceRow {
  q: string
  answers: string[]
}

export interface FormRow {
  label: string
  value: string
}

/** Index of the latest agent turn that raised structured questions. */
export function latestQuestionTurnIndex(turnList: ClarifyTurn[]): number {
  for (let i = turnList.length - 1; i >= 0; i--) {
    const t = turnList[i]
    if (t.role === 'agent' && t.questions?.length) return i
  }
  return -1
}

/** Index of the latest agent turn that raised ask_form forms. */
export function latestFormTurnIndex(turnList: ClarifyTurn[]): number {
  for (let i = turnList.length - 1; i >= 0; i--) {
    const t = turnList[i]
    if (t.role === 'agent' && t.forms?.length) return i
  }
  return -1
}

/** Reply envelopes (choice / form / skip) in the viewer's locale plus the zh / en forms. */
export function createReplyText(translate: (key: string) => string) {
  const choicePrefix = () => translate('pages.clarify.choicePrefix')
  const formPrefix = () => translate('pages.clarify.formPrefix')
  const skipPrefix = () => translate('pages.clarify.skipPrefix')
  const skipUserReplyLabel = () => translate('pages.clarify.skipUserReplyLabel')

  function textHasChoicePrefix(text: string): boolean {
    return text.startsWith(choicePrefix()) || text.startsWith(CHOICE_PREFIX_ZH) || text.startsWith(CHOICE_PREFIX_EN)
  }

  function stripChoicePrefix(text: string): string {
    const p = choicePrefix()
    if (text.startsWith(p)) return text.slice(p.length)
    if (text.startsWith(CHOICE_PREFIX_ZH)) return text.slice(CHOICE_PREFIX_ZH.length)
    if (text.startsWith(CHOICE_PREFIX_EN)) return text.slice(CHOICE_PREFIX_EN.length)
    return text
  }

  function isChoiceReply(text: string): boolean {
    return !!text && textHasChoicePrefix(text)
  }

  function textHasFormPrefix(text: string): boolean {
    return text.startsWith(formPrefix()) || text.startsWith(FORM_PREFIX_ZH)
  }

  function stripFormPrefix(text: string): string {
    const p = formPrefix()
    if (text.startsWith(p)) return text.slice(p.length)
    if (text.startsWith(FORM_PREFIX_ZH)) return text.slice(FORM_PREFIX_ZH.length)
    return text
  }

  function isFormReply(text: string): boolean {
    return !!text && textHasFormPrefix(text)
  }

  function textHasSkipPrefix(text: string): boolean {
    if (!text) return false
    return (
      text.startsWith(skipPrefix() + '\n') ||
      text.startsWith(SKIP_PREFIX_ZH + '\n') ||
      text.startsWith(SKIP_PREFIX_EN + '\n')
    )
  }

  /** Three-line skip envelope: label / user-label / original (may be empty or multiline). */
  function formatSkipReply(userText: string): string {
    return `${skipPrefix()}\n${skipUserReplyLabel()}\n${userText}`
  }

  /**
   * A human turn / queue item that closes the current ask_question card:
   * structured choice, skip envelope, free text, or any attachment.
   */
  function closesQuestionRound(
    text: string,
    images?: ClarifyImage[] | null,
    anns?: ReactAnnotation[] | null,
  ): boolean {
    if (isChoiceReply(text) || isFormReply(text) || textHasSkipPrefix(text)) return true
    if ((images?.length || 0) > 0 || (anns?.length || 0) > 0) return true
    return !!(text || '').trim()
  }

  function hasHumanReplyAfter(turnList: ClarifyTurn[], qIdx: number): boolean {
    for (let i = qIdx + 1; i < turnList.length; i++) {
      const t = turnList[i]
      if (t.role === 'human' && closesQuestionRound(t.text, t.images, t.annotations)) return true
    }
    return false
  }

  // A submitted "我的选择:" / "My choices:" human turn is rendered as structured
  // cards instead of a raw markdown bullet list. A bullet may wrap: lines that
  // do not start another "- " stay in the same item, then the first "→" splits
  // the prompt from answers. Returns null for any other message.
  function parseChoiceSummary(text: string): ChoiceRow[] | null {
    if (!text || !textHasChoicePrefix(text)) return null
    const bodyText = stripChoicePrefix(text)
    const chunks: string[] = []
    let buf: string[] | null = null
    for (const raw of bodyText.split('\n')) {
      const line = raw.replace(/\r$/, '')
      const trimmed = line.trim()
      if (trimmed.startsWith('- ')) {
        if (buf) chunks.push(buf.join('\n'))
        buf = [trimmed.slice(2)]
        continue
      }
      if (buf) buf.push(line)
    }
    if (buf) chunks.push(buf.join('\n'))

    const rows: ChoiceRow[] = []
    for (const chunk of chunks) {
      const idx = chunk.indexOf('→')
      if (idx === -1) continue
      const q = chunk.slice(0, idx).trim()
      const answers = chunk
        .slice(idx + 1)
        .split('、')
        .map((a) => a.trim())
        .filter(Boolean)
      if (q) rows.push({ q, answers })
    }
    return rows.length ? rows : null
  }

  function parseFormSummary(text: string): FormRow[] | null {
    if (!text || !textHasFormPrefix(text)) return null
    const bodyText = stripFormPrefix(text)
    const rows: FormRow[] = []
    for (const raw of bodyText.split('\n')) {
      const line = raw.trim()
      if (!line.startsWith('- ')) continue
      const body = line.slice(2)
      const idx = body.indexOf('→')
      if (idx === -1) continue
      const label = body.slice(0, idx).trim()
      const value = body.slice(idx + 1).trim()
      if (label) rows.push({ label, value })
    }
    return rows.length ? rows : null
  }

  return {
    choicePrefix,
    formPrefix,
    skipPrefix,
    skipUserReplyLabel,
    textHasChoicePrefix,
    stripChoicePrefix,
    isChoiceReply,
    textHasSkipPrefix,
    isSkipReply: textHasSkipPrefix,
    formatSkipReply,
    closesQuestionRound,
    hasHumanReplyAfter,
    parseChoiceSummary,
    parseFormSummary,
  }
}
