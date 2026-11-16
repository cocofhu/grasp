import { nextTick, ref } from 'vue'
import { describe, expect, it } from 'vitest'
import type { ReactForm, ReactQuestion } from '@/lib/shared/types'
import { useFormDraft, useQuestionDeck } from './clarifyAnswers'

const db: ReactQuestion = { id: 'db', prompt: '用哪个数据库?', options: [{ id: 'pg', label: 'Postgres', recommended: true }, { id: 'my', label: 'MySQL' }] }
const langs: ReactQuestion = { id: 'lang', prompt: '支持哪些语言?', allowMultiple: true, options: [{ id: 'zh', label: '中文' }, { id: 'en', label: 'English' }] }

describe('useQuestionDeck', () => {
  it('preselects the recommendation and summarizes answers in the choice format', () => {
    const deck = useQuestionDeck(ref([db, langs]))
    expect(deck.isSelected('db', 'pg')).toBe(true)
    deck.toggle(db, 'my')
    deck.toggle(langs, 'zh')
    deck.toggle(langs, 'en')
    deck.toggleOther(langs)
    deck.other.value.lang = '日本語'
    expect(deck.summary('我的选择:')).toBe('我的选择:\n- 用哪个数据库? → MySQL\n- 支持哪些语言? → 中文、English、日本語')
  })

  it('lets a checked 其他 replace a single choice and skips unanswered questions', () => {
    const deck = useQuestionDeck(ref([db, langs]))
    deck.other.value.db = 'SQLite'
    expect(deck.summary('P')).toBe('P\n- 用哪个数据库? → Postgres')
    deck.toggleOther(db)
    expect(deck.summary('P')).toBe('P\n- 用哪个数据库? → SQLite')
  })

  it('resets to the first card only when the question ids change', async () => {
    const questions = ref([db, langs])
    const deck = useQuestionDeck(questions)
    deck.nextCard()
    expect(deck.curQuestion.value?.id).toBe('lang')
    questions.value = [{ ...db }, { ...langs }]
    await nextTick()
    expect(deck.step.value).toBe(1)
    questions.value = [langs]
    await nextTick()
    expect(deck.step.value).toBe(0)
  })
})

describe('useFormDraft', () => {
  it('is filled only when every field has a value, seeding field defaults', () => {
    const forms = ref<ReactForm[]>([{ fields: [{ name: 'url', label: '地址', type: 'url', value: 'https://example.com' }, { name: 'user', label: '账号' }] }])
    const draft = useFormDraft(forms)
    expect(draft.values.value[draft.fieldKey(0, 'url')]).toBe('https://example.com')
    expect(draft.filled.value).toBe(false)
    draft.values.value[draft.fieldKey(0, 'user')] = 'ada'
    expect(draft.filled.value).toBe(true)
    expect(draft.summary('我的填写:')).toBe('我的填写:\n- 地址 → https://example.com\n- 账号 → ada')
  })
})
